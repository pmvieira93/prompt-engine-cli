// Package rules adds the user's standing guidelines to prompts as they are
// submitted (PRD FR-4.2): the contents of templates/append.txt are pasted
// at the end of the prompt just before the Enter that sends it. Sections of
// the file are added only to prompts with their words (PRD Appendix 6).
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

	"github.com/rivo/uniseg"

	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
	"github.com/pmvieira93/prompt-engine-cli/internal/input"
)

// AppendFile is the template appended to submitted prompts.
const AppendFile = "append.txt"

// maxSize bounds a template, so a wrong path can't paste a huge file.
const maxSize = 64 << 10

// Injector decides, at each submit, whether to add the rules and how.
type Injector struct {
	Dir string // the templates directory

	// Ready reports whether text can be inserted right now: the child's
	// cursor is in its prompt (not a dialog) and it accepts bracketed
	// paste, so the rules' newlines don't submit early. Nil means ready.
	Ready func() bool
	// MinWords returns the fewest words a prompt needs to get the rules,
	// so "yes" or "continue" stays as typed. Nil means any prompt.
	MinWords func() int
	// Typed returns what the developer typed to get prompt, which may be a
	// rewrite: rewrites add words, so sections match on the typed text.
	// Nil means prompt was typed.
	Typed func(prompt string) string
	Logf  func(format string, a ...any)

	mu        sync.Mutex
	cached    template
	cachedErr error
	cachedAt  time.Time
	last      []string // the sections added to the last prompt
}

// BeforeSubmit returns the input to send just before the Enter that
// submits s, or nil to submit it unchanged.
func (in *Injector) BeforeSubmit(s input.State) []byte {
	insert, _, _ := in.Prepare(s)
	return insert
}

// Prepare is BeforeSubmit that also returns the rules text being appended
// ("" if none) and the sections in it. The template is read on every call,
// so edits apply to the next prompt without a restart.
func (in *Injector) Prepare(s input.State) (insert []byte, appended string, sections []string) {
	t, err := in.load()
	switch {
	case err != nil:
		in.logf("rules: not added: %v", err)
		return nil, "", nil
	case t.empty():
		return nil, "", nil
	}
	if reason := SkipReason(s); reason != "" {
		in.logf("rules: not added (%s)", reason)
		return nil, "", nil
	}
	in.setLast(nil)
	if in.short(s.Text) {
		in.logf("rules: not added (short prompt)")
		return nil, "", nil
	}
	text, sections := t.forPrompt(in.typed(s.Text))
	if text == "" {
		in.logf("rules: not added (no section matches)")
		return nil, "", nil
	}
	if in.Ready != nil && !in.Ready() {
		in.logf("rules: not added (cursor not in the prompt, or no bracketed paste)")
		return nil, "", nil
	}

	var b strings.Builder
	// Paste at the end even if the cursor is mid-text. Right moves one
	// user-perceived character in claude, like the input buffer does.
	for range uniseg.GraphemeClusterCount(string([]rune(s.Text)[s.Cursor:])) {
		b.WriteString("\x1b[C")
	}
	b.WriteString("\x1b[200~" + Separator)
	b.WriteString(text)
	b.WriteString("\x1b[201~")
	unit, lines := "lines", strings.Count(text, "\n")+1
	if lines == 1 {
		unit = "line"
	}
	note := fmt.Sprintf("%d %s", lines, unit)
	if len(sections) > 0 {
		note += "; sections: " + strings.Join(sections, ", ")
	}
	in.logf("rules: appended %s (%s)", AppendFile, note)
	in.setLast(sections)
	return []byte(b.String()), text, sections
}

// short reports whether text has too few words to get the rules. The
// context lines a rewrite ends with don't count: they alone are 3 words.
func (in *Injector) short(text string) bool {
	return in.MinWords != nil && len(strings.Fields(expand.StripContext(text))) < in.MinWords()
}

// Text returns the rules that would be appended now to prompt, for
// previews.
func (in *Injector) Text(prompt string) string {
	if in.short(prompt) {
		return ""
	}
	t, _ := in.loadCached()
	text, _ := t.forPrompt(in.typed(prompt))
	return text
}

// Check returns what's wrong with the template now, if anything.
func (in *Injector) Check() error {
	_, err := in.load()
	return err
}

// Status returns the side panel's lines about the template: what's wrong
// with it, or with sections, the ones added to the last prompt.
func (in *Injector) Status() []string {
	t, err := in.loadCached()
	if err != nil {
		return []string{"⚠ rules not added: " + err.Error()}
	}
	if len(t.sections) == 0 {
		return nil
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	last := "none"
	if len(in.last) > 0 {
		last = strings.Join(in.last, ", ")
	}
	unit := "sections"
	if len(t.sections) == 1 {
		unit = "section"
	}
	return []string{fmt.Sprintf("rules:       %d %s; last prompt: %s", len(t.sections), unit, last)}
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

func (in *Injector) setLast(sections []string) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.last = sections
}

func (in *Injector) typed(prompt string) string {
	if in.Typed != nil {
		return in.Typed(prompt)
	}
	return prompt
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

// Separator goes between a prompt and the rules appended to it.
const Separator = "\n\n"

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

// read returns the template with escape characters removed (one could end
// the bracketed paste early) and line endings normalized. A missing file
// means no rules.
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
	text := strings.ReplaceAll(string(data), "\x1b", "")
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), nil
}

func (in *Injector) logf(format string, a ...any) {
	if in.Logf != nil {
		in.Logf(format, a...)
	}
}
