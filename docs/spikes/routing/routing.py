#!/usr/bin/env python3
"""Routing spike for PRD Appendix 5, spike steps 3 and 4.

Can a prompt be routed before Claude sees it, and by what: keyword rules,
a non-generative classifier (embeddings from Ollama with a linear model),
or a small generative model through Ollama?

    python3 -I docs/spikes/routing/routing.py extract   # prompts and derived labels
    python3 -I docs/spikes/routing/routing.py sample    # a file to label by hand
    python3 -I docs/spikes/routing/routing.py label     # label it in the terminal, one key per answer
    python3 -I docs/spikes/routing/routing.py run       # compare the three, print totals

Every prompt stays on this machine, in DATA (outside the repository);
only totals are printed. Ollama runs locally; nothing is sent anywhere.

Three labels per prompt (Appendix 5, section 4, step 3):

  kind     self   a self-contained question, no project needed (FR-11.3)
           inv    a bug, ticket or review to investigate first (FR-11.6)
           agent  other work for the agent
  tier     light  a small task a lighter model could do (FR-11.4)
           heavy  the rest
  new      yes    starts a new task: a /clear hint could fit (FR-11.4)
           no     continues the last one

The labels are derived from what Claude did in each turn (see derive), and
checked against the developer's own labels for a sample. The embeddings
classifier learns either from the derived labels or from the hand labels.
Results: docs/spikes/routing.md.
"""
import argparse
import collections
import csv
import json
import math
import os
import random
import re
import sys
import time
import urllib.request

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "token-baseline"))
from baseline import context_tokens, transcripts  # noqa: E402
from turns import is_prompt, text_of  # noqa: E402

DATA = os.path.expanduser("~/.local/state/prompt-engine/spike-routing")
OLLAMA = "http://localhost:11434"
EMBED_MODEL = "nomic-embed-text"
GEN_MODEL = "qwen2.5:3b"
KINDS, TIERS, NEWS = ("self", "inv", "agent"), ("light", "heavy"), ("yes", "no")

READS = {"Read", "Grep", "Glob", "LS", "NotebookRead", "WebFetch", "WebSearch"}
EDITS = {"Edit", "Write", "MultiEdit", "NotebookEdit"}


def path_of(tool):
    i = tool.get("input") or {}
    return i.get("file_path") or i.get("notebook_path") or i.get("path")


# ---------------------------------------------------------------- extract

def read_turns(path):
    """The developer's prompts in one transcript, each with what Claude did
    until the next prompt."""
    records = []
    for line in open(path, errors="ignore"):
        try:
            records.append(json.loads(line))
        except ValueError:
            continue
    records.sort(key=lambda r: r.get("timestamp", ""))
    turns, cur, seen, context = [], None, set(), 0
    for r in records:
        if r.get("isSidechain"):
            continue
        msg = r.get("message") if isinstance(r.get("message"), dict) else {}
        if r.get("type") == "user":
            text = text_of(msg.get("content"))
            if is_prompt(r, text) and not text.lstrip().startswith("/"):
                cur = dict(text=text.strip(), context=context, calls=0, out=0, tools=[], files=[], first=not turns)
                turns.append(cur)
        elif r.get("type") == "assistant" and cur is not None:
            mid, u = msg.get("id"), msg.get("usage")
            if u and mid not in seen and not str(msg.get("model", "")).startswith("<"):
                seen.add(mid)
                cur["calls"] += 1
                cur["out"] += u.get("output_tokens", 0)
                context = context_tokens(u)
            for b in msg.get("content") or []:
                if isinstance(b, dict) and b.get("type") == "tool_use":
                    cur["tools"].append(b.get("name"))
                    p = path_of(b)
                    if p and b.get("name") in READS | EDITS:
                        cur["files"].append(p)
    return turns


def derive(turns):
    """Labels from what Claude did in each turn: proxies, checked by hand.

    kind: no tool used -> self; three or more reads before the first edit
    (or with no edit) -> inv; else agent. tier: at most 3 calls and 1,500
    output tokens -> light. new: the transcript's first prompt (a new
    session or /clear), or a turn whose files share none with the three
    turns before; unknown when the turn touched no file."""
    for i, t in enumerate(turns):
        tools = t["tools"]
        first_edit = next((j for j, n in enumerate(tools) if n in EDITS), len(tools))
        reads = sum(1 for n in tools[:first_edit] if n in READS or n == "Bash")
        t["kind"] = "self" if not tools else "inv" if reads >= 3 else "agent"
        t["tier"] = "light" if t["calls"] <= 3 and t["out"] <= 1500 else "heavy"
        before = {f for u in turns[max(0, i - 3):i] for f in u["files"]}
        if t["first"]:
            t["new"] = "yes"
        elif t["files"] and before:
            t["new"] = "no" if set(t["files"]) & before else "yes"
        else:
            t["new"] = None
        t["prev"] = turns[i - 1]["text"] if i else ""


def extract(_args):
    os.makedirs(DATA, exist_ok=True)
    out, n = open(os.path.join(DATA, "prompts.jsonl"), "w"), 0
    for path in sorted(transcripts()):
        turns = read_turns(path)
        derive(turns)
        for i, t in enumerate(turns):
            t["id"] = f"{os.path.basename(path)[:8]}-{i}"
            del t["tools"], t["files"]
            out.write(json.dumps(t) + "\n")
            n += 1
    out.close()
    print(f"{n} prompts in {DATA}/prompts.jsonl")
    show_counts(load())


def load(name="prompts.jsonl"):
    return [json.loads(line) for line in open(os.path.join(DATA, name))]


def show_counts(ps):
    for label, values in (("kind", KINDS), ("tier", TIERS), ("new", NEWS + (None,))):
        c = collections.Counter(p[label] for p in ps)
        print(f"  {label:5} " + ", ".join(f"{v or 'unknown'} {c[v]}" for v in values))


# ----------------------------------------------------------------- sample

def sample(args):
    path = os.path.join(DATA, "hand.tsv")
    if os.path.exists(path) and not args.force:
        sys.exit(f"{path} exists; --force writes a new one (and loses its labels)")
    ps, rng = load(), random.Random(7)
    by = collections.defaultdict(list)
    for p in ps:
        by[p["kind"]].append(p)
    picked = []
    for kind in KINDS:  # the same number of each derived kind, so the rare ones are checked too
        picked += rng.sample(by[kind], min(len(by[kind]), args.n // len(KINDS)))
    rng.shuffle(picked)
    with open(path, "w", newline="") as f:
        w = csv.writer(f, delimiter="\t")
        w.writerow(["id", "kind", "tier", "new", "prompt", "previous prompt"])
        for p in picked:
            flat = lambda s: " ".join(s.split())[:400]
            w.writerow([p["id"], "", "", "", flat(p["text"]), flat(p["prev"])])
    print(f"{len(picked)} prompts to label in {path}")
    print("Fill kind (self/inv/agent), tier (light/heavy) and new (yes/no); leave a cell empty if unsure.")


KEYS = {"kind": {"s": "self", "i": "inv", "a": "agent"}, "tier": {"l": "light", "h": "heavy"},
        "new": {"y": "yes", "n": "no"}}


def key():
    """One key press, without Enter."""
    import termios
    import tty
    fd = sys.stdin.fileno()
    old = termios.tcgetattr(fd)
    try:
        tty.setraw(fd)
        # Straight from the terminal: sys.stdin's buffer could hold keys back.
        return os.read(fd, 1).decode(errors="ignore")
    finally:
        termios.tcsetattr(fd, termios.TCSADRAIN, old)


def label(_args):
    """Labels hand.tsv in the terminal: one prompt at a time, one key per
    answer, saved after each prompt."""
    if not sys.stdin.isatty():
        sys.exit("label needs a terminal: run it in a terminal window of your own")
    path = os.path.join(DATA, "hand.tsv")
    with open(path, newline="") as f:
        rows = list(csv.DictReader(f, delimiter="\t"))
    fields = ["id", "kind", "tier", "new", "prompt", "previous prompt"]

    def save():
        tmp = path + ".tmp"
        with open(tmp, "w", newline="") as f:
            w = csv.DictWriter(f, fieldnames=fields, delimiter="\t", extrasaction="ignore")
            w.writeheader()
            w.writerows(rows)
        os.replace(tmp, path)

    todo = [r for r in rows if not all((r.get(k) or "").strip() for k in KEYS)]
    print(f"{len(rows) - len(todo)} of {len(rows)} labelled. Keys: shown with each question; "
          "Enter skips one, q saves and quits.\n")
    for n, row in enumerate(todo, 1):
        print(f"\033[1m[{len(rows) - len(todo) + n}/{len(rows)}]\033[0m")
        print(f"  previous: {row['previous prompt'][:200] or '(none: first prompt)'}")
        print(f"  \033[1mprompt:\033[0m   {row['prompt']}\n")
        for field, keys in KEYS.items():
            if (row.get(field) or "").strip():
                continue
            choices = "  ".join(f"{k}={v}" for k, v in keys.items())
            while True:
                print(f"  {field:5} ({choices}, Enter=skip): ", end="", flush=True)
                k = key().lower()
                if k == "q" or k == "\x03":
                    save()
                    print("\nsaved")
                    return
                if k in keys or k in "\r\n":
                    row[field] = keys.get(k, "")
                    print(row[field] or "skipped")
                    break
                print("?")
        save()
        print()
    print(f"All done: saved to {path}")


def hand_labels():
    path = os.path.join(DATA, "hand.tsv")
    if not os.path.exists(path):
        return {}
    labels = {}
    for row in csv.DictReader(open(path), delimiter="\t"):
        got = {k: (row.get(k) or "").strip().lower() or None for k in ("kind", "tier", "new")}
        if any(got.values()):
            labels[row["id"]] = got
    return labels


# ------------------------------------------------------------------ rules

INVESTIGATE = re.compile(r"\b(bug|error|crash\w*|fail\w*|broken|wrong|why|issue|ticket|review|investigat\w*|debug\w*|"
                         r"doesn'?t work|not working|exception|panic\w*|regression|look into|check)\b", re.I)
QUESTION = re.compile(r"^(what|how|why|when|which|explain|is|are|can|does|do|should)\b|\?\s*$", re.I)
PROJECT = re.compile(r"[\w./-]+\.(go|py|ts|js|md|ya?ml|json|sh|txt)\b|\b(this|our|my|the) (code|repo|project|app|"
                     r"file|function|test|branch|pr)\b|#\d+|\bPR\b", re.I)
BACKREF = re.compile(r"\b(it|that|this|those|these|them|also|again|same|above|previous|continue|go ahead|yes|ok|"
                     r"okay|sure|merge|commit|push|reply|fix (all|them|it))\b", re.I)


def rules(p):
    text = p["text"]
    words = len(text.split())
    if INVESTIGATE.search(text):
        kind = "inv"
    elif QUESTION.search(text.strip()) and not PROJECT.search(text):
        kind = "self"
    else:
        kind = "agent"
    tier = "light" if words <= 12 and kind != "inv" else "heavy"
    new = "yes" if p["first"] or (words >= 8 and not BACKREF.search(text)) else "no"
    return dict(kind=kind, tier=tier, new=new)


# ------------------------------------------------------------- embeddings

def ollama(endpoint, body, timeout=120):
    req = urllib.request.Request(f"{OLLAMA}/api/{endpoint}", data=json.dumps(body).encode(),
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.loads(r.read())


def embeddings(ps):
    """Each prompt's vector (cached), and the median time per prompt."""
    path = os.path.join(DATA, f"embed-{EMBED_MODEL}.json")
    cache = json.load(open(path)) if os.path.exists(path) else {}
    times = []
    for p in ps:
        if p["id"] in cache:
            continue
        start = time.monotonic()
        # nomic-embed-text wants a task prefix.
        v = ollama("embed", {"model": EMBED_MODEL, "input": "classification: " + p["text"][:2000]})["embeddings"][0]
        times.append(time.monotonic() - start)
        cache[p["id"]] = v
    json.dump(cache, open(path, "w"))
    return cache, (sorted(times)[len(times) // 2] if times else None)


def cosine(a, b):
    dot = sum(x * y for x, y in zip(a, b))
    return dot / (math.sqrt(sum(x * x for x in a)) * math.sqrt(sum(y * y for y in b)) or 1)


def train(xs, ys, classes, epochs=30, rate=0.1, l2=1e-4):
    """Multinomial logistic regression by stochastic gradient descent, in
    plain Python, on vectors scaled to unit length."""
    norm = lambda x: [v / (math.sqrt(sum(u * u for u in x)) or 1) for v in x]
    xs = [norm(x) for x in xs]
    order = list(range(len(xs)))
    rng = random.Random(7)
    dim = len(xs[0])
    w = [[0.0] * dim for _ in classes]
    b = [0.0] * len(classes)
    idx = {c: i for i, c in enumerate(classes)}
    # Classes weighted by rarity, so the rare kinds aren't ignored.
    count = collections.Counter(ys)
    weight = {c: len(ys) / (len(classes) * count[c]) for c in count}
    for _ in range(epochs):
        rng.shuffle(order)
        for n in order:
            x, y = xs[n], ys[n]
            scores = [sum(wi * xi for wi, xi in zip(w[k], x)) + b[k] for k in range(len(classes))]
            top = max(scores)
            exps = [math.exp(s - top) for s in scores]
            total = sum(exps)
            for k in range(len(classes)):
                g = (exps[k] / total - (1 if idx[y] == k else 0)) * weight[y] * rate
                row = w[k]
                for j in range(dim):
                    row[j] -= g * x[j] + rate * l2 * row[j]
                b[k] -= g

    def predict(x):
        x = norm(x)
        return classes[max(range(len(classes)), key=lambda k: sum(wi * xi for wi, xi in zip(w[k], x)) + b[k])]
    return predict


def features(p, vecs, label):
    v = vecs[p["id"]]
    if label != "new":
        return v
    # Few labels exist for "new", so a few features rather than the vector:
    # how close the prompt is to the one before, whether it's the first,
    # whether it refers back, and its length.
    sid, n = p["id"].rsplit("-", 1)
    prev = vecs.get(f"{sid}-{int(n) - 1}")
    return [cosine(v, prev) if prev else 0.0, 1.0 if p["first"] else 0.0,
            1.0 if BACKREF.search(p["text"]) else 0.0, math.log(1 + len(p["text"].split())) / 5]


def classifier(ps, vecs, label, classes, truth, folds=5):
    """Each prompt's prediction from a model trained on the other folds,
    split by session so a session's prompts don't help predict each other.
    truth gives the labels to learn from."""
    sessions = sorted({p["id"].split("-")[0] for p in ps})
    random.Random(7).shuffle(sessions)
    fold = {s: i % folds for i, s in enumerate(sessions)}
    pred = {}
    for f in range(folds):
        train_ps = [p for p in ps if fold[p["id"].split("-")[0]] != f and truth.get(p["id"]) is not None]
        if len({truth[p["id"]] for p in train_ps}) < 2:
            continue
        model = train([features(p, vecs, label) for p in train_ps], [truth[p["id"]] for p in train_ps], classes)
        for p in ps:
            if fold[p["id"].split("-")[0]] == f:
                pred[p["id"]] = model(features(p, vecs, label))
    return pred


# ------------------------------------------------------------- generative

GEN_PROMPT = """You route a developer's prompt to a coding assistant. Answer with JSON only.

kind: "self" if it is a self-contained question answerable without the developer's project (e.g. what a regex or git option does); "inv" if it reports a bug, an error, a ticket or asks for a review, so the code must be investigated first; "agent" for any other work on the project.
tier: "light" if a small, fast model could do it (a short answer, a one-line change); "heavy" otherwise.
new: "yes" if it starts a new task unrelated to the previous prompt; "no" if it continues it (a reply, a follow-up, a yes, a correction).

Previous prompt: {prev}
Prompt: {text}"""

SCHEMA = {"type": "object", "required": ["kind", "tier", "new"], "properties": {
    "kind": {"enum": list(KINDS)}, "tier": {"enum": list(TIERS)}, "new": {"enum": list(NEWS)}}}


def generative(ps):
    path = os.path.join(DATA, f"gen-{GEN_MODEL.replace(':', '_')}.json")
    cache = json.load(open(path)) if os.path.exists(path) else {}
    for i, p in enumerate(ps):
        if p["id"] in cache:
            continue
        start = time.monotonic()
        try:
            r = ollama("generate", {"model": GEN_MODEL, "stream": False, "format": SCHEMA, "keep_alive": "10m",
                                    "options": {"temperature": 0},
                                    "prompt": GEN_PROMPT.format(prev=p["prev"][:500] or "(none)", text=p["text"][:1500])})
            got = json.loads(r["response"])
        except (ValueError, OSError) as e:
            got = {"error": str(e)[:100]}
        got["seconds"] = time.monotonic() - start
        cache[p["id"]] = got
        if i % 50 == 0:
            json.dump(cache, open(path, "w"))
    json.dump(cache, open(path, "w"))
    return cache


# -------------------------------------------------------------------- run

def accuracy(pred, truth, classes):
    """Accuracy, and per class: how often a prediction of it is right
    (precision) and how many of it are found (recall)."""
    pairs = [(truth[i], pred[i]) for i in truth if truth[i] is not None and pred.get(i) is not None]
    if not pairs:
        return "no labels"
    right = sum(t == p for t, p in pairs)
    common = collections.Counter(t for t, _ in pairs).most_common(1)[0]
    parts = [f"{100 * right / len(pairs):3.0f}% of {len(pairs)} (always '{common[0]}': {100 * common[1] / len(pairs):.0f}%)"]
    for c in classes:
        said = sum(p == c for _, p in pairs)
        real = sum(t == c for t, _ in pairs)
        hit = sum(t == p == c for t, p in pairs)
        parts.append(f"{c}: right {hit}/{said}, found {hit}/{real}")
    return "; ".join(parts)


def run(args):
    ps = load()
    hand = hand_labels()
    print(f"{len(ps)} prompts; {len(hand)} labelled by hand")
    show_counts(ps)
    vecs, embed_s = embeddings(ps)
    gen = {} if args.no_gen else generative(ps)
    approaches = {"rules": {p["id"]: rules(p) for p in ps}}
    # kind and tier learn from the derived labels; new, which the
    # transcripts rarely show, from the hand labels.
    emb = {label: classifier(ps, vecs, label, classes, {p["id"]: p[label] for p in ps}) for label, classes in
           (("kind", KINDS), ("tier", TIERS))}
    emb["new"] = classifier(ps, vecs, "new", NEWS, {i: h["new"] for i, h in hand.items()})
    approaches["embeddings"] = {p["id"]: {label: emb[label].get(p["id"]) for label in emb} for p in ps}
    # The same classifier learning every label from the hand labels alone:
    # whether it is the method or the derived labels that fall short.
    if hand:
        own = {label: classifier(ps, vecs, label, classes, {i: h[label] for i, h in hand.items()})
               for label, classes in (("kind", KINDS), ("tier", TIERS), ("new", NEWS))}
        approaches["emb (hand)"] = {i: {label: own[label].get(i) for label in own} for i in hand}
    if gen:
        approaches[GEN_MODEL] = {i: g for i, g in gen.items() if "error" not in g}
    for label, classes in (("kind", KINDS), ("tier", TIERS), ("new", NEWS)):
        print(f"\n{label}")
        derived = {p["id"]: p[label] for p in ps}
        if hand:
            print(f"  derived labels against the hand labels: "
                  f"{accuracy(derived, {i: h[label] for i, h in hand.items()}, classes)}")
        for name, pred in approaches.items():
            mine = {i: v.get(label) for i, v in pred.items()}
            print(f"  {name:12} vs derived: {accuracy(mine, derived, classes)}")
            if hand:
                print(f"  {'':12} vs hand:    {accuracy(mine, {i: h[label] for i, h in hand.items()}, classes)}")
    secs = sorted(g["seconds"] for g in gen.values()) if gen else []
    print("\nTime per prompt: rules under 1 ms; embeddings "
          f"{embed_s * 1000:.0f} ms (median, model loaded)" if embed_s else "\nTime per prompt: rules under 1 ms",
          f"; {GEN_MODEL} {secs[len(secs) // 2]:.2f} s (median)" if secs else "")
    errors = sum("error" in g for g in gen.values())
    if errors:
        print(f"{GEN_MODEL}: {errors} replies failed")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)
    sub.add_parser("extract")
    sub.add_parser("label")
    s = sub.add_parser("sample")
    s.add_argument("-n", type=int, default=60)
    s.add_argument("--force", action="store_true")
    r = sub.add_parser("run")
    r.add_argument("--no-gen", action="store_true", help="leave out the generative model")
    args = ap.parse_args()
    {"extract": extract, "sample": sample, "label": label, "run": run}[args.cmd](args)


if __name__ == "__main__":
    main()
