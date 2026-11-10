package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pmvieira93/prompt-engine-cli/internal/config"
	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
	"github.com/pmvieira93/prompt-engine-cli/internal/guards"
	"github.com/pmvieira93/prompt-engine-cli/internal/input"
	"github.com/pmvieira93/prompt-engine-cli/internal/rules"
)

const rewriteFlag = "--pe-rewrite"

// rewriteArgs finds --pe-rewrite "<prompt>" before any "--".
func rewriteArgs(args []string) (prompt string, ok bool, err error) {
	for i, a := range args {
		switch a {
		case "--":
			return "", false, nil
		case rewriteFlag:
			if len(args) != i+2 {
				return "", false, fmt.Errorf("usage: prompt-engine %s \"<prompt>\"", rewriteFlag)
			}
			return args[i+1], true, nil
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
	Style        string `json:"style,omitempty"`
	// Guidance is what the hook adds to the prompt when it reaches claude
	// (PRD Appendix 7): it isn't part of Prompt.
	Guidance  string   `json:"guidance,omitempty"`
	Sections  []string `json:"sections,omitempty"`
	RewriteMS int64    `json:"rewrite_ms"`
}

// rewrite prints, as JSON, what pressing Enter in auto mode would send
// for typed, and the guidance the hook would add: the same rewrite, guard
// rails and keyword sections, with no session context, since there is no
// session. It returns 0, or 2 if the guards file is invalid.
func rewrite(ctx context.Context, w, errw io.Writer, cfg config.Config, typed string) int {
	res := rewriteResult{Typed: typed, Prompt: typed}
	r := expand.Rules{}
	if cfg.Expand.Guards != "off" {
		path, err := guardsPath(cfg)
		if err == nil {
			r, err = guards.Load(path)
		}
		if err != nil {
			fmt.Fprintf(errw, "prompt-engine: %v\n", err)
			return 2
		}
	}

	s := input.State{Text: typed, Synced: true}
	plan := expand.PlanRewrite(typed, nil)
	switch {
	case rules.SkipReason(s) != "":
		res.NotRewritten = rules.SkipReason(s)
	case len(strings.Fields(typed)) < cfg.Expand.MinWords:
		res.NotRewritten = "short prompt"
	case plan.Skip != "":
		res.NotRewritten = plan.Skip
	default:
		actx, cancel := context.WithTimeout(ctx, cfg.Expand.AutoTimeout+wordsTime(plan.Words))
		start := time.Now()
		masked, style, err := expandPrompt(actx, cfg, &r, plan, expand.SessionContext{})
		cancel()
		res.RewriteMS, res.Style = time.Since(start).Milliseconds(), style
		if err != nil {
			res.NotRewritten = "expansion failed: " + err.Error()
			break
		}
		out, stop := review(&r, plan, masked, expand.SessionContext{})
		if stop != nil {
			res.NotRewritten, res.Guard = stop.Reason, stop.Guard
			break
		}
		res.Prompt, res.Rewritten = out, true
	}

	// The guidance goes by what was typed, as in a session.
	if dir, err := cfg.TemplatesDir(); err == nil {
		in := &rules.Injector{Dir: dir, MinWords: func() int { return cfg.Templates.MinWords }}
		res.Guidance, res.Sections = in.Prepare(s)
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
