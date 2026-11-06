package rules

import (
	"slices"
	"strings"
	"testing"

	"github.com/pmvieira93/prompt-engine-cli/internal/input"
)

const sectioned = `Keep changes small.

[review|PR]
Check the error paths too.

[test|tests|coverage]
Test behaviour, not implementation.

Run them first.

[debug|bug]
Find the cause first.
`

func TestParseNoSections(t *testing.T) {
	got, err := parse("\n- one\n- two [not a header]\n\n")
	if err != nil || got.always != "- one\n- two [not a header]" || len(got.sections) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestParseKeepsBracketLines(t *testing.T) {
	const text = "[note] see [docs]\n[a] and [b]\n- [x] done"
	got, err := parse(text)
	if err != nil || got.always != text || len(got.sections) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestHyphenatedWord(t *testing.T) {
	tmpl, err := parse("[bug-fix]\ntext\n")
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := tmpl.forPrompt("start the bug-fix branch", nil); text != "text" {
		t.Errorf("no match: %q", text)
	}
	if text, _ := tmpl.forPrompt("fix the bug", nil); text != "" {
		t.Errorf("matched part of the word: %q", text)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]struct{ text, want string }{
		"space":        {"[two words]\ntext\n", `line 1: "two words" isn't a word`},
		"empty header": {"rules\n\n[]\ntext\n", `line 3: "" isn't a word`},
		"empty word":   {"[test|]\ntext\n", `line 1: "" isn't a word`},
		"symbol":       {"[c++]\ntext\n", `line 1: "c++" isn't a word`},
		"same section": {"[test|tests|TEST]\ntext\n", `line 1: "test" is already used`},
		"two sections": {"[test]\ntext\n\n[check|test]\ntext\n", `line 4: "test" is already used`},
		"no letters":   {"[---]\ntext\n", `line 1: "---" isn't a word`},
		"no text":      {"[test]\n\n[debug]\ntext\n", "line 1: [test] has no text"},
		"last no text": {"[test]\ntext\n\n[debug]\n", "line 4: [debug] has no text"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parse(tc.text)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestForPrompt(t *testing.T) {
	tmpl, err := parse(sectioned)
	if err != nil {
		t.Fatal(err)
	}
	const always = "Keep changes small."
	const test = "Test behaviour, not implementation.\n\nRun them first."
	cases := []struct {
		typed, text string
		names       []string
	}{
		{"explain the config loader", always, nil},
		{"add coverage to the parser", always + "\n\n" + test, []string{"test"}},
		{"Tests, please.", always + "\n\n" + test, []string{"test"}},
		{"look at PR #331", always + "\n\nCheck the error paths too.", []string{"review"}},
		{"debug the loader", always + "\n\nFind the cause first.", []string{"debug"}},
		{"debugging the loader, latest attest", always, nil},
		{"review and test this", always + "\n\nCheck the error paths too.\n\n" + test, []string{"review", "test"}},
		{"update it\n\nContext:\n- Branch: fix/bug", always, nil},
		{"fix the 'bug' now", always + "\n\nFind the cause first.", []string{"debug"}},
		{"fix this bug's cause", always + "\n\nFind the cause first.", []string{"debug"}},
		{"fix the bug—it crashes", always + "\n\nFind the cause first.", []string{"debug"}},
		{"fix the “bug” here", always + "\n\nFind the cause first.", []string{"debug"}},
		{"fix the bug-fix branch", always + "\n\nFind the cause first.", []string{"debug"}},
		{"see [Pasted text #1 +5 lines] and the `crash`\n```\na bug\n```", always, nil},
	}
	for _, tc := range cases {
		text, names := tmpl.forPrompt(tc.typed, nil)
		if text != tc.text || !slices.Equal(names, tc.names) {
			t.Errorf("%q:\n got %q %q\nwant %q %q", tc.typed, text, names, tc.text, tc.names)
		}
	}
}

func TestSectionsOnly(t *testing.T) {
	in, logs := injector(t, "[test]\nRun them first.\n")
	if got := in.BeforeSubmit(state("explain the loader")); got != nil {
		t.Fatalf("no match: injected %q", got)
	}
	if !strings.Contains(strings.Join(*logs, ""), "not added (no section matches)") {
		t.Errorf("logs = %q", *logs)
	}
	if _, text, names := in.Prepare(state("run the test")); text != "Run them first." || !slices.Equal(names, []string{"test"}) {
		t.Errorf("match: %q %q", text, names)
	}
}

func TestSectionsMatchTypedText(t *testing.T) {
	in, _ := injector(t, "[review]\nCheck the error paths too.\n")
	const rewrite = "Can you review the loader?"
	in.Typed = func(s input.State) (string, []string) {
		if s.Text == rewrite {
			return "look at the loader", nil
		}
		return s.Text, s.Pastes
	}
	if got := in.BeforeSubmit(state(rewrite)); got != nil {
		t.Fatalf("matched a word only the rewrite has: %q", got)
	}
	if in.Text(rewrite) != "" {
		t.Error("the preview has a section only the rewrite matches")
	}
	in.Typed = func(input.State) (string, []string) { return "please review the loader", nil }
	if got := in.BeforeSubmit(state("Look at the loader.")); got == nil {
		t.Error("no match on the typed text")
	}
}

func TestBadTemplateAddsNothing(t *testing.T) {
	in, logs := injector(t, "keep it small\n\n[two words]\ntext\n")
	if got := in.BeforeSubmit(state("explain the loader")); got != nil {
		t.Fatalf("injected %q", got)
	}
	if !strings.Contains(strings.Join(*logs, ""), "not added") {
		t.Errorf("logs = %q", *logs)
	}
	if err := in.Check(); err == nil || !strings.Contains(err.Error(), "append.txt line 3") {
		t.Errorf("Check = %v", err)
	}
	if got := in.Status(nil); len(got) != 1 || !strings.Contains(got[0], "append.txt line 3") {
		t.Errorf("Status = %q", got)
	}
}

func TestStatus(t *testing.T) {
	plain, _ := injector(t, "keep it small")
	if got := plain.Status(nil); got != nil {
		t.Errorf("no sections: %q", got)
	}
	in, _ := injector(t, sectioned)
	if got := strings.Join(in.Status([]string{"review", "test"}), ""); !strings.Contains(got, "3 sections; last prompt: review, test") {
		t.Errorf("Status = %q", got)
	}
	if got := strings.Join(in.Status(nil), ""); !strings.Contains(got, "last prompt: none") {
		t.Errorf("no match: %q", got)
	}
}

func TestShortGoesByTypedText(t *testing.T) {
	in, _ := injector(t, "rules")
	in.MinWords = func() int { return 3 }
	const rewrite = "Please fix the failing tests."
	in.Typed = func(input.State) (string, []string) { return "fix tests", nil }
	if got := in.BeforeSubmit(state(rewrite)); got != nil {
		t.Fatalf("a short prompt's rewrite got rules: %q", got)
	}
	if in.Text(rewrite) != "" {
		t.Error("the preview has the rules")
	}
}

func TestSectionsSkipPastes(t *testing.T) {
	in, _ := injector(t, "[crash]\nFind the cause first.\n")
	const paste = "line 1: crash in main\nline 2: failed"
	s := state("explain this failure\n" + paste)
	s.Pastes = []string{paste}
	if got := in.BeforeSubmit(s); got != nil {
		t.Fatalf("matched a paste: %q", got)
	}
	s = state("explain this crash\n" + paste)
	s.Pastes = []string{paste}
	if in.BeforeSubmit(s) == nil {
		t.Error("no match on the typed words")
	}
}

func TestBOM(t *testing.T) {
	in, _ := injector(t, "\ufeff[test]\nRun them first.\n")
	if got := in.BeforeSubmit(state("explain the loader")); got != nil {
		t.Fatalf("the section was read as text: %q", got)
	}
}
