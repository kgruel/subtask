package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kgruel/subtask/internal/homedir"
)

// PluginStatus describes the installed state of the subtask Claude Code plugin.
//
// The plugin lives at ~/.claude/plugins/subtask and may be:
//   - absent (Exists == false)
//   - a symlink to a developer working tree (IsSymlink == true)
//   - a real directory installed via Claude Code's plugin marketplace
//
// HasManifest reports whether the plugin actually looks valid — i.e., contains
// .claude-plugin/plugin.json. A symlink to a missing or invalid path can leave
// Exists == true but HasManifest == false.
type PluginStatus struct {
	Path          string
	Exists        bool
	IsSymlink     bool
	SymlinkTarget string
	HasManifest   bool
}

// PluginInstallResult reports what InstallPluginDev did.
type PluginInstallResult struct {
	Path      string // where the symlink was created
	SourceDir string // the resolved absolute source directory
	Action    string // "created", "updated", or "noop"
}

// PluginPath returns the Claude Code plugin path for the subtask plugin under
// the given base directory (usually the user's home).
func PluginPath(baseDir string) string {
	if baseDir == "" {
		return ""
	}
	return filepath.Join(baseDir, ".claude", "plugins", "subtask")
}

// PluginManifestPath returns the path to a plugin's plugin.json manifest.
func PluginManifestPath(pluginDir string) string {
	if pluginDir == "" {
		return ""
	}
	return filepath.Join(pluginDir, ".claude-plugin", "plugin.json")
}

// GetPluginStatus inspects the subtask plugin install location for the current
// user.
func GetPluginStatus() (PluginStatus, error) {
	homeDir, err := homedir.Dir()
	if err != nil {
		return PluginStatus{}, err
	}
	return GetPluginStatusFor(homeDir)
}

// GetPluginStatusFor inspects the plugin install location under baseDir.
func GetPluginStatusFor(baseDir string) (PluginStatus, error) {
	path := PluginPath(baseDir)
	if path == "" {
		return PluginStatus{}, errors.New("invalid base directory")
	}

	st := PluginStatus{Path: path}

	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return PluginStatus{}, err
	}
	st.Exists = true

	if info.Mode()&os.ModeSymlink != 0 {
		st.IsSymlink = true
		target, err := os.Readlink(path)
		if err == nil {
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}
			if abs, err := filepath.Abs(target); err == nil {
				target = abs
			}
			st.SymlinkTarget = target
		}
	}

	if _, err := os.Stat(PluginManifestPath(path)); err == nil {
		st.HasManifest = true
	}

	return st, nil
}

// InstallPluginDev creates a symlink from ~/.claude/plugins/subtask to
// sourceDir, intended for plugin developers who want their working-tree edits
// to take effect immediately.
//
// Validation:
//   - sourceDir must exist and contain .claude-plugin/plugin.json
//   - if the target path is a regular directory or file, returns an error to
//     avoid clobbering a marketplace install or stray artifact
//   - if the target path is a symlink, it's replaced (idempotent)
func InstallPluginDev(sourceDir string) (PluginInstallResult, error) {
	homeDir, err := homedir.Dir()
	if err != nil {
		return PluginInstallResult{}, err
	}
	return InstallPluginDevTo(homeDir, sourceDir)
}

// InstallPluginDevTo is the testable form of InstallPluginDev with an explicit
// base directory.
func InstallPluginDevTo(baseDir, sourceDir string) (PluginInstallResult, error) {
	if baseDir == "" {
		return PluginInstallResult{}, errors.New("invalid base directory")
	}
	if sourceDir == "" {
		return PluginInstallResult{}, errors.New("source directory is required")
	}

	absSource, err := filepath.Abs(sourceDir)
	if err != nil {
		return PluginInstallResult{}, fmt.Errorf("resolve source: %w", err)
	}

	srcInfo, err := os.Stat(absSource)
	if err != nil {
		return PluginInstallResult{}, fmt.Errorf("source directory %q: %w", absSource, err)
	}
	if !srcInfo.IsDir() {
		return PluginInstallResult{}, fmt.Errorf("source %q is not a directory", absSource)
	}
	if _, err := os.Stat(PluginManifestPath(absSource)); err != nil {
		return PluginInstallResult{}, fmt.Errorf("source %q is not a plugin (missing .claude-plugin/plugin.json)", absSource)
	}

	target := PluginPath(baseDir)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return PluginInstallResult{}, err
	}

	res := PluginInstallResult{Path: target, SourceDir: absSource}

	info, err := os.Lstat(target)
	if err != nil {
		if !os.IsNotExist(err) {
			return PluginInstallResult{}, err
		}
		if err := os.Symlink(absSource, target); err != nil {
			return PluginInstallResult{}, fmt.Errorf("create symlink: %w", err)
		}
		res.Action = "created"
		return res, nil
	}

	// Path exists. If it's a symlink, replace if needed; if it's a real entry, refuse.
	if info.Mode()&os.ModeSymlink == 0 {
		return PluginInstallResult{}, fmt.Errorf("refusing to replace existing %s (not a symlink)\n\nIf this is a marketplace install, uninstall it first via Claude Code's plugin manager.\nIf it's stray, remove it manually: rm -rf %s", target, target)
	}

	currentTarget, err := os.Readlink(target)
	if err == nil {
		resolved := currentTarget
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(filepath.Dir(target), resolved)
		}
		if abs, err := filepath.Abs(resolved); err == nil {
			resolved = abs
		}
		if resolved == absSource {
			res.Action = "noop"
			return res, nil
		}
	}

	if err := os.Remove(target); err != nil {
		return PluginInstallResult{}, fmt.Errorf("remove existing symlink: %w", err)
	}
	if err := os.Symlink(absSource, target); err != nil {
		return PluginInstallResult{}, fmt.Errorf("create symlink: %w", err)
	}
	res.Action = "updated"
	return res, nil
}
