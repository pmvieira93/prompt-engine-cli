package ghost

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pmvieira93/prompt-engine-cli/internal/input"
	"github.com/pmvieira93/prompt-engine-cli/internal/suggest"
)

// Worker requests a suggestion once typing pauses for Debounce, cancelling
// any request made for an older prompt state.
type Worker struct {
	Tracker *input.Tracker
	Ctrl    *Controller
	// Suggester, Debounce, and Timeout are the initial settings; Update
	// replaces them while Run runs. A nil Suggester means no suggestions;
	// the worker still resyncs the input buffer from the screen.
	Suggester suggest.Suggester
	Debounce  time.Duration
	Timeout   time.Duration
	Logf      func(format string, a ...any)

	mu sync.Mutex
}

// Update replaces the worker's settings, e.g. after the config is edited.
// They apply from the next pause in typing.
func (w *Worker) Update(s suggest.Suggester, debounce, timeout time.Duration) {
	w.mu.Lock()
	w.Suggester, w.Debounce, w.Timeout = s, debounce, timeout
	w.mu.Unlock()
}

func (w *Worker) settings() (suggest.Suggester, time.Duration, time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Suggester, w.Debounce, w.Timeout
}

// Run blocks until ctx is done.
func (w *Worker) Run(ctx context.Context) {
	changed := w.Tracker.Subscribe()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	cancel := context.CancelFunc(func() {})
	defer func() { cancel() }()

	for {
		select {
		case <-ctx.Done():
			return
		case <-changed:
			cancel() // the prompt moved on; any in-flight answer is stale
			_, debounce, _ := w.settings()
			timer.Reset(debounce)
		case <-timer.C:
			s := w.Tracker.Snapshot()
			if !s.Synced && !w.Ctrl.Disabled() {
				// Replayed keys lost track of the prompt (history, a menu,
				// autocomplete). Take it from the screen instead; the
				// resync notifies subscribers, which re-arms this timer.
				if text, cur, ok := w.Ctrl.ScreenPrompt(); ok && w.Tracker.Resync(s.Rev, text, cur) {
					w.logf("buffer resynced from screen rev=%d %q", s.Rev, text)
					continue
				}
			}
			if input.CompletionMenuMayBeOpen(s.Text) {
				continue // Tab belongs to claude's menu
			}
			suggester, _, timeout := w.settings()
			if suggester == nil || w.Ctrl.Disabled() || !s.Synced || s.Cursor != utf8.RuneCountInString(s.Text) || strings.TrimSpace(s.Text) == "" {
				continue
			}
			rctx, rcancel := context.WithTimeout(ctx, timeout)
			cancel = rcancel
			w.logf("suggestion requested rev=%d %q", s.Rev, s.Text)
			go w.request(rctx, suggester, s)
		}
	}
}

// request runs one suggestion call. Failures are logged and dropped so the
// typing path is never affected (FR-1.3).
func (w *Worker) request(ctx context.Context, suggester suggest.Suggester, s input.State) {
	defer func() {
		if r := recover(); r != nil {
			w.logf("suggestion panic: %v", r)
		}
	}()
	start := time.Now()
	sug, err := suggester.Suggest(ctx, s.Text, s.Cursor)
	switch {
	case err != nil:
		w.logf("suggestion error rev=%d: %v", s.Rev, err)
	case sug == "":
	case w.Ctrl.Show(sug, s.Rev):
		w.logf("suggestion shown rev=%d %q (%s)", s.Rev, sug, time.Since(start).Round(time.Microsecond))
	default:
		w.logf("suggestion discarded rev=%d %q (prompt changed or cursor not in prompt)", s.Rev, sug)
	}
}

func (w *Worker) logf(format string, a ...any) {
	if w.Logf != nil {
		w.Logf(format, a...)
	}
}
