package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
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

// newTaskPause is how long typing pauses before a prompt is checked for a
// new task.
const newTaskPause = time.Second

// embedModel gives the vectors the new-task classifier was trained on.
const embedModel = "nomic-embed-text"

// backRef is a word that refers to the conversation so far: the routing
// spike's BACKREF, which ignores case.
var backRef = regexp.MustCompile(`(?i)\b(it|that|this|those|these|them|also|again|same|above|previous|continue|go ahead|yes|ok|` +
	`okay|sure|merge|commit|push|reply|fix (all|them|it))\b`)

// newTaskWeights are a logistic regression on [cosine with the previous
// prompt, first prompt, backRef, log(1+words)/5] scaled to unit length,
// then the bias; trained on the developer's 60 hand labels (routing spike,
// docs/spikes/routing.md: 90%, never "new" for a continuing prompt).
var newTaskWeights = [5]float64{-6.0313, 3.6789, -0.2616, 5.0222, 0.5849}

// newTaskThreshold is the score above which a prompt starts a new task.
// Cross-validated on the hand labels: at 0, 9 of 14 new tasks and no
// follow-up; at -0.5, 10 of 14 and still none; at -0.75, 11 but 2 of 46
// follow-ups too (2026-11-23).
const newTaskThreshold = -0.5

// newTaskScore is how much a prompt looks like the start of a new task,
// given the cosine between its vector and the previous prompt's: above
// newTaskThreshold, it is one.
func newTaskScore(text string, cosine float64) float64 {
	x := []float64{cosine, 0, 0, math.Log(1+float64(len(strings.Fields(text)))) / 5}
	if backRef.MatchString(text) {
		x[2] = 1
	}
	var norm float64
	for _, v := range x {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		norm = 1
	}
	score := newTaskWeights[4]
	for i, v := range x {
		score += newTaskWeights[i] * v / norm
	}
	return score
}

func cosine(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range min(len(a), len(b)) {
		dot, na, nb = dot+a[i]*b[i], na+a[i]*a[i], nb+b[i]*b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

// embed returns text's vector from Ollama at endpoint, as the classifier
// was trained: with nomic-embed-text's task prefix, the first 2,000
// characters.
func embed(ctx context.Context, endpoint, text string) ([]float64, error) {
	if r := []rune(text); len(r) > 2000 {
		text = string(r[:2000])
	}
	body, _ := json.Marshal(map[string]string{"model": embedModel, "input": "classification: " + text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Embeddings [][]float64 `json:"embeddings"`
		Error      string      `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%s: %w", resp.Status, err)
	}
	switch {
	case out.Error != "":
		return nil, errors.New(out.Error)
	case len(out.Embeddings) == 0:
		return nil, fmt.Errorf("%s: no embedding", resp.Status)
	}
	return out.Embeddings[0], nil
}

// fakeEmbed stands in for embed in tests: a vector of word counts, so
// prompts sharing no word are unrelated.
func fakeEmbed(_ context.Context, _, text string) ([]float64, error) {
	v := make([]float64, 64)
	for _, w := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		h.Write([]byte(w))
		v[h.Sum32()%64]++
	}
	return v, nil
}

// retry calls try up to n times, 250ms apart, until it returns true, and
// reports whether it did.
func retry(n int, try func() bool) bool {
	for range n {
		if try() {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}
