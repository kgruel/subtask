package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/zippoxer/subtask/pkg/git"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/testutil"
)

// gitCmd runs a git command in the given directory.
func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

// TestIntegrationDetection_ManualMerge verifies that a task closed after
// manual merge (git merge) shows as merged.
func TestIntegrationDetection_ManualMerge(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	// Create a task
	env.CreateTask("test/manual", "Manual merge test", "main", "Test manual merge detection")

	// Set up state with workspace
	state := &task.State{
		Workspace:     env.Workspaces[0],
		SupervisorPID: os.Getpid(),
		StartedAt:     time.Now(),
	}
	env.CreateTaskState("test/manual", state)

	// Create the task branch in workspace and make changes
	gitCmd(t, env.Workspaces[0], "checkout", "-b", "test/manual")
	featureFile := filepath.Join(env.Workspaces[0], "feature.txt")
	os.WriteFile(featureFile, []byte("feature content"), 0644)
	gitCmd(t, env.Workspaces[0], "add", "feature.txt")
	gitCmd(t, env.Workspaces[0], "commit", "-m", "Add feature")

	// Manually merge task branch into main (in root repo)
	gitCmd(t, env.RootDir, "merge", "test/manual", "-m", "Merge test/manual")

	// Verify integration detection finds the branch as merged
	target := git.EffectiveTarget(env.Workspaces[0], "main")
	reason := git.IsIntegrated(env.Workspaces[0], "test/manual", target)
	assert.NotEmpty(t, reason, "manual merge should be detected as integrated")
}

// TestIntegrationDetection_SquashMerge verifies that a task closed after
// squash merge (different history) shows as merged.
func TestIntegrationDetection_SquashMerge(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	// Create a task
	env.CreateTask("test/squash", "Squash merge test", "main", "Test squash merge detection")

	// Set up state with workspace
	state := &task.State{
		Workspace:     env.Workspaces[0],
		SupervisorPID: os.Getpid(),
		StartedAt:     time.Now(),
	}
	env.CreateTaskState("test/squash", state)

	// Create the task branch in workspace and make changes
	gitCmd(t, env.Workspaces[0], "checkout", "-b", "test/squash")
	featureFile := filepath.Join(env.Workspaces[0], "feature.txt")
	os.WriteFile(featureFile, []byte("feature content"), 0644)
	gitCmd(t, env.Workspaces[0], "add", "feature.txt")
	gitCmd(t, env.Workspaces[0], "commit", "-m", "Add feature")

	// Squash merge task branch into main (creates different history)
	gitCmd(t, env.RootDir, "merge", "--squash", "test/squash")
	gitCmd(t, env.RootDir, "commit", "-m", "Squash merge test/squash")

	// Verify integration detection finds the branch as merged
	// Should be TreesMatch or MergeAddsNothing
	target := git.EffectiveTarget(env.Workspaces[0], "main")
	reason := git.IsIntegrated(env.Workspaces[0], "test/squash", target)
	assert.NotEmpty(t, reason, "squash merge should be detected as integrated")
}

// TestIntegrationDetection_NotMerged verifies that a task closed without
// merging (abandoned) does NOT show as merged.
func TestIntegrationDetection_NotMerged(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	// Create a task
	env.CreateTask("test/abandon", "Abandoned test", "main", "Test abandoned detection")

	// Set up state with workspace
	state := &task.State{
		Workspace:     env.Workspaces[0],
		SupervisorPID: os.Getpid(),
		StartedAt:     time.Now(),
	}
	env.CreateTaskState("test/abandon", state)

	// Create the task branch in workspace and make changes
	gitCmd(t, env.Workspaces[0], "checkout", "-b", "test/abandon")
	featureFile := filepath.Join(env.Workspaces[0], "feature.txt")
	os.WriteFile(featureFile, []byte("feature content"), 0644)
	gitCmd(t, env.Workspaces[0], "add", "feature.txt")
	gitCmd(t, env.Workspaces[0], "commit", "-m", "Add feature")

	// Verify integration detection does NOT find the branch as merged
	target := git.EffectiveTarget(env.Workspaces[0], "main")
	reason := git.IsIntegrated(env.Workspaces[0], "test/abandon", target)
	assert.Empty(t, reason, "abandoned task should NOT be detected as integrated")
}

// TestIntegrationDetection_CherryPick verifies that a task whose changes
// were cherry-picked (different commits, same content) shows as merged.
func TestIntegrationDetection_CherryPick(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	// Create a task
	env.CreateTask("test/cherry", "Cherry-pick test", "main", "Test cherry-pick detection")

	// Set up state with workspace
	state := &task.State{
		Workspace:     env.Workspaces[0],
		SupervisorPID: os.Getpid(),
		StartedAt:     time.Now(),
	}
	env.CreateTaskState("test/cherry", state)

	// Create the task branch in workspace and make changes
	gitCmd(t, env.Workspaces[0], "checkout", "-b", "test/cherry")
	featureFile := filepath.Join(env.Workspaces[0], "feature.txt")
	os.WriteFile(featureFile, []byte("feature content"), 0644)
	gitCmd(t, env.Workspaces[0], "add", "feature.txt")
	gitCmd(t, env.Workspaces[0], "commit", "-m", "Add feature")

	// Cherry-pick the changes to main (different commit SHA, same content)
	// First, get the commit SHA
	gitCmd(t, env.RootDir, "cherry-pick", "test/cherry")

	// Verify integration detection finds the branch as merged
	// Could be SameCommit (fast-forward) or TreesMatch depending on scenario
	target := git.EffectiveTarget(env.Workspaces[0], "main")
	reason := git.IsIntegrated(env.Workspaces[0], "test/cherry", target)
	assert.NotEmpty(t, reason, "cherry-picked changes should be detected as integrated")
}
