package input

import (
	"slices"
	"sync"
)

// State is a snapshot of the tracked prompt.
type State struct {
	Text   string
	Cursor int // in runes
	Synced bool
	// Slack is how many characters Text may lack: it was read from a
	// prompt spanning rows on the screen, which can hide a space at each
	// break. 0 when it's exact.
	Slack int
	// Pastes is what was pasted into the prompt since it was last
	// cleared, oldest first; edits since may have changed it.
	Pastes []string
	// Rev identifies this state: it changes whenever text, cursor, or sync
	// state does, so a result computed for one State can be checked for
	// staleness later.
	Rev uint64
}

// Tracker is a pass-through pty.Filter that keeps a Buffer in step with the
// user's input. It is safe to read from other goroutines while the PTY input
// goroutine feeds it.
type Tracker struct {
	// OnKeys, if set before use, sees each input chunk and the keys decoded
	// from it. It runs on the input goroutine and must not block.
	OnKeys func(raw []byte, keys []Key)

	mu   sync.Mutex
	dec  Decoder
	buf  *Buffer
	subs []chan struct{}
}

// NewTracker returns a Tracker with an empty buffer.
func NewTracker() *Tracker {
	return &Tracker{buf: NewBuffer(), subs: []chan struct{}{make(chan struct{}, 1)}}
}

// Filter decodes p into the buffer and returns p unchanged.
func (t *Tracker) Filter(p []byte) []byte {
	t.mu.Lock()
	rev := t.buf.Rev()
	keys := t.dec.Decode(p)
	if t.OnKeys != nil {
		t.OnKeys(p, keys)
	}
	for _, k := range keys {
		t.buf.Apply(k)
	}
	dirty := t.buf.Rev() != rev
	subs := t.subs
	t.mu.Unlock()

	if dirty {
		notify(subs)
	}
	return p
}

// Resync replaces the tracked prompt with text and cursor, as read from the
// child's screen, if the state is still at revision rev: input that arrived
// since rev is newer than any screen read made for it. It reports whether
// the state changed.
func (t *Tracker) Resync(rev uint64, text string, cursor, slack int) bool {
	t.mu.Lock()
	if t.buf.Rev() != rev {
		t.mu.Unlock()
		return false
	}
	t.buf.Set(text, cursor)
	t.buf.setSlack(slack)
	changed := t.buf.Rev() != rev
	subs := t.subs
	t.mu.Unlock()

	if changed {
		notify(subs)
	}
	return changed
}

func notify(subs []chan struct{}) {
	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default: // a notification is already pending
		}
	}
}

// Snapshot returns the current prompt state.
func (t *Tracker) Snapshot() State {
	t.mu.Lock()
	defer t.mu.Unlock()
	return State{Text: t.buf.Text(), Cursor: t.buf.Cursor(), Synced: t.buf.Synced(), Slack: t.buf.Slack(), Rev: t.buf.Rev(),
		Pastes: slices.Clone(t.buf.Pastes())}
}

// Changed receives after the state changes. Notifications coalesce: a slow
// reader sees one signal for many changes and should call Snapshot.
func (t *Tracker) Changed() <-chan struct{} { return t.subs[0] }

// Subscribe returns a new channel with the same semantics as Changed, for an
// additional independent reader.
func (t *Tracker) Subscribe() <-chan struct{} {
	ch := make(chan struct{}, 1)
	t.mu.Lock()
	t.subs = append(t.subs[:len(t.subs):len(t.subs)], ch)
	t.mu.Unlock()
	return ch
}
