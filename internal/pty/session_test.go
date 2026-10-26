package pty

import (
	"bytes"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a bytes.Buffer safe for the output goroutine and the test.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func run(t *testing.T, s *Session, cmd *exec.Cmd) int {
	t.Helper()
	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		code, err := s.Run(cmd)
		done <- result{code, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Run: %v", r.err)
		}
		return r.code
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
		return 0
	}
}

func TestRunPropagatesExitCode(t *testing.T) {
	out := &syncBuffer{}
	s := &Session{Stdin: strings.NewReader(""), Stdout: out}
	if code := run(t, s, exec.Command("sh", "-c", "echo hi; exit 3")); code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}
	if !strings.Contains(out.String(), "hi") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunReportsSignalExit(t *testing.T) {
	s := &Session{Stdin: strings.NewReader(""), Stdout: io.Discard}
	if code := run(t, s, exec.Command("sh", "-c", "kill -TERM $$")); code != 128+15 {
		t.Fatalf("exit code = %d, want 143", code)
	}
}

func TestFilterInterceptsInput(t *testing.T) {
	out := &syncBuffer{}
	s := &Session{
		// \x04 (Ctrl+D) ends cat's input in canonical mode.
		Stdin:  strings.NewReader("hello\n\x04"),
		Stdout: out,
		Filter: FilterFunc(func(p []byte) []byte { return bytes.ToUpper(p) }),
	}
	run(t, s, exec.Command("cat"))
	if !strings.Contains(out.String(), "HELLO") {
		t.Fatalf("filtered input not seen by child: %q", out.String())
	}
}

func TestFilterPanicFallsBackToPassThrough(t *testing.T) {
	out := &syncBuffer{}
	reported := make(chan error, 2)
	s := &Session{
		Stdin:         strings.NewReader("hello\n\x04"),
		Stdout:        out,
		Filter:        FilterFunc(func([]byte) []byte { panic("boom") }),
		OnFilterError: func(err error) { reported <- err },
	}
	run(t, s, exec.Command("cat"))
	if !strings.Contains(out.String(), "hello") {
		t.Fatalf("raw input not passed through: %q", out.String())
	}
	if len(reported) != 1 {
		t.Fatalf("OnFilterError called %d times, want 1", len(reported))
	}
}
