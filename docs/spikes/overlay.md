# Spike: overlay ghost text (approach A)

**Branch:** `spike/overlay-ghost-text` · **Status:** working prototype · **Date:** 2026-10-26

`claude`'s output still goes straight to the real terminal, byte for byte. Ghost text is drawn on top of it with escape codes, between `claude`'s own redraws. No screen emulation.

## How it works

The approach depends on one observation about how Claude Code 2.1.308 renders. Every redraw is one synchronized-output frame, and the frame ends by moving the real cursor to the input caret with an absolute move, then showing it:

```
ESC[?2026h ESC[?25l … diff … ESC[30;1H ESC[27;4H ESC[?25h ESC[?2026l
                                        ^ caret      ^ cursor shown
```

| Piece | File | Role |
|---|---|---|
| Suggester | `internal/suggest` | `Suggester` interface plus `Fake`, which completes a fixed vocabulary offline, with an optional delay set by `PROMPT_ENGINE_FAKE_DELAY` |
| Screen tracker | `internal/overlay/screen.go` | A minimal parser of `claude`'s output. It tracks whether we're mid-frame or mid-escape-sequence, whether the cursor is visible, and the last absolute cursor position. Anything else that moves the cursor makes the position "unknown". |
| Overlay | `internal/overlay/overlay.go` | The PTY output hook. It forwards output unchanged and draws or erases ghost text only when idle: between frames, with the cursor known and visible. Drawing uses one synchronized update: save cursor, move to the caret, grey text (SGR 90), restore cursor. After each frame the ghost is repainted if the caret hasn't moved, or dropped if it has. |
| Interceptor | `internal/overlay/interceptor.go` | The `pty.Filter`. With a ghost visible, Tab or Right accepts it (the key is replaced by the suggestion text) and Esc dismisses it (the key is swallowed). Any other key erases the ghost and then passes through. Mouse, focus and other terminal reports never dismiss it. The bytes actually forwarded are fed to the `Tracker`, so accepted text shows up in the buffer. |
| Worker | `internal/overlay/worker.go` | Debounces on `Tracker.Subscribe()` (150ms), cancels stale requests, and only asks when the buffer is synced and the caret is at the end. Results are dropped if the buffer changed, or a key was pressed, since the request. |

Staleness is guarded by a keypress counter that is bumped under the overlay lock *before* a key is forwarded. A suggestion requested before a keypress can therefore never be drawn after it.

FR-1.3, fall back to pass-through: a panic in the drawing code disables the overlay, and `claude`'s output keeps flowing. A panic in the suggester is caught in the request goroutine. A panic in the interceptor triggers the existing `pty.Session` fallback, which also erases the ghost and disables the overlay.

## Measurements

All of these use a PTY harness against real `claude` 2.1.308: 100×30 terminal, macOS on Apple Silicon, `Fake` suggester with no delay. The script is in the scratchpad, not the repo.

### 1. Echo delay for normal typing

This is the time from writing one key to receiving the end of `claude`'s redraw frame (`?2026l`). There are 40 keys per run, none of which match the vocabulary, so the full suggestion path runs but no ghost is drawn.

| Setup | Run 1 median / p95 | Run 2 median / p95 |
|---|---|---|
| `claude` directly | 8.09 / 12.46 ms | 8.07 / 14.95 ms |
| Phase 1 pass-through (`llm.enabled: false`) | 7.46 / 14.09 ms | 7.46 / 8.81 ms |
| **Overlay** | **8.29 / 11.12 ms** | **8.41 / 16.18 ms** |

The overlay adds **under 1ms median**, which is within run-to-run noise. Nearly all of the time is `claude`'s own rendering.

### 2. Keystroke → ghost text on screen, excluding the suggester

Over 15 trials: **median 152.4ms** (min 151.0, max 152.5), including the 150ms debounce. So the overlay itself adds **about 2ms**. The suggester returns in about 0.2–0.5ms, per the debug log.

### 3. Memory of the `prompt-engine` process during a `claude` session

**5.8–6.3 MB RSS**, measured with `ps -o rss`. For comparison, `claude` itself is about 350 MB. That's well inside the 30 MB target.

## What works (checked against real `claude` through the harness)

- **Showing:** a ghost appears after a pause: `please impl` → grey `ement` right after the caret.
- **Accepting:** Tab and Right Arrow accept. `claude` receives the text as typed, and the buffer stays in sync (`please implement`).
- **Dismissing:**
  - Esc dismisses and is swallowed, so `claude` keeps the text and isn't interrupted.
  - Typing over a ghost erases it first. Typing `ormanc` at 12ms per key over the visible ghost `ormance` left no stale fragments, and a fresh ghost `e` followed.
- **Right edge:** at the edge, the ghost is cut at a character boundary and leaves the last column free, so there's no auto-wrap. For example, `mentation` became `menta`.
- **Resizing:** the ghost is dropped, with no leftovers in the rendered screen.
- **Spinner redraws:** while `claude` was busy and animating the spinner, the ghost survived each redraw (repainted, not flickering away).
- **Slash menu:** no false ghost on `/he`.
- **Pass-through:** with no ghost visible, Tab and Esc go to `claude` untouched, so `claude`'s own Tab autocomplete still works.

## Known glitches and risks

1. **`claude` doesn't know the ghost is there.** Its renderer only redraws cells it thinks changed. Ghost cells are always erased before a key is forwarded, which keeps this safe today. But if `claude` ever draws something into those cells *without moving the caret*, the ghost repaint would overwrite it, and the erase would then blank it until `claude` next redraws that cell. An example would be a right-aligned hint on the input row. This didn't happen with the current layout, where the cells right of the caret are blank, but any Claude Code UI change could break it. The overlay can't detect it.
2. **It depends on undocumented rendering details.** It relies on Claude Code using synchronized-output frames, ending each frame with an absolute cursor move to the caret, and showing the cursor there. If a future version ends frames with a relative move or keeps the cursor hidden, ghost text silently stops appearing. That fails safe, since nothing is drawn, but the feature is lost. Other CLIs (Phase 5) may not render this way at all.
3. **Resize and caret moves rely on `claude` cleaning up.** When `claude` moves the caret itself, the ghost is dropped *without* being erased, because erasing could wipe `claude`'s new content. That assumes `claude` repaints those cells. The rendered screen was clean in the harness, but that used the `pyte` emulator, not iTerm2, so behavior during iTerm2's resize is unverified.

Smaller issues:
- Only the part of a long suggestion that fits on the line is shown, but accepting inserts all of it.
- Selecting text with the mouse copies ghost text too.
- An erase waits for a gap between frames, which can delay that one keypress by up to 30ms. This was never seen in the measurements.
- Grey (SGR 90) may be hard to read on some themes.

## Pros / cons

| Pros | Cons |
|---|---|
| Small: about 570 lines of Go including the suggester, no new dependencies | Fragile by design: correctness depends on `claude`'s rendering habits |
| No measurable keystroke delay; 6 MB memory | Can't know what's on screen, so it can't avoid `claude`'s content |
| `claude` renders exactly as before; nothing is emulated or rewritten | Only works where the terminal cursor sits at the input caret |
| Fails safe: when unsure, it draws nothing | Doesn't solve the "am I in a dialog?" problem: the cursor being visible is only a hint |

## Verdict

- **Ghost text: viable for the MVP.** It works against Claude Code 2.1.308 with no measurable cost. The risks are about future-proofing rather than correctness today, and it fails safe. It would need a regression check on each Claude Code release (the harness scenario above).
- **FR-5 side panel: not viable as an overlay.** `claude` thinks it owns the full width. It erases to end of line and clears the screen constantly, so anything we draw beside it gets wiped every frame. A *right-hand* panel could be faked by telling `claude` its terminal is narrower and repainting the panel inside each of `claude`'s frames, before the frame-end sequence. That's a hack and it fights `claude` on every redraw. A *left-hand* panel would mean rewriting every column `claude` addresses, which amounts to building a terminal emulator anyway, i.e. approach B.

## Manual test checklist (iTerm2)

**Setup:**
```sh
git checkout spike/overlay-ghost-text
go build -o bin/prompt-engine ./cmd/prompt-engine
```
Terminal A: `PROMPT_ENGINE_DEBUG_LOG=/tmp/pe.log bin/prompt-engine`
Terminal B: `tail -f /tmp/pe.log | grep ghost`

| # | Steps | Expected |
|---|---|---|
| G1 | Type `please impl` and wait | Grey `ement` appears after the caret within about 0.2s. Log: `ghost requested` then `ghost shown` |
| G2 | Press Tab | The text becomes `please implement` in normal color, with the caret after it. Log: `ghost accepted "ement" (Tab)` |
| G3 | Type ` the perf`, wait, then press Right Arrow | `performance` completes. Log: `accepted … (Right)` |
| G4 | Type ` refa`, wait, then press Esc | The ghost disappears, `refa` stays, and `claude` doesn't react to Esc. Log: `dismissed (Esc)` |
| G5 | Type ` docu`, wait, then quickly type `ment` | No grey fragments are left after `document`. Log: `dismissed (typed …)` |
| G6 | With a ghost visible, drag the window narrower | The ghost goes away and no grey text is left behind. Log: `dropped: caret moved` |
| G7 | With no ghost visible, type `@` then Tab | `claude`'s own file autocomplete works as normal |
| G8 | With a ghost visible, move the mouse around and click into Terminal B and back | The ghost stays put |
| G9 | Type `fix tes`, then press Left Arrow | No ghost while the caret isn't at the end of the text |
| G10 | `PROMPT_ENGINE_FAKE_DELAY=400ms bin/prompt-engine`, then type `expl` and keep typing `ain` before the ghost appears | No ghost for the old text. Log: `request canceled (stale)` or `discarded` |
| G11 | Set `llm: {enabled: false}` in the config | No ghost at all; behaves like Phase 1 |

Report any grey text left on screen after a step, together with the matching `ghost` log lines.
