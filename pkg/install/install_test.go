package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeSubtaskCheckout creates a minimal subtask checkout layout in dir:
// go.mod with the correct module path and pkg/install/SKILL.md with given content.
func writeSubtaskCheckout(t *testing.T, dir string, skillContent []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/kgruel/subtask\n\ngo 1.24\n"), 0o644))
	skillDir := filepath.Join(dir, "pkg", "install")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), skillContent, 0o644))
}

func TestDetectLocalSKILL_NotInCheckout(t *testing.T) {
	dir := t.TempDir()
	content, path, found, err := DetectLocalSKILL(dir)
	require.NoError(t, err)
	require.False(t, found)
	require.Empty(t, path)
	require.Nil(t, content)
}

func TestDetectLocalSKILL_SubtaskCheckout(t *testing.T) {
	dir := t.TempDir()
	wantContent := []byte("# skill content from repo\n")
	writeSubtaskCheckout(t, dir, wantContent)

	content, path, found, err := DetectLocalSKILL(dir)
	require.NoError(t, err)
	require.True(t, found)
	// EvalSymlinks may resolve platform-level symlinks (e.g. /var -> /private/var on macOS).
	wantPath, _ := filepath.EvalSymlinks(filepath.Join(dir, "pkg", "install", "SKILL.md"))
	require.Equal(t, wantPath, path)
	require.Equal(t, wantContent, content)
}

func TestDetectLocalSKILL_SubtaskCheckout_FromSubdir(t *testing.T) {
	root := t.TempDir()
	wantContent := []byte("# skill from repo root\n")
	writeSubtaskCheckout(t, root, wantContent)

	// Detect from a subdirectory — should walk up and find the root.
	subdir := filepath.Join(root, "cmd", "subtask")
	require.NoError(t, os.MkdirAll(subdir, 0o755))

	content, path, found, err := DetectLocalSKILL(subdir)
	require.NoError(t, err)
	require.True(t, found)
	wantPath, _ := filepath.EvalSymlinks(filepath.Join(root, "pkg", "install", "SKILL.md"))
	require.Equal(t, wantPath, path)
	require.Equal(t, wantContent, content)
}

func TestDetectLocalSKILL_WrongModule(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/someone/else\n\ngo 1.24\n"), 0o644))
	skillDir := filepath.Join(dir, "pkg", "install")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("content"), 0o644))

	_, _, found, err := DetectLocalSKILL(dir)
	require.NoError(t, err)
	require.False(t, found)
}

func TestDetectLocalSKILL_CheckoutButUnreadableSKILL(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/kgruel/subtask\n\ngo 1.24\n"), 0o644))
	// Create the pkg/install dir but NOT the SKILL.md file.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "pkg", "install"), 0o755))

	content, path, found, err := DetectLocalSKILL(dir)
	require.Error(t, err)
	require.True(t, found)
	require.NotEmpty(t, path)
	require.Nil(t, content)
}

func TestDetectLocalSKILL_SymlinkedCwd(t *testing.T) {
	// Simulate: cwd is a symlink that resolves into a subtask checkout subdir.
	// DetectLocalSKILL must resolve the symlink before walking, otherwise it
	// never reaches the real repo root and returns found=false.
	root := t.TempDir()
	wantContent := []byte("# skill via symlink\n")
	writeSubtaskCheckout(t, root, wantContent)

	// Create a symlink pointing at a subdir of the checkout.
	linkTarget := filepath.Join(root, "cmd", "subtask")
	require.NoError(t, os.MkdirAll(linkTarget, 0o755))
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "link")
	require.NoError(t, os.Symlink(linkTarget, link))

	content, path, found, err := DetectLocalSKILL(link)
	require.NoError(t, err)
	require.True(t, found, "should detect checkout via symlinked cwd")
	wantPath, _ := filepath.EvalSymlinks(filepath.Join(root, "pkg", "install", "SKILL.md"))
	require.Equal(t, wantPath, path)
	require.Equal(t, wantContent, content)
}

func TestInstallTo_ContentOverride(t *testing.T) {
	home := t.TempDir()
	custom := []byte("# custom skill content\n")

	path, updated, err := InstallTo(home, custom)
	require.NoError(t, err)
	require.True(t, updated)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, custom, got)

	// Second call with same content should be a noop.
	_, updated, err = InstallTo(home, custom)
	require.NoError(t, err)
	require.False(t, updated)
}

func TestInstallTo_WritesEmbeddedSkill(t *testing.T) {
	home := t.TempDir()

	path, updated, err := InstallTo(home)
	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, filepath.Join(home, ".claude", "skills", "subtask", "SKILL.md"), path)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, Embedded(), got)
}

func TestUninstallFrom_RemovesSkillFile(t *testing.T) {
	home := t.TempDir()

	path, _, err := InstallTo(home)
	require.NoError(t, err)

	_, err = os.Stat(path)
	require.NoError(t, err)

	removedPath, err := UninstallFrom(home)
	require.NoError(t, err)
	require.Equal(t, path, removedPath)

	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestGetSkillStatusFor(t *testing.T) {
	home := t.TempDir()

	st, err := GetSkillStatusFor(home)
	require.NoError(t, err)
	require.False(t, st.Installed)
	require.False(t, st.UpToDate)
	require.NotEmpty(t, st.Path)
	require.Len(t, st.EmbeddedSHA256, 64)
	require.Empty(t, st.InstalledSHA256)

	_, _, err = InstallTo(home)
	require.NoError(t, err)

	st, err = GetSkillStatusFor(home)
	require.NoError(t, err)
	require.True(t, st.Installed)
	require.True(t, st.UpToDate)
	require.Len(t, st.InstalledSHA256, 64)

	// Drift the installed skill.
	require.NoError(t, os.WriteFile(st.Path, []byte("different"), 0o644))

	st, err = GetSkillStatusFor(home)
	require.NoError(t, err)
	require.True(t, st.Installed)
	require.False(t, st.UpToDate)
}
