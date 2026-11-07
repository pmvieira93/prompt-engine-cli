# Prompt rewrites

[README](../../README.md) · [Getting Started](../GETTING_STARTED.md) · [Configuration reference](../reference.md)

A local model rewrites your prompt into a clearer one.

## Modes

It works in three modes (`expand.on_enter`):

| Mode | What happens |
|---|---|
| `off` (default) | **Ctrl+Q, then e** rewrites the prompt in place. You review it, edit it or undo it (**Ctrl+Q, then u**), try another style (**Ctrl+Q, then s**), and press Enter yourself. |
| `preview` | When you pause, a rewrite is prepared in the background and shown in the side panel. **Ctrl+Q, then e** applies it. **Enter** sends what you typed. |
| `auto` | **Enter** rewrites the prompt, then sends it. Prompts shorter than 3 words (`yes`, `continue`) go as typed, and so does a prompt the model can't rewrite within 5 s. |

In `preview` mode, the rewrite waits in the side panel while you type; **Ctrl+Q, then e** takes it:

![A rewrite prepared while typing pauses, shown in the panel, then applied and sent](../media/preview.gif)

## What every rewrite keeps

Every rewrite follows the same rules:

- **It keeps your intent.** It fixes typos and spells out abbreviations, but never adds goals, files or steps you didn't mention.
- **It keeps names exactly as written:** agents, skills, commands, `@mentions`, paths, branches, and issue and PR numbers.
- **It keeps your language** and every limit or negation ("don't", "only", "without").
- **It keeps what you pasted untouched.** Only the words you typed around a paste are corrected.
- **It knows the session.** The model sees your last 3 prompts, so "add tests for it" resolves. A `Context:` block with the git branch and staged files is added to the result. For more, see [Session context](#session-context).
- **Guard rails stop bad rewrites.** They catch a rewrite that changes your meaning, for example by:
  - asking you a question back, or turning your question into an instruction;
  - dropping a name, a quote or a whole sentence;
  - adding a task, or something destructive (*delete, reset, force push*), that you didn't ask for;
  - losing a negation, or changing the language.

  In `auto` mode the prompt is then sent as typed; in `preview` mode no rewrite is offered. Set `panel.guard_details: true` to see which guard stopped a rewrite, and why. To change the guards, see [Your own guard rails](guard-rails.md).

## Session context

How much the rewrite model knows about the session is set by `expand.context`. Each level adds to the one before.

![With context: reply, "tell me a fun fact about the second one" is rewritten to name the colour Claude listed second](../media/context.gif)

| Level | The model sees | Resolves |
|---|---|---|
| `none` | Your prompt alone | Nothing |
| `prompts` (default) | Your last prompts | "add tests for **it**", "**same** for the client" |
| `reply` (recommended) | Those, and the end of Claude Code's last reply | "do **the first one**", "**yes, go ahead**", "fix **the second one**" |
| `session` | All that, and a running summary of the session | The same as `reply`, with the earlier turns summarised |

- **`reply`** reads Claude Code's own transcript (`~/.claude/projects/…`), on your machine. That's why it's opt-in. Only the transcript holding a prompt this session sent is used, so there's no reply context before your first prompt. If the transcript can't be read, rewrites fall back to `prompts`. In testing it resolved 15 of 18 follow-ups that pointed at a reply, where `prompts` resolved none. The other 3 were stopped by the guard rails, so they'd be sent as typed. It added nothing to unrelated prompts.
- **`session`** also asks the rewrite model to update a summary after each reply. That costs a few seconds of model time per reply, and in testing the rewrites came out no better than with `reply`: vaguer, and once an answer instead of a rewrite.
- **Known limit.** Asking to translate something from the reply ("say the second one in portuguese") can make the model answer instead of rewriting. With the shortcut you see it and can undo it; in `auto` and `preview` mode the language guard stops it.
- **Guard rails still apply.** A rewrite may take words from the reply only when your prompt points at it ("that", "the second one", "go ahead"). Even then, an added delete, reset or force push is stopped.

## Styles

`expand.style` sets who the wording is for:

| Style | Wording |
|---|---|
| `default` | The model decides |
| `llm` | Terse and imperative, for the assistant to act on |
| `human` | Natural, in your own voice |
| `both` | Natural sentences, with bullets when there are several parts |

**A style for one prompt:** **Ctrl+Q, then s** picks the next style for the prompt you're writing, in the order `default` → `llm` → `human` → `both`, starting after the one in use. The choice lasts until the prompt is sent or cleared; `expand.style` stays your setting.
- Before a rewrite, the next rewrite uses that style: Ctrl+Q, then e, the preview, or Enter in `auto` mode.
- On a rewrite, it rewrites **your original text** again in the next style, so you can compare styles without retyping. Undo still brings back your text.
- With the side panel closed, a faint `style: llm` shows in the box for a moment. With it open, the panel's title names the style: `Suggested rewrite (llm, for this prompt)`.

## Your own instructions

To use your own instructions instead, write them in `templates/system.txt`. They replace every style.
