package input

import (
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
)

// Buffer mirrors the text in the wrapped CLI's prompt box by replaying the
// keys the user sends it. It cannot see the child's screen, so any key whose
// effect it can't predict (history navigation, autocomplete, unknown
// sequences) marks the buffer as out of sync until the next submit or clear.
type Buffer struct {
	runes  []rune
	cursor int
	synced bool
	// slack: how many characters the text may lack, when it was read from
	// a prompt spanning rows on the screen (State.Slack).
	slack int
	rev   uint64
	// pastes holds what was pasted since the prompt was last empty, so a
	// rewrite can keep it exactly (PRD Appendix 1, FR-7.4e).
	pastes []string
}

// NewBuffer returns an empty, in-sync buffer.
func NewBuffer() *Buffer { return &Buffer{synced: true} }

// Text returns the current prompt text.
func (b *Buffer) Text() string { return string(b.runes) }

// Cursor returns the cursor position in runes.
func (b *Buffer) Cursor() int { return b.cursor }

// Synced reports whether the buffer is believed to match the child's prompt.
func (b *Buffer) Synced() bool { return b.synced }

// Slack is how many characters the text may lack (State.Slack).
func (b *Buffer) Slack() int {
	if len(b.runes) == 0 {
		return 0
	}
	return b.slack
}

func (b *Buffer) setSlack(n int) {
	if b.slack != n {
		b.slack = n
		b.rev++
	}
}

// Pastes returns the text of each paste since the prompt was last
// cleared, oldest first. Later edits may have changed or removed them.
func (b *Buffer) Pastes() []string { return b.pastes }

// Rev increments on every change to text, cursor, or sync state.
func (b *Buffer) Rev() uint64 { return b.rev }

// Apply updates the buffer for k. On Enter it returns the submitted text and
// true, and the buffer resets.
func (b *Buffer) Apply(k Key) (submitted string, ok bool) {
	switch k.Type {
	case KeyRunes:
		b.insert(k.Runes)
	case KeyPaste:
		rs := normalizeNewlines(k.Runes)
		b.insert(rs)
		if len(rs) > 0 {
			b.pastes = append(b.pastes, string(rs))
		}
	case KeyNewline:
		b.insert([]rune{'\n'})
	case KeyEnter:
		submitted = b.Text()
		b.reset()
		return submitted, true
	case KeyBackspace:
		b.deleteRange(b.prevGrapheme(), b.cursor)
	case KeyDelete:
		b.deleteRange(b.cursor, b.nextGrapheme())
	case KeyLeft:
		b.moveTo(b.prevGrapheme())
	case KeyRight:
		if len(b.runes) == 0 {
			// In an empty box, claude may be showing a suggested prompt
			// (after a reply), which → accepts, as Tab does (recorded
			// with claude 2.1.308; Ctrl+E and Ctrl+F don't): the prompt
			// is then whatever it suggested, read from the screen.
			b.desync()
			break
		}
		b.moveTo(b.nextGrapheme())
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
		b.moveTo(b.prevGrapheme())
	case 'f':
		b.moveTo(b.nextGrapheme())
	case 'd':
		b.deleteRange(b.cursor, b.nextGrapheme())
	case 'h':
		b.deleteRange(b.prevGrapheme(), b.cursor)
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
	if len(b.runes) == 0 {
		b.slack = 0 // what's typed next is known exactly
	}
	b.rev++
}

func (b *Buffer) moveTo(pos int) {
	pos = min(max(pos, 0), len(b.runes))
	if pos != b.cursor {
		b.cursor = pos
		b.rev++
	}
}

// Set replaces the buffer with text and the cursor at rune offset cursor
// (clamped), and marks it in sync: used when the child's actual prompt is
// known from its screen.
func (b *Buffer) Set(text string, cursor int) {
	rs := []rune(text)
	cursor = min(max(cursor, 0), len(rs))
	if b.synced && b.cursor == cursor && string(b.runes) == text {
		return
	}
	b.runes, b.cursor, b.synced = rs, cursor, true
	if len(rs) == 0 {
		b.pastes = nil
	}
	b.rev++
}

func (b *Buffer) reset() {
	if len(b.runes) == 0 && b.cursor == 0 && b.synced {
		return
	}
	b.runes, b.cursor, b.synced, b.pastes, b.slack = b.runes[:0], 0, true, nil, 0
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

// prevGrapheme returns the start of the user-perceived character before the
// cursor, so one Backspace removes a whole emoji like 👍🏽 or 🇵🇹 as the
// child's editor does.
func (b *Buffer) prevGrapheme() int {
	prev := 0
	for _, end := range b.graphemeEnds() {
		if end >= b.cursor {
			break
		}
		prev = end
	}
	return prev
}

// nextGrapheme returns the end of the user-perceived character after the cursor.
func (b *Buffer) nextGrapheme() int {
	for _, end := range b.graphemeEnds() {
		if end > b.cursor {
			return end
		}
	}
	return len(b.runes)
}

// graphemeEnds returns the rune offset at the end of each grapheme cluster.
func (b *Buffer) graphemeEnds() []int {
	var ends []int
	n := 0
	g := uniseg.NewGraphemes(string(b.runes))
	for g.Next() {
		n += len(g.Runes())
		ends = append(ends, n)
	}
	return ends
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

// CompletionMenuMayBeOpen reports whether claude may be showing a
// completion menu for text: the last word is an @-mention, or the text is
// a slash command, not yet followed by a space. Tab picks from that menu,
// so ghost text, which Tab would accept instead, must not compete with it.
// (Enter still submits.)
func CompletionMenuMayBeOpen(text string) bool {
	if strings.HasSuffix(text, " ") || strings.HasSuffix(text, "\n") {
		return false
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false
	}
	last := fields[len(fields)-1]
	return strings.HasPrefix(last, "@") || (len(fields) == 1 && strings.HasPrefix(last, "/"))
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
