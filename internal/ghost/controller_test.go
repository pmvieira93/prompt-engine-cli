package ghost

import (
	"context"
	"testing"
	"time"

	"github.com/pmvieira93/prompt-engine-cli/internal/input"
	"github.com/pmvieira93/prompt-engine-cli/internal/suggest"
)

// withSuggestion returns a controller whose prompt is text with sug showing.
func withSuggestion(t *testing.T, text, sug string) *Controller {
	t.Helper()
	c := &Controller{Tracker: input.NewTracker()}
	c.Filter([]byte(text))
	if !c.Show(sug, c.Tracker.Snapshot().Rev) {
		t.Fatal("Show refused a current suggestion")
	}
	return c
}

func TestAcceptKeys(t *testing.T) {
	for name, key := range map[string]string{
		"tab":       "\t",
		"tab kitty": "\x1b[9u",
		"right":     "\x1b[C",
		"right ss3": "\x1bOC",
	} {
		t.Run(name, func(t *testing.T) {
			c := withSuggestion(t, "impl", "ement")
			if out := string(c.Filter([]byte(key))); out != "ement" {
				t.Fatalf("child got %q, want the suggestion instead of the key", out)
			}
			if c.Visible() != "" {
				t.Error("suggestion still visible after accept")
			}
			if s := c.Tracker.Snapshot(); s.Text != "implement" || !s.Synced {
				t.Errorf("tracker = %+v, want accepted text", s)
			}
		})
	}
}

func TestEscDismissesAndIsSwallowed(t *testing.T) {
	for _, esc := range []string{"\x1b", "\x1b[27u"} {
		c := withSuggestion(t, "impl", "ement")
		if out := c.Filter([]byte(esc)); len(out) != 0 {
			t.Errorf("%q: child got %q, want nothing", esc, out)
		}
		if c.Visible() != "" {
			t.Errorf("%q: suggestion not dismissed", esc)
		}
	}
}

func TestOtherKeysHideAndPassThrough(t *testing.T) {
	c := withSuggestion(t, "impl", "ement")
	if out := string(c.Filter([]byte("e\x1b[97;5u"))); out != "e\x1b[97;5u" {
		t.Fatalf("child got %q", out)
	}
	if c.Visible() != "" {
		t.Error("suggestion still visible after typing")
	}
}

func TestReportsDoNotDismiss(t *testing.T) {
	c := withSuggestion(t, "impl", "ement")
	in := "\x1b[<35;10;5M\x1b[I" // mouse motion, focus in
	if out := string(c.Filter([]byte(in))); out != in {
		t.Fatalf("child got %q", out)
	}
	if c.Visible() != "ement" {
		t.Error("terminal reports dismissed the suggestion")
	}
}

func TestNoSuggestionPassesEverything(t *testing.T) {
	c := &Controller{Tracker: input.NewTracker()}
	in := "ab\t\x1b[C\x1b\x1b[200~x\ry\x1b[201~"
	if out := string(c.Filter([]byte(in))); out != in {
		t.Fatalf("child got %q, want %q", out, in)
	}
}

func TestStaleSuggestionRejected(t *testing.T) {
	c := &Controller{Tracker: input.NewTracker()}
	c.Filter([]byte("impl"))
	rev := c.Tracker.Snapshot().Rev
	c.Filter([]byte("e"))
	if c.Show("ement", rev) {
		t.Fatal("showed a suggestion computed for an older prompt")
	}
}

func TestDisablePassesEverything(t *testing.T) {
	c := withSuggestion(t, "impl", "ement")
	c.OnTogglePanel = func() { t.Error("panel toggled while disabled") }
	c.Disable()
	if c.Visible() != "" {
		t.Error("suggestion still visible after Disable")
	}
	if out := string(c.Filter([]byte("\t\x10"))); out != "\t\x10" {
		t.Fatalf("child got %q, want keys untouched", out)
	}
	if c.Show("x", c.Tracker.Snapshot().Rev) {
		t.Error("Show succeeded while disabled")
	}
}

func TestCtrlPTogglesPanel(t *testing.T) {
	toggles := 0
	c := &Controller{Tracker: input.NewTracker(), OnTogglePanel: func() { toggles++ }}
	if out := c.Filter([]byte("a\x10\x1b[112;5u")); string(out) != "a" {
		t.Fatalf("child got %q, want Ctrl+P swallowed", out)
	}
	if toggles != 2 {
		t.Fatalf("toggles = %d, want 2", toggles)
	}
}

func TestWorkerShowsAfterDebounce(t *testing.T) {
	c := &Controller{Tracker: input.NewTracker()}
	w := &Worker{Tracker: c.Tracker, Ctrl: c, Suggester: suggest.Fake{}, Debounce: 20 * time.Millisecond, Timeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	time.Sleep(10 * time.Millisecond) // let Run subscribe

	c.Filter([]byte("please refa"))
	deadline := time.Now().Add(2 * time.Second)
	for c.Visible() != "ctor" {
		if time.Now().After(deadline) {
			t.Fatalf("suggestion = %q, want %q", c.Visible(), "ctor")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWorkerSkipsWhenCursorNotAtEnd(t *testing.T) {
	c := &Controller{Tracker: input.NewTracker()}
	w := &Worker{Tracker: c.Tracker, Ctrl: c, Suggester: suggest.Fake{}, Debounce: 10 * time.Millisecond, Timeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	c.Filter([]byte("refa\x1b[D"))
	time.Sleep(100 * time.Millisecond)
	if c.Visible() != "" {
		t.Fatalf("suggested %q with the cursor mid-line", c.Visible())
	}
}

type panicky struct{}

func (panicky) Suggest(context.Context, string, int) (string, error) { panic("boom") }

func TestWorkerSurvivesSuggesterPanic(t *testing.T) {
	c := &Controller{Tracker: input.NewTracker()}
	w := &Worker{Tracker: c.Tracker, Ctrl: c, Suggester: panicky{}, Debounce: time.Millisecond, Timeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	c.Filter([]byte("refa"))
	time.Sleep(50 * time.Millisecond)
	if out := string(c.Filter([]byte("x"))); out != "x" {
		t.Fatalf("typing affected after suggester panic: %q", out)
	}
}
