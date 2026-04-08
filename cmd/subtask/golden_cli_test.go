package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/render"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/task/migrate/gitredesign"
	"github.com/kgruel/subtask/pkg/testutil"
	"github.com/kgruel/subtask/pkg/workflow"
	"github.com/kgruel/subtask/pkg/workspace"
)

func withFixedNow(t *testing.T, now time.Time) {
	t.Helper()
	prev := nowFunc
	nowFunc = func() time.Time { return now }
	t.Cleanup(func() { nowFunc = prev })
}

func withOutputMode(t *testing.T, pretty bool) {
	t.Helper()

	prevPretty := render.Pretty
	prevProfile := lipgloss.ColorProfile()

	render.Pretty = pretty
	if pretty {
		lipgloss.SetColorProfile(termenv.ANSI256)
	} else {
		lipgloss.SetColorProfile(termenv.Ascii)
	}

	t.Cleanup(func() {
		render.Pretty = prevPretty
		lipgloss.SetColorProfile(prevProfile)
	})
}

func captureStdoutStderr(t *testing.T, fn func() error) (string, string, error) {
	t.Helper()

	origOut := os.Stdout
	origErr := os.Stderr

	rOut, wOut, err := os.Pipe()
	require.NoError(t, err)
	rErr, wErr, err := os.Pipe()
	require.NoError(t, err)

	type readResult struct {
		b   []byte
		err error
	}
	outCh := make(chan readResult, 1)
	errCh := make(chan readResult, 1)

	go func() {
		b, err := io.ReadAll(rOut)
		outCh <- readResult{b: b, err: err}
	}()
	go func() {
		b, err := io.ReadAll(rErr)
		errCh <- readResult{b: b, err: err}
	}()

	restore := func() {
		os.Stdout = origOut
		os.Stderr = origErr
	}

	os.Stdout = wOut
	os.Stderr = wErr
	defer restore()

	runErr := fn()

	// Restore stdout/stderr before closing the pipes so other goroutines/tests don't
	// attempt to write to a closed pipe.
	restore()

	require.NoError(t, wOut.Close())
	require.NoError(t, wErr.Close())

	stdoutRes := <-outCh
	stderrRes := <-errCh
	require.NoError(t, stdoutRes.err)
	require.NoError(t, stderrRes.err)

	return string(stdoutRes.b), string(stderrRes.b), runErr
}

func writeProgressJSON(t *testing.T, taskName string, json string) {
	t.Helper()
	path := filepath.Join(task.Dir(taskName), "PROGRESS.json")
	require.NoError(t, os.WriteFile(path, []byte(strings.TrimSpace(json)+"\n"), 0o644))
}

func overwriteWorkspaceReadme(t *testing.T, workspacePath string, content string) {
	t.Helper()
	path := filepath.Join(workspacePath, "README.md")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func mustHistoryOpen(t *testing.T, baseBranch string) []history.Event {
	t.Helper()
	baseCommit := gitCmdOutput(t, ".", "rev-parse", "HEAD")
	return []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": baseBranch, "base_commit": baseCommit})},
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
	}
}

func TestGolden_List_Empty(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	withFixedNow(t, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))

	for _, pretty := range []bool{false, true} {
		t.Run(modeName(pretty), func(t *testing.T) {
			withOutputMode(t, pretty)

			stdout, stderr, err := captureStdoutStderr(t, (&ListCmd{}).Run)
			require.NoError(t, err)
			require.Empty(t, stderr)
			testutil.AssertGoldenOutput(t, "testdata/list/empty", stdout)
		})
	}
}

func TestGolden_List_DefaultClosedFill_ZeroNonClosed(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	withFixedNow(t, now)

	// Create 12 closed tasks, with 12 being most recent.
	for i := 1; i <= 12; i++ {
		taskName := fmt.Sprintf("a/closed%02d", i)
		env.CreateTask(taskName, fmt.Sprintf("Closed %02d", i), "main", "Closed description")
		ts := now.Add(-time.Duration(12-i) * time.Minute)
		env.CreateTaskHistory(taskName, []history.Event{
			{Type: "task.opened", TS: ts.Add(-time.Second), Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")})},
			{Type: "task.closed", TS: ts, Data: mustJSON(map[string]any{"reason": "close"})},
		})
	}

	for _, pretty := range []bool{false, true} {
		t.Run(modeName(pretty), func(t *testing.T) {
			withOutputMode(t, pretty)

			stdout, stderr, err := captureStdoutStderr(t, (&ListCmd{}).Run)
			require.NoError(t, err)
			require.Empty(t, stderr)
			testutil.AssertGoldenOutput(t, "testdata/list/closed_fill_0_non_closed", stdout)
		})
	}
}

func TestGolden_List_DefaultClosedFill_FiveNonClosed(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	withFixedNow(t, now)

	// Create closed tasks that sort before open tasks by name.
	for i := 1; i <= 12; i++ {
		taskName := fmt.Sprintf("a/closed%02d", i)
		env.CreateTask(taskName, fmt.Sprintf("Closed %02d", i), "main", "Closed description")
		ts := now.Add(-time.Duration(12-i) * time.Minute)
		env.CreateTaskHistory(taskName, []history.Event{
			{Type: "task.opened", TS: ts.Add(-time.Second), Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")})},
			{Type: "task.closed", TS: ts, Data: mustJSON(map[string]any{"reason": "close"})},
		})
	}

	// Create 5 non-closed tasks.
	for i := 1; i <= 5; i++ {
		taskName := fmt.Sprintf("z/open%02d", i)
		env.CreateTask(taskName, fmt.Sprintf("Open %02d", i), "main", "Open description")
		ts := now.Add(-time.Duration(5-i) * time.Second)
		env.CreateTaskHistory(taskName, []history.Event{
			{Type: "task.opened", TS: ts, Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")})},
			{Type: "stage.changed", TS: ts, Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
		})
	}

	for _, pretty := range []bool{false, true} {
		t.Run(modeName(pretty), func(t *testing.T) {
			withOutputMode(t, pretty)

			stdout, stderr, err := captureStdoutStderr(t, (&ListCmd{}).Run)
			require.NoError(t, err)
			require.Empty(t, stderr)
			testutil.AssertGoldenOutput(t, "testdata/list/closed_fill_5_non_closed", stdout)
		})
	}
}

func TestGolden_List_DefaultClosedFill_TwelveNonClosed(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	withFixedNow(t, now)

	// Create some closed tasks that should not be shown (12 non-closed already exceeds 10).
	for i := 1; i <= 3; i++ {
		taskName := fmt.Sprintf("a/closed%02d", i)
		env.CreateTask(taskName, fmt.Sprintf("Closed %02d", i), "main", "Closed description")
		ts := now.Add(-time.Duration(3-i) * time.Minute)
		env.CreateTaskHistory(taskName, []history.Event{
			{Type: "task.opened", TS: ts.Add(-time.Second), Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")})},
			{Type: "task.closed", TS: ts, Data: mustJSON(map[string]any{"reason": "close"})},
		})
	}

	// Create 12 non-closed tasks.
	for i := 1; i <= 12; i++ {
		taskName := fmt.Sprintf("z/open%02d", i)
		env.CreateTask(taskName, fmt.Sprintf("Open %02d", i), "main", "Open description")
		ts := now.Add(-time.Duration(12-i) * time.Second)
		env.CreateTaskHistory(taskName, []history.Event{
			{Type: "task.opened", TS: ts, Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")})},
			{Type: "stage.changed", TS: ts, Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
		})
	}

	for _, pretty := range []bool{false, true} {
		t.Run(modeName(pretty), func(t *testing.T) {
			withOutputMode(t, pretty)

			stdout, stderr, err := captureStdoutStderr(t, (&ListCmd{}).Run)
			require.NoError(t, err)
			require.Empty(t, stderr)
			testutil.AssertGoldenOutput(t, "testdata/list/closed_fill_12_non_closed", stdout)
		})
	}
}

func TestGolden_List_SingleTask(t *testing.T) {
	env := testutil.NewTestEnv(t, 2)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	withFixedNow(t, now)

	taskName := "demo/single"
	env.CreateTask(taskName, "Single task", "main", "Description")
	env.CreateTaskState(taskName, &task.State{
		Workspace: env.Workspaces[0],
	})
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", TS: now.Add(-50 * time.Minute), Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")})},
		{Type: "stage.changed", TS: now.Add(-49 * time.Minute), Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
		{Type: "worker.finished", TS: now.Add(-45 * time.Minute), Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 0, "tool_calls": 0, "outcome": "replied"})},
	})
	env.CreateTaskProgress(taskName, &task.Progress{
		ToolCalls:  7,
		LastActive: now.Add(-10 * time.Minute),
	})
	writeProgressJSON(t, taskName, `
[
  {"step":"Write tests","done":true},
  {"step":"Update snapshots","done":false}
]`)
	overwriteWorkspaceReadme(t, env.Workspaces[0], "# Test Repo\nline one\nline two\n")
	gitCmd(t, env.Workspaces[0], "add", "README.md")
	gitCmd(t, env.Workspaces[0], "commit", "-m", "Update README")

	for _, pretty := range []bool{false, true} {
		t.Run(modeName(pretty), func(t *testing.T) {
			withOutputMode(t, pretty)

			stdout, stderr, err := captureStdoutStderr(t, (&ListCmd{}).Run)
			require.NoError(t, err)
			require.Empty(t, stderr)
			testutil.AssertGoldenOutput(t, "testdata/list/single", stdout)
		})
	}
}

func TestGolden_List_MultiStatus(t *testing.T) {
	env := testutil.NewTestEnv(t, 4)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	withFixedNow(t, now)

	baseCommit := gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")

	// a/open (idle)
	env.CreateTask("a/draft", "Draft task", "main", "Draft description")
	env.CreateTaskHistory("a/draft", []history.Event{
		{Type: "task.opened", TS: now.Add(-10 * time.Minute), Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", TS: now.Add(-10 * time.Minute), Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
	})

	// b/open + running
	env.CreateTask("b/working", "Working task", "main", "Working description")
	env.CreateTaskState("b/working", &task.State{
		Workspace:     env.Workspaces[0],
		SupervisorPID: os.Getpid(),
		StartedAt:     now.Add(-2 * time.Minute),
	})
	env.CreateTaskHistory("b/working", []history.Event{
		{Type: "task.opened", TS: now.Add(-9 * time.Minute), Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", TS: now.Add(-9 * time.Minute), Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
	})
	env.CreateTaskProgress("b/working", &task.Progress{
		ToolCalls:  3,
		LastActive: now.Add(-30 * time.Second),
	})
	writeProgressJSON(t, "b/working", `
[
  {"step":"Investigate","done":false},
  {"step":"Fix","done":false}
]`)
	overwriteWorkspaceReadme(t, env.Workspaces[0], "# Test Repo\nworking change\n")
	gitCmd(t, env.Workspaces[0], "add", "README.md")
	gitCmd(t, env.Workspaces[0], "commit", "-m", "Worker change")

	// c/replied (with context)
	env.CreateTask("c/replied", "Replied task", "main", "Replied description")
	loaded, err := task.Load("c/replied")
	require.NoError(t, err)
	loaded.FollowUp = "ctx/base"
	require.NoError(t, loaded.Save())

	env.CreateTaskState("c/replied", &task.State{
		Workspace: env.Workspaces[1],
	})
	env.CreateTaskHistory("c/replied", []history.Event{
		{Type: "task.opened", TS: now.Add(-2 * time.Hour), Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", TS: now.Add(-90 * time.Minute), Data: mustJSON(map[string]any{"from": "implement", "to": "review"})},
		{Type: "worker.finished", TS: now.Add(-90 * time.Minute), Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 0, "tool_calls": 0, "outcome": "replied"})},
	})
	env.CreateTaskProgress("c/replied", &task.Progress{
		ToolCalls:  12,
		LastActive: now.Add(-2 * time.Hour),
	})
	writeProgressJSON(t, "c/replied", `
[
  {"step":"Write plan","done":true},
  {"step":"Implement","done":true},
  {"step":"Review","done":false}
]`)
	overwriteWorkspaceReadme(t, env.Workspaces[1], "one\ntwo\nthree\n")
	gitCmd(t, env.Workspaces[1], "add", "README.md")
	gitCmd(t, env.Workspaces[1], "commit", "-m", "Replied changes")

	// d/error
	env.CreateTask("d/error", "Error task", "main", "Error description")
	env.CreateTaskState("d/error", &task.State{
		Workspace: env.Workspaces[3],
		LastError: "something went wrong",
	})
	env.CreateTaskHistory("d/error", []history.Event{
		{Type: "task.opened", TS: now.Add(-3 * time.Hour), Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", TS: now.Add(-3 * time.Hour), Data: mustJSON(map[string]any{"from": "implement", "to": "review"})},
		{Type: "worker.finished", TS: now.Add(-3 * time.Hour), Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 0, "tool_calls": 0, "outcome": "error"})},
	})

	// e/merged (workspace freed)
	env.CreateTask("e/closed", "Closed task", "main", "Closed description")
	env.CreateTaskState("e/closed", &task.State{
		Workspace: env.Workspaces[2],
	})
	env.CreateTaskHistory("e/closed", []history.Event{
		{Type: "task.opened", TS: now.Add(-24 * time.Hour), Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", TS: now.Add(-23 * time.Hour), Data: mustJSON(map[string]any{"from": "", "to": "ready"})},
		{Type: "task.merged", TS: now.Add(-23 * time.Hour), Data: mustJSON(map[string]any{"commit": baseCommit, "into": "main"})},
	})

	for _, pretty := range []bool{false, true} {
		t.Run(modeName(pretty), func(t *testing.T) {
			withOutputMode(t, pretty)

			stdout, stderr, err := captureStdoutStderr(t, (&ListCmd{All: true}).Run)
			require.NoError(t, err)
			require.Empty(t, stderr)
			testutil.AssertGoldenOutput(t, "testdata/list/multi_status", stdout)
		})
	}
}

func TestGolden_Show_Draft(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	withFixedNow(t, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))

	taskName := "show/draft"
	require.NoError(t, (&task.Task{
		Name:        taskName,
		Title:       "Draft task",
		BaseBranch:  "main",
		Description: "Draft description",
		Schema:      gitredesign.TaskSchemaVersion,
	}).Save())
	require.NoError(t, workflow.CopyToTask("default", taskName))
	require.NoError(t, history.WriteAll(taskName, mustHistoryOpen(t, "main")))

	for _, pretty := range []bool{false, true} {
		t.Run(modeName(pretty), func(t *testing.T) {
			withOutputMode(t, pretty)

			stdout, stderr, err := captureStdoutStderr(t, (&ShowCmd{Task: taskName}).Run)
			require.NoError(t, err)
			require.Empty(t, stderr)
			testutil.AssertGoldenOutput(t, "testdata/show/draft", stdout)
		})
	}
}

func TestGolden_Show_RepliedWithProgressAndDiff(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	withFixedNow(t, now)

	// Use a stable, short workspace path so pretty output box widths are deterministic.
	gitCmd(t, env.RootDir, "worktree", "add", "--detach", "ws1")
	overwriteWorkspaceReadme(t, "ws1", "# Test Repo\none\ntwo\nthree\nfour\n")
	gitCmd(t, "ws1", "add", "README.md")
	gitCmd(t, "ws1", "commit", "-m", "Replied changes")

	taskName := "show/replied"
	env.CreateTask(taskName, "Replied task", "main", "Replied description")
	require.NoError(t, workflow.CopyToTask("default", taskName))
	require.NoError(t, os.WriteFile(filepath.Join(task.Dir(taskName), "PLAN.md"), []byte("# Plan\n"), 0o644))
	writeProgressJSON(t, taskName, `
[
  {"step":"First step","done":true},
  {"step":"Second step","done":false},
  {"step":"Third step","done":false}
]`)

	env.CreateTaskState(taskName, &task.State{
		Workspace: "ws1",
	})
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", TS: now.Add(-4 * time.Hour), Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")})},
		{Type: "stage.changed", TS: now.Add(-3 * time.Hour), Data: mustJSON(map[string]any{"from": "implement", "to": "review"})},
		{Type: "worker.finished", TS: now.Add(-3 * time.Hour), Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 0, "tool_calls": 0, "outcome": "replied"})},
	})

	for _, pretty := range []bool{false, true} {
		t.Run(modeName(pretty), func(t *testing.T) {
			withOutputMode(t, pretty)

			stdout, stderr, err := captureStdoutStderr(t, (&ShowCmd{Task: taskName}).Run)
			require.NoError(t, err)
			require.Empty(t, stderr)
			testutil.AssertGoldenOutput(t, "testdata/show/replied", stdout)
		})
	}
}

func TestGolden_Show_ModelReasoning(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	withFixedNow(t, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))

	cfg, err := workspace.LoadConfig()
	require.NoError(t, err)
	cfg.Adapter = "codex"
	cfg.Model = "gpt-5.2"
	cfg.Reasoning = "high"
	require.NoError(t, cfg.Save())

	taskName := "show/model-reasoning"
	env.CreateTask(taskName, "Model reasoning task", "main", "Description")
	env.CreateTaskHistory(taskName, mustHistoryOpen(t, "main"))

	for _, pretty := range []bool{false, true} {
		t.Run(modeName(pretty), func(t *testing.T) {
			withOutputMode(t, pretty)

			stdout, stderr, err := captureStdoutStderr(t, (&ShowCmd{Task: taskName}).Run)
			require.NoError(t, err)
			require.Empty(t, stderr)
			testutil.AssertGoldenOutput(t, "testdata/show/model_reasoning", stdout)
		})
	}
}

func TestGolden_Errors(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	withFixedNow(t, now)

	taskName := "err/working"
	env.CreateTask(taskName, "Working task", "main", "Working description")
	env.CreateTaskState(taskName, &task.State{
		Workspace:     env.Workspaces[0],
		SupervisorPID: os.Getpid(),
		StartedAt:     now.Add(-1 * time.Minute),
	})
	env.CreateTaskHistory(taskName, mustHistoryOpen(t, "main"))

	for _, pretty := range []bool{false, true} {
		t.Run(modeName(pretty), func(t *testing.T) {
			withOutputMode(t, pretty)

			_, _, err := captureStdoutStderr(t, (&CloseCmd{Task: taskName}).Run)
			require.Error(t, err)
			testutil.AssertGoldenOutput(t, "testdata/errors/close_working", err.Error()+"\n")

			_, _, err = captureStdoutStderr(t, (&ShowCmd{Task: "no/such-task"}).Run)
			require.Error(t, err)
			testutil.AssertGoldenOutput(t, "testdata/errors/show_not_found", err.Error()+"\n")
		})
	}
}

func modeName(pretty bool) string {
	if pretty {
		return "pretty"
	}
	return "plain"
}
