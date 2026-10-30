// Package transcript reads Claude Code's session transcript, for the
// assistant's last reply (L2) and the session summary (L3) given to the
// expansion model (PRD Appendix 1, FR-6.3).
//
// Claude Code writes each session to
// ~/.claude/projects/<working directory, with "/" as "-">/<session id>.jsonl,
// one JSON record per line. As recorded from version 2.1.308:
//   - a prompt is a "user" record whose message content is a string (or a
//     list of text and image blocks); tool results are "user" records too,
//     as lists of tool_result blocks;
//   - a reply is one or more "assistant" records with thinking, text and
//     tool_use blocks; the last one has stop_reason "end_turn";
//   - records with isSidechain (subagents) or isMeta are not part of the
//     conversation, and prompts that start with "<" are notifications,
//     slash command output or shell mode, not the developer's requests.
//
// The format is Claude Code's own, not a published one, so anything this
// package can't read is treated as no transcript: expansion falls back to
// the earlier prompts (L1).
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// tailSize bounds how much of a transcript is read: its end, where the
// last turn is. Records with request data can be hundreds of kilobytes.
const tailSize = 8 << 20

// Root returns the folder Claude Code keeps transcripts in:
// $CLAUDE_CONFIG_DIR/projects, or ~/.claude/projects.
func Root() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "projects"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// Finder locates the transcript of the session prompt-engine started.
type Finder struct {
	Root  string    // see Root
	Cwd   string    // the session's working directory
	Since time.Time // when the session started: older transcripts are other sessions

	mu         sync.Mutex
	matched    string // the transcript last found
	matchedFor string // the prompt it was found by
}

// Path returns the session's transcript: the most recently written one,
// since the session started, that holds sent, the last prompt sent. It
// returns "" before the first prompt, or if no transcript holds it:
// without that, another session's transcript could be taken for this
// one. Seen in testing: before a prompt was sent, the newest transcript
// in the project was another Claude Code session's, in the same folder.
func (f *Finder) Path(sent string) string {
	if strings.TrimSpace(sent) == "" {
		return ""
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.matched != "" && f.matchedFor == sent {
		if _, err := os.Stat(f.matched); err == nil {
			return f.matched
		}
	}
	dir := f.projectDir()
	if dir == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	type candidate struct {
		path string
		mod  time.Time
	}
	var found []candidate
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().Before(f.Since.Add(-time.Second)) {
			continue
		}
		found = append(found, candidate{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	// Newest first: after /clear, the new transcript holds the prompt.
	sort.Slice(found, func(i, j int) bool { return found[i].mod.After(found[j].mod) })
	for _, c := range found {
		if hasPrompt(c.path, sent) {
			f.matched, f.matchedFor = c.path, sent
			return c.path
		}
	}
	return ""
}

// hasPrompt reports whether the transcript at path has sent among its
// prompts.
func hasPrompt(path, sent string) bool {
	data, err := readTail(path, tailSize)
	if err != nil {
		return false
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		var r record
		if len(line) == 0 || json.Unmarshal(line, &r) != nil || r.Type != "user" || r.IsSidechain || r.IsMeta {
			continue
		}
		var m message
		if json.Unmarshal(r.Message, &m) != nil {
			continue
		}
		if prompt, ok := promptText(content(m.Content)); ok && samePrompt(prompt, sent) {
			return true
		}
	}
	return false
}

// projectDir finds the folder for f.Cwd: Claude Code's name for it
// replaces "/" with "-" (recorded), and probably every character that
// isn't a letter or digit (not seen yet), so both are tried, then any
// folder whose recent transcripts were written in f.Cwd.
func (f *Finder) projectDir() string {
	for _, name := range []string{escape(f.Cwd), strings.ReplaceAll(f.Cwd, "/", "-")} {
		if dir := filepath.Join(f.Root, name); isDir(dir) {
			return dir
		}
	}
	dirs, err := os.ReadDir(f.Root)
	if err != nil {
		return ""
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		dir := filepath.Join(f.Root, d.Name())
		files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		for _, p := range files {
			if info, err := os.Stat(p); err == nil && !info.ModTime().Before(f.Since) && cwdOf(p) == f.Cwd {
				return dir
			}
		}
	}
	return ""
}

func escape(path string) string {
	return strings.Map(func(r rune) rune {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return r
		}
		return '-'
	}, path)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// cwdOf returns the working directory recorded in a transcript's first
// records.
func cwdOf(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for i := 0; i < 20 && sc.Scan(); i++ {
		var r struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.Cwd != "" {
			return r.Cwd
		}
	}
	return ""
}

// samePrompt reports whether a prompt in the transcript is the one sent:
// the transcript has what claude received, which may have rules appended.
func samePrompt(inTranscript, sent string) bool {
	a, b := strings.Join(strings.Fields(inTranscript), " "), strings.Join(strings.Fields(sent), " ")
	n := min(len(a), len(b), 80)
	return n > 0 && a[:n] == b[:n]
}

// Turn is one exchange: a prompt and the reply to it.
type Turn struct {
	ID     string // the last record's uuid: changes as the reply grows
	Prompt string
	// Reply is the text after the assistant's last tool call: its answer,
	// not the "Now I'll read…" between tool calls.
	Reply string
	Done  bool // the reply has ended
}

type record struct {
	Type        string          `json:"type"`
	UUID        string          `json:"uuid"`
	IsSidechain bool            `json:"isSidechain"`
	IsMeta      bool            `json:"isMeta"`
	Message     json.RawMessage `json:"message"`
}

type message struct {
	Content    json.RawMessage `json:"content"`
	StopReason string          `json:"stop_reason"`
}

type block struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// LastTurn returns the transcript's last turn. Its prompt is "" if it
// began before the part read.
func LastTurn(path string) (Turn, error) {
	data, err := readTail(path, tailSize)
	if err != nil {
		return Turn{}, err
	}
	var t Turn
	var reply []string
	seen := false
	for _, line := range bytes.Split(data, []byte("\n")) {
		var r record
		if len(line) == 0 || json.Unmarshal(line, &r) != nil || r.IsSidechain || r.IsMeta {
			continue
		}
		if r.Type != "user" && r.Type != "assistant" {
			continue
		}
		var m message
		if json.Unmarshal(r.Message, &m) != nil {
			continue
		}
		text, blocks := content(m.Content)
		switch r.Type {
		case "user":
			if prompt, ok := promptText(text, blocks); ok {
				t, reply, seen = Turn{ID: r.UUID, Prompt: prompt}, nil, true
				continue
			}
			for _, b := range blocks {
				if b.Type == "tool_result" {
					reply = nil // what came before was between tool calls
					t.Done = false
				}
			}
		case "assistant":
			seen = true
			t.ID = r.UUID
			for _, b := range blocks {
				switch b.Type {
				case "text":
					if s := strings.TrimSpace(b.Text); s != "" {
						reply = append(reply, s)
					}
				case "tool_use":
					reply = nil
				}
			}
			t.Done = m.StopReason == "end_turn"
		}
	}
	if !seen {
		return Turn{}, errors.New("no conversation in the transcript")
	}
	t.Reply = strings.Join(reply, "\n\n")
	return t, nil
}

// content decodes a message's content: a string, or a list of blocks.
func content(raw json.RawMessage) (string, []block) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	var blocks []block
	_ = json.Unmarshal(raw, &blocks)
	return "", blocks
}

// promptText returns the developer's prompt in a user record, if it is
// one: not tool results, and not a notification, slash command output or
// shell mode line, which start with a tag such as "<task-notification>".
func promptText(text string, blocks []block) (string, bool) {
	if text == "" {
		var parts []string
		for _, b := range blocks {
			switch b.Type {
			case "text":
				parts = append(parts, b.Text)
			case "tool_result":
				return "", false
			}
		}
		text = strings.Join(parts, "\n")
	}
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "<") {
		return "", false
	}
	return text, true
}

// readTail returns up to n bytes from the end of path, from the start of
// a line.
func readTail(path string, n int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	start := max(0, info.Size()-n)
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	if start > 0 {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	return data, nil
}

// Shorten keeps a long reply's start and end, which in testing held what
// a follow-up refers to: the list of options, and the closing question.
// words bounds the result; the start gets a quarter of it.
func Shorten(reply string, words int) string {
	fields := strings.Fields(reply)
	if len(fields) <= words {
		return strings.TrimSpace(reply)
	}
	head := words / 4
	return strings.Join(fields[:head], " ") + " … " + strings.Join(fields[len(fields)-(words-head):], " ")
}
