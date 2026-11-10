# Cost benchmark

The same tasks, run with Claude Code alone and with `prompt-engine`, compared on cost, tokens, time, turns and tasks done ([PRD Appendix 5](../docs/PRD-APPENDIX-5.md), section 8).

```sh
python3 -I bench/run.py check                         # each task: fails on the project, passes with its solution
python3 -I bench/run.py run --tasks split-cents       # one task, both setups, one run each
python3 -I bench/run.py run --runs 3                  # every task, three runs per setup
python3 -I bench/run.py report bench/results/*.jsonl  # medians per task and setup
```

**Every run is a real Claude session, billed to the account `claude` is signed in to.** `run` prints how many sessions it will start; each is capped with `--budget` (default $1). A run of all six tasks costs about $0.35 per setup with Sonnet.

## What's here

* `project/`: a small Go module (`ledger`) with planted bugs and features still to write. Its own tests pass.
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
   * `--permission-mode acceptEdits`, the tools in `ALLOWED`, and no one answering permission prompts: anything else is denied.
3. The hidden test is added and `go test ./...` decides pass or fail.

The two setups:

* **`alone`**: the prompt as typed.
* **`pe`**: the prompt after `prompt-engine --pe-rewrite`, with your config (or `--pe-config`). The guidance from `append.txt` reaches Claude through `prompt-engine`'s own hook, as in a session: the harness fills the hook's queue and passes the same `--settings`. `hook_gave` in the results says whether the hook added it.

The runs alternate setups, so a slower hour affects both alike. `report` compares costs only on tasks both setups finished, with the success rate beside it.

## Adding a task

Add `tasks/<name>/` with the three files. Write the solution in a copy of `project/`, check the hidden test passes, and save `git diff` as `solution.patch`; then `run.py check`. The prompt should be rough but fair: whatever the hidden test checks must follow from the prompt and the doc comments in `project/`.
