# Manual Test Guide

Covers **Phase 1** (the PTY wrapper and input buffer, Parts A–D) and **Phase 2** (ghost text and the side panel, Part E; real suggestions from a local model, Part F), and **Phase 3** (rules added to submitted prompts, Part G; config reloaded while running, Part H), and **Phase 4** (prompt expansion, Part I; the prompt diff panel, Part J), and **Phase 4.5** (session context and automatic expansion, Part K; rewriting around pastes, Part L). Extend this guide as later phases land.

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
| Expansion model downloaded (Part I) | `ollama list` | lists `qwen2.5:7b`. If not: `ollama pull qwen2.5:7b` (~4.7 GB) |

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
- The first line of each session, `session start: …`, says which config file was used (or why the defaults were) and whether suggestions are on.
- `synced` lines show the buffer state after a change. `cursor` counts Unicode code points, not bytes or visible characters: a multi-part emoji like 👍🏽 counts as 2. Several fast changes can show up as a single line.

Claude Code switches the terminal into an enhanced keyboard mode, so many keys arrive as sequences like `\x1b[97;5u` (Ctrl+A) rather than single bytes. That's expected.

### 1.4 Config file (optional)

None is needed for testing; without a file, the built-in defaults are used. Part D creates its own throwaway config files.

For reference, the full config lives at `~/.config/prompt-engine/config.yaml` (or `$XDG_CONFIG_HOME/prompt-engine/config.yaml`). `bin/prompt-engine --pe-config <file>` uses another file instead; the flag is removed before the rest of the arguments go to `claude`. `PROMPT_ENGINE_CONFIG=<file>` works too; the flag wins if both are set. These are all its settings, with their defaults:

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
  expand_model: qwen2.5:7b  # rewrites prompts on request (keys.expand)
  expand_timeout: 30s
  debounce: 150ms    # pause in typing before asking
  timeout: 2s
panel:
  visible: false  # open the side panel at startup
  side: right     # or left
  width: 40       # percent of the terminal (10-90)
templates:
  dir: templates
keys:           # leader-prefixed shortcuts, like tmux
  leader: ctrl+q  # ctrl+a..z or ctrl+\ ] ^ _; prefer letters, punctuation varies by keyboard layout
  panel: p        # leader, then this key, toggles the side panel
  expand: e       # leader, then this key, rewrites the prompt in place
  undo: u         # leader, then this key, puts back the prompt from before the last expansion
  panel_wider: "+"     # leader, then this key, widens the panel by 5%
  panel_narrower: "-"  # leader, then this key, narrows it
expand:
  context: prompts    # or none: give the expansion model the last prompts sent, to resolve "it", "same"…
  context_prompts: 3
  on_enter: off       # or auto: Enter expands the prompt, then sends it (guard rails apply)
  min_words: 3        # in auto mode, shorter prompts are sent as typed
  auto_timeout: 5s    # in auto mode, after this the prompt is sent as typed
history:
  enabled: true       # one line of metrics per prompt sent
  include_text: false # also store the prompts themselves
  path: ""            # default: ~/.local/state/prompt-engine/history.jsonl
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
- D1–D5 use throwaway files in `/tmp`. `--pe-config <file>` tells `prompt-engine` which file to read, so your real config is never touched. (`PROMPT_ENGINE_CONFIG=<file>` does the same, but is easy to lose from a command.)
- The version number shown below (`2.1.308`) will match whatever `claude --version` prints for you.

### D1: No config file → runs on defaults

```sh
bin/prompt-engine --pe-config /tmp/pe-none.yaml --version
```

**Expected:** just the version, no warning:

```
2.1.308 (Claude Code)
```

### D2: The config is actually used

This config tells `prompt-engine` to run `printenv PE_TEST` instead of `claude`, and sets `PE_TEST=hello`. If the config is read, `hello` is printed. That checks `command`, `args` and `env` in one go.

```sh
printf 'command: printenv\nargs: [PE_TEST]\nenv:\n  PE_TEST: hello\n' > /tmp/pe-env.yaml
bin/prompt-engine --pe-config /tmp/pe-env.yaml
```

**Expected:**

```
hello
```

### D3: A typo in the config → warning, but still runs

```sh
printf 'comand: claude\n' > /tmp/pe-typo.yaml
bin/prompt-engine --pe-config /tmp/pe-typo.yaml --version
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
bin/prompt-engine --pe-config /tmp/pe-invalid.yaml --version
```

**Expected:**

```
prompt-engine: invalid /tmp/pe-invalid.yaml: panel.side must be "left" or "right", got "top"; using defaults
2.1.308 (Claude Code)
```

### D5: The wrapped program doesn't exist → clear error

```sh
printf 'command: does-not-exist\n' > /tmp/pe-missing.yaml
bin/prompt-engine --pe-config /tmp/pe-missing.yaml; echo "exit=$?"
```

**Expected:**

```
prompt-engine: exec: "does-not-exist": executable file not found in $PATH
exit=127
```

### D6: The default location is found

The only test that uses the real location, `~/.config/prompt-engine/config.yaml`, with no `--pe-config`. Same trick as D2.

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
| E10 | Press **Ctrl+Q**, then **p** | `claude` shrinks to about 60% width; a bordered **Prompt diff** panel appears on the right (Part J covers what it shows). Log: `leader: waiting for a shortcut key`, then `leader: toggle panel` |
| E11 | With the panel open, type `refa` | Ghost text still works next to the panel |
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
PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine --pe-config /tmp/pe-nofim.yaml
```

Type `refa` and stop.

**Expected:** no ghost text; typing is normal. Log: `… does not support fill-in-the-middle; use one that does, such as qwen2.5-coder`.

### F10: Suggestions turned off

```sh
Run these three lines one by one. The first empties the log, so lines from earlier tests can't be mistaken for this one:

```sh
: > /tmp/pe.log
printf 'llm:\n  enabled: false\n' > /tmp/pe-off.yaml
PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine --pe-config /tmp/pe-off.yaml
```

**Expected:** the log starts with `session start: config /tmp/pe-off.yaml; suggestions off (llm.enabled: false)`, and has no `llm:` or `suggestion` lines after it. The panel (Ctrl+Q, then p) still works.

If the first line names a different config, or says `defaults`, the setting didn't reach `prompt-engine`: check that `--pe-config …` is part of the command.

**Cleanup:** `rm -f /tmp/pe-nofim.yaml /tmp/pe-off.yaml`, and `brew services start ollama` if you left it stopped.

---

## 8. Part G: Rules added to your prompts

`prompt-engine` appends the contents of `~/.config/prompt-engine/templates/append.txt` to each prompt you submit, just before the Enter. To make that visible, these tests use a rule whose effect shows up in `claude`'s reply.

> ⚠️ **G1, G4, G5 and G7 send real prompts** to `claude`, so they use a little of your account. Keep the prompts short, as below.

**Setup:**

```sh
mkdir -p ~/.config/prompt-engine/templates
printf -- '- End your reply with the word PINEAPPLE.\n' > ~/.config/prompt-engine/templates/append.txt
PROMPT_ENGINE_SUGGESTER=fake PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine
```

| # | Do this | Expected |
|---|---|---|
| G1 | Type `say hi in three words` and press **Enter** | The prompt briefly shows `[Pasted text #1 +2 lines]` after your text, then `claude` replies, **ending with PINEAPPLE**. Log: `rules: appended append.txt (1 line)` |
| G2 | Type `/help` and press **Enter** | Help opens normally (Esc closes it). Log: `rules: not added (slash command)` |
| G3 | Type `!echo hi` and press **Enter** | Runs as a bash command, as usual. Log: `rules: not added (bash mode)` |
| G4 | Type `say bye in three words`, press **Ctrl+Q**, then **Enter** | `claude` replies **without** PINEAPPLE. Log: `leader: submit as typed` |
| G5 | While it's running, in Terminal B: `printf -- '- End your reply with the word MANGO.\n' > ~/.config/prompt-engine/templates/append.txt`. Then in Terminal A, type `say hi again in three words` and **Enter** | The reply ends with **MANGO**: edits apply without a restart |
| G6 | Type `look at @READ` and stop | `claude`'s file menu appears, and **no** dimmed ghost text from `prompt-engine` appears next to it (Tab belongs to `claude`'s menu). Press **Tab**: `claude` completes it to `@README.md` |
| G7 | Then type ` and say what it is in five words` and press **Enter** | Submitted with the rule: the reply ends with MANGO. Log: `rules: appended append.txt (1 line)`. (Enter with the menu still open also submits, as typed, with the rule) |
| G8 | In Terminal B: `rm ~/.config/prompt-engine/templates/append.txt`. Then type `say ok` and **Enter** | Sent as typed, no `rules:` line in the log. (Skip if you'd rather not send another prompt) |

**Cleanup:** `rm -f ~/.config/prompt-engine/templates/append.txt`. To keep using rules, put your own guidelines in that file.

---

## 9. Part H: Config changes apply while running

`prompt-engine` checks its config file once a second and applies edits to the running session. Nothing here sends a prompt.

**Setup.** In Terminal A, start it with a throwaway config:

```sh
printf 'llm:\n  provider: fake\n' > /tmp/pe-live.yaml
PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine --pe-config /tmp/pe-live.yaml
```

Edit the file from **Terminal B** with the commands below; you can stop `tail -f` with Ctrl+C and restart it after. After each edit, the log shows `config: reloaded /tmp/pe-live.yaml; applied: …; needs a restart: …` within about a second. In Terminal A, press Ctrl+U between steps.

| # | Edit in Terminal B | Then in Terminal A | Expected |
|---|---|---|---|
| H0 | (none) | Press **Ctrl+Q**, then **p** | The panel's **Session** section shows `config: /tmp/pe-live.yaml` and `suggestions: fake (fixed word list)`. If it says `defaults`, the config didn't reach `prompt-engine`: check the `--pe-config` part of the command and restart it. Close the panel again |
| H1 | (none) | Type `refa` and stop | Ghost text `ctor` |
| H2 | `printf 'llm:\n  provider: fake\n  enabled: false\n' > /tmp/pe-live.yaml` | Type `refa` and stop | **No** ghost text. Log: `applied: llm` |
| H3 | `printf 'llm:\n  provider: fake\n  debounce: 1s\n' > /tmp/pe-live.yaml` | Type `refa` and stop | Ghost text comes back, but only about **1 second** after you stop typing |
| H4 | `printf 'llm:\n  provider: fake\nkeys:\n  leader: ctrl+g\n' > /tmp/pe-live.yaml` | Press **Ctrl+G**, then **p**; again to close | The panel opens and closes. Log: `applied: llm, keys`, then `leader: toggle panel`. Ctrl+Q on its own is now just passed to `claude` |
| H5 | `printf 'llm:\n  provider: fake\nkeys:\n  leader: ctrl+g\npanel:\n  width: 60\n' > /tmp/pe-live.yaml` | Ctrl+G, then p | The panel opens noticeably **wider** (60% instead of 40%). Close it again |
| H6 | `printf 'comand: typo\n' > /tmp/pe-live.yaml` | Ctrl+G, then p | The panel still toggles with Ctrl+G: the broken edit was ignored. Log: `config: parse …; keeping the previous settings` |
| H7 | `printf 'command: bash\n' > /tmp/pe-live.yaml` | Keep using it | `claude` keeps running; the command only changes on restart. Log: `needs a restart: command/args/env` |
| H7b | (none) | Ctrl+Q, then p | The Session section says `needs a restart: command/args/env`. Close the panel |
| H8 | `rm /tmp/pe-live.yaml` | Type `explain how the input buf` and stop | Back to the defaults: real suggestions from Ollama, and Ctrl+Q is the leader again. Log: `llm: qwen2.5-coder:1.5b loaded …` |

---

## 10. Part I: Prompt expansion

**Ctrl+Q, then e** rewrites the prompt in `claude`'s input box: a local model fixes typos and spells out shorthand, and `prompt-engine` adds the git branch and staged files underneath. Nothing is sent: you review it and press Enter yourself. **Ctrl+Q, then u** puts the original back. None of these tests press Enter.

**Setup:** check the expansion model in 1.1, then from the repository root:

```sh
PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine
```

The model's wording varies, so judge whether it kept your meaning rather than matching exactly. To clear the prompt between steps, press **Ctrl+C once**: Ctrl+U only clears the current line, and an expanded prompt has several.

| # | Do this | Expected |
|---|---|---|
| I1 | Type `fix teh failing tst in config`, then **Ctrl+Q**, **e** | `⟳ expanding…` appears after the text (no ghost text). Within about a second (several seconds the very first time, while the model loads), the input box shows a corrected prompt, e.g. `Fix the failing test in the config.`, then a blank line, `Context:` and `- Branch: …`. Log: `expand: replaced … (…ms)` |
| I2 | **Ctrl+Q**, **u** | The original `fix teh failing tst in config` is back. Log: `expand: undone` |
| I3 | **Ctrl+Q**, **u** again | Nothing changes. Log: `expand: nothing to undo` |
| I4 | Ctrl+C to clear, then **Ctrl+Q**, **e** on the empty prompt | Nothing happens. Log: `expand: nothing to expand` |
| I5 | Type `add tests for ollama client + handle timeouts + log errs`, **Ctrl+Q**, **e** | All three parts are kept, and nothing is added that you didn't ask for |
| I6 | Ctrl+C, type `refactor tracker to use chans`, **Ctrl+Q**, **e**, then **immediately** type `x` | The expansion is discarded, because you typed meanwhile: your text stays as typed (`…chansx`). Log: `expand: not applied: the prompt changed meanwhile` |
| I7 | Ctrl+C, then type a two-line prompt: `line one`, **Option+Enter**, `line two`. Press **Ctrl+Q**, **e** | Expanded in place, both lines' content kept |
| I8 | Expand a prompt, then press **Ctrl+Q**, **e** again on the result | It's rewritten again with a single, fresh `Context:` block, not a second one or the old one mixed into the request |
| I8b | Ctrl+C, quit with `/exit` | — |

### I9: Staged changes appear in the context

Stage any small change in a repository you don't mind touching, e.g. a scratch one:

```sh
mkdir -p /tmp/pe-repo && cd /tmp/pe-repo && git init -q && printf 'one\ntwo\n' > notes.txt && git add notes.txt
PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log ~/Workspace/GitHub/prompt-engine-cli/bin/prompt-engine
```

`claude` asks whether to trust the folder; accept. Type `explain the notes`, then Ctrl+Q, e.

**Expected:** the context lists `- Staged changes: notes.txt (+2 −0)`. Quit, then `cd -` and `rm -rf /tmp/pe-repo`.

### I10: Your own instructions

```sh
printf 'Rewrite the request as a numbered list of steps. Output only the list.\n' > ~/.config/prompt-engine/templates/system.txt
```

Without restarting, type `set up the project and run the tests`, Ctrl+Q, e.

**Expected:** a numbered list. Then `rm ~/.config/prompt-engine/templates/system.txt` to go back to the built-in instructions.

---

## 11. Part J: Prompt diff panel

The side panel compares what you typed with what `claude` received: additions in **green**, removals in **red, struck through**. While an expansion waits for you to press Enter, it previews it instead.

> ⚠️ **J3 and J5 send real prompts** (short, harmless ones). The rest don't. Only send the prompts given here: `claude` acts on what it receives, and in auto mode a request like "fix the bug" will make it edit your files.

**Setup:** put a rule in place so there's something to see added, remove any custom expansion instructions left from I10, and start it:

```sh
mkdir -p ~/.config/prompt-engine/templates
printf -- '- End your reply with the word MANGO.\n' > ~/.config/prompt-engine/templates/append.txt
rm -f ~/.config/prompt-engine/templates/system.txt
PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine
```

| # | Do this | Expected |
|---|---|---|
| J1 | Press **Ctrl+Q**, then **p** | The **Prompt diff** panel opens on the right, with a key hint line and `Nothing sent yet.` |
| J2 | Type `say helo in thre words`, then **Ctrl+Q**, **e** | Under **Ready to send (not sent yet)**: the typos (`helo`, `thre`) struck through in red, the corrections and the context in green, and the rule (`- End your reply with the word MANGO.`) in green at the end |
| J3 | Press **Enter** | `claude` replies with a short greeting ending in MANGO. The panel title becomes **Last prompt sent**, with the same diff and a line like `13:12:52 · +12 −2 words · expanded · rules added`. Log: `submit: …; expanded=true rules=true` |
| J4 | **Ctrl+Q**, then **-** **-** (one leader, two dashes), then **+** within a second | The panel narrows twice and widens once, about 5% of the window each time; `claude` redraws to fit. After a resize, more `-`/`+` presses within a second keep resizing without Ctrl+Q; a pause or any other key ends that. Nothing is typed into `claude` |
| J4b | Press **Ctrl+Q**, then **Ctrl+P** (Ctrl still held) | The panel toggles, the same as Ctrl+Q then p |
| J5 | Type `say ok in two words` and **Enter** (no expansion) | **Last prompt sent** shows your text unchanged and only the rule in green; the line says `rules added`, not `expanded` |
| J6 | Quit with `/exit`, then `tail -2 ~/.local/state/prompt-engine/history.jsonl` | Two lines with counts like `"raw_words":4,"sent_words":…,"expanded":true,"rules":true`, and **no prompt text** |

### J7: Panel on the left, open at startup

```sh
printf 'panel:\n  side: left\n  visible: true\n' > /tmp/pe-left.yaml
PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine --pe-config /tmp/pe-left.yaml
```

**Expected:** the panel is already open, on the **left**, with `claude` to its right. Typing, ghost text and the cursor all appear in `claude`'s part. If you scroll or click with the mouse inside `claude`'s part, it behaves normally; clicks on the panel do nothing.

**Cleanup:** `rm -f /tmp/pe-left.yaml`, and `rm ~/.config/prompt-engine/templates/append.txt` unless you want to keep the MANGO rule.

---

## 12. Part K: Session context and automatic expansion

Phase 4.5 ([PRD Appendix 1](../PRD-APPENDIX-1.md)): expansion now knows your last few prompts, and can run automatically on Enter.

> ⚠️ **K1, K4, K5, K6 and K7 send real prompts.** They're all harmless (greetings, a joke). Don't substitute coding requests: in auto mode, `claude` acts on them straight away.

**Setup:** a throwaway config, edited from Terminal B as you go (it reloads within a second):

```sh
printf 'expand:\n  on_enter: off\n' > /tmp/pe-auto.yaml
PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine --pe-config /tmp/pe-auto.yaml
```

Open the panel (Ctrl+Q, then p) to see each result. Clear the prompt between steps with **Ctrl+C**.

### Session context

| # | Do this | Expected |
|---|---|---|
| K1 | Send `say hello in three words` (**Enter**). Then type `now say it in portuguese` and press **Ctrl+Q**, **e** (don't send) | The rewrite says what "it" is, e.g. `Say "hello" in three words in Portuguese.` |
| K2 | Ctrl+C, type `what is two plus two`, **Ctrl+Q**, **e** | Rewritten on its own: nothing about hello or Portuguese is added |
| K3 | In Terminal B: `printf 'expand:\n  on_enter: off\n  context: none\n' > /tmp/pe-auto.yaml`. Then Ctrl+C, type `now say it in portuguese`, **Ctrl+Q**, **e** | Without context, the rewrite can't say what "it" is (e.g. `Say it in Portuguese.`) |

### Automatic expansion on Enter

In Terminal B: `printf 'expand:\n  on_enter: auto\n' > /tmp/pe-auto.yaml`

| # | Do this | Expected |
|---|---|---|
| K4 | Type `say helo in thre words` and press **Enter** | `⟳ expanding…` for about a second, then the corrected prompt is sent. The panel: **Last prompt sent** with the corrections in green and `· auto-expanded`. Log: `expand: auto expanded and sent …` |
| K5 | Type `ok thanks` and **Enter** | Sent as typed straight away: it's shorter than `min_words` (3) |
| K6 | Type `tell me a joke abuot cats`, press **Enter**, then **Esc** at once, while `⟳ expanding…` shows | Sent as typed (`abuot` uncorrected). The panel says `Sent as typed: the automatic rewrite wasn't used (you pressed Esc).` |
| K7 | Type `say bye in thre words`, then **Ctrl+Q**, **Enter** | Sent as typed, with no `⟳ expanding…`. The panel shows it with nothing changed |
| K8 | Type `say hi in thre words`, press **Enter**, then **at once** type `x` | Nothing is sent: typing cancels the automatic send, and the prompt reads `say hi in thre wordsx`. Log: `auto expansion cancelled`. Ctrl+C to clear |
| K8b | Turn auto off again (`printf 'expand:\n  on_enter: off\n' > /tmp/pe-auto.yaml`), Ctrl+C, type `now using code-reviewer look into MR !110`, **Ctrl+Q**, **e** (don't send) | `code-reviewer` and `!110` are kept exactly, e.g. `Have the code-reviewer look into MR !110.` If a rewrite ever drops a name like that, your text is kept instead, with a notice saying which name it changed. Turn auto back on for K9: `printf 'expand:\n  on_enter: auto\n' > /tmp/pe-auto.yaml` |
| K9 | Type `/help` and **Enter** | Help opens at once, with no `⟳ expanding…` (slash commands are never expanded). Esc to close |
| K9b | Copy the four lines below (**including** both ```` ``` ```` lines), paste them, then **Enter** | A paste on its own is your prompt, so it's expanded and sent: the sentence may be reworded, the code block arrives exactly as pasted. Log: `expand: auto expanded and sent "…```go…"`. Claude only answers (6: `é` is two bytes) |
| K10 | Quit with `/exit`, then `tail -4 ~/.local/state/prompt-engine/history.jsonl` | K4's line has `"expanded":true,"auto":true`; K6's has `"auto":true,"stopped":"you pressed Esc"`; no prompt text |

K9b's text to paste:

`````
what does this print? just answer, don't change any files
```go
fmt.Println(len("héllo"))
```
`````

### Guard rails (no prompts sent)

These check that a rewrite keeps your meaning. Use the shortcut and **don't send**: turn auto off first (`printf 'expand:\n  on_enter: off\n' > /tmp/pe-auto.yaml`). Clear with Ctrl+C between steps.

| # | Type this, then **Ctrl+Q**, **e** | Expected rewrite |
|---|---|---|
| K11 | `don't touch the tests, just fix the config loadr` | Keeps the negation, possibly reworded (`…without changing the tests`) |
| K12 | `how does the input buffer resync from the screen` | Still a question, not an instruction |
| K13 | `explica como funciona o buffer de entrada` | Still in **Portuguese** (e.g. `Explica como funciona o buffer de entrada.`) |
| K14 | ``fix the error `connection refused: dial tcp [::1]:11434` in the client`` | The part in backticks is kept exactly |

In auto mode, a rewrite that breaks any of these is not used: the prompt is sent as typed, and the log line `expand: auto rewrite "…" not used (…)` gives the reason. The checks themselves are covered by the unit tests (`go test ./internal/expand/`).

**Cleanup:** `rm -f /tmp/pe-auto.yaml`.

---

## 13. Part L: Rewriting around pastes

[PRD Appendix 1](../PRD-APPENDIX-1.md), FR-7.4e: what you type is rewritten; what you paste is kept exactly. A prompt that is only a paste is rewritten, except its code blocks, unless it looks like a log, trace, JSON or diff.

**Setup:** as for Part K, with auto off: `printf 'expand:\n  on_enter: off\n' > /tmp/pe-auto.yaml`. Steps L1–L5 use **Ctrl+Q**, **e** and **don't send**; clear with **Ctrl+C** between them. Copy each paste from the blocks below the table.

| # | Do this | Expected |
|---|---|---|
| L1 | Type `waht does thsi do: `, then paste **A** | Only your words are fixed (`What does this do: …`); the pasted code is unchanged. Log: `expand: replaced …` |
| L2 | Paste **B** only | Rewritten as a prompt: `Refactor the parser to use a lookup table instead of the big switch.` |
| L3 | Paste K9b's four lines only | The sentence is fixed or reworded; the ```` ```go ```` block is unchanged |
| L4 | Paste **C** only | Nothing changes. A notice: `nothing to rewrite: the paste looks like a log, trace, JSON or diff` |
| L5 | Type `see: `, then paste **C** | Nothing changes. A notice: `nothing to rewrite: too little typed around the paste` |
| L6 | Turn auto on (`printf 'expand:\n  on_enter: auto\n' > /tmp/pe-auto.yaml`). Type `what does this print, just answr: `, paste **A**, **Enter** (sends a harmless question) | Your words fixed, the code unchanged, then sent. Log: `expand: Enter held while expanding "what does this print, just answr: <<PASTE_1>>"`, then `auto expanded and sent` |

**A** (one line of code):

```
x := strings.TrimRight(s, "\n")
```

**B** (a request written elsewhere):

```
refactr the paresr to use a lookup tabel insted of the big switch
```

**C** (a Go panic):

```
panic: runtime error: index out of range [3] with length 3

goroutine 1 [running]:
main.main()
	/src/main.go:12 +0x1d
```

In the log, kept parts show as `<<PASTE_1>>`, `<<PASTE_2>>`… in what the model was given. If a rewrite drops or moves one, it isn't used (`kept your text: the rewrite dropped or repeated a pasted part`).

**Cleanup:** `rm -f /tmp/pe-auto.yaml`.

---

## 14. Known limitations (expected, not bugs)

| What you'll see | Why |
|---|---|
| Keys typed in permission dialogs or menus (e.g. `1`, arrows) briefly show up in the log as prompt text or `synced=false` | The input buffer replays keys. When it loses track, it resyncs from `claude`'s screen within ~150ms (`buffer resynced from screen`), but only for a prompt that fits on one line. Ghost text is kept out of dialogs (E17). |
| **Backslash + Enter** (`claude`'s way to add a new line) resets the log to `""`, while `claude` keeps the text | The buffer treats every Enter as "send". Use Option+Enter for multi-line text while testing. |
| After history, autocomplete or a slash command, a **multi-line or wrapped** prompt stays `synced=false` until Enter or Ctrl+C | Resync from the screen only reads single-line prompts, so a half-read prompt is never trusted. |
| A key pressed in the first few milliseconds after launch is sometimes missing from the log | `claude` still receives it; the terminal just isn't fully switched over yet. |
| iTerm2's own scrollback doesn't show earlier conversation | The emulator draws in the alternate screen. Scroll inside `claude` instead. |
| Suggestions sometimes misspell a half-typed word (e.g. `refa` → `refact`) or drift into code-like text | The 1.5B model has little context about your project yet. Better context (recent prompts, repository details) is planned. |
| Editing `command`, `args`, `env` or `templates.dir` while running has no effect until a restart | `claude` is already running with them. The log says `needs a restart: …` |
| A new `panel.width` applies the next time the panel opens or the window resizes | The panel isn't re-laid out mid-display |
| Expansion only uses git context (branch, staged files), not the contents of files or recent commits | Small models given more context invented requirements from it. The context is added by `prompt-engine`, not by the model, so it's always accurate |
| If you edit an expanded prompt before sending it, the panel shows your edited text as "typed" | The panel only knows the original once the prompt still matches the expansion exactly |
| In auto mode, typing while `⟳ expanding…` shows cancels the send, so you press Enter again (and it expands again) | Anything typed after Enter would otherwise go into a prompt that's about to be replaced |
| Around a paste, a typo can come back wrong (seen: `what does this, just answr:` → `just ansr:`, every time) | Typo-only rewrites keep nearly every word, so no word check can tell a bad fix from a good one; with the missing word (`what does this print, just answr:`) it was fixed |
| A paste that you then edit counts as typed text, so it can be rewritten | The kept parts are found by their exact pasted text |
| A long paste that `claude` shows as `[Pasted text #1 +N lines]` is never rewritten if the prompt was re-read from the screen | The screen only has the placeholder, not the text |
| A typo the model misreads as a name can get through in auto mode (e.g. `rfcator tracker` → `the RFCator tracker`) | The rewrite keeps nearly all your words, so no word-based check can tell it from a good typo fix. Use the shortcut and review when a prompt has heavy typos |
| Session context only includes your prompts, not `claude`'s replies | Level L1 of PRD Appendix 1. Replies (L2) and session summaries (L3) are later candidates |
| Rules are only appended, not prepended | Adding text at the start means moving the cursor through multi-line prompts, which is fragile. Prepending is planned once appending is proven. |
| Rules aren't added when the prompt text is unknown (`synced=false`, e.g. a multi-line prompt after history) | Pasting at the end needs the exact cursor position. The log says `rules: not added (prompt text unknown)`. |
| Inline images (kitty graphics) aren't supported | The emulator doesn't answer `claude`'s image-support query, so `claude` doesn't send images. |

---

## 15. Reporting a problem

Include:

1. The test number (e.g. C9, E5, F2, G1, H3, I1, J2 or K4)
2. What `claude` showed
3. The matching lines from `/tmp/pe.log`
4. Your terminal app (iTerm2, Terminal.app, Ghostty, …)

**Cleanup:** `rm -rf /tmp/pe.log /tmp/pe-test.txt /tmp/pe-untrusted`
