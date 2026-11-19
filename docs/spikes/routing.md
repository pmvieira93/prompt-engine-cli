# Spike: routing prompts (Appendix 5, step 3)

Branch `spike/routing`, 2026-11-19. Step 3 done; step 4 (other decision models) not yet.

**Question:** can a prompt be sorted before Claude Code sees it, cheaply and locally, so that `prompt-engine` knows when to offer a local answer (FR-11.3), a lighter model or a `/clear` (FR-11.4), or a local pre-investigation (FR-11.6)? And by what: keyword rules, a small non-generative classifier, or a small generative model?

**Short answer:** partly.
* **Whether a prompt starts a new task:** yes. A classifier on embeddings was right 90% of the time against 77% for always answering "continues". It never called a continuing prompt new, so it would never suggest `/clear` in the middle of a task.
* **What kind of work it is:** somewhat. The classifier was right 70% of the time against 53%, but it missed every prompt that asked to investigate (a bug, a review), the kind that would start FR-11.6. Keyword rules found 4 of those 6.
* **Whether the work is light or heavy:** no. Nothing beat always answering "light". "go ahead" or "fix all of them" says nothing about the work by itself.
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
* **Three approaches**, on the same prompts:
  * **keyword rules**: regular expressions for questions, investigation words ("bug", "review", "why"...), project references and back-references ("it", "go ahead", "yes"...), like FR-12's keyword sections;
  * **embeddings**: `nomic-embed-text` through Ollama, with a logistic regression on top (for `new`, on the similarity to the previous prompt and a few flags). Trained either on the derived labels or on the hand labels, and always tested on sessions it didn't learn from (5 folds split by session);
  * **generative**: `qwen2.5:3b` through Ollama, given the prompt, the previous prompt and the three labels' meanings, answering with a JSON schema.

```sh
python3 -I docs/spikes/routing/routing.py extract   # the prompts and derived labels
python3 -I docs/spikes/routing/routing.py sample    # the 60 to label by hand
python3 -I docs/spikes/routing/routing.py run       # compare, print totals
```

---

## 2. Results

Against the 60 hand labels. The first row is what always giving the commonest answer scores.

| | kind | tier | new task? |
|---|---|---|---|
| Always the commonest answer | 53% (`agent`) | 75% (`light`) | 77% (`no`) |
| Keyword rules | 63% | 62% | 78% |
| Embeddings, trained on the derived labels | 32% | 50% | — |
| **Embeddings, trained on the hand labels** | **70%** | 75% | **90%** |
| `qwen2.5:3b` | 48% | 68% | 53% |
| The derived labels themselves | 50% | 63% | 73% (of 11 known) |

The 60 split as: kind 22 `self`, 6 `inv`, 32 `agent`; tier 45 `light`, 15 `heavy`; new 14 `yes`, 46 `no`.

**Time per prompt**, with the model loaded: rules under 1 ms; embeddings about 14 ms; `qwen2.5:3b` 0.82 s (median).

### 2.1 Per label

* **New task.** The embeddings classifier called 8 prompts new, all rightly, and found 8 of the 14 new tasks; every one of the 46 continuing prompts was kept as continuing. The rules found 7 of the 14 but called 6 continuing prompts new. `qwen2.5:3b` called 20 prompts new and was right on 3.
* **Kind.** The embeddings classifier found 16 of the 22 `self` questions and 26 of the 32 `agent` tasks, and none of the 6 `inv`. The rules found 4 of the 6 `inv` ("review it first", "what's the issue with F9") but only 9 of the 22 `self`. `qwen2.5:3b` called 23 prompts `inv`, 4 rightly.
* **Tier.** The embeddings classifier, with only 15 `heavy` examples to learn from, said `heavy` 4 times and found 2 of them. The rules and `qwen2.5:3b` found more `heavy` prompts (9 and 10 of 15), at the cost of calling many light ones heavy. Most prompts are short replies ("commit, merge and push", "go ahead"): whether one is heavy depends on what came before, not on its words.

### 2.2 The derived labels

Against the derived labels, the embeddings classifier trained on them looked best (71% on tier against a 57% baseline, 50% on kind against 37%). Against the hand labels, the same classifier fell below the baseline: it had learned the derived labels, and they were wrong too often.

* **`inv`**: 20 prompts in the sample read files before editing, and only 1 of them was an investigation by hand. Claude reads files before most edits.
* **`heavy`**: many tool calls doesn't mean heavy work. "commit, merge and push" takes several calls and is light.
* **`new`**: the transcripts rarely show it; 104 of the 669 prompts have one.

So labels worked out from the transcripts can't stand in for the developer's. A classifier for this has to learn from labels given by hand.

---

## 3. What this doesn't show

* **The sample is small and narrow.** 60 prompts, 6 of them `inv`; 38 come from one long session on this project. The sample was drawn evenly across the derived kinds, so its mix isn't the developer's real one. One more label right or wrong moves a result by almost 2 points.
* **The hand labels started as Claude's.** The developer accepted all 60 proposals, which may lean the labels toward how Claude reads the prompts.
* **Whether local answers are good enough** (step 3's last measure) wasn't measured: that needs the `self` prompts answered by a local model and judged, which is FR-11.3's own spike.
* **Memory** alongside the models already loaded for ghost text and rewrites (step 4).
* **Other decision models.** Two were raised after the run:
  * **Laya** (Convai Innovations, Apache 2.0): ModernBERT-based checkpoints of 322–421M parameters, the kind of non-generative classifier Appendix 5, section 2 names. It takes a choice among labels described in words, and returns a confidence. Its own published figures say that without fine-tuning it scores below the majority baseline on its benchmark, and that its headline result needs fine-tuning on about 30,000 examples. It runs in Python (PyTorch) or as its own local server, not through Ollama. It could be run on the same 60 prompts as step 4.
  * **Jev** (TypeSafe AI) is a hosted service: the prompts would leave the machine, which this spike rules out.

---

## 4. What it suggests

For the developer to decide (Appendix 5, section 10):

1. **The `/clear` hint (FR-11.4) is the one to build first.** Telling a new task from a continuation works, errs on the safe side, and section 8.4 already found it the more valuable hint: switching model or clearing pays off mainly at the start of a task. It would combine the classifier with the context size from FR-11.1.
2. **A model hint can't come from the prompt alone.** Light or heavy wasn't readable from the words typed. A hint would need what the session is doing (the last turns' tool calls, the context size), or should be dropped.
3. **FR-11.6 should start from an explicit trigger.** No approach found investigation prompts reliably. Words the developer chooses, like FR-12's sections, or a shortcut, are more reliable than a classifier for now.
4. **Local answers (FR-11.3) need more labels.** The classifier finds most `self` questions, but 6 of the 22 it called `self` weren't. Offered beside the prompt, a wrong one costs a wasted local answer, not a wrong reply.
5. **A small generative model isn't worth it for routing.** `qwen2.5:3b` was below the embeddings on every label and 60 times slower.
6. **Labels come from the developer.** Any classifier here needs hand labels. The history (FR-5.4) could gather them as the developer works: a hint followed or dismissed is a label.
