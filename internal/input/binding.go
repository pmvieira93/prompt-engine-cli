package input

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Binding is a user-configurable key: either Ctrl+<char> or a single
// printable character. The zero Binding matches nothing.
type Binding struct {
	ctrl byte // 'a'..'z', '\\', ']', '^', '_'
	r    rune
}

// ParseBinding parses "ctrl+]", "ctrl+a", or a single character like "p".
func ParseBinding(s string) (Binding, error) {
	if rest, ok := strings.CutPrefix(strings.ToLower(s), "ctrl+"); ok {
		if len(rest) == 1 && isCtrlChar(rest[0]) {
			return Binding{ctrl: rest[0]}, nil
		}
		return Binding{}, fmt.Errorf("unsupported key %q: ctrl+ must be followed by a-z or one of \\ ] ^ _", s)
	}
	if r, size := utf8.DecodeRuneInString(s); size > 0 && size == len(s) && r >= 0x21 && r != 0x7f {
		return Binding{r: r}, nil
	}
	return Binding{}, fmt.Errorf("unsupported key %q: use ctrl+<key> or a single character", s)
}

func isCtrlChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || c == '\\' || c == ']' || c == '^' || c == '_'
}

// IsZero reports whether b is the zero Binding.
func (b Binding) IsZero() bool { return b == Binding{} }

// Matches reports whether k is this key. A run of typed characters matches a
// character binding when it starts with that character; see Rest.
func (b Binding) Matches(k Key) bool {
	switch {
	case b.ctrl != 0:
		return k.Type == KeyCtrl && k.Ctrl == b.ctrl
	case b.r != 0:
		return k.Type == KeyRunes && len(k.Runes) > 0 && k.Runes[0] == b.r
	}
	return false
}

// Rest returns the input left over after a matching character binding, for
// typed runs like "px" that arrive in a single read.
func (b Binding) Rest(k Key) []byte {
	if b.r != 0 && k.Type == KeyRunes && len(k.Runes) > 1 {
		return []byte(string(k.Runes[1:]))
	}
	return nil
}

// String renders b the way ParseBinding accepts it.
func (b Binding) String() string {
	switch {
	case b.ctrl != 0:
		return "ctrl+" + string(rune(b.ctrl))
	case b.r != 0:
		return string(b.r)
	}
	return ""
}
