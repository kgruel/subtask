package task

import (
	"os"
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
