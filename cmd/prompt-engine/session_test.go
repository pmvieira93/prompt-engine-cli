package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pmvieira93/prompt-engine-cli/internal/config"
	"github.com/pmvieira93/prompt-engine-cli/internal/transcript"
)

// A transcript as Claude Code writes it, trimmed to the fields read.
func writeTranscript(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

const (
	tPrompt = `{"type":"user","uuid":"u1","message":{"content":"why does the build fail on linux"}}`
	tReply  = `{"type":"assistant","uuid":"a1","message":{"content":[{"type":"text","text":"termios.go uses TIOCGETA, which is macOS only. Should I split it?"}],"stop_reason":"end_turn"}}`
	tNext   = `{"type":"user","uuid":"u2","message":{"content":"yes go ahead"}}`
	tReply2 = `{"type":"assistant","uuid":"a2","message":{"content":[{"type":"text","text":"Done: termios_darwin.go and termios_linux.go."}],"stop_reason":"end_turn"}}`
)

// sent is the last prompt the test session sent.
var sent = "why does the build fail on linux"

func testSession(t *testing.T) (*sessionInfo, string, *[]string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "-work-app", "s.jsonl")
	var mu sync.Mutex
	var logged []string
	s := &sessionInfo{
		finder:   &transcript.Finder{Root: root, Cwd: "/work/app", Since: time.Now().Add(-time.Minute)},
		lastSent: func() string { return sent },
		logf: func(f string, a ...any) {
			mu.Lock()
			defer mu.Unlock()
			logged = append(logged, f)
		},
		every: 10 * time.Millisecond,
	}
	return s, path, &logged
}

func TestSessionReply(t *testing.T) {
	s, path, _ := testSession(t)
	if got := s.reply(); got != "" {
		t.Errorf("no transcript yet: %q", got)
	}
	writeTranscript(t, path, tPrompt, tReply)
	if got := s.reply(); got != "termios.go uses TIOCGETA, which is macOS only. Should I split it?" {
		t.Errorf("reply = %q", got)
	}
	// A new prompt with no reply yet: nothing to resolve against.
	writeTranscript(t, path, tPrompt, tReply, tNext)
	if got := s.reply(); got != "" {
		t.Errorf("waiting for a reply: %q", got)
	}
}

func TestSessionSummary(t *testing.T) {
	s, path, _ := testSession(t)
	writeTranscript(t, path, tPrompt, tReply)
	cfg := config.Default()
	cfg.LLM.Provider = "fake" // a fixed summary: "Asked: <prompt>."
	cfg.Expand.Context = "session"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.run(ctx, func() config.Config { return cfg })

	waitFor := func(want string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for s.Summary() != want {
			if time.Now().After(deadline) {
				t.Fatalf("summary = %q, want %q", s.Summary(), want)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitFor("Asked: why does the build fail on linux.")

	// While a rewrite runs, the summary waits.
	release := s.hold()
	writeTranscript(t, path, tPrompt, tReply, tNext, tReply2)
	time.Sleep(50 * time.Millisecond)
	if got := s.Summary(); got != "Asked: why does the build fail on linux." {
		t.Errorf("updated during a rewrite: %q", got)
	}
	release()
	release() // releasing twice is harmless
	waitFor("Asked: why does the build fail on linux. Asked: yes go ahead.")
}

func TestSessionSummaryOnlyAtSessionLevel(t *testing.T) {
	s, path, _ := testSession(t)
	writeTranscript(t, path, tPrompt, tReply)
	cfg := config.Default()
	cfg.LLM.Provider = "fake"
	cfg.Expand.Context = "reply"
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	s.run(ctx, func() config.Config { return cfg })
	if got := s.Summary(); got != "" {
		t.Errorf("summary at the reply level: %q", got)
	}
}
