# Your own guard rails

[README](../../README.md) · [Getting Started](../GETTING_STARTED.md) · [Configuration reference](../reference.md)

Every rewrite is checked by built-in guard rails ([what they catch](rewrites.md#what-every-rewrite-keeps)). This page is about adding your own.

Add your own rules in **`~/.config/prompt-engine/templates/guards.txt`**, beside `append.txt` and `system.txt`. Write one rule per line; lines starting with `#` are notes:

```
never add: deploy, rollback, production
never add: force push
always keep: staging, main
turn off: question
```

| Line | Meaning |
|---|---|
| `never add: …` | The rewrite must not add these words or phrases, if you didn't type them |
| `always keep: …` | If you typed one of these, the rewrite must keep it |
| `turn off: …` | Turn off built-in guards, by name |

- **Each rule is used twice.**
  - **Told to the model,** so it keeps the rules to begin with. Only the words that matter for the prompt are named: the `never add` words you didn't type, and the `always keep` words you did.
  - **Checked after the rewrite,** in case the model ignores them. That holds in every mode, Ctrl+Q, e included.
- **Words match whole,** in any case, with quotes, apostrophes and a final "'s" ignored. They match in their forms: `deploy` also catches "deploys", "deployed" and "deploying", and `commit` catches "committed". A hyphenated word counts as its parts ("production-like" has "production").
- **Phrases:** `force push` matches its words in a row, with forms on the last word ("force pushed").
- **Short words catch more:** `stag` also catches "staging". The rules ignore words that only appear inside a paste.
- **The built-in guards stay on.** Your rules add to them. `turn off:` takes the guard names the panel and the log show; the template lists them all. `empty` can't be turned off.
- **Mistakes are caught.** A line that isn't a rule, an unknown guard, or more than 50 words in all is named by its line number. The previous rules are kept, and edits apply as soon as you save. The file follows `templates.dir`, which needs a restart.
- **Weakening shows.** The panel's Session section lists your rules and the guards turned off. Turning off `destructive`, `pastes` or `names` also gets a warning, since in `auto` mode a rewrite it would stop is then sent unchecked.
- **Switch your rules off** without deleting the file: `expand.guards: off` in `config.yaml`. The built-in guards stay on, and the panel says the file is ignored.
- **Try a rule first:** `prompt-engine --pe-check-guards "<what you typed>" "<a rewrite>"` says which guard stops the rewrite, or that it passes. It needs no model, and checks as `auto` and `preview` do. Ctrl+Q, e checks less, since you see the rewrite before sending it: only names, quotes and your rules.

**Template:** [`docs/guards.example.txt`](../guards.example.txt) explains each rule and lists every built-in guard.
