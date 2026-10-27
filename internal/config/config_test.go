package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pmvieira93/prompt-engine-cli/internal/input"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFileMissingReturnsDefaults(t *testing.T) {
	cfg, err := LoadFile(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Command != "claude" || cfg.Source() != "" {
		t.Fatalf("expected defaults, got %+v", cfg)
	}
}

func TestLoadFileEmptyReturnsDefaults(t *testing.T) {
	cfg, err := LoadFile(writeConfig(t, ""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LLM.Debounce != 150*time.Millisecond {
		t.Fatalf("expected default debounce, got %v", cfg.LLM.Debounce)
	}
}

func TestLoadFilePartialOverride(t *testing.T) {
	path := writeConfig(t, `
command: /usr/local/bin/claude
args: ["--verbose"]
llm:
  model: llama3.2:1b
  debounce: 250ms
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Command != "/usr/local/bin/claude" || len(cfg.Args) != 1 {
		t.Errorf("command/args not applied: %+v", cfg)
	}
	if cfg.LLM.Model != "llama3.2:1b" || cfg.LLM.Debounce != 250*time.Millisecond {
		t.Errorf("llm not applied: %+v", cfg.LLM)
	}
	if cfg.LLM.Endpoint != "http://localhost:11434" || cfg.Panel.Width != 40 {
		t.Errorf("defaults lost on partial override: %+v", cfg)
	}
	if cfg.Source() != path {
		t.Errorf("source = %q, want %q", cfg.Source(), path)
	}
}

func TestLoadFileErrorsFallBackToDefaults(t *testing.T) {
	cases := map[string]string{
		"unknown key": "comand: claude\n",
		"bad yaml":    "llm: [\n",
		"invalid":     "panel:\n  side: top\n",
		"bad leader":  "keys:\n  leader: ctrl+1\n",
		"same keys":   "keys:\n  leader: p\n  panel: p\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := LoadFile(writeConfig(t, body))
			if err == nil {
				t.Fatal("expected error")
			}
			if cfg.Command != "claude" || cfg.Panel.Side != "right" {
				t.Fatalf("expected defaults on error, got %+v", cfg)
			}
		})
	}
}

func TestPathResolution(t *testing.T) {
	t.Setenv(EnvConfigPath, "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if p, _ := Path(); p != "/xdg/prompt-engine/config.yaml" {
		t.Errorf("XDG path = %q", p)
	}

	// Relative XDG_CONFIG_HOME is invalid per spec and must be ignored.
	t.Setenv("XDG_CONFIG_HOME", "relative")
	if p, _ := Path(); !strings.HasSuffix(p, "/.config/prompt-engine/config.yaml") {
		t.Errorf("fallback path = %q", p)
	}

	t.Setenv(EnvConfigPath, "/custom.yaml")
	if p, _ := Path(); p != "/custom.yaml" {
		t.Errorf("override path = %q", p)
	}
}

func TestTemplatesDirRelativeToConfig(t *testing.T) {
	path := writeConfig(t, "")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := cfg.TemplatesDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(filepath.Dir(path), "templates"); dir != want {
		t.Errorf("templates dir = %q, want %q", dir, want)
	}
}

func TestDefaultLeaderIsCtrlQ(t *testing.T) {
	leader, err := input.ParseBinding(Default().Keys.Leader)
	if err != nil {
		t.Fatal(err)
	}
	var d input.Decoder
	for _, in := range []string{"\x11", "\x1b[113;5u", "\x1b[27;5;113~"} {
		keys := d.Decode([]byte(in))
		if len(keys) != 1 || !leader.Matches(keys[0]) {
			t.Errorf("default leader does not match %q", in)
		}
	}
}
