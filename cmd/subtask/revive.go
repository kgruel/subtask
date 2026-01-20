package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zippoxer/subtask/pkg/git"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/workspace"
)

// acquireReviveWorkspace acquires a fresh workspace (not reusing excludeWorkspacePath) and checks out the task branch.
// The caller MUST call Release() after persisting state to avoid workspace reuse races.
func acquireReviveWorkspace(taskName, excludeWorkspacePath string) (*workspace.Acquisition, error) {
	pool := workspace.NewPool()
	acq, err := pool.AcquireExcluding(excludeWorkspacePath)
	if err != nil {
		return nil, err
	}

	ws := acq.Entry

	// Branch name is the task name (e.g., fix/bug).
	if !git.BranchExists(ws.Path, taskName) {
		acq.Release()

		// Check durable status to give a better message.
		tail, _ := history.Tail(taskName)
		if tail.TaskStatus == task.TaskStatusMerged {
			return nil, fmt.Errorf("task %s is merged; nothing to resume\n\n"+
				"To reopen it on a fresh branch:\n"+
				"  subtask send %s \"...\"",
				taskName, taskName)
		}

		return nil, fmt.Errorf("cannot resume %s: branch no longer exists\n\n"+
			"To follow up with a new task (preserves conversation):\n"+
			"  subtask draft <new-task> --follow-up %s --base-branch main --title \"...\"",
			taskName, taskName)
	}

	if err := git.Checkout(ws.Path, taskName); err != nil {
		acq.Release()
		return nil, fmt.Errorf("failed to checkout branch %q: %w", taskName, err)
	}

	ensureTaskSymlink(ws.Path, taskName)

	return acq, nil
}

// ensureTaskSymlink makes the task folder available inside the workspace at:
//
//	<workspace>/.subtask/tasks/<escaped-task-name> -> <main-repo>/.subtask/tasks/<escaped-task-name>
func ensureTaskSymlink(workspacePath, taskName string) {
	taskDirAbs := filepath.Join(task.ProjectDirAbs(), "tasks", task.EscapeName(taskName))
	wsTasksDir := filepath.Join(workspacePath, ".subtask", "tasks")
	wsTaskDir := filepath.Join(wsTasksDir, task.EscapeName(taskName))

	_ = os.MkdirAll(wsTasksDir, 0755)
	_ = os.Remove(wsTaskDir)
	if err := os.Symlink(taskDirAbs, wsTaskDir); err != nil {
		printWarning(fmt.Sprintf("failed to symlink task folder: %v", err))
	}
}

func detachWorkspaceHead(workspacePath string) {
	if err := git.RunSilent(workspacePath, "checkout", "--detach", "HEAD"); err != nil {
		printWarning(fmt.Sprintf("failed to detach HEAD: %v", err))
	}
}
