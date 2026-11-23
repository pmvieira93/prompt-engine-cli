package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pmvieira93/prompt-engine-cli/internal/config"
	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
	"github.com/pmvieira93/prompt-engine-cli/internal/ghost"
	"github.com/pmvieira93/prompt-engine-cli/internal/history"
	"github.com/pmvieira93/prompt-engine-cli/internal/input"
	"github.com/pmvieira93/prompt-engine-cli/internal/rules"
)

// Hints on how Claude Code is used (PRD Appendix 5, FR-11.4, section 11).

// handoffStep is how much the context must grow after a handoff hint
// before it's shown again.
const handoffStep = 40_000

// handoffDue reports whether a handoff hint is due at a context of
// context tokens, given hints.handoff_at and the context when the hint was
// last shown (0 for not yet), and what to remember as last.
func handoffDue(context, at, last int64) (due bool, next int64) {
	switch {
	case at <= 0 || context < at:
		return false, 0 // below it, as after /clear: due again once it's reached
	case last == 0 || context >= last+handoffStep:
		return true, context
	}
	return false, last
}

// defaultHandoff is the handoff prompt when templates/handoff.txt is
// missing or empty; {transcript} becomes Claude Code's transcript file, in
// either. The structure is the developer's (PR #42).
const defaultHandoff = `Write HANDOFF.md so a fresh session can continue this work, with this structure:

# Handoff: [short and descriptive title]

**Date:** [current date]
**Status:** [in progress / blocked / awaiting decision]

## 1. Objective
What is being done and why, in 2–4 sentences.

## 2. Essential Context
Tech stack, constraints, decisions already made and their rationale, and the worktree if one was used to work in parallel.

## 3. What Has Been Done
Chronological list of what has been produced, attempted, or discarded. If something was discarded, explain why.

## 4. Current State
Where exactly we left off. What works, what is broken.

## 5. Next Steps
Ordered, actionable list of what to do next. Be specific.

## 6. Open Questions
Things that still require a decision or investigation.

## 7. Relevant Artifacts
Files, snippets, links, or commands that the next session will need.

## 8. Instructions for the Next Session
Expected tone, level of detail, pitfalls to avoid.

---
*Original session: {transcript}. Indicative only: consult it only if the user asks.*`

// afterHandoff is offered as ghost text after /clear, once a handoff was
// sent. shortcut: it names HANDOFF.md whatever templates/handoff.txt asks
// for; make it a template too if someone renames the file.
const afterHandoff = "Read HANDOFF.md and continue from it."

// handoffPrompt is templates/handoff.txt in dir, or defaultHandoff, with
// {transcript} replaced by transcript ("unknown" when none was found).
func handoffPrompt(dir, transcript string) string {
	text := defaultHandoff
	if data, err := os.ReadFile(filepath.Join(dir, "handoff.txt")); err == nil && strings.TrimSpace(string(data)) != "" {
		text = strings.TrimSpace(string(data))
	}
	if transcript == "" {
		transcript = "unknown"
	}
	return strings.ReplaceAll(text, "{transcript}", transcript)
}

// newTaskPause is how long typing pauses before a prompt is checked for a
// new task.
const newTaskPause = time.Second

// embedModel gives the vectors the new-task classifier was trained on.
// install.sh downloads and checks it as EMBED_MODEL: change both.
const embedModel = "nomic-embed-text"

// backRef is a word that refers to the conversation so far: the routing
// spike's BACKREF, which ignores case.
var backRef = regexp.MustCompile(`(?i)\b(it|that|this|those|these|them|also|again|same|above|previous|continue|go ahead|yes|ok|` +
	`okay|sure|merge|commit|push|reply|fix (all|them|it))\b`)

// newTaskWeights are a logistic regression on [cosine with the previous
// prompt, first prompt, backRef, log(1+words)/5] scaled to unit length,
// then the bias; trained on the developer's 60 hand labels (routing spike,
// docs/spikes/routing.md: 90%, never "new" for a continuing prompt).
var newTaskWeights = [5]float64{-6.0313, 3.6789, -0.2616, 5.0222, 0.5849}

// newTaskThreshold is the score above which a prompt starts a new task.
// Cross-validated on the hand labels: at 0, 9 of 14 new tasks and no
// follow-up; at -0.5, 10 of 14 and still none; at -0.75, 11 but 2 of 46
// follow-ups too (2026-11-23).
const newTaskThreshold = -0.5

// newTaskScore is how much a prompt looks like the start of a new task,
// given the cosine between its vector and the previous prompt's: above
// newTaskThreshold, it is one.
func newTaskScore(text string, cosine float64) float64 {
	x := []float64{cosine, 0, 0, math.Log(1+float64(len(strings.Fields(text)))) / 5}
	if backRef.MatchString(text) {
		x[2] = 1
	}
	var norm float64
	for _, v := range x {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		norm = 1
	}
	score := newTaskWeights[4]
	for i, v := range x {
		score += newTaskWeights[i] * v / norm
	}
	return score
}

func cosine(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range min(len(a), len(b)) {
		dot, na, nb = dot+a[i]*b[i], na+a[i]*a[i], nb+b[i]*b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

// embed returns text's vector from Ollama at endpoint, as the classifier
// was trained: with nomic-embed-text's task prefix, the first 2,000
// characters.
func embed(ctx context.Context, endpoint, text string) ([]float64, error) {
	if r := []rune(text); len(r) > 2000 {
		text = string(r[:2000])
	}
	body, _ := json.Marshal(map[string]string{"model": embedModel, "input": "classification: " + text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Embeddings [][]float64 `json:"embeddings"`
		Error      string      `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%s: %w", resp.Status, err)
	}
	switch {
	case out.Error != "":
		return nil, errors.New(out.Error)
	case len(out.Embeddings) == 0:
		return nil, fmt.Errorf("%s: no embedding", resp.Status)
	}
	return out.Embeddings[0], nil
}

// fakeEmbed stands in for embed in tests: a vector of word counts, so
// prompts sharing no word are unrelated.
func fakeEmbed(_ context.Context, _, text string) ([]float64, error) {
	v := make([]float64, 64)
	for _, w := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		h.Write([]byte(w))
		v[h.Sum32()%64]++
	}
	return v, nil
}

// retry calls try up to n times, 250ms apart, until it returns true, and
// reports whether it did.
func retry(n int, try func() bool) bool {
	for range n {
		if try() {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

// hints shows the context hints and keeps what they need: the hint shown
// since the last prompt, whether it was taken, and the prompt to put back
// after /clear.
type hints struct {
	ctrl    *ghost.Controller
	tracker *input.Tracker
	sess    *sessionInfo
	live    *atomic.Pointer[config.Config]
	logf    func(string, ...any)

	// mu guards the fields below. Never call ctrl while holding it: Enter
	// calls sent, which takes it, with ctrl locked, so the other order
	// deadlocks.
	mu        sync.Mutex
	shown     string // the hint shown since the last prompt: "handoff" or "new_task"
	followed  bool   // its shortcut was used, and not undone
	last      int64  // the context the handoff hint was last shown at
	handedOff bool   // a handoff prompt was sent: offer to read it after /clear
	restore   string // the typed prompt to put back after /clear
	stashed   bool   // restore is from keys.clear: kept until it's back
	putting   bool   // putBack is running
	checked   string // the prompt last checked for a new task
	typed     string // the last prompt sent, as typed: the classifier compares with it
	prev      []float64
	prevText  string // the prompt prev is the vector of
	embedErr  string // the last embedding error, logged once
}

// usage is told the context size when it changes: it shows the handoff
// hint once it's due.
func (h *hints) usage(context int64) {
	h.mu.Lock()
	due, last := handoffDue(context, int64(h.live.Load().Hints.HandoffAt), h.last)
	h.last = last
	if due {
		h.shown = "handoff"
	}
	h.mu.Unlock()
	if !due {
		return
	}
	keys := h.ctrl.CurrentKeys()
	msg := fmt.Sprintf("context %s: write a handoff and start fresh? %s then %s", tokens(context), keys.Leader, keys.Handoff)
	h.logf("hint: handoff, context %s", tokens(context))
	// The notice is drawn at the cursor, so only once it's in the
	// prompt: the context grows during a reply, maybe under a dialog.
	// The panel line shows meanwhile.
	go func() {
		for range 1200 { // 10 minutes
			h.mu.Lock()
			pending := h.shown == "handoff" && !h.followed
			h.mu.Unlock()
			if !pending {
				return
			}
			if h.ctrl.Gate == nil || h.ctrl.Gate() {
				h.ctrl.SetBriefNotice(msg)
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

// follow records whether the hint's shortcut was taken: a handoff, or its
// undo.
func (h *hints) follow(taken bool) {
	h.mu.Lock()
	h.followed = taken
	h.mu.Unlock()
}

// putBack types the prompt kept for after /clear into the empty prompt,
// once Claude has redrawn it, or once what was typed meanwhile is sent.
func (h *hints) putBack() {
	h.mu.Lock()
	text := h.restore
	if text == "" || h.putting {
		h.mu.Unlock()
		return
	}
	h.putting = true
	h.mu.Unlock()
	// shortcut: given up after 2 minutes of a non-empty prompt; keep it
	// on disk if that's ever seen to lose one.
	ok := retry(480, func() bool {
		h.mu.Lock()
		dropped := h.restore != text
		h.mu.Unlock()
		if dropped {
			return true // dropped, by a prompt sent after a handoff
		}
		// A prompt sent meanwhile changes the revision, so Replace refuses.
		s := h.tracker.Snapshot()
		if !s.Synced || s.Text != "" || (h.ctrl.Gate != nil && !h.ctrl.Gate()) || h.ctrl.Replace(s.Rev, "", text) != nil {
			return false
		}
		h.mu.Lock()
		if h.restore == text {
			h.restore, h.stashed = "", false
		}
		h.mu.Unlock()
		h.logf("hint: put back %q after /clear", text)
		return true
	})
	h.mu.Lock()
	h.putting = false
	if !ok {
		h.restore, h.stashed = "", false
	}
	h.mu.Unlock()
	if !ok {
		h.logf("hint: couldn't put back %q after /clear", text)
		flashNotice(h.ctrl, "couldn't put your prompt back after /clear")
	}
}

// cleared is told when /clear is seen: it puts the kept prompt back, or,
// after a handoff, offers to read it.
func (h *hints) cleared() {
	h.mu.Lock()
	offer := h.handedOff && h.restore == ""
	h.handedOff = false
	h.typed, h.checked = "", "" // a new conversation: no prompt before
	if !h.followed {
		// /clear took the hint's "start fresh", without a handoff: no
		// label is better than "dismissed".
		h.shown = ""
	}
	h.mu.Unlock()
	go h.putBack()
	if !offer {
		return
	}
	go retry(20, func() bool {
		ok := h.ctrl.Show(afterHandoff, h.tracker.Snapshot().Rev) // once Claude has redrawn its empty prompt
		if ok {
			h.logf("hint: offered %q after /clear", afterHandoff)
		}
		return ok
	})
}

// clear answers keys.clear: /clear, then the typed prompt back, unsent,
// for a new task in a fresh context.
func (h *hints) clear(s input.State) {
	path, _ := h.sess.transcriptPath()
	t, ok := h.sess.turn()
	switch {
	case !s.Synced:
		flashNotice(h.ctrl, "clear: the prompt isn't readable here")
	case expand.HasPlaceholder(s.Text):
		// Typed back, a long paste would only be its "[Pasted text]"
		// label; a short one is plain text, and comes back.
		flashNotice(h.ctrl, "clear: a [Pasted text] can't be put back: type /clear yourself")
	case path == "":
		// /clear is seen in the transcript: without it, the prompt
		// would never be put back.
		flashNotice(h.ctrl, "clear: Claude's transcript isn't found: type /clear yourself")
	case ok && !t.Done:
		// shortcut: a prompt sent a moment ago isn't in the transcript
		// yet, so this can miss a reply that just started.
		flashNotice(h.ctrl, "clear: Claude is still replying")
	default:
		if err := h.ctrl.Replace(s.Rev, s.Text, "/clear"); err != nil {
			h.logf("clear: not applied: %v", err)
			flashNotice(h.ctrl, "clear: "+err.Error())
			return
		}
		h.mu.Lock()
		if typed := strings.TrimSpace(s.Text); typed != "" || h.restore == "" {
			h.restore = typed
		} // else the new task kept by a handoff comes back
		h.stashed = h.restore != ""
		h.followed = h.shown == "new_task" // after the handoff hint, /clear alone isn't a handoff
		restore := h.restore
		h.mu.Unlock()
		h.logf("clear: sending /clear, then putting back %q", restore)
		if err := h.ctrl.Submit(h.tracker.Snapshot().Rev, "/clear"); err != nil {
			// Enter on /clear still puts the prompt back.
			h.logf("clear: /clear not sent: %v", err)
			flashNotice(h.ctrl, "clear: /clear not sent: "+err.Error())
			return
		}
		// In case /clear isn't seen in the transcript: the prompt
		// mustn't be lost. A no-op once it's back.
		time.AfterFunc(8*time.Second, h.putBack)
	}
}

// checkNewTask shows the new-task hint when the prompt starts a new task
// and the context is large: /clear or a handoff first.
func (h *hints) checkNewTask(ctx context.Context) {
	cfg := h.live.Load()
	at := int64(cfg.Hints.NewTaskAt)
	var size int64
	if t := h.sess.tally.Load(); t != nil {
		size = t.Context
	}
	s := h.tracker.Snapshot()
	h.mu.Lock()
	prev := h.typed
	skip := h.shown != "" || h.checked == s.Text
	prevVec := h.prev
	if h.prevText != prev {
		prevVec = nil
	}
	h.mu.Unlock()
	embedder := embed
	if os.Getenv(envSuggester) == "fake" {
		embedder = fakeEmbed
	} else if cfg.LLM.Provider != "ollama" {
		return // the embedding model runs through Ollama, like the rewrites
	}
	if skip || at <= 0 || size < at || rules.SkipReason(s) != "" || prev == "" {
		return
	}
	h.mu.Lock()
	h.checked = s.Text
	h.mu.Unlock()
	failed := func(err error) {
		h.mu.Lock()
		if h.checked == s.Text {
			h.checked = "" // checked again at the next pause
		}
		again := h.embedErr == err.Error()
		h.embedErr = err.Error()
		h.mu.Unlock()
		if !again {
			h.logf("hint: new task not checked: %v (it needs `ollama pull %s`; logged once until it works)", err, embedModel)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second) // the model may need loading
	defer cancel()
	if prevVec == nil {
		v, err := embedder(ctx, cfg.LLM.Endpoint, prev)
		if err != nil {
			failed(err)
			return
		}
		prevVec = v
		h.mu.Lock()
		h.prev, h.prevText = v, prev
		h.mu.Unlock()
	}
	v, err := embedder(ctx, cfg.LLM.Endpoint, s.Text)
	if err != nil {
		failed(err)
		return
	}
	h.mu.Lock()
	h.embedErr = ""
	h.mu.Unlock()
	sim := cosine(v, prevVec)
	score := newTaskScore(s.Text, sim)
	h.logf("hint: new task? %s (score %.2f, similarity %.2f)", map[bool]string{true: "yes", false: "no"}[score > newTaskThreshold], score, sim)
	if score <= newTaskThreshold {
		return
	}
	k := h.ctrl.CurrentKeys()
	msg := fmt.Sprintf("context %s, a new task? %s then %s: /clear first · %s then %s: a handoff first",
		tokens(size), k.Leader, k.Clear, k.Leader, k.Handoff)
	// Shown, and so recorded, only once the notice can be drawn: the
	// history's labels mustn't count a hint nobody saw.
	retry(40, func() bool {
		drawable := h.ctrl.Gate == nil || h.ctrl.Gate()
		h.mu.Lock()
		stale := h.shown != "" || h.tracker.Snapshot().Text != s.Text
		show := !stale && drawable
		if show {
			h.shown = "new_task"
		}
		h.mu.Unlock()
		if show {
			h.logf("hint: new task, context %s: %q", tokens(size), s.Text)
			h.ctrl.SetBriefNotice(msg)
		}
		return stale || show
	})
	h.ctrl.Redraw()
}

// watchTyping checks the prompt for a new task each time typing pauses.
func (h *hints) watchTyping(ctx context.Context) {
	var timer *time.Timer
	for range h.tracker.Subscribe() {
		if s := h.tracker.Snapshot(); s.Synced && strings.TrimSpace(s.Text) == "" {
			// An emptied prompt starts another: its new task gets the
			// hint again. Seen in hand tests: Ctrl+C, then the next task.
			h.mu.Lock()
			if h.shown == "new_task" && !h.followed {
				h.shown, h.checked = "", ""
			}
			h.mu.Unlock()
		}
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(newTaskPause, func() { h.checkNewTask(ctx) })
	}
}

// sent records the hint shown before sub, and whether it was taken, and
// keeps what the next /clear needs. handoff is whether sub is the handoff
// prompt, and typed what it replaced.
func (h *hints) sent(sub *history.Submission, handoff bool, typed string) {
	pasted := expand.HasPlaceholder(typed)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.shown != "" || h.followed {
		sub.Hint, sub.HintFollowed = cmp.Or(h.shown, "handoff"), h.followed
	}
	if !h.stashed {
		h.restore = "" // a prompt sent after a handoff: it isn't followed by /clear
	}
	if handoff {
		h.handedOff = true // until /clear, even with prompts after it
		if h.shown == "new_task" && !pasted && !h.stashed {
			// The new task, after /clear, rather than reading the
			// handoff; not with a long paste's label, which would come back as is.
			h.restore = typed
		}
	} else {
		h.typed = sub.Raw
	}
	h.shown, h.followed = "", false
}

// status is the panel's line about the hint shown, if one is.
func (h *hints) status() []string {
	keys := h.ctrl.CurrentKeys()
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.followed:
	case h.shown == "handoff":
		return []string{fmt.Sprintf("hint:        a handoff (%s then %s)", keys.Leader, keys.Handoff)}
	case h.shown == "new_task":
		return []string{fmt.Sprintf("hint:        a new task: /clear (%s then %s) or a handoff (%s then %s)",
			keys.Leader, keys.Clear, keys.Leader, keys.Handoff)}
	}
	return nil
}
