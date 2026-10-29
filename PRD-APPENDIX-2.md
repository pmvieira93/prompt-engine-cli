# PRD Appendix 2: Rewrite Styles

**Extends:** [PRD.md](PRD.md) version 1.2.0 (section 4.3, prompt enhancement) and [PRD Appendix 1](PRD-APPENDIX-1.md) (FR-7)  
**Appendix Version:** 1.2.0  
**Date:** 2026-10-29  
**Status:** Built (FR-8.1 to FR-8.5); FR-8.6 is a later candidate

---

## 1. Background

Expansion (Phase 4, and automatically on Enter since Phase 4.5) rewrites a rough prompt with a local model (`llm.expand_model`, default `qwen2.5:7b`). The instructions are fixed: keep the intent and the names, fix typos, and use a short bullet list for requests with several parts. The result reads well to both a person and Claude Code, but its wording is never tuned for either.

The rewrite has two readers:

* **Claude Code**, which acts on it;
* **the developer**, who reviews it before sending (Ctrl+Q, then e; and `preview` mode, FR-7.6), and reads it later in the diff panel and the history.

This appendix records an idea raised after Phase 4.5: let the developer choose which reader the wording is tuned for.

---

## 2. Proposal: a rewrite style (FR-8)

### 2.1 Styles

| `expand.style` | Wording | Example for `fix teh login bug, dont touch the api` |
|---|---|---|
| `default` | Today's instructions: the model decides | `Fix the login bug without changing the API.` |
| `llm` | Tuned for the model: structured and explicit, goal first, then constraints; names exact; no filler | `Fix the login bug.`<br>`Constraint: don't change the API.` |
| `human` | Tuned for people: natural, short sentences, as written to a colleague | `Fix the login bug, but don't touch the API.` |
| `both` | Natural sentences, structured only where there are several parts | `Fix the login bug without changing the API.` |

*`auto` was the first name proposed for `default`; it was changed because `expand.on_enter` already has an `auto` value.*

### 2.2 Principles of communication

Every style follows five principles. A 7B model can't act on the principle names themselves, so each style turns them into explicit instructions:

| Principle | `llm` | `human` |
|---|---|---|
| **Speech** (voice) | Imperative, addressed to the assistant | First person, conversational |
| **Way of expression** (tone) | Neutral and direct, no politeness | Natural; polite if the developer was |
| **Way of explaining** (order) | Goal, then details, then constraints | The developer's own order |
| **Words** | Precise technical terms; names exact; no synonyms for names | Everyday words; names exact |
| **Articulation** (form) | Bullets for separate parts, one idea per line | Sentences; bullets only for real lists |

`both` takes the `human` voice, tone and words, with the `llm` order and form when a request has several parts.

### 2.3 Requirements

* **FR-8.1:** `expand.style: default | llm | human | both` in config, applied on reload (FR-4.3) and shown in the panel's Session section. Default: `default`.
* **FR-8.2:** Each style is an instruction set built from 2.2. Rules shared by all styles are kept in every one: keep the intent, never add goals or steps, keep names, keep negations, keep the language (Appendix 1, FR-7.4b and FR-7.4c).
* **FR-8.3:** The style applies only to rewrites. Typo-only rewrites (around a paste, or over 60 words; Appendix 1, FR-7.4e) are unchanged.
* **FR-8.4:** A developer's own `templates/system.txt` still replaces the instructions, whatever the style.
* **FR-8.5:** The history (FR-5.4) records the style used for each expanded prompt.
* **FR-8.6 (later, optional):** Choose the style for a single prompt with a leader key.

As built:

* The instructions are those settled in 2.6 (`internal/expand/styles.go`).
* The history's `style` field says which instructions a rewrite used: the style, `typos` for a typo-only rewrite (FR-8.3), or `custom` for `templates/system.txt` (FR-8.4). The log's expansion lines end with `style …` too.
* In `preview` mode, a suggestion prepared under another style isn't offered after a reload: the panel hides it, and the expand shortcut rewrites the prompt again in the new style.

### 2.4 Risks

* **Structure invites invention.** In Phase 4, asking small models for more structure made them add requirements. The `llm` style must keep the "never add goals, components, files or steps" rule, and is evaluated before shipping (2.5).
* **Guard rails.** Structured rewrites are longer, so the length guard (more than about 3× + 15 words) may stop `llm` rewrites. Measure first; don't loosen the guard in advance.
* **Latency.** Longer output takes longer, which matters in `auto` mode. Measure per style.

### 2.5 Acceptance

As for the expansion model and session context: a fixed set of about 10 prompts (single requests, several parts, negations, names, a question, a non-English prompt) rewritten in each style with `qwen2.5:7b`. For each, record whether the meaning was kept, whether it passed the guard rails, its length and its time. A style ships only if it keeps meaning as well as `default`.

### 2.6 Evaluation results (2026-10-28)

11 prompts, each rewritten twice per style with `qwen2.5:7b` (temperature 0.2), then checked by the guard rails (Appendix 1, FR-7.4). The prompts were a single request, a refactor, a request in three parts, an agent and MR name, a question, a file and setting with "only… not…", Portuguese, a polite request with a guess, a request in four parts ending "dont commit", a vague report ("it still fails, same as before") and a writing task. "Meaning kept" was judged by reading each rewrite.

| Style | Round 1: passed guards | Final: passed guards | Final: meaning kept | Words (avg) | Time (avg) |
|---|---|---|---|---|---|
| `default` | 20/22 | 20/22 | 22/22 | 13.9 | 2.4 s |
| `llm` | 13/22 | 22/22 | 22/22 | 11.0 | 2.2 s |
| `human` | 10/22 | 20/22 | 22/22 | 12.0 | 2.3 s |
| `both` | 18/22 | 21/22 | 21/22 | 12.7 | 2.3 s |

What the first instructions got wrong, and the fix:

* **`llm`: "Put what must not be done on a line starting `Constraint:`"** made the model invent constraints: "Do not repeat previous steps", "Do not include any other information". This is the risk in 2.4. Fix: constraints only if the developer stated them; no fixed label.
* **`llm`: "Precise technical terms"** turned "using code-reviewer look into MR !110" into "Review the code in merge request !110", losing the agent. Fix: "Use the developer's terms; 'using X' means 'Use X'".
* **`llm`: imperative everywhere** turned the report "it still fails, same as before" into "Ensure the issue persists as before and provide the same failure details". It passed the guard rails, so the instructions must prevent it. Fix: "Only a request becomes an instruction; a report or a question stays as it is".
* **`human`: "first person, conversational"** gave "Can you …?" requests and added "Any ideas?". The guard rails rejected 12 of 22 as the model asking a question. Fix, shared by all styles: a request is written as a statement, with no added question, greeting or closing line.

Findings:

* **For short prompts the styles differ little.** All four gave the same rewrite for the single requests, the refactor, the Portuguese prompt and the writing task. The differences show in tone and length. `llm` drops politeness and hedging ("Check why `make test` is slow on CI, suspect it's the race detector."). `human` keeps them ("Can you please check… I think it might be the race detector."). `both` falls in between.
* **Only `default` used bullets** (the three- and four-part requests). The new styles kept those requests as one sentence, even when told to use bullets.
* **Length and time are no concern.** All styles are as short as `default` or shorter, and as fast. The length guard didn't stop any rewrite.
* **Remaining slips:** `llm` turns a "why…" question into "Explain why…", which is harmless. `both` once turned a question into "Can you investigate why?" and once added "Only include the shortcuts mentioned", which the guard rails stopped.
* **Not about styles:** the "only… not…" negation check stopped correct rewrites ("only change the default not the validation" → "don't change the validation") in `default` and `human`. Every style also turned the European Portuguese "ficheiro" into the Brazilian "arquivo".

Verdict: `llm` and `human` meet 2.5: they keep meaning as well as `default`. `both` is close, with one slip in 22 that the guard rails didn't stop. The settled instructions are below.

Rules shared by `llm`, `human` and `both` (FR-8.2):

```
- Keep the developer's intent exactly. Never add goals, components, files, or steps they didn't mention.
- Keep names exactly as written: agents, skills, commands, @mentions, files, paths, branches, and issue, MR or PR numbers. A word with a hyphen, slash, dot, @, #, ! or digits is a name: never split, translate or drop it.
- Keep every limit and negation ("don't", "only", "without", "never").
- If the request is vague, keep it vague: do not guess what "it" refers to.
- If the developer asks a question, keep it a question.
- Keep the developer's language.
- Fix typos and spell out abbreviations.
- A request stays a request: write it as a statement, not as a "Can you…?" question. Never add a question, a greeting or a closing line.
```

Per style:

```
llm:   Imperative, addressed to the assistant. Neutral and direct: no greetings, politeness or filler.
       Goal first, then details, then what must not be done, if the developer said so. Never add a constraint they didn't state.
       Use the developer's terms; "using X" means "Use X".
       Only a request becomes an instruction. A report ("it still fails") or a question stays as it is.
       For several parts, one bullet per part they mentioned.
human: Natural, as written to a colleague, in the developer's voice. Polite only if the developer was.
       Keep the developer's order of ideas.
       Everyday words, short sentences.
       Sentences, not bullets, unless the developer listed several items.
both:  Written as the developer speaking: natural, short sentences, everyday words. Polite only if the developer was.
       A single request stays one or two sentences.
       For several parts: the goal first, then one bullet per part they mentioned, then what must not be done.
```

Each ends with "Output only the rewritten prompt, under 60 words."

---

## 3. Roadmap

After `preview` mode (Appendix 1, FR-7.6), since preview shows the rewrite before it's sent, the natural place to compare styles:

1. ~~The evaluation (2.5), to settle each style's instructions.~~ Done (2.6).
2. ~~`expand.style` (FR-8.1 to FR-8.5).~~ Done.
3. ~~A manual test part in `docs/TESTING.md`.~~ Done (Part N).

Later: per-prompt style (FR-8.6).
