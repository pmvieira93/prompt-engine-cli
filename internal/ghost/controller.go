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

	mu       sync.Mutex
	dec      input.Decoder
	sug      string
	disabled bool
}

// Disable hides any suggestion and turns interception off for good, e.g.
// when nothing can render suggestions any more. Input then only feeds the
// tracker.
func (c *Controller) Disable() {
	c.mu.Lock()
	c.disabled, c.sug = true, ""
	c.mu.Unlock()
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
		case k.Type == input.KeyCtrl && k.Ctrl == 'p' && c.OnTogglePanel != nil:
			toggles++ // swallowed: reserved for the side panel
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
		c.OnTogglePanel()
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
