package index_test

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/git"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/testutil"

	taskindex "github.com/zippoxer/subtask/pkg/task/index"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, out)
	return strings.TrimSpace(string(out))
}

func TestIndex_IntegrationRefresh_Ancestor(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	ctx := context.Background()

	name := "idx/ancestor"
	env.CreateTask(name, "Ancestor", "main", "desc")
	env.CreateTaskHistory(name, []history.Event{{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})}})

	ws := env.Workspaces[0]
	gitOut(t, ws, "checkout", "-b", name)
	require.NoError(t, os.WriteFile(filepath.Join(ws, "a.txt"), []byte("a\n"), 0o644))
	gitOut(t, ws, "add", "a.txt")
	gitOut(t, ws, "commit", "-m", "a")

	idx, err := taskindex.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	// Prime snapshot.
	require.NoError(t, idx.Refresh(ctx, taskindex.RefreshPolicy{
		Git: taskindex.GitPolicy{Mode: taskindex.GitOpenOnly, IncludeIntegration: true},
	}))

	// External merge (history-preserving).
	gitOut(t, env.RootDir, "checkout", "main")
	gitOut(t, env.RootDir, "merge", "--no-ff", name, "-m", "Merge "+name)

	require.NoError(t, idx.Refresh(ctx, taskindex.RefreshPolicy{
		Git: taskindex.GitPolicy{Mode: taskindex.GitOpenOnly, IncludeIntegration: true},
	}))

	rec, ok, err := idx.Get(ctx, name)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, string(git.IntegratedAncestor), strings.TrimSpace(rec.IntegratedReason))
}

func TestIndex_IntegrationRefresh_Squash(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	ctx := context.Background()

	name := "idx/squash"
	env.CreateTask(name, "Squash", "main", "desc")
	env.CreateTaskHistory(name, []history.Event{{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})}})

	ws := env.Workspaces[0]
	gitOut(t, ws, "checkout", "-b", name)
	require.NoError(t, os.WriteFile(filepath.Join(ws, "s.txt"), []byte("s\n"), 0o644))
	gitOut(t, ws, "add", "s.txt")
	gitOut(t, ws, "commit", "-m", "s")

	idx, err := taskindex.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	// Prime snapshot.
	require.NoError(t, idx.Refresh(ctx, taskindex.RefreshPolicy{
		Git: taskindex.GitPolicy{Mode: taskindex.GitOpenOnly, IncludeIntegration: true},
	}))

	// External squash merge.
	gitOut(t, env.RootDir, "checkout", "main")
	gitOut(t, env.RootDir, "merge", "--squash", name)
	gitOut(t, env.RootDir, "commit", "-m", "Squash "+name)

	require.NoError(t, idx.Refresh(ctx, taskindex.RefreshPolicy{
		Git: taskindex.GitPolicy{Mode: taskindex.GitOpenOnly, IncludeIntegration: true},
	}))

	rec, ok, err := idx.Get(ctx, name)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, string(git.IntegratedMergeAddsNothing), strings.TrimSpace(rec.IntegratedReason))
}

func TestIndex_IntegrationForceTasks_DoesNotHideUnrelatedRefChanges(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	ctx := context.Background()

	a := "idx/force-a"
	b := "idx/force-b"
	env.CreateTask(a, "A", "main", "desc")
	env.CreateTask(b, "B", "main", "desc")
	env.CreateTaskHistory(a, []history.Event{{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})}})
	env.CreateTaskHistory(b, []history.Event{{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})}})

	ws := env.Workspaces[0]
	gitOut(t, ws, "checkout", "-b", a)
	require.NoError(t, os.WriteFile(filepath.Join(ws, "a.txt"), []byte("a\n"), 0o644))
	gitOut(t, ws, "add", "a.txt")
	gitOut(t, ws, "commit", "-m", "a")

	gitOut(t, ws, "checkout", "--detach")
	gitOut(t, ws, "checkout", "-b", b)
	require.NoError(t, os.WriteFile(filepath.Join(ws, "b.txt"), []byte("b\n"), 0o644))
	gitOut(t, ws, "add", "b.txt")
	gitOut(t, ws, "commit", "-m", "b1")

	idx, err := taskindex.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	// Prime snapshot.
	require.NoError(t, idx.Refresh(ctx, taskindex.RefreshPolicy{
		Git: taskindex.GitPolicy{Mode: taskindex.GitOpenOnly, IncludeIntegration: true},
	}))

	// External change on b: new commit.
	require.NoError(t, os.WriteFile(filepath.Join(ws, "b.txt"), []byte("b2\n"), 0o644))
	gitOut(t, ws, "add", "b.txt")
	gitOut(t, ws, "commit", "-m", "b2")
	bHead := gitOut(t, env.RootDir, "rev-parse", b)

	// Force refresh for a only; must not overwrite snapshot without accounting for b.
	require.NoError(t, idx.Refresh(ctx, taskindex.RefreshPolicy{
		Git: taskindex.GitPolicy{
			Mode:               taskindex.GitTasks,
			Tasks:              []string{a},
			IncludeIntegration: true,
		},
	}))

	db, err := sql.Open("sqlite", filepath.Join(env.RootDir, ".subtask", "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var got sql.NullString
	require.NoError(t, db.QueryRow(`SELECT git_last_branch_head FROM tasks WHERE name = ?;`, b).Scan(&got))
	require.True(t, got.Valid)
	require.Equal(t, bHead, strings.TrimSpace(got.String))
}

func TestIndex_IntegrationForceTasks_ClearsStaleWhenBranchMoves(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	ctx := context.Background()

	name := "idx/clear-stale"
	env.CreateTask(name, "Clear stale", "main", "desc")
	env.CreateTaskHistory(name, []history.Event{{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})}})

	ws := env.Workspaces[0]
	gitOut(t, ws, "checkout", "-b", name)
	require.NoError(t, os.WriteFile(filepath.Join(ws, "x.txt"), []byte("x\n"), 0o644))
	gitOut(t, ws, "add", "x.txt")
	gitOut(t, ws, "commit", "-m", "x")

	idx, err := taskindex.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	// Prime snapshot.
	require.NoError(t, idx.Refresh(ctx, taskindex.RefreshPolicy{
		Git: taskindex.GitPolicy{Mode: taskindex.GitOpenOnly, IncludeIntegration: true},
	}))

	// External merge (history-preserving) so ancestor check is true.
	gitOut(t, env.RootDir, "checkout", "main")
	gitOut(t, env.RootDir, "merge", "--no-ff", name, "-m", "Merge "+name)

	require.NoError(t, idx.Refresh(ctx, taskindex.RefreshPolicy{
		Git: taskindex.GitPolicy{Mode: taskindex.GitOpenOnly, IncludeIntegration: true},
	}))
	rec, ok, err := idx.Get(ctx, name)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, strings.TrimSpace(rec.IntegratedReason))

	// Branch moves after being integrated: new commit not in main.
	gitOut(t, ws, "checkout", name)
	require.NoError(t, os.WriteFile(filepath.Join(ws, "x.txt"), []byte("x2\n"), 0o644))
	gitOut(t, ws, "add", "x.txt")
	gitOut(t, ws, "commit", "-m", "x2")

	// Force refresh for this task (send-path behavior) must clear stale integration.
	require.NoError(t, idx.Refresh(ctx, taskindex.RefreshPolicy{
		Git: taskindex.GitPolicy{
			Mode:               taskindex.GitTasks,
			Tasks:              []string{name},
			IncludeIntegration: true,
		},
	}))
	rec, ok, err = idx.Get(ctx, name)
	require.NoError(t, err)
	require.True(t, ok)
	require.Empty(t, strings.TrimSpace(rec.IntegratedReason))
}
