# Spike: routing prompts (Appendix 5, step 3)

Branch `spike/routing`, 2026-11-19. Step 3 done. Eight decision models through Ollama added on 2026-11-20 (Laya, Jev and six smaller ones).

**Question:** can a prompt be sorted before Claude Code sees it, cheaply and locally, so that `prompt-engine` knows when to offer a local answer (FR-11.3), a lighter model or a `/clear` (FR-11.4), or a local pre-investigation (FR-11.6)? And by what: keyword rules, a small non-generative classifier, a small generative model, or a model made for decisions?

**Short answer:** mostly, with two models side by side.
* **Whether a prompt starts a new task:** yes. A classifier on embeddings, trained on the developer's labels, was right 90% of the time against 77% for always answering "continues". It never called a continuing prompt new, so it would never suggest `/clear` in the middle of a task. It needs 0.3 GB and 14 ms.
* **Whether the work is light or heavy:** yes, with a decision model of 4B or more. Perplexity's `pplx-decider-flash` (4B, 4-bit) was right 88% of the time against 75% for always answering "light", and right on all 42 answers it gave with a confidence of 0.5 or more. It takes 4.6 GB, which doesn't grow with use, and 1 s per prompt. Jev (9B) did a little better (90–92%) in 8–16 GB, growing with use. Nothing without a decision model beat the baseline: "go ahead" or "fix all of them" says little about the work by itself.
* **What kind of work it is:** partly. The best reached 70–73% against 53% (the embeddings classifier, `d3` and `pplx-decider-flash`), but no approach found the investigation prompts (a bug, a review) reliably, and those would start FR-11.6.
* **Labels:** labels worked out from what Claude did in each turn turned out to be poor stand-ins for the developer's own (50% agreement on the kind), so a classifier has to learn from labels given by hand.

---

## 1. How it was measured

[`routing/routing.py`](routing/routing.py) reads Claude Code's local transcripts and prints totals only. The prompts, the labels and the models' answers stay in `~/.local/state/prompt-engine/spike-routing/`; Ollama runs locally, and nothing is sent anywhere.

* **669 typed prompts** from 64 sessions, each with the prompt before it. Slash commands are left out.
* **Three labels per prompt** (Appendix 5, section 4, step 3):

  | Label | Values | For |
  |---|---|---|
  | kind | `self`: a question answered without the project; `inv`: a bug, ticket or review to investigate first; `agent`: other work for the agent | FR-11.3, FR-11.6 |
  | tier | `light`: a small task a lighter model could do; `heavy`: the rest | FR-11.4, model hint |
  | new | `yes`: starts a new task; `no`: continues the last one | FR-11.4, `/clear` hint |

* **Two sets of labels:**
  * **derived**, for all 669, from what Claude did in the turn: no tools means `self`, three or more reads before the first edit means `inv`; at most 3 tool calls and 1,500 output tokens means `light`; the first prompt of a session, or no files shared with the last three turns, means a new task. Most prompts (565 of 669) get no derived `new`.
  * **by hand**, for a sample of 60 (20 of each derived kind). Claude proposed the three labels for each prompt, from the prompt and the one before it, and the developer checked them in six rounds of ten. All 60 were accepted as proposed.
* **Approaches**, on the same prompts:
  * **keyword rules**: regular expressions for questions, investigation words ("bug", "review", "why"...), project references and back-references ("it", "go ahead", "yes"...), like FR-12's keyword sections;
  * **embeddings**: `nomic-embed-text` through Ollama, with a logistic regression on top (for `new`, on the similarity to the previous prompt and a few flags). Trained either on the derived labels or on the hand labels, and always tested on sessions it didn't learn from (5 folds split by session);
  * **generative**: `qwen2.5:3b` through Ollama, given the prompt, the previous prompt and the three labels' meanings, answering with a JSON schema;
  * **decision models**, through Ollama's decision endpoint (`/v1/systemone`), on the 60 hand-labelled prompts only. Each gets the prompt and the previous prompt, and three questions whose options are described in words; it picks one per question, with a confidence. No training and no examples: the descriptions are all they get, written once and not tuned. Ollama lists 14 decision models; these were tried, leaving out those of 27B or more, those made for images, and one for non-commercial use only:

    | Model (Ollama tag) | Publisher | License | Parameters | On disk |
    |---|---|---|---|---|
    | `kev:0.8b-q8_0` | Jared Palmer | Apache 2.0 | 0.8B | 0.8 GB |
    | `tev1:0.8b-q8_0` | Together AI | not stated for the model | 0.8B | 0.8 GB |
    | `d1-omni:600m` | Liquid AI | LFM Open License 1.0 | 0.6B | 0.85 GB |
    | `laya` | Convai Innovations | Apache 2.0 | 421M, ModernBERT, no text generation | 0.8 GB |
    | `strands-decider:2b-nvfp4` | Strands Agents | Apache 2.0 | 2B, 4-bit, Apple Silicon only | 1.3 GB |
    | `d3:2b` | vLLM Semantic Router | Apache 2.0 | 2B | 3.1 GB |
    | `pplx-decider-flash:4b-nvfp4` | Perplexity | Apache 2.0 | 4B, 4-bit, built on Qwen3.5 | 4.6 GB |
    | `autotrust-jev:9b-nvfp4` | AutoTrust | Apache 2.0 | 9B, 4-bit | 8.0 GB |
    | `autotrust-jev:9b` | AutoTrust | Apache 2.0 | 9B, Jev's smallest size | 11 GB |

    Jev needs Ollama 0.40.16 or later.

```sh
python3 -I docs/spikes/routing/routing.py extract   # the prompts and derived labels
python3 -I docs/spikes/routing/routing.py sample    # the 60 to label by hand
python3 -I docs/spikes/routing/routing.py run       # compare, print totals
```

`ROUTING_DECISION=<tag>,<tag>` picks the decision models `run` asks.

---

## 2. Results

Against the 60 hand labels. The first row is what always giving the commonest answer scores.

| | kind | tier | new task? |
|---|---|---|---|
| Always the commonest answer | 53% (`agent`) | 75% (`light`) | 77% (`no`) |
| Keyword rules | 63% | 62% | 78% |
| Embeddings, trained on the derived labels | 32% | 50% | — |
| **Embeddings, trained on the hand labels** | 70% | 75% | **90%** |
| `qwen2.5:3b` | 48% | 68% | 53% |
| `kev` 0.8B | 53% | 68% | 57% |
| `tev1` 0.8B | 52% | 68% | 72% |
| `d1-omni` 0.6B | 57% | 72% | 70% |
| Laya | 68% | 65% | 53% |
| `strands-decider` 2B | 60% | 78% | 28% |
| `d3` 2B | **73%** | 83% | 57% |
| **`pplx-decider-flash` 4B** | 70% | **88%** | 82% |
| Jev 9B, 4-bit | 63% | **92%** | 73% |
| Jev 9B | 65% | **90%** | 73% |
| The derived labels themselves | 50% | 63% | 73% (of 11 known) |

The 60 split as: kind 22 `self`, 6 `inv`, 32 `agent`; tier 45 `light`, 15 `heavy`; new 14 `yes`, 46 `no`.

### 2.1 Cost

On an Apple Silicon Mac with 24 GB; memory from `ollama ps`, each model loaded alone; times are medians for the three questions, with the model loaded.

| Model | Used for | Memory after 1 prompt | after 60 | Per prompt | First answer from cold |
|---|---|---|---|---|---|
| `qwen2.5-coder:1.5b` | ghost text, today | 1.1 GB | | | |
| `qwen2.5:7b` | rewrites, today | 4.6 GB | | | |
| Keyword rules | | | | under 1 ms | |
| `nomic-embed-text` | the embeddings classifier | 0.3 GB | | 14 ms | |
| `qwen2.5:3b` | generative routing | 2.1 GB | | 0.82 s | |
| `kev` 0.8B | decision | 4.4 GB | 4.4 GB | 0.22 s | 1.6 s |
| `tev1` 0.8B | decision | 1.6 GB | 1.6 GB | 0.26 s | 0.8 s |
| `d1-omni` 0.6B | decision | 2.8 GB | 2.8 GB | 0.11 s | 1.1 s |
| Laya | decision | 1.0 GB | | 0.49 s | 1.4 s |
| `strands-decider` 2B | decision | 1.4 GB | **7.3 GB** | 0.46 s | 1.3 s |
| `d3` 2B | decision | 3.1 GB | 3.1 GB | 0.69 s | 1.4 s |
| `pplx-decider-flash` 4B | decision | 4.6 GB | 4.6 GB | 0.98 s | 3.4 s |
| Jev 9B, 4-bit | decision | 8.2 GB | **16 GB** | 1.6 s | 3.4 s |
| Jev 9B | decision | 11 GB | **19 GB** | 1.6 s | |

Three models grew with use, all on Ollama's MLX runner: Jev 4-bit went from 8.2 GB after 1 prompt to 9.1 GB after 5, 12 GB after 20 and 16 GB after 60, and `strands-decider` from 1.4 to 7.3 GB. Whether they stop growing wasn't measured. The others stayed the same. Some small models take more memory than their size suggests (`kev` 0.8B: 4.4 GB), likely from the context they reserve.

### 2.2 Per label

* **New task.** The embeddings classifier called 8 prompts new, all rightly, and found 8 of the 14 new tasks; every one of the 46 continuing prompts was kept as continuing. `pplx-decider-flash` found 10 of the 14 but called 7 continuing prompts new; the rules found 7 and called 6 wrongly; Jev found 8 and called 10 wrongly. `d3` and `strands-decider` called most prompts new (36 and 55 of 60).
* **Tier.** `pplx-decider-flash` called 16 prompts heavy, 12 rightly, and found 12 of the 15 heavy ones; Jev called 15, 12 rightly; `d3` called 9, 7 rightly. The embeddings classifier, with only 15 `heavy` examples to learn from, said `heavy` 4 times and found 2 of them. Most prompts are short replies ("commit, merge and push", "go ahead"), whose weight depends on what came before; only the decision models of 2B or more did better than guessing.
* **Kind.**
  * `self` (local answers): `d3` and the embeddings classifier both found 16 of the 22, each wrong 6 times when it said `self`. `pplx-decider-flash` found 12 and was wrong once; Jev found 5 and was never wrong. Laya called 34 `self`, 13 wrongly.
  * `inv` (FR-11.6): `strands-decider` found 5 of the 6, calling 12 prompts `inv`; the rules, Laya and Jev found 4; `pplx-decider-flash` 3, calling 11; the embeddings classifier none.

### 2.3 The derived labels

Against the derived labels, the embeddings classifier trained on them looked best (71% on tier against a 57% baseline, 50% on kind against 37%). Against the hand labels, the same classifier fell below the baseline: it had learned the derived labels, and they were wrong too often.

* **`inv`**: 20 prompts in the sample read files before editing, and only 1 of them was an investigation by hand. Claude reads files before most edits.
* **`heavy`**: many tool calls doesn't mean heavy work. "commit, merge and push" takes several calls and is light.
* **`new`**: the transcripts rarely show it; 104 of the 669 prompts have one.

So labels worked out from the transcripts can't stand in for the developer's. A classifier for this has to learn from labels given by hand.

### 2.4 The decision models' confidence

Each answer comes with a confidence, so a hint could be offered only when the model is sure. Answers with a confidence of 0.5 or more, and how many of them were right:

| Model | kind | tier | new |
|---|---|---|---|
| `kev` 0.8B | 21, 81% | 22, 86% | none |
| `tev1` 0.8B | 46, 65% | 24, 88% | 15, 73% |
| `d1-omni` 0.6B | 38, 71% | 23, 91% | 19, 79% |
| Laya | none | 7, 100% | 7, 71% |
| `strands-decider` 2B | 4, 75% | 12, 83% | 27, 30% |
| `d3` 2B | 30, 90% | 32, 94% | 27, 56% |
| **`pplx-decider-flash` 4B** | 45, 82% | **42, 100%** | 41, 85% |
| Jev 9B, 4-bit | 33, 82% | 29, 100% | 17, 82% |
| Jev 9B | 33, 82% | 33, 97% | 19, 84% |

`pplx-decider-flash`'s confidence means the most: it is sure of more answers than the others, and right on them; at 0.7 or more, its 35 `kind` answers were right 34 times. Laya is unsure of almost everything here: on kind, 1 of its 60 answers reached 0.3. Laya's own page puts it at 65% on kinds of task it wasn't trained for, and short developer replies like these are far from them.

---

## 3. What this doesn't show

* **The sample is small and narrow.** 60 prompts, 6 of them `inv` and 15 `heavy`; 38 come from one long session on this project. The sample was drawn evenly across the derived kinds, so its mix isn't the developer's real one. One more label right or wrong moves a result by almost 2 points, so the order of models within a few points of each other means little.
* **The hand labels started as Claude's.** The developer accepted all 60 proposals, which may lean the labels toward how a large language model reads the prompts. That could favour the larger decision models.
* **Whether local answers are good enough** (step 3's last measure) wasn't measured: that needs the `self` prompts answered by a local model and judged, which is FR-11.3's own spike.
* **The models over a whole session.** Memory was measured over 60 prompts; how far the growing ones go over hundreds, and how the machine behaves with several models loaded near its limit, weren't measured.
* **Linux.** Every run was on an Apple Silicon Mac. `pplx-decider-flash`'s 4-bit tag and `strands-decider` use the MLX runner, which is for Macs only; `pplx-decider-flash` has a 5.6 GB tag, not tried, and `d3` and the embeddings run on Linux too.
* **Tuned option wordings.** The decision models' options were described once. Better wordings might help some, but tuning them on these 60 would only fit the test.

---

## 4. What it suggests

For the developer to decide (Appendix 5, section 10):

1. **The `/clear` hint (FR-11.4) is the one to build first,** with the embeddings classifier: 90%, never wrong on a continuing prompt, 0.3 GB and 14 ms. Section 8.4 already found it the more valuable hint: switching model or clearing pays off mainly at the start of a task. It would combine the classifier with the context size from FR-11.1.
2. **The model hint can come from `pplx-decider-flash`,** offered only when it is confident (0.5 or more: 42 of 60 prompts, all right). It adds 4.6 GB that doesn't grow, and about 1 s after the prompt is sent. With the ghost-text and rewrite models it makes about 10.6 GB, which fits a 24 GB Mac. Jev was slightly more accurate but needs 8–16 GB, growing with use.
3. **FR-11.6 should start from an explicit trigger.** The best found 4 or 5 of the 6 investigation prompts, and called others investigations too. Words the developer chooses, like FR-12's sections, or a shortcut, are more reliable for now.
4. **Local answers (FR-11.3) need more labels.** `pplx-decider-flash` and Jev are rarely wrong when they say `self`, but miss many; the embeddings classifier and `d3` find more but are wrong more. Offered beside the prompt, a wrong one costs a wasted local answer, not a wrong reply.
5. **The small models aren't worth it for routing.** `qwen2.5:3b` and the decision models under 1 GB were near the baseline; `strands-decider` called most prompts new tasks and grew to 7.3 GB. Laya matched the classifier on kind without training, but was unsure of nearly every answer; it could be looked at again once there are enough labels to train it.
6. **Labels come from the developer.** Any classifier here needs hand labels. The history (FR-5.4) could gather them as the developer works: a hint followed or dismissed is a label.
