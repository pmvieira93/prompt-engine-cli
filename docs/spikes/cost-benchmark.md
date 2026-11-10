# Cost benchmark: Claude alone vs with prompt-engine (Appendix 5, section 8)

Branch `feat/benchmark-tasks`, 2026-11-10. Harness and project in [`bench/`](../../bench/README.md).

**Question:** does `prompt-engine` as it is today change what a task costs in Claude Code: tokens, dollars, turns, time, or whether the task gets done?

**Short answer:** no measurable difference. Over 12 tasks, 3 runs each, both setups finished every task, with the same number of turns on average (7.06) and a mean cost per run 0.8% lower with `prompt-engine` (1.1% on the task medians), well inside the run-to-run noise (about 5% within a task, up to 12% between two setups that sent the very same prompt). The rewrite adds about a second. This is what section 8.4 expected: what the developer types is under 1% of the input, so rewriting it can't move the cost much. The benchmark is now the baseline that FR-11.3, FR-11.4 and FR-11.6 have to beat.

---

## 1. How it was measured

* **12 tasks** on a small Go project (`ledger`, about 330 lines with its tests): 6 bugs and 6 features. Some prompts name what to change ("add csv export for the transactions"); others only describe a symptom ("the budget says I still have money left for groceries but I definitely overspent") or a wish ("I want to see where most of my money goes"). A hidden test, added after Claude finishes, decides whether the task is done.
* **Two setups**, each task **3 times**, 72 sessions in all:
  * **alone:** the prompt as typed, to `claude -p`;
  * **pe:** the prompt after `prompt-engine --pe-rewrite`, with the developer's own config: the `default` style and `qwen2.5:7b` for rewrites, the built-in guard rails, and no `append.txt`, so **no guidance**. This is the rewrite only, as the developer uses it today.
* **The same for both:** Claude Code 2.1.311, Sonnet (`claude-sonnet-5-5` on every call), a fresh copy of the project for each run, no user settings, plugins or MCP servers, the same tools allowed. The setups took turns going first, task by task.
* Cost, tokens and turns are Claude Code's own figures (`--output-format json`); time is the wall time of the session plus the rewrite.

```sh
python3 -I bench/run.py run --runs 3
python3 -I bench/run.py report bench/results/<file>.jsonl --chart docs/spikes/cost-benchmark.svg
```

The raw results stay local; they hold the task prompts (which are in the repo anyway), not the developer's own.

---

## 2. Results

![Median cost and turns per task, Claude alone vs with prompt-engine](cost-benchmark.svg)

| | Claude alone | with prompt-engine |
|---|---|---|
| Tasks done | 36 of 36 runs | 36 of 36 runs |
| Cost, sum of the 12 task medians | $0.730 | $0.722 (−1.1%) |
| Mean cost per run | $0.0607 | $0.0602 (−0.8%) |
| Mean turns per run | 7.06 | 7.06 |
| Mean time per run, rewrite included | 12.6 s | 13.9 s (+1.3 s) |
| The project's tests, and Claude's own, all passing | 36 of 36 | 36 of 36 |

Total spend: $4.35 for the 72 sessions.

### 2.1 The rewrite

* **Used in 8 of the 12 tasks** (24 of 36 runs). The model gave the same result in all three runs of each task, so a task was either always rewritten or never.
* **Stopped by a guard rail in the other 4**, on every run, and sent as typed:
  * `added-task` in 3: the rewrite added "check", "fix, handle" or "ensure" to a bug report that only described the symptom;
  * `asked-back` in 1: for "let me search my transactions", the model asked a question instead of rewriting.
* **Small changes when used:** a capital letter and a full stop ("I want to see where most of my money goes."), or a sentence restated ("Fix the issue where splitting a bill between people results in lost cents."), with the git branch added as context. The median prompt went from 52 to 71 characters.
* **Time:** median 0.8 s, at most 2.3 s, with the model already loaded.

### 2.2 How big the noise is

Two checks say the differences per task are noise, not the rewrite:

* **The same prompt, different costs.** In the 4 tasks where a guard stopped the rewrite, both setups sent exactly the same text, yet their medians differ by −4.8%, −3.4%, −0.8% and **+11.8%**. That's the spread of Claude itself between two sets of 3 runs.
* **The rewritten tasks fall in the same range:** from −6.9% (`monthly-totals`) to +8.0% (`csv-export`). Within one task and setup, cost varies by about 5% (median coefficient of variation), and in 11 of 12 tasks the two setups' ranges overlap.

`prompt-engine` came out cheaper on 10 of the 12 tasks, but 3 of those 10 sent the same prompt in both setups, so the count says nothing about the rewrite. Going first or second didn't matter either: $0.0600 against $0.0608 per run, with the same tokens written to the cache.

### 2.3 What this doesn't show

* **Harder or vaguer work.** Every task was done by both setups in every run, so the success rate can't separate them. A clearer prompt may matter where Claude can go wrong; these tasks were too easy for that.
* **Guidance.** The developer has no `append.txt`, so none was added. The harness supports it (the hook delivered it in the pilot); a run with a typical `append.txt` would measure it.
* **Interactive sessions.** These are fresh one-prompt sessions. In a long session the prompt is an even smaller share of the context (the [token baseline](token-baseline.md): 97.9% is context read back).
* **Other models.** Sonnet only.

---

## 3. What it means

1. **The rewrite is not a cost saving.** It neither saves nor costs tokens; it adds about a second. Its case is the one Appendix 5, section 7 already made: Claude doing what the developer meant ([Appendix 6](../PRD-APPENDIX-6.md)). This benchmark can't show that either, since every task was done both ways.
2. **The guards work as meant here, and are strict on bug reports.** On three symptom-only bug reports, the model's rewrite turned the symptom into an instruction ("fix", "handle", "ensure"), and `added-task` stopped it each time. That is the rule doing its job: it keeps the developer's words when the model adds what they didn't ask for.
3. **A saving has to come from what Claude carries, not what it's sent.** That is FR-11.4 (model and context hints) and FR-11.6 (pre-investigation). Each can now be measured as a third setup on the same tasks, against this baseline: a feature is kept only if it beats it on cost without losing tasks.
4. **The tasks need to be harder to measure FR-11.6.** Its point is less searching; in a project this small there's little to search. A larger project, or tasks spread over more files, comes with it.
