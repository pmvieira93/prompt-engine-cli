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
	words []string // as expand.SectionWord; words[0] names the section
	text  string
}

// template is append.txt: text for every prompt, then its sections.
type template struct {
	always   string
	sections []section
}

// parse reads a template's text. A file with no header is all always text,
// and a header in a code block is the block's text. A header with a word
// that isn't one, a word in two sections or a section with no text is an
// error naming the line.
func parse(text string) (template, error) {
	var t template
	var seen []string
	var body []string
	headerAt := 0
	inCode := false
	flush := func() error {
		joined := strings.TrimSpace(strings.Join(body, "\n"))
		body = nil
		if len(t.sections) == 0 {
			t.always = joined
			return nil
		}
		if joined == "" {
			return fmt.Errorf("line %d: [%s] has no text", headerAt, strings.Join(t.sections[len(t.sections)-1].words, "|"))
		}
		t.sections[len(t.sections)-1].text = joined
		return nil
	}
	for i, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCode = !inCode
		}
		m := header.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil || inCode {
			body = append(body, line)
			continue
		}
		if err := flush(); err != nil {
			return template{}, err
		}
		headerAt = i + 1
		var s section
		for _, w := range strings.Split(m[1], "|") {
			word, ok := expand.SectionWord(w)
			if !ok {
				return template{}, fmt.Errorf("line %d: %q isn't a word (letters, digits, -, _ and ')", headerAt, strings.TrimSpace(w))
			}
			if slices.Contains(seen, word) {
				return template{}, fmt.Errorf("line %d: %q is already used", headerAt, word)
			}
			seen = append(seen, word)
			s.words = append(s.words, word)
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
	if len(t.sections) == 0 {
		return t.always, nil
	}
	words := expand.TypedWords(typed, pastes)
	var parts []string
	if t.always != "" {
		parts = append(parts, t.always)
	}
	for _, s := range t.sections {
		if slices.ContainsFunc(s.words, func(w string) bool { return slices.Contains(words, w) }) {
			parts = append(parts, s.text)
			names = append(names, s.words[0])
		}
	}
	return strings.Join(parts, "\n\n"), names
}
