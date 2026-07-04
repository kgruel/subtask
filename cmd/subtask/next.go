package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/kgruel/subtask/pkg/routine"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/task/migrate"
	"github.com/kgruel/subtask/pkg/task/store"
)

// NextCmd implements 'subtask next'.
type NextCmd struct {
	Task string `arg:"" help:"Task name"`
}

func (c *NextCmd) Run() error {
	if _, err := preflightProject(); err != nil {
		return err
	}
	if err := migrate.EnsureSchema(c.Task); err != nil {
		return err
	}

	t, err := task.Load(c.Task)
	if err != nil {
		return err
	}
	v, err := store.BuildView(context.Background(), c.Task, nil, store.BuildViewOptions{})
	if err != nil {
		return err
	}
	tail, err := history.Tail(c.Task)
	if err != nil {
		return err
	}

	fmt.Print(formatNextCue(c.Task, t, v, tail))
	return nil
}

func formatNextCue(taskName string, t *task.Task, v *task.View, tail history.TailInfo) string {
	var b strings.Builder
	userStatus := task.UserStatusFor(v.Status, v.WorkerStatus)

	switch userStatus {
	case task.UserStatusDraft:
		if strings.TrimSpace(t.FollowUp) != "" {
			fmt.Fprintf(&b, "Task drafted as a follow-up to %s.\n\n", t.FollowUp)
			fmt.Fprintf(&b, "Next command:\n  subtask send %s \"Continue from the prior task and ...\"\n", taskName)
		} else {
			b.WriteString("Task drafted but never dispatched.\n\n")
			fmt.Fprintf(&b, "Next command:\n  subtask send %s \"...\"\n", taskName)
		}
	case task.UserStatusRunning:
		fmt.Fprintf(&b, "Worker running%s.\n\n", statusSuffix(v.StatusText, "working"))
		fmt.Fprintf(&b, "Next commands:\n  subtask wait %s\n  subtask interrupt %s\n\nOr wait for the worker reply.\n", taskName, taskName)
	case task.UserStatusReplied:
		unread, err := taskHasUnreadReply(taskName)
		if err == nil && unread {
			fmt.Fprintf(&b, "Worker replied%s. Unread.\n\n", statusSuffix(v.StatusText, "replied"))
			fmt.Fprintf(&b, "Next command:\n  subtask reply %s\n", taskName)
		} else {
			fmt.Fprintf(&b, "Worker replied%s. Already read.\n\n", statusSuffix(v.StatusText, "replied"))
			fmt.Fprintf(&b, "Next commands:\n  subtask send %s \"...\"\n", taskName)
			if strings.TrimSpace(t.Routine) != "" {
				fmt.Fprintf(&b, "  subtask stage %s <next-step>\n", taskName)
			}
		}
	case task.UserStatusError:
		if strings.TrimSpace(v.Error) == "interrupted" {
			fmt.Fprintf(&b, "Last run interrupted%s.\n\n", statusSuffix(v.StatusText, "interrupted"))
			fmt.Fprintf(&b, "Next command:\n  subtask send %s \"...\"\n", taskName)
		} else {
			errTail := strings.TrimSpace(v.Error)
			if errTail == "" {
				errTail = "unknown error"
			}
			fmt.Fprintf(&b, "Last run failed: %s\n\n", errTail)
			fmt.Fprintf(&b, "Next commands:\n  subtask log %s\n  subtask trace %s\n  subtask send %s \"...\"\n", taskName, taskName, taskName)
		}
	case task.UserStatusMerged:
		if sha := shortSHA(tail.LastMergedCommit); sha != "" {
			into := strings.TrimSpace(tail.BaseBranch)
			if into == "" {
				into = t.BaseBranch
			}
			fmt.Fprintf(&b, "Task merged into %s at %s. No next worker action.\n", into, sha)
		} else {
			b.WriteString("Task merged. No next worker action.\n")
		}
	case task.UserStatusClosed:
		b.WriteString("Task closed. No next worker action.\n")
	}

	if userStatus == task.UserStatusMerged || userStatus == task.UserStatusClosed {
		return b.String()
	}

	terminal, terminalStep := routineTerminalStep(t, v)
	if terminal {
		fmt.Fprintf(&b, "\nRoutine is at terminal step %q. Use `subtask merge %s -m \"...\"`, `subtask close %s`, or `subtask stage %s <other>` to proceed.\n",
			terminalStep, taskName, taskName, taskName)
		return b.String()
	}

	if guidance := formatRoutineStepGuidance(v, taskName); guidance != "" {
		b.WriteString("\n")
		b.WriteString(guidance)
	}
	return b.String()
}

func routineTerminalStep(t *task.Task, v *task.View) (bool, string) {
	if t == nil || v == nil || v.Routine == nil || strings.TrimSpace(v.Routine.CurrentStep) == "" || strings.TrimSpace(t.Routine) == "" {
		return false, ""
	}
	r, err := routine.LoadByName(t.Routine)
	if err != nil {
		return false, ""
	}
	step := r.GetStep(v.Routine.CurrentStep)
	if step == nil {
		return false, ""
	}
	return step.Kind == routine.KindTerminal, step.ID
}

func statusSuffix(statusText, prefix string) string {
	statusText = strings.TrimSpace(statusText)
	prefix = strings.TrimSpace(prefix)
	if statusText == prefix || statusText == "" {
		return ""
	}
	if strings.HasPrefix(statusText, prefix+" ") {
		return " " + strings.TrimSpace(strings.TrimPrefix(statusText, prefix))
	}
	return " (" + statusText + ")"
}
