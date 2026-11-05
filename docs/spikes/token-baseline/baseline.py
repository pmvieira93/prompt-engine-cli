#!/usr/bin/env python3
"""Token baseline for PRD Appendix 5, step 1.

Reads Claude Code's local transcripts and prints totals only. No prompt text
is printed, and nothing is sent anywhere.

    python3 docs/spikes/token-baseline/baseline.py [-prompts out.json] [-skip id]

-prompts saves the typed prompts for labelling. That file holds real prompts,
so keep it out of the repo. -skip leaves one session out of the second set of
figures, for a session long enough to outweigh the rest.
"""
import argparse
import collections
import glob
import json
import os
import statistics as st


def typed_text(record):
    """Returns what the developer typed, or None for tool results and notices."""
    content = record.get("message", {}).get("content")
    if isinstance(content, list):
        if any(isinstance(b, dict) and b.get("type") == "tool_result" for b in content):
            return None
        parts = [b.get("text", "") for b in content if isinstance(b, dict) and b.get("type") == "text"]
        if not parts:
            return None
        content = "\n".join(parts)
    if not isinstance(content, str) or not content.strip():
        return None
    if content.lstrip().startswith(("<", "Caveat:", "[Request interrupted", "This session is being continued")):
        return None
    return content


def read_session(path, prompts):
    seen = set()
    s = dict(id=os.path.basename(path)[:8], api=0, typed=0, inp=0, cw=0, cr=0, out=0, peak=0)
    for line in open(path, errors="ignore"):
        try:
            r = json.loads(line)
        except ValueError:
            continue
        if r.get("type") == "assistant":
            m = r.get("message", {})
            u, mid = m.get("usage"), m.get("id")
            if not u or mid in seen:
                continue
            seen.add(mid)
            inp, cw, cr = (u.get(k, 0) for k in ("input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"))
            s["api"] += 1
            s["inp"] += inp
            s["cw"] += cw
            s["cr"] += cr
            s["out"] += u.get("output_tokens", 0)
            s["peak"] = max(s["peak"], inp + cw + cr)
        elif r.get("type") == "user" and not r.get("isMeta") and not r.get("isSidechain"):
            text = typed_text(r)
            if text is not None:
                s["typed"] += 1
                prompts.append(dict(project=os.path.basename(os.path.dirname(path)), text=text, time=r.get("timestamp", "")))
    return s


def total(s):
    return s["inp"] + s["cw"] + s["cr"] + s["out"]


def report(name, sessions):
    real = [s for s in sessions if s["typed"] >= 5]
    print(f"\n{name}: {len(sessions)} sessions, {len(real)} with 5 or more prompts")
    if not real:
        return
    print("  prompts per session, median", st.median(s["typed"] for s in real))
    print("  API calls per session, median", st.median(s["api"] for s in real))
    print("  peak context, median", int(st.median(s["peak"] for s in real)), "max", max(s["peak"] for s in real))
    print("  tokens per typed prompt, median", int(st.median(total(s) / s["typed"] for s in real)))


def main():
    p = argparse.ArgumentParser()
    p.add_argument("-prompts")
    p.add_argument("-skip", default="")
    args = p.parse_args()

    files = glob.glob(os.path.expanduser("~/.claude/projects/*/*.jsonl"))
    prompts = []
    sessions = [s for s in (read_session(f, prompts) for f in files) if s["api"]]
    if args.prompts:
        with open(args.prompts, "w") as f:
            json.dump(prompts, f)

    all_tokens = sum(total(s) for s in sessions)
    print(f"{len(files)} transcripts, {len(prompts)} typed prompts, {len({p['project'] for p in prompts})} projects")
    print(f"total tokens {all_tokens}")
    for key, name in (("cr", "cache read"), ("cw", "cache write"), ("inp", "fresh input"), ("out", "output")):
        print(f"  {name} {sum(s[key] for s in sessions) / all_tokens * 100:.2f}%")
    typed_tokens = sum(len(p["text"]) for p in prompts) / 4  # about 4 characters per token
    print(f"typed text about {int(typed_tokens)} tokens, {typed_tokens / all_tokens * 100:.4f}% of the total")
    print("API calls per typed prompt", round(sum(s["api"] for s in sessions) / max(1, len(prompts)), 1))
    words = sorted(len(p["text"].split()) for p in prompts)
    print("prompt words: median", st.median(words), "p90", words[int(0.9 * len(words))], "max", words[-1])
    largest = sorted(sessions, key=total, reverse=True)[:2]
    print("largest sessions", [(s["id"], total(s), s["typed"]) for s in largest])

    report("all sessions", sessions)
    if args.skip:
        report("without the skipped session", [s for s in sessions if s["id"] != args.skip])


if __name__ == "__main__":
    main()
