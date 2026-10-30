# PRD Appendix 4: Guard Rails from a File

**Extends:** [PRD.md](../PRD.md) version 1.2.4 (section 4.4, configuration and custom rules) and [PRD Appendix 1](PRD-APPENDIX-1.md) (FR-7.4, the guard rails)  
**Appendix Version:** 1.0.0  
**Date:** 2026-10-30  
**Status:** Proposed. Agreed as the direction on 2026-10-30; not scheduled.

---

## 1. Background

A rewrite is used only if it passes the guard rails ([Appendix 1](PRD-APPENDIX-1.md), FR-7.4): 15 checks in `internal/expand/guard.go` that compare what was typed with the rewrite. Each was added after a bad rewrite seen in testing.

All of it is code, so changing a guard needs a new release, even when the change is only a word:

* **The word lists are English and some Portuguese.** That covers destructive actions, task and action verbs, negations, question openers, and words that point back ("that", "the second one"). A developer writing in another language, or using team words ("deploy", "rollback"), can't extend them.
* **The thresholds are fixed:** `length` stops a rewrite of more than 3 × the typed words + 15, and `replaced` one where more than 60% of the typed words are gone.
* **A guard can't be turned off.** In real use, the `question` guard stopped rewrites often enough to be noticed (2026-10-30, examples still to be collected). Today the only way to loosen it is a code change.
* **There is no way to add a guard of one's own,** such as "never let a rewrite add *production*".

---

## 2. Proposal (FR-10)

A guard rails file, `~/.config/prompt-engine/guards.yaml` (beside `config.yaml`), that **extends** the built-in guards rather than replacing them. Changes to the built-in defaults in later releases then still apply.

**What the file holds**

* **FR-10.1: Word lists.** Add words to a guard's list, or remove them, for:
  * `destructive`;
  * `question` (action verbs, and the words that make a prompt a question);
  * `added-task` (task verbs);
  * `negation`;
  * the words that point back (Appendix 1, 2.5).
* **FR-10.2: Thresholds.** `length` (the factor and the allowance) and `replaced` (the share of typed words that may go).
* **FR-10.3: On or off.** Turn off any built-in guard by name.
* **FR-10.4: Custom guards.** Named rules of two kinds:
  * **added words:** stop a rewrite that has one of these words (or word starts) when the typed prompt has none of them;
  * **kept words:** stop a rewrite that drops one of these words when the typed prompt has it.

  A custom guard's name shows in the panel and the log like a built-in one.

**What stays in code.** `names`, `quotes`, `pastes`, `dropped-part`, `language` and `earlier-prompts` are small algorithms: they parse names and paths, split sentences, follow pasted parts, detect the language, and resolve references. The file only adjusts their word lists and thresholds, where they have them. Writing their logic as YAML rules would be harder to read and test than the Go it replaced.

**Example**

```yaml
# ~/.config/prompt-engine/guards.yaml
destructive:
  add: [deploy, rollback, migrate]
negation:
  add: [nunca, jamais]
question:
  enabled: false        # turned off: shown in the panel's Session section
replaced:
  max_share: 0.7        # built in: 0.6
custom:
  - name: prod-words
    added: [production, prod]
  - name: keep-staging
    kept: [staging]
```

**Behaviour**

* **FR-10.5: Loading.** Read at startup and again whenever the file changes, as `config.yaml` is.
  * Unknown keys, unknown guard names and invalid values are rejected with a clear warning, and the previous rules are kept.
  * No file means the built-in guards, as today.
* **FR-10.6: Visible weakening.** The panel's Session section lists the guards that are turned off or loosened. Turning off `destructive` or `pastes` gets a warning, since in `auto` mode a rewrite then goes out unchecked by it.
* **FR-10.7: Trying a rule.** `prompt-engine --pe-check-guards "<typed>" "<rewrite>"` prints which guard would stop the rewrite, and why, or that it passes. It needs no model and no session, so a new rule can be tried before relying on it.

---

## 3. Risks and mitigations

* **Weakening the guards by accident.** Loosening is easy, and in `auto` mode its effect is silent. Mitigations:
  * the Session section lists what's turned off or loosened (FR-10.6);
  * the history's `guard` field shows what is still being stopped.
* **Untested rules.** Each built-in guard has tests built from real bad rewrites; a custom one has none. `--pe-check-guards` (FR-10.7) lets the developer test one by hand.
* **Scope.** This needs a file format, validation, live reload, the Session section, the check command, documentation and tests. The documentation is the README and the Getting Started guide, plus a commented `guards.example.yaml` that is kept in step with the defaults by a test, as `config.example.yaml` is.

---

## 4. Open decisions

* **Where the file lives:** a separate `guards.yaml` (proposed, since the lists can grow long), or a `guards:` section in `config.yaml`.
* **Whether `pastes` and `names` may be turned off at all,** or only loosened.
* **Whether to look at the `question` examples first.** If loosening that guard turns out to be a word-list change, it's a natural first case for the file.

---

## 5. Roadmap

Not scheduled. When picked up: FR-10.1 to FR-10.5 first, with the example file and its test. Then FR-10.6 and FR-10.7.
