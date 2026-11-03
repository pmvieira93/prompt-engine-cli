package expand

import (
	"strings"
	"testing"
)

// The developer's guards.txt (PRD Appendix 4) reaches the checks.
func TestRules(t *testing.T) {
	guard := func(r *Rules, typed, rewritten string) string {
		if stop := r.CheckSession(typed, rewritten, SessionContext{}); stop != nil {
			return stop.Guard
		}
		return ""
	}
	r := &Rules{
		NeverAdd:   []Rule{{Line: 1, Words: []string{"deploy", "production"}}, {Line: 2, Words: []string{"dry run"}}},
		AlwaysKeep: []Rule{{Line: 3, Words: []string{"staging"}}},
	}
	for _, c := range []struct{ typed, rewritten, want string }{
		{"tidy the branch", "Tidy the branch and deploy it.", "never-add"},
		{"tidy the branch", "Tidy the branch; it deploys later.", "never-add"}, // a form of the word
		{"restart the api", "Restart the production api.", "never-add"},
		{"deploy the api", "Deploy the api.", ""},              // typed
		{"restart the api", "Restart the deployment api.", ""}, // another word
		{"tidy the branch", "Tidy the branch after a dry run.", "never-add"},
		{"run the dry tests", "Run the dry tests.", ""}, // "dry" and "run", not "dry run"
		{"restart the staging api", "Restart the api in stage.", "always-keep"},
		{"restart the staging api", "Restart the staging api.", ""},
	} {
		if got := guard(r, c.typed, c.rewritten); got != c.want {
			t.Errorf("%q → %q: stopped by %q, want %q", c.typed, c.rewritten, got, c.want)
		}
	}
	stop := r.CheckSession("restart the api", "Restart the production api.", SessionContext{})
	if stop == nil || stop.Detail != "production" || !strings.Contains(stop.Reason, "guards.txt line 1") {
		t.Errorf("stop = %+v, want the line named", stop)
	}

	// A built-in guard turned off.
	if got := guard(nil, "why does the test fail?", "Why does the test fail? Fix the test."); got != "question" {
		t.Fatalf("built in: stopped by %q, want question", got)
	}
	off := &Rules{Off: map[string]bool{"question": true}}
	if got := guard(off, "why does the test fail?", "Why does the test fail? Fix the test."); got != "" {
		t.Errorf("question off: stopped by %q", got)
	}
}

func TestRulesInstructions(t *testing.T) {
	if got := (&Rules{}).Instructions(); got != "" {
		t.Errorf("no rules: %q", got)
	}
	r := &Rules{
		NeverAdd:   []Rule{{Line: 1, Words: []string{"deploy"}}, {Line: 2, Words: []string{"force push"}}},
		AlwaysKeep: []Rule{{Line: 3, Words: []string{"staging"}}},
	}
	want := "\nNever add these words unless the developer wrote them: deploy, force push." +
		"\nAlways keep these words when the developer wrote them: staging."
	if got := r.Instructions(); got != want {
		t.Errorf("Instructions() = %q, want %q", got, want)
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
