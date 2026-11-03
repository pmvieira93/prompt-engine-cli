package expand

import "testing"

// The guards file's changes (PRD Appendix 4) reach the checks.
func TestRules(t *testing.T) {
	guard := func(r Rules, typed, rewritten string) string {
		if stop := r.CheckSession(typed, rewritten, SessionContext{}); stop != nil {
			return stop.Guard
		}
		return ""
	}
	def := DefaultRules()

	// FR-10.1: word lists.
	r := DefaultRules()
	r.Destructive = append(r.Destructive, "deploy")
	if got := guard(def, "tidy the branch", "Tidy the branch and deploy it."); got != "" {
		t.Errorf("built in, deploy stopped by %q", got)
	}
	if got := guard(r, "tidy the branch", "Tidy the branch and deploy it."); got != "destructive" {
		t.Errorf("deploy added: stopped by %q, want destructive", got)
	}
	r = DefaultRules()
	r.Negations = append(r.Negations, "jamais")
	if got := guard(r, "jamais touch the tests", "Touch the tests."); got != "negation" {
		t.Errorf("jamais: stopped by %q, want negation", got)
	}
	r = DefaultRules()
	r.QuestionWords = append(r.QuestionWords, "maybe")
	if got := guard(r, "maybe the config is wrong", "Maybe the config is wrong; fix it."); got != "question" {
		t.Errorf("maybe as a question word: stopped by %q, want question", got)
	}

	// FR-10.2: thresholds.
	long := "Fix the typo in the readme, and keep every other line of the readme exactly as it is now please."
	if got := guard(def, "fix the readme typo", long); got != "" {
		t.Fatalf("built in: stopped by %q", got)
	}
	r = DefaultRules()
	r.LengthAllowance = 0
	if got := guard(r, "fix the readme typo", long); got != "length" {
		t.Errorf("allowance 0: stopped by %q, want length", got)
	}
	if got := guard(def, "make the panel wider", "Increase the side panel's width."); got != "replaced" {
		t.Fatalf("built in: stopped by %q, want replaced", got)
	}
	r = DefaultRules()
	r.MaxRemoved = 0.9
	if got := guard(r, "make the panel wider", "Increase the side panel's width."); got != "" {
		t.Errorf("max_share 0.9: stopped by %q", got)
	}

	// FR-10.3: a guard turned off.
	r = DefaultRules()
	if got := guard(def, "why does the test fail?", "Why does the test fail? Fix the test."); got != "question" {
		t.Fatalf("built in: stopped by %q, want question", got)
	}
	r.Off = map[string]bool{"question": true}
	if got := guard(r, "why does the test fail?", "Why does the test fail? Fix the test."); got != "" {
		t.Errorf("question off: stopped by %q", got)
	}

	// FR-10.4: custom guards.
	r = DefaultRules()
	r.Custom = []Custom{{Name: "prod-words", Added: []string{"prod"}}, {Name: "keep-staging", Kept: []string{"staging"}}}
	if got := guard(r, "restart the api", "Restart the production api."); got != "prod-words" {
		t.Errorf("production added: stopped by %q, want prod-words", got)
	}
	if got := guard(r, "restart the prod api", "Restart the production api."); got != "" {
		t.Errorf("prod typed: stopped by %q", got)
	}
	if got := guard(r, "restart the staging api", "Restart the api in stage."); got != "keep-staging" {
		t.Errorf("staging dropped: stopped by %q, want keep-staging", got)
	}
	if got := guard(r, "restart the staging api", "Restart the staging api."); got != "" {
		t.Errorf("staging kept: stopped by %q", got)
	}
}

func TestRestoreMoved(t *testing.T) {
	p := Plan{Masked: "fix " + Marker(1) + " and " + Marker(2), Kept: []string{"A", "B"}}
	if _, err := p.Restore("Fix " + Marker(2) + " and " + Marker(1)); err == nil {
		t.Error("Restore: a moved part passed")
	}
	got, err := p.RestoreMoved("Fix " + Marker(2) + " and " + Marker(1))
	if err != nil || got != "Fix B and A" {
		t.Errorf("RestoreMoved = %q, %v", got, err)
	}
	if _, err := p.RestoreMoved("Fix " + Marker(1) + " and " + Marker(1)); err == nil {
		t.Error("RestoreMoved: a repeated part passed")
	}
}

func TestWeakened(t *testing.T) {
	def := DefaultRules()
	if got := def.Weakened(def); len(got) != 0 {
		t.Errorf("built in: %q", got)
	}
}
