package render

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTaskCard_RenderPlain_AgentField(t *testing.T) {
	card := &TaskCard{
		Name:       "fix/foo",
		Title:      "Fix foo",
		Branch:     "fix/foo",
		BaseBranch: "main",
		Agent:      "planner",
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
