package expand

import (
	"regexp"
	"strings"

	"github.com/pmvieira93/prompt-engine-cli/internal/diff"
)

// MaxRemoved is the share of the typed words a rewrite may drop before
// Guard rejects it. Fixing typos removes some ("teh" → "the"); dropping
// most of them means the model misread the request.
const MaxRemoved = 0.75

var placeholder = regexp.MustCompile(`\[[^\]\n]{3,}\]`)

// nameChars mark a typed word as a name the rewrite must keep: an agent
// ("code-reviewer"), a file, a path, a mention, an issue number.
const nameChars = "-_/.@#!0123456789"

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
	for _, w := range strings.Fields(StripContext(typed)) {
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
func Guard(typed, rewritten string) string {
	typed, rewritten = StripContext(typed), StripContext(rewritten)
	switch {
	case rewritten == "":
		return "the rewrite was empty"
	case placeholder.MatchString(rewritten) && !placeholder.MatchString(typed):
		return "the rewrite has a placeholder to fill in"
	case strings.HasSuffix(rewritten, "?") && !strings.HasSuffix(strings.TrimSpace(typed), "?"):
		return "the model asked a question instead of rewriting"
	}
	if lost := LostNames(typed, rewritten); len(lost) > 0 {
		return "the rewrite dropped or changed " + strings.Join(lost, ", ")
	}
	words := len(strings.Fields(typed))
	if _, removed := diff.Counts(diff.Words(strings.ToLower(typed), strings.ToLower(rewritten))); words > 0 &&
		float64(removed)/float64(words) > MaxRemoved {
		return "the rewrite replaced most of what you typed"
	}
	return ""
}
