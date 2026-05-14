package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

func TestListCmd_JSON_ParsesClean(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	env.CreateTask("fix/alpha", "Alpha fix", "main", "Do the alpha fix")
	env.CreateTask("feat/beta", "Beta feature", "main", "Add beta")

	stdout, stderr, err := captureStdoutStderr(t, (&ListCmd{JSON: true}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)

	var items []listJSONItem
	require.NoError(t, json.Unmarshal([]byte(stdout), &items))
	require.Len(t, items, 2)

	names := make(map[string]bool)
	for _, it := range items {
		names[it.Name] = true
		require.NotEmpty(t, it.TaskDir)
		require.NotEmpty(t, it.HistoryPath)
	}
	require.True(t, names["fix/alpha"])
	require.True(t, names["feat/beta"])
}

func TestListCmd_JSON_RoundTrips(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	env.CreateTask("task/one", "Task one", "main", "Description one")

	stdout, _, err := captureStdoutStderr(t, (&ListCmd{JSON: true}).Run)
	require.NoError(t, err)

	var items []listJSONItem
	require.NoError(t, json.Unmarshal([]byte(stdout), &items))

	// Round-trip: marshal again and compare to the original unmarshal.
	second, err := json.MarshalIndent(items, "", "  ")
	require.NoError(t, err)

	var items2 []listJSONItem
	require.NoError(t, json.Unmarshal(second, &items2))
	require.Equal(t, items, items2)
}

func TestListCmd_JSON_AllFlag(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	// Create 10 open tasks to fill the default list target count (10),
	// leaving no room for the closed task in the default output.
	const defaultTargetCount = 10
	for i := range defaultTargetCount {
		env.CreateTask(fmt.Sprintf("open/task-%02d", i), "Open task", "main", "")
	}

	// Closed task: needs a task.closed event so the index marks it as closed.
	closedName := "fix/closed-one"
	env.CreateTask(closedName, "Closed task", "main", "")
	env.CreateTaskHistory(closedName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{Type: "task.closed", Data: mustJSON(map[string]any{"reason": "close"})},
	})

	// Default list: 10 open tasks fill the count; closed task is excluded.
	stdout, _, err := captureStdoutStderr(t, (&ListCmd{JSON: true}).Run)
	require.NoError(t, err)
	var open []listJSONItem
	require.NoError(t, json.Unmarshal([]byte(stdout), &open))
	require.Len(t, open, defaultTargetCount)
	for _, it := range open {
		require.NotEqual(t, closedName, it.Name, "closed task must not appear in default list")
	}

	// --all list: includes the closed task too.
	stdout, _, err = captureStdoutStderr(t, (&ListCmd{JSON: true, All: true}).Run)
	require.NoError(t, err)
	var all []listJSONItem
	require.NoError(t, json.Unmarshal([]byte(stdout), &all))
	require.Len(t, all, defaultTargetCount+1)

	var found *listJSONItem
	for i := range all {
		if all[i].Name == closedName {
			found = &all[i]
			break
		}
	}
	require.NotNil(t, found, "closed task must appear in --all output")
	require.Equal(t, "closed", found.Status)
}

func TestListCmd_JSON_Empty(t *testing.T) {
	testutil.NewTestEnv(t, 1)

	stdout, _, err := captureStdoutStderr(t, (&ListCmd{JSON: true}).Run)
	require.NoError(t, err)

	var items []listJSONItem
	require.NoError(t, json.Unmarshal([]byte(stdout), &items))
	require.Empty(t, items)
}
