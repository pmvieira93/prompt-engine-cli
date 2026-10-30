package ghost

import (
	"context"
	"errors"
	"fmt"
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
		Keys:          Keys{Leader: leader, Panel: panel},
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
		{"Ctrl still held: Ctrl+P", "\x1d\x10", "", 1},
		{"Ctrl still held, kitty encoding", "\x1d\x1b[112;5u", "", 1},
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
	c.SetScreen(func() (string, int, int, bool) { return "refa", 4, 0, true })
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
	c.SetKeys(Keys{Leader: g, Panel: x})
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
	c.Keys.Expand, _ = input.ParseBinding("e")
	c.Keys.Undo, _ = input.ParseBinding("u")
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

func TestReplaceCoversSlack(t *testing.T) {
	// Read from two rows on the screen: claude's text may have one more
	// character (a space before the newline, seen in testing), so one more
	// Right and Backspace are sent.
	c, out, _ := expandController(t)
	c.Filter([]byte("\x1b[A")) // history: the buffer loses track
	if !c.Tracker.Resync(c.Tracker.Snapshot().Rev, "Check it.\nNow", 13, 1) {
		t.Fatal("resync not applied")
	}
	s := c.Tracker.Snapshot()
	if err := c.Replace(s.Rev, s.Text, "Fix."); err != nil {
		t.Fatal(err)
	}
	if want := "\x1b[C" + strings.Repeat("\x7f", 14) + "Fix."; out.String() != want {
		t.Fatalf("wrote %q\nwant  %q", out.String(), want)
	}
	if s := c.Tracker.Snapshot(); s.Text != "Fix." || s.Slack != 0 {
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

func TestShiftMouse(t *testing.T) {
	cases := []struct {
		in   string
		dx   int
		want string
		ok   bool
	}{
		{"\x1b[<0;50;10M", 0, "\x1b[<0;50;10M", true},
		{"\x1b[<0;50;10M", 40, "\x1b[<0;10;10M", true},
		{"\x1b[<35;41;3m", 40, "\x1b[<35;1;3m", true},
		{"\x1b[<0;40;10M", 40, "", false}, // over the panel
	}
	for _, tc := range cases {
		got, ok := shiftMouse([]byte(tc.in), tc.dx)
		if string(got) != tc.want || ok != tc.ok {
			t.Errorf("shiftMouse(%q, %d) = %q, %v; want %q, %v", tc.in, tc.dx, got, ok, tc.want, tc.ok)
		}
	}
}

func TestFilterShiftsMouse(t *testing.T) {
	c := &Controller{Tracker: input.NewTracker(), MouseOffset: func() int { return 30 }}
	if out := string(c.Filter([]byte("\x1b[<0;10;5M\x1b[<0;35;5Mhi"))); out != "\x1b[<0;5;5Mhi" {
		t.Fatalf("child got %q", out)
	}
}

func TestResizeRepeats(t *testing.T) {
	toggles := 0
	c := leaderController(&toggles)
	c.Keys.Wider, _ = input.ParseBinding("+")
	c.Keys.Narrower, _ = input.ParseBinding("-")
	var deltas []int
	c.OnResizePanel = func(d int) { deltas = append(deltas, d) }

	// Leader once, then repeats: none of the keys reach the child.
	if out := string(c.Filter([]byte("\x1d-"))); out != "" {
		t.Fatalf("child got %q", out)
	}
	if out := string(c.Filter([]byte("-"))); out != "" {
		t.Fatalf("repeat reached the child: %q", out)
	}
	if out := string(c.Filter([]byte("+"))); out != "" {
		t.Fatalf("repeat reached the child: %q", out)
	}
	// Any other key ends repeating and goes to the child; a later - is text.
	if out := string(c.Filter([]byte("x-"))); out != "x-" {
		t.Fatalf("after repeating: child got %q", out)
	}
	if fmt.Sprint(deltas) != "[-1 -1 1]" {
		t.Fatalf("deltas = %v", deltas)
	}
}

func TestResizeRunInOneRead(t *testing.T) {
	toggles := 0
	c := leaderController(&toggles)
	c.Keys.Wider, _ = input.ParseBinding("+")
	c.Keys.Narrower, _ = input.ParseBinding("-")
	var deltas []int
	c.OnResizePanel = func(d int) { deltas = append(deltas, d) }
	if out := string(c.Filter([]byte("\x1d--+ok"))); out != "ok" {
		t.Fatalf("child got %q", out)
	}
	if fmt.Sprint(deltas) != "[-1 -1 1]" {
		t.Fatalf("deltas = %v", deltas)
	}
}

func TestKeyHookAndSubmit(t *testing.T) {
	c, out, _ := expandController(t)
	var submitted []string
	c.BeforeSubmit = func(s input.State) []byte {
		submitted = append(submitted, s.Text)
		return []byte("<rules>")
	}
	var held input.State
	c.KeyHook = func(k input.Key, s input.State) bool {
		if k.Type == input.KeyEnter {
			held = s // hold back Enter, as auto expansion does
			return true
		}
		return false
	}

	if got := string(c.Filter([]byte("fix it\r"))); got != "fix it" {
		t.Fatalf("child got %q: Enter wasn't held back", got)
	}
	if held.Text != "fix it" || len(submitted) != 0 {
		t.Fatalf("held %+v, submitted %q", held, submitted)
	}

	// Later: send it. A stale revision is refused.
	if err := c.Submit(held.Rev+1, "fix it"); !errors.Is(err, ErrStale) {
		t.Fatalf("stale Submit: %v", err)
	}
	if err := c.Submit(held.Rev, "fix it"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "<rules>\r" || strings.Join(submitted, "|") != "fix it" {
		t.Fatalf("wrote %q, submitted %q", out.String(), submitted)
	}
	if s := c.Tracker.Snapshot(); s.Text != "" {
		t.Fatalf("prompt not reset after Submit: %+v", s)
	}

	// The leader still works with a hook installed, and leader+Enter
	// bypasses it.
	c.Filter([]byte("again\x1d\r"))
	if held.Text != "fix it" {
		t.Fatalf("leader+Enter reached the hook: %+v", held)
	}
}

// enterController returns a controller whose screen shows screen in the
// prompt, recording what reaches the child and each screen wait and read.
func enterController(screen string, readable bool) (*Controller, *recorder, *[]string) {
	toggles := 0
	c := leaderController(&toggles)
	out := &recorder{}
	c.SetOutput(out)
	var calls []string
	c.SetScreen(func() (string, int, int, bool) {
		calls = append(calls, "read")
		return screen, len([]rune(screen)), 0, readable
	})
	c.WaitScreen = func(time.Time) bool {
		calls = append(calls, fmt.Sprintf("wait after %q", out.String()))
		return true
	}
	return c, out, &calls
}

// TestEnterResyncsFromScreen: → in an empty box accepts claude's suggested
// prompt, which the buffer can't replay; Enter right after must read it
// from the screen, so the rules are added and it is recorded.
func TestEnterResyncsFromScreen(t *testing.T) {
	c, _, calls := enterController("what's in src?", true)
	var seen []input.State
	c.BeforeSubmit = func(s input.State) []byte {
		seen = append(seen, s)
		return []byte("<rules>")
	}

	c.Filter([]byte("\x1b[C"))
	if out := string(c.Filter([]byte("\r"))); out != "<rules>\r" {
		t.Fatalf("child got %q", out)
	}
	if len(seen) != 1 || seen[0].Text != "what's in src?" || !seen[0].Synced {
		t.Fatalf("BeforeSubmit saw %+v", seen)
	}
	if strings.Join(*calls, "|") != `wait after ""|read` {
		t.Fatalf("calls = %q, want a wait, then a screen read", *calls)
	}
	if s := c.Tracker.Snapshot(); s.Text != "" || !s.Synced {
		t.Fatalf("after submit: %+v", s)
	}
}

// TestEnterInSameReadSendsKeysFirst: with → and Enter in one read, → must
// reach the child before the wait, or the screen could never show it.
func TestEnterInSameReadSendsKeysFirst(t *testing.T) {
	c, rec, calls := enterController("what's in src?", true)
	var seen []string
	c.BeforeSubmit = func(s input.State) []byte {
		seen = append(seen, s.Text)
		return []byte("<rules>")
	}
	if out := string(c.Filter([]byte("\x1b[C\r"))); out != "<rules>\r" {
		t.Fatalf("Filter returned %q", out)
	}
	if rec.String() != "\x1b[C" || strings.Join(*calls, "|") != `wait after "\x1b[C"|read` {
		t.Fatalf("written %q, calls %q", rec.String(), *calls)
	}
	if strings.Join(seen, "|") != "what's in src?" {
		t.Fatalf("BeforeSubmit saw %q", seen)
	}
}

// TestEnterResyncReachesKeyHook: auto expansion's hook decides on the
// prompt read from the screen, not an unknown one.
func TestEnterResyncReachesKeyHook(t *testing.T) {
	c, _, _ := enterController("explain the main function", true)
	var held input.State
	c.KeyHook = func(k input.Key, s input.State) bool {
		if k.Type == input.KeyEnter {
			held = s
			return true
		}
		return false
	}
	c.Filter([]byte("\t"))
	if out := string(c.Filter([]byte("\r"))); out != "" {
		t.Fatalf("child got %q: Enter wasn't held back", out)
	}
	if held.Text != "explain the main function" || !held.Synced {
		t.Fatalf("hook saw %+v", held)
	}
}

func TestEnterInSyncDoesNotReadScreen(t *testing.T) {
	c, rec, calls := enterController("stale", true)
	c.BeforeSubmit = func(input.State) []byte { return nil }
	if out := string(c.Filter([]byte("hello\r"))); out != "hello\r" {
		t.Fatalf("child got %q", out)
	}
	if len(*calls) != 0 || rec.String() != "" {
		t.Fatalf("calls %q, written %q: want neither while in sync", *calls, rec.String())
	}
}

func TestEnterUnreadableScreenLeavesPromptUnknown(t *testing.T) {
	c, _, _ := enterController("", false)
	var seen []input.State
	c.BeforeSubmit = func(s input.State) []byte {
		seen = append(seen, s)
		return nil
	}
	c.Filter([]byte("\x1b[A"))
	c.Filter([]byte("\r"))
	if len(seen) != 1 || seen[0].Synced {
		t.Fatalf("BeforeSubmit saw %+v, want the prompt still unknown", seen)
	}
}
