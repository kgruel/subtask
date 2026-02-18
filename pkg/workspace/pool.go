package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zippoxer/subtask/internal/filelock"
	"github.com/zippoxer/subtask/pkg/git"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
)

// Pool manages workspace allocation.
type Pool struct{}

// NewPool creates a workspace pool.
func NewPool() *Pool {
	return &Pool{}
}

// Acquisition holds a workspace and its lock. Call Release() when done.
type Acquisition struct {
	Entry    *Entry
	lockFile *os.File
}

// Release releases the workspace lock. Must be called after state is saved.
func (a *Acquisition) Release() {
	if a.lockFile != nil {
		_ = filelock.Unlock(a.lockFile)
		_ = a.lockFile.Close()
		a.lockFile = nil
	}
}

// Acquire finds an available workspace with file locking to prevent races.
// The caller MUST call Release() on the returned Acquisition after saving state.
func (p *Pool) Acquire() (*Acquisition, error) {
	return p.AcquireExcluding()
}

// AcquireExcluding finds an available workspace, excluding any paths provided.
// The caller MUST call Release() on the returned Acquisition after saving state.
func (p *Pool) AcquireExcluding(excludePaths ...string) (*Acquisition, error) {
	// Clean up stale tasks first
	task.CleanupStaleTasks()

	// Lock to prevent race conditions when multiple tasks dispatch in parallel
	lockPath := filepath.Join(task.InternalDir(), "workspace.lock")
	os.MkdirAll(filepath.Dir(lockPath), 0755)

	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open workspace lock: %w", err)
	}

	if err := filelock.LockExclusive(lockFile); err != nil {
		lockFile.Close()
		return nil, fmt.Errorf("failed to acquire workspace lock: %w", err)
	}
	unlockAndClose := func() {
		_ = filelock.Unlock(lockFile)
		_ = lockFile.Close()
	}

	cfg, err := LoadConfig()
	if err != nil {
		unlockAndClose()
		return nil, err
	}

	// Discover workspaces from disk
	workspaces, err := ListWorkspaces()
	if err != nil {
		unlockAndClose()
		return nil, fmt.Errorf("failed to list workspaces: %w", err)
	}

	// Now safely check for available workspaces
	tasks, err := task.List()
	if err != nil {
		unlockAndClose()
		return nil, err
	}

	// Build set of occupied workspaces
	occupied := make(map[string]bool)
	for _, name := range tasks {
		state, err := task.LoadState(name)
		if err != nil {
			continue
		}
		if state == nil || state.Workspace == "" {
			continue
		}

		// If this machine thinks the task has a workspace but the portable history says it's not open,
		// treat it as free (common after syncing tasks across machines).
		if state.SupervisorPID == 0 {
			if tail, err := history.Tail(name); err == nil && tail.TaskStatus != task.TaskStatusOpen {
				_, _ = task.TryWithLock(name, func() error {
					st, err := task.LoadState(name)
					if err != nil || st == nil {
						return err
					}
					st.Workspace = ""
					st.SupervisorPID = 0
					st.StartedAt = time.Time{}
					st.LastError = ""
					st.SessionID = ""
					st.Adapter = ""
					return st.Save(name)
				})
				continue
			}
		}

		// Skip stale supervisors (they will be cleaned up shortly).
		if state.IsStale() {
			continue
		}
		occupied[state.Workspace] = true
	}

	// Exclude requested workspaces (treated as occupied).
	for _, p := range excludePaths {
		if p != "" {
			occupied[p] = true
		}
	}

	// Find first available
	for i := range workspaces {
		ws := &workspaces[i]
		if !occupied[ws.Path] {
			return &Acquisition{Entry: ws, lockFile: lockFile}, nil
		}
	}

	maxWorkspaces := cfg.MaxWorkspaces
	if maxWorkspaces <= 0 {
		maxWorkspaces = DefaultMaxWorkspaces
	}

	if len(workspaces) < maxWorkspaces {
		// Create a new workspace (detached worktree).
		repoRoot := task.ProjectRoot()

		escapedPath := task.EscapePath(repoRoot)
		wsID, wsPath, err := nextWorkspaceIDAndPath(escapedPath, workspaces)
		if err != nil {
			unlockAndClose()
			return nil, err
		}

		// Clean up stale worktree references (e.g., user deleted a worktree folder).
		_ = git.RunQuiet(repoRoot, "worktree", "prune")
		// Remove any stale registration at this path (ignore errors).
		_ = git.RunQuiet(repoRoot, "worktree", "remove", "--force", wsPath)

		if err := os.MkdirAll(filepath.Dir(wsPath), 0755); err != nil {
			unlockAndClose()
			return nil, fmt.Errorf("failed to create workspace dir: %w", err)
		}
		if err := git.RunQuiet(repoRoot, "worktree", "add", "--detach", wsPath); err != nil {
			unlockAndClose()
			return nil, fmt.Errorf("failed to create worktree workspace-%d: %w", wsID, err)
		}

		ws := &Entry{
			Name: fmt.Sprintf("workspace-%d", wsID),
			Path: wsPath,
			ID:   wsID,
		}
		return &Acquisition{Entry: ws, lockFile: lockFile}, nil
	}

	// No workspace available - release lock.
	unlockAndClose()

	return nil, fmt.Errorf("all workspaces occupied\n\n"+
		"All %d workspaces are occupied. Close a task first:\n"+
		"  subtask list\n"+
		"  subtask close <task>", len(workspaces))
}

// ForTask returns the workspace assigned to a task, if any.
func ForTask(taskName string) (string, error) {
	state, err := task.LoadState(taskName)
	if err != nil {
		return "", err
	}
	if state == nil {
		return "", fmt.Errorf("task %q has no workspace", taskName)
	}
	return state.Workspace, nil
}

func nextWorkspaceIDAndPath(escapedRepoPath string, workspaces []Entry) (int, string, error) {
	used := make(map[int]bool, len(workspaces))
	for _, ws := range workspaces {
		if ws.ID > 0 {
			used[ws.ID] = true
		}
	}

	// Pick the smallest unused positive ID (robust to gaps).
	for id := 1; ; id++ {
		if used[id] {
			continue
		}
		wsPath := filepath.Join(task.WorkspacesDir(), fmt.Sprintf("%s--%d", escapedRepoPath, id))
		if _, err := os.Stat(wsPath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return 0, "", err
		}
		return id, wsPath, nil
	}
}
