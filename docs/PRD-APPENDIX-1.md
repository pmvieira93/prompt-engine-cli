# PRD Appendix 1: Session Context and Automatic Enhancement

**Extends:** [PRD.md](../PRD.md) version 1.1.0 (sections 4.2, 4.3, 4.5 and the Phase 4 roadmap)  
**Appendix Version:** 1.11.0  
**Date:** 2026-10-30  
**Status:** Built: FR-6.1 and FR-7.1 to FR-7.6 (with FR-7.4f) in Phase 4.5; FR-6.2 and FR-6.3 after it (2.5)

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
* **FR-6.3:** L2 and L3 (`expand.context: reply | session`), once L1 has been evaluated. Built after Phase 4.5; see 2.5.

### 2.3 Risks and mitigations

* **The model invents requirements from context.** This was observed in Phase 4: given recent commit messages as context, every model tested treated them as the topic of the request ("fix … in config" became "fix the config reload functionality"). Mitigations:
  * label the context as being for resolving references only ("do not add tasks from it");
  * keep it short (the last 3–5 prompts at L1);
  * evaluate before enabling by default (2.4).
* **Latency.** More context means slower rewrites. Keep L1 small, and measure.
* **Privacy.** Everything stays local, but L2 reads Claude Code's own transcripts. It must stay opt-in and clearly documented.

### 2.4 Acceptance

Before choosing a default, compare L0 and L1 on a fixed set of follow-up prompts (as was done to pick the expansion model in Phase 4). L1 becomes the default only if it resolves references better **without** adding requirements.

### 2.5 As built: ghost text context, L2 and L3 (FR-6.2, FR-6.3)

*Built 2026-10-30. L2 reads Claude Code's transcript, chosen over reading its screen: the transcript has the whole reply, while the screen has only what's still shown, mixed with tool boxes and spinners.*

**FR-6.2: Ghost text context** (`ghost.context: none | prompts`, default `prompts`; `ghost.context_prompts`, default 3)

* The suggestion model gets the last prompts as comment lines above the usual header, one line each (a newline would end the completion).
* Evaluation with `qwen2.5-coder:1.5b` on 10 cases:
  * all 7 follow-ups got suggestions that fit the session ("now add tests for the ret" → `ry mechanism in the http client.`, where L0 gave `urn value of the function`);
  * one of 3 unrelated prompts took an earlier topic ("explain how the input buf" → `…used in the payment service`);
  * the time was the same, 26–99 ms.
* **Decision:** on by default. A suggestion is easy to ignore, so the occasional earlier topic costs little.

**FR-6.3: L2 and L3** (`expand.context: reply | session`; the default stays `prompts`, since reading transcripts must be opt-in, 2.3)

* **Where the reply comes from.** Claude Code writes each session to `~/.claude/projects/<working directory, "/" as "-">/<session id>.jsonl` (or under `$CLAUDE_CONFIG_DIR`). As recorded from version 2.1.308:
  * prompts are `user` records with string content;
  * tool results are `user` records with `tool_result` blocks;
  * a reply is `assistant` records with `thinking`, `text` and `tool_use` blocks, the last with `stop_reason: end_turn`;
  * subagent (`isSidechain`) and `isMeta` records are skipped, and so are prompts starting with `<` (task notifications, slash command output, shell mode).
* **Which transcript.** The most recently written transcript that holds the last prompt sent, among those written since `prompt-engine` started (newest first, so after `/clear` the new one wins). Before the first prompt, none is used. Anything unreadable falls back to L1.
  * *Seen in manual testing:* the first version took the newest transcript when nothing had been sent yet. That was another Claude Code session in the same project folder, and its conversation went into the L3 summary. It stayed on the machine (the local model wrote the summary), but it was the wrong session.
  * *After `/clear`*, the summary starts again: `claude` keeps none of the earlier conversation, so neither does L3. The `/clear` command is recorded at the start of the new transcript.
  * *Checked live (2026-10-30)*, with VHS, Claude Code 2.1.308 and a plain `claude` sent "say hi in three words" in the same folder while `prompt-engine` ran:
    * before the first prompt, Ctrl+Q e read no transcript, both before and after the other session wrote its newer one;
    * after "name two colours…", the transcript read was this session's, and "tell me a fun fact about the second one" became "…about Red" (the reply was Blue, Red);
    * at the `session` level, the summaries held only colours and fruits, nothing of "say hi";
    * after `/clear`, the new transcript was read once a prompt was sent ("…about it" became "…about Mars"). But the summary kept "three fruits … still open", which `claude` no longer knew: fixed as above, and checked again.
* **What is used.** The reply is the text after the assistant's last tool call (its answer, not "Now I'll read…"). It's shortened to 150 words: the first quarter and the end, where options and closing questions are.
* **L3.** After each finished reply, the rewrite model updates an 80-word summary from the summary so far, the prompt and the reply. A rewrite cancels a summary in progress, which runs again afterwards, so rewrites never wait for it.
* **Guard rails.** A prompt **refers back** when it has "it", "that", "same"… or, only when a reply or summary was given, "the first one", "option 2", "yes, go ahead". When it does, the words a rewrite takes from the context are not counted as added by these guards:
  * `added-task`, `question`, `negation` (only added negations: a negation typed must stay) and `earlier-prompts`;
  * `replaced` doesn't apply when the prompt points at a reply, since "do the first one" is meant to be replaced;
  * `destructive`, `asked-back`, `names`, `quotes` and `length` apply as before.

  Without the "only when a reply was given" condition, an L1 rewrite of "fix the second one" built from the previous *prompt* got through.
* **Also fixed:** "do" starts a question only before "you", "I", "we" or "they". "do the first one" counted as a question before.

* **How the reply is given.** The last request and the reply go together, after the earlier requests ("The last request: … / The assistant's last reply, which the request may point at: …"), with the instruction to name what the request points at. Each part was needed, as seen in testing:
  * in `"""` quotes, the reply was left alone ("say the second one in portuguese" kept "the second one");
  * listed apart from its request, an older request won ("now the first one in french", after a list of fruits, took the colours asked for two requests earlier);
  * naming the request inside the reply's label made the model answer the request.
* **Also fixed in the guard rails.**
  * Negations inside quotes or `code` are names, not instructions, so they aren't counted: before, a rewrite that dropped "don't touch the tests" passed thanks to a quoted "only… not…".
  * "…" ends a word, so "only…" counts as "only".

**Evaluation.** `qwen2.5:7b`, 2 tries per level, each checked by the guard rails as in `auto` mode:
* 8 follow-ups after recorded-style exchanges;
* 4 more after a list of colours or fruits (the K16 and K20 cases);
* 3 unrelated prompts.

| Prompt | L1 (prompts) | L2 (+ reply) | L3 (+ summary) |
|---|---|---|---|
| "do the first one" (2 options offered) | Wrong ("Why does the config reload…?"), stopped | "Apply panel.visible live by opening or closing the panel when it changes." | "Apply changes to the panel live." (vague) |
| "ok go with option 2 and update the docs too" | "Update the code with option 2…" (vague) | "Apply panel.visible as it is and update the docs to reflect this." | Dropped "update the docs", stopped |
| "yes go ahead" (after "Should I split it?") | Wrong ("Fix the build failure on Linux."), stopped | "Sure, split it into termios_darwin.go and termios_linux.go." | Added a task, stopped |
| "it still fails, same error" | "The build still fails with the same error on Linux." | Copied the reply's question, stopped | Added a task, stopped |
| "fix the second one but dont touch the tests" | Wrong (the previous prompt), stopped | Right once; once added a negation, stopped | "Fix the "only… not…" guard for false positives, but don't touch the tests." |
| "explain that again more simply" | Wrong, stopped | "Explain the two guard rails for false positives more simply." | Right, longer |
| "tell me a fun fact about the second one" (after "1. Blue 2. Orange") | "…the second colour from the earlier numbered list", stopped | "Tell me a fun fact about Orange." | Same |
| "write a haiku about the second one" (after 3 fruits) | Wrong ("the third fruit") | "Write a haiku about Mango." | Same |
| "say the second one in portuguese" | "Say the second colour in Portuguese." | Answered it ("Orange em português é laranja."), stopped | "Say the second one in Portuguese: 2. Orange" |
| "now the first one in french" (after 3 fruits) | Wrong ("the first colour") | "Name the first fruit in French: 1. Apple" | Answered it ("1. Apple (in French: Pomme)") and **passed** |
| 3 unrelated prompts | Nothing added | Nothing added | Nothing added |

* **L2** resolved the follow-ups pointing at a reply in 15 of 18 tries. The other 3 were stopped and would be sent as typed, and no wrong rewrite got through. L1 resolved none: it guessed from the earlier prompts, and passed 2 wrong guesses ("the third fruit", "the first colour").
* **L3** was no better: vaguer or wordier rewrites, and once it answered the request and passed the guards. The summaries took 1.5–8 s of model time after each reply.
* **Known limit.** Asking to translate something in the reply can make the model answer instead of rewriting. The language guard stops it in `auto` and `preview` mode; with the shortcut, the developer sees it and can undo it.
* **Time.** L2 and L3 rewrites took 0.4–3 s (5 s for the longest), up to about 1.5 s more than L1, since the request is longer.
* **Decision:** `reply` is the recommended level and `session` stays available, not recommended. The default stays `prompts` until the developer opts in to transcript reading.

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
| `preview` | The rewrite is prepared in the background while the developer pauses, the panel shows it, and a key accepts it; Enter alone sends the prompt as typed |

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
  * adds a task to a prompt that asked for none: a report, or a request with a verb not in the list (*fix, ensure, check, investigate…*). Seen: "it still fais" → "Fix the error in the sentence: it still fails."; a list of "same …" → "Ensure the "hello" message is displayed…", taken from an earlier prompt. A typed word with the same first three letters counts, so a typo'd verb ("refactr") isn't a new task. It's checked before the negations, since such rewrites often add one too ("Ensure the code runs **without** errors");
  * drops a whole part of the prompt: a sentence or line with three or more content words, none of which is in the rewrite (`dropped-part`). Seen: "…keep the answer short; this promtp is just long enough to wrap onto a second row" lost its second sentence, and only "only", when it was there, got the rewrite stopped. Words are compared by their first four letters, so fixed typos count; replayed on 243 rewrites, it stopped only the 3 that dropped that sentence;
  * changes quoted or `code` text (this one applies to the shortcut too);
  * changes the language. Seen: Portuguese and Spanish prompts came back in English. `prompt-engine` detects the language and names it in the request when it isn't English, since naming languages in the instructions made the model translate English prompts;
  * brings in details from earlier prompts when the request doesn't refer back to them. Seen: "Don't touch the tests or the config loader." added to an unrelated request;
  * is far longer than what was typed (more than about 3× + 15 words);
  * lacks most of the typed words, compared in any order (more than 60%).

  **Seen in real use (2026-10-30).** In `preview` mode (style `default`, `expand.context: prompts`) the panel often showed "No rewrite suggested … the rewrite turned your question into an instruction (changes, run, commit)". We had none of the typed prompts, so we replayed 45 prompts with `qwen2.5:7b`: questions about changes, tests and commits, polite requests ("can you proceed"), questions ending with a request ("…, can you take a look"), typo'd and Portuguese ones. Each ran with no earlier prompts and with three sets of three (two in English, one in Portuguese, all running tests and committing changes), 2 tries each: 360 rewrites. Before the fix, 149 passed and the `question` guard stopped 161. The causes:
  * **Fixed typos counted as new words** (32 stops, false positives). "shuold i comit the chnages now?" → "Should I commit the changes now?" was stopped for "changes, commit", and "what chages are neded to rnu it on linux?" for "changes, run". With earlier prompts, the same rewrites were stopped by `earlier-prompts` instead ("comit" isn't "commit").
  * **Short Portuguese questions weren't recognised** (24 stops). "posso fazer commit das alterações agora?" has one known word ("das"), so the model wasn't told the language and wrote "Can I commit the changes now?", stopped for "changes". "consegues ver porque falha no linux?" was also stopped as a lost negation, since "no" counted as English.
  * **Earlier prompts in place of the question** (most of the rest; the guard is right). After "run go test ./… then commit the changes", "anything else before we merge?" and "what would you do next" became "Run go test ./… and make sure everything passes, then commit the changes.": exactly "changes, run, commit". "is everything ok now?" became "Is everything ok now with the --verbose flag added, tests run, and changes committed?". Telling the model "a question stays the same question: never answer it, and never replace it with an earlier request" changed nothing (63 → 59 stops on the same 120 tries), so it wasn't kept.
  * **A context word under four letters didn't match its other forms** (about 9 stops). "is it ready to commit?" refers back, but "Is the --verbose flag added to the CLI and ready to commit?" was stopped for "added", since "add" and "added" have different four-letter keys.

  Fixed: a rewrite word one typo away from a typed word (a letter added, dropped or changed, or two swapped; only a swap under four letters, since "and" → "add" is another word) isn't new for `question` and `earlier-prompts`; common Portuguese words ("está", "já", "posso", "consegues", "agora", "alterações"…) are known; and a three-letter context word matches the words it starts ("run", "running"). After the fix, 210 of 360 passed and `question` stopped 103: all 40 typo'd tries passed, 38 of 56 Portuguese tries passed (12 before), and every question replaced by an earlier prompt or answered is still stopped. *Left as is:* "can you do the same for the server package" refers back ("same"), so its rewrite may take all three earlier prompts (both tries, one before the fix); 7 tries without context use an action word as a synonym ("which files did we touch" → "Which files did we modify?"); and statements whose second word is "is", "are" or "can" ("tests are failing on ci…") count as questions, so a task added to them is named `question` rather than `added-task`. The developer's own typed text would show which of these causes they hit.
* **FR-7.4d:** `auto` holds Enter for at most `expand.auto_timeout` (default 5 s), plus time for long prompts (FR-7.4e). *Originally it also left prompts that looked pasted (over 400 characters, 6+ lines, or a code block) unexpanded; FR-7.4e replaced that.*
* **FR-7.4e:** Fix what the developer typed, and keep what they pasted. This replaces FR-7.4d's all-or-nothing skip, and applies to expansion by shortcut too:

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

  As built (tested with `qwen2.5:7b`):
  * Kept parts reach the model as markers (`<<PASTE_1>>`), which it copied exactly and in place in every test; square-bracket markers got moved. A rewrite that drops, repeats or reorders a marker is not used.
  * Typo-only instructions are used around a paste and for prompts over 60 words, since the rewording instructions ask for under 60 words. In typo mode the model copied the "Request:" label, so the label is "Message:", and an echoed label is removed.
  * A 92-word prompt took 5.3 s, so the time limit grows by about 6 s per 100 words, up to 300 words.
  * Session context (FR-6) gives earlier prompts with their pasted parts as "(pasted text)". Seen in testing: given an earlier pasted trace, the model put it in place of a new paste's marker.
  * A question at the end of a prompt ("…, can you fix it") counts as a question, so a rewrite that adds its "?" isn't stopped.
  * Typo fixes kept the markers in every test, and fixed 7 of 8 prompts; one (`what does this, just answr:`) came back as `just ansr:` every time. Like the RFCator case, no word-based check can catch it.
  * Not handled: a paste edited afterwards counts as typed; a long paste that `claude` shows as `[Pasted text #1 +N lines]` is not rewritten if the prompt text came from the screen.
* **FR-7.4f:** Each guard rail has a short name, used in the log and the history (`guard`): `empty`, `placeholder`, `asked-back`, `language`, `names`, `quotes`, `destructive`, `question`, `added-task`, `dropped-part`, `negation`, `earlier-prompts`, `length`, `replaced`, and `pastes` (a marker dropped or moved). With `panel.guard_details: true` (default off), the panel also shows, in two short lines, the guard and what tripped it, then the start of the rewrite it stopped (`Guard added-task: ensure, check` / `Rejected: "Ensure the code runs…"`), for preview suggestions and for prompts auto mode sent as typed. A first version with the full reason and the whole rewrite was too long for the panel. The log always has both in full.
* **FR-7.5:** The history log (FR-5.4) records whether a prompt was auto-expanded, whether a guard rail stopped it, and which (`guard`); with `history.include_text`, also the rewrite it stopped (`rejected`).
* **FR-7.6:** `preview` mode, for when `auto` is too blunt. As built:
  * After a pause in typing (`expand.preview_pause`, default 1 s), the rewrite is prepared in the background; typing again cancels it.
  * The panel shows it as "Suggested rewrite (not used yet)", or "No rewrite suggested" with the reason. With the panel closed, a notice at the cursor says a rewrite is ready.
  * The expand shortcut (Ctrl+Q, then e) applies a ready suggestion at once, so no new key is needed; without one it expands as before. Enter always sends what was typed.
  * Suggestions pass the same guard rails as `auto` (FR-7.4 to FR-7.4e), so what is offered is what `auto` would send.
  * A rewrite a guard rail stops gets one more try: the model gives a different rewrite each time, and in testing one try in three turned a report into a task where the next didn't. `auto` doesn't retry, since Enter is held while it runs.
  * Cost: the expansion model runs after every pause, about 1 s each with `qwen2.5:7b`.

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

Built after the Phase 4.5 merge: rewriting typed text around pastes (FR-7.4e), then `preview` mode (FR-7.6), rewrite styles ([PRD Appendix 2](PRD-APPENDIX-2.md)), and then ghost text with session context (FR-6.2) and L2/L3 context (FR-6.3), in 2.5.
