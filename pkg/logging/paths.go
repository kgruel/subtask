package logging

import (
	"os"
	"path/filepath"

	"github.com/kgruel/subtask/internal/homedir"
	"github.com/kgruel/subtask/internal/pathesc"
)

// globalDir returns ~/.subtask.
func globalDir() string {
	home, _ := homedir.Dir()
	return filepath.Join(home, ".subtask")
}

// projectRoot returns the directory containing a .subtask/ folder at or above cwd.
// If none exists, it returns cwd.
func projectRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	cwd = filepath.Clean(cwd)

	dir := cwd
	for {
		candidate := filepath.Join(dir, ".subtask")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return cwd
}

// escapePath names this project's log file. It shares the workspace naming
// convention via internal/pathesc — pkg/task.EscapePath is the same call — so
// the log for a repo sits under the same escaped root as its workspaces and
// project state. pkg/task imports this package, so the convention has to come
// from below both rather than from pkg/task directly.
func escapePath(p string) string {
	return pathesc.Escape(p)
}
