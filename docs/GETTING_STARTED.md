# Getting Started

From install to your first rewritten prompt in five minutes. For every setting and option, see the [README](../README.md).

---

## 1. Install

```sh
git clone https://github.com/pmvieira93/prompt-engine-cli.git
cd prompt-engine-cli
scripts/install.sh
```

The script checks your system and installs what's missing, asking first. It downloads the ghost text model and ends with a health check. When it says **✓ prompt-engine is ready**, you're set.

If it says `~/.local/bin is not on your PATH`, add the line it prints to `~/.zshrc` or `~/.bashrc`, then open a new terminal.

---

## 2. First run

Open a project and start `prompt-engine` where you would run `claude`:

```sh
cd ~/my-project
prompt-engine
```

You'll see Claude Code as usual. Now try each feature:

1. **Ghost text.**
   - Type `refa` and pause. Grey italic text finishes the word: `refactor the…`.
   - Press **Tab** to accept it, or **Esc** to dismiss it.
2. **The side panel.**
   - Press **Ctrl+Q**, then **p**. A panel opens next to Claude Code, showing what you typed against what was sent.
   - Press the same keys again to close it.
3. **A rewrite.** This needs the rewrite model; if you skipped it, run `scripts/install.sh --expand-model` first.
   - Type `explain waht the confg loader does` (typos included).
   - Press **Ctrl+Q**, then **e**. The prompt is rewritten in place, with the typos fixed.
   - Press **Ctrl+Q**, then **u** to undo it, or **Enter** to send it.

---

## 3. Shortcut cheat sheet

| Keys | Action |
|---|---|
| **Tab** or **→** | Accept the ghost text |
| **Esc** | Dismiss the ghost text (Claude Code doesn't see this Esc) |
| **Ctrl+Q**, then **e** | Rewrite the prompt, or apply the prepared preview |
| **Ctrl+Q**, then **u** | Undo the last rewrite |
| **Ctrl+Q**, then **p** | Open or close the side panel |
| **Ctrl+Q**, then **+** / **−** | Widen or narrow the panel |
| **Ctrl+Q**, then **Esc** | Cancel the leader key |
| **Ctrl+Q**, **Ctrl+Q** | Send a real Ctrl+Q to Claude Code |

Everything else, including every other Ctrl key, goes to Claude Code unchanged.

---

## 4. Common recipes

Add these to `~/.config/prompt-engine/config.yaml`. Changes apply as soon as you save, with no restart needed.

**See a rewrite before you send** (preview):

```yaml
expand:
  on_enter: preview
```

Pause after typing, and a rewrite appears in the side panel. **Ctrl+Q, then e** uses it; **Enter** sends what you typed.

**Rewrite every prompt on Enter** (auto):

```yaml
expand:
  on_enter: auto
```

Short replies like `yes` or `continue` go as typed. If a guard rail finds the rewrite changed your meaning, your original prompt is sent instead.

**Let rewrites understand "do the first one"** (Claude Code's last reply):

```yaml
expand:
  context: reply
```

After Claude Code replies with a list or options, `tell me more about the second one` is rewritten to name what the second item says. This reads Claude Code's transcript, on your machine, from your first prompt on.

**Choose who the wording is for:**

```yaml
expand:
  style: llm      # terse and direct; or human, both, default
```

**See why a rewrite was stopped:**

```yaml
panel:
  guard_details: true
```

**Add your rules to every prompt.** Write them, one per line, in `~/.config/prompt-engine/templates/append.txt`:

```text
Run the tests before saying it works.
Keep changes minimal; don't refactor unrelated code.
```

**Change how ghost text looks:**

```yaml
ghost:
  style: color:117   # light blue; or faint, or italic
```

**Keep the panel open, on the left:**

```yaml
panel:
  visible: true      # applies at the next start
  side: left
```

**Use `prompt-engine` every time you type `claude`:**

```sh
echo 'alias claude=prompt-engine' >> ~/.zshrc
```

---

## 5. Troubleshooting

| Symptom | Check | Fix |
|---|---|---|
| No ghost text | `scripts/install.sh --check` | Start Ollama (`brew services start ollama` or `ollama serve`), or pull the model it names |
| Ghost text is slow | Ollama runs both models at once | Close other heavy apps, or try `llm.model: qwen2.5-coder:0.5b` |
| A setting is ignored | The panel's **Session** section shows the error | Fix the key or value it names; the template is [`config.example.yaml`](config.example.yaml) |
| `ghost.style` rejected | The value must be on one line | `style: color:117`, not a nested `color:` key |
| Rewrites never apply | `panel.guard_details: true` | Each stop names its guard. Rephrase, or use `preview` to review |
| "the second one" isn't named | `context: reading the transcript` in the debug log | Set `expand.context: reply`, and send a first prompt: the transcript is only used once it holds one of yours |
| Odd characters on screen | Run with `PROMPT_ENGINE_PASSTHROUGH=1` | If they remain, the cause isn't `prompt-engine` |
| Need details for a bug report | `PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log prompt-engine` | Attach the log. Use harmless prompts: it records what you type |

The health check is safe to run at any time and changes nothing:

```sh
scripts/install.sh --check
```

---

## Next steps

- [README](../README.md): every feature, setting, environment variable and model option.
- [Testing guide](TESTING.md): a feature-by-feature walk-through, with what to expect.
