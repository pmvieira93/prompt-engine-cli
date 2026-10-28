# PRD Appendix 1: Session Context and Automatic Enhancement

**Extends:** [PRD.md](PRD.md) version 1.1.0 (sections 4.2, 4.3, 4.5 and the Phase 4 roadmap)  
**Appendix Version:** 1.4.0  
**Date:** 2026-10-28  
**Status:** Proposed, planned as Phase 4.5

---

## 1. Background

Phase 4 delivered prompt enhancement as a **manual, reviewed** step:

* The developer presses the leader key, then `e` (Ctrl+Q, then e) to rewrite the prompt in Claude Code's input box with a local model (`llm.expand_model`, default `qwen2.5:7b`). `prompt-engine` appends the git branch and staged files itself.
* The rewrite is not sent: the developer reviews it, edits or undoes it (Ctrl+Q, then u), and presses Enter.
* The side panel shows what was typed against what was sent (FR-5).

The model only sees the current request. This appendix records two ideas raised during the Phase 4 review, to build on that once Phase 4 is in use.

---

## 2. Idea A: Session context for the local model (FR-6)

### 2.1 Problem

In a long Claude Code session, prompts often refer back: "now add tests for it", "also handle the error case", "same for the other file". Without knowing what came before, the local model can't rewrite these well, so expansion helps least where follow-ups are most common.

### 2.2 Proposal

Give the local model **session context** at a configurable level:

| Level | Context given to the model | Source | Cost and risk |
|---|---|---|---|
| **L0** | None (Phase 4 behaviour) | — | — |
| **L1** | The developer's last few prompts in this session | Already recorded by `prompt-engine` for the diff panel | Cheap and reliable. Low risk |
| **L2** | L1, plus a short summary of Claude Code's last reply | Claude Code's session transcript (`~/.claude/projects/…`), or the emulator's screen | Resolves "fix that". Ties `prompt-engine` to Claude Code's internal file format; summarising adds latency |
| **L3** | A rolling summary of the whole session, updated in the background after each reply | The local model, between turns | Richest context. Highest CPU use, and the most room for drift |

* **FR-6.1:** Support levels L0 and L1, selected in config (`expand.context: none | prompts`).
* **FR-6.2:** Optionally use L1 context for ghost-text suggestions too, so they follow the session's topic.
* **FR-6.3 (later):** L2 and L3, once L1 has been evaluated.

### 2.3 Risks and mitigations

* **The model invents requirements from context.** This was observed in Phase 4: given recent commit messages as context, every model tested treated them as the topic of the request ("fix … in config" became "fix the config reload functionality"). Mitigations:
  * label the context as being for resolving references only ("do not add tasks from it");
  * keep it short (the last 3–5 prompts at L1);
  * evaluate before enabling by default (2.4).
* **Latency.** More context means slower rewrites. Keep L1 small, and measure.
* **Privacy.** Everything stays local, but L2 reads Claude Code's own transcripts. It must stay opt-in and clearly documented.

### 2.4 Acceptance

Before choosing a default, compare L0 and L1 on a fixed set of follow-up prompts (as was done to pick the expansion model in Phase 4). L1 becomes the default only if it resolves references better **without** adding requirements.

---

## 3. Idea B: Automatic enhancement on Enter (FR-7)

### 3.1 Problem

Once a developer trusts the rewrites, pressing the expand shortcut and reviewing every prompt becomes friction. They want enhancement applied by default, with the diff panel showing afterwards what changed.

### 3.2 Proposal

A setting for when expansion runs:

| `expand.on_enter` | Behaviour |
|---|---|
| `off` (default) | Phase 4 behaviour: expansion only on the shortcut, reviewed before sending |
| `auto` | Enter expands the prompt, then sends it. The panel (on either side) shows the diff afterwards |
| `preview` (later, optional) | The rewrite is prepared in the background while the developer pauses, the panel shows it, and a key accepts it; Enter alone sends the prompt as typed |

* **FR-7.1:** `off` and `auto` modes.
* **FR-7.2:** In `auto`, the same exceptions as rules injection apply (slash commands, bash mode, memory notes, dialogs, unknown prompt text). Very short prompts ("yes", "continue", "1") are also sent as typed.
* **FR-7.3:** Escape hatches in `auto`:
  * Esc while "⟳ expanding…" is showing sends the prompt as typed;
  * the leader then Enter (already "submit as typed") skips expansion for one prompt.
* **FR-7.4:** Guard rails against bad rewrites (a stopped rewrite sends the prompt as typed, see 3.4). In Phase 4 testing the model produced "the RFCator tracker" (from the typo "rfcator"), "the ghost.txt file" and "[insert specific technology or method here]". In `auto`, a rewrite is **not** used when:
  * it replaces most of the words (the threshold is to be tuned on real prompts), or
  * it contains placeholder brackets, or
  * it drops or changes a name the developer typed: an agent, a skill, a command, a mention, a file, a path, or an issue, MR or PR number (any word with a hyphen, slash, dot, `@`, `#`, `!` or digits), or
  * the model errors or times out.

  The panel and the log say why.
* **FR-7.4b:** The name check also applies to expansion by shortcut: a rewrite that loses a name is not applied, the typed text is kept, and a notice says which name. Seen in Phase 4.5 testing: "now using code-reviewer look into MR !110" became "Can you review the code in merge request !110?". The expansion instructions also tell the model to keep such names; with that, every test rewrite kept them.
* **FR-7.4c:** Meaning guard rails, tuned on real 7B rewrites. In `auto`, a rewrite is also not used when it:
  * changes the number of negations (*not, never, without, only…*). Rewording is fine: "don't touch the tests" → "without changing the tests";
  * adds a destructive action the developer didn't ask for (*delete, remove, reset, force, push…*);
  * turns a question into an instruction ("and what about multi line prompts" → "Can you handle multi-line prompts?");
  * changes quoted or `code` text (this one applies to the shortcut too);
  * changes the language. Seen: Portuguese and Spanish prompts came back in English. `prompt-engine` detects the language and names it in the request when it isn't English, since naming languages in the instructions made the model translate English prompts;
  * brings in details from earlier prompts when the request doesn't refer back to them. Seen: "Don't touch the tests or the config loader." added to an unrelated request;
  * is far longer than what was typed (more than about 3× + 15 words);
  * lacks most of the typed words, compared in any order (more than 60%).
* **FR-7.4d:** `auto` leaves prompts that look pasted (over 400 characters, 6+ lines, or a code block) unexpanded, and holds Enter for at most `expand.auto_timeout` (default 5 s).
* **FR-7.4e (later):** Fix what the developer typed, and keep what they pasted. This replaces FR-7.4d's all-or-nothing skip, and the same rule should apply to expansion by shortcut:

  | Prompt | What is rewritten |
  |---|---|
  | Only typed text | All of it |
  | Typed text and a paste | Only the typed part. The paste is kept exactly: it is something being shown to the CLI (code, a log, a quote, a message) |
  | Only a paste | The paste is treated as the prompt (written elsewhere) and rewritten, except code blocks inside it, which are kept exactly |

  * Pasted spans are known from bracketed paste; code blocks (```` ``` ````) are kept exactly wherever they come from.
  * When there is a paste, the typed part gets typo fixes only: it is already worded around the paste.
  * A paste on its own that isn't a request (a stack trace, log lines, JSON, a diff, recognised by the shape of its lines) is sent as is. When unsure, it is sent as is.
  * Size: about 1 s per 100 words with the 7B model, so auto expansion stops at about 300 words of prose (code not counted), with the time limit scaled to the length.
  * The meaning guard rails (FR-7.4b, FR-7.4c) apply to the rewritten parts. If the parts can't be put back together exactly, the prompt is sent as typed.
* **FR-7.5:** The history log (FR-5.4) records whether a prompt was auto-expanded, and whether a guard rail stopped it.
* **FR-7.6 (later, optional):** `preview` mode, if `auto` turns out too blunt.

### 3.3 Risks

* **Nothing is reviewed before the CLI acts on it.** In Claude Code's auto mode, a misread request is executed immediately. This was observed in Phase 4 testing, when a test prompt ("fix the bug in the config") made Claude Code edit the repository. This is why `auto` is opt-in and the guard rails come first.
* **Latency on every Enter:** about 0.5–1.5 s with the 7B model once loaded, and about 3 s for the first prompt while it loads. Short prompts skip expansion (FR-7.2); the model can be warmed at startup when `auto` is on.

**Known limit:** a typo misread as a name ("rfcator tracker" → "the RFCator tracker") keeps nearly all the typed words, so no word-based check can tell it from a good typo fix.

### 3.4 Decision: a stopped rewrite sends the prompt as typed

When a guard rail stops a rewrite in `auto` mode, `prompt-engine` **sends the prompt as typed**, and the panel and log say why. The developer is never blocked.

*Decided 2026-10-28. The alternative, stopping and keeping the prompt in the input box for the developer to choose, was rejected.*

---

## 4. Roadmap: Phase 4.5

In order:

1. **L1 session context** (FR-6.1), with the L0/L1 comparison (2.4) before choosing its default.
2. **Automatic enhancement** (`expand.on_enter: auto`, FR-7.1 to FR-7.5), with the guard rails, the escape hatches, and short-prompt skipping.
3. A manual test round (the next part of `docs/TESTING.md`) before merging.

Later candidates: rewriting typed text around pastes (FR-7.4e, next after Phase 4.5 merges), L2/L3 context (FR-6.3), ghost text with session context (FR-6.2), and `preview` mode (FR-7.6).
