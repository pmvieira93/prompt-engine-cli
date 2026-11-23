package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pmvieira93/prompt-engine-cli/internal/history"
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

func TestNewTask(t *testing.T) {
	for _, c := range []struct {
		text   string
		cosine float64
		want   bool
	}{
		{"add a CSV export to the ledger report, with a header row and one line per entry", 0.3, true},
		{"add a CSV export to the ledger report, with a header row and one line per entry", 0.7, false}, // close to the last prompt
		{"fix it", 0.9, false},
		{"Write a haiku about the ocean and the moon at night.", 0.544, true}, // the hand test that missed at threshold 0
	} {
		if got := newTaskScore(c.text, c.cosine) > newTaskThreshold; got != c.want {
			t.Errorf("new task for %q at %v = %v, want %v", c.text, c.cosine, got, c.want)
		}
	}
}

func TestEmbed(t *testing.T) {
	var asked map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&asked)
		if r.URL.Path != "/api/embed" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"embeddings":[[1,2,3]]}`))
	}))
	defer srv.Close()
	v, err := embed(context.Background(), srv.URL+"/", "hello")
	if err != nil || len(v) != 3 || v[2] != 3 {
		t.Fatalf("embed = %v, %v", v, err)
	}
	if asked["model"] != embedModel || asked["input"] != "classification: hello" {
		t.Fatalf("asked %v", asked)
	}
	if !backRef.MatchString("OK. Same for Parse") {
		t.Fatal("backRef must ignore case, as the spike's BACKREF does")
	}
	if c := cosine([]float64{1, 0}, []float64{1, 0}); c != 1 {
		t.Fatalf("cosine = %v", c)
	}
}

func TestHintsSent(t *testing.T) {
	h := &hints{shown: "new_task"}
	sub := history.Submission{Raw: "fix the build"}
	h.sent(&sub, false, "")
	if sub.Hint != "new_task" || sub.HintFollowed || h.shown != "" || h.typed != "fix the build" {
		t.Fatalf("dismissed hint: sub %+v, hints %+v", sub, h)
	}

	// A handoff after the new-task hint: the new task comes back after
	// /clear, and the classifier still compares with the last typed prompt.
	h.shown, h.followed = "new_task", true
	sub = history.Submission{Raw: "Write HANDOFF.md"}
	h.sent(&sub, true, "add dark mode")
	if sub.Hint != "new_task" || !sub.HintFollowed || !h.handedOff || h.restore != "add dark mode" || h.typed != "fix the build" {
		t.Fatalf("handoff: sub %+v, hints %+v", sub, h)
	}

	// A paste's label can't be typed back.
	h = &hints{shown: "new_task", followed: true}
	h.sent(&history.Submission{}, true, "explain [Pasted text #1 +40 lines]")
	if h.restore != "" {
		t.Fatalf("kept %q", h.restore)
	}
}
