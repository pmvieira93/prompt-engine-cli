# Spike: ghost text via an in-process terminal emulator (approach B)

Branch `spike/vterm-ghost-text`. Prototype, not production code.

**Question:** can `prompt-engine` run `claude` inside its own terminal emulator, draw that screen with Bubble Tea, and add ghost text (and later the FR-5 side panel) on top, within the PRD budgets of **<100ms** latency and **<30MB** memory?

**Short answer:** yes, with caveats. Typing latency goes from about 0.1ms to about 11ms (well under 100ms). Memory stays under 30MB only with two tuning knobs. The side panel works and comes almost for free. And the emulator lets us see `claude`'s screen, which fixes the "keys typed in dialogs" limitation from Phase 1 for ghost text.

---

## 1. How it works

```
stdin ─► pty.Session filter (ghost.Controller) ─► PTY ─► claude
claude ─► PTY ─► x/vt emulator ─► Bubble Tea view (+ ghost text, panel) ─► stdout
emulator replies (CPR, DA2, DECRQM, …) ─► PTY
```

| Piece | Choice | Why |
|---|---|---|
| Emulator | `github.com/charmbracelet/x/vt` v0.1.0 | Maintained by Charm, same cell model (`ultraviolet`) as Bubble Tea v2 and lipgloss v2, so the screen is copied cell by cell with no re-parsing. Handles alt screen, SGR, wide chars and graphemes, scrollback. |
| Renderer | `charm.land/bubbletea/v2` v2.1.0, **render-only** (`WithInput(nil)`) | Input stays on the existing raw stdin pump, so keystrokes reach `claude` byte for byte (kitty CSI u, pastes, mouse). With input off, Bubble Tea also sends no terminal queries of its own, so nothing extra lands in `claude`'s input. |
| Styling | `charm.land/lipgloss/v2` v2.0.6 | Canvas and panel border. |
| Interception | `internal/ghost.Controller` (pty filter) | While a suggestion is visible: Tab or Right accepts it (the suggestion is typed into `claude` instead of the key), Esc dismisses it and is swallowed, and any other key hides it and passes through. With no suggestion, every byte passes through. |
| Suggestions | `internal/suggest` (`Suggester`, `Fake`), `internal/ghost.Worker` | 150ms debounce, stale requests cancelled, a result discarded if the prompt changed since it was requested. |

Dependency versions are pinned; all were published at least two weeks before this spike (x/vt v0.1.0: 2026-10-12, Bubble Tea v2.1.0: 2026-10-08, lipgloss v2.0.6: 2026-08-11).

### Talking to `claude` as its terminal

`claude` now talks to our emulator, not to iTerm2. Three things needed handling:

1. **Queries.**
   - **Answered by the emulator:** cursor position, DA2, mode status and focus events, which have to reflect the emulator's own state.
   - **Forwarded to the real terminal**, with its replies coming back through stdin to `claude`: DA1, XTVERSION and the kitty keyboard flags query. DA1 is forwarded too so the replies stay in order, because `claude` uses DA1 as an "end of replies" marker.
   - **Left unanswered**, so `claude` concludes they're unsupported: kitty graphics.
2. **Keyboard and input modes.** The real terminal has to encode keys the way `claude` asked for, so `claude`'s mode changes are forwarded:
   - kitty keyboard push/pop/set and modifyOtherKeys, sent as raw sequences;
   - cursor-keys mode, sent raw;
   - mouse, focus and bracketed paste, mapped onto Bubble Tea's view settings.

   On exit everything is undone: the alternate screen is left, mouse and paste are turned off, kitty flags are popped and modifyOtherKeys is reset. Verified in the harness.
3. **Newlines.** With no input tty, Bubble Tea assumes the terminal turns `\n` into `\r\n`. `term.MakeRaw` switches that off, so the screen came out as a staircase. The fix: `Session.KeepOutputProcessing` turns OPOST/ONLCR back on. Only Bubble Tea writes to the real terminal in this mode, so that's safe.

### Using the screen: ghost text only at the prompt

In a dialog (for example "trust this folder"), the cursor isn't on the prompt line. Pass-through can't tell the difference, and in the first test ghost text was drawn inside the dialog. With the emulator, `Controller.Gate` only allows a suggestion when the cursor is visible and the first non-blank cell on its row is `claude`'s `❯` prompt marker.

### Fallback (FR-1.3)

If Bubble Tea's renderer dies mid-session (a panic in `View` is recovered by Bubble Tea, which also restores the terminal), the app switches to pass-through:
- `claude`'s output goes straight to the terminal;
- the alternate screen and `claude`'s input modes are re-applied;
- `claude` is resized down and back up so it repaints;
- ghost text is disabled, so Tab can't accept a suggestion nobody can see.

Tested by forcing a panic with `PROMPT_ENGINE_SPIKE_RENDER_PANIC=1`: `claude` repainted, typing continued, exit code 0.

---

## 2. Measurements

All on Apple M4 (macOS), Go 1.27.3, measured with `cmd/vtharness`: a Go PTY driver that types, times when bytes appear in the output, and mirrors the screen into its own x/vt emulator so it can be dumped. "Pass-through" is the Phase 1 build (master `40ccb2b`). `cmd/vtprobe` is a smaller tool that runs a program in x/vt and prints where its cursor is; it's how I checked that `claude` keeps the real cursor at the end of the prompt (needed to place ghost text) in both renderers.

### 2.1 Echo latency (keystroke → echoed character in the terminal output)

150 keystrokes into `cat` (cooked mode), one at a time:

| Path | p50 | p95 | max |
|---|---|---|---|
| `cat` directly, no wrapper | 0.05ms | 0.11ms | 0.25ms |
| Pass-through wrapper (Phase 1) | 0.15ms | 0.27–0.30ms | 1.3–2.7ms |
| vterm, Bubble Tea default 60 FPS | 3–18ms | 19.4ms | 21.3ms |
| **vterm, 120 FPS (final)** | **10.6ms** | **11.1–11.4ms** | **12–21ms** |

The extra time is almost all Bubble Tea's frame tick, not drawing (see 2.3). 120 FPS is Bubble Tea's maximum. Going lower would need a custom renderer that flushes as soon as something changes.

### 2.2 Ghost text latency

From typing `impl` to `ement` appearing on screen, 30 rounds, with the fake suggester (which takes 5–16µs):

| | p50 | p95 | max |
|---|---|---|---|
| Total (includes the 150ms debounce) | 156.1ms | 157.0ms | 157.5ms |
| **Minus debounce = render overhead** | **~6ms** | ~7ms | ~7.5ms |

### 2.3 Full-frame render cost (`go test -bench View ./internal/tui`)

Building one full frame: emulator → canvas → styled string. The screen is filled with 256-color text.

| Size | Time per frame | Allocated per frame |
|---|---|---|
| 80×24 | 0.13ms | 248 KB |
| 200×60 | 0.82ms | 1.55 MB |
| 200×60 + ghost text | 0.82ms | 1.55 MB |
| 200×60 + ghost text + panel | 0.86ms | 1.07 MB |

Bubble Tea's own diff-and-write step comes on top of this, and it's included in 2.1 and 2.2.

### 2.4 Memory and CPU

The PRD budget is **<30 MB RSS** for the `prompt-engine` process alone (not `claude`).

| Scenario | Pass-through | vterm |
|---|---|---|
| Real `claude` session, idle at the prompt | 5.5 MB | **21.6–22.5 MB** |
| While the child streams 200 colored lines/s (heavier than `claude` streaming tokens), default scrollback (10k lines) | 5.5 MB | 58–62 MB ✗ |
| … scrollback capped at 500 lines | — | 32.5 MB ✗ |
| … plus a soft Go memory limit of 28 MB **(final)** | — | **24.6 MB ✓** |
| CPU while streaming (final) | 0.2–0.3% | **14.7–16.7%** of one core |
| CPU idle with a real `claude` session | 0% | 0.6–1.4% |

A memory limit of 20 MB is below the live heap (about 17 MB) and made the GC run constantly: 136% CPU while streaming, 70% while idle. So the 28 MB limit is a knob that needs care, not a fix. The real fix is to stop allocating about 1 MB per frame: reuse buffers, and redraw only changed lines.

---

## 3. What works

Verified against real `claude` (v2.1.308) in the harness:
- **Screen:** both of `claude`'s renderers, classic and fullscreen (alternate screen), with colors, box drawing and the logo intact.
- **Ghost text:**
  - Dimmed text at the cursor; Tab and Right accept, Esc dismisses, typing hides it.
  - The cursor doesn't move while ghost text is shown.
  - Correct placement after wide characters and emoji (`👍🏽 é 中文 refa` → `ctor` drawn right after `refa`).
  - Not shown in dialogs.
- **Resize:** `claude` re-lays out on window resize (100×30 → 70×20).
- **Side panel (Ctrl+Q then p):** `claude` is resized to 60% width and re-lays out (the long path becomes `~/…/`). The panel shows the live buffer, sync state and current suggestion. Ctrl+Q then p again restores full width.
- **Terminal handling:** modes and queries are forwarded, and the terminal is fully restored on exit. Exit codes are passed through.
- **Fallback:** a renderer failure falls back to pass-through without freezing the screen.

---

## 4. Known glitches and risks

1. **Every keystroke now costs about 11ms instead of 0.1ms**, because Bubble Tea redraws on a fixed 120 FPS tick. That's well under 100ms and unlikely to be noticed, but it's a 100× regression over pass-through, and it's the floor as long as Bubble Tea does the rendering.
2. **Memory has little headroom.** Each frame allocates up to 1.5 MB. RSS stays under 30 MB only with a 500-line scrollback cap and a 28 MB soft GC limit, and the limit made the GC thrash when set too low. A larger terminal or longer sessions need the allocation fix, otherwise the budget will be missed.
3. **`claude` isn't talking to the real terminal any more,** so terminal features beyond text and input are degraded or lost:
   - Terminal scrollback is lost, since we draw in the alternate screen. In the classic renderer, earlier conversation can't be scrolled with iTerm2's scrollback; the fullscreen renderer manages its own history.
   - Copy to clipboard (OSC 52), notifications and progress (OSC 9), hyperlinks (OSC 8) and iTerm2 inline images aren't forwarded (untested; probably broken).
   - Color queries (OSC 10/11) return the emulator's defaults, not your real colors, so `claude`'s automatic light/dark theme detection may pick the wrong one.
   - XTVERSION is forwarded, so `claude` believes it's talking to iTerm2 and may send iTerm2-only sequences, which the emulator ignores.

Smaller ones:
- **Shortcuts use a leader key, like tmux** (default Ctrl+Q, configurable under `keys:` in `config.yaml`). Ctrl+Q was picked because it's a letter, so it types the same on every keyboard layout (Ctrl+] needs Option on some layouts and arrives as Ctrl+9), and `claude` doesn't use it; terminal flow control is off in raw mode. The first version took Ctrl+P, which `claude` uses for previous line/history, and the PRD's alternative, Ctrl+D, is `claude`'s exit key. With a leader, every plain Ctrl+<key> reaches `claude`. Pressing the leader twice sends it to `claude`; Esc or any unbound key cancels.
- **The "is the cursor on the prompt?" check depends on `claude`'s current `❯` marker.** A UI change upstream would silently switch ghost text off. It fails safe: no suggestions, never wrong ones.
- **Mouse events over the panel still go to `claude`,** with x-coordinates past `claude`'s width.
- **Not tested in a real iTerm2 window:** only in the PTY harness, which stands in for iTerm2. Real-terminal behaviour (kitty keyboard negotiation, focus, IME, Option-drag selection) needs the manual checklist below.

---

## 5. Pros and cons vs. pass-through + overlay (approach A)

**Pros**
- **Ghost text and the side panel are composed into one coherent frame,** so `claude`'s redraws can't erase or misplace them.
- **FR-5 side panel is nearly free:** resize the child pane, draw beside it. Done in the stub.
- **Screen awareness:** we know where `claude`'s cursor is and what's on its row. That gates ghost text to the real prompt, and could later fix the Phase 1 dialog limitation for the input buffer too.

**Cons**
- **Typing latency:** about 11ms vs 0.1ms.
- **Memory:** about 20–25 MB vs 5.5 MB, with tuning needed to stay under 30 MB.
- **CPU:** about 15% vs 0.3% while output streams.
- **Transparency and complexity:** `claude` no longer talks to the real terminal, so every terminal feature has to be emulated or forwarded. That's an open-ended compatibility surface, with more code and more ways to break.

---

## 6. Manual test checklist (iTerm2)

### Setup

Check out the branch and build:

```sh
git checkout spike/vterm-ghost-text
go build -o bin/prompt-engine ./cmd/prompt-engine
```

You need two terminals side by side: Terminal A runs the wrapper, Terminal B watches the log.

In Terminal A, from a folder you've already trusted in `claude`:

```sh
PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine
```

In Terminal B:

```sh
tail -f /tmp/pe.log
```

Fake suggestions complete the last word you typed from this list: implement, refactor, explain, function, repository, documentation, performance, integration, tests. To simulate a slower model, add `PROMPT_ENGINE_FAKE_DELAY=300ms`.

To compare with the Phase 1 behaviour, run with `PROMPT_ENGINE_PASSTHROUGH=1`.

### Steps

| # | Do this | Expected |
|---|---|---|
| V1 | Start it | Normal `claude` screen, colors intact, no stray characters. Typing feels as instant as plain `claude`. |
| V2 | Type `please impl` and wait ¼ s | Dimmed `ement` right after the cursor; the cursor doesn't move. Log: `suggestion requested` then `suggestion shown`. |
| V3 | Press Tab | `please implement` is now real text in `claude`'s box. Log: `suggestion accepted "ement"`. |
| V4 | Type ` the te`, wait, then press Right Arrow | `tests` completed. Right Arrow accepts like Tab. |
| V5 | Type ` refa`, wait, then press Esc | Ghost text disappears and `claude` does **not** react to the Esc (nothing is interrupted). Log: `dismissed (esc)`. |
| V6 | Type ` expl`, wait, then type `x` | Ghost text disappears and `x` is typed normally. |
| V7 | Ctrl+U to clear, type `impl`, then immediately press Left Arrow and wait ½ s | No suggestion: the cursor isn't at the end of the text. |
| V8 | Press Ctrl+Q, then p | `claude` shrinks to the left about 60% and redraws. A bordered panel on the right shows the buffer and suggestion. Ctrl+Q then p again restores full width. Ctrl+P on its own now reaches `claude` (moves up / history). |
| V9 | Resize the iTerm2 window narrower, then wider | `claude` redraws to fit both times, with the panel open and closed. |
| V10 | Paste multi-line text | Same as plain `claude` (it collapses to `[Pasted text]` the same way). |
| V11 | Shift+Enter / Option+Enter | Inserts a new line, same as plain `claude`. |
| V12 | Scroll with the mouse wheel inside `claude` | Same behaviour as plain `claude` in the same renderer. |
| V13 | Start `claude` in an untrusted folder (trust dialog appears) and type `impl` | **No** ghost text in the dialog. |
| V14 | Quit with `/exit` | Back at the shell. Typed text echoes normally; arrow keys, Ctrl+A and Ctrl+E work in the shell (keyboard modes were restored). `echo $?` is `0`. |
| V15 | Start with `PROMPT_ENGINE_SPIKE_RENDER_PANIC=1` and type `refa` | The screen flickers once and `claude` keeps working without ghost text. Log: `falling back to pass-through`. |

If something looks wrong, note the step and the log lines, and run the same step with `PROMPT_ENGINE_PASSTHROUGH=1` to see whether it's caused by the emulator.

---

## 7. Verdict

| Target | Result |
|---|---|
| <100ms ghost-text latency | ✓ About 6ms of render overhead beyond the debounce. Typing echo about 11ms. |
| <30MB idle footprint | ✓ 20–25 MB, but only with the scrollback cap and the GC soft limit. Headroom is thin until per-frame allocation is reduced. |
| Viable for ghost text | ✓ Placement is robust against redraws, wide characters and dialogs. |
| Viable for the FR-5 side panel | ✓ The stub works now; resizing and re-layout come from the PTY. |

**Recommendation:** viable. It's the only one of the two approaches where the FR-5 panel and screen-aware behaviour come naturally. Before production:
1. Cut per-frame allocation, so memory headroom doesn't depend on a GC limit.
2. Decide which of the lost terminal features (OSC 52, OSC 8, OSC 9, color queries) to forward.
3. Run the iTerm2 checklist above, because every result here comes from a PTY harness, not a real terminal window.
