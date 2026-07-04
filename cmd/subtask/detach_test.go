package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

func TestBuildChildArgs(t *testing.T) {
	const promptFile = "/tmp/detach-prompt-abc.txt"

	cases := []struct {
		name     string
		cmd      SendCmd
		wantHas  []string
		wantMiss []string
	}{
		{
			name:     "minimal forwards nothing extra",
			cmd:      SendCmd{Task: "fix/x"},
			wantHas:  []string{"send", "fix/x", "--detach-child", promptFile},
			wantMiss: []string{"--detach", "--adapter", "--provider", "--model", "--reasoning", "--agent", "--quiet", "--pinned-base"},
		},
		{
			name: "all overrides forwarded",
			cmd: SendCmd{
				Task: "fix/y", Adapter: "codex", Provider: "openai", Model: "gpt",
				Reasoning: "high", Agent: "reviewer", Quiet: true, PinnedBase: true,
			},
			wantHas: []string{
				"--detach-child", promptFile,
				"--adapter", "codex", "--provider", "openai", "--model", "gpt",
				"--reasoning", "high", "--agent", "reviewer", "--quiet", "--pinned-base",
			},
			wantMiss: []string{"--detach"},
		},
		{
			// The prompt crosses the fork boundary only via the temp file, never
			// argv (avoids ARG_MAX/quoting hazards and keeps secrets out of `ps`).
			name:     "positional prompt is never forwarded in argv",
			cmd:      SendCmd{Task: "fix/z", Prompt: "super secret prompt body"},
			wantHas:  []string{"--detach-child", promptFile},
			wantMiss: []string{"--detach", "super secret prompt body"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := tc.cmd.buildChildArgs(promptFile)
			require.GreaterOrEqual(t, len(args), 4)
			assert.Equal(t, "send", args[0], "argv[0] is the send subcommand")
			assert.Equal(t, tc.cmd.Task, args[1], "argv[1] is the task name")
			for _, w := range tc.wantHas {
				assert.Contains(t, args, w)
			}
			for _, w := range tc.wantMiss {
				assert.NotContains(t, args, w, "must never re-detach or carry a positional prompt")
			}
			if tc.cmd.Prompt != "" {
				// Belt-and-suspenders: the prompt must not appear even as a
				// substring of any single arg (e.g. concatenated into a flag).
				assert.NotContains(t, strings.Join(args, "\x00"), tc.cmd.Prompt,
					"the prompt body must never leak into argv")
			}
		})
	}
}

func TestResolvePrompt_DetachChild_RoundTripAndConsume(t *testing.T) {
	dir := t.TempDir()
	pf := filepath.Join(dir, "detach-prompt.txt")
	require.NoError(t, os.WriteFile(pf, []byte("  do the thing  "), 0o644))

	c := &SendCmd{Task: "fix/x", DetachChild: pf}
	got, err := c.resolvePrompt()
	require.NoError(t, err)
	assert.Equal(t, "do the thing", got, "content is trimmed")
	assert.True(t, c.detached, "reading a detach-child prompt marks the run detached")

	_, statErr := os.Stat(pf)
	assert.True(t, os.IsNotExist(statErr), "the prompt file is one-shot: removed after read")
}

func TestResolvePrompt_DetachChild_EmptyIsError(t *testing.T) {
	dir := t.TempDir()
	pf := filepath.Join(dir, "detach-prompt.txt")
	require.NoError(t, os.WriteFile(pf, []byte("   \n  "), 0o644))

	_, err := (&SendCmd{Task: "fix/x", DetachChild: pf}).resolvePrompt()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt is required")
}

func TestResolvePrompt_DetachChild_MissingFile(t *testing.T) {
	_, err := (&SendCmd{Task: "fix/x", DetachChild: "/no/such/detach-prompt.txt"}).resolvePrompt()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not read its prompt file")
}

func TestChildEnv_ForcesPlainPreservesRest(t *testing.T) {
	t.Setenv("SUBTASK_OUTPUT", "pretty")
	t.Setenv("SUBTASK_TEST_DETACH_POLL_MS", "5")

	env := childEnv()

	outputCount := 0
	var sawPlain, sawTestVar, sawPath bool
	for _, kv := range env {
		switch {
		case strings.HasPrefix(kv, "SUBTASK_OUTPUT="):
			outputCount++
			if kv == "SUBTASK_OUTPUT=plain" {
				sawPlain = true
			}
		case kv == "SUBTASK_TEST_DETACH_POLL_MS=5":
			sawTestVar = true
		case strings.HasPrefix(kv, "PATH="):
			sawPath = true
		}
	}
	assert.Equal(t, 1, outputCount, "the inherited SUBTASK_OUTPUT=pretty is dropped; only plain remains")
	assert.True(t, sawPlain, "child log output is forced plain")
	assert.True(t, sawTestVar, "SUBTASK_TEST_* is preserved for test/barrier determinism")
	assert.True(t, sawPath, "PATH is preserved so the child can resolve adapter CLIs")
}

func TestRun_DetachAndDetachChild_MutuallyExclusive(t *testing.T) {
	// The mutual-exclusion guard is the first line of Run(), so no project
	// environment is needed to reach it.
	err := (&SendCmd{Task: "fix/x", Detach: true, DetachChild: "/tmp/p.txt"}).Run()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestRunDetachParent_AdvisoryPreCheck_NoSpawn(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	name := "fix/busy"
	env.CreateTask(name, "busy", "main", "")
	// A live claim held by this very process: IsStale() is false, so the
	// advisory pre-check must reject without spawning.
	env.CreateTaskState(name, &task.State{SupervisorPID: os.Getpid()})

	called := false
	orig := detachStart
	detachStart = func(cmd *exec.Cmd) error { called = true; return nil }
	defer func() { detachStart = orig }()

	err := (&SendCmd{Task: name, Detach: true}).runDetachParent("hello")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "still working")
	assert.False(t, called, "advisory pre-check must fail before spawning a child")
}

// pollClaim: a claim written by the child (SupervisorPID==childPID) is the
// most direct success signal.
func TestPollClaim_ClaimObserved_Success(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	name := "fix/claim"
	env.CreateTask(name, "claim", "main", "")

	const childPID = 4242421
	env.CreateTaskState(name, &task.State{SupervisorPID: childPID})

	c := &SendCmd{Task: name, Quiet: true}
	exited := make(chan error, 1) // never fires
	require.NoError(t, c.pollClaim(childPID, exited, "", "", time.Now()))
}

// pollClaim: the A-BLOCKER regression. A fast child that claimed→ran→cleared→
// exited 0 inside one poll gap leaves no claim, but the clean exit means the
// dispatch still succeeded.
func TestPollClaim_CleanExitNoClaim_Success(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	name := "fix/fastexit"
	env.CreateTask(name, "fastexit", "main", "")

	c := &SendCmd{Task: name, Quiet: true}
	exited := make(chan error, 1)
	exited <- nil // clean exit, no claim ever observed
	require.NoError(t, c.pollClaim(999999, exited, "", "", time.Now()))
}

// pollClaim: a present claim wins even when the child has also exited nonzero
// (claim-raced-exit); the run succeeded.
func TestPollClaim_ClaimPresentWithDirtyExit_Success(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	name := "fix/raced"
	env.CreateTask(name, "raced", "main", "")

	const childPID = 515151
	env.CreateTaskState(name, &task.State{SupervisorPID: childPID})

	c := &SendCmd{Task: name, Quiet: true}
	exited := make(chan error, 1)
	exited <- fmt.Errorf("exit status 1")
	require.NoError(t, c.pollClaim(childPID, exited, "", "", time.Now()))
}

// pollClaim: a nonzero exit with no matching claim is a dispatch failure that
// relays the supervisor log tail and removes the never-read prompt file.
func TestPollClaim_DirtyExitNoClaim_FailsWithLogTail(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	name := "fix/dirty"
	env.CreateTask(name, "dirty", "main", "")

	logPath := filepath.Join(t.TempDir(), "supervisor.log")
	require.NoError(t, os.WriteFile(logPath, []byte("panic: missing adapter binary\n"), 0o644))
	promptPath := filepath.Join(t.TempDir(), "detach-prompt-x.txt")
	require.NoError(t, os.WriteFile(promptPath, []byte("hi"), 0o644))

	c := &SendCmd{Task: name, Quiet: true}
	exited := make(chan error, 1)
	exited <- fmt.Errorf("exit status 2")

	err := c.pollClaim(31337, exited, promptPath, logPath, time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exited before claiming")
	assert.Contains(t, err.Error(), "missing adapter binary", "relays the log tail")

	_, statErr := os.Stat(promptPath)
	assert.True(t, os.IsNotExist(statErr), "an unclaimed early-exit removes the prompt file")
}

// pollClaim: on the handshake timeout the parent must NOT kill the child and
// must NOT remove the prompt file; it returns a nonzero advisory naming the pid.
func TestPollClaim_Timeout_NonFatalAdvisory_KeepsPrompt(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	name := "fix/timeout"
	env.CreateTask(name, "timeout", "main", "")
	t.Setenv("SUBTASK_TEST_DETACH_TIMEOUT_MS", "40")
	t.Setenv("SUBTASK_TEST_DETACH_POLL_MS", "5")

	promptPath := filepath.Join(t.TempDir(), "detach-prompt-x.txt")
	require.NoError(t, os.WriteFile(promptPath, []byte("still needed"), 0o644))
	logPath := filepath.Join(t.TempDir(), "supervisor.log")

	const childPID = 246810
	c := &SendCmd{Task: name, Quiet: true}
	exited := make(chan error, 1) // child never exits, never claims

	err := c.pollClaim(childPID, exited, promptPath, logPath, time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has not claimed the task within")
	assert.Contains(t, err.Error(), fmt.Sprintf("%d", childPID), "advisory names the child pid")
	assert.Contains(t, err.Error(), "do NOT blindly re-send", "advisory warns against a duplicating retry")

	_, statErr := os.Stat(promptPath)
	assert.NoError(t, statErr, "the timeout path must NOT remove the prompt file (child may still read it)")
}

// pollClaim: the symmetric twin of the clean-exit fix. A fast child that
// claimed→ran→errored→cleared its own SupervisorPID→exited nonzero within one
// poll gap leaves no live claim, but a worker.finished stamped after dispatch
// proves the run happened — so the parent must report a post-claim run failure,
// NOT "exited before claiming" (which would falsely imply nothing ran), and
// must NOT remove the prompt file (the child already consumed it).
func TestPollClaim_DirtyExitPostClaimRun_ReportsRunFailure(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	name := "fix/postclaim"
	env.CreateTask(name, "postclaim", "main", "")

	// A dispatch instant just before the (seeded) run's events.
	dispatchedAt := time.Now().Add(-time.Second)
	ranTS := time.Now()
	env.CreateTaskHistory(name, []history.Event{
		{Type: history.EventTypeWorkerStarted, TS: ranTS, Data: mustJSON(map[string]any{"run_id": "r1", "detached": true})},
		{Type: history.EventTypeWorkerFinished, TS: ranTS, Data: mustJSON(map[string]any{"run_id": "r1", "outcome": "error", "error": "boom"})},
	})
	// The child cleared its own claim on the way out: no live SupervisorPID.
	env.CreateTaskState(name, &task.State{LastError: "boom"})

	logPath := filepath.Join(t.TempDir(), "supervisor.log")
	require.NoError(t, os.WriteFile(logPath, []byte("worker failed: boom\n"), 0o644))
	promptPath := filepath.Join(t.TempDir(), "detach-prompt-x.txt")
	require.NoError(t, os.WriteFile(promptPath, []byte("hi"), 0o644))

	c := &SendCmd{Task: name, Quiet: true}
	exited := make(chan error, 1)
	exited <- fmt.Errorf("exit status 1")

	err := c.pollClaim(4242422, exited, promptPath, logPath, dispatchedAt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claimed the task but its run failed", "must report a post-claim run failure")
	assert.NotContains(t, err.Error(), "exited before claiming", "must not imply nothing ran")

	_, statErr := os.Stat(promptPath)
	assert.NoError(t, statErr, "a post-claim run failure must NOT remove the prompt file (the child owned it)")
}
