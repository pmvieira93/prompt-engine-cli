package expand

import (
	"fmt"
	"slices"
)

// Guards are the built-in guard rails, by the name a Stop gives them, in
// the order they are checked. "pastes" is checked when the kept parts are
// put back (Plan.Restore).
var Guards = []string{
	"empty", "placeholder", "asked-back", "language", "names", "quotes", "destructive",
	"question", "added-task", "dropped-part", "negation", "earlier-prompts", "length", "replaced", "pastes",
}

// Rules are the word lists, thresholds and switches the guard rails use
// (PRD Appendix 4): the built-in ones (DefaultRules), or those with a
// guards file's changes.
type Rules struct {
	// Destructive are word stems a rewrite must not add.
	Destructive []string
	// Actions are word stems a rewrite must not add to a question.
	Actions []string
	// Tasks are word stems a rewrite must not add to a report.
	Tasks []string
	// Negations are words that limit or reverse a request. "no" is one
	// too, except in Portuguese, where it means "in the".
	Negations []string
	// QuestionWords make a prompt a question when one comes first or
	// second.
	QuestionWords []string
	// PointsBack are words that point at something said before ("it",
	// "that"); PointsAtReply, ones that do when the model was given the
	// assistant's reply or the summary ("the second one", "yes").
	PointsBack, PointsAtReply []string
	// A rewrite of more than LengthFactor × the typed words +
	// LengthAllowance words is too long.
	LengthFactor    float64
	LengthAllowance int
	// MaxRemoved is the share of the typed words a rewrite may lack.
	MaxRemoved float64
	// Off are the built-in guards turned off, by name.
	Off map[string]bool
	// Custom are guards of one's own, checked after destructive.
	Custom []Custom
}

// A Custom guard stops a rewrite that adds one of Added, or drops one of
// Kept. Both are word stems: "prod" is also "production".
type Custom struct {
	Name        string
	Added, Kept []string
}

// DefaultRules returns the built-in rules, as a copy the caller may change.
func DefaultRules() Rules {
	actions := []string{"fix", "chang", "updat", "add", "remov", "delet", "refactor", "implement", "handl", "writ", "creat", "renam", "replac", "modif", "mov", "run", "commit", "push", "instal", "build", "show"}
	return Rules{
		Destructive:   []string{"delet", "remov", "drop", "reset", "forc", "rm", "overwrit", "revert", "push", "wip", "truncat", "destroy", "purg", "eras", "discard", "uninstal"},
		Actions:       actions,
		Tasks:         append([]string{"ensur", "verif", "investigat", "check", "explain", "debug", "resolv", "diagnos", "troubleshoot"}, actions...),
		Negations:     []string{"not", "never", "without", "nothing", "only", "except", "avoid", "dont", "nao", "não", "sem", "nunca", "nada", "apenas", "só"},
		QuestionWords: []string{"why", "how", "what", "when", "where", "which", "who", "does", "is", "are", "can", "could", "should", "would", "will", "explain"},
		PointsBack: []string{"it", "its", "that", "this", "these", "those", "there", "them", "they", "same", "also", "again", "too",
			"isso", "isto", "mesmo", "também"},
		PointsAtReply: []string{"one", "ones", "first", "second", "third", "last", "option", "above", "previous", "suggested", "both", "either",
			"yes", "yep", "sure", "ok", "okay", "ahead",
			"primeiro", "primeira", "segundo", "segunda", "opção", "sim"},
		LengthFactor:    3,
		LengthAllowance: 15,
		MaxRemoved:      MaxRemoved,
	}
}

var defaultRules = DefaultRules()

// on reports whether the built-in guard is turned on.
func (r *Rules) on(guard string) bool { return !r.Off[guard] }

// Weakened lists, for the panel and the log, the guards turned off or
// loosened compared with def: "question off", "replaced 0.6 → 0.7",
// "negation: 2 words removed".
func (r *Rules) Weakened(def Rules) []string {
	var out []string
	for _, g := range Guards {
		if r.Off[g] {
			out = append(out, g+" off")
		}
	}
	lists := []struct {
		guard     string
		got, want []string
	}{
		{"destructive", r.Destructive, def.Destructive},
		{"question", r.Actions, def.Actions},
		{"question", r.QuestionWords, def.QuestionWords},
		{"added-task", r.Tasks, def.Tasks},
		{"negation", r.Negations, def.Negations},
	}
	removed := map[string]int{}
	for _, l := range lists {
		if r.Off[l.guard] {
			continue
		}
		for _, w := range l.want {
			if !slices.Contains(l.got, w) {
				removed[l.guard]++
			}
		}
	}
	for _, g := range Guards {
		if n := removed[g]; n > 0 {
			out = append(out, fmt.Sprintf("%s: %d %s removed", g, n, plural(n, "word", "words")))
		}
	}
	if !r.Off["length"] && (r.LengthFactor > def.LengthFactor || r.LengthAllowance > def.LengthAllowance) {
		out = append(out, fmt.Sprintf("length %g×+%d → %g×+%d", def.LengthFactor, def.LengthAllowance, r.LengthFactor, r.LengthAllowance))
	}
	if !r.Off["replaced"] && r.MaxRemoved > def.MaxRemoved {
		out = append(out, fmt.Sprintf("replaced %g → %g", def.MaxRemoved, r.MaxRemoved))
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// IsWord reports whether w, in lower case, is one word as the guards read
// words: letters, digits, apostrophes, hyphens and underscores.
func IsWord(w string) bool {
	ws := words(w)
	return len(ws) == 1 && ws[0] == w
}
