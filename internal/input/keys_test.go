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
	}
	for _, c := range cases {
		var d Decoder
		if got := types(d.Decode([]byte(c.in))); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Decode(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestDecodeRawRoundTrips(t *testing.T) {
	in := "ab\x1b[A\x1b[200~x\ry\x1b[201~\x1bOH\r"
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
