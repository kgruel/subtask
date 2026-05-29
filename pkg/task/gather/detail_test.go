package gather_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/gather"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/task/migrate/gitredesign"
	"github.com/kgruel/subtask/pkg/testutil"
)

// TestDetail_AgentFieldSurvivestIndexProjection verifies that the Agent field
// is populated in TaskDetail even though the SQLite index projection doesn't
// store it — the gather layer must fall back to TASK.md on disk.
func TestDetail_AgentFieldSurvivesIndexProjection(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	repoDir := env.RootDir

	taskName := "fix/agent-gather"
	baseCommit := gitHead(t, repoDir)

	// Save task with Agent set (simulates `subtask draft --agent test-agent`).
	t.Helper()
	tsk := &task.Task{
		Name:        taskName,
		Title:       "Test agent gather",
		BaseBranch:  "main",
		Description: "desc",
		Agent:       "test-agent",
		Schema:      gitredesign.TaskSchemaVersion,
	}
	require.NoError(t, tsk.Save())

	// Write minimal history so the index refresh can find the task.
	events := []history.Event{
		{
			TS:   time.Now().UTC(),
			Type: "task.opened",
			Data: mustJSON(t, map[string]any{
				"reason":      "draft",
				"base_branch": "main",
				"base_ref":    "main",
				"base_commit": baseCommit,
			}),
		},
	}
	env.CreateTaskHistory(taskName, events)

	d, err := gather.Detail(context.Background(), taskName)
	require.NoError(t, err)
	require.NotNil(t, d.Task)
	require.Equal(t, "test-agent", d.Task.Agent,
		"gather.Detail must expose Agent from TASK.md even when the SQLite index doesn't carry it")
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}
