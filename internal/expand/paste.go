package expand

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Rewriting around pastes (PRD Appendix 1, FR-7.4e): what the developer
// typed is rewritten, and what they pasted is kept exactly, as it's
// something shown to the CLI (code, a log, a quote). A prompt that is only
// a paste was written elsewhere, so it is rewritten too, except its code
// blocks. Kept text reaches the model as markers, which the 7B model
// copied exactly and in place in every test; square-bracket markers got
// moved.

// MaxProseWords is the most text a prompt may have, besides kept parts,
// to be rewritten: a 92-word prompt took 5.3 s with the 7B model.
const MaxProseWords = 300

// MaxExpandWords is the most text that is reworded rather than only
// corrected: the instructions ask for under 60 words, so a longer prompt
// would be cut short.
const MaxExpandWords = 60

// Marker stands for the nth kept part (from 1) in the text the model sees.
func Marker(n int) string { return fmt.Sprintf("<<PASTE_%d>>", n) }

var markerPattern = regexp.MustCompile(`<<PASTE_\d+>>`)

// Plan is how a prompt is rewritten: the text the model sees, with each
// kept part replaced by its Marker.
type Plan struct {
	Masked string
	Kept   []string // Kept[i] is behind Marker(i+1)
	// TypoOnly asks for corrections, not rewording: for text typed
	// around a paste, which is already worded around it, and for long
	// prompts.
	TypoOnly bool
	// Words counts the words the model rewrites, markers excluded.
	Words int
	// Skip says why the prompt is sent as is; "" when it can be rewritten.
	Skip string
}

var (
	codeBlock = regexp.MustCompile("(?s)```.*?(```|$)")
	// claude shows a long paste as "[Pasted text #1 +5 lines]"; if that's
	// what the screen gave, the pasted text itself is unknown.
	claudePlaceholder = regexp.MustCompile(`\[Pasted text #\d+`)
)

// PlanRewrite splits text into what is rewritten and what is kept, given
// what was pasted into it (input.State.Pastes).
func PlanRewrite(text string, pastes []string) Plan {
	text = StripContext(text)
	if claudePlaceholder.MatchString(text) {
		return Plan{Skip: "the prompt shows a [Pasted text] placeholder, so its text is unknown"}
	}
	if markerPattern.MatchString(text) {
		return Plan{Skip: "the prompt contains a " + Marker(1) + "-style marker"}
	}

	var spans [][2]int
	for _, p := range pastes {
		// Surrounding blank lines may have been trimmed off text.
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		// A paste may have been edited since: then it's no longer found
		// and counts as typed.
		from := 0
		for {
			i := strings.Index(text[from:], p)
			if i < 0 {
				break
			}
			start, end := from+i, from+i+len(p)
			if !overlaps(spans, start, end) {
				spans = append(spans, [2]int{start, end})
				break
			}
			from = start + 1
		}
	}
	pasted := len(spans) > 0
	typed := words(strings.ToLower(remove(text, spans)))
	if pasted && len(typed) == 0 {
		// Only a paste: it's the prompt itself, unless it's data.
		spans = nil
	}
	if pasted && len(typed) > 0 && len(strings.Fields(remove(text, spans))) < 2 {
		return Plan{Skip: "too little typed around the paste"}
	}
	for _, m := range codeBlock.FindAllStringIndex(text, -1) {
		if !overlaps(spans, m[0], m[1]) {
			spans = append(spans, [2]int{m[0], m[1]})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })

	var p Plan
	var b strings.Builder
	last := 0
	for _, s := range spans {
		b.WriteString(text[last:s[0]])
		p.Kept = append(p.Kept, text[s[0]:s[1]])
		b.WriteString(Marker(len(p.Kept)))
		last = s[1]
	}
	b.WriteString(text[last:])
	p.Masked = b.String()
	prose := markerPattern.ReplaceAllString(p.Masked, " ")
	p.Words = len(words(strings.ToLower(prose)))
	switch {
	case p.Words == 0:
		p.Skip = "nothing but pasted text or code to rewrite"
	case pasted && len(typed) == 0 && looksLikeData(prose):
		p.Skip = "the paste looks like a log, trace, JSON or diff"
	case p.Words > MaxProseWords:
		p.Skip = fmt.Sprintf("over %d words", MaxProseWords)
	}
	p.TypoOnly = (pasted && len(typed) > 0) || p.Words > MaxExpandWords
	return p
}

// Restore puts the kept parts back into a rewrite of p.Masked. Each
// marker must be there once, in order; otherwise the parts can't be put
// back exactly.
func (p Plan) Restore(rewritten string) (string, error) {
	found := markerPattern.FindAllString(rewritten, -1)
	if len(found) != len(p.Kept) {
		return "", errors.New("the rewrite dropped or repeated a pasted part")
	}
	for i, m := range found {
		if m != Marker(i+1) {
			return "", errors.New("the rewrite moved a pasted part")
		}
	}
	for i, k := range p.Kept {
		rewritten = strings.Replace(rewritten, Marker(i+1), k, 1)
	}
	return rewritten, nil
}

// ForContext is text as given to the model as an earlier prompt, with
// pasted parts left out: given in full, the model put an earlier pasted
// trace in place of a new paste's marker.
func ForContext(text string, pastes []string) string {
	p := PlanRewrite(text, pastes)
	switch {
	case p.Skip == "" || p.Masked != "" && len(pastes) == 0:
		return strings.TrimSpace(markerPattern.ReplaceAllString(p.Masked, "(pasted text)"))
	case len(pastes) > 0:
		return "(pasted text)" // a pasted log, or little typed around it
	default:
		return StripContext(text)
	}
}

func overlaps(spans [][2]int, start, end int) bool {
	for _, s := range spans {
		if start < s[1] && s[0] < end {
			return true
		}
	}
	return false
}

// remove returns text without the spans, which must not overlap.
func remove(text string, spans [][2]int) string {
	sorted := append([][2]int(nil), spans...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i][0] < sorted[j][0] })
	var b strings.Builder
	last := 0
	for _, s := range sorted {
		b.WriteString(text[last:s[0]])
		b.WriteString(" ")
		last = s[1]
	}
	b.WriteString(text[last:])
	return b.String()
}

// dataLine matches lines of logs, stack traces and diffs.
var dataLine = regexp.MustCompile(`^(\s+at |\s*File "|Traceback|goroutine \d+|panic:|\s*\S+\.\w+:\d+|\d{4}-\d\d-\d\d|\d\d:\d\d:\d\d|\[?(DEBUG|INFO|WARN|WARNING|ERROR|FATAL)\b|[+-]{3} |@@ |[+-][^\s+-]|[{}\[\]]|"\w+":|\$ |> )`)

// looksLikeData reports whether pasted text is a log, a stack trace, JSON
// or a diff rather than a request: sent as is, since "fixing" it would
// change what the CLI is shown.
func looksLikeData(text string) bool {
	t := strings.TrimSpace(text)
	if json.Valid([]byte(t)) {
		return true
	}
	var lines, data int
	for _, l := range strings.Split(t, "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		lines++
		if dataLine.MatchString(l) {
			data++
		}
	}
	// Unsure counts as data: a request rarely has such lines.
	return lines > 0 && data*10 >= lines*3
}
