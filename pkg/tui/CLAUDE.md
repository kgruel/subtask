# TUI

Bubble Tea TUI. See main CLAUDE.md for project context.

Notable libraries beyond the standard Bubble Tea stack:
- `bubblezone` - click zone tracking (wrap with `zone.Mark()`, scan with `zone.Scan()`)
- `charmbracelet/x/ansi` - ANSI-aware string functions (`StringWidth`, `Truncate`, `Strip`)
- `glamour` - markdown rendering

## File Map

| File | Purpose |
|------|---------|
| `model.go` | Core model struct, Init/Update/View |
| `model_helpers.go` | Resize logic, viewport updates, tab content rendering |
| `view_list.go` | Task list table, search bar, status line |
| `view_detail.go` | Detail header, tab bar, content routing |
| `tabs.go` | Tab enum and titles |
| `styles.go` | All lipgloss styles - add new styles here |
| `mouse.go` | Click handling, double-click detection |
| `mouse_selection.go` | Text selection with drag, clipboard copy |
| `changes_*.go` | Diff tab (render, helpers, tree, loader) |
| `overlay_*.go` | Alert/confirm dialogs |
| `help.go` | Help overlay |
| `status.go` | Status line, floating toast |
| `zones.go` | Zone ID generation for click tracking |
| `actions.go` | Merge/close/abandon commands |

## Architecture

```
viewList (task table)
    │
    └──[Enter]──→ viewDetail
                     ├── tabOverview     (viewport)
                     ├── tabConversation (viewport)
                     ├── tabDiff         (custom split-pane, own scroll)
                     └── tabConflicts    (viewport)
```

Data refreshes every 2s via `tickMsg` → fetch commands → loaded messages.

## Non-Obvious Patterns

**Selection by name, not index** - `selectedTaskName` preserves selection across list refreshes. Always update it when changing `selected`.

**Stale async responses** - Check `msg.taskName == m.selectedTaskName` in message handlers. Diff tab uses `diffLoadID` for additional staleness detection.

**`zone.Scan()` must be last** - Always the final call in View() or clicks won't work.

**`ansi.StringWidth()` not `len()`** - For display width of styled strings. Use `ansi.Truncate()` for safe truncation, `ansi.Strip()` for plain text.

**Overlay input capture** - Update() checks overlays first (alert → confirm → help). When active, they consume all input.

## Diff Tab

The diff tab is the most complex component - it doesn't use a viewport, has its own scroll state, and manages a split-pane layout with file tree + diff content.

Key fields:
- `diffScrollY` - vertical scroll position
- `diffPathSelected` - currently selected file
- `diffDocCache` - LRU cache of parsed diffs (max 12)
- `diffLoadID` - incremented on each load to detect stale responses
- `diffSideBySide` - unified vs side-by-side mode

Loading flow: `fetchDiffFilesCmd()` → file list → `selectDiffPath()` → `fetchDiffDocCmd()` → cache and render.

## Testing

```go
func TestTUI_Feature(t *testing.T) {
    env := testutil.NewTestEnv(t, workspaceCount)
    env.CreateTask(...)

    tm, out := newTestTUI(t)  // disables ticker, inits bubblezone
    waitForContains(t, tm, out, 2*time.Second, "expected text")

    tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
    waitForContains(t, tm, out, 2*time.Second, "result")

    tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}
```

Testability hooks: `disableTicker` prevents background refreshes, `nowFunc` allows time injection.

## Adding Features

**New tab**: Add to `tabs.go` enum, add viewport to model, add case in `updateTabContent()` and `renderDetailContentArea()`.

**New overlay**: Create `overlay_<name>.go`, add state struct to model with `active() bool`, add to View() render chain, add input capture at top of Update().

**New action**: Add to `actionKind` enum in `actions.go`, add key binding in Update(), add to help.go.

## After UI Changes

**ALWAYS verify visually with tmux after making UI changes:**

```bash
go build ./cmd/subtask && tmux new-session -d -s test -x 100 -y 40 './subtask' && sleep 1 && tmux send-keys -t test Enter && sleep 1 && tmux capture-pane -t test -p && tmux kill-session -t test
```

Don't say "done" until you've seen it looks right. Width calculations, alignment, colors - verify them.

**For layout/width math** - trace through with actual numbers before coding. Don't assume lipgloss Width/Padding/Border behavior - test it.
