package index_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	taskindex "github.com/kgruel/subtask/pkg/task/index"
	"github.com/kgruel/subtask/pkg/testutil"
)

// bumpStateSig forces the next refresh to see a changed signature for the task
// (the "state" sig part), so shouldInvalidateGit fires and invalidateGitCache
// runs against it.
func bumpStateSig(t *testing.T, name string) {
	t.Helper()
	require.NoError(t, (&task.State{LastError: "sig-bump"}).Save(name))
}

// TestIndex_MergedTaskChanges_NoOpFinalize_SurvivesReInvalidation guards the
// invalidate path that TestIndex_MergedTaskChanges_NoOpFinalizeKeepsFrozenStats
// (recompute path) does not reach. On a fresh index the task has no prior
// signature, so the first refresh never invalidates it. A signature change
// before a second refresh triggers invalidateGitCache for the merged task; the
// frozen line counts (projected from history) must survive rather than be
// NULLed.
func TestIndex_MergedTaskChanges_NoOpFinalize_SurvivesReInvalidation(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	ctx := context.Background()

	_ = gitCommitFile(t, "work.txt", "one\ntwo\nthree\n", "setup work")

	name := "merged/noop-reinvalidate"
	env.CreateTask(name, "No-op finalize re-invalidate", "main", "desc")

	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	env.CreateTaskHistory(name, []history.Event{
		{TS: now.Add(-2 * time.Minute), Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{TS: now.Add(-1 * time.Minute), Type: "task.merged", Data: mustJSON(map[string]any{
			"commit":          "",
			"changes_added":   5,
			"changes_removed": 3,
		})},
	})

	idx, err := taskindex.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	policy := taskindex.RefreshPolicy{Git: taskindex.GitPolicy{Mode: taskindex.GitOpenOnly}}

	// First refresh records the signature and projects the frozen stats.
	require.NoError(t, idx.Refresh(ctx, policy))
	items, err := idx.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, 5, items[0].LinesAdded)
	require.Equal(t, 3, items[0].LinesRemoved)

	// Second refresh after a signature change invalidates the git cache.
	bumpStateSig(t, name)
	require.NoError(t, idx.Refresh(ctx, policy))
	items, err = idx.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, 5, items[0].LinesAdded, "frozen merged stats must survive re-invalidation")
	require.Equal(t, 3, items[0].LinesRemoved, "frozen merged stats must survive re-invalidation")
}

// TestIndex_ClosedTaskChanges_SurvivesReInvalidation covers the case that
// cannot self-heal at all: refreshGit has no recompute branch for closed tasks
// (no workspace, no merge commit), so once invalidateGitCache NULLed the frozen
// close stats they were lost permanently. The CASE guard must preserve them.
func TestIndex_ClosedTaskChanges_SurvivesReInvalidation(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	ctx := context.Background()

	name := "closed/with-changes"
	env.CreateTask(name, "Closed with changes", "main", "desc")

	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	env.CreateTaskHistory(name, []history.Event{
		{TS: now.Add(-2 * time.Minute), Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{TS: now.Add(-1 * time.Minute), Type: "task.closed", Data: mustJSON(map[string]any{
			"changes_added":   7,
			"changes_removed": 2,
		})},
	})

	idx, err := taskindex.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	policy := taskindex.RefreshPolicy{Git: taskindex.GitPolicy{Mode: taskindex.GitOpenOnly}}

	require.NoError(t, idx.Refresh(ctx, policy))
	items, err := idx.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, 7, items[0].LinesAdded)
	require.Equal(t, 2, items[0].LinesRemoved)

	bumpStateSig(t, name)
	require.NoError(t, idx.Refresh(ctx, policy))
	items, err = idx.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, 7, items[0].LinesAdded, "frozen close stats must survive re-invalidation")
	require.Equal(t, 2, items[0].LinesRemoved, "frozen close stats must survive re-invalidation")
}
