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

// Tally is a conversation's token usage so far.
type Tally struct {
	Total Usage // every call in the transcript
	// Turn is the calls since the last prompt: one prompt can cause many,
	// as Claude uses tools. TurnDone says its reply has ended.
	Turn     Usage
	TurnDone bool
	Context  int64  // the last call's input: how large the context is
	Model    string // the last call's model
	// Subagents is the calls of the subagents the conversation started,
	// which keep their own transcripts and contexts.
	Subagents Usage
	// TurnSubagents is how many of the subagents' calls came after the
	// last prompt.
	TurnSubagents int
}

// UsageReader tallies a transcript's usage, reading only what was added
// since the last Read: a long session's transcript runs to hundreds of
// megabytes.
type UsageReader struct {
	path  string
	main  usageFile
	subs  map[string]*usageFile
	tally Tally
	// subagentsBefore is the subagents' calls when the turn began.
	subagentsBefore int
}

// usageFile is how far a file has been read, and the calls counted in it.
type usageFile struct {
	offset int64
	seen   map[string]bool // message ids: a reply in several records is one call
}

type usageRecord struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	Message     struct {
		ID         string          `json:"id"`
		Model      string          `json:"model"`
		StopReason string          `json:"stop_reason"`
		Content    json.RawMessage `json:"content"`
		Usage      *struct {
			Input      int64 `json:"input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			Output     int64 `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// Read returns the usage in the transcript at path and in its subagents'
// transcripts. A new path starts a new tally: after /clear, Claude Code
// starts a new transcript, and the conversation with it.
func (r *UsageReader) Read(path string) (Tally, error) {
	if path != r.path {
		*r = UsageReader{path: path}
	}
	if path == "" {
		return Tally{}, nil
	}
	err := r.read(path, &r.main, true)
	dir := strings.TrimSuffix(path, ".jsonl")
	subs, _ := filepath.Glob(filepath.Join(dir, "subagents", "*.jsonl"))
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
	r.tally.TurnSubagents = r.tally.Subagents.Calls - r.subagentsBefore
	return r.tally, err
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
			sub := r.tally.Subagents
			r.tally = Tally{Subagents: sub}
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
	// Most lines are tool results and request data; only these two types
	// matter, and the check is cheaper than decoding.
	if !bytes.Contains(line, []byte(`"assistant"`)) && !bytes.Contains(line, []byte(`"user"`)) {
		return
	}
	var rec usageRecord
	if json.Unmarshal(line, &rec) != nil || rec.IsMeta || (main && rec.IsSidechain) {
		return
	}
	m := rec.Message
	switch rec.Type {
	case "user":
		if !main {
			return
		}
		text, blocks := content(m.Content)
		if _, ok := promptText(text, blocks); ok {
			r.tally.Turn, r.tally.TurnDone = Usage{}, false
			r.subagentsBefore = r.tally.Subagents.Calls
		}
	case "assistant":
		if main && m.StopReason != "" {
			r.tally.TurnDone = m.StopReason == "end_turn"
		}
		// "<synthetic>" replies, such as an API error, made no call.
		if m.Usage == nil || m.ID == "" || f.seen[m.ID] || strings.HasPrefix(m.Model, "<") {
			return
		}
		f.seen[m.ID] = true
		u := Usage{Calls: 1, Input: m.Usage.Input, CacheWrite: m.Usage.CacheWrite, CacheRead: m.Usage.CacheRead, Output: m.Usage.Output}
		if !main {
			r.tally.Subagents.Add(u)
			return
		}
		r.tally.Total.Add(u)
		r.tally.Turn.Add(u)
		r.tally.Context, r.tally.Model = u.In(), m.Model
	}
}
