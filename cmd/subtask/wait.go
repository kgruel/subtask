package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/kgruel/subtask/pkg/routine"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
)

const (
	// pendingAdvanceGuardPolls bounds the auto-advance liveness guard: a task
	// stuck in pending-auto-advance (replied + SupervisorPID==0) for this many
	// consecutive polls is concluded to have a dead supervisor. PID==0 is never
	// stale, so no reap can ever fire for this crash-in-window case; the streak
	// is the only signal wait has.
	//
	// Set to 15 (~30s at the 2s production interval), not a handful, because a
	// legitimate agent-swap auto-advance can hold the PID==0 window for several
	// seconds: a cross-adapter transition clears the session, rebuilds the
	// prompt (BuildPrompt), cold-starts the new adapter, and runs a pre-claim
	// CleanupStaleTasks that is O(tasks). A false complete-with-error on a
	// healthy routine is worse than slow detection of a genuinely dead
	// supervisor, and 30s matches the detach handshake budget rationale.
	pendingAdvanceGuardPolls = 15
	defaultWaitPollInterval  = 2 * time.Second
)

// WaitCmd blocks until a threshold of the named tasks reach a completion
// state (merged/closed, worker error, or a settled reply). Read-only: it
// polls state.json + history.jsonl and never mutates task, state, history,
// or the index. Works without a TTY — headless leads are the primary caller.
type WaitCmd struct {
	Tasks []string `arg:"" name:"task" help:"Task name(s) to wait on."`

	All bool `help:"Return when ALL named tasks complete (default)." xor:"count"`
	Any bool `help:"Return when the FIRST named task completes." xor:"count"`
	N   *int `short:"n" name:"n" help:"Return when N named tasks have completed." xor:"count"`

	Timeout time.Duration `help:"Give up after this duration (e.g. 90s, 10m). 0 = wait forever." default:"0"`
	JSON    bool          `short:"j" help:"Emit a JSON array of per-task results on completion."`

	// Test seam: fired once per poll cycle (nil in production). Lets
	// concurrency tests drive an exact number of polls without sleeps,
	// mirroring SendCmd's testHarness injection pattern.
	pollHook func()

	// Test seam: when non-nil, run() reads interrupts from this channel
	// instead of registering an OS signal handler, so a test can drive the
	// interrupt/exit-code path deterministically.
	testSigCh chan os.Signal

	// progressDirty tracks whether a transient TTY progress line is currently
	// drawn (and thus must be cleared before any other stdout/stderr write).
	progressDirty bool
}

// taskOutcome is one task's classification for a single poll. Complete/Errored
// drive the barrier and exit code; Pending/Draft feed the loop's guard streak
// and one-shot draft warning.
type taskOutcome struct {
	Name     string
	Label    string
	Complete bool
	Errored  bool
	Pending  bool
	Draft    bool
}

// waitJSONResult is the per-task shape emitted by --json. Status uses the
// task.UserStatus vocabulary for consistency with list/show; the error
// subtype detail rides in the plain-mode label only.
type waitJSONResult struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Complete bool   `json:"complete"`
	Error    bool   `json:"error"`
}

// Run is the thin wrapper: run() carries the whole loop and returns a semantic
// exit code so tests can drive it in-process without an os.Exit killing the
// test binary. Exit codes: 0 satisfied, 2 satisfied-with-error, 3 timeout,
// 128+signal on interrupt; a returned error is the exit-1 (kong) path.
func (c *WaitCmd) Run() error {
	code, err := c.run()
	if err != nil {
		return err
	}
	if code != 0 {
		os.Exit(code)
	}
	return nil
}

func (c *WaitCmd) run() (int, error) {
	if _, err := preflightProject(); err != nil {
		return 0, err
	}
	if len(c.Tasks) == 0 {
		return 0, fmt.Errorf("wait requires at least one task\n\nTip: subtask wait <task>...")
	}

	names, dupes := dedupeStrings(c.Tasks)
	if len(dupes) > 0 {
		c.clearProgress()
		fmt.Fprintf(os.Stderr, "note: ignoring duplicate task name(s): %s\n", strings.Join(dupes, ", "))
	}

	for _, name := range names {
		_, err := task.Load(name)
		if err == nil {
			continue
		}
		// task.Load failed. Distinguish a genuinely missing task from one whose
		// TASK.md exists but can't be parsed (e.g. malformed frontmatter): the
		// former gets the discoverability Tip; the latter surfaces the real
		// parse error pointed at the task folder, rather than being masked as
		// "not found".
		if _, statErr := os.Stat(task.Path(name)); os.IsNotExist(statErr) {
			return 0, fmt.Errorf("task %q not found\n\nTip: run `subtask list` to see task names", name)
		}
		return 0, fmt.Errorf("%w\n\nTip: check %s", err, task.Dir(name))
	}

	k, err := c.resolveThreshold(len(names))
	if err != nil {
		return 0, err
	}

	// Preserve WHICH signal fired so the exit code can be 128+N per-signal.
	// signal.NotifyContext collapses SIGINT and SIGTERM into one ctx.Done()
	// and cannot distinguish them, so wait registers the channel directly.
	sigCh := c.testSigCh
	if sigCh == nil {
		sigCh = make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(sigCh)
	}

	// The only cross-poll memory. Never persisted (zero new persistent state).
	pendingStreak := make(map[string]int) // consecutive pending-auto-advance polls
	warnedDraft := make(map[string]bool)  // draft warning emitted once
	printed := make(map[string]string)    // last streamed label per task (plain mode)

	ticker := time.NewTicker(waitPollInterval())
	defer ticker.Stop()

	var timeoutCh <-chan time.Time
	if c.Timeout > 0 {
		timer := time.NewTimer(c.Timeout)
		defer timer.Stop()
		timeoutCh = timer.C
	}

	for {
		if c.pollHook != nil {
			c.pollHook()
		}

		// Classify every named task afresh each poll — completion is a
		// property recomputed each cycle, never latched. A task that was
		// complete last poll but was revived (merged→open) this poll simply
		// stops counting; the only thing carried across polls is the guard
		// streak, which resets when the pending condition breaks.
		outcomes := make([]taskOutcome, len(names))
		completeCount := 0
		for i, name := range names {
			o := classify(name)

			if o.Draft && !warnedDraft[name] {
				c.clearProgress()
				fmt.Fprintf(os.Stderr, "note: task %q is a draft (never dispatched); waiting — it completes once sent\n", name)
				warnedDraft[name] = true
			}

			if o.Pending {
				pendingStreak[name]++
				if pendingStreak[name] >= pendingAdvanceGuardPolls {
					// Supervisor cleared SupervisorPID (send.go clear) then died
					// before the recursive re-claim. PID==0 is never stale, so no
					// reap can ever fire — the streak is the only way to conclude
					// the supervisor is gone.
					o.Label = "error (supervisor died mid-advance)"
					o.Complete = true
					o.Errored = true
					o.Pending = false
				}
			} else {
				pendingStreak[name] = 0
			}

			if !c.JSON && o.Complete && printed[name] != o.Label {
				c.clearProgress()
				streamWaitLine(o)
				printed[name] = o.Label
			}

			outcomes[i] = o
			if o.Complete {
				completeCount++
			}
		}

		c.renderProgress(outcomes, completeCount)

		if completeCount >= k {
			return c.finalizeWait(outcomes, printed, 0), nil
		}

		select {
		case sig := <-sigCh:
			c.clearProgress()
			// Honor the --json contract even on interrupt: emit the same JSON
			// array shape as a normal finalize to stdout; plain mode keeps the
			// human-readable stderr snapshot.
			if c.JSON {
				printWaitJSON(outcomes)
			} else {
				printOutcomesStderr(outcomes)
			}
			return signalExitCode(sig), nil
		case <-timeoutCh:
			return c.finalizeWait(outcomes, printed, 3), nil
		case <-ticker.C:
		}
	}
}

// classify reads state.json + the history tail (both lock-free, best-effort)
// and returns the task's outcome. Rows are evaluated in order; first match
// wins. Every st.-dereferencing clause is guarded by st != nil because
// LoadState returns (nil,nil) for a never-dispatched draft.
func classify(name string) taskOutcome {
	o := taskOutcome{Name: name}
	st, _ := task.LoadState(name)
	tail, _ := history.Tail(name)

	switch {
	case tail.TaskStatus == task.TaskStatusMerged:
		o.Label, o.Complete = "merged", true
	case tail.TaskStatus == task.TaskStatusClosed:
		o.Label, o.Complete = "closed", true
	case st != nil && st.SupervisorPID != 0 && !st.IsStale():
		o.Label = "working"
	case st != nil && st.SupervisorPID != 0 && st.IsStale():
		// Dead process behind a live PID: replicate the staleness reap's
		// conclusion live and read-only (IsStale probes processAlive now).
		o.Label, o.Complete, o.Errored = "error (supervisor died)", true, true
	case (st != nil && st.LastError != "") || tail.LastRunOutcome == "error":
		o.Label, o.Complete, o.Errored = "error (worker failed)", true, true
	case tail.LastRunOutcome == "replied" && pendingAutoAdvance(name, tail):
		// The replied step would auto-dispatch another round; not done yet.
		o.Label, o.Pending = "working", true
	case tail.LastRunOutcome == "replied":
		o.Label, o.Complete = "replied", true
	case tail.LastRunOutcome == "" && tail.RunningRunID != "":
		// Started, never finished, PID already 0: only reachable if state.json
		// was externally zeroed without a worker.finished. Defensive.
		o.Label, o.Complete, o.Errored = "error (run did not finish)", true, true
	default:
		o.Label, o.Draft = "draft", true
	}
	return o
}

// pendingAutoAdvance reports whether a routine auto-dispatch is imminent for
// the reply just observed. Evaluated against the stage stamped on the most
// recent worker.finished (tail.LastRunStage), not tail.Stage — auto-advance
// moves tail.Stage to the next step after appending worker.finished, so
// tail.Stage would already point past the step the reply belongs to.
func pendingAutoAdvance(name string, tail history.TailInfo) bool {
	t, _ := task.Load(name)
	if t == nil || t.Routine == "" {
		return false
	}
	r, err := routine.LoadByName(t.Routine)
	if err != nil || r == nil {
		return false // corrupt/undefined routine → not pending; the reply stands
	}
	stage := tail.LastRunStage
	if stage == "" {
		stage = tail.Stage // legacy worker.finished lacked the stamp
	}
	if stage == "" {
		stage = r.EntryStep()
	}
	would, err := routine.WouldAutoDispatch(name, r, stage)
	if err != nil {
		return false // malformed produces-artifact → let the run stand as replied
	}
	return would
}

// resolveThreshold collapses --all/--any/-n into a single "return when K
// complete" integer. N is *int so -n 0 (provided, value 0) reaches the range
// check instead of aliasing to unset/--all.
func (c *WaitCmd) resolveThreshold(nTasks int) (int, error) {
	switch {
	case c.Any:
		return 1, nil
	case c.N != nil:
		if *c.N < 1 || *c.N > nTasks {
			return 0, fmt.Errorf("-n %d is out of range: %d task(s) named\n\nTip: use 1..%d", *c.N, nTasks, nTasks)
		}
		return *c.N, nil
	default:
		return nTasks, nil
	}
}

// finalizeWait prints the terminal output and returns the exit code. code is 0
// for a satisfied barrier (upgraded to 2 if any completing task errored) or 3
// for a timeout (returned as-is — a timeout is distinct from a satisfied
// barrier regardless of whether some task errored).
func (c *WaitCmd) finalizeWait(outcomes []taskOutcome, printed map[string]string, code int) int {
	c.clearProgress()
	if c.JSON {
		printWaitJSON(outcomes)
	} else {
		flushWaitPlain(outcomes, printed)
	}
	if code != 0 {
		return code
	}
	for _, o := range outcomes {
		if o.Complete && o.Errored {
			return 2
		}
	}
	return 0
}

// renderProgress redraws a single transient status line on stderr for TTY
// callers. Kept off stdout so scripted output stays clean; skipped without a
// TTY and in JSON mode. Only outcomes actually labeled "working" count as
// working; drafts and other non-complete states are reported as pending so the
// count reflects real worker activity. A trailing ESC[K clears any remnant of a
// longer previous line.
func (c *WaitCmd) renderProgress(outcomes []taskOutcome, completeCount int) {
	if c.JSON || !isatty.IsTerminal(os.Stderr.Fd()) {
		return
	}
	working, pending := 0, 0
	for _, o := range outcomes {
		switch {
		case o.Complete:
			// counted via completeCount
		case o.Label == "working":
			working++
		default:
			pending++
		}
	}
	fmt.Fprintf(os.Stderr, "\rwaiting: %d/%d complete (%d working, %d pending)\x1b[K", completeCount, len(outcomes), working, pending)
	c.progressDirty = true
}

// clearProgress wipes the transient progress line and returns the cursor to
// column 0 before any other stdout/stderr write, mirroring watch.go's redraw
// discipline. It is a no-op unless a progress line is currently drawn (which
// only happens in TTY, non-JSON mode), so callers can invoke it unconditionally.
func (c *WaitCmd) clearProgress() {
	if !c.progressDirty {
		return
	}
	fmt.Fprint(os.Stderr, "\r\x1b[K")
	c.progressDirty = false
}

// flushWaitPlain streams any task whose current label was not already emitted
// so the lead sees every named task's final state on finalize.
func flushWaitPlain(outcomes []taskOutcome, printed map[string]string) {
	for _, o := range outcomes {
		if printed[o.Name] != o.Label {
			streamWaitLine(o)
			printed[o.Name] = o.Label
		}
	}
}

func printWaitJSON(outcomes []taskOutcome) {
	results := make([]waitJSONResult, len(outcomes))
	for i, o := range outcomes {
		results[i] = waitJSONResult{
			Name:     o.Name,
			Status:   waitStatusString(o),
			Complete: o.Complete,
			Error:    o.Errored,
		}
	}
	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return
	}
	fmt.Print(string(out) + "\n")
}

func printOutcomesStderr(outcomes []taskOutcome) {
	for _, o := range outcomes {
		fmt.Fprintf(os.Stderr, "%s\t%s\n", o.Name, o.Label)
	}
}

func streamWaitLine(o taskOutcome) {
	fmt.Printf("%s\t%s\n", o.Name, o.Label)
}

// waitStatusString maps an outcome to the task.UserStatus vocabulary. The
// error subtype (supervisor died vs worker failed) collapses to "error".
func waitStatusString(o taskOutcome) string {
	switch {
	case o.Errored:
		return string(task.UserStatusError)
	case o.Label == "merged":
		return string(task.UserStatusMerged)
	case o.Label == "closed":
		return string(task.UserStatusClosed)
	case o.Draft:
		return string(task.UserStatusDraft)
	case o.Complete:
		return string(task.UserStatusReplied)
	default:
		return string(task.UserStatusRunning)
	}
}

func signalExitCode(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return 130
}

// waitPollInterval is the poll cadence, overridable only via the unexported
// test env knob (consistent with SUBTASK_TEST_* naming).
func waitPollInterval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("SUBTASK_TEST_WAIT_INTERVAL_MS")); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return defaultWaitPollInterval
}

// dedupeStrings returns the input with duplicates removed (first-seen order
// preserved) and the list of names that were collapsed (each once).
func dedupeStrings(in []string) (unique, dupes []string) {
	seen := make(map[string]bool, len(in))
	dupeSeen := make(map[string]bool)
	for _, s := range in {
		if seen[s] {
			if !dupeSeen[s] {
				dupeSeen[s] = true
				dupes = append(dupes, s)
			}
			continue
		}
		seen[s] = true
		unique = append(unique, s)
	}
	return unique, dupes
}
