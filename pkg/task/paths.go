package task

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zippoxer/subtask/internal/homedir"
)

var projectDirCache struct {
	mu       sync.Mutex
	computed bool
	cwd      string
	abs      string
	ok       bool
}

// GlobalDir returns ~/.subtask.
func GlobalDir() string {
	home, _ := homedir.Dir()
	return filepath.Join(home, ".subtask")
}

// ProjectDir returns .subtask in current dir.
func ProjectDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ".subtask"
	}
	cwd = canonicalPath(cwd)
	if abs, ok := projectDirAbsFrom(cwd); ok {
		rel, err := filepath.Rel(cwd, abs)
		if err == nil && rel != "" {
			return rel
		}
		// Fall back to absolute path if Rel fails for some reason.
		return abs
	}
	// No project found; preserve prior behavior (e.g. subtask init creates .subtask in cwd).
	return ".subtask"
}

// ProjectDirAbs returns the absolute path to the project's .subtask directory.
// If no .subtask directory exists in the cwd or any parent, it returns "<cwd>/.subtask".
func ProjectDirAbs() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ".subtask"
	}
	cwd = canonicalPath(cwd)
	if abs, ok := projectDirAbsFrom(cwd); ok {
		return abs
	}
	return filepath.Join(cwd, ".subtask")
}

// ProjectRoot returns the absolute path to the project root (the parent of .subtask).
// If no .subtask directory exists in the cwd or any parent, it returns the current working directory.
func ProjectRoot() string {
	return filepath.Dir(ProjectDirAbs())
}

// TasksDir returns .subtask/tasks.
func TasksDir() string {
	return filepath.Join(ProjectDir(), "tasks")
}

// InternalDir returns .subtask/internal.
func InternalDir() string {
	return filepath.Join(ProjectDir(), "internal")
}

// ConfigPath returns .subtask/config.json.
func ConfigPath() string {
	return filepath.Join(ProjectDir(), "config.json")
}

// EscapeName converts "fix/epoch-boundary" to "fix--epoch-boundary".
func EscapeName(name string) string {
	return strings.ReplaceAll(name, "/", "--")
}

// UnescapeName converts "fix--epoch-boundary" to "fix/epoch-boundary".
func UnescapeName(escaped string) string {
	return strings.ReplaceAll(escaped, "--", "/")
}

// Dir returns .subtask/tasks/<escaped-name>.
func Dir(name string) string {
	return filepath.Join(TasksDir(), EscapeName(name))
}

// Path returns the TASK.md path.
func Path(name string) string {
	return filepath.Join(Dir(name), "TASK.md")
}

// StatePath returns the state.json path.
func StatePath(name string) string {
	return filepath.Join(InternalDir(), EscapeName(name), "state.json")
}

// HistoryPath returns the history.jsonl path.
func HistoryPath(name string) string {
	return filepath.Join(Dir(name), "history.jsonl")
}

// WorkspacesDir returns ~/.subtask/workspaces.
func WorkspacesDir() string {
	return filepath.Join(GlobalDir(), "workspaces")
}

// EscapePath converts a path to a safe directory name.
// It resolves symlinks first to ensure consistency across different cwd resolutions.
func EscapePath(p string) string {
	// Resolve symlinks to get consistent paths (e.g., /var -> /private/var on macOS)
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	p = strings.ReplaceAll(p, string(os.PathSeparator), "-")
	// Windows drive letters and paths include ':' which is invalid in filenames.
	p = strings.ReplaceAll(p, ":", "-")
	// Additional Windows-invalid filename characters.
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

func findProjectDirAbs(startDir string) (string, bool) {
	dir := startDir
	for {
		candidate := filepath.Join(dir, ".subtask")
		// Check for config.json to distinguish project .subtask from global ~/.subtask
		configPath := filepath.Join(candidate, "config.json")
		if _, err := os.Stat(configPath); err == nil {
			return candidate, true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

func projectDirAbsFrom(cwd string) (string, bool) {
	projectDirCache.mu.Lock()
	defer projectDirCache.mu.Unlock()

	if projectDirCache.computed && projectDirCache.cwd == cwd {
		return projectDirCache.abs, projectDirCache.ok
	}

	abs, ok := findProjectDirAbs(cwd)
	abs = canonicalPath(abs)
	projectDirCache.computed = true
	projectDirCache.cwd = cwd
	projectDirCache.abs = abs
	projectDirCache.ok = ok
	return abs, ok
}
