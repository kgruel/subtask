package task

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{65 * time.Second, "1m"},
		{125 * time.Second, "2m"},
		{65 * time.Minute, "1h5m"},
		{120 * time.Minute, "2h"},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, FormatDuration(tt.d))
	}
}

func TestUserStatusText(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	startedAt := now.Add(-5 * time.Minute)

	tests := []struct {
		name      string
		ts        TaskStatus
		ws        WorkerStatus
		startedAt time.Time
		lastRunMS int
		lastError string
		want      string
	}{
		{"merged", TaskStatusMerged, WorkerStatusReplied, time.Time{}, 0, "", "✓ merged"},
		{"closed", TaskStatusClosed, WorkerStatusReplied, time.Time{}, 0, "", "closed"},
		{"working", TaskStatusOpen, WorkerStatusRunning, startedAt, 0, "", "working (5m)"},
		{"replied", TaskStatusOpen, WorkerStatusReplied, time.Time{}, 120000, "", "replied (2m)"},
		{"error", TaskStatusOpen, WorkerStatusError, time.Time{}, 60000, "oops", "error (1m)"},
		{"interrupted", TaskStatusOpen, WorkerStatusError, time.Time{}, 30000, "interrupted", "interrupted (30s)"},
		{"draft", TaskStatusOpen, WorkerStatusNotStarted, time.Time{}, 0, "", "draft"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UserStatusText(tt.ts, tt.ws, tt.startedAt, tt.lastRunMS, tt.lastError, now)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLoadReviewSummary(t *testing.T) {
	tmp := t.TempDir()
	initGitRepo(t, tmp)

	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(orig) })

	resetProjectCache()

	taskName := "test-task"
	reviewDir := filepath.Join(tmp, ".subtask", "tasks", taskName, "reviews")
	err := os.MkdirAll(reviewDir, 0755)
	require.NoError(t, err)

	// Create some review files
	files := []string{
		"20240101T120000Z-run1-code-openai.md",
		"20240101T130000Z-run2-style-anthropic.md",
	}
	for _, f := range files {
		err = os.WriteFile(filepath.Join(reviewDir, f), []byte("content"), 0644)
		require.NoError(t, err)
	}

	summary := LoadReviewSummary(taskName)
	require.NotNil(t, summary)
	require.Equal(t, 2, summary.Count)
	require.Equal(t, "style", summary.LastKind)
	require.Equal(t, "anthropic", summary.LastAdapter)
}

func TestAgentView_Label(t *testing.T) {
	tests := []struct {
		name  string
		agent AgentView
		want  string
	}{
		{
			name: "named with adapter and model",
			agent: AgentView{
				Name:    "opus-planner",
				Adapter: "claude",
				Model:   "opus",
			},
			want: "opus-planner (claude/opus)",
		},
		{
			name: "named only",
			agent: AgentView{
				Name: "opus-planner",
			},
			want: "opus-planner",
		},
		{
			name: "named and model only",
			agent: AgentView{
				Name:  "opus-planner",
				Model: "opus",
			},
			want: "opus-planner (opus)",
		},
		{
			name: "no-name with adapter and model",
			agent: AgentView{
				Adapter: "claude",
				Model:   "sonnet",
			},
			want: "claude/sonnet (no named agent)",
		},
		{
			name: "no-name with model only",
			agent: AgentView{
				Model: "sonnet",
			},
			want: "sonnet (no named agent)",
		},
		{
			name:  "all empty",
			agent: AgentView{},
			want:  "Worker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.agent.Label())
		})
	}
}
