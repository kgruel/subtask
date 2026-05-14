package task

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTaskSaveLoad_IncludesModelAndReasoning(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	in := &Task{
		Name:        "test/model",
		Title:       "Title",
		BaseBranch:  "main",
		FollowUp:    "prev/task",
		Model:       "gpt-5.2",
		Reasoning:   "high",
		Description: "Description",
	}
	require.NoError(t, in.Save())

	out, err := Load(in.Name)
	require.NoError(t, err)
	require.Equal(t, in.Name, out.Name)
	require.Equal(t, in.Title, out.Title)
	require.Equal(t, in.BaseBranch, out.BaseBranch)
	require.Equal(t, in.FollowUp, out.FollowUp)
	require.Equal(t, in.Model, out.Model)
	require.Equal(t, in.Reasoning, out.Reasoning)
	require.Equal(t, in.Description, out.Description)
}

func TestTaskSaveLoad_AgentRoundTrip(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	in := &Task{
		Name:        "test/agent",
		Title:       "Title",
		BaseBranch:  "main",
		Agent:       "planner",
		Description: "Description",
	}
	require.NoError(t, in.Save())

	raw, err := os.ReadFile(in.Path())
	require.NoError(t, err)
	require.Contains(t, string(raw), "agent: planner")

	out, err := Load(in.Name)
	require.NoError(t, err)
	require.Equal(t, "planner", out.Agent)
}

func TestTaskSaveLoad_RoutineRoundTrip(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	in := &Task{
		Name:        "test/routine",
		Title:       "Title",
		BaseBranch:  "main",
		Routine:     "jira-ticket",
		Description: "Description",
	}
	require.NoError(t, in.Save())

	raw, err := os.ReadFile(in.Path())
	require.NoError(t, err)
	require.Contains(t, string(raw), "routine: jira-ticket")

	out, err := Load(in.Name)
	require.NoError(t, err)
	require.Equal(t, "jira-ticket", out.Routine)
}

func TestTaskSave_OmitsRoutineWhenEmpty(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	in := &Task{
		Name:        "test/no-routine",
		Title:       "Title",
		BaseBranch:  "main",
		Description: "Description",
	}
	require.NoError(t, in.Save())

	raw, err := os.ReadFile(in.Path())
	require.NoError(t, err)
	require.False(t, strings.Contains(string(raw), "routine:"), "frontmatter must omit routine: when Task.Routine is empty")
}

func TestTaskSave_OmitsAgentWhenEmpty(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	in := &Task{
		Name:        "test/no-agent",
		Title:       "Title",
		BaseBranch:  "main",
		Description: "Description",
	}
	require.NoError(t, in.Save())

	raw, err := os.ReadFile(in.Path())
	require.NoError(t, err)
	require.False(t, strings.Contains(string(raw), "agent:"), "frontmatter must omit agent: when Task.Agent is empty")
}
