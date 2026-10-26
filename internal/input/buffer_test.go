package input

import (
	"testing"
	"time"
)

// feed decodes in as one chunk and applies every key, returning the last
// submitted text.
func feed(b *Buffer, in string) (submitted string) {
	var d Decoder
	for _, k := range d.Decode([]byte(in)) {
		if s, ok := b.Apply(k); ok {
			submitted = s
		}
	}
	return submitted
}

func TestBufferEditing(t *testing.T) {
	cases := []struct {
		name, in, text string
		cursor         int
	}{
		{"typing", "hello", "hello", 5},
		{"backspace", "helo\x7f\x7fllo", "hello", 5},
		{"insert mid-line", "hllo\x1b[D\x1b[D\x1b[D" + "e", "hello", 2},
		{"delete forward", "hxello\x1b[H\x1b[C\x1b[3~", "hello", 1},
		{"ctrl+u", "foo bar\x15baz", "baz", 3},
		{"ctrl+w", "fix the  bug\x17", "fix the  ", 9},
		{"ctrl+k", "hello world\x01\x06\x06\x06\x06\x06\x0b", "hello", 5},
		{"cursor clamps", "\x1b[D\x7fab\x1b[C\x1b[C", "ab", 2},
		{"multi-line home/end", "one\ntwo\x01X\x05Y", "one\nXtwoY", 9},
		{"paste normalizes CRLF", "\x1b[200~a\r\nb\rc\x1b[201~", "a\nb\nc", 5},
		{"unicode", "naïve 👍\x7f", "naïve ", 6},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := NewBuffer()
			feed(b, c.in)
			if b.Text() != c.text || b.Cursor() != c.cursor {
				t.Errorf("got (%q, %d), want (%q, %d)", b.Text(), b.Cursor(), c.text, c.cursor)
			}
			if !b.Synced() {
				t.Error("buffer unexpectedly out of sync")
			}
		})
	}
}

func TestBufferSubmitResets(t *testing.T) {
	b := NewBuffer()
	if got := feed(b, "line one\x1b\rline two\r"); got != "line one\nline two" {
		t.Fatalf("submitted %q", got)
	}
	if b.Text() != "" || b.Cursor() != 0 {
		t.Fatalf("buffer not reset: %q", b.Text())
	}
}

func TestBufferDesyncUntilReset(t *testing.T) {
	for _, key := range []string{"\x1b[A", "\t", "\x1bb", "\x12"} {
		b := NewBuffer()
		feed(b, "abc"+key)
		if b.Synced() {
			t.Errorf("%q: expected desync", key)
		}
		feed(b, "\x03")
		if !b.Synced() || b.Text() != "" {
			t.Errorf("%q: Ctrl+C did not resync", key)
		}
	}
}

func TestTrackerNotifiesOnChange(t *testing.T) {
	tr := NewTracker()
	if out := tr.Filter([]byte("hi")); string(out) != "hi" {
		t.Fatalf("filter modified input: %q", out)
	}
	select {
	case <-tr.Changed():
	case <-time.After(time.Second):
		t.Fatal("no change notification")
	}
	if s := tr.Snapshot(); s.Text != "hi" || s.Cursor != 2 || !s.Synced {
		t.Fatalf("snapshot = %+v", s)
	}

	tr.Filter([]byte("\x1b[H\x1b[H")) // first moves, second is a no-op
	<-tr.Changed()
	tr.Filter([]byte("\x1b[H"))
	select {
	case <-tr.Changed():
		t.Fatal("notified without a change")
	default:
	}
}
