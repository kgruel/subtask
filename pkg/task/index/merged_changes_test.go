package index_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/task/history"
	taskindex "github.com/zippoxer/subtask/pkg/task/index"
	"github.com/zippoxer/subtask/pkg/testutil"
)

func gitCommitFile(t *testing.T, path string, content string, message string) string {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	cmd := exec.Command("git", "add", path)
	cmd.Dir = "."
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", message)
	cmd.Dir = "."
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = "."
	out, err := cmd.Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func TestIndex_MergedTaskChanges_FromLatestMergeCommit(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	ctx := context.Background()

	setup := gitCommitFile(t, "work.txt", "one\ntwo\nthree\n", "setup work")
	require.NotEmpty(t, setup)
	mergeCommit := gitCommitFile(t, "work.txt", "one\ntwo-changed\nthree\nfour\n", "merge A")

	name := "merged/one"
	env.CreateTask(name, "Merged task", "main", "desc")

	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	env.CreateTaskHistory(name, []history.Event{
		{TS: now.Add(-2 * time.Minute), Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{TS: now.Add(-1 * time.Minute), Type: "task.merged", Data: mustJSON(map[string]any{"commit": mergeCommit})},
	})

	idx, err := taskindex.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	require.NoError(t, idx.Refresh(ctx, taskindex.RefreshPolicy{
		Git: taskindex.GitPolicy{Mode: taskindex.GitOpenOnly},
	}))

	items, err := idx.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, name, items[0].Name)
	require.Equal(t, 2, items[0].LinesAdded)
	require.Equal(t, 1, items[0].LinesRemoved)
}

func TestIndex_MergedTaskChanges_ReopenAndMergeAgainUsesLatestCommit(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	ctx := context.Background()

	_ = gitCommitFile(t, "work.txt", "one\ntwo\nthree\n", "setup work")
	commitA := gitCommitFile(t, "work.txt", "one\ntwo-changed\nthree\nfour\n", "merge A")
	commitB := gitCommitFile(t, "work.txt", "one\ntwo-changed-again\nfour\nfive\nsix\n", "merge B")

	name := "merged/twice"
	env.CreateTask(name, "Merged twice", "main", "desc")

	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	env.CreateTaskHistory(name, []history.Event{
		{TS: now.Add(-10 * time.Minute), Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{TS: now.Add(-9 * time.Minute), Type: "task.merged", Data: mustJSON(map[string]any{"commit": commitA})},
		{TS: now.Add(-2 * time.Minute), Type: "task.opened", Data: mustJSON(map[string]any{"reason": "reopen", "from": "merged", "base_branch": "main"})},
		{TS: now.Add(-1 * time.Minute), Type: "task.merged", Data: mustJSON(map[string]any{"commit": commitB})},
	})

	idx, err := taskindex.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	require.NoError(t, idx.Refresh(ctx, taskindex.RefreshPolicy{
		Git: taskindex.GitPolicy{Mode: taskindex.GitOpenOnly},
	}))

	items, err := idx.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, name, items[0].Name)
	require.Equal(t, 3, items[0].LinesAdded)
	require.Equal(t, 2, items[0].LinesRemoved)
}

func TestIndex_MergedTaskChanges_MissingCommitShowsEmpty(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	ctx := context.Background()

	name := "merged/missing"
	env.CreateTask(name, "Missing commit", "main", "desc")

	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	env.CreateTaskHistory(name, []history.Event{
		{TS: now.Add(-2 * time.Minute), Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{TS: now.Add(-1 * time.Minute), Type: "task.merged", Data: mustJSON(map[string]any{"commit": strings.Repeat("0", 40)})},
	})

	idx, err := taskindex.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	require.NoError(t, idx.Refresh(ctx, taskindex.RefreshPolicy{
		Git: taskindex.GitPolicy{Mode: taskindex.GitOpenOnly},
	}))

	items, err := idx.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, name, items[0].Name)
	require.Equal(t, 0, items[0].LinesAdded)
	require.Equal(t, 0, items[0].LinesRemoved)
}
