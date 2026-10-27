// Package suggest produces inline completions for the prompt being typed.
package suggest

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Suggester returns the completion suffix to insert at cursor (a rune
// offset into text). An empty result means no suggestion.
type Suggester interface {
	Suggest(ctx context.Context, text string, cursor int) (string, error)
}

// DefaultWords is the vocabulary Fake completes from.
var DefaultWords = []string{
	"implement", "refactor", "explain", "function", "repository",
	"documentation", "performance", "integration", "tests",
}

// Fake is a deterministic, offline Suggester: when the cursor is at the end
// of text and the last word (2+ characters) is a proper prefix of a known
// word, it returns the rest of that word.
type Fake struct {
	// Words overrides DefaultWords.
	Words []string
	// Delay mimics model latency; the call honors ctx cancellation.
	Delay time.Duration
}

// Suggest implements Suggester.
func (f Fake) Suggest(ctx context.Context, text string, cursor int) (string, error) {
	if f.Delay > 0 {
		t := time.NewTimer(f.Delay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-t.C:
		}
	}
	if cursor != utf8.RuneCountInString(text) {
		return "", nil
	}
	word := text[strings.LastIndexFunc(text, unicode.IsSpace)+1:]
	if utf8.RuneCountInString(word) < 2 {
		return "", nil
	}
	words := f.Words
	if words == nil {
		words = DefaultWords
	}
	lower := strings.ToLower(word)
	for _, w := range words {
		if len(w) > len(lower) && strings.HasPrefix(w, lower) {
			return w[len(lower):], nil
		}
	}
	return "", nil
}
