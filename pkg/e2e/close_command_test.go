package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

func TestCloseCommand_FreezesStats(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "test/close"
	env.CreateTask(taskName, "Test close command", "main", "Test close")

	baseCommit := strings.TrimSpace(gitCmd(t, env.RootDir, "rev-parse", "HEAD"))
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
	})

	env.CreateTaskState(taskName, &task.State{Workspace: env.Workspaces[0]})

	ws := env.Workspaces[0]
	gitCmd(t, ws, "checkout", "-b", taskName)
	f := filepath.Join(ws, "feature.txt")
	require.NoError(t, os.WriteFile(f, []byte("line 1\nline 2\n"), 0o644))
	gitCmd(t, ws, "add", "feature.txt")
	gitCmd(t, ws, "commit", "-m", "Add feature")
	branchHead := strings.TrimSpace(gitCmd(t, ws, "rev-parse", "HEAD"))

	subtaskBin := buildSubtask(t)
	cmd := exec.Command(subtaskBin, "close", taskName)
	cmd.Dir = env.RootDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "close should succeed: %s", out)

	evs, err := history.Read(taskName, history.ReadOptions{EventsOnly: true})
	require.NoError(t, err)
	var closedEv history.Event
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type == "task.closed" {
			closedEv = evs[i]
			break
		}
	}
	require.Equal(t, "task.closed", closedEv.Type)
	var closedData struct {
		Reason         string `json:"reason"`
		BaseBranch     string `json:"base_branch"`
		BaseCommit     string `json:"base_commit"`
		BranchHead     string `json:"branch_head"`
		ChangesAdded   int    `json:"changes_added"`
		ChangesRemoved int    `json:"changes_removed"`
		CommitCount    int    `json:"commit_count"`
		FrozenError    string `json:"frozen_error"`
	}
	require.NoError(t, json.Unmarshal(closedEv.Data, &closedData))
	assert.Equal(t, "close", closedData.Reason)
	assert.Equal(t, "main", closedData.BaseBranch)
	assert.Equal(t, baseCommit, closedData.BaseCommit)
	assert.Equal(t, branchHead, closedData.BranchHead)
	assert.Equal(t, 2, closedData.ChangesAdded)
	assert.Equal(t, 0, closedData.ChangesRemoved)
	assert.Equal(t, 1, closedData.CommitCount)
	assert.Empty(t, strings.TrimSpace(closedData.FrozenError))

	state, err := task.LoadState(taskName)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Empty(t, state.Workspace)
}
