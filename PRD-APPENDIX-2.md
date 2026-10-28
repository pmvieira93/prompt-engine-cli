# PRD Appendix 2: Rewrite Styles

**Extends:** [PRD.md](PRD.md) version 1.2.0 (section 4.3, prompt enhancement) and [PRD Appendix 1](PRD-APPENDIX-1.md) (FR-7)  
**Appendix Version:** 1.0.0  
**Date:** 2026-10-28  
**Status:** Proposed, planned after `preview` mode (FR-7.6)

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

### 2.4 Risks

* **Structure invites invention.** In Phase 4, asking small models for more structure made them add requirements. The `llm` style must keep the "never add goals, components, files or steps" rule, and is evaluated before shipping (2.5).
* **Guard rails.** Structured rewrites are longer, so the length guard (more than about 3× + 15 words) may stop `llm` rewrites. Measure first; don't loosen the guard in advance.
* **Latency.** Longer output takes longer, which matters in `auto` mode. Measure per style.

### 2.5 Acceptance

As for the expansion model and session context: a fixed set of about 10 prompts (single requests, several parts, negations, names, a question, a non-English prompt) rewritten in each style with `qwen2.5:7b`. For each, record whether the meaning was kept, whether it passed the guard rails, its length and its time. A style ships only if it keeps meaning as well as `default`.

---

## 3. Roadmap

After `preview` mode (Appendix 1, FR-7.6), since preview shows the rewrite before it's sent, the natural place to compare styles:

1. The evaluation (2.5), to settle each style's instructions.
2. `expand.style` (FR-8.1 to FR-8.5).
3. A manual test part in `docs/TESTING.md`.

Later: per-prompt style (FR-8.6).
