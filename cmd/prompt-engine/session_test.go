package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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
		finder: &transcript.Finder{Root: root, Cwd: "/work/app", Since: time.Now().Add(-time.Minute)},
		sent:   func() []string { return []string{sent} },
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

// After /clear, claude writes a new transcript with none of the earlier
// conversation: the summary starts again from it.
func TestSessionSummaryRestartsWithANewTranscript(t *testing.T) {
	s, path, logged := testSession(t)
	var last atomic.Pointer[string]
	last.Store(&sent)
	s.sent = func() []string { return []string{*last.Load()} }
	writeTranscript(t, path, tPrompt, tReply)
	cfg := config.Default()
	cfg.LLM.Provider = "fake"
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

	// As recorded: the /clear command is the new transcript's first record.
	cleared := filepath.Join(filepath.Dir(path), "after-clear.jsonl")
	writeTranscript(t, cleared,
		`{"type":"user","uuid":"c1","message":{"content":"<command-name>/clear</command-name>"}}`,
		`{"type":"user","uuid":"u3","message":{"content":"name a planet"}}`,
		`{"type":"assistant","uuid":"a3","message":{"content":[{"type":"text","text":"Mars."}],"stop_reason":"end_turn"}}`)
	planet := "name a planet"
	last.Store(&planet)
	waitFor("Asked: name a planet.")
	if got := s.reply(); got != "Mars." {
		t.Errorf("reply = %q, want the new transcript's", got)
	}
	s.mu.Lock() // logf is called with s.mu held
	defer s.mu.Unlock()
	if !slices.Contains(*logged, "context: summary cleared: the session has a new transcript") {
		t.Errorf("not logged: %q", *logged)
	}
}

func TestSessionForgetsAfterClear(t *testing.T) {
	s, path, logged := testSession(t)
	writeTranscript(t, path, tPrompt, tReply)
	if s.reply() == "" {
		t.Fatal("no reply before /clear")
	}
	s.mu.Lock()
	s.summary, s.summaryOf = "Asked why the build fails.", path
	s.mu.Unlock()
	cleared := make(chan struct{})
	s.onClear = func(time.Time) { close(cleared) }

	at := time.Now()
	s.slashCommand(at)
	// As recorded from Claude Code 2.1.309: /clear writes a new transcript at once.
	clear := filepath.Join(filepath.Dir(path), "new.jsonl")
	if err := os.WriteFile(clear, []byte(`{"type":"user","timestamp":"`+at.UTC().Format(time.RFC3339Nano)+
		`","message":{"content":"<command-name>/clear</command-name>"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cleared:
	case <-time.After(3 * time.Second):
		t.Fatalf("onClear not called; log: %q", *logged)
	}
	if got := s.Summary(); got != "" {
		t.Errorf("summary after /clear: %q", got)
	}
}

func TestSessionSamePromptAfterClear(t *testing.T) {
	s, path, _ := testSession(t)
	var mu sync.Mutex
	prompts := []string{sent}
	s.sent = func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(prompts) }
	writeTranscript(t, path, tPrompt, tReply)
	if s.reply() == "" {
		t.Fatal("no reply before /clear")
	}
	cleared := make(chan struct{})
	s.onClear = func(time.Time) { mu.Lock(); prompts = nil; mu.Unlock(); close(cleared) }

	at := time.Now()
	s.slashCommand(at)
	fresh := filepath.Join(filepath.Dir(path), "new.jsonl")
	writeTranscript(t, fresh, `{"type":"user","timestamp":"`+at.UTC().Format(time.RFC3339Nano)+
		`","message":{"content":"<command-name>/clear</command-name>"}}`)
	select {
	case <-cleared:
	case <-time.After(3 * time.Second):
		t.Fatal("onClear not called")
	}
	if got := s.reply(); got != "" {
		t.Errorf("reply right after /clear: %q", got)
	}
	// The same prompt again, answered in the new transcript.
	writeTranscript(t, fresh, `{"type":"user","message":{"content":"<command-name>/clear</command-name>"}}`,
		tPrompt, tReply2)
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(fresh, later, later); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	prompts = []string{sent}
	mu.Unlock()
	if got := s.reply(); got != "Done: termios_darwin.go and termios_linux.go." {
		t.Errorf("reply after the same prompt again = %q, want the new transcript's", got)
	}
}

// usageReply is tReply with the usage Claude Code records: 3 calls' worth.
func usageReply(id string, in, read, out int) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"id":%q,"model":"claude-opus-5-5","stop_reason":"end_turn",`+
		`"content":[{"type":"text","text":"Done."}],"usage":{"input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d}}}`, id, in, read, out)
}

// The panel shows the tokens, and each prompt's turn goes to the history
// once: when the next prompt starts it, or when the session ends.
func TestSessionUsage(t *testing.T) {
	s, path, _ := testSession(t)
	var last atomic.Value
	last.Store(sent)
	s.sent = func() []string { return []string{last.Load().(string)} }
	turns := make(chan transcript.TurnUsage, 4)
	s.onTurn = func(t transcript.TurnUsage) { turns <- t }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.watchUsage(ctx)
		close(done)
	}()
	next := func() transcript.TurnUsage {
		t.Helper()
		select {
		case u := <-turns:
			return u
		case <-time.After(2 * time.Second):
			t.Fatal("no turn recorded")
			return transcript.TurnUsage{}
		}
	}
	none := func(when string) {
		t.Helper()
		select {
		case u := <-turns:
			t.Errorf("%s: a turn recorded: %+v", when, u)
		case <-time.After(100 * time.Millisecond):
		}
	}
	if lines := s.usageLines(); lines != nil {
		t.Errorf("before a transcript: %q", lines)
	}

	writeTranscript(t, path, tPrompt, usageReply("m1", 3, 1_200_000, 4500))
	deadline := time.Now().Add(2 * time.Second)
	for s.usageLines() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	want := []string{
		"tokens:      context 1.2M · claude-opus-5-5",
		"  reply:     1.2M in (99% cached), 4.5k out",
		"  so far:    1.2M in (99% cached), 4.5k out",
		"  calls:     1 in the reply, 1 so far",
	}
	if got := s.usageLines(); !slices.Equal(got, want) {
		t.Errorf("lines:\n%q\nwant\n%q", got, want)
	}
	none("before the next prompt")

	last.Store("yes go ahead")
	writeTranscript(t, path, tPrompt, usageReply("m1", 3, 1_200_000, 4500), tNext, usageReply("m2", 10, 50_000, 120))
	if u := next(); u.Prompt != "why does the build fail on linux" || u.Usage.Calls != 1 || u.Usage.Output != 4500 {
		t.Fatalf("first turn: %+v", u)
	}
	none("the second turn in progress")
	cancel()
	<-done
	if u := next(); u.Prompt != "yes go ahead" || u.Usage.Output != 120 {
		t.Fatalf("the turn in progress at the end: %+v", u)
	}
}

func TestTokens(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 950: "950", 1234: "1.2k", 9_960: "10k", 276_468: "276k", 999_600: "1.0M",
		1_420_000: "1.4M", 9_960_000: "10M", 889_000_000: "889M"} {
		if got := tokens(n); got != want {
			t.Errorf("tokens(%d) = %q, want %q", n, got, want)
		}
	}
}
