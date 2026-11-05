// Command shared-run is the spike for PRD Appendix 4 (FR-10.8) and Appendix
// 6 (FR-12): it sends labelled prompts to a local model as plain text, with
// guard lines, and as JSON with an intent, and records each reply, the guard
// that stops it, its tokens and its time.
//
//	go run ./docs/spikes/shared-run -prompts labelled.json -out runs.jsonl
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/pmvieira93/prompt-engine-cli/internal/expand"
)

// intents are the section names of FR-12.1; analysis and investigate share
// a section.
var intents = []string{"review", "test", "investigate", "analysis", "debug", "none"}

const intentRule = `
Reply with JSON only: "response" is the rewritten prompt, and "intent" is the kind of work the request asks for, one of: review, test, investigate, analysis, debug, or none if no other fits.`

var (
	echoedLabel = regexp.MustCompile(`^\s*(Request|Message)( \([^)]*\))?:\s*`)
	question    = regexp.MustCompile(`(?i)\?\s*$|^\s*(what|why|how|which|who|when|where|is|are|do|does|did|can|could|should)\b`)
	negation    = regexp.MustCompile(`(?i)\b(not|never|without|only|no|don't|doesn't|didn't|can't|won't)\b`)
	quoted      = regexp.MustCompile("[\"`]|'[^']+'")
)

// guardLines are FR-10.8's lines for text, each only when it matters.
func guardLines(text string) string {
	lines := "\nNever add actions that delete, reset, push or overwrite anything."
	if question.MatchString(text) {
		lines += "\nIf the request is a question, keep it a question: don't turn it into a task."
	}
	if negation.MatchString(text) {
		lines += "\nKeep every not, never, without and only."
	}
	if quoted.MatchString(text) {
		lines += "\nCopy quoted text exactly."
	}
	return lines
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type reply struct {
	Message         message `json:"message"`
	Error           string  `json:"error"`
	DoneReason      string  `json:"done_reason"`
	PromptEvalCount int     `json:"prompt_eval_count"`
	EvalCount       int     `json:"eval_count"`
	TotalDuration   int64   `json:"total_duration"`
}

type result struct {
	ID       int     `json:"id"`
	Label    string  `json:"label"`
	Variant  string  `json:"variant"`
	Seed     int     `json:"seed"`
	Typed    string  `json:"typed"`
	Rewrite  string  `json:"rewrite"`
	Intent   string  `json:"intent,omitempty"`
	Parsed   bool    `json:"parsed"`
	Guard    string  `json:"guard,omitempty"`
	Detail   string  `json:"detail,omitempty"`
	Cut      bool    `json:"cut"`
	Typos    bool    `json:"typos"`
	In       int     `json:"in"`
	Out      int     `json:"out"`
	Seconds  float64 `json:"seconds"`
	Failures string  `json:"error,omitempty"`
}

func chat(endpoint, model, system, user string, tokens, seed int, format any) (reply, error) {
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"messages":   []message{{"system", system}, {"user", user}},
		"stream":     false,
		"keep_alive": "30m",
		"format":     format,
		"options":    map[string]any{"temperature": 0.2, "num_predict": tokens, "seed": seed},
	})
	resp, err := http.Post(endpoint+"/api/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return reply{}, err
	}
	defer resp.Body.Close()
	var r reply
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r)
	if err == nil && r.Error != "" {
		err = fmt.Errorf("%s", r.Error)
	}
	return r, err
}

func run(endpoint, model string, id int, label, text, variant string, seed int) result {
	r := result{ID: id, Label: label, Variant: variant, Seed: seed, Typed: text}
	plan := expand.PlanRewrite(text, nil)
	r.Typos = plan.TypoOnly
	system, name := expand.DefaultSystem, "Request"
	if plan.TypoOnly {
		system, name = expand.TypoSystem, "Message"
	}
	var format any
	switch variant {
	case "lines":
		system += guardLines(text)
	case "json":
		system += intentRule
		format = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"response": map[string]any{"type": "string"},
				"intent":   map[string]any{"type": "string", "enum": intents},
			},
			"required": []string{"response", "intent"},
		}
	}
	request := name + ": " + text
	if lang := expand.Language(text); lang != "" {
		request = name + " (in " + lang + "; write the rewrite in " + lang + "): " + text
	}
	tokens := max(200, 2*len(strings.Fields(text)))
	got, err := chat(endpoint, model, system, request, tokens, seed, format)
	if err != nil {
		r.Failures = err.Error()
		return r
	}
	r.In, r.Out = got.PromptEvalCount, got.EvalCount
	r.Seconds = float64(got.TotalDuration) / float64(time.Second)
	r.Cut = got.DoneReason == "length"
	content := got.Message.Content
	r.Parsed = variant != "json"
	if variant == "json" {
		var j struct{ Response, Intent string }
		if json.Unmarshal([]byte(content), &j) == nil && j.Response != "" {
			content, r.Intent, r.Parsed = j.Response, j.Intent, true
		}
	}
	rewritten := strings.TrimSpace(echoedLabel.ReplaceAllString(content, ""))
	if !strings.Contains(rewritten, "\n") {
		rewritten = strings.TrimPrefix(rewritten, "- ")
	}
	r.Rewrite = rewritten
	if stop := expand.CheckSession(text, rewritten, expand.SessionContext{}); stop != nil {
		r.Guard, r.Detail = stop.Guard, stop.Detail
	}
	return r
}

func main() {
	prompts := flag.String("prompts", "", "labelled prompts (JSON)")
	out := flag.String("out", "", "results (JSON lines)")
	model := flag.String("model", "qwen2.5:7b", "Ollama model")
	endpoint := flag.String("endpoint", "http://localhost:11434", "Ollama endpoint")
	seeds := flag.Int("seeds", 3, "runs of each prompt and variant")
	flag.Parse()

	data, err := os.ReadFile(*prompts)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var set []struct {
		ID          int
		Label, Text string
	}
	if err := json.Unmarshal(data, &set); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for seed := 1; seed <= *seeds; seed++ {
		for _, p := range set {
			for _, variant := range []string{"plain", "lines", "json"} {
				if err := enc.Encode(run(*endpoint, *model, p.ID, p.Label, p.Text, variant, seed)); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
			}
		}
		fmt.Printf("seed %d done\n", seed)
	}
}
