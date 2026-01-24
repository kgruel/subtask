package git

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCleanupStaleMergeSimTmpDirs_RemovesOldDirs(t *testing.T) {
	// Create a fake stale directory in os.TempDir().
	tmp := os.TempDir()
	stale, err := os.MkdirTemp(tmp, mergeSimTmpDirPrefix)
	if err != nil {
		t.Fatal(err)
	}

	// Make it look old enough to be cleaned.
	old := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	cleanupStaleMergeSimTmpDirs()

	if _, err := os.Stat(stale); err == nil {
		t.Fatalf("expected stale mergesim dir to be removed: %s", stale)
	}
}

func TestSimulateMerge_Concurrent_Index(t *testing.T) {
	t.Setenv(mergeSimForceEnvVar, "index")

	dir := testRepo(t)

	// Create a simple non-conflicting change on feature.
	gitCmd(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "feature.txt")
	gitCmd(t, dir, "commit", "-m", "feature")

	// Create a different change on master.
	gitCmd(t, dir, "checkout", "master")
	if err := os.WriteFile(filepath.Join(dir, "master.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "master.txt")
	gitCmd(t, dir, "commit", "-m", "master")

	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := simulateMerge(dir, "master", "feature")
			errs <- err
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("simulateMerge returned error: %v", err)
		}
	}
}
