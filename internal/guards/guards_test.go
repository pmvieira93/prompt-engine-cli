package guards

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
)

// The example file turns nothing off and changes nothing, and its
// "# built-in" lines are the built-in lists.
func TestExampleFile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "guards.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Parse(data)
	if err != nil {
		t.Fatalf("docs/guards.example.yaml: %v", err)
	}
	def := expand.DefaultRules()
	r.Off, def.Off = nil, nil
	if !reflect.DeepEqual(r, def) {
		t.Errorf("docs/guards.example.yaml differs from the built-in rules:\n%+v\nwant %+v", r, def)
	}

	lists := map[string][]string{
		"destructive": def.Destructive, "question": def.Actions, "question.question_words": def.QuestionWords,
		"added-task": def.Tasks, "negation": def.Negations, "points_back": def.PointsBack, "points_at_reply": def.PointsAtReply,
	}
	builtIn := regexp.MustCompile(`(?m)^# built-in ([a-z_.-]+): (.*)$`)
	seen := map[string]bool{}
	for _, m := range builtIn.FindAllStringSubmatch(string(data), -1) {
		want, ok := lists[m[1]]
		if !ok {
			t.Errorf("# built-in %s: no such list", m[1])
			continue
		}
		seen[m[1]] = true
		if got := strings.Split(m[2], ", "); !reflect.DeepEqual(got, want) {
			t.Errorf("# built-in %s:\n%s\nwant %s", m[1], m[2], strings.Join(want, ", "))
		}
	}
	for name := range lists {
		if !seen[name] {
			t.Errorf("no # built-in %s line", name)
		}
	}
	// Every built-in guard has its section.
	for _, g := range expand.Guards {
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(g) + `:$`).Match(data) {
			t.Errorf("no section for guard %s", g)
		}
	}
}

func TestParse(t *testing.T) {
	r, err := Parse([]byte(`
destructive:
  add: [Deploy, rollback]
negation:
  add: [jamais]
  remove: [only, notthere]
question:
  enabled: false
  question_words:
    add: [qual]
length:
  factor: 4
  allowance: 20
replaced:
  max_share: 0.7
pastes:
  enabled: false
custom:
  - name: prod-words
    added: [production, prod]
  - name: keep-staging
    kept: [staging]
`))
	if err != nil {
		t.Fatal(err)
	}
	def := expand.DefaultRules()
	if got := r.Destructive[len(def.Destructive):]; !reflect.DeepEqual(got, []string{"deploy", "rollback"}) {
		t.Errorf("destructive added %q, want lower case", got)
	}
	if slices.Contains(r.Negations, "only") || !slices.Contains(r.Negations, "jamais") {
		t.Errorf("negations = %q", r.Negations)
	}
	if !slices.Contains(r.QuestionWords, "qual") {
		t.Errorf("question words = %q", r.QuestionWords)
	}
	if !r.Off["question"] || !r.Off["pastes"] || r.Off["destructive"] {
		t.Errorf("off = %v", r.Off)
	}
	if r.LengthFactor != 4 || r.LengthAllowance != 20 || r.MaxRemoved != 0.7 {
		t.Errorf("thresholds = %g, %d, %g", r.LengthFactor, r.LengthAllowance, r.MaxRemoved)
	}
	want := []expand.Custom{{Name: "prod-words", Added: []string{"production", "prod"}}, {Name: "keep-staging", Kept: []string{"staging"}}}
	if !reflect.DeepEqual(r.Custom, want) {
		t.Errorf("custom = %+v", r.Custom)
	}

	weakened, warnings, custom := Notes(r)
	if want := []string{"question off", "pastes off", "negation: 1 word removed", "length 3×+15 → 4×+20", "replaced 0.6 → 0.7"}; !reflect.DeepEqual(weakened, want) {
		t.Errorf("weakened = %q, want %q", weakened, want)
	}
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "pastes is off") {
		t.Errorf("warnings = %q", warnings)
	}
	if !reflect.DeepEqual(custom, []string{"prod-words", "keep-staging"}) {
		t.Errorf("custom = %q", custom)
	}
}

// Adding words, or a stricter threshold, isn't weakening.
func TestNotesStricter(t *testing.T) {
	r, err := Parse([]byte("destructive:\n  add: [deploy]\nreplaced:\n  max_share: 0.5\nlength:\n  factor: 2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if weakened, warnings, _ := Notes(r); len(weakened)+len(warnings) > 0 {
		t.Errorf("weakened %q, warnings %q", weakened, warnings)
	}
}

func TestParseRejects(t *testing.T) {
	for _, tc := range []struct{ file, want string }{
		{"questoin:\n  enabled: false\n", "questoin"},
		{"names:\n  add: [x]\n", "add"},
		{"replaced:\n  max_share: 1.5\n", "replaced.max_share"},
		{"replaced:\n  max_share: 0\n", "replaced.max_share"},
		{"length:\n  factor: 0.5\n", "length.factor"},
		{"length:\n  allowance: -1\n", "length.allowance"},
		{"negation:\n  add: [two words]\n", "not one word"},
		{"negation:\n  add: [jamais]\n  remove: [Jamais]\n", "both added and removed"},
		{"custom:\n  - name: question\n    added: [x]\n", "built-in"},
		{"custom:\n  - name: Prod\n    added: [x]\n", "lower case"},
		{"custom:\n  - name: a\n    added: [x]\n  - name: a\n    kept: [y]\n", "used twice"},
		{"custom:\n  - name: a\n", "needs added or kept"},
		{"destructive: [deploy]\n", "cannot unmarshal"},
	} {
		if _, err := Parse([]byte(tc.file)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err = %v, want %q", tc.file, err, tc.want)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	r, err := Load(path)
	if err != nil || !reflect.DeepEqual(r, expand.DefaultRules()) {
		t.Errorf("no file: %v, %+v", err, r)
	}
	if err := os.WriteFile(path, []byte("question:\n  enabled: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err = Load(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("invalid file: err = %v", err)
	}
	if !reflect.DeepEqual(r, expand.DefaultRules()) {
		t.Errorf("invalid file: not the built-in rules: %+v", r)
	}
}

func TestPath(t *testing.T) {
	t.Setenv("PROMPT_ENGINE_CONFIG", "/work/pe/my.yaml")
	if got, _ := Path(); got != "/work/pe/guards.yaml" {
		t.Errorf("Path() = %q", got)
	}
}
