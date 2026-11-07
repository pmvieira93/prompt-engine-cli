// Package rules adds the user's standing guidelines to prompts as they are
// submitted (PRD FR-4.2): the contents of templates/append.txt reach claude
// through a hook, as context for the prompt (PRD Appendix 7). Sections of the
// file are added only to prompts with their words (PRD Appendix 6).
package rules

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
	"github.com/pmvieira93/prompt-engine-cli/internal/input"
)

// AppendFile is the template added to submitted prompts.
const AppendFile = "append.txt"

// maxSize bounds a template, so a wrong path can't send a huge file.
const maxSize = 64 << 10

// Injector decides, at each submit, whether to add the rules and how.
type Injector struct {
	Dir string // the templates directory

	// Ready reports whether Enter sends the prompt right now: the child's
	// cursor is in its prompt, not a dialog. Nil means ready.
	Ready func() bool
	// MinWords returns the fewest words a prompt needs to get the rules,
	// so "yes" or "continue" stays as typed. Nil means any prompt.
	MinWords func() int
	// Typed returns what the developer typed to get s, and the pastes in
	// it, since s may be a rewrite: rewrites add words, so the rules go by
	// the typed text. Nil means s was typed.
	Typed func(s input.State) (string, []string)
	Logf  func(format string, a ...any)

	mu        sync.Mutex
	cached    template
	cachedErr error
	cachedAt  time.Time
}

// Prepare returns the rules for prompt s ("" if none) and the sections in
// them. The template is read on every call, so edits apply to the next
// prompt without a restart.
func (in *Injector) Prepare(s input.State) (appended string, sections []string) {
	t, err := in.load()
	switch {
	case err != nil:
		in.logf("rules: not added: %v", err)
		return "", nil
	case t.empty():
		return "", nil
	}
	if reason := SkipReason(s); reason != "" {
		in.logf("rules: not added (%s)", reason)
		return "", nil
	}
	typed, pastes := in.typed(s)
	if in.short(typed) {
		in.logf("rules: not added (short prompt)")
		return "", nil
	}
	text, sections := t.forPrompt(typed, pastes)
	if text == "" {
		in.logf("rules: not added (no section matches)")
		return "", nil
	}
	if in.Ready != nil && !in.Ready() {
		in.logf("rules: not added (cursor not in the prompt)")
		return "", nil
	}
	unit, lines := "lines", strings.Count(text, "\n")+1
	if lines == 1 {
		unit = "line"
	}
	note := fmt.Sprintf("%d %s", lines, unit)
	if len(sections) > 0 {
		note += "; sections: " + strings.Join(sections, ", ")
	}
	in.logf("rules: appended %s (%s)", AppendFile, note)
	return Heading + "\n" + text, sections
}

// short reports whether text has too few words to get the rules. The
// context lines a rewrite ends with don't count: they alone are 3 words.
func (in *Injector) short(text string) bool {
	return in.MinWords != nil && len(strings.Fields(expand.StripContext(text))) < in.MinWords()
}

// Text returns the rules that would be appended now to prompt, for
// previews.
func (in *Injector) Text(prompt string) string {
	typed, pastes := in.typed(input.State{Text: prompt})
	if in.short(typed) {
		return ""
	}
	t, _ := in.loadCached()
	text, _ := t.forPrompt(typed, pastes)
	if text == "" {
		return ""
	}
	return Heading + "\n" + text
}

// Check returns what's wrong with the template now, if anything.
func (in *Injector) Check() error {
	_, err := in.load()
	return err
}

// Status returns the side panel's lines about the template: what's wrong
// with it, or with sections, last, the ones added to the last prompt.
func (in *Injector) Status(last []string) []string {
	t, err := in.loadCached()
	if err != nil {
		return []string{"⚠ rules not added: " + err.Error()}
	}
	if len(t.sections) == 0 {
		return nil
	}
	added := "none"
	if len(last) > 0 {
		added = strings.Join(last, ", ")
	}
	unit := "sections"
	if len(t.sections) == 1 {
		unit = "section"
	}
	return []string{fmt.Sprintf("rules:       %d %s; last prompt: %s", len(t.sections), unit, added)}
}

// loadCached is load at most once a second, since a preview or the panel
// is redrawn far more often than that.
func (in *Injector) loadCached() (template, error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if time.Since(in.cachedAt) > time.Second {
		in.cached, in.cachedErr = in.load()
		in.cachedAt = time.Now()
	}
	return in.cached, in.cachedErr
}

func (in *Injector) typed(s input.State) (string, []string) {
	if in.Typed != nil {
		return in.Typed(s)
	}
	return s.Text, s.Pastes
}

// load reads and parses the template. No file means no rules.
func (in *Injector) load() (template, error) {
	text, err := in.read()
	if err != nil {
		return template{}, err
	}
	t, err := parse(text)
	if err != nil {
		return template{}, fmt.Errorf("%s %w", AppendFile, err)
	}
	return t, nil
}

// Separator goes between a prompt and its rules where they're shown together.
const Separator = "\n\n"

// Heading starts the rules added to a prompt, so they read apart from it.
const Heading = "# Guidance"

// SkipReason says why Enter on s doesn't send a message to the model (so
// it gets no rules), or "" if it does.
func SkipReason(s input.State) string {
	trimmed := strings.TrimSpace(s.Text)
	switch {
	case !s.Synced:
		return "prompt text unknown"
	case trimmed == "":
		return "empty prompt"
	case strings.HasPrefix(trimmed, "/"):
		return "slash command"
	case strings.HasPrefix(trimmed, "!"):
		return "bash mode"
	case strings.HasPrefix(trimmed, "#"):
		return "memory note"
	}
	return ""
}

// read returns the template with escape characters removed (the panel
// draws the rules, and one could move its cursor), line endings normalized
// and any byte order mark dropped. A missing file means no rules.
func (in *Injector) read() (string, error) {
	path := filepath.Join(in.Dir, AppendFile)
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if info.Size() > maxSize {
		return "", fmt.Errorf("%s is %d bytes, over the %d-byte limit", path, info.Size(), maxSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	text = strings.ReplaceAll(text, "\x1b", "")
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), nil
}

func (in *Injector) logf(format string, a ...any) {
	if in.Logf != nil {
		in.Logf(format, a...)
	}
}
