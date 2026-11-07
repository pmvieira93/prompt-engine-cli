package rules

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
)

// EnvQueue names the folder that holds the rules for each prompt, so the
// hook, which claude starts, can find them.
const EnvQueue = "PROMPT_ENGINE_RULES_QUEUE"

// Queue hands the rules chosen on Enter to the hook claude runs when the
// prompt reaches it, which may be much later if claude is busy. Each prompt
// sent has an entry, with no text if it gets no rules, so the hook can tell
// that from a prompt it doesn't recognise.
type Queue struct {
	Dir string
	seq atomic.Uint64
}

type entry struct {
	Prompt string `json:"prompt"`
	Text   string `json:"text"`
}

// NewQueue makes a private folder for one session's rules. Close removes it.
func NewQueue() (*Queue, error) {
	dir, err := os.MkdirTemp("", "prompt-engine-rules-")
	if err != nil {
		return nil, err
	}
	return &Queue{Dir: dir}, nil
}

// Close removes the queue and anything still in it.
func (q *Queue) Close() error { return os.RemoveAll(q.Dir) }

// Put records the rules for prompt. It must finish before the Enter that
// sends the prompt, as the hook can run right after it.
func (q *Queue) Put(prompt, text string) error {
	data, err := json.Marshal(entry{prompt, text})
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%012d.json", q.seq.Add(1))
	tmp := filepath.Join(q.Dir, "."+name)
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(q.Dir, name))
}

// Take removes and returns the rules for prompt: the oldest entry with the
// same text, along with any older ones (their Enter sent nothing). If none
// has it, the oldest entry, since prompts reach the hook in the order they
// were sent and claude can change a prompt's text, as Tab on an @-mention
// can. ok is false if the queue is empty.
func Take(dir, prompt string) (text string, ok bool) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var names []string
	var entries []entry
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".") {
			continue // still being written
		}
		name := filepath.Join(dir, f.Name())
		data, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		var e entry
		if json.Unmarshal(data, &e) != nil {
			continue
		}
		names, entries = append(names, name), append(entries, e)
	}
	if len(entries) == 0 {
		return "", false
	}
	found := 0
	want := bare(prompt)
	for i, e := range entries {
		if bare(e.Prompt) == want {
			found = i
			break
		}
	}
	for _, name := range names[:found+1] {
		os.Remove(name)
	}
	return entries[found].Text, true
}

// pasteTag is what claude puts around a large paste before the hook sees it.
var pasteTag = regexp.MustCompile(`</?pasted_content id="[0-9a-f]{4}">`)

// bare returns prompt without what claude changes in it: tags around
// pastes, and space.
func bare(prompt string) string {
	return strings.Join(strings.Fields(pasteTag.ReplaceAllString(prompt, "")), "")
}

// Settings returns claude's --settings value that runs command as the hook.
func Settings(command string) string {
	hook := map[string]any{"type": "command", "command": command}
	data, _ := json.Marshal(map[string]any{"hooks": map[string]any{
		"UserPromptSubmit": []any{map[string]any{"hooks": []any{hook}}},
	}})
	return string(data)
}

// Hook answers claude's UserPromptSubmit hook: it reads the prompt from in
// and writes the rules for it to out, as context for that prompt. Whatever
// goes wrong, the prompt must still go, so it adds nothing and says so
// through logf.
func Hook(in io.Reader, out io.Writer, dir string, logf func(string, ...any)) {
	var hook struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(in).Decode(&hook); err != nil {
		logf("rules: hook: can't read the prompt: %v", err)
		return
	}
	text, ok := Take(dir, hook.Prompt)
	switch {
	case !ok:
		logf("rules: hook: no rules waiting for this prompt")
		return
	case text == "":
		return
	}
	logf("rules: hook gave claude %d bytes", len(text))
	json.NewEncoder(out).Encode(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     "UserPromptSubmit",
		"additionalContext": text,
	}})
}
