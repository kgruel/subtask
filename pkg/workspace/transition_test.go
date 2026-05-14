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
		return workspace.FromState{Stage: raw, PresetName: "from-preset-" + raw}
	}

	from, err := workspace.ApplyStageTransition(taskName, "beta", "to-preset", nil, time.Now().UTC(), resolveFrom)
	require.NoError(t, err)
	require.Equal(t, "alpha", from.Stage, "FromState.Stage must equal the raw tail.Stage observed under lock")
	require.Equal(t, "from-preset-alpha", from.PresetName, "FromState.PresetName must equal the resolver's output")
}
