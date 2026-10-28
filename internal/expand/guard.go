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
	words := len(strings.Fields(typed))
	if _, removed := diff.Counts(diff.Words(strings.ToLower(typed), strings.ToLower(rewritten))); words > 0 &&
		float64(removed)/float64(words) > MaxRemoved {
		return "the rewrite replaced most of what you typed"
	}
	return ""
}
