# PRD Appendix 4: Guard Rails from a File

**Extends:** [PRD.md](../PRD.md) version 1.2.4 (section 4.4, configuration and custom rules) and [PRD Appendix 1](PRD-APPENDIX-1.md) (FR-7.4, the guard rails)  
**Appendix Version:** 1.2.0  
**Date:** 2026-11-03  
**Status:** Built on 2026-11-03 as a plain-text `templates/guards.txt` (section 6), in place of the `guards.yaml` first proposed (section 2). Agreed as the direction on 2026-10-30.

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

> **Superseded on 2026-11-03.** This YAML design was built, then replaced on review: the file was too hard to read and write. What was built instead is in section 6. The proposal is kept for the record.

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

* ~~**Where the file lives:** a separate `guards.yaml` (proposed, since the lists can grow long), or a `guards:` section in `config.yaml`.~~ A separate `guards.yaml`, beside `config.yaml`. *Superseded by the format decision below: `templates/guards.txt` (section 6).*
* ~~**Whether `pastes` and `names` may be turned off at all,** or only loosened.~~ Any guard may be turned off. Turning off `destructive`, `pastes` or `names` gets a warning.
* **The format (on review of PR #16):** the `guards.yaml` built from section 2 had 15 sections, two kinds of word list (word starts and whole words) and thresholds: "a lot of confuse and very hard to understand". Replaced by `templates/guards.txt`: one rule per line, in near-English, beside `append.txt` and `system.txt`. Its rules are also told to the model, not only checked. The built-in word lists and thresholds stay in code, tuned by tests.
* ~~**Whether to look at the `question` examples first.**~~ Built without waiting: the history and logs had no `question` stops to look at (the 11 logged stops were `earlier-prompts`, `names`, `dropped-part`, `destructive`, `asked-back` and `added-task`). The `question` lists can now be tuned when a bad stop shows up.

---

## 5. Roadmap

Built on 2026-11-03 (section 6). Possible later: more kinds of line, such as `also a negation: jamais`, when a real need shows up.

---

## 6. As built (2026-11-03): `templates/guards.txt`

**The file** is `guards.txt` in the templates folder (`templates.dir`, so `~/.config/prompt-engine/templates/guards.txt` by default). It has one rule per line. `#` starts a note, and a blank line is ignored.

```
never add: deploy, rollback, production
never add: force push
always keep: staging, main
turn off: question
```

| Line | Meaning | Replaces (section 2) |
|---|---|---|
| `never add: …` | The rewrite must not add these words or phrases if the typed prompt has none of them | FR-10.4 "added words" |
| `always keep: …` | If the typed prompt has one of these, the rewrite must keep it | FR-10.4 "kept words" |
| `turn off: …` | Turn off built-in guards, by name | FR-10.3 |

**How the rules are used**
* **Told to the model:** `never add` and `always keep` are added to the rewrite instructions of each request, `system.txt` included. Only the words that matter for the prompt are named: "Never add these words: …" (those not typed) and "Keep these words exactly: …" (those typed). Naming a typed word as one never to add would invite the model to drop it. The rules are read once per rewrite, so the model and the check use the same file even if it's saved meanwhile.
* **Checked afterwards:** right after `destructive`, as guards named `never-add` and `always-keep`. The reason names the line ("the rewrite added "production" (guards.txt line 3: never add)").
  * In `auto` and `preview` mode, with every built-in guard.
  * With the shortcut (Ctrl+Q, e), with `names` and `quotes` only, as before, since the developer sees the rewrite before sending it. `turn off:` applies there too.
* **Matching:**
  * whole words, any case, with quotes, apostrophes and a final "'s" ignored;
  * hyphenated words split into their parts ("production-like" has "production");
  * forms: -s, -es, -ed, -d and -ing, a doubled last letter ("committed"), and the irregular verb forms;
  * a phrase ("force push") matches its words in a row, with forms on the last word only;
  * a short rule catches more ("stag" is also "staging").

  Words are letters, digits, `-`, `_` and `'`, so a file name like `main.go` isn't a valid word; the `names` guard already keeps names. Words only inside a paste don't count, since the rules see the text with pastes masked.
* **Limits:**
  * at most 50 words and phrases in `never add` and `always keep` together, since the model may be told each one;
  * `turn off: empty` is refused, as an empty rewrite would replace the prompt;
  * the file follows `templates.dir`, which needs a restart.
* **`turn off: pastes`:** a pasted part the rewrite moved is put back where it now is. One dropped or repeated still stops the rewrite.

**Behaviour (FR-10.5 to FR-10.7, as proposed)**
* **Loading:** read at startup, then polled every second.
  * A line that isn't a rule, a word that isn't one, or an unknown guard rejects the file, naming the line: `…/guards.txt line 2: no guard "questoin" (the guards: …)`.
  * While running, the previous rules are kept. At startup, there's a warning and the built-in guards alone.
  * No file means the built-in guards alone.
* **Session section:** `guards: …/guards.txt: never add 4; always keep 2; off: question` (or `built-in only (no …)`), and a `⚠` line for each of `destructive`, `pastes` and `names` turned off. A rejected edit adds `last guards.txt edit rejected: …`. The log has the same lines.
* **`--pe-check-guards "<typed>" "<rewrite>"`:** says whether the rewrite passes, or which guard stops it and why, as in `auto` mode. It uses no session context and no pastes. It exits 0 if the rewrite passes, 1 if a guard stops it, and 2 (with the error on stderr) if `guards.txt` is invalid.

**Dropped from section 2:** editing the built-in word lists (FR-10.1), the thresholds (FR-10.2), `points_back`, and the word-start syntax. These were internal tuning, and confusing to read.

**Template:** `docs/guards.example.txt` explains the three rules, lists every built-in guard with what it stops, and has commented examples. A test checks that it has no active rule, that it lists every guard, and that its examples parse. Release archives include it.

**Tests:**
* `internal/guards`: parsing, errors by line, the example file and the notes;
* `internal/expand/rules_test.go`: matching, the checks, a guard turned off, the instructions, `RestoreMoved`;
* `TestExpandWithGuardRules`: the instructions reach the model, with and without `system.txt`;
* the check command, and `review` with `pastes` off;
* the e2e `guards` case: a rule stops an auto rewrite, then an edit while running replaces it, and deleting the file leaves the built-in guards;
* review fixes (2026-11-03): spellings (quotes, apostrophes, hyphens, doubled letters), `Instructions(typed)`, the shortcut check (`CheckShortcut`), `empty` always on, the word cap, and the panel lines;
* testing guide, Part Q.
