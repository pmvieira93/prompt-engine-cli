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
// missing or empty; {transcript} becomes Claude Code's transcript file, in
// either. The structure is the developer's (PR #42).
const defaultHandoff = `Write HANDOFF.md so a fresh session can continue this work, with this structure:

# Handoff: [short and descriptive title]

**Date:** [current date]
**Status:** [in progress / blocked / awaiting decision]

## 1. Objective
What is being done and why, in 2–4 sentences.

## 2. Essential Context
Tech stack, constraints, decisions already made and their rationale, and the worktree if one was used to work in parallel.

## 3. What Has Been Done
Chronological list of what has been produced, attempted, or discarded. If something was discarded, explain why.

## 4. Current State
Where exactly we left off. What works, what is broken.

## 5. Next Steps
Ordered, actionable list of what to do next. Be specific.

## 6. Open Questions
Things that still require a decision or investigation.

## 7. Relevant Artifacts
Files, snippets, links, or commands that the next session will need.

## 8. Instructions for the Next Session
Expected tone, level of detail, pitfalls to avoid.

---
*Original session: {transcript}. Indicative only: consult it only if the user asks.*`

// afterHandoff is offered as ghost text after /clear, once a handoff was
// sent. shortcut: it names HANDOFF.md whatever templates/handoff.txt asks
// for; make it a template too if someone renames the file.
const afterHandoff = "Read HANDOFF.md and continue from it."

// handoffPrompt is templates/handoff.txt in dir, or defaultHandoff, with
// {transcript} replaced by transcript ("unknown" when none was found).
func handoffPrompt(dir, transcript string) string {
	text := defaultHandoff
	if data, err := os.ReadFile(filepath.Join(dir, "handoff.txt")); err == nil && strings.TrimSpace(string(data)) != "" {
		text = strings.TrimSpace(string(data))
	}
	if transcript == "" {
		transcript = "unknown"
	}
	return strings.ReplaceAll(text, "{transcript}", transcript)
}
