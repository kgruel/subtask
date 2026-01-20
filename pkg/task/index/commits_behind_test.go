package index_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	taskindex "github.com/zippoxer/subtask/pkg/task/index"
	"github.com/zippoxer/subtask/pkg/testutil"
)

func commitEmpty(t *testing.T, dir, msg string) {
	t.Helper()
	gitOut(t, dir, "commit", "--allow-empty", "-m", msg)
}

func TestIndex_CommitsBehind_UsesTaskRef_WhenBranchExists(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// Draft-time base commit.
	baseCommit := gitOut(t, env.RootDir, "rev-parse", "HEAD")

	// Base branch advances.
	commitEmpty(t, env.RootDir, "main-1")
	commitEmpty(t, env.RootDir, "main-2")

	// Task branch created at the old base commit, then rebased onto current main.
	taskName := "behind/rebased"
	gitOut(t, env.RootDir, "switch", "-c", taskName, baseCommit)
	commitEmpty(t, env.RootDir, "task-1")
	gitOut(t, env.RootDir, "rebase", "main")
	gitOut(t, env.RootDir, "switch", "main")

	// Sanity: main advanced relative to the pinned base commit.
	require.NotEqual(t, baseCommit, gitOut(t, env.RootDir, "rev-parse", "main"))
	require.Greater(t, mustAtoi(t, gitOut(t, env.RootDir, "rev-list", "--count", baseCommit+"..main")), 0)

	env.CreateTask(taskName, "Rebased task", "main", "Description")
	env.CreateTaskState(taskName, &task.State{Workspace: ""})
	env.CreateTaskHistory(taskName, []history.Event{
		{TS: now, Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{TS: now, Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
	})

	idx, err := taskindex.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	require.NoError(t, idx.Refresh(ctx, taskindex.RefreshPolicy{
		Git: taskindex.GitPolicy{
			Mode:  taskindex.GitTasks,
			Tasks: []string{taskName},
		},
	}))

	rec, ok, err := idx.Get(ctx, taskName)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 0, rec.CommitsBehind)
}

func TestIndex_CommitsBehind_FallsBackToBaseCommit_WhenBranchMissing(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	baseCommit := gitOut(t, env.RootDir, "rev-parse", "HEAD")
	commitEmpty(t, env.RootDir, "main-1")
	commitEmpty(t, env.RootDir, "main-2")

	taskName := "behind/draft-only"
	env.CreateTask(taskName, "Draft-only task", "main", "Description")
	env.CreateTaskState(taskName, &task.State{Workspace: ""})
	env.CreateTaskHistory(taskName, []history.Event{
		{TS: now, Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{TS: now, Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
	})

	idx, err := taskindex.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	require.NoError(t, idx.Refresh(ctx, taskindex.RefreshPolicy{
		Git: taskindex.GitPolicy{
			Mode:  taskindex.GitTasks,
			Tasks: []string{taskName},
		},
	}))

	rec, ok, err := idx.Get(ctx, taskName)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 2, rec.CommitsBehind)
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, ch := range strings.TrimSpace(s) {
		if ch < '0' || ch > '9' {
			t.Fatalf("not an int: %q", s)
		}
		n = n*10 + int(ch-'0')
	}
	return n
}
