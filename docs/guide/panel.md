# Side panel and history

[README](../../README.md) · [Getting Started](../GETTING_STARTED.md) · [Configuration reference](../reference.md)

## Side panel

**Ctrl+Q, then p** opens a panel next to Claude Code. It shows:

- **Prompt diff:** what you typed against what was sent, with every change highlighted.
- **Session:** the current mode, style and models, any setting that needs a restart, and your `guards.txt` rules and the guards it turns off.
- **Tokens:** what Claude Code has used, read from its transcript once a reply arrives:
  - **context:** the size of the last API call's input, and the model. A long session pays for its context on every call, so this is the number to watch;
  - **reply** and **so far:** input and output tokens for the last reply and for the conversation since it started or since `/clear`, with the share of input read from Claude's cache;
  - **calls:** how many API calls the reply and the conversation made. One prompt often makes several, as Claude uses tools. Subagents' calls count in **so far**, and a line under it says how many there were, in all and in the reply.
- **Guard details:** with `panel.guard_details`, which guard stopped a rewrite and the start of what it stopped.

The panel goes on the right or the left. **Ctrl+Q, then + or −** resizes it.

## History

Each prompt you send adds one JSON line to `~/.local/state/prompt-engine/history.jsonl`. The line records whether the prompt was rewritten, in which style, whether rules were added and from which sections, and which guard stopped a rewrite. The text itself is recorded only with `history.include_text: true`. Each line also names the session (`"session"`, a short id that the debug log names too), so the lines of sessions running at once can be told apart.

When a prompt's turn ends, a second line records its tokens. A turn ends when you send the next prompt, or when the session ends, so an interrupted reply is recorded too. The line has `"kind": "usage"`, the prompt's time (`"prompt_time"`, the `"time"` of the prompt's line), the API calls, the input tokens (fresh, written to the cache and read from it), the output tokens, the context size and the model, and how many calls subagents made. It holds numbers only, never text.
