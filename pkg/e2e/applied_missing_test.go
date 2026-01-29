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

func TestAppliedContentDetection_ShowsAppliedAndMergeNoOps(t *testing.T) {
	run := func(t *testing.T, force string) {
		env := testutil.NewTestEnv(t, 1)

		taskName := "test/applied"
		env.CreateTask(taskName, "Applied task", "main", "Applied")
		baseCommit := strings.TrimSpace(gitCmd(t, env.RootDir, "rev-parse", "HEAD"))
		env.CreateTaskHistory(taskName, []history.Event{
			{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
			{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
			{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 0, "tool_calls": 0, "outcome": "replied"})},
		})

		// Create task state (simulating a task that has been run)
		state := &task.State{
			Workspace: env.Workspaces[0],
		}
		env.CreateTaskState(taskName, state)

		// Create workspace with task branch and a commit.
		ws := env.Workspaces[0]
		gitCmd(t, ws, "checkout", "-b", taskName)

		featureFile := filepath.Join(ws, "feature.txt")
		require.NoError(t, os.WriteFile(featureFile, []byte("line 1\n"), 0644))
		gitCmd(t, ws, "add", "feature.txt")
		gitCmd(t, ws, "commit", "-m", "Add feature")

		// Simulate a squash merge (or independently-applied change) into main with a different commit.
		mainFile := filepath.Join(env.RootDir, "feature.txt")
		require.NoError(t, os.WriteFile(mainFile, []byte("line 1\n"), 0644))
		gitCmd(t, env.RootDir, "add", "feature.txt")
		gitCmd(t, env.RootDir, "commit", "-m", "Apply feature via squash")

		subtaskBin := buildSubtask(t)

		// List should show "applied (+A -R)" for plain output.
		cmd := exec.Command(subtaskBin, "list")
		cmd.Dir = env.RootDir
		cmd.Env = append(os.Environ(), "SUBTASK_MERGE_SIM_FORCE="+force)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "list should succeed: %s", out)
		require.Contains(t, string(out), "applied (+1 -0)")

		// Show should explain that content is already in base.
		cmd = exec.Command(subtaskBin, "show", taskName)
		cmd.Dir = env.RootDir
		cmd.Env = append(os.Environ(), "SUBTASK_MERGE_SIM_FORCE="+force)
		out, err = cmd.CombinedOutput()
		require.NoError(t, err, "show should succeed: %s", out)
		require.Contains(t, string(out), "Already in base branch. Run `subtask merge` to mark as merged.")

		// Merge should no-op (main already contains the change) but still mark the task as merged.
		mainBefore := strings.TrimSpace(gitCmd(t, env.RootDir, "rev-parse", "main"))

		cmd = exec.Command(subtaskBin, "merge", taskName, "-m", "Merge applied task")
		cmd.Dir = env.RootDir
		cmd.Env = append(os.Environ(), "SUBTASK_MERGE_SIM_FORCE="+force)
		out, err = cmd.CombinedOutput()
		require.NoError(t, err, "merge should succeed: %s", out)

		mainAfter := strings.TrimSpace(gitCmd(t, env.RootDir, "rev-parse", "main"))
		require.Equal(t, mainBefore, mainAfter, "merge should not create a new main commit when content is already in base")

		tail, err := history.Tail(taskName)
		require.NoError(t, err)
		require.Equal(t, task.TaskStatusMerged, tail.TaskStatus)

		events, err := history.Read(taskName, history.ReadOptions{EventsOnly: true})
		require.NoError(t, err)
		var mergedEv history.Event
		for i := len(events) - 1; i >= 0; i-- {
			if events[i].Type == "task.merged" {
				mergedEv = events[i]
				break
			}
		}
		require.Equal(t, "task.merged", mergedEv.Type)
		var mergedData struct {
			Via            string `json:"via"`
			Method         string `json:"method"`
			ChangesAdded   int    `json:"changes_added"`
			ChangesRemoved int    `json:"changes_removed"`
			CommitCount    int    `json:"commit_count"`
		}
		require.NoError(t, json.Unmarshal(mergedEv.Data, &mergedData))
		require.Equal(t, "subtask", mergedData.Via)
		require.True(t, mergedData.Method == "merge_adds_nothing" || mergedData.Method == "trees_match", "unexpected method: %q", mergedData.Method)
		require.Equal(t, 1, mergedData.ChangesAdded)
		require.Equal(t, 0, mergedData.ChangesRemoved)
		require.Equal(t, 1, mergedData.CommitCount)
	}

	t.Run("merge-tree", func(t *testing.T) {
		if !mergeTreeWriteTreeSupported() {
			t.Skip("git merge-tree --write-tree not supported")
		}
		run(t, "merge-tree")
	})
	t.Run("index", func(t *testing.T) {
		run(t, "index")
	})
}

func mergeTreeWriteTreeSupported() bool {
	cmd := exec.Command("git", "merge-tree", "-h")
	out, _ := cmd.CombinedOutput() // -h exits non-zero
	s := string(out)
	return strings.Contains(s, "--write-tree") && strings.Contains(s, "--merge-base") && strings.Contains(s, "--name-only")
}

func TestMissingBranch_ShowsMissingInListAndShow(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "test/missing"
	env.CreateTask(taskName, "Missing branch task", "main", "Missing")
	baseCommit := strings.TrimSpace(gitCmd(t, env.RootDir, "rev-parse", "HEAD"))
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 0, "tool_calls": 0, "outcome": "replied"})},
	})

	// Create a branch (not checked out in a worktree) and then delete it to simulate an external delete.
	gitCmd(t, env.RootDir, "checkout", "-b", taskName)
	require.NoError(t, os.WriteFile(filepath.Join(env.RootDir, "missing.txt"), []byte("x\n"), 0644))
	gitCmd(t, env.RootDir, "add", "missing.txt")
	gitCmd(t, env.RootDir, "commit", "-m", "Add missing file")
	gitCmd(t, env.RootDir, "checkout", "main")
	gitCmd(t, env.RootDir, "branch", "-D", taskName)

	subtaskBin := buildSubtask(t)

	cmd := exec.Command(subtaskBin, "list")
	cmd.Dir = env.RootDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "list should succeed: %s", out)

	found := false
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), taskName+" ") {
			found = true
			require.Contains(t, line, "missing")
			break
		}
	}
	require.True(t, found, "expected list output to include task %q", taskName)

	cmd = exec.Command(subtaskBin, "show", taskName)
	cmd.Dir = env.RootDir
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, "show should succeed: %s", out)
	require.Contains(t, string(out), "Changes: missing")
	require.Contains(t, string(out), "Branch was deleted or commit objects are missing.")
}
