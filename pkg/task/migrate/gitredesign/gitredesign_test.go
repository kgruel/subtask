package gitredesign_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/task/migrate/gitredesign"
	"github.com/zippoxer/subtask/pkg/testutil"
)

func TestEnsure_SkipsTasksAtCurrentSchemaWithoutReadingHistory(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	repoDir := env.RootDir

	taskName := "migrate/skip"
	env.CreateTask(taskName, "Skip", "main", "desc") // schema=gitredesign.TaskSchemaVersion

	// If Ensure tried to read history.jsonl, it would hit a permission error.
	historyPath := task.HistoryPath(taskName)
	require.NoError(t, os.WriteFile(historyPath, []byte("x\n"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(historyPath, 0o644) })

	require.NoError(t, gitredesign.Ensure(repoDir))
}

func TestEnsure_BackfillsAndBumpsSchema_Idempotent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	repoDir := env.RootDir

	taskName := "migrate/backfill"
	require.NoError(t, (&task.Task{
		Name:        taskName,
		Title:       "Backfill",
		BaseBranch:  "main",
		Description: "desc",
		Schema:      1, // v0.1.1 / schema1
	}).Save())

	// Create a task branch so inferBaseCommit can use merge-base.
	gitCmd(t, repoDir, "checkout", "-b", taskName, "main")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "task.txt"), []byte("task\n"), 0o644))
	gitCmd(t, repoDir, "add", "task.txt")
	gitCmd(t, repoDir, "commit", "-m", "task commit")
	gitCmd(t, repoDir, "checkout", "main")

	// Create an arbitrary commit to use as the "merged commit" in legacy history.
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "merged.txt"), []byte("merged\n"), 0o644))
	gitCmd(t, repoDir, "add", "merged.txt")
	gitCmd(t, repoDir, "commit", "-m", "merged commit")
	mergedCommit := strings.TrimSpace(gitCmd(t, repoDir, "rev-parse", "HEAD"))

	// Legacy-ish history: opened missing base_commit, merged missing frozen stats.
	env.CreateTaskHistory(taskName, []history.Event{
		{TS: time.Now().UTC(), Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{TS: time.Now().UTC(), Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "ready"})},
		{TS: time.Now().UTC(), Type: "task.merged", Data: mustJSON(map[string]any{"commit": mergedCommit, "into": "main"})},
	})

	before, err := os.ReadFile(task.HistoryPath(taskName))
	require.NoError(t, err)

	require.NoError(t, gitredesign.Ensure(repoDir))

	after, err := os.ReadFile(task.HistoryPath(taskName))
	require.NoError(t, err)
	require.NotEqual(t, string(before), string(after))

	// Schema bumped.
	loaded, err := task.Load(taskName)
	require.NoError(t, err)
	require.Equal(t, gitredesign.TaskSchemaVersion, loaded.Schema)

	// Backfilled base_commit + base_ref.
	events, err := history.Read(taskName, history.ReadOptions{})
	require.NoError(t, err)
	var openedData map[string]any
	require.NoError(t, json.Unmarshal(events[0].Data, &openedData))
	baseCommit, _ := openedData["base_commit"].(string)
	baseRef, _ := openedData["base_ref"].(string)
	require.NotEmpty(t, strings.TrimSpace(baseCommit))
	require.Equal(t, "main", strings.TrimSpace(baseRef))

	// Backfilled merged frozen stats (or recorded a frozen_error).
	var mergedData map[string]any
	require.NoError(t, json.Unmarshal(events[len(events)-1].Data, &mergedData))
	_, hasAdded := mergedData["changes_added"]
	_, hasErr := mergedData["frozen_error"]
	require.True(t, hasAdded || hasErr)

	// Idempotent: Ensure again should skip (schema already bumped) and not rewrite history.
	before2, err := os.ReadFile(task.HistoryPath(taskName))
	require.NoError(t, err)
	require.NoError(t, gitredesign.Ensure(repoDir))
	after2, err := os.ReadFile(task.HistoryPath(taskName))
	require.NoError(t, err)
	require.Equal(t, string(before2), string(after2))
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
