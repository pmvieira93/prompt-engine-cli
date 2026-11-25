package main

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pmvieira93/prompt-engine-cli/internal/config"
	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
	"github.com/pmvieira93/prompt-engine-cli/internal/ghost"
	"github.com/pmvieira93/prompt-engine-cli/internal/history"
	"github.com/pmvieira93/prompt-engine-cli/internal/input"
)

// expansions is prompt expansion: leader, then keys.expand, rewrites the
// prompt in place for the user to review; keys.undo puts the original
// back; keys.style picks the style for this prompt (PRD Appendix 2,
// FR-8.6); keys.handoff puts the handoff prompt in place, like a rewrite.
type expansions struct {
	ctrl    *ghost.Controller
	tracker *input.Tracker
	sess    *sessionInfo
	live    *atomic.Pointer[config.Config]
	logf    func(string, ...any)
	// What it shares with the other rewrites, from runGhost.
	context func() expand.SessionContext      // what the model is told
	rules   func(config.Config) *expand.Rules // the guard rails
	preview *previews
	hint    *hints

	// mu guards the rewrite in place. The preview's lock and styleMu may be
	// taken with it held, never the other way round. Enter calls sent,
	// which takes it, with ctrl locked: never call ctrl while holding it.
	mu                 sync.Mutex
	running            bool
	original, expanded string
	pastes             []string // original's
	style              string   // of expanded, for the history

	// The style for the prompt being written: keys.style picks it, and it
	// lasts until the prompt is sent or cleared. "" is expand.style.
	styleMu     sync.Mutex
	promptStyle string
}

// styleFor is the style a rewrite with cfg uses for this prompt.
func (x *expansions) styleFor(cfg config.Config) string {
	x.styleMu.Lock()
	defer x.styleMu.Unlock()
	if x.promptStyle != "" {
		return x.promptStyle
	}
	return cfg.Expand.Style
}

func (x *expansions) withStyle(cfg config.Config) config.Config {
	cfg.Expand.Style = x.styleFor(cfg)
	return cfg
}

// chosen is how the panel and notices name the style for this prompt
// when keys.style picked one; plain otherwise.
func (x *expansions) chosen(plain string) string {
	x.styleMu.Lock()
	defer x.styleMu.Unlock()
	if x.promptStyle == "" {
		return plain
	}
	return x.promptStyle + ", for this prompt"
}

// watchTyping drops the style for this prompt once it's sent or cleared.
func (x *expansions) watchTyping() {
	for range x.tracker.Subscribe() {
		if x.tracker.Snapshot().Text != "" {
			continue
		}
		x.styleMu.Lock()
		was := x.promptStyle
		x.promptStyle = ""
		x.styleMu.Unlock()
		if was != "" {
			x.logf("style: back to %s (the prompt was sent or cleared)", x.live.Load().Expand.Style)
		}
	}
}

// isExpanded reports whether text is a rewrite waiting to be sent.
func (x *expansions) isExpanded(text string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.expanded != "" && text == x.expanded
}

// set keeps out, in style, as the rewrite in place of s, for undo, the
// panel and the history.
func (x *expansions) set(s input.State, out, style string) {
	x.mu.Lock()
	x.original, x.pastes, x.expanded, x.style = s.Text, s.Pastes, out, style
	x.mu.Unlock()
}

// rewrite rewrites src (its text and pastes) in the style for this prompt
// and puts the result in place of s, the prompt as it stands: src itself,
// or an earlier rewrite of it. The caller has checked the plan and set
// running. If the style was changed meanwhile, it rewrites src again in
// that style, so only the last one chosen stays.
func (x *expansions) rewrite(ctx context.Context, s, src input.State, plan expand.Plan) {
	var after string // a notice to show briefly once done
	var used string  // the style the rewrite in place has, if it's done
	defer func() {
		x.mu.Lock()
		now := x.tracker.Snapshot()
		again := used != "" && slices.Contains(expand.Styles, used) && used != x.styleFor(*x.live.Load()) &&
			now.Synced && now.Text == x.expanded
		if !again {
			x.running = false
		}
		x.mu.Unlock()
		x.ctrl.SetNotice("")
		if after != "" {
			flashNotice(x.ctrl, after)
		}
		if again {
			x.logf("expand: the style changed to %s while rewriting; rewriting again", x.styleFor(*x.live.Load()))
			go x.rewrite(ctx, now, src, plan)
		}
	}()
	cfg := x.withStyle(*x.live.Load())
	if c := x.chosen(""); c != "" {
		x.ctrl.SetNotice("⟳ expanding… (" + c + ")")
	} else {
		x.ctrl.SetNotice("⟳ expanding…")
	}
	ectx, cancel := context.WithTimeout(ctx, cfg.LLM.ExpandTimeout+wordsTime(plan.Words))
	defer cancel()
	start := time.Now()
	release := x.sess.hold()
	rules := x.rules(cfg) // the same for the model and the check
	masked, style, err := expandPrompt(ectx, cfg, rules, plan, x.context())
	release()
	if err != nil {
		x.logf("expand: failed: %v", err)
		return
	}
	if stop := rules.CheckShortcut(plan.Masked, masked); stop != nil {
		x.logf("expand: not applied: the rewrite %q was stopped by guard %s (%s)", masked, stop.Guard, stop.Reason)
		after = "kept your text: " + stop.Reason
		return
	}
	restore := plan.Restore
	if rules.Off["pastes"] {
		restore = plan.RestoreMoved
	}
	out, err := restore(masked)
	if err != nil {
		x.logf("expand: not applied: %v: %q", err, masked)
		after = "kept your text: " + err.Error()
		return
	}
	if err := x.ctrl.Replace(s.Rev, s.Text, out); err != nil {
		x.logf("expand: not applied: %v", err)
		return
	}
	x.set(src, out, style)
	used = style
	if s.Text != src.Text {
		x.logf("expand: rewrote %q again: replaced %q with %q (%s, style %s)", src.Text, s.Text, out, time.Since(start).Round(time.Millisecond), style)
		return
	}
	x.logf("expand: replaced %q with %q (%s, style %s)", s.Text, out, time.Since(start).Round(time.Millisecond), style)
}

// expand answers keys.expand: the preview's rewrite if one is ready, or a
// rewrite of the prompt.
func (x *expansions) expand(ctx context.Context, s input.State) {
	if !s.Synced || strings.TrimSpace(s.Text) == "" {
		x.logf("expand: nothing to expand (empty prompt, or its text is unknown)")
		return
	}
	x.mu.Lock()
	if x.running {
		x.mu.Unlock()
		x.logf("expand: already expanding")
		return
	}
	if out, style := x.preview.ready(s); out != "" {
		x.mu.Unlock()
		if err := x.ctrl.Replace(s.Rev, s.Text, out); err != nil {
			x.logf("expand: preview not applied: %v", err)
			return
		}
		x.set(s, out, style)
		x.logf("expand: used the preview: replaced %q with %q", s.Text, out)
		return
	}
	plan := expand.PlanRewrite(s.Text, s.Pastes)
	if plan.Skip != "" {
		x.mu.Unlock()
		x.logf("expand: nothing to rewrite: %s", plan.Skip)
		flashNotice(x.ctrl, "nothing to rewrite: "+plan.Skip)
		return
	}
	x.running = true
	x.mu.Unlock()
	go x.rewrite(ctx, s, s, plan)
}

// nextStyle answers keys.style: the next style for this prompt. On a
// rewrite, it rewrites the original text again in that style.
func (x *expansions) nextStyle(ctx context.Context, s input.State) {
	if !s.Synced || strings.TrimSpace(s.Text) == "" {
		x.logf("style: nothing to choose a style for (empty prompt, or its text is unknown)")
		return
	}
	cfg := *x.live.Load()
	was := x.styleFor(cfg)
	next := expand.NextStyle(was)
	x.styleMu.Lock()
	x.promptStyle = next
	if next == cfg.Expand.Style {
		x.promptStyle = ""
	}
	x.styleMu.Unlock()
	x.logf("style: %s for this prompt (was %s)", next, was)

	x.mu.Lock()
	held := x.expanded != "" && s.Text == x.expanded
	src := input.State{Text: s.Text, Pastes: s.Pastes}
	if held {
		src.Text, src.Pastes = x.original, x.pastes
	}
	plan := expand.PlanRewrite(src.Text, src.Pastes)
	note := "style: " + next
	used := styleName(x.withStyle(cfg), plan.TypoOnly)
	switch used {
	case "typos":
		note += " (not used: typos only)"
	case "custom":
		note += " (not used: templates/system.txt)"
	}
	start := held && !x.running && plan.Skip == "" && used == next
	if start {
		x.running = true
	}
	x.mu.Unlock()
	if !x.ctrl.PanelOpen() {
		briefNotice(x.ctrl, note)
	}
	switch {
	case start:
		go x.rewrite(ctx, s, src, plan)
	case cfg.Expand.OnEnter == "preview" && !held:
		go x.preview.prepare(ctx) // the ready suggestion has the old style
	}
	x.ctrl.Redraw()
}

// undo answers keys.undo: the text the rewrite in place replaced.
func (x *expansions) undo(s input.State) {
	x.mu.Lock()
	original, expanded := x.original, x.expanded
	x.mu.Unlock()
	if expanded == "" || s.Text != expanded {
		x.logf("expand: nothing to undo (the prompt isn't the last expansion)")
		return
	}
	if err := x.ctrl.Replace(s.Rev, expanded, original); err != nil {
		x.logf("expand: undo not applied: %v", err)
		return
	}
	x.mu.Lock()
	handoff := x.style == "handoff"
	x.mu.Unlock()
	if handoff {
		x.hint.follow(false)
	}
	x.logf("expand: undone, back to %q", original)
}

// handoff answers keys.handoff: the handoff prompt in place of the typed
// one, which keys.undo brings back, like a rewrite's.
func (x *expansions) handoff(s input.State) {
	if !s.Synced {
		x.logf("handoff: the prompt's text is unknown")
		flashNotice(x.ctrl, "handoff: the prompt isn't readable here")
		return
	}
	x.mu.Lock()
	running := x.running
	x.mu.Unlock()
	if running {
		x.logf("handoff: a rewrite is running")
		return
	}
	dir, _ := x.live.Load().TemplatesDir()
	transcript, _ := x.sess.transcriptPath()
	text := handoffPrompt(dir, transcript)
	if err := x.ctrl.Replace(s.Rev, s.Text, text); err != nil {
		x.logf("handoff: not applied: %v", err)
		flashNotice(x.ctrl, "handoff: "+err.Error())
		return
	}
	x.set(s, text, "handoff")
	x.hint.follow(true)
	x.logf("handoff: replaced %q with the handoff prompt", s.Text)
}

// typed is what was typed for the prompt s, before a rewrite (not the
// handoff prompt), with its pastes: what the rules are chosen for.
func (x *expansions) typed(s input.State) (string, []string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.expanded != "" && s.Text == x.expanded && x.style != "handoff" {
		return x.original, x.pastes
	}
	return s.Text, s.Pastes
}

// sent records in sub whether the prompt s was a rewrite, and forgets the
// rewrite. It reports whether s was the handoff prompt, and what was typed
// before it.
func (x *expansions) sent(sub *history.Submission, s input.State) (handoff bool, typed string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.expanded != "" && s.Text == x.expanded && x.style != "handoff" {
		sub.Raw, sub.Expanded, sub.Style = x.original, true, x.style
	}
	handoff = x.expanded != "" && s.Text == x.expanded && x.style == "handoff"
	typed = strings.TrimSpace(x.original)
	x.original, x.pastes, x.expanded, x.style = "", nil, "", ""
	return handoff, typed
}

// waiting returns the rewrite in place and the text it replaced, or "".
func (x *expansions) waiting() (original, expanded string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.original, x.expanded
}
