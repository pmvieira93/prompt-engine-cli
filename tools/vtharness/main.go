// Command vtharness drives a terminal program under a PTY from a script on
// stdin, for latency, memory, and rendering checks. It mirrors the program's output into an
// x/vt emulator so the screen a user would see can be dumped.
//
//	vtharness [-cols 100] [-rows 30] -- prog args... < script
//
// Script lines:
//
//	sleep 500ms
//	send "text\r"                 Go-quoted bytes to type
//	wait "text" 5s                wait until the screen contains text
//	dump                          print the screen, cursor row marked
//	echolat 50                    type 50 distinct chars, report echo latency
//	ghostlat 20 "impl" "ement"    type a prefix, time until the suffix renders
//	rss                           resident memory of the program (not its children)
//	cpu 5s                        CPU used by the program over the interval
//	reply on|off                  answer DA1 / kitty / XTVERSION queries like a real terminal
//	resize 80 24                  resize the terminal (sends SIGWINCH)
//	seen "\x1b[>5u"               count occurrences of bytes in raw output so far
//	exit                          wait for the program to exit and print its code
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/vt"
	cpty "github.com/creack/pty"
)

type harness struct {
	ptmx *os.File
	cmd  *exec.Cmd
	emu  *vt.SafeEmulator

	mu    sync.Mutex
	cond  *sync.Cond
	out   []byte
	reply bool
}

func main() {
	cols := flag.Int("cols", 100, "terminal width")
	rows := flag.Int("rows", 30, "terminal height")
	flag.Parse()

	h := &harness{emu: vt.NewSafeEmulator(*cols, *rows)}
	h.cond = sync.NewCond(&h.mu)
	h.cmd = exec.Command(flag.Arg(0), flag.Args()[1:]...)
	h.cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	var err error
	h.ptmx, err = cpty.StartWithSize(h.cmd, &cpty.Winsize{Cols: uint16(*cols), Rows: uint16(*rows)})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	go io.Copy(io.Discard, h.emu) // our emulator's own replies are not sent anywhere
	go h.readLoop()

	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := h.run(line); err != nil {
			fmt.Printf("ERROR %s: %v\n", line, err)
		}
	}
	_ = h.cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { h.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = h.cmd.Process.Kill()
	}
}

func (h *harness) readLoop() {
	buf := make([]byte, 64*1024)
	for {
		n, err := h.ptmx.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			h.emu.Write(chunk)
			h.mu.Lock()
			h.out = append(h.out, chunk...)
			reply := h.reply
			h.cond.Broadcast()
			h.mu.Unlock()
			if reply {
				h.answer(chunk)
			}
		}
		if err != nil {
			h.mu.Lock()
			h.cond.Broadcast()
			h.mu.Unlock()
			return
		}
	}
}

// answer mimics iTerm2 for the queries prompt-engine forwards.
func (h *harness) answer(chunk []byte) {
	if bytes.Contains(chunk, []byte("\x1b[?u")) {
		h.ptmx.WriteString("\x1b[?0u")
	}
	if bytes.Contains(chunk, []byte("\x1b[>0q")) {
		h.ptmx.WriteString("\x1bP>|vtharness 1.0\x1b\\")
	}
	if bytes.Contains(chunk, []byte("\x1b[c")) {
		h.ptmx.WriteString("\x1b[?62;4c")
	}
}

func (h *harness) mark() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.out)
}

// waitBytes waits until want appears in output written after offset from.
func (h *harness) waitBytes(from int, want []byte, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	go func() { time.Sleep(timeout); h.mu.Lock(); h.cond.Broadcast(); h.mu.Unlock() }()
	h.mu.Lock()
	defer h.mu.Unlock()
	for !bytes.Contains(h.out[from:], want) {
		if time.Now().After(deadline) {
			return false
		}
		h.cond.Wait()
	}
	return true
}

func (h *harness) run(line string) error {
	verb, rest, _ := strings.Cut(line, " ")
	args := splitArgs(rest)
	switch verb {
	case "sleep":
		d, err := time.ParseDuration(args[0])
		if err != nil {
			return err
		}
		time.Sleep(d)
	case "send":
		s, err := strconv.Unquote(args[0])
		if err != nil {
			return err
		}
		_, err = h.ptmx.WriteString(s)
		return err
	case "reply":
		h.mu.Lock()
		h.reply = args[0] == "on"
		h.mu.Unlock()
	case "wait":
		s, _ := strconv.Unquote(args[0])
		d, _ := time.ParseDuration(args[1])
		deadline := time.Now().Add(d)
		for !strings.Contains(h.emu.String(), s) {
			if time.Now().After(deadline) {
				return fmt.Errorf("timed out waiting for %q", s)
			}
			time.Sleep(10 * time.Millisecond)
		}
	case "dump":
		h.dump()
	case "echolat":
		n, _ := strconv.Atoi(args[0])
		return h.echoLatency(n)
	case "ghostlat":
		n, _ := strconv.Atoi(args[0])
		prefix, _ := strconv.Unquote(args[1])
		suffix, _ := strconv.Unquote(args[2])
		return h.ghostLatency(n, prefix, suffix)
	case "rss":
		out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(h.cmd.Process.Pid)).Output()
		if err != nil {
			return err
		}
		kb, _ := strconv.Atoi(strings.TrimSpace(string(out)))
		fmt.Printf("RSS %.1f MB\n", float64(kb)/1024)
	case "cpu":
		d, _ := time.ParseDuration(args[0])
		before := h.cpuTime()
		time.Sleep(d)
		used := h.cpuTime() - before
		fmt.Printf("CPU %s over %s (%.1f%% of one core)\n", used, d, 100*used.Seconds()/d.Seconds())
	case "resize":
		c, _ := strconv.Atoi(args[0])
		r, _ := strconv.Atoi(args[1])
		h.emu.Resize(c, r)
		return cpty.Setsize(h.ptmx, &cpty.Winsize{Cols: uint16(c), Rows: uint16(r)})
	case "seen":
		s, _ := strconv.Unquote(args[0])
		h.mu.Lock()
		n := bytes.Count(h.out, []byte(s))
		h.mu.Unlock()
		fmt.Printf("SEEN %q x%d\n", s, n)
	case "exit":
		done := make(chan error, 1)
		go func() { done <- h.cmd.Wait() }()
		select {
		case err := <-done:
			fmt.Printf("EXIT %v (code %d)\n", err, h.cmd.ProcessState.ExitCode())
			time.Sleep(100 * time.Millisecond) // let the reader drain final output
		case <-time.After(10 * time.Second):
			return fmt.Errorf("program did not exit")
		}
	default:
		return fmt.Errorf("unknown command")
	}
	return nil
}

func (h *harness) cpuTime() time.Duration {
	out, err := exec.Command("ps", "-o", "time=", "-p", strconv.Itoa(h.cmd.Process.Pid)).Output()
	if err != nil {
		return 0
	}
	// Format: [hh:]mm:ss.cc
	parts := strings.Split(strings.TrimSpace(string(out)), ":")
	var d time.Duration
	for _, p := range parts {
		f, _ := strconv.ParseFloat(p, 64)
		d = d*60 + time.Duration(f*float64(time.Second))
	}
	return d
}

func (h *harness) dump() {
	pos := h.emu.CursorPosition()
	fmt.Printf("--- screen cursor=(%d,%d) hidden=%v\n", pos.X, pos.Y, h.emu.CursorHidden())
	for y, l := range strings.Split(h.emu.String(), "\n") {
		if strings.TrimSpace(l) == "" && y != pos.Y {
			continue
		}
		m := " "
		if y == pos.Y {
			m = ">"
		}
		fmt.Printf("%s%2d|%s\n", m, y, strings.TrimRight(l, " "))
	}
	fmt.Println("---")
}

// echoLatency types n distinct non-ASCII characters, which can't be confused
// with escape-sequence bytes, and times each one's first appearance.
func (h *harness) echoLatency(n int) error {
	chars := []rune("éñüøåçßþðæ")
	var lat []time.Duration
	for i := range n {
		c := string(chars[i%len(chars)])
		from := h.mark()
		start := time.Now()
		h.ptmx.WriteString(c)
		if !h.waitBytes(from, []byte(c), 2*time.Second) {
			return fmt.Errorf("char %d (%q) never echoed", i, c)
		}
		lat = append(lat, time.Since(start))
		time.Sleep(30 * time.Millisecond)
	}
	report("echo", lat)
	return nil
}

func (h *harness) ghostLatency(n int, prefix, suffix string) error {
	var lat []time.Duration
	for i := range n {
		from := h.mark()
		start := time.Now()
		h.ptmx.WriteString(prefix)
		if !h.waitBytes(from, []byte(suffix), 3*time.Second) {
			return fmt.Errorf("round %d: suffix %q never rendered", i, suffix)
		}
		lat = append(lat, time.Since(start))
		h.ptmx.WriteString("\x1b") // dismiss
		time.Sleep(50 * time.Millisecond)
		h.ptmx.WriteString(strings.Repeat("\x7f", len([]rune(prefix))))
		time.Sleep(300 * time.Millisecond)
	}
	report("ghost", lat)
	return nil
}

func report(name string, lat []time.Duration) {
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	p := func(q float64) time.Duration { return lat[int(q*float64(len(lat)-1))].Round(10 * time.Microsecond) }
	fmt.Printf("%s latency n=%d p50=%s p95=%s max=%s\n", name, len(lat), p(0.5), p(0.95), lat[len(lat)-1].Round(10*time.Microsecond))
}

// splitArgs splits on spaces outside double quotes.
func splitArgs(s string) []string {
	var args []string
	var cur strings.Builder
	inQ, esc := false, false
	for _, r := range s {
		switch {
		case esc:
			esc = false
		case r == '\\' && inQ:
			esc = true
		case r == '"':
			inQ = !inQ
		case r == ' ' && !inQ:
			if cur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		args = append(args, cur.String())
	}
	return args
}
