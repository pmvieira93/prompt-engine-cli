package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pmvieira93/prompt-engine-cli/internal/config"
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
