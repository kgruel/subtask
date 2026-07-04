package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
)

// taskInternalDir returns the runtime internal dir for a task (where
// state.json, supervisor.log, and detach prompt files live).
func taskInternalDir(root, taskName string) string {
	return filepath.Join(task.ProjectsDir(), task.EscapePath(root), "internal", task.EscapeName(taskName))
}

func draftTask(t *testing.T, binPath, root, name string) {
	t.Helper()
	cmd := exec.Command(binPath, "draft", name, "Detach test task",
		"--base-branch", "main", "--title", "Detach test")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "draft %s failed: %s", name, out)
}

var dispatchedPIDRe = regexp.MustCompile(`detached supervisor, pid (\d+)`)

func parseDispatchedPID(t *testing.T, out string) int {
	t.Helper()
	m := dispatchedPIDRe.FindStringSubmatch(out)
	require.Len(t, m, 2, "dispatch line must name the child pid; got: %s", out)
	pid, err := strconv.Atoi(m[1])
	require.NoError(t, err)
	return pid
}

// TestDetachCLI_FullRoundTrip proves the end-to-end contract: send --detach
// returns 0 after the detached child claims, the child (a distinct process)
// runs to completion writing the same history/reply surfaces as a foreground
// run, and worker.started carries the detached:true provenance.
func TestDetachCLI_FullRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping detach CLI test in short mode")
	}
	t.Setenv("SUBTASK_DIR", t.TempDir())

	binPath := buildSubtask(t)
	mockWorkerPath := mockWorkerPathForSubtask(binPath)
	root := setupParallelTestRepo(t, 2, mockWorkerPath)

	taskName := "detach/roundtrip"
	draftTask(t, binPath, root, taskName)

	cmd := exec.Command(binPath, "send", "--detach", taskName, mockPrompt("Do the detached work"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SUBTASK_TEST_DETACH_POLL_MS=20", "SUBTASK_TEST_DETACH_TIMEOUT_MS=30000")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "send --detach should exit 0: %s", out)

	got := string(out)
	assert.Contains(t, got, "Dispatched "+taskName)
	childPID := parseDispatchedPID(t, got)
	assert.NotEqual(t, cmd.Process.Pid, childPID, "the detached child is a distinct process from the launching CLI")

	// supervisor.log lands under the internal dir, never the repo.
	logPath := filepath.Join(taskInternalDir(root, taskName), "supervisor.log")
	require.Eventually(t, func() bool {
		_, statErr := os.Stat(logPath)
		return statErr == nil
	}, 5*time.Second, 20*time.Millisecond, "supervisor.log should be created")

	// wait blocks until the detached run settles, then reply reads it back.
	waitCmd := exec.Command(binPath, "wait", taskName, "--timeout", "60s")
	waitCmd.Dir = root
	waitCmd.Env = append(os.Environ(), "SUBTASK_TEST_WAIT_INTERVAL_MS=50")
	wout, werr := waitCmd.CombinedOutput()
	require.NoError(t, werr, "wait should exit 0 for a replied task: %s", wout)
	assert.Contains(t, string(wout), taskName+"\treplied")

	replyCmd := exec.Command(binPath, "reply", taskName)
	replyCmd.Dir = root
	rout, rerr := replyCmd.CombinedOutput()
	require.NoError(t, rerr, "reply failed: %s", rout)
	assert.Contains(t, string(rout), "Mock completed")

	// worker.started carries detached:true (additive provenance).
	histPath := filepath.Join(root, ".subtask", "tasks", task.EscapeName(taskName), "history.jsonl")
	histBytes, err := os.ReadFile(histPath)
	require.NoError(t, err)
	assert.Contains(t, string(histBytes), "worker.started")
	assert.Contains(t, string(histBytes), `"detached":true`)

	// The prompt temp file was consumed by the child (read once, then gone).
	prompts, _ := filepath.Glob(filepath.Join(taskInternalDir(root, taskName), task.DetachPromptPattern))
	assert.Empty(t, prompts, "the child unlinks its prompt file after reading it")
}

// TestDetachCLI_HandshakeBarrier_DispatchMeansClaimed proves deterministically
// (no sleeps) that send --detach does not return until the child has claimed:
// the child is held at the pre-claim send barrier, the parent's handshake is
// still blocked, and only after release does the parent return 0.
func TestDetachCLI_HandshakeBarrier_DispatchMeansClaimed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping detach CLI test in short mode")
	}
	t.Setenv("SUBTASK_DIR", t.TempDir())

	binPath := buildSubtask(t)
	mockWorkerPath := mockWorkerPathForSubtask(binPath)
	root := setupParallelTestRepo(t, 2, mockWorkerPath)

	taskName := "detach/barrier"
	draftTask(t, binPath, root, taskName)

	barrierDir := filepath.Join(t.TempDir(), "send-barrier")
	cmd := exec.Command(binPath, "send", "--detach", taskName, mockPrompt("Barrier work"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"SUBTASK_TEST_SEND_BARRIER_DIR="+barrierDir,
		"SUBTASK_TEST_SEND_BARRIER_N=2",
		"SUBTASK_TEST_SEND_BARRIER_TIMEOUT_MS=30000",
		"SUBTASK_TEST_DETACH_POLL_MS=20",
		"SUBTASK_TEST_DETACH_TIMEOUT_MS=30000",
	)
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf
	require.NoError(t, cmd.Start())

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// The child reaches the pre-claim barrier and writes its own participant file.
	require.Eventually(t, func() bool {
		ents, _ := os.ReadDir(barrierDir)
		return len(ents) >= 1
	}, 15*time.Second, 10*time.Millisecond, "child should reach the send barrier")

	// The child is blocked pre-claim, so the parent cannot have confirmed a
	// claim yet: send --detach must still be running.
	select {
	case werr := <-done:
		t.Fatalf("send --detach returned before the child claimed (barrier still held): err=%v out=%s", werr, outBuf.String())
	default:
	}

	// Release the barrier (2nd participant) so the child proceeds to claim+run.
	require.NoError(t, os.WriteFile(filepath.Join(barrierDir, "release"), []byte("go"), 0o644))

	select {
	case werr := <-done:
		require.NoError(t, werr, "send --detach should exit 0 once the child claims: %s", outBuf.String())
	case <-time.After(30 * time.Second):
		// Do not read outBuf here: the process is still running and its output
		// copier may be writing concurrently.
		t.Fatal("send --detach did not return after barrier release")
	}
	assert.Contains(t, outBuf.String(), "Dispatched "+taskName)

	// Drain the still-running detached child before the temp dirs are cleaned
	// up, so cleanup does not race the child's workspace/state writes.
	waitCmd := exec.Command(binPath, "wait", taskName, "--timeout", "60s")
	waitCmd.Dir = root
	waitCmd.Env = append(os.Environ(), "SUBTASK_TEST_WAIT_INTERVAL_MS=50")
	_, _ = waitCmd.CombinedOutput()
}

// TestDetachCLI_HandshakeTimeout_NoKill proves the T1 policy: when the child
// cannot claim within the handshake budget, the parent returns a nonzero
// "not confirmed" advisory WITHOUT killing the child — the barrier-held child
// is still alive and, once released, claims and finishes normally.
func TestDetachCLI_HandshakeTimeout_NoKill(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping detach CLI test in short mode")
	}
	t.Setenv("SUBTASK_DIR", t.TempDir())

	binPath := buildSubtask(t)
	mockWorkerPath := mockWorkerPathForSubtask(binPath)
	root := setupParallelTestRepo(t, 2, mockWorkerPath)

	taskName := "detach/timeout"
	draftTask(t, binPath, root, taskName)

	barrierDir := filepath.Join(t.TempDir(), "send-barrier")
	cmd := exec.Command(binPath, "send", "--detach", taskName, mockPrompt("Slow to claim"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"SUBTASK_TEST_SEND_BARRIER_DIR="+barrierDir,
		"SUBTASK_TEST_SEND_BARRIER_N=2",
		"SUBTASK_TEST_SEND_BARRIER_TIMEOUT_MS=30000",
		"SUBTASK_TEST_DETACH_POLL_MS=20",
		"SUBTASK_TEST_DETACH_TIMEOUT_MS=600", // parent gives up while child is barrier-held
	)
	out, err := cmd.CombinedOutput()
	require.Error(t, err, "handshake timeout must be a nonzero exit; out=%s", out)
	assert.Contains(t, string(out), "has not claimed the task within")

	// The child is still alive, blocked at the barrier (proof it was not killed).
	require.Eventually(t, func() bool {
		ents, _ := os.ReadDir(barrierDir)
		return len(ents) >= 1
	}, 15*time.Second, 10*time.Millisecond, "barrier-held child should still be alive after the parent gave up")

	// Release: the surviving child claims and finishes normally.
	require.NoError(t, os.WriteFile(filepath.Join(barrierDir, "release"), []byte("go"), 0o644))

	waitCmd := exec.Command(binPath, "wait", taskName, "--timeout", "60s")
	waitCmd.Dir = root
	waitCmd.Env = append(os.Environ(), "SUBTASK_TEST_WAIT_INTERVAL_MS=50")
	wout, werr := waitCmd.CombinedOutput()
	require.NoError(t, werr, "the un-killed child should complete: %s", wout)
	assert.Contains(t, string(wout), taskName+"\treplied")
}

// TestDetachCLI_DoubleDispatch_FlockRace proves the concurrency guard at the
// level the advisory pre-check cannot cover: two `send --detach` parents whose
// children are released together at the pre-claim barrier race the per-task
// flock. Exactly one child wins the claim (its parent exits 0); the loser hits
// errTaskWorking under the lock, exits nonzero, and its parent relays the
// already-working error. Only one worker.started is ever recorded.
func TestDetachCLI_DoubleDispatch_FlockRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping detach CLI test in short mode")
	}
	t.Setenv("SUBTASK_DIR", t.TempDir())

	binPath := buildSubtask(t)
	mockWorkerPath := mockWorkerPathForSubtask(binPath)
	root := setupParallelTestRepo(t, 2, mockWorkerPath)

	taskName := "detach/double"
	draftTask(t, binPath, root, taskName)

	barrierDir := filepath.Join(t.TempDir(), "send-barrier")
	// Both children sleep once they claim, so whichever wins holds a live claim
	// while the loser exits — making the loser's rejection observable.
	prompt := mockPrompt("Racer") + "\n/MockRunCommand " + sleepCommandForPlatform(3)
	env := append(os.Environ(),
		"SUBTASK_TEST_SEND_BARRIER_DIR="+barrierDir,
		"SUBTASK_TEST_SEND_BARRIER_N=2", // both children gate here, then release together
		"SUBTASK_TEST_SEND_BARRIER_TIMEOUT_MS=30000",
		"SUBTASK_TEST_DETACH_POLL_MS=20",
		"SUBTASK_TEST_DETACH_TIMEOUT_MS=30000",
	)

	type res struct {
		out string
		err error
	}
	results := make(chan res, 2)
	for range 2 {
		go func() {
			cmd := exec.Command(binPath, "send", "--detach", taskName, prompt)
			cmd.Dir = root
			cmd.Env = env
			out, err := cmd.CombinedOutput()
			results <- res{out: string(out), err: err}
		}()
	}

	winners, losers := 0, 0
	var loserOut string
	for range 2 {
		r := <-results
		if r.err == nil {
			winners++
			assert.Contains(t, r.out, "Dispatched "+taskName, "the winner confirms dispatch")
		} else {
			losers++
			loserOut = r.out
		}
	}
	assert.Equal(t, 1, winners, "exactly one parent wins the claim race")
	assert.Equal(t, 1, losers, "exactly one parent loses the claim race")
	assert.Contains(t, loserOut, "still working", "the loser relays the already-working error")

	// Drain the surviving detached winner before the temp dirs are cleaned up.
	waitCmd := exec.Command(binPath, "wait", taskName, "--timeout", "60s")
	waitCmd.Dir = root
	waitCmd.Env = append(os.Environ(), "SUBTASK_TEST_WAIT_INTERVAL_MS=50")
	_, _ = waitCmd.CombinedOutput()

	// Exactly one worker.started was recorded — the loser never began a run.
	histPath := filepath.Join(root, ".subtask", "tasks", task.EscapeName(taskName), "history.jsonl")
	histBytes, err := os.ReadFile(histPath)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(histBytes), `"type":"worker.started"`), "loser must not have started a second run")
}

// TestDetachCLI_InterruptDuringRun covers spec §12 I2: a detached child gated at
// the pre-claim barrier is released, claims, and starts a long run; `subtask
// interrupt` then group-signals it. The child's in-process handler records the
// interrupt handshake and an error finish, clears its claim, and exits.
func TestDetachCLI_InterruptDuringRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping detach CLI test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("skipping interrupt CLI test on Windows")
	}
	t.Setenv("SUBTASK_DIR", t.TempDir())

	binPath := buildSubtask(t)
	mockWorkerPath := mockWorkerPathForSubtask(binPath)
	root := setupParallelTestRepo(t, 2, mockWorkerPath)

	taskName := "detach/interrupt"
	draftTask(t, binPath, root, taskName)

	barrierDir := filepath.Join(t.TempDir(), "send-barrier")
	prompt := mockPrompt("Long detached run") + "\n/MockRunCommand " + sleepCommandForPlatform(30)

	cmd := exec.Command(binPath, "send", "--detach", taskName, prompt)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"SUBTASK_TEST_SEND_BARRIER_DIR="+barrierDir,
		"SUBTASK_TEST_SEND_BARRIER_N=2",
		"SUBTASK_TEST_SEND_BARRIER_TIMEOUT_MS=30000",
		"SUBTASK_TEST_DETACH_POLL_MS=20",
		"SUBTASK_TEST_DETACH_TIMEOUT_MS=30000",
	)
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf
	require.NoError(t, cmd.Start())

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// Child reaches the pre-claim barrier.
	require.Eventually(t, func() bool {
		ents, _ := os.ReadDir(barrierDir)
		return len(ents) >= 1
	}, 15*time.Second, 10*time.Millisecond, "child should reach the send barrier")

	// Release (2nd participant) so the child claims and starts the sleep run.
	require.NoError(t, os.WriteFile(filepath.Join(barrierDir, "release"), []byte("go"), 0o644))

	select {
	case werr := <-done:
		require.NoError(t, werr, "send --detach should exit 0 once the child claims: %s", outBuf.String())
	case <-time.After(30 * time.Second):
		t.Fatal("send --detach did not return after barrier release")
	}

	escaped := task.EscapeName(taskName)
	statePathCandidates := []string{
		filepath.Join(task.ProjectsDir(), task.EscapePath(root), "internal", escaped, "state.json"),
	}

	// Interrupt the running detached child (retry through the brief pre-run window).
	deadline := time.Now().Add(15 * time.Second)
	var interrupted bool
	for time.Now().Before(deadline) {
		ic := exec.Command(binPath, "interrupt", taskName)
		ic.Dir = root
		out, err := ic.CombinedOutput()
		if err == nil && strings.Contains(string(out), "Sent SIGINT") {
			interrupted = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.True(t, interrupted, "interrupt should signal the detached child")

	// The child clears its claim and records the interruption.
	var cleared task.State
	require.NoError(t, waitForAnyState(t, taskName, statePathCandidates, func(s task.State) bool {
		cleared = s
		return s.SupervisorPID == 0
	}))
	require.Contains(t, strings.ToLower(cleared.LastError), "interrupted")

	// History records the interrupt handshake and an error finish.
	historyPath := filepath.Join(root, ".subtask", "tasks", escaped, "history.jsonl")
	events := readHistoryEvents(t, historyPath)
	require.True(t, hasHistoryEvent(events, "worker.interrupt", func(data map[string]any) bool {
		return data["action"] == "received"
	}), "expected worker.interrupt received")
	require.True(t, hasHistoryEvent(events, "worker.finished", func(data map[string]any) bool {
		return data["outcome"] == "error" && strings.Contains(strings.ToLower(toString(data["error"])), "interrupted")
	}), "expected worker.finished error=interrupted")
}

// TestDetachCLI_Composition_AutoAdvanceChainOneChild covers the spec §12
// composition case: a routine with an advance:auto chain dispatched via
// `send --detach` runs the entire chain inside ONE detached child (the
// in-process auto-advance recursion never re-detaches), and every round's
// worker.started carries detached:true.
func TestDetachCLI_Composition_AutoAdvanceChainOneChild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping detach CLI test in short mode")
	}
	t.Setenv("SUBTASK_DIR", t.TempDir())

	binPath := buildSubtask(t)
	mockWorkerPath := mockWorkerPathForSubtask(binPath)
	root := setupParallelTestRepo(t, 2, mockWorkerPath)

	// Project routine fixture: first→second auto-advance, then a terminal.
	routineDir := filepath.Join(root, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routineDir, 0o755))
	routineYAML := "name: chain\n" +
		"description: Auto-advance chain for the detach composition test.\n" +
		"steps:\n" +
		"  - id: first\n" +
		"    advance: auto\n" +
		"    instructions: |\n" +
		"      First step.\n" +
		"  - id: second\n" +
		"    advance: auto\n" +
		"    worker_instructions: Do the second step.\n" +
		"    instructions: |\n" +
		"      Second step.\n" +
		"  - id: done\n" +
		"    kind: terminal\n" +
		"    instructions: |\n" +
		"      Done.\n"
	require.NoError(t, os.WriteFile(filepath.Join(routineDir, "chain.yaml"), []byte(routineYAML), 0o644))

	taskName := "detach/chain"
	draftCmd := exec.Command(binPath, "draft", taskName, "Chain task",
		"--base-branch", "main", "--title", "Chain test", "--routine", "chain")
	draftCmd.Dir = root
	dout, derr := draftCmd.CombinedOutput()
	require.NoError(t, derr, "draft --routine chain failed: %s", dout)

	cmd := exec.Command(binPath, "send", "--detach", taskName, mockPrompt("Kick off the chain"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SUBTASK_TEST_DETACH_POLL_MS=20", "SUBTASK_TEST_DETACH_TIMEOUT_MS=30000")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "send --detach should exit 0: %s", out)
	assert.Equal(t, 1, strings.Count(string(out), "Dispatched "+taskName), "the parent detaches exactly once")

	// Wait for the full chain to settle (wait blocks through auto-advance rounds).
	waitCmd := exec.Command(binPath, "wait", taskName, "--timeout", "60s")
	waitCmd.Dir = root
	waitCmd.Env = append(os.Environ(), "SUBTASK_TEST_WAIT_INTERVAL_MS=50")
	wout, werr := waitCmd.CombinedOutput()
	require.NoError(t, werr, "wait should settle the chain: %s", wout)

	histPath := filepath.Join(root, ".subtask", "tasks", task.EscapeName(taskName), "history.jsonl")
	histBytes, err := os.ReadFile(histPath)
	require.NoError(t, err)
	hist := string(histBytes)

	// Both rounds ran, and both stamped detached:true provenance.
	assert.Equal(t, 2, strings.Count(hist, `"type":"worker.started"`), "the chain runs two worker rounds")
	assert.Equal(t, 2, strings.Count(hist, `"detached":true`), "every round carries detached provenance")

	// No re-detach: the child never acts as a detach parent, so its log (child
	// stdout/stderr) never contains a dispatch-confirmation line.
	logPath := filepath.Join(taskInternalDir(root, taskName), "supervisor.log")
	if logBytes, statErr := os.ReadFile(logPath); statErr == nil {
		assert.NotContains(t, string(logBytes), "detached supervisor", "the auto-advance chain must not re-detach")
	}
}
