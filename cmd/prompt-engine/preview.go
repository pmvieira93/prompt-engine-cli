package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pmvieira93/prompt-engine-cli/internal/config"
	"github.com/pmvieira93/prompt-engine-cli/internal/diff"
	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
	"github.com/pmvieira93/prompt-engine-cli/internal/ghost"
	"github.com/pmvieira93/prompt-engine-cli/internal/input"
	"github.com/pmvieira93/prompt-engine-cli/internal/rules"
	"github.com/pmvieira93/prompt-engine-cli/internal/tui"
)

// previews is preview mode (expand.on_enter: preview, PRD Appendix 1,
// FR-7.6): when typing pauses, a rewrite is prepared in the background and
// the panel shows it; the expand shortcut then applies it at once, and
// Enter sends the prompt as typed. It passes the same guard rails as auto
// mode, so what's offered is what auto mode would send.
type previews struct {
	ctrl    *ghost.Controller
	tracker *input.Tracker
	sess    *sessionInfo
	live    *atomic.Pointer[config.Config]
	logf    func(string, ...any)
	// What it shares with the other rewrites, from runGhost.
	style    func(config.Config) string        // the style for this prompt
	chosen   func(plain string) string         // how the panel names it
	expanded func(text string) bool            // text is a rewrite waiting to be sent
	context  func() expand.SessionContext      // what the model is told
	rules    func(config.Config) *expand.Rules // the guard rails

	// mu guards the fields below. It may be taken with the expansion's
	// held, never the other way round.
	mu       sync.Mutex
	text     string // the prompt the preview is for
	out      string // its rewrite, or "" if there is none
	reason   string // why there is none
	outStyle string // out's style (expand.Expander.StyleName)
	asked    string // expand.style when it was prepared
	guard    string // the guard rail that stopped the rewrite, if one did
	detail   string // what tripped it (expand.Stop.Detail)
	rejected string // the rewrite it stopped
	pending  bool   // being prepared
	cancel   context.CancelFunc
}

// ready returns the rewrite prepared for the prompt s and its style, or ""
// if there is none.
func (p *previews) ready(s input.State) (string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// A style changed since, by a reload or keys.style, makes it stale.
	if !s.Synced || p.pending || p.text != s.Text || p.asked != p.style(*p.live.Load()) {
		return "", ""
	}
	return p.out, p.outStyle
}

// prepare rewrites the prompt as it stands, unless that's done or under
// way, and keeps the result for the panel and ready.
func (p *previews) prepare(ctx context.Context) {
	c := *p.live.Load()
	c.Expand.Style = p.style(c)
	cfg := &c
	s := p.tracker.Snapshot()
	expanded := p.expanded(s.Text)
	p.mu.Lock()
	// Ready, or being prepared; not one cancelled before it was ready,
	// or the same text typed again would never get one.
	prepared := p.pending || p.out != "" || p.reason != ""
	if cfg.Expand.OnEnter != "preview" || p.text == s.Text && p.asked == cfg.Expand.Style && prepared {
		p.mu.Unlock()
		return
	}
	show := s.Synced && !expanded && rules.SkipReason(s) == "" && len(strings.Fields(s.Text)) >= cfg.Expand.MinWords
	plan := expand.PlanRewrite(s.Text, s.Pastes)
	if p.cancel != nil {
		p.cancel() // a rewrite in another style
		p.cancel = nil
	}
	p.text, p.out, p.reason, p.pending = s.Text, "", "", false
	p.outStyle, p.asked = "", cfg.Expand.Style
	p.guard, p.detail, p.rejected = "", "", ""
	switch {
	case !show:
		p.text = "" // nothing to offer: the panel shows the last prompt sent
		p.mu.Unlock()
		p.ctrl.Redraw()
		return
	case plan.Skip != "":
		p.reason = plan.Skip
		p.mu.Unlock()
		p.logf("expand: no preview: %s", plan.Skip)
		p.ctrl.Redraw()
		return
	}
	pctx, cancel := context.WithCancel(ctx)
	p.pending, p.cancel = true, cancel
	p.mu.Unlock()
	p.ctrl.Redraw()

	// The model gives a different rewrite on another try: in testing,
	// one try in three turned "it still fails, same …" into a task,
	// and the next was fine. So a stopped rewrite gets a second try.
	start := time.Now()
	sc := p.context()
	var out, reason, style, guard, detail, rejected string
	for try := 1; try <= 2; try++ {
		tctx, tcancel := context.WithTimeout(pctx, cfg.LLM.ExpandTimeout+wordsTime(plan.Words))
		release := p.sess.hold()
		rules := p.rules(*cfg) // the same for the model and the check
		masked, st, err := expandPrompt(tctx, *cfg, rules, plan, sc)
		release()
		tcancel()
		if pctx.Err() != nil {
			cancel()
			return // typing resumed: a newer preview is on its way
		}
		style = st
		if err != nil {
			reason, guard, detail, rejected = "the rewrite failed: "+err.Error(), "", "", ""
			break
		}
		var stop *expand.Stop
		if out, stop = review(rules, plan, masked, sc); stop == nil {
			reason, guard, detail, rejected = "", "", "", ""
			break
		}
		reason, guard, detail, rejected = stop.Reason, stop.Guard, stop.Detail, expand.StripContext(masked)
		if try == 1 {
			p.logf("expand: preview rewrite %q stopped by guard %s (%s); asking again", rejected, guard, reason)
		}
	}
	cancel()
	p.mu.Lock()
	if p.text != s.Text || !p.pending || p.asked != cfg.Expand.Style {
		p.mu.Unlock()
		return
	}
	p.out, p.reason, p.pending, p.cancel = out, reason, false, nil
	p.outStyle, p.guard, p.detail, p.rejected = style, guard, detail, rejected
	p.mu.Unlock()
	took := time.Since(start).Round(time.Millisecond)
	if out == "" {
		rejectedBy := ""
		if guard != "" {
			rejectedBy = fmt.Sprintf("; guard %s rejected %q", guard, rejected)
		}
		p.logf("expand: no preview for %q: %s (%s)%s", plan.Masked, reason, took, rejectedBy)
	} else {
		p.logf("expand: preview ready for %q: %q (%s, style %s)", s.Text, out, took, style)
		// Not over a notice already shown, e.g. a hint: seen hiding the
		// new-task hint 0.3s after it showed.
		if !p.ctrl.PanelOpen() && p.ctrl.Notice() == "" {
			k := p.ctrl.CurrentKeys()
			flashNotice(p.ctrl, fmt.Sprintf("✎ rewrite ready · %s then %s uses it, %s then %s shows it", k.Leader, k.Expand, k.Leader, k.Panel))
		}
	}
	p.ctrl.Redraw()
}

// watchTyping cancels a preview once the prompt changes, and prepares
// another when typing pauses.
func (p *previews) watchTyping(ctx context.Context) {
	var timer *time.Timer
	for range p.tracker.Subscribe() {
		p.mu.Lock()
		if p.cancel != nil && p.tracker.Snapshot().Text != p.text {
			p.cancel() // the prompt changed: that rewrite is stale
			p.cancel, p.pending = nil, false
			p.logf("expand: preview for %q cancelled: the prompt changed", p.text)
		}
		p.mu.Unlock()
		if p.live.Load().Expand.OnEnter != "preview" {
			continue
		}
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(p.live.Load().Expand.PreviewPause, func() { p.prepare(ctx) })
	}
}

// diff is what the panel shows of the preview for the prompt as it
// stands, or nil if there's none.
func (p *previews) diff() *tui.Diff {
	s := p.tracker.Snapshot()
	p.mu.Lock()
	text, out, reason, pending, asked := p.text, p.out, p.reason, p.pending, p.asked
	guard, detail, rejected := p.guard, p.detail, p.rejected
	p.mu.Unlock()
	if ec := p.live.Load().Expand; ec.OnEnter != "preview" || p.style(*p.live.Load()) != asked || !s.Synced || text == "" || text != s.Text {
		return nil
	}
	k := p.ctrl.CurrentKeys()
	switch {
	case pending:
		return &tui.Diff{Title: "Preparing a rewrite…", Ops: diff.Prompt(text, text)}
	case out == "":
		notes := []string{"Not offered: " + reason + "."}
		if guard != "" && p.live.Load().Panel.GuardDetails {
			notes = guardNotes("Guard ", guard, detail, rejected)
		}
		return &tui.Diff{Title: "No rewrite suggested", Ops: diff.Prompt(text, text),
			Notes: append(notes, "Enter sends what you typed.")}
	}
	return &tui.Diff{
		Title: "Suggested rewrite (" + p.chosen("not used yet") + ")",
		Ops:   diff.Prompt(text, out),
		Notes: []string{fmt.Sprintf("%s then %s uses it; Enter sends what you typed.", k.Leader, k.Expand)},
	}
}
