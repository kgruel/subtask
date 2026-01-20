package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/harness"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/testutil"
)

// TestGetTaskProgress tests parsing PROGRESS.json for X/Y display.
func TestGetTaskProgress(t *testing.T) {
	_ = testutil.NewTestEnv(t, 1)

	tests := []struct {
		name     string
		json     string
		expected string
	}{
		{
			name: "all pending",
			json: `[
				{"step": "Step 1", "done": false},
				{"step": "Step 2", "done": false},
				{"step": "Step 3", "done": false}
			]`,
			expected: "0/3",
		},
		{
			name: "some done",
			json: `[
				{"step": "Step 1", "done": true},
				{"step": "Step 2", "done": true},
				{"step": "Step 3", "done": false},
				{"step": "Step 4", "done": false}
			]`,
			expected: "2/4",
		},
		{
			name: "all done",
			json: `[
				{"step": "Step 1", "done": true},
				{"step": "Step 2", "done": true}
			]`,
			expected: "2/2",
		},
		{
			name:     "empty array",
			json:     `[]`,
			expected: "",
		},
		{
			name:     "invalid json",
			json:     `not valid json`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskName := "test/" + strings.ReplaceAll(tt.name, " ", "-")

			// Create task
			tk := &task.Task{
				Name:        taskName,
				Title:       tt.name,
				BaseBranch:  "main",
				Description: "test",
			}
			require.NoError(t, tk.Save())

			// Write PROGRESS.json
			progressPath := task.Dir(taskName) + "/PROGRESS.json"
			require.NoError(t, os.WriteFile(progressPath, []byte(tt.json), 0644))

			steps := task.LoadProgressSteps(taskName)
			done, total := task.CountProgressSteps(steps)
			progress := ""
			if total > 0 {
				progress = fmt.Sprintf("%d/%d", done, total)
			}
			assert.Equal(t, tt.expected, progress)
		})
	}
}

// TestGetTaskProgress_NoFile tests when PROGRESS.json doesn't exist.
func TestGetTaskProgress_NoFile(t *testing.T) {
	_ = testutil.NewTestEnv(t, 1)

	tk := &task.Task{
		Name:        "test/no-progress",
		Title:       "No progress file",
		BaseBranch:  "main",
		Description: "test",
	}
	require.NoError(t, tk.Save())

	steps := task.LoadProgressSteps("test/no-progress")
	done, total := task.CountProgressSteps(steps)
	progress := ""
	if total > 0 {
		progress = fmt.Sprintf("%d/%d", done, total)
	}
	assert.Equal(t, "", progress)
}

// TestSnapshotTaskFiles tests file snapshot functionality.
func TestSnapshotTaskFiles(t *testing.T) {
	_ = testutil.NewTestEnv(t, 1)

	taskName := "test/snapshot"
	tk := &task.Task{
		Name:        taskName,
		Title:       "Snapshot test",
		BaseBranch:  "main",
		Description: "test",
	}
	require.NoError(t, tk.Save())

	taskDir := task.Dir(taskName)

	// Add some files
	require.NoError(t, os.WriteFile(taskDir+"/PLAN.md", []byte("# Plan"), 0644))
	require.NoError(t, os.WriteFile(taskDir+"/PROGRESS.json", []byte("[]"), 0644))

	// Take snapshot
	snapshot := snapshotTaskFilesForTest(taskName)

	// Should include PLAN.md and PROGRESS.json
	assert.Contains(t, snapshot, "PLAN.md")
	assert.Contains(t, snapshot, "PROGRESS.json")

	// Should NOT include TASK.md or history.jsonl
	assert.NotContains(t, snapshot, "TASK.md")
	assert.NotContains(t, snapshot, "history.jsonl")
}

// TestChangedTaskFiles tests detecting file changes.
func TestChangedTaskFiles(t *testing.T) {
	_ = testutil.NewTestEnv(t, 1)

	taskName := "test/changes"
	tk := &task.Task{
		Name:        taskName,
		Title:       "Changes test",
		BaseBranch:  "main",
		Description: "test",
	}
	require.NoError(t, tk.Save())

	taskDir := task.Dir(taskName)

	// Create initial file
	require.NoError(t, os.WriteFile(taskDir+"/PROGRESS.json", []byte("[]"), 0644))

	// Take before snapshot
	before := snapshotTaskFilesForTest(taskName)

	// Wait a bit and modify file
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, os.WriteFile(taskDir+"/PROGRESS.json", []byte(`[{"step":"done","done":true}]`), 0644))

	// Add new file
	require.NoError(t, os.WriteFile(taskDir+"/NOTES.md", []byte("# Notes"), 0644))

	// Take after snapshot
	after := snapshotTaskFilesForTest(taskName)

	// Detect changes
	changed := changedTaskFilesForTest(before, after)

	assert.Contains(t, changed, "PROGRESS.json", "modified file should be detected")
	assert.Contains(t, changed, "NOTES.md", "new file should be detected")
}

// TestBuildPrompt tests the prompt format.
func TestBuildPrompt(t *testing.T) {
	_ = testutil.NewTestEnv(t, 1)

	taskName := "test/prompt"
	tk := &task.Task{
		Name:        taskName,
		Title:       "Prompt test",
		BaseBranch:  "main",
		Description: "Do the thing",
	}
	require.NoError(t, tk.Save())

	// Build prompt
	prompt := harness.BuildPrompt(tk, "/tmp/workspace", false, "Implement as described.", nil)

	// Should have task header
	assert.Contains(t, prompt, "# Task")
	assert.Contains(t, prompt, "Name: test/prompt")
	assert.Contains(t, prompt, "Title: Prompt test")
	assert.Contains(t, prompt, "Branch: test/prompt")
	assert.Contains(t, prompt, "Directory: .subtask/tasks/test--prompt")

	// Should have description
	assert.Contains(t, prompt, "## Description")
	assert.Contains(t, prompt, "Do the thing")

	// Should have separator and prompt
	assert.Contains(t, prompt, "--------------------")
	assert.Contains(t, prompt, "Implement as described.")
}

// TestTaskFolderSymlink tests that task folder is accessible in worktree.
func TestTaskFolderSymlink(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "test/symlink"
	tk := &task.Task{
		Name:        taskName,
		Title:       "Symlink test",
		BaseBranch:  "main",
		Description: "test",
	}
	require.NoError(t, tk.Save())

	// Get absolute path of task dir in main repo
	mainRepoRoot, _ := os.Getwd()
	taskDirAbs := filepath.Join(mainRepoRoot, task.Dir(taskName))

	// Simulate what run.go does: create symlink in worktree
	wsTasksDir := filepath.Join(env.Workspaces[0], ".subtask", "tasks")
	wsTaskDir := filepath.Join(wsTasksDir, task.EscapeName(taskName))
	require.NoError(t, os.MkdirAll(wsTasksDir, 0755))
	require.NoError(t, os.Symlink(taskDirAbs, wsTaskDir))

	// Write a file to main repo task folder
	require.NoError(t, os.WriteFile(taskDirAbs+"/PLAN.md", []byte("# Plan from main"), 0644))

	// Verify it's accessible from worktree via symlink
	content, err := os.ReadFile(wsTaskDir + "/PLAN.md")
	require.NoError(t, err)
	assert.Equal(t, "# Plan from main", string(content))

	// Write from worktree and verify it appears in main repo
	require.NoError(t, os.WriteFile(wsTaskDir+"/PROGRESS.json", []byte("[]"), 0644))

	content, err = os.ReadFile(taskDirAbs + "/PROGRESS.json")
	require.NoError(t, err)
	assert.Equal(t, "[]", string(content))
}

type taskFileSnapshot map[string]time.Time

func snapshotTaskFilesForTest(taskName string) taskFileSnapshot {
	snapshot := make(taskFileSnapshot)
	taskDir := task.Dir(taskName)

	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return snapshot
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if e.Name() == "TASK.md" || e.Name() == "history.jsonl" {
			continue
		}
		path := taskDir + "/" + e.Name()
		info, err := os.Stat(path)
		if err == nil {
			snapshot[e.Name()] = info.ModTime()
		}
	}
	return snapshot
}

func changedTaskFilesForTest(before, after taskFileSnapshot) []string {
	var changed []string
	for name, afterTime := range after {
		beforeTime, existed := before[name]
		if !existed || afterTime.After(beforeTime) {
			changed = append(changed, name)
		}
	}
	return changed
}
