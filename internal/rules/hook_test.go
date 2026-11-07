package rules

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func queue(t *testing.T) *Queue {
	t.Helper()
	q, err := NewQueue()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { q.Close() })
	return q
}

func put(t *testing.T, q *Queue, prompt, text string) {
	t.Helper()
	if err := q.Put(prompt, text); err != nil {
		t.Fatal(err)
	}
}

func TestTakeInOrder(t *testing.T) {
	q := queue(t)
	put(t, q, "first", "one")
	put(t, q, "second", "two")
	for _, want := range []struct{ prompt, text string }{{"first", "one"}, {"second", "two"}} {
		if got, ok := Take(q.Dir, want.prompt); !ok || got != want.text {
			t.Fatalf("%s: got %q, %v", want.prompt, got, ok)
		}
	}
	if _, ok := Take(q.Dir, "third"); ok {
		t.Fatal("the queue should be empty")
	}
}

// An Enter that sent nothing leaves an entry; the next match drops it.
func TestTakeDropsOlderEntries(t *testing.T) {
	q := queue(t)
	put(t, q, "answered a dialog", "stale")
	put(t, q, "real prompt", "rules")
	if got, _ := Take(q.Dir, "real prompt"); got != "rules" {
		t.Fatalf("got %q", got)
	}
	if files, _ := os.ReadDir(q.Dir); len(files) != 0 {
		t.Fatalf("left %d entries", len(files))
	}
}

func TestTakeSeesThroughPasteTags(t *testing.T) {
	q := queue(t)
	put(t, q, "other", "wrong")
	put(t, q, "explain: a\nb\nc\nd", "rules")
	sent := "explain: \n\n<pasted_content id=\"abc7\">\na\nb\nc\nd\n</pasted_content id=\"abc7\">\n"
	if got, _ := Take(q.Dir, sent); got != "rules" {
		t.Fatalf("got %q", got)
	}
}

func TestTakeUnknownTextGetsOldest(t *testing.T) {
	q := queue(t)
	put(t, q, "look at @READ and say ok", "rules")
	if got, ok := Take(q.Dir, "look at @README.md  and say ok"); !ok || got != "rules" {
		t.Fatalf("got %q, %v", got, ok)
	}
}

func TestSettingsCarriesCommand(t *testing.T) {
	var s struct {
		Hooks map[string][]struct {
			Hooks []struct{ Type, Command string } `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(Settings("'/a b/pe' --pe-hook")), &s); err != nil {
		t.Fatal(err)
	}
	h := s.Hooks["UserPromptSubmit"][0].Hooks[0]
	if h.Type != "command" || h.Command != "'/a b/pe' --pe-hook" {
		t.Fatalf("hook = %+v", h)
	}
}

func hook(dir, in string) string {
	var out bytes.Buffer
	Hook(strings.NewReader(in), &out, dir, func(string, ...any) {})
	return out.String()
}

func TestHookAddsContext(t *testing.T) {
	q := queue(t)
	put(t, q, "fix it now", "# Guidance\nrules")
	var reply struct {
		Specific struct {
			Event   string `json:"hookEventName"`
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(hook(q.Dir, `{"prompt":"fix it now"}`)), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Specific.Event != "UserPromptSubmit" || reply.Specific.Context != "# Guidance\nrules" {
		t.Fatalf("reply = %+v", reply)
	}
}

// The prompt must go whatever happens, so these write nothing at all.
func TestHookAddsNothing(t *testing.T) {
	q := queue(t)
	put(t, q, "as typed", "")
	if got := hook(q.Dir, `{"prompt":"as typed"}`); got != "" {
		t.Errorf("no rules: %q", got)
	}
	if got := hook(q.Dir, `{"prompt":"nothing waiting"}`); got != "" {
		t.Errorf("empty queue: %q", got)
	}
	if got := hook(q.Dir, `not json`); got != "" {
		t.Errorf("bad input: %q", got)
	}
	if got := hook("/no/such/folder", `{"prompt":"x"}`); got != "" {
		t.Errorf("no folder: %q", got)
	}
}
