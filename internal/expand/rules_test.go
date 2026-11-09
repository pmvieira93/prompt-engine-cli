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
		// Seen in review: quotes, apostrophes, hyphens and doubled letters.
		{"restart the api", "Restart the production's api.", "never-add"},
		{"restart the api", "Restart the production’s api.", "never-add"},
		{"restart the api", "Restart the 'production' api.", "never-add"},
		{"restart the api", "Restart the production-like api.", "never-add"},
		{"restart the staging api", "Restart the 'staging' api.", ""},
		{"restart the staging api", "Restart the staging-api.", ""},
		// Dashes and curly double quotes sit between words.
		{"restart the api", "Restart the api—production next.", "never-add"},
		{"restart the api", "Restart the “production” api.", "never-add"},
		{"restart staging—then check it", "Restart it, then check it.", "always-keep"},
		{"restart staging—then check it", "Restart staging, then check it.", ""},
	} {
		if got := guard(r, c.typed, c.rewritten); got != c.want {
			t.Errorf("%q → %q: stopped by %q, want %q", c.typed, c.rewritten, got, c.want)
		}
	}
	// "staging-api" typed has "staging" (names would stop this first).
	if stop := r.check("restart the staging-api", "Restart the api."); stop == nil || stop.Guard != "always-keep" {
		t.Errorf("staging-api: %+v, want always-keep", stop)
	}
	commit := &Rules{NeverAdd: []Rule{{Line: 1, Words: []string{"commit", "ship", "deploy", "force push"}}}}
	for _, rewritten := range []string{"Tidy it; committed.", "Tidy it before shipping.", "Tidy it; deploys later.", "Tidy it, then force pushed."} {
		if got := guard(commit, "tidy it", rewritten); got != "never-add" && got != "destructive" {
			t.Errorf("%q: stopped by %q, want never-add", rewritten, got)
		}
	}
	// Only a phrase's last word takes forms: "forced push" isn't "force push".
	if got := guard(&Rules{NeverAdd: []Rule{{Line: 1, Words: []string{"dry run"}}}}, "tidy it", "Tidy it; dried runs later."); got == "never-add" {
		t.Errorf("dried runs: stopped by never-add")
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

// Only the words that matter for the prompt are named: a typed word in a
// "never add" list would invite the model to drop it.
func TestRulesInstructions(t *testing.T) {
	r := &Rules{
		NeverAdd:   []Rule{{Line: 1, Words: []string{"deploy", "production"}}, {Line: 2, Words: []string{"force push"}}},
		AlwaysKeep: []Rule{{Line: 3, Words: []string{"staging", "main"}}},
	}
	for _, c := range []struct{ typed, want string }{
		{"restart the api", "\nNever add these words: deploy, production, force push."},
		{"deploy the staging api", "\nNever add these words: production, force push.\nKeep these words exactly: staging."},
		{"deploy production with a force push on main", "\nKeep these words exactly: main."},
	} {
		if got := r.Instructions(c.typed); got != c.want {
			t.Errorf("Instructions(%q) = %q, want %q", c.typed, got, c.want)
		}
	}
	if got := (&Rules{}).Instructions("x"); got != "" {
		t.Errorf("no rules: %q", got)
	}
}

// The shortcut checks names, quotes and the developer's rules, and honours
// turn off.
func TestCheckShortcut(t *testing.T) {
	r := &Rules{NeverAdd: []Rule{{Line: 4, Words: []string{"production"}}}}
	if stop := r.CheckShortcut("restart the api", "Restart the production api."); stop == nil || stop.Guard != "never-add" {
		t.Errorf("never add: %+v", stop)
	}
	if stop := r.CheckShortcut("use code-reviewer", "Review the code."); stop == nil || stop.Guard != "names" {
		t.Errorf("names: %+v", stop)
	}
	off := &Rules{Off: map[string]bool{"names": true}}
	if stop := off.CheckShortcut("use code-reviewer", "Review the code."); stop != nil {
		t.Errorf("names off: %+v", stop)
	}
	// Not a guard the shortcut checks: the developer sees the rewrite.
	if stop := (*Rules)(nil).CheckShortcut("why does it fail?", "Fix it."); stop != nil {
		t.Errorf("question: %+v", stop)
	}
}

// empty stays on whatever Off says.
func TestEmptyAlwaysOn(t *testing.T) {
	r := &Rules{Off: map[string]bool{"empty": true, "language": true, "replaced": true, "dropped-part": true}}
	if stop := r.CheckSession("fix the parser now", "", SessionContext{}); stop == nil || stop.Guard != "empty" {
		t.Errorf("empty off: %+v", stop)
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
