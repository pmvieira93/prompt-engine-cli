// Package tui renders the wrapped CLI through an in-process terminal
// emulator, so prompt-engine can compose its own UI (ghost text, side panel)
// on top of the child's screen.
//
// Data flow:
//
//	stdin ─► pty.Session filter (ghost.Controller) ─► PTY ─► child
//	child ─► PTY ─► vt emulator ─► Bubble Tea view (+ ghost, panel) ─► stdout
//	emulator replies (CPR, DA2, DECRQM, …) ─► PTY
//
// Bubble Tea is used for rendering only (no input): keystrokes stay
// byte-exact for the child. Input-affecting modes the child sets (mouse,
// focus, bracketed paste, cursor keys, kitty keyboard, modifyOtherKeys) are
// forwarded to the real terminal so it encodes keys the way the child
// expects, and queries the emulator can't answer truthfully (DA1, XTVERSION,
// kitty keyboard flags) are forwarded too, so their replies arrive in order.
package tui

import (
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	cpty "github.com/creack/pty"
	"github.com/rivo/uniseg"
	"golang.org/x/term"

	"github.com/pmvieira93/prompt-engine-cli/internal/ghost"
	"github.com/pmvieira93/prompt-engine-cli/internal/input"
	"github.com/pmvieira93/prompt-engine-cli/internal/pty"
)

// Options configures Run.
type Options struct {
	Ctrl          *ghost.Controller
	Tracker       *input.Tracker
	PanelPercent  int // side panel width as a percentage of the terminal
	OnFilterError func(error)
}

// Run executes cmd inside the emulator-backed UI and returns its exit code.
func Run(cmd *exec.Cmd, opt Options) (int, error) {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		cols, rows = 80, 24
	}
	a := newApp(cols, rows, opt)
	a.prog = tea.NewProgram(&model{app: a}, tea.WithInput(nil), tea.WithoutSignalHandler())

	opt.Ctrl.OnChange = a.Wake
	opt.Ctrl.OnTogglePanel = a.TogglePanel
	go a.wakeLoop()
	go func() {
		for range opt.Tracker.Subscribe() {
			a.Wake() // the panel shows the tracked prompt
		}
	}()

	teaDone := make(chan error, 1)
	go func() {
		_, err := a.prog.Run()
		teaDone <- err
	}()

	session := &pty.Session{
		Stdout:        a,
		Filter:        opt.Ctrl,
		OnFilterError: opt.OnFilterError,
		ManualSize:    true,
		OnStart:       a.attach,
	}
	code, err := session.Run(cmd)

	a.prog.Quit()
	<-teaDone
	a.restore(os.Stdout)
	return code, err
}

type app struct {
	emu  *vt.SafeEmulator
	prog *tea.Program
	opt  Options
	ptmx atomic.Pointer[os.File]

	mu       sync.Mutex
	modes    map[ansi.DECMode]bool // input modes the child enabled
	raw      []string              // sequences queued for the real terminal
	kitty    int                   // kitty keyboard flags pushed on our behalf
	modOther bool
	title    string

	panel         atomic.Bool
	resizePending atomic.Bool
	wake          chan struct{}
}

func newApp(cols, rows int, opt Options) *app {
	a := &app{
		emu:   vt.NewSafeEmulator(cols, rows),
		opt:   opt,
		modes: map[ansi.DECMode]bool{},
		wake:  make(chan struct{}, 1),
	}
	a.emu.SetCallbacks(vt.Callbacks{
		EnableMode:  func(m ansi.Mode) { a.setMode(m, true) },
		DisableMode: func(m ansi.Mode) { a.setMode(m, false) },
		Title:       func(t string) { a.locked(func() { a.title = t }); a.Wake() },
		Bell:        func() { a.queueRaw("\a") },
	})
	a.registerForwarding()
	return a
}

// Write receives child output.
func (a *app) Write(p []byte) (int, error) {
	n, err := a.emu.Write(p)
	a.Wake()
	return n, err
}

// attach is called once the child is running.
func (a *app) attach(ptmx *os.File) {
	a.ptmx.Store(ptmx)
	// Emulator replies are written synchronously from inside emu.Write, so
	// this reader must always be running or output would deadlock.
	go func() { _, _ = io.Copy(ptmx, a.emu) }()
}

// Wake schedules a redraw. It never blocks.
func (a *app) Wake() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

func (a *app) wakeLoop() {
	for range a.wake {
		a.prog.Send(refreshMsg{})
	}
}

// TogglePanel shows or hides the side panel.
func (a *app) TogglePanel() {
	a.panel.Store(!a.panel.Load())
	a.resizePending.Store(true)
	a.Wake()
}

func (a *app) locked(f func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f()
}

func (a *app) queueRaw(seq string) {
	a.locked(func() { a.raw = append(a.raw, seq) })
	a.Wake()
}

func (a *app) setMode(m ansi.Mode, on bool) {
	dm, ok := m.(ansi.DECMode)
	if !ok {
		return
	}
	switch dm {
	case ansi.ModeCursorKeys:
		// Bubble Tea doesn't manage DECCKM; forward it verbatim.
		if on {
			a.queueRaw(ansi.SetMode(dm))
		} else {
			a.queueRaw(ansi.ResetMode(dm))
		}
	case ansi.ModeMouseNormal, ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent,
		ansi.ModeMouseExtSgr, ansi.ModeFocusEvent, ansi.ModeBracketedPaste:
		// Mapped onto the Bubble Tea view in View.
		a.locked(func() { a.modes[dm] = on })
		a.Wake()
	}
}

// registerForwarding claims the sequences that must reach the real terminal.
// Handlers registered here run before the emulator's built-in ones.
func (a *app) registerForwarding() {
	fwd := func(seq string) bool { a.queueRaw(seq); return true }
	csi := a.emu.RegisterCsiHandler

	csi(ansi.Command('>', 0, 'u'), func(p ansi.Params) bool { // kitty push
		flags, _, _ := p.Param(0, 0)
		a.locked(func() { a.kitty++ })
		return fwd(ansi.PushKittyKeyboard(flags))
	})
	csi(ansi.Command('<', 0, 'u'), func(p ansi.Params) bool { // kitty pop
		n, _, _ := p.Param(0, 1)
		a.locked(func() { a.kitty = max(a.kitty-n, 0) })
		return fwd(ansi.PopKittyKeyboard(n))
	})
	csi(ansi.Command('=', 0, 'u'), func(p ansi.Params) bool { // kitty set
		flags, _, _ := p.Param(0, 0)
		mode, _, _ := p.Param(1, 1)
		return fwd(ansi.KittyKeyboard(flags, mode))
	})
	csi(ansi.Command('?', 0, 'u'), func(ansi.Params) bool { return fwd(ansi.RequestKittyKeyboard) })
	csi(ansi.Command('>', 0, 'q'), func(ansi.Params) bool { return fwd("\x1b[>0q") }) // XTVERSION
	csi(ansi.Command(0, 0, 'c'), func(p ansi.Params) bool {                           // DA1
		if n, _, _ := p.Param(0, 0); n != 0 {
			return false
		}
		return fwd("\x1b[c")
	})
	csi(ansi.Command('>', 0, 'm'), func(p ansi.Params) bool { // modifyOtherKeys
		if res, _, _ := p.Param(0, 0); res != 4 {
			return false
		}
		mode, _, _ := p.Param(1, 0)
		a.locked(func() { a.modOther = mode != 0 })
		return fwd(ansi.ModifyOtherKeys(mode))
	})
}

// restore undoes the keyboard modes forwarded on the child's behalf, which
// Bubble Tea doesn't know to reset.
func (a *app) restore(w io.Writer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.kitty > 0 {
		_, _ = io.WriteString(w, ansi.PopKittyKeyboard(a.kitty))
	}
	if a.modOther {
		_, _ = io.WriteString(w, ansi.ModifyOtherKeys(0))
	}
	_, _ = io.WriteString(w, ansi.ResetMode(ansi.ModeCursorKeys))
}

// childSize is the emulator/PTY size for a terminal of w×h cells.
func (a *app) childSize(w, h int) (int, int) {
	if a.panel.Load() {
		return w - panelWidth(w, a.opt.PanelPercent), h
	}
	return w, h
}

func panelWidth(w, percent int) int {
	return max(w*percent/100, 20)
}

func (a *app) resizeChild(w, h int) {
	cw, ch := a.childSize(w, h)
	if cw < 1 || ch < 1 {
		return
	}
	a.emu.Resize(cw, ch)
	if p := a.ptmx.Load(); p != nil {
		_ = cpty.Setsize(p, &cpty.Winsize{Cols: uint16(cw), Rows: uint16(ch)})
	}
}

type refreshMsg struct{}

type model struct {
	app    *app
	w, h   int
	canvas *lipgloss.Canvas
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.app.resizeChild(m.w, m.h)
	case refreshMsg:
		if m.app.resizePending.Swap(false) {
			m.app.resizeChild(m.w, m.h)
		}
		var raw []string
		m.app.locked(func() { raw, m.app.raw = m.app.raw, nil })
		if len(raw) > 0 {
			cmds := make([]tea.Cmd, len(raw))
			for i, s := range raw {
				cmds[i] = tea.Raw(s)
			}
			return m, tea.Sequence(cmds...)
		}
	}
	return m, nil
}

var ghostStyle = uv.Style{Fg: ansi.IndexedColor(244), Attrs: uv.AttrFaint}

func (m *model) View() tea.View {
	if m.w == 0 || m.h == 0 {
		return tea.NewView("")
	}
	if m.canvas == nil || m.canvas.Width() != m.w || m.canvas.Height() != m.h {
		m.canvas = lipgloss.NewCanvas(m.w, m.h)
	} else {
		m.canvas.Clear()
	}

	emu := m.app.emu
	ew, eh := emu.Width(), emu.Height()
	emu.Draw(m.canvas, uv.Rect(0, 0, ew, eh))
	pos := emu.CursorPosition()
	if sug := m.app.opt.Ctrl.Visible(); sug != "" {
		drawGhost(m.canvas, pos.X, pos.Y, ew, sug)
	}
	if m.app.panel.Load() && ew < m.w {
		// lipgloss Layer.Draw ignores X/Y and repaints its whole area, so
		// draw the panel into its own rectangle directly.
		uv.NewStyledString(m.panelView(m.w-ew)).Draw(m.canvas, uv.Rect(ew, 0, m.w-ew, m.h))
	}

	v := tea.NewView(m.canvas.Render())
	v.AltScreen = true
	m.app.mu.Lock()
	switch {
	case m.app.modes[ansi.ModeMouseAnyEvent]:
		v.MouseMode = tea.MouseModeAllMotion
	case m.app.modes[ansi.ModeMouseButtonEvent], m.app.modes[ansi.ModeMouseNormal]:
		v.MouseMode = tea.MouseModeCellMotion
	}
	v.ReportFocus = m.app.modes[ansi.ModeFocusEvent]
	v.DisableBracketedPasteMode = !m.app.modes[ansi.ModeBracketedPaste]
	v.WindowTitle = m.app.title
	m.app.mu.Unlock()

	if !emu.CursorHidden() {
		c := tea.NewCursor(pos.X, pos.Y)
		style, blink := emu.CursorStyle()
		c.Shape, c.Blink = cursorShape(style), blink
		v.Cursor = c
	}
	return v
}

// drawGhost writes s dimmed from (x, y), clipped at the child's right edge.
func drawGhost(scr uv.Screen, x, y, maxX int, s string) {
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		cluster := g.Str()
		w := uniseg.StringWidth(cluster)
		if cluster == "\n" || x+w > maxX {
			return
		}
		scr.SetCell(x, y, &uv.Cell{Content: cluster, Width: w, Style: ghostStyle})
		x += w
	}
}

var panelBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("8")).Padding(0, 1)

func (m *model) panelView(w int) string {
	s := m.app.opt.Tracker.Snapshot()
	body := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Render("Prompt diff (FR-5 stub)"),
		"",
		"Ctrl+P hides this panel.",
		"",
		"buffer:     "+quote(s.Text),
		"synced:     "+boolStr(s.Synced),
		"suggestion: "+quote(m.app.opt.Ctrl.Visible()),
	)
	return panelBorder.Width(w).Height(m.h).MaxWidth(w).Render(body)
}

func cursorShape(s vt.CursorStyle) tea.CursorShape {
	switch s {
	case vt.CursorUnderline:
		return tea.CursorUnderline
	case vt.CursorBar:
		return tea.CursorBar
	}
	return tea.CursorBlock
}

func quote(s string) string { return "\"" + s + "\"" }

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
