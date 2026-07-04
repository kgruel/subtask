package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/kgruel/subtask/internal/detach"
	"github.com/kgruel/subtask/pkg/render"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
)

// detachStart is the spawn seam (stubbable in tests). Production starts the
// re-exec'd child; the child then runs the identical foreground Run() body.
var detachStart = func(cmd *exec.Cmd) error { return cmd.Start() }

const (
	defaultDetachPollInterval    = 50 * time.Millisecond
	defaultDetachHandshakeBudget = 30 * time.Second
)

// resolvePrompt returns the run's prompt. For a detached child (DetachChild
// set) it reads and unlinks the parent-staged prompt file and marks the run as
// detached; otherwise it is the foreground arg-or-stdin path.
func (c *SendCmd) resolvePrompt() (string, error) {
	if c.DetachChild != "" {
		b, rerr := os.ReadFile(c.DetachChild)
		if rerr != nil {
			return "", fmt.Errorf("detached supervisor could not read its prompt file %s: %w\n\nThe parent may have exited before the child read it; re-run `subtask send %s`.", c.DetachChild, rerr, c.Task)
		}
		_ = os.Remove(c.DetachChild) // one-shot: read once, then gone (child owns its lifecycle)
		c.detached = true
		p := strings.TrimSpace(string(b))
		if p == "" {
			return "", fmt.Errorf("prompt is required")
		}
		return p, nil
	}
	prompt := strings.TrimSpace(c.Prompt)
	if prompt == "" {
		prompt = readStdinIfAvailable()
	}
	if prompt == "" {
		return "", fmt.Errorf("prompt is required\n\nProvide a prompt as argument or via stdin (heredoc/pipe)")
	}
	return prompt, nil
}

// runDetachParent is the entire parent path for `send --detach`: it re-execs
// this binary as a detached child, then blocks only until the child has
// claimed the task (or failed to). It never takes the task lock or writes
// state — the child does all claiming.
func (c *SendCmd) runDetachParent(prompt string) error {
	// Advisory pre-check — fail fast WITHOUT spawning if the task is already
	// held by a live supervisor. The child re-checks authoritatively under the
	// flock; this is UX only and TOCTOU-safe by design.
	if st, _ := task.LoadState(c.Task); st != nil && st.SupervisorPID != 0 && !st.IsStale() {
		return errTaskWorking(c.Task)
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not locate the subtask binary to launch a detached supervisor: %w\n\nReinstall (`subtask update`), or re-run without --detach to dispatch in the foreground.", err)
	}

	dir := task.DetachDir(c.Task) // task internal dir, never the repo (portability)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("could not create the supervisor state dir %s: %w", dir, err)
	}

	// Prompt via temp file: only a PATH crosses the fork boundary, never an
	// open handle (sidesteps the Windows delete-while-open sharing violation).
	pf, err := os.CreateTemp(dir, task.DetachPromptPattern)
	if err != nil {
		return fmt.Errorf("could not stage the prompt for a detached supervisor: %w", err)
	}
	promptPath := pf.Name()
	if _, err := pf.WriteString(prompt); err != nil {
		pf.Close()
		os.Remove(promptPath)
		return err
	}
	if err := pf.Close(); err != nil {
		os.Remove(promptPath)
		return err
	}

	// Supervisor log: child stdout+stderr land here, never the terminal.
	logPath := task.SupervisorLogPath(c.Task)
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		os.Remove(promptPath)
		return fmt.Errorf("could not open the supervisor log %s: %w", logPath, err)
	}

	cmd := exec.Command(self, c.buildChildArgs(promptPath)...)
	cmd.Stdin = nil      // Go wires nil stdin to os.DevNull; no handle to leak
	cmd.Stdout = logf    // child renders into the log, not the terminal
	cmd.Stderr = logf    // (cmd.Dir defaults to the parent cwd → same GitRoot/InternalDir)
	cmd.Env = childEnv() // inherit env, but force SUBTASK_OUTPUT=plain
	detach.Detached(cmd) // Setsid (unix) / DETACHED_PROCESS|CREATE_NEW_PROCESS_GROUP (windows)

	// Capture the dispatch instant BEFORE the child can start so that a
	// worker.finished the child stamps (even one from a fast claim→run→fail
	// inside a single poll gap) is unambiguously "after dispatch" (see
	// detachChildRan / pollClaim's post-claim-failure branch).
	dispatchedAt := time.Now()
	if err := detachStart(cmd); err != nil {
		logf.Close()
		os.Remove(promptPath)
		return fmt.Errorf("could not launch a detached supervisor for %q: %w\n\nRetry, or run without --detach.", c.Task, err)
	}
	// Parent releases its own log handle immediately; the child holds its own
	// dup (same handle-lifetime discipline as the prompt file, Windows-safe).
	logf.Close()

	return c.awaitClaim(cmd, promptPath, logPath, dispatchedAt)
}

// buildChildArgs constructs the child argv from struct fields (never os.Args),
// forwarding only what a send legitimately carries, adding --detach-child, and
// deliberately dropping --detach and the positional prompt so the child can
// never re-detach. --follow-up and the routine are task-intrinsic and loaded
// by the child itself, so they are not forwarded.
func (c *SendCmd) buildChildArgs(promptPath string) []string {
	args := []string{"send", c.Task, "--detach-child", promptPath}
	if c.Adapter != "" {
		args = append(args, "--adapter", c.Adapter)
	}
	if c.Provider != "" {
		args = append(args, "--provider", c.Provider)
	}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	if c.Reasoning != "" {
		args = append(args, "--reasoning", c.Reasoning)
	}
	if c.Agent != "" {
		args = append(args, "--agent", c.Agent)
	}
	if c.Quiet {
		args = append(args, "--quiet")
	}
	if c.PinnedBase {
		args = append(args, "--pinned-base")
	}
	return args
}

// awaitClaim wires the child process into the lock-free claim handshake. The
// parent only observes state.json; "dispatched" ≡ "claimed" so a `subtask
// wait` fired immediately after `send --detach` can never observe idle.
func (c *SendCmd) awaitClaim(cmd *exec.Cmd, promptPath, logPath string, dispatchedAt time.Time) error {
	// Guard a broken spawn seam: a detachStart that returns nil without actually
	// starting the process leaves cmd.Process nil, which would panic on .Pid.
	if cmd.Process == nil {
		os.Remove(promptPath)
		return fmt.Errorf("internal: detached supervisor for %q was not started (no child process)", c.Task)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	return c.pollClaim(cmd.Process.Pid, exited, promptPath, logPath, dispatchedAt)
}

// pollClaim is the handshake loop, split out so tests drive it with a synthetic
// exit channel and no real process. Ordering is load-bearing: probe the claim
// before the exit channel, and treat a clean exit (werr==nil) as success — a
// fast child can claim→run→clear→exit 0 inside one poll gap. dispatchedAt is
// the instant the child was spawned; it lets a dirty exit tell a post-claim run
// failure (a worker.finished stamped after dispatch) apart from a pre-claim
// death.
func (c *SendCmd) pollClaim(childPID int, exited <-chan error, promptPath, logPath string, dispatchedAt time.Time) error {
	poll := detachPollInterval()
	deadline := time.Now().Add(detachHandshakeTimeout())

	for {
		// (a) Claim observed first — the most direct signal (child wrote
		// SupervisorPID under the flock).
		if st, _ := task.LoadState(c.Task); st != nil && st.SupervisorPID == childPID {
			return c.printDispatched(childPID)
		}
		// (b) Child already exited — checked AFTER the claim probe.
		select {
		case werr := <-exited:
			if werr == nil {
				return c.printDispatched(childPID)
			}
			st, _ := task.LoadState(c.Task)
			// Our own claim raced the exit and is still live → the run is
			// underway; dispatch succeeded.
			if st != nil && st.SupervisorPID == childPID {
				return c.printDispatched(childPID)
			}
			// Another supervisor holds a live claim (a double-dispatch we lost,
			// or a concurrent send that grabbed the task first). Nothing of ours
			// ran; the child's own errTaskWorking is in the log tail. The child
			// already read+unlinked its prompt, so os.Remove is a no-op guard.
			if st != nil && st.SupervisorPID != 0 && !st.IsStale() {
				os.Remove(promptPath)
				return fmt.Errorf("detached supervisor for %q exited before claiming the task (%v)\n\n%s\n\nFull log: %s",
					c.Task, werr, tailFile(logPath, 8192), logPath)
			}
			// No live claim. Distinguish a genuine pre-claim death from a fast
			// child that claimed→ran→failed→cleared its own claim inside one poll
			// gap: a worker.finished stamped after dispatch proves the run
			// happened, so the wording must report a post-claim run failure —
			// never imply nothing ran.
			if c.detachChildRan(dispatchedAt) {
				return fmt.Errorf("detached supervisor for %q claimed the task but its run failed (%v)\n\n%s\n\nThe failure is recorded in the task history — inspect it with:\n  subtask show %s\n  subtask log %s\n\nFull log: %s",
					c.Task, werr, tailFile(logPath, 8192), c.Task, c.Task, logPath)
			}
			os.Remove(promptPath) // child never read it
			return fmt.Errorf("detached supervisor for %q exited before claiming the task (%v)\n\n%s\n\nFull log: %s",
				c.Task, werr, tailFile(logPath, 8192), logPath)
		default:
		}
		if time.Now().After(deadline) {
			// No-kill timeout policy. The child's pre-claim path runs
			// preflightProject (git), migrate.EnsureSchema, and
			// task.CleanupStaleTasks (O(project tasks) with a liveness syscall
			// each) BEFORE its first claim, so under the N-way fan-out this
			// feature targets a live child can miss a fixed window without being
			// wedged. A pre-claim child holds no claim and no workspace, so
			// leaving it running corrupts nothing; killing it would misreport a
			// slow-but-live dispatch as failed. Report a non-fatal "not
			// confirmed yet" advisory (nonzero exit) and leave the child alone —
			// the prompt file is NOT removed (the child may still read it).
			return fmt.Errorf("detached supervisor for %q has not claimed the task within %s; it is still running (pid %d)\n\nThe supervisor may just be slow (large project / concurrent fan-out); it will claim and run on its own, so this dispatch may still complete — do NOT blindly re-send the same prompt (a retry would duplicate the run). Check first:\n  subtask show %s\n  subtask wait %s\nTo abort it: subtask interrupt %s (once it has claimed) or kill %d\n\nFull log: %s",
				c.Task, detachHandshakeTimeout(), childPID, c.Task, c.Task, c.Task, childPID, logPath)
		}
		time.Sleep(poll)
	}
}

// detachChildRan reports whether the detached child actually claimed and ran —
// i.e. a worker.finished event was stamped after the parent dispatched it. Used
// on a dirty child exit with no live claim to tell a post-claim run failure
// (the child claimed→ran→failed→cleared its own claim within one poll gap) apart
// from a pre-claim death, so the error wording never implies nothing ran. The
// dispatchedAt lower bound keeps a stale worker.finished from a PRIOR run from
// being mistaken for this dispatch's.
func (c *SendCmd) detachChildRan(dispatchedAt time.Time) bool {
	evs, err := history.Read(c.Task, history.ReadOptions{Since: dispatchedAt, EventsOnly: true})
	if err != nil {
		return false
	}
	for _, ev := range evs {
		if ev.Type == history.EventTypeWorkerFinished {
			return true
		}
	}
	return false
}

// printDispatched writes the dispatch-confirmation line (suppressed under -q).
// Routed through the render package per repo output convention.
func (c *SendCmd) printDispatched(childPID int) error {
	if c.Quiet {
		return nil
	}
	render.Info(fmt.Sprintf("Dispatched %s (detached supervisor, pid %d).\nRetrieve the reply once it finishes:\n  subtask wait %s && subtask reply %s",
		c.Task, childPID, c.Task, c.Task))
	return nil
}

// childEnv copies the parent environment for the detached child but forces
// SUBTASK_OUTPUT=plain so the child's log is never polluted with ANSI escapes
// (the invoking shell may have SUBTASK_OUTPUT=pretty). Everything else —
// SUBTASK_TEST_*, HOME, PATH, adapter-CLI resolution env — is preserved for
// parity and test determinism.
func childEnv() []string {
	base := os.Environ()
	out := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if strings.HasPrefix(kv, "SUBTASK_OUTPUT=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "SUBTASK_OUTPUT=plain")
}

// tailFile returns up to the last maxBytes of a file, trimmed. Best-effort:
// any error yields "".
func tailFile(path string, maxBytes int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	if size := info.Size(); size > maxBytes {
		if _, err := f.Seek(size-maxBytes, io.SeekStart); err != nil {
			return ""
		}
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// detachPollInterval / detachHandshakeTimeout read SUBTASK_TEST_DETACH_POLL_MS
// / SUBTASK_TEST_DETACH_TIMEOUT_MS (consistent with the SUBTASK_TEST_* test-knob
// convention) so tests run without real sleeps.
func detachPollInterval() time.Duration {
	return envDurationMS("SUBTASK_TEST_DETACH_POLL_MS", defaultDetachPollInterval)
}

func detachHandshakeTimeout() time.Duration {
	return envDurationMS("SUBTASK_TEST_DETACH_TIMEOUT_MS", defaultDetachHandshakeBudget)
}

func envDurationMS(name string, def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return def
}
