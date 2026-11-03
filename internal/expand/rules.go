package expand

import (
	"fmt"
	"slices"
	"strings"
)

// GuardsFile, in the templates directory, holds the developer's own guard
// rails (PRD Appendix 4): one rule per line.
const GuardsFile = "guards.txt"

// Guards are the built-in guard rails, by the name a Stop gives them, in
// the order they are checked. "pastes" is checked when the kept parts are
// put back (Plan.Restore).
var Guards = []string{
	"empty", "placeholder", "asked-back", "language", "names", "quotes", "destructive",
	"question", "added-task", "dropped-part", "negation", "earlier-prompts", "length", "replaced", "pastes",
}

// Rules are the developer's guard rails from guards.txt. The zero value is
// the built-in guards alone.
type Rules struct {
	// Off are the built-in guards turned off, by name.
	Off map[string]bool
	// NeverAdd and AlwaysKeep are the "never add:" and "always keep:"
	// lines, in order.
	NeverAdd, AlwaysKeep []Rule
}

// A Rule is one line of guards.txt: words or phrases ("force push"), in
// lower case.
type Rule struct {
	Line  int
	Words []string
}

// on reports whether the built-in guard is turned on.
func (r *Rules) on(guard string) bool { return !r.Off[guard] }

// check stops a rewrite that adds a word a "never add:" line names, or
// drops one an "always keep:" line names.
func (r *Rules) check(typed, rewritten string) *Stop {
	t, w := words(strings.ToLower(typed)), words(strings.ToLower(rewritten))
	for _, rule := range r.NeverAdd {
		for _, p := range rule.Words {
			if hasPhrase(w, p) && !hasPhrase(t, p) {
				return &Stop{Guard: "never-add", Detail: p,
					Reason: fmt.Sprintf("the rewrite added %q (guards.txt line %d: never add)", p, rule.Line)}
			}
		}
	}
	for _, rule := range r.AlwaysKeep {
		for _, p := range rule.Words {
			if hasPhrase(t, p) && !hasPhrase(w, p) {
				return &Stop{Guard: "always-keep", Detail: "lost " + p,
					Reason: fmt.Sprintf("the rewrite dropped %q (guards.txt line %d: always keep)", p, rule.Line)}
			}
		}
	}
	return nil
}

// Instructions are the lines added to the model's instructions, so it
// keeps the rules to begin with; check catches it when it doesn't.
func (r *Rules) Instructions() string {
	join := func(rules []Rule) string {
		var all []string
		for _, rule := range rules {
			all = append(all, rule.Words...)
		}
		return strings.Join(all, ", ")
	}
	var b strings.Builder
	if len(r.NeverAdd) > 0 {
		b.WriteString("\nNever add these words unless the developer wrote them: " + join(r.NeverAdd) + ".")
	}
	if len(r.AlwaysKeep) > 0 {
		b.WriteString("\nAlways keep these words when the developer wrote them: " + join(r.AlwaysKeep) + ".")
	}
	return b.String()
}

// hasPhrase reports whether text, as words, has phrase: its words in a
// row, each as written or in one of its forms ("deploy" is also
// "deploys", "deployed", "deploying").
func hasPhrase(text []string, phrase string) bool {
	ps := words(phrase)
	if len(ps) == 0 {
		return false
	}
next:
	for i := 0; i+len(ps) <= len(text); i++ {
		for j, p := range ps {
			if !isForm(text[i+j], p) {
				continue next
			}
		}
		return true
	}
	return false
}

// isForm reports whether w is word, or word with -s, -es, -ed, -d or
// -ing, or another of its forms (verbForms): "deploys", "staged".
func isForm(w, word string) bool {
	for _, suffix := range []string{"", "s", "es", "ed", "d", "ing"} {
		if w == word+suffix {
			return true
		}
	}
	return slices.Contains(verbForms(word), w)
}

// IsPhrase reports whether p, in lower case, is words as the guards read
// them: letters, digits, apostrophes, hyphens and underscores, separated
// by single spaces.
func IsPhrase(p string) bool {
	return p != "" && strings.Join(words(p), " ") == p
}
