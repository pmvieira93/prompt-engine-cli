// Package pty runs a child process inside a pseudo-terminal and proxies the
// user's terminal to it, passing every input chunk through an optional Filter
// so keystrokes can be intercepted before the child sees them.
package pty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	cpty "github.com/creack/pty"
	"golang.org/x/term"
)

// DefaultDrainTimeout bounds how long Run keeps copying output after the child
// exits. A grandchild that inherited the PTY can keep it open indefinitely.
const DefaultDrainTimeout = 250 * time.Millisecond

// Filter transforms raw input bytes before they are written to the child.
// It runs on the input goroutine and must not block. The returned slice may
// alias p; it is consumed before the next read.
type Filter interface {
	Filter(p []byte) []byte
}

// FilterFunc adapts a function to the Filter interface.
type FilterFunc func(p []byte) []byte

func (f FilterFunc) Filter(p []byte) []byte { return f(p) }

// Session proxies a terminal to a child process running in a PTY.
type Session struct {
	// Stdin and Stdout default to os.Stdin and os.Stdout. When Stdin is a
	// terminal it is switched to raw mode and its size is mirrored to the PTY.
	Stdin  io.Reader
	Stdout io.Writer

	// Filter intercepts input. Nil means pass-through.
	Filter Filter
	// OnFilterError is called at most once, if Filter panics. The filter is
	// then disabled and input passes through untouched for the rest of the
	// session, so a broken interceptor never locks the user out.
	OnFilterError func(error)

	// DrainTimeout overrides DefaultDrainTimeout.
	DrainTimeout time.Duration

	// OnStart, if set, receives the PTY master once the child is running and
	// before I/O starts, e.g. to answer terminal queries or resize the PTY.
	OnStart func(ptmx *os.File)
	// ManualSize stops Run from mirroring the terminal size to the PTY on
	// SIGWINCH; the caller resizes the PTY itself (see OnStart).
	ManualSize bool
	// StartSize, if set, gives the PTY's size at start instead of the
	// terminal's: e.g. narrower, when a side panel is open from the start.
	StartSize func() (cols, rows int)
	// KeepOutputProcessing leaves OPOST|ONLCR on in raw mode, for callers
	// that render to the terminal themselves and write bare LFs (Bubble Tea
	// does when it doesn't own the input tty).
	KeepOutputProcessing bool

	filterOff atomic.Bool
}

// Run starts cmd in a new PTY, proxies I/O until it exits, and returns its
// exit code (128+N if it was killed by signal N). The caller's terminal is
// restored before Run returns.
func (s *Session) Run(cmd *exec.Cmd) (int, error) {
	stdin, stdout := s.Stdin, s.Stdout
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}

	tty, _ := stdin.(*os.File)
	if tty != nil && !term.IsTerminal(int(tty.Fd())) {
		tty = nil
	}

	var size *cpty.Winsize
	switch {
	case s.StartSize != nil:
		cols, rows := s.StartSize()
		size = &cpty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}
	case tty != nil:
		size, _ = cpty.GetsizeFull(tty)
	}
	ptmx, err := cpty.StartWithSize(cmd, size)
	if err != nil {
		return 0, fmt.Errorf("start %s: %w", cmd.Path, err)
	}
	defer ptmx.Close()
	if s.OnStart != nil {
		s.OnStart(ptmx)
	}

	if tty != nil {
		state, err := term.MakeRaw(int(tty.Fd()))
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return 0, fmt.Errorf("set raw mode: %w", err)
		}
		defer term.Restore(int(tty.Fd()), state)
		if s.KeepOutputProcessing {
			_ = enableOutputProcessing(int(tty.Fd()))
		}
		if !s.ManualSize {
			defer mirrorSize(tty, ptmx)()
		}
	}
	defer forwardSignals(cmd.Process)()

	outDone := make(chan struct{})
	go func() {
		// EIO on Linux / EOF on macOS once the child side closes.
		_, _ = io.Copy(stdout, ptmx)
		close(outDone)
	}()
	// The input goroutine may stay blocked in Read after Run returns; it
	// exits on its next read or write error.
	go s.pumpInput(ptmx, stdin)

	waitErr := cmd.Wait()

	drain := s.DrainTimeout
	if drain <= 0 {
		drain = DefaultDrainTimeout
	}
	select {
	case <-outDone:
	case <-time.After(drain):
		ptmx.Close()
		<-outDone
	}

	return exitCode(waitErr)
}

func (s *Session) pumpInput(dst io.Writer, src io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(s.filter(buf[:n])); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// filter applies s.Filter, falling back to pass-through permanently if it panics.
func (s *Session) filter(p []byte) (out []byte) {
	if s.Filter == nil || s.filterOff.Load() {
		return p
	}
	defer func() {
		if r := recover(); r != nil {
			s.filterOff.Store(true)
			if s.OnFilterError != nil {
				s.OnFilterError(fmt.Errorf("input filter panicked: %v", r))
			}
			out = p
		}
	}()
	return s.Filter.Filter(p)
}

// mirrorSize copies the terminal size to the PTY on every SIGWINCH.
func mirrorSize(tty, ptmx *os.File) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			_ = cpty.InheritSize(tty, ptmx)
		}
	}()
	return func() {
		signal.Stop(ch)
		close(ch)
	}
}

// forwardSignals relays termination signals sent to the wrapper to the child.
// In raw mode Ctrl+C arrives as a byte, so these only come from kill(1) or a
// closing terminal.
func forwardSignals(p *os.Process) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	go func() {
		for sig := range ch {
			_ = p.Signal(sig)
		}
	}()
	return func() {
		signal.Stop(ch)
		close(ch)
	}
}

func exitCode(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return 0, err
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal()), nil
	}
	return ee.ExitCode(), nil
}
