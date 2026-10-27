package tui

import (
	"bytes"
	"fmt"
	"strconv"
)

// maxOSC bounds an OSC payload; a longer one is dropped.
const maxOSC = 1 << 20

// oscFilter takes OSC sequences (ESC ] payload, ended by BEL or ESC \) out
// of the child's output before it reaches the emulator. The emulator's
// parser ends an OSC string at any 0x9C byte, the 8-bit ST, even though in
// UTF-8 that byte is part of characters like "✳" (E2 9C B3). claude puts
// such characters in its window title, so the title was cut short and the
// rest of it was printed onto the screen, over claude's prompt.
//
// Each complete sequence goes to handle, which returns the bytes to pass on
// in its place (nil to drop it). State carries across writes, since a
// sequence can be split between them.
type oscFilter struct {
	handle func(payload []byte) []byte

	state    int
	payload  []byte
	overflow bool // the payload passed maxOSC; drop the sequence
}

const (
	oscGround     = iota
	oscEsc        // saw ESC outside a sequence
	oscPayload    // inside ESC ] ...
	oscPayloadEsc // saw ESC inside the payload: ST, or the end of it
)

// Filter returns p with OSC sequences replaced by what handle returns. It
// only copies when p contains a sequence.
func (f *oscFilter) Filter(p []byte) []byte {
	if f.state == oscGround && bytes.IndexByte(p, 0x1b) < 0 {
		return p // fast path: plain output
	}
	out := make([]byte, 0, len(p))
	for _, b := range p {
		out = f.step(out, b)
	}
	return out
}

func (f *oscFilter) step(out []byte, b byte) []byte {
	switch f.state {
	case oscGround:
		if b == 0x1b {
			f.state = oscEsc
			return out
		}
		return append(out, b)
	case oscEsc:
		switch b {
		case ']':
			f.state, f.payload, f.overflow = oscPayload, f.payload[:0], false
			return out
		case 0x1b:
			return append(out, 0x1b) // the first ESC wasn't an OSC; this one might be
		}
		f.state = oscGround
		return append(out, 0x1b, b)
	case oscPayload:
		switch {
		case b == 0x07:
			f.state = oscGround
			return f.dispatch(out)
		case b == 0x1b:
			f.state = oscPayloadEsc
		case len(f.payload) >= maxOSC:
			f.overflow = true
		default:
			f.payload = append(f.payload, b)
		}
		return out
	default: // oscPayloadEsc
		if b == '\\' {
			f.state = oscGround
			return f.dispatch(out)
		}
		// ESC without \ cancels the sequence and starts another.
		f.state = oscEsc
		return f.step(out, b)
	}
}

func (f *oscFilter) dispatch(out []byte) []byte {
	if f.overflow {
		return out
	}
	return append(out, f.handle(f.payload)...)
}

// handleOSC applies one OSC sequence from the child: titles directly,
// sequences only the real terminal can act on are forwarded to it intact,
// and the rest go on to the emulator where the 0x9C bug can't bite.
func (a *app) handleOSC(payload []byte) []byte {
	cmdStr, rest, _ := bytes.Cut(payload, []byte(";"))
	cmd, err := strconv.Atoi(string(cmdStr))
	if err != nil {
		return nil
	}
	seq := "\x1b]" + string(payload) + "\a"
	switch cmd {
	case 0, 2: // window title (and icon name)
		a.locked(func() { a.title = string(rest) })
		a.Wake()
		return nil
	case 1: // icon name only
		return nil
	case 7, 9, 52, 777: // cwd, notifications and progress, clipboard
		a.queueRaw(seq)
		return nil
	case 10, 11, 12, 110, 111, 112: // default colors
		// A query must be answered by the real terminal, not by the
		// emulator's built-in white-on-black, so theme detection works.
		// Sets and resets apply to both.
		a.queueRaw(seq)
		if bytes.Contains(payload, []byte(";?")) {
			return nil
		}
		return []byte(seq)
	case 8: // hyperlink: the emulator stores it on the cells
		if bytes.IndexByte(payload, 0x9c) >= 0 {
			payload = percentEncodeHigh(payload)
		}
		return []byte("\x1b]" + string(payload) + "\a")
	}
	if bytes.IndexByte(payload, 0x9c) >= 0 {
		return nil // the emulator would misparse it
	}
	return []byte(seq)
}

// percentEncodeHigh percent-encodes bytes outside ASCII, which keeps a
// hyperlink's URL valid while removing any 0x9C byte.
func percentEncodeHigh(b []byte) []byte {
	var out []byte
	for _, c := range b {
		if c < 0x80 {
			out = append(out, c)
			continue
		}
		out = fmt.Appendf(out, "%%%02X", c)
	}
	return out
}
