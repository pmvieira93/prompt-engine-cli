// Command prompt-engine wraps an AI CLI (Claude Code by default) in a PTY so
// that user input can be intercepted and enhanced before it reaches the tool.
//
// All command-line arguments are forwarded to the wrapped binary, so
// prompt-engine is a drop-in replacement: `alias claude=prompt-engine`.
package main

import (
	"bytes"
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
	"github.com/pmvieira93/prompt-engine-cli/internal/input"
	"github.com/pmvieira93/prompt-engine-cli/internal/pty"
)

// envActive marks a child already running under prompt-engine, so a nested
// invocation (e.g. via a shell alias) does not wrap twice.
const envActive = "PROMPT_ENGINE_ACTIVE"

// envDebugLog names a file that receives decoded input and every
// input-buffer state change.
const envDebugLog = "PROMPT_ENGINE_DEBUG_LOG"

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
	if path := os.Getenv(envDebugLog); path != "" {
		if err := logStates(path, tracker); err != nil {
			warn("debug log: %v", err)
		}
	}

	filterErr := make(chan error, 1)
	session := &pty.Session{
		Filter:        tracker,
		OnFilterError: func(err error) { filterErr <- err },
	}
	code, err := session.Run(cmd)
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

// logStates appends decoded input and each tracker state change to path
// until the process exits. Mouse reports are skipped: with any-motion
// tracking on, every pointer move would otherwise flood the log.
func logStates(path string, t *input.Tracker) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	var mu sync.Mutex
	logf := func(format string, a ...any) {
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
	return nil
}

func warn(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "prompt-engine: "+format+"\n", a...)
}
