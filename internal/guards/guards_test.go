package guards

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
)

// The example file has no rules until one is uncommented, lists every
// built-in guard, and its examples parse.
func TestExampleFile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "guards.example.txt"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Parse(string(data))
	if err != nil {
		t.Fatalf("docs/guards.example.txt: %v", err)
	}
	if !reflect.DeepEqual(r, expand.Rules{}) {
		t.Errorf("rules without uncommenting: %+v", r)
	}
	for _, g := range expand.Guards {
		if !regexp.MustCompile(`(?m)^#   ` + regexp.QuoteMeta(g) + ` +\S`).Match(data) {
			t.Errorf("guard %s isn't listed", g)
		}
	}
	var examples []string
	for _, line := range strings.Split(string(data), "\n") {
		if l := strings.TrimPrefix(line, "# "); strings.HasPrefix(l, "never add:") || strings.HasPrefix(l, "always keep:") || strings.HasPrefix(l, "turn off:") {
			examples = append(examples, l)
		}
	}
	r, err = Parse(strings.Join(examples, "\n"))
	if err != nil || len(examples) < 4 || len(r.NeverAdd) != 2 || len(r.AlwaysKeep) != 1 || !r.Off["question"] {
		t.Errorf("examples %q: %+v, %v", examples, r, err)
	}
}

func TestParse(t *testing.T) {
	r, err := Parse(`
# a note
never add: Deploy, rollback,  force   push
Always Keep: staging   # a note after a rule
turn off: question, pastes
`)
	if err != nil {
		t.Fatal(err)
	}
	want := expand.Rules{
		Off:        map[string]bool{"question": true, "pastes": true},
		NeverAdd:   []expand.Rule{{Line: 3, Words: []string{"deploy", "rollback", "force push"}}},
		AlwaysKeep: []expand.Rule{{Line: 4, Words: []string{"staging"}}},
	}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("Parse = %+v, want %+v", r, want)
	}
	summary, warnings := Notes(r)
	if summary != "never add 3; always keep 1; off: question, pastes" {
		t.Errorf("summary = %q", summary)
	}
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "pastes is off") {
		t.Errorf("warnings = %q", warnings)
	}
	if summary, warnings := Notes(expand.Rules{}); summary != "no rules" || warnings != nil {
		t.Errorf("no rules: %q, %q", summary, warnings)
	}
}

func TestParseRejects(t *testing.T) {
	for _, tc := range []struct{ file, want string }{
		{"never ad: prod", `line 1: "never ad: prod" isn't a rule`},
		{"\n\ndeploy, rollback", "line 3:"},
		{"turn off: questoin", `line 1: no guard "questoin"`},
		{"never add:", "line 1: never add: names nothing"},
		{"never add: , ,", "names nothing"},
		{"always keep: main.go", `"main.go" isn't a word or phrase`},
	} {
		if _, err := Parse(tc.file); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err = %v, want %q", tc.file, err, tc.want)
		}
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), expand.GuardsFile)
	if r, err := Load(path); err != nil || !reflect.DeepEqual(r, expand.Rules{}) {
		t.Errorf("no file: %v, %+v", err, r)
	}
	if err := os.WriteFile(path, []byte("never add: prod\nturn off: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), path+" line 2") {
		t.Errorf("invalid file: err = %v", err)
	}
	if !reflect.DeepEqual(r, expand.Rules{}) {
		t.Errorf("invalid file: rules %+v", r)
	}
}
