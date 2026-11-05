#!/usr/bin/env python3
"""Prints the figures in guard-lines.md and intent.md from a shared-run output.

    python3 docs/spikes/shared-run/analyse.py runs.jsonl
"""
import collections
import json
import re
import statistics as st
import sys

VARIANTS = ("plain", "lines", "json")
SECTIONS = ("review", "test", "inv", "debug", "none")


def section(intent):
    """investigate and analysis are one section."""
    return "inv" if intent in ("investigate", "analysis") else intent


def same_words(a, b):
    return re.sub(r"\W", "", a).lower() == re.sub(r"\W", "", b).lower()


def keyword(text):
    """A plain match on the section's own words, for comparison with the model."""
    t = text.lower()
    if re.search(r"\b(bug|debug|error|crash|failing|broken)\b", t):
        return "debug"
    if re.search(r"\breview\b", t):
        return "review"
    if re.search(r"\b(tests?|testing)\b", t):
        return "test"
    if re.search(r"\b(investigate|analy[sz]e|analysis|why)\b", t):
        return "inv"
    return "none"


def scores(pairs):
    """Prints precision and recall per section from (guess, label) pairs."""
    print(f"  correct {sum(g == l for g, l in pairs)}/{len(pairs)}")
    for s in SECTIONS:
        hit = sum(g == s == l for g, l in pairs)
        print(f"  {s}: right {hit} of {sum(g == s for g, _ in pairs)} picked, found {hit} of {sum(l == s for _, l in pairs)}")
    print(f"  wrong section {sum(g not in ('none', l) for g, l in pairs)}, missed {sum(g == 'none' != l for g, l in pairs)}")


rows = [json.loads(line) for line in open(sys.argv[1])]
rows_of = lambda v: [r for r in rows if r["variant"] == v]
ids = sorted({r["id"] for r in rows})
seeds = sorted({r["seed"] for r in rows})
at = {(r["id"], r["seed"], r["variant"]): r for r in rows}
stopped = lambda r: bool(r.get("guard"))

print(f"{len(rows)} rows, {len(ids)} prompts, {sum(bool(r.get('error')) for r in rows)} errors")
for v in VARIANTS:
    x = rows_of(v)
    print(f"\n{v}: {len(x)} runs")
    print("  stops", sum(map(stopped, x)), dict(collections.Counter(r["guard"] for r in x if stopped(r))))
    print("  prompts stopped", len({r["id"] for r in x if stopped(r)}))
    print("  same words as typed", sum(same_words(r["rewrite"], r["typed"]) for r in x))
    print("  cut off", sum(bool(r["cut"]) for r in x), "unparsed", sum(not r["parsed"] for r in x))
    print("  tokens in", st.median(r["in"] for r in x), "out", st.median(r["out"] for r in x))
    print("  seconds", round(st.median(r["seconds"] for r in x), 2), [round(st.median(r["seconds"] for r in x if r["seed"] == s), 2) for s in seeds])
    differ = sum(at[i, seeds[0], v]["rewrite"] != at[i, s, v]["rewrite"] for i in ids for s in seeds[1:])
    print(f"  differs from its first seed {differ}/{len(ids) * (len(seeds) - 1)}")
    print("  'debug' added", sum("debug" in r["rewrite"].lower() and "debug" not in r["typed"].lower() for r in x))

for v in VARIANTS[1:]:
    pairs = [(at[i, s, "plain"], at[i, s, v]) for i in ids for s in seeds]
    print(f"\n{v} against plain: differs {sum(a['rewrite'] != b['rewrite'] for a, b in pairs)}/{len(pairs)},",
          f"stopped by both {sum(stopped(a) and stopped(b) for a, b in pairs)},",
          f"stop gone {sum(stopped(a) and not stopped(b) for a, b in pairs)},",
          f"new stop {sum(stopped(b) and not stopped(a) for a, b in pairs)}")

js = rows_of("json")
print("\nmodel intent, by run:")
scores([(section(r["intent"]), section(r["label"])) for r in js])
print("  as picked:", {l: dict(collections.Counter(r["intent"] for r in js if r["label"] == l)) for l in sorted({r["label"] for r in js})})
print("  same section on every seed", sum(len({section(at[i, s, "json"]["intent"]) for s in seeds}) == 1 for i in ids), f"of {len(ids)}")
print("\nkeywords, by prompt:")
scores([(keyword(at[i, seeds[0], "json"]["typed"]), section(at[i, seeds[0], "json"]["label"])) for i in ids])
