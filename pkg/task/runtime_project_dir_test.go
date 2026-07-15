package task

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgruel/subtask/internal/pathesc"
)

// deepRoot escapes to more than pathesc.MaxLen chars, so it is a root whose
// name changed shape when the cap landed. Roots this deep are exactly the ones
// an older subtask would have given a long, uncapped directory.
var deepRoot = filepath.Join(string(filepath.Separator),
	"Users", "someone", "Documents", "work", "clients", "acme",
	"backend-services", "platform", "api-gateway", "checkout")

// TestRuntimeProjectDir_ShortRootUnaffected pins the no-op case: a root that
// already fits under the cap must resolve exactly where it always did, with no
// legacy lookup involved. This is why the cap needs no migration for the vast
// majority of installs.
func TestRuntimeProjectDir_ShortRootUnaffected(t *testing.T) {
	t.Setenv("SUBTASK_DIR", t.TempDir())

	root := filepath.Join(string(filepath.Separator), "Users", "zippo", "Code", "finality")
	raw := pathesc.Raw(root)
	if len(raw) > pathesc.MaxLen {
		t.Fatalf("test precondition broken: %q is already over MaxLen", raw)
	}

	want := filepath.Join(ProjectsDir(), raw)
	if got := RuntimeProjectDir(root); got != want {
		t.Errorf("RuntimeProjectDir(%q) = %q, want %q", root, got, want)
	}
}

// TestRuntimeProjectDir_PrefersExistingLegacyDir is the backward-compatibility
// contract. State written under the pre-cap name holds workspace assignments and
// session IDs; silently moving to the capped name would strand live tasks. If
// the legacy directory is on disk, it wins.
func TestRuntimeProjectDir_PrefersExistingLegacyDir(t *testing.T) {
	t.Setenv("SUBTASK_DIR", t.TempDir())

	legacy := pathesc.Raw(deepRoot)
	if len(legacy) <= pathesc.MaxLen {
		t.Fatalf("test precondition broken: %q does not exceed MaxLen (%d)", legacy, pathesc.MaxLen)
	}

	legacyDir := filepath.Join(ProjectsDir(), legacy)
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := RuntimeProjectDir(deepRoot); got != legacyDir {
		t.Errorf("RuntimeProjectDir did not prefer the existing legacy dir\n got: %q\nwant: %q", got, legacyDir)
	}
}

// TestRuntimeProjectDir_CapsWhenNoLegacyDir covers the fresh-install side of the
// same root: with nothing on disk, a new name is capped.
func TestRuntimeProjectDir_CapsWhenNoLegacyDir(t *testing.T) {
	t.Setenv("SUBTASK_DIR", t.TempDir())

	got := RuntimeProjectDir(deepRoot)
	name := filepath.Base(got)

	if len(name) > pathesc.MaxLen {
		t.Errorf("RuntimeProjectDir = %q, component is %d chars, want <= %d", got, len(name), pathesc.MaxLen)
	}
	if name != pathesc.Escape(deepRoot) {
		t.Errorf("RuntimeProjectDir component = %q, want the capped name %q", name, pathesc.Escape(deepRoot))
	}
	if !strings.HasPrefix(got, ProjectsDir()) {
		t.Errorf("RuntimeProjectDir = %q, want it under %q", got, ProjectsDir())
	}
}
