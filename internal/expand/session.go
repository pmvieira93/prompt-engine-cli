package expand

import (
	"context"
	"errors"
	"strings"
)

// Context levels (expand.context; PRD Appendix 1, FR-6). Each includes
// the ones before it.
const (
	ContextNone    = "none"    // L0: the request alone
	ContextPrompts = "prompts" // L1: the developer's last prompts
	ContextReply   = "reply"   // L2: and the end of the assistant's last reply
	ContextSession = "session" // L3: and a running summary of the session
)

// ContextLevels are the values of expand.context.
var ContextLevels = []string{ContextNone, ContextPrompts, ContextReply, ContextSession}

// SessionContext is what the model is told about the session, so it can
// tell what "it", "that" or "the second one" refer to.
type SessionContext struct {
	Prompts []string // earlier prompts, oldest first (L1)
	Reply   string   // the end of the assistant's last reply (L2)
	Summary string   // a running summary of the session (L3)
}

// Texts returns all of it, for the guard rails, which stop a rewrite that
// takes details from it when the request doesn't refer back.
func (s SessionContext) Texts() []string {
	texts := append([]string(nil), s.Prompts...)
	for _, t := range []string{s.Reply, s.Summary} {
		if t != "" {
			texts = append(texts, t)
		}
	}
	return texts
}

// ReplyRule is added to the instructions when the summary or the
// assistant's reply is given: like EarlierRule, it keeps the model to
// resolving references, so it doesn't answer the reply or copy it.
const ReplyRule = `
The session summary and the assistant's last reply are given for the same reason: to tell what "that", "the second one" or "do it" refer to. When the request points at something in them ("the second one", "that option"), name it in the rewrite: this is not guessing. Take from them only what the request points to. Never answer the reply, and never copy its wording or its questions. A report ("it still fails") stays a report: don't add the reply's explanation.`

// request frames text for the model with what it is told about the
// session; rule is what to add to the instructions ("" for nothing).
func (s SessionContext) request(text string) (request, rule string) {
	var parts []string
	if s.Summary != "" {
		parts = append(parts, "Summary of the session so far:\n"+s.Summary)
	}
	earlier, last := s.Prompts, ""
	if s.Reply != "" && len(earlier) > 0 {
		// The reply goes with the request it answers: listed with the
		// others, "now the first one in french" took the colours asked
		// for two requests back, not the fruits in the reply.
		earlier, last = earlier[:len(earlier)-1], earlier[len(earlier)-1]
	}
	if len(earlier) > 0 {
		parts = append(parts, "Earlier requests in this session (oldest first):\n- "+strings.Join(earlier, "\n- "))
	}
	if len(s.Prompts) > 0 {
		rule = "\n" + EarlierRule
	}
	if s.Reply != "" {
		// In quotes, the reply was left alone: "say the second one in
		// portuguese", after "1. Blue 2. Orange", kept "the second one".
		reply := "The assistant's last reply, which the request may point at:\n" + s.Reply
		if last != "" {
			reply = "The last request: " + last + "\n" + reply
		}
		parts = append(parts, reply)
	}
	if s.Reply != "" || s.Summary != "" {
		rule += "\n" + ReplyRule
	}
	parts = append(parts, text)
	return strings.Join(parts, "\n\n"), rule
}

// SummarySystem instructs the model that keeps the L3 summary.
const SummarySystem = `You keep a short running summary of a developer's session with an AI coding assistant (Claude Code).
You are given the summary so far, the developer's latest request and the assistant's reply. Write the updated summary:
- what is being worked on, what was decided or done, and what is still open;
- files, functions, commands, errors and numbers exactly as written;
- drop what no longer matters.
Plain sentences, at most 80 words. Output only the summary.`

// Summarize returns the session summary updated with one exchange: the
// developer's request and the assistant's reply (L3).
func (e *Expander) Summarize(ctx context.Context, summary, prompt, reply string) (string, error) {
	if strings.TrimSpace(prompt) == "" && strings.TrimSpace(reply) == "" {
		return summary, nil
	}
	if summary == "" {
		summary = "(none yet)"
	}
	input := "Summary so far:\n" + summary +
		"\n\nThe developer's request:\n" + prompt +
		"\n\nThe assistant's reply:\n" + reply
	out, err := e.chat(ctx, SummarySystem, input, 200)
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "Summary:"))
	if out == "" {
		return "", errors.New("the model returned an empty summary")
	}
	return out, nil
}
