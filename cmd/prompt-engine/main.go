// Command prompt-engine wraps an AI CLI (Claude Code by default) in a PTY so
// that user input can be intercepted and enhanced before it reaches the tool.
//
// All command-line arguments are forwarded to the wrapped binary, so
// prompt-engine is a drop-in replacement: `alias claude=prompt-engine`. The
// one exception is --pe-config <file> (or --pe-config=<file>), which picks
// the config file and is removed; the --pe- prefix keeps it clear of the
// wrapped CLI's own flags. A "--" ends this, passing what follows as is.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/pmvieira93/prompt-engine-cli/internal/config"
	"github.com/pmvieira93/prompt-engine-cli/internal/diff"
	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
	"github.com/pmvieira93/prompt-engine-cli/internal/ghost"
	"github.com/pmvieira93/prompt-engine-cli/internal/history"
	"github.com/pmvieira93/prompt-engine-cli/internal/input"
	"github.com/pmvieira93/prompt-engine-cli/internal/pty"
	"github.com/pmvieira93/prompt-engine-cli/internal/rules"
	"github.com/pmvieira93/prompt-engine-cli/internal/suggest"
	"github.com/pmvieira93/prompt-engine-cli/internal/tui"
)

// envActive marks a child already running under prompt-engine, so a nested
// invocation (e.g. via a shell alias) does not wrap twice.
const envActive = "PROMPT_ENGINE_ACTIVE"

// envDebugLog names a file that receives decoded input and every
// input-buffer state change.
const envDebugLog = "PROMPT_ENGINE_DEBUG_LOG"

// Troubleshooting and test switches:
//   - PROMPT_ENGINE_PASSTHROUGH=1 runs the plain pass-through wrapper instead
//     of the emulator UI, to tell whether a glitch comes from the emulator.
//   - PROMPT_ENGINE_SUGGESTER (ollama or fake) overrides llm.provider, e.g.
//     to run the manual tests with the fake suggester's fixed results.
//   - PROMPT_ENGINE_FAKE_DELAY (a duration) slows the fake suggester down to
//     mimic a model.
//   - PROMPT_ENGINE_DEBUG_OUTPUT names a file that receives a copy of all
//     of the child's output, to debug how it draws.
//   - PROMPT_ENGINE_TEST_RENDER_PANIC=1 makes the renderer panic on the first
//     ghost text, to exercise the pass-through fallback (FR-1.3).
const (
	envPassthrough = "PROMPT_ENGINE_PASSTHROUGH"
	envSuggester   = "PROMPT_ENGINE_SUGGESTER"
	envFakeDelay   = "PROMPT_ENGINE_FAKE_DELAY"
	envRenderPanic = "PROMPT_ENGINE_TEST_RENDER_PANIC"
	envDebugOutput = "PROMPT_ENGINE_DEBUG_OUTPUT"
)

func main() {
	os.Exit(run())
}

func run() int {
	cliArgs, cfgPath, err := ownFlags(os.Args[1:])
	if err != nil {
		warn("%v", err)
		return 2
	}
	if cfgPath != "" {
		// The flag wins over the environment. Setting the variable keeps
		// every reader of the config path (the reload watcher too) in step.
		os.Setenv(config.EnvConfigPath, cfgPath)
	}
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		warn("%v; using defaults", cfgErr)
	}

	bin, err := exec.LookPath(cfg.Command)
	if err != nil {
		warn("%v", err)
		return 127
	}
	args := append(slices.Clone(cfg.Args), cliArgs...)
	env := os.Environ()
	for k, v := range cfg.Env {
		env = append(env, k+"="+v)
	}

	// Without an interactive terminal on both ends there is nothing to
	// intercept, and a PTY would make the child emit TUI escape codes into a
	// pipe or file. Replace ourselves with the child instead.
	interactive := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
	if !interactive || os.Getenv(envActive) != "" {
		err := syscall.Exec(bin, append([]string{cfg.Command}, args...), env)
		warn("exec %s: %v", bin, err)
		return 126
	}

	cmd := exec.Command(bin, args...)
	cmd.Env = append(env, envActive+"=1")

	tracker := input.NewTracker()
	var logf func(string, ...any)
	if path := os.Getenv(envDebugLog); path != "" {
		if logf, err = logStates(path, tracker); err != nil {
			warn("debug log: %v", err)
		} else {
			logStartup(logf, cfg, cfgErr)
		}
	}

	filterErr := make(chan error, 1)
	onFilterError := func(err error) { filterErr <- err }
	var code int
	if os.Getenv(envPassthrough) != "" {
		session := &pty.Session{Filter: tracker, OnFilterError: onFilterError}
		code, err = session.Run(cmd)
	} else {
		code, err = runGhost(cmd, cfg, cfgErr, tracker, logf, onFilterError)
	}
	if err != nil {
		warn("%v", err)
		return 1
	}
	select {
	case err := <-filterErr:
		warn("input interception disabled mid-session: %v", err)
	default:
	}
	return code
}

// runGhost runs cmd in the emulator UI with fake ghost-text suggestions.
func runGhost(cmd *exec.Cmd, cfg config.Config, cfgErr error, tracker *input.Tracker, logf func(string, ...any), onFilterError func(error)) (int, error) {
	if logf == nil {
		logf = func(string, ...any) {} // no debug log
	}
	// cfg has been validated (or is the defaults), so these parse.
	ctrl := &ghost.Controller{Tracker: tracker, Logf: logf}
	ctrl.SetKeys(bindings(cfg.Keys))
	var injector *rules.Injector
	if dir, err := cfg.TemplatesDir(); err != nil {
		logf("rules: off: %v", err)
	} else {
		injector = &rules.Injector{Dir: dir, Logf: logf}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The worker runs even with suggestions off, so a config edit can turn
	// them on, and so it keeps resyncing the input buffer for the rules.
	worker := &ghost.Worker{
		Tracker:   tracker,
		Ctrl:      ctrl,
		Suggester: suggesterFor(ctx, cfg.LLM, logf),
		Debounce:  cfg.LLM.Debounce,
		Timeout:   cfg.LLM.Timeout,
		Logf:      logf,
	}
	go worker.Run(ctx)

	// Reload the config when it's edited (FR-4.3). Settings that only
	// matter at startup are reported rather than applied.
	var live atomic.Pointer[config.Config]
	live.Store(&cfg)
	// status is what the side panel shows about the config in use.
	var status atomic.Pointer[[]string]
	setStatus := func(cfg config.Config, cfgErr error, note string) {
		src, llm := describeConfig(cfg, cfgErr)
		lines := []string{"config:      " + src, "suggestions: " + llm}
		if note != "" {
			lines = append(lines, note)
		}
		status.Store(&lines)
	}
	setStatus(cfg, cfgErr, "")
	if path, err := config.Path(); err != nil {
		logf("config: not watching for edits: %v", err)
	} else {
		go config.Watch(ctx, path, time.Second, func(next config.Config, err error) {
			if err != nil {
				msg := strings.ReplaceAll(err.Error(), "\n", " ")
				logf("config: %s; keeping the previous settings", msg)
				setStatus(*live.Load(), nil, "last edit rejected: "+msg)
				return
			}
			prev := live.Swap(&next)
			var applied, restart []string
			if next.LLM != prev.LLM {
				worker.Update(suggesterFor(ctx, next.LLM, logf), next.LLM.Debounce, next.LLM.Timeout)
				applied = append(applied, "llm")
			}
			if next.Keys != prev.Keys {
				ctrl.SetKeys(bindings(next.Keys))
				applied = append(applied, "keys")
			}
			if next.Panel.Side != prev.Panel.Side || next.Panel.Width != prev.Panel.Width {
				applied = append(applied, "panel")
			}
			if next.Expand != prev.Expand {
				applied = append(applied, "expand") // read on each expansion and Enter
			}
			if next.History != prev.History {
				applied = append(applied, "history") // read on each submit
			}
			if next.Panel.Visible != prev.Panel.Visible {
				restart = append(restart, "panel.visible") // only opens it at startup
			}
			if next.Command != prev.Command || !slices.Equal(next.Args, prev.Args) || !maps.Equal(next.Env, prev.Env) {
				restart = append(restart, "command/args/env")
			}
			if next.Templates != prev.Templates {
				restart = append(restart, "templates")
			}
			logf("config: reloaded %s; applied: %s; needs a restart: %s",
				path, listOrNone(applied), listOrNone(restart))
			note := ""
			if len(restart) > 0 {
				note = "needs a restart: " + listOrNone(restart)
			}
			setStatus(next, nil, note)
		})
	}

	// Prompt expansion: leader, then keys.expand, rewrites the prompt in
	// place for the user to review; keys.undo puts the original back.
	// The prompts sent in this session, as sent without the rules, for the
	// expansion model's context (expand.context: prompts).
	var recent struct {
		sync.Mutex
		prompts []string
	}
	earlierPrompts := func() []string {
		ec := live.Load().Expand
		if ec.Context != "prompts" {
			return nil
		}
		recent.Lock()
		defer recent.Unlock()
		return slices.Clone(recent.prompts[max(0, len(recent.prompts)-ec.ContextPrompts):])
	}

	var expansion struct {
		sync.Mutex
		running            bool
		original, expanded string
	}
	var autoExpand func(context.Context, context.CancelFunc, config.Config, input.State, expand.Plan)
	if cfg.Expand.OnEnter == "auto" {
		go warmExpander(ctx, cfg, logf) // so the first Enter isn't slowed by loading it
	}
	ctrl.OnExpand = func(s input.State) {
		if !s.Synced || strings.TrimSpace(s.Text) == "" {
			logf("expand: nothing to expand (empty prompt, or its text is unknown)")
			return
		}
		expansion.Lock()
		if expansion.running {
			expansion.Unlock()
			logf("expand: already expanding")
			return
		}
		plan := expand.PlanRewrite(s.Text, s.Pastes)
		if plan.Skip != "" {
			expansion.Unlock()
			logf("expand: nothing to rewrite: %s", plan.Skip)
			flashNotice(ctrl, "nothing to rewrite: "+plan.Skip)
			return
		}
		expansion.running = true
		expansion.Unlock()

		go func() {
			var after string // a notice to show briefly once done
			defer func() {
				expansion.Lock()
				expansion.running = false
				expansion.Unlock()
				ctrl.SetNotice("")
				if after != "" {
					flashNotice(ctrl, after)
				}
			}()
			cfg := live.Load()
			ctrl.SetNotice("⟳ expanding…")
			ectx, cancel := context.WithTimeout(ctx, cfg.LLM.ExpandTimeout+wordsTime(plan.Words))
			defer cancel()
			start := time.Now()
			masked, err := expandPrompt(ectx, *cfg, plan, earlierPrompts())
			if err != nil {
				logf("expand: failed: %v", err)
				return
			}
			if lost := append(expand.LostNames(plan.Masked, masked), expand.LostQuotes(plan.Masked, masked)...); len(lost) > 0 {
				logf("expand: not applied: the rewrite %q dropped or changed %s", masked, strings.Join(lost, ", "))
				after = "kept your text: the rewrite changed " + strings.Join(lost, ", ")
				return
			}
			out, err := plan.Restore(masked)
			if err != nil {
				logf("expand: not applied: %v: %q", err, masked)
				after = "kept your text: " + err.Error()
				return
			}
			if err := ctrl.Replace(s.Rev, s.Text, out); err != nil {
				logf("expand: not applied: %v", err)
				return
			}
			expansion.Lock()
			expansion.original, expansion.expanded = s.Text, out
			expansion.Unlock()
			logf("expand: replaced %q with %q (%s)", s.Text, out, time.Since(start).Round(time.Millisecond))
		}()
	}
	ctrl.OnUndo = func(s input.State) {
		expansion.Lock()
		original, expanded := expansion.original, expansion.expanded
		expansion.Unlock()
		if expanded == "" || s.Text != expanded {
			logf("expand: nothing to undo (the prompt isn't the last expansion)")
			return
		}
		if err := ctrl.Replace(s.Rev, expanded, original); err != nil {
			logf("expand: undo not applied: %v", err)
			return
		}
		logf("expand: undone, back to %q", original)
	}

	// Automatic expansion on Enter (expand.on_enter: auto, PRD Appendix 1,
	// FR-7): the hook holds back Enter on a real prompt, a goroutine
	// expands it, and the rewrite is sent if the guard rails pass, or the
	// prompt as typed if not. Esc meanwhile sends it as typed; typing
	// cancels the send. The hook runs with the controller locked, so only
	// the goroutine calls the controller.
	var auto struct {
		sync.Mutex
		pending, escaped bool
		cancel           context.CancelFunc
		// For the record of the next prompt sent.
		ran     bool
		stopped string
	}
	ctrl.KeyHook = func(k input.Key, s input.State) bool {
		auto.Lock()
		defer auto.Unlock()
		if auto.pending {
			if k.Type == input.KeyEscape {
				auto.escaped = true
				auto.cancel()
				return true
			}
			auto.pending = false
			auto.cancel()
			logf("expand: auto expansion cancelled (you kept typing)")
			return false
		}
		cfg := live.Load()
		if k.Type != input.KeyEnter || cfg.Expand.OnEnter != "auto" ||
			rules.SkipReason(s) != "" || len(strings.Fields(s.Text)) < cfg.Expand.MinWords {
			return false
		}
		plan := expand.PlanRewrite(s.Text, s.Pastes)
		if plan.Skip != "" {
			logf("expand: not auto-expanded (%s)", plan.Skip)
			return false
		}
		expansion.Lock()
		already := expansion.expanded != "" && s.Text == expansion.expanded
		expansion.Unlock()
		if already {
			return false // expanded with the shortcut and reviewed: send it
		}
		actx, cancel := context.WithTimeout(ctx, cfg.Expand.AutoTimeout+wordsTime(plan.Words))
		auto.pending, auto.escaped, auto.cancel = true, false, cancel
		logf("expand: Enter held while expanding %q (Esc sends it as typed)", plan.Masked)
		go autoExpand(actx, cancel, *cfg, s, plan)
		return true
	}
	autoExpand = func(actx context.Context, cancel context.CancelFunc, cfg config.Config, s input.State, plan expand.Plan) {
		defer cancel()
		ctrl.SetNotice("⟳ expanding…")
		start := time.Now()
		earlier := earlierPrompts()
		masked, err := expandPrompt(actx, cfg, plan, earlier)
		ctrl.SetNotice("")

		auto.Lock()
		if !auto.pending {
			auto.Unlock()
			return // cancelled by typing: nothing to send
		}
		escaped := auto.escaped
		auto.pending, auto.escaped = false, false
		auto.Unlock()

		var reason, out string
		switch {
		case escaped:
			reason = "you pressed Esc"
		case err != nil:
			reason = "expansion failed: " + err.Error()
		default:
			// The guards see what the model rewrote, pastes as markers.
			if reason = expand.Guard(plan.Masked, masked, earlier...); reason == "" {
				if out, err = plan.Restore(masked); err != nil {
					reason = err.Error()
				}
			}
		}
		if reason == "" {
			if err := ctrl.Replace(s.Rev, s.Text, out); err != nil {
				reason = "couldn't replace the prompt: " + err.Error()
			} else {
				expansion.Lock()
				expansion.original, expansion.expanded = s.Text, out
				expansion.Unlock()
				auto.Lock()
				auto.ran, auto.stopped = true, ""
				auto.Unlock()
				if err := ctrl.Submit(tracker.Snapshot().Rev, out); err != nil {
					logf("expand: auto expansion not sent: %v", err)
					return
				}
				logf("expand: auto expanded and sent %q (%s)", out, time.Since(start).Round(time.Millisecond))
				return
			}
		}
		logf("expand: auto rewrite %q not used (%s); sending as typed", masked, reason)
		auto.Lock()
		auto.ran, auto.stopped = true, reason
		auto.Unlock()
		if err := ctrl.Submit(s.Rev, s.Text); err != nil {
			logf("expand: not sent: %v", err)
		}
	}

	// On each prompt sent: append the rules, and record what was typed
	// against what was sent, for the panel and the history (FR-5).
	var lastSent atomic.Pointer[history.Submission]
	var record func(s input.State, appended string)
	ctrl.BeforeSubmit = func(s input.State) []byte {
		var insert []byte
		var appended string
		if injector != nil {
			insert, appended = injector.Prepare(s)
		}
		record(s, appended)
		return insert
	}
	// Leader then Enter sends the prompt as typed: no rules, but recorded.
	ctrl.OnSubmitAsTyped = func(s input.State) { record(s, "") }
	record = func(s input.State, appended string) {
		if rules.SkipReason(s) != "" {
			return // not a message to the model
		}
		sub := history.Submission{Time: time.Now(), Raw: s.Text, Sent: s.Text, Rules: appended != ""}
		expansion.Lock()
		if expansion.expanded != "" && s.Text == expansion.expanded {
			sub.Raw, sub.Expanded = expansion.original, true
		}
		expansion.original, expansion.expanded = "", ""
		expansion.Unlock()
		auto.Lock()
		sub.Auto, sub.Stopped = auto.ran, auto.stopped
		auto.ran, auto.stopped = false, ""
		auto.Unlock()
		if appended != "" {
			sub.Sent += rules.Separator + appended
		}
		lastSent.Store(&sub)
		recent.Lock()
		recent.prompts = append(recent.prompts, expand.ForContext(s.Text, s.Pastes))
		if len(recent.prompts) > 20 { // the most expand.context_prompts allows
			recent.prompts = recent.prompts[1:]
		}
		recent.Unlock()
		added, removed := diff.Counts(sub.Diff())
		logf("submit: %d words typed, %d sent (+%d −%d); expanded=%v rules=%v",
			len(strings.Fields(sub.Raw)), len(strings.Fields(sub.Sent)), added, removed, sub.Expanded, sub.Rules)
		if h := live.Load().History; h.Enabled {
			go recordHistory(h, sub, logf)
		}
	}
	panelDiff := func() *tui.Diff {
		// An expansion waiting to be sent: preview it, with the rules.
		expansion.Lock()
		original, expanded := expansion.original, expansion.expanded
		expansion.Unlock()
		if s := tracker.Snapshot(); expanded != "" && s.Synced && s.Text == expanded {
			preview := expanded
			if injector != nil {
				if r := injector.Text(); r != "" {
					preview += rules.Separator + r
				}
			}
			return &tui.Diff{
				Title: "Ready to send (not sent yet)",
				Ops:   diff.Words(original, preview),
				Notes: []string{"Enter sends it; the undo key puts your text back."},
			}
		}
		sub := lastSent.Load()
		if sub == nil {
			return nil
		}
		ops := sub.Diff()
		added, removed := diff.Counts(ops)
		note := fmt.Sprintf("%s · +%d −%d words", sub.Time.Format("15:04:05"), added, removed)
		switch {
		case sub.Auto && sub.Expanded:
			note += " · auto-expanded"
		case sub.Expanded:
			note += " · expanded"
		}
		if sub.Rules {
			note += " · rules added"
		}
		notes := []string{note}
		if sub.Stopped != "" {
			notes = append(notes, "Sent as typed: the automatic rewrite wasn't used ("+sub.Stopped+").")
		}
		return &tui.Diff{Title: "Last prompt sent", Ops: ops, Notes: notes}
	}

	var rawOutput io.Writer
	if path := os.Getenv(envDebugOutput); path != "" {
		f, err := os.Create(path)
		if err != nil {
			logf("debug output: %v", err)
		} else {
			defer f.Close()
			rawOutput = f
		}
	}

	return tui.Run(cmd, tui.Options{
		Ctrl:          ctrl,
		Tracker:       tracker,
		PanelPercent:  func() int { return live.Load().Panel.Width },
		Status:        func() []string { return *status.Load() },
		Diff:          panelDiff,
		PanelLeft:     func() bool { return live.Load().Panel.Side == "left" },
		PanelOpen:     cfg.Panel.Visible,
		OnFilterError: onFilterError,
		Logf:          logf,
		Rules:         injector,
		RawOutput:     rawOutput,
		PanicOnGhost:  os.Getenv(envRenderPanic) != "",
	})
}

// logStartup records which config a session runs with, since the warning
// for a bad config file is hidden as soon as the child draws its screen.
func logStartup(logf func(string, ...any), cfg config.Config, cfgErr error) {
	src, llm := describeConfig(cfg, cfgErr)
	logf("session start: config %s; suggestions %s", src, llm)
}

// describeConfig says where cfg came from and what suggestions it gives,
// for the debug log and the side panel.
func describeConfig(cfg config.Config, cfgErr error) (src, llm string) {
	src = cfg.Source()
	switch {
	case cfgErr != nil:
		src = "defaults, because the config file failed: " + strings.ReplaceAll(cfgErr.Error(), "\n", " ")
	case src == "":
		path, _ := config.Path()
		src = "defaults (no file at " + path + ")"
	}
	llm = "off (llm.enabled: false)"
	if cfg.LLM.Enabled {
		provider := cfg.LLM.Provider
		if p := os.Getenv(envSuggester); p != "" {
			provider = p
		}
		switch {
		case provider == "fake":
			llm = "fake (fixed word list)"
		default:
			llm = fmt.Sprintf("%s, model %s", provider, cfg.LLM.Model)
		}
		if os.Getenv(envSuggester) != "" {
			llm += " (from " + envSuggester + ")"
		}
	}
	return src, llm
}

// recordHistory appends sub to the history file h names.
func recordHistory(h config.HistoryConfig, sub history.Submission, logf func(string, ...any)) {
	path := h.Path
	if path == "" {
		var err error
		if path, err = history.DefaultPath(); err != nil {
			logf("history: %v", err)
			return
		}
	}
	if err := (&history.Log{Path: path, IncludeText: h.IncludeText}).Append(sub); err != nil {
		logf("history: %v", err)
	}
}

// wordsTime is the extra time allowed to rewrite a prompt of n words:
// a 92-word prompt took 5.3 s with the 7B model, so about 6 s per 100.
func wordsTime(n int) time.Duration { return time.Duration(n) * 60 * time.Millisecond }

// flashNotice shows msg at the cursor for a few seconds.
func flashNotice(ctrl *ghost.Controller, msg string) {
	ctrl.SetNotice(msg)
	time.AfterFunc(4*time.Second, func() {
		if ctrl.Notice() == msg {
			ctrl.SetNotice("")
		}
	})
}

// warmExpander loads the expansion model in the background.
func warmExpander(ctx context.Context, cfg config.Config, logf func(string, ...any)) {
	if cfg.LLM.Provider != "ollama" || os.Getenv(envSuggester) == "fake" {
		return
	}
	wctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	start := time.Now()
	e := &expand.Expander{Endpoint: cfg.LLM.Endpoint, Model: cfg.LLM.ExpandModel, KeepAlive: "30m"}
	if err := e.Warm(wctx); err != nil {
		logf("expand: can't load %s: %v", cfg.LLM.ExpandModel, err)
		return
	}
	logf("expand: %s loaded in %s", cfg.LLM.ExpandModel, time.Since(start).Round(time.Millisecond))
}

// bindings parses validated key settings.
func bindings(k config.KeysConfig) ghost.Keys {
	parse := func(s string) input.Binding { b, _ := input.ParseBinding(s); return b }
	return ghost.Keys{
		Leader: parse(k.Leader), Panel: parse(k.Panel),
		Expand: parse(k.Expand), Undo: parse(k.Undo),
		Wider: parse(k.Wider), Narrower: parse(k.Narrower),
	}
}

// expandPrompt rewrites plan.Masked with the configured model and appends
// git context for the working directory; the caller restores the kept
// parts. With the fake provider it uses a fixed, offline rewrite, so tests
// are repeatable.
func expandPrompt(ctx context.Context, cfg config.Config, plan expand.Plan, earlier []string) (string, error) {
	text := plan.Masked
	dir, _ := os.Getwd()
	provider := cfg.LLM.Provider
	if p := os.Getenv(envSuggester); p != "" {
		provider = p
	}
	templates, _ := cfg.TemplatesDir()
	e := &expand.Expander{
		Endpoint:     cfg.LLM.Endpoint,
		Model:        cfg.LLM.ExpandModel,
		KeepAlive:    "30m",
		TemplatesDir: templates,
		Context:      expand.GitContext(dir),
		Earlier:      func() []string { return earlier },
		TypoOnly:     plan.TypoOnly,
	}
	if provider == "fake" {
		lines := e.Context(ctx)
		out := "Please " + expand.StripContext(text) + "."
		if len(earlier) > 0 {
			out += " (Following: " + earlier[len(earlier)-1] + ")"
		}
		if len(lines) > 0 {
			out += "\n\nContext:\n- " + strings.Join(lines, "\n- ")
		}
		return out, nil
	}
	return e.Expand(ctx, text)
}

// suggesterFor returns the suggester cfg asks for, or nil when
// suggestions are off.
func suggesterFor(ctx context.Context, cfg config.LLMConfig, logf func(string, ...any)) suggest.Suggester {
	if !cfg.Enabled {
		return nil
	}
	return newSuggester(ctx, cfg, logf)
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

// newSuggester builds the configured suggester. For Ollama it loads the
// model in the background, so the first suggestion doesn't wait for it;
// if the server is down, each suggestion fails fast and is only logged.
func newSuggester(ctx context.Context, cfg config.LLMConfig, logf func(string, ...any)) suggest.Suggester {
	provider := cfg.Provider
	if p := os.Getenv(envSuggester); p != "" {
		provider = p
	}
	if provider == "fake" {
		delay, _ := time.ParseDuration(os.Getenv(envFakeDelay))
		return suggest.Fake{Delay: delay}
	}

	o := &suggest.Ollama{Endpoint: cfg.Endpoint, Model: cfg.Model, MaxTokens: cfg.MaxTokens, KeepAlive: "30m"}
	go func() {
		wctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		start := time.Now()
		if err := o.Warm(wctx); err != nil {
			logf("llm: can't use %s at %s, so no suggestions until that's fixed: %v", cfg.Model, cfg.Endpoint, err)
			return
		}
		logf("llm: %s loaded in %s", cfg.Model, time.Since(start).Round(time.Millisecond))
	}()
	return o
}

// logStates appends decoded input and each tracker state change to path
// until the process exits. Mouse reports are skipped: with any-motion
// tracking on, every pointer move would otherwise flood the log.
func logStates(path string, t *input.Tracker) (logf func(string, ...any), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	logf = func(format string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(f, time.Now().Format("15:04:05.000")+" "+format+"\n", a...)
	}

	t.OnKeys = func(raw []byte, keys []input.Key) {
		var names []string
		for _, k := range keys {
			if !bytes.HasPrefix(k.Raw, []byte("\x1b[<")) {
				names = append(names, fmt.Sprintf("%s=%q", k, k.Raw))
			}
		}
		if len(names) > 0 {
			logf("input  %q -> %s", raw, strings.Join(names, " "))
		}
	}
	go func() {
		for range t.Changed() {
			s := t.Snapshot()
			logf("synced=%-5v cursor=%-3d %q", s.Synced, s.Cursor, s.Text)
		}
	}()
	return logf, nil
}

// ownFlags takes prompt-engine's own --pe-config flag out of args and
// returns the rest, for the wrapped CLI, unchanged and in order.
func ownFlags(args []string) (rest []string, cfgPath string, err error) {
	const flag = "--pe-config"
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return append(rest, args[i:]...), cfgPath, nil
		case a == flag:
			if i+1 >= len(args) || args[i+1] == "" {
				return nil, "", fmt.Errorf("%s needs a file: %s <file>", flag, flag)
			}
			cfgPath = args[i+1]
			i++
		case strings.HasPrefix(a, flag+"="):
			if cfgPath = strings.TrimPrefix(a, flag+"="); cfgPath == "" {
				return nil, "", fmt.Errorf("%s needs a file: %s=<file>", flag, flag)
			}
		default:
			rest = append(rest, a)
		}
	}
	return rest, cfgPath, nil
}

func warn(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "prompt-engine: "+format+"\n", a...)
}
