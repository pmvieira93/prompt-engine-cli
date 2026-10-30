package expand

import "strings"

// Common short words per language, for telling a prompt's language apart.
// Words shared with English ("a", "no") are left out. Short questions
// need everyday words too: "posso fazer commit das alterações agora?" had
// only "das", so the model wasn't told its language and answered in
// English ("Can I commit the changes now?").
var stopwords = map[string][]string{
	"English":    {"the", "to", "of", "and", "in", "for", "with", "on", "is", "it", "this", "that", "how", "what", "why", "from", "by", "an"},
	"Portuguese": {"o", "os", "as", "um", "uma", "do", "da", "dos", "das", "na", "nos", "nas", "em", "para", "com", "não", "é", "ao", "à", "que", "de", "como", "e", "por", "isso", "este", "esta", "ficheiro", "arquivo", "está", "estão", "já", "posso", "podes", "pode", "consegues", "fazer", "agora", "tudo", "também", "ainda", "porque", "alterações", "mudanças"},
	"Spanish":    {"el", "la", "los", "las", "un", "una", "del", "al", "en", "con", "para", "que", "de", "como", "es", "y", "por", "lo", "esto", "esta", "está", "porque"},
	"French":     {"le", "les", "un", "une", "des", "du", "et", "est", "pour", "avec", "dans", "que", "ce", "cette", "sur", "pas", "comment"},
	"German":     {"der", "die", "das", "und", "ist", "mit", "für", "auf", "nicht", "ein", "eine", "den", "dem", "wie", "zu"},
	"Italian":    {"il", "lo", "gli", "di", "del", "della", "che", "con", "per", "una", "è", "non", "come", "nel", "alla"},
}

// Language guesses the language text is written in from its common short
// words, returning "" for English or when unsure. The expansion model is
// told the language only when it isn't English: in testing, it translated
// Portuguese and Spanish prompts to English unless told, and translated
// English ones when the instructions named another language.
func Language(text string) string {
	counts := map[string]int{}
	for _, w := range strings.Fields(strings.ToLower(StripContext(text))) {
		w = strings.Trim(w, ".,;:!?\"'()[]")
		for lang, words := range stopwords {
			for _, s := range words {
				if w == s {
					counts[lang]++
				}
			}
		}
	}
	best, bestCount := "", 0
	for lang, n := range counts {
		if lang != "English" && (n > bestCount || (n == bestCount && lang < best)) {
			best, bestCount = lang, n
		}
	}
	if bestCount < 2 || bestCount <= counts["English"] {
		return ""
	}
	return best
}
