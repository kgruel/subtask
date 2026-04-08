package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

func TestExternalMergeDetection_WritesTaskMerged(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "test/external-merge"
	env.CreateTask(taskName, "External merge detection", "main", "Detect external merges")

	baseCommit := strings.TrimSpace(gitCmd(t, env.RootDir, "rev-parse", "HEAD"))
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
		{Type: "worker.finished", TS: time.Now().UTC(), Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 0, "tool_calls": 0, "outcome": "replied"})},
	})
	env.CreateTaskState(taskName, &task.State{Workspace: env.Workspaces[0]})

	ws := env.Workspaces[0]
	gitCmd(t, ws, "checkout", "-b", taskName)
	f := filepath.Join(ws, "feature.txt")
	require.NoError(t, os.WriteFile(f, []byte("hello\n"), 0o644))
	gitCmd(t, ws, "add", "feature.txt")
	gitCmd(t, ws, "commit", "-m", "Add feature")
	branchHead := strings.TrimSpace(gitCmd(t, ws, "rev-parse", "HEAD"))

	// Merge branch into base outside of subtask (fast-forward).
	gitCmd(t, env.RootDir, "checkout", "main")
	gitCmd(t, env.RootDir, "merge", "--ff-only", taskName)
	baseHead := strings.TrimSpace(gitCmd(t, env.RootDir, "rev-parse", "main"))
	require.Equal(t, branchHead, baseHead, "fast-forward merge should move main to branch tip")

	subtaskBin := buildSubtask(t)
	cmd := exec.Command(subtaskBin, "show", taskName)
	cmd.Dir = env.RootDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "show should succeed: %s", out)

	tail, err := history.Tail(taskName)
	require.NoError(t, err)
	assert.Equal(t, task.TaskStatusMerged, tail.TaskStatus)

	evs, err := history.Read(taskName, history.ReadOptions{EventsOnly: true})
	require.NoError(t, err)
	var mergedEv history.Event
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type == "task.merged" {
			mergedEv = evs[i]
			break
		}
	}
	require.Equal(t, "task.merged", mergedEv.Type)
	var mergedData struct {
		Via            string `json:"via"`
		Method         string `json:"method"`
		BaseBranch     string `json:"base_branch"`
		BaseCommit     string `json:"base_commit"`
		BranchHead     string `json:"branch_head"`
		BaseHead       string `json:"base_head"`
		ChangesAdded   int    `json:"changes_added"`
		ChangesRemoved int    `json:"changes_removed"`
		CommitCount    int    `json:"commit_count"`
		FrozenError    string `json:"frozen_error"`
	}
	require.NoError(t, json.Unmarshal(mergedEv.Data, &mergedData))
	assert.Equal(t, "detected", mergedData.Via)
	assert.Equal(t, "ancestor", mergedData.Method)
	assert.Equal(t, "main", mergedData.BaseBranch)
	assert.Equal(t, baseCommit, mergedData.BaseCommit)
	assert.Equal(t, branchHead, mergedData.BranchHead)
	assert.Equal(t, baseHead, mergedData.BaseHead)
	assert.Equal(t, 1, mergedData.ChangesAdded)
	assert.Equal(t, 0, mergedData.ChangesRemoved)
	assert.Equal(t, 1, mergedData.CommitCount)
	assert.Empty(t, strings.TrimSpace(mergedData.FrozenError))
}
