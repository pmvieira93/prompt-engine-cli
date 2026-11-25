package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pmvieira93/prompt-engine-cli/internal/config"
	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
	"github.com/pmvieira93/prompt-engine-cli/internal/ghost"
	"github.com/pmvieira93/prompt-engine-cli/internal/guards"
	"github.com/pmvieira93/prompt-engine-cli/internal/history"
	"github.com/pmvieira93/prompt-engine-cli/internal/input"
	"github.com/pmvieira93/prompt-engine-cli/internal/rules"
)

// autoSkip says why auto mode sends s as typed without asking the model,
// or "" if it rewrites it. plan is s's plan.
func autoSkip(cfg config.Config, s input.State, plan expand.Plan) string {
	if reason := rules.SkipReason(s); reason != "" {
		return reason
	}
	if len(strings.Fields(s.Text)) < cfg.Expand.MinWords {
		return "short prompt"
	}
	return plan.Skip
}

// autoResult is a rewrite in auto mode: the text to send, or why the
// prompt goes as typed.
type autoResult struct {
	out    string // "" when not used
	masked string // the model's reply, pastes still masked
	style  string
	// reason says why the rewrite isn't used; guard, detail and rejected
	// are set when a guard rail stopped it.
	reason, guard, detail, rejected string
}

// autoRewrite asks the model for plan's rewrite and checks it with the
// guard rails, as auto mode does on Enter.
func autoRewrite(ctx context.Context, cfg config.Config, r *expand.Rules, plan expand.Plan, sc expand.SessionContext) autoResult {
	masked, style, err := expandPrompt(ctx, cfg, r, plan, sc)
	res := autoResult{masked: masked, style: style}
	if err != nil {
		res.reason = "expansion failed: " + err.Error()
		return res
	}
	out, stop := review(r, plan, masked, sc)
	if stop != nil {
		res.reason, res.guard, res.detail, res.rejected = stop.Reason, stop.Guard, stop.Detail, expand.StripContext(masked)
		return res
	}
	res.out = out
	return res
}

const rewriteFlag = "--pe-rewrite"

// rewriteArgs finds --pe-rewrite "<prompt>" before any "--". Nothing
// else may come with it: claude's arguments would be ignored.
func rewriteArgs(args []string) (prompt string, ok bool, err error) {
	for i, a := range args {
		switch a {
		case "--":
			return "", false, nil
		case rewriteFlag:
			if i != 0 || len(args) != 2 {
				return "", false, fmt.Errorf("usage: prompt-engine [--pe-config <file>] %s \"<prompt>\"", rewriteFlag)
			}
			return args[1], true, nil
		}
	}
	return "", false, nil
}

// rewriteResult is what --pe-rewrite prints: what auto mode would send
// for the prompt, for the cost benchmark (PRD Appendix 5, section 8).
type rewriteResult struct {
	Typed  string `json:"typed"`
	Prompt string `json:"prompt"` // the rewrite, or the typed text when it isn't used
	// Rewritten is false when the prompt goes as typed, and NotRewritten
	// says why: too short, a skip, a failed rewrite or a guard.
	Rewritten    bool   `json:"rewritten"`
	NotRewritten string `json:"not_rewritten,omitempty"`
	Guard        string `json:"guard,omitempty"`
	GuardDetail  string `json:"guard_detail,omitempty"`
	Rejected     string `json:"rejected,omitempty"` // the rewrite a guard stopped
	Style        string `json:"style,omitempty"`
	// Guidance is what the hook adds to the prompt when it reaches claude
	// (PRD Appendix 7): it isn't part of Prompt. A session adds it only
	// when it runs claude with its hook, so the caller decides.
	Guidance string   `json:"guidance,omitempty"`
	Sections []string `json:"sections,omitempty"`
	// HookSettings is the --settings value that makes claude run the hook,
	// as a session passes it, when there is guidance. With
	// PROMPT_ENGINE_RULES_QUEUE set, the guidance is also put in that
	// queue for the hook, and Queued says so.
	HookSettings string `json:"hook_settings,omitempty"`
	Queued       bool   `json:"queued,omitempty"`
	RewriteMS    int64  `json:"rewrite_ms"` // the model already loaded
}

// rewrite prints, as JSON, what pressing Enter in auto mode would send
// for typed, and the guidance the hook would add: the same rewrite, guard
// rails and keyword sections, with no session context, since there is no
// session. It returns 0, 2 if the guards file is invalid, or 1 if the
// guidance can't be queued.
func rewrite(ctx context.Context, w, errw io.Writer, cfg config.Config, typed string) int {
	res := rewriteResult{Typed: typed, Prompt: typed}
	// As in a session: with no templates folder, the built-in guards only.
	r := expand.Rules{}
	if path, err := guardsPath(cfg); err == nil && cfg.Expand.Guards != "off" {
		if r, err = guards.Load(path); err != nil {
			fmt.Fprintf(errw, "prompt-engine: %v\n", err)
			return 2
		}
	}

	s := input.State{Text: typed, Synced: true}
	plan := expand.PlanRewrite(typed, nil)
	if res.NotRewritten = autoSkip(cfg, s, plan); res.NotRewritten == "" {
		// A session loads the model when it starts, so the time limit
		// below doesn't include loading it.
		warmExpander(ctx, cfg, func(string, ...any) {})
		actx, cancel := context.WithTimeout(ctx, cfg.Expand.AutoTimeout+wordsTime(plan.Words))
		start := time.Now()
		a := autoRewrite(actx, cfg, &r, plan, expand.SessionContext{})
		cancel()
		res.RewriteMS, res.Style = time.Since(start).Milliseconds(), a.style
		res.NotRewritten, res.Guard, res.GuardDetail, res.Rejected = a.reason, a.guard, a.detail, a.rejected
		if a.out != "" {
			res.Prompt, res.Rewritten = a.out, true
		}
	}

	// The guidance goes by what was typed, as in a session.
	if dir, err := cfg.TemplatesDir(); err == nil {
		in := &rules.Injector{Dir: dir, MinWords: func() int { return cfg.Templates.MinWords }}
		res.Guidance, res.Sections = in.Prepare(s)
	}
	if res.Guidance != "" {
		exe, err := os.Executable()
		if err == nil {
			exe, err = filepath.Abs(exe)
		}
		if err != nil {
			fmt.Fprintf(errw, "prompt-engine: %v\n", err)
			return 1
		}
		res.HookSettings = rules.Settings(shellQuote(exe) + " " + hookFlag)
		if dir := os.Getenv(rules.EnvQueue); dir != "" {
			if err := (&rules.Queue{Dir: dir}).Put(res.Prompt, res.Guidance); err != nil {
				fmt.Fprintf(errw, "prompt-engine: queue the guidance: %v\n", err)
				return 1
			}
			res.Queued = true
		}
	}

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		fmt.Fprintf(errw, "prompt-engine: %v\n", err)
		return 1
	}
	return 0
}

// autos is automatic expansion on Enter (expand.on_enter: auto, PRD
// Appendix 1, FR-7): key holds back Enter on a real prompt, a goroutine
// expands it, and the rewrite is sent if the guard rails pass, or the
// prompt as typed if not. Esc meanwhile sends it as typed; typing cancels
// the send.
type autos struct {
	ctrl    *ghost.Controller
	tracker *input.Tracker
	sess    *sessionInfo
	live    *atomic.Pointer[config.Config]
	logf    func(string, ...any)
	// What it shares with the other rewrites, from runGhost.
	style    func(config.Config) string        // the style for this prompt
	context  func() expand.SessionContext      // what the model is told
	rules    func(config.Config) *expand.Rules // the guard rails
	expanded func(text string) bool            // text is a rewrite waiting to be sent
	// setExpanded keeps out, s's rewrite in style, for undo, the panel and
	// the history.
	setExpanded func(s input.State, out, style string)

	// mu guards the fields below. key runs with the controller locked, so
	// only expand calls the controller, and never while holding it.
	mu               sync.Mutex
	pending, escaped bool
	cancel           context.CancelFunc
	// For the record of the next prompt sent.
	ran      bool
	stopped  string
	guard    string // the guard rail that stopped the rewrite, if one did
	detail   string // what tripped it (expand.Stop.Detail)
	rejected string // the rewrite it stopped
}

// key is the controller's KeyHook: it reports whether it holds back k.
func (a *autos) key(ctx context.Context, k input.Key, s input.State) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending {
		if k.Type == input.KeyEscape {
			a.escaped = true
			a.cancel()
			return true
		}
		a.pending = false
		a.cancel()
		a.logf("expand: auto expansion cancelled (you kept typing)")
		return false
	}
	cfg := a.live.Load()
	if k.Type != input.KeyEnter || cfg.Expand.OnEnter != "auto" {
		return false
	}
	plan := expand.PlanRewrite(s.Text, s.Pastes)
	if reason := autoSkip(*cfg, s, plan); reason != "" {
		if reason == plan.Skip {
			a.logf("expand: not auto-expanded (%s)", plan.Skip)
		}
		return false
	}
	if a.expanded(s.Text) {
		return false // expanded with the shortcut and reviewed: send it
	}
	actx, cancel := context.WithTimeout(ctx, cfg.Expand.AutoTimeout+wordsTime(plan.Words))
	a.pending, a.escaped, a.cancel = true, false, cancel
	a.logf("expand: Enter held while expanding %q (Esc sends it as typed)", plan.Masked)
	c := *cfg
	c.Expand.Style = a.style(c)
	go a.expand(actx, cancel, c, s, plan)
	return true
}

// expand rewrites the prompt s held back by key, and sends the rewrite, or
// s as typed, unless typing cancelled it.
func (a *autos) expand(actx context.Context, cancel context.CancelFunc, cfg config.Config, s input.State, plan expand.Plan) {
	defer cancel()
	a.ctrl.SetNotice("⟳ expanding…")
	start := time.Now()
	sc := a.context()
	release := a.sess.hold()
	r := autoRewrite(actx, cfg, a.rules(cfg), plan, sc)
	release()
	a.ctrl.SetNotice("")

	a.mu.Lock()
	if !a.pending {
		a.mu.Unlock()
		return // cancelled by typing: nothing to send
	}
	escaped := a.escaped
	a.pending, a.escaped = false, false
	a.mu.Unlock()

	out, style := r.out, r.style
	reason, guard, detail, rejected := r.reason, r.guard, r.detail, r.rejected
	if escaped {
		reason, guard, detail, rejected = "you pressed Esc", "", "", ""
	}
	if reason == "" {
		if err := a.ctrl.Replace(s.Rev, s.Text, out); err != nil {
			reason = "couldn't replace the prompt: " + err.Error()
		} else {
			a.setExpanded(s, out, style)
			a.mu.Lock()
			a.ran, a.stopped, a.guard, a.detail, a.rejected = true, "", "", "", ""
			a.mu.Unlock()
			if err := a.ctrl.Submit(a.tracker.Snapshot().Rev, out); err != nil {
				a.logf("expand: auto expansion not sent: %v", err)
				return
			}
			a.logf("expand: auto expanded and sent %q (%s, style %s)", out, time.Since(start).Round(time.Millisecond), style)
			return
		}
	}
	if guard != "" {
		reason = "guard " + guard + ": " + reason
	}
	a.logf("expand: auto rewrite %q not used (%s); sending as typed", r.masked, reason)
	a.mu.Lock()
	a.ran, a.stopped, a.guard, a.detail, a.rejected = true, reason, guard, detail, rejected
	a.mu.Unlock()
	if err := a.ctrl.Submit(s.Rev, s.Text); err != nil {
		a.logf("expand: not sent: %v", err)
	}
}

// sent records in sub what auto mode did with it, and forgets it for the
// next prompt.
func (a *autos) sent(sub *history.Submission) {
	a.mu.Lock()
	defer a.mu.Unlock()
	sub.Auto, sub.Stopped, sub.Guard, sub.GuardDetail, sub.Rejected = a.ran, a.stopped, a.guard, a.detail, a.rejected
	a.ran, a.stopped, a.guard, a.detail, a.rejected = false, "", "", "", ""
}
