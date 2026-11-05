# Spike: telling the model the built-in guards (Appendix 4, FR-10.8)

Branch `spike/shared-run`, 2026-11-05. Prototype, not production code.

**Question:** if the rewrite instructions name the built-in guards, do fewer rewrites get stopped, without more stops elsewhere?

**Short answer:** no. Stops went from 21 to 19 across 129 runs, and the same 8 of 43 prompts were stopped. Two guards stopped firing, but others fired more, so the lines fail the test in Appendix 4 §7. The recommendation is to drop them and fix a false alarm in the `names` check instead, which removes more stops.

---

## 1. How it was tested

The program is [`shared-run/main.go`](shared-run/main.go), shared with the [intent spike](intent.md). It sent 43 real prompts from the developer's own history to `qwen2.5:7b`, three times each (seeds 1–3), at temperature 0.2, with the same request and token limit as prompt-engine. Each reply went through `expand.CheckSession`, the check auto mode uses.

The prompts are real, so they stay local and only totals are in this report. `shared-run/analyse.py` prints every figure below from the run's output.

| Variant | Instructions |
|---|---|
| Today | `DefaultSystem` (or `TypoSystem`), unchanged |
| Guard lines | The same, plus the lines below |

Lines added, as proposed in Appendix 4 §7:

| Guard | When | Line |
|---|---|---|
| `destructive` | Always | "Never add actions that delete, reset, push or overwrite anything." |
| `question` | The prompt looks like a question | "If the request is a question, keep it a question: don't turn it into a task." |
| `negation` | It has a negation | "Keep every not, never, without and only." |
| `quotes` | It has quoted text | "Copy quoted text exactly." |

---

## 2. Results

| 129 runs each | Today | Guard lines |
|---|---|---|
| Guard stops | 21 | 19 |
| Prompts stopped at least once (of 43) | 8 | 8 |
| Tokens in (median) | 207 | 235 |
| Tokens out (median) | 13 | 13 |
| Seconds (median) | 1.86 | 2.03 |
| Replies cut off | 0 | 0 |

| Guard | Today | Guard lines |
|---|---|---|
| `names` | 8 | 9 |
| `replaced` | 6 | 8 |
| `negation` | 4 | 0 |
| `asked-back` | 3 | 0 |
| `dropped-part` | 0 | 2 |

Run by run, 15 stops happened in both variants, 6 went away and 4 were new.

**Noise.** With the same prompt and instructions, a rewrite differs from its first seed in 30 of 86 runs (35%). The two variants differ in 68 of 129 (53%). The lines do change the rewrites, but a change of 2 stops in 129 can't be told apart from that noise.

**Time.** Later seeds were slower for every variant, for a reason not found, so the time difference is only a rough guide.

---

## 3. What the numbers show

* **`negation` went to 0, but the prompts didn't get through.** One prompt stopped by `negation` today was stopped by `names` with the line, because the rewrite then lost a label instead.
* **The biggest guards weren't helped.** `names` and `replaced` make up 14 of today's 21 stops, and both already have a line in `DefaultSystem`. Telling the model more doesn't reach them.
* **`names` has false alarms.** One prompt was stopped 9 times across the 3 variants (3 today) with the rewrite otherwise fine. The rewrite had reworded a word pair written with a slash, and had dropped an abbreviation with a dot. `LostNames` (`internal/expand/guard.go`) treats any word with one of `nameChars` as a name, and both contain one. That's 9 of all 53 stops in the three variants of the run, more than the lines removed.
* **The other `names` stops were real:** the rewrite dropped a test label or a timestamp from a bug report.

---

## 4. Not tested

* **`language`:** there was no line for it, and the prompts look to be all English.
* **`destructive` and `question`:** neither guard fired in any variant, so their lines showed nothing either way.
* **`system.txt`:** the runs used the default instructions, so the two gaps in Appendix 4 §7 (lines lost with `system.txt`, and a guard turned off still being told) weren't measured. They are still open.

**Appendix 4 correction.** The table in §7 lists `language` as not told to the model. `Expand` already names a non-English language in the request label ("Request (in Portuguese; write the rewrite in Portuguese): …"), so it is told in part.

---

## 5. Recommendation

**Drop the new lines.** They add about 28 tokens to every request with no measurable gain.

**Fix the `names` false alarms instead:** don't treat ordinary words with a slash, or abbreviations with a dot, as names. This needs care, because paths such as `src/app` have a slash too and must stay protected.

The decision is the developer's, and Appendix 4 isn't updated yet.
