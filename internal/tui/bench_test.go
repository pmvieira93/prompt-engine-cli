package tui

import (
	"fmt"
	"strings"
	"testing"
)

// fillScreen writes a full screen of 256-color styled text, roughly what
// claude's transcript looks like, so View has every cell to draw.
func fillScreen(m *model) {
	var b strings.Builder
	for y := range m.h {
		fmt.Fprintf(&b, "\x1b[%d;1H", y+1)
		for x := 0; x < m.w; x += 8 {
			fmt.Fprintf(&b, "\x1b[38;5;%dmword%03d ", (x+y)%256, x)
		}
	}
	b.WriteString("\x1b[0m\x1b[1;1H> impl")
	m.app.emu.Write([]byte(b.String()))
}

func benchView(b *testing.B, w, h int, ghost, panel bool) {
	m := newTestModel(w, h)
	if panel {
		m.app.panel.Store(true)
		m.app.resizeChild(w, h)
	}
	fillScreen(m)
	if ghost {
		m.app.opt.Tracker.Filter([]byte("impl"))
		m.app.opt.Ctrl.Show("ement", m.app.opt.Tracker.Snapshot().Rev)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}

func BenchmarkView80x24(b *testing.B)            { benchView(b, 80, 24, false, false) }
func BenchmarkView200x60(b *testing.B)           { benchView(b, 200, 60, false, false) }
func BenchmarkView200x60Ghost(b *testing.B)      { benchView(b, 200, 60, true, false) }
func BenchmarkView200x60GhostPanel(b *testing.B) { benchView(b, 200, 60, true, true) }
