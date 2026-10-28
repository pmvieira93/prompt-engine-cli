// Package diff compares two texts word by word, for showing how a prompt
// was changed before it was sent (PRD FR-5.2).
package diff

import (
	"strings"
	"unicode"
)

// Kind says what happened to a run of text.
type Kind int

const (
	Equal  Kind = iota // in both texts
	Insert             // only in the new text
	Delete             // only in the old text
)

// Op is a run of text with the same Kind.
type Op struct {
	Kind Kind
	Text string
}

// maxCells bounds the comparison table (old tokens × new tokens). Prompts
// longer than that are shown as one deletion and one insertion.
const maxCells = 4_000_000

// Words returns the edits that turn old into new, comparing whole words
// and the whitespace between them, so a fixed typo shows as one replaced
// word rather than scattered letters.
func Words(old, new string) []Op {
	a, b := tokens(old), tokens(new)
	if len(a)*len(b) > maxCells {
		return merge([]Op{{Delete, old}, {Insert, new}})
	}

	// lcs[i][j] is the length of the longest common subsequence of a[i:]
	// and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}

	var ops []Op
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops = append(ops, Op{Equal, a[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, Op{Delete, a[i]})
			i++
		default:
			ops = append(ops, Op{Insert, b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		ops = append(ops, Op{Delete, a[i]})
	}
	for ; j < len(b); j++ {
		ops = append(ops, Op{Insert, b[j]})
	}
	return merge(ops)
}

// Counts returns how many words were added and removed.
func Counts(ops []Op) (added, removed int) {
	for _, op := range ops {
		n := len(strings.Fields(op.Text))
		switch op.Kind {
		case Insert:
			added += n
		case Delete:
			removed += n
		}
	}
	return added, removed
}

// tokens splits s into words and runs of whitespace, keeping both, so the
// ops reassemble the texts exactly.
func tokens(s string) []string {
	var out []string
	start, space := 0, false
	for i, r := range s {
		if isSpace := unicode.IsSpace(r); i == 0 {
			space = isSpace
		} else if isSpace != space {
			out = append(out, s[start:i])
			start, space = i, isSpace
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// merge joins adjacent ops of the same kind, drops empty ones, and puts a
// deletion before the insertion that replaces it.
func merge(ops []Op) []Op {
	var out []Op
	for _, op := range ops {
		if op.Text == "" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Kind == op.Kind {
			out[n-1].Text += op.Text
			continue
		}
		out = append(out, op)
	}
	return out
}
