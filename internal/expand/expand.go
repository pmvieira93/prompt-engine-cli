// Package expand turns a rough prompt into a clearer one on request (PRD
// FR-2.4, FR-3.1): a local model rewrites the wording, and git context is
// appended by prompt-engine itself.
//
// The split is deliberate. In testing, small models given repository
// context (branch, recent commits) treated it as the topic and invented
// requirements from it; told to stay faithful, they used none of it. So the
// model only rewrites the request, and the context is added verbatim, where
// it can't be hallucinated.
package expand

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SystemFile, in the templates directory, replaces DefaultSystem.
const SystemFile = "system.txt"

// DefaultSystem is the instruction the model gets when there is no
// templates/system.txt.
const DefaultSystem = `You rewrite a developer's short, rough request to an AI coding assistant (Claude Code) into a clear prompt.
Rules:
- Keep the developer's intent exactly. Never add goals, components, files, or steps they didn't mention.
- Keep names exactly as written: agents, skills, commands, @mentions, files, paths, branches, and issue, MR or PR numbers. A word with a hyphen, slash, dot, @, #, ! or digits is a name: never split, translate or drop it.
- If the request is vague, keep it vague: do not guess what "it" refers to.
- Fix typos and spell out abbreviations.
- For requests with several parts, use a short bullet list, one bullet per part they mentioned.
- Write as the developer speaking to the assistant. Output only the rewritten prompt, under 60 words.`

// EarlierRule is added to the instructions when earlier prompts are given,
// to keep the model from treating them as part of the request: in testing,
// context given without such a rule became the topic.
const EarlierRule = `
Earlier requests in this session are given only so you can tell what words like "it", "there", "that" or "same" refer to. Never add goals, steps, or details from them that the current request doesn't ask for. If the current request doesn't refer back, ignore them.`

// TypoSystem replaces the instructions for text typed around a paste,
// which is already worded around it, and for long prompts, which
// rewording would cut short (PRD Appendix 1, FR-7.4e).
const TypoSystem = `You fix typos in a developer's message to an AI coding assistant (Claude Code).
Rules:
- Only fix spelling, missing letters and obvious grammar slips. Keep every other word, the word order and the meaning.
- Keep names exactly as written: agents, skills, commands, @mentions, files, paths, branches, and issue, MR or PR numbers.
- Output only the corrected message.`

// MarkerRule is added to the instructions when the text has markers for
// kept parts (see Marker).
const MarkerRule = `
The text contains markers like <<PASTE_1>> that stand for text the developer pasted. Copy each marker exactly, once, in the same place. Never write what it might contain.`

// echoedLabel matches the request's label when the model copies it.
var echoedLabel = regexp.MustCompile(`^\s*(Request|Message)( \([^)]*\))?:\s*`)

// Expander rewrites prompts with an Ollama chat model.
type Expander struct {
	Endpoint     string
	Model        string
	KeepAlive    string
	TemplatesDir string // where SystemFile is looked up; "" for none
	// Context returns the lines appended under "Context:", e.g. from
	// GitContext. Nil appends nothing.
	Context func(ctx context.Context) []string
	// Earlier returns the prompts sent earlier in the session, oldest
	// first, for the model to resolve references with ("add tests for
	// it"). Nil or empty means none.
	Earlier func() []string
	// TypoOnly uses TypoSystem: corrections, no rewording (Plan.TypoOnly).
	TypoOnly bool
	Client   *http.Client
}

// Expand returns text rewritten, with any context appended.
func (e *Expander) Expand(ctx context.Context, text string) (string, error) {
	if text = StripContext(text); text == "" {
		return "", errors.New("nothing to expand besides the context lines")
	}
	system, err := e.system()
	if err != nil {
		return "", err
	}
	if e.TypoOnly {
		system = TypoSystem
	}
	if markerPattern.MatchString(text) {
		system += "\n" + MarkerRule
	}
	// In typo mode the model copied a "Request:" label into its answer,
	// but not "Message:".
	label := "Request"
	if e.TypoOnly {
		label = "Message"
	}
	request := label + ": " + text
	if lang := Language(text); lang != "" {
		// Named only for other languages: naming one in the instructions
		// made the model translate English requests into it.
		request = label + " (in " + lang + "; write the rewrite in " + lang + "): " + text
	}
	if e.Earlier != nil {
		if earlier := e.Earlier(); len(earlier) > 0 {
			system += "\n" + EarlierRule
			request = "Earlier requests in this session (oldest first):\n- " +
				strings.Join(earlier, "\n- ") + "\n\n" + request
		}
	}
	// Room for a corrected long prompt: about 1.5 tokens a word.
	tokens := max(200, 2*len(strings.Fields(text)))
	rewritten, err := e.chat(ctx, system, request, tokens)
	if err != nil {
		return "", err
	}
	rewritten = strings.TrimSpace(echoedLabel.ReplaceAllString(rewritten, ""))
	if !strings.Contains(rewritten, "\n") {
		// A one-item list ("- Add tests") is just the item.
		rewritten = strings.TrimPrefix(rewritten, "- ")
	}
	if rewritten == "" {
		return "", errors.New("the model returned nothing")
	}
	if e.Context == nil {
		return rewritten, nil
	}
	lines := e.Context(ctx)
	if len(lines) == 0 {
		return rewritten, nil
	}
	return rewritten + "\n\nContext:\n- " + strings.Join(lines, "\n- "), nil
}

// StripContext removes the context lines Expand appends ("Context:",
// "- Branch: …", "- Staged changes: …"), so expanding an already expanded
// prompt doesn't send stale context to the model as part of the request.
// Fresh context is appended after the rewrite.
func StripContext(text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "Context:" || strings.HasPrefix(t, "- Branch: ") || strings.HasPrefix(t, "- Staged changes: ") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// Warm loads the model, so the first expansion isn't slowed by it.
func (e *Expander) Warm(ctx context.Context) error {
	_, err := e.chat(ctx, "Reply with OK.", "OK", 200)
	return err
}

func (e *Expander) system() (string, error) {
	if e.TemplatesDir == "" {
		return DefaultSystem, nil
	}
	data, err := os.ReadFile(filepath.Join(e.TemplatesDir, SystemFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return DefaultSystem, nil
	case err != nil:
		return "", err
	}
	if s := strings.TrimSpace(string(data)); s != "" {
		return s, nil
	}
	return DefaultSystem, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model     string         `json:"model"`
	Messages  []chatMessage  `json:"messages"`
	Stream    bool           `json:"stream"`
	KeepAlive string         `json:"keep_alive,omitempty"`
	Options   map[string]any `json:"options"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
	Error   string      `json:"error"`
}

func (e *Expander) chat(ctx context.Context, system, request string, tokens int) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model: e.Model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: request},
		},
		KeepAlive: e.KeepAlive,
		Options:   map[string]any{"temperature": 0.2, "num_predict": tokens},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(e.Endpoint, "/")+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	client := e.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var r chatResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return "", fmt.Errorf("ollama: %s: %w", resp.Status, err)
	}
	switch {
	case r.Error != "":
		return "", fmt.Errorf("ollama: %s", r.Error)
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("ollama: %s", resp.Status)
	}
	return r.Message.Content, nil
}
