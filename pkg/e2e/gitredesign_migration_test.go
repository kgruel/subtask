package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/testutil"
)

func TestGitRedesignMigration_BackfillsBaseCommit(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "legacy/nobasecommit"
	require.NoError(t, (&task.Task{
		Name:        taskName,
		Title:       "Legacy task missing base_commit",
		BaseBranch:  "main",
		Description: "Legacy",
		Schema:      1,
	}).Save())
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
	})

	// Create the task branch so the migration can infer merge-base.
	ws := env.Workspaces[0]
	gitCmd(t, ws, "checkout", "-b", taskName)

	wantBaseCommit := strings.TrimSpace(gitCmd(t, env.RootDir, "rev-parse", "main"))

	subtaskBin := buildSubtask(t)
	cmd := exec.Command(subtaskBin, "list")
	cmd.Dir = env.RootDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "list should succeed: %s", out)

	evs, err := history.Read(taskName, history.ReadOptions{EventsOnly: true})
	require.NoError(t, err)

	var opened history.Event
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type == "task.opened" {
			opened = evs[i]
			break
		}
	}
	require.Equal(t, "task.opened", opened.Type)

	var d struct {
		BaseBranch string `json:"base_branch"`
		BaseCommit string `json:"base_commit"`
		BaseRef    string `json:"base_ref"`
	}
	require.NoError(t, json.Unmarshal(opened.Data, &d))
	require.Equal(t, "main", d.BaseBranch)
	require.Equal(t, "main", d.BaseRef)
	require.Equal(t, wantBaseCommit, d.BaseCommit)
}

func TestGitRedesignMigration_LegacyTaskShowsApplied(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "legacy/applied"
	require.NoError(t, (&task.Task{
		Name:        taskName,
		Title:       "Legacy task applied",
		BaseBranch:  "main",
		Description: "Legacy applied",
		Schema:      1,
	}).Save())
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})}, // legacy: no base_commit
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 0, "tool_calls": 0, "outcome": "replied"})},
	})
	env.CreateTaskState(taskName, &task.State{Workspace: env.Workspaces[0]})

	// Create branch + commit in workspace.
	ws := env.Workspaces[0]
	gitCmd(t, ws, "checkout", "-b", taskName)
	f := filepath.Join(ws, "feature.txt")
	require.NoError(t, os.WriteFile(f, []byte("hello\n"), 0o644))
	gitCmd(t, ws, "add", "feature.txt")
	gitCmd(t, ws, "commit", "-m", "Add feature")

	// Apply the same change to main via a different commit (squash-like).
	mainFile := filepath.Join(env.RootDir, "feature.txt")
	require.NoError(t, os.WriteFile(mainFile, []byte("hello\n"), 0o644))
	gitCmd(t, env.RootDir, "add", "feature.txt")
	gitCmd(t, env.RootDir, "commit", "-m", "Apply feature")

	subtaskBin := buildSubtask(t)
	cmd := exec.Command(subtaskBin, "list")
	cmd.Dir = env.RootDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "list should succeed: %s", out)
	require.Contains(t, string(out), "applied (+1 -0)")

	// Ensure the opened event was backfilled with base_commit.
	evs, err := history.Read(taskName, history.ReadOptions{EventsOnly: true})
	require.NoError(t, err)

	var opened history.Event
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type == "task.opened" {
			opened = evs[i]
			break
		}
	}
	require.Equal(t, "task.opened", opened.Type)

	var d struct {
		BaseCommit string `json:"base_commit"`
	}
	require.NoError(t, json.Unmarshal(opened.Data, &d))
	require.NotEmpty(t, strings.TrimSpace(d.BaseCommit))
}
