// Package history records the prompts sent to the wrapped CLI: what the
// user typed against what was sent after prompt-engine enhanced it (PRD
// FR-5.4), so they can see over time how expansion and rules change their
// prompts.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pmvieira93/prompt-engine-cli/internal/diff"
)

// Submission is one prompt sent to the wrapped CLI.
type Submission struct {
	Time     time.Time
	Raw      string // what the user typed
	Sent     string // what the CLI received
	Expanded bool   // Raw was rewritten by expansion
	Style    string // the rewrite's style, when Expanded (expand.Expander.StyleName)
	Rules    bool   // rules were appended
	Auto     bool   // expansion ran automatically on Enter
	Stopped  string // why an automatic rewrite wasn't used, if it wasn't
	Guard    string // the guard rail that stopped it, if one did (expand.Stop)
	// GuardDetail is what tripped it, for the panel; not recorded.
	GuardDetail string
	Rejected    string // the rewrite it stopped
}

// Diff compares what was typed with what was sent.
func (s Submission) Diff() []diff.Op { return diff.Words(s.Raw, s.Sent) }

// record is one line of the history file. Prompt text is only included
// when asked for: the file is local, but prompts can hold anything.
type record struct {
	Time         time.Time `json:"time"`
	RawChars     int       `json:"raw_chars"`
	SentChars    int       `json:"sent_chars"`
	RawWords     int       `json:"raw_words"`
	SentWords    int       `json:"sent_words"`
	WordsAdded   int       `json:"words_added"`
	WordsRemoved int       `json:"words_removed"`
	Expanded     bool      `json:"expanded"`
	Style        string    `json:"style,omitempty"`
	Rules        bool      `json:"rules"`
	Auto         bool      `json:"auto,omitempty"`
	Stopped      string    `json:"stopped,omitempty"`
	Guard        string    `json:"guard,omitempty"`
	Raw          string    `json:"raw,omitempty"`
	Sent         string    `json:"sent,omitempty"`
	Rejected     string    `json:"rejected,omitempty"`
}

// Log appends submissions to a JSON Lines file.
type Log struct {
	Path        string
	IncludeText bool

	mu sync.Mutex
}

// DefaultPath is $XDG_STATE_HOME/prompt-engine/history.jsonl, falling back
// to ~/.local/state/prompt-engine/history.jsonl.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "prompt-engine", "history.jsonl"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "prompt-engine", "history.jsonl"), nil
}

// Append writes one line for s, creating the file and its directory if
// needed.
func (l *Log) Append(s Submission) error {
	added, removed := diff.Counts(s.Diff())
	r := record{
		Time:         s.Time.UTC().Round(time.Millisecond),
		RawChars:     len([]rune(s.Raw)),
		SentChars:    len([]rune(s.Sent)),
		RawWords:     len(strings.Fields(s.Raw)),
		SentWords:    len(strings.Fields(s.Sent)),
		WordsAdded:   added,
		WordsRemoved: removed,
		Expanded:     s.Expanded,
		Style:        s.Style,
		Rules:        s.Rules,
		Auto:         s.Auto,
		Stopped:      s.Stopped,
		Guard:        s.Guard,
	}
	if l.IncludeText {
		r.Raw, r.Sent, r.Rejected = s.Raw, s.Sent, s.Rejected
	}
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
