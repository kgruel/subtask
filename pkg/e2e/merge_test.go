package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/testutil"
)

// TestMergeCommand verifies that subtask merge properly squashes, rebases,
// updates main, deletes the branch, and marks the task as merged.
func TestMergeCommand_NoOriginRemote(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	// Create a task
	taskName := "test/merge"
	env.CreateTask(taskName, "Test merge command", "main", "Test merge")
	env.CreateTaskHistory(taskName, []history.Event{{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})}})

	// Create task state (simulating a task that has been run)
	state := &task.State{
		Workspace: env.Workspaces[0],
	}
	env.CreateTaskState(taskName, state)

	// Create workspace with task branch and commits
	ws := env.Workspaces[0]
	gitCmd(t, ws, "checkout", "-b", taskName)

	// Add multiple commits to test squashing
	featureFile := filepath.Join(ws, "feature.txt")
	os.WriteFile(featureFile, []byte("line 1\n"), 0644)
	gitCmd(t, ws, "add", "feature.txt")
	gitCmd(t, ws, "commit", "-m", "Add feature part 1")

	os.WriteFile(featureFile, []byte("line 1\nline 2\n"), 0644)
	gitCmd(t, ws, "add", "feature.txt")
	gitCmd(t, ws, "commit", "-m", "Add feature part 2")

	// Verify we have 2 commits on the branch
	commits := gitCmd(t, ws, "rev-list", "--count", "main.."+taskName)
	assert.Equal(t, "2\n", commits, "should have 2 commits before merge")

	// Capture main tip for fast-forward assertion.
	mainBefore := strings.TrimSpace(gitCmd(t, env.RootDir, "rev-parse", "main"))

	// Build subtask command
	subtaskBin := buildSubtask(t)

	// Run merge command
	cmd := exec.Command(subtaskBin, "merge", taskName, "-m", "Merge test feature")
	cmd.Dir = env.RootDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "merge should succeed: %s", output)

	// Verify merge output
	assert.Contains(t, string(output), "Squashing 2 commits")
	assert.Contains(t, string(output), "Merged test/merge into main")

	// Verify main was updated in root repo
	mainLog := gitCmd(t, env.RootDir, "log", "--oneline", "-1")
	assert.Contains(t, mainLog, "Merge test feature")
	mainMsg := gitCmd(t, env.RootDir, "log", "-1", "--format=%B")
	assert.Contains(t, mainMsg, "Subtask-Task: "+taskName)
	gitCmd(t, env.RootDir, "merge-base", "--is-ancestor", mainBefore, "main")

	// Verify branch was deleted in workspace
	branches := gitCmd(t, ws, "branch", "-a")
	assert.NotContains(t, branches, taskName, "task branch should be deleted")

	// Verify workspace is detached
	status := gitCmd(t, ws, "status", "--short", "--branch")
	assert.Contains(t, status, "no branch", "workspace should be detached")

	// Verify task is marked as merged in history and runtime state is cleared.
	tail, err := history.Tail(taskName)
	require.NoError(t, err)
	assert.Equal(t, task.TaskStatusMerged, tail.TaskStatus)
	assert.NotEmpty(t, tail.LastMergedCommit)

	finalState, err := task.LoadState(taskName)
	require.NoError(t, err)
	require.NotNil(t, finalState)
	assert.Empty(t, finalState.Workspace)

	// Verify feature file exists on main
	mainFile := filepath.Join(env.RootDir, "feature.txt")
	content, err := os.ReadFile(mainFile)
	require.NoError(t, err)
	// Normalize line endings for cross-platform compatibility
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	assert.Equal(t, "line 1\nline 2\n", normalized)
}

func TestMergeCommand_LocalMainAheadOfOrigin(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	// Create a task
	taskName := "test/merge-ahead"
	env.CreateTask(taskName, "Test merge with local main ahead of origin", "main", "Test merge")
	env.CreateTaskHistory(taskName, []history.Event{{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})}})

	// Create task state (simulating a task that has been run)
	state := &task.State{
		Workspace: env.Workspaces[0],
	}
	env.CreateTaskState(taskName, state)

	// Set up a local bare "origin" remote and push the current main to it.
	originDir := filepath.Join(t.TempDir(), "origin.git")
	gitCmd(t, env.RootDir, "init", "--bare", originDir)
	gitCmd(t, env.RootDir, "remote", "add", "origin", originDir)
	gitCmd(t, env.RootDir, "push", "-u", "origin", "main")
	gitCmd(t, env.RootDir, "fetch", "origin")

	// Make local main ahead of origin/main by one commit (not pushed).
	aheadFile := filepath.Join(env.RootDir, "ahead.txt")
	os.WriteFile(aheadFile, []byte("local ahead\n"), 0644)
	gitCmd(t, env.RootDir, "add", "ahead.txt")
	gitCmd(t, env.RootDir, "commit", "-m", "Local main ahead")
	assert.Equal(t, "1", strings.TrimSpace(gitCmd(t, env.RootDir, "rev-list", "--count", "origin/main..main")))

	// Create workspace with task branch and commits (based on origin/main).
	ws := env.Workspaces[0]
	gitCmd(t, ws, "checkout", "-b", taskName, "origin/main")

	featureFile := filepath.Join(ws, "feature.txt")
	os.WriteFile(featureFile, []byte("line 1\n"), 0644)
	gitCmd(t, ws, "add", "feature.txt")
	gitCmd(t, ws, "commit", "-m", "Add feature part 1")

	os.WriteFile(featureFile, []byte("line 1\nline 2\n"), 0644)
	gitCmd(t, ws, "add", "feature.txt")
	gitCmd(t, ws, "commit", "-m", "Add feature part 2")

	// Capture main tip for fast-forward assertion (includes the ahead commit).
	mainBefore := strings.TrimSpace(gitCmd(t, env.RootDir, "rev-parse", "main"))

	// Build subtask command
	subtaskBin := buildSubtask(t)

	// Run merge command (should succeed even though local main is ahead of origin/main).
	cmd := exec.Command(subtaskBin, "merge", taskName, "-m", "Merge test feature")
	cmd.Dir = env.RootDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "merge should succeed: %s", output)

	// Verify main was updated in root repo and fast-forwarded from its previous tip.
	mainLog := gitCmd(t, env.RootDir, "log", "--oneline", "-1")
	assert.Contains(t, mainLog, "Merge test feature")
	gitCmd(t, env.RootDir, "merge-base", "--is-ancestor", mainBefore, "main")

	tail, err := history.Tail(taskName)
	require.NoError(t, err)
	assert.Equal(t, task.TaskStatusMerged, tail.TaskStatus)
}

// TestMergeWithConflicts verifies that merge handles conflicts gracefully
func TestMergeWithConflicts(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	// Create a task
	taskName := "test/conflict"
	env.CreateTask(taskName, "Test merge conflict", "main", "Test conflict")
	env.CreateTaskHistory(taskName, []history.Event{{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})}})

	// Create task state
	conflictState := &task.State{
		Workspace: env.Workspaces[0],
	}
	env.CreateTaskState(taskName, conflictState)

	// Create conflicting changes in main
	conflictFile := filepath.Join(env.RootDir, "conflict.txt")
	os.WriteFile(conflictFile, []byte("main version\n"), 0644)
	gitCmd(t, env.RootDir, "add", "conflict.txt")
	gitCmd(t, env.RootDir, "commit", "-m", "Add conflict file on main")

	// Create workspace with task branch and conflicting commit
	ws := env.Workspaces[0]
	gitCmd(t, ws, "checkout", "-b", taskName)
	wsConflictFile := filepath.Join(ws, "conflict.txt")
	os.WriteFile(wsConflictFile, []byte("branch version\n"), 0644)
	gitCmd(t, ws, "add", "conflict.txt")
	gitCmd(t, ws, "commit", "-m", "Add conflict file on branch")

	// Build subtask command
	subtaskBin := buildSubtask(t)

	// Run merge command - should fail with conflict
	cmd := exec.Command(subtaskBin, "merge", taskName, "-m", "Try merge")
	cmd.Dir = env.RootDir
	output, err := cmd.CombinedOutput()
	require.Error(t, err, "merge should fail with conflicts")

	// Verify error message mentions conflicts
	assert.Contains(t, string(output), "merge failed: conflicts")
	assert.Contains(t, string(output), "Manual:", "should suggest manual resolution")

	// Verify task is NOT marked as merged.
	tail, err := history.Tail(taskName)
	require.NoError(t, err)
	assert.NotEqual(t, task.TaskStatusMerged, tail.TaskStatus)
}
