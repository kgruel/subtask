package install

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/kgruel/subtask/internal/homedir"
)

//go:embed SKILL.md
var embeddedSkill []byte

// SkillStatus describes the installation state of the embedded skill.
type SkillStatus struct {
	Path            string
	Installed       bool
	UpToDate        bool
	EmbeddedSHA256  string
	InstalledSHA256 string
}

// Embedded returns the embedded skill contents.
func Embedded() []byte {
	return bytes.Clone(embeddedSkill)
}

// SkillPath returns the Claude Code skill path for the given base directory (usually the user's home directory).
func SkillPath(baseDir string) string {
	if baseDir == "" {
		return ""
	}
	return filepath.Join(baseDir, ".claude", "skills", "subtask", "SKILL.md")
}

// Install writes the embedded skill to the Claude Code skill location (user scope).
func Install() (string, bool, error) {
	homeDir, err := homedir.Dir()
	if err != nil {
		return "", false, err
	}
	return InstallTo(homeDir)
}

// InstallTo writes the skill to the Claude Code skill location under baseDir (user scope).
// Pass content to override the embedded skill (e.g. from a local repo checkout).
func InstallTo(baseDir string, content ...[]byte) (string, bool, error) {
	path := SkillPath(baseDir)
	if path == "" {
		return "", false, errors.New("invalid base directory")
	}
	data := embeddedSkill
	if len(content) > 0 {
		data = content[0]
	}
	return syncToPath(path, data)
}

// InstallToProject writes the skill to the project-scoped Claude Code skill location.
// projectRoot should be the git root of the project.
// Pass content to override the embedded skill (e.g. from a local repo checkout).
func InstallToProject(projectRoot string, content ...[]byte) (string, bool, error) {
	path := ProjectSkillPath(projectRoot)
	if path == "" {
		return "", false, errors.New("invalid project root")
	}
	data := embeddedSkill
	if len(content) > 0 {
		data = content[0]
	}
	return syncToPath(path, data)
}

// ProjectSkillPath returns the Claude Code skill path for project scope.
func ProjectSkillPath(projectRoot string) string {
	if projectRoot == "" {
		return ""
	}
	return filepath.Join(projectRoot, ".claude", "skills", "subtask", "SKILL.md")
}

// Uninstall removes the skill from the Claude Code skill location (user scope).
func Uninstall() (string, error) {
	homeDir, err := homedir.Dir()
	if err != nil {
		return "", err
	}
	return UninstallFrom(homeDir)
}

// UninstallFrom removes the skill from the Claude Code skill location under baseDir.
func UninstallFrom(baseDir string) (string, error) {
	path := SkillPath(baseDir)
	if path == "" {
		return "", errors.New("invalid base directory")
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}

// GetSkillStatus returns installation status for the Claude Code skill location (user scope).
func GetSkillStatus() (SkillStatus, error) {
	homeDir, err := homedir.Dir()
	if err != nil {
		return SkillStatus{}, err
	}
	return GetSkillStatusFor(homeDir)
}

// GetSkillStatusFor returns status for baseDir without consulting environment.
func GetSkillStatusFor(baseDir string) (SkillStatus, error) {
	path := SkillPath(baseDir)
	if path == "" {
		return SkillStatus{}, errors.New("invalid base directory")
	}

	embeddedSHA := sha256Hex(embeddedSkill)
	st := SkillStatus{
		Path:           path,
		Installed:      false,
		UpToDate:       false,
		EmbeddedSHA256: embeddedSHA,
	}

	installed, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return SkillStatus{}, err
	}

	st.Installed = true
	st.InstalledSHA256 = sha256Hex(installed)
	st.UpToDate = bytes.Equal(installed, embeddedSkill)
	return st, nil
}

// DetectLocalSKILL walks from cwd toward the filesystem root, looking for a
// subtask checkout: a directory containing both a go.mod whose first module
// declaration is "module github.com/kgruel/subtask" and a pkg/install/SKILL.md
// file.
//
// Return values:
//   - found=false, err=nil  — not in a subtask checkout; use the embed
//   - found=true,  err=nil  — repo SKILL loaded successfully
//   - found=true,  err!=nil — checkout detected but SKILL unreadable; caller must surface error
//
// Note: forks with a different module path will not trigger repo-aware mode
// (v1 limitation — accepted because the module path is the canonical identity).
func DetectLocalSKILL(cwd string) (content []byte, path string, found bool, err error) {
	// Resolve symlinks so that a cwd like /tmp/link -> <repo>/cmd/subtask
	// walks through the real path and finds the repo root.
	if resolved, resolveErr := filepath.EvalSymlinks(cwd); resolveErr == nil {
		cwd = resolved
	}
	dir := cwd
	for {
		gomod := filepath.Join(dir, "go.mod")
		if _, statErr := os.Stat(gomod); statErr == nil {
			if isSubtaskModule(gomod) {
				skillPath := filepath.Join(dir, "pkg", "install", "SKILL.md")
				data, readErr := os.ReadFile(skillPath)
				if readErr != nil {
					return nil, skillPath, true, readErr
				}
				return data, skillPath, true, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return nil, "", false, nil
}

// isSubtaskModule returns true if the go.mod at path declares
// "module github.com/kgruel/subtask" as its module path.
func isSubtaskModule(gomodPath string) bool {
	f, err := os.Open(gomodPath)
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if mod, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(mod) == "github.com/kgruel/subtask"
		}
	}
	return false
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// syncToPath writes data to destPath if it differs from the current contents.
// Returns (path, updated, err).
func syncToPath(destPath string, data []byte) (string, bool, error) {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return "", false, err
	}
	if existing, err := os.ReadFile(destPath); err == nil && bytes.Equal(existing, data) {
		return destPath, false, nil
	}
	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		return "", false, err
	}
	return destPath, true, nil
}

func isSkillInstalled(baseDir string) bool {
	path := SkillPath(baseDir)
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
