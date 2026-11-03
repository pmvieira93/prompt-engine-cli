// Package guards reads guards.txt, the developer's own guard rails (PRD
// Appendix 4): one rule per line, in near-English.
//
//	never add: deploy, rollback, force push
//	always keep: staging
//	turn off: question
package guards

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
)

// Risky are the guards whose turning off gets a warning: in auto mode, a
// rewrite they would stop then goes out unchecked.
var Risky = []string{"destructive", "pastes", "names"}

// kinds are the rules a line can start with.
var kinds = []string{"never add", "always keep", "turn off"}

// Load reads the guards file at path. No file is not an error: it means no
// rules. On an error the rules are empty too, so the caller can warn and
// keep what it had.
func Load(path string) (expand.Rules, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return expand.Rules{}, nil
	}
	if err != nil {
		return expand.Rules{}, fmt.Errorf("read %s: %w", expand.GuardsFile, err)
	}
	r, err := Parse(string(data))
	if err != nil {
		return expand.Rules{}, fmt.Errorf("%s %w", path, err)
	}
	return r, nil
}

// Parse reads a guards file's text. A line that isn't a rule, a word that
// isn't one, or an unknown guard is an error naming the line.
func Parse(text string) (expand.Rules, error) {
	var r expand.Rules
	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		if j := strings.Index(line, "#"); j >= 0 {
			line = line[:j] // a note
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kind, rest, ok := strings.Cut(line, ":")
		kind = strings.Join(strings.Fields(strings.ToLower(kind)), " ")
		if !ok || !slices.Contains(kinds, kind) {
			return expand.Rules{}, fmt.Errorf("line %d: %q isn't a rule: start it with never add:, always keep: or turn off:", n, line)
		}
		var items []string
		for _, item := range strings.Split(rest, ",") {
			item = strings.Join(strings.Fields(strings.ToLower(item)), " ")
			if item == "" {
				continue
			}
			if !expand.IsPhrase(item) {
				return expand.Rules{}, fmt.Errorf("line %d: %q isn't a word or phrase (letters, digits, - and ')", n, item)
			}
			items = append(items, item)
		}
		if len(items) == 0 {
			return expand.Rules{}, fmt.Errorf("line %d: %s: names nothing", n, kind)
		}
		switch kind {
		case "never add":
			r.NeverAdd = append(r.NeverAdd, expand.Rule{Line: n, Words: items})
		case "always keep":
			r.AlwaysKeep = append(r.AlwaysKeep, expand.Rule{Line: n, Words: items})
		case "turn off":
			for _, g := range items {
				if !slices.Contains(expand.Guards, g) {
					return expand.Rules{}, fmt.Errorf("line %d: no guard %q (the guards: %s)", n, g, strings.Join(expand.Guards, ", "))
				}
				if r.Off == nil {
					r.Off = map[string]bool{}
				}
				r.Off[g] = true
			}
		}
	}
	return r, nil
}

// Notes describes the rules for the side panel's Session section and the
// log: a summary, and a warning for each guard in Risky turned off.
func Notes(r expand.Rules) (summary string, warnings []string) {
	count := func(rules []expand.Rule) int {
		n := 0
		for _, rule := range rules {
			n += len(rule.Words)
		}
		return n
	}
	var parts []string
	if n := count(r.NeverAdd); n > 0 {
		parts = append(parts, fmt.Sprintf("never add %d", n))
	}
	if n := count(r.AlwaysKeep); n > 0 {
		parts = append(parts, fmt.Sprintf("always keep %d", n))
	}
	var off []string
	for _, g := range expand.Guards {
		if r.Off[g] {
			off = append(off, g)
			if slices.Contains(Risky, g) {
				warnings = append(warnings, fmt.Sprintf("%s is off: in auto mode, a rewrite it would stop is sent unchecked", g))
			}
		}
	}
	if len(off) > 0 {
		parts = append(parts, "off: "+strings.Join(off, ", "))
	}
	if len(parts) == 0 {
		return "no rules", warnings
	}
	return strings.Join(parts, "; "), warnings
}
