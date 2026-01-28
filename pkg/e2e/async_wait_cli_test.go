package e2e

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseAsyncOutputPath(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Output: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Output: "))
		}
	}
	t.Fatalf("missing Output line in: %s", out)
	return ""
}

func mockPromptWithCommands(base string, commands ...string) string {
	base = strings.TrimSpace(base)
	var b strings.Builder
	b.WriteString(base)
	for _, cmd := range commands {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		b.WriteString("\n/MockRunCommand ")
		b.WriteString(cmd)
	}
	return b.String()
}

func sleepCommandForPlatform() string {
	if runtime.GOOS == "windows" {
		return "ping -n 2 127.0.0.1 >NUL"
	}
	return "sleep 1"
}

func TestAsyncSendAndWait_SuccessAndWaitAgain(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping async send/wait CLI test in short mode")
	}
	t.Setenv("SUBTASK_DIR", t.TempDir())

	binPath := buildSubtask(t)
	mockWorkerPath := mockWorkerPathForSubtask(binPath)
	root := setupParallelTestRepo(t, 2, mockWorkerPath)

	taskName := "async/basic"

	draftCmd := exec.Command(binPath, "draft", taskName, "Test task description",
		"--base-branch", "main", "--title", "Async send test")
	draftCmd.Dir = root
	out, err := draftCmd.CombinedOutput()
	require.NoError(t, err, "draft failed: %s", out)

	sendCmd := exec.Command(binPath, "send", taskName, "--async", mockPrompt("Do something async"))
	sendCmd.Dir = root
	out, err = sendCmd.CombinedOutput()
	require.NoError(t, err, "async send failed: %s", out)
	assert.Contains(t, string(out), "Dispatched to task "+taskName)
	outputPath := parseAsyncOutputPath(t, string(out))

	waitCmd := exec.Command(binPath, "wait", taskName)
	waitCmd.Dir = root
	out, err = waitCmd.CombinedOutput()
	require.NoError(t, err, "wait failed: %s", out)
	assert.Equal(t, outputPath, strings.TrimSpace(string(out)))

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "Mock completed")

	// Wait again on an already-finished task.
	waitCmd2 := exec.Command(binPath, "wait", taskName)
	waitCmd2.Dir = root
	out, err = waitCmd2.CombinedOutput()
	require.NoError(t, err, "wait (already finished) failed: %s", out)
	assert.Equal(t, outputPath, strings.TrimSpace(string(out)))
}

func TestAsyncSend_ParallelAttemptsFailCleanly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping async send/wait CLI test in short mode")
	}
	t.Setenv("SUBTASK_DIR", t.TempDir())

	binPath := buildSubtask(t)
	mockWorkerPath := mockWorkerPathForSubtask(binPath)
	root := setupParallelTestRepo(t, 2, mockWorkerPath)

	taskName := "async/parallel"

	draftCmd := exec.Command(binPath, "draft", taskName, "Test task description",
		"--base-branch", "main", "--title", "Async send parallel test")
	draftCmd.Dir = root
	out, err := draftCmd.CombinedOutput()
	require.NoError(t, err, "draft failed: %s", out)

	slowPrompt := mockPromptWithCommands(
		"Do something slowly",
		sleepCommandForPlatform(),
		"echo toolcall-1",
		"echo toolcall-2",
		"echo toolcall-3",
	)

	first := exec.Command(binPath, "send", taskName, "--async", slowPrompt)
	first.Dir = root
	out, err = first.CombinedOutput()
	require.NoError(t, err, "first async send failed: %s", out)
	outputPath := parseAsyncOutputPath(t, string(out))

	second := exec.Command(binPath, "send", taskName, "--async", mockPrompt("This should fail"))
	second.Dir = root
	out2, err := second.CombinedOutput()
	require.Error(t, err, "expected second async send to fail: %s", out2)
	assert.Contains(t, string(out2), "still working")

	waitCmd := exec.Command(binPath, "wait", taskName)
	waitCmd.Dir = root
	out, err = waitCmd.CombinedOutput()
	require.NoError(t, err, "wait failed: %s", out)
	assert.Equal(t, outputPath, strings.TrimSpace(string(out)))
}

func TestAsyncWait_ErrorOutcomeExits1(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping async send/wait CLI test in short mode")
	}
	t.Setenv("SUBTASK_DIR", t.TempDir())

	binPath := buildSubtask(t)
	mockWorkerPath := mockWorkerPathForSubtask(binPath)
	root := setupParallelTestRepo(t, 2, mockWorkerPath)

	taskName := "async/error"

	draftCmd := exec.Command(binPath, "draft", taskName, "Test task description",
		"--base-branch", "main", "--title", "Async wait error test")
	draftCmd.Dir = root
	out, err := draftCmd.CombinedOutput()
	require.NoError(t, err, "draft failed: %s", out)

	sendCmd := exec.Command(binPath, "send", taskName, "--async", mockPromptWithCommands("Make it fail", "exit 1"))
	sendCmd.Dir = root
	out, err = sendCmd.CombinedOutput()
	require.NoError(t, err, "async send failed: %s", out)
	outputPath := parseAsyncOutputPath(t, string(out))

	waitCmd := exec.Command(binPath, "wait", taskName)
	waitCmd.Dir = root
	out, err = waitCmd.CombinedOutput()
	require.Error(t, err, "expected wait to exit non-zero")
	assert.Equal(t, outputPath, strings.TrimSpace(string(out)))

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "command failed")
}
