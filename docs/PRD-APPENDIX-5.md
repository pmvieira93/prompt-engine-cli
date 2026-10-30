# PRD Appendix 5: Cost-Aware Local Assistance

**Extends:** [PRD.md](../PRD.md) version 1.2.5 (section 2, core value proposition; section 4.2, the local AI engine; the Phase 5 roadmap)  
**Appendix Version:** 1.0.0  
**Date:** 2026-10-30  
**Status:** Proposed, spike first. Raised on 2026-10-30; nothing is built until the spike (section 4) shows savings worth building.

---

## 1. Background

`prompt-engine` uses local models (Ollama) for two things: ghost text and prompt rewrites. The idea is to use them for a third: **lowering what a session with a hosted AI coding assistant costs, in tokens and in time**. The local model would do the work it can do well, and leave the hosted model (Claude Code with Opus or Sonnet, and later GitHub Copilot, Gemini and others) the work that needs it.

### 1.1 Where the tokens go

`prompt-engine` only sees what the developer types and pastes. Most of a session's tokens are Claude Code's own context:
* its system prompt and tool definitions;
* the files it reads and the tool output;
* the whole conversation, resent on each turn.

So making the typed prompt shorter saves almost nothing directly. The savings `prompt-engine` can reach come from:

1. **Fewer turns.** A clear first prompt avoids round trips ("what do you mean by…?"). Expansion already aims at this; it isn't measured.
2. **Smaller inputs.** Pasted logs, stack traces and diffs are often thousands of tokens, mostly noise, and they stay in the context for the rest of the session.
3. **Not asking the hosted model.** Some questions don't need the agent: "what does this regex do?", "explain this error message".
4. **A cheaper model or a smaller context.** A lighter model for easy tasks (Claude Code's `/model`), and `/compact` or `/clear` once the context holds things that no longer matter.

### 1.2 What can be measured

Claude Code's session transcript (read since FR-6.3, [Appendix 1](PRD-APPENDIX-1.md), 2.5) records each reply's token usage, in the `usage` field of `assistant` records. Savings can be measured from the developer's real sessions, not estimated.

---

## 2. Proposal (FR-11)

Ranked by expected savings against risk. Every action is a **suggestion the developer approves**; `prompt-engine` never changes what is sent, or which model answers, on its own.

* **FR-11.1: Cost visibility.** The panel shows the session's tokens so far (input, output, cache reads) and those of the last reply, from the transcript.
  * The history records them per prompt.
  * This comes first: the other features are judged by it.
* **FR-11.2: Condensed pastes.** When a paste is large (a log, a stack trace, a diff), the local model prepares a condensed version: the errors, the lines around them, and what repeats, counted.
  * The panel shows both sizes, and a shortcut swaps the condensed version in.
  * The original is kept: the shortcut undoes it, and the developer can always send the full paste.
* **FR-11.3: Local answers.** A small local classifier decides whether a prompt needs the agent (files, commands, edits, the session) or is a self-contained question.
  * For the latter, the local model's answer is offered in the panel; Enter still sends the prompt to Claude Code as usual.
  * Guards stop local answers that look unsure or that reference the project.
* **FR-11.4: Model and context hints.** The panel suggests a lighter model for small, self-contained tasks, and `/compact` or `/clear` when the context is large and the new prompt doesn't refer back to it.
  * `prompt-engine` only suggests; the developer types the command.
* **FR-11.5 (later, not in scope yet): Split calls below Claude Code.** A local proxy between Claude Code and its API (Claude Code accepts a custom API address) could:
  * send small calls (titles, summaries, quick checks) to a local model;
  * condense tool output before it reaches the hosted model.

  This is the most powerful option, and the most fragile:
  * it depends on the API format;
  * it changes how the agent behaves and what is billed;
  * it needs its own appendix and a separate decision.

### 2.1 Other providers

FR-11.1 to FR-11.4 work at the prompt, so they carry over to the other CLIs of Phase 5 (GitHub Copilot, Gemini, Aider) once `prompt-engine` wraps them. Only reading token usage (FR-11.1) depends on each CLI's own records.

---

## 3. Risks

* **A condensed paste drops the line that mattered.** Mitigations:
  * the developer sees both versions and chooses;
  * the original is kept;
  * the spike measures how often the cause survives.
* **A wrong local answer, trusted.** Mitigations:
  * local answers are offered beside the prompt, never instead of it;
  * they are labelled with the local model's name;
  * guards stop unsure ones.
* **Savings too small to matter.** Since most tokens are Claude Code's own context (1.1), the savings may be modest. The spike decides before anything is built.
* **Memory and speed.** A third use of the local models competes with ghost text and rewrites for the same memory and GPU.

---

## 4. Spike: does it pay off?

Before building, answer with data:

1. **Baseline.** From the developer's own Claude Code transcripts (read locally, nothing sent anywhere):
   * tokens per session and per turn;
   * how much of the input comes from pastes;
   * how many turns are clarifications;
   * how large the context grows before `/compact` or `/clear`.
2. **Condensing.** 10–20 real pasted logs, traces and diffs, each condensed by candidate local models. Measure:
   * tokens saved;
   * whether the cause is still there, checked by hand and by sending both versions to Claude Code with the same question.
3. **Routing.** A set of real prompts, labelled "needs the agent" or "self-contained". Measure:
   * the classifier's accuracy (wrong "local" decisions are the costly ones);
   * how many local answers are good enough.
4. **Models.** Compare the local models available in Ollama at the time for each use (classifying, condensing, answering), with speed and memory on the developer's machines. Classifying is likely fine with a small model (1.5–3B); condensing and answering likely need 7B or more. The spike records which.
5. **Outcome.** For each of FR-11.1 to FR-11.4: build, change, or drop, with the numbers. FR-11.1 is likely worth building whatever the outcome, since it's what shows the others' effect.

The spike's code and results go in `docs/spikes/`, like the earlier spikes.

---

## 5. Open decisions

* **Where local answers appear:** the panel (proposed), or inline in the prompt box.
* **Whether FR-11.5 (the API proxy) is ever in scope.**
* **Whether token usage goes in the history by default,** or only with an option.

---

## 6. Roadmap

Not scheduled. When picked up: the spike (section 4), then FR-11.1, then whichever of FR-11.2 to FR-11.4 the spike supports.
