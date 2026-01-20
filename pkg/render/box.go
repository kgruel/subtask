package render

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

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
	Model         string
	Reasoning     string
	Workspace     string
	Progress      string // "3/5" or empty
	ProgressSteps []ProgressStep
	Workflow      string
	Stage         string // Formatted progression string
	TaskDir       string // Task directory path (e.g., .subtask/tasks/fix--foo)
	Files         []string
	LinesAdded    int // Git diff stats
	LinesRemoved  int
	CommitsBehind int
	ConflictFiles []string
}

// RenderPlain renders the task card as plain key-value text.
func (c *TaskCard) RenderPlain() string {
	var buf strings.Builder

	fmt.Fprintf(&buf, "Task: %s\n", c.Name)
	fmt.Fprintf(&buf, "Title: %s\n", c.Title)
	fmt.Fprintf(&buf, "Branch: %s (based on %s)\n", c.Branch, c.BaseBranch)
	if c.Model != "" {
		if c.Reasoning != "" {
			fmt.Fprintf(&buf, "Model: %s (%s)\n", c.Model, c.Reasoning)
		} else {
			fmt.Fprintf(&buf, "Model: %s\n", c.Model)
		}
	}
	if c.Workspace != "" {
		fmt.Fprintf(&buf, "Workspace: %s\n", c.Workspace)
	}

	fmt.Fprintf(&buf, "Status: %s\n", c.TaskStatus)
	if c.Error != "" {
		fmt.Fprintf(&buf, "Error: %s\n", c.Error)
	}

	// Git changes
	if c.TaskStatus != "" {
		fmt.Fprintf(&buf, "Changes: %s\n", formatChanges(c.LinesAdded, c.LinesRemoved))
	}

	if len(c.ConflictFiles) > 0 {
		fmt.Fprintf(&buf, "Conflicts: %s\n", strings.Join(c.ConflictFiles, ", "))
	}

	if c.Progress != "" {
		fmt.Fprintf(&buf, "Progress: %s\n", c.Progress)
	}
	if c.Workflow != "" {
		fmt.Fprintf(&buf, "Workflow: %s\n", c.Workflow)
	}
	if c.Stage != "" {
		fmt.Fprintf(&buf, "Stage: %s\n", c.Stage)
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

	lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Status"), Status(c.TaskStatus)))
	if c.Error != "" {
		lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Error"), styleError.Render(c.Error)))
	}

	// Branch
	branchInfo := fmt.Sprintf("%s %s", c.Branch, styleDim.Render("(based on "+c.BaseBranch+")"))
	lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Branch"), branchInfo))

	// Model (and reasoning)
	if c.Model != "" {
		modelInfo := c.Model
		if c.Reasoning != "" {
			modelInfo = fmt.Sprintf("%s %s", c.Model, styleDim.Render("("+c.Reasoning+")"))
		}
		lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Model"), modelInfo))
	}

	// Workspace
	if c.Workspace != "" {
		lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Workspace"), c.Workspace))
	}

	// Changes (git diff stats)
	if c.TaskStatus != "" {
		lines = append(lines, fmt.Sprintf("%s %s", styleBold.Render("Changes"), formatChangesColored(c.LinesAdded, c.LinesRemoved)))
	}

	if len(c.ConflictFiles) > 0 {
		lines = append(lines, fmt.Sprintf("%s  %s", styleBold.Render("Conflicts"), styleDim.Render(strings.Join(c.ConflictFiles, ", "))))
	}

	// Stage
	if c.Stage != "" {
		lines = append(lines, fmt.Sprintf("%s   %s", styleBold.Render("Stage"), c.Stage))
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
