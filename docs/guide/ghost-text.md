# Ghost text

[README](../../README.md) · [Getting Started](../GETTING_STARTED.md) · [Configuration reference](../reference.md)

Pause while typing, and a suggestion appears after the cursor in grey italics.

![Typing "list the files in this dire", pausing, and accepting "ctory" with Tab](../media/ghost.gif)

- **Tab** or **→** accepts it, and **Esc** dismisses it. Typing on also dismisses it.
- It finishes half-typed words (`refa` → `refactor`) and continues sentences. This uses a fill-in-the-middle model.
- It only shows where it can't get in the way. It stays out of Claude Code's dialogs, its `@` file menu and `/` command menu, and doesn't show when the cursor isn't at the end of the prompt.
- Suggestions follow the session: the model sees your last 3 prompts, so after `say hello in three words in portuguese`, typing `now say goodbye in thr` gets `ee words in portuguese`.
- Suggestions that would only repeat what you already typed are dropped.
- Typing never waits for the model. A suggestion is requested after a short pause (150 ms), and one that takes longer than 2 s is dropped.
- The style is yours to pick: `italic` (default), `faint`, or any colour from the 256-colour palette.

To change how it looks or what the model sees, see `ghost.*` and `llm.*` in the [configuration reference](../reference.md#configuration). Which model to use: [Local models](../models.md).
