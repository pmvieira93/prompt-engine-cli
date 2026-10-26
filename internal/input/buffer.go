package input

import "unicode"

// Buffer mirrors the text in the wrapped CLI's prompt box by replaying the
// keys the user sends it. It cannot see the child's screen, so any key whose
// effect it can't predict (history navigation, autocomplete, unknown
// sequences) marks the buffer as out of sync until the next submit or clear.
type Buffer struct {
	runes  []rune
	cursor int
	synced bool
	rev    uint64
}

// NewBuffer returns an empty, in-sync buffer.
func NewBuffer() *Buffer { return &Buffer{synced: true} }

// Text returns the current prompt text.
func (b *Buffer) Text() string { return string(b.runes) }

// Cursor returns the cursor position in runes.
func (b *Buffer) Cursor() int { return b.cursor }

// Synced reports whether the buffer is believed to match the child's prompt.
func (b *Buffer) Synced() bool { return b.synced }

// Rev increments on every change to text, cursor, or sync state.
func (b *Buffer) Rev() uint64 { return b.rev }

// Apply updates the buffer for k. On Enter it returns the submitted text and
// true, and the buffer resets.
func (b *Buffer) Apply(k Key) (submitted string, ok bool) {
	switch k.Type {
	case KeyRunes:
		b.insert(k.Runes)
	case KeyPaste:
		b.insert(normalizeNewlines(k.Runes))
	case KeyNewline:
		b.insert([]rune{'\n'})
	case KeyEnter:
		submitted = b.Text()
		b.reset()
		return submitted, true
	case KeyBackspace:
		b.deleteRange(b.cursor-1, b.cursor)
	case KeyDelete:
		b.deleteRange(b.cursor, b.cursor+1)
	case KeyLeft:
		b.moveTo(b.cursor - 1)
	case KeyRight:
		b.moveTo(b.cursor + 1)
	case KeyHome:
		b.moveTo(b.lineStart())
	case KeyEnd:
		b.moveTo(b.lineEnd())
	case KeyEscape:
		// Interrupts the agent in Claude Code; the prompt text is kept.
	case KeyReport:
		// Terminal replies reach the child but never touch the prompt.
	case KeyCtrl:
		b.applyCtrl(k.Ctrl)
	default:
		// Up/Down (history), Tab (autocomplete), and unknown sequences
		// change the prompt in ways we can't replay.
		b.desync()
	}
	return "", false
}

func (b *Buffer) applyCtrl(c byte) {
	switch c {
	case 'a':
		b.moveTo(b.lineStart())
	case 'e':
		b.moveTo(b.lineEnd())
	case 'b':
		b.moveTo(b.cursor - 1)
	case 'f':
		b.moveTo(b.cursor + 1)
	case 'd':
		b.deleteRange(b.cursor, b.cursor+1)
	case 'h':
		b.deleteRange(b.cursor-1, b.cursor)
	case 'k':
		b.deleteRange(b.cursor, b.lineEnd())
	case 'u':
		b.deleteRange(b.lineStart(), b.cursor)
	case 'w':
		b.deleteRange(b.wordStart(), b.cursor)
	case 'c':
		b.reset() // clears the prompt in Claude Code
	default:
		b.desync()
	}
}

func (b *Buffer) insert(rs []rune) {
	if len(rs) == 0 {
		return
	}
	b.runes = append(b.runes[:b.cursor], append(rs, b.runes[b.cursor:]...)...)
	b.cursor += len(rs)
	b.rev++
}

func (b *Buffer) deleteRange(from, to int) {
	from, to = max(from, 0), min(to, len(b.runes))
	if from >= to {
		return
	}
	b.runes = append(b.runes[:from], b.runes[to:]...)
	b.cursor = from
	b.rev++
}

func (b *Buffer) moveTo(pos int) {
	pos = min(max(pos, 0), len(b.runes))
	if pos != b.cursor {
		b.cursor = pos
		b.rev++
	}
}

func (b *Buffer) reset() {
	if len(b.runes) == 0 && b.cursor == 0 && b.synced {
		return
	}
	b.runes, b.cursor, b.synced = b.runes[:0], 0, true
	b.rev++
}

func (b *Buffer) desync() {
	if b.synced {
		b.synced = false
		b.rev++
	}
}

func (b *Buffer) lineStart() int {
	i := b.cursor
	for i > 0 && b.runes[i-1] != '\n' {
		i--
	}
	return i
}

func (b *Buffer) lineEnd() int {
	i := b.cursor
	for i < len(b.runes) && b.runes[i] != '\n' {
		i++
	}
	return i
}

// wordStart finds the Ctrl+W boundary: skip spaces, then non-spaces.
func (b *Buffer) wordStart() int {
	i := b.cursor
	for i > 0 && unicode.IsSpace(b.runes[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(b.runes[i-1]) {
		i--
	}
	return i
}

// normalizeNewlines converts CRLF and CR in pasted text to LF.
func normalizeNewlines(rs []rune) []rune {
	out := make([]rune, 0, len(rs))
	for i, r := range rs {
		if r == '\r' {
			if i+1 < len(rs) && rs[i+1] == '\n' {
				continue
			}
			r = '\n'
		}
		out = append(out, r)
	}
	return out
}
