// Package input decodes raw terminal input into keys and mirrors the line the
// user is editing in the wrapped CLI's prompt.
package input

import (
	"bytes"
	"unicode/utf8"
)

// KeyType identifies a decoded key.
type KeyType int

const (
	KeyUnknown KeyType = iota // unrecognized sequence; Raw holds its bytes
	KeyRunes                  // one or more printable characters
	KeyPaste                  // bracketed paste; Runes holds the content
	KeyEnter                  // CR
	KeyNewline                // LF, Alt+Enter, or Shift+Enter (CSI u)
	KeyTab
	KeyBackspace
	KeyDelete
	KeyEscape
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyCtrl // Ctrl+<letter>; Ctrl holds 'a'..'z'
)

// Key is one decoded keypress (or a run of typed characters).
type Key struct {
	Type  KeyType
	Runes []rune // KeyRunes, KeyPaste
	Ctrl  byte   // KeyCtrl
	// Raw is the exact input that produced this key, so a filter can
	// forward it unchanged. It is only valid until the next Decode call.
	Raw []byte
}

const esc = 0x1b

var (
	pasteStart = []byte("\x1b[200~")
	pasteEnd   = []byte("\x1b[201~")
)

// Decoder turns a stream of input chunks into keys. Sequences split across
// chunks are held until complete. An ESC at the very end of a chunk is taken
// as the Escape key: terminals write escape sequences in a single write, so a
// lone trailing ESC is almost always the key itself.
type Decoder struct {
	pending []byte
	paste   []byte
	inPaste bool
}

// Decode consumes p and returns the keys completed so far.
func (d *Decoder) Decode(p []byte) []Key {
	buf := p
	if len(d.pending) > 0 {
		buf = append(d.pending, p...)
		d.pending = nil
	}

	var keys []Key
	for len(buf) > 0 {
		if d.inPaste {
			i := bytes.Index(buf, pasteEnd)
			if i < 0 {
				keep := partialSuffix(buf, pasteEnd)
				d.paste = append(d.paste, buf[:len(buf)-keep]...)
				d.pending = append(d.pending, buf[len(buf)-keep:]...)
				break
			}
			d.paste = append(d.paste, buf[:i]...)
			raw := make([]byte, 0, len(pasteStart)+len(d.paste)+len(pasteEnd))
			raw = append(append(append(raw, pasteStart...), d.paste...), pasteEnd...)
			keys = append(keys, Key{Type: KeyPaste, Runes: []rune(string(d.paste)), Raw: raw})
			d.paste, d.inPaste = nil, false
			buf = buf[i+len(pasteEnd):]
			continue
		}

		k, n := decodeOne(buf)
		if n == 0 {
			d.pending = append(d.pending, buf...)
			break
		}
		k.Raw = buf[:n]
		if bytes.Equal(k.Raw, pasteStart) {
			d.inPaste = true
		} else {
			keys = append(keys, k)
		}
		buf = buf[n:]
	}
	return keys
}

// decodeOne decodes the key at the start of b, returning n == 0 if b holds
// only an incomplete sequence.
func decodeOne(b []byte) (Key, int) {
	switch c := b[0]; {
	case c == esc:
		return decodeEscape(b)
	case c == '\r':
		return Key{Type: KeyEnter}, 1
	case c == '\n':
		return Key{Type: KeyNewline}, 1
	case c == '\t':
		return Key{Type: KeyTab}, 1
	case c == 0x7f || c == 0x08:
		return Key{Type: KeyBackspace}, 1
	case c >= 0x01 && c <= 0x1a:
		return Key{Type: KeyCtrl, Ctrl: 'a' + c - 1}, 1
	case c < 0x20:
		return Key{Type: KeyUnknown}, 1
	}
	return decodeRunes(b)
}

// decodeRunes collects a run of printable UTF-8 characters.
func decodeRunes(b []byte) (Key, int) {
	var runes []rune
	n := 0
	for n < len(b) && b[n] >= 0x20 && b[n] != 0x7f {
		if !utf8.FullRune(b[n:]) {
			break
		}
		r, size := utf8.DecodeRune(b[n:])
		if r == utf8.RuneError && size == 1 {
			if n == 0 {
				return Key{Type: KeyUnknown}, 1
			}
			break
		}
		runes = append(runes, r)
		n += size
	}
	if n == 0 {
		return Key{}, 0 // incomplete UTF-8
	}
	return Key{Type: KeyRunes, Runes: runes}, n
}

func decodeEscape(b []byte) (Key, int) {
	if len(b) == 1 {
		return Key{Type: KeyEscape}, 1
	}
	switch b[1] {
	case '[':
		return decodeCSI(b)
	case 'O':
		if len(b) < 3 {
			return Key{}, 0
		}
		if t, ok := ss3Keys[b[2]]; ok {
			return Key{Type: t}, 3
		}
		return Key{Type: KeyUnknown}, 3
	case '\r':
		return Key{Type: KeyNewline}, 2
	case esc:
		return Key{Type: KeyEscape}, 1
	}
	// Alt+<char>: ESC followed by one character.
	if !utf8.FullRune(b[1:]) {
		return Key{}, 0
	}
	_, size := utf8.DecodeRune(b[1:])
	return Key{Type: KeyUnknown}, 1 + size
}

var ss3Keys = map[byte]KeyType{
	'A': KeyUp, 'B': KeyDown, 'C': KeyRight, 'D': KeyLeft, 'H': KeyHome, 'F': KeyEnd,
}

var csiKeys = map[string]KeyType{
	"A": KeyUp, "B": KeyDown, "C": KeyRight, "D": KeyLeft, "H": KeyHome, "F": KeyEnd,
	"1A": KeyUp, "1B": KeyDown, "1C": KeyRight, "1D": KeyLeft, "1H": KeyHome, "1F": KeyEnd,
	"1~": KeyHome, "7~": KeyHome, "4~": KeyEnd, "8~": KeyEnd, "3~": KeyDelete,
	"13;2u": KeyNewline, // Shift+Enter (kitty / fixterms)
}

// decodeCSI decodes ESC [ <params> <intermediates> <final>.
func decodeCSI(b []byte) (Key, int) {
	i := 2
	for i < len(b) && b[i] >= 0x20 && b[i] <= 0x3f {
		i++
	}
	if i == len(b) {
		return Key{}, 0
	}
	if b[i] < 0x40 || b[i] > 0x7e {
		return Key{Type: KeyUnknown}, i // malformed: drop the prefix
	}
	n := i + 1
	if t, ok := csiKeys[string(b[2:n])]; ok {
		return Key{Type: t}, n
	}
	return Key{Type: KeyUnknown}, n
}

// partialSuffix returns the length of the longest suffix of b that is a
// proper prefix of marker.
func partialSuffix(b, marker []byte) int {
	for k := min(len(marker)-1, len(b)); k > 0; k-- {
		if bytes.HasSuffix(b, marker[:k]) {
			return k
		}
	}
	return 0
}
