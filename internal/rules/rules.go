// Package rules adds the user's standing guidelines to prompts as they are
// submitted (PRD FR-4.2): the contents of templates/append.txt are pasted
// at the end of the prompt just before the Enter that sends it.
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

	"github.com/pmvieira93/prompt-engine-cli/internal/input"
)

// AppendFile is the template appended to every submitted prompt.
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
	Logf  func(format string, a ...any)

	mu       sync.Mutex
	cached   string
	cachedAt time.Time
}

// BeforeSubmit returns the input to send just before the Enter that
// submits s, or nil to submit it unchanged.
func (in *Injector) BeforeSubmit(s input.State) []byte {
	insert, _ := in.Prepare(s)
	return insert
}

// Prepare is BeforeSubmit that also returns the rules text being appended
// ("" if none). The template is read on every call, so edits apply to the
// next prompt without a restart.
func (in *Injector) Prepare(s input.State) (insert []byte, appended string) {
	text, err := in.read()
	switch {
	case err != nil:
		in.logf("rules: not added: %v", err)
		return nil, ""
	case text == "":
		return nil, ""
	}
	if reason := SkipReason(s); reason != "" {
		in.logf("rules: not added (%s)", reason)
		return nil, ""
	}
	if in.Ready != nil && !in.Ready() {
		in.logf("rules: not added (cursor not in the prompt, or no bracketed paste)")
		return nil, ""
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
	in.logf("rules: appended %s (%d %s)", AppendFile, lines, unit)
	return []byte(b.String()), text
}

// Text returns the rules that would be appended now, for previews. It
// reads the template at most once a second, since a preview is redrawn
// far more often than that.
func (in *Injector) Text() string {
	in.mu.Lock()
	defer in.mu.Unlock()
	if time.Since(in.cachedAt) > time.Second {
		in.cached, _ = in.read()
		in.cachedAt = time.Now()
	}
	return in.cached
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
// the bracketed paste early), line endings normalized, and surrounding
// blank space trimmed. A missing file means no rules.
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
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	return strings.TrimSpace(text), nil
}

func (in *Injector) logf(format string, a ...any) {
	if in.Logf != nil {
		in.Logf(format, a...)
	}
}
