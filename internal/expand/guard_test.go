package expand

import "testing"

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
	}
	for _, tc := range cases {
		reason := Guard(tc.typed, tc.rewritten)
		if (reason != "") != tc.stopped {
			t.Errorf("Guard(%q, %q) = %q, want stopped=%v", tc.typed, tc.rewritten, reason, tc.stopped)
		}
	}
}
