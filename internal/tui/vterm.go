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
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	cpty "github.com/creack/pty"
	"github.com/rivo/uniseg"
	"golang.org/x/term"

	"github.com/pmvieira93/prompt-engine-cli/internal/diff"
	"github.com/pmvieira93/prompt-engine-cli/internal/ghost"
	"github.com/pmvieira93/prompt-engine-cli/internal/input"
	"github.com/pmvieira93/prompt-engine-cli/internal/pty"
	"github.com/pmvieira93/prompt-engine-cli/internal/rules"
)

// Options configures Run.
type Options struct {
	Ctrl    *ghost.Controller
	Tracker *input.Tracker
	// PanelPercent returns the side panel's width as a percentage of the
	// terminal. It's read at every layout, so a reloaded config applies the
	// next time the panel opens or the window resizes.
	PanelPercent  func() int
	OnFilterError func(error)
	Logf          func(format string, a ...any) // optional
	// Rules, if set, gets told when the child can take inserted text.
	Rules *rules.Injector
	// Status, if set, returns lines describing the session (e.g. which
	// config is in use) for the side panel. It's called at every redraw.
	Status func() []string
	// Diff, if set, returns the prompt comparison the side panel shows
	// (PRD FR-5), or nil when there is none yet. Called at every redraw.
	Diff func() *Diff
	// PanelLeft reports whether the panel goes left of the child rather
	// than right. It's read at every layout. Nil means right.
	PanelLeft func() bool
	// PanelOpen opens the panel at startup.
	PanelOpen bool
	// RawOutput, if set, receives a copy of everything the child writes,
	// for debugging how it draws. Writes happen on the output goroutine.
	RawOutput io.Writer

	// PanicOnGhost makes the renderer panic when it first draws ghost text,
	// to exercise the pass-through fallback. Testing only.
	PanicOnGhost bool
}

// Diff is what the side panel compares: what was typed against what was,
// or will be, sent.
type Diff struct {
	Title string // e.g. "Last prompt sent"
	Ops   []diff.Op
	Notes []string // e.g. "+6 −3 words · expanded · rules"
}

// Run executes cmd inside the emulator-backed UI and returns its exit code.
func Run(cmd *exec.Cmd, opt Options) (int, error) {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		cols, rows = 80, 24
	}
	// The GC lets the heap grow to twice the live heap (~12 MB at 200x60)
	// between cycles, which lands just over the PRD's 30 MB budget during
	// heavy output. A soft limit caps that with room to spare; measured at
	// 200x60 while streaming, it holds ~24 MB with no extra CPU.
	debug.SetMemoryLimit(memoryLimit)
	a := newApp(cols, rows, opt)
	// 120 FPS (Bubble Tea's cap) halves the frame-tick share of echo latency;
	// idle cost is unaffected since frames are only produced on change.
	a.prog = tea.NewProgram(&model{app: a}, tea.WithInput(nil), tea.WithoutSignalHandler(), tea.WithFPS(120))

	opt.Ctrl.OnChange = a.Wake
	opt.Ctrl.OnTogglePanel = a.TogglePanel
	opt.Ctrl.Gate = a.cursorInPrompt
	opt.Ctrl.SetScreen(a.promptText)
	opt.Ctrl.SetPanelOpen(a.panel.Load)
	opt.Ctrl.CanInsert = a.canInsert
	opt.Ctrl.OnResizePanel = a.ResizePanel
	opt.Ctrl.MouseOffset = a.mouseOffset
	// Start the child at the size it will have: narrower if the panel opens
	// right away. Shrinking it just after it first draws left claude's
	// screen garbled until the next resize.
	a.panel.Store(opt.PanelOpen)
	a.termWidth.Store(int32(cols))
	startCols, startRows := a.childSize(cols, rows)
	a.emu.Resize(startCols, startRows)
	if opt.Rules != nil {
		opt.Rules.Ready = a.canInsert
	}
	go a.wakeLoop()
	go func() {
		for range opt.Tracker.Subscribe() {
			a.Wake() // the panel shows the tracked prompt
		}
	}()

	var sessionDone atomic.Bool
	teaDone := make(chan error, 1)
	go func() {
		_, err := a.prog.Run()
		if p := a.renderErr.Load(); p != nil {
			err = *p
		}
		if !sessionDone.Load() {
			// The renderer died under a live session (e.g. a panic in View,
			// which Bubble Tea recovers and restores the terminal from).
			// Fall back to pass-through rather than freeze the screen (FR-1.3).
			a.enterFallback(err)
		}
		teaDone <- err
	}()

	session := &pty.Session{
		Stdout:        a,
		Filter:        opt.Ctrl,
		OnFilterError: opt.OnFilterError,
		ManualSize:    true,
		StartSize:     func() (int, int) { return startCols, startRows },
		OnStart:       a.attach,
		// Bubble Tea assumes LF→CRLF mapping when it has no input tty.
		KeepOutputProcessing: true,
	}
	code, err := session.Run(cmd)
	sessionDone.Store(true)

	a.prog.Quit()
	<-teaDone
	a.restore(os.Stdout)
	return code, err
}

const (
	scrollbackLines = 1        // x/vt ignores 0; nothing reads the scrollback
	memoryLimit     = 28 << 20 // a ceiling; the live heap is ~12 MB at 200x60
)

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
	panelSteps    atomic.Int32 // ResizePanel steps on top of the configured width
	termWidth     atomic.Int32 // the real terminal's width, for mouseOffset
	resizePending atomic.Bool
	osc           oscFilter // only touched by Write, on the output goroutine
	fallback      atomic.Bool
	renderErr     atomic.Pointer[error]
	wake          chan struct{}
}

func newApp(cols, rows int, opt Options) *app {
	a := &app{
		emu:   vt.NewSafeEmulator(cols, rows),
		opt:   opt,
		modes: map[ansi.DECMode]bool{},
		wake:  make(chan struct{}, 1),
	}
	// Scrollback lines are never displayed (we render in the alternate screen
	// and claude keeps its own history), yet each cell is a ~112-byte struct:
	// even 500 lines cost ~11 MB at 200 columns. Keep the minimum.
	a.emu.SetScrollbackSize(scrollbackLines)
	a.emu.SetCallbacks(vt.Callbacks{
		EnableMode:  func(m ansi.Mode) { a.setMode(m, true) },
		DisableMode: func(m ansi.Mode) { a.setMode(m, false) },
		Bell:        func() { a.queueRaw("\a") },
	})
	a.registerForwarding()
	a.osc.handle = a.handleOSC
	return a
}

// Write receives child output.
func (a *app) Write(p []byte) (int, error) {
	if a.opt.RawOutput != nil {
		_, _ = a.opt.RawOutput.Write(p)
	}
	if a.fallback.Load() {
		return os.Stdout.Write(p)
	}
	if _, err := a.emu.Write(a.osc.Filter(p)); err != nil {
		return 0, err
	}
	a.Wake()
	return len(p), nil
}

// attach is called once the child is running.
func (a *app) attach(ptmx *os.File) {
	a.ptmx.Store(ptmx)
	a.opt.Ctrl.SetOutput(ptmx)
	// Emulator replies are written synchronously from inside emu.Write, so
	// this reader must always be running or output would deadlock.
	go func() { _, _ = io.Copy(ptmx, a.emu) }()
}

// renderFailed records the first render failure and stops the renderer.
// Quit runs on its own goroutine: View is called from Bubble Tea's event
// loop, which must keep running to receive the quit message.
func (a *app) renderFailed(err error) {
	if a.renderErr.CompareAndSwap(nil, &err) {
		go a.prog.Quit()
	}
}

// enterFallback switches to plain pass-through: child output goes straight
// to the terminal, the child's input modes are re-applied (Bubble Tea reset
// them on exit), and a resize makes the child redraw its whole screen.
func (a *app) enterFallback(cause error) {
	if a.opt.Logf != nil {
		a.opt.Logf("renderer stopped (%v); falling back to pass-through", cause)
	}
	a.panel.Store(false)
	a.opt.Ctrl.Disable() // suggestions can't be seen, so keys must not accept them
	a.fallback.Store(true)

	// Bubble Tea left the alternate screen on exit; the child may live there.
	if a.emu.IsAltScreen() {
		_, _ = io.WriteString(os.Stdout, ansi.SetMode(ansi.ModeAltScreenSaveCursor))
	}
	_, _ = io.WriteString(os.Stdout, ansi.EraseEntireScreen+ansi.CursorHomePosition)
	a.mu.Lock()
	for m, on := range a.modes {
		if on {
			_, _ = io.WriteString(os.Stdout, ansi.SetMode(m))
		}
	}
	a.mu.Unlock()

	setSize := func(dw int) {
		w, h, err := term.GetSize(int(os.Stdout.Fd()))
		if p := a.ptmx.Load(); err == nil && p != nil {
			_ = cpty.Setsize(p, &cpty.Winsize{Cols: uint16(w + dw), Rows: uint16(h)})
		}
	}
	// Shrink then restore, so the child repaints even if the size is unchanged.
	// The pause lets it observe the intermediate size; resize handlers are
	// usually debounced.
	setSize(-1)
	time.Sleep(200 * time.Millisecond)
	setSize(0)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			setSize(0)
		}
	}()
}

// promptMarker starts Claude Code's input line. Dialogs (trust, permission,
// menus) don't put the cursor on a line that starts with it.
const promptMarker = "❯"

// cursorInPrompt reports whether the child's visible cursor is on its prompt
// line, using the emulator screen: something a pass-through wrapper can't see.
func (a *app) cursorInPrompt() bool {
	if a.emu.CursorHidden() {
		return false
	}
	pos := a.emu.CursorPosition()
	if a.firstContent(pos.Y, pos.X) == promptMarker {
		return true
	}
	// A continuation row of a multi-line prompt: walk up to the marker's
	// row. It must sit right under the input box's top rule, since earlier
	// prompts in the transcript are drawn with the marker too.
	for y := pos.Y - 1; y >= 0; y-- {
		switch a.firstContent(y, a.emu.Width()) {
		case promptMarker:
			return a.isRule(y - 1)
		case "─":
			return false
		}
	}
	return false
}

// firstContent returns the first non-blank cell's content in row y,
// looking at columns before limit, or "" if there is none.
func (a *app) firstContent(y, limit int) string {
	if y < 0 {
		return ""
	}
	for x := 0; x < limit; x++ {
		if c := a.emu.CellAt(x, y); c != nil && !isBlank(c.Content) {
			return c.Content
		}
	}
	return ""
}

// promptText reads claude's prompt from the screen: the text after the
// prompt marker on the cursor's row, and the cursor offset in runes. It only
// reports a prompt that fits on one row, with the input box's bottom rule
// directly below, so a wrapped or multi-line prompt is never half-read. Faint
// cells are skipped: they are claude's placeholder ("Try ..."), not text.
func (a *app) promptText() (text string, cursor int, ok bool) {
	if a.emu.CursorHidden() {
		return "", 0, false
	}
	pos := a.emu.CursorPosition()
	marker := -1
	for x := 0; x < pos.X; x++ {
		c := a.emu.CellAt(x, pos.Y)
		if c == nil || isBlank(c.Content) {
			continue
		}
		if c.Content == promptMarker {
			marker = x
		}
		break
	}
	start := marker + 2 // the marker, then a (non-breaking) space
	if marker < 0 || pos.X < start || !a.isRule(pos.Y+1) {
		return "", 0, false
	}

	w := a.emu.Width()
	var rs []rune
	cursor, end := -1, 0
	for x := start; x < w; x++ {
		if x == pos.X {
			cursor = len(rs)
		}
		c := a.emu.CellAt(x, pos.Y)
		if c == nil || c.Content == "" || c.Style.Attrs&uv.AttrFaint != 0 {
			continue // wide-character continuation, or placeholder
		}
		if x == w-1 && !isBlank(c.Content) {
			return "", 0, false // may continue on the next row
		}
		rs = append(rs, []rune(c.Content)...)
		if !isBlank(c.Content) {
			end = len(rs)
		}
	}
	if cursor < 0 {
		cursor = len(rs)
	}
	// Keep spaces typed before the cursor; drop the row's trailing padding.
	return string(rs[:max(end, cursor)]), cursor, true
}

// canInsert reports whether text can be inserted into claude's prompt: the
// cursor is in it, and bracketed paste is on so newlines don't submit.
func (a *app) canInsert() bool {
	a.mu.Lock()
	paste := a.modes[ansi.ModeBracketedPaste]
	a.mu.Unlock()
	return paste && a.cursorInPrompt()
}

// isRule reports whether row y is a horizontal rule, like the lines above
// and below claude's input box.
func (a *app) isRule(y int) bool {
	if y >= a.emu.Height() {
		return false
	}
	for x := range a.emu.Width() {
		c := a.emu.CellAt(x, y)
		if c == nil || isBlank(c.Content) {
			continue
		}
		return c.Content == "─"
	}
	return false
}

func isBlank(s string) bool { return s == "" || s == " " || s == "\u00a0" }

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

// panelStep is how much ResizePanel changes the panel's width, in percent.
const panelStep = 5

// ResizePanel widens (delta > 0) or narrows (delta < 0) the panel by
// panelStep percent per step, within 10–90%. It's kept on top of the
// configured panel.width.
func (a *app) ResizePanel(delta int) {
	a.panelSteps.Add(int32(delta))
	if a.panel.Load() {
		a.resizePending.Store(true)
	}
	a.Wake()
}

// panelPercent is the panel's current width as a percentage.
func (a *app) panelPercent() int {
	p := a.opt.PanelPercent() + int(a.panelSteps.Load())*panelStep
	clamped := min(max(p, 10), 90)
	if clamped != p { // don't let steps pile up past the limit
		a.panelSteps.Store(int32((clamped - a.opt.PanelPercent()) / panelStep))
	}
	return clamped
}

// leftPanel reports whether the panel is open on the left of the child.
func (a *app) leftPanel() bool {
	return a.panel.Load() && a.opt.PanelLeft != nil && a.opt.PanelLeft()
}

// mouseOffset is how far right of the terminal's column 1 the child's
// screen starts: the panel's width when it's on the left.
func (a *app) mouseOffset() int {
	if !a.leftPanel() {
		return 0
	}
	return int(a.termWidth.Load()) - a.emu.Width()
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
		return w - panelWidth(w, a.panelPercent()), h
	}
	return w, h
}

func panelWidth(w, percent int) int {
	return max(w*percent/100, 20)
}

func (a *app) resizeChild(w, h int) {
	a.termWidth.Store(int32(w))
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

// drawEmulator copies the emulator's screen onto scr. It replaces
// vt.Emulator.Draw, which heap-allocates a clone of every cell on every
// frame (about 1.5 MB at 200x60); the canvas stores cells by value, so one
// reused cell is enough. Unlike Draw, it doesn't substitute default colors
// set by the child with OSC 10/11: cells without a color keep the real
// terminal's defaults.
func drawEmulator(scr uv.Screen, emu *vt.SafeEmulator, ox, w, h int) {
	var c uv.Cell
	for y := range h {
		for x := 0; x < w; {
			src := emu.CellAt(x, y)
			if src == nil {
				x++
				continue
			}
			c = *src
			scr.SetCell(ox+x, y, &c)
			x += max(c.Width, 1)
		}
	}
}

var ghostStyle = uv.Style{Fg: ansi.IndexedColor(244), Attrs: uv.AttrFaint}

// View renders one frame. A panic while drawing is recovered here rather
// than by Bubble Tea, whose handler prints a stack trace over the session;
// the renderer then quits and Run falls back to pass-through.
func (m *model) View() (v tea.View) {
	defer func() {
		if r := recover(); r != nil {
			m.app.renderFailed(fmt.Errorf("render panic: %v", r))
			v = tea.NewView("")
		}
	}()
	return m.view()
}

func (m *model) view() tea.View {
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
	ox := 0 // where the child's screen starts: right of a left panel
	if m.app.leftPanel() && ew < m.w {
		ox = m.w - ew
	}
	drawEmulator(m.canvas, emu, ox, ew, eh)
	pos := emu.CursorPosition()
	if sug := m.app.opt.Ctrl.Visible(); sug != "" {
		if m.app.opt.PanicOnGhost {
			panic("PanicOnGhost: simulated renderer failure")
		}
		drawGhost(m.canvas, ox+pos.X, pos.Y, ox+ew, sug)
	} else if note := m.app.opt.Ctrl.Notice(); note != "" {
		drawGhost(m.canvas, ox+pos.X, pos.Y, ox+ew, "  "+note)
	}
	if m.app.panel.Load() && ew < m.w {
		// lipgloss Layer.Draw ignores X/Y and repaints its whole area, so
		// draw the panel into its own rectangle directly.
		px := ew
		if ox > 0 {
			px = 0
		}
		uv.NewStyledString(m.panelView(m.w-ew)).Draw(m.canvas, uv.Rect(px, 0, m.w-ew, m.h))
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
		c := tea.NewCursor(ox+pos.X, pos.Y)
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

var (
	panelTitle = lipgloss.NewStyle().Bold(true)
	panelDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	diffInsert = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	diffDelete = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Strikethrough(true)
)

func (m *model) panelView(w int) string {
	keys := m.app.opt.Ctrl.CurrentKeys()
	lines := []string{
		panelTitle.Render("Prompt diff"),
		panelDim.Render(fmt.Sprintf("%s then %s: hide · %s/%s: width · %s: expand · %s: undo",
			keys.Leader, keys.Panel, keys.Wider, keys.Narrower, keys.Expand, keys.Undo)),
		"",
	}
	var d *Diff
	if m.app.opt.Diff != nil {
		d = m.app.opt.Diff()
	}
	if d == nil {
		lines = append(lines, panelDim.Render("Nothing sent yet. Prompts you send appear here, with what prompt-engine added in green."))
	} else {
		lines = append(lines, panelTitle.Render(d.Title), renderDiff(d.Ops))
		for _, n := range d.Notes {
			lines = append(lines, panelDim.Render(n))
		}
	}
	if m.app.opt.Status != nil {
		lines = append(lines, "", panelTitle.Render("Session"))
		lines = append(lines, m.app.opt.Status()...)
	}
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	// Width wraps long lines (a long prompt, a config path) rather than
	// cutting them off.
	return panelBorder.Width(w).Height(m.h).MaxWidth(w).Render(body)
}

// renderDiff shows ops inline: unchanged text as is, additions in green,
// removals in red and struck through.
func renderDiff(ops []diff.Op) string {
	var b strings.Builder
	for _, op := range ops {
		switch op.Kind {
		case diff.Insert:
			b.WriteString(styleLines(diffInsert, op.Text))
		case diff.Delete:
			b.WriteString(styleLines(diffDelete, op.Text))
		default:
			b.WriteString(op.Text)
		}
	}
	return b.String()
}

// styleLines styles each line of text separately, so line breaks inside a
// change don't carry the style onto the next line.
func styleLines(st lipgloss.Style, text string) string {
	parts := strings.Split(text, "\n")
	for i, p := range parts {
		if p != "" {
			parts[i] = st.Render(p)
		}
	}
	return strings.Join(parts, "\n")
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
