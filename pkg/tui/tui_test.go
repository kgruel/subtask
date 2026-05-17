package tui

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
	zone "github.com/lrstanley/bubblezone"
)

func TestTUI_HeadlessLaunchAndQuit(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	tm, out := newTestTUI(t)
	waitForOutput(t, tm, out, 2*time.Second, func(s string) bool {
		return strings.Contains(s, "No tasks yet")
	})

	tm.Type("q")
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestTUI_Navigation_ListToDetailAndBack(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	baseCommit := gitRevParse(t, ".", "main")

	env.CreateTask("task1", "Task 1", "main", "First task")
	env.CreateTaskHistory("task1", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 1000, "tool_calls": 0, "outcome": "replied"})},
	})

	env.CreateTask("task2", "Task 2", "main", "Second task")
	env.CreateTaskHistory("task2", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r2", "duration_ms": 1000, "tool_calls": 0, "outcome": "replied"})},
	})

	tm, out := newTestTUI(t)

	waitForContains(t, tm, out, 2*time.Second, "task1")
	waitForContains(t, tm, out, 2*time.Second, "task2")

	// Open the selected task (list is sorted by recent activity).
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	waitForContains(t, tm, out, 2*time.Second, "Overview")
	waitForContains(t, tm, out, 2*time.Second, "task2")

	// Wait for the overview description to render, then back to list.
	waitForContains(t, tm, out, 2*time.Second, "Second task")
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitForContains(t, tm, out, 2*time.Second, "navigate")

	tm.Type("q")
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestTUI_ConversationAndDiffTabsRender(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "fix/ui"
	baseCommit := gitRevParse(t, ".", "main")

	env.CreateTask(taskName, "UI Task", "main", "desc")
	env.CreateTaskState(taskName, &task.State{
		Workspace: env.Workspaces[0],
	})
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
		{Type: "message", Role: "lead", Content: "Hello from lead"},
		{Type: "message", Role: "worker", Content: "Hello from worker"},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 1000, "tool_calls": 0, "outcome": "replied"})},
	})

	// Create a committed change for diff tab.
	ws := env.Workspaces[0]
	runGit(t, ws, "checkout", "-b", taskName)
	if err := os.WriteFile(filepath.Join(ws, "file.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, ws, "add", ".")
	runGit(t, ws, "commit", "-m", "Add file")

	tm, out := newTestTUI(t)
	waitForContains(t, tm, out, 2*time.Second, taskName)

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForContains(t, tm, out, 2*time.Second, "Overview")

	// Conversation tab ([2]).
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	waitForContains(t, tm, out, 2*time.Second, "Hello from lead")

	// Artifacts tab ([3]).
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	waitForContains(t, tm, out, 2*time.Second, "Artifacts")

	// Changes tab ([4] after renumbering).
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	waitForContains(t, tm, out, 2*time.Second, "file.txt")
	waitForContains(t, tm, out, 2*time.Second, "hello")

	tm.Type("q")
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestTUI_Conversation_ShowsWorkerErrorReason(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	taskName := "fix/worker-error"
	baseCommit := gitRevParse(t, ".", "main")

	env.CreateTask(taskName, "Error Task", "main", "desc")
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 1000, "tool_calls": 0, "outcome": "error", "error_message": "codex failed: exit status 1"})},
	})

	tm, out := newTestTUI(t)
	waitForContains(t, tm, out, 2*time.Second, taskName)

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForContains(t, tm, out, 2*time.Second, "Overview")

	// Conversation tab ([2]) should show worker error reason.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	waitForContains(t, tm, out, 2*time.Second, "Worker error:")
	waitForContains(t, tm, out, 2*time.Second, "codex failed: exit status 1")

	tm.Type("q")
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestTUI_Actions_MergeAndAbandon(t *testing.T) {
	t.Run("merge", func(t *testing.T) {
		env := testutil.NewTestEnv(t, 1)

		taskName := "fix/merge"
		baseCommit := gitRevParse(t, ".", "main")

		env.CreateTask(taskName, "Merge Title", "main", "desc")
		env.CreateTaskState(taskName, &task.State{
			Workspace: env.Workspaces[0],
		})
		env.CreateTaskHistory(taskName, []history.Event{
			{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
			{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
			{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 1000, "tool_calls": 0, "outcome": "replied"})},
		})

		ws := env.Workspaces[0]
		runGit(t, ws, "checkout", "-b", taskName)
		if err := os.WriteFile(filepath.Join(ws, "hello.txt"), []byte("hello\n"), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		runGit(t, ws, "add", ".")
		runGit(t, ws, "commit", "-m", "Add hello")

		tm, out := newTestTUI(t)
		waitForContains(t, tm, out, 2*time.Second, taskName)

		tm.Send(tea.KeyMsg{Type: tea.KeyCtrlG})
		waitForContains(t, tm, out, 2*time.Second, "Merge "+taskName+"? (y/n)")

		tm.Type("y")
		waitForContains(t, tm, out, 10*time.Second, "Merged "+taskName)

		tm.Type("q")
		tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))

		st, err := task.LoadState(taskName)
		if err != nil || st == nil {
			t.Fatalf("load state: %v", err)
		}
		if st.Workspace != "" {
			t.Fatalf("expected workspace cleared, got %q", st.Workspace)
		}

		tail, err := history.Tail(taskName)
		if err != nil {
			t.Fatalf("tail history: %v", err)
		}
		if tail.TaskStatus != task.TaskStatusMerged {
			t.Fatalf("expected merged task status, got %q", tail.TaskStatus)
		}
		if _, err := os.Stat("hello.txt"); err != nil {
			t.Fatalf("expected hello.txt on main worktree: %v", err)
		}
	})

	t.Run("abandon", func(t *testing.T) {
		env := testutil.NewTestEnv(t, 1)

		taskName := "fix/abandon"
		baseCommit := gitRevParse(t, ".", "main")

		env.CreateTask(taskName, "Abandon Title", "main", "desc")
		env.CreateTaskState(taskName, &task.State{
			Workspace: env.Workspaces[0],
		})
		env.CreateTaskHistory(taskName, []history.Event{
			{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
			{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
			{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 1000, "tool_calls": 0, "outcome": "replied"})},
		})

		env.MakeDirty(0)

		tm, out := newTestTUI(t)
		waitForContains(t, tm, out, 2*time.Second, taskName)

		tm.Send(tea.KeyMsg{Type: tea.KeyCtrlX})
		waitForContains(t, tm, out, 2*time.Second, "Abandon "+taskName+"? Discards changes. (y/n)")

		tm.Type("y")
		waitForContains(t, tm, out, 5*time.Second, "Abandoned "+taskName+".")

		tm.Type("q")
		tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))

		st, err := task.LoadState(taskName)
		if err != nil || st == nil {
			t.Fatalf("load state: %v", err)
		}
		if st.Workspace != "" {
			t.Fatalf("expected workspace cleared, got %q", st.Workspace)
		}

		tail, err := history.Tail(taskName)
		if err != nil {
			t.Fatalf("tail history: %v", err)
		}
		if tail.TaskStatus != task.TaskStatusClosed {
			t.Fatalf("expected closed task status, got %q", tail.TaskStatus)
		}
		if !testutil.IsClean(t, env.Workspaces[0]) {
			t.Fatalf("expected clean workspace after abandon")
		}
	})
}

func TestTUI_Artifacts_EnterViewModeAndEsc(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	taskName := "fix/artifacts-view"
	baseCommit := gitRevParse(t, ".", "main")

	env.CreateTask(taskName, "Artifact Test", "main", "desc")
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "artifact.produced", Data: mustJSON(map[string]any{"name": "report.md", "path": "report.md", "kind": "review"})},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 1000, "tool_calls": 0, "outcome": "replied"})},
	})

	// Create the actual artifact file.
	taskDir := task.Dir(taskName)
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "report.md"), []byte("# Hello\nThis is a report.\n"), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	tm, out := newTestTUI(t)
	waitForContains(t, tm, out, 2*time.Second, taskName)

	// Open detail view.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForContains(t, tm, out, 2*time.Second, "Overview")

	// Switch to Artifacts tab and wait for list to load.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	waitForOutput(t, tm, out, 2*time.Second, func(s string) bool {
		return strings.Contains(s, "▶ report.md")
	})

	// Press Enter to open view mode. Wait for rendered file body — this string only
	// appears after the async fetch completes and the viewport renders in view mode.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForContains(t, tm, out, 2*time.Second, "This is a report")

	// Press Esc to return to list mode — should NOT exit to task list.
	// After Esc, we're still in detail view (Esc from list mode exits; Esc from view mode goes back to list).
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitForOutput(t, tm, out, 2*time.Second, func(s string) bool {
		// After Esc from view mode, we're back in list mode (Artifacts tab still active).
		// Press Esc again to exit detail, then look for list footer.
		return strings.Contains(s, "▶ report.md") // list mode restored
	})

	// Press Esc to exit detail.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitForContains(t, tm, out, 2*time.Second, "navigate")

	tm.Type("q")
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestTUI_Artifacts_CopyContentToast(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	taskName := "fix/artifacts-copy"
	baseCommit := gitRevParse(t, ".", "main")

	env.CreateTask(taskName, "Artifact Copy Test", "main", "desc")
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "artifact.produced", Data: mustJSON(map[string]any{"name": "output.txt", "path": "output.txt", "kind": "review"})},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 1000, "tool_calls": 0, "outcome": "replied"})},
	})

	taskDir := task.Dir(taskName)
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	content := "hello from artifact\n"
	if err := os.WriteFile(filepath.Join(taskDir, "output.txt"), []byte(content), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	tm, out := newTestTUI(t)
	waitForContains(t, tm, out, 2*time.Second, taskName)

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForContains(t, tm, out, 2*time.Second, "Overview")

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	// Wait for the Artifacts tab to load (bullet "▶" only appears in the list view).
	waitForOutput(t, tm, out, 2*time.Second, func(s string) bool {
		return strings.Contains(s, "▶ output.txt")
	})

	// Press y to copy content — should show "Copied N bytes" toast.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	waitForContains(t, tm, out, 2*time.Second, "Copied")

	tm.Type("q")
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func newTestTUI(t *testing.T) (*teatest.TestModel, *bytes.Buffer) {
	t.Helper()

	zone.NewGlobal()
	t.Cleanup(func() {
		zone.Close()
		zone.DefaultManager = nil
	})

	m := newModel()
	m.disableTicker = true

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
	return tm, &bytes.Buffer{}
}

func waitForContains(t *testing.T, tm *teatest.TestModel, out *bytes.Buffer, timeout time.Duration, substr string) {
	t.Helper()
	waitForOutput(t, tm, out, timeout, func(s string) bool {
		return strings.Contains(s, substr)
	})
}

func waitForOutput(t *testing.T, tm *teatest.TestModel, out *bytes.Buffer, timeout time.Duration, cond func(s string) bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, _ = io.ReadAll(io.TeeReader(tm.Output(), out))
		s := ansi.Strip(out.String())
		if cond(s) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout after %s\n\nlast output:\n%s", timeout, ansi.Strip(out.String()))
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func gitRevParse(t *testing.T, dir string, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
