package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/pmvieira93/prompt-engine-cli/internal/ghost"
	"github.com/pmvieira93/prompt-engine-cli/internal/input"
)

func newTestModel(w, h int) *model {
	tr := input.NewTracker()
	a := newApp(w, h, Options{Ctrl: &ghost.Controller{Tracker: tr}, Tracker: tr, PanelPercent: 40})
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
