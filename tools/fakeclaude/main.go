// Command fakeclaude stands in for Claude Code in end-to-end tests
// (scripts/e2e.sh), so they run without an account or a network. It draws
// what prompt-engine reads from claude's screen, as recorded from claude
// 2.1.308:
//
//   - the input box: a "❯ " line between two rules of "─";
//   - after each reply, a suggested next prompt, faint, in the empty box,
//     which → or Tab accepts;
//   - bracketed paste, which prompt-engine uses to append rules.
//
// Each submitted prompt is appended to the -log file as one JSON string
// per line, so a test can check exactly what claude would have received.
//
//	fakeclaude [-log file] [-suggest "show me main.go"]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/term"
)

const (
	pasteStart = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
)

type fake struct {
	log        *os.File
	suggestion string // offered after each reply
	offered    string // shown now, in the empty box
	text       []rune
	transcript []string
	inPaste    bool
}

func main() {
	logPath := flag.String("log", "", "append each submitted prompt here, as a JSON string per line")
	suggest := flag.String("suggest", "show me main.go", "the suggested next prompt shown after a reply")
	flag.Parse()

	f := &fake{suggestion: *suggest}
	if *logPath != "" {
		file, err := os.OpenFile(*logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fakeclaude:", err)
			os.Exit(1)
		}
		defer file.Close()
		f.log = file
	}

	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err == nil {
		defer term.Restore(int(os.Stdin.Fd()), state)
	}
	fmt.Print("\x1b[?2004h") // bracketed paste on
	defer fmt.Print("\x1b[?2004l\x1b[2J\x1b[H")

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	go func() {
		for range winch {
			f.draw()
		}
	}()

	f.draw()
	buf := make([]byte, 4096)
	pending := ""
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return
		}
		pending += string(buf[:n])
		var quit bool
		pending, quit = f.input(pending)
		if quit {
			return
		}
		f.draw()
	}
}

// input applies what it can of in, and returns what's left of an
// incomplete escape sequence, and whether to exit.
func (f *fake) input(in string) (rest string, quit bool) {
	for in != "" {
		if f.inPaste {
			end := strings.Index(in, pasteEnd)
			if end < 0 {
				f.insert(in)
				return "", false
			}
			f.insert(in[:end])
			in, f.inPaste = in[end+len(pasteEnd):], false
			continue
		}
		switch {
		case strings.HasPrefix(in, pasteStart):
			in, f.inPaste = in[len(pasteStart):], true
		case strings.HasPrefix(in, "\x1b[C"): // →
			f.accept()
			in = in[3:]
		case in[0] == 0x1b:
			// Other escape sequences (arrows, terminal replies): skip a
			// CSI sequence whole, wait for the rest of an incomplete one.
			if len(in) < 2 {
				return in, false
			}
			if in[1] != '[' {
				in = in[2:]
				continue
			}
			i := 2
			for i < len(in) && (in[i] < 0x40 || in[i] > 0x7e) {
				i++
			}
			if i == len(in) {
				return in, false
			}
			in = in[i+1:]
		case in[0] == '\t':
			f.accept()
			in = in[1:]
		case in[0] == '\r':
			f.submit()
			in = in[1:]
		case in[0] == 0x7f || in[0] == 0x08:
			if len(f.text) > 0 {
				f.text = f.text[:len(f.text)-1]
			}
			in = in[1:]
		case in[0] == 0x03: // Ctrl+C clears the prompt
			f.text, f.offered = nil, ""
			in = in[1:]
		case in[0] == 0x04: // Ctrl+D exits
			return "", true
		case in[0] < 0x20:
			in = in[1:]
		default:
			r, size := utf8.DecodeRuneInString(in)
			f.insert(string(r))
			in = in[size:]
		}
	}
	return "", false
}

func (f *fake) insert(s string) {
	f.text = append(f.text, []rune(strings.ReplaceAll(s, "\r", "\n"))...)
	f.offered = ""
}

// accept takes the offered suggestion, as → and Tab do in claude.
func (f *fake) accept() {
	if len(f.text) == 0 && f.offered != "" {
		f.text, f.offered = []rune(f.offered), ""
	}
}

func (f *fake) submit() {
	sent := string(f.text)
	if strings.TrimSpace(sent) == "" {
		return
	}
	if f.log != nil {
		line, _ := json.Marshal(sent)
		fmt.Fprintf(f.log, "%s\n", line)
	}
	first, _, _ := strings.Cut(sent, "\n")
	f.transcript = append(f.transcript, "❯ "+first, "● Reply to: "+first, "")
	f.text, f.offered = nil, f.suggestion
}

// draw repaints the screen: the transcript at the top, the input box at the
// bottom, the cursor in the box.
func (f *fake) draw() {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w < 10 || h < 6 {
		w, h = 80, 24
	}
	var b strings.Builder
	b.WriteString("\x1b[?25l\x1b[2J\x1b[H")
	room := h - 4
	lines := f.transcript
	if len(lines) > room {
		lines = lines[len(lines)-room:]
	}
	for i, l := range lines {
		fmt.Fprintf(&b, "\x1b[%d;1H%s", i+1, l)
	}
	rule := strings.Repeat("─", w)
	shown := strings.ReplaceAll(string(f.text), "\n", "↵")
	fmt.Fprintf(&b, "\x1b[%d;1H%s\x1b[%d;1H❯ %s", h-3, rule, h-2, shown)
	if len(f.text) == 0 && f.offered != "" {
		fmt.Fprintf(&b, "\x1b[2m%s\x1b[22m", f.offered)
	}
	fmt.Fprintf(&b, "\x1b[%d;1H%s", h-1, rule)
	fmt.Fprintf(&b, "\x1b[%d;%dH\x1b[?25h", h-2, 3+utf8.RuneCountInString(shown))
	os.Stdout.WriteString(b.String())
}
