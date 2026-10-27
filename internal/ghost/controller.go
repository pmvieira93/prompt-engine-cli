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
// PanelKey toggles the panel, Leader twice sends Leader itself to the child,
// and Esc or any other key cancels. Terminal reports (mouse motion, focus)
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

	mu       sync.Mutex
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
	out = c.Tracker.Filter(out)
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
	return out
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
