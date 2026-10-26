# Manual Test Guide

Covers **Phase 1**: the PTY wrapper and input buffer. Extend this guide as later phases land.

**What's being tested:** `prompt-engine` runs `claude` inside its own pseudo-terminal and records what you type. Nothing should look or feel different from running `claude` directly. The only thing you can see is the debug log, which should mirror `claude`'s prompt box.

---

## 1. Setup

### 1.1 Prerequisites

| Check | Command | Expected |
|---|---|---|
| Go installed | `go version` | `go1.26` or newer |
| Claude Code installed and logged in | `claude --version` | `2.1.x (Claude Code)` |

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
15:26:30.701 input  "\x1b[97;5u" -> Ctrl+a
15:26:30.734 synced=true  cursor=11  "fix the bug"
```

- `input` lines show the exact bytes your terminal sent and the key(s) they were decoded as. Use these to report bugs. Mouse movement, focus changes and terminal replies are not logged.
- `synced` lines show the buffer state after a change. `cursor` counts Unicode code points, not bytes or visible characters: a multi-part emoji like 👍🏽 counts as 2. Several fast changes can show up as a single line.

Claude Code switches the terminal into an enhanced keyboard mode, so many keys arrive as sequences like `\x1b[97;5u` (Ctrl+A) rather than single bytes. That's expected.

### 1.4 Optional config file

None is needed; without a file, the built-in defaults are used. To try the config tests in Part D, create `~/.config/prompt-engine/config.yaml`:

```yaml
command: claude
args: []
env: {}
llm:            # not used until Phase 2
  enabled: true
  endpoint: http://localhost:11434
  model: qwen2.5:1.5b
  debounce: 150ms
  timeout: 2s
panel:          # not used until Phase 4
  visible: false
  side: right
  width: 40
templates:
  dir: templates
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

Quit `claude` between each test.

| # | Setup | Run | Expected |
|---|---|---|---|
| D1 | No config file (or rename it) | `bin/prompt-engine` | Starts normally, no warnings |
| D2 | Config with `env: {PE_TEST: hello}` | In `claude`: `! echo $PE_TEST` | `hello` |
| D3 | Config with a typo: `comand: claude` | `bin/prompt-engine --version` | Warning `field comand not found … using defaults`, then the version still prints |
| D4 | Config with `panel: {side: top}` | `bin/prompt-engine --version` | Warning `panel.side must be "left" or "right"…`, then still runs |
| D5 | Config with `command: does-not-exist` | `bin/prompt-engine; echo $?` | `executable file not found in $PATH`, exit `127` |
| D6 | Put a config at `/tmp/alt.yaml` with `env: {PE_TEST: alt}` | `PROMPT_ENGINE_CONFIG=/tmp/alt.yaml bin/prompt-engine`, then `! echo $PE_TEST` | `alt` |

Afterwards, restore or delete the test config.

---

## 6. Known limitations (expected, not bugs)

| What you'll see | Why |
|---|---|
| Keys typed in permission dialogs or menus (e.g. `1`, arrows) show up in the log as prompt text or `synced=false` until the next Enter | The wrapper can't see `claude`'s screen yet. That will come with the Phase 2 screen-reading work. |
| **Backslash + Enter** (`claude`'s way to add a new line) resets the log to `""`, while `claude` keeps the text | The buffer treats every Enter as "send". Use Option+Enter for multi-line text while testing. |
| Picking a slash-command or file suggestion leaves the log `synced=false` | Autocomplete changes the text in ways we can't predict, so this is intended. |
| A key pressed in the first few milliseconds after launch is sometimes missing from the log | `claude` still receives it; the terminal just isn't fully switched over yet. |

---

## 7. Reporting a problem

Include:

1. The test number (e.g. C9)
2. What `claude` showed
3. The matching lines from `/tmp/pe.log`
4. Your terminal app (iTerm2, Terminal.app, Ghostty, …)

**Cleanup:** `rm -f /tmp/pe.log /tmp/pe-test.txt /tmp/alt.yaml`
