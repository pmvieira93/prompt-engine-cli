// Package config loads prompt-engine settings from the XDG config directory
// (~/.config/prompt-engine/config.yaml by default), layering the file over
// built-in defaults.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	appDir   = "prompt-engine"
	fileName = "config.yaml"

	// EnvConfigPath overrides the config file location entirely.
	EnvConfigPath = "PROMPT_ENGINE_CONFIG"
)

// Config is the full runtime configuration.
type Config struct {
	// Command is the wrapped binary, resolved via $PATH.
	Command string `yaml:"command"`
	// Args are prepended to any arguments passed on the command line.
	Args []string `yaml:"args"`
	// Env is merged into the child's environment.
	Env map[string]string `yaml:"env"`

	LLM       LLMConfig       `yaml:"llm"`
	Panel     PanelConfig     `yaml:"panel"`
	Templates TemplatesConfig `yaml:"templates"`

	// path is the file this config was loaded from ("" if defaults only).
	path string
}

// LLMConfig configures the local suggestion backend (Phase 2).
type LLMConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Endpoint string        `yaml:"endpoint"`
	Model    string        `yaml:"model"`
	Debounce time.Duration `yaml:"debounce"`
	Timeout  time.Duration `yaml:"timeout"`
}

// PanelConfig configures the prompt diff side panel (Phase 4).
type PanelConfig struct {
	Visible bool   `yaml:"visible"`
	Side    string `yaml:"side"`
	Width   int    `yaml:"width"`
}

// TemplatesConfig locates prompt templates. Relative paths resolve against
// the config directory.
type TemplatesConfig struct {
	Dir string `yaml:"dir"`
}

// Default returns the built-in configuration used when no file exists.
func Default() Config {
	return Config{
		Command: "claude",
		LLM: LLMConfig{
			Enabled:  true,
			Endpoint: "http://localhost:11434",
			Model:    "qwen2.5:1.5b",
			Debounce: 150 * time.Millisecond,
			Timeout:  2 * time.Second,
		},
		Panel: PanelConfig{
			Side:  "right",
			Width: 40,
		},
		Templates: TemplatesConfig{
			Dir: "templates",
		},
	}
}

// Dir returns the prompt-engine config directory: $XDG_CONFIG_HOME/prompt-engine,
// falling back to ~/.config/prompt-engine. os.UserConfigDir is deliberately not
// used because it resolves to ~/Library/Application Support on macOS.
func Dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, appDir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", appDir), nil
}

// Path returns the config file location, honoring $PROMPT_ENGINE_CONFIG.
func Path() (string, error) {
	if p := os.Getenv(EnvConfigPath); p != "" {
		return p, nil
	}
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load reads the config file from Path. A missing file is not an error: the
// defaults are returned. On any error the returned Config is still the
// defaults, so callers can warn and continue.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Default(), err
	}
	return LoadFile(path)
}

// LoadFile reads the config at path over the defaults. Unknown keys are
// rejected so typos surface instead of being silently ignored.
func LoadFile(path string) (Config, error) {
	cfg := Default()

	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	loaded := cfg
	if err := dec.Decode(&loaded); err != nil && !errors.Is(err, io.EOF) {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	loaded.path = path
	if err := loaded.Validate(); err != nil {
		return cfg, fmt.Errorf("invalid %s: %w", path, err)
	}
	return loaded, nil
}

// Validate reports the first invalid setting.
func (c Config) Validate() error {
	switch {
	case c.Command == "":
		return errors.New("command must not be empty")
	case c.LLM.Debounce < 0:
		return errors.New("llm.debounce must not be negative")
	case c.LLM.Timeout <= 0:
		return errors.New("llm.timeout must be positive")
	case c.Panel.Side != "left" && c.Panel.Side != "right":
		return fmt.Errorf("panel.side must be \"left\" or \"right\", got %q", c.Panel.Side)
	case c.Panel.Width < 10 || c.Panel.Width > 90:
		return fmt.Errorf("panel.width must be a percentage between 10 and 90, got %d", c.Panel.Width)
	}
	return nil
}

// Source returns the file the config was loaded from, or "" for defaults.
func (c Config) Source() string { return c.path }

// TemplatesDir returns the absolute templates directory.
func (c Config) TemplatesDir() (string, error) {
	if filepath.IsAbs(c.Templates.Dir) {
		return c.Templates.Dir, nil
	}
	base := filepath.Dir(c.path)
	if c.path == "" {
		dir, err := Dir()
		if err != nil {
			return "", err
		}
		base = dir
	}
	return filepath.Join(base, c.Templates.Dir), nil
}
