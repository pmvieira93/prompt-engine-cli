package diff

import (
	"strings"
	"testing"
)

// reassemble rebuilds both texts from ops, which must give them back exactly.
func reassemble(ops []Op) (old, new string) {
	var o, n strings.Builder
	for _, op := range ops {
		if op.Kind != Insert {
			o.WriteString(op.Text)
		}
		if op.Kind != Delete {
			n.WriteString(op.Text)
		}
	}
	return o.String(), n.String()
}

func TestWords(t *testing.T) {
	cases := []struct {
		old, new string
		want     []Op
	}{
		{"fix teh bug", "fix the bug", []Op{{Equal, "fix "}, {Delete, "teh"}, {Insert, "the"}, {Equal, " bug"}}},
		{"say hi", "say hi\n\n- End with MANGO.", []Op{{Equal, "say hi"}, {Insert, "\n\n- End with MANGO."}}},
		{"same", "same", []Op{{Equal, "same"}}},
		{"", "new", []Op{{Insert, "new"}}},
		{"gone", "", []Op{{Delete, "gone"}}},
	}
	for _, tc := range cases {
		got := Words(tc.old, tc.new)
		if len(got) != len(tc.want) {
			t.Errorf("Words(%q, %q) = %+v, want %+v", tc.old, tc.new, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("Words(%q, %q)[%d] = %+v, want %+v", tc.old, tc.new, i, got[i], tc.want[i])
			}
		}
	}
}

func TestWordsRoundTrips(t *testing.T) {
	pairs := [][2]string{
		{"fix teh failing tst in config", "Fix the failing test in the config.\n\nContext:\n- Branch: main"},
		{"café 👍🏽  two  spaces nbsp", "café 👍 two spaces nbsp!"},
		{"a b c d e", "e d c b a"},
	}
	for _, p := range pairs {
		ops := Words(p[0], p[1])
		if o, n := reassemble(ops); o != p[0] || n != p[1] {
			t.Errorf("round trip of %q -> %q gave %q -> %q", p[0], p[1], o, n)
		}
	}
}

func TestCounts(t *testing.T) {
	added, removed := Counts(Words("fix teh bug", "Fix the bug.\n\nContext:\n- Branch: main"))
	if added != 7 || removed != 3 { // nothing matches: case and punctuation differ; "-" is a word
		t.Fatalf("added %d, removed %d", added, removed)
	}
}

func TestWordsHugeFallsBack(t *testing.T) {
	old := strings.Repeat("a ", 2100)
	new := strings.Repeat("b ", 2100)
	ops := Words(old, new)
	if o, n := reassemble(ops); o != old || n != new || len(ops) != 2 {
		t.Fatalf("got %d ops", len(ops))
	}
}

func TestPrompt(t *testing.T) {
	// Seen in testing: the dropped sentence lined up with the context.
	old := "keep it short;\nthis is long enough to wrap"
	rewrite := "Keep it short.\n\nContext:\n- Branch: main"
	got := Prompt(old, rewrite)
	last := got[len(got)-1]
	if last.Kind != Insert || !strings.HasSuffix(last.Text, "\n\nContext:\n- Branch: main") {
		t.Fatalf("context not added whole, at the end: %+v", got)
	}
	for _, op := range got[:len(got)-1] {
		if strings.Contains(op.Text, "Branch") || strings.Contains(op.Text, "Context") {
			t.Fatalf("context mixed into the words: %+v", got)
		}
	}
	var o, n strings.Builder // the ops still rebuild both texts
	for _, op := range got {
		if op.Kind != Insert {
			o.WriteString(op.Text)
		}
		if op.Kind != Delete {
			n.WriteString(op.Text)
		}
	}
	if o.String() != old || n.String() != rewrite {
		t.Fatalf("ops rebuild %q / %q", o.String(), n.String())
	}
	// The same context on both sides is unchanged.
	same := "Fix it.\n\nContext:\n- Branch: main"
	if got := Prompt(same, same); len(got) != 1 || got[0].Kind != Equal {
		t.Fatalf("unchanged prompt: %+v", got)
	}
}
