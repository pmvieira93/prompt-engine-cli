# Spikes

## Rendering (Phase 2)

Two prototypes of ghost-text rendering were built in parallel and tested against real Claude Code 2.1.308 in iTerm2:

| Report | Approach | Branch (local) |
|---|---|---|
| [overlay.md](overlay.md) | **A. Overlay**: claude's output passes straight through; ghost text is drawn on top with cursor escape codes | `spike/overlay-ghost-text` |
| [vterm.md](vterm.md) | **B. Emulator**: claude's output goes into an in-process terminal emulator (`charmbracelet/x/vt`), rendered by Bubble Tea with ghost text composed in | `spike/vterm-ghost-text` |

### Decision (2026-10-27): B, the emulator

Manual testing found no difference in typing feel, and both passed every shared test. B was chosen because:

1. **Only B can render the FR-5 side panel.** claude erases whole lines and the screen on every frame, so anything drawn beside it in A is wiped.
2. **B can read claude's screen**: it already keeps ghost text out of dialogs, and can resync the input buffer after history, autocomplete, and slash commands instead of giving up until Enter.
3. **A depends on undocumented details of how claude redraws**, so a Claude Code release could silently disable it; B behaves like any terminal.
4. **B works with other terminal CLIs** (Phase 5); A is tuned to claude.

Costs accepted with B: about twice the code, ~22 MB memory vs ~6 MB (target < 30 MB), more CPU while claude streams, and terminal features (clipboard, links, notifications) that must be forwarded to the real terminal.

Shortcuts use a tmux-style leader key, default **Ctrl+Q** (configurable under `keys:`), so every plain Ctrl+<key> still reaches claude.

The reports are kept as written at the time; file and switch names in them may have changed since.

## Rewrites and tokens (2026-11-05)

| Report | Question | Status |
|---|---|---|
| [guard-lines.md](guard-lines.md) | Appendix 4, FR-10.8: does telling the model the guards mean fewer stops? | Done. Decided: lines dropped, `names` false alarms fixed |
| [intent.md](intent.md) | Appendix 6, FR-12: can the rewrite reply carry a reliable intent? | Done. Proposed: use keyword sections, leave the rewrite alone |
| [token-baseline.md](token-baseline.md) | Appendix 5, step 1: where do Claude Code's tokens go? | Partly done |

The first two share the program in `shared-run/`. Their prompts are real, so they stay local and the reports hold totals only.
