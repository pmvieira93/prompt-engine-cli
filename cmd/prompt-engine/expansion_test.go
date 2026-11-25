package main

import (
	"testing"

	"github.com/pmvieira93/prompt-engine-cli/internal/config"
	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
	"github.com/pmvieira93/prompt-engine-cli/internal/history"
	"github.com/pmvieira93/prompt-engine-cli/internal/input"
)

func TestExpansionsSent(t *testing.T) {
	x := &expansions{}
	x.set(input.State{Text: "fix teh build"}, "Fix the build.", expand.StyleLLM)
	s := input.State{Text: "Fix the build.", Synced: true}
	if typed, _ := x.typed(s); typed != "fix teh build" {
		t.Fatalf("rules chosen for %q", typed)
	}
	sub := history.Submission{Raw: s.Text}
	handoff, typed := x.sent(&sub, s)
	if handoff || typed != "fix teh build" || sub.Raw != "fix teh build" || !sub.Expanded || sub.Style != expand.StyleLLM {
		t.Fatalf("rewrite: handoff %v, typed %q, sub %+v", handoff, typed, sub)
	}
	if x.isExpanded(s.Text) {
		t.Fatal("the rewrite is still kept once sent")
	}

	// The handoff prompt isn't a rewrite: the rules and the history take it
	// as typed, and the hints get what it replaced.
	x.set(input.State{Text: " add dark mode "}, "Write HANDOFF.md", "handoff")
	s = input.State{Text: "Write HANDOFF.md", Synced: true}
	if typed, _ := x.typed(s); typed != s.Text {
		t.Fatalf("rules chosen for %q", typed)
	}
	sub = history.Submission{Raw: s.Text}
	handoff, typed = x.sent(&sub, s)
	if !handoff || typed != "add dark mode" || sub.Expanded || sub.Raw != s.Text {
		t.Fatalf("handoff: handoff %v, typed %q, sub %+v", handoff, typed, sub)
	}
}

func TestExpansionsStyleForThisPrompt(t *testing.T) {
	cfg := config.Default()
	x := &expansions{}
	if got := x.styleFor(cfg); got != cfg.Expand.Style {
		t.Fatalf("no style chosen: %q", got)
	}
	if got := x.chosen("plain"); got != "plain" {
		t.Fatalf("no style chosen: named %q", got)
	}
	x.promptStyle = expand.StyleHuman
	if got := x.styleFor(cfg); got != expand.StyleHuman {
		t.Fatalf("chosen: %q", got)
	}
	if got := x.chosen("plain"); got != expand.StyleHuman+", for this prompt" {
		t.Fatalf("chosen: named %q", got)
	}
}
