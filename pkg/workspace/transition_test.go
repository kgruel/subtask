package workspace_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
	"github.com/kgruel/subtask/pkg/workspace"
)

// TestApplyStageTransition_ConcurrentReadsFromInsideLock is the
// regression for the codex round-5 P2: the first iteration of the
// extracted helper read history.Tail BEFORE acquiring the lock, so two
// concurrent transitions could observe the same stale fromStage and
// both write a stage.changed event whose `from` referred to a
// pre-race state.
//
// Invariant under test: after a chain of transitions, each event's
// `from` must equal the previous event's `to`. With the bug, the
// second concurrent writer's `from` would still be the initial stage,
// breaking the chain.
//
// The race window is short, so we run several iterations and start
// both goroutines via a sync.WaitGroup to maximize the chance of
// triggering it. With the fix landed, history is always consistent.
func TestApplyStageTransition_ConcurrentReadsFromInsideLock(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	_ = env

	const taskName = "race/transitions"
	env.CreateTask(taskName, "Concurrent transitions", "main", "")

	// Seed history with an initial stage.changed so the from-chain has
	// a known starting value.
	initData, _ := json.Marshal(map[string]any{"from": "", "to": "initial"})
	require.NoError(t, history.WriteAll(taskName, []history.Event{
		{TS: time.Now().UTC(), Type: "task.opened"},
		{TS: time.Now().UTC(), Type: "stage.changed", Data: initData},
	}))

	const iterations = 20
	resolveFrom := func(raw string) workspace.FromState {
		// No defaulting needed — history.WriteAll seeded a stage.
		return workspace.FromState{Stage: raw}
	}

	for i := 0; i < iterations; i++ {
		var wg sync.WaitGroup
		wg.Add(2)

		// Both goroutines pick distinct target stages so we can tell
		// the writes apart in history.
		targetA := "step-a"
		targetB := "step-b"
		start := make(chan struct{})

		go func() {
			defer wg.Done()
			<-start
			_, err := workspace.ApplyStageTransition(taskName, targetA, "", nil, time.Now().UTC(), resolveFrom)
			require.NoError(t, err)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, err := workspace.ApplyStageTransition(taskName, targetB, "", nil, time.Now().UTC(), resolveFrom)
			require.NoError(t, err)
		}()

		close(start)
		wg.Wait()

		// Read all stage.changed events and verify the from→to chain
		// has no break. Each event's `from` must equal the previous
		// event's `to`. With the bug, two concurrent writers could
		// both emit from=<initial>/to=<their target>, breaking the
		// chain.
		evs, err := history.Read(taskName, history.ReadOptions{})
		require.NoError(t, err)

		var prevTo string
		var stageEvents int
		for _, ev := range evs {
			if ev.Type != "stage.changed" {
				continue
			}
			var d struct {
				From string `json:"from"`
				To   string `json:"to"`
			}
			require.NoError(t, json.Unmarshal(ev.Data, &d))
			if stageEvents > 0 {
				require.Equal(t, prevTo, d.From,
					"iteration %d, stage event %d: from=%q must equal previous event's to=%q (concurrent ApplyStageTransition observed stale fromStage)",
					i, stageEvents, d.From, prevTo)
			}
			prevTo = d.To
			stageEvents++
		}

		// Reset history for the next iteration so we don't accumulate
		// 40+ events and slow the test.
		resetData, _ := json.Marshal(map[string]any{"from": "", "to": "initial"})
		require.NoError(t, history.WriteAll(taskName, []history.Event{
			{TS: time.Now().UTC(), Type: "task.opened"},
			{TS: time.Now().UTC(), Type: "stage.changed", Data: resetData},
		}))
	}
}

// TestApplyStageTransition_ReturnsObservedFromState confirms the
// returned FromState matches what the resolveFrom closure was given
// (i.e. the value tail.Stage held inside the lock).
func TestApplyStageTransition_ReturnsObservedFromState(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	const taskName = "ret/from"
	in := &task.Task{Name: taskName, Title: "t", BaseBranch: "main"}
	require.NoError(t, in.Save())

	initData, _ := json.Marshal(map[string]any{"from": "", "to": "alpha"})
	require.NoError(t, history.WriteAll(taskName, []history.Event{
		{TS: time.Now().UTC(), Type: "stage.changed", Data: initData},
	}))

	resolveFrom := func(raw string) workspace.FromState {
		return workspace.FromState{Stage: raw, AgentName: "from-agent-" + raw}
	}

	from, err := workspace.ApplyStageTransition(taskName, "beta", "to-agent", nil, time.Now().UTC(), resolveFrom)
	require.NoError(t, err)
	require.Equal(t, "alpha", from.Stage, "FromState.Stage must equal the raw tail.Stage observed under lock")
	require.Equal(t, "from-agent-alpha", from.AgentName, "FromState.AgentName must equal the resolver's output")
}

// TestApplyStageTransition_SameStepNoOp verifies that targeting the current
// step returns FromState.NoOp = true and writes no history event. The check
// happens under the task lock so callers always observe the actual current
// step, not a stale pre-lock read.
func TestApplyStageTransition_SameStepNoOp(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	const taskName = "noop/same"
	in := &task.Task{Name: taskName, Title: "t", BaseBranch: "main"}
	require.NoError(t, in.Save())

	initData, _ := json.Marshal(map[string]any{"from": "", "to": "alpha"})
	require.NoError(t, history.WriteAll(taskName, []history.Event{
		{TS: time.Now().UTC(), Type: "stage.changed", Data: initData},
	}))

	resolveFrom := func(raw string) workspace.FromState {
		return workspace.FromState{Stage: raw}
	}

	from, err := workspace.ApplyStageTransition(taskName, "alpha", "", nil, time.Now().UTC(), resolveFrom)
	require.NoError(t, err)
	require.True(t, from.NoOp, "targeting the current step must return NoOp=true")
	require.Equal(t, "alpha", from.Stage)

	// History must not have grown.
	evs, err := history.Read(taskName, history.ReadOptions{})
	require.NoError(t, err)
	stageEvents := 0
	for _, ev := range evs {
		if ev.Type == "stage.changed" {
			stageEvents++
		}
	}
	require.Equal(t, 1, stageEvents, "no new stage.changed event on same-step no-op")
}

// TestApplyStageTransition_ConcurrentSameStepNoOp is the race regression for
// the unlocked same-step detection bug: the old code read history.Tail outside
// the lock, so a concurrent advance could cause `subtask stage <task> X` to
// print "already on step X" after the task had moved off X.
//
// Fix: the no-op check lives inside ApplyStageTransition's lock, so it observes
// the actual current step at lock-acquisition time, not a stale pre-lock read.
//
// This test fires many concurrent same-step calls against a task that sits on
// a known step. All must return NoOp=true and write zero history events.
func TestApplyStageTransition_ConcurrentSameStepNoOp(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	const taskName = "noop/concurrent"
	in := &task.Task{Name: taskName, Title: "t", BaseBranch: "main"}
	require.NoError(t, in.Save())

	initData, _ := json.Marshal(map[string]any{"from": "", "to": "alpha"})
	require.NoError(t, history.WriteAll(taskName, []history.Event{
		{TS: time.Now().UTC(), Type: "stage.changed", Data: initData},
	}))

	resolveFrom := func(raw string) workspace.FromState {
		return workspace.FromState{Stage: raw}
	}

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	start := make(chan struct{})
	results := make([]workspace.FromState, goroutines)
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = workspace.ApplyStageTransition(
				taskName, "alpha", "", nil, time.Now().UTC(), resolveFrom)
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "goroutine %d", i)
		require.True(t, results[i].NoOp, "goroutine %d: targeting current step must be a no-op under lock", i)
	}

	// No new stage.changed events must have been written.
	evs, err := history.Read(taskName, history.ReadOptions{})
	require.NoError(t, err)
	stageEvents := 0
	for _, ev := range evs {
		if ev.Type == "stage.changed" {
			stageEvents++
		}
	}
	require.Equal(t, 1, stageEvents, "concurrent same-step calls must write zero history events")
}
