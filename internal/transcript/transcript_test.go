package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Records shaped as Claude Code 2.1.308 writes them, trimmed to the fields
// that are read.
const (
	prompt1   = `{"type":"user","uuid":"u1","cwd":"/work/app","message":{"role":"user","content":"why does the parser crash"}}`
	thinking  = `{"type":"assistant","uuid":"a1","message":{"content":[{"type":"thinking","thinking":""},{"type":"text","text":"Let me read the parser."}],"stop_reason":"tool_use"}}`
	toolUse   = `{"type":"assistant","uuid":"a2","message":{"content":[{"type":"tool_use","id":"t1","name":"Read","input":{}}],"stop_reason":"tool_use"}}`
	result    = `{"type":"user","uuid":"r1","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"..."}]}}`
	answer    = `{"type":"assistant","uuid":"a3","message":{"content":[{"type":"text","text":"A nil check is missing in parser.go:42.\n\nShould I fix it?"}],"stop_reason":"end_turn"}}`
	notice    = `{"type":"user","uuid":"n1","message":{"content":"<task-notification>\n<task-id>x</task-id>"}}`
	sidechain = `{"type":"user","uuid":"s1","isSidechain":true,"message":{"content":"subagent prompt"}}`
	meta      = `{"type":"user","uuid":"m1","isMeta":true,"message":{"content":"Caveat: …"}}`
	other     = `{"type":"ai-title","title":"x"}`
	prompt2   = `{"type":"user","uuid":"u2","message":{"content":[{"type":"text","text":"fix that"}]}}`
)

func write(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLastTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	write(t, path, prompt1, other, thinking, toolUse, result, sidechain, meta, answer, notice, `not json`)
	turn, err := LastTurn(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Turn{ID: "a3", Prompt: "why does the parser crash", Reply: "A nil check is missing in parser.go:42.\n\nShould I fix it?", Done: true}
	if turn != want {
		t.Errorf("got  %+v\nwant %+v", turn, want)
	}

	// A new prompt starts a turn with no reply yet.
	write(t, path, prompt1, answer, prompt2)
	if turn, _ := LastTurn(path); turn.Prompt != "fix that" || turn.Reply != "" || turn.Done {
		t.Errorf("after a new prompt: %+v", turn)
	}

	// Mid-reply: the text before a tool call is not the answer.
	write(t, path, prompt1, thinking, toolUse)
	if turn, _ := LastTurn(path); turn.Reply != "" || turn.Done {
		t.Errorf("mid-reply: %+v", turn)
	}

	write(t, path, other)
	if _, err := LastTurn(path); err == nil {
		t.Error("no conversation: want an error")
	}
	if _, err := LastTurn(filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
		t.Error("missing file: want an error")
	}
}

func TestReadTailStartsAtALine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	write(t, path, prompt1, answer)
	data, err := readTail(path, int64(len(answer)+5))
	if err != nil || strings.TrimSpace(string(data)) != answer {
		t.Fatalf("tail = %q, %v", data, err)
	}
}

func TestFinderPath(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "-work-my-app")
	old := filepath.Join(dir, "old.jsonl")
	a := filepath.Join(dir, "a.jsonl")
	b := filepath.Join(dir, "b.jsonl")
	write(t, old, prompt1, answer)
	since := time.Now()
	past := since.Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	f := &Finder{Root: root, Cwd: "/work/my_app", Since: since} // "_" escapes to "-"
	if p := f.Path("why does the parser crash"); p != "" {
		t.Errorf("only an older session: %q", p)
	}

	write(t, a, prompt1, answer)          // another session, written last
	write(t, b, prompt1, answer, prompt2) // this one
	later := since.Add(time.Minute)
	if err := os.Chtimes(a, later, later); err != nil {
		t.Fatal(err)
	}
	// Before a prompt is sent, no transcript is this session's, however new.
	if p := f.Path(""); p != "" {
		t.Errorf("nothing sent yet: %q", p)
	}
	if p := f.Path("fix that\n\nRun the tests."); p != b {
		t.Errorf("by the prompt sent (rules appended): %q, want %q", p, b)
	}
	// A prompt in both: the most recently written.
	if p := f.Path("why does the parser crash"); p != a {
		t.Errorf("in both: %q, want %q", p, a)
	}
	if p := f.Path("never sent"); p != "" {
		t.Errorf("a prompt no transcript holds: %q", p)
	}
	// Found once, it's kept for the same prompt, until it's gone.
	if p := f.Path("fix that"); p != b {
		t.Fatalf("Path = %q", p)
	}
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	if p := f.Path("fix that"); p != "" {
		t.Errorf("after the transcript went: %q", p)
	}
}

func TestFinderFindsAnUnexpectedFolderName(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "renamed", "s.jsonl")
	write(t, path, prompt1, answer) // prompt1 records cwd /work/app
	f := &Finder{Root: root, Cwd: "/work/app", Since: time.Now().Add(-time.Minute)}
	if p := f.Path("why does the parser crash"); p != path {
		t.Errorf("Path = %q, want %q", p, path)
	}
	f = &Finder{Root: root, Cwd: "/elsewhere", Since: time.Now().Add(-time.Minute)}
	if p := f.Path("why does the parser crash"); p != "" {
		t.Errorf("another project: %q", p)
	}
}

func TestRoot(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/custom/claude")
	if r, _ := Root(); r != "/custom/claude/projects" {
		t.Errorf("Root = %q", r)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if r, _ := Root(); !strings.HasSuffix(r, filepath.Join(".claude", "projects")) {
		t.Errorf("Root = %q", r)
	}
}

func TestShorten(t *testing.T) {
	if got := Shorten("  short reply ", 10); got != "short reply" {
		t.Errorf("short: %q", got)
	}
	long := strings.Repeat("w ", 30) + "end"
	got := Shorten(long, 8)
	if got != "w w … w w w w w end" {
		t.Errorf("long: %q", got)
	}
}
