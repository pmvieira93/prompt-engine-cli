package input

import "testing"

func TestParseBinding(t *testing.T) {
	for _, s := range []string{"ctrl+]", "Ctrl+A", "ctrl+\\", "p", "?", "é"} {
		b, err := ParseBinding(s)
		if err != nil || b.IsZero() {
			t.Errorf("ParseBinding(%q) = %v, %v", s, b, err)
		}
	}
	for _, s := range []string{"", "ctrl+", "ctrl+1", "ctrl+ab", "alt+p", "pp", " "} {
		if _, err := ParseBinding(s); err == nil {
			t.Errorf("ParseBinding(%q) succeeded", s)
		}
	}
}

func TestBindingMatches(t *testing.T) {
	leader, _ := ParseBinding("ctrl+]")
	panel, _ := ParseBinding("p")
	var d Decoder
	keys := d.Decode([]byte("\x1d\x1b[93;5u\x1b[112;5up"))
	want := []bool{true, true, false, true}
	for i, k := range keys {
		if got := leader.Matches(k) || panel.Matches(k); got != want[i] {
			t.Errorf("key %d (%s): matched = %v, want %v", i, k, got, want[i])
		}
	}
	if !panel.Matches(keys[3]) {
		t.Error("plain p should match the panel binding")
	}
}
