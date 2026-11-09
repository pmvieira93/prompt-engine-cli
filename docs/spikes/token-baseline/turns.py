#!/usr/bin/env python3
"""Pastes and clarifications for PRD Appendix 5, step 1.

Reads Claude Code's local transcripts and prints totals only. No prompt text
is printed, and nothing is sent anywhere.

    python3 docs/spikes/token-baseline/turns.py [-skip id] [-replies out.json]

It answers the two items baseline.py left open:

  * how much of the input comes from pastes, as typed and as read back:
    each message stays in the context, so it is read again by every later
    API call until /compact or /clear;
  * how many turns are clarifications: Claude asks, in text or with
    AskUserQuestion, for a choice about the task or for information, and
    the developer answers. Questions offering the next step ("Shall I merge
    it?", "Which would you like?") are counted apart.

Claude Code tags a paste of 3 or more lines as <pasted_content id="…">;
shorter pastes look like typing, so the paste share is a lower bound.

-replies saves the replies to Claude's questions, with the question and its
kind, for checking the split by hand. That file holds real prompts, so keep
it out of the repo. -skip leaves one session (by its first 8 characters)
out of the figures.
"""
import argparse
import collections
import json
import os
import re
import statistics as st
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from baseline import context_tokens, transcripts, typed_text  # noqa: E402

PASTE = re.compile(r'<pasted_content id="([^"]*)">(.*?)</pasted_content(?: id="\1")?>', re.S)
# Claude's questions are of three kinds:
#   * permission: it offers the next step ("Shall I merge it?");
#   * next step: it offers a choice of what to do next ("Shall I merge it,
#     or write the notes first?", "Which would you like?");
#   * clarification: anything else, asking for a choice about the task or
#     for information ("How should this behave?", "Which of the two
#     happened?"). Only these could a clearer prompt have avoided.
# An offer opens the question or one of its clauses.
OFFER = re.compile(
    r"(?:^|[,—:]\s*)(?:so,? |and |then,? |also,? )?(shall i|should i|want me to|do you want me to|"
    r"would you like me to|can i|may i|ready (?:for me )?to|okay to|ok to|want (?:it|both|that|them)\b|"
    r"queres que|quer que|devo|posso)\b",
    re.I,
)
CHOICE = re.compile(r"\b(or|ou)\b", re.I)
NEXT = re.compile(
    r"^(which\b.*\b(would you like|do you want|do you prefer)|which (way|option|one|direction)\b|"
    r"what would you like|what do you want to work on|(do you want|would you rather)\b.*\bor\b)",
    re.I,
)
# A prompt that corrects or restates what Claude did, without being asked.
CORRECTION = re.compile(r"^(no\b|nope|not\b|wrong|that's not|that is not|i meant|actually|instead)", re.I)
HUMAN_SOURCES = {"typed", "suggestion_accepted", "queued"}
CODE = re.compile(r"```.*?```", re.S)
LIST_ITEM = re.compile(r"^\s*(?:[-*]|\d+[.)])\s")
# The API drops thinking from earlier turns, so it isn't read back; within
# a turn it is, but the transcript doesn't keep its text.
COUNTED = ("text", "tool_use", "tool_result")


def text_of(content):
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "\n".join(b.get("text", "") for b in content if isinstance(b, dict) and b.get("type") == "text")
    return ""


def chars_of(content):
    """The characters a message adds to the context for later calls."""
    if isinstance(content, str):
        return len(content)
    n = 0
    for b in content or []:
        if not isinstance(b, dict) or b.get("type") not in COUNTED:
            continue
        if b["type"] == "text":
            n += len(b.get("text", ""))
        elif b["type"] == "tool_use":
            n += len(json.dumps(b.get("input", {})))
        else:
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
    return text.lstrip().startswith("<pasted_content") or typed_text(r) is not None


def final_question(text):
    """Returns the question a reply ends on, or None: the last sentence with
    a "?" in the last paragraph, or in the one before a closing list."""
    text = re.sub(r"[*_`]", "", CODE.sub("", text))
    paragraphs = [p.strip() for p in re.split(r"\n\s*\n", text) if p.strip()]
    if not paragraphs:
        return None
    last = paragraphs[-1:]
    if all(LIST_ITEM.match(line) for line in paragraphs[-1].splitlines()) and len(paragraphs) > 1:
        last = paragraphs[-2:]
    for paragraph in reversed(last):
        # A "." ends a sentence only before a space, so "v0.1.5?" is kept.
        sentences = re.findall(r"(?:[^.?!\n]|\.(?!\s))*\?", paragraph)
        if sentences:
            return sentences[-1].strip()[-200:]
    return None


def question_kind(question):
    if OFFER.search(question):
        return "next step" if CHOICE.search(question) else "permission"
    return "next step" if NEXT.match(question) else "clarification"


def read_session(path):
    s = collections.Counter()
    sid = os.path.basename(path)[:8]
    records = []
    for line in open(path, errors="ignore"):
        try:
            records.append(json.loads(line))
        except ValueError:
            continue
    records.sort(key=lambda r: r.get("timestamp", ""))

    # added: [kind, characters, the API call count when it was added] of
    # each message in the context, until /compact resets it. A message is
    # read back by every call after it.
    added, seen = [], set()
    # The question Claude's last words asked, and AskUserQuestion's.
    last_question, asking = None, {}
    # The context's growth between calls, against the characters added in
    # between, gives the characters per token actually seen; thinking is
    # in the context within a turn but not in the transcript, so this is
    # below 4.
    last_context, since = None, 0
    before_first = 0  # characters sent with the first call, besides the system prompt
    replies = []

    def add(kind, n):
        nonlocal since
        added.append((kind, n, s["api"]))
        since += n

    def answer(question, reply):
        kind = question_kind(question)
        s["after a question"] += 1
        s["after " + kind] += 1
        replies.append(dict(question=question, kind=kind, reply=reply[:300]))

    for r in records:
        if r.get("isSidechain"):
            continue
        kind = r.get("type")
        msg = r.get("message") if isinstance(r.get("message"), dict) else {}
        content = msg.get("content")
        if kind == "assistant":
            mid, u = msg.get("id"), msg.get("usage")
            if u and mid not in seen:
                seen.add(mid)
                context = context_tokens(u)
                s["api"] += 1
                if s["api"] == 1:
                    # The first call is the system prompt and tools, which
                    # every later call sends again, and what came before it.
                    s["base"] = max(0, context - before_first // 4)
                s["base read"] += s["base"]
                s["context"] += context
                if last_context is not None and 0 < context - last_context < 200_000 and since:
                    s["growth tokens"] += context - last_context
                    s["growth chars"] += since
                last_context, since = context, 0
            add("assistant", chars_of(content))
            blocks = [b for b in content or [] if isinstance(b, dict)]
            for b in blocks:
                if b.get("type") == "tool_use" and b.get("name") == "AskUserQuestion":
                    questions = (b.get("input") or {}).get("questions") or [{}]
                    asking[b.get("id")] = questions[0].get("question", "")
            # Text or a tool call is Claude's latest word; thinking isn't.
            if any(b.get("type") in ("text", "tool_use") for b in blocks):
                last_question = final_question(text_of(content))
        elif kind == "user":
            if r.get("isCompactSummary"):
                s["resets"] += 1
                last_context, since = None, 0
                s.update(read_back(added, s["api"]))
                added = []
            text = text_of(content)
            if r.get("toolUseResult") is not None:
                for b in content if isinstance(content, list) else []:
                    if isinstance(b, dict) and b.get("tool_use_id") in asking:
                        s["prompts"] += 1  # the developer's answer, picked or typed
                        answer(asking.pop(b["tool_use_id"]), "(AskUserQuestion)")
            if not is_prompt(r, text):
                add("tool result" if r.get("toolUseResult") is not None else "other", chars_of(content))
                if not s["api"]:
                    before_first += added[-1][1]
                continue
            pastes = list(PASTE.finditer(text))
            pasted = sum(len(m.group(2)) for m in pastes)
            typed = len(text) - sum(len(m.group(0)) for m in pastes)  # the tags are neither
            s["prompts"] += 1
            s["pastes"] += len(pastes)
            s["prompts with a paste"] += 1 if pastes else 0
            s["images"] += len(r.get("imagePasteIds") or [])
            s["typed chars"] += typed
            s["pasted chars"] += pasted
            add("typed", typed)
            add("pasted", pasted)
            if not s["api"]:
                before_first += len(text)
            reply = PASTE.sub(" ", text).strip()
            if last_question is not None:
                answer(last_question, reply)
            elif CORRECTION.match(reply):
                s["correction"] += 1
            last_question = None
        elif kind == "attachment":
            n = sum(len(x.get("content", "")) for x in r.get("rendered") or [] if isinstance(x, dict))
            add("attachment", n)
            if not s["api"]:
                before_first += n
    s.update(read_back(added, s["api"]))
    return sid, s, replies


def read_back(added, calls):
    """Characters each kind of message is read back for, over the API calls after it."""
    out = collections.Counter()
    for kind, n, at in added:
        out["read " + kind] += n * (calls - at)
        out["chars " + kind] += n
    return out


def report(name, sessions):
    t = collections.Counter()
    for s in sessions.values():
        t.update({k: v for k, v in s.items() if k != "base"})
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
    print(f"  {'system':12} {pct(t['base read'], context):>7} of the input tokens   "
          "(system prompt and tools: each session's first call, less what came before it, sent again by every call)")
    rest = context - t["base read"] - sum(t["read " + k] for k in kinds) / 4
    print(f"  {'unexplained':12} {pct(rest, context):>7}   (thinking within a turn, images, and the 4-character estimate)")
    if t["growth chars"] and t["growth tokens"]:
        cpt = t["growth chars"] / t["growth tokens"]
        print(f"  the context grows by a token per {cpt:.1f} characters of transcript; at that rate (an upper bound),")
        print(f"  typed text is {pct(t['read typed'] / cpt, context)} and pastes {pct(t['read pasted'] / cpt, context)} of the input tokens")

    print("\nclarifications (prompts include answers to AskUserQuestion)")
    print(f"  prompts after Claude asked a question {t['after a question']} ({pct(t['after a question'], t['prompts'])})")
    for kind, label in (("permission", "permission for the next step"), ("next step", "a choice of next step"),
                        ("clarification", "a choice about the task, or information")):
        print(f"    asking {label:40} {t['after ' + kind]} ({pct(t['after ' + kind], t['prompts'])})")
    print(f"  prompts correcting Claude unasked {t['correction']} ({pct(t['correction'], t['prompts'])})")
    per = [s["after clarification"] / s["prompts"] for s in sessions.values() if s["prompts"] >= 5]
    if per:
        print(f"  clarifications per session with 5+ prompts ({len(per)}): median {pct(st.median(per), 1)}, max {pct(max(per), 1)}")


def pct(a, b):
    return f"{a / b * 100:.1f}%" if b else "n/a"


def main():
    p = argparse.ArgumentParser()
    p.add_argument("-skip", default="")
    p.add_argument("-replies")
    args = p.parse_args()

    sessions, replies = {}, []
    for f in transcripts():
        sid, s, r = read_session(f)
        if s["api"]:
            sessions[sid] = s
            replies += r
    if args.replies:
        with open(args.replies, "w") as f:
            json.dump(replies, f, indent=1)

    report("all sessions", sessions)
    if args.skip:
        report("without " + args.skip, {k: v for k, v in sessions.items() if k != args.skip})


if __name__ == "__main__":
    main()
