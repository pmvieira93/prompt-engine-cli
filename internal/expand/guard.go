package expand

import (
	"fmt"
	"regexp"
	"slices"
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

// quotedName matches quoted and `code` text, curly quotes too: its
// negations are names, not instructions ("fix the \"only… not…\" guard").
var quotedName = regexp.MustCompile("\"[^\"\n]*\"|“[^”\n]*”|`[^`\n]*`")

// negations counts words that limit or reverse a request. A rewrite may
// reword them ("don't touch the tests" → "without changing the tests"),
// but must not have fewer.
func negations(text string) int {
	text = quotedName.ReplaceAllString(text, " ")
	// In Portuguese "no" means "in the", not "no".
	noIsNegation := Language(text) != "Portuguese"
	n := 0
	// "…" is not a letter: "only…" is "only".
	for _, w := range words(strings.NewReplacer("n't", " not", "cannot", "can not", "…", " ").Replace(strings.ToLower(text))) {
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

// contextNegations is how many negations the session context has, when
// typed refers back to it: a rewrite resolving "go with option 2" may
// take "only read at startup" from the reply, but never drops one typed.
func contextNegations(typed string, sc SessionContext) int {
	if !refersBack(typed, sc) {
		return 0
	}
	n := 0
	for _, e := range sc.Texts() {
		n += negations(e)
	}
	return n
}

// destructive and actions are word stems: a rewrite must not add the
// former, or the latter to a question.
var (
	destructive = []string{"delet", "remov", "drop", "reset", "forc", "rm", "overwrit", "revert", "push", "wip", "truncat", "destroy", "purg", "eras", "discard", "uninstal"}
	actions     = []string{"fix", "chang", "updat", "add", "remov", "delet", "refactor", "implement", "handl", "writ", "creat", "renam", "replac", "modif", "mov", "run", "commit", "push", "instal", "build"}
)

// tasks are word stems of what a rewrite must not ask for when typed is
// a report: actions, and asking for an investigation.
var tasks = append([]string{"ensur", "verif", "investigat", "check", "explain", "debug", "resolv", "diagnos", "troubleshoot"}, actions...)

// isStem reports whether word w starts with stem; a short stem ("rm",
// "mov") only for a short word.
func isStem(w, stem string) bool {
	return strings.HasPrefix(w, stem) && (len(stem) > 2 || len(w) <= len(stem)+2)
}

// newWords returns the words of rewritten starting with one of stems that
// no word of typed starts with.
func newWords(typed, rewritten string, stems []string) []string {
	has := func(text, stem string) string {
		for _, w := range words(strings.ToLower(text)) {
			if isStem(w, stem) {
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

// besidesTypos returns the words of ws that aren't a typed word with a
// typo fixed. Seen in preview mode: "shuold i comit the chnages now?"
// became "Should I commit the changes now?", and was stopped as a question
// turned into an instruction ("changes, commit").
func besidesTypos(typed string, ws []string) []string {
	typedWords := words(strings.ToLower(typed))
	var kept []string
next:
	for _, w := range ws {
		for _, t := range typedWords {
			if typoOf(t, w) {
				continue next
			}
		}
		kept = append(kept, w)
	}
	return kept
}

// typoOf reports whether t is w with one typo: a letter added, dropped or
// changed, or two letters swapped. A word under four letters counts only
// with a swap ("rnu"), since one changed letter makes another word ("and",
// "add").
func typoOf(t, w string) bool {
	a, b := []rune(t), []rune(w)
	if len(a) > len(b) {
		a, b = b, a
	}
	i := 0
	for i < len(a) && a[i] == b[i] {
		i++
	}
	switch {
	case i == len(b): // the same word
		return false
	case len(a) == len(b) && i+1 < len(a) && a[i] == b[i+1] && a[i+1] == b[i]:
		return string(a[i+2:]) == string(b[i+2:]) // swapped
	case len(a) < 4:
		return false
	case len(a) == len(b):
		return string(a[i+1:]) == string(b[i+1:]) // changed
	case len(a)+1 == len(b):
		return string(a[i:]) == string(b[i+1:]) // added or dropped
	}
	return false
}

// fromEarlier returns words the rewrite took from earlier prompts when
// typed doesn't refer back to them. Seen in testing: with "don't touch the
// tests, just fix the config loader" as context, an unrelated "fix the
// error … in the client" came back with "Don't touch the tests or the
// config loader." added.
func fromEarlier(typed, rewritten string, sc SessionContext) []string {
	earlier := sc.Texts()
	if len(earlier) == 0 || refersBack(typed, sc) {
		return nil // using the context is the point
	}
	typedWords := map[string]bool{}
	for _, w := range words(strings.ToLower(typed)) {
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
	// "shuold i comit the chnages now?", after "commit the changes…", took
	// nothing from it.
	leaked = besidesTypos(typed, leaked)
	if len(leaked) < 2 { // one shared word is likely chance
		return nil
	}
	return leaked
}

// refersBack reports whether typed points at something said before:
// "it", "that", "same", or, when the model was given the assistant's
// reply or the session summary, "the second one", "option 2" or "yes, go
// ahead", which point at a reply. Words a rewrite takes from the context
// are then what it was asked to resolve. In testing, the reply's words
// let a rewrite at L1 (earlier prompts only) resolve "the second one" to
// the previous prompt, wrongly.
func refersBack(typed string, sc SessionContext) bool {
	reply := sc.Reply != "" || sc.Summary != ""
	for _, w := range words(strings.ToLower(typed)) {
		switch w {
		case "it", "its", "that", "this", "these", "those", "there", "them", "they", "same", "also", "again", "too",
			"isso", "isto", "mesmo", "também":
			return true
		case "one", "ones", "first", "second", "third", "last", "option", "above", "previous", "suggested", "both", "either",
			"yes", "yep", "sure", "ok", "okay", "ahead",
			"primeiro", "primeira", "segundo", "segunda", "opção", "sim":
			if reply {
				return true
			}
		}
	}
	return false
}

// pointsAtReply reports whether typed refers back while the model was
// given the assistant's reply or the summary: "do the first one" is then
// meant to be replaced by what it points at.
func pointsAtReply(typed string, sc SessionContext) bool {
	return (sc.Reply != "" || sc.Summary != "") && refersBack(typed, sc)
}

// fromContext returns a filter keeping the words that aren't in the
// session context, compared by their first four letters ("split" and
// "splitting"), or nil when typed doesn't refer back to it. A three-letter
// word matches the words it starts: after "run the tests and fix any
// failures", "is it ready?" → "…are the tests running…" took "running"
// from the context, but was stopped for it.
func fromContext(typed string, sc SessionContext) func([]string) []string {
	earlier := sc.Texts()
	if len(earlier) == 0 || !refersBack(typed, sc) {
		return nil
	}
	key := func(w string) string {
		if r := []rune(w); len(r) > 4 {
			return string(r[:4])
		}
		return w
	}
	seen := map[string]bool{}
	var short []string // under four letters: "run" is "running"'s key
	for _, e := range earlier {
		for _, w := range words(strings.ToLower(e)) {
			seen[key(w)] = true
			if n := len([]rune(w)); n == 3 {
				short = append(short, w)
			}
		}
	}
	return func(ws []string) []string {
		var kept []string
	next:
		for _, w := range ws {
			if seen[key(w)] {
				continue
			}
			for _, s := range short {
				if isStem(w, s) {
					continue next
				}
			}
			kept = append(kept, w)
		}
		return kept
	}
}

// addedTasks returns the tasks a rewrite asks for when typed asks for
// none of them: it reports something ("it still fails, same error
// message"), or asks with a verb not in tasks ("tidy the branch"). Seen
// in testing: "it still fais" became "Fix the error in the sentence: it
// still fails.", and a list of "same …" became "Ensure the "hello" message
// is displayed…", from an earlier prompt. A typed word with the same first
// three letters counts as asking, so a typo ("refactr") doesn't make the
// fixed word ("Refactor") new.
func addedTasks(typed, rewritten string) []string {
	typedWords := words(strings.ToLower(typed))
	for _, w := range typedWords {
		for _, stem := range tasks {
			if isStem(w, stem) {
				return nil // a request, not a report
			}
		}
	}
	var added []string
next:
	for _, w := range words(strings.ToLower(rewritten)) {
		for _, stem := range tasks {
			if !isStem(w, stem) {
				continue
			}
			for _, t := range typedWords {
				if len(t) >= 3 && strings.HasPrefix(w, t[:3]) {
					continue next
				}
			}
			if !slices.Contains(added, w) {
				added = append(added, w)
			}
			continue next
		}
	}
	return added
}

// droppedPart returns, shortened, a sentence or line of typed with at
// least three content words none of which the rewrite has, or "". Seen in
// testing: "…keep the answer short; this promtp is just long enough to
// wrap onto a second row" lost its second sentence, and only "only", when
// it was there, got the rewrite stopped. Words are compared by their first
// four letters, so a fixed typo ("promtp" → "prompt") still counts.
func droppedPart(typed, rewritten string) string {
	key := func(w string) string {
		if r := []rune(w); len(r) > 4 {
			return string(r[:4])
		}
		return w
	}
	have := map[string]bool{}
	for _, w := range words(strings.ToLower(rewritten)) {
		have[key(w)] = true
	}
	for _, part := range sentence.Split(typed, -1) {
		content, kept := 0, false
		for _, w := range words(strings.ToLower(part)) {
			if len(w) < 3 || stopWords[w] {
				continue
			}
			content++
			kept = kept || have[key(w)]
		}
		if content >= 3 && !kept {
			return shortPart(strings.TrimSpace(part))
		}
	}
	return ""
}

// sentence splits a prompt into its sentences and lines.
var sentence = regexp.MustCompile(`\n+|[.;:?!]+(\s+|$)`)

// stopWords don't show whether a part of a prompt was kept.
var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "this": true, "that": true, "these": true, "those": true,
	"are": true, "was": true, "were": true, "you": true, "your": true, "can": true, "but": true, "from": true,
	"into": true, "onto": true, "just": true, "also": true, "then": true, "them": true, "they": true, "have": true,
	"has": true, "its": true, "our": true, "out": true, "all": true, "any": true, "please": true, "pls": true,
}

// shortPart cuts a part to about 40 characters for a reason.
func shortPart(s string) string {
	if r := []rune(s); len(r) > 40 {
		return strings.TrimSpace(string(r[:40])) + "…"
	}
	return s
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
		case "why", "how", "what", "when", "where", "which", "who", "does", "is", "are", "can", "could", "should", "would", "will", "explain":
			return true
		case "do": // "do you…", but not "do the first one"
			if ws := words(strings.ToLower(text)); i+1 < len(ws) {
				switch ws[i+1] {
				case "you", "i", "we", "they":
					return true
				}
			}
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

// A Stop is why a rewrite isn't used: the guard rail that stopped it, by
// a short name for the panel and the log, and the reason.
type Stop struct {
	Guard string // e.g. "negation"
	// Detail is what tripped it, in a few words, for the panel: "ensure,
	// check" for added-task, "negations 0 → 1". "" when the name says it
	// all.
	Detail string
	Reason string // e.g. "the rewrite changed a negation (not, never, without, only…)"
}

// Guard checks a rewrite before it's sent without review (PRD Appendix 1,
// FR-7.4) and returns why it must not be used, or "" if it may.
func Guard(typed, rewritten string, earlier ...string) string {
	if stop := Check(typed, rewritten, earlier...); stop != nil {
		return stop.Reason
	}
	return ""
}

// Check is Guard with the guard rail's name: nil if the rewrite may be
// used. The checks come from bad rewrites seen in testing: "rfcator
// tracker" became "the RFCator tracker", a gap became "[insert specific
// technology here]", and without instructions the model asked for details
// instead. earlier are the prompts given to the model as session context.
// A rewrite asking for something else (a question or a report turned into
// a task) is checked before the negations, since such a rewrite often adds one
// too ("Ensure the code runs without errors"), and the change of kind is
// the clearer reason.
func Check(typed, rewritten string, earlier ...string) *Stop {
	return CheckSession(typed, rewritten, SessionContext{Prompts: earlier})
}

// CheckSession is Check with all the session context the model was given
// (PRD Appendix 1, FR-6).
func CheckSession(typed, rewritten string, sc SessionContext) *Stop {
	typed, rewritten = StripContext(typed), StripContext(rewritten)
	stop := func(guard, detail, reason string) *Stop { return &Stop{Guard: guard, Detail: detail, Reason: reason} }
	list := strings.Join
	// When typed points back ("do the second one"), what the rewrite takes
	// from the session context isn't invented (PRD Appendix 1, FR-6.3):
	// only destructive actions stay stopped wherever they come from.
	resolved := fromContext(typed, sc)
	if resolved == nil {
		resolved = func(ws []string) []string { return ws }
	}
	switch {
	case rewritten == "":
		return stop("empty", "", "the rewrite was empty")
	case placeholder.MatchString(rewritten) && !placeholder.MatchString(typed):
		return stop("placeholder", placeholder.FindString(rewritten), "the rewrite has a placeholder to fill in")
	case strings.HasSuffix(rewritten, "?") && !isQuestion(typed):
		return stop("asked-back", "", "the model asked a question instead of rewriting")
	case Language(typed) != Language(rewritten):
		return stop("language", languageName(typed)+" → "+languageName(rewritten), "the rewrite changed the language")
	}
	if lost := LostNames(typed, rewritten); len(lost) > 0 {
		return stop("names", "lost "+list(lost, ", "), "the rewrite dropped or changed "+list(lost, ", "))
	}
	if lost := LostQuotes(typed, rewritten); len(lost) > 0 {
		return stop("quotes", "changed "+list(lost, ", "), "the rewrite changed quoted text "+list(lost, ", "))
	}
	if added := newWords(typed, rewritten, destructive); len(added) > 0 {
		return stop("destructive", list(added, ", "), "the rewrite added a destructive action ("+list(added, ", ")+")")
	}
	if isQuestion(typed) {
		if added := resolved(besidesTypos(typed, newWords(typed, rewritten, actions))); len(added) > 0 {
			return stop("question", list(added, ", "), "the rewrite turned your question into an instruction ("+list(added, ", ")+")")
		}
	} else if added := resolved(addedTasks(typed, rewritten)); len(added) > 0 {
		return stop("added-task", list(added, ", "), "the rewrite added a task you didn't ask for ("+list(added, ", ")+")")
	}
	if part := droppedPart(typed, rewritten); part != "" {
		return stop("dropped-part", "“"+part+"”", "the rewrite dropped part of what you typed (“"+part+"”)")
	}
	if n, m := negations(typed), negations(rewritten); n != m && !(m > n && m-n <= contextNegations(typed, sc)) {
		return stop("negation", fmt.Sprintf("negations %d → %d", n, m), "the rewrite changed a negation (not, never, without, only…)")
	}
	if leaked := fromEarlier(typed, rewritten, sc); len(leaked) > 0 {
		return stop("earlier-prompts", "from earlier: "+list(leaked, ", "), "the rewrite added details from your earlier prompts ("+list(leaked, ", ")+")")
	}
	if t, r := len(strings.Fields(typed)), len(strings.Fields(rewritten)); r > 3*t+15 {
		return stop("length", fmt.Sprintf("%d → %d words", t, r), "the rewrite is much longer than what you typed")
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
	if len(typedWords) > 0 && float64(missing)/float64(len(typedWords)) > MaxRemoved && !pointsAtReply(typed, sc) {
		return stop("replaced", fmt.Sprintf("%d of %d words gone", missing, len(typedWords)), "the rewrite replaced most of what you typed")
	}
	return nil
}

// languageName is Language, with "English" for "".
func languageName(text string) string {
	if l := Language(text); l != "" {
		return l
	}
	return "English"
}
