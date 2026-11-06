package rules

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
)

// header is a line that starts a section: "[test|tests|coverage]".
var header = regexp.MustCompile(`^\[([^\[\]]*)\]$`)

// section is a part of the template added only to prompts with one of its
// words (PRD Appendix 6).
type section struct {
	names []string   // as written, in lower case; names[0] names the section
	words [][]string // each name as expand.Words: "bug-fix" is two in a row
	text  string
}

// template is append.txt: text for every prompt, then its sections.
type template struct {
	always   string
	sections []section
}

// parse reads a template's text. A file with no header is all always text.
// A header with a word that isn't one, a word in two sections or a section
// with no text is an error naming the line.
func parse(text string) (template, error) {
	var t template
	var seen []string
	var body []string
	headerAt := 0
	flush := func() error {
		joined := strings.TrimSpace(strings.Join(body, "\n"))
		body = nil
		if len(t.sections) == 0 {
			t.always = joined
			return nil
		}
		if joined == "" {
			return fmt.Errorf("line %d: [%s] has no text", headerAt, strings.Join(t.sections[len(t.sections)-1].names, "|"))
		}
		t.sections[len(t.sections)-1].text = joined
		return nil
	}
	for i, line := range strings.Split(text, "\n") {
		m := header.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			body = append(body, line)
			continue
		}
		if err := flush(); err != nil {
			return template{}, err
		}
		headerAt = i + 1
		var s section
		for _, w := range strings.Split(m[1], "|") {
			w = strings.ToLower(strings.TrimSpace(w))
			run := expand.Words(w)
			if !expand.IsPhrase(w) || strings.Contains(w, " ") || len(run) == 0 {
				return template{}, fmt.Errorf("line %d: %q isn't a word (letters, digits, -, _ and ')", headerAt, w)
			}
			if slices.Contains(seen, w) {
				return template{}, fmt.Errorf("line %d: %q is already used", headerAt, w)
			}
			seen = append(seen, w)
			s.names = append(s.names, w)
			s.words = append(s.words, run)
		}
		t.sections = append(t.sections, s)
	}
	if err := flush(); err != nil {
		return template{}, err
	}
	return t, nil
}

// empty reports whether the template adds nothing to any prompt.
func (t template) empty() bool {
	return t.always == "" && len(t.sections) == 0
}

// forPrompt returns the text to add to typed, which had pastes in it, and
// the names of the sections in it.
func (t template) forPrompt(typed string, pastes []string) (text string, names []string) {
	words := expand.TypedWords(typed, pastes)
	var parts []string
	if t.always != "" {
		parts = append(parts, t.always)
	}
	for _, s := range t.sections {
		if slices.ContainsFunc(s.words, func(run []string) bool { return hasRun(words, run) }) {
			parts = append(parts, s.text)
			names = append(names, s.names[0])
		}
	}
	return strings.Join(parts, "\n\n"), names
}

// hasRun reports whether words has run, in a row.
func hasRun(words, run []string) bool {
	for i := 0; i+len(run) <= len(words); i++ {
		if slices.Equal(words[i:i+len(run)], run) {
			return true
		}
	}
	return false
}
