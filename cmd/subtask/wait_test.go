package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

func intPtr(n int) *int { return &n }

// runWaitCapture runs c.run() with stdout/stderr captured, returning the
// semantic exit code, error, and the two streams.
func runWaitCapture(t *testing.T, c *WaitCmd) (code int, runErr error, stdout, stderr string) {
	t.Helper()
	stdout, stderr, _ = captureStdoutStderr(t, func() error {
		code, runErr = c.run()
		return nil
	})
	return code, runErr, stdout, stderr
}

// sendReplied drives a task to a settled worker reply via the mock harness.
func sendReplied(t *testing.T, name string) {
	t.Helper()
	mock := harness.NewMockHarness().WithResult("reply "+name, "sess-"+name)
	require.NoError(t, (&SendCmd{Task: name, Prompt: "Go"}).WithHarness(mock).Run())
}

// seedMerged / seedClosed stamp a terminal task status into history without a
// real merge/close (deterministic, no git plumbing).
func seedMerged(t *testing.T, env *testutil.TestEnv, name string) {
	t.Helper()
	env.CreateTask(name, "merged task", "main", "desc")
	env.CreateTaskHistory(name, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "first-send", "base_branch": "main"})},
		{Type: "task.merged", Data: mustJSON(map[string]any{"commit": "abc123", "method": "squash"})},
	})
}

func seedClosed(t *testing.T, env *testutil.TestEnv, name string) {
	t.Helper()
	env.CreateTask(name, "closed task", "main", "desc")
	env.CreateTaskHistory(name, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "first-send", "base_branch": "main"})},
		{Type: "task.closed", Data: mustJSON(map[string]any{})},
	})
}

func TestWait_AnyRepliedTask_Exit0(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	name := "fix/reply"
	env.CreateTask(name, "reply", "main", "")
	sendReplied(t, name)

	code, err, stdout, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{name}, Any: true})
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, name+"\treplied")
}

func TestWait_MergedTask_Exit0(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	name := "fix/merged"
	seedMerged(t, env, name)

	code, err, stdout, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{name}})
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, name+"\tmerged")
}

func TestWait_ClosedTask_Exit0(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	name := "fix/closed"
	seedClosed(t, env, name)

	code, err, stdout, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{name}})
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, name+"\tclosed")
}

func TestWait_WorkerError_Exit2(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	name := "fix/err"
	env.CreateTask(name, "err", "main", "")
	mock := harness.NewMockHarness().WithError(errors.New("boom"))
	// Send returns the worker error; that is expected here.
	_ = (&SendCmd{Task: name, Prompt: "Go"}).WithHarness(mock).Run()

	code, err, stdout, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{name}})
	require.NoError(t, err)
	assert.Equal(t, 2, code)
	assert.Contains(t, stdout, name+"\terror")
}

func TestWait_AllTwoReplied_Exit0(t *testing.T) {
	env := testutil.NewTestEnv(t, 2)
	withOutputMode(t, false)

	a, b := "fix/a", "fix/b"
	env.CreateTask(a, "a", "main", "")
	env.CreateTask(b, "b", "main", "")
	sendReplied(t, a)
	sendReplied(t, b)

	code, err, stdout, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{a, b}})
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, a+"\treplied")
	assert.Contains(t, stdout, b+"\treplied")
}

func TestWait_NOne_OverRepliedAndDraft_Exit0(t *testing.T) {
	env := testutil.NewTestEnv(t, 2)
	withOutputMode(t, false)

	replied, draft := "fix/replied", "fix/draft"
	env.CreateTask(replied, "replied", "main", "")
	env.CreateTask(draft, "draft", "main", "")
	sendReplied(t, replied)

	code, err, stdout, stderr := runWaitCapture(t, &WaitCmd{Tasks: []string{replied, draft}, N: intPtr(1)})
	require.NoError(t, err)
	assert.Equal(t, 0, code, "n=1 met by the replied task even though the other is a draft")
	assert.Contains(t, stdout, replied+"\treplied")
	assert.Contains(t, stderr, "has no started run yet")
}

func TestWait_DraftWithTimeout_Exit3_WarnsOnce(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)
	// Small interval so several polls elapse within the timeout; the warning
	// must still be emitted exactly once.
	t.Setenv("SUBTASK_TEST_WAIT_INTERVAL_MS", "2")

	name := "fix/draftonly"
	env.CreateTask(name, "draft", "main", "")

	code, err, stdout, stderr := runWaitCapture(t, &WaitCmd{Tasks: []string{name}, Timeout: 40 * time.Millisecond})
	require.NoError(t, err)
	assert.Equal(t, 3, code)
	assert.Equal(t, 1, strings.Count(stderr, "has no started run yet"), "draft warning must be emitted exactly once")
	assert.Contains(t, stdout, name+"\tdraft", "timeout finalize prints current status")
}

func TestWait_TaskNotFound_ErrorBeforeLoop(t *testing.T) {
	testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	code, err, _, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{"no/such"}})
	require.Error(t, err)
	assert.Equal(t, 0, code, "not-found returns via error (kong exit 1), not a semantic code")
	assert.Contains(t, err.Error(), `task "no/such" not found`)
	assert.Contains(t, err.Error(), "Tip:")
}

func TestWait_NOutOfRange(t *testing.T) {
	env := testutil.NewTestEnv(t, 2)
	withOutputMode(t, false)

	a, b := "fix/a", "fix/b"
	env.CreateTask(a, "a", "main", "")
	env.CreateTask(b, "b", "main", "")

	// -n 0 (provided, value 0) must reach the range check, not alias to --all.
	code, err, _, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{a, b}, N: intPtr(0)})
	require.Error(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, err.Error(), "out of range")

	// -n 5 over 2 tasks is unsatisfiable.
	_, err5 := (&WaitCmd{Tasks: []string{a, b}, N: intPtr(5)}).resolveThreshold(2)
	require.Error(t, err5)
	assert.Contains(t, err5.Error(), "out of range")
}

func TestWait_ResolveThreshold(t *testing.T) {
	cases := []struct {
		name   string
		cmd    WaitCmd
		nTasks int
		want   int
		errStr string
	}{
		{"default-all", WaitCmd{}, 3, 3, ""},
		{"any", WaitCmd{Any: true}, 3, 1, ""},
		{"n-mid", WaitCmd{N: intPtr(2)}, 3, 2, ""},
		{"n-zero", WaitCmd{N: intPtr(0)}, 3, 0, "out of range"},
		{"n-over", WaitCmd{N: intPtr(5)}, 3, 0, "out of range"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.cmd.resolveThreshold(tc.nTasks)
			if tc.errStr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errStr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestWait_FlagParsing verifies kong's xor + *int wiring empirically:
// conflicting selectors are a usage error, -n 0 is "provided", and no flag
// leaves N unset (falls through to --all).
func TestWait_FlagParsing(t *testing.T) {
	parse := func(args []string) (*WaitCmd, error) {
		var cli struct {
			Wait WaitCmd `cmd:""`
		}
		p, err := kong.New(&cli, kong.Name("subtask"), kong.Exit(func(int) {}))
		require.NoError(t, err)
		_, perr := p.Parse(args)
		return &cli.Wait, perr
	}

	_, err := parse([]string{"wait", "a", "b", "-n", "2", "--all"})
	require.Error(t, err, "-n with --all must be a usage error")

	c, err := parse([]string{"wait", "a", "b", "-n", "0"})
	require.NoError(t, err)
	require.NotNil(t, c.N, "-n 0 must be recorded as provided")
	assert.Equal(t, 0, *c.N)

	c, err = parse([]string{"wait", "a", "b"})
	require.NoError(t, err)
	assert.Nil(t, c.N, "no -n flag must leave N unset")
	assert.False(t, c.All)
	assert.False(t, c.Any)
}

func TestWait_DuplicateNames_CollapsedWithNote(t *testing.T) {
	env := testutil.NewTestEnv(t, 2)
	withOutputMode(t, false)

	a, b := "fix/a", "fix/b"
	env.CreateTask(a, "a", "main", "")
	env.CreateTask(b, "b", "main", "")
	sendReplied(t, a)
	sendReplied(t, b)

	// [a, a, b] with --all must collapse to {a, b} (2 members), satisfied when
	// both complete — not spuriously satisfiable by a alone.
	code, err, stdout, stderr := runWaitCapture(t, &WaitCmd{Tasks: []string{a, a, b}})
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, stderr, "ignoring duplicate task name(s): "+a)
	assert.Contains(t, stdout, a+"\treplied")
	assert.Contains(t, stdout, b+"\treplied")
}

// Dedup discrimination (a): [A, A] is one barrier member, so -n 2 is
// unsatisfiable and must be rejected up-front with the out-of-range error
// keyed off the DEDUPED count ("1 task(s) named"). A broken dedup that kept
// both copies would see 2 members and accept -n 2. Existence is checked before
// resolveThreshold, so A must exist to reach the range check.
func TestWait_DedupThenNOutOfRange(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	a := "fix/a"
	env.CreateTask(a, "a", "main", "")

	code, err, _, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{a, a}, N: intPtr(2)})
	require.Error(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, err.Error(), "out of range")
	assert.Contains(t, err.Error(), "1 task(s) named", "range check must count deduped members, not raw args")
}

// Dedup discrimination (b): [a, a, b] with only `a` replied and -n 2 must NOT
// be satisfied — after dedup the members are {a, b} and only a is complete, so
// the barrier of 2 is unmet and wait times out (exit 3). A broken dedup would
// count a twice, reach completeCount==2, and wrongly exit 0.
func TestWait_DedupPreventsDoubleCount_Exit3(t *testing.T) {
	env := testutil.NewTestEnv(t, 2)
	withOutputMode(t, false)
	t.Setenv("SUBTASK_TEST_WAIT_INTERVAL_MS", "2")

	a, b := "fix/a", "fix/b"
	env.CreateTask(a, "a", "main", "")
	env.CreateTask(b, "b", "main", "")
	sendReplied(t, a) // b stays a draft

	code, err, _, stderr := runWaitCapture(t, &WaitCmd{Tasks: []string{a, a, b}, N: intPtr(2), Timeout: 40 * time.Millisecond})
	require.NoError(t, err)
	assert.Equal(t, 3, code, "dedup collapses [a,a,b]→{a,b}; only a is complete so n=2 is unmet")
	assert.Contains(t, stderr, "ignoring duplicate task name(s): "+a)
}

func TestWait_TimeoutWithWorkingTask_Exit3(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	name := "fix/working"
	env.CreateTask(name, "working", "main", "")
	// A live supervisor PID (this test process) with no worker.finished: the
	// task classifies as working and never completes.
	env.CreateTaskHistory(name, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "first-send", "base_branch": "main"})},
	})
	env.CreateTaskState(name, &task.State{SupervisorPID: os.Getpid()})

	code, err, stdout, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{name}, Timeout: 30 * time.Millisecond})
	require.NoError(t, err)
	assert.Equal(t, 3, code)
	assert.Contains(t, stdout, name+"\tworking")
}

// pendingSeed writes a routine whose advance:auto step's declaration-order
// successor is a regular step with worker_instructions, then seeds history so
// the last worker.finished is `replied` stamped at the auto step, with
// SupervisorPID cleared. This reproduces the Trap A auto-advance window.
func pendingSeed(t *testing.T, env *testutil.TestEnv, name string) {
	t.Helper()
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "adv.yaml"), []byte(
		`name: adv
steps:
  - id: work
    advance: auto
    worker_instructions: do the work
  - id: more
    worker_instructions: keep going
  - id: done
    kind: terminal
`), 0o644))

	require.NoError(t, (&DraftCmd{
		Task:        name,
		Title:       "pending",
		Description: "mid auto-advance",
		Base:        "main",
		Routine:     "adv",
	}).Run())

	env.CreateTaskHistory(name, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "first-send", "base_branch": "main"})},
		{Type: "message", Role: "worker", Content: "ok"},
		{Type: "worker.finished", Data: mustJSON(map[string]any{
			"run_id": "r1", "outcome": "replied", "stage": "work",
		})},
	})
	env.CreateTaskState(name, &task.State{SupervisorPID: 0})
}

// pendingBadArtifactSeed drafts a task on a routine whose advance:auto step
// reads a produces artifact to pick its branch, then writes that artifact with
// malformed (unterminated) frontmatter and seeds a replied history stamped at
// the auto step. The auto-advance DECISION errors — the supervisor's own
// advance would have failed the same way — so wait must surface it, not treat
// the reply as clean.
func pendingBadArtifactSeed(t *testing.T, env *testutil.TestEnv, name string) {
	t.Helper()
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "advbad.yaml"), []byte(
		`name: advbad
steps:
  - id: work
    advance: auto
    produces: PLAN.md
    worker_instructions: do the work
    branches:
      - to: work
        when: artifact.field
        field: needs_rework
  - id: done
    kind: terminal
`), 0o644))

	require.NoError(t, (&DraftCmd{
		Task:        name,
		Title:       "bad artifact",
		Description: "auto-advance reads a malformed artifact",
		Base:        "main",
		Routine:     "advbad",
	}).Run())

	// Unterminated frontmatter → readArtifactBool (thus WouldAutoDispatch) errors.
	require.NoError(t, os.WriteFile(
		filepath.Join(task.Dir(name), "PLAN.md"),
		[]byte("---\nneeds_rework: true"), 0o644))

	env.CreateTaskHistory(name, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "first-send", "base_branch": "main"})},
		{Type: "message", Role: "worker", Content: "ok"},
		{Type: "worker.finished", Data: mustJSON(map[string]any{
			"run_id": "r1", "outcome": "replied", "stage": "work",
		})},
	})
	env.CreateTaskState(name, &task.State{SupervisorPID: 0})
}

// Trap A error variant: a replied task whose routine auto-advance decision
// FAILS (malformed produces frontmatter) must report complete-with-error
// (exit 2) with the new label, not a clean replied (exit 0). Swallowing the
// error would let wait report success while the supervisor's advance actually
// broke.
func TestWait_PendingAutoAdvance_DecisionError_Exit2(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	name := "fix/advbad"
	pendingBadArtifactSeed(t, env, name)

	code, err, stdout, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{name}, Any: true})
	require.NoError(t, err)
	assert.Equal(t, 2, code, "a failed auto-advance decision is complete-with-error, not a clean reply")
	assert.Contains(t, stdout, name+"\terror (auto-advance failed)")
}

// Trap A: a replied task mid auto-advance (PID==0) must NOT be reported
// complete. With a timeout shorter than the guard streak, wait times out
// (exit 3) rather than falsely returning 0.
func TestWait_PendingAutoAdvance_NotComplete(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)
	// Large interval so the timeout fires on the first poll, before the
	// liveness guard's third-poll conclusion.
	t.Setenv("SUBTASK_TEST_WAIT_INTERVAL_MS", "5000")

	name := "fix/pending"
	pendingSeed(t, env, name)

	code, err, _, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{name}, Any: true, Timeout: 20 * time.Millisecond})
	require.NoError(t, err)
	assert.Equal(t, 3, code, "pending-auto-advance holds the task; barrier not met, timeout wins")
}

// Liveness guard: pending-auto-advance with no live claim persisting for
// pendingAdvanceGuardPolls concludes the supervisor died mid-advance
// (complete-with-error, exit 2). Driven deterministically via pollHook.
func TestWait_PendingAutoAdvance_LivenessGuard_Exit2(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)
	t.Setenv("SUBTASK_TEST_WAIT_INTERVAL_MS", "1")

	name := "fix/pending-dead"
	pendingSeed(t, env, name)

	polls := 0
	c := &WaitCmd{Tasks: []string{name}, Any: true, pollHook: func() { polls++ }}
	code, err, stdout, _ := runWaitCapture(t, c)
	require.NoError(t, err)
	assert.Equal(t, 2, code)
	assert.Equal(t, pendingAdvanceGuardPolls, polls, "guard concludes after exactly N polls")
	assert.Contains(t, stdout, "supervisor died mid-advance")
}

// Hard-kill mid-run (row 4): a SupervisorPID whose process is dead, with no
// worker.finished, is complete-with-error via a live IsStale() probe — no
// staleness reap required.
func TestWait_HardKillMidRun_Exit2(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("processAlive uses unix semantics")
	}
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	name := "fix/hardkill"
	env.CreateTask(name, "hardkill", "main", "")
	env.CreateTaskHistory(name, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "first-send", "base_branch": "main"})},
	})

	// Spawn+reap a child to obtain a definitely-dead PID.
	cmd := exec.Command("sleep", "0.05")
	require.NoError(t, cmd.Start())
	deadPID := cmd.Process.Pid
	require.NoError(t, cmd.Wait())

	env.CreateTaskState(name, &task.State{SupervisorPID: deadPID})

	code, err, stdout, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{name}})
	require.NoError(t, err)
	assert.Equal(t, 2, code)
	assert.Contains(t, stdout, name+"\terror (supervisor died)")
}

// Revival race: `send --detach` on a merged task claims SupervisorPID in
// state.json before the reopening event reaches history.jsonl. In that window
// the tail still says merged while a run is genuinely underway — a live claim
// must outrank the previous lifecycle's terminal status, or `send --detach X;
// wait X` returns success against the run it was supposed to wait for.
func TestWait_Classify_LiveSupervisorOutranksStaleMergedTail(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	name := "fix/revived-merged"
	seedMerged(t, env, name)
	// This process is definitionally alive, so IsStale() is false.
	env.CreateTaskState(name, &task.State{SupervisorPID: os.Getpid()})

	o := classify(name)
	assert.Equal(t, "working", o.Label)
	assert.False(t, o.Complete, "a live supervisor claim must not satisfy the barrier")
	assert.False(t, o.Errored)
}

func TestWait_Classify_LiveSupervisorOutranksStaleClosedTail(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	name := "fix/revived-closed"
	seedClosed(t, env, name)
	env.CreateTaskState(name, &task.State{SupervisorPID: os.Getpid()})

	o := classify(name)
	assert.Equal(t, "working", o.Label)
	assert.False(t, o.Complete)
}

// Converse of the revival race: only a LIVE claim outranks a terminal task
// status. A task merged with a leftover dead PID in state.json is merged — not
// a supervisor death — so the stale-PID row must stay below merged/closed.
func TestWait_Classify_StaleSupervisorDoesNotShadowMerged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("processAlive uses unix semantics")
	}
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	name := "fix/merged-stale-pid"
	seedMerged(t, env, name)

	// Spawn+reap a child to obtain a definitely-dead PID.
	cmd := exec.Command("sleep", "0.05")
	require.NoError(t, cmd.Start())
	deadPID := cmd.Process.Pid
	require.NoError(t, cmd.Wait())
	env.CreateTaskState(name, &task.State{SupervisorPID: deadPID})

	code, err, stdout, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{name}})
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, name+"\tmerged")
}

// Read-only proof: waiting over a merged and an errored task must not mutate
// state.json, history.jsonl, or create/modify the index.
func TestWait_ReadOnly_NoMutation(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	merged := "fix/ro-merged"
	seedMerged(t, env, merged)

	errored := "fix/ro-error"
	env.CreateTask(errored, "errored", "main", "")
	env.CreateTaskHistory(errored, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "first-send", "base_branch": "main"})},
		{Type: "message", Role: "worker", Content: "fail"},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "outcome": "error"})},
	})

	// Warm up the project layout so preflight's one-time bootstrap is not
	// mistaken for a wait-side write.
	_, err := preflightProject()
	require.NoError(t, err)

	type sig struct {
		size int64
		mod  time.Time
	}
	snap := func() map[string]sig {
		t.Helper()
		m := map[string]sig{}
		for _, p := range []string{
			filepath.Join(task.Dir(merged), "history.jsonl"),
			filepath.Join(task.Dir(errored), "history.jsonl"),
			task.StatePath(merged),
			task.StatePath(errored),
			task.IndexPath(),
		} {
			if fi, statErr := os.Stat(p); statErr == nil {
				m[p] = sig{fi.Size(), fi.ModTime()}
			}
		}
		return m
	}

	before := snap()
	code, err, _, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{merged, errored}})
	require.NoError(t, err)
	assert.Equal(t, 2, code, "errored task makes the satisfied barrier exit 2")

	after := snap()
	assert.Equal(t, before, after, "wait must not touch state.json, history.jsonl, or the index")
}

// --json happy path: a replied + merged pair with --all --json exits 0 and
// prints a parseable array using the task.UserStatus vocabulary, with the
// complete/error booleans set per task.
func TestWait_JSON_HappyPath(t *testing.T) {
	env := testutil.NewTestEnv(t, 2)
	withOutputMode(t, false)

	replied, merged := "fix/jr", "fix/jm"
	env.CreateTask(replied, "replied", "main", "")
	sendReplied(t, replied)
	seedMerged(t, env, merged)

	code, err, stdout, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{replied, merged}, JSON: true})
	require.NoError(t, err)
	assert.Equal(t, 0, code)

	var results []waitJSONResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &results))
	require.Len(t, results, 2)

	byName := map[string]waitJSONResult{}
	for _, r := range results {
		byName[r.Name] = r
	}

	assert.Equal(t, string(task.UserStatusReplied), byName[replied].Status)
	assert.True(t, byName[replied].Complete)
	assert.False(t, byName[replied].Error)

	assert.Equal(t, string(task.UserStatusMerged), byName[merged].Status)
	assert.True(t, byName[merged].Complete)
	assert.False(t, byName[merged].Error)
}

// Interrupt + --json contract: on SIGINT in JSON mode wait emits the standard
// JSON array to stdout (same shape as a normal finalize) and returns 130. The
// injected testSigCh + a large poll interval and no timeout make the interrupt
// the only ready select case, so the path fires deterministically on poll 1.
func TestWait_Interrupt_JSON_Exit130(t *testing.T) {
	env := testutil.NewTestEnv(t, 2)
	withOutputMode(t, false)
	t.Setenv("SUBTASK_TEST_WAIT_INTERVAL_MS", "600000")

	replied, working := "fix/ir", "fix/iw"
	env.CreateTask(replied, "replied", "main", "")
	sendReplied(t, replied)

	// A live supervisor PID (this test process) with no worker.finished keeps
	// the second task in `working` so the --all barrier is never met and the
	// loop blocks in select.
	env.CreateTask(working, "working", "main", "")
	env.CreateTaskHistory(working, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "first-send", "base_branch": "main"})},
	})
	env.CreateTaskState(working, &task.State{SupervisorPID: os.Getpid()})

	sigCh := make(chan os.Signal, 1)
	sigCh <- syscall.SIGINT
	c := &WaitCmd{Tasks: []string{replied, working}, JSON: true, testSigCh: sigCh}

	code, err, stdout, _ := runWaitCapture(t, c)
	require.NoError(t, err)
	assert.Equal(t, 130, code, "SIGINT → 128+2")

	var results []waitJSONResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &results), "interrupt in --json mode must still emit a valid JSON array on stdout")
	require.Len(t, results, 2)
}

// Boundary error: a task whose TASK.md exists but is unparseable must surface
// the real parse error (pointed at the task folder), NOT be masked as
// "not found".
func TestWait_CorruptTaskMd_SurfacesParseError(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	name := "fix/corrupt"
	env.CreateTask(name, "corrupt", "main", "")
	// Clobber TASK.md with content that has no frontmatter: task.Load returns
	// a parse error, not a not-found error.
	require.NoError(t, os.WriteFile(task.Path(name), []byte("no frontmatter here"), 0o644))

	code, err, _, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{name}})
	require.Error(t, err)
	assert.Equal(t, 0, code)
	assert.NotContains(t, err.Error(), "not found", "a parse failure must not be reported as a missing task")
	assert.Contains(t, err.Error(), "TASK.md")
	assert.Contains(t, err.Error(), "Tip:")
}
