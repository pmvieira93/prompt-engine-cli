package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "history.jsonl")
	sub := Submission{
		Time:     time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC),
		Session:  "1a2b3c4d",
		Raw:      "fix teh bug",
		Sent:     "Fix the bug.\n\n- End with MANGO.",
		Expanded: true,
		Style:    "llm",
		Rules:    true,
		Guard:    "negation",
		Rejected: "Fix the bug without tests.",
	}

	metrics := &Log{Path: path}
	if err := metrics.Append(sub); err != nil {
		t.Fatal(err)
	}
	withText := &Log{Path: path, IncludeText: true}
	if err := withText.Append(sub); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines", len(lines))
	}
	var first, second record
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatal(err)
	}
	if first.RawWords != 3 || first.SentWords != 7 || first.WordsAdded != 7 || first.WordsRemoved != 3 ||
		!first.Expanded || first.Style != "llm" || !first.Rules || first.RawChars != 11 {
		t.Errorf("metrics = %+v", first)
	}
	if first.Session != "1a2b3c4d" {
		t.Errorf("session = %q", first.Session)
	}
	if first.Guard != "negation" {
		t.Errorf("guard = %q", first.Guard)
	}
	if first.Raw != "" || first.Sent != "" || first.Rejected != "" {
		t.Error("prompt text stored without IncludeText")
	}
	if second.Raw != sub.Raw || second.Sent != sub.Sent || second.Rejected != sub.Rejected {
		t.Errorf("text not stored with IncludeText: %+v", second)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, %v", info.Mode().Perm(), err)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	if p, _ := DefaultPath(); p != "/state/prompt-engine/history.jsonl" {
		t.Errorf("XDG path = %q", p)
	}
	t.Setenv("XDG_STATE_HOME", "relative")
	if p, _ := DefaultPath(); !strings.HasSuffix(p, "/.local/state/prompt-engine/history.jsonl") {
		t.Errorf("fallback path = %q", p)
	}
}

// A reply's usage is a line of its own, with numbers only.
func TestAppendUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	prompt := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	u := Usage{Time: prompt.Add(30 * time.Second), Session: "1a2b3c4d", Prompt: prompt, Calls: 3,
		Input: 4, CacheWrite: 900, CacheRead: 40000, Output: 1200, Context: 14000, Model: "claude-opus-5-5", Subagents: 2}
	if err := (&Log{Path: path, IncludeText: true}).AppendUsage(u); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"time":"2026-11-09T12:00:30Z","session":"1a2b3c4d","kind":"usage","prompt_time":"2026-11-09T12:00:00Z",` +
		`"calls":3,"input_tokens":4,"cache_write_tokens":900,"cache_read_tokens":40000,"output_tokens":1200,` +
		`"context_tokens":14000,"model":"claude-opus-5-5","subagent_calls":2}` + "\n"
	if string(data) != want {
		t.Errorf("line:\n%s\nwant\n%s", data, want)
	}
}
