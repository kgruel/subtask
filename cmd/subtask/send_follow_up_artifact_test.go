package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

// hasFollowUpSessionEvent reports whether the task recorded a
// worker.session action=follow_up event (i.e. a session was seeded).
func hasFollowUpSessionEvent(t *testing.T, taskName string) bool {
	t.Helper()
	evs, err := history.Read(taskName, history.ReadOptions{EventsOnly: true})
	require.NoError(t, err)
	for _, ev := range evs {
		if ev.Type != "worker.session" {
			continue
		}
		var d struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(ev.Data, &d); err == nil && d.Action == "follow_up" {
			return true
		}
	}
	return false
}

// followUpArtifactOnlyStamped reports whether any worker.started event carries
// the additive follow_up_artifact_only:true provenance field (item 4).
func followUpArtifactOnlyStamped(t *testing.T, taskName string) bool {
	t.Helper()
	evs, err := history.Read(taskName, history.ReadOptions{EventsOnly: true})
	require.NoError(t, err)
	for _, ev := range evs {
		if ev.Type != "worker.started" {
			continue
		}
		var d struct {
			FollowUpArtifactOnly bool `json:"follow_up_artifact_only"`
		}
		if err := json.Unmarshal(ev.Data, &d); err == nil && d.FollowUpArtifactOnly {
			return true
		}
	}
	return false
}

// TestSend_FollowUpMergedClaudeParent_ArtifactFallback: a claude follow-up whose
// parent was merged/closed (Workspace == "") can no longer duplicate the parent
// session. Previously a hard error; now it degrades to a fresh session + warn,
// and BuildPrompt injects the parent's artifacts as ## Parent Context.
func TestSend_FollowUpMergedClaudeParent_ArtifactFallback(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)
	setProjectAdapter(t, "claude", "claude-opus-4-5-20251101")

	parent := "parent/merged"
	env.CreateTask(parent, "Parent", "main", "Parent work.")
	env.CreateTaskHistory(parent, append(mustHistoryOpen(t, "main"),
		history.Event{Type: "worker.session", Data: mustJSON(map[string]any{
			"action": "follow_up", "harness": "claude", "session_id": "sess-parent",
		})}))
	// Merged: session recorded, workspace zeroed (as merge/close do).
	env.CreateTaskState(parent, &task.State{SessionID: "sess-parent", Adapter: "claude", Workspace: ""})

	child := "child/merged"
	env.CreateTask(child, "Child", "main", "Child work.")
	env.CreateTaskHistory(child, mustHistoryOpen(t, "main"))
	ct, err := task.Load(child)
	require.NoError(t, err)
	ct.FollowUp = parent
	require.NoError(t, ct.Save())

	mock := harness.NewMockHarness().
		WithDuplicateError(errors.New("duplicate session requires both oldCwd and newCwd")).
		WithResult("child reply", "")

	_, stderr, err := captureStdoutStderr(t, (&SendCmd{Task: child, Prompt: "Continue"}).WithHarness(mock).Run)
	require.NoError(t, err, "merged/closed claude parent must not hard-fail")

	require.Equal(t, 1, mock.RunCallCount())
	require.Empty(t, mock.LastRunCall().ContinueFrom, "child must run on a fresh session")
	require.False(t, hasFollowUpSessionEvent(t, child), "no session was seeded, so no follow_up event")
	require.Contains(t, stderr, "merged/closed")
	require.True(t, followUpArtifactOnlyStamped(t, child), "the degrade must stamp worker.started provenance")
}

// TestSend_FollowUpLiveParent_StillDuplicates: a follow-up from a live parent
// (workspace + session intact) still duplicates the session — no regression.
func TestSend_FollowUpLiveParent_StillDuplicates(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)
	setProjectAdapter(t, "claude", "claude-opus-4-5-20251101")

	parent := "parent/live"
	env.CreateTask(parent, "Parent", "main", "Parent work.")
	env.CreateTaskHistory(parent, append(mustHistoryOpen(t, "main"),
		history.Event{Type: "worker.session", Data: mustJSON(map[string]any{
			"action": "follow_up", "harness": "claude", "session_id": "sess-parent",
		})}))
	env.CreateTaskState(parent, &task.State{SessionID: "sess-parent", Adapter: "claude", Workspace: env.RootDir})

	child := "child/live"
	env.CreateTask(child, "Child", "main", "Child work.")
	env.CreateTaskHistory(child, mustHistoryOpen(t, "main"))
	ct, err := task.Load(child)
	require.NoError(t, err)
	ct.FollowUp = parent
	require.NoError(t, ct.Save())

	mock := harness.NewMockHarness().WithDuplicateResult("dup-123").WithResult("child reply", "dup-123")

	require.NoError(t, (&SendCmd{Task: child, Prompt: "Continue"}).WithHarness(mock).Run())

	cs, err := task.LoadState(child)
	require.NoError(t, err)
	require.NotNil(t, cs)
	require.Equal(t, "dup-123", cs.SessionID)
	require.True(t, hasFollowUpSessionEvent(t, child), "a live-parent dup must record a follow_up event")
	require.False(t, followUpArtifactOnlyStamped(t, child), "a successful resume is not an artifact-only degrade")
}

// TestSend_FollowUpLiveClaudeParent_DupFailsIsHardError: a claude parent that is
// still live (workspace present) but whose session dup fails (corrupt/missing
// session file) must keep today's hard error — it is NOT soft-degraded, so the
// failure stays diagnosable instead of being mislabeled "merged/closed".
func TestSend_FollowUpLiveClaudeParent_DupFailsIsHardError(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)
	setProjectAdapter(t, "claude", "claude-opus-4-5-20251101")

	parent := "parent/corrupt"
	env.CreateTask(parent, "Parent", "main", "Parent work.")
	env.CreateTaskHistory(parent, append(mustHistoryOpen(t, "main"),
		history.Event{Type: "worker.session", Data: mustJSON(map[string]any{
			"action": "follow_up", "harness": "claude", "session_id": "sess-parent",
		})}))
	// Live parent: workspace still exists (not merged/closed).
	env.CreateTaskState(parent, &task.State{SessionID: "sess-parent", Adapter: "claude", Workspace: env.RootDir})

	child := "child/corrupt"
	env.CreateTask(child, "Child", "main", "Child work.")
	env.CreateTaskHistory(child, mustHistoryOpen(t, "main"))
	ct, err := task.Load(child)
	require.NoError(t, err)
	ct.FollowUp = parent
	require.NoError(t, ct.Save())

	mock := harness.NewMockHarness().
		WithDuplicateError(errors.New("claude session not found at ...")).
		WithResult("child reply", "")

	_, _, err = captureStdoutStderr(t, (&SendCmd{Task: child, Prompt: "Continue"}).WithHarness(mock).Run)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to duplicate follow-up session")
	require.Equal(t, 0, mock.RunCallCount(), "the worker must not run on a hard dup failure")
	require.False(t, hasFollowUpSessionEvent(t, child))
}

// TestSend_FollowUpNeverDispatchedParent_ArtifactOnly: a follow-up whose parent
// is a real task that was never sent (no session at all) must not hard-error.
// It seeds no session but injects the parent's artifacts as ## Parent Context.
func TestSend_FollowUpNeverDispatchedParent_ArtifactOnly(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)
	setProjectAdapter(t, "claude", "claude-opus-4-5-20251101")

	parent := "parent/draft-only"
	env.CreateTask(parent, "Parent", "main", "Parent work.")
	// Hand-written PLAN.md, never dispatched (no state, no worker.session).
	require.NoError(t, os.WriteFile(filepath.Join(task.Dir(parent), "PLAN.md"), []byte("# Plan\n"), 0o644))

	child := "child/draft-only"
	env.CreateTask(child, "Child", "main", "Child work.")
	env.CreateTaskHistory(child, mustHistoryOpen(t, "main"))
	ct, err := task.Load(child)
	require.NoError(t, err)
	ct.FollowUp = parent
	require.NoError(t, ct.Save())

	mock := harness.NewMockHarness().WithResult("ok", "")

	_, stderr, err := captureStdoutStderr(t, (&SendCmd{Task: child, Prompt: "Continue"}).WithHarness(mock).Run)
	require.NoError(t, err, "a never-dispatched parent must not hard-fail")

	require.Equal(t, 1, mock.RunCallCount())
	require.Empty(t, mock.LastRunCall().ContinueFrom, "no session seeded")
	require.False(t, hasFollowUpSessionEvent(t, child))

	prompt := mock.LastRunCall().Prompt
	require.Contains(t, prompt, "## Parent Context")
	// Prompts render every path in slash form on every OS (see renderParentContext),
	// so the expectation is the slash-form of the native path, not filepath.Join's.
	require.Contains(t, prompt, filepath.ToSlash(filepath.Join(task.DirAbs(parent), "PLAN.md")))

	require.NotContains(t, stderr, "merged/closed", "no session existed to fail to resume, so no degrade warning")
	require.False(t, followUpArtifactOnlyStamped(t, child), "a never-dispatched parent has no session to degrade from")
}

// TestSend_FollowUpCrossAdapterMergedParent_ArtifactFallback: a merged/closed
// parent whose session ran a DIFFERENT adapter (claude) than the child's
// project adapter (codex) must not hard-fail. The unresumable cross-adapter
// session degrades to artifact-only continuity: no session seeded, ## Parent
// Context injected, and a warn naming the adapter mismatch. This is the
// documented cross-family aggregation flow (claude workers → codex aggregator).
func TestSend_FollowUpCrossAdapterMergedParent_ArtifactFallback(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)
	setProjectAdapter(t, "codex", "gpt-5-codex")

	parent := "parent/xadapter-merged"
	env.CreateTask(parent, "Parent", "main", "Parent work.")
	require.NoError(t, os.WriteFile(filepath.Join(task.Dir(parent), "PLAN.md"), []byte("# Plan\n"), 0o644))
	env.CreateTaskHistory(parent, append(mustHistoryOpen(t, "main"),
		history.Event{Type: "worker.session", Data: mustJSON(map[string]any{
			"action": "follow_up", "harness": "claude", "session_id": "sess-parent",
		})}))
	// Merged: claude session recorded, workspace zeroed (as merge/close do).
	env.CreateTaskState(parent, &task.State{SessionID: "sess-parent", Adapter: "claude", Workspace: ""})

	child := "child/xadapter-merged"
	env.CreateTask(child, "Child", "main", "Child work.")
	env.CreateTaskHistory(child, mustHistoryOpen(t, "main"))
	ct, err := task.Load(child)
	require.NoError(t, err)
	ct.FollowUp = parent
	require.NoError(t, ct.Save())

	mock := harness.NewMockHarness().WithResult("child reply", "")

	_, stderr, err := captureStdoutStderr(t, (&SendCmd{Task: child, Prompt: "Aggregate"}).WithHarness(mock).Run)
	require.NoError(t, err, "a cross-adapter merged parent must not hard-fail")

	require.Equal(t, 1, mock.RunCallCount())
	require.Empty(t, mock.LastRunCall().ContinueFrom, "no cross-adapter session may be resumed")
	require.False(t, hasFollowUpSessionEvent(t, child), "no session was seeded")

	prompt := mock.LastRunCall().Prompt
	require.Contains(t, prompt, "## Parent Context")

	// The warn names both adapters so the lead understands why the session
	// didn't resume.
	require.Contains(t, stderr, "parent session is claude")
	require.Contains(t, stderr, "codex")

	require.True(t, followUpArtifactOnlyStamped(t, child), "the cross-adapter degrade must stamp worker.started provenance")
}

// TestSend_FollowUpCrossAdapterLiveParent_ArtifactFallback: a LIVE parent
// (workspace intact) whose session ran a different adapter must not hard-fail
// either — a claude session file can't be resumed by codex regardless of
// whether the parent workspace still exists, so this degrades to
// artifact-only continuity just like the merged/closed case, with a warn
// that makes clear the parent is still live.
func TestSend_FollowUpCrossAdapterLiveParent_ArtifactFallback(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)
	setProjectAdapter(t, "codex", "gpt-5-codex")

	parent := "parent/xadapter-live"
	env.CreateTask(parent, "Parent", "main", "Parent work.")
	require.NoError(t, os.WriteFile(filepath.Join(task.Dir(parent), "PLAN.md"), []byte("# Plan\n"), 0o644))
	env.CreateTaskHistory(parent, append(mustHistoryOpen(t, "main"),
		history.Event{Type: "worker.session", Data: mustJSON(map[string]any{
			"action": "follow_up", "harness": "claude", "session_id": "sess-parent",
		})}))
	// Live parent: workspace still present (not merged/closed).
	env.CreateTaskState(parent, &task.State{SessionID: "sess-parent", Adapter: "claude", Workspace: env.RootDir})

	child := "child/xadapter-live"
	env.CreateTask(child, "Child", "main", "Child work.")
	env.CreateTaskHistory(child, mustHistoryOpen(t, "main"))
	ct, err := task.Load(child)
	require.NoError(t, err)
	ct.FollowUp = parent
	require.NoError(t, ct.Save())

	mock := harness.NewMockHarness().WithResult("child reply", "")

	_, stderr, err := captureStdoutStderr(t, (&SendCmd{Task: child, Prompt: "Aggregate"}).WithHarness(mock).Run)
	require.NoError(t, err, "a live cross-adapter parent must not hard-fail")

	require.Equal(t, 1, mock.RunCallCount())
	require.Empty(t, mock.LastRunCall().ContinueFrom, "no cross-adapter session may be resumed")
	require.False(t, hasFollowUpSessionEvent(t, child), "no session was seeded")

	prompt := mock.LastRunCall().Prompt
	require.Contains(t, prompt, "## Parent Context")

	// The warn distinguishes "parent is still live" from the merged/closed
	// wording, but still names both adapters.
	require.Contains(t, stderr, "parent is still live")
	require.Contains(t, stderr, "claude")
	require.Contains(t, stderr, "codex")

	require.True(t, followUpArtifactOnlyStamped(t, child), "the cross-adapter degrade must stamp worker.started provenance")
}
