package expand

import (
	"regexp"
	"strings"
)

// MaxRemoved is the share of the typed words a rewrite may lack, in any
// order, before Guard rejects it. Fixing typos and rewording lose some
// (good rewrites in testing lacked up to 40%); lacking most means the
// model misread the request. Order is ignored: "don't touch the tests,
// just fix the config" → "Just fix the config without changing the tests"
// is fine, but compared in order it looked like 89% removed.
const MaxRemoved = 0.6

var placeholder = regexp.MustCompile(`\[[^\]\n]{3,}\]`)

// nameChars mark a typed word as a name the rewrite must keep: an agent
// ("code-reviewer"), a file, a path, a mention, an issue number.
const nameChars = "-_/.@#!0123456789"

// LostQuotes returns the spans of typed in `backticks` or "double quotes"
// (commands, error messages, strings) that the rewrite doesn't contain
// exactly.
func LostQuotes(typed, rewritten string) []string {
	var lost []string
	for _, q := range quoted.FindAllString(StripContext(typed), -1) {
		if !strings.Contains(rewritten, q) {
			lost = append(lost, q)
		}
	}
	return lost
}

var quoted = regexp.MustCompile("`[^`\n]+`|\"[^\"\n]+\"")

// negations counts words that limit or reverse a request. A rewrite may
// reword them ("don't touch the tests" → "without changing the tests"),
// but must not have fewer.
func negations(text string) int {
	// In Portuguese "no" means "in the", not "no".
	noIsNegation := Language(text) != "Portuguese"
	n := 0
	for _, w := range words(strings.NewReplacer("n't", " not", "cannot", "can not").Replace(strings.ToLower(text))) {
		switch w {
		case "no":
			if noIsNegation {
				n++
			}
		case "not", "never", "without", "nothing", "only", "except", "avoid", "dont", "nao", "não", "sem", "nunca", "nada", "apenas", "só":
			n++
		}
	}
	return n
}

// destructive and actions are word stems: a rewrite must not add the
// former, or the latter to a question.
var (
	destructive = []string{"delet", "remov", "drop", "reset", "forc", "rm", "overwrit", "revert", "push", "wip", "truncat", "destroy", "purg", "eras", "discard", "uninstal"}
	actions     = []string{"fix", "chang", "updat", "add", "remov", "delet", "refactor", "implement", "handl", "writ", "creat", "renam", "replac", "modif", "mov", "run", "commit", "push", "instal", "build"}
)

// newWords returns the words of rewritten starting with one of stems that
// no word of typed starts with.
func newWords(typed, rewritten string, stems []string) []string {
	has := func(text, stem string) string {
		for _, w := range words(strings.ToLower(text)) {
			if strings.HasPrefix(w, stem) && (len(stem) > 2 || len(w) <= len(stem)+2) {
				return w
			}
		}
		return ""
	}
	var added []string
	for _, stem := range stems {
		if w := has(rewritten, stem); w != "" && has(typed, stem) == "" {
			added = append(added, w)
		}
	}
	return added
}

// fromEarlier returns words the rewrite took from earlier prompts when
// typed doesn't refer back to them. Seen in testing: with "don't touch the
// tests, just fix the config loader" as context, an unrelated "fix the
// error … in the client" came back with "Don't touch the tests or the
// config loader." added.
func fromEarlier(typed, rewritten string, earlier []string) []string {
	if len(earlier) == 0 {
		return nil
	}
	typedWords := map[string]bool{}
	for _, w := range words(strings.ToLower(typed)) {
		switch w {
		case "it", "its", "that", "this", "these", "those", "there", "them", "they", "same", "also", "again", "too", "isso", "isto", "mesmo", "também":
			return nil // refers back: using the context is the point
		}
		typedWords[w] = true
	}
	inEarlier := map[string]bool{}
	for _, e := range earlier {
		for _, w := range words(strings.ToLower(e)) {
			inEarlier[w] = true
		}
	}
	var leaked []string
	seen := map[string]bool{}
	for _, w := range words(strings.ToLower(StripContext(rewritten))) {
		if len(w) >= 4 && inEarlier[w] && !typedWords[w] && !seen[w] {
			leaked = append(leaked, w)
			seen[w] = true
		}
	}
	if len(leaked) < 2 { // one shared word is likely chance
		return nil
	}
	return leaked
}

// isQuestion reports whether typed asks something rather than asks for
// something to be done.
func isQuestion(typed string) bool {
	t := strings.TrimSpace(StripContext(typed))
	if strings.HasSuffix(t, "?") {
		return true
	}
	// The question may also end the prompt, without its "?": "the
	// error is … when I run make, can you fix it".
	last := t[strings.LastIndexAny(t, ".,;:\n")+1:]
	return startsQuestion(t) || startsQuestion(last)
}

func startsQuestion(text string) bool {
	for i, w := range words(strings.ToLower(text)) {
		if i > 1 { // "and what about…": the question word comes first or second
			break
		}
		switch w {
		case "why", "how", "what", "when", "where", "which", "who", "does", "do", "is", "are", "can", "could", "should", "would", "will", "explain":
			return true
		}
	}
	return false
}

// words splits text into words: letters, digits, apostrophes, hyphens.
func words(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return !(r == '\'' || r == '-' || r == '_' || ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') || r > 127)
	})
}

// LostNames returns the names in typed (words like "code-reviewer",
// "!110", "header.tsx") that the rewrite no longer contains. Seen in
// testing: "now using code-reviewer look into MR !110" became "Can you
// review the code in merge request !110?". Comparison ignores case,
// hyphens and underscores, so "e2e" → "E2E" and "multi-line" →
// "multiline" are fine, but "code-reviewer" → "code reviewer" isn't.
func LostNames(typed, rewritten string) []string {
	norm := func(s string) string {
		return strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(s))
	}
	have := norm(rewritten)
	var lost []string
	// Quoted spans are checked whole by LostQuotes. Splitting them into
	// words here misread "`…:11434`in" (no space after the quote) as a name.
	for _, w := range strings.Fields(quoted.ReplaceAllString(StripContext(typed), " ")) {
		w = strings.TrimRight(strings.TrimLeft(w, "\"'(["), "\"')],.;:?")
		if !strings.ContainsAny(w, nameChars) || strings.Trim(w, "0123456789") == "" {
			continue // an ordinary word, or a plain number ("3" may become "three")
		}
		if !strings.Contains(have, norm(w)) {
			lost = append(lost, w)
		}
	}
	return lost
}

// Guard checks a rewrite before it's sent without review (PRD Appendix 1,
// FR-7.4) and returns why it must not be used, or "" if it may. The
// checks come from bad rewrites seen in testing: "rfcator tracker"
// became "the RFCator tracker", a gap became "[insert specific technology
// here]", and without instructions the model asked for details instead.
// earlier are the prompts given to the model as session context.
func Guard(typed, rewritten string, earlier ...string) string {
	typed, rewritten = StripContext(typed), StripContext(rewritten)
	switch {
	case rewritten == "":
		return "the rewrite was empty"
	case placeholder.MatchString(rewritten) && !placeholder.MatchString(typed):
		return "the rewrite has a placeholder to fill in"
	case strings.HasSuffix(rewritten, "?") && !isQuestion(typed):
		return "the model asked a question instead of rewriting"
	case Language(typed) != Language(rewritten):
		return "the rewrite changed the language"
	}
	if lost := LostNames(typed, rewritten); len(lost) > 0 {
		return "the rewrite dropped or changed " + strings.Join(lost, ", ")
	}
	if lost := LostQuotes(typed, rewritten); len(lost) > 0 {
		return "the rewrite changed quoted text " + strings.Join(lost, ", ")
	}
	if negations(rewritten) != negations(typed) {
		return "the rewrite changed a negation (not, never, without, only…)"
	}
	if leaked := fromEarlier(typed, rewritten, earlier); len(leaked) > 0 {
		return "the rewrite added details from your earlier prompts (" + strings.Join(leaked, ", ") + ")"
	}
	if added := newWords(typed, rewritten, destructive); len(added) > 0 {
		return "the rewrite added a destructive action (" + strings.Join(added, ", ") + ")"
	}
	if isQuestion(typed) {
		if added := newWords(typed, rewritten, actions); len(added) > 0 {
			return "the rewrite turned your question into an instruction (" + strings.Join(added, ", ") + ")"
		}
	}
	if t, r := len(strings.Fields(typed)), len(strings.Fields(rewritten)); r > 3*t+15 {
		return "the rewrite is much longer than what you typed"
	}
	typedWords := words(strings.ToLower(typed))
	have := map[string]bool{}
	for _, w := range words(strings.ToLower(rewritten)) {
		have[w] = true
	}
	missing := 0
	for _, w := range typedWords {
		if !have[w] {
			missing++
		}
	}
	if len(typedWords) > 0 && float64(missing)/float64(len(typedWords)) > MaxRemoved {
		return "the rewrite replaced most of what you typed"
	}
	return ""
}
