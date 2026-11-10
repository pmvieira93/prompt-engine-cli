package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pmvieira93/prompt-engine-cli/internal/config"
)

func TestRewriteArgs(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		prompt  string
		ok, err bool
	}{
		{[]string{"--pe-rewrite", "fix the bug"}, "fix the bug", true, false},
		{[]string{"--pe-rewrite"}, "", false, true},
		{[]string{"--pe-rewrite", "a", "b"}, "", false, true},
		{[]string{"--", "--pe-rewrite", "a"}, "", false, false}, // the child's
		{[]string{"-p", "hi"}, "", false, false},
	} {
		prompt, ok, err := rewriteArgs(tc.args)
		if prompt != tc.prompt || ok != tc.ok || (err != nil) != tc.err {
			t.Errorf("rewriteArgs(%q) = %q, %v, %v", tc.args, prompt, ok, err)
		}
	}
}

func TestRewrite(t *testing.T) {
	t.Setenv(envSuggester, "fake")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("templates:\n  dir: rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "rules"), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "rules", name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("append.txt", "Keep changes small.\n\n[test|tests]\nRun the tests.\n")
	run := func(typed string) (int, rewriteResult, string) {
		t.Helper()
		var out, errs strings.Builder
		code := rewrite(context.Background(), &out, &errs, cfg, typed)
		var res rewriteResult
		if code == 0 {
			if err := json.Unmarshal([]byte(out.String()), &res); err != nil {
				t.Fatalf("%q: %v", out.String(), err)
			}
		}
		return code, res, errs.String()
	}

	code, res, _ := run("fix the parser tests")
	if code != 0 || !res.Rewritten || !strings.HasPrefix(res.Prompt, "Please fix the parser tests.") || res.Style != "default" {
		t.Fatalf("rewritten: %d %+v", code, res)
	}
	if !strings.HasPrefix(res.Guidance, "# Guidance\nKeep changes small.") || !strings.Contains(res.Guidance, "Run the tests.") ||
		len(res.Sections) != 1 || res.Sections[0] != "test" || strings.Contains(res.Prompt, "Guidance") {
		t.Errorf("guidance: %+v", res)
	}

	// Too short to rewrite, and to get the guidance.
	if _, res, _ = run("continue"); res.Rewritten || res.NotRewritten != "short prompt" || res.Prompt != "continue" || res.Guidance != "" {
		t.Errorf("short: %+v", res)
	}
	if _, res, _ = run("/clear the whole context"); res.Rewritten || res.NotRewritten != "slash command" {
		t.Errorf("slash command: %+v", res)
	}

	// A guard that stops the rewrite: the prompt goes as typed.
	write("guards.txt", "never add: please\n")
	if _, res, _ = run("fix the parser tests"); res.Rewritten || res.Guard == "" || res.Prompt != "fix the parser tests" || res.Sections[0] != "test" {
		t.Errorf("guard: %+v", res)
	}
	// With the guards off, the file is ignored.
	cfg.Expand.Guards = "off"
	if _, res, _ = run("fix the parser tests"); !res.Rewritten {
		t.Errorf("guards off: %+v", res)
	}
	cfg.Expand.Guards = "on"

	// An invalid guards file is an error, with nothing on stdout.
	write("guards.txt", "nonsense line\n")
	var out, errs strings.Builder
	if code := rewrite(context.Background(), &out, &errs, cfg, "fix the parser tests"); code != 2 || out.Len() > 0 || !strings.Contains(errs.String(), "prompt-engine:") {
		t.Errorf("invalid guards: %d %q %q", code, out.String(), errs.String())
	}
}
