---
name: terminal-live-verify
description: Drive a CLI/TUI program inside a real terminal (herdr pane or tmux), capture rendered snapshots and raw byte streams, and verify what actually ships; use it after any TUI rendering change — unit tests and emulators are not a substitute.
---

# Terminal live verify

Drive the program under test inside a controlled real terminal and read back what it actually renders. Use it to verify TUI rendering changes, to match the reference CLI's behavior, and to reproduce problems that only show up on a real terminal (wrapping, cell widths, scrolling, scrollback residue).

## Herdr

```
herdr tab create --label tui-check --cwd "$PWD" --focus    # parse pane_id / tab_id from the JSON
herdr pane run       <pane> "<command>"        # launch (sends the command line plus Enter)
herdr pane send-text <pane> "query text"       # type text
herdr pane send-keys <pane> Enter              # press Enter; send C-c for interrupt
herdr pane read      <pane> --source recent --lines 400    # snapshot
herdr tab close      <tab>                     # cleanup
```

- read sources: `visible` (current screen), `recent` (includes scrollback), `recent-unwrapped` (logical lines).
- C-c must be sent twice: the first press only cancels the running turn, the second exits.
- Clean up when done: close the tab and remove any /tmp scratch files.

## tmux

```
tmux new-session -d -s tui -x 200 -y 50
tmux send-keys -t tui "cmd" Enter
tmux capture-pane -p -t tui -S -300       # negative -S pulls scrollback
tmux resize-window -t tui -x 100 -y 30    # change geometry to reproduce small-screen issues
tmux kill-session -t tui
```

## One terminal or both?

For a quick debug pass, driving the scenario in a single terminal is enough. For compatibility verification of rendering (widths, wrapping, scrollback), run the same scenario in BOTH herdr and tmux and compare the snapshots: the two emulators disagree on ambiguous rune widths, autowrap semantics and tab stops, and a fix that holds in one can still leak in the other.

## Analyzing snapshots

- `recent` snapshots include output from earlier runs: split at the last startup banner line and analyze only the region after it.
- To find leaked or residual rows, count suspect lines (spinners, rules, status rows); they should appear once at the final dock position or not at all.
- To find unwanted wrapping, histogram line lengths; any line wider than the terminal got wrapped.
- Never assume the terminal width; when the exact geometry matters, print fixed-length probe lines and observe where they fold.

## Capturing raw bytes

```
script -q -c "/tmp/nomad-pinned" /tmp/cap.ts
```

`script` wraps the process in a pty and logs every byte the terminal received (including \r\n). Replay cap.ts through the scrollEmu in internal/cli/scrollback_pty_test.go and diff it line by line against the terminal snapshot to locate the first diverging byte. The pty applies ONLCR, so when replaying a stream that contains bare \n, replace \n with \r\n first.

## Matching the reference implementation

Feed the same query to the reference CLI (if a launch alias is configured, start it, wait for the input box, then type the query; interactive turns can be slow — poll read until the end-of-turn marker appears) and to this project's binary, read both panes and compare marker by marker (● / ⎿ / … +N lines and friends).

## Terminal quirks this repository has already hit

- herdr reports East Asian ambiguous runes (─ · … ↓) as one cell over the DSR cursor column but renders them two cells wide: any width calibration based on DSR is untrustworthy, and structural rows must be ASCII.
- Only a real terminal exposes these: stream rewriting wrappers (color downsamplers) swallowing non-SGR escape sequences, stderr logs interleaving with the render stream, tab-stop expansion widths. When exactly N rows leak per cycle, first check whether some process writes to the terminal outside the accounted path.
