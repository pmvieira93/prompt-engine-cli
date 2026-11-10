# Configuration reference

[README](../README.md) · [Getting Started](GETTING_STARTED.md) · [Install](install.md)

- [Configuration](#configuration)
- [Flags](#flags)
- [Environment variables](#environment-variables)
- [Files and locations](#files-and-locations)

## Configuration

The settings live in **`~/.config/prompt-engine/config.yaml`**, or under `$XDG_CONFIG_HOME/prompt-engine/` when that is set.

- **Every key is optional.** Anything you leave out keeps its default.
- **Mistakes are caught.** An unknown key or an invalid value gives a clear warning, and the defaults (or, while running, your previous settings) are kept.
- **Changes apply live**, except the keys marked *restart* below.

**Template:** [`docs/config.example.yaml`](config.example.yaml) lists every setting at its default, with a comment on each.

### All settings

| Key | Default | Description |
|---|---|---|
| **Wrapped CLI** | | |
| `command` | `claude` | The CLI to wrap. Only Claude Code is supported. *Restart* |
| `args` | `[]` | Arguments placed before the ones you type. *Restart* |
| `env` | `{}` | Extra environment variables for the CLI. *Restart* |
| **`llm`**: local models | | |
| `llm.enabled` | `true` | `false` turns ghost text off |
| `llm.provider` | `ollama` | `ollama`, or `fake` (a fixed word list, for testing) |
| `llm.endpoint` | `http://localhost:11434` | Ollama's address |
| `llm.model` | `qwen2.5-coder:1.5b` | Ghost text model; must support fill-in-the-middle |
| `llm.max_tokens` | `8` | Length of a suggestion (1–64) |
| `llm.debounce` | `150ms` | Pause in typing before a suggestion is requested |
| `llm.timeout` | `2s` | Slower suggestions are dropped |
| `llm.expand_model` | `qwen2.5:7b` | Rewrite model |
| `llm.expand_timeout` | `30s` | Time limit for a rewrite you asked for, including loading the model |
| **`expand`**: prompt rewrites | | |
| `expand.on_enter` | `off` | `off`, `preview` or `auto` ([modes](guide/rewrites.md#modes)) |
| `expand.style` | `default` | `default`, `llm`, `human` or `both` |
| `expand.context` | `prompts` | `none`, `prompts`, `reply` or `session` ([levels](guide/rewrites.md#session-context)) |
| `expand.context_prompts` | `3` | How many earlier prompts the model sees (1–20) |
| `expand.preview_pause` | `1s` | `preview`: how long typing must pause |
| `expand.min_words` | `3` | `auto`: shorter prompts are sent as typed |
| `expand.auto_timeout` | `5s` | `auto`: after this, the prompt is sent as typed |
| `expand.guards` | `on` | `off` ignores `templates/guards.txt`, your own guard rails, while it's there. The built-in guards apply either way |
| **`ghost`**: ghost text | | |
| `ghost.style` | `italic` | `italic`, `faint` or `color:N` (0–255) |
| `ghost.context` | `prompts` | `prompts`: suggestions follow your last prompts; `none` doesn't |
| `ghost.context_prompts` | `3` | How many earlier prompts the ghost text model sees (1–20) |
| **`panel`**: side panel | | |
| `panel.visible` | `false` | Open the panel at startup. *Restart* |
| `panel.side` | `right` | `right` or `left` |
| `panel.width` | `40` | Width, in percent of the terminal (10–90) |
| `panel.guard_details` | `false` | Show which guard stopped a rewrite, and what it stopped |
| **`keys`**: shortcuts, pressed after the leader | | |
| `keys.leader` | `ctrl+q` | `ctrl+a`…`ctrl+z`, or `ctrl+\ ] ^ _` |
| `keys.expand` | `e` | Rewrite the prompt, or apply the preview |
| `keys.undo` | `u` | Put back the prompt from before the last rewrite |
| `keys.style` | `s` | The next rewrite style, for this prompt only |
| `keys.panel` | `p` | Toggle the side panel |
| `keys.panel_wider` | `+` | Widen the panel by 5% |
| `keys.panel_narrower` | `-` | Narrow the panel by 5% |
| **`templates`** | | |
| `templates.dir` | `templates` | Folder for `append.txt`, `system.txt` and `guards.txt`, relative to the config file. *Restart* |
| `templates.min_words` | `3` | Shorter prompts don't get `append.txt`. `1` adds it to every prompt |
| **`history`** | | |
| `history.enabled` | `true` | Record one line per prompt sent, and one with its token usage when its turn ends |
| `history.include_text` | `false` | Also record the prompts' text |
| `history.path` | `""` | Default: `~/.local/state/prompt-engine/history.jsonl`. `PROMPT_ENGINE_HISTORY` wins over it |

Durations use Go's format: `150ms`, `2s`, `1m`.

## Flags

Every argument goes on to `claude`, except these, which start with `--pe-`:

| Flag | Purpose |
|---|---|
| `--pe-config <file>` | Use this config file (wins over `PROMPT_ENGINE_CONFIG`) |
| `--pe-version` | Print `prompt-engine`'s version and exit (`--version` still gives Claude Code's) |
| `--pe-init-guards` | Write the guard rails template to `guards.txt` if there's none, and print its path. The install script runs it |
| `--pe-check-guards "<typed>" "<rewrite>"` | Say which guard rail stops the rewrite, or that it passes, with your `guards.txt`. Exits 0 if it passes, 1 if stopped, 2 if `guards.txt` is invalid |
| `--pe-rewrite "<prompt>"` | Print, as JSON, what auto mode would send for the prompt: the rewrite (or the prompt as typed, and why), its style and time, and the guidance the hook would add, kept apart. No session is started, so there is no session context. For scripts such as the cost benchmark. Exits 0, or 2 if `guards.txt` is invalid |

## Environment variables

| Variable | Purpose |
|---|---|
| `PROMPT_ENGINE_CONFIG` | Path to the config file, overriding the default location. The `--pe-config <file>` flag does the same and wins over the variable. It is removed before the other arguments reach `claude`. |
| `XDG_CONFIG_HOME` | Base folder for the config (default `~/.config`) |
| `XDG_STATE_HOME` | Base folder for the history (default `~/.local/state`) |
| `CLAUDE_CONFIG_DIR` | Claude Code's folder (default `~/.claude`), where `expand.context: reply` and `session` find its transcripts |
| **Troubleshooting** | |
| `PROMPT_ENGINE_DEBUG_LOG` | File for a detailed log: keys, the prompt as tracked, suggestions, rewrites and guards. Its first line names the session's id |
| `PROMPT_ENGINE_HISTORY` | The history file, over `history.path`: e.g. one per project, set in your shell or with direnv |
| `PROMPT_ENGINE_DEBUG_OUTPUT` | File for a raw copy of everything Claude Code draws |
| `PROMPT_ENGINE_PASSTHROUGH` | `1` runs a plain pass-through with no ghost text or panel, to tell whether a glitch is `prompt-engine`'s |
| **Testing** | |
| `PROMPT_ENGINE_SUGGESTER` | `fake` or `ollama`, overriding `llm.provider` |
| `PROMPT_ENGINE_FAKE_DELAY` | A delay for the fake suggester, such as `300ms`, to mimic a model |
| `PROMPT_ENGINE_TEST_RENDER_PANIC` | `1` makes the display layer fail, to test the pass-through fallback |
| **Internal** | |
| `PROMPT_ENGINE_ACTIVE` | Set for the wrapped CLI, so a nested `prompt-engine` doesn't wrap twice. To test `prompt-engine` from such a session, use `scripts/test-env.sh` ([testing guide](TESTING.md), 1.2b) |

## Files and locations

| Path | What it holds |
|---|---|
| `~/.config/prompt-engine/config.yaml` | Your settings |
| `~/.config/prompt-engine/templates/append.txt` | Rules added to the prompts you send |
| `~/.config/prompt-engine/templates/system.txt` | Your own rewrite instructions (replaces every style) |
| `~/.config/prompt-engine/templates/guards.txt` | Your own guard rails (optional) |
| `~/.local/state/prompt-engine/history.jsonl` | The history of sent prompts |
| `~/.claude/projects/<project>/<session>.jsonl` | Claude Code's transcript: read, never written, with `expand.context: reply` or `session` |
| `~/.local/bin/prompt-engine` | The binary, as installed by the script |
