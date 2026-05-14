package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

func TestDraftCmd_ChildDraftedEventOnParent(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	// Draft parent task.
	parent := &DraftCmd{
		Task:        "parent",
		Title:       "Parent Task",
		Description: "Parent description",
		Base:        "main",
	}
	require.NoError(t, parent.Run())

	// Draft child with --follow-up pointing at parent.
	child := &DraftCmd{
		Task:        "child",
		Title:       "Child Task",
		Description: "Child description",
		Base:        "main",
		FollowUp:    "parent",
	}
	require.NoError(t, child.Run())

	// Read parent's history and find the child.drafted event.
	events, err := history.Read("parent", history.ReadOptions{})
	require.NoError(t, err)

	var found bool
	for _, ev := range events {
		if ev.Type != "child.drafted" {
			continue
		}
		var d struct {
			ChildName  string `json:"child_name"`
			BaseCommit string `json:"base_commit"`
		}
		require.NoError(t, json.Unmarshal(ev.Data, &d))
		require.Equal(t, "child", d.ChildName)
		require.NotEmpty(t, d.BaseCommit)
		found = true
	}
	require.True(t, found, "child.drafted event not found in parent history")
}
