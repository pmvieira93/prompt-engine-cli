// Package guards reads the guard rails file (PRD Appendix 4, FR-10):
// changes to the built-in guards' word lists, thresholds and switches,
// and guards of one's own.
package guards

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/pmvieira93/prompt-engine-cli/internal/config"
	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
)

// FileName is the guard rails file, beside config.yaml.
const FileName = "guards.yaml"

// Path returns where the guard rails file is: beside the config file,
// wherever that is ($PROMPT_ENGINE_CONFIG, --pe-config).
func Path() (string, error) {
	cfg, err := config.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(cfg), FileName), nil
}

// Risky are the guards whose turning off gets a warning (FR-10.6): in
// auto mode, a rewrite they would stop then goes out unchecked.
var Risky = []string{"destructive", "pastes", "names"}

// words changes a word list: Add puts words in, Remove takes them out. A
// removed word that isn't in the list is ignored, so a later release that
// drops it doesn't break the file.
type words struct {
	Add    []string `yaml:"add"`
	Remove []string `yaml:"remove"`
}

type toggle struct {
	Enabled *bool `yaml:"enabled"`
}

type listGuard struct {
	toggle `yaml:",inline"`
	words  `yaml:",inline"`
}

type file struct {
	Empty          toggle    `yaml:"empty"`
	Placeholder    toggle    `yaml:"placeholder"`
	AskedBack      toggle    `yaml:"asked-back"`
	Language       toggle    `yaml:"language"`
	Names          toggle    `yaml:"names"`
	Quotes         toggle    `yaml:"quotes"`
	Destructive    listGuard `yaml:"destructive"`
	AddedTask      listGuard `yaml:"added-task"`
	DroppedPart    toggle    `yaml:"dropped-part"`
	Negation       listGuard `yaml:"negation"`
	EarlierPrompts toggle    `yaml:"earlier-prompts"`
	Pastes         toggle    `yaml:"pastes"`
	Question       struct {
		listGuard     `yaml:",inline"`
		QuestionWords words `yaml:"question_words"`
	} `yaml:"question"`
	Length struct {
		toggle    `yaml:",inline"`
		Factor    *float64 `yaml:"factor"`
		Allowance *int     `yaml:"allowance"`
	} `yaml:"length"`
	Replaced struct {
		toggle   `yaml:",inline"`
		MaxShare *float64 `yaml:"max_share"`
	} `yaml:"replaced"`
	PointsBack    words `yaml:"points_back"`
	PointsAtReply words `yaml:"points_at_reply"`
	Custom        []struct {
		Name  string   `yaml:"name"`
		Added []string `yaml:"added"`
		Kept  []string `yaml:"kept"`
	} `yaml:"custom"`
}

var customName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Load reads the guard rails file at path over the built-in rules. No
// file is not an error: the built-in rules are returned. On an error the
// built-in rules are returned too, so the caller can warn and keep what
// it had.
func Load(path string) (expand.Rules, error) {
	r := expand.DefaultRules()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, fmt.Errorf("read guards: %w", err)
	}
	got, err := Parse(data)
	if err != nil {
		return r, fmt.Errorf("invalid %s: %w", path, err)
	}
	return got, nil
}

// Parse reads a guard rails file's content over the built-in rules.
// Unknown keys (a misspelt guard) and invalid values are errors.
func Parse(data []byte) (expand.Rules, error) {
	r := expand.DefaultRules()
	var f file
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return r, err
	}

	r.Off = map[string]bool{}
	toggles := map[string]toggle{
		"empty": f.Empty, "placeholder": f.Placeholder, "asked-back": f.AskedBack, "language": f.Language,
		"names": f.Names, "quotes": f.Quotes, "destructive": f.Destructive.toggle, "question": f.Question.toggle,
		"added-task": f.AddedTask.toggle, "dropped-part": f.DroppedPart, "negation": f.Negation.toggle,
		"earlier-prompts": f.EarlierPrompts, "length": f.Length.toggle, "replaced": f.Replaced.toggle, "pastes": f.Pastes,
	}
	for name, t := range toggles {
		if t.Enabled != nil && !*t.Enabled {
			r.Off[name] = true
		}
	}

	lists := []struct {
		key  string
		list *[]string
		w    words
	}{
		{"destructive", &r.Destructive, f.Destructive.words},
		{"question", &r.Actions, f.Question.words},
		{"question.question_words", &r.QuestionWords, f.Question.QuestionWords},
		{"added-task", &r.Tasks, f.AddedTask.words},
		{"negation", &r.Negations, f.Negation.words},
		{"points_back", &r.PointsBack, f.PointsBack},
		{"points_at_reply", &r.PointsAtReply, f.PointsAtReply},
	}
	for _, l := range lists {
		next, err := apply(*l.list, l.w)
		if err != nil {
			return expand.DefaultRules(), fmt.Errorf("%s: %w", l.key, err)
		}
		*l.list = next
	}

	if v := f.Length.Factor; v != nil {
		if *v < 1 || *v > 100 {
			return expand.DefaultRules(), fmt.Errorf("length.factor: %g is not between 1 and 100", *v)
		}
		r.LengthFactor = *v
	}
	if v := f.Length.Allowance; v != nil {
		if *v < 0 || *v > 1000 {
			return expand.DefaultRules(), fmt.Errorf("length.allowance: %d is not between 0 and 1000", *v)
		}
		r.LengthAllowance = *v
	}
	if v := f.Replaced.MaxShare; v != nil {
		if *v <= 0 || *v > 1 {
			return expand.DefaultRules(), fmt.Errorf("replaced.max_share: %g is not above 0 and at most 1", *v)
		}
		r.MaxRemoved = *v
	}

	for i, c := range f.Custom {
		where := fmt.Sprintf("custom[%d]", i)
		switch {
		case !customName.MatchString(c.Name):
			return expand.DefaultRules(), fmt.Errorf("%s.name: %q must be lower case letters, digits and hyphens", where, c.Name)
		case slices.Contains(expand.Guards, c.Name):
			return expand.DefaultRules(), fmt.Errorf("%s.name: %q is a built-in guard", where, c.Name)
		case slices.ContainsFunc(r.Custom, func(o expand.Custom) bool { return o.Name == c.Name }):
			return expand.DefaultRules(), fmt.Errorf("%s.name: %q is used twice", where, c.Name)
		case len(c.Added) == 0 && len(c.Kept) == 0:
			return expand.DefaultRules(), fmt.Errorf("%s (%s): needs added or kept words", where, c.Name)
		}
		added, err := apply(nil, words{Add: c.Added})
		if err != nil {
			return expand.DefaultRules(), fmt.Errorf("%s (%s).added: %w", where, c.Name, err)
		}
		kept, err := apply(nil, words{Add: c.Kept})
		if err != nil {
			return expand.DefaultRules(), fmt.Errorf("%s (%s).kept: %w", where, c.Name, err)
		}
		r.Custom = append(r.Custom, expand.Custom{Name: c.Name, Added: added, Kept: kept})
	}
	return r, nil
}

// apply returns list with w's changes, its words in lower case.
func apply(list []string, w words) ([]string, error) {
	out := slices.Clone(list)
	check := func(s string) (string, error) {
		s = strings.ToLower(strings.TrimSpace(s))
		if !expand.IsWord(s) {
			return "", fmt.Errorf("%q is not one word", s)
		}
		return s, nil
	}
	var removed []string
	for _, s := range w.Remove {
		s, err := check(s)
		if err != nil {
			return nil, err
		}
		removed = append(removed, s)
		out = slices.DeleteFunc(out, func(x string) bool { return x == s })
	}
	for _, s := range w.Add {
		s, err := check(s)
		if err != nil {
			return nil, err
		}
		if slices.Contains(removed, s) {
			return nil, fmt.Errorf("%q is both added and removed", s)
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out, nil
}

// Notes describes the rules for the side panel's Session section and the
// log (FR-10.6): what is turned off or loosened, with a warning for the
// guards in Risky, and the custom guards. Nil for the built-in rules.
func Notes(r expand.Rules) (weakened []string, warnings []string, custom []string) {
	weakened = r.Weakened(expand.DefaultRules())
	for _, g := range Risky {
		if r.Off[g] {
			warnings = append(warnings, fmt.Sprintf("%s is off: in auto mode, a rewrite it would stop is sent unchecked", g))
		}
	}
	for _, c := range r.Custom {
		custom = append(custom, c.Name)
	}
	return weakened, warnings, custom
}
