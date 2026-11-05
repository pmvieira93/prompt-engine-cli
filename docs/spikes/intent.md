# Spike: rules by intent from a JSON reply (Appendix 6, FR-12)

Branch `spike/shared-run`, 2026-11-05. Prototype, not production code.

**Question:** can the rewrite model return the prompt's intent reliably, so `append.txt` sections are added only when they fit, while rewrites keep working as they do today?

**Short answer:** not as proposed. The intent is reliable for `review` and `debug`, but weak for `test` and poor for `investigate` and `analysis`. Asking for JSON also changes the rewrites and takes about twice as long. A plain keyword match adds far fewer wrong sections, needs no model, and leaves the rewrite alone, at the cost of missing more.

---

## 1. How it was tested

The program is [`shared-run/main.go`](shared-run/main.go), shared with the [guard lines spike](guard-lines.md). It sent 43 real prompts to `qwen2.5:7b`, three times each (seeds 1–3), with today's request and with the same request asking for JSON. The reply format used Ollama's `format` schema: `response`, plus `intent` as one of `review`, `test`, `investigate`, `analysis`, `debug` or `none`.

The prompts were labelled by hand before any model output was seen:

| Label | Prompts |
|---|---|
| `none` | 15 |
| `debug` | 9 |
| `analysis` | 6 |
| `investigate` | 5 |
| `review` | 4 |
| `test` | 4 |

The prompts are real, so they stay local and only totals are in this report. `shared-run/analyse.py` prints every figure below from the run's output. `investigate` and `analysis` count as one section, as in the example `[investigate|analysis]`.

---

## 2. Intent results

| Section | Right when picked | Found of those labelled |
|---|---|---|
| `review` | 12 of 12 | 12 of 12 |
| `debug` | 27 of 35 | 27 of 27 |
| `test` | 9 of 21 | 9 of 12 |
| `investigate` + `analysis` | 8 of 11 | **8 of 33** |
| `none` | 37 of 50 | 37 of 45 |

Overall, 93 of 129 runs (72%) were right. In the other runs, 23 (18%) added the wrong section and 13 (10%) missed one. 39 of 43 prompts got the same section on every seed, so the model is steady even when it's wrong.

The labels `investigate` and `analysis` were mostly picked as `none`, `test` or `debug`. Some labels can be argued: a request for a verdict on tests could fairly be `test`.

---

## 3. What JSON does to the rewrite

| 129 runs each | Today | JSON |
|---|---|---|
| Rewrites that only changed case or punctuation, or nothing | 19 | **46** |
| Rewrites that differ from today's | n/a | 99 |
| Guard stops | 21 | 13 |
| "debug" added where it wasn't typed | 0 | **7** |
| Tokens in (median) | 207 | 254 |
| Tokens out (median) | 13 | 26 |
| Seconds (median) | 1.86 | 4.29 |
| Replies cut off, or JSON not parsed | n/a | 0 |

* **It rewrites less.** The model left the text close to as typed more than twice as often, and the fall in stops is probably down to that. Some of those rewrites were arguably better, because they added less, but it's a change in behaviour either way. FR-12 requires the current expand features to keep working as they are.
* **The intent leaks into the text.** With the intent in the same reply, "debug" appeared in 7 rewrites where it wasn't typed, against 0 today. In one prompt, a request to fix something became a request to debug it, on all three seeds.
* **It takes about twice as long,** in all three seeds: 2.41s against 1.2s in the fastest. Later seeds were slower for every variant, for a reason not found.
* **Parsing was never the problem.** With a schema, all 129 replies were valid JSON and none was cut off.

The noise is 35% for today's request against itself (30 of 86), so the 77% difference (99 of 129) is a real change.

---

## 4. Keywords for comparison

Appendix 6 §4 asks whether a keyword match would do. `keyword` in `analyse.py` matches a few words per section. It needs no model, so each prompt counts once:

| | Model (129 runs) | Keywords (43 prompts) |
|---|---|---|
| Right | 72% | 67% |
| Wrong section added | 18% | **5%** (2) |
| Section missed | 10% | **28%** (12) |

Keywords miss prompts that don't use the section's words: 10 of the 11 `investigate` and `analysis` prompts, and 2 of the 4 `review` prompts. A missed section is the milder failure, because the prompt just gets no rules. A wrong one is the "boring to see it every time" case FR-12 wants to remove.

**This list flatters keywords.** It was written after seeing the prompts and the results, and it's the same 43 prompts.

---

## 5. Limits

* **Small sample:** `review` and `test` have 4 prompts each.
* **One labeller:** the labels are open to review.
* **One model:** only `qwen2.5:7b`, and one developer's prompts.
* **Not tested:** a separate request just for the intent, a list of intents, and a stopped rewrite's intent (Appendix 6 §5).

---

## 6. Recommendation

**Change FR-12:**

1. Don't put the intent in the rewrite reply, so the rewrite request stays exactly as it is.
2. Use keyword sections such as `[test: test, tests, coverage]`. They need no model, work with `expand.on_enter: off`, add no time, and fit the one-rule-per-line format.
3. If keywords miss too much in use, test a separate intent-only request next. It wouldn't touch the rewrite, but it does add a request.

The decision is the developer's, and Appendix 6 isn't updated yet.
