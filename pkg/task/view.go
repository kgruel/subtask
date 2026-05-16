package task

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// View is a unified data model for rendering a task's current state.
// Built by store.BuildView and consumed by various UI surfaces.
type View struct {
	Name       string
	Title      string
	Branch     string
	BaseBranch string

	Status       TaskStatus
	WorkerStatus WorkerStatus
	IsTerminal   bool
	StatusText   string // pre-resolved via UserStatusText
	Error        string

	Agent AgentView
	Routine *RoutineView // nil when not routine-driven

	Changes       ChangesView
	Commits       CommitsView
	Conflicts     []string
	Artifacts     []ArtifactInfo
	ProgressSteps []ProgressStep
	Reviews       *ReviewSummaryView

	// Verbose-only fields
	BaseCommit string
	Workspace  string
	TaskDir    string
	TaskFiles  []string
}

// AgentView represents the resolved identity of the agent working on the task.
type AgentView struct {
	Name      string // raw name; empty when no named agent
	Adapter   string
	Model     string
	Reasoning string
}

// RoutineView represents the routine-driven state of a task.
type RoutineView struct {
	Name        string
	Source      string
	CurrentStep string     // routine-relative step id; "" when terminal
	Steps       []StepView // for diagram rendering
	StepAgent   string     // resolved agent for current step (empty when no per-step override)
}

// StepView carries diagram data for one routine step.
type StepView struct {
	ID       string
	Kind     string // "regular", "gate", "terminal"
	Agent    string
	Options  []OptionView
	Branches []BranchView
}

// OptionView is a named edge from a gate step.
type OptionView struct {
	Name string
	Next string
}

// BranchView is a conditional edge from a regular step.
type BranchView struct {
	Field string
	To    string
}

// ChangesView represents lines added/removed and apply status.
type ChangesView struct {
	Added   int
	Removed int
	Status  string
	Err     error
}

// CommitsView represents the task's commit count.
type CommitsView struct {
	Count int
	Err   error
	Show  bool // true if status is "open"
}

// ReviewSummaryView holds aggregated review metadata.
type ReviewSummaryView struct {
	Count       int
	LastTS      time.Time
	LastKind    string
	LastAdapter string
}

// FormatDuration formats a duration for display (e.g., "5s", "2m", "1h10m").
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, minutes)
}

// UserStatusText returns the human-readable status string for a task.
func UserStatusText(ts TaskStatus, ws WorkerStatus, startedAt time.Time, lastRunMS int, lastError string, now time.Time) string {
	switch UserStatusFor(ts, ws) {
	case UserStatusMerged:
		return "✓ merged"
	case UserStatusClosed:
		return "closed"
	case UserStatusRunning:
		if !startedAt.IsZero() {
			return fmt.Sprintf("working (%s)", FormatDuration(now.Sub(startedAt)))
		}
		return "working"
	case UserStatusReplied:
		if lastRunMS > 0 {
			return fmt.Sprintf("replied (%s)", FormatDuration(time.Duration(lastRunMS)*time.Millisecond))
		}
		return "replied"
	case UserStatusError:
		if lastError == "interrupted" {
			if lastRunMS > 0 {
				return fmt.Sprintf("interrupted (%s)", FormatDuration(time.Duration(lastRunMS)*time.Millisecond))
			}
			return "interrupted"
		}
		if lastRunMS > 0 {
			return fmt.Sprintf("error (%s)", FormatDuration(time.Duration(lastRunMS)*time.Millisecond))
		}
		return "error"
	case UserStatusDraft:
		return "draft"
	default:
		return ""
	}
}

// LoadReviewSummary scans the task's reviews/ directory and returns aggregate info.
func LoadReviewSummary(taskName string) *ReviewSummaryView {
	dir := ReviewsDir(taskName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	type parsed struct {
		name    string
		ts      time.Time
		kind    string
		adapter string
	}
	var matched []parsed
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".md")
		parts := strings.SplitN(stem, "-", 4)
		if len(parts) != 4 {
			continue
		}
		ts, err := time.Parse("20060102T150405Z", parts[0])
		if err != nil {
			continue
		}
		matched = append(matched, parsed{name: e.Name(), ts: ts, kind: parts[2], adapter: parts[3]})
	}
	if len(matched) == 0 {
		return nil
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].name < matched[j].name })
	last := matched[len(matched)-1]
	return &ReviewSummaryView{
		Count:       len(matched),
		LastTS:      last.ts,
		LastKind:    last.kind,
		LastAdapter: last.adapter,
	}
}
