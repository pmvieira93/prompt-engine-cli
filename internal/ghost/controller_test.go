package ghost

import (
	"context"
	"errors"
	"strings"
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

func leaderController(toggles *int) *Controller {
	leader, _ := input.ParseBinding("ctrl+]")
	panel, _ := input.ParseBinding("p")
	return &Controller{
		Tracker:       input.NewTracker(),
		Leader:        leader,
		PanelKey:      panel,
		OnTogglePanel: func() { *toggles++ },
	}
}

func TestLeaderShortcuts(t *testing.T) {
	cases := []struct {
		name, in, child string
		toggles         int
	}{
		{"legacy leader then p", "a\x1dp", "a", 1},
		{"kitty leader then p", "\x1b[93;5up", "", 1},
		{"split across reads", "\x1d|p", "", 1},
		{"reports don't cancel", "\x1d\x1b[<35;10;5M\x1b[Ip", "\x1b[<35;10;5M\x1b[I", 1},
		{"leader twice sends it", "\x1d\x1d", "\x1d", 0},
		{"esc cancels", "\x1d|\x1b|p", "p", 0},
		{"unbound key is swallowed", "\x1dxq", "q", 0},
		{"rest of a typed run passes", "\x1dpxy", "xy", 1},
		{"Ctrl+P reaches the child", "\x10\x1b[112;5u", "\x10\x1b[112;5u", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			toggles := 0
			c := leaderController(&toggles)
			var child string
			for _, chunk := range strings.Split(tc.in, "|") {
				child += string(c.Filter([]byte(chunk)))
			}
			if child != tc.child || toggles != tc.toggles {
				t.Fatalf("child got %q with %d toggles, want %q with %d", child, toggles, tc.child, tc.toggles)
			}
		})
	}
}

func TestLeaderKeepsBufferInSync(t *testing.T) {
	toggles := 0
	c := leaderController(&toggles)
	c.Filter([]byte("fix\x1dp bug"))
	if s := c.Tracker.Snapshot(); s.Text != "fix bug" || !s.Synced {
		t.Fatalf("snapshot = %+v", s)
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

// TestWorkerResyncsFromScreen reproduces browsing claude's @ menu: arrow
// keys desync the buffer, and the screen is used to recover.
func TestWorkerResyncsFromScreen(t *testing.T) {
	c := &Controller{Tracker: input.NewTracker()}
	c.SetScreen(func() (string, int, bool) { return "refa", 4, true })
	w := &Worker{Tracker: c.Tracker, Ctrl: c, Suggester: suggest.Fake{}, Debounce: 20 * time.Millisecond, Timeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	time.Sleep(10 * time.Millisecond) // let Run subscribe

	c.Filter([]byte("@\x1b[B\x7frefa"))
	if c.Tracker.Snapshot().Synced {
		t.Fatal("arrow key should have desynced the buffer")
	}
	deadline := time.Now().Add(2 * time.Second)
	for c.Visible() != "ctor" {
		if time.Now().After(deadline) {
			t.Fatalf("suggestion = %q, want %q (state %+v)", c.Visible(), "ctor", c.Tracker.Snapshot())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s := c.Tracker.Snapshot(); s.Text != "refa" || s.Cursor != 4 || !s.Synced {
		t.Fatalf("state after resync = %+v", s)
	}
}

func TestBeforeSubmit(t *testing.T) {
	toggles := 0
	c := leaderController(&toggles)
	var seen []input.State
	c.BeforeSubmit = func(s input.State) []byte {
		seen = append(seen, s)
		return []byte("<rules>")
	}

	// Text and Enter in one read: BeforeSubmit must see the text.
	if out := string(c.Filter([]byte("hello\r"))); out != "hello<rules>\r" {
		t.Fatalf("child got %q", out)
	}
	if len(seen) != 1 || seen[0].Text != "hello" {
		t.Fatalf("BeforeSubmit saw %+v", seen)
	}
	if s := c.Tracker.Snapshot(); s.Text != "" || !s.Synced {
		t.Fatalf("after submit: %+v", s)
	}

	// Leader then Enter submits as typed.
	if out := string(c.Filter([]byte("bye\x1d\r"))); out != "bye\r" {
		t.Fatalf("leader+Enter: child got %q", out)
	}
	if len(seen) != 1 {
		t.Fatalf("BeforeSubmit called for leader+Enter")
	}
}

// TestWorkerQuietNearCompletionMenu: while claude's @ or / menu may be
// open, Tab must pick from it, so no ghost text competes for Tab.
func TestWorkerQuietNearCompletionMenu(t *testing.T) {
	c := &Controller{Tracker: input.NewTracker()}
	w := &Worker{Tracker: c.Tracker, Ctrl: c, Suggester: suggest.Fake{Words: []string{"readme", "help"}},
		Debounce: 10 * time.Millisecond, Timeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	time.Sleep(10 * time.Millisecond) // let Run subscribe

	for _, text := range []string{"look at @rea", "\x15/hel"} {
		c.Filter([]byte(text))
		time.Sleep(80 * time.Millisecond)
		if v := c.Visible(); v != "" {
			t.Fatalf("%q: ghost text %q shown next to claude's menu", text, v)
		}
	}
	// Sanity check: the same suggester does complete outside a menu.
	c.Filter([]byte("\x15rea"))
	deadline := time.Now().Add(2 * time.Second)
	for c.Visible() != "dme" {
		if time.Now().After(deadline) {
			t.Fatalf("suggestion = %q outside a menu, want %q", c.Visible(), "dme")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWorkerUpdate(t *testing.T) {
	c := &Controller{Tracker: input.NewTracker()}
	w := &Worker{Tracker: c.Tracker, Ctrl: c, Debounce: 10 * time.Millisecond, Timeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	time.Sleep(10 * time.Millisecond) // let Run subscribe

	// No suggester: nothing is shown.
	c.Filter([]byte("refa"))
	time.Sleep(60 * time.Millisecond)
	if v := c.Visible(); v != "" {
		t.Fatalf("suggestion %q with suggestions off", v)
	}

	// Turning suggestions on applies from the next pause.
	w.Update(suggest.Fake{}, 10*time.Millisecond, time.Second)
	c.Filter([]byte("\x7fa"))
	deadline := time.Now().Add(2 * time.Second)
	for c.Visible() != "ctor" {
		if time.Now().After(deadline) {
			t.Fatalf("suggestion = %q after Update, want %q", c.Visible(), "ctor")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSetKeys(t *testing.T) {
	toggles := 0
	c := leaderController(&toggles) // leader ctrl+]
	c.Filter([]byte("\x1d"))        // arm the old leader
	g, _ := input.ParseBinding("ctrl+g")
	x, _ := input.ParseBinding("x")
	c.SetKeys(g, x, input.Binding{}, input.Binding{})
	if out := string(c.Filter([]byte("p"))); out != "p" {
		t.Fatalf("SetKeys should disarm a pending leader; child got %q", out)
	}
	if out := string(c.Filter([]byte("\x07x\x1d"))); out != "\x1d" || toggles != 1 {
		t.Fatalf("new keys: child got %q, toggles %d", out, toggles)
	}
}

// recorder collects what Replace writes to the child.
type recorder struct{ strings.Builder }

func expandController(t *testing.T) (*Controller, *recorder, *[]string) {
	t.Helper()
	toggles := 0
	c := leaderController(&toggles) // leader ctrl+]
	c.ExpandKey, _ = input.ParseBinding("e")
	c.UndoKey, _ = input.ParseBinding("u")
	var got []string
	c.OnExpand = func(s input.State) { got = append(got, "expand:"+s.Text) }
	c.OnUndo = func(s input.State) { got = append(got, "undo:"+s.Text) }
	out := &recorder{}
	c.SetOutput(out)
	return c, out, &got
}

func TestLeaderExpandAndUndo(t *testing.T) {
	c, _, got := expandController(t)
	// Text and the shortcut in one read: the action sees the text.
	if out := string(c.Filter([]byte("fix teh bug\x1de"))); out != "fix teh bug" {
		t.Fatalf("child got %q", out)
	}
	c.Filter([]byte("\x1du"))
	if strings.Join(*got, "|") != "expand:fix teh bug|undo:fix teh bug" {
		t.Fatalf("actions = %q", *got)
	}
}

func TestReplace(t *testing.T) {
	c, out, _ := expandController(t)
	c.Filter([]byte("fix 👍🏽 teh\x1b[D\x1b[D"))
	s := c.Tracker.Snapshot()
	if err := c.Replace(s.Rev, s.Text, "Fix the bug.\n\nContext:\n- Branch: main\tx"); err != nil {
		t.Fatal(err)
	}
	// Two Rights to the end, 9 Backspaces (f i x space 👍🏽 space t e h: the
	// emoji is one), then the new text typed, with line breaks as Alt+Enter
	// and the tab as spaces.
	want := "\x1b[C\x1b[C" + strings.Repeat("\x7f", 9) + "Fix the bug.\x1b\r\x1b\rContext:\x1b\r- Branch: main    x"
	if out.String() != want {
		t.Fatalf("wrote %q\nwant  %q", out.String(), want)
	}
	if s := c.Tracker.Snapshot(); s.Text != "Fix the bug.\n\nContext:\n- Branch: main    x" || !s.Synced {
		t.Fatalf("tracker after Replace: %+v", s)
	}
}

func TestReplaceRefusesStale(t *testing.T) {
	c, out, _ := expandController(t)
	c.Filter([]byte("fix"))
	s := c.Tracker.Snapshot()
	c.Filter([]byte("ed")) // typed while the model was working
	if err := c.Replace(s.Rev, s.Text, "Fix."); !errors.Is(err, ErrStale) {
		t.Fatalf("err = %v", err)
	}
	c.CanInsert = func() bool { return false }
	s = c.Tracker.Snapshot()
	if err := c.Replace(s.Rev, s.Text, "Fix."); err == nil {
		t.Fatal("replaced while the cursor wasn't in the prompt")
	}
	if out.Len() != 0 {
		t.Fatalf("wrote %q", out.String())
	}
}

func TestNoticeBlocksSuggestions(t *testing.T) {
	c := &Controller{Tracker: input.NewTracker()}
	c.Filter([]byte("refa"))
	rev := c.Tracker.Snapshot().Rev
	if !c.Show("ctor", rev) {
		t.Fatal("Show failed")
	}
	c.SetNotice("⟳ expanding…")
	if c.Visible() != "" {
		t.Fatal("the notice didn't replace the suggestion")
	}
	if c.Show("ctor", rev) {
		t.Fatal("a suggestion was shown over the notice")
	}
	c.SetNotice("")
	if !c.Show("ctor", rev) {
		t.Fatal("suggestions didn't resume after the notice")
	}
}
