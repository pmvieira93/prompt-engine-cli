# Spike: token baseline (Appendix 5, step 1)

Branch `spike/shared-run`, 2026-11-05. Partly done.

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

## 4. Not measured

Step 1 asked for two more items, and steps 2–5 of Appendix 5 §4 weren't started:

* how much of the input comes from pastes;
* how many turns are clarifications.

Tokens per session and context growth are covered above, except that the size just before `/compact` or `/clear` wasn't taken: only each session's peak.
