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
	"strings"
)

// SystemFile, in the templates directory, replaces DefaultSystem.
const SystemFile = "system.txt"

// DefaultSystem is the instruction the model gets when there is no
// templates/system.txt.
const DefaultSystem = `You rewrite a developer's short, rough request to an AI coding assistant (Claude Code) into a clear prompt.
Rules:
- Keep the developer's intent exactly. Never add goals, components, files, or steps they didn't mention.
- If the request is vague, keep it vague: do not guess what "it" refers to.
- Fix typos and spell out abbreviations.
- For requests with several parts, use a short bullet list, one bullet per part they mentioned.
- Write as the developer speaking to the assistant. Output only the rewritten prompt, under 60 words.`

// Expander rewrites prompts with an Ollama chat model.
type Expander struct {
	Endpoint     string
	Model        string
	KeepAlive    string
	TemplatesDir string // where SystemFile is looked up; "" for none
	// Context returns the lines appended under "Context:", e.g. from
	// GitContext. Nil appends nothing.
	Context func(ctx context.Context) []string
	Client  *http.Client
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
	rewritten, err := e.chat(ctx, system, text)
	if err != nil {
		return "", err
	}
	rewritten = strings.TrimSpace(rewritten)
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

func (e *Expander) chat(ctx context.Context, system, text string) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model: e.Model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: "Request: " + text},
		},
		KeepAlive: e.KeepAlive,
		Options:   map[string]any{"temperature": 0.2, "num_predict": 200},
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
