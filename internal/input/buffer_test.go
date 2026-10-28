package input

import (
	"strings"
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
		{"skin-tone emoji", "ok 👍🏽\x7f", "ok ", 3},
		{"variation selector", "love ❤️\x7f", "love ", 5},
		{"flag", "pt 🇵🇹\x7f", "pt ", 3},
		{"family ZWJ", "hi 👨‍👩‍👧\x7f", "hi ", 3},
		{"combining accent", "cafe\u0301\x7f", "caf", 3},
		{"move over emoji", "a👍🏽b\x1b[D\x1b[D\x1b[3~", "ab", 1},
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

func TestBufferIgnoresTerminalReplies(t *testing.T) {
	b := NewBuffer()
	var d Decoder
	// Reply split across reads, interleaved with typing and focus events.
	for _, chunk := range []string{"\x1bP>|iTerm2 3.", "7.1\x1b\\\x1b_Gi=31;OK\x1b\\fix", "\x1b[O\x1b[I teh"} {
		for _, k := range d.Decode([]byte(chunk)) {
			b.Apply(k)
		}
	}
	if b.Text() != "fix teh" || !b.Synced() {
		t.Fatalf("got (%q, synced=%v), want (\"fix teh\", true)", b.Text(), b.Synced())
	}
}

// TestBufferKittyEditing replays test-guide steps C1-C7 as iTerm2 sends them
// once claude enables the kitty keyboard protocol.
func TestBufferKittyEditing(t *testing.T) {
	b := NewBuffer()
	steps := []struct {
		in, text string
		cursor   int
	}{
		{"fix teh", "fix teh", 7},
		{"\x7f\x7fhe bug", "fix the bug", 11},
		{"\x1b[D\x1b[D\x1b[D", "fix the bug", 8},
		{"nasty ", "fix the nasty bug", 14},
		{"\x1b[97;5u", "fix the nasty bug", 0},
		{"\x1b[101;5u", "fix the nasty bug", 17},
		{"\x1b[D\x1b[D\x1b[D\x1b[119;5u", "fix the bug", 8},
		{"\x1b[117;5u", "bug", 0},
	}
	for _, s := range steps {
		feed(b, s.in)
		if b.Text() != s.text || b.Cursor() != s.cursor || !b.Synced() {
			t.Fatalf("after %q: got (%q, %d, synced=%v), want (%q, %d)",
				s.in, b.Text(), b.Cursor(), b.Synced(), s.text, s.cursor)
		}
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

func TestBufferRecordsPastes(t *testing.T) {
	b := NewBuffer()
	feed(b, "see: \x1b[200~a\r\nb\x1b[201~ and \x1b[200~c\x1b[201~")
	if got := strings.Join(b.Pastes(), "|"); got != "a\nb|c" {
		t.Fatalf("pastes = %q, want %q", got, "a\nb|c")
	}
	feed(b, "\x03")
	if len(b.Pastes()) != 0 {
		t.Fatalf("pastes kept after Ctrl+C: %q", b.Pastes())
	}
	feed(b, "\x1b[200~x\x1b[201~\r")
	if len(b.Pastes()) != 0 {
		t.Fatalf("pastes kept after submit: %q", b.Pastes())
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

func TestTrackerResync(t *testing.T) {
	tr := NewTracker()
	tr.Filter([]byte("abc\x1b[A"))
	s := tr.Snapshot()
	if s.Synced {
		t.Fatal("expected desync")
	}
	tr.Filter([]byte("x")) // newer input than the screen read
	if tr.Resync(s.Rev, "refa", 4) {
		t.Fatal("resync applied over newer input")
	}
	s = tr.Snapshot()
	if !tr.Resync(s.Rev, "refa", 9) {
		t.Fatal("resync not applied")
	}
	if s := tr.Snapshot(); s.Text != "refa" || s.Cursor != 4 || !s.Synced {
		t.Fatalf("snapshot = %+v (cursor should clamp to 4)", s)
	}
	if tr.Resync(tr.Snapshot().Rev, "refa", 4) {
		t.Error("identical resync reported a change")
	}
}

func TestEnterAfterMentionSubmits(t *testing.T) {
	// In claude, Enter with the @ menu open submits the text as typed;
	// Tab is what picks a file.
	for _, text := range []string{"look at @READ", "/help", "look at @README.md "} {
		b := NewBuffer()
		if got := feed(b, text+"\r"); got != text || b.Text() != "" || !b.Synced() {
			t.Errorf("%q: submitted %q, then text %q synced=%v", text, got, b.Text(), b.Synced())
		}
	}
}

func TestCompletionMenuMayBeOpen(t *testing.T) {
	cases := map[string]bool{
		"look at @READ":        true,
		"@inter":               true,
		"/hel":                 true,
		"/compact":             true,
		"look at @README.md ":  false,
		"/compact focus":       false,
		"see /usr/bin":         false,
		"email me@example.com": false,
		"plain text":           false,
		"":                     false,
		"two lines\n@x":        true,
		"done @x\n":            false,
	}
	for text, want := range cases {
		if got := CompletionMenuMayBeOpen(text); got != want {
			t.Errorf("CompletionMenuMayBeOpen(%q) = %v, want %v", text, got, want)
		}
	}
}
