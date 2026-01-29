package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeConflictFiles_NoConflicts(t *testing.T) {
	run := func(t *testing.T, force string) {
		t.Setenv(mergeSimForceEnvVar, force)

		dir := testRepo(t)

		// Base file on master.
		if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitCmd(t, dir, "add", "base.txt")
		gitCmd(t, dir, "commit", "-m", "add base")

		// Feature changes a different file.
		gitCmd(t, dir, "checkout", "-b", "feature")
		if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitCmd(t, dir, "add", "feature.txt")
		gitCmd(t, dir, "commit", "-m", "feature change")

		conflicts, err := MergeConflictFiles(dir, "master", "feature")
		if err != nil {
			t.Fatalf("MergeConflictFiles returned error: %v", err)
		}
		if len(conflicts) != 0 {
			t.Fatalf("expected no conflicts, got %v", conflicts)
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

func TestMergeConflictFiles_WithConflicts(t *testing.T) {
	run := func(t *testing.T, force string) {
		t.Setenv(mergeSimForceEnvVar, force)

		dir := testRepo(t)

		// Create a file on master.
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("base\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitCmd(t, dir, "add", "file.txt")
		gitCmd(t, dir, "commit", "-m", "base file")

		// Feature edits file.txt.
		gitCmd(t, dir, "checkout", "-b", "feature")
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("feature\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitCmd(t, dir, "add", "file.txt")
		gitCmd(t, dir, "commit", "-m", "feature edit")

		// Master edits file.txt differently.
		gitCmd(t, dir, "checkout", "master")
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("master\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitCmd(t, dir, "add", "file.txt")
		gitCmd(t, dir, "commit", "-m", "master edit")

		conflicts, err := MergeConflictFiles(dir, "master", "feature")
		if err != nil {
			t.Fatalf("MergeConflictFiles returned error: %v", err)
		}
		if strings.Join(conflicts, ",") != "file.txt" {
			t.Fatalf("expected [file.txt], got %v", conflicts)
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

func TestMergeConflictFiles_NoFalseConflictsOnDeletedFile_WithMultipleMergeBases(t *testing.T) {
	if !mergeTreeWriteTreeSupported() {
		t.Skip("git merge-tree --write-tree not supported")
	}
	t.Setenv(mergeSimForceEnvVar, "merge-tree")

	dir := testRepo(t)

	// Base file on master.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "a.txt")
	gitCmd(t, dir, "commit", "-m", "add a")

	// Branch A: modify a.txt.
	gitCmd(t, dir, "checkout", "-b", "A")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("A1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "a.txt")
	gitCmd(t, dir, "commit", "-m", "A1")
	a1 := strings.TrimSpace(gitCmd(t, dir, "rev-parse", "HEAD"))

	// Branch B: touch a different file.
	gitCmd(t, dir, "checkout", "master")
	gitCmd(t, dir, "checkout", "-b", "B")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("B1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "b.txt")
	gitCmd(t, dir, "commit", "-m", "B1")
	b1 := strings.TrimSpace(gitCmd(t, dir, "rev-parse", "HEAD"))

	// Create a criss-cross merge with multiple merge bases:
	// - A merges B (B1 is now a merge base)
	// - B merges A1 by commit hash (A1 is now a merge base)
	gitCmd(t, dir, "checkout", "A")
	gitCmd(t, dir, "merge", "--no-ff", "B", "-m", "merge B into A")
	gitCmd(t, dir, "checkout", "B")
	gitCmd(t, dir, "merge", "--no-ff", a1, "-m", "merge A1 into B")

	// A deletes a.txt (simulates "worker deleted file").
	gitCmd(t, dir, "checkout", "A")
	gitCmd(t, dir, "rm", "a.txt")
	gitCmd(t, dir, "commit", "-m", "A delete a")

	all := strings.Fields(strings.ReplaceAll(strings.TrimSpace(gitCmd(t, dir, "merge-base", "--all", "A", "B")), "\n", " "))
	if len(all) < 2 {
		t.Fatalf("expected multiple merge bases, got %v", all)
	}

	// `git merge-tree --write-tree A B` uses recursive merge-base selection and is clean.
	{
		cmd := exec.Command("git", "merge-tree", "--write-tree", "--name-only", "A", "B")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git merge-tree --write-tree A B unexpectedly conflicted: %v\n%s", err, out)
		}
	}

	// But forcing a single merge-base (the one returned by `git merge-base A B`) can create a false conflict.
	mb := strings.TrimSpace(gitCmd(t, dir, "merge-base", "A", "B"))
	if mb != b1 {
		t.Fatalf("expected git merge-base A B to pick B1=%s, got %s", b1, mb)
	}
	{
		cmd := exec.Command("git", "merge-tree", "--write-tree", "--name-only", "--merge-base", mb, "A", "B")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected forced merge-base merge-tree to conflict, but it succeeded:\n%s", out)
		}
		if !strings.Contains(string(out), "a.txt") {
			t.Fatalf("expected forced merge-base merge-tree to mention a.txt, got:\n%s", out)
		}
	}

	// Subtask should follow git's default merge-base selection and report no conflicts.
	conflicts, err := MergeConflictFiles(dir, "A", "B")
	if err != nil {
		t.Fatalf("MergeConflictFiles returned error: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %v", conflicts)
	}
}
