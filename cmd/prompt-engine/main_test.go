package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pmvieira93/prompt-engine-cli/internal/config"
	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
)

func TestOwnFlags(t *testing.T) {
	cases := []struct {
		args     []string
		rest     []string
		cfg      string
		wantsErr bool
	}{
		{[]string{"--resume", "-p", "hi"}, []string{"--resume", "-p", "hi"}, "", false},
		{[]string{"--pe-config", "/tmp/a.yaml", "--resume"}, []string{"--resume"}, "/tmp/a.yaml", false},
		{[]string{"-p", "hi", "--pe-config=/tmp/b.yaml"}, []string{"-p", "hi"}, "/tmp/b.yaml", false},
		{[]string{"--", "--pe-config", "x"}, []string{"--", "--pe-config", "x"}, "", false}, // after --: for the child
		{[]string{"--pe-config"}, nil, "", true},
		{[]string{"--pe-config="}, nil, "", true},
		{nil, nil, "", false},
	}
	for _, tc := range cases {
		rest, cfg, err := ownFlags(tc.args)
		if (err != nil) != tc.wantsErr || cfg != tc.cfg || !slices.Equal(rest, tc.rest) {
			t.Errorf("ownFlags(%q) = %q, %q, %v; want %q, %q, error %v", tc.args, rest, cfg, err, tc.rest, tc.cfg, tc.wantsErr)
		}
	}
}

func TestWantsVersion(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"--pe-version"}, true},
		{[]string{"--resume", "--pe-version"}, true},
		{[]string{"--", "--pe-version"}, false}, // for the child
		{[]string{"--version"}, false},          // claude's own
		{nil, false},
	} {
		if got := wantsVersion(tc.args); got != tc.want {
			t.Errorf("wantsVersion(%q) = %v", tc.args, got)
		}
	}
}

func TestExpansionMode(t *testing.T) {
	cfg := config.Default()
	for mode, want := range map[string]string{
		"off":     "on request (ctrl+q then e)",
		"preview": "preview after a 1s pause",
		"auto":    "automatic on Enter",
	} {
		cfg.Expand.OnEnter = mode
		if got := expansionMode(cfg); got != want {
			t.Errorf("%s: %q, want %q", mode, got, want)
		}
	}
	cfg.Expand.OnEnter, cfg.Expand.PreviewPause = "preview", 1500*time.Millisecond
	if got := expansionMode(cfg); got != "preview after a 1.5s pause" {
		t.Errorf("custom pause: %q", got)
	}
}

func TestTilde(t *testing.T) {
	t.Setenv("HOME", "/home/dev")
	for in, want := range map[string]string{
		"/home/dev/.config/prompt-engine/config.yaml": "~/.config/prompt-engine/config.yaml",
		"/home/dev":         "~",
		"/home/developer/x": "/home/developer/x", // not inside /home/dev
		"/tmp/pe.yaml":      "/tmp/pe.yaml",
		"":                  "",
	} {
		if got := tilde(in); got != want {
			t.Errorf("tilde(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGuardNotes(t *testing.T) {
	got := guardNotes("Guard ", "added-task", "ensure, runs", "Ensure the code runs without errors.\nCheck for the same error message, code and environment.")
	want := []string{"Guard added-task: ensure, runs", "Rejected: “Ensure the code runs without errors. Check for…”"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("guardNotes = %q\nwant         %q", got, want)
	}
	if got := guardNotes("Guard ", "empty", "", "")[0]; got != "Guard empty" {
		t.Errorf("without a detail: %q", got)
	}
	if got := shorten("short", 50); got != "short" {
		t.Errorf("shorten kept %q", got)
	}
}

func TestCheckGuardsArgs(t *testing.T) {
	for _, tc := range []struct {
		args           []string
		typed, rewrite string
		ok, err        bool
	}{
		{[]string{"--pe-check-guards", "a", "b"}, "a", "b", true, false},
		{[]string{"--pe-check-guards", "a"}, "", "", false, true},
		{[]string{"--pe-check-guards", "a", "b", "c"}, "", "", false, true},
		{[]string{"--", "--pe-check-guards", "a", "b"}, "", "", false, false}, // the child's
		{[]string{"--resume"}, "", "", false, false},
	} {
		typed, rewrite, ok, err := checkGuardsArgs(tc.args)
		if typed != tc.typed || rewrite != tc.rewrite || ok != tc.ok || (err != nil) != tc.err {
			t.Errorf("checkGuardsArgs(%q) = %q, %q, %v, %v", tc.args, typed, rewrite, ok, err)
		}
	}
}

func TestCheckGuards(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	run := func(typed, rewrite string) (int, string) {
		var out, errs strings.Builder
		code := checkGuards(&out, &errs, cfg, typed, rewrite)
		if code == 2 {
			if out.Len() > 0 {
				t.Errorf("an error on stdout: %q", out.String())
			}
			return code, errs.String()
		}
		return code, out.String()
	}
	if code, out := run("fix the typo", "Fix the typo."); code != 0 || !strings.Contains(out, "built-in guards only") || !strings.Contains(out, "Passes") {
		t.Errorf("passes: %d %q", code, out)
	}
	path := filepath.Join(dir, "templates", "guards.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("turn off: destructive\nnever add: production\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out := run("restart the api", "Restart the production api.")
	if code != 1 || !strings.Contains(out, `Stopped by never-add (production): the rewrite added "production" (guards.txt line 2: never add).`) {
		t.Errorf("never add: %d %q", code, out)
	}
	if !strings.Contains(out, "(never add 1; off: destructive)") || !strings.Contains(out, "Warning: destructive is off") {
		t.Errorf("no summary or warning: %q", out)
	}
	if err := os.WriteFile(path, []byte("turn off: questoin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := run("a", "b"); code != 2 || !strings.Contains(out, `line 1: no guard "questoin"`) {
		t.Errorf("invalid file: %d %q", code, out)
	}
}

func TestReviewPastesOff(t *testing.T) {
	plan := expand.Plan{Masked: "explain " + expand.Marker(1) + " then " + expand.Marker(2), Kept: []string{"A", "B"}}
	moved := "Explain " + expand.Marker(2) + " then " + expand.Marker(1)
	var r expand.Rules
	if _, stop := review(&r, plan, moved, expand.SessionContext{}); stop == nil || stop.Guard != "pastes" {
		t.Errorf("built in: %+v, want stopped by pastes", stop)
	}
	r.Off = map[string]bool{"pastes": true}
	if out, stop := review(&r, plan, moved, expand.SessionContext{}); stop != nil || out != "Explain B then A" {
		t.Errorf("pastes off: %q, %+v", out, stop)
	}
}

// The panel's guard lines, with no file, with rules, and with a risky
// guard turned off.
func TestGuardLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guards.txt")
	var logged []string
	logf := func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }
	if got := guardLines(path, expand.Rules{}, logf); len(got) != 1 || !strings.Contains(got[0], "built-in only (no ") {
		t.Errorf("no file: %q", got)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := expand.Rules{Off: map[string]bool{"names": true}, NeverAdd: []expand.Rule{{Line: 1, Words: []string{"prod"}}}}
	got := guardLines(path, r, logf)
	if len(got) != 2 || !strings.HasSuffix(got[0], "guards.txt: never add 1; off: names") || !strings.HasPrefix(got[1], "⚠ names is off") {
		t.Errorf("rules: %q", got)
	}
	if !slices.ContainsFunc(logged, func(l string) bool { return strings.Contains(l, "guards: warning: names is off") }) {
		t.Errorf("warning not logged: %q", logged)
	}
}
