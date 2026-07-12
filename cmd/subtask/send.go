package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kgruel/subtask/pkg/agent"
	"github.com/kgruel/subtask/pkg/git"
	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/logging"
	"github.com/kgruel/subtask/pkg/routine"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/task/migrate"
	"github.com/kgruel/subtask/pkg/task/store"
	"github.com/kgruel/subtask/pkg/workspace"
)

// SendCmd implements 'subtask send'.
type SendCmd struct {
	Task     string `arg:"" help:"Task name"`
	Prompt   string `arg:"" optional:"" help:"Message to send (or use stdin)"`
	Adapter  string `help:"Override adapter for this prompt (does not persist)"`
	Provider string `help:"Override provider for this prompt (adapter-dependent; does not persist)"`
	Model    string `help:"Override model for this prompt (does not persist)"`
	// Reasoning is adapter-dependent (e.g. codex, pi); not persisted.
	Reasoning string `help:"Override reasoning for this prompt (adapter-dependent; does not persist)"`
	Agent     string `help:"Agent override for adapter/model/reasoning (does not persist)"`
	Quiet     bool   `short:"q" help:"Suppress non-essential output (print reply only)"`
	// PinnedBase opts into branching from the draft-time captured base commit
	// instead of re-resolving the task's base branch to its current local HEAD.
	// Only affects the first send (when the task branch does not yet exist).
	PinnedBase bool `name:"pinned-base" help:"On first send, branch from the draft-time base commit instead of current base-branch HEAD"`

	// Detach dispatches the supervision run in a detached background process
	// and returns as soon as that process has claimed the task. Retrieve the
	// reply with `subtask reply <task>` after `subtask wait <task>`.
	Detach bool `help:"Dispatch in a detached supervisor process; return once it has claimed the task. Retrieve the reply with 'subtask reply <task>' after 'subtask wait <task>'."`

	// DetachChild (hidden) is set by the parent when it re-execs itself; its
	// value is the prompt temp-file path. Its presence means "I am the detached
	// supervisor child": read the prompt from that file (not arg/stdin) and
	// stamp detached:true on worker.started. Never set by a human.
	DetachChild string `name:"detach-child" hidden:"" help:"internal: detached-supervisor prompt-file path"`

	// Internal: injected harness for testing
	testHarness harness.Harness

	// dispatchDepth counts how many auto-advance rounds have chained
	// within a single user-level `subtask send`. Routine branches can
	// loop back (e.g. needs_more_data: true on the produced artifact
	// keeps re-entering the same step), so the recursion is NOT bounded
	// by step count in general. Cap with autoDispatchCap.
	dispatchDepth int

	// detached marks a run whose supervisor is a detached child. Set once in
	// resolvePrompt when DetachChild is present, stamped on worker.started as
	// provenance (no behavioral reads), and propagated across the auto-advance
	// recursion so every round of a detached chain carries it.
	detached bool
}

// autoDispatchCap bounds the number of routine auto-advance rounds that
// can chain inside a single `subtask send` invocation. The cap is a
// safety net for loopback misuse, not a feature limit — if a routine
// legitimately needs more iterations, the lead can re-run `subtask
// send` to continue.
const autoDispatchCap = 25

// WithHarness returns a copy with injected harness for testing.
func (c *SendCmd) WithHarness(h harness.Harness) *SendCmd {
	c.testHarness = h
	return c
}

// Run executes the send command.
func (c *SendCmd) Run() error {
	// Defense-in-depth: the child is spawned with --detach-child and never
	// --detach, so this combination is unreachable in normal operation.
	if c.Detach && c.DetachChild != "" {
		return fmt.Errorf("internal: --detach and --detach-child are mutually exclusive")
	}

	prompt, err := c.resolvePrompt()
	if err != nil {
		return err
	}

	// Requirements: git + global config (config may be migrated on first access).
	res, err := preflightProject()
	if err != nil {
		return err
	}
	cfg := res.Config

	// Ensure schema/history exist (one-time).
	if err := migrate.EnsureSchema(c.Task); err != nil {
		return err
	}

	t, err := task.Load(c.Task)
	if err != nil {
		return fmt.Errorf("task %q not found\n\nCreate it first:\n  subtask draft %s --base-branch <branch> --title \"...\"",
			c.Task, c.Task)
	}

	// The parent re-execs this same command as a detached child that runs the
	// byte-identical foreground body below and does its own claiming. The parent
	// only spawns, confirms the claim, and returns — so every state/staleness/
	// interrupt consumer keys off the child's real PID with no new state field.
	if c.Detach {
		return c.runDetachParent(prompt)
	}

	// Best-effort cleanup for stale supervisor PIDs.
	task.CleanupStaleTasks()

	// Ensure the supervisor is in its own process group so that other processes
	// (harness CLIs, etc.) can be interrupted via a single group signal.
	task.EnsureOwnProcessGroup()

	// Determine durable task state.
	tail, _ := history.Tail(c.Task)

	progress, _ := task.LoadProgress(c.Task)
	if progress == nil {
		progress = &task.Progress{}
	}

	// Create harness (needed for context session migration).
	// Resolve overrides so cfg reflects the effective adapter/model/reasoning for this run.
	var agentOverride *workspace.AgentSpec
	if c.Agent != "" {
		ag, agErr := agent.LoadByName(c.Agent)
		if agErr != nil {
			return agErr
		}
		spec := ag.AgentSpec()
		agentOverride = &spec
	}
	r, err := workspace.Resolve(cfg, t, workspace.ResolveOverrides{
		Adapter:   c.Adapter,
		Provider:  c.Provider,
		Model:     c.Model,
		Reasoning: c.Reasoning,
		Agent:     agentOverride,
	})
	if err != nil {
		return err
	}
	runAgent := c.resolvedEventAgent(cfg, t, r, tail.Stage)
	var h harness.Harness
	if c.testHarness != nil {
		h = c.testHarness
	} else {
		cfg = workspace.ConfigWithOverrides(cfg, r.Adapter, r.Provider, r.Model, r.Reasoning)
		h, err = harness.New(cfg)
		if err != nil {
			return err
		}
	}

	// Acquire/reuse workspace + mark running + write history start events.
	runID, err := history.NewRunID()
	if err != nil {
		return err
	}

	var runToolCalls atomic.Int64

	// Start time is stored atomically so the SIGINT handler can read it safely. We update
	// it later once the worker is about to run (excluding workspace prep time).
	var startedUnixNano atomic.Int64
	startedUnixNano.Store(time.Now().UTC().UnixNano())

	// Setup signal handling early so an interrupt during workspace prep doesn't leave a
	// stuck SupervisorPID claim.
	sigChan := make(chan os.Signal, 1)
	sigStop := make(chan struct{})
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		var sig os.Signal
		select {
		case sig = <-sigChan:
		case <-sigStop:
			return
		}
		finished := time.Now().UTC()
		started := time.Unix(0, startedUnixNano.Load()).UTC()
		durationMS := int(finished.Sub(started).Milliseconds())
		if durationMS < 0 {
			durationMS = 0
		}
		errMsg := "interrupted"

		owned := false
		_ = task.WithLock(c.Task, func() error {
			st, _ := task.LoadState(c.Task)
			if st == nil {
				return nil
			}
			if st.SupervisorPID != os.Getpid() {
				return nil
			}

			owned = true
			_ = history.AppendLocked(c.Task, history.Event{
				Type: "worker.interrupt",
				Data: mustJSON(map[string]any{
					"action":          "received",
					"run_id":          runID,
					"signal":          sig.String(),
					"supervisor_pid":  os.Getpid(),
					"supervisor_pgid": task.SelfProcessGroupID(),
				}),
				TS: finished,
			})
			_ = history.AppendLocked(c.Task, history.Event{
				Type: "worker.finished",
				Data: mustJSON(map[string]any{
					"run_id":        runID,
					"duration_ms":   durationMS,
					"outcome":       "error",
					"error":         errMsg,
					"error_message": errMsg,
					"tool_calls":    int(runToolCalls.Load()),
					"stage":         tail.Stage,
					"agent":         runAgent,
				}),
				TS: finished,
			})

			st.SupervisorPID = 0
			st.SupervisorPGID = 0
			st.StartedAt = time.Time{}
			st.LastError = errMsg
			return st.Save(c.Task)
		})

		if owned {
			logging.Error("harness", fmt.Sprintf("task=%s %s error: %s", c.Task, cfg.Adapter, errMsg))
			logging.Info("worker", fmt.Sprintf("task=%s finished outcome=error duration=%s", c.Task, finished.Sub(started).Round(time.Second)))
		}
		os.Exit(1)
	}()
	defer func() {
		close(sigStop)
		signal.Stop(sigChan)
	}()

	wsPath, prevWorkspace, continueFrom, repoStatus, err := c.prepareWorkspaceAndState(cfg, h, t, tail, prompt, runID, runAgent)
	if err != nil {
		return err
	}

	// If we continued a session and the workspace changed, migrate it if the
	// adapter supports it. For the claude handler migration is a MOVE, so a
	// failure means the worker would resume against a stale/missing session. We
	// do NOT hard-fail here: prepareWorkspaceAndState has already claimed
	// SupervisorPID and appended worker.started, and a bare return would skip
	// the worker-failure cleanup below, leaving the task stuck "working" until
	// stale cleanup. Instead warn (the worker still runs, resuming a
	// possibly-incomplete session) and append the "migrated" event only on
	// actual success, so history.jsonl never asserts a migration that didn't
	// happen.
	if continueFrom != "" && prevWorkspace != "" && filepath.Clean(prevWorkspace) != filepath.Clean(wsPath) {
		if err := h.MigrateSession(continueFrom, prevWorkspace, wsPath); err != nil {
			// The exact outcome depends on how far migration got (no session,
			// or a partially-relocated one), so don't over-claim "no context".
			c.warn(fmt.Sprintf("could not migrate %s session to the new workspace: %v\n  The worker will still resume, but its prior context may be missing or stale. If the run looks wrong, start a fresh task.", cfg.Adapter, err))
		} else {
			_ = history.Append(c.Task, history.Event{
				Type: "worker.session",
				Data: mustJSON(map[string]any{
					"action":     "migrated",
					"harness":    cfg.Adapter,
					"session_id": continueFrom,
				}),
			})
		}
	}

	// Build prompt. A failure here (e.g. missing agent file) must route
	// through the worker-failure cleanup below — prepareWorkspaceAndState
	// has already claimed SupervisorPID and appended worker.started, so a
	// bare return would leave the task stuck "running" until stale
	// cleanup. We assign the error to runErr, skip h.Run, and let the
	// existing failure path emit worker.finished + clear state.
	fullPrompt, buildErr := harness.BuildPrompt(t, wsPath, false, tail.Stage, prompt, repoStatus)

	// Reset start time for the worker run (exclude workspace preparation).
	startedUnixNano.Store(time.Now().UTC().UnixNano())

	// Snapshot shared files before execution (exclude history.jsonl).
	sharedBefore := SnapshotTaskFiles(c.Task)

	var result *harness.Result
	var runErr error
	// resolvedWorkerLabel is set in the else branch so spinner and result footer share
	// the same label (including any --model/--adapter/--agent override applied to cfg).
	var resolvedWorkerLabel string
	if buildErr != nil {
		runErr = buildErr
	} else {
		c.info(fmt.Sprintf("Sending to task: %s", c.Task))
		v, _ := store.BuildView(context.Background(), c.Task, cfg, store.BuildViewOptions{Stage: tail.Stage})
		if v != nil {
			v.Agent.Adapter = r.Adapter
			v.Agent.Model = r.Model
			v.Agent.Reasoning = r.Reasoning
			if c.Agent != "" {
				v.Agent.Name = c.Agent
			}
			resolvedWorkerLabel = v.Agent.Label()
		} else {
			resolvedWorkerLabel = task.AgentView{
				Name:    t.Agent,
				Adapter: r.Adapter,
				Model:   r.Model,
			}.Label()
		}
		c.info(fmt.Sprintf("[Waiting for %s...]", resolvedWorkerLabel))

		// runToolCalls is tracked atomically for accurate interruption accounting.
		callbacks := harness.Callbacks{
			OnToolCall: func(tm time.Time) {
				runToolCalls.Add(1)
				progress.ToolCalls++
				progress.LastActive = tm
				_ = progress.Save(c.Task)
			},
			OnSessionStart: func(sessionID string) {
				_ = task.WithLock(c.Task, func() error {
					st, _ := task.LoadState(c.Task)
					if st == nil {
						st = &task.State{}
					}
					st.SessionID = sessionID
					st.Adapter = cfg.Adapter
					return st.Save(c.Task)
				})
				_ = history.Append(c.Task, history.Event{
					Type: "worker.session",
					Data: mustJSON(map[string]any{
						"action":     "started",
						"harness":    cfg.Adapter,
						"session_id": sessionID,
					}),
				})
			},
		}

		result, runErr = h.Run(context.Background(), wsPath, fullPrompt, continueFrom, callbacks)
	}
	finished := time.Now().UTC()
	started := time.Unix(0, startedUnixNano.Load()).UTC()
	durationMS := int(finished.Sub(started).Milliseconds())

	reply := ""
	nextSessionID := ""
	if result != nil {
		reply = result.Reply
		nextSessionID = result.SessionID
	}

	// Defensive: treat "success with empty reply" as an error so we don't write empty
	// worker messages to history.jsonl. This can happen if a harness/CLI fails to
	// surface an error (or returns AgentReplied=false without a hard error).
	if runErr == nil && strings.TrimSpace(reply) == "" {
		errMsg := "worker produced empty reply"
		if result != nil && strings.TrimSpace(result.SessionID) != "" {
			errMsg = fmt.Sprintf("%s (session %s)", errMsg, strings.TrimSpace(result.SessionID))
		}
		if result != nil && strings.TrimSpace(result.Error) == "" {
			result.Error = errMsg
		}
		runErr = fmt.Errorf("%s", errMsg)
	}

	if runErr != nil {
		errMsg := strings.TrimSpace(runErr.Error())
		if result != nil && strings.TrimSpace(result.Error) != "" {
			errMsg = strings.TrimSpace(result.Error)
		}
		if errMsg == "" {
			errMsg = "worker failed"
		}

		// When a subprocess in the supervisor's process group receives SIGINT, some
		// harnesses surface this as an exec error ("signal: interrupt") rather than
		// triggering our process-level signal handler. Treat that case as an
		// interruption for consistent state/history semantics.
		if isLikelyInterruptedError(errMsg) {
			errMsg = "interrupted"
			_ = history.Append(c.Task, history.Event{
				Type: "worker.interrupt",
				Data: mustJSON(map[string]any{
					"action":          "received",
					"run_id":          runID,
					"signal":          "SIGINT",
					"supervisor_pid":  os.Getpid(),
					"supervisor_pgid": task.SelfProcessGroupID(),
				}),
				TS: finished,
			})
		}

		_ = history.Append(c.Task, history.Event{
			Type: "worker.finished",
			Data: mustJSON(map[string]any{
				"run_id":        runID,
				"duration_ms":   durationMS,
				"tool_calls":    int(runToolCalls.Load()),
				"outcome":       "error",
				"error":         errMsg,
				"error_message": errMsg,
				"stage":         tail.Stage,
				"agent":         runAgent,
			}),
			TS: finished,
		})

		// Clear running fields after history is written, before printing/returning.
		_ = task.WithLock(c.Task, func() error {
			st, _ := task.LoadState(c.Task)
			if st == nil {
				st = &task.State{}
			}
			st.SupervisorPID = 0
			st.SupervisorPGID = 0
			st.StartedAt = time.Time{}
			st.LastError = errMsg
			if nextSessionID != "" {
				st.SessionID = nextSessionID
			}
			return st.Save(c.Task)
		})

		logging.Error("harness", fmt.Sprintf("task=%s %s error: %s", c.Task, cfg.Adapter, errMsg))
		logging.Info("worker", fmt.Sprintf("task=%s finished outcome=error duration=%s", c.Task, finished.Sub(started).Round(time.Second)))
		return runErr
	}

	// Snapshot shared files after execution and find changes.
	sharedAfter := SnapshotTaskFiles(c.Task)
	changedFiles := ChangedTaskFiles(sharedBefore, sharedAfter)

	// Success: append worker message + finish event.
	//
	// Stamp the stage active when the worker ran on worker.finished. The
	// routine auto-advance below moves t.Stage, so by the time `subtask
	// unread` evaluates the notification policy, tail.Stage is already
	// past the silent step. Recording the stage at finish time lets the
	// unread reader evaluate notify: false / surface: false against the
	// stage the reply ACTUALLY belongs to, not the post-advance stage.
	_ = history.Append(c.Task, history.Event{
		Type:    "message",
		Role:    "worker",
		Content: reply,
		Data:    mustJSON(map[string]any{"agent": runAgent}),
		TS:      finished,
	})
	_ = history.Append(c.Task, history.Event{
		Type: "worker.finished",
		Data: mustJSON(map[string]any{
			"run_id":      runID,
			"duration_ms": durationMS,
			"tool_calls":  int(runToolCalls.Load()),
			"outcome":     "replied",
			"stage":       tail.Stage,
			"agent":       runAgent,
		}),
		TS: finished,
	})

	// Commit the run's session/error outcome after history is written, before
	// printing output. The supervisor claim (SupervisorPID/PGID/StartedAt) is
	// deliberately NOT cleared here: the auto-advance decision and the next
	// round's pre-claim work (preflight, migrate, stale cleanup) run in this
	// same process, and holding the claim across that window is what lets
	// `wait` (and list/show) distinguish a live advancing supervisor (live
	// PID) from one that died mid-advance (stale PID). Every non-dispatch
	// exit below releases the claim; a dispatched round re-claims under this
	// same PID inside next.Run().
	_ = task.WithLock(c.Task, func() error {
		st, _ := task.LoadState(c.Task)
		if st == nil {
			st = &task.State{}
		}
		st.LastError = ""
		if nextSessionID != "" {
			st.SessionID = nextSessionID
			st.Adapter = cfg.Adapter
		}
		return st.Save(c.Task)
	})

	// Auto-advance stage/step if the current node has advance: auto. Runs
	// after the cleanup block so transition layers next-step state (adapter
	// swap, session clear) on top of the just-committed run's
	// session/adapter — not the other way around.
	//
	// displayStage tracks the step to show in the footer. When auto-advance
	// lands on a gate or terminal (Dispatch=false, NextStep!=""), HandleAutoAdvance
	// has already persisted that transition, so tail.Stage is stale.
	displayStage := tail.Stage

	// Routine auto-advance: re-enter SendCmd when the step's advance policy
	// triggers immediately after the worker finishes.
	if t.Routine != "" {
		r, rErr := routine.LoadByName(t.Routine)
		if rErr != nil {
			c.releaseSupervisorClaim()
			return rErr
		}
		currentStep := tail.Stage
		if currentStep == "" {
			currentStep = r.EntryStep()
		}
		adv, advErr := routine.HandleAutoAdvance(c.Task, r, currentStep, finished)
		if advErr != nil {
			c.recordAutoAdvanceFailure(advErr)
			return advErr
		}
		if adv.NextStep != "" && !adv.Dispatch {
			displayStage = adv.NextStep
		}
		if adv.Dispatch {
			// Re-enter SendCmd for the new step. Each dispatch is a full
			// worker round-trip, but routine branches support loopbacks
			// (a step can have a `branches:` edge back to itself), so the
			// chain is NOT bounded by step count. The cap protects against
			// a runaway loop where the produced artifact keeps satisfying
			// the loopback predicate.
			if c.dispatchDepth+1 >= autoDispatchCap {
				capErr := fmt.Errorf("auto-advance dispatch limit reached (%d rounds in a single send) — routine may be stuck in a loop; inspect the produced artifact and re-run `subtask send %s` to continue if intentional, or fix the loopback condition", autoDispatchCap, c.Task)
				c.recordAutoAdvanceFailure(capErr)
				return capErr
			}
			// Propagate user-set output mode across the recursion. Quiet
			// is the only field of c that carries past a single round
			// — Adapter/Model/etc are deliberately not propagated so a
			// later routine step's agent binding (or the task's
			// adapter snapshot) wins over a one-shot CLI override.
			// PinnedBase only affects first send, so it's irrelevant
			// for the auto-advance path (branch already exists). detached
			// rides the whole chain so every round stamps its provenance;
			// Detach/DetachChild deliberately do not, so the chain never
			// re-detaches and runs entirely under one child PID.
			next := &SendCmd{
				Task:        c.Task,
				Prompt:      adv.DispatchPrompt,
				Quiet:       c.Quiet,
				testHarness: c.testHarness,
				detached:    c.detached,
			}
			next.dispatchDepth = c.dispatchDepth + 1
			// The recursive round re-claims under this same PID (the claim
			// guards exempt os.Getpid()) and releases on its own exit paths,
			// so no release here on either branch.
			if err := next.Run(); err != nil {
				c.recordAutoAdvanceFailure(err)
				return err
			}
			return nil
		}
	}

	// No dispatch happened (no routine, or the advance landed on a gate or
	// terminal): the advance window is over, release the held claim.
	c.releaseSupervisorClaim()

	logging.Info("worker", fmt.Sprintf("task=%s finished outcome=replied duration=%s", c.Task, finished.Sub(started).Round(time.Second)))

	if c.Quiet {
		if reply != "" {
			fmt.Print(reply)
			if !strings.HasSuffix(reply, "\n") {
				fmt.Println()
			}
		}
		return nil
	}

	PrintWorkerResultWithStage(c.Task, reply, int(runToolCalls.Load()), changedFiles, displayStage, resolvedWorkerLabel)
	return nil
}

// releaseSupervisorClaim zeroes SupervisorPID/PGID/StartedAt, but only when
// the claim is this process's own — a claim held by any other PID (a newer
// run, live or not) is never clobbered. This is the counterpart of holding
// the claim across the post-reply auto-advance window: every exit from that
// window that does not dispatch another round must release.
func (c *SendCmd) releaseSupervisorClaim() {
	_ = task.WithLock(c.Task, func() error {
		st, _ := task.LoadState(c.Task)
		if st == nil || st.SupervisorPID != os.Getpid() {
			return nil
		}
		st.SupervisorPID = 0
		st.SupervisorPGID = 0
		st.StartedAt = time.Time{}
		return st.Save(c.Task)
	})
}

// recordAutoAdvanceFailure durably stamps a post-reply auto-advance failure
// onto state.LastError so wait/show/list/unread can see it. By the time this
// fires, the just-finished round's worker.finished(outcome=replied) is already
// committed and state.LastError already cleared, so a failed auto-advance
// decision (HandleAutoAdvance error) or a failed recursive dispatch (next.Run
// error) would otherwise leave no durable trace: under --detach the error only
// reaches the supervisor log, and under -q it is swallowed entirely. Releases
// this process's own advance-window claim in the same locked write; any other
// live claim (a newer run) is never clobbered.
func (c *SendCmd) recordAutoAdvanceFailure(err error) {
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		msg = "unknown error"
	}
	_ = task.WithLock(c.Task, func() error {
		st, _ := task.LoadState(c.Task)
		if st == nil {
			return nil
		}
		switch st.SupervisorPID {
		case os.Getpid():
			st.SupervisorPID = 0
			st.SupervisorPGID = 0
			st.StartedAt = time.Time{}
		case 0:
			// already released (e.g. a failed recursive dispatch cleared it)
		default:
			return nil // a different claim owns the state; never clobber
		}
		st.LastError = "auto-advance failed: " + msg
		return st.Save(c.Task)
	})
}

func (c *SendCmd) prepareWorkspaceAndState(cfg *workspace.Config, h harness.Harness, t *task.Task, tail history.TailInfo, prompt, runID string, runAgent history.EventAgent) (wsPath, prevWorkspace, continueFrom string, repoStatus *harness.RepoStatus, err error) {
	now := time.Now().UTC()

	var st *task.State
	if loaded, err := task.LoadState(c.Task); err == nil {
		st = loaded
	}

	// Session compatibility: don't attempt to continue a session across harnesses.
	if st != nil && strings.TrimSpace(st.SessionID) != "" {
		prevHarness := sessionHarnessForTask(c.Task, st)
		if prevHarness != "" && prevHarness != cfg.Adapter {
			// Best-effort: persist inferred harness for future runs.
			if strings.TrimSpace(st.Adapter) == "" {
				_ = task.WithLock(c.Task, func() error {
					locked, _ := task.LoadState(c.Task)
					if locked == nil {
						locked = &task.State{}
					}
					if strings.TrimSpace(locked.Adapter) == "" {
						locked.Adapter = prevHarness
						_ = locked.Save(c.Task)
					}
					return nil
				})
			}
			return "", "", "", nil, fmt.Errorf("task %q has an existing session from harness %q, but this project is configured for %q\n\n"+
				"Sessions are not compatible across harnesses.\n"+
				"Tip: clear the session by deleting state.json, or use a new task.",
				c.Task, prevHarness, cfg.Adapter)
		}
	}

	// Hard guard: don't allow two concurrent sends on the same machine. Our
	// own PID is exempt: an auto-advance round holds the claim across the
	// advance window and re-claims here in the same process.
	if st != nil && st.SupervisorPID != 0 && st.SupervisorPID != os.Getpid() && !st.IsStale() {
		return "", "", "", nil, errTaskWorking(c.Task)
	}

	// Test-only: deterministic barrier to coordinate concurrent send attempts.
	if err := maybeWaitSendBarrier(); err != nil {
		return "", "", "", nil, err
	}

	claimedPID := os.Getpid()
	claimed := false
	defer func() {
		if !claimed || err == nil {
			return
		}
		errMsg := strings.TrimSpace(err.Error())
		if errMsg == "" {
			errMsg = "send failed"
		}
		_ = task.WithLock(c.Task, func() error {
			locked, _ := task.LoadState(c.Task)
			if locked == nil {
				return nil
			}
			if locked.SupervisorPID != claimedPID {
				return nil
			}
			locked.SupervisorPID = 0
			locked.SupervisorPGID = 0
			locked.StartedAt = time.Time{}
			locked.LastError = errMsg
			return locked.Save(c.Task)
		})
	}()

	// Claim the task early (before git worktree operations) to prevent a race where two sends
	// concurrently try to check out the same branch in different worktrees.
	if err := task.WithLock(c.Task, func() error {
		locked, _ := task.LoadState(c.Task)
		if locked == nil {
			locked = &task.State{}
		}
		if locked.SupervisorPID != 0 && locked.SupervisorPID != claimedPID && !locked.IsStale() {
			return errTaskWorking(c.Task)
		}
		locked.SupervisorPID = claimedPID
		locked.SupervisorPGID = task.SelfProcessGroupID()
		locked.StartedAt = now
		locked.LastError = ""
		locked.Adapter = cfg.Adapter
		return locked.Save(c.Task)
	}); err != nil {
		return "", "", "", nil, err
	}
	claimed = true

	// Reuse workspace when available.
	if st != nil && st.Workspace != "" {
		if info, err := os.Stat(st.Workspace); err == nil && info.IsDir() {
			wsPath = st.Workspace
			c.info(fmt.Sprintf("Using existing workspace: %s", abbreviatePath(wsPath)))
		}
	}

	// Acquire new workspace if needed.
	if wsPath == "" {
		pool := workspace.NewPool()
		acq, err := pool.Acquire()
		if err != nil {
			return "", "", "", nil, err
		}
		wsPath = acq.Entry.Path
		defer acq.Release()
		c.info(fmt.Sprintf("Assigned workspace: %s", abbreviatePath(wsPath)))

		// Ensure task branch exists (open tasks reuse branch; merged tasks reopen from base).
		branchExists := git.BranchExists(wsPath, t.Name)
		switch tail.TaskStatus {
		case task.TaskStatusMerged:
			branchExists = false
		}

		firstSend := false
		if branchExists {
			if err := git.Checkout(wsPath, t.Name); err != nil {
				return "", "", "", nil, fmt.Errorf("failed to checkout branch %q: %w", t.Name, err)
			}
		} else {
			firstSend = tail.TaskStatus == task.TaskStatusOpen
			// Default: re-resolve to current base-branch HEAD on first send so
			// pre-drafted tasks pick up commits that landed between draft and send.
			// `--pinned-base` opts into the older "branch from draft-time commit" behavior.
			baseRef := ""
			if c.PinnedBase && tail.TaskStatus != task.TaskStatusMerged {
				baseRef = strings.TrimSpace(tail.BaseCommit)
			}
			if err := git.SetupBranch(wsPath, t.Name, t.BaseBranch, baseRef); err != nil {
				// If the recorded base commit is missing (e.g., rewritten history), fall back to base branch HEAD.
				if baseRef != "" {
					if err2 := git.SetupBranch(wsPath, t.Name, t.BaseBranch, ""); err2 == nil {
						baseRef = ""
					} else {
						return "", "", "", nil, fmt.Errorf("git setup failed: %w", err)
					}
				} else {
					return "", "", "", nil, fmt.Errorf("git setup failed: %w", err)
				}
			}
		}

		ensureTaskSymlink(wsPath, c.Task)

		// Record a task.opened event whenever we created a fresh branch (first send,
		// or reopen from merged/closed). The event captures the actual base commit
		// the worker is starting from, which downstream code reads for diff/staleness.
		switch {
		case tail.TaskStatus != task.TaskStatusOpen:
			baseCommit, _ := git.Output(wsPath, "rev-parse", "HEAD")
			data := mustJSON(map[string]any{
				"reason":      "reopen",
				"from":        string(tail.TaskStatus),
				"branch":      c.Task,
				"base_branch": t.BaseBranch,
				"base_commit": baseCommit,
			})
			_ = history.Append(c.Task, history.Event{Type: "task.opened", Data: data})
		case firstSend:
			baseCommit, _ := git.Output(wsPath, "rev-parse", "HEAD")
			data := mustJSON(map[string]any{
				"reason":      "first-send",
				"branch":      c.Task,
				"base_branch": t.BaseBranch,
				"base_commit": baseCommit,
				"pinned":      c.PinnedBase,
			})
			_ = history.Append(c.Task, history.Event{Type: "task.opened", Data: data})
		}
	}

	// Compute repoStatus warning (best-effort).
	if t.BaseBranch != "" {
		// Local-first: compare against the local base branch only.
		target := t.BaseBranch
		if git.BranchExists(wsPath, target) {
			conflicts, err := git.MergeConflictFiles(wsPath, target, "HEAD")
			if err == nil && len(conflicts) > 0 {
				repoStatus = &harness.RepoStatus{ConflictFiles: conflicts}
			}
		}
	}

	// Follow-up: seed session from a previous task/session (before marking running).
	var followUpSeed *followUpSeed
	// followUpArtifactOnly marks a follow-up that degraded to artifact-only
	// continuity: either a merged/closed claude parent whose session can't be
	// duplicated (set in the dup block below), or a merged/closed parent whose
	// session harness is incompatible with this adapter (set here, from the
	// seed). Both drive the continuity-downgrade warn and the worker.started
	// provenance stamp.
	var followUpArtifactOnly bool
	if (st == nil || strings.TrimSpace(st.SessionID) == "") && strings.TrimSpace(t.FollowUp) != "" {
		seed, err := resolveFollowUpSeed(cfg.Adapter, t.FollowUp)
		if err != nil {
			return "", "", "", nil, err
		}
		followUpSeed = seed
		// resolveFollowUpSeed already cleared the session for a cross-adapter
		// merged/closed parent; flag the degrade so the warn + provenance fire
		// even though the dup block below is skipped (empty session).
		if seed != nil && strings.TrimSpace(seed.IncompatibleParentHarness) != "" {
			followUpArtifactOnly = true
		}
	}

	// Set running state and append start events.
	err = task.WithLock(c.Task, func() error {
		locked, _ := task.LoadState(c.Task)
		if locked == nil {
			locked = &task.State{}
		}
		if locked.SupervisorPID != 0 && !locked.IsStale() && locked.SupervisorPID != os.Getpid() {
			return errTaskWorking(c.Task)
		}
		prevWorkspace = locked.Workspace

		locked.Workspace = wsPath
		locked.SupervisorPID = os.Getpid()
		locked.SupervisorPGID = task.SelfProcessGroupID()
		locked.StartedAt = now
		locked.LastError = ""
		locked.Adapter = cfg.Adapter

		// If this is a follow-up task, duplicate (or continue) the prior session once
		// and persist it before running.
		if strings.TrimSpace(locked.SessionID) == "" && followUpSeed != nil && strings.TrimSpace(followUpSeed.FromSessionID) != "" {
			newSessionID := ""
			if cfg.Adapter != "opencode" {
				dup, derr := h.DuplicateSession(followUpSeed.FromSessionID, followUpSeed.FromWorkspace, wsPath)
				if derr == nil && strings.TrimSpace(dup) != "" {
					newSessionID = strings.TrimSpace(dup)
				} else if cfg.Adapter == "claude" {
					if strings.TrimSpace(followUpSeed.FromWorkspace) == "" {
						// Parent workspace is gone (merged/closed) → claude's
						// cwd-keyed session files can't be duplicated. Start the
						// child on a fresh session; BuildPrompt's "## Parent
						// Context" block carries the parent's artifacts forward
						// (artifacts-first continuity). Warn after the lock.
						followUpArtifactOnly = true
					} else {
						// Live parent, but duplication failed for another reason
						// (missing/corrupt session file, unresolved projects root,
						// I/O error). Unexpected and actionable — do NOT silently
						// degrade it with a false "merged/closed" cause; keep the
						// hard failure so diagnosability isn't lost.
						if derr == nil {
							derr = fmt.Errorf("duplicate session returned empty session ID")
						}
						return fmt.Errorf("failed to duplicate follow-up session from %q: %w\n\nTip: run without --follow-up to start a fresh session.", t.FollowUp, derr)
					}
				}
			}
			// Non-claude adapters continue the original session as before (codex
			// sessions are global; opencode skips duplication). Claude never
			// continues-original: it needs the parent workspace's session files,
			// which is exactly what's missing here.
			if strings.TrimSpace(newSessionID) == "" && cfg.Adapter != "claude" {
				newSessionID = strings.TrimSpace(followUpSeed.FromSessionID)
			}
			if strings.TrimSpace(newSessionID) != "" {
				locked.SessionID = newSessionID
				locked.Adapter = cfg.Adapter
				_ = history.AppendLocked(c.Task, history.Event{
					Type: "worker.session",
					Data: mustJSON(map[string]any{
						"action":       "follow_up",
						"harness":      cfg.Adapter,
						"session_id":   newSessionID,
						"from_task":    t.FollowUp,
						"from_session": followUpSeed.FromSessionID,
					}),
					TS: now,
				})
			}
		}
		if err := locked.Save(c.Task); err != nil {
			return err
		}

		// Persist the lead message + run start.
		_ = history.AppendLocked(c.Task, history.Event{
			Type:    "message",
			Role:    "lead",
			Content: prompt,
			TS:      now,
		})
		startedData := map[string]any{
			"run_id":       runID,
			"prompt_bytes": len([]byte(prompt)),
			"agent":        runAgent,
		}
		if c.detached {
			startedData["detached"] = true
		}
		// Provenance for the continuity downgrade: the warn is invisible under
		// -q and lands in supervisor.log under --detach, so stamp it on the
		// event too (additive field, no behavioral reads — mirrors "detached").
		if followUpArtifactOnly {
			startedData["follow_up_artifact_only"] = true
		}
		_ = history.AppendLocked(c.Task, history.Event{
			Type: "worker.started",
			Data: mustJSON(startedData),
			TS:   now,
		})
		logging.Info("worker", fmt.Sprintf("task=%s started run=%s", c.Task, runID))

		continueFrom = strings.TrimSpace(locked.SessionID)
		return nil
	})
	if err != nil {
		return "", "", "", nil, err
	}

	if followUpArtifactOnly {
		if followUpSeed != nil && strings.TrimSpace(followUpSeed.IncompatibleParentHarness) != "" {
			c.warn(fmt.Sprintf(
				"follow-up %q: parent session is %s, this task runs %s; continuing with its artifacts (TASK.md/PLAN.md/PROGRESS.json and produced files) injected as read-only context.",
				t.FollowUp, followUpSeed.IncompatibleParentHarness, cfg.Adapter))
		} else {
			c.warn(fmt.Sprintf(
				"follow-up %q was merged/closed; its %s conversation can't be resumed.\n"+
					"  Continuing with its artifacts (TASK.md/PLAN.md/PROGRESS.json and produced files) injected as read-only context.",
				t.FollowUp, cfg.Adapter))
		}
	}

	return wsPath, prevWorkspace, continueFrom, repoStatus, nil
}

func (c *SendCmd) resolvedEventAgent(cfg *workspace.Config, t *task.Task, r workspace.Resolved, stage string) history.EventAgent {
	name := ""
	if v, _ := store.BuildView(context.Background(), c.Task, cfg, store.BuildViewOptions{Stage: stage}); v != nil {
		name = v.Agent.Name
	}
	if c.Agent != "" {
		name = c.Agent
	}
	if name == "" && t != nil {
		name = t.Agent
	}
	return history.EventAgent{
		Name:      name,
		Adapter:   r.Adapter,
		Model:     r.Model,
		Reasoning: r.Reasoning,
	}
}

const (
	testSendBarrierDirEnv       = "SUBTASK_TEST_SEND_BARRIER_DIR"
	testSendBarrierNEnv         = "SUBTASK_TEST_SEND_BARRIER_N"
	testSendBarrierTimeoutMSEnv = "SUBTASK_TEST_SEND_BARRIER_TIMEOUT_MS"
)

func maybeWaitSendBarrier() error {
	dir := strings.TrimSpace(os.Getenv(testSendBarrierDirEnv))
	if dir == "" {
		return nil
	}

	n := 2
	if s := strings.TrimSpace(os.Getenv(testSendBarrierNEnv)); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			n = v
		}
	}
	timeout := 5 * time.Second
	if s := strings.TrimSpace(os.Getenv(testSendBarrierTimeoutMSEnv)); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			timeout = time.Duration(v) * time.Millisecond
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// Signal arrival.
	p := filepath.Join(dir, fmt.Sprintf("%d", os.Getpid()))
	if f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644); err == nil {
		_, _ = f.WriteString("ok\n")
		_ = f.Close()
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ents, err := os.ReadDir(dir)
		if err == nil && len(ents) >= n {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("send barrier timed out waiting for %d participants (%s)", n, dir)
}

func (c *SendCmd) info(msg string) {
	if c.Quiet {
		return
	}
	printInfo(msg)
}

func (c *SendCmd) warn(msg string) {
	if c.Quiet {
		return
	}
	printWarning(msg)
}

// errTaskWorking is the concurrency-guard error returned when a send is
// attempted while another supervisor already holds the task. The three claim
// checkpoints in prepareWorkspaceAndState share it so the interrupt-recovery
// hint stays in one place. (stage.go and pkg/task/ops use deliberately shorter
// variants — those are not folded in here.)
func errTaskWorking(taskName string) error {
	return fmt.Errorf("task %s is still working\n\nYou'll be notified when done, then you can send more context.\nTo correct a worker going the wrong direction:\n  subtask interrupt %s && subtask send %s \"...\"", taskName, taskName, taskName)
}

// readStdinIfAvailable reads from stdin only if data is piped (non-blocking).
func readStdinIfAvailable() string {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return ""
	}
	if (fi.Mode() & os.ModeCharDevice) != 0 {
		return ""
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func isLikelyInterruptedError(msg string) bool {
	msg = strings.ToLower(strings.TrimSpace(msg))
	return strings.Contains(msg, "signal: interrupt") || strings.Contains(msg, "interrupted")
}
