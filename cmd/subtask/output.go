package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zippoxer/subtask/internal/homedir"
	"github.com/zippoxer/subtask/pkg/git"
	"github.com/zippoxer/subtask/pkg/render"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/workflow"
	"github.com/zippoxer/subtask/pkg/workspace"
)

var nowFunc = time.Now

// Progress output helpers - delegate to render package

func printSuccess(msg string) {
	render.Success(msg)
}

func printInfo(msg string) {
	render.Info(msg)
}

func printWarning(msg string) {
	render.Warning(msg)
}

// Section formatting helpers - delegate to render package

func printSection(title string) {
	render.Section(title)
}

func printSectionContent(content string) {
	render.SectionContent(content)
}

// TaskInfo holds display information for a task.
type TaskInfo struct {
	Name          string
	Title         string
	TaskStatus    task.TaskStatus
	WorkerStatus  task.WorkerStatus
	Stage         string // Current workflow stage
	Workspace     string
	BaseBranch    string // For git diff
	StartedAt     time.Time
	LastActive    time.Time
	ToolCalls     int
	FollowUp      string
	Progress      string // "X/Y" from PROGRESS.json
	LinesAdded    int    // Git diff stats
	LinesRemoved  int
	CommitsBehind int // Commits base branch has that the task ref doesn't
	LastRunMS     int
	LastError     string

	IntegratedReason string
}

// PrintTaskList prints a formatted table of tasks.
func PrintTaskList(tasks []TaskInfo, workspaces []workspace.Entry) {
	fmt.Print(RenderTaskList(tasks, workspaces))
}

// RenderTaskList renders a formatted table of tasks.
func RenderTaskList(tasks []TaskInfo, workspaces []workspace.Entry) string {
	// Track which workspaces are used
	usedWorkspaces := make(map[string]bool)

	// Build rows
	var rows []render.TaskRow
	for _, t := range tasks {
		status := userStatusTextWithIntegration(t.TaskStatus, t.WorkerStatus, t.StartedAt, t.LastRunMS, t.LastError, t.IntegratedReason)

		stage := t.Stage
		if stage == "" {
			stage = "-"
		}

		progress := t.Progress
		if progress == "" {
			progress = "-"
		}

		lastActivity := "-"
		if !t.LastActive.IsZero() {
			lastActivity = formatTimeAgo(t.LastActive)
		}

		if t.Workspace != "" {
			usedWorkspaces[t.Workspace] = true
		}

		title := t.Title
		if t.FollowUp != "" {
			title += fmt.Sprintf(" (← %s)", t.FollowUp)
		}

		rows = append(rows, render.TaskRow{
			Name:          t.Name,
			Status:        status,
			Stage:         stage,
			Progress:      progress,
			LinesAdded:    t.LinesAdded,
			LinesRemoved:  t.LinesRemoved,
			CommitsBehind: t.CommitsBehind,
			LastActive:    lastActivity,
			Title:         title,
		})
	}

	// Calculate available workspaces
	availableCount := 0
	for _, ws := range workspaces {
		if !usedWorkspaces[ws.Path] {
			availableCount++
		}
	}

	footer := ""
	if availableCount > 0 {
		footer = fmt.Sprintf("(%d workspace(s) available)", availableCount)
	}

	// Use render package
	table := &render.TaskListTable{
		Tasks:  rows,
		Footer: footer,
	}
	if render.Pretty {
		return table.RenderPretty()
	}
	return table.RenderPlain()
}

func userStatusText(ts task.TaskStatus, ws task.WorkerStatus, startedAt time.Time, lastRunMS int, lastError string) string {
	switch task.UserStatusFor(ts, ws) {
	case task.UserStatusMerged:
		return "✓ merged"
	case task.UserStatusClosed:
		return "closed"
	case task.UserStatusRunning:
		if !startedAt.IsZero() {
			return fmt.Sprintf("working (%s)", render.FormatDuration(nowFunc().Sub(startedAt)))
		}
		return "working"
	case task.UserStatusReplied:
		if lastRunMS > 0 {
			return fmt.Sprintf("replied (%s)", render.FormatDuration(time.Duration(lastRunMS)*time.Millisecond))
		}
		return "replied"
	case task.UserStatusError:
		if lastError == "interrupted" {
			if lastRunMS > 0 {
				return fmt.Sprintf("interrupted (%s)", render.FormatDuration(time.Duration(lastRunMS)*time.Millisecond))
			}
			return "interrupted"
		}
		if lastRunMS > 0 {
			return fmt.Sprintf("error (%s)", render.FormatDuration(time.Duration(lastRunMS)*time.Millisecond))
		}
		return "error"
	case task.UserStatusDraft:
		return "draft"
	default:
		return "—"
	}
}

func userStatusTextWithIntegration(ts task.TaskStatus, ws task.WorkerStatus, startedAt time.Time, lastRunMS int, lastError string, integratedReason string) string {
	// Don't show "merged" if worker is actively running
	if ws != task.WorkerStatusRunning && strings.TrimSpace(integratedReason) != "" && ts != task.TaskStatusMerged {
		return "✓ merged"
	}
	return userStatusText(ts, ws, startedAt, lastRunMS, lastError)
}

// formatTimeAgo formats a time as "Xm ago" or "Xs ago".
func formatTimeAgo(t time.Time) string {
	d := nowFunc().Sub(t)
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}

// abbreviatePath shortens a path for display.
func abbreviatePath(path string) string {
	home, _ := homedir.Dir()
	if strings.HasPrefix(path, home) {
		path = "~" + path[len(home):]
	}

	// Truncate middle if too long
	if len(path) > 50 {
		path = path[:20] + "..." + path[len(path)-27:]
	}
	return path
}

// TaskFileSnapshot captures modification times of task folder files.
type TaskFileSnapshot map[string]time.Time

// SnapshotTaskFiles captures the current state of task folder files.
// Excludes TASK.md and history.jsonl (always present).
func SnapshotTaskFiles(taskName string) TaskFileSnapshot {
	snapshot := make(TaskFileSnapshot)
	taskDir := task.Dir(taskName)

	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return snapshot
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// Skip files that are always present
		if e.Name() == "TASK.md" || e.Name() == "history.jsonl" {
			continue
		}
		path := filepath.Join(taskDir, e.Name())
		info, err := os.Stat(path)
		if err == nil {
			snapshot[e.Name()] = info.ModTime()
		}
	}
	return snapshot
}

// ChangedTaskFiles returns files that changed between before and after snapshots.
func ChangedTaskFiles(before, after TaskFileSnapshot) []string {
	var changed []string

	// Check for modified or new files
	for name, afterTime := range after {
		beforeTime, existed := before[name]
		if !existed || afterTime.After(beforeTime) {
			changed = append(changed, name)
		}
	}

	return changed
}

// PrintWorkerResult prints the worker reply with footer (no stage info).
func PrintWorkerResult(taskName string, reply string, toolCalls int, changedFiles []string) {
	PrintWorkerResultWithStage(taskName, reply, toolCalls, changedFiles, "")
}

// PrintWorkerResultWithStage prints the worker reply with stage info.
func PrintWorkerResultWithStage(taskName string, reply string, toolCalls int, changedFiles []string, stage string) {
	fmt.Printf("Worker replied (%d tool calls)", toolCalls)

	// Print reply
	if reply != "" {
		render.Section("Reply")
		render.SectionContent(reply)
	}

	// Print changed task files
	if len(changedFiles) > 0 {
		render.Section("Changed")
		render.SectionContent(strings.Join(changedFiles, ", "))
	}

	// Print workspace path
	state, _ := task.LoadState(taskName)
	if state != nil && state.Workspace != "" {
		render.Section("Workspace")
		render.SectionContent(state.Workspace)
	}

	// Conflicts (only when merge would fail).
	if state != nil && state.Workspace != "" {
		if t, err := task.Load(taskName); err == nil && t.BaseBranch != "" {
			// Local-first: compare against the local base branch only.
			target := t.BaseBranch
			if git.BranchExists(state.Workspace, target) {
				conflicts, err := git.MergeConflictFiles(state.Workspace, target, "HEAD")
				if err == nil && len(conflicts) > 0 {
					var b strings.Builder
					b.WriteString("Cannot merge cleanly. Conflicting files:\n")
					for _, f := range conflicts {
						b.WriteString("  - ")
						b.WriteString(f)
						b.WriteString("\n")
					}
					render.Section("Conflicts")
					render.SectionContent(strings.TrimRight(b.String(), "\n"))
				}
			}
		}
	}

	// Print workflow and stage info
	wf, err := workflow.LoadFromTask(taskName)
	if err == nil && wf != nil {
		// Show lead instructions
		if wf.Instructions.Lead != "" {
			render.Section("Workflow: " + wf.Name)
			render.SectionContent(wf.Instructions.Lead)
		}

		// Show stage info
		if stage != "" {
			render.Section("Stage: " + stage)
			fmt.Println(render.FormatStageProgression(wf.StageNames(), stage))
			fmt.Println()

			// Print stage guidance
			stageInfo := wf.GetStage(stage)
			if stageInfo != nil && stageInfo.Instructions != "" {
				lines := strings.Split(strings.TrimSpace(stageInfo.Instructions), "\n")
				for _, line := range lines {
					// Replace <task> placeholder with actual task name
					line = strings.ReplaceAll(line, "<task>", taskName)
					fmt.Println(line)
				}
			}
		}
	}

	// Print history path (syncable source of truth).
	render.Section("History")
	render.SectionContent(filepath.ToSlash(task.HistoryPath(taskName)) + "\n\nView:\n  subtask log " + taskName)
}
