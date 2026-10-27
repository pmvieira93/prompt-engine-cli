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
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/pmvieira93/prompt-engine-cli/internal/config"
	"github.com/pmvieira93/prompt-engine-cli/internal/ghost"
	"github.com/pmvieira93/prompt-engine-cli/internal/input"
	"github.com/pmvieira93/prompt-engine-cli/internal/pty"
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
//   - PROMPT_ENGINE_TEST_RENDER_PANIC=1 makes the renderer panic on the first
//     ghost text, to exercise the pass-through fallback (FR-1.3).
const (
	envPassthrough = "PROMPT_ENGINE_PASSTHROUGH"
	envSuggester   = "PROMPT_ENGINE_SUGGESTER"
	envFakeDelay   = "PROMPT_ENGINE_FAKE_DELAY"
	envRenderPanic = "PROMPT_ENGINE_TEST_RENDER_PANIC"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load()
	if err != nil {
		warn("%v; using defaults", err)
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if cfg.LLM.Enabled {
		worker := &ghost.Worker{
			Tracker:   tracker,
			Ctrl:      ctrl,
			Suggester: newSuggester(ctx, cfg.LLM, logf),
			Debounce:  cfg.LLM.Debounce,
			Timeout:   cfg.LLM.Timeout,
			Logf:      logf,
		}
		go worker.Run(ctx)
	}

	return tui.Run(cmd, tui.Options{
		Ctrl:          ctrl,
		Tracker:       tracker,
		PanelPercent:  cfg.Panel.Width,
		OnFilterError: onFilterError,
		Logf:          logf,
		PanicOnGhost:  os.Getenv(envRenderPanic) != "",
	})
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
