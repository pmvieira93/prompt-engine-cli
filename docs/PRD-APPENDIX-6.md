# PRD Appendix 6: Rules Chosen by Intent

**Extends:** [PRD.md](../PRD.md) version 1.3.5 (FR-4.2, static rules from `templates/append.txt`) and [PRD Appendix 1](PRD-APPENDIX-1.md) (FR-7, the rewrite on Enter)  
**Appendix Version:** 1.0.0  
**Date:** 2026-11-04  
**Status:** Proposed, spike first. Raised on 2026-11-04; nothing is built until the spike (section 4) shows the model picks the intent reliably.

---

## 1. Background

`templates/append.txt` is added to the end of every prompt sent (FR-4.2). That fits a rule that always applies, but not one that only matters for some work: "Run the tests before saying it works" means nothing on a request to explain a function. Seeing the same text on every prompt gets boring, and it costs tokens each time.

The rewrite already asks the local model about each prompt, so it can also say what kind of work the prompt is. That answer can decide which rules are added.

**Today, the model returns plain text:** the rewrite, in `message.content`. The diff doesn't come from the model: the tool builds it by comparing the typed text with the rewrite. So the reply can change shape without touching the diff, the guards or the styles, as long as the same rewrite comes out of it.

---

## 2. Proposal (FR-12)

* **FR-12.1: Sections in `append.txt`.** A line `[name]` starts a section, which runs to the next one. Several names for one section are separated by `|`. Text before the first section is added to every prompt that isn't short (`templates.min_words`), as today.

  ```
  Keep changes small.

  [review]
  Check the error paths too.

  [test]
  Test behaviour, not implementation.

  [investigate|analysis]
  Don't change any code yet: report what you find.

  [debug]
  Find the cause before proposing a fix.
  ```

  A file with no sections works exactly as it does today. A name used twice, or one that isn't a word, rejects the file, naming the line, as `guards.txt` does.

* **FR-12.2: The model's reply.** When `append.txt` has sections, the rewrite asks for JSON, using Ollama's structured output so the reply must have this shape:

  ```json
  { "response": "Review the login handler.", "intent": "review" }
  ```

  * `response` is the rewrite. It goes through the guards, the diff and the styles as today.
  * `intent` is one of the section names or `none`. It's a fixed list, as free text ("bugfix", "fixing") wouldn't match a section, and `none` is there so the model isn't pushed to pick one.
  * The model judges what was typed, not the rewrite, which may have drifted.
  * The typed prompt and a status aren't asked for. The tool already has the prompt, and the guards judge the rewrite: a model can't be trusted to judge its own work.
  * With no sections, the request stays as it is today: plain text and no schema.

* **FR-12.3: What gets added.** On Enter, the text before the first section is added, then the section matching the intent:

  | Intent | Added |
  |---|---|
  | Matches a section | The always-added text and that section |
  | `none` | The always-added text |
  | A name with no section | The always-added text |
  | No intent (no rewrite ran, see below) | The always-added text |

* **FR-12.4: Expand keeps working.** If the reply isn't valid JSON, the whole reply is treated as the rewrite, with no intent, so a model that ignores the schema changes nothing. A reply cut off in the middle of the JSON can't be read, so the prompt is kept as typed, like a rewrite a guard stopped.

* **FR-12.5: Visible.** The Session section shows the last intent and the section added, the `preview` panel shows the rules that will really be added, and the history has an `intent` field.

---

## 3. Risks

* **No rewrite means no intent.** `expand.on_enter` is `off` by default, so most prompts never reach the model. Short prompts (`expand.min_words`), timeouts and Ollama being down have the same effect. Only the always-added text goes out then. Enter isn't delayed to ask the model, so sections work with `auto`, `preview` and the shortcut.
* **A wrong intent drops a rule silently.** Today `append.txt` is guaranteed. A policy that must hold belongs in the always-added text, and sections are for reminders that are fine to miss. The documentation and the template say so.
* **Intents that overlap.** A small model may not tell `investigate`, `analysis` and `debug` apart, or may see `review` and `test` in one prompt. Only one intent is returned, so the other section is left out.
* **The shortcut happens before Enter.** The intent from Ctrl+Q, e must be kept until the prompt is sent, and dropped if the prompt is edited afterwards, since it may no longer fit.
* **Rewrite quality.** Asking for JSON may change how the model rewrites, and the instructions were tuned on plain text. The spike measures this.
* **Long prompts.** JSON takes some of the tokens allowed for the reply, and escaping adds more. Typo mode on a long prompt is the most likely to be cut off.

---

## 4. Spike: does it work?

Before any code, with a small script against `qwen2.5:7b`:

1. **Prompts.** About 30 real prompts from the history, each labelled by hand with its intent, using the sections in FR-12.1 plus some that should be `none`.
2. **Intent.** How often the model picks the right one, per intent. How often it picks one when it should be `none`, and how often `investigate`, `analysis` and `debug` are confused.
3. **Rewrites.** The same prompts with plain text and with JSON. Compare the guard stops and read the differences.
4. **Time and tokens.** The added time per rewrite, and how often a long prompt's reply is cut off.
5. **Outcome.** Build, change or drop. If the intent is unreliable, a keyword match needs no model and also works with `expand.on_enter: off`, for example `[test: test, tests, coverage]`.

---

## 5. Open decisions

* **One intent or several.** A list would cover "review and test this", but gives the model more room to add wrong ones.
* **Keywords as well as the model.** Whether a keyword match should fill in when no rewrite ran, so sections also work with `on_enter: off`.
* **A guard-stopped rewrite.** Whether its intent is still used. It describes the typed prompt, but it came from a reply that was thrown away.
* **Prefix rules.** Whether the same sections should later apply to rules added at the start of a prompt.

---

## 6. Roadmap

Not scheduled. When picked up: the spike (section 4), then FR-12.1 and FR-12.3, then FR-12.2, FR-12.4 and FR-12.5, if the spike supports them.
