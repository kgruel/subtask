package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kgruel/subtask/internal/homedir"
	"github.com/kgruel/subtask/pkg/git"
	"github.com/kgruel/subtask/pkg/render"
	"github.com/kgruel/subtask/pkg/routine"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/store"
	"github.com/kgruel/subtask/pkg/workspace"
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
	ChangesStatus string // "", "applied", "missing"
	LastRunMS     int
	LastError     string
	HasReview     bool // True if task has at least one persisted review file
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
		status := userStatusText(t.TaskStatus, t.WorkerStatus, t.StartedAt, t.LastRunMS, t.LastError)

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
			ChangesStatus: t.ChangesStatus,
			LastActive:    lastActivity,
			Title:         title,
			HasReview:     t.HasReview,
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
	PrintWorkerResultWithStage(taskName, reply, toolCalls, changedFiles, "", "")
}

// PrintWorkerResultWithStage prints the worker reply with stage info.
// When workerLabel is non-empty it is used directly; otherwise the label is
// resolved from the task snapshot (fallback for callers without a live cfg).
func PrintWorkerResultWithStage(taskName string, reply string, toolCalls int, changedFiles []string, stage, workerLabel string) {
	v, _ := store.BuildView(context.Background(), taskName, nil, store.BuildViewOptions{Stage: stage})

	label := workerLabel
	if label == "" {
		if v != nil {
			label = task.WorkerLabel(v.Agent.Name, "", v.Agent.Adapter, v.Agent.Model)
		} else {
			label = resolveWorkerLabelForTask(taskName, stage)
		}
	}
	fmt.Printf("%s replied (%d tool calls)", label, toolCalls)

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

	// Print workspace: diff stats (when non-empty) + path.
	if v != nil && v.Workspace != "" {
		var statsLine string
		if v.BaseBranch != "" {
			if base, err := git.ResolveDiffBase(v.Workspace, "HEAD", v.BaseBranch); err == nil {
				if stats, err := git.DiffNumstat(v.Workspace, base); err == nil && len(stats) > 0 {
					var added, removed int
					for _, s := range stats {
						added += s.Added
						removed += s.Removed
					}
					fileWord := "files"
					if len(stats) == 1 {
						fileWord = "file"
					}
					statsLine = fmt.Sprintf("+%d -%d across %d %s (see subtask diff %s)", added, removed, len(stats), fileWord, taskName)
				}
			}
		}
		render.Section("Workspace")
		if statsLine != "" {
			render.SectionContent(statsLine + "\n" + v.Workspace)
		} else {
			render.SectionContent(v.Workspace)
		}
	}

	// Conflicts (only when merge would fail).
	if v != nil && v.Workspace != "" {
		if v.BaseBranch != "" {
			// Local-first: compare against the local base branch only.
			target := v.BaseBranch
			if git.BranchExists(v.Workspace, target) {
				conflicts, err := git.MergeConflictFiles(v.Workspace, target, "HEAD")
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

	// Print routine and step info (routine-driven tasks only).
	if v != nil && v.Routine != nil {
		render.Section("Routine: " + v.Routine.Name + routine.SourceSuffix(v.Routine.Source))
		fmt.Println(render.FormatRoutineDiagram(routineDiagramSteps(v.Routine.Steps), v.Routine.CurrentStep))
		if v.Routine.StepAgent != "" {
			fmt.Printf("Agent: %s\n", v.Routine.StepAgent)
		}
		fmt.Println()

		if v.Routine.Instructions != "" {
			lines := strings.Split(strings.TrimSpace(v.Routine.Instructions), "\n")
			for _, line := range lines {
				line = strings.ReplaceAll(line, "<task>", taskName)
				fmt.Println(line)
			}
		}
	}

	// Print history path (syncable source of truth).
	render.Section("History")
	render.SectionContent(filepath.ToSlash(task.HistoryPath(taskName)) + "\n\nView:\n  subtask log " + taskName)
}

// resolveWorkerLabelForTask returns a WorkerLabel for the given task and stage.
// Falls back to "Worker" if the task cannot be loaded.
func resolveWorkerLabelForTask(taskName, stage string) string {
	t, err := task.Load(taskName)
	if err != nil {
		return "Worker"
	}
	cfg, _ := workspace.LoadConfig()
	var stepAgent string
	if stage != "" && t.Routine != "" {
		if r, err := routine.LoadByName(t.Routine); err == nil {
			if step := r.GetStep(stage); step != nil {
				stepAgent = step.Agent
			}
		}
	}
	return task.WorkerLabel(stepAgent, t.Agent, workspace.ResolveAdapter(cfg, t, ""), workspace.ResolveModel(cfg, t, ""))
}
