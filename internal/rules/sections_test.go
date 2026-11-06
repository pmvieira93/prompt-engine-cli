package rules

import (
	"slices"
	"strings"
	"testing"
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

func TestParseErrors(t *testing.T) {
	cases := map[string]struct{ text, want string }{
		"space":        {"[two words]\ntext\n", `line 1: "two words" isn't a word`},
		"empty header": {"rules\n\n[]\ntext\n", `line 3: "" isn't a word`},
		"empty word":   {"[test|]\ntext\n", `line 1: "" isn't a word`},
		"symbol":       {"[c++]\ntext\n", `line 1: "c++" isn't a word`},
		"same section": {"[test|tests|TEST]\ntext\n", `line 1: "test" is already used`},
		"two sections": {"[test]\ntext\n\n[check|test]\ntext\n", `line 4: "test" is already used`},
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
	}
	for _, tc := range cases {
		text, names := tmpl.forPrompt(tc.typed)
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
	in.Typed = func(prompt string) string {
		if prompt == rewrite {
			return "look at the loader"
		}
		return prompt
	}
	if got := in.BeforeSubmit(state(rewrite)); got != nil {
		t.Fatalf("matched a word only the rewrite has: %q", got)
	}
	if in.Text(rewrite) != "" {
		t.Error("the preview has a section only the rewrite matches")
	}
	in.Typed = func(string) string { return "please review the loader" }
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
	if got := in.Status(); len(got) != 1 || !strings.Contains(got[0], "append.txt line 3") {
		t.Errorf("Status = %q", got)
	}
}

func TestStatus(t *testing.T) {
	plain, _ := injector(t, "keep it small")
	if got := plain.Status(); got != nil {
		t.Errorf("no sections: %q", got)
	}
	in, _ := injector(t, sectioned)
	in.MinWords = func() int { return 3 }
	in.BeforeSubmit(state("review and test this"))
	if got := strings.Join(in.Status(), ""); !strings.Contains(got, "3 sections; last prompt: review, test") {
		t.Errorf("Status = %q", got)
	}
	in.BeforeSubmit(state("/compact")) // not a prompt: the last one stands
	if got := strings.Join(in.Status(), ""); !strings.Contains(got, "review, test") {
		t.Errorf("after a slash command: %q", got)
	}
	in.BeforeSubmit(state("explain the loader"))
	if got := strings.Join(in.Status(), ""); !strings.Contains(got, "last prompt: none") {
		t.Errorf("no match: %q", got)
	}
}
