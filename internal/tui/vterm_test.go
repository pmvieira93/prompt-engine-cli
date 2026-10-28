package tui

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/pmvieira93/prompt-engine-cli/internal/diff"
	"github.com/pmvieira93/prompt-engine-cli/internal/ghost"
	"github.com/pmvieira93/prompt-engine-cli/internal/input"
)

func newTestModel(w, h int) *model {
	tr := input.NewTracker()
	a := newApp(w, h, Options{Ctrl: &ghost.Controller{Tracker: tr}, Tracker: tr, PanelPercent: func() int { return 40 }})
	return &model{app: a, w: w, h: h}
}

func screenLines(m *model) []string {
	return strings.Split(ansi.Strip(m.View().Content), "\n")
}

func TestViewPanelLayout(t *testing.T) {
	m := newTestModel(80, 12)
	m.app.emu.Write([]byte("hello"))
	m.app.panel.Store(true)
	m.app.resizeChild(80, 12)
	lines := screenLines(m)
	if len(lines) != 12 {
		t.Fatalf("got %d lines, want 12", len(lines))
	}
	for i, l := range lines {
		t.Logf("%2d|%s|", i, l)
		if w := ansi.StringWidth(l); w != 80 {
			t.Errorf("line %d width %d, want 80", i, w)
		}
	}
	if !strings.HasPrefix(lines[0], "hello") {
		t.Errorf("child content lost: %q", lines[0])
	}
	if got := strings.Index(lines[1], "│"); got < 0 || ansi.StringWidth(lines[1][:got]) != 48 {
		t.Errorf("panel border not at column 48: %q", lines[1])
	}
}

func TestCursorInPrompt(t *testing.T) {
	cases := map[string]bool{
		"❯ refa":           true,
		"  ❯ refa":         true,  // indented prompt
		"   ❯ No, exit":    true,  // menu items use the marker too; cursor would be hidden
		"> refa":           false, // not claude's prompt
		"Trust this? refa": false,
	}
	for screen, want := range cases {
		m := newTestModel(40, 3)
		m.app.emu.Write([]byte(screen))
		if got := m.app.cursorInPrompt(); got != want {
			t.Errorf("%q: cursorInPrompt = %v, want %v", screen, got, want)
		}
	}
	m := newTestModel(40, 3)
	m.app.emu.Write([]byte("❯ refa\x1b[?25l"))
	if m.app.cursorInPrompt() {
		t.Error("hidden cursor counted as in prompt")
	}
}

func TestViewGhostAtCursor(t *testing.T) {
	m := newTestModel(40, 3)
	m.app.emu.Write([]byte("> impl"))
	m.app.opt.Tracker.Filter([]byte("impl"))
	if !m.app.opt.Ctrl.Show("ement", m.app.opt.Tracker.Snapshot().Rev) {
		t.Fatal("Show refused a current suggestion")
	}
	v := m.View()
	if got := strings.Split(ansi.Strip(v.Content), "\n")[0]; !strings.HasPrefix(got, "> implement") {
		t.Fatalf("line 0 = %q, want ghost after cursor", got)
	}
	if v.Cursor == nil || v.Cursor.X != 6 || v.Cursor.Y != 0 {
		t.Fatalf("cursor = %+v, want (6,0): ghost text must not move it", v.Cursor)
	}
	if !strings.Contains(v.Content, "ement") || !strings.Contains(v.Content, "\x1b[") {
		t.Fatal("ghost text not styled")
	}
}

// TestDrawEmulatorMatchesDraw checks drawEmulator against vt's own Draw,
// including styles, wide characters, and emoji.
func TestDrawEmulatorMatchesDraw(t *testing.T) {
	m := newTestModel(60, 8)
	fillScreen(m)
	m.app.emu.Write([]byte("\x1b[5;1H\x1b[1;4;38;2;255;0;0;48;5;22mbold\x1b[0m 中文 👍🏽 é\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\"))

	want := lipgloss.NewCanvas(60, 8)
	m.app.emu.Draw(want, uv.Rect(0, 0, 60, 8))
	got := lipgloss.NewCanvas(60, 8)
	drawEmulator(got, m.app.emu, 0, 60, 8)

	if g, w := got.Render(), want.Render(); g != w {
		t.Fatalf("drawEmulator differs from Draw:\ngot  %q\nwant %q", g, w)
	}
}

// replies collects what the emulator answers to the child.
type replies struct {
	mu sync.Mutex
	b  strings.Builder
}

func (r *replies) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b.Write(p)
}

func (r *replies) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b.String()
}

func queued(a *app) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return strings.Join(a.raw, "")
}

func TestForwardsOSCToRealTerminal(t *testing.T) {
	m := newTestModel(40, 5)
	r := &replies{}
	go func() { _, _ = io.Copy(r, m.app.emu) }()

	m.app.Write([]byte(
		"\x1b]9;build done ✳\a" +
			"\x1b]9;4;1;50\a" +
			"\x1b]777;notify;claude;needs input\x1b\\" +
			"\x1b]52;c;aGVsbG8=\a" +
			"\x1b]7;file://host/tmp\a" +
			"\x1b]11;?\a" +
			"\x1b]10;rgb:ffff/0000/0000\a"))

	got := queued(m.app)
	for _, want := range []string{
		"\x1b]9;build done ✳\a", "\x1b]9;4;1;50\a", "\x1b]777;notify;claude;needs input\a",
		"\x1b]52;c;aGVsbG8=\a", "\x1b]7;file://host/tmp\a", "\x1b]11;?\a", "\x1b]10;rgb:ffff/0000/0000\a",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("not forwarded intact: %q", want)
		}
	}
	if fg := m.app.emu.ForegroundColor(); fg == nil {
		t.Error("color set was not applied to the emulator")
	}
	time.Sleep(50 * time.Millisecond) // let any reply arrive
	if strings.Contains(r.String(), "]11;") {
		t.Errorf("emulator answered a color query itself: %q", r.String())
	}
}

// TestTitleWithUTF8 reproduces claude's "✳ Three-word greeting" title: its
// 0x9C byte made the emulator end the title early and print the rest of it
// over the prompt.
func TestTitleWithUTF8(t *testing.T) {
	m := newTestModel(40, 3)
	const title = "✳ Three-word greeting"
	seq := "\x1b[2;3H\x1b]0;" + title + "\a❯"
	// Split mid-character, as a read from the child can.
	for _, part := range []string{seq[:9], seq[9:12], seq[12:]} {
		m.app.Write([]byte(part))
	}
	if m.app.title != title {
		t.Errorf("title = %q, want %q", m.app.title, title)
	}
	if screen := m.app.emu.Render(); strings.Contains(screen, "greeting") || !strings.Contains(screen, "❯") {
		t.Fatalf("title text leaked onto the screen, or the output after it was lost: %q", screen)
	}
}

func TestOSCFilter(t *testing.T) {
	var seen []string
	f := &oscFilter{handle: func(p []byte) []byte {
		seen = append(seen, string(p))
		return []byte("<" + string(p) + ">")
	}}
	cases := []struct{ in, out string }{
		{"plain text", "plain text"},
		{"a\x1b]0;t\x07b", "a<0;t>b"},
		{"\x1b]8;;u\x1b\\x", "<8;;u>x"},
		{"\x1b[31mred", "\x1b[31mred"},          // other sequences pass
		{"\x1b\x1b]2;x\a", "\x1b<2;x>"},         // a lone ESC before an OSC
		{"\x1b]2;cut\x1b]2;next\a", "<2;next>"}, // ESC ] cancels and restarts
		{"\x1b]2;cut\x1b[1m", "\x1b[1m"},        // ESC [ cancels
	}
	for _, tc := range cases {
		if got := string(f.Filter([]byte(tc.in))); got != tc.out {
			t.Errorf("Filter(%q) = %q, want %q", tc.in, got, tc.out)
		}
	}
	// Split across writes.
	var got string
	for _, part := range []string{"x\x1b", "]0;ti", "tle\x1b", "\\y"} {
		got += string(f.Filter([]byte(part)))
	}
	if got != "x<0;title>y" {
		t.Errorf("split: got %q", got)
	}
	// A runaway sequence is dropped without printing its payload.
	big := "\x1b]0;" + strings.Repeat("z", maxOSC+10) + "\aend"
	if got := string(f.Filter([]byte(big))); got != "end" {
		t.Errorf("runaway: got %d bytes", len(got))
	}
}

func TestHyperlinkWithC1ByteIsEncoded(t *testing.T) {
	m := newTestModel(40, 3)
	m.app.Write([]byte("\x1b]8;;https://example.com/✳\x1b\\x\x1b]8;;\x1b\\"))
	if c := m.View().Content; !strings.Contains(c, "https://example.com/%E2%9C%B3") {
		t.Fatalf("link not preserved: %q", c)
	}
}

func TestViewKeepsHyperlinks(t *testing.T) {
	m := newTestModel(40, 3)
	m.app.emu.Write([]byte("\x1b]8;;https://example.com\x1b\\docs\x1b]8;;\x1b\\"))
	if c := m.View().Content; !strings.Contains(c, "https://example.com") {
		t.Fatalf("hyperlink dropped from the frame: %q", c)
	}
}

func TestPromptText(t *testing.T) {
	const rule = "\r\n──────────────────────────────"
	cases := []struct {
		name, screen string
		text         string
		cursor       int
		ok           bool
	}{
		{"typed", "❯ refa" + rule + "\x1b[1;7H", "refa", 4, true},
		{"cursor mid-text", "❯ refa" + rule + "\x1b[1;5H", "refa", 2, true},
		{"placeholder is empty", "❯ \x1b[2mTry \"fix the bug\"\x1b[22m" + rule + "\x1b[1;3H", "", 0, true},
		{"trailing space before cursor", "❯ fix " + rule + "\x1b[1;7H", "fix ", 4, true},
		{"wide characters", "❯ 中文" + rule + "\x1b[1;7H", "中文", 2, true},
		{"no rule below", "❯ refa\r\nmore text" + "\x1b[1;7H", "", 0, false},
		{"not the prompt", "> refa" + rule + "\x1b[1;7H", "", 0, false},
		{"may wrap", "❯ " + strings.Repeat("x", 38) + rule + "\x1b[1;40H", "", 0, false},
		{"hidden cursor", "❯ refa" + rule + "\x1b[1;7H\x1b[?25l", "", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(40, 4)
			m.app.emu.Write([]byte(tc.screen))
			text, cursor, ok := m.app.promptText()
			if text != tc.text || cursor != tc.cursor || ok != tc.ok {
				t.Fatalf("promptText = (%q, %d, %v), want (%q, %d, %v)", text, cursor, ok, tc.text, tc.cursor, tc.ok)
			}
		})
	}
}

func TestPanelShowsStatus(t *testing.T) {
	m := newTestModel(100, 14)
	path := "/private/tmp/some/long/directory/structure/pe-live.yaml"
	m.app.opt.Status = func() []string {
		return []string{"config:      " + path, "suggestions: fake (fixed word list)"}
	}
	m.app.panel.Store(true)
	m.app.resizeChild(100, 14)
	screen := strings.Join(screenLines(m), "\n")
	for _, want := range []string{"Session", "suggestions: fake", "pe-live.yaml"} {
		if !strings.Contains(screen, want) {
			t.Errorf("panel is missing %q:\n%s", want, screen)
		}
	}
}

func TestCursorInMultiLinePrompt(t *testing.T) {
	const rule = "──────────────────────────────"
	cases := []struct {
		name, screen string
		want         bool
	}{
		{"continuation row", rule + "\r\n❯ Fix it.\r\n\r\n  Context:\r\n  - Branch: main" + "\r\n" + rule + "\x1b[5;17H", true},
		{"earlier prompt in the transcript", "❯ old prompt\r\n  its answer\r\n\r\n" + rule + "\x1b[2;12H", false},
		{"below the input box", rule + "\r\n❯ hi\r\n" + rule + "\r\n  status line\x1b[4;5H", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(40, 8)
			m.app.emu.Write([]byte(tc.screen))
			if got := m.app.cursorInPrompt(); got != tc.want {
				t.Fatalf("cursorInPrompt = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLeftPanelLayout(t *testing.T) {
	m := newTestModel(80, 6)
	m.app.opt.PanelLeft = func() bool { return true }
	m.app.panel.Store(true)
	m.app.resizeChild(80, 6)
	m.app.emu.Write([]byte("hello"))
	lines := screenLines(m)
	pw := 80 - m.app.emu.Width()
	if !strings.HasPrefix(lines[0], "╭") {
		t.Fatalf("panel not on the left: %q", lines[0])
	}
	if got := []rune(lines[0]); len(got) < pw+5 || string(got[pw:pw+5]) != "hello" {
		t.Fatalf("child not drawn right of the panel (width %d): %q", pw, lines[0])
	}
	if off := m.app.mouseOffset(); off != pw {
		t.Fatalf("mouseOffset = %d, want %d", off, pw)
	}
}

func TestResizePanelSteps(t *testing.T) {
	m := newTestModel(100, 6)
	if p := m.app.panelPercent(); p != 40 {
		t.Fatalf("start = %d", p)
	}
	m.app.ResizePanel(1)
	m.app.ResizePanel(1)
	if p := m.app.panelPercent(); p != 50 {
		t.Fatalf("after two steps wider = %d, want 50", p)
	}
	for range 20 {
		m.app.ResizePanel(-1)
	}
	if p := m.app.panelPercent(); p != 10 {
		t.Fatalf("narrowest = %d, want 10", p)
	}
	m.app.ResizePanel(1) // one step back from the limit, not twenty
	if p := m.app.panelPercent(); p != 15 {
		t.Fatalf("after the limit = %d, want 15", p)
	}
}

func TestRenderDiff(t *testing.T) {
	got := renderDiff([]diff.Op{{Kind: diff.Equal, Text: "fix "}, {Kind: diff.Delete, Text: "teh"}, {Kind: diff.Insert, Text: "the\nbug"}})
	if ansi.Strip(got) != "fix tehthe\nbug" {
		t.Fatalf("text = %q", ansi.Strip(got))
	}
	if !strings.Contains(got, "\x1b[") || strings.Count(got, "\n") != 1 {
		t.Fatalf("styling = %q", got)
	}
}

func TestPanelShowsDiff(t *testing.T) {
	m := newTestModel(100, 14)
	m.app.opt.Diff = func() *Diff {
		return &Diff{Title: "Last prompt sent", Ops: diff.Words("fix teh bug", "fix the bug\n\n- End with MANGO."), Notes: []string{"+4 −1 words"}}
	}
	m.app.panel.Store(true)
	m.app.resizeChild(100, 14)
	screen := strings.Join(screenLines(m), "\n")
	for _, want := range []string{"Prompt diff", "Last prompt sent", "MANGO", "+4 −1 words"} {
		if !strings.Contains(screen, want) {
			t.Errorf("panel is missing %q:\n%s", want, screen)
		}
	}
}
