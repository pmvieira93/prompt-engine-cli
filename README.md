# prompt-engine

[![CI](https://github.com/pmvieira93/prompt-engine-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/pmvieira93/prompt-engine-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/pmvieira93/prompt-engine-cli?include_prereleases)](https://github.com/pmvieira93/prompt-engine-cli/releases)

**Better prompts for Claude Code, written as you type, without leaving your terminal.**

`prompt-engine` wraps [Claude Code](https://claude.com/claude-code) in your terminal. You use `claude` exactly as before, and `prompt-engine` adds three things on top:

- **Ghost text**: grey completions that finish your words and sentences as you type.
- **Prompt rewrites**: a rough prompt becomes a clear one, with typos fixed and names kept.
- **Rules**: your standing guidelines are added to every prompt you send.

Everything runs **locally** through [Ollama](https://ollama.com). Your prompts never leave your machine before they reach Claude Code.

![A rough prompt with typos is rewritten with Ctrl+Q, e; once sent, the side panel shows what changed](docs/media/rewrite.gif)

> New here? Start with **[Getting Started](docs/GETTING_STARTED.md)**: install, first run and a shortcut cheat sheet in five minutes.

---

## Contents

- [Features](#features)
- [Requirements](#requirements)
- [Installation](#installation)
- [Local models](#local-models)
- [Configuration](#configuration)
- [Flags](#flags)
- [Environment variables](#environment-variables)
- [Files and locations](#files-and-locations)
- [Uninstall](#uninstall)
- [Documentation](#documentation)

---

## Features

### Ghost text

Pause while typing, and a suggestion appears after the cursor in grey italics.

![Typing "list the files in this dire", pausing, and accepting "ctory" with Tab](docs/media/ghost.gif)

- **Tab** or **→** accepts it, and **Esc** dismisses it. Typing on also dismisses it.
- It finishes half-typed words (`refa` → `refactor`) and continues sentences. This uses a fill-in-the-middle model.
- It only shows where it can't get in the way. It stays out of Claude Code's dialogs, its `@` file menu and `/` command menu, and doesn't show when the cursor isn't at the end of the prompt.
- Suggestions follow the session: the model sees your last 3 prompts, so after `say hello in three words in portuguese`, typing `now say goodbye in thr` gets `ee words in portuguese`.
- Suggestions that would only repeat what you already typed are dropped.
- Typing never waits for the model. A suggestion is requested after a short pause (150 ms), and one that takes longer than 2 s is dropped.
- The style is yours to pick: `italic` (default), `faint`, or any colour from the 256-colour palette.

### Prompt rewrites (expansion)

A local model rewrites your prompt into a clearer one. It works in three modes (`expand.on_enter`):

| Mode | What happens |
|---|---|
| `off` (default) | **Ctrl+Q, then e** rewrites the prompt in place. You review it, edit it or undo it (**Ctrl+Q, then u**), and press Enter yourself. |
| `preview` | When you pause, a rewrite is prepared in the background and shown in the side panel. **Ctrl+Q, then e** applies it. **Enter** sends what you typed. |
| `auto` | **Enter** rewrites the prompt, then sends it. Prompts shorter than 3 words (`yes`, `continue`) go as typed, and so does a prompt the model can't rewrite within 5 s. |

In `preview` mode, the rewrite waits in the side panel while you type; **Ctrl+Q, then e** takes it:

![A rewrite prepared while typing pauses, shown in the panel, then applied and sent](docs/media/preview.gif)

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

  In `auto` mode the prompt is then sent as typed; in `preview` mode no rewrite is offered. Set `panel.guard_details: true` to see which guard stopped a rewrite, and why.

### Session context

How much the rewrite model knows about the session is set by `expand.context`. Each level adds to the one before.

![With context: reply, "tell me a fun fact about the second one" is rewritten to name the colour Claude listed second](docs/media/context.gif)

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

**Rewrite styles** (`expand.style`) set who the wording is for:

| Style | Wording |
|---|---|
| `default` | The model decides |
| `llm` | Terse and imperative, for the assistant to act on |
| `human` | Natural, in your own voice |
| `both` | Natural sentences, with bullets when there are several parts |

To use your own instructions instead, write them in `templates/system.txt`.

### Rules

Put your standing guidelines in `~/.config/prompt-engine/templates/append.txt`, for example "Run the tests before saying it works." They are added to the end of every prompt you send.

- Slash commands, `!` shell commands, `#` memory notes and empty prompts are skipped.
- Edits to the file apply to your next prompt.

### Side panel

**Ctrl+Q, then p** opens a panel next to Claude Code. It shows:

- **Prompt diff:** what you typed against what was sent, with every change highlighted.
- **Session:** the current mode, style and models, and any setting that needs a restart.
- **Guard details:** with `panel.guard_details`, which guard stopped a rewrite and the start of what it stopped.

The panel goes on the right or the left. **Ctrl+Q, then + or −** resizes it.

### History

Each prompt you send adds one JSON line to `~/.local/state/prompt-engine/history.jsonl`. The line records whether the prompt was rewritten, in which style, whether rules were added, and which guard stopped a rewrite. The text itself is recorded only with `history.include_text: true`.

### Built to stay out of the way

- **Drop-in.** `prompt-engine` passes every argument on to `claude` (`prompt-engine --resume` works). Every plain Ctrl key still belongs to Claude Code; `prompt-engine`'s own shortcuts sit behind one leader key (**Ctrl+Q**). Press Ctrl+Q twice to send a real Ctrl+Q.
- **Live settings.** Edits to the config file apply while it runs. The panel says when a setting needs a restart.
- **Safe fallback.**
  - If the display layer fails, `prompt-engine` switches to plain pass-through and your session continues.
  - Without a terminal (for example `prompt-engine -p "…" | less`), it hands over to `claude` directly.

---

## Requirements

| | Minimum | Recommended |
|---|---|---|
| **System** | macOS or Linux (Windows isn't supported) | macOS on Apple Silicon |
| **Memory** | 8 GB, for ghost text | 16 GB, to keep the rewrite model loaded too |
| **Disk** | 3 GB, for Go's build cache, the binary and the ghost text model | 8 GB, with the rewrite model |
| **Claude Code** | Installed and signed in | Latest version |
| **Ollama** | Running locally, or reachable over the network | Latest version |
| **Go** | 1.26, to build from source | Latest version |
| **Terminal** | 256 colours and bracketed paste | iTerm2 (tested), Terminal.app, or any modern terminal |

---

## Installation

### Install script (recommended)

```sh
git clone https://github.com/pmvieira93/prompt-engine-cli.git
cd prompt-engine-cli
./install.sh
```

The script asks before it changes anything. In order, it:

1. **Checks your system:** OS, memory, disk and package manager.
2. **Finds your AI coding CLIs.** Claude Code is the one supported today. Others it finds (Gemini, GitHub Copilot, Codex…) are listed as "found, not supported yet" and can't be chosen until they're tested.
3. **Lists missing dependencies** (Homebrew on macOS, Go, Ollama, Claude Code) and offers to install them all.
4. **Builds `prompt-engine`** into `~/.local/bin`, and writes a commented config file if you have none.
5. **Downloads the models.** The ghost text model is required. The rewrite model is optional: download it now, or later with `./install.sh --expand-model`.
6. **Runs a health check:**
   - the binary starts Claude Code;
   - your config is valid;
   - Ollama answers;
   - each model returns a real completion.

| Option | Effect |
|---|---|
| `--check` | Only run the health check |
| `--expand-model` | Only download the rewrite model |
| `--yes` | Answer yes to every question (unattended) |
| `--prefix DIR` | Install somewhere other than `~/.local/bin` |
| `--skip-expand` | Don't offer the rewrite model |
| `--dry-run` | Show what would be done, change nothing |

### From a release

Each [release](https://github.com/pmvieira93/prompt-engine-cli/releases) has a ready-built binary for macOS and Linux (Apple Silicon or Intel, arm64 or amd64), with `checksums.txt` to verify it:

```sh
# Pick your version and platform: darwin_arm64, darwin_amd64, linux_amd64 or linux_arm64.
curl -fsSLO https://github.com/pmvieira93/prompt-engine-cli/releases/download/v1.0.0/prompt-engine_v1.0.0_darwin_arm64.tar.gz
tar -xzf prompt-engine_v1.0.0_darwin_arm64.tar.gz
mv prompt-engine_v1.0.0_darwin_arm64/prompt-engine ~/.local/bin/
prompt-engine --pe-version
```

You still need Claude Code, Ollama and the models (steps 1 and 2 below). The binary isn't signed: if macOS blocks it, run `xattr -d com.apple.quarantine ~/.local/bin/prompt-engine`.

### Manual install

```sh
# 1. Dependencies (macOS; on Linux use your package manager and ollama.com/download)
brew install go ollama
brew services start ollama
curl -fsSL https://claude.ai/install.sh | bash   # Claude Code; run `claude` once to sign in

# 2. Models
ollama pull qwen2.5-coder:1.5b   # ghost text (required, ~1 GB)
ollama pull qwen2.5:7b           # prompt rewrites (optional, ~4.7 GB)

# 3. Build
go build -o ~/.local/bin/prompt-engine ./cmd/prompt-engine

# 4. Optional: start from the full config template
mkdir -p ~/.config/prompt-engine
cp docs/config.example.yaml ~/.config/prompt-engine/config.yaml
```

Then run `prompt-engine` where you would run `claude`. To make it your default, add `alias claude=prompt-engine` to your shell profile. `prompt-engine` won't wrap itself twice.

---

## Local models

`prompt-engine` uses two models, each chosen for its job:

| Capability | Default | Size | Required | Why this model |
|---|---|---|---|---|
| Ghost text (`llm.model`) | `qwen2.5-coder:1.5b` | ~1 GB | Yes | It supports **fill-in-the-middle**, which finishing a half-typed word needs. It answers in 50–300 ms, fast enough to feel instant. |
| Prompt rewrites (`llm.expand_model`) | `qwen2.5:7b` | ~4.7 GB | No | In testing, 7B was the smallest size that rewrote **faithfully**. The 1.5B and 3B models invented requirements and misread shorthand. It takes about 1–3 s, which is fine because it only runs on a pause, on request, or on Enter. |

**Other options.** Both settings take any Ollama model name, so you can trade speed for quality.

| Capability | Option | Trade-off |
|---|---|---|
| Ghost text | `qwen2.5-coder:0.5b` | Faster, on smaller machines, with weaker suggestions |
| Ghost text | `qwen2.5-coder:3b` | Better suggestions, at a higher latency |
| Ghost text | Other fill-in-the-middle coders (`deepseek-coder`, `starcoder2`, `codegemma`) | Not tested with `prompt-engine`. The model **must** support fill-in-the-middle; `./install.sh --check` verifies it. |
| Rewrites | `qwen2.5:14b` | Closer rewrites if you have the memory (~9 GB). Not yet tested. |
| Rewrites | Other 7–9B instruction models (`llama3.1:8b`, `gemma2:9b`) | Not tested. The guard rails catch rewrites that drift. |

**Ollama on another machine.** Point `llm.endpoint` at it, for example `http://gpu-box:11434`.

**No models at all.** With `llm.enabled: false`, ghost text is off, and `prompt-engine` still adds your rules and shows the panel.

---

## Configuration

The settings live in **`~/.config/prompt-engine/config.yaml`**, or under `$XDG_CONFIG_HOME/prompt-engine/` when that is set.

- **Every key is optional.** Anything you leave out keeps its default.
- **Mistakes are caught.** An unknown key or an invalid value gives a clear warning, and the defaults (or, while running, your previous settings) are kept.
- **Changes apply live**, except the keys marked *restart* below.

**Template:** [`docs/config.example.yaml`](docs/config.example.yaml) lists every setting at its default, with a comment on each.

### Reference

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
| `expand.on_enter` | `off` | `off`, `preview` or `auto` ([modes](#prompt-rewrites-expansion)) |
| `expand.style` | `default` | `default`, `llm`, `human` or `both` |
| `expand.context` | `prompts` | `none`, `prompts`, `reply` or `session` ([levels](#session-context)) |
| `expand.context_prompts` | `3` | How many earlier prompts the model sees (1–20) |
| `expand.preview_pause` | `1s` | `preview`: how long typing must pause |
| `expand.min_words` | `3` | `auto`: shorter prompts are sent as typed |
| `expand.auto_timeout` | `5s` | `auto`: after this, the prompt is sent as typed |
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
| `keys.panel` | `p` | Toggle the side panel |
| `keys.panel_wider` | `+` | Widen the panel by 5% |
| `keys.panel_narrower` | `-` | Narrow the panel by 5% |
| **`templates`** | | |
| `templates.dir` | `templates` | Folder for `append.txt` and `system.txt`, relative to the config file. *Restart* |
| **`history`** | | |
| `history.enabled` | `true` | Record one line per prompt sent |
| `history.include_text` | `false` | Also record the prompts' text |
| `history.path` | `""` | Default: `~/.local/state/prompt-engine/history.jsonl` |

Durations use Go's format: `150ms`, `2s`, `1m`.

---

## Flags

Every argument goes on to `claude`, except these two, which start with `--pe-`:

| Flag | Purpose |
|---|---|
| `--pe-config <file>` | Use this config file (wins over `PROMPT_ENGINE_CONFIG`) |
| `--pe-version` | Print `prompt-engine`'s version and exit (`--version` still gives Claude Code's) |

## Environment variables

| Variable | Purpose |
|---|---|
| `PROMPT_ENGINE_CONFIG` | Path to the config file, overriding the default location. The `--pe-config <file>` flag does the same and wins over the variable. It is removed before the other arguments reach `claude`. |
| `XDG_CONFIG_HOME` | Base folder for the config (default `~/.config`) |
| `XDG_STATE_HOME` | Base folder for the history (default `~/.local/state`) |
| `CLAUDE_CONFIG_DIR` | Claude Code's folder (default `~/.claude`), where `expand.context: reply` and `session` find its transcripts |
| **Troubleshooting** | |
| `PROMPT_ENGINE_DEBUG_LOG` | File for a detailed log: keys, the prompt as tracked, suggestions, rewrites and guards |
| `PROMPT_ENGINE_DEBUG_OUTPUT` | File for a raw copy of everything Claude Code draws |
| `PROMPT_ENGINE_PASSTHROUGH` | `1` runs a plain pass-through with no ghost text or panel, to tell whether a glitch is `prompt-engine`'s |
| **Testing** | |
| `PROMPT_ENGINE_SUGGESTER` | `fake` or `ollama`, overriding `llm.provider` |
| `PROMPT_ENGINE_FAKE_DELAY` | A delay for the fake suggester, such as `300ms`, to mimic a model |
| `PROMPT_ENGINE_TEST_RENDER_PANIC` | `1` makes the display layer fail, to test the pass-through fallback |
| **Internal** | |
| `PROMPT_ENGINE_ACTIVE` | Set for the wrapped CLI, so a nested `prompt-engine` doesn't wrap twice. To test `prompt-engine` from such a session, use `scripts/test-env.sh` ([testing guide](docs/TESTING.md), 1.2b) |

---

## Files and locations

| Path | What it holds |
|---|---|
| `~/.config/prompt-engine/config.yaml` | Your settings |
| `~/.config/prompt-engine/templates/append.txt` | Rules added to every prompt |
| `~/.config/prompt-engine/templates/system.txt` | Your own rewrite instructions (replaces every style) |
| `~/.local/state/prompt-engine/history.jsonl` | The history of sent prompts |
| `~/.claude/projects/<project>/<session>.jsonl` | Claude Code's transcript: read, never written, with `expand.context: reply` or `session` |
| `~/.local/bin/prompt-engine` | The binary, as installed by the script |

---

## Uninstall

```sh
rm ~/.local/bin/prompt-engine
rm -r ~/.config/prompt-engine ~/.local/state/prompt-engine   # settings and history
ollama rm qwen2.5-coder:1.5b qwen2.5:7b                      # the models, if nothing else uses them
```

---

## Documentation

| Document | For |
|---|---|
| [Getting Started](docs/GETTING_STARTED.md) | First run, the shortcut cheat sheet, common recipes and troubleshooting |
| [Config template](docs/config.example.yaml) | Every setting, commented |
| [Testing guide](docs/TESTING.md) | The manual test plan, feature by feature, and the automated end-to-end and live smoke tests (1.2b) |
| [PRD](PRD.md), [Appendix 1](docs/PRD-APPENDIX-1.md), [Appendix 2](docs/PRD-APPENDIX-2.md) | Requirements, design decisions and what testing showed |

## License

[MIT](LICENSE)
