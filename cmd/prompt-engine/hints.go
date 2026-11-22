package main

import (
	"os"
	"path/filepath"
	"strings"
)

// Hints on how Claude Code is used (PRD Appendix 5, FR-11.4, section 11).

// handoffStep is how much the context must grow after a handoff hint
// before it's shown again.
const handoffStep = 40_000

// handoffDue reports whether a handoff hint is due at a context of
// context tokens, given hints.handoff_at and the context when the hint was
// last shown (0 for not yet), and what to remember as last.
func handoffDue(context, at, last int64) (due bool, next int64) {
	switch {
	case at <= 0 || context < at:
		return false, 0 // below it, as after /clear: due again once it's reached
	case last == 0 || context >= last+handoffStep:
		return true, context
	}
	return false, last
}

// defaultHandoff is the handoff prompt when templates/handoff.txt is
// missing or empty.
const defaultHandoff = "Write HANDOFF.md for a fresh session to continue this work: the goal, what's done, " +
	"the decisions made and why, the files involved, what's left, and open questions. Keep it under a page."

// afterHandoff is offered as ghost text after /clear, once a handoff was
// sent. shortcut: it names HANDOFF.md whatever templates/handoff.txt asks
// for; make it a template too if someone renames the file.
const afterHandoff = "Read HANDOFF.md and continue from it."

// handoffPrompt is templates/handoff.txt in dir, or defaultHandoff.
func handoffPrompt(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "handoff.txt"))
	if text := strings.TrimSpace(string(data)); err == nil && text != "" {
		return text
	}
	return defaultHandoff
}
