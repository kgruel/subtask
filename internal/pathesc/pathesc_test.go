package pathesc

import (
	"path/filepath"
	"strings"
	"testing"
)

// deepRoot is longer than MaxLen once escaped — the shape that broke `git
// worktree add` on Windows with "'$GIT_DIR' too big".
const deepRoot = "/Users/runneradmin/AppData/Local/Temp/TestPoolAcquire_CreatesFirstWorkspaceWhenNoneExist4057403907/002"

// TestEscape_NeverExceedsMaxLen is the ratchet: the cap is the whole point of
// this package, so it is asserted as a property over a table rather than left to
// review vigilance. Every entry must hold for any path, however deep.
func TestEscape_NeverExceedsMaxLen(t *testing.T) {
	for _, p := range []string{
		"/",
		"/a",
		"/Users/zippo/Code/finality",
		deepRoot,
		deepRoot + deepRoot,
		strings.Repeat("/aaaaaaaaaaaaaaaa", 64),
		`C:\Users\runneradmin\AppData\Local\Temp\` + strings.Repeat("x", 300),
	} {
		if got := Escape(p); len(got) > MaxLen {
			t.Errorf("Escape(%.40q...) = %d chars, want <= %d\n  got: %s", p, len(got), MaxLen, got)
		}
	}
}

// TestEscape_ShortPathsUnchanged pins the backward-compatibility contract: a
// root that already fits keeps its pre-cap name byte-for-byte, so existing
// workspaces and project state on disk keep resolving. Only over-long names —
// the ones that were already broken on Windows — change shape.
func TestEscape_ShortPathsUnchanged(t *testing.T) {
	p := "/Users/zippo/Code/finality"
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	want := Raw(abs)
	if got := Escape(p); got != want {
		t.Errorf("Escape(%q) = %q, want unchanged %q", p, got, want)
	}
	if len(want) > MaxLen {
		t.Fatalf("test precondition broken: %q is already over MaxLen", want)
	}
}

// TestEscape_TruncatedKeepsTailAndDisambiguates covers what truncation must not
// lose: the repo-identifying tail stays readable, and two roots that differ only
// in a truncated-away prefix must not collide onto one workspace.
func TestEscape_TruncatedKeepsTailAndDisambiguates(t *testing.T) {
	a := Escape("/one/very/long/prefix/that/gets/truncated/away/entirely/here/myrepo")
	b := Escape("/another/very/long/prefix/that/gets/truncated/away/too/here/myrepo")

	if !strings.HasSuffix(a, "myrepo") {
		t.Errorf("Escape lost the identifying tail: %q", a)
	}
	if a == b {
		t.Errorf("distinct roots collided onto %q", a)
	}
}

// TestTruncate_StableNaming pins the exact escaped name for a known input.
//
// The convention is persisted: it names directories that already exist on users'
// disks, and a task finds its workspace by re-deriving the name. Changing the
// hash width, the tail length, or the separator would silently strand every
// workspace whose root is over MaxLen. If this test fails, that is the change
// you are making — the fix is a migration, not a new golden value.
func TestTruncate_StableNaming(t *testing.T) {
	const in = "-Users-runneradmin-AppData-Local-Temp-TestPoolAcquire_CreatesFirstWorkspaceWhenNoneExist4057403907-002"
	const want = "7400b6ee-cquire_CreatesFirstWorkspaceWhenNoneExist4057403907-002"

	got := Truncate(in)
	if got != want {
		t.Errorf("Truncate(%q)\n  = %q\n want %q", in, got, want)
	}
	if len(got) > MaxLen {
		t.Errorf("Truncate produced %d chars, want <= %d", len(got), MaxLen)
	}
}

// TestEscape_NoPathSeparators ensures the result is a single component — it is
// always Joined as one directory name.
func TestEscape_NoPathSeparators(t *testing.T) {
	for _, p := range []string{"/Users/zippo/Code/finality", deepRoot} {
		got := Escape(p)
		if strings.ContainsRune(got, filepath.Separator) || strings.ContainsAny(got, `/\:`) {
			t.Errorf("Escape(%q) = %q, want no separators or colons", p, got)
		}
	}
}
