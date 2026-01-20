package index_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	taskindex "github.com/zippoxer/subtask/pkg/task/index"
	"github.com/zippoxer/subtask/pkg/testutil"
)

func TestIndex_ListAll_DetectedMergeDoesNotAffectSortOrder(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	ctx := context.Background()

	oldClosedAt := time.Date(2020, 1, 2, 12, 0, 0, 0, time.UTC)
	newMergedAt := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	detectedMergedAt := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

	oldName := "sort/old"
	newName := "sort/new"

	env.CreateTask(oldName, "Old task", "main", "desc")
	env.CreateTaskHistory(oldName, []history.Event{
		{TS: oldClosedAt.Add(-1 * time.Hour), Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{TS: oldClosedAt, Type: "task.closed"},
		{TS: detectedMergedAt, Type: "task.merged", Data: mustJSON(map[string]any{"via": "detected"})},
	})

	env.CreateTask(newName, "New task", "main", "desc")
	env.CreateTaskHistory(newName, []history.Event{
		{TS: newMergedAt.Add(-1 * time.Hour), Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{TS: newMergedAt, Type: "task.merged", Data: mustJSON(map[string]any{"commit": "abc"})},
	})

	idx, err := taskindex.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	require.NoError(t, idx.Refresh(ctx, taskindex.RefreshPolicy{
		Git: taskindex.GitPolicy{Mode: taskindex.GitNone},
	}))

	items, err := idx.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, items, 2)

	require.Equal(t, newName, items[0].Name)
	require.Equal(t, oldName, items[1].Name)
	require.True(t, items[1].LastHistory.Equal(oldClosedAt))
	require.Equal(t, task.TaskStatusMerged, items[1].TaskStatus)
}
