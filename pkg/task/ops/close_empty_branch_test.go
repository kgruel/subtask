package ops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/git"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/testutil"
)

func TestCloseTask_DeletesEmptyBranchBestEffort(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "close/empty"
	env.CreateTask(taskName, "Empty branch close", "main", "desc")
	env.CreateTaskState(taskName, &task.State{Workspace: env.Workspaces[0]})
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
	})

	// Create branch with no unique commits.
	require.NoError(t, git.RunSilent(env.Workspaces[0], "checkout", "-b", taskName, "main"))

	_, err := CloseTask(taskName, false, nil)
	require.NoError(t, err)

	require.False(t, git.BranchExists(env.RootDir, taskName), "expected empty branch to be deleted on close")
}

func TestCloseTask_DoesNotDeleteBranchWithCommits(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "close/nonempty"
	env.CreateTask(taskName, "Non-empty branch close", "main", "desc")
	env.CreateTaskState(taskName, &task.State{Workspace: env.Workspaces[0]})
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
	})

	require.NoError(t, git.RunSilent(env.Workspaces[0], "checkout", "-b", taskName, "main"))
	writeFile(t, env.Workspaces[0], "x.txt", "x\n")
	commitAll(t, env.Workspaces[0], "add x")

	_, err := CloseTask(taskName, false, nil)
	require.NoError(t, err)

	require.True(t, git.BranchExists(env.RootDir, taskName), "expected non-empty branch to remain on close")
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	require.NoError(t, git.RunSilent(dir, "add", "."))
	require.NoError(t, git.RunSilent(dir, "commit", "-m", msg))
}
