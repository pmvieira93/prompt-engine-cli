package input

import (
	"reflect"
	"testing"
)

func types(keys []Key) []KeyType {
	out := make([]KeyType, len(keys))
	for i, k := range keys {
		out[i] = k.Type
	}
	return out
}

func TestDecodeSingleChunk(t *testing.T) {
	cases := []struct {
		in   string
		want []KeyType
	}{
		{"héllo", []KeyType{KeyRunes}},
		{"a\rb", []KeyType{KeyRunes, KeyEnter, KeyRunes}},
		{"\x7f\t\x03", []KeyType{KeyBackspace, KeyTab, KeyCtrl}},
		{"\x1b[A\x1b[B\x1b[C\x1b[D", []KeyType{KeyUp, KeyDown, KeyRight, KeyLeft}},
		{"\x1bOH\x1b[F\x1b[3~", []KeyType{KeyHome, KeyEnd, KeyDelete}},
		{"\x1b", []KeyType{KeyEscape}},
		{"\x1b\x1b[A", []KeyType{KeyEscape, KeyUp}},
		{"\x1b\r\x1b[13;2u\n", []KeyType{KeyNewline, KeyNewline, KeyNewline}},
		{"\x1bb\x1b[1;5C", []KeyType{KeyUnknown, KeyUnknown}},
		// Terminal replies seen from iTerm2 at claude startup.
		{"\x1bP>|iTerm2 3.7.1\x1b\\\x1b_Gi=31;OK\x1b\\", []KeyType{KeyReport, KeyReport}},
		{"\x1b]11;rgb:0000/0000/0000\x07", []KeyType{KeyReport}},
		{"\x1b[?62;4c\x1b[?2026;2$y\x1b[12;40R", []KeyType{KeyReport, KeyReport, KeyReport}},
		{"\x1b[I\x1b[O", []KeyType{KeyReport, KeyReport}},
		{"\x1bP", []KeyType{KeyUnknown}}, // Alt+Shift+P typed alone
		// Kitty keyboard protocol and modifyOtherKeys (enabled by claude).
		{"\x1b[97;5u\x1b[27;5;101~", []KeyType{KeyCtrl, KeyCtrl}},
		{"\x1b[27u\x1b[13u\x1b[13;2u\x1b[9u\x1b[127u", []KeyType{KeyEscape, KeyEnter, KeyNewline, KeyTab, KeyBackspace}},
		{"\x1b[97;69u", []KeyType{KeyCtrl}},                          // Ctrl+A with Caps Lock on
		{"\x1b[97;5:3u", []KeyType{KeyReport}},                       // key release event
		{"\x1b[97;7u\x1b[57399u", []KeyType{KeyUnknown, KeyUnknown}}, // Ctrl+Alt+A, keypad 0
		{"\x1d\x1c\x1b[93;5u", []KeyType{KeyCtrl, KeyCtrl, KeyCtrl}}, // Ctrl+] Ctrl+\\ (legacy, kitty)
	}
	for _, c := range cases {
		var d Decoder
		if got := types(d.Decode([]byte(c.in))); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Decode(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestDecodeModifiedKeys(t *testing.T) {
	var d Decoder
	keys := d.Decode([]byte("\x1b[119;5u\x1b[97:65;2u\x1b[233u"))
	if len(keys) != 3 {
		t.Fatalf("got %d keys", len(keys))
	}
	if keys[0].Ctrl != 'w' {
		t.Errorf("ctrl = %q, want 'w'", keys[0].Ctrl)
	}
	if string(keys[1].Runes) != "A" || string(keys[2].Runes) != "é" {
		t.Errorf("runes = %q, %q", string(keys[1].Runes), string(keys[2].Runes))
	}
}

func TestDecodeRawRoundTrips(t *testing.T) {
	in := "ab\x1b[A\x1b[200~x\ry\x1b[201~\x1bOH\x1bP>|t\x1b\\\x1b[I\r"
	var d Decoder
	var raw []byte
	for _, k := range d.Decode([]byte(in)) {
		raw = append(raw, k.Raw...)
	}
	if string(raw) != in {
		t.Fatalf("raw = %q, want %q", raw, in)
	}
}

func TestDecodeSplitAcrossChunks(t *testing.T) {
	var d Decoder
	var keys []Key
	// "é" split mid-rune, CSI split mid-sequence, paste end marker split.
	for _, chunk := range []string{"\xc3", "\xa9\x1b[", "D\x1b[200~line1\r\nli", "ne2\x1b[2", "01~!"} {
		keys = append(keys, d.Decode([]byte(chunk))...)
	}
	want := []KeyType{KeyRunes, KeyLeft, KeyPaste, KeyRunes}
	if got := types(keys); !reflect.DeepEqual(got, want) {
		t.Fatalf("types = %v, want %v", got, want)
	}
	if string(keys[0].Runes) != "é" {
		t.Errorf("runes = %q", string(keys[0].Runes))
	}
	if string(keys[2].Runes) != "line1\r\nline2" {
		t.Errorf("paste = %q", string(keys[2].Runes))
	}
}
