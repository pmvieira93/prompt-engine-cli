// Package ghost manages inline "ghost text" suggestions: it decides when to
// ask for one, holds the visible suggestion, and intercepts the keys that
// accept or dismiss it.
package ghost

import (
	"sync"

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

	// Leader and PanelKey are the configured shortcut keys. A zero Leader
	// turns shortcuts off.
	Leader, PanelKey input.Binding

	// BeforeSubmit, if set before use, is called on each Enter with the
	// prompt as it stands, and returns input to send just before that
	// Enter (e.g. rules to append), or nil. Leader then Enter skips it.
	BeforeSubmit func(s input.State) []byte

	mu       sync.Mutex
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

// SetKeys replaces the shortcut keys, e.g. after the config is edited. It
// may be called while input is being filtered.
func (c *Controller) SetKeys(leader, panel input.Binding) {
	c.mu.Lock()
	c.Leader, c.PanelKey = leader, panel
	c.armed = false
	c.mu.Unlock()
}

// Keys returns the current shortcut keys.
func (c *Controller) Keys() (leader, panel input.Binding) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Leader, c.PanelKey
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
	var res []byte // input already fed to the tracker
	out := make([]byte, 0, len(p))
	for _, k := range c.dec.Decode(p) {
		switch {
		case c.armed && k.Type == input.KeyReport:
			out = append(out, k.Raw...)
		case c.armed:
			c.armed = false
			switch {
			case c.PanelKey.Matches(k):
				c.logf("leader: toggle panel")
				toggles++
				out = append(out, c.PanelKey.Rest(k)...)
			case c.Leader.Matches(k):
				c.logf("leader: sent %s to child", c.Leader)
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
		case !c.Leader.IsZero() && c.Leader.Matches(k):
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
	return res
}

// Show makes s the visible suggestion if the prompt is still at revision
// rev. It reports whether the suggestion was shown.
func (c *Controller) Show(s string, rev uint64) bool {
	c.mu.Lock()
	ok := s != "" && !c.disabled && c.Tracker.Snapshot().Rev == rev && (c.Gate == nil || c.Gate())
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
