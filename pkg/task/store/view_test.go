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

	v, err := store.BuildView(context.Background(), taskName)
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

	v, err := store.BuildView(context.Background(), taskName)
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

	v, err := store.BuildView(context.Background(), taskName)
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
