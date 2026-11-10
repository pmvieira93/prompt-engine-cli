#!/usr/bin/env python3
"""Cost benchmark: the same tasks with Claude Code alone and with
prompt-engine (PRD Appendix 5, section 8).

    bench/run.py check                 each task fails on the project, passes with its solution
    bench/run.py run [options]         run the tasks, one JSON line per run
    bench/run.py report <results>...   medians per task and setup

Each run copies bench/project to a fresh folder (a git repository, as a
real project is), sends the task's prompt to `claude -p`, then adds the
task's hidden test and runs `go test`. Claude never sees the hidden tests.

Setups:
  alone  the prompt as typed;
  pe     the prompt after `prompt-engine --pe-rewrite`, with the guidance
         given to claude through prompt-engine's own hook, as in a session.

Every run uses real Claude sessions, billed to the account `claude` is
signed in to. Start small: `run --tasks split-cents --runs 1`.
"""

import argparse
import json
import os
import re
import shutil
import statistics
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timezone
from pathlib import Path

BENCH = Path(__file__).resolve().parent
ROOT = BENCH.parent
PROJECT = BENCH / "project"
TASKS = BENCH / "tasks"
HIDDEN = "bench_hidden_test.go"  # the name the hidden test gets in the copy

# Tools claude may use without asking; anything else is denied, as no one
# answers permission prompts.
ALLOWED = "Read,Edit,Write,Glob,Grep,Bash(go test:*),Bash(go build:*),Bash(go vet:*),Bash(gofmt:*)"


def tasks(names=None):
    found = sorted(p.name for p in TASKS.iterdir() if (p / "prompt.txt").exists())
    if not names:
        return found
    unknown = set(names) - set(found)
    if unknown:
        sys.exit(f"unknown tasks: {', '.join(sorted(unknown))} (tasks: {', '.join(found)})")
    return names


# Variables a Claude Code or prompt-engine session sets for what runs in
# it; passed on, they would make claude or prompt-engine act as nested.
# Those that choose the account, provider or config (CLAUDE_CONFIG_DIR,
# CLAUDE_CODE_OAUTH_TOKEN, CLAUDE_CODE_USE_BEDROCK, PROMPT_ENGINE_CONFIG...)
# are kept.
SESSION_VARS = re.compile(r"^(CLAUDECODE|CLAUDE_PID|CLAUDE_EFFORT|CLAUDE_CODE_(ENTRYPOINT|EXECPATH|SESSION_.*|CHILD_SESSION|"
                          r"MESSAGING_.*|SSE_PORT)|PROMPT_ENGINE_(?!CONFIG$).*)$")


def clean_env(**extra):
    """The environment without the session variables of a Claude Code or
    prompt-engine session running this script."""
    env = {k: v for k, v in os.environ.items() if not SESSION_VARS.match(k)}
    env.update(extra)
    return env


def git(cwd, *args):
    # The developer's own signing and hooks don't apply to the copy.
    subprocess.run(["git", "-c", "user.name=bench", "-c", "user.email=bench@example.com",
                    "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", *args],
                   cwd=cwd, check=True, capture_output=True)


def copy_project(dest):
    shutil.copytree(PROJECT, dest)
    git(dest, "init", "-q")
    git(dest, "add", "-A")
    git(dest, "commit", "-qm", "base")


def test_names(*files):
    return sorted({m for f in files for m in re.findall(r"^func (Test\w+)\(", Path(f).read_text(), re.M)})


def go_test(project, pattern=None):
    cmd = ["go", "test"] + (["-run", pattern] if pattern else []) + ["./..."]
    try:
        r = subprocess.run(cmd, cwd=project, capture_output=True, text=True, timeout=180)
    except subprocess.TimeoutExpired:
        return False, "go test timed out"
    return r.returncode == 0, (r.stdout + r.stderr)[-2000:]


def hidden_tests_pass(project, task):
    """Whether the task is done: its hidden test and the project's own
    tests pass. Tests Claude added don't decide it."""
    hidden = TASKS / task / "hidden_test.go.in"
    names = test_names(hidden, *PROJECT.glob("*_test.go"))
    shutil.copy(hidden, project / HIDDEN)
    try:
        return go_test(project, "^(" + "|".join(names) + ")$")
    finally:
        (project / HIDDEN).unlink(missing_ok=True)


def check(_args):
    bad = 0
    for task in tasks():
        with tempfile.TemporaryDirectory(prefix="pe-bench-") as tmp:
            project = Path(tmp) / "project"
            copy_project(project)
            before, _ = hidden_tests_pass(project, task)
            subprocess.run(["git", "apply", str(TASKS / task / "solution.patch")], cwd=project, check=True)
            after, out = hidden_tests_pass(project, task)
        ok = not before and after
        bad += not ok
        print(f"{task}: {'ok' if ok else 'BAD'} (base {'passes' if before else 'fails'}, solution {'passes' if after else 'fails'})")
        if not after:
            print(out)
    return 1 if bad else 0


def build_pe(dest):
    out = Path(dest) / "prompt-engine"
    subprocess.run(["go", "build", "-o", str(out), "./cmd/prompt-engine"], cwd=ROOT, check=True)
    return str(out)


def rewrite(pe, cfg, project, prompt, queue):
    """prompt-engine's rewrite of prompt, with the guidance put in queue
    for its hook, as a session does on Enter; None and the error if it
    failed."""
    cmd = [pe] + (["--pe-config", cfg] if cfg else []) + ["--pe-rewrite", prompt]
    try:
        r = subprocess.run(cmd, cwd=project, capture_output=True, text=True, timeout=300,
                           env=clean_env(PROMPT_ENGINE_RULES_QUEUE=str(queue)))
    except subprocess.TimeoutExpired:
        return None, "prompt-engine --pe-rewrite timed out"
    if r.returncode != 0:
        return None, f"prompt-engine --pe-rewrite failed ({r.returncode}): {r.stderr.strip()}"
    return json.loads(r.stdout), ""


def one_run(args, pe, task, setup, n, out):
    prompt = (TASKS / task / "prompt.txt").read_text().strip()
    with tempfile.TemporaryDirectory(prefix="pe-bench-") as tmp:
        tmp = Path(tmp)
        project = tmp / "project"
        copy_project(project)
        rec = {"time": datetime.now(timezone.utc).isoformat(timespec="seconds"), "task": task, "setup": setup,
               "run": n, "model": args.model, "typed": prompt}
        sent, extra_args, env = prompt, [], clean_env()
        if setup == "pe":
            queue = tmp / "queue"
            queue.mkdir()
            rw, err = rewrite(pe, args.pe_config, project, prompt, queue)
            if rw is None:
                # Recorded as a failed run; the others go on.
                rec["error"], rec["pass"] = err, False
                out.write(json.dumps(rec) + "\n")
                out.flush()
                print(f"{task} {setup} #{n}: not run: {err}")
                return
            sent = rw["prompt"]
            rec.update(rewritten=rw["rewritten"], not_rewritten=rw.get("not_rewritten", ""), guard=rw.get("guard", ""),
                       style=rw.get("style", ""), rewrite_ms=rw["rewrite_ms"], sections=rw.get("sections", []),
                       guidance_chars=len(rw.get("guidance", "")))
            if rw.get("queued"):
                # prompt-engine's own hook adds the guidance, as in a session.
                extra_args = ["--settings", rw["hook_settings"]]
                env = clean_env(PROMPT_ENGINE_RULES_QUEUE=str(queue), PROMPT_ENGINE_DEBUG_LOG=str(tmp / "hook.log"))
        rec["sent"] = sent
        cmd = ["claude", "-p", "--output-format", "json", "--model", args.model,
               "--setting-sources", "project", "--strict-mcp-config", "--no-session-persistence",
               "--permission-mode", "acceptEdits", "--permission-prompts", "none",
               "--allowedTools", ALLOWED, "--max-budget-usd", str(args.budget), *extra_args]
        start = time.monotonic()
        try:
            r = subprocess.run(cmd, input=sent, cwd=project, capture_output=True, text=True, env=env, timeout=args.timeout)
            stdout, code = r.stdout, r.returncode
        except subprocess.TimeoutExpired as e:
            # The partial output is bytes, even with text=True.
            stdout, code = (e.stdout or b"").decode(errors="replace"), "timeout"
        rec["wall_ms"] = int((time.monotonic() - start) * 1000)
        rec["exit"] = code
        try:
            res = json.loads(stdout)
        except (json.JSONDecodeError, TypeError):
            res = {"is_error": True, "result": stdout[-500:]}
        tokens = {"input": 0, "cache_write": 0, "cache_read": 0, "output": 0}
        for u in (res.get("modelUsage") or {}).values():
            tokens["input"] += u.get("inputTokens", 0)
            tokens["cache_write"] += u.get("cacheCreationInputTokens", 0)
            tokens["cache_read"] += u.get("cacheReadInputTokens", 0)
            tokens["output"] += u.get("outputTokens", 0)
        rec.update(cost_usd=res.get("total_cost_usd"), duration_ms=res.get("duration_ms"), turns=res.get("num_turns"),
                   is_error=res.get("is_error", True), subtype=res.get("subtype"), tokens=tokens,
                   models=sorted((res.get("modelUsage") or {}).keys()),
                   denials=len(res.get("permission_denials") or []))
        if (tmp / "hook.log").exists():
            rec["hook_gave"] = "hook gave claude" in (tmp / "hook.log").read_text()
        rec["pass"], rec["test_output"] = hidden_tests_pass(project, task)
        # Tests Claude added, apart: they don't decide the task.
        rec["all_tests_pass"], _ = go_test(project)
        changed = subprocess.run(["git", "status", "--short"], cwd=project, capture_output=True, text=True).stdout
        rec["changed"] = changed.split("\n")[:-1]
    out.write(json.dumps(rec) + "\n")
    out.flush()
    cost = rec["cost_usd"]
    print(f"{task} {setup} #{n}: {'pass' if rec['pass'] else 'FAIL'}, "
          f"${cost:.4f}, {rec['turns']} turns, {rec['wall_ms'] / 1000:.0f}s" if cost is not None
          else f"{task} {setup} #{n}: no result ({rec['exit']})")


def run(args):
    names = tasks(args.tasks)
    setups = args.setups.split(",")
    total = len(names) * len(setups) * args.runs
    print(f"{total} Claude sessions: {len(names)} tasks x {len(setups)} setups x {args.runs} runs, model {args.model}, "
          f"at most ${args.budget} each")
    results = BENCH / "results"
    results.mkdir(exist_ok=True)
    path = Path(args.out) if args.out else results / f"{datetime.now():%Y%m%d-%H%M%S}.jsonl"
    with tempfile.TemporaryDirectory(prefix="pe-bench-bin-") as bindir, open(path, "a") as out:
        pe = args.pe or build_pe(bindir)
        # The setups take turns going first, so neither always pays to put
        # Claude Code's system prompt in the cache, or always meets a
        # slower minute first.
        i = 0
        for n in range(1, args.runs + 1):
            for task in names:
                for setup in setups if i % 2 == 0 else setups[::-1]:
                    one_run(args, pe, task, setup, n, out)
                i += 1
    print(f"results: {path}")
    return 0


def median(xs):
    xs = [x for x in xs if x is not None]
    return statistics.median(xs) if xs else None


def cell(x, fmt, width):
    return f"{'-' if x is None else format(x, fmt):>{width}}"


def finished(r):
    """A run that did the task, with its cost known."""
    return r["pass"] and r.get("cost_usd") is not None


def report(args):
    recs = [json.loads(line) for p in args.results for line in open(p) if line.strip()]
    by = {}
    for r in recs:
        by.setdefault((r["task"], r["setup"]), []).append(r)
    setups = sorted({r["setup"] for r in recs})
    print(f"{'task':16} {'setup':6} {'pass':>5} {'cost $':>8} {'in tok':>9} {'out tok':>8} {'turns':>5} {'time s':>7}")
    done = {}  # (task, setup): the finished runs
    for (task, setup), rs in sorted(by.items()):
        ok = [r for r in rs if finished(r)]
        done[(task, setup)] = ok
        tin = median([sum(r["tokens"][k] for k in ("input", "cache_write", "cache_read")) for r in ok])
        wall = median([r["wall_ms"] + r.get("rewrite_ms", 0) for r in ok])
        print(f"{task:16} {setup:6} {len(ok):>2}/{len(rs):<2} {cell(median([r['cost_usd'] for r in ok]), '.4f', 8)} "
              f"{cell(tin, '.0f', 9)} {cell(median([r['tokens']['output'] for r in ok]), '.0f', 8)} "
              f"{cell(median([r['turns'] for r in ok]), '.0f', 5)} {cell(wall and wall / 1000, '.0f', 7)}")
    # Costs compare only on tasks every setup finished (PRD Appendix 5, 8.1).
    both = sorted({t for t, _ in by if all(done.get((t, s)) for s in setups)})
    print(f"\nTasks every setup finished: {len(both)} of {len({t for t, _ in by})}")
    for s in setups:
        rs = [r for (t, ss), v in by.items() if ss == s for r in v]
        cost = sum(median([r["cost_usd"] for r in done[(t, s)]]) for t in both)
        wall = sum(median([r["wall_ms"] + r.get("rewrite_ms", 0) for r in done[(t, s)]]) for t in both)
        print(f"  {s:6} success {sum(finished(r) for r in rs)}/{len(rs)}; on those tasks: ${cost:.4f}, {wall / 1000:.0f}s")
    return 0


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    sub.add_parser("check", help="check each task's hidden test and solution")
    r = sub.add_parser("run", help="run the benchmark (real Claude sessions)")
    r.add_argument("--tasks", nargs="*", help="task names (default: all)")
    r.add_argument("--setups", default="alone,pe", help="comma-separated: alone, pe (default: both)")
    r.add_argument("--runs", type=int, default=1, help="runs per task and setup (default 1)")
    r.add_argument("--model", default="sonnet", help="claude --model (default sonnet), the same for every run")
    r.add_argument("--budget", type=float, default=1.0, help="claude --max-budget-usd per run (default 1.0)")
    r.add_argument("--timeout", type=int, default=900, help="seconds per run (default 900)")
    r.add_argument("--pe", help="prompt-engine binary (default: built from this repository)")
    r.add_argument("--pe-config", help="prompt-engine config for the pe setup (default: your usual one)")
    r.add_argument("--out", help="results file (default: bench/results/<time>.jsonl)")
    p = sub.add_parser("report", help="summarize results files")
    p.add_argument("results", nargs="+")
    args = ap.parse_args()
    return {"check": check, "run": run, "report": report}[args.cmd](args)


if __name__ == "__main__":
    sys.exit(main())
