package store_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/task/store"
	"github.com/kgruel/subtask/pkg/testutil"
)

func TestBuildView_Basic(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	taskName := "fix/view-test"

	env.CreateTask(taskName, "Test View", "main", "desc")

	v, err := store.BuildView(context.Background(), taskName, nil, store.BuildViewOptions{})
	require.NoError(t, err)
	require.Equal(t, taskName, v.Name)
	require.Equal(t, "Test View", v.Title)
	require.Equal(t, "draft", v.StatusText)
	require.False(t, v.IsTerminal)
}

func TestBuildView_WithAgent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	taskName := "fix/agent-test"

	tk := env.CreateTask(taskName, "Agent Test", "main", "desc")
	tk.Agent = "senior-dev"
	require.NoError(t, tk.Save())

	v, err := store.BuildView(context.Background(), taskName, nil, store.BuildViewOptions{})
	require.NoError(t, err)
	require.Equal(t, "senior-dev", v.Agent.Name)
}

func TestBuildView_WithRoutine(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	taskName := "fix/routine-test"

	// Create routine file
	routineDir := filepath.Join(env.RootDir, ".subtask", "routines")
	os.MkdirAll(routineDir, 0755)
	routineYaml := `
name: test-routine
steps:
  - id: plan
    agent: architect
  - id: done
    kind: terminal
`
	err := os.WriteFile(filepath.Join(routineDir, "test-routine.yaml"), []byte(routineYaml), 0644)
	require.NoError(t, err)

	tk := env.CreateTask(taskName, "Routine Test", "main", "desc")
	tk.Routine = "test-routine"
	require.NoError(t, tk.Save())

	// Set stage via history event
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "stage.changed", Data: json.RawMessage(`{"to":"plan"}`)},
	})

	v, err := store.BuildView(context.Background(), taskName, nil, store.BuildViewOptions{})
	require.NoError(t, err)
	require.NotNil(t, v.Routine)
	require.Equal(t, "test-routine", v.Routine.Name)
	require.Equal(t, "plan", v.Routine.CurrentStep)
	require.Equal(t, "architect", v.Routine.StepAgent)
	require.Equal(t, "architect", v.Agent.Name)
	require.Len(t, v.Routine.Steps, 2)
	require.Equal(t, "plan", v.Routine.Steps[0].ID)
	require.Equal(t, "done", v.Routine.Steps[1].ID)
}

func TestBuildView_TerminalRoutineTask_ClearsActiveFields(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	// Create routine file
	routineDir := filepath.Join(env.RootDir, ".subtask", "routines")
	os.MkdirAll(routineDir, 0755)
	routineYaml := `
name: test-routine
steps:
  - id: plan
    agent: architect
    instructions: "Do it."
  - id: done
    kind: terminal
`
	err := os.WriteFile(filepath.Join(routineDir, "test-routine.yaml"), []byte(routineYaml), 0644)
	require.NoError(t, err)

	statuses := []history.Event{
		{Type: "task.merged", Data: json.RawMessage(`{"base_branch":"main","commit":"abc"}`)},
		{Type: "task.closed", Data: json.RawMessage(`{}`)},
	}

	for _, statusEv := range statuses {
		t.Run(string(statusEv.Type), func(t *testing.T) {
			taskName := "fix/terminal-" + string(statusEv.Type)
			tk := env.CreateTask(taskName, "Terminal Test", "main", "desc")
			tk.Routine = "test-routine"
			require.NoError(t, tk.Save())

			// Set stage and status via history events
			env.CreateTaskHistory(taskName, []history.Event{
				{Type: "stage.changed", Data: json.RawMessage(`{"to":"plan"}`)},
				statusEv,
			})

			v, err := store.BuildView(context.Background(), taskName, nil, store.BuildViewOptions{})
			require.NoError(t, err)
			require.True(t, v.IsTerminal)
			require.NotNil(t, v.Routine)
			require.Equal(t, "test-routine", v.Routine.Name)
			require.Len(t, v.Routine.Steps, 2)

			// Assertions for cleared active fields
			require.Empty(t, v.Routine.CurrentStep)
			require.Empty(t, v.Routine.Diagram)
			require.Empty(t, v.Routine.StepAgent)
			require.Empty(t, v.Routine.Instructions)

			// F1 fix: Agent name should revert to task-level (empty in this fixture)
			require.Empty(t, v.Agent.Name)

			// F2 fix: ResolveListAgent should also return task-level (empty)
			require.Empty(t, store.ResolveListAgent(taskName, "plan"))
		})
	}

	t.Run("TaskWithNamedAgent_RevertsToIt", func(t *testing.T) {
		taskName := "fix/terminal-revert"
		tk := env.CreateTask(taskName, "Terminal Revert Test", "main", "desc")
		tk.Routine = "test-routine"
		tk.Agent = "senior-dev"
		require.NoError(t, tk.Save())

		env.CreateTaskHistory(taskName, []history.Event{
			{Type: "stage.changed", Data: json.RawMessage(`{"to":"plan"}`)},
			{Type: "task.merged", Data: json.RawMessage(`{"base_branch":"main","commit":"abc"}`)},
		})

		v, err := store.BuildView(context.Background(), taskName, nil, store.BuildViewOptions{})
		require.NoError(t, err)
		require.True(t, v.IsTerminal)
		// Should be senior-dev, NOT architect from the 'plan' step
		require.Equal(t, "senior-dev", v.Agent.Name)

		// ResolveListAgent should also return senior-dev
		require.Equal(t, "senior-dev", store.ResolveListAgent(taskName, "plan"))
	})
}
