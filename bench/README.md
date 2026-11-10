# Cost benchmark

The same tasks, run with Claude Code alone and with `prompt-engine`, compared on cost, tokens, time, turns and tasks done ([PRD Appendix 5](../docs/PRD-APPENDIX-5.md), section 8).

```sh
python3 -I bench/run.py check                         # each task: fails on the project, passes with its solution
python3 -I bench/run.py run --tasks split-cents       # one task, both setups, one run each
python3 -I bench/run.py run --runs 3                  # every task, three runs per setup
python3 -I bench/run.py report bench/results/*.jsonl  # medians per task and setup
python3 -I bench/run.py report bench/results/*.jsonl --chart chart.svg  # and a chart of them
```

`--chart` draws, per task, the median cost and turns of each setup, with the spread of the runs and each setup's success rate. Only finished runs count; a setup with none says so instead of drawing a zero. It adapts to light and dark mode. The pilot below has one run each, so it only shows the form:

![Pilot chart: median cost and turns per task, Claude alone vs with prompt-engine](pilot-chart.svg)

**Every run is a real Claude session, billed to the account `claude` is signed in to.** `run` prints how many sessions it will start; each is capped with `--budget` (default $1). One run of all twelve tasks costs about $0.75 per setup with Sonnet.

## What's here

* `project/`: a small Go module (`ledger`: amounts, transactions, CSV import and export, budgets, recurring payments, reports) with planted bugs and features still to write. Its own tests pass.
* Twelve tasks, six bugs and six features. Some name what to change ("add csv export"); others only describe a symptom ("the budget says I still have money left") or a wish ("I want to see where most of my money goes"), so Claude has to find the code.
* `tasks/<name>/`:
  * `prompt.txt`: the prompt, rough, the way a developer types it;
  * `hidden_test.go.in`: the test that decides whether the task is done, added to the project only after Claude has finished;
  * `solution.patch`: a fix that passes it, so `check` can show each task is fair.
* `run.py`: the harness. `results/` (not committed) gets one JSON line per run.

## How a run works

1. `project/` is copied to a fresh folder and made a git repository, as a real project is.
2. The prompt goes to `claude -p`, with the same flags for both setups:
   * `--model` fixed (default `sonnet`), `--output-format json` for the cost and tokens;
   * `--setting-sources project` and `--strict-mcp-config`, so your own settings, plugins and MCP servers don't change the result;
   * the environment without a surrounding Claude Code or `prompt-engine` session's variables, keeping those that pick the account or config (`CLAUDE_CONFIG_DIR`, `CLAUDE_CODE_OAUTH_TOKEN`, `PROMPT_ENGINE_CONFIG`...);
   * `--permission-mode acceptEdits`, the tools in `ALLOWED`, and no one answering permission prompts: anything else is denied.
3. The hidden test is added. The task is done when it and the project's own tests pass; tests Claude added are recorded apart (`all_tests_pass`) and don't decide it.

The two setups:

* **`alone`**: the prompt as typed.
* **`pe`**: the prompt after `prompt-engine --pe-rewrite`, with your config (or `--pe-config`). The guidance from `append.txt` reaches Claude through `prompt-engine`'s own hook, as in a session: `--pe-rewrite` puts it in the hook's queue and gives the `--settings` a session passes. `hook_gave` in the results says whether the hook added it. A rewrite that fails is recorded as a failed run, and the others go on.

The setups take turns going first, task by task, so neither always pays to put Claude Code's system prompt in the prompt cache, or always meets a slower minute first. `report` compares costs only on tasks both setups finished (passed, with the cost known), with the success rate beside it.

## Adding a task

Add `tasks/<name>/` with the three files. Write the solution in a copy of `project/`, check the hidden test passes, and save `git diff` as `solution.patch`; then `run.py check`. The prompt should be rough but fair: whatever the hidden test checks must follow from the prompt and the doc comments in `project/`.
