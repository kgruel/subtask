package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitsBehind(t *testing.T) {
	dir := testRepo(t)

	base := strings.TrimSpace(gitCmd(t, dir, "rev-parse", "HEAD"))
	gitCmd(t, dir, "commit", "--allow-empty", "-m", "one")
	gitCmd(t, dir, "commit", "--allow-empty", "-m", "two")
	gitCmd(t, dir, "commit", "--allow-empty", "-m", "three")

	behind, err := CommitsBehind(dir, base, "master")
	if err != nil {
		t.Fatalf("CommitsBehind returned error: %v", err)
	}
	if behind != 3 {
		t.Fatalf("expected 3 behind, got %d", behind)
	}
}

func TestOverlappingFiles(t *testing.T) {
	dir := testRepo(t)

	base := strings.TrimSpace(gitCmd(t, dir, "rev-parse", "HEAD"))

	// Worker branch changes a.txt + b.txt
	gitCmd(t, dir, "checkout", "-b", "worker")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("worker"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("worker"), 0o644)
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "worker changes")

	// Target branch changes a.txt + b.txt + c.txt
	gitCmd(t, dir, "checkout", "master")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("master"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("master"), 0o644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("master"), 0o644)
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "master changes")

	overlap, err := OverlappingFiles(dir, base, "worker", "master")
	if err != nil {
		t.Fatalf("OverlappingFiles returned error: %v", err)
	}
	want := []string{"a.txt", "b.txt"}
	if strings.Join(overlap, ",") != strings.Join(want, ",") {
		t.Fatalf("expected overlap %v, got %v", want, overlap)
	}
}
