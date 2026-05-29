package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/testutil"
)

// TestDraftCmd_InvalidBranchName_RejectedNoOrphan verifies that an invalid git
// branch name fails at draft time (subtask's boundary) without creating an
// orphan task folder, rather than failing later at first send.
func TestDraftCmd_InvalidBranchName_RejectedNoOrphan(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	bad := "fix bug" // space is not a valid branch name
	err := (&DraftCmd{Task: bad, Title: "x", Description: "y", Base: "main"}).Run()
	require.Error(t, err, "draft should reject an invalid branch name")
	require.Contains(t, err.Error(), "valid git branch name")

	// No task folder should have been written.
	_, loadErr := task.Load(bad)
	require.Error(t, loadErr, "no orphan task should exist after a rejected draft")

	// A valid name still drafts fine.
	require.NoError(t, (&DraftCmd{Task: "fix/valid", Title: "x", Description: "y", Base: "main"}).Run())
}
