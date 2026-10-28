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
		{"rfcator tracker to use chans", "Use channels for the RFCator tracker.", true},
		{"refactor tracker to use ", "Refactor the tracker to use [insert specific technology or method here].", true},
		{"make it faster", "Could you please provide more details about what you want to speed up?", true},
		{"fix it", "", true},
		// Names must survive (seen in testing with the 7B model).
		{"now using code-reviewer look into MR !110", "Can you review the code in merge request !110?", true},
		{"now using code-reviewer look into MR !110", "Have the code-reviewer look into MR !110.", false},
		{"with test-runner run the e2e suite", "Run the E2E suite with the test runner.", true},
		{"with test-runner run the e2e suite", "Run the E2E suite with the test-runner.", false},
		{"say hi in 3 words", "Say hi in three words.", false},
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
	}
	for _, tc := range cases {
		if got := strings.Join(LostNames(tc.typed, tc.rewritten), " "); got != tc.lost {
			t.Errorf("LostNames(%q, %q) = %q, want %q", tc.typed, tc.rewritten, got, tc.lost)
		}
	}
}
