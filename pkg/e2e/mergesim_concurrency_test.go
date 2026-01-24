package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/testutil"
)

func TestAppliedContentDetection_IndexFallback_ConcurrentList(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "test/applied-concurrent"
	env.CreateTask(taskName, "Applied task (concurrent)", "main", "Applied")
	baseCommit := strings.TrimSpace(gitCmd(t, env.RootDir, "rev-parse", "HEAD"))
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 0, "tool_calls": 0, "outcome": "replied"})},
	})

	state := &task.State{Workspace: env.Workspaces[0]}
	env.CreateTaskState(taskName, state)

	// Create workspace with task branch and a commit.
	ws := env.Workspaces[0]
	gitCmd(t, ws, "checkout", "-b", taskName)
	require.NoError(t, os.WriteFile(filepath.Join(ws, "feature.txt"), []byte("line 1\n"), 0o644))
	gitCmd(t, ws, "add", "feature.txt")
	gitCmd(t, ws, "commit", "-m", "Add feature")

	// Simulate a squash-merge (or independently-applied change) into main with a different commit.
	require.NoError(t, os.WriteFile(filepath.Join(env.RootDir, "feature.txt"), []byte("line 1\n"), 0o644))
	gitCmd(t, env.RootDir, "add", "feature.txt")
	gitCmd(t, env.RootDir, "commit", "-m", "Apply feature via squash")

	subtaskBin := buildSubtask(t)

	const n = 6
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(subtaskBin, "list")
			cmd.Dir = env.RootDir
			cmd.Env = append(os.Environ(), "SUBTASK_MERGE_SIM_FORCE=index")
			out, err := cmd.CombinedOutput()
			if err != nil {
				errs <- fmt.Errorf("list failed: %w: %s", err, out)
				return
			}
			if !strings.Contains(string(out), "applied (+1 -0)") {
				errs <- fmt.Errorf("expected applied in list output, got:\n%s", out)
				return
			}
			errs <- nil
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
}
