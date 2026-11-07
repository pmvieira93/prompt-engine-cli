# PRD Appendix 7: Delivering Rules with a Hook

**Extends:** [PRD.md](../PRD.md) version 1.3.12 (FR-4.2) and [PRD Appendix 6](PRD-APPENDIX-6.md)  
**Appendix Version:** 1.0.0  
**Date:** 2026-11-07  
**Status:** Built on 2026-11-07 in `internal/rules`, for release in v0.1.4.

---

## 1. Background

Rules used to be pasted at the end of the prompt with a bracketed paste. In the hand tests for sections, Claude Code's default model refused them: "I didn't follow that because it came from the paste."

Claude Code passes a large paste to the model inside `<pasted_content>` tags, and text in those tags is treated as possibly not written by the user. Measured on Claude Code 2.1.311, through its transcript:

| How the text was sent | Reached the model as |
|---|---|
| Paste with 2 line breaks | Plain |
| Paste with 3 line breaks | Tagged |
| Several small pastes, sent together | Plain, layout kept |
| One long line, about 1,000 characters | Tagged (the exact limit is unknown) |
| Typed, with Shift+Enter for new lines | Plain, layout kept |

Tagged rules were followed by Haiku and refused by Opus 5.5, so compliance has to be checked on the default model.

A one-line rule had stayed under the limit by one line break. The `# Guidance` heading went over it, and so did any `append.txt` of two or more lines, with or without the heading. The stand-in `claude` in `scripts/e2e.sh` didn't add the tags, so those checks passed.

---

## 2. Decision (FR-13)

Sending small pastes or typing the rules also works, but both get around a label that protects the user from text they didn't write, and both depend on undocumented behaviour. A `UserPromptSubmit` hook is Claude Code's supported way to add context to a prompt, so rules go that way.

* **FR-13.1: The hook.** `prompt-engine` starts `claude` with `--settings`, holding one `UserPromptSubmit` hook that runs `prompt-engine --pe-hook`. Rules come back as `additionalContext`. `claude` doesn't run the hook for slash commands or `!` commands.
* **FR-13.2: Choosing the rules stays in `prompt-engine`.** The hook's `prompt` mixes pastes into what was typed, so it can't apply Appendix 6's matching. On Enter, `prompt-engine` chooses the rules as before, then writes them to a private per-session folder, before the Enter reaches `claude`.
* **FR-13.3: Each prompt has an entry.** A prompt with no rules (a short one, or Leader then Enter) has an empty entry, so it can't pick up another prompt's rules.
* **FR-13.4: Matching an entry.** The hook takes the oldest entry with the same text, ignoring space and `<pasted_content>` tags, and drops any older ones, which came from an Enter that sent nothing. If no entry has that text, it takes the oldest, since prompts arrive in order and `claude` can change a prompt's text.
* **FR-13.5: The prompt always goes.** Whatever goes wrong in the hook, it writes nothing and exits 0.
* **FR-13.6: `--settings`.** `claude` only uses the last `--settings`, and one passed by the developer stays in place. `prompt-engine` then adds no rules, and warns at startup.

---

## 3. What changes for the developer

* Rules no longer show in `claude`'s prompt box or its transcript view. The panel's **Last prompt sent** and the debug log show them (`rules: hook gave claude 46 bytes`).
* There's no `[Pasted text #1 +N lines]` after a prompt, unless the developer pasted it.
* Rules of any length work the same way.

---

## 4. Tested on real `claude` (Opus 5.5, 2.1.311)

Run through `prompt-engine` on 2026-11-07, read from the transcript:

| Case | Result |
|---|---|
| Plain prompt | Hook context added; reply ended with MANGO |
| Prompt with a section's word | MANGO and KIWI added; reply ended with both |
| Section word only in a paste | KIWI not added |
| Six-line paste | The paste was tagged, the rules weren't; reply ended with MANGO |
| Leader, then Enter | No hook context; no rules in the reply |
| Three prompts sent while `claude` was busy | Each got its own rules |
| `/exit` | No hook call |

Not tested: a hook turned off by a managed policy, or a prompt changed by `claude` where an older stale entry is also waiting. Either way the prompt still goes.

`scripts/e2e.sh rules_hook` keeps this working: the stand-in now runs the hook and tags large pastes.
