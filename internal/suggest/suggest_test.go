package suggest

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFakeSuggest(t *testing.T) {
	cases := []struct {
		text   string
		cursor int
		want   string
	}{
		{"please impl", 11, "ement"},
		{"Refa", 4, "ctor"}, // case-insensitive match
		{"write te", 8, "sts"},
		{"i", 1, ""},                // last word too short
		{"tests", 5, ""},            // not a proper prefix
		{"impl now", 4, ""},         // cursor not at end
		{"explain ", 8, ""},         // trailing space: empty last word
		{"café perf", 9, "ormance"}, // cursor counts runes
		{"zzz", 3, ""},              // unknown word
	}
	for _, c := range cases {
		got, err := Fake{}.Suggest(context.Background(), c.text, c.cursor)
		if err != nil || got != c.want {
			t.Errorf("Suggest(%q, %d) = %q, %v; want %q", c.text, c.cursor, got, err, c.want)
		}
	}
}

func TestFakeDelayHonorsCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Fake{Delay: time.Second}.Suggest(ctx, "impl", 4)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("Suggest ignored cancellation")
	}
}
