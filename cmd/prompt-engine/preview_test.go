package main

import (
	"sync/atomic"
	"testing"

	"github.com/pmvieira93/prompt-engine-cli/internal/config"
	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
	"github.com/pmvieira93/prompt-engine-cli/internal/input"
)

func TestPreviewReady(t *testing.T) {
	var live atomic.Pointer[config.Config]
	cfg := config.Default()
	cfg.Expand.Style = expand.StyleLLM
	live.Store(&cfg)
	style := expand.StyleLLM
	p := &previews{live: &live, style: func(config.Config) string { return style },
		text: "fix teh build", out: "Fix the build.", outStyle: expand.StyleLLM, asked: expand.StyleLLM}
	typed := input.State{Text: "fix teh build", Synced: true}

	if out, st := p.ready(typed); out != "Fix the build." || st != expand.StyleLLM {
		t.Fatalf("ready: %q, %q", out, st)
	}
	for name, s := range map[string]input.State{
		"other text":     {Text: "fix teh build now", Synced: true},
		"text not known": {Text: "fix teh build"},
	} {
		if out, _ := p.ready(s); out != "" {
			t.Errorf("%s: got %q", name, out)
		}
	}
	style = expand.StyleHuman // keys.style, or a reload
	if out, _ := p.ready(typed); out != "" {
		t.Errorf("another style: got %q", out)
	}
	style, p.pending = expand.StyleLLM, true
	if out, _ := p.ready(typed); out != "" {
		t.Errorf("being prepared again: got %q", out)
	}
}
