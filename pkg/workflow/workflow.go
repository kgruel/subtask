package workflow

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/kgruel/subtask/pkg/task"
)

//go:embed templates/*.yaml
var embeddedTemplates embed.FS

// Stage represents a workflow stage.
type Stage struct {
	Name         string `yaml:"name"`
	Instructions string `yaml:"instructions"`
}

// Instructions contains guidance for lead and worker.
type Instructions struct {
	Lead   string `yaml:"lead"`
	Worker string `yaml:"worker"`
}

// Workflow represents a workflow template.
type Workflow struct {
	Name         string       `yaml:"name"`
	Description  string       `yaml:"description"`
	Instructions Instructions `yaml:"instructions"`
	Stages       []Stage      `yaml:"stages"`
}

// WorkflowsDir returns the path to the workflows directory.
func WorkflowsDir() string {
	return filepath.Join(task.ProjectDir(), "workflows")
}

// TemplateDir returns the path to a workflow template directory.
func TemplateDir(name string) string {
	return filepath.Join(WorkflowsDir(), name)
}

// Load loads a workflow by name.
// First checks for local override in .subtask/workflows/<name>/WORKFLOW.yaml,
// then falls back to embedded default.
func Load(workflowDir string) (*Workflow, error) {
	name := filepath.Base(workflowDir)
	return LoadByName(name)
}

// LoadByName loads a workflow by name.
// First checks for local override, then falls back to embedded default.
func LoadByName(name string) (*Workflow, error) {
	// Try local override first
	localPath := filepath.Join(WorkflowsDir(), name, "WORKFLOW.yaml")
	if data, err := os.ReadFile(localPath); err == nil {
		return parseWorkflow(data)
	}

	// Fall back to embedded
	return loadEmbedded(name)
}

// loadEmbedded loads a workflow from embedded templates.
func loadEmbedded(name string) (*Workflow, error) {
	path := fmt.Sprintf("templates/%s.yaml", name)
	data, err := embeddedTemplates.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("workflow %q not found", name)
	}
	return parseWorkflow(data)
}

// parseWorkflow parses YAML data into a Workflow.
func parseWorkflow(data []byte) (*Workflow, error) {
	var w Workflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("invalid workflow: %w", err)
	}

	if len(w.Stages) == 0 {
		return nil, fmt.Errorf("workflow has no stages")
	}

	return &w, nil
}

// LoadFromTask loads a workflow from a task's folder (if WORKFLOW.yaml exists).
func LoadFromTask(taskName string) (*Workflow, error) {
	taskDir := task.Dir(taskName)
	path := filepath.Join(taskDir, "WORKFLOW.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No workflow for this task
		}
		return nil, err
	}

	var w Workflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("invalid workflow in task: %w", err)
	}

	return &w, nil
}

// StageIndex returns the index of a stage by name, or -1 if not found.
func (w *Workflow) StageIndex(name string) int {
	for i, s := range w.Stages {
		if s.Name == name {
			return i
		}
	}
	return -1
}

// GetStage returns a stage by name, or nil if not found.
func (w *Workflow) GetStage(name string) *Stage {
	idx := w.StageIndex(name)
	if idx < 0 {
		return nil
	}
	return &w.Stages[idx]
}

// NextStage returns the next stage name after current, or "" if current is last.
func (w *Workflow) NextStage(current string) string {
	idx := w.StageIndex(current)
	if idx < 0 || idx >= len(w.Stages)-1 {
		return ""
	}
	return w.Stages[idx+1].Name
}

// FirstStage returns the first stage name.
func (w *Workflow) FirstStage() string {
	if len(w.Stages) == 0 {
		return ""
	}
	return w.Stages[0].Name
}

// StageNames returns all stage names.
func (w *Workflow) StageNames() []string {
	names := make([]string, len(w.Stages))
	for i, s := range w.Stages {
		names[i] = s.Name
	}
	return names
}

// FormatProgression returns a formatted stage progression string.
// Example: "plan → implement → review" with current stage marked.
func (w *Workflow) FormatProgression(current string) string {
	names := w.StageNames()
	return FormatProgression(names, current)
}

// FormatProgression formats stage names with current stage in parentheses.
// Example: "plan → (implement) → review → ready"
func FormatProgression(stages []string, current string) string {
	if len(stages) == 0 {
		return ""
	}

	parts := make([]string, len(stages))
	for i, name := range stages {
		if name == current {
			parts[i] = "(" + name + ")"
		} else {
			parts[i] = name
		}
	}

	return strings.Join(parts, " → ")
}

// CopyToTask copies WORKFLOW.yaml to a task folder.
// Reads from local override or embedded template.
func CopyToTask(workflowName, taskName string) error {
	dstDir := task.Dir(taskName)

	// Ensure destination exists
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}

	dst := filepath.Join(dstDir, "WORKFLOW.yaml")

	// Skip if destination already exists
	if _, err := os.Stat(dst); err == nil {
		return nil
	}

	// Try local override first
	localPath := filepath.Join(WorkflowsDir(), workflowName, "WORKFLOW.yaml")
	data, err := os.ReadFile(localPath)
	if err != nil {
		// Fall back to embedded
		embeddedPath := fmt.Sprintf("templates/%s.yaml", workflowName)
		data, err = embeddedTemplates.ReadFile(embeddedPath)
		if err != nil {
			return fmt.Errorf("workflow %q not found", workflowName)
		}
	}

	if err := os.WriteFile(dst, data, 0644); err != nil {
		return fmt.Errorf("failed to write WORKFLOW.yaml: %w", err)
	}

	return nil
}
