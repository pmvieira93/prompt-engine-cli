package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pmvieira93/prompt-engine-cli/internal/config"
	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
	"github.com/pmvieira93/prompt-engine-cli/internal/guards"
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
