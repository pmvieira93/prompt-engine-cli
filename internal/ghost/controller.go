// Package ghost manages inline "ghost text" suggestions: it decides when to
// ask for one, holds the visible suggestion, and intercepts the keys that
// accept or dismiss it.
package ghost

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/rivo/uniseg"

	"github.com/pmvieira93/prompt-engine-cli/internal/input"
)

// Controller is a pty.Filter that sits in front of a Tracker. While a
// suggestion is visible, Tab or Right Arrow accepts it (the suggestion is
// sent to the child as typed text instead of the key), Esc dismisses it, and
// any other key hides it and passes through. With no suggestion visible,
// input passes through unchanged.
//
// prompt-engine's own shortcuts are leader-prefixed, like tmux: Leader then
// PanelKey toggles the panel, Leader then Enter submits without
// BeforeSubmit, Leader twice sends Leader itself to the child, and Esc or
// any other key cancels. Terminal reports (mouse motion, focus)
// arriving in between pass through without cancelling.
type Controller struct {
	Tracker *input.Tracker

	// Logf, OnChange, and OnTogglePanel are optional and must not block.
	// OnChange fires when the visible suggestion changes.
	Logf          func(format string, a ...any)
	OnChange      func()
	OnTogglePanel func()
	// Gate, if set, must approve every suggestion before it is shown, e.g.
	// by checking that the child's cursor is in its prompt, not a dialog.
	Gate func() bool

	// Keys are the shortcut keys, set before use or with SetKeys. A zero
	// Leader turns shortcuts off.
	Keys Keys

	// MouseOffset, if set, returns how many columns right of the terminal's
	// first column the child's screen starts (a panel on its left). Mouse
	// reports are shifted by it, and ones over the panel dropped.
	MouseOffset func() int

	// OnResizePanel, if set, is called with +1 after Leader then
	// Keys.Wider, and -1 after Leader then Keys.Narrower. It must not block.
	OnResizePanel func(delta int)

	// OnExpand and OnUndo, if set before use, are called after Leader then
	// ExpandKey or UndoKey, with the prompt as it stands. They must not
	// block: start the work and apply it later with Replace.
	OnExpand, OnUndo func(s input.State)

	// CanInsert, if set, must approve every Replace: e.g. the child's cursor
	// is in its prompt, not a dialog.
	CanInsert func() bool

	// BeforeSubmit, if set before use, is called on each Enter with the
	// prompt as it stands, and returns input to send just before that
	// Enter (e.g. rules to append), or nil. Leader then Enter skips it.
	BeforeSubmit func(s input.State) []byte

	mu       sync.Mutex
	output   io.Writer // to the child, for Replace
	notice   string    // shown at the cursor, e.g. "expanding…"
	screen   func() (text string, cursor int, ok bool)
	dec      input.Decoder
	sug      string
	disabled bool
	armed    bool // Leader was pressed; the next key is a command
}

// Disable hides any suggestion and turns interception off for good, e.g.
// when nothing can render suggestions any more. Input then only feeds the
// tracker.
func (c *Controller) Disable() {
	c.mu.Lock()
	c.disabled, c.sug = true, ""
	c.mu.Unlock()
}

// SetScreen installs a reader for the prompt as the child's screen shows
// it, used to resync the input buffer when replayed keys can't tell what
// the child did with them. It may be called while the worker runs.
func (c *Controller) SetScreen(f func() (text string, cursor int, ok bool)) {
	c.mu.Lock()
	c.screen = f
	c.mu.Unlock()
}

// ScreenPrompt returns the prompt text and cursor (in runes) shown on the
// child's screen, or ok == false if there is no screen reader or the
// prompt can't be read reliably.
func (c *Controller) ScreenPrompt() (text string, cursor int, ok bool) {
	c.mu.Lock()
	f := c.screen
	c.mu.Unlock()
	if f == nil {
		return "", 0, false
	}
	return f()
}

// Keys are prompt-engine's shortcuts: Leader, then one of the others.
type Keys struct {
	Leader, Panel, Expand, Undo, Wider, Narrower input.Binding
}

// SetKeys replaces the shortcut keys, e.g. after the config is edited. It
// may be called while input is being filtered.
func (c *Controller) SetKeys(k Keys) {
	c.mu.Lock()
	c.Keys = k
	c.armed = false
	c.mu.Unlock()
}

// SetOutput sets where Replace writes: the child's input.
func (c *Controller) SetOutput(w io.Writer) {
	c.mu.Lock()
	c.output = w
	c.mu.Unlock()
}

// SetNotice sets a short message shown at the cursor, like ghost text but
// not acceptable with Tab; "" clears it.
func (c *Controller) SetNotice(msg string) {
	c.mu.Lock()
	changed := c.notice != msg || (msg != "" && c.sug != "")
	c.notice = msg
	if msg != "" {
		c.sug = "" // the notice replaces any suggestion, so Tab can't take it
	}
	c.mu.Unlock()
	if changed && c.OnChange != nil {
		c.OnChange()
	}
}

// Notice returns the message set with SetNotice.
func (c *Controller) Notice() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.notice
}

// ErrStale reports a Replace for a prompt that has changed since.
var ErrStale = errors.New("the prompt changed meanwhile")

// Replace swaps the prompt text from for to in the child, as typed input:
// it moves to the end, deletes from character by character, and types to,
// with line breaks as Alt+Enter so they don't submit. Typed rather than
// pasted, because claude collapses a multi-line paste to "[Pasted text]"
// and the user couldn't review it. It only applies if the prompt is still
// from at revision rev, so it never overwrites what the user typed since.
func (c *Controller) Replace(rev uint64, from, to string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.Tracker.Snapshot()
	switch {
	case c.output == nil:
		return errors.New("no output to the child")
	case s.Rev != rev || !s.Synced || s.Text != from:
		return ErrStale
	case c.CanInsert != nil && !c.CanInsert():
		return errors.New("the cursor isn't in the prompt")
	}
	var b strings.Builder
	rs := []rune(s.Text)
	for range uniseg.GraphemeClusterCount(string(rs[s.Cursor:])) {
		b.WriteString("\x1b[C")
	}
	b.WriteString(strings.Repeat("\x7f", uniseg.GraphemeClusterCount(from)))
	b.WriteString(typed(to))
	c.sug = ""
	_, err := c.output.Write(c.Tracker.Filter([]byte(b.String())))
	return err
}

// shiftMouse moves an SGR mouse report (ESC [ < b ; x ; y M|m) dx columns
// left, so its column is relative to the child's screen. It reports false
// for a position left of that screen, over the panel.
func shiftMouse(raw []byte, dx int) ([]byte, bool) {
	if dx == 0 {
		return raw, true
	}
	body, final := raw[3:len(raw)-1], raw[len(raw)-1]
	parts := strings.Split(string(body), ";")
	if len(parts) != 3 {
		return raw, true
	}
	x, err := strconv.Atoi(parts[1])
	if err != nil {
		return raw, true
	}
	if x -= dx; x < 1 {
		return nil, false
	}
	return fmt.Appendf(nil, "\x1b[<%s;%d;%s%c", parts[0], x, parts[2], final), true
}

// typed encodes text as keystrokes: line breaks as Alt+Enter (a new line
// in claude's prompt), tabs as spaces (Tab would autocomplete), and other
// control characters dropped.
func typed(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var b strings.Builder
	for _, r := range text {
		switch {
		case r == '\n' || r == '\r':
			b.WriteString("\x1b\r")
		case r == '\t':
			b.WriteString("    ")
		case r < 0x20 || r == 0x7f:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CurrentKeys returns the current shortcut keys.
func (c *Controller) CurrentKeys() Keys {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Keys
}

// Disabled reports whether Disable was called.
func (c *Controller) Disabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disabled
}

// Filter implements pty.Filter.
func (c *Controller) Filter(p []byte) []byte {
	c.mu.Lock()
	if c.disabled {
		defer c.mu.Unlock()
		return c.Tracker.Filter(p)
	}
	before := c.sug
	toggles := 0
	var actions []func() // OnExpand/OnUndo calls, run after unlocking
	var res []byte       // input already fed to the tracker
	out := make([]byte, 0, len(p))
	for _, k := range c.dec.Decode(p) {
		switch {
		case c.armed && k.Type == input.KeyReport:
			out = append(out, k.Raw...)
		case c.armed:
			c.armed = false
			switch {
			case c.Keys.Panel.MatchesHeld(k):
				c.logf("leader: toggle panel")
				toggles++
				out = append(out, c.Keys.Panel.Rest(k)...)
			case c.Keys.Wider.Matches(k) && c.OnResizePanel != nil, c.Keys.Narrower.Matches(k) && c.OnResizePanel != nil:
				delta := 1
				if c.Keys.Narrower.Matches(k) {
					delta = -1
				}
				c.logf("leader: resize panel %+d", delta)
				actions = append(actions, func() { c.OnResizePanel(delta) })
			case c.Keys.Expand.MatchesHeld(k) && c.OnExpand != nil, c.Keys.Undo.MatchesHeld(k) && c.OnUndo != nil:
				expand := c.Keys.Expand.MatchesHeld(k)
				// Bring the tracker up to this key, so the action sees the
				// prompt as it is now.
				res = append(res, c.Tracker.Filter(out)...)
				out = out[:0]
				s, fn := c.Tracker.Snapshot(), c.OnUndo
				if expand {
					c.logf("leader: expand prompt")
					fn = c.OnExpand
				} else {
					c.logf("leader: undo expansion")
				}
				actions = append(actions, func() { fn(s) })
				rest := c.Keys.Undo.Rest(k)
				if expand {
					rest = c.Keys.Expand.Rest(k)
				}
				out = append(out, rest...)
			case c.Keys.Leader.Matches(k):
				c.logf("leader: sent %s to child", c.Keys.Leader)
				out = append(out, k.Raw...)
			case k.Type == input.KeyEnter:
				c.logf("leader: submit as typed")
				out = append(out, k.Raw...)
			case k.Type == input.KeyEscape:
				c.logf("leader: cancelled")
			default:
				c.logf("leader: no shortcut for %s", k)
				if k.Type == input.KeyRunes && len(k.Runes) > 1 {
					out = append(out, string(k.Runes[1:])...) // only the first character was the command
				}
			}
		case !c.Keys.Leader.IsZero() && c.Keys.Leader.Matches(k):
			c.armed = true
			c.logf("leader: waiting for a shortcut key")
		case c.sug != "" && (k.Type == input.KeyTab || k.Type == input.KeyRight):
			c.logf("suggestion accepted %q", c.sug)
			out = append(out, c.sug...)
			c.sug = ""
		case c.sug != "" && k.Type == input.KeyEscape:
			c.logf("suggestion dismissed (esc) %q", c.sug)
			c.sug = ""
		case k.Type == input.KeyEnter && c.BeforeSubmit != nil:
			if c.sug != "" {
				c.logf("suggestion dismissed (typing) %q", c.sug)
				c.sug = ""
			}
			// Bring the tracker up to this Enter, so BeforeSubmit sees text
			// typed or pasted in the same read.
			res = append(res, c.Tracker.Filter(out)...)
			out = append(out[:0], c.BeforeSubmit(c.Tracker.Snapshot())...)
			out = append(out, k.Raw...)
		case k.Type == input.KeyReport && c.MouseOffset != nil && bytes.HasPrefix(k.Raw, []byte("\x1b[<")):
			if raw, ok := shiftMouse(k.Raw, c.MouseOffset()); ok {
				out = append(out, raw...)
			}
		default:
			if c.sug != "" && k.Type != input.KeyReport {
				c.logf("suggestion dismissed (typing) %q", c.sug)
				c.sug = ""
			}
			out = append(out, k.Raw...)
		}
	}
	// Feed the tracker while still holding mu, so Show can't publish a
	// suggestion for a buffer state this input is about to replace.
	res = append(res, c.Tracker.Filter(out)...)
	changed := c.sug != before
	c.mu.Unlock()

	if changed && c.OnChange != nil {
		c.OnChange()
	}
	for range toggles {
		if c.OnTogglePanel != nil {
			c.OnTogglePanel()
		}
	}
	for _, act := range actions {
		act()
	}
	return res
}

// Show makes s the visible suggestion if the prompt is still at revision
// rev. It reports whether the suggestion was shown.
func (c *Controller) Show(s string, rev uint64) bool {
	c.mu.Lock()
	ok := s != "" && !c.disabled && c.notice == "" && c.Tracker.Snapshot().Rev == rev && (c.Gate == nil || c.Gate())
	if ok {
		c.sug = s
	}
	c.mu.Unlock()
	if ok && c.OnChange != nil {
		c.OnChange()
	}
	return ok
}

// Visible returns the suggestion currently on screen, or "".
func (c *Controller) Visible() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sug
}

func (c *Controller) logf(format string, a ...any) {
	if c.Logf != nil {
		c.Logf(format, a...)
	}
}
