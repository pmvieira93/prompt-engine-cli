package expand

import (
	"strings"
	"testing"
)

func TestGuard(t *testing.T) {
	cases := []struct {
		typed, rewritten string
		stopped          bool
	}{
		// Rewrites from testing that should go through.
		{"fix teh failing tst in config", "Fix the failing test in the config.\n\nContext:\n- Branch: main", false},
		{"add tests for ollama client + handle timeouts + log errs", "Add tests for ollama client, handle timeouts, and log errors.", false},
		{"now add tests for it", "Add tests for the retry mechanism with exponential backoff in the ollama client.", false},
		{"say helo in thre words", `Say "hello" in three words.`, false},
		{"what does the leader key do?", "What does the leader key do?", false},
		// Bad ones from testing that should be stopped.
		// A misread typo that keeps most words isn't caught (known limit).
		{"rfcator tracker to use chans", "Use channels for the RFCator tracker.", false},
		{"fix the login bug", "Write a blog post about web security.", true},
		{"don't touch the tests, just fix the config loadr", "Just fix the config loader without changing the tests.", false},
		{"refactor tracker to use ", "Refactor the tracker to use [insert specific technology or method here].", true},
		{"make it faster", "Could you please provide more details about what you want to speed up?", true},
		{"fix it", "", true},
		// Names must survive (seen in testing with the 7B model).
		{"now using code-reviewer look into MR !110", "Can you review the code in merge request !110?", true},
		{"now using code-reviewer look into MR !110", "Have the code-reviewer look into MR !110.", false},
		{"with test-runner run the e2e suite", "Run the E2E suite with the test runner.", true},
		{"with test-runner run the e2e suite", "Run the E2E suite with the test-runner.", false},
		{"say hi in 3 words", "Say hi in three words.", false},
		// A question at the end, without its "?" (seen around a paste).
		{"the eror is <<PASTE_1>> when i run make, can yuo fix it", "The error is <<PASTE_1>> when I run make, can you fix it?", false},
		// A report must stay a report (seen in preview mode).
		{"it still fails, same as before", "It still fails, same as before.", false},
		{"it still fais", "Fix the error in the sentence: it still fails.", true},
		{"it still fails, same error message, same code, same environment, same machine, same operating system,",
			`Ensure the "hello" message is displayed using three words. It still fails with the same error message, code, environment, machine, and operating system.`, true},
		{"the tracker loses sync after a paste", "Investigate why the tracker loses sync after a paste.", true},
		// A whole sentence dropped (seen in Part O).
		{"say hello in three words, and keep the answer short;\nthis promtp is just long enough to wrap onto a second row",
			"Say hello in three words, and keep the answer short.", true},
		{"say hello in three words, and keep the answer short; this prompt is just long enough to wrap onto a second row",
			"Say hello in three words; keep the answer short.", true},
		// Sentences merged or reworded keep something of each.
		{"fix the login bug. dont touch the api", "Fix the login bug without changing the API.", false},
		{"add a retry to the client. make it exponential with max 5 attempts", "Add an exponential retry with at most 5 attempts to the client.", false},
		// A typo in the verb isn't a new task.
		{"refactr the paresr to use a lookup tabel", "Refactor the parser to use a lookup table.", false},
		{"updte the readme", "Update the README.", false},
		{"tell me a joke abuot cats", "Tell me a joke about cats.", false},
	}
	for _, tc := range cases {
		reason := Guard(tc.typed, tc.rewritten)
		if (reason != "") != tc.stopped {
			t.Errorf("Guard(%q, %q) = %q, want stopped=%v", tc.typed, tc.rewritten, reason, tc.stopped)
		}
	}
}

func TestLostNames(t *testing.T) {
	cases := []struct {
		typed, rewritten string
		lost             string
	}{
		{"ask frontend-dev to fix header.tsx", "Ask frontend-dev to fix header.tsx.", ""},
		{"ask frontend-dev to fix header.tsx", "Ask the frontend developer to fix the header.", "frontend-dev header.tsx"},
		{"@security-auditor check auth.go", "@security-auditor, check auth.go.", ""},
		{"fix the multi-line bug", "Fix the multiline bug.", ""},
		{"review (#110), then merge", "Review #110, then merge.", ""},
		{"run it 3 times", "Run it three times.", ""},
		{"fix the error `dial tcp [::1]:11434`in the client", "Fix the error `dial tcp [::1]:11434` in the client.", ""},
	}
	for _, tc := range cases {
		if got := strings.Join(LostNames(tc.typed, tc.rewritten), " "); got != tc.lost {
			t.Errorf("LostNames(%q, %q) = %q, want %q", tc.typed, tc.rewritten, got, tc.lost)
		}
	}
}

// TestGuardMeaning: rewrites of qwen2.5:7b collected in testing must pass;
// each kind of meaning change must be stopped, with its own reason.
func TestGuardMeaning(t *testing.T) {
	pass := [][2]string{
		{"don't touch the tests, just fix the config loader", "Just fix the config loader without changing the tests."},
		{"fix the parser but dont change the public api", "Fix the parser without changing the public API."},
		{"only update the readme, nothing else", "Update only the README, no other changes."},
		{"refactor the tracker without changing behaviour", "Refactor the tracker without changing its behavior."},
		{"why does the ghost text flicker on resize?", "Why does the ghost text flicker when the element is resized?"},
		{"how does the input buffer resync from the screen", "How does the input buffer resync from the screen?"},
		{"is the panel width saved between sessions", "Is the panel width saved between sessions?"},
		{"fix the error `connection refused: dial tcp [::1]:11434`", "Fix the error `connection refused: dial tcp [::1]:11434`."},
		{`grep for "TODO(pedro)" and list them`, `Grep for "TODO(pedro)" and list the results.`},
		{"run `go test -run TestGuard ./internal/expand/` and fix failures", "Run `go test -run TestGuard ./internal/expand/` and fix any failures."},
		{"remove the debug log from main.go", "Remove the debug log from main.go"},
		{"corrige o erro no ficheiro de configuração", "Corrija o erro no arquivo de configuração."},
		{"explica como funciona o buffer de entrada", "Explica como funciona o buffer de entrada."},
		{"arregla el error en la configuracion", "Corrige el error en la configuración."},
	}
	for _, p := range pass {
		if reason := Guard(p[0], p[1]); reason != "" {
			t.Errorf("Guard(%q, %q) stopped a good rewrite: %s", p[0], p[1], reason)
		}
	}

	stop := []struct{ typed, rewritten, reason string }{
		{"don't touch the tests, just fix the config", "Fix the config and update the tests.", "negation"},
		{"refactor the tracker without changing behaviour", "Refactor the tracker.", "negation"},
		{"clean up the unused helpers in tui", "Delete the unused helpers in tui.", "destructive"},
		{"tidy the branch", "Tidy the branch and force push it.", "destructive"},
		{"and what about multi line prompts", "Can you handle multi-line prompts?", "question into an instruction"},
		{"why does the panel flicker", "Fix the panel flicker.", "question into an instruction"},
		{"fix the error `connection refused`", "Fix the connection refused error.", "quoted text"},
		{"explica como funciona o buffer de entrada", "Explain how the input buffer works.", "language"},
		{"fix teh failing tst in config", "Corrija o teste falhando no arquivo de configuração.", "language"},
		{"add tests", "Add unit tests for every package, covering all error paths, edge cases, concurrency, timeouts, retries, and malformed input, then run them in CI.", "longer"},
		{"fix the parser", "Fix the parser, but don't change the public API.", "negation"}, // added, not dropped
	}
	for _, c := range stop {
		reason := Guard(c.typed, c.rewritten)
		if !strings.Contains(reason, c.reason) {
			t.Errorf("Guard(%q, %q) = %q, want a reason about %q", c.typed, c.rewritten, reason, c.reason)
		}
	}
}

func TestLanguage(t *testing.T) {
	cases := map[string]string{
		"corrige o erro no ficheiro de configuração":                "Portuguese",
		"explica como funciona o buffer de entrada":                 "Portuguese",
		"adiciona testes para o cliente ollama e trata os timeouts": "Portuguese",
		"arregla el error en la configuracion":                      "Spanish",
		"fix teh failing tst in config":                             "",
		"refactor tracker to use chans":                             "",
		"add tests":                                                 "",
		"don't add a section to the README":                         "",
	}
	for text, want := range cases {
		if got := Language(text); got != want {
			t.Errorf("Language(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestGuardContextLeak(t *testing.T) {
	earlier := []string{"don't touch the tests, just fix the config loadr"}
	typed := "fix the error `connection refused: dial tcp [::1]:11434` in the client"
	leaky := "Fix the error `connection refused: dial tcp [::1]:11434` in the client. Don't touch the tests or the config loadr."
	if reason := Guard(typed, leaky, earlier...); !strings.Contains(reason, "negation") && !strings.Contains(reason, "earlier prompts") {
		t.Errorf("leaked context not stopped: %q", reason)
	}
	// Without the extra negation, the leak check alone catches it.
	leaky = "Fix the error `connection refused: dial tcp [::1]:11434` in the client and the config loadr tests."
	if reason := Guard(typed, leaky, earlier...); !strings.Contains(reason, "earlier prompts") {
		t.Errorf("leak not stopped: %q", reason)
	}
	// Referring back makes using the context the point.
	if reason := Guard("now add tests for it", "Add tests for the retry mechanism in the ollama client.",
		"add a retry to the ollama client with exponential backoff"); reason != "" {
		t.Errorf("a resolved reference was stopped: %q", reason)
	}
}

func TestCheckNamesTheGuard(t *testing.T) {
	cases := []struct{ typed, rewritten, guard string }{
		// Seen in testing: the added task also adds a negation; the task
		// is the clearer reason.
		{"it still fails, same error message, same code, same environment", "Ensure the code runs without errors. Report any new observations.", "added-task"},
		{"don't touch the tests, just fix the config", "Fix the config and update the tests.", "negation"},
		{"tidy the branch", "Tidy the branch and force push it.", "destructive"},
		{"now using code-reviewer look into MR !110", "Review the code in MR !110.", "names"},
		{"why does it flicker", "Fix the flicker.", "question"},
		{"fix it", "", "empty"},
	}
	for _, tc := range cases {
		stop := Check(tc.typed, tc.rewritten)
		if stop == nil || stop.Guard != tc.guard {
			t.Errorf("Check(%q, %q) = %+v, want guard %q", tc.typed, tc.rewritten, stop, tc.guard)
		}
	}
	for _, tc := range []struct{ typed, rewritten, detail string }{
		{"it still fails", "Ensure it runs. Check it.", "ensure, runs, check"},
		{"fix the bug", "Fix the bug without tests.", "negations 0 → 1"},
		{"now using code-reviewer look into MR !110", "Review MR !110.", "lost code-reviewer"},
		{"corrige o erro no ficheiro", "Fix the error in the file.", "Portuguese → English"},
	} {
		if stop := Check(tc.typed, tc.rewritten); stop == nil || stop.Detail != tc.detail {
			t.Errorf("Check(%q, %q) = %+v, want detail %q", tc.typed, tc.rewritten, stop, tc.detail)
		}
	}
	if stop := Check("fix teh bug", "Fix the bug."); stop != nil {
		t.Errorf("a good rewrite was stopped: %+v", stop)
	}
}

// Cases from the L1/L2/L3 evaluation (PRD Appendix 1, FR-6.3): the
// assistant's last reply lets a rewrite resolve what a follow-up points at.
func TestCheckSessionResolvesTheReply(t *testing.T) {
	panel := SessionContext{
		Prompts: []string{"why does the config reload ignore panel.visible"},
		Reply:   "panel.visible is only read at startup. Two options:\n1. Apply it live, by opening or closing the panel when it changes.\n2. Keep it as it is, and say so in the README.\nWhich do you prefer?",
	}
	build := SessionContext{
		Prompts: []string{"the build fails on linux"},
		Reply:   "internal/pty/termios.go uses unix.TIOCGETA, which exists only on macOS. I can split it into termios_darwin.go and termios_linux.go. Should I?",
	}
	guards := SessionContext{
		Prompts: []string{"review the guard rails for false positives"},
		Reply:   "I found two:\n1. added-task stops rewrites of typo'd verbs.\n2. negation stops \"only… not…\" rewrites that keep the meaning.",
	}
	for _, c := range []struct {
		sc                SessionContext
		typed, rewritten  string
		guard             string // "" for none
		guardWithoutReply string
	}{
		// "do" is not a question here, and the pointer words are replaced.
		{panel, "do the first one", "Apply panel.visible live by opening or closing the panel when it changes.", "", "added-task"},
		// The reply's "only" is resolved, not added.
		{panel, "ok go with option 2 and update the docs too", "Apply option 2 and update the docs to reflect that panel.visible is only read at startup.", "", "negation"},
		{build, "yes go ahead", "Split termios.go into termios_darwin.go and termios_linux.go.", "", "replaced"},
		{guards, "fix the second one but dont touch the tests", "Fix the guard rail for \"only… not…\" rewrites that keep the meaning, but don't touch the tests.", "", ""},
		// A negation typed must still be kept.
		{guards, "fix the second one but dont touch the tests", "Fix the \"only… not…\" guard and update the tests.", "negation", "negation"},
		// The model copied the reply's question: still stopped.
		{build, "it still fails, same error", "The build still fails. Should I split termios.go?", "asked-back", "asked-back"},
		// Destructive actions are stopped wherever they come from.
		{SessionContext{Reply: "Should I delete the old snapshots?"}, "yes go ahead", "Delete the old snapshots.", "destructive", "destructive"},
	} {
		got := ""
		if st := CheckSession(c.typed, c.rewritten, c.sc); st != nil {
			got = st.Guard
		}
		if got != c.guard {
			t.Errorf("%q → %q: guard %q, want %q", c.typed, c.rewritten, got, c.guard)
		}
		// With only the earlier prompts (L1), "the first one" or "go ahead"
		// point at nothing the model was given, so the guards apply.
		got = ""
		if st := CheckSession(c.typed, c.rewritten, SessionContext{Prompts: c.sc.Prompts}); st != nil {
			got = st.Guard
		}
		if got != c.guardWithoutReply {
			t.Errorf("L1 %q → %q: guard %q, want %q", c.typed, c.rewritten, got, c.guardWithoutReply)
		}
	}
}

func TestDoStartsAQuestionOnlyBeforeASubject(t *testing.T) {
	for text, want := range map[string]bool{
		"do you know why it fails": true,
		"do we need the lock":      true,
		"do the first one":         false,
		"do it":                    false,
		"does it work":             true,
	} {
		if got := isQuestion(text); got != want {
			t.Errorf("isQuestion(%q) = %v, want %v", text, got, want)
		}
	}
}
