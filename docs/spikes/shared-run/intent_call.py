#!/usr/bin/env python3
"""Asks for the intent in a request of its own, separate from the rewrite,
with bare section names and with one-line hints. See ../intent.md, section 5.

    python3 docs/spikes/shared-run/intent_call.py labelled.json out.jsonl [limit]
    python3 docs/spikes/shared-run/intent_call.py -score out.jsonl
"""
import collections, json, statistics as st, sys, time, urllib.request

MODEL = "qwen2.5:7b"
SECTIONS = ["review", "test", "investigate|analysis", "debug", "none"]
HINTS = {
    "review": "asks to check code or a PR",
    "test": "asks to write or run tests",
    "investigate|analysis": "asks a question about how something works, with no code change",
    "debug": "something is broken and needs fixing",
    "none": "anything else",
}
SYSTEM = "You classify a developer's request. Reply with one intent:\n"
BARE = SYSTEM + "\n".join("- " + s for s in SECTIONS)
HINTED = SYSTEM + "\n".join(f"- {s}: {HINTS[s]}" for s in SECTIONS)
FORMAT = {"type": "object", "properties": {"intent": {"type": "string", "enum": SECTIONS}}, "required": ["intent"]}


def ask(system, text, seed):
    body = {"model": MODEL, "stream": False, "format": FORMAT,
            "messages": [{"role": "system", "content": system}, {"role": "user", "content": "Request: " + text}],
            "options": {"temperature": 0.2, "num_predict": 30, "seed": seed}}
    req = urllib.request.Request("http://localhost:11434/api/chat", json.dumps(body).encode(), {"Content-Type": "application/json"})
    start = time.time()
    with urllib.request.urlopen(req, timeout=120) as r:
        got = json.load(r)
    return json.loads(got["message"]["content"])["intent"], got.get("prompt_eval_count", 0), got.get("eval_count", 0), time.time() - start


def score(path):
    rows = [json.loads(line) for line in open(path)]
    print(len(rows), "rows,", sum("error" in r for r in rows), "errors")
    section = lambda s: "investigate|analysis" if s in ("investigate", "analysis") else s
    for variant in ("bare", "hints"):
        x = [r for r in rows if r["variant"] == variant and "error" not in r]
        pairs = [(r["intent"], section(r["label"])) for r in x]
        print(f"\n{variant}: correct {sum(g == l for g, l in pairs)}/{len(pairs)}")
        for s in SECTIONS:
            hit = sum(g == s == l for g, l in pairs)
            print(f"  {s}: right {hit} of {sum(g == s for g, _ in pairs)} picked, found {hit} of {sum(l == s for _, l in pairs)}")
        print(f"  wrong section {sum(g not in ('none', l) for g, l in pairs)}, missed {sum(g == 'none' != l for g, l in pairs)}")
        print("  tokens in", st.median(r["tokens_in"] for r in x), "seconds", round(st.median(r["seconds"] for r in x), 2))
        seen = collections.defaultdict(set)
        for r in x:
            seen[r["id"]].add(r["intent"])
        print("  same section on every seed", sum(len(v) == 1 for v in seen.values()), "of", len(seen))


def run():
    prompts = json.load(open(sys.argv[1]))
    if len(sys.argv) > 3:
        prompts = prompts[:int(sys.argv[3])]
    with open(sys.argv[2], "w") as out:
        for seed in (1, 2, 3):
            for p in prompts:
                for variant, system in (("bare", BARE), ("hints", HINTED)):
                    try:
                        intent, i, o, s = ask(system, p["text"], seed)
                        row = dict(id=p["id"], label=p["label"], variant=variant, seed=seed, intent=intent, tokens_in=i, tokens_out=o, seconds=round(s, 3))
                    except Exception as e:
                        row = dict(id=p["id"], label=p["label"], variant=variant, seed=seed, error=str(e))
                    out.write(json.dumps(row) + "\n")
                    out.flush()
            print("seed", seed, "done", flush=True)


if sys.argv[1] == "-score":
    score(sys.argv[2])
else:
    run()
