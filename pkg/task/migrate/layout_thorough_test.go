package migrate

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/internal/filelock"
	"github.com/zippoxer/subtask/pkg/task"
)

func TestEnsureLayout_HappyPath_CopiesSidecarsAndDeletesLegacy(t *testing.T) {
	t.Setenv("SUBTASK_DIR", t.TempDir())

	repoRoot := t.TempDir()
	require.NoError(t, copyDir(filepath.Join("testdata", "legacy", "basic"), filepath.Join(repoRoot, ".subtask")))

	// Simulate sqlite sidecars.
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, ".subtask", "index.db-wal"), []byte("wal"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, ".subtask", "index.db-shm"), []byte("shm"), 0o644))

	require.NoError(t, EnsureLayout(repoRoot))

	projectDir := filepath.Join(task.ProjectsDir(), task.EscapePath(repoRoot))
	require.FileExists(t, filepath.Join(projectDir, "index.db"))
	require.FileExists(t, filepath.Join(projectDir, "index.db-wal"))
	require.FileExists(t, filepath.Join(projectDir, "index.db-shm"))

	// Legacy runtime state removed from repo.
	require.NoDirExists(t, filepath.Join(repoRoot, ".subtask", "internal"))
	require.NoFileExists(t, filepath.Join(repoRoot, ".subtask", "index.db"))
	require.NoFileExists(t, filepath.Join(repoRoot, ".subtask", "index.db-wal"))
	require.NoFileExists(t, filepath.Join(repoRoot, ".subtask", "index.db-shm"))
}

func TestEnsureLayout_IdempotentAcrossProcesses_SecondRunNoOp(t *testing.T) {
	t.Setenv("SUBTASK_DIR", t.TempDir())

	repoRoot := t.TempDir()
	require.NoError(t, copyDir(filepath.Join("testdata", "legacy", "basic"), filepath.Join(repoRoot, ".subtask")))

	require.NoError(t, runEnsureLayoutSubprocess(t, repoRoot))

	projectDir := filepath.Join(task.ProjectsDir(), task.EscapePath(repoRoot))
	statePath := filepath.Join(projectDir, "internal", "legacy--basic", "state.json")
	before := readFileString(t, statePath)

	require.NoError(t, runEnsureLayoutSubprocess(t, repoRoot))

	after := readFileString(t, statePath)
	require.Equal(t, before, after)
}

func TestEnsureLayout_PartialDestExists_MergeNoClobber(t *testing.T) {
	t.Setenv("SUBTASK_DIR", t.TempDir())

	repoRoot := t.TempDir()
	require.NoError(t, copyDir(filepath.Join("testdata", "legacy", "basic"), filepath.Join(repoRoot, ".subtask")))

	projectDir := filepath.Join(task.ProjectsDir(), task.EscapePath(repoRoot))
	destInternal := filepath.Join(projectDir, "internal")
	require.NoError(t, os.MkdirAll(destInternal, 0o755))

	// Pre-create a file that should not be overwritten by mergeDirNoClobber.
	preexistingState := filepath.Join(destInternal, "legacy--basic", "state.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(preexistingState), 0o755))
	require.NoError(t, os.WriteFile(preexistingState, []byte(`{"preexisting":true}`), 0o644))

	// Pre-create index.db so migration doesn't overwrite it.
	destIndex := filepath.Join(projectDir, "index.db")
	require.NoError(t, os.WriteFile(destIndex, []byte("dest-index"), 0o644))

	require.NoError(t, EnsureLayout(repoRoot))

	require.Equal(t, `{"preexisting":true}`, strings.TrimSpace(readFileString(t, preexistingState)))
	require.Equal(t, "dest-index", strings.TrimSpace(readFileString(t, destIndex)))

	// Files that didn't exist should be copied.
	require.FileExists(t, filepath.Join(destInternal, "legacy--basic", "progress.json"))
	require.FileExists(t, filepath.Join(destInternal, "legacy--basic", "op.lock"))

	// Legacy sources removed.
	require.NoDirExists(t, filepath.Join(repoRoot, ".subtask", "internal"))
	require.NoFileExists(t, filepath.Join(repoRoot, ".subtask", "index.db"))
}

func TestEnsureLayout_ConcurrentMigrations_SerializedByLock(t *testing.T) {
	t.Setenv("SUBTASK_DIR", t.TempDir())

	repoRoot := t.TempDir()
	require.NoError(t, copyDir(filepath.Join("testdata", "legacy", "basic"), filepath.Join(repoRoot, ".subtask")))

	projectDir := filepath.Join(task.ProjectsDir(), task.EscapePath(repoRoot))
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	lockPath := filepath.Join(projectDir, "migrate.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lockFile.Close() })
	require.NoError(t, filelock.LockExclusive(lockFile))

	cmd1, out1 := startEnsureLayoutSubprocess(t, repoRoot)
	cmd2, out2 := startEnsureLayoutSubprocess(t, repoRoot)
	t.Cleanup(func() {
		_ = cmd1.Process.Kill()
		_ = cmd2.Process.Kill()
	})

	done1 := waitCmdAsync(cmd1)
	done2 := waitCmdAsync(cmd2)

	select {
	case err := <-done1:
		t.Fatalf("process 1 finished unexpectedly while lock held: %v\n%s", err, out1.String())
	case <-time.After(500 * time.Millisecond):
	}
	select {
	case err := <-done2:
		t.Fatalf("process 2 finished unexpectedly while lock held: %v\n%s", err, out2.String())
	case <-time.After(10 * time.Millisecond):
	}

	require.NoError(t, filelock.Unlock(lockFile))

	require.NoError(t, waitWithTimeout(done1, 5*time.Second), out1.String())
	require.NoError(t, waitWithTimeout(done2, 5*time.Second), out2.String())

	// Migration succeeded and cleaned up legacy runtime.
	require.NoDirExists(t, filepath.Join(repoRoot, ".subtask", "internal"))
	require.NoFileExists(t, filepath.Join(repoRoot, ".subtask", "index.db"))
	require.FileExists(t, filepath.Join(projectDir, "internal", "legacy--basic", "state.json"))
	require.FileExists(t, filepath.Join(projectDir, "index.db"))
}

func TestEnsureLayout_CrashAfterCopyBeforeDelete_RerunCleansUp(t *testing.T) {
	t.Setenv("SUBTASK_DIR", t.TempDir())

	repoRoot := t.TempDir()
	require.NoError(t, copyDir(filepath.Join("testdata", "legacy", "basic"), filepath.Join(repoRoot, ".subtask")))

	projectDir := filepath.Join(task.ProjectsDir(), task.EscapePath(repoRoot))
	legacyInternal := filepath.Join(repoRoot, ".subtask", "internal")
	legacyIndex := filepath.Join(repoRoot, ".subtask", "index.db")
	destInternal := filepath.Join(projectDir, "internal")
	destIndex := filepath.Join(projectDir, "index.db")

	require.NoError(t, os.MkdirAll(destInternal, 0o755))
	require.NoError(t, mergeDirNoClobber(legacyInternal, destInternal))
	require.NoError(t, copyFileAtomic(legacyIndex, destIndex))

	// "Crash": legacy sources still exist, but dest already has copied data.
	require.DirExists(t, legacyInternal)
	require.FileExists(t, legacyIndex)
	require.FileExists(t, filepath.Join(destInternal, "legacy--basic", "state.json"))
	require.FileExists(t, destIndex)

	require.NoError(t, EnsureLayout(repoRoot))

	require.NoDirExists(t, legacyInternal)
	require.NoFileExists(t, legacyIndex)
}

func TestEnsureLayout_NoLegacyRuntime_NoOp(t *testing.T) {
	t.Setenv("SUBTASK_DIR", t.TempDir())

	repoRoot := t.TempDir()
	require.NoError(t, EnsureLayout(repoRoot))

	// Does not create repo-local runtime state.
	require.NoDirExists(t, filepath.Join(repoRoot, ".subtask", "internal"))
	require.NoFileExists(t, filepath.Join(repoRoot, ".subtask", "index.db"))

	// Does not promote config when legacy config is absent.
	require.NoFileExists(t, task.ConfigPath())
}

func TestEnsureLayout_DestUnwritable_ReturnsErrorAndLeavesLegacyUntouched(t *testing.T) {
	t.Setenv("SUBTASK_DIR", t.TempDir())

	projectsDir := filepath.Join(task.GlobalDir(), "projects")
	require.NoError(t, os.MkdirAll(projectsDir, 0o755))
	require.NoError(t, os.Chmod(projectsDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(projectsDir, 0o755) })

	repoRoot := t.TempDir()
	legacyInternal := filepath.Join(repoRoot, ".subtask", "internal", "some-task")
	require.NoError(t, os.MkdirAll(legacyInternal, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(legacyInternal, "state.json"), []byte("state"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, ".subtask", "index.db"), []byte("db"), 0o644))

	err := EnsureLayout(repoRoot)
	require.Error(t, err)

	// Legacy files remain on error.
	require.DirExists(t, filepath.Join(repoRoot, ".subtask", "internal"))
	require.FileExists(t, filepath.Join(repoRoot, ".subtask", "index.db"))
	require.Equal(t, "state", strings.TrimSpace(readFileString(t, filepath.Join(legacyInternal, "state.json"))))
}

func TestEnsureLayout_LargeInternalFolder_CopiesAll(t *testing.T) {
	t.Setenv("SUBTASK_DIR", t.TempDir())

	repoRoot := t.TempDir()
	legacyInternal := filepath.Join(repoRoot, ".subtask", "internal")
	require.NoError(t, os.MkdirAll(legacyInternal, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, ".subtask", "index.db"), []byte("db"), 0o644))

	const taskDirs = 100
	const filesPerTask = 3
	expected := make(map[string]string, taskDirs*filesPerTask)
	for i := 0; i < taskDirs; i++ {
		dir := filepath.Join(legacyInternal, fmt.Sprintf("task-%03d", i))
		require.NoError(t, os.MkdirAll(dir, 0o755))
		for j := 0; j < filesPerTask; j++ {
			rel := filepath.Join(fmt.Sprintf("task-%03d", i), fmt.Sprintf("file-%d.txt", j))
			content := fmt.Sprintf("task=%d file=%d", i, j)
			require.NoError(t, os.WriteFile(filepath.Join(legacyInternal, rel), []byte(content), 0o644))
			expected[rel] = content
		}
	}

	require.NoError(t, EnsureLayout(repoRoot))

	projectDir := filepath.Join(task.ProjectsDir(), task.EscapePath(repoRoot))
	destInternal := filepath.Join(projectDir, "internal")
	for rel, content := range expected {
		dstPath := filepath.Join(destInternal, rel)
		require.FileExists(t, dstPath)
		require.Equal(t, content, strings.TrimSpace(readFileString(t, dstPath)))
	}

	require.NoDirExists(t, legacyInternal)
	require.NoFileExists(t, filepath.Join(repoRoot, ".subtask", "index.db"))
}

func TestHelperProcessEnsureLayout(t *testing.T) {
	if os.Getenv("SUBTASK_HELPER_PROCESS") != "1" {
		return
	}
	repoRoot := strings.TrimSpace(os.Getenv("SUBTASK_HELPER_REPO_ROOT"))
	if repoRoot == "" {
		_, _ = fmt.Fprintln(os.Stderr, "missing SUBTASK_HELPER_REPO_ROOT")
		os.Exit(2)
	}
	if err := EnsureLayout(repoRoot); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}

func runEnsureLayoutSubprocess(t *testing.T, repoRoot string) error {
	t.Helper()
	cmd, buf := startEnsureLayoutSubprocess(t, repoRoot)
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("EnsureLayout subprocess failed: %w\n%s", err, buf.String())
	}
	return nil
}

func startEnsureLayoutSubprocess(t *testing.T, repoRoot string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcessEnsureLayout$", "-test.v")
	cmd.Env = append(os.Environ(),
		"SUBTASK_HELPER_PROCESS=1",
		"SUBTASK_HELPER_REPO_ROOT="+repoRoot,
	)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	require.NoError(t, cmd.Start())
	return cmd, &buf
}

func waitCmdAsync(cmd *exec.Cmd) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- cmd.Wait() }()
	return ch
}

func waitWithTimeout(ch <-chan error, timeout time.Duration) error {
	select {
	case err := <-ch:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("timeout after %s", timeout)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}
