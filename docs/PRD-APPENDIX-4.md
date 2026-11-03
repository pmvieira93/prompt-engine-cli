# PRD Appendix 4: Guard Rails from a File

**Extends:** [PRD.md](../PRD.md) version 1.2.4 (section 4.4, configuration and custom rules) and [PRD Appendix 1](PRD-APPENDIX-1.md) (FR-7.4, the guard rails)  
**Appendix Version:** 1.1.0  
**Date:** 2026-11-03  
**Status:** Built on 2026-11-03 (FR-10.1 to FR-10.7; see section 6). Agreed as the direction on 2026-10-30.

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

Decided on 2026-11-03:

* ~~**Where the file lives:** a separate `guards.yaml` (proposed, since the lists can grow long), or a `guards:` section in `config.yaml`.~~ A separate `guards.yaml`, beside `config.yaml`.
* ~~**Whether `pastes` and `names` may be turned off at all,** or only loosened.~~ Any guard may be turned off. Turning off `destructive`, `pastes` or `names` gets a warning.
* ~~**Whether to look at the `question` examples first.**~~ Built without waiting: the history and logs had no `question` stops to look at (the 11 logged stops were `earlier-prompts`, `names`, `dropped-part`, `destructive`, `asked-back` and `added-task`). The `question` lists can now be tuned when a bad stop shows up.

---

## 5. Roadmap

FR-10.1 to FR-10.7 built together on 2026-11-03 (section 6).

---

## 6. As built (2026-11-03)

As proposed, with these details:

* **Where:** `guards.yaml` in the folder of the config file in use, so it follows `PROMPT_ENGINE_CONFIG` and `--pe-config`. It is polled every second, as `config.yaml` is.
* **The keys** are the guards' names (`asked-back`, `added-task`, …). Each has `enabled`. The guards with word lists also take `add` and `remove`:
  * `destructive`, `question` (action verbs) and `added-task` (task verbs) are lists of word starts, as in the code: `delet` is "delete", "deleted", "deleting";
  * `question.question_words`, `negation`, `points_back` and `points_at_reply` are whole words.
* **`added-task` has its own list.** In code it was the action verbs plus the investigation verbs, so adding to `question` doesn't change it.
* **Thresholds:** `length.factor` (1–100, built in 3), `length.allowance` (0–1000, built in 15) and `replaced.max_share` (above 0, at most 1; built in 0.6).
* **Words** are lower-cased and must be one word. A word both added and removed is an error. A removed word that isn't in the list is ignored, so a later release that drops it doesn't break the file.
* **Custom guards** are checked right after `destructive`. Their words are word starts. A name is lower case letters, digits and hyphens, unique, and not a built-in guard's. The reason reads "the rewrite added production" or "the rewrite dropped staging".
* **`pastes` off:** a pasted part the rewrite moved is put back where it now is. One dropped or repeated still stops the rewrite, since it can't be put back.
* **Rejected files:** at startup, a warning, and the built-in guards; while running, the previous guards are kept. Either way the Session section says why.
* **Session section:** a `guards:` line (the file, or "built-in", and the custom guards), a `weakened:` line (guards turned off, words removed from a list, thresholds loosened; adding words or tightening isn't listed), and one `⚠` line per risky guard turned off. The log has the same.
* **`--pe-check-guards`** uses no session context, so `earlier-prompts` can't stop the rewrite, and there are no pastes. It exits 0 if the rewrite passes, 1 if a guard stops it, and 2 if `guards.yaml` is invalid.
* **`docs/guards.example.yaml`** has a section for every guard. Its `# built-in` lines list the built-in word lists. A test checks that the file parses to the built-in rules and that those lines match. Release archives include it.
* **Tests:** `internal/guards` (the file, the example, the notes), `internal/expand/rules_test.go` (each change reaches the checks), the check command, and the e2e `guards` case (a custom guard stops an auto rewrite, then an edit turns it off while running). Testing guide, Part Q.
