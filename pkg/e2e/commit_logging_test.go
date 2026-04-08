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

func TestCommitLogging_WritesTaskCommitEvents(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "test/commit-log"
	env.CreateTask(taskName, "Commit logging", "main", "Log commits to history")

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
	commitSHA := strings.TrimSpace(gitCmd(t, ws, "rev-parse", "HEAD"))

	subtaskBin := buildSubtask(t)
	cmd := exec.Command(subtaskBin, "show", taskName)
	cmd.Dir = env.RootDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "show should succeed: %s", out)

	evs, err := history.Read(taskName, history.ReadOptions{EventsOnly: true})
	require.NoError(t, err)

	found := false
	for _, ev := range evs {
		if ev.Type != "task.commit" {
			continue
		}
		var d struct {
			SHA         string `json:"sha"`
			Subject     string `json:"subject"`
			AuthorName  string `json:"author_name"`
			AuthorEmail string `json:"author_email"`
			AuthoredAt  int64  `json:"authored_at"`
			SeenAt      int64  `json:"seen_at"`
		}
		require.NoError(t, json.Unmarshal(ev.Data, &d))
		if strings.TrimSpace(d.SHA) != commitSHA {
			continue
		}
		found = true
		assert.Equal(t, "Add feature", d.Subject)
		assert.Equal(t, "Test User", d.AuthorName)
		assert.Equal(t, "test@test.com", d.AuthorEmail)
		assert.Greater(t, d.AuthoredAt, int64(0))
		assert.Greater(t, d.SeenAt, int64(0))
	}
	require.True(t, found, "expected task.commit event for %s", commitSHA)
}
