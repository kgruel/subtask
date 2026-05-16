package render

import (
	"strings"
	"testing"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/stretchr/testify/require"
)

func TestTaskCard_RenderPlain_AgentField(t *testing.T) {
	card := &TaskCard{
		Name:       "fix/foo",
		Title:      "Fix foo",
		Branch:     "fix/foo",
		BaseBranch: "main",
		Agent:      "planner",
		AgentIsNamed: true,
	}
	out := card.RenderPlain()
	require.Contains(t, out, "Agent: planner")
}

func TestTaskCard_RenderPlain_NoAgentField(t *testing.T) {
	card := &TaskCard{
		Name:       "fix/foo",
		Title:      "Fix foo",
		Branch:     "fix/foo",
		BaseBranch: "main",
	}
	out := card.RenderPlain()
	require.NotContains(t, out, "Agent:")
}

func TestTaskCard_RenderPlain_RoutineShadow(t *testing.T) {
	card := &TaskCard{
		Name:          "fix/foo",
		Title:         "Fix foo",
		Branch:        "fix/foo",
		BaseBranch:    "main",
		Routine:       "default",
		RoutineSource: "shadow",
	}
	out := card.RenderPlain()
	require.Contains(t, out, "Routine: default (project shadow)")
}

func TestTaskCard_RenderPlain_RoutineProject(t *testing.T) {
	card := &TaskCard{
		Name:          "fix/foo",
		Title:         "Fix foo",
		Branch:        "fix/foo",
		BaseBranch:    "main",
		Routine:       "release",
		RoutineSource: "project",
	}
	out := card.RenderPlain()
	require.Contains(t, out, "Routine: release (project)")
	require.NotContains(t, out, "project shadow")
}

func TestTaskCard_RenderPlain_RoutineCanonical(t *testing.T) {
	card := &TaskCard{
		Name:          "fix/foo",
		Title:         "Fix foo",
		Branch:        "fix/foo",
		BaseBranch:    "main",
		Routine:       "default",
		RoutineSource: "canonical",
	}
	out := card.RenderPlain()
	require.Contains(t, out, "Routine: default\n")
	require.NotContains(t, out, "project")
}

func TestTaskCard_RenderPretty_AgentField(t *testing.T) {
	Pretty = false // use plain-mode rendering; pretty requires a terminal
	card := &TaskCard{
		Name:       "fix/foo",
		Title:      "Fix foo",
		Branch:     "fix/foo",
		BaseBranch: "main",
		Agent:      "reviewer",
		AgentIsNamed: true,
	}
	out := card.RenderPlain()
	require.True(t, strings.Contains(out, "Agent: reviewer"), "plain output must contain Agent: reviewer")
}

func TestTaskCard_RenderPretty_RoutineShadow(t *testing.T) {
	Pretty = false
	card := &TaskCard{
		Name:          "fix/foo",
		Title:         "Fix foo",
		Branch:        "fix/foo",
		BaseBranch:    "main",
		Routine:       "they-plan",
		RoutineSource: "shadow",
	}
	out := card.RenderPlain()
	require.Contains(t, out, "project shadow")
}

func TestTaskCardFromView_Identity(t *testing.T) {
	tests := []struct {
		name     string
		view     *task.View
		want     string
		isNamed  bool
	}{
		{
			name: "named agent with adapter and model",
			view: &task.View{
				Agent: task.AgentView{Name: "planner", Adapter: "openai", Model: "gpt-4"},
			},
			want:    "planner (openai/gpt-4)",
			isNamed: true,
		},
		{
			name: "named agent with model only",
			view: &task.View{
				Agent: task.AgentView{Name: "planner", Model: "gpt-4"},
			},
			want:    "planner (gpt-4)",
			isNamed: true,
		},
		{
			name: "unnamed agent with adapter and model",
			view: &task.View{
				Agent: task.AgentView{Adapter: "openai", Model: "gpt-4"},
			},
			want:    "openai/gpt-4",
			isNamed: false,
		},
		{
			name: "unnamed agent with model only (regression fix)",
			view: &task.View{
				Agent: task.AgentView{Model: "gpt-4"},
			},
			want:    "gpt-4",
			isNamed: false,
		},
		{
			name: "named agent with reasoning",
			view: &task.View{
				Agent: task.AgentView{Name: "planner", Adapter: "openai", Model: "gpt-4", Reasoning: "high"},
			},
			want:    "planner (openai/gpt-4, reasoning:high)",
			isNamed: true,
		},
		{
			name: "unnamed agent with reasoning",
			view: &task.View{
				Agent: task.AgentView{Adapter: "openai", Model: "gpt-4", Reasoning: "high"},
			},
			want:    "openai/gpt-4 (reasoning:high)",
			isNamed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := TaskCardFromView(tt.view, false)
			require.Equal(t, tt.want, card.Agent)
			require.Equal(t, tt.isNamed, card.AgentIsNamed)
		})
	}
}
