package expand

import (
	"strings"
	"testing"
)

func TestPlanRewrite(t *testing.T) {
	code := "what does this print? just answer\n```go\nfmt.Println(len(\"héllo\"))\n```\n"
	trace := "panic: runtime error: index out of range\n\ngoroutine 1 [running]:\nmain.main()\n\t/src/main.go:12 +0x1d\nexit status 2"
	cases := []struct {
		name     string
		text     string
		pastes   []string
		masked   string
		kept     string // joined with |
		typoOnly bool
		skip     string // a substring of Plan.Skip
	}{
		{name: "only typed", text: "fix the config loadr",
			masked: "fix the config loadr"},
		{name: "typed around a paste", text: "waht does thsi do: x := f(y)", pastes: []string{"x := f(y)"},
			masked: "waht does thsi do: <<PASTE_1>>", kept: "x := f(y)", typoOnly: true},
		{name: "a paste ending the prompt with a newline", text: "explain this:\nfoo()\n", pastes: []string{"foo()\n"},
			masked: "explain this:\n<<PASTE_1>>", kept: "foo()", typoOnly: true},
		{name: "two pastes", text: "compare a.go with b.go and tell me wich is fastr", pastes: []string{"a.go", "b.go"},
			masked: "compare <<PASTE_1>> with <<PASTE_2>> and tell me wich is fastr", kept: "a.go|b.go", typoOnly: true},
		{name: "only a paste is the prompt, except its code", text: code, pastes: []string{code},
			masked: "what does this print? just answer\n<<PASTE_1>>", kept: "```go\nfmt.Println(len(\"héllo\"))\n```"},
		{name: "typed code block is kept", text: "why is ```x++``` slow", masked: "why is <<PASTE_1>> slow", kept: "```x++```"},
		{name: "unclosed code block runs to the end", text: "fix this ```go\nx := 1", masked: "fix this <<PASTE_1>>", kept: "```go\nx := 1"},
		{name: "an edited paste counts as typed", text: "fix teh bug in x := f(z)", pastes: []string{"x := f(y)"},
			masked: "fix teh bug in x := f(z)"},
		{name: "too little typed", text: "see: " + trace, pastes: []string{trace}, skip: "too little typed"},
		{name: "a pasted trace is data", text: trace, pastes: []string{trace}, skip: "log, trace"},
		{name: "pasted JSON is data", text: `{"a": 1, "b": [2]}`, pastes: []string{`{"a": 1, "b": [2]}`}, skip: "log, trace"},
		{name: "pasted diff is data", text: "--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new", pastes: []string{"--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new"}, skip: "log, trace"},
		{name: "a pasted request with bullets isn't data", text: "add tests for:\n- the parser\n- the lexer", pastes: []string{"add tests for:\n- the parser\n- the lexer"},
			masked: "add tests for:\n- the parser\n- the lexer"},
		{name: "only code", text: "```go\nx := 1\n```", skip: "nothing but"},
		{name: "claude's placeholder", text: "fix [Pasted text #1 +5 lines]", skip: "placeholder"},
		{name: "too long", text: strings.Repeat("word ", MaxProseWords+1), skip: "over 300 words"},
		{name: "long prompts get typo fixes only", text: strings.Repeat("word ", MaxExpandWords+1),
			masked: strings.TrimSpace(strings.Repeat("word ", MaxExpandWords+1)), typoOnly: true},
		{name: "context lines are dropped", text: "fix it\n\nContext:\n- Branch: main", masked: "fix it"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := PlanRewrite(tc.text, tc.pastes)
			if tc.skip != "" {
				if !strings.Contains(p.Skip, tc.skip) {
					t.Fatalf("Skip = %q, want it to mention %q", p.Skip, tc.skip)
				}
				return
			}
			if p.Skip != "" {
				t.Fatalf("Skip = %q, want a rewrite", p.Skip)
			}
			if p.Masked != tc.masked || strings.Join(p.Kept, "|") != tc.kept || p.TypoOnly != tc.typoOnly {
				t.Fatalf("got masked %q kept %q typoOnly %v\nwant masked %q kept %q typoOnly %v",
					p.Masked, strings.Join(p.Kept, "|"), p.TypoOnly, tc.masked, tc.kept, tc.typoOnly)
			}
		})
	}
}

func TestPlanRestore(t *testing.T) {
	p := Plan{Kept: []string{"a.go", "```x```"}}
	got, err := p.Restore("Compare <<PASTE_1>> with <<PASTE_2>>.\n\nContext:\n- Branch: main")
	if err != nil || got != "Compare a.go with ```x```.\n\nContext:\n- Branch: main" {
		t.Fatalf("Restore = %q, %v", got, err)
	}
	for _, bad := range []string{
		"Compare <<PASTE_1>>.",                         // dropped
		"Compare <<PASTE_2>> with <<PASTE_1>>.",        // moved
		"Compare <<PASTE_1>> <<PASTE_1>> <<PASTE_2>>.", // repeated
	} {
		if _, err := p.Restore(bad); err == nil {
			t.Errorf("Restore(%q) succeeded, want an error", bad)
		}
	}
}

func TestForContext(t *testing.T) {
	trace := "panic: nil map\n\tmain.go:3"
	cases := []struct {
		text   string
		pastes []string
		want   string
	}{
		{"fix it\n\nContext:\n- Branch: main", nil, "fix it"},
		{"why does this fail: " + trace, []string{trace}, "why does this fail: (pasted text)"},
		{trace, []string{trace}, "(pasted text)"},
		{"is ```x++``` slow", nil, "is (pasted text) slow"},
	}
	for _, tc := range cases {
		if got := ForContext(tc.text, tc.pastes); got != tc.want {
			t.Errorf("ForContext(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}
