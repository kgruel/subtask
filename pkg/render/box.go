package render

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/kgruel/subtask/pkg/task"
)

// routineSourceSuffix returns the display suffix for a routine source string.
// Mirrors routine.SourceSuffix without importing pkg/routine (no cycle).
func routineSourceSuffix(source string) string {
	switch source {
	case "shadow":
		return " (project shadow)"
	case "project":
		return " (project)"
	default:
		return ""
	}
}

// Box wraps content in a box (pretty mode) or returns as-is (plain mode).
type Box struct {
	Title   string
	Content string
}

// RenderPlain returns the content as-is with optional title.
func (b *Box) RenderPlain() string {
	if b.Title != "" {
		return fmt.Sprintf("%s\n%s", b.Title, b.Content)
	}
	return b.Content
}

// RenderPretty renders content in a styled box with optional title.
func (b *Box) RenderPretty() string {
	if b.Title != "" {
		return styleBoxTitle.Render(b.Content) + "\n"
	}
	return styleBox.Render(b.Content) + "\n"
}

// Print renders and prints the box.
func (b *Box) Print() {
	if Pretty {
		fmt.Print(b.RenderPretty())
	} else {
		fmt.Print(b.RenderPlain())
	}
}

// PrintBox prints content optionally in a box.
func PrintBox(content string) {
	b := &Box{Content: content}
	b.Print()
}

// PrintTitledBox prints content in a box with a title.
func PrintTitledBox(title, content string) {
	b := &Box{Title: title, Content: content}
	b.Print()
}

// ProgressStep represents a step in PROGRESS.json.
type ProgressStep struct {
	Step string
	Done bool
}

// TaskCard renders a task details card (for show command).
type TaskCard struct {
	Name          string
	Title         string
	TaskStatus    string
	Error         string
	Branch        string
	BaseBranch    string
	BaseCommit    string
	Model         string
	Reasoning     string
	Agent         string // Agent name, if bound (task-level or current routine step)
	Workspace     string
	Progress      string // "3/5" or empty
	ProgressSteps []ProgressStep
	Routine       string
	RoutineSource string // "canonical", "shadow", or "project" — drives suffix display
	Stage         string // Formatted progression string
	TaskDir         string // Task directory path (e.g., .subtask/tasks/fix--foo)
	Files           []string
	LinesAdded      int // Git diff stats
	LinesRemoved    int
	ChangesStatus   string // "", "applied", "missing"
	ChangesError    string
	CommitCount     int
	CommitError     string
	ShowCommits     bool
	ConflictFiles   []string

	ReviewCount    int
	LastReviewTS   time.Time
	LastReviewKind string
	LastReviewer   string

	Verbose   bool
	Artifacts []task.ArtifactInfo
}

// RenderPlain renders the task card as plain key-value text.
func (c *TaskCard) RenderPlain() string {
	var buf strings.Builder

	fmt.Fprintf(&buf, "Task: %s\n", c.Name)
	fmt.Fprintf(&buf, "Title: %s\n", c.Title)
	fmt.Fprintf(&buf, "Branch: %s (based on %s)\n", c.Branch, c.BaseBranch)
	if c.BaseCommit != "" {
		fmt.Fprintf(&buf, "Base commit: %s\n", c.BaseCommit)
	}
	if c.Model != "" {
		if c.Reasoning != "" {
			fmt.Fprintf(&buf, "Model: %s (%s)\n", c.Model, c.Reasoning)
		} else {
			fmt.Fprintf(&buf, "Model: %s\n", c.Model)
		}
	}
	if c.Agent != "" && c.Agent != "Worker" {
		fmt.Fprintf(&buf, "Agent: %s\n", c.Agent)
	}
	if c.Workspace != "" {
		fmt.Fprintf(&buf, "Workspace: %s\n", c.Workspace)
	}

	statusStr := c.TaskStatus
	if !c.Verbose {
		statusStr = stripStatusAge(statusStr)
	}
	fmt.Fprintf(&buf, "Status: %s\n", statusStr)
	if c.Error != "" {
		fmt.Fprintf(&buf, "Error: %s\n", c.Error)
	}

	// Git changes + commit count
	if c.TaskStatus != "" {
		switch strings.TrimSpace(c.ChangesStatus) {
		case "missing":
			fmt.Fprintf(&buf, "Changes: missing\n")
			indent := strings.Repeat(" ", len("Changes: "))
			fmt.Fprintf(&buf, "%sBranch was deleted or commit objects are missing.\n", indent)
			fmt.Fprintf(&buf, "%sRun `subtask close` to close, or restore the branch and retry.\n", indent)
		default:
			if c.ChangesError != "" {
				fmt.Fprintf(&buf, "Changes: %s\n", c.ChangesError)
			} else {
				fmt.Fprintf(&buf, "Changes: %s\n", formatChanges(c.LinesAdded, c.LinesRemoved))
				if strings.TrimSpace(c.ChangesStatus) == "applied" {
					indent := strings.Repeat(" ", len("Changes: "))
					fmt.Fprintf(&buf, "%sAlready in base branch. Run `subtask merge` to mark as merged.\n", indent)
				}
			}
		}
		if c.ShowCommits {
			if c.CommitError != "" {
				fmt.Fprintf(&buf, "Commits: %s\n", c.CommitError)
			} else {
				fmt.Fprintf(&buf, "Commits: %d\n", c.CommitCount)
			}
		}
	}

	if c.ReviewCount > 0 {
		tsStr := c.LastReviewTS.UTC().Format("2006-01-02 15:04 UTC")
		fmt.Fprintf(&buf, "Reviews: %d (latest: %s, %s by %s)\n", c.ReviewCount, tsStr, c.LastReviewKind, c.LastReviewer)
	}

	if len(c.ConflictFiles) > 0 {
		fmt.Fprintf(&buf, "Conflicts: %s\n", strings.Join(c.ConflictFiles, ", "))
	}

	if len(c.Artifacts) > 0 {
		fmt.Fprintf(&buf, "Artifacts:\n")
		for _, a := range c.Artifacts {
			if a.Missing {
				fmt.Fprintf(&buf, "  %s (missing, %s)\n", a.Name, a.Kind)
			} else {
				fmt.Fprintf(&buf, "  %s (%s, %s)\n", a.Name, formatArtifactSize(a.Size), a.Kind)
			}
		}
	}

	if c.Progress != "" {
		fmt.Fprintf(&buf, "Progress: %s\n", c.Progress)
	}
	if c.Routine != "" {
		fmt.Fprintf(&buf, "Routine: %s%s\n", c.Routine, routineSourceSuffix(c.RoutineSource))
	}
	if c.Stage != "" {
		fmt.Fprintf(&buf, "Flow: %s\n", c.Stage)
	}

	if len(c.ProgressSteps) > 0 {
		fmt.Fprintf(&buf, "\nSteps:\n")
		for _, step := range c.ProgressSteps {
			check := "[ ]"
			if step.Done {
				check = "[x]"
			}
			fmt.Fprintf(&buf, "  %s %s\n", check, step.Step)
		}
	}

	if c.TaskDir != "" {
		taskDir := filepath.ToSlash(c.TaskDir)
		if len(c.Files) > 0 {
			fmt.Fprintf(&buf, "\nDirectory: %s (contains %s)\n", taskDir, strings.Join(c.Files, ", "))
		} else {
			fmt.Fprintf(&buf, "\nDirectory: %s\n", taskDir)
		}
	}

	return buf.String()
}

// FormatDuration formats a duration for display.
// Returns: Xs, Xm, or XhYm
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

// RenderPretty renders the task card with styling and box.
func (c *TaskCard) RenderPretty() string {
	var lines []string

	// Task name as header with └ for title
	lines = append(lines, styleHighlight.Bold(true).Render(c.Name))
	lines = append(lines, styleDim.Render("└ "+c.Title))
	lines = append(lines, "")

	prettyStatusStr := c.TaskStatus
	if !c.Verbose {
		prettyStatusStr = stripStatusAge(prettyStatusStr)
	}
	lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Status"), Status(prettyStatusStr)))
	if c.Error != "" {
		lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Error"), styleError.Render(c.Error)))
	}

	// Branch
	branchInfo := fmt.Sprintf("%s %s", c.Branch, styleDim.Render("(based on "+c.BaseBranch+")"))
	lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Branch"), branchInfo))
	if strings.TrimSpace(c.BaseCommit) != "" {
		lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Base"), styleDim.Render(strings.TrimSpace(c.BaseCommit))))
	}

	// Model (and reasoning)
	if c.Model != "" {
		modelInfo := c.Model
		if c.Reasoning != "" {
			modelInfo = fmt.Sprintf("%s %s", c.Model, styleDim.Render("("+c.Reasoning+")"))
		}
		lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Model"), modelInfo))
	}
	if c.Agent != "" && c.Agent != "Worker" {
		lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Agent"), c.Agent))
	}

	// Workspace
	if c.Workspace != "" {
		lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Workspace"), c.Workspace))
	}

	// Changes + commits
	if c.TaskStatus != "" {
		switch strings.TrimSpace(c.ChangesStatus) {
		case "missing":
			lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Changes"), styleDim.Render("missing")))
			lines = append(lines, fmt.Sprintf("%s  %s", styleDim.Render(""), styleDim.Render("Branch was deleted or commit objects are missing.")))
			lines = append(lines, fmt.Sprintf("%s  %s", styleDim.Render(""), styleDim.Render("Run `subtask close` to close, or restore the branch and retry.")))
		default:
			if c.ChangesError != "" {
				lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Changes"), styleError.Render(c.ChangesError)))
			} else {
				lines = append(lines, fmt.Sprintf("%s %s", styleBold.Render("Changes"), formatChangesColored(c.LinesAdded, c.LinesRemoved)))
				if strings.TrimSpace(c.ChangesStatus) == "applied" {
					lines = append(lines, fmt.Sprintf("%s  %s", styleDim.Render(""), styleDim.Render("Already in base branch. Run `subtask merge` to mark as merged.")))
				}
			}
		}
		if c.ShowCommits {
			if c.CommitError != "" {
				lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Commits"), styleError.Render(c.CommitError)))
			} else {
				lines = append(lines, fmt.Sprintf("%s  %d", styleBold.Render("Commits"), c.CommitCount))
			}
		}
	}

	if c.ReviewCount > 0 {
		tsStr := c.LastReviewTS.UTC().Format("2006-01-02 15:04 UTC")
		reviewInfo := fmt.Sprintf("%d (latest: %s, %s by %s)", c.ReviewCount, tsStr, c.LastReviewKind, c.LastReviewer)
		lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Reviews"), reviewInfo))
	}

	if len(c.ConflictFiles) > 0 {
		lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Conflicts"), styleDim.Render(strings.Join(c.ConflictFiles, ", "))))
	}

	if len(c.Artifacts) > 0 {
		var parts []string
		for _, a := range c.Artifacts {
			if a.Missing {
				parts = append(parts, fmt.Sprintf("%s (missing, %s)", a.Name, a.Kind))
			} else {
				parts = append(parts, fmt.Sprintf("%s (%s, %s)", a.Name, formatArtifactSize(a.Size), a.Kind))
			}
		}
		lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Artifacts"), strings.Join(parts, "\n           ")))
	}

	// Routine (name + shadow marker) and Stage progression
	if c.Routine != "" {
		routineLabel := c.Routine
		if suffix := routineSourceSuffix(c.RoutineSource); suffix != "" {
			routineLabel = fmt.Sprintf("%s %s", c.Routine, styleDim.Render(strings.TrimSpace(suffix)))
		}
		lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Routine"), routineLabel))
	}
	if c.Stage != "" {
		lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Flow"), c.Stage))
	}

	// Progress steps with checkboxes
	if len(c.ProgressSteps) > 0 {
		lines = append(lines, "")
		lines = append(lines, styleBold.Render("Progress"))
		for _, step := range c.ProgressSteps {
			var checkbox string
			if step.Done {
				checkbox = styleSuccess.Render("[✓]")
			} else {
				checkbox = styleDim.Render("[ ]")
			}
			stepText := step.Step
			if step.Done {
				stepText = styleDim.Render(step.Step)
			}
			lines = append(lines, fmt.Sprintf("%s %s", checkbox, stepText))
		}
	}

	// Directory section
	if c.TaskDir != "" {
		taskDir := filepath.ToSlash(c.TaskDir)
		lines = append(lines, "")
		if len(c.Files) > 0 {
			lines = append(lines, styleBold.Render("Directory")+"  "+taskDir+" "+styleDim.Render("(contains "+strings.Join(c.Files, ", ")+")"))
		} else {
			lines = append(lines, styleBold.Render("Directory")+"  "+taskDir)
		}
	}

	content := strings.Join(lines, "\n")

	return styleBox.Render(content) + "\n"
}

// Print renders and prints the task card.
func (c *TaskCard) Print() {
	if Pretty {
		fmt.Print(c.RenderPretty())
	} else {
		fmt.Print(c.RenderPlain())
	}
}

// stripStatusAge removes the parenthetical timing annotation from a status
// string, e.g. "replied (1s)" → "replied".
func stripStatusAge(s string) string {
	if i := strings.Index(s, " ("); i >= 0 {
		return s[:i]
	}
	return s
}

// formatArtifactSize returns a human-readable byte count.
func formatArtifactSize(n int64) string {
	const kb = 1024
	const mb = 1024 * kb
	if n < kb {
		return fmt.Sprintf("%dB", n)
	}
	if n < mb {
		return fmt.Sprintf("%.1fKB", float64(n)/kb)
	}
	return fmt.Sprintf("%.1fMB", float64(n)/mb)
}
