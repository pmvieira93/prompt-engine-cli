package input

import "sync"

// State is a snapshot of the tracked prompt.
type State struct {
	Text   string
	Cursor int // in runes
	Synced bool
}

// Tracker is a pass-through pty.Filter that keeps a Buffer in step with the
// user's input. It is safe to read from other goroutines while the PTY input
// goroutine feeds it.
type Tracker struct {
	mu      sync.Mutex
	dec     Decoder
	buf     *Buffer
	changed chan struct{}
}

// NewTracker returns a Tracker with an empty buffer.
func NewTracker() *Tracker {
	return &Tracker{buf: NewBuffer(), changed: make(chan struct{}, 1)}
}

// Filter decodes p into the buffer and returns p unchanged.
func (t *Tracker) Filter(p []byte) []byte {
	t.mu.Lock()
	rev := t.buf.Rev()
	for _, k := range t.dec.Decode(p) {
		t.buf.Apply(k)
	}
	dirty := t.buf.Rev() != rev
	t.mu.Unlock()

	if dirty {
		select {
		case t.changed <- struct{}{}:
		default: // a notification is already pending
		}
	}
	return p
}

// Snapshot returns the current prompt state.
func (t *Tracker) Snapshot() State {
	t.mu.Lock()
	defer t.mu.Unlock()
	return State{Text: t.buf.Text(), Cursor: t.buf.Cursor(), Synced: t.buf.Synced()}
}

// Changed receives after the state changes. Notifications coalesce: a slow
// reader sees one signal for many changes and should call Snapshot.
func (t *Tracker) Changed() <-chan struct{} { return t.changed }
