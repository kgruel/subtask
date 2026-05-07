package install

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kgruel/subtask/internal/homedir"
	pluginembed "github.com/kgruel/subtask/plugin"
)

// ownerMarkerFile is written at the plugin root when we install it.
// Its presence marks the directory as binary-managed (not marketplace).
const ownerMarkerFile = ".subtask-binary-installed"

// PluginStatus describes the installed state of the subtask Claude Code plugin.
//
// The plugin lives at ~/.claude/plugins/subtask and may be:
//   - absent (Exists == false)
//   - a symlink to a developer working tree (IsSymlink == true)
//   - a real directory installed via Claude Code's plugin marketplace
//   - a real directory installed by the subtask binary (IsBinaryInstalled == true)
//
// HasManifest reports whether the plugin actually looks valid — i.e., contains
// .claude-plugin/plugin.json. A symlink to a missing or invalid path can leave
// Exists == true but HasManifest == false.
type PluginStatus struct {
	Path              string
	Exists            bool
	IsSymlink         bool
	SymlinkTarget     string
	HasManifest       bool
	IsBinaryInstalled bool // true when ownerMarkerFile is present (we installed it)
}

// PluginBinaryResult reports the outcome of InstallPluginBinaryTo or UninstallPluginBinaryFrom.
type PluginBinaryResult struct {
	Path   string
	Action string // "installed", "updated", "noop", "dev_link", "marketplace", "stray", "removed", "nothing"
	Note   string // human-readable explanation for non-install actions
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

	if _, err := os.Stat(filepath.Join(path, ownerMarkerFile)); err == nil {
		st.IsBinaryInstalled = true
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

// InstallPluginBinary installs the embedded plugin to ~/.claude/plugins/subtask.
// version is written into the owner marker for diagnostics.
func InstallPluginBinary(version string) (PluginBinaryResult, error) {
	homeDir, err := homedir.Dir()
	if err != nil {
		return PluginBinaryResult{}, err
	}
	return InstallPluginBinaryTo(homeDir, version)
}

// InstallPluginBinaryTo is the testable form of InstallPluginBinary.
//
// Decision matrix:
//   - absent                    → create dir, write files, drop marker → "installed"
//   - symlink + valid manifest  → leave it (dev link)                  → "dev_link"
//   - symlink + no manifest     → warn, skip                           → "stray"
//   - real dir + our marker     → rewrite files, idempotent            → "updated" or "noop"
//   - real dir + no marker      → warn, skip (marketplace)             → "marketplace"
func InstallPluginBinaryTo(baseDir, version string) (PluginBinaryResult, error) {
	if baseDir == "" {
		return PluginBinaryResult{}, errors.New("invalid base directory")
	}
	pluginPath := PluginPath(baseDir)
	res := PluginBinaryResult{Path: pluginPath}

	info, err := os.Lstat(pluginPath)
	if err != nil && !os.IsNotExist(err) {
		return PluginBinaryResult{}, err
	}

	if err != nil {
		// Path does not exist — fresh install.
		if err := writePluginFiles(pluginPath, version); err != nil {
			return PluginBinaryResult{}, err
		}
		res.Action = "installed"
		return res, nil
	}

	if info.Mode()&os.ModeSymlink != 0 {
		// Symlink — check if it resolves to a valid plugin.
		st, _ := GetPluginStatusFor(baseDir)
		if st.HasManifest {
			res.Action = "dev_link"
			res.Note = fmt.Sprintf("linked (dev) at %s", pluginPath)
		} else {
			res.Action = "stray"
			res.Note = fmt.Sprintf("symlink at %s points to missing or invalid target; skipping", pluginPath)
		}
		return res, nil
	}

	// Real directory — check for our marker.
	markerPath := filepath.Join(pluginPath, ownerMarkerFile)
	if _, err := os.Stat(markerPath); err != nil {
		// No marker — marketplace or manual install; don't clobber.
		res.Action = "marketplace"
		res.Note = fmt.Sprintf("%s exists without our ownership marker; leaving it alone", pluginPath)
		return res, nil
	}

	// Our directory — check if files need updating.
	changed, err := pluginFilesChanged(pluginPath)
	if err != nil {
		return PluginBinaryResult{}, err
	}
	if !changed {
		res.Action = "noop"
		return res, nil
	}
	if err := writePluginFiles(pluginPath, version); err != nil {
		return PluginBinaryResult{}, err
	}
	res.Action = "updated"
	return res, nil
}

// UninstallPluginBinary removes the binary-installed plugin from ~/.claude/plugins/subtask.
func UninstallPluginBinary() (PluginBinaryResult, error) {
	homeDir, err := homedir.Dir()
	if err != nil {
		return PluginBinaryResult{}, err
	}
	return UninstallPluginBinaryFrom(homeDir)
}

// UninstallPluginBinaryFrom is the testable form of UninstallPluginBinary.
//
// Decision matrix:
//   - absent                    → "nothing"
//   - symlink + valid manifest  → leave it (dev link)        → "dev_link"
//   - symlink + no manifest     → leave it (stray)           → "stray"
//   - real dir + our marker     → remove the directory       → "removed"
//   - real dir + no marker      → leave it, note marketplace → "marketplace"
//
// Symlinks are never auto-removed: they're either deliberate developer setups
// (`subtask install --plugin-dev`) or pre-existing state we don't own. Defaults
// preserve (CLAUDE.md design principle #8).
func UninstallPluginBinaryFrom(baseDir string) (PluginBinaryResult, error) {
	if baseDir == "" {
		return PluginBinaryResult{}, errors.New("invalid base directory")
	}
	pluginPath := PluginPath(baseDir)
	res := PluginBinaryResult{Path: pluginPath}

	info, err := os.Lstat(pluginPath)
	if err != nil {
		if os.IsNotExist(err) {
			res.Action = "nothing"
			return res, nil
		}
		return PluginBinaryResult{}, err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		st, _ := GetPluginStatusFor(baseDir)
		if st.HasManifest {
			res.Action = "dev_link"
			res.Note = fmt.Sprintf("dev symlink at %s preserved; remove manually with: rm %s", pluginPath, pluginPath)
		} else {
			res.Action = "stray"
			res.Note = fmt.Sprintf("symlink at %s points to a missing or invalid target; preserved (remove manually with: rm %s)", pluginPath, pluginPath)
		}
		return res, nil
	}

	// Real directory.
	markerPath := filepath.Join(pluginPath, ownerMarkerFile)
	if _, err := os.Stat(markerPath); err != nil {
		res.Action = "marketplace"
		res.Note = "marketplace-installed plugin was not removed; uninstall with: /plugin uninstall subtask"
		return res, nil
	}

	if err := os.RemoveAll(pluginPath); err != nil {
		return PluginBinaryResult{}, fmt.Errorf("remove plugin directory: %w", err)
	}
	res.Action = "removed"
	return res, nil
}

// writePluginFiles writes the embedded plugin tree to pluginPath, setting
// executable permissions on .sh files and dropping the owner marker.
func writePluginFiles(pluginPath, version string) error {
	if err := os.MkdirAll(pluginPath, 0o755); err != nil {
		return err
	}

	err := fs.WalkDir(pluginembed.Files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		dest := filepath.Join(pluginPath, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := pluginembed.Files.ReadFile(path)
		if err != nil {
			return err
		}
		perm := fs.FileMode(0o644)
		if strings.HasSuffix(path, ".sh") {
			perm = 0o755
		}
		return os.WriteFile(dest, data, perm)
	})
	if err != nil {
		return err
	}

	// Drop/update the owner marker.
	markerPath := filepath.Join(pluginPath, ownerMarkerFile)
	return os.WriteFile(markerPath, []byte(version+"\n"), 0o644)
}

// pluginFilesChanged reports whether any embedded file differs from disk.
// Returns true if any file is missing or has different content.
func pluginFilesChanged(pluginPath string) (bool, error) {
	var changed bool
	err := fs.WalkDir(pluginembed.Files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		embedded, err := pluginembed.Files.ReadFile(path)
		if err != nil {
			return err
		}
		dest := filepath.Join(pluginPath, filepath.FromSlash(path))
		ondisk, err := os.ReadFile(dest)
		if err != nil || !bytes.Equal(embedded, ondisk) {
			changed = true
		}
		return nil
	})
	return changed, err
}
