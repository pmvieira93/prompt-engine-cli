# Guidance

[README](../../README.md) · [Getting Started](../GETTING_STARTED.md) · [Configuration reference](../reference.md)

Put your standing guidelines in `~/.config/prompt-engine/templates/append.txt`, for example "Run the tests before saying it works." They reach Claude Code as context for every prompt you send, under a `# Guidance` heading, except short ones like "yes" or "ok thanks": those with fewer words than `templates.min_words` (3).

```text
fix the login form

# Guidance
Run the tests before saying it works.
```

This is guidance, not enforcement: Claude reads it, but nothing checks that it was followed, and some prompts don't get it. For an instruction that should always be there, use `CLAUDE.md`. For something that must never happen, use Claude Code's permissions or hooks.

- Slash commands, `!` shell commands, `#` memory notes and empty prompts are skipped.
- Edits to the file apply to your next prompt.

## Sections

To add some rules only when they fit, put them in sections. A line like `[test|tests]` starts a section, and it's added only to prompts with one of its words. The sections and words are your own:

```text
Keep changes small.

[review|PR|MR]
Check the error paths too.

[test|tests|coverage]
Test behaviour, not implementation.

[investigate|analysis|why]
Don't change any code yet: report what you find.

[debug|bug|crash]
Find the cause before proposing a fix.
```

- **Text before the first section is added to every prompt that gets guidelines,** so it's the most reliable place in this file, though not a guarantee. "Fix the crash" gets "Keep changes small." and the `[debug|bug|crash]` text.
- **Whole words only, ignoring case.** `bug` matches "Bug", "'bug'" and "bug's" but not "debug", and `test` doesn't match "tests" unless you list it. A prompt can get several sections.
- **Only what you wrote counts:** not the rewrite's words or code blocks, nor pastes, unless a paste is the whole prompt. A word that's only mentioned still matches: "all tests passed" gets the test section.
- **A mistake stops all the rules** until it's fixed: a word used twice, a section with no text, or a line that is only `[…]` and isn't words. `[…]` lines inside a code block are left as they are. The warning names the line, and it also shows in the side panel. A file with no such lines works as before.

## How it reaches Claude Code

The guidance comes through a hook that `prompt-engine` sets up when it starts. Claude Code reads it as context for the prompt, so it doesn't show in Claude Code's prompt box or transcript view: the side panel's **Last prompt sent** shows it. Why a hook and not text added to the prompt: [PRD Appendix 7](../PRD-APPENDIX-7.md).

- The hook is only set up when the wrapped `command` is `claude`.
- If you pass your own `--settings` to `claude`, it replaces the hook, so no guidance is added and `prompt-engine` warns at startup.
