package e2e

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWaitCLI_BlocksThenExitsWithErrorCode drives the real `subtask wait`
// binary against two tasks: one dispatched in the background (in-flight) and
// one that errors. wait must block until both settle, print each task's
// terminal line, and exit 2 because a completing task errored.
func TestWaitCLI_BlocksThenExitsWithErrorCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping wait CLI test in short mode")
	}
	t.Setenv("SUBTASK_DIR", t.TempDir())

	binPath := buildSubtask(t)
	mockWorkerPath := mockWorkerPathForSubtask(binPath)
	root := setupParallelTestRepo(t, 2, mockWorkerPath)

	okTask, errTask := "wait/ok", "wait/err"
	for _, name := range []string{okTask, errTask} {
		draft := exec.Command(binPath, "draft", name, "Wait barrier task",
			"--base-branch", "main", "--title", "Wait test")
		draft.Dir = root
		out, err := draft.CombinedOutput()
		require.NoError(t, err, "draft %s failed: %s", name, out)
	}

	// Dispatch the ok task in the background so it is genuinely in-flight when
	// wait starts polling (Start, not Wait).
	okCmd := exec.Command(binPath, "send", okTask, mockPrompt("Do the work"))
	okCmd.Dir = root
	require.NoError(t, okCmd.Start())
	defer func() { _ = okCmd.Wait() }()

	// The err task fails a command; the mock worker exits non-zero and send
	// records a worker error. Its own exit is non-zero — expected.
	errCmd := exec.Command(binPath, "send", errTask, "Please fail\n/MockRunCommand exit 1")
	errCmd.Dir = root
	_, _ = errCmd.CombinedOutput()

	waitCmd := exec.Command(binPath, "wait", okTask, errTask, "--timeout", "60s")
	waitCmd.Dir = root
	waitCmd.Env = append(os.Environ(), "SUBTASK_TEST_WAIT_INTERVAL_MS=50")
	out, err := waitCmd.CombinedOutput()

	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee, "wait should exit non-zero; output: %s", out)
	assert.Equal(t, 2, ee.ExitCode(), "a completing task errored → exit 2; output: %s", out)

	got := string(out)
	assert.True(t, strings.Contains(got, okTask+"\treplied"), "expected ok task replied line; got: %s", got)
	assert.True(t, strings.Contains(got, errTask+"\terror"), "expected err task error line; got: %s", got)
}
