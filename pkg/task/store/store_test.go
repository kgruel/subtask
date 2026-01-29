package store_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/task/store"
	"github.com/zippoxer/subtask/pkg/testutil"
)

func TestStoreGet_OpenTask_PrStyleChangesAndCommitCount(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	repoDir := env.RootDir

	taskName := "fix/prstyle"
	baseCommit := gitCmd(t, repoDir, "rev-parse", "HEAD")

	// Task branch commit.
	gitCmd(t, repoDir, "checkout", "-b", taskName, "main")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "task.txt"), []byte("task\n"), 0o644))
	gitCmd(t, repoDir, "add", "task.txt")
	gitCmd(t, repoDir, "commit", "-m", "task commit")

	// Base branch advances independently.
	gitCmd(t, repoDir, "checkout", "main")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test Repo\nbase\n"), 0o644))
	gitCmd(t, repoDir, "add", "README.md")
	gitCmd(t, repoDir, "commit", "-m", "base commit")

	env.CreateTask(taskName, "PR-style", "main", "desc")
	env.CreateTaskHistory(taskName, repliedHistory("main", baseCommit))

	s := store.New()
	view, err := s.Get(context.Background(), taskName, store.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, 1, view.Commits.Count)
	require.GreaterOrEqual(t, view.Changes.Added, 1)
	require.Equal(t, 0, view.Changes.Removed)
	require.Empty(t, view.Changes.Status)
}

func TestStoreList_OpenTask_AppliedWhenContentInBase(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	repoDir := env.RootDir

	taskName := "fix/applied"
	baseCommit := gitCmd(t, repoDir, "rev-parse", "HEAD")

	// Commit on task branch.
	gitCmd(t, repoDir, "checkout", "-b", taskName, "main")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "task.txt"), []byte("task\n"), 0o644))
	gitCmd(t, repoDir, "add", "task.txt")
	gitCmd(t, repoDir, "commit", "-m", "task commit")

	// Apply the same change to main via a different commit (squash-like).
	gitCmd(t, repoDir, "checkout", "main")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "task.txt"), []byte("task\n"), 0o644))
	gitCmd(t, repoDir, "add", "task.txt")
	gitCmd(t, repoDir, "commit", "-m", "apply task")

	env.CreateTask(taskName, "Applied", "main", "desc")
	env.CreateTaskHistory(taskName, repliedHistory("main", baseCommit))

	s := store.New()
	res, err := s.List(context.Background(), store.ListOptions{All: true})
	require.NoError(t, err)

	var got *store.TaskListItem
	for i := range res.Tasks {
		if res.Tasks[i].Name == taskName {
			got = &res.Tasks[i]
			break
		}
	}
	require.NotNil(t, got)
	require.Equal(t, store.ChangesStatusApplied, got.Changes.Status)
	require.Equal(t, "open", string(got.TaskStatus))
}

func TestStoreList_OpenTask_MissingBranchMarkedMissing(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	repoDir := env.RootDir

	taskName := "fix/missing"
	baseCommit := gitCmd(t, repoDir, "rev-parse", "HEAD")

	// No branch exists for this task, but history indicates it previously ran.
	env.CreateTask(taskName, "Missing", "main", "desc")
	env.CreateTaskHistory(taskName, repliedHistory("main", baseCommit))

	s := store.New()
	res, err := s.List(context.Background(), store.ListOptions{All: true})
	require.NoError(t, err)

	var got *store.TaskListItem
	for i := range res.Tasks {
		if res.Tasks[i].Name == taskName {
			got = &res.Tasks[i]
			break
		}
	}
	require.NotNil(t, got)
	require.Equal(t, store.ChangesStatusMissing, got.Changes.Status)
	require.Error(t, got.Changes.Err)
}

func TestStoreGet_MergedTask_ShowsFrozenStats(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	repoDir := env.RootDir

	taskName := "fix/merged"
	baseCommit := gitCmd(t, repoDir, "rev-parse", "HEAD")

	env.CreateTask(taskName, "Merged", "main", "desc")
	env.CreateTaskHistory(taskName, []history.Event{
		{TS: time.Now().UTC(), Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_ref": "main", "base_commit": baseCommit})},
		{TS: time.Now().UTC(), Type: "task.merged", Data: mustJSON(map[string]any{"via": "subtask", "method": "squash", "into": "main", "branch": taskName, "commit": "deadbeef", "changes_added": 10, "changes_removed": 5})},
	})

	s := store.New()
	view, err := s.Get(context.Background(), taskName, store.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "merged", string(view.TaskStatus))
	require.Equal(t, 10, view.Changes.Added)
	require.Equal(t, 5, view.Changes.Removed)
}

func repliedHistory(baseBranch, baseCommit string) []history.Event {
	return []history.Event{
		{TS: time.Now().UTC(), Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": baseBranch, "base_ref": baseBranch, "base_commit": baseCommit})},
		{TS: time.Now().UTC(), Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
		{TS: time.Now().UTC(), Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 0, "tool_calls": 0, "outcome": "replied"})},
	}
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out))
}
