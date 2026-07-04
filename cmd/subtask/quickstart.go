package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/store"
)

// QuickstartCmd implements 'subtask quickstart'.
type QuickstartCmd struct {
	First bool `help:"Show the first-task flow even when this project already has tasks"`
}

func (c *QuickstartCmd) Run() error {
	if _, err := preflightProject(); err != nil {
		return fmt.Errorf("not in a subtask-initialized project. cd to a git project with Subtask installed, or run subtask install first")
	}

	// Use the shared store.List so quickstart's open-count matches `subtask list`
	// exactly (store does write-on-read ancestor-merge detection that the old
	// gather layer lacked).
	data, err := store.New().List(context.Background(), store.ListOptions{All: true})
	if err != nil {
		return err
	}
	if c.First || len(data.Tasks) == 0 {
		fmt.Print(firstTaskQuickstart())
		return nil
	}

	openCount := 0
	unreadCount := 0
	for _, it := range data.Tasks {
		if it.TaskStatus != task.TaskStatusOpen {
			continue
		}
		openCount++
		unread, err := taskHasUnreadReply(it.Name)
		if err == nil && unread {
			unreadCount++
		}
	}

	fmt.Print(existingTasksQuickstart(openCount, len(data.Tasks), unreadCount))
	return nil
}

func firstTaskQuickstart() string {
	return strings.TrimLeft(`
Welcome to subtask. Here's your first task in four steps:

1. Draft a task:
     subtask draft fix/example --routine default --base-branch <branch> \
       --title "Short description" "Longer description of what to do"

2. Dispatch the worker:
     subtask send fix/example "Go ahead."

3. Read the worker's reply when it's done:
     subtask reply fix/example
     # Or for full state: subtask show fix/example

4. Inspect the diff and merge (or close without merging):
     subtask diff fix/example
     subtask merge fix/example -m "Commit message"
     # or: subtask close fix/example

Need richer flows? See ~/.claude/skills/subtask/SKILL.md (routines, agents, review).
`, "\n")
}

func existingTasksQuickstart(openCount, totalCount, unreadCount int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project has %d open task(s) (%d total, including merged/closed), %d with unread replies.\n\n",
		openCount, totalCount, unreadCount)
	b.WriteString("What you probably want:\n")
	b.WriteString("  subtask list              - see all open tasks\n")
	b.WriteString("  subtask list -a           - include merged/closed\n")
	b.WriteString("  subtask unread            - open tasks waiting on your read\n")
	b.WriteString("  subtask wait <task>...    - block until named tasks finish\n")
	b.WriteString("  subtask next <task>       - state-aware next-step cue for a specific task\n\n")
	b.WriteString("First-task guide: subtask quickstart --first\n")
	return b.String()
}
