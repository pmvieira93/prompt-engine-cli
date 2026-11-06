# PRD Appendix 6: Rules Chosen by Intent

**Extends:** [PRD.md](../PRD.md) version 1.3.5 (FR-4.2, static rules from `templates/append.txt`) and [PRD Appendix 1](PRD-APPENDIX-1.md) (FR-7, the rewrite on Enter)  
**Appendix Version:** 2.1.0  
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

  A file with no sections works exactly as it does today. A word used twice, one that isn't a word, or a section with no text rejects the file, naming the line, as `guards.txt` does. A rejected file adds no rules until it's fixed: the warning shows at startup and in the Session section, and the log names it on each prompt.

* **FR-12.2: Matching.** On Enter, each section's words are looked for in the typed prompt.
  * It's a whole-word match that ignores case, so `bug` matches "Bug" but not "debug", and `test` doesn't match "tests" unless that's listed too.
  * It reads what was typed, not the rewrite, which adds words of its own (in the spike, "review" in 9 of 129 rewrites), and leaves out `Context:` lines, as `templates.min_words` does.
  * No model is involved, so it works with every `expand.on_enter` setting and adds no time.

* **FR-12.3: What gets added.** The text before the first section is added, then each section with a matching word, in the file's order:

  | Prompt | Added |
  |---|---|
  | Has a word from one section | The always-added text and that section |
  | Has words from several sections | The always-added text and those sections |
  | Has none of the words | The always-added text |
  | Short (`templates.min_words`) | Nothing, as today |

* **FR-12.4: Expand doesn't change.** The rewrite request, the guards, the diff and the styles stay exactly as they are. The rewrite's reply stays plain text.

* **FR-12.5: Visible.** The Session section shows the sections added to the last prompt, the "Ready to send" panel shows the rules that will really be added, and the history has a `sections` field.

---

## 3. Risks

* **A prompt without the words gets no section.** "Please have a look" gets no `[review]` unless `look` is listed. In the spike, keywords missed a section on 28% of the prompts that needed one. A policy that must hold belongs in the always-added text, and sections are for reminders that are fine to miss. The documentation and the template say so.
* **A word that's only mentioned still matches.** "all tests passed, commit it" gets the `[test]` section. The developer can remove the word from the list.
* **Word lists take some tuning.** Words typed often need adding, and languages other than the one the list is written in won't match, so a developer who writes in two languages lists both.
* **A section's words sit in its header.** A long list makes a long line. That's accepted to keep one simple format.
* **A line that is only `[…]` now starts a section.** An `append.txt` with such a line changes behaviour, or is rejected if the line isn't words.

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

FR-12.1 to FR-12.5 were built on 2026-11-06. What's left is the open decisions in section 5.
