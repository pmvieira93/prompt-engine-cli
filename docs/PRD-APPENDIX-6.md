# PRD Appendix 6: Rules Chosen by Intent

**Extends:** [PRD.md](../PRD.md) version 1.3.5 (FR-4.2, static rules from `templates/append.txt`) and [PRD Appendix 1](PRD-APPENDIX-1.md) (FR-7, the rewrite on Enter)  
**Appendix Version:** 2.3.0  
**Date:** 2026-11-06  
**Status:** Built on 2026-11-06 in `internal/rules`. Raised on 2026-11-04 with the local model naming the intent. Changed on 2026-11-05 to matching words the developer lists, after a [spike](spikes/intent.md) (section 4).

---

## 1. Background

`templates/append.txt` is added to the end of every prompt sent (FR-4.2). That fits a rule that always applies, but not one that only matters for some work: "Run the tests before saying it works" means nothing on a request to explain a function. Seeing the same text on every prompt gets boring, and it costs tokens each time.

The words of the prompt usually show what kind of work it is, so they can decide which rules are added. The sections and their words are the developer's own, in `append.txt`.

---

## 2. Proposal (FR-12)

* **FR-12.1: Sections in `append.txt`.** A line `[word]` starts a section, which runs to the next one. Several words for one section are separated by `|`. The sections and their words are the developer's own: the ones below are only an example. Text before the first section is added to every prompt that isn't short (`templates.min_words`), as today.

  ```
  Keep changes small.

  [review|PR|MR]
  Check the error paths too.

  [test|tests|coverage]
  Test behaviour, not implementation.

  [investigate|analysis|why]
  Don't change any code yet: report what you find.

  [debug|bug|crash]
  Find the cause before proposing a fix.
  ```

  A file with no sections works exactly as it does today. A `[word]` line inside a code block is the block's text. A word used twice, one that isn't a word, or a section with no text rejects the file, naming the line, as `guards.txt` does. A rejected file adds no rules until it's fixed: the warning shows at startup and in the Session section, and the log names it on each prompt.

* **FR-12.2: Matching.** On Enter, each section's words are looked for in the typed prompt.
  * It's a whole-word match that ignores case, as `guards.txt` reads words, so `bug` matches "Bug", "'bug'" and "bug's" but not "debug", and `test` doesn't match "tests" unless that's listed too. A hyphenated or apostrophe word in a section matches only as a whole: `[bug-fix]` needs "bug-fix", and `[what's]` isn't matched by "what".
  * It reads what was typed, not the rewrite, which adds words of its own (in the spike, "review" in 9 of 129 rewrites), and leaves out `Context:` lines, as `templates.min_words` does. Pastes and code blocks are left out too, or a pasted log would match every section, unless a paste is the whole prompt. `templates.min_words` also counts the typed text.
  * No model is involved, so it works with every `expand.on_enter` setting and adds no time.

* **FR-12.3: What gets added.** The text before the first section is added, then each section with a matching word, in the file's order:

  | Prompt | Added |
  |---|---|
  | Has a word from one section | The always-added text and that section |
  | Has words from several sections | The always-added text and those sections |
  | Has none of the words | The always-added text |
  | Short (`templates.min_words`) | Nothing, as today |

  What's added starts with the line `# Guidance`, always, so it reads apart from the prompt.

* **FR-12.4: Expand doesn't change.** The rewrite request, the guards, the diff and the styles stay exactly as they are. The rewrite's reply stays plain text.

* **FR-12.5: Visible.** The Session section shows the sections added to the last prompt, the "Ready to send" panel shows the rules that will really be added, and the history has a `sections` field.

---

## 3. Risks

* **A prompt without the words gets no section.** "Please have a look" gets no `[review]` unless `look` is listed. In the spike, keywords missed a section on 28% of the prompts that needed one. The always-added text is the more reliable place, and sections are for reminders that are fine to miss.
* **This is guidance, not enforcement.** Claude reads the text, but nothing checks that it's followed, and some prompts don't get it: short ones, slash commands, and Leader then Enter. `CLAUDE.md` is the place for an instruction that should always be there, and Claude Code's permissions or hooks for something that must never happen. The documentation says so.
* **A word that's only mentioned still matches.** "all tests passed, commit it" gets the `[test]` section. The developer can remove the word from the list.
* **An edited rewrite is matched as it stands.** Once the text in the box isn't the rewrite, it counts as typed, words the model added included. A second rewrite is the same: it starts from the first one.
* **The panel reads `append.txt`** at most once a second while open. That's only noticeable on a slow network folder.
* **Word lists take some tuning.** Words typed often need adding, and languages other than the one the list is written in won't match, so a developer who writes in two languages lists both.
* **A section's words sit in its header.** A long list makes a long line. That's accepted to keep one simple format.
* **A line that is only `[…]`, with no other brackets inside, now starts a section.** An `append.txt` with such a line changes behaviour, or is rejected if the line isn't words. `[NOTE]` or `[1]` can't be told from a section meant as one, so the rules after them stop going on every prompt. The Session line's section count is the way to spot it.

---

## 4. Spike: does it work?

Run on 2026-11-05 with 43 real prompts and `qwen2.5:7b`. Full results: [spikes/intent.md](spikes/intent.md).

The first proposal had the model name the intent in a JSON reply, with the rewrite. That changed the rewrites: 46 of 129 stayed nearly as typed against 19 today, and the intent leaked into the text. It also took about twice as long, and got no intent at all with `expand.on_enter: off`, the default.

| | Right | Wrong section | Missed |
|---|---|---|---|
| JSON with the rewrite | 72% | 18% | 10% |
| Separate request, bare names | 61% | 36% | 2% |
| Separate request, with hints | 70% | 23% | 7% |
| Keywords | 67% | **5%** | 28% |

A wrong section is the repeated text this appendix sets out to remove, while a missed one just means no extra rule. Keywords also leave the rewrite alone and add no time, so they were chosen. The keyword list was written after seeing the results, so its score is flattering.

---

## 5. Open decisions

* **Phrases.** Whether a section can list words with spaces, such as `have a look`.
* **Word forms.** Whether `test` should also match "tests" and "testing", or each form stays listed. Listing is simpler and predictable.
* **Prefix rules.** Whether the same sections should later apply to rules added at the start of a prompt.

---

## 6. Roadmap

FR-12.1 to FR-12.5 were built on 2026-11-06. What's left is the open decisions in section 5, and section 7, measured and set aside.

---

## 7. Set aside (2026-11-06): sections for confirmations (FR-12.6)

**Status:** Measured and not built. Kept so the idea and its numbers aren't lost.

**Background.** Claude often ends a reply with a question or a next step, and the answer is just "yes, go ahead". That prompt has no words to match, so it gets no section, even when the work has one.

**FR-12.6: Proposal.** When a prompt is a confirmation, the section comes from the end of Claude's last reply:
* **A confirmation** starts with "yes", "ok", "go ahead" or similar, has about 4 words at most, and has no section word of its own.
* **It reads the last paragraph,** usually where the question is, with the same section words. `transcript.LastTurn` already reads the last reply.
* **No model is involved.** Reading the file can happen in the background when the reply ends, so Enter doesn't wait, and the match itself is instant.
* **Only the matching section is added,** with its `# Guidance` heading, not the always-added text. This is an exception to `templates.min_words`, accepted by the developer, so "yes" stays clean unless a section matches.

**Measured** on the developer's history, with the example section words from FR-12.1:

| Typed prompts | Count |
|---|---|
| All | 526 |
| Start with a confirmation | 86 |
| Of those, no section word of their own | 62 |
| Of those, 4 words at most | 18 |

| Where the words were looked for (of the 62) | Prompts matched |
|---|---|
| Last sentence | 11 |
| Last paragraph | 18 |
| Whole reply | 53, of which 31 matched several sections |

Reading the whole reply matches almost everything, so only its end can work. Of the 18 short confirmations, read by hand against the last paragraph:

| Result | Prompts |
|---|---|
| No section matched | 14 |
| Matched, but wrong | 3 |
| Matched, and arguable | 1 |
| Matched, and clearly right | 0 |

**Why it's set aside.**
* **Claude's questions name the action or the item, not the kind of work:** "Should I start on this?" or "Want me to start with #123?". Those have no section word.
* **The matches it did make were mostly wrong.** All 3 were `PR` matching `[review]` in a question like "Shall I commit and push this to PR #123?", where the task was committing.
* **Doing better needs the earlier conversation,** not the last paragraph. That points back to a model reading the exchange, which is slower and which section 4 found less reliable at picking a section.

**Limits of the measurement:** 18 prompts, one developer, and only the example words. A developer's own sections may fit better, for example one for `commit`.

**If picked up:** repeat this on the developer's own sections first. Other CLIs (Phase 5) have no Claude transcript, so the reply would have to be read from the screen.

