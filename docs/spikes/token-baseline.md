# Spike: token baseline (Appendix 5, step 1)

Branch `spike/shared-run`, 2026-11-05. Pastes and clarifications (section 4) added on branch `spike-pastes-clarifications`, 2026-11-09. Step 1 done.

**Question:** where do Claude Code's tokens go in real use, before anything is built to reduce them?

**Short answer:** almost all of them are context read back from the cache (97.9%). Typed prompts are about 0.001% of the total when each is counted once. They are read back on later calls too, so their true share is higher, but it is still tiny next to the context around them. Savings have to come from how much context a session carries and how many calls each prompt causes.

---

## 1. How it was measured

[`token-baseline/baseline.py`](token-baseline/baseline.py) reads Claude Code's local transcripts (`~/.claude/projects/*/*.jsonl`) and prints totals only. Nothing is sent anywhere, and no prompt text is printed. It counts each API call once by message id, takes the tokens from each call's `usage`, and counts as typed prompts the user messages that aren't tool results or notices.

The local model isn't involved. It's listed with the [guard lines](guard-lines.md) and [intent](intent.md) spikes only because they were planned together.

---

## 2. Results

63 transcripts, 55 sessions with usage, 504 typed prompts, 7 projects.

| Where the tokens go | Share |
|---|---|
| Cache read | 97.90% |
| Cache write | 1.87% |
| Output | 0.22% |
| Fresh input | under 0.01% |

Total: about 1,200M tokens. Typed text is about 11,400 tokens at 4 characters per token, or **about 0.001%**, counting each prompt once. A prompt stays in the context and is read back on later calls, which this figure leaves out. For scale, all 504 prompts together are about a twelfth of one median session's peak context.

| Sessions with 5 or more prompts | All (6) | Without the longest (5) |
|---|---|---|
| Prompts per session (median) | 11 | 6 |
| API calls per session (median) | 50 | 9 |
| Peak context in tokens (median) | 132,702 | 49,758 |
| Peak context in tokens (max) | 941,311 | 941,311 |
| Tokens per typed prompt (median) | 421,489 | 63,048 |

Across all sessions, each typed prompt causes 6.7 API calls. Prompts run to a median of 8 words, 32 at the 90th percentile, and 211 at most.

**One session outweighs the rest.** The session that built this spike has 803M tokens and 304 prompts, two thirds of the total. The next has 377M and 94 prompts, so those two are almost everything. Treat the medians as a rough guide, and "without the longest" as closer to a normal session.

---

## 3. What it means for Appendix 5

* **Rewriting doesn't save tokens by length.** Typed text is a small part of what each call reads back, so shorter prompts would change little. Any saving would come from fewer wrong turns, which isn't measured yet.
* **Context size is the lever.** Each API call reads the session's context back, so a long session pays for it again on every call. `/compact` and `/clear` timing, and how much a session carries, matter far more than the prompt.
* **Rewrite tokens are local.** The tokens in the other two spikes are Ollama's, so they cost time rather than Claude usage.

---

## 4. Pastes and clarifications

Measured 2026-11-09 with [`token-baseline/turns.py`](token-baseline/turns.py), on the same kind of data: 63 sessions with usage, 633 prompts, 3,756 API calls, 7 projects. Prompts here include answers picked in `AskUserQuestion`. It prints totals only. One session (this spike's own, 2,630 API calls) is two thirds of the calls, so every figure is also given without it.

### 4.1 Pastes

Claude Code tags a paste of 3 or more lines as `<pasted_content id="…">` in the transcript. Shorter pastes look like typing, so these are lower bounds.

| | All sessions | Without the longest |
|---|---|---|
| Prompts with a tagged paste | 21 of 633 (3.3%) | 11 of 246 (4.5%) |
| Pasted share of what was sent | 21.5% of the characters | 9.8% |
| Images pasted | 10 | 0 |

All 21 pastes are in this project, and there are 8 different ones. Seven are short probes from testing paste tagging and guidance, under 200 characters each. The eighth, a 1,500-character instruction text pasted into 7 prompts, is 86% of the pasted characters. The other 6 projects have no tagged paste at all. None of the pastes is the kind FR-11.2 is meant for: a long log, stack trace or diff.

**Share of the input tokens.** A message stays in the context, and every later API call reads it again until `/compact`. Counting each paste once per later call, pastes are **0.1%** of the input tokens and typed text 0.2% (at 4 characters per token).

The rest of the input splits like this:

| What the context reads back | Share of the input tokens |
|---|---|
| Claude's own replies and tool calls (text and tool input, not thinking) | 19.7% |
| Tool results (files read, command output) | 12.1% |
| System prompt and tools (each session's first call, less the messages before it, sent again by every call) | 10.0% |
| Attachments (environment, reminders) | 4.5% |
| Other user records (command output, notices) | 2.4% |
| Typed text | 0.2% |
| Pastes | 0.1% |
| Not accounted for | 51.0% |

Earlier turns' thinking isn't counted: the API drops it. Within a turn, thinking stays in the context while Claude uses tools, but the transcript doesn't keep its text. That, images, and the 4-characters-per-token estimate make up the last row. Measured from the context's growth between calls, the context grows by about one token per 1.7 characters of transcript. Even at that rate, which overstates plain text, typed text is 0.4% and pastes 0.2% of the input tokens.

### 4.2 Clarifications

Claude's questions fall into three kinds, by how the question in its last paragraph is worded, or by the `AskUserQuestion` question:

* **permission:** it offers the next step ("Shall I merge it?");
* **next step:** it offers a choice of what to do next ("Shall I merge it, or write the notes first?", "Which would you like?");
* **clarification:** anything else. It asks for a choice about the task ("How should this behave?", "Where should the guard rules live?") or for information ("Which of the two happened?").

Only clarifications are turns a clearer first prompt could have avoided. The split was checked by hand against the replies, and a handful of each kind lands on the wrong side.

| Prompts that answer… | All sessions | Without the longest |
|---|---|---|
| a question asking permission | 110 (17.4%) | 20 (8.1%) |
| a question offering a choice of next step | 41 (6.5%) | 7 (2.8%) |
| a clarification | 48 (7.6%) | 16 (6.5%) |
| nothing: they correct Claude unasked ("no, …", "I meant …") | 5 (0.8%) | 2 (0.8%) |

Of the 48 clarifications, 18 were asked with `AskUserQuestion`, and about 12 ask for the result of a hand test ("did both sides redraw cleanly?"). The rest are design decisions that came up during the work: how a feature should behave, where a file should live, which model to compare.

**Clarifications are uncommon, and mostly not about the prompt.** About one prompt in 13 answers one. Most ask for a decision the developer hadn't made yet, or for something only the developer could see, such as a hand-test result. A clearer first prompt wouldn't have answered them. Fewer than 1 prompt in 100 corrects a misunderstanding. Permission and next-step questions are common, but they are the workflow working as asked: in this project, Claude commits, merges and releases only when told.

---

## 5. What it means for Appendix 5

* **FR-11.2, condensed pastes: drop.** Large pastes are rare in this developer's work, and they are a fraction of a percent of the tokens even when read back on every call. Condensing them can't save anything that shows.
* **Rewrites don't pay for themselves in tokens.** Typed text is under half a percent of the input, and clarifications that a clearer prompt would have avoided are a small part of 7.6% of prompts. A rewrite's value is a prompt that says what the developer means ([Appendix 6](../PRD-APPENDIX-6.md)), so that Claude does the right work. Rework after a misunderstood prompt would cost far more than a clarifying question, but it wasn't measured here: unasked corrections (0.8%) are only its most visible part.
* **The lever is what Claude Code carries, not what the developer sends.** Claude's own replies, tool results, the system prompt and thinking are nearly all of it. `prompt-engine` sees none of them before they're sent. FR-11.4 (hints for `/compact`, `/clear` and a lighter model) is the only item aimed there. FR-11.5, the proxy below Claude Code, is the only one that could change them directly.
* **FR-11.1, cost visibility, is still worth building.** It's cheap, and it's what would show any of the others working.

Steps 2 and 3 of the spike (condensing and routing with local models) aren't needed for FR-11.2: the baseline already decides it. Routing (FR-11.3) is a question about local answers, not about the prompt's size, and needs real prompts labelled by hand to judge.

**Caveat:** one developer, two weeks, one project doing most of the work. A developer who pastes logs all day would see different numbers. `turns.py` runs on anyone's transcripts.
