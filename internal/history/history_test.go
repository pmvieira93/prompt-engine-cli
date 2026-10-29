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
