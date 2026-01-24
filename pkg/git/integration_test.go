package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testRepo creates a temporary git repo for testing.
func testRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup command %v failed: %v\n%s", args, err, out)
		}
	}

	// Some environments configure init.defaultBranch=main; force master to keep these tests stable.
	// Prefer init's -b flag (newer git), but fall back to plain init.
	{
		cmd := exec.Command("git", "init", "-b", "master")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			// Fall back for older git versions.
			_ = out
			run("init")
		}
	}
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	run("commit", "--allow-empty", "-m", "Initial commit")
	run("checkout", "-B", "master")

	return dir
}

// gitCmd runs a git command in the given directory.
func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestIsIntegrated_SameCommit(t *testing.T) {
	dir := testRepo(t)

	// Create feature branch at same commit as main
	gitCmd(t, dir, "checkout", "-b", "feature")

	// Both branches point to same commit
	reason := IsIntegrated(dir, "feature", "master")
	if reason != IntegratedSameCommit {
		t.Errorf("expected IntegratedSameCommit, got %q", reason)
	}
}

func TestIsIntegrated_Ancestor(t *testing.T) {
	dir := testRepo(t)

	// Create feature branch
	gitCmd(t, dir, "checkout", "-b", "feature")

	// Add a commit on master (so feature becomes ancestor of master)
	gitCmd(t, dir, "checkout", "master")
	gitCmd(t, dir, "commit", "--allow-empty", "-m", "Commit on master")

	// Feature is now ancestor of master
	reason := IsIntegrated(dir, "feature", "master")
	if reason != IntegratedAncestor {
		t.Errorf("expected IntegratedAncestor, got %q", reason)
	}
}

func TestIsIntegrated_NoAddedChanges(t *testing.T) {
	dir := testRepo(t)

	// Create feature branch with no file changes (just metadata commits)
	gitCmd(t, dir, "checkout", "-b", "feature")
	gitCmd(t, dir, "commit", "--allow-empty", "-m", "Empty commit on feature")

	// Merge feature into master (fast-forward)
	gitCmd(t, dir, "checkout", "master")
	gitCmd(t, dir, "merge", "feature")

	// Add another commit to master
	gitCmd(t, dir, "commit", "--allow-empty", "-m", "Another on master")

	// Feature has no file changes relative to master
	reason := IsIntegrated(dir, "feature", "master")
	// Should be Ancestor since feature is in master's history
	if reason != IntegratedAncestor {
		t.Errorf("expected IntegratedAncestor, got %q", reason)
	}
}

func TestIsIntegrated_TreesMatch(t *testing.T) {
	dir := testRepo(t)

	// Create a file on master
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content"), 0644)
	gitCmd(t, dir, "add", "file.txt")
	gitCmd(t, dir, "commit", "-m", "Add file")

	// Create feature branch and modify file
	gitCmd(t, dir, "checkout", "-b", "feature")
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("modified"), 0644)
	gitCmd(t, dir, "add", "file.txt")
	gitCmd(t, dir, "commit", "-m", "Modify file on feature")

	// Cherry-pick the same change to master (different commit, same tree)
	gitCmd(t, dir, "checkout", "master")
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("modified"), 0644)
	gitCmd(t, dir, "add", "file.txt")
	gitCmd(t, dir, "commit", "-m", "Same change on master")

	// Trees match even though history differs
	reason := IsIntegrated(dir, "feature", "master")
	if reason != IntegratedTreesMatch {
		t.Errorf("expected IntegratedTreesMatch, got %q", reason)
	}
}

func TestIsIntegrated_MergeAddsNothing(t *testing.T) {
	run := func(t *testing.T, force string) {
		t.Setenv(mergeSimForceEnvVar, force)

		dir := testRepo(t)

		// Create a file on master
		os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content"), 0644)
		gitCmd(t, dir, "add", "file.txt")
		gitCmd(t, dir, "commit", "-m", "Add file")

		// Create feature branch and add a second file
		gitCmd(t, dir, "checkout", "-b", "feature")
		os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature content"), 0644)
		gitCmd(t, dir, "add", "feature.txt")
		gitCmd(t, dir, "commit", "-m", "Add feature file")

		// Squash-merge feature into master (creates different history)
		gitCmd(t, dir, "checkout", "master")
		gitCmd(t, dir, "merge", "--squash", "feature")
		gitCmd(t, dir, "commit", "-m", "Squash merge feature")

		// Add an extra file on master so trees don't match
		// This prevents TreesMatch from triggering before MergeAddsNothing
		os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("extra"), 0644)
		gitCmd(t, dir, "add", "extra.txt")
		gitCmd(t, dir, "commit", "-m", "Add extra file on master")

		// Now: master has file.txt, feature.txt, extra.txt
		// Feature has file.txt, feature.txt
		// Trees don't match, but merging feature adds nothing
		reason := IsIntegrated(dir, "feature", "master")
		if reason != IntegratedMergeAddsNothing {
			t.Errorf("expected IntegratedMergeAddsNothing, got %q", reason)
		}
	}

	t.Run("merge-tree", func(t *testing.T) {
		if !mergeTreeWriteTreeSupported() {
			t.Skip("git merge-tree --write-tree not supported")
		}
		run(t, "merge-tree")
	})
	t.Run("index", func(t *testing.T) {
		run(t, "index")
	})
}

func TestIsIntegrated_NotIntegrated(t *testing.T) {
	dir := testRepo(t)

	// Create a file on master
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content"), 0644)
	gitCmd(t, dir, "add", "file.txt")
	gitCmd(t, dir, "commit", "-m", "Add file")

	// Create feature branch with a different file
	gitCmd(t, dir, "checkout", "-b", "feature")
	os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature content"), 0644)
	gitCmd(t, dir, "add", "feature.txt")
	gitCmd(t, dir, "commit", "-m", "Add feature file")

	// Feature is not integrated (has changes not in master)
	reason := IsIntegrated(dir, "feature", "master")
	if reason != "" {
		t.Errorf("expected empty (not integrated), got %q", reason)
	}
}

func TestIsIntegrated_BranchNotExists(t *testing.T) {
	dir := testRepo(t)

	// Non-existent branch should return empty
	reason := IsIntegrated(dir, "nonexistent", "master")
	if reason != "" {
		t.Errorf("expected empty for non-existent branch, got %q", reason)
	}
}

func TestBranchExists(t *testing.T) {
	dir := testRepo(t)

	if !BranchExists(dir, "master") {
		t.Error("expected master to exist")
	}

	if BranchExists(dir, "nonexistent") {
		t.Error("expected nonexistent to not exist")
	}
}

func TestEffectiveTarget_NoRemote(t *testing.T) {
	dir := testRepo(t)

	// Without remote, should return the target as-is
	target := EffectiveTarget(dir, "master")
	if target != "master" {
		t.Errorf("expected master, got %s", target)
	}
}

func TestEffectiveTarget_PrefersOriginWhenAhead(t *testing.T) {
	dir := testRepo(t)

	base := gitCmd(t, dir, "rev-parse", "master")

	// Create a new commit, then reset master back so origin/master can be "ahead".
	gitCmd(t, dir, "commit", "--allow-empty", "-m", "upstream commit")
	upstream := gitCmd(t, dir, "rev-parse", "master")
	gitCmd(t, dir, "reset", "--hard", strings.TrimSpace(base))

	// Ensure HasRemote(dir) is true.
	gitCmd(t, dir, "remote", "add", "origin", filepath.Join(t.TempDir(), "origin.git"))

	// Create a local origin/master tracking ref that points ahead of master.
	gitCmd(t, dir, "update-ref", "refs/remotes/origin/master", strings.TrimSpace(upstream))

	target := EffectiveTarget(dir, "master")
	if target != "origin/master" {
		t.Errorf("expected origin/master, got %s", target)
	}
}
