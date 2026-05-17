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

	// 10 open tasks — all should appear in both default and --all output.
	const openCount = 10
	for i := range openCount {
		env.CreateTask(fmt.Sprintf("open/task-%02d", i), "Open task", "main", "")
	}

	// Closed task: must be absent from default, present in --all.
	closedName := "fix/closed-one"
	env.CreateTask(closedName, "Closed task", "main", "")
	env.CreateTaskHistory(closedName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{Type: "task.closed", Data: mustJSON(map[string]any{"reason": "close"})},
	})

	// Merged task: must be absent from default, present in --all.
	mergedName := "fix/merged-one"
	env.CreateTask(mergedName, "Merged task", "main", "")
	env.CreateTaskHistory(mergedName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{Type: "task.merged", Data: mustJSON(map[string]any{"via": "detected", "base_branch": "main"})},
	})

	// Default list: open only — merged and closed are absent.
	stdout, _, err := captureStdoutStderr(t, (&ListCmd{JSON: true}).Run)
	require.NoError(t, err)
	var defaultItems []listJSONItem
	require.NoError(t, json.Unmarshal([]byte(stdout), &defaultItems))
	require.Len(t, defaultItems, openCount)
	for _, it := range defaultItems {
		require.NotEqual(t, closedName, it.Name, "closed task must not appear in default list")
		require.NotEqual(t, mergedName, it.Name, "merged task must not appear in default list")
	}

	// --all list: includes closed and merged tasks too.
	stdout, _, err = captureStdoutStderr(t, (&ListCmd{JSON: true, All: true}).Run)
	require.NoError(t, err)
	var all []listJSONItem
	require.NoError(t, json.Unmarshal([]byte(stdout), &all))
	require.Len(t, all, openCount+2)

	statusByName := make(map[string]string, len(all))
	for _, it := range all {
		statusByName[it.Name] = it.Status
	}
	require.Equal(t, "closed", statusByName[closedName], "closed task must appear in --all output")
	require.Equal(t, "merged", statusByName[mergedName], "merged task must appear in --all output")
}

func TestListCmd_JSON_Empty(t *testing.T) {
	testutil.NewTestEnv(t, 1)

	stdout, _, err := captureStdoutStderr(t, (&ListCmd{JSON: true}).Run)
	require.NoError(t, err)

	var items []listJSONItem
	require.NoError(t, json.Unmarshal([]byte(stdout), &items))
	require.Empty(t, items)
}
