package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandoffDue(t *testing.T) {
	const at = 120_000
	var last int64
	for _, step := range []struct {
		context int64
		due     bool
	}{
		{50_000, false},
		{120_000, true},  // reached
		{130_000, false}, // shown at 120k
		{159_999, false},
		{160_000, true}, // 40k more
		{170_000, false},
		{8_000, false},  // /clear
		{125_000, true}, // reached again
	} {
		var due bool
		due, last = handoffDue(step.context, at, last)
		if due != step.due {
			t.Fatalf("at %d: due = %v", step.context, due)
		}
	}
	if due, _ := handoffDue(500_000, 0, 0); due {
		t.Fatal("due with hints.handoff_at 0")
	}
}

func TestHandoffPrompt(t *testing.T) {
	dir := t.TempDir()
	got := handoffPrompt(dir, "/p/s.jsonl")
	if !strings.HasPrefix(got, "Write HANDOFF.md") || !strings.Contains(got, "## 8. Instructions for the Next Session") ||
		!strings.Contains(got, "*Original session: /p/s.jsonl.") {
		t.Fatalf("no file: %q", got)
	}
	if got := handoffPrompt(dir, ""); !strings.Contains(got, "Original session: unknown.") {
		t.Fatalf("no transcript: %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "handoff.txt"), []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := handoffPrompt(dir, ""); !strings.HasPrefix(got, "Write HANDOFF.md") {
		t.Fatalf("empty file: %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "handoff.txt"), []byte("Write NOTES.md; see {transcript}.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := handoffPrompt(dir, "/p/s.jsonl"); got != "Write NOTES.md; see /p/s.jsonl." {
		t.Fatalf("file: %q", got)
	}
}
