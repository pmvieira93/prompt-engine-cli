# Spike: rules by intent from a JSON reply (Appendix 6, FR-12)

Branch `spike/shared-run`, 2026-11-05. Prototype, not production code.

**Question:** can the rewrite model return the prompt's intent reliably, so `append.txt` sections are added only when they fit, while rewrites keep working as they do today?

**Short answer:** not as proposed. The intent is reliable for `review` and `debug`, but weak for `test` and poor for `investigate` and `analysis`. Asking for JSON also changes the rewrites and takes about twice as long. A separate intent request, even with hints, adds wrong sections more often. A plain keyword match adds far fewer, needs no model, and leaves the rewrite alone, at the cost of missing more. Keywords were chosen.

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

## 5. A separate intent request

Ollama keeps no session: each request stands alone, so a prompt at the start of a session can't prepare it, and the intents have to go with every request. The JSON test above already did that. What could still help is a request of its own, which can't touch the rewrite, and telling the model what each section means.

[`shared-run/intent_call.py`](shared-run/intent_call.py) sent the same 43 prompts three times each with the sections as bare names, and with one-line hints such as "asks a question about how something works, with no code change". All 258 requests worked.

| | Bare names | With hints |
|---|---|---|
| Right | 61% | 70% |
| Wrong section added | 36% | 23% |
| Section missed | 2% | 7% |
| `investigate` + `analysis` found (of 33) | 18 | 12 |
| Same section on every seed (of 43) | 40 | 43 |
| Tokens in (median) | 54 | 92 |
| Seconds (median) | 1.53 | 1.53 |

* **Hints help, but not enough.** They cut wrong sections, but still add more than JSON with the rewrite (18%) or keywords (5%). Prompts that should get nothing got a section in 15 of 45 runs.
* **A separate request protects the rewrite,** but adds about 1.5 seconds per prompt, so it couldn't run on Enter without delaying it.
* **The hints were written once and not tuned.** Better ones might score higher.

---

## 6. Limits

* **Small sample:** `review` and `test` have 4 prompts each.
* **One labeller:** the labels are open to review.
* **One model:** only `qwen2.5:7b`, and one developer's prompts.
* **Not tested:** the word lists in real use.

---

## 7. Decision (2026-11-05)

**Use keyword sections,** such as `[test|tests|coverage]`, with sections and words defined by each developer. They need no model, work with `expand.on_enter: off`, add no time, and fit the one-rule-per-line format. The rewrite request stays exactly as it is. Appendix 6 has the new proposal.

If keywords miss too much in use, a separate intent request is the fallback to look at again.
