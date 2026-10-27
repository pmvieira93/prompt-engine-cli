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
	"github.com/pmvieira93/prompt-engine-cli/internal/ghost"
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
		code, err = runGhost(cmd, cfg, tracker, logf, onFilterError)
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
func runGhost(cmd *exec.Cmd, cfg config.Config, tracker *input.Tracker, logf func(string, ...any), onFilterError func(error)) (int, error) {
	if logf == nil {
		logf = func(string, ...any) {} // no debug log
	}
	// cfg has been validated (or is the defaults), so these parse.
	leader, _ := input.ParseBinding(cfg.Keys.Leader)
	panel, _ := input.ParseBinding(cfg.Keys.Panel)
	ctrl := &ghost.Controller{Tracker: tracker, Logf: logf, Leader: leader, PanelKey: panel}
	var injector *rules.Injector
	if dir, err := cfg.TemplatesDir(); err != nil {
		logf("rules: off: %v", err)
	} else {
		injector = &rules.Injector{Dir: dir, Logf: logf}
		ctrl.BeforeSubmit = injector.BeforeSubmit
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
	if path, err := config.Path(); err != nil {
		logf("config: not watching for edits: %v", err)
	} else {
		go config.Watch(ctx, path, time.Second, func(next config.Config, err error) {
			if err != nil {
				logf("config: %s; keeping the previous settings", strings.ReplaceAll(err.Error(), "\n", " "))
				return
			}
			prev := live.Swap(&next)
			var applied, restart []string
			if next.LLM != prev.LLM {
				worker.Update(suggesterFor(ctx, next.LLM, logf), next.LLM.Debounce, next.LLM.Timeout)
				applied = append(applied, "llm")
			}
			if next.Keys != prev.Keys {
				leader, _ := input.ParseBinding(next.Keys.Leader)
				panel, _ := input.ParseBinding(next.Keys.Panel)
				ctrl.SetKeys(leader, panel)
				applied = append(applied, "keys")
			}
			if next.Panel != prev.Panel {
				applied = append(applied, "panel")
			}
			if next.Command != prev.Command || !slices.Equal(next.Args, prev.Args) || !maps.Equal(next.Env, prev.Env) {
				restart = append(restart, "command/args/env")
			}
			if next.Templates != prev.Templates {
				restart = append(restart, "templates")
			}
			logf("config: reloaded %s; applied: %s; needs a restart: %s",
				path, listOrNone(applied), listOrNone(restart))
		})
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
	src := cfg.Source()
	switch {
	case cfgErr != nil:
		src = fmt.Sprintf("defaults, because the config file failed: %v", cfgErr)
	case src == "":
		path, _ := config.Path()
		src = "defaults (no file at " + path + ")"
	}
	llm := "off (llm.enabled: false)"
	if cfg.LLM.Enabled {
		provider := cfg.LLM.Provider
		if p := os.Getenv(envSuggester); p != "" {
			provider = p + " (from " + envSuggester + ")"
		}
		llm = fmt.Sprintf("%s, model %s", provider, cfg.LLM.Model)
	}
	logf("session start: config %s; suggestions %s", src, llm)
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
