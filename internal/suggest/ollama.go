package suggest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

// promptHeader frames the text as a request to a coding assistant. Without
// it, small code models drift into writing code ("document = ...");
// examples of whole requests made them copy the examples instead.
const promptHeader = "# A developer's request to an AI coding assistant:\n"

// Ollama completes prompts with a local Ollama server, using
// fill-in-the-middle (the API's suffix field) so that a word cut off
// mid-way is finished rather than restarted. The model must support FIM,
// e.g. qwen2.5-coder; plain chat models are rejected by Ollama with
// ErrNoFIM.
type Ollama struct {
	Endpoint  string // e.g. http://localhost:11434
	Model     string
	MaxTokens int    // completion length; small keeps latency low
	KeepAlive string // how long Ollama keeps the model loaded, e.g. "30m"
	Client    *http.Client
}

// ErrNoFIM reports a model that can't do fill-in-the-middle completion.
var ErrNoFIM = errors.New("model does not support fill-in-the-middle; use one that does, such as qwen2.5-coder")

type generateRequest struct {
	Model     string         `json:"model"`
	Prompt    string         `json:"prompt"`
	Suffix    string         `json:"suffix"`
	Stream    bool           `json:"stream"`
	KeepAlive string         `json:"keep_alive,omitempty"`
	Options   map[string]any `json:"options"`
}

type generateResponse struct {
	Response string `json:"response"`
	Error    string `json:"error"`
}

// Suggest implements Suggester.
func (o *Ollama) Suggest(ctx context.Context, text string, cursor int) (string, error) {
	if cursor != utf8.RuneCountInString(text) || strings.TrimSpace(text) == "" {
		return "", nil
	}
	out, err := o.generate(ctx, promptHeader+text, o.MaxTokens)
	if err != nil {
		return "", err
	}
	out = clean(text, out)
	if repeats(text, out) {
		return "", &RepeatError{Suggestion: out}
	}
	return out, nil
}

// RepeatError is returned instead of a suggestion that repeats the prompt
// (see repeats), so it can be logged.
type RepeatError struct{ Suggestion string }

func (e *RepeatError) Error() string { return fmt.Sprintf("%q repeats the prompt", e.Suggestion) }

// Warm loads the model so the first suggestion isn't delayed by it.
func (o *Ollama) Warm(ctx context.Context) error {
	_, err := o.generate(ctx, promptHeader, 1)
	return err
}

func (o *Ollama) generate(ctx context.Context, prompt string, maxTokens int) (string, error) {
	body, err := json.Marshal(generateRequest{
		Model:     o.Model,
		Prompt:    prompt,
		Suffix:    "\n", // the request ends at the end of the line
		KeepAlive: o.KeepAlive,
		Options: map[string]any{
			"temperature": 0,
			"num_predict": maxTokens,
			"stop":        []string{"\n"},
		},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(o.Endpoint, "/")+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var r generateResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return "", fmt.Errorf("ollama: %s: %w", resp.Status, err)
	}
	switch {
	case strings.Contains(r.Error, "does not support insert"):
		return "", fmt.Errorf("ollama: %s: %w", o.Model, ErrNoFIM)
	case r.Error != "":
		return "", fmt.Errorf("ollama: %s", r.Error)
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("ollama: %s", resp.Status)
	}
	return r.Response, nil
}

// clean turns raw model output into ghost text: a single line, without
// trailing space, and without a second space after text that already ends
// in one.
func clean(text, out string) string {
	if i := strings.IndexAny(out, "\r\n"); i >= 0 {
		out = out[:i]
	}
	out = strings.TrimRight(out, " \t")
	if strings.HasSuffix(text, " ") {
		out = strings.TrimLeft(out, " ")
	}
	return out
}

// repeats reports whether at least two of the three-word runs within out,
// and at least half, are already in text. Seen in testing: asked to
// continue a list ("it still fails, same error message, same code, same
// environment, …"), the model cycled through the same items again, and
// accepting each suggestion grew the prompt in a loop. Only runs of out's
// own words count: runs that start in text made " dependencies, same
// code, same environment," look mostly new. A parallel phrase ("add tests
// for the parser, then add tests" + " for the lexer") has a single run
// within out, so it's kept. Words are compared by their first four letters, so a
// typo doesn't hide a repeat: "environemtent" typed, "environment"
// suggested.
func repeats(text, out string) bool {
	words := func(s string) []string {
		ws := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})
		for i, w := range ws {
			if r := []rune(w); len(r) > 4 {
				ws[i] = string(r[:4])
			}
		}
		return ws
	}
	typed, all := words(text), words(text+out)
	first := len(typed) // index in all of the first word out adds to
	if t := []rune(text); len(t) > 0 && (unicode.IsLetter(t[len(t)-1]) || unicode.IsDigit(t[len(t)-1])) {
		first-- // a word out finishes ("refa" + "ctor") counts as added
		typed = typed[:max(0, len(typed)-1)]
	}
	seen := map[[3]string]bool{}
	for i := 2; i < len(typed); i++ {
		seen[[3]string{typed[i-2], typed[i-1], typed[i]}] = true
	}
	var added, again int
	for i := max(2, first+2); i < len(all); i++ {
		added++
		if seen[[3]string{all[i-2], all[i-1], all[i]}] {
			again++
		}
	}
	return again >= 2 && 2*again >= added
}
