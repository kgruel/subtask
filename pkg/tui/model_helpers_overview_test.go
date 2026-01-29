package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/store"
	"github.com/zippoxer/subtask/pkg/workflow"
)

func TestWrapWithIndent_IndentsContinuationLines(t *testing.T) {
	lines := wrapWithIndent("one two three", 8, 2)
	if len(lines) < 2 {
		t.Fatalf("lines=%d want>=2", len(lines))
	}
	if strings.HasPrefix(lines[0], "  ") {
		t.Fatalf("first line unexpectedly indented: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  ") {
		t.Fatalf("second line missing indent: %q", lines[1])
	}
}

func TestUpdateOverviewContent_RendersProgressAndWorkflow(t *testing.T) {
	m := newModel()
	m.vpOverview = viewport.New(80, 30)

	m.detail = store.TaskView{
		Task: &task.Task{
			Name:        "fix/overview",
			Title:       "Overview Task",
			BaseBranch:  "main",
			Description: "hello",
		},
		TaskStatus:   task.TaskStatusOpen,
		WorkerStatus: task.WorkerStatusReplied,
		Stage:        "review",
		ProgressSteps: []task.ProgressStep{
			{Step: "first step", Done: true},
			{Step: "second step", Done: false},
		},
		Workflow: &workflow.Workflow{
			Name: "test",
			Stages: []workflow.Stage{
				{Name: "plan"},
				{Name: "review"},
			},
		},
	}

	m.updateOverviewContent()
	out := ansi.Strip(m.vpOverview.View())

	if !strings.Contains(out, "Progress 1/2") {
		t.Fatalf("expected progress header in output; got:\n%s", out)
	}
	if !strings.Contains(out, "■ first step") {
		t.Fatalf("expected done progress step in output; got:\n%s", out)
	}
	if !strings.Contains(out, "□ second step") {
		t.Fatalf("expected pending progress step in output; got:\n%s", out)
	}
	if !strings.Contains(out, "Workflow") || !strings.Contains(out, "plan") || !strings.Contains(out, "review") {
		t.Fatalf("expected workflow section in output; got:\n%s", out)
	}
}
