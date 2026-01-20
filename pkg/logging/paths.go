package logging

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/zippoxer/subtask/internal/homedir"
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

// escapePath matches the workspace naming convention (EscapePath in pkg/task):
// resolve symlinks (best-effort), then replace path separators and a few filename-invalid characters with '-'.
func escapePath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	p = strings.ReplaceAll(p, string(os.PathSeparator), "-")
	p = strings.ReplaceAll(p, ":", "-")
	p = strings.NewReplacer(
		"<", "-",
		">", "-",
		"\"", "-",
		"|", "-",
		"?", "-",
		"*", "-",
	).Replace(p)
	return p
}
