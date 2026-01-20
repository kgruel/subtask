package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeConflictFiles_NoConflicts(t *testing.T) {
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

func TestMergeConflictFiles_WithConflicts(t *testing.T) {
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
