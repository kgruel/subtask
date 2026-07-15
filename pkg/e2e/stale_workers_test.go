package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// staleWorkersScriptPath returns the path to stale-workers.sh relative to the module root.
func staleWorkersScriptPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	// thisFile is .../pkg/e2e/stale_workers_test.go; go up two dirs to get module root.
	moduleRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(moduleRoot, "plugin", "scripts", "stale-workers.sh")
}

// runStaleWorkers invokes stale-workers.sh with the given payload JSON as stdin.
// Returns stdout+stderr combined. Applies any extra env vars.
func runStaleWorkers(t *testing.T, scriptPath string, extraEnv []string, payloadJSON string) (string, int) {
	t.Helper()
	// Look bash up on PATH rather than assuming /bin/bash, which is not where it
	// lives on every unix (NixOS, for one).
	bash, err := exec.LookPath("bash")
	require.NoError(t, err, "bash not found on PATH")
	cmd := exec.Command(bash, scriptPath)
	cmd.Stdin = strings.NewReader(payloadJSON)
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("unexpected exec error: %v", err)
		}
	}
	return string(out), code
}

// makeTaskHistory creates .subtask/tasks/<name>/history.jsonl in baseDir and sets
// the file's mtime to the given age in the past. Returns the history file path.
func makeTaskHistory(t *testing.T, baseDir, taskName string, content string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(baseDir, ".subtask", "tasks", taskName)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	p := filepath.Join(dir, "history.jsonl")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	mtime := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(p, mtime, mtime))
	return p
}

func historyWithRunningWorker(runID string, startedAt time.Time) string {
	ts := startedAt.UTC().Format("2006-01-02T15:04:05Z")
	return fmt.Sprintf(`{"type":"worker.started","ts":"%s","data":{"run_id":"%s"}}`, ts, runID) + "\n"
}

func historyWithFinishedWorker(runID string, startedAt time.Time) string {
	ts := startedAt.UTC().Format("2006-01-02T15:04:05Z")
	started := fmt.Sprintf(`{"type":"worker.started","ts":"%s","data":{"run_id":"%s"}}`, ts, runID) + "\n"
	finished := fmt.Sprintf(`{"type":"worker.finished","ts":"%s","data":{"run_id":"%s"}}`, ts, runID) + "\n"
	return started + finished
}

func skipIfNoJQ(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available")
	}
}

// skipStaleWorkersOnWindows skips the stale-workers hook tests on Windows.
//
// The hook is a bash script, and Claude Code on Windows runs it through
// git-bash — but this harness does not model that arrangement. It stubs the
// `subtask` command with a .bat (addStubCommandToPATH), which bash will not
// resolve via `command -v` since it does not apply PATHEXT. So the script under
// test would take a different branch than it does in production and the run
// would prove nothing.
//
// The script's own logic is OS-independent and fully covered on unix; what is
// genuinely Windows-specific — whether Claude Code invokes the hook correctly
// under git-bash — is outside what this harness exercises on any platform.
func skipStaleWorkersOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hook scripts are bash; Claude Code on Windows runs them via git-bash, which this harness's .bat command stubs do not model")
	}
}

func TestStaleWorkers_NoSubtaskDir_SilentExit(t *testing.T) {
	skipStaleWorkersOnWindows(t)
	skipIfNoJQ(t)
	script := staleWorkersScriptPath(t)
	addStubCommandToPATH(t, "subtask")

	cwd := t.TempDir() // no .subtask/ here
	out, code := runStaleWorkers(t, script, nil, fmt.Sprintf(`{"cwd":%q}`, cwd))
	assert.Equal(t, 0, code)
	assert.Empty(t, strings.TrimSpace(out))
}

func TestStaleWorkers_FreshWorker_SilentExit(t *testing.T) {
	skipStaleWorkersOnWindows(t)
	skipIfNoJQ(t)
	script := staleWorkersScriptPath(t)
	addStubCommandToPATH(t, "subtask")

	base := t.TempDir()
	// history.jsonl modified 1 minute ago; threshold 30 minutes → not stale.
	makeTaskHistory(t, base, "fix--fresh", historyWithRunningWorker("run1", time.Now().Add(-2*time.Hour)), 1*time.Minute)

	out, code := runStaleWorkers(t, script,
		[]string{"SUBTASK_STALE_THRESHOLD_MIN=30"},
		fmt.Sprintf(`{"cwd":%q}`, base))
	assert.Equal(t, 0, code)
	assert.Empty(t, strings.TrimSpace(out), "fresh worker should produce no output")
}

func TestStaleWorkers_StaleWorker_EmitsContext(t *testing.T) {
	skipStaleWorkersOnWindows(t)
	skipIfNoJQ(t)
	script := staleWorkersScriptPath(t)
	addStubCommandToPATH(t, "subtask")

	base := t.TempDir()
	// history.jsonl modified 60 minutes ago; worker started 2h ago; threshold 30m.
	makeTaskHistory(t, base, "fix--stale", historyWithRunningWorker("run1", time.Now().Add(-2*time.Hour)), 60*time.Minute)

	out, code := runStaleWorkers(t, script,
		[]string{"SUBTASK_STALE_THRESHOLD_MIN=30"},
		fmt.Sprintf(`{"cwd":%q}`, base))
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "hookSpecificOutput", "should emit hook JSON")
	assert.Contains(t, out, "fix/stale", "task name should be unescaped")
	assert.Contains(t, out, "subtask trace fix/stale")
	assert.Contains(t, out, "subtask interrupt fix/stale")
}

func TestStaleWorkers_FinishedWorker_SilentExit(t *testing.T) {
	skipStaleWorkersOnWindows(t)
	skipIfNoJQ(t)
	script := staleWorkersScriptPath(t)
	addStubCommandToPATH(t, "subtask")

	base := t.TempDir()
	// Worker started AND finished — should not appear as stale even if file is old.
	makeTaskHistory(t, base, "fix--done", historyWithFinishedWorker("run1", time.Now().Add(-2*time.Hour)), 60*time.Minute)

	out, code := runStaleWorkers(t, script,
		[]string{"SUBTASK_STALE_THRESHOLD_MIN=30"},
		fmt.Sprintf(`{"cwd":%q}`, base))
	assert.Equal(t, 0, code)
	assert.Empty(t, strings.TrimSpace(out), "finished worker should not appear stale")
}

func TestStaleWorkers_MultipleStale_EmitsAll(t *testing.T) {
	skipStaleWorkersOnWindows(t)
	skipIfNoJQ(t)
	script := staleWorkersScriptPath(t)
	addStubCommandToPATH(t, "subtask")

	base := t.TempDir()
	makeTaskHistory(t, base, "feat--alpha", historyWithRunningWorker("r1", time.Now().Add(-2*time.Hour)), 60*time.Minute)
	makeTaskHistory(t, base, "feat--beta", historyWithRunningWorker("r2", time.Now().Add(-3*time.Hour)), 90*time.Minute)

	out, code := runStaleWorkers(t, script,
		[]string{"SUBTASK_STALE_THRESHOLD_MIN=30"},
		fmt.Sprintf(`{"cwd":%q}`, base))
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "feat/alpha")
	assert.Contains(t, out, "feat/beta")
	assert.Contains(t, out, "Stale workers")
}

func TestStaleWorkers_MalformedHistoryJSONL_DoesNotCrash(t *testing.T) {
	skipStaleWorkersOnWindows(t)
	skipIfNoJQ(t)
	script := staleWorkersScriptPath(t)
	addStubCommandToPATH(t, "subtask")

	base := t.TempDir()
	makeTaskHistory(t, base, "broken--task", "not json at all\n{invalid}\n", 60*time.Minute)

	_, code := runStaleWorkers(t, script,
		[]string{"SUBTASK_STALE_THRESHOLD_MIN=30"},
		fmt.Sprintf(`{"cwd":%q}`, base))
	// Must not crash (exit non-zero with a bash error).
	assert.Equal(t, 0, code, "malformed history.jsonl should not crash the script")
}

func TestStaleWorkers_CwdFromPayload_TakesPrecedence(t *testing.T) {
	skipStaleWorkersOnWindows(t)
	skipIfNoJQ(t)
	script := staleWorkersScriptPath(t)
	addStubCommandToPATH(t, "subtask")

	// Payload cwd points to a stale task; shell cwd is irrelevant.
	base := t.TempDir()
	makeTaskHistory(t, base, "feat--explicit", historyWithRunningWorker("r1", time.Now().Add(-2*time.Hour)), 60*time.Minute)

	// Pass cwd explicitly — even if shell cwd has no .subtask/, script should find it.
	out, code := runStaleWorkers(t, script,
		[]string{"SUBTASK_STALE_THRESHOLD_MIN=30"},
		fmt.Sprintf(`{"cwd":%q}`, base))
	assert.Equal(t, 0, code)
	assert.Contains(t, out, "feat/explicit", "cwd from payload should be used")
}
