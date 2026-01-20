package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	zone "github.com/lrstanley/bubblezone"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/testutil"
)

func TestTUI_Selection_OverviewRightPane_NoSidebarBleed(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	baseCommit := gitRevParse(t, ".", "main")

	taskName := "task1"
	leftText := "LEFTTEXT"
	env.CreateTask(taskName, "Task 1", "main", leftText+"\n"+leftText)
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
	})

	var copied string
	oldClipboardWrite := clipboardWrite
	clipboardWrite = func(text string) error { copied = text; return nil }
	t.Cleanup(func() { clipboardWrite = oldClipboardWrite })

	tm, out := newTestTUI(t)
	waitForContains(t, tm, out, 2*time.Second, taskName)

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForContains(t, tm, out, 2*time.Second, "Overview")
	waitForContains(t, tm, out, 2*time.Second, leftText)
	waitForContains(t, tm, out, 2*time.Second, "Details")

	waitForOutput(t, tm, out, 2*time.Second, func(string) bool {
		zi := zone.Get(zoneOverviewPane())
		return zi != nil && !zi.IsZero()
	})
	zi := zone.Get(zoneOverviewPane())
	x := zi.EndX - 1
	y := zi.StartY + 1

	tm.Send(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
	tm.Send(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x, Y: y + 1})
	tm.Send(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: x, Y: y + 1})

	waitForContains(t, tm, out, 2*time.Second, "Copied")
	if copied == "" {
		t.Fatalf("expected clipboard text to be captured")
	}
	if strings.Contains(copied, leftText) {
		t.Fatalf("expected copied text to exclude left pane content; got %q", copied)
	}

	tm.Type("q")
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestTUI_ClickVsDrag_DragDoesNotTriggerClick(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	baseCommit := gitRevParse(t, ".", "main")

	env.CreateTask("task1", "Task 1", "main", "First task")
	env.CreateTaskHistory("task1", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
	})
	env.CreateTask("task2", "Task 2", "main", "Second task")
	env.CreateTaskHistory("task2", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r2", "duration_ms": 1000, "tool_calls": 0, "outcome": "replied"})},
	})

	var copied string
	oldClipboardWrite := clipboardWrite
	clipboardWrite = func(text string) error { copied = text; return nil }
	t.Cleanup(func() { clipboardWrite = oldClipboardWrite })

	tm, out := newTestTUI(t)
	waitForContains(t, tm, out, 2*time.Second, "task1")
	waitForContains(t, tm, out, 2*time.Second, "task2")

	waitForOutput(t, tm, out, 2*time.Second, func(string) bool {
		zi := zone.Get(zoneTaskRow("task1"))
		return zi != nil && !zi.IsZero()
	})
	zi1 := zone.Get(zoneTaskRow("task1"))
	zi2 := zone.Get(zoneTaskRow("task2"))

	// Click task1 (no drag) to select it.
	x1, y1 := zi1.StartX+1, zi1.StartY
	tm.Send(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x1, Y: y1})
	tm.Send(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: x1, Y: y1})

	// Drag on task2 to trigger selection/copy; this must not change the list selection.
	x2, y2 := zi2.StartX+1, zi2.StartY
	tm.Send(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x2, Y: y2})
	tm.Send(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x2 + 2, Y: y2})
	tm.Send(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: x2 + 2, Y: y2})

	waitForContains(t, tm, out, 2*time.Second, "Copied")
	if copied == "" {
		t.Fatalf("expected clipboard text to be captured")
	}

	// Enter should still open task1 if the drag didn't trigger a click/select on task2.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForContains(t, tm, out, 2*time.Second, "task1")
	waitForContains(t, tm, out, 2*time.Second, "First task")

	tm.Type("q")
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}
