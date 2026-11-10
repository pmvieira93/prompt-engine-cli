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


def clean_env(**extra):
    """The environment without Claude Code's and prompt-engine's variables,
    which a session running this script would pass on."""
    env = {k: v for k, v in os.environ.items() if not k.startswith(("CLAUDE", "PROMPT_ENGINE_"))}
    env.update(extra)
    return env


def git(cwd, *args):
    subprocess.run(["git", "-c", "user.name=bench", "-c", "user.email=bench@example.com", *args],
                   cwd=cwd, check=True, capture_output=True)


def copy_project(dest):
    shutil.copytree(PROJECT, dest)
    git(dest, "init", "-q")
    git(dest, "add", "-A")
    git(dest, "commit", "-qm", "base")


def hidden_tests_pass(project, task):
    shutil.copy(TASKS / task / "hidden_test.go.in", project / HIDDEN)
    try:
        r = subprocess.run(["go", "test", "./..."], cwd=project, capture_output=True, text=True, timeout=180)
    except subprocess.TimeoutExpired:
        return False, "go test timed out"
    finally:
        (project / HIDDEN).unlink(missing_ok=True)
    return r.returncode == 0, (r.stdout + r.stderr)[-2000:]


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


def rewrite(pe, cfg, project, prompt):
    cmd = [pe] + (["--pe-config", cfg] if cfg else []) + ["--pe-rewrite", prompt]
    r = subprocess.run(cmd, cwd=project, capture_output=True, text=True, env=clean_env(), timeout=300)
    if r.returncode != 0:
        sys.exit(f"prompt-engine --pe-rewrite failed ({r.returncode}): {r.stderr.strip()}")
    return json.loads(r.stdout)


def hook_settings(pe):
    """The settings prompt-engine gives claude in a session (rules.Settings)."""
    quoted = "'" + pe.replace("'", "'\\''") + "'"
    hook = {"type": "command", "command": quoted + " --pe-hook"}
    return json.dumps({"hooks": {"UserPromptSubmit": [{"hooks": [hook]}]}})


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
            rw = rewrite(pe, args.pe_config, project, prompt)
            sent = rw["prompt"]
            rec.update(rewritten=rw["rewritten"], not_rewritten=rw.get("not_rewritten", ""), guard=rw.get("guard", ""),
                       style=rw.get("style", ""), rewrite_ms=rw["rewrite_ms"], sections=rw.get("sections", []),
                       guidance_chars=len(rw.get("guidance", "")))
            if rw.get("guidance"):
                # The queue the hook reads, as a session fills it on Enter.
                queue = tmp / "queue"
                queue.mkdir()
                (queue / "000000000001.json").write_text(json.dumps({"prompt": sent, "text": rw["guidance"]}))
                extra_args = ["--settings", hook_settings(pe)]
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
            stdout, code = e.stdout or "", "timeout"
        rec["wall_ms"] = int((time.monotonic() - start) * 1000)
        rec["exit"] = code
        try:
            res = json.loads(stdout)
        except (json.JSONDecodeError, TypeError):
            res = {"is_error": True, "result": (stdout or "")[-500:]}
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
        # The runs alternate setups, so a slower hour hits both alike.
        for n in range(1, args.runs + 1):
            for task in names:
                for setup in setups:
                    one_run(args, pe, task, setup, n, out)
    print(f"results: {path}")
    return 0


def median(xs):
    xs = [x for x in xs if x is not None]
    return statistics.median(xs) if xs else None


def report(args):
    recs = [json.loads(line) for p in args.results for line in open(p) if line.strip()]
    by = {}
    for r in recs:
        by.setdefault((r["task"], r["setup"]), []).append(r)
    setups = sorted({r["setup"] for r in recs})
    print(f"{'task':16} {'setup':6} {'pass':>5} {'cost $':>8} {'in tok':>9} {'out tok':>8} {'turns':>5} {'time s':>7}")
    finished = {}  # (task, setup): the passing runs
    for (task, setup), rs in sorted(by.items()):
        ok = [r for r in rs if r["pass"]]
        finished[(task, setup)] = ok
        tin = median([sum(r["tokens"][k] for k in ("input", "cache_write", "cache_read")) for r in ok])
        wall = median([r["wall_ms"] + r.get("rewrite_ms", 0) for r in ok])
        cost = median([r["cost_usd"] for r in ok])
        print(f"{task:16} {setup:6} {len(ok):>2}/{len(rs):<2} "
              f"{cost if cost is None else f'{cost:.4f}':>8} {tin if tin is None else f'{tin:.0f}':>9} "
              f"{median([r['tokens']['output'] for r in ok]) or 0:>8.0f} {median([r['turns'] for r in ok]) or 0:>5.0f} "
              f"{wall if wall is None else f'{wall / 1000:.0f}':>7}")
    # Costs compare only on tasks every setup finished (PRD Appendix 5, 8.1).
    both = sorted({t for t, _ in by if all(finished.get((t, s)) for s in setups)})
    print(f"\nTasks every setup finished: {len(both)} of {len({t for t, _ in by})}")
    for s in setups:
        rs = [r for (t, ss), v in by.items() if ss == s for r in v]
        cost = sum(median([r["cost_usd"] for r in finished[(t, s)]]) for t in both)
        wall = sum(median([r["wall_ms"] + r.get("rewrite_ms", 0) for r in finished[(t, s)]]) for t in both)
        print(f"  {s:6} success {sum(r['pass'] for r in rs)}/{len(rs)}; on those tasks: ${cost:.4f}, {wall / 1000:.0f}s")
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
