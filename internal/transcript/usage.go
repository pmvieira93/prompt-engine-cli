package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Usage is the tokens some API calls used, from the usage Claude Code
// records with each reply (PRD Appendix 5, FR-11.1).
type Usage struct {
	Calls      int
	Input      int64 // not read from the cache
	CacheWrite int64
	CacheRead  int64
	Output     int64
}

// In is the input tokens: fresh, written to the cache and read from it.
func (u Usage) In() int64 { return u.Input + u.CacheWrite + u.CacheRead }

// Add adds o's calls and tokens to u.
func (u *Usage) Add(o Usage) {
	u.Calls += o.Calls
	u.Input += o.Input
	u.CacheWrite += o.CacheWrite
	u.CacheRead += o.CacheRead
	u.Output += o.Output
}

func (u *Usage) sub(o Usage) {
	u.Add(Usage{-o.Calls, -o.Input, -o.CacheWrite, -o.CacheRead, -o.Output})
}

// Tally is a conversation's token usage so far.
type Tally struct {
	Total Usage // every call in the transcript
	// Turn is the calls since the last prompt: one prompt can cause many,
	// as Claude uses tools.
	Turn    Usage
	Context int64  // the last call's input: how large the context is
	Model   string // the last call's model
	// Subagents is the calls of the subagents the conversation started,
	// which keep their own transcripts and contexts.
	Subagents Usage
	// TurnSubagents is how many of the subagents' calls came after the
	// last prompt.
	TurnSubagents int
}

// TurnUsage is the usage of one prompt's turn, from the prompt to the
// next one: however its reply ended, interrupted included.
type TurnUsage struct {
	Prompt    string // as the transcript has it, to tell which prompt it was
	Usage     Usage
	Subagents int // the subagents' calls during the turn
	Context   int64
	Model     string
}

// UsageReader tallies a transcript's usage, reading only what was added
// since the last Read: a long session's transcript runs to hundreds of
// megabytes.
type UsageReader struct {
	path  string
	main  usageFile
	subs  map[string]*usageFile
	tally Tally
	// prompt is the current turn's prompt, and subagentsBefore the
	// subagents' calls when it began.
	prompt          string
	subagentsBefore int
	ended           []TurnUsage
}

// usageFile is how far a file has been read, and the calls counted in it.
type usageFile struct {
	offset int64
	seen   map[string]bool // message ids: a reply in several records is one call
	usage  Usage           // what this file added, to take back if it starts over
}

type usageRecord struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	Message     struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			Input      int64 `json:"input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			Output     int64 `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// Only these record types matter; most lines are tool results and request
// data, and looking for the type is cheaper than decoding them. Claude
// Code writes compact JSON; the spaced form is in case that changes.
var usageTypes = [][]byte{[]byte(`"type":"user"`), []byte(`"type":"assistant"`), []byte(`"type": "user"`), []byte(`"type": "assistant"`)}

// Read returns the usage in the transcript at path and in its subagents'
// transcripts. A different path starts a new tally: after /clear, Claude
// Code starts a new transcript, and the conversation with it. An empty
// path, as while the transcript is being looked for, keeps the tally.
func (r *UsageReader) Read(path string) (Tally, error) {
	if path == "" {
		return r.tally, nil
	}
	if path != r.path {
		*r = UsageReader{path: path}
	}
	// The subagents first: their calls before a prompt read in the same
	// pass then count in the turn before it.
	var err error
	subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents", "*.jsonl"))
	for _, sub := range subs {
		if r.subs == nil {
			r.subs = map[string]*usageFile{}
		}
		f := r.subs[sub]
		if f == nil {
			f = &usageFile{}
			r.subs[sub] = f
		}
		err = errors.Join(err, r.read(sub, f, false))
	}
	err = errors.Join(r.read(path, &r.main, true), err)
	r.tally.TurnSubagents = r.tally.Subagents.Calls - r.subagentsBefore
	return r.tally, err
}

// Ended returns the turns that ended since the last call: a turn ends
// when the next prompt starts.
func (r *UsageReader) Ended() []TurnUsage {
	ended := r.ended
	r.ended = nil
	return ended
}

// Current returns the turn in progress, for when the session ends before
// another prompt does, and whether it made any calls.
func (r *UsageReader) Current() (TurnUsage, bool) {
	t := TurnUsage{Prompt: r.prompt, Usage: r.tally.Turn, Subagents: r.tally.Subagents.Calls - r.subagentsBefore,
		Context: r.tally.Context, Model: r.tally.Model}
	return t, t.Usage.Calls > 0 || t.Subagents > 0
}

// read counts the complete lines added to path since f.offset. In the
// main transcript, a prompt starts a new turn and subagent records are
// skipped; in a subagent's, every call counts toward Subagents.
func (r *UsageReader) read(path string, f *usageFile, main bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() < f.offset {
		// Rewritten from the start: count it again.
		if main {
			r.tally = Tally{Subagents: r.tally.Subagents}
			r.prompt, r.subagentsBefore = "", r.tally.Subagents.Calls
		} else {
			r.tally.Subagents.sub(f.usage)
		}
		*f = usageFile{}
	}
	if _, err := file.Seek(f.offset, io.SeekStart); err != nil {
		return err
	}
	if f.seen == nil {
		f.seen = map[string]bool{}
	}
	in := bufio.NewReaderSize(file, 64<<10)
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			// A line without its "\n" is still being written: read it next time.
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		f.offset += int64(len(line))
		r.record(bytes.TrimSpace(line), f, main)
	}
}

func (r *UsageReader) record(line []byte, f *usageFile, main bool) {
	wanted := false
	for _, t := range usageTypes {
		if bytes.Contains(line, t) {
			wanted = true
			break
		}
	}
	var rec usageRecord
	if !wanted || json.Unmarshal(line, &rec) != nil || rec.IsMeta || (main && rec.IsSidechain) {
		return
	}
	m := rec.Message
	switch rec.Type {
	case "user":
		if !main {
			return
		}
		text, blocks := content(m.Content)
		if prompt, ok := promptText(text, blocks); ok {
			if t, any := r.Current(); any {
				r.ended = append(r.ended, t)
			}
			r.tally.Turn, r.prompt, r.subagentsBefore = Usage{}, prompt, r.tally.Subagents.Calls
		}
	case "assistant":
		// "<synthetic>" replies, such as an API error, made no call.
		if m.Usage == nil || m.ID == "" || f.seen[m.ID] || strings.HasPrefix(m.Model, "<") {
			return
		}
		f.seen[m.ID] = true
		u := Usage{Calls: 1, Input: m.Usage.Input, CacheWrite: m.Usage.CacheWrite, CacheRead: m.Usage.CacheRead, Output: m.Usage.Output}
		f.usage.Add(u)
		if !main {
			r.tally.Subagents.Add(u)
			return
		}
		r.tally.Total.Add(u)
		r.tally.Turn.Add(u)
		r.tally.Context, r.tally.Model = u.In(), m.Model
	}
}
