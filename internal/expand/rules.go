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
// put back (Plan.Restore). All but "empty" can be turned off.
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
	t, w := ruleWords(typed), ruleWords(rewritten)
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

// CheckShortcut is what a rewrite asked for with the shortcut (keys.expand)
// must pass before it replaces the prompt: the names and quotes guards,
// unless turned off, and the developer's rules. The rest are left to the
// developer, who sees the rewrite before sending it. A nil r is no rules.
func (r *Rules) CheckShortcut(typed, rewritten string) *Stop {
	if r == nil {
		r = &Rules{}
	}
	typed, rewritten = StripContext(typed), StripContext(rewritten)
	if lost := LostNames(typed, rewritten); len(lost) > 0 && r.on("names") {
		return &Stop{Guard: "names", Detail: "lost " + strings.Join(lost, ", "), Reason: "the rewrite changed " + strings.Join(lost, ", ")}
	}
	if lost := LostQuotes(typed, rewritten); len(lost) > 0 && r.on("quotes") {
		return &Stop{Guard: "quotes", Detail: "changed " + strings.Join(lost, ", "), Reason: "the rewrite changed " + strings.Join(lost, ", ")}
	}
	return r.check(typed, rewritten)
}

// Instructions are the lines added to the model's instructions for typed,
// so it keeps the rules to begin with; check catches it when it doesn't.
// Only the words that matter for typed are named: those it must not add
// (not typed), and those it must keep (typed). Naming a typed word as one
// never to add would invite the model to drop it.
func (r *Rules) Instructions(typed string) string {
	t := ruleWords(StripContext(typed))
	pick := func(rules []Rule, typed bool) string {
		var all []string
		for _, rule := range rules {
			for _, p := range rule.Words {
				if hasPhrase(t, p) == typed && !slices.Contains(all, p) {
					all = append(all, p)
				}
			}
		}
		return strings.Join(all, ", ")
	}
	var b strings.Builder
	if never := pick(r.NeverAdd, false); never != "" {
		b.WriteString("\nNever add these words: " + never + ".")
	}
	if keep := pick(r.AlwaysKeep, true); keep != "" {
		b.WriteString("\nKeep these words exactly: " + keep + ".")
	}
	return b.String()
}

// ruleWords splits text into the words rules are matched against: in
// lower case, without quotes or apostrophes around them or a final "'s",
// and hyphenated words split ("production-like" has "production").
func ruleWords(text string) []string {
	text = strings.NewReplacer("’", "'", "‘", "'").Replace(strings.ToLower(text))
	var out []string
	for _, w := range words(text) {
		for _, part := range strings.Split(w, "-") {
			part = strings.Trim(part, "'_")
			part = strings.TrimSuffix(part, "'s")
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// hasPhrase reports whether text, as ruleWords, has phrase: its words in
// a row, the last as written or in one of its forms ("deploy" is also
// "deploys", "deployed", "deploying"; "force push" also "force pushed").
func hasPhrase(text []string, phrase string) bool {
	ps := ruleWords(phrase)
	if len(ps) == 0 {
		return false
	}
next:
	for i := 0; i+len(ps) <= len(text); i++ {
		for j, p := range ps {
			if w := text[i+j]; w != p && (j < len(ps)-1 || !isForm(w, p)) {
				continue next
			}
		}
		return true
	}
	return false
}

// isForm reports whether w is word with -s, -es, -ed, -d or -ing, with
// the last letter doubled ("committed", "shipping"), or another of its
// forms (verbForms).
func isForm(w, word string) bool {
	last := word[len(word)-1:]
	for _, suffix := range []string{"s", "es", "ed", "d", "ing", last + "ed", last + "ing"} {
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
