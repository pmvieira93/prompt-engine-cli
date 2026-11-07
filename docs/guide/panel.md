# Side panel and history

[README](../../README.md) · [Getting Started](../GETTING_STARTED.md) · [Configuration reference](../reference.md)

## Side panel

**Ctrl+Q, then p** opens a panel next to Claude Code. It shows:

- **Prompt diff:** what you typed against what was sent, with every change highlighted.
- **Session:** the current mode, style and models, any setting that needs a restart, and your `guards.txt` rules and the guards it turns off.
- **Guard details:** with `panel.guard_details`, which guard stopped a rewrite and the start of what it stopped.

The panel goes on the right or the left. **Ctrl+Q, then + or −** resizes it.

## History

Each prompt you send adds one JSON line to `~/.local/state/prompt-engine/history.jsonl`. The line records whether the prompt was rewritten, in which style, whether rules were added and from which sections, and which guard stopped a rewrite. The text itself is recorded only with `history.include_text: true`. Each line also names the session (`"session"`, a short id that the debug log names too), so the lines of sessions running at once can be told apart.
