#!/usr/bin/env python3
"""Pastes and clarifications for PRD Appendix 5, step 1.

Reads Claude Code's local transcripts and prints totals only. No prompt text
is printed, and nothing is sent anywhere.

    python3 docs/spikes/token-baseline/turns.py [-skip id] [-replies out.json]

It answers the two items baseline.py left open:

  * how much of the input comes from pastes, as typed and as read back:
    each message stays in the context, so it is read again by every later
    API call until /compact or /clear;
  * how many turns are clarifications: Claude asks for a choice or for
    information (in text or with AskUserQuestion) and the developer answers.
    A question that offers the next step ("Shall I merge it?") asks for
    permission instead, and is counted apart.

Claude Code tags a paste of 3 or more lines as <pasted_content id="…">;
shorter pastes look like typing, so the paste share is a lower bound.

-replies saves the replies to Claude's questions, with the question's last
line and its kind, for checking the split by hand. That file holds real prompts, so keep
it out of the repo. -skip leaves one session (by its first 8 characters)
out of the figures.
"""
import argparse
import collections
import glob
import json
import os
import re
import statistics as st

PASTE = re.compile(r'<pasted_content id="([^"]*)">(.*?)</pasted_content id="\1">', re.S)
# A question that offers the next step ("Shall I merge it?") asks for
# permission; any other asks for a choice or for information, which is a
# clarification.
OFFER = re.compile(
    r"\b(shall i|should i|want me to|do you want me to|would you like me to|can i|may i|"
    r"ready (?:for me )?to|okay to|ok to|want (?:it|both|that|them)\b)\b",
    re.I,
)
# A prompt that corrects or restates what Claude did, without being asked.
CORRECTION = re.compile(r"^(no\b|nope|not\b|wrong|that's not|that is not|i meant|actually|instead)", re.I)
HUMAN_SOURCES = {"typed", "suggestion_accepted", "queued"}


def text_of(content):
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "\n".join(b.get("text", "") for b in content if isinstance(b, dict) and b.get("type") == "text")
    return ""


def chars_of(content):
    """All the characters a message adds to the context, tool input included."""
    if isinstance(content, str):
        return len(content)
    n = 0
    for b in content or []:
        if not isinstance(b, dict):
            continue
        if b.get("type") in ("text", "thinking"):
            n += len(b.get("text", "") or b.get("thinking", ""))
        elif b.get("type") == "tool_use":
            n += len(json.dumps(b.get("input", {})))
        elif b.get("type") == "tool_result":
            n += len(text_of(b.get("content")))
    return n


def is_prompt(r, text):
    """Whether a user record is something the developer sent."""
    if r.get("toolUseResult") is not None or r.get("isMeta") or r.get("isSidechain") or r.get("isCompactSummary"):
        return False
    if not text.strip():
        return False
    source = r.get("promptSource")
    if source is not None:
        return source in HUMAN_SOURCES
    # Older versions don't say: leave out notices and command output.
    if text.lstrip().startswith("<pasted_content"):
        return True
    return not text.lstrip().startswith(("<", "Caveat:", "[Request interrupted", "This session is being continued"))


def asked(record):
    """Returns the question an assistant message ends on, or None."""
    content = record.get("message", {}).get("content")
    if not isinstance(content, list):
        return None
    for b in content:
        if isinstance(b, dict) and b.get("type") == "tool_use" and b.get("name") == "AskUserQuestion":
            return "AskUserQuestion"
    text = text_of(content).rstrip()
    if text.endswith("?"):
        return text.splitlines()[-1][-200:]
    return None


def read_session(path):
    s = collections.Counter(id=0)
    s["id"] = os.path.basename(path)[:8]
    records = []
    for line in open(path, errors="ignore"):
        try:
            records.append(json.loads(line))
        except ValueError:
            continue
    records.sort(key=lambda r: r.get("timestamp", ""))

    # added: (kind, characters) of each message in the context, in order;
    # calls[i]: how many API calls came after the i-th message, until the
    # context was reset by /compact or /clear.
    added, seen, last_question = [], set(), None
    # The context's growth between calls, against the characters added in
    # between, gives the characters per token actually seen; thinking is
    # in the context but not in the transcript, so this is below 4.
    last_context, since = None, 0
    replies = []
    for r in records:
        if r.get("isSidechain"):
            continue
        kind = r.get("type")
        msg = r.get("message", {}) if isinstance(r.get("message"), dict) else {}
        if kind == "assistant":
            mid = msg.get("id")
            u = msg.get("usage")
            if u and mid not in seen:
                seen.add(mid)
                s["api"] += 1
                if s["api"] == 1:
                    # The first call is mostly the system prompt and tools,
                    # which every later call sends again.
                    s["base"] = sum(u.get(k, 0) for k in ("input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"))
                s["base read"] += s["base"]
                context = sum(u.get(k, 0) for k in ("input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"))
                s["context"] += context
                if last_context is not None and 0 < context - last_context < 200_000 and since:
                    s["growth tokens"] += context - last_context
                    s["growth chars"] += since
                last_context, since = context, 0
                for item in added:
                    item[2] += 1
            added.append(["assistant", chars_of(msg.get("content")), 0])
            since += added[-1][1]
            q = asked(r)
            if q is not None:
                last_question = q
            elif any(isinstance(b, dict) and b.get("type") == "text" for b in msg.get("content") or []):
                last_question = None
        elif kind == "user":
            if r.get("isCompactSummary"):
                s["resets"] += 1
                last_context, since = None, 0
                s.update(read_back(added))
                added = []
            content = msg.get("content")
            text = text_of(content)
            if not is_prompt(r, text):
                added.append(["tool result" if r.get("toolUseResult") is not None else "other", chars_of(content), 0])
                since += added[-1][1]
                continue
            pasted = sum(len(m.group(2)) for m in PASTE.finditer(text))
            s["prompts"] += 1
            s["pastes"] += len(PASTE.findall(text))
            s["prompts with a paste"] += 1 if pasted else 0
            s["images"] += len(r.get("imagePasteIds") or [])
            s["typed chars"] += len(text) - pasted
            s["pasted chars"] += pasted
            added.append(["typed", len(text) - pasted, 0])
            added.append(["pasted", pasted, 0])
            since += len(text)
            reply = PASTE.sub(" ", text).strip()
            if last_question is not None:
                s["after a question"] += 1
                offer = last_question != "AskUserQuestion" and OFFER.search(last_question)
                s["after permission" if offer else "after clarification"] += 1
                replies.append(dict(question=last_question, kind="permission" if offer else "clarification", reply=reply[:300]))
            elif CORRECTION.match(reply):
                s["correction"] += 1
            last_question = None
        elif kind == "attachment":
            n = sum(len(x.get("content", "")) for x in r.get("rendered") or [] if isinstance(x, dict))
            added.append(["attachment", n, 0])
            since += n
    s.update(read_back(added))
    return s, replies


def read_back(added):
    """Characters each kind of message is read back for, over the API calls after it."""
    out = collections.Counter()
    for kind, n, calls in added:
        out["read " + kind] += n * calls
        out["chars " + kind] += n
    return out


def report(name, sessions):
    t = collections.Counter()
    for s in sessions:
        t.update({k: v for k, v in s.items() if k not in ("id", "base")})
    print(f"\n{name}: {len(sessions)} sessions, {t['prompts']} prompts, {t['api']} API calls, {t['resets']} compactions")

    print("\npastes")
    print(f"  prompts with a tagged paste {t['prompts with a paste']} ({pct(t['prompts with a paste'], t['prompts'])}), "
          f"{t['pastes']} pastes, {t['images']} images")
    print(f"  pasted characters {t['pasted chars']}, typed {t['typed chars']}: "
          f"pastes are {pct(t['pasted chars'], t['pasted chars'] + t['typed chars'])} of what was sent")

    print("\nwhat the context reads back (estimated at 4 characters per token)")
    kinds = ["typed", "pasted", "tool result", "assistant", "attachment", "other"]
    context = t["context"]
    for k in kinds:
        print(f"  {k:12} {pct(t['read ' + k] / 4, context):>7} of the input tokens   (added once: {t['chars ' + k] // 4} tokens)")
    print(f"  {'system':12} {pct(t['base read'], context):>7} of the input tokens   (system prompt and tools: each session's first call, sent again by every call)")
    rest = context - t["base read"] - sum(t["read " + k] for k in kinds) / 4
    print(f"  {'unexplained':12} {pct(rest, context):>7}   (thinking, images, history from before a resume)")
    cpt = t["growth chars"] / max(1, t["growth tokens"])
    print(f"  the context grows by a token per {cpt:.1f} characters of transcript; at that rate (an upper bound),")
    print(f"  typed text is {pct(t['read typed'] / cpt, context)} and pastes {pct(t['read pasted'] / cpt, context)} of the input tokens")
    print("\nclarifications")
    print(f"  prompts after Claude asked a question {t['after a question']} ({pct(t['after a question'], t['prompts'])})")
    print(f"    asking permission for the next step     {t['after permission']} ({pct(t['after permission'], t['prompts'])})")
    print(f"    asking for a choice or information      {t['after clarification']} ({pct(t['after clarification'], t['prompts'])})")
    print(f"  prompts correcting Claude unasked {t['correction']} ({pct(t['correction'], t['prompts'])})")
    per = [s["after a question"] / s["prompts"] for s in sessions if s["prompts"] >= 5]
    if per:
        print(f"  share of prompts after a question, per session with 5+ prompts: median {pct(st.median(per), 1)}")


def pct(a, b):
    return f"{a / b * 100:.1f}%" if b else "n/a"


def main():
    p = argparse.ArgumentParser()
    p.add_argument("-skip", default="")
    p.add_argument("-replies")
    args = p.parse_args()

    sessions, replies = [], []
    for f in glob.glob(os.path.expanduser("~/.claude/projects/*/*.jsonl")):
        s, r = read_session(f)
        if s["api"]:
            sessions.append(s)
            replies += r
    if args.replies:
        with open(args.replies, "w") as f:
            json.dump(replies, f, indent=1)

    report("all sessions", sessions)
    if args.skip:
        report("without " + args.skip, [s for s in sessions if s["id"] != args.skip])


if __name__ == "__main__":
    main()
