# Manual Test Guide

Covers **Phase 1** (the PTY wrapper and input buffer, Parts A–D) and **Phase 2** (ghost text and the side panel, Part E; real suggestions from a local model, Part F). Extend this guide as later phases land.

**What's being tested:** `prompt-engine` runs `claude` inside its own pseudo-terminal, draws `claude`'s screen through a built-in terminal emulator, and records what you type. Apart from ghost-text suggestions and the side panel, nothing should look or feel different from running `claude` directly.

Suggestions come from a local model through **Ollama** (Part F). For repeatable results, Part E uses the **fake suggester** instead (`PROMPT_ENGINE_SUGGESTER=fake`), which completes the last word from a fixed list: implement, refactor, explain, function, repository, documentation, performance, integration, tests.

> ⚠️ **Don't press Enter** in Parts C and E unless a step says so: it sends a real prompt to `claude` and uses your account. To quit, type `/exit` and press Enter.

---

## 1. Setup

### 1.1 Prerequisites

| Check | Command | Expected |
|---|---|---|
| Go installed | `go version` | `go1.26` or newer |
| Claude Code installed and logged in | `claude --version` | `2.1.x (Claude Code)` |
| Ollama running (Part F) | `curl -s localhost:11434/api/version` | `{"version":"…"}`. If not: `brew install ollama && brew services start ollama` |
| Suggestion model downloaded (Part F) | `ollama list` | lists `qwen2.5-coder:1.5b`. If not: `ollama pull qwen2.5-coder:1.5b` (~1 GB) |

### 1.2 Build and run the unit tests

From the repository root:

```sh
go test -race ./...
go build -o bin/prompt-engine ./cmd/prompt-engine
```

**Expected:** `ok` for `config`, `input` and `pty`, and a binary at `bin/prompt-engine`.

### 1.3 Two terminals side by side

You need two terminal windows (or split panes) open at the same time. `claude` takes over the whole of the window it runs in, so the log has to be shown somewhere else. With both visible, you type in one and see the log update in the other.

**Easiest setup:** split one window with `claude` on the left and the log on the right.
- **iTerm2 / Ghostty:** Cmd+D splits the window side by side.
- **Terminal.app:** has no split view; open a second window with Cmd+N and put the two next to each other.

**Terminal A (left): where you type.** It runs `prompt-engine` ("the wrapper"), which starts `claude` inside it. From the repository root:

```sh
PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine
```

`claude` fills this window. All test input in Parts B and C is typed here.

**Terminal B (right): watch only.** It shows each line as it's added to the debug log. Any folder works:

```sh
touch /tmp/pe.log && tail -f /tmp/pe.log
```

Don't type anything here; just watch it. Press Ctrl+C to stop it when you're done.

To clear the log between tests, run `: > /tmp/pe.log` in a third tab, or just watch the newest lines.

**Log line format:** two kinds of lines.

```
15:26:30.701 input  "\x1b[97;5u" -> Ctrl+a="\x1b[97;5u"
15:26:30.734 synced=true  cursor=11  "fix the bug"
15:26:31.120 input  "\x1b[O" -> Report="\x1b[O"
```

- `input` lines show the exact bytes your terminal sent (left of `->`), then each key they were decoded as, with that key's own bytes (`Key="bytes"`). Use these to report bugs.
- `Report` keys are terminal replies and events, such as focus out (`\x1b[O`) and in (`\x1b[I`) when you switch windows. They're passed to `claude` but never change the buffer. Mouse movement is not logged.
- `synced` lines show the buffer state after a change. `cursor` counts Unicode code points, not bytes or visible characters: a multi-part emoji like 👍🏽 counts as 2. Several fast changes can show up as a single line.

Claude Code switches the terminal into an enhanced keyboard mode, so many keys arrive as sequences like `\x1b[97;5u` (Ctrl+A) rather than single bytes. That's expected.

### 1.4 Config file (optional)

None is needed for testing; without a file, the built-in defaults are used. Part D creates its own throwaway config files.

For reference, the full config lives at `~/.config/prompt-engine/config.yaml` (or `$XDG_CONFIG_HOME/prompt-engine/config.yaml`, or wherever `PROMPT_ENGINE_CONFIG` points). These are all its settings, with their defaults:

```yaml
command: claude
args: []
env: {}
llm:
  enabled: true      # false turns suggestions off
  provider: ollama   # or fake (fixed word list, for testing)
  endpoint: http://localhost:11434
  model: qwen2.5-coder:1.5b  # must support fill-in-the-middle
  max_tokens: 8      # length of each suggestion
  debounce: 150ms    # pause in typing before asking
  timeout: 2s
panel:          # not used until Phase 4
  visible: false
  side: right
  width: 40
templates:
  dir: templates
keys:           # leader-prefixed shortcuts, like tmux
  leader: ctrl+q  # ctrl+a..z or ctrl+\ ] ^ _; prefer letters, punctuation varies by keyboard layout
  panel: p        # leader, then this key, toggles the side panel
```

---

## 2. Part A: Quick checks without a session

| # | Command (Terminal A) | Expected |
|---|---|---|
| A1 | `bin/prompt-engine --version` | Same output as `claude --version` |
| A2 | `bin/prompt-engine --version \| cat` | Same output, with no stray color codes or garbage (output isn't a terminal, so it runs `claude` directly) |
| A3 | `bin/prompt-engine -p "reply with just: ok" \| cat` | `ok` (or similar), clean text |
| A4 | `echo $?` right after A1 | `0` |

---

## 3. Part B: Behaves exactly like `claude`

Start it in Terminal A:

```sh
PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine
```

| # | Do this | Expected in `claude` |
|---|---|---|
| B1 | Watch it start | Normal `claude` welcome screen, colors and layout intact |
| B2 | Type a short prompt and press Enter (e.g. `say hi in 3 words`) | `claude` answers normally; typing feels instant |
| B3 | Drag the window narrower, then wider | `claude` redraws to the new width with no broken wrapping |
| B4 | Type `/` | Slash-command menu appears; arrows move through it |
| B5 | Run `! echo $PROMPT_ENGINE_ACTIVE` (bash mode) | Prints `1` (the double-wrap guard is set) |
| B6 | Ask for something that needs permission (e.g. `create a file /tmp/pe-test.txt`) | Permission dialog appears; arrows and Enter work |
| B7 | Quit with `/exit` | Back at your shell with a normal prompt; what you type is visible as you type it |
| B8 | `echo $?` | `0` |
| B9 | Start it again, then press Ctrl+C twice to quit | Same behavior as plain `claude`; shell works normally afterwards |
| B10 | Start it again. From another terminal, run `pkill -TERM -f bin/prompt-engine` | `claude` closes, your terminal returns to normal, and `echo $?` is `claude`'s own exit code (often `143`) |

**If the terminal looks broken after any exit** (nothing shows when you type, odd line breaks), that's a bug: run `reset` to recover and report which step it was.

---

## 4. Part C: Input buffer tracking (the log)

Start it as in Part B and keep Terminal B visible. **Don't press Enter** unless the step says to; just watch the log.

| # | Do this in `claude` | Expected last log line |
|---|---|---|
| C1 | Type `fix teh` | `synced=true cursor=7 "fix teh"` |
| C2 | Backspace twice, type `he bug` | `cursor=11 "fix the bug"` |
| C3 | Left arrow 3× | `cursor=8 "fix the bug"` |
| C4 | Type `nasty ` | `cursor=14 "fix the nasty bug"` |
| C5 | Ctrl+A, then Ctrl+E | `cursor=0` → then `cursor=17` |
| C6 | Ctrl+W | `cursor=14 "fix the nasty "` |
| C7 | Ctrl+U | `cursor=0 ""` |
| C8 | Type `café 👍`, then Backspace. Repeat with a multi-part emoji such as 👍🏽, ❤️ or 🇵🇹 | `"café "` with `cursor=5`. One Backspace removes the whole emoji, matching `claude`. |
| C9 | Paste two lines of text from somewhere | The pasted text appears in the log with `\n` between the lines, inserted at the cursor. (`claude` may show a long paste as `[Pasted text #1]`, but the log has the real text; that's expected.) |
| C10 | Press Enter to send | `synced=true cursor=0 ""` |
| C11 | Type `abc`, then Up arrow | `synced=false …` (history changed the box in a way we can't predict) |
| C12 | Press Ctrl+C once | `synced=true cursor=0 ""` (back in sync) |
| C13 | Type `abc`, then Tab | `synced=false` |
| C14 | Ctrl+C, type `line1`, then **Option+Enter**\*, then `line2` | `"line1\nline2"`, still synced |
| C15 | **Untested guess:** type `hello`, then Esc once | Log keeps `"hello"`. **Look at `claude`:** if its box is now empty, the guess was wrong, so report it. |
| C16 | **Untested guess:** type `hello`, then Ctrl+C once | Log shows `""`. **Look at `claude`:** if `hello` is still there, the guess was wrong, so report it. |

\* Option+Enter only sends a newline if your terminal treats Option as Meta (iTerm2: Profiles → Keys → Left Option = Esc+; Terminal.app: "Use Option as Meta key"). If yours doesn't, skip this step.

**What a pass looks like:** whenever `synced=true`, the logged text matches `claude`'s prompt box character for character.

---

## 5. Part D: Config

Checks how `prompt-engine` reads its config file. Each test creates a small config file, runs one command, and compares the output.

- Run everything in Terminal A, from the repository root. No `claude` session or debug log is needed.
- D1–D5 use throwaway files in `/tmp`. `PROMPT_ENGINE_CONFIG` tells `prompt-engine` which file to read, so your real config is never touched.
- The version number shown below (`2.1.308`) will match whatever `claude --version` prints for you.

### D1: No config file → runs on defaults

```sh
PROMPT_ENGINE_CONFIG=/tmp/pe-none.yaml bin/prompt-engine --version
```

**Expected:** just the version, no warning:

```
2.1.308 (Claude Code)
```

### D2: The config is actually used

This config tells `prompt-engine` to run `printenv PE_TEST` instead of `claude`, and sets `PE_TEST=hello`. If the config is read, `hello` is printed. That checks `command`, `args` and `env` in one go.

```sh
printf 'command: printenv\nargs: [PE_TEST]\nenv:\n  PE_TEST: hello\n' > /tmp/pe-env.yaml
PROMPT_ENGINE_CONFIG=/tmp/pe-env.yaml bin/prompt-engine
```

**Expected:**

```
hello
```

### D3: A typo in the config → warning, but still runs

```sh
printf 'comand: claude\n' > /tmp/pe-typo.yaml
PROMPT_ENGINE_CONFIG=/tmp/pe-typo.yaml bin/prompt-engine --version
```

**Expected:** a warning naming the bad key, then the version anyway:

```
prompt-engine: parse /tmp/pe-typo.yaml: yaml: unmarshal errors:
  line 1: field comand not found in type config.Config; using defaults
2.1.308 (Claude Code)
```

### D4: An invalid value → warning, but still runs

```sh
printf 'panel:\n  side: top\n' > /tmp/pe-invalid.yaml
PROMPT_ENGINE_CONFIG=/tmp/pe-invalid.yaml bin/prompt-engine --version
```

**Expected:**

```
prompt-engine: invalid /tmp/pe-invalid.yaml: panel.side must be "left" or "right", got "top"; using defaults
2.1.308 (Claude Code)
```

### D5: The wrapped program doesn't exist → clear error

```sh
printf 'command: does-not-exist\n' > /tmp/pe-missing.yaml
PROMPT_ENGINE_CONFIG=/tmp/pe-missing.yaml bin/prompt-engine; echo "exit=$?"
```

**Expected:**

```
prompt-engine: exec: "does-not-exist": executable file not found in $PATH
exit=127
```

### D6: The default location is found

The only test that uses the real location, `~/.config/prompt-engine/config.yaml`, with no `PROMPT_ENGINE_CONFIG`. Same trick as D2.

> **If you already have a config there**, back it up first: `mv ~/.config/prompt-engine/config.yaml ~/.config/prompt-engine/config.yaml.bak`, and restore it afterwards.

```sh
mkdir -p ~/.config/prompt-engine
printf 'command: printenv\nargs: [PE_TEST]\nenv:\n  PE_TEST: from-default-location\n' > ~/.config/prompt-engine/config.yaml
bin/prompt-engine
```

**Expected:**

```
from-default-location
```

**Then remove it right away**, or `prompt-engine` will keep running `printenv` instead of `claude`:

```sh
rm ~/.config/prompt-engine/config.yaml
```

### Cleanup

```sh
rm -f /tmp/pe-env.yaml /tmp/pe-typo.yaml /tmp/pe-invalid.yaml /tmp/pe-missing.yaml
```

---

## 6. Part E: Ghost text and side panel

Start it with the **fake suggester**, so the expected results below are exact, and with the log open in Terminal B:

```sh
PROMPT_ENGINE_SUGGESTER=fake PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine
```

Between tests, press **Ctrl+U** to clear the prompt.

| # | Do this | Expected |
|---|---|---|
| E1 | Start it | Normal `claude` screen, colors intact, no stray characters. Typing feels as instant as plain `claude`. |
| E2 | Type `please impl` and wait ¼ second | Dimmed `ement` appears right after the cursor; the cursor doesn't move. Log: `suggestion requested`, then `suggestion shown` |
| E3 | Press **Tab** | `please implement` becomes normal text in `claude`'s box. Log: `suggestion accepted "ement"` |
| E4 | Type ` the perf`, wait, press **Right Arrow** | Completes to `performance` |
| E5 | Type ` refa`, wait, press **Esc** | Ghost text disappears, `refa` stays, and `claude` does **not** react to Esc. Log: `suggestion dismissed (esc)` |
| E6 | Type ` docu`, wait, then quickly type `ment` | Ghost text disappears; no grey fragments left |
| E7 | Ctrl+U, type `impl`, then press Left Arrow right away and wait ½ second | No ghost text (the cursor isn't at the end) |
| E8 | Ctrl+U, type `@`, then **Tab** | `claude`'s own file autocomplete works (Tab is only taken while ghost text is showing) |
| E8b | Ctrl+U, type `@`, browse the menu with Up/Down **without choosing**, Backspace the `@`, then type `refa` and wait | Ghost text `ctor` appears. Log: `synced=false` while browsing, then `buffer resynced from screen`, then `suggestion shown` for `refa` |
| E9 | Type a few words, then press **Ctrl+P** | `claude` handles it (moves up / history); prompt-engine doesn't take it |
| E10 | Press **Ctrl+Q**, then **p** | `claude` shrinks to about 60% width; a bordered panel on the right shows the prompt text and the current suggestion. Log: `leader: waiting for a shortcut key`, then `leader: toggle panel` |
| E11 | With the panel open, type `refa` | The panel updates live with the text and the suggestion |
| E12 | With the panel open, resize the window narrower, then wider (drag the window edge or the split-pane divider) | Both sides redraw to fit, no overlapping text |
| E13 | Press **Ctrl+Q**, then **p** again | Back to full width, no leftover border |
| E14 | Press **Ctrl+Q**, then **Esc** | Nothing happens. Log: `leader: cancelled` |
| E15 | Press **Ctrl+Q** twice | A real Ctrl+Q is sent to `claude`. Log: `leader: sent ctrl+q to child` |
| E16 | Quit with `/exit` + Enter, then `echo $?` | Back at a normal shell; typing, arrows and Ctrl+A/E work (keyboard modes restored); exit code `0` |

### E17: No ghost text in dialogs

A trust dialog only appears in a folder `claude` hasn't seen before:

```sh
mkdir -p /tmp/pe-untrusted && cd /tmp/pe-untrusted
PROMPT_ENGINE_SUGGESTER=fake PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log ~/Workspace/GitHub/prompt-engine-cli/bin/prompt-engine
```

When the "Do you trust this folder?" dialog appears, type `impl` and wait. **Don't press Enter.** Quit with **Ctrl+C**, then `cd` back.

**Expected:** no ghost text in the dialog. Log: `suggestion discarded … (prompt changed or cursor not in prompt)`.

### E18: Slow model

```sh
PROMPT_ENGINE_SUGGESTER=fake PROMPT_ENGINE_FAKE_DELAY=400ms PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine
```

Type `expl`, and keep typing `ain` before ghost text appears.

**Expected:** no ghost text for the old word; the log shows the stale request being cancelled or discarded.

### E19: Renderer failure falls back to pass-through (FR-1.3)

```sh
PROMPT_ENGINE_SUGGESTER=fake PROMPT_ENGINE_TEST_RENDER_PANIC=1 PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine
```

Type `refa`. The renderer is made to crash as soon as ghost text is drawn, which happens at `re`.

**Expected:** the screen redraws once with **no** error text or stack trace, and `claude` keeps working without ghost text. Log: `renderer stopped (render panic: …); falling back to pass-through`, and no suggestion lines after it.

### E20: Pass-through switch

```sh
PROMPT_ENGINE_PASSTHROUGH=1 PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine
```

**Expected:** behaves exactly like Phase 1, with no ghost text and no panel. If a glitch in Part E also happens here, the emulator isn't the cause.

---

## 7. Part F: Real suggestions with Ollama

Check the Ollama prerequisites in 1.1 first. Start it normally (no `PROMPT_ENGINE_SUGGESTER`), with the log open in Terminal B:

```sh
PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine
```

The model's wording varies, so judge whether a suggestion is a sensible continuation rather than an exact match.

| # | Do this | Expected |
|---|---|---|
| F1 | Start it and wait a couple of seconds | Log: `llm: qwen2.5-coder:1.5b loaded in …` (about 1 s the first time, much less once loaded) |
| F2 | Type `explain how the input buf` and stop | Within about ¼ second, dimmed text that **finishes the word** and continues, e.g. `fer is used in …`. Log: `suggestion shown … (1xxms)` |
| F3 | Press **Tab** | The suggestion becomes real text. A follow-up suggestion may appear after the next pause |
| F4 | Ctrl+U, type `make the tests pa` and stop | Completes `pass`, not a misspelling |
| F5 | Ctrl+U, type `why does the ghost text ` (ending with a space) and stop | The suggestion starts with a word, with no double space |
| F6 | Type quickly without pausing | No suggestions while typing; one appears only after you stop. Typing never lags |
| F7 | Look at the times in the `suggestion shown` lines | Mostly 50–300 ms |

### F8: Ollama stopped

```sh
brew services stop ollama
PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine
```

Type `explain how the input buf` and stop.

**Expected:** no ghost text, and typing works exactly as normal. Log: `llm: can't use …` at startup, and `suggestion error … connection refused` after each pause.

Then, **without restarting** `prompt-engine`, run `brew services start ollama` in Terminal B, wait a few seconds, and type another word in Terminal A.

**Expected:** suggestions come back (the first may take about a second while the model loads).

### F9: A model without fill-in-the-middle

```sh
ollama pull qwen2.5:1.5b    # skip if already downloaded
printf 'llm:\n  model: qwen2.5:1.5b\n' > /tmp/pe-nofim.yaml
PROMPT_ENGINE_CONFIG=/tmp/pe-nofim.yaml PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine
```

Type `refa` and stop.

**Expected:** no ghost text; typing is normal. Log: `… does not support fill-in-the-middle; use one that does, such as qwen2.5-coder`.

### F10: Suggestions turned off

```sh
printf 'llm:\n  enabled: false\n' > /tmp/pe-off.yaml
PROMPT_ENGINE_CONFIG=/tmp/pe-off.yaml PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine
```

**Expected:** no `llm:` or `suggestion` lines in the log at all. The panel (Ctrl+Q, then p) still works.

**Cleanup:** `rm -f /tmp/pe-nofim.yaml /tmp/pe-off.yaml`, and `brew services start ollama` if you left it stopped.

---

## 8. Known limitations (expected, not bugs)

| What you'll see | Why |
|---|---|
| Keys typed in permission dialogs or menus (e.g. `1`, arrows) briefly show up in the log as prompt text or `synced=false` | The input buffer replays keys. When it loses track, it resyncs from `claude`'s screen within ~150ms (`buffer resynced from screen`), but only for a prompt that fits on one line. Ghost text is kept out of dialogs (E17). |
| **Backslash + Enter** (`claude`'s way to add a new line) resets the log to `""`, while `claude` keeps the text | The buffer treats every Enter as "send". Use Option+Enter for multi-line text while testing. |
| After history, autocomplete or a slash command, a **multi-line or wrapped** prompt stays `synced=false` until Enter or Ctrl+C | Resync from the screen only reads single-line prompts, so a half-read prompt is never trusted. |
| A key pressed in the first few milliseconds after launch is sometimes missing from the log | `claude` still receives it; the terminal just isn't fully switched over yet. |
| iTerm2's own scrollback doesn't show earlier conversation | The emulator draws in the alternate screen. Scroll inside `claude` instead. |
| Suggestions sometimes misspell a half-typed word (e.g. `refa` → `refact`) or drift into code-like text | The 1.5B model has little context about your project yet. Better context (recent prompts, repository details) is planned. |
| Inline images (kitty graphics) aren't supported | The emulator doesn't answer `claude`'s image-support query, so `claude` doesn't send images. |

---

## 9. Reporting a problem

Include:

1. The test number (e.g. C9, E5 or F2)
2. What `claude` showed
3. The matching lines from `/tmp/pe.log`
4. Your terminal app (iTerm2, Terminal.app, Ghostty, …)

**Cleanup:** `rm -rf /tmp/pe.log /tmp/pe-test.txt /tmp/pe-untrusted`
