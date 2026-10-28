// Command prompt-engine wraps an AI CLI (Claude Code by default) in a PTY so
// that user input can be intercepted and enhanced before it reaches the tool.
//
// All command-line arguments are forwarded to the wrapped binary, so
// prompt-engine is a drop-in replacement: `alias claude=prompt-engine`.
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
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		warn("%v; using defaults", cfgErr)
	}

	bin, err := exec.LookPath(cfg.Command)
	if err != nil {
		warn("%v", err)
		return 127
	}
	args := append(slices.Clone(cfg.Args), os.Args[1:]...)
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
	var expansion struct {
		sync.Mutex
		running            bool
		original, expanded string
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
		expansion.running = true
		expansion.Unlock()

		go func() {
			defer func() {
				expansion.Lock()
				expansion.running = false
				expansion.Unlock()
				ctrl.SetNotice("")
			}()
			cfg := live.Load()
			ctrl.SetNotice("⟳ expanding…")
			ectx, cancel := context.WithTimeout(ctx, cfg.LLM.ExpandTimeout)
			defer cancel()
			start := time.Now()
			out, err := expandPrompt(ectx, *cfg, s.Text)
			if err != nil {
				logf("expand: failed: %v", err)
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

	// On each prompt sent: append the rules, and record what was typed
	// against what was sent, for the panel and the history (FR-5).
	var lastSent atomic.Pointer[history.Submission]
	ctrl.BeforeSubmit = func(s input.State) []byte {
		var insert []byte
		var appended string
		if injector != nil {
			insert, appended = injector.Prepare(s)
		}
		if rules.SkipReason(s) != "" {
			return insert // not a message to the model
		}
		sub := history.Submission{Time: time.Now(), Raw: s.Text, Sent: s.Text, Rules: appended != ""}
		expansion.Lock()
		if expansion.expanded != "" && s.Text == expansion.expanded {
			sub.Raw, sub.Expanded = expansion.original, true
		}
		expansion.original, expansion.expanded = "", ""
		expansion.Unlock()
		if appended != "" {
			sub.Sent += rules.Separator + appended
		}
		lastSent.Store(&sub)
		added, removed := diff.Counts(sub.Diff())
		logf("submit: %d words typed, %d sent (+%d −%d); expanded=%v rules=%v",
			len(strings.Fields(sub.Raw)), len(strings.Fields(sub.Sent)), added, removed, sub.Expanded, sub.Rules)
		if h := live.Load().History; h.Enabled {
			go recordHistory(h, sub, logf)
		}
		return insert
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
		if sub.Expanded {
			note += " · expanded"
		}
		if sub.Rules {
			note += " · rules added"
		}
		return &tui.Diff{Title: "Last prompt sent", Ops: ops, Notes: []string{note}}
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

// bindings parses validated key settings.
func bindings(k config.KeysConfig) ghost.Keys {
	parse := func(s string) input.Binding { b, _ := input.ParseBinding(s); return b }
	return ghost.Keys{
		Leader: parse(k.Leader), Panel: parse(k.Panel),
		Expand: parse(k.Expand), Undo: parse(k.Undo),
		Wider: parse(k.Wider), Narrower: parse(k.Narrower),
	}
}

// expandPrompt rewrites text with the configured model and appends git
// context for the working directory. With the fake provider it uses a
// fixed, offline rewrite, so tests are repeatable.
func expandPrompt(ctx context.Context, cfg config.Config, text string) (string, error) {
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
	}
	if provider == "fake" {
		lines := e.Context(ctx)
		out := "Please " + expand.StripContext(text) + "."
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

func warn(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "prompt-engine: "+format+"\n", a...)
}
