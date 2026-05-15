package task

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkerLabel(t *testing.T) {
	tests := []struct {
		name      string
		stepAgent string
		taskAgent string
		adapter   string
		model     string
		want      string
	}{
		// Level 1: step agent wins over everything
		{
			name:      "step agent with adapter/model",
			stepAgent: "opus-planner",
			taskAgent: "sonnet-coder",
			adapter:   "claude",
			model:     "opus",
			want:      "opus-planner (claude/opus)",
		},
		{
			name:      "step agent only",
			stepAgent: "opus-planner",
			want:      "opus-planner",
		},
		// Level 2: task snapshot agent
		{
			name:      "task agent with adapter/model",
			taskAgent: "sonnet-coder",
			adapter:   "claude",
			model:     "sonnet",
			want:      "sonnet-coder (claude/sonnet)",
		},
		{
			name:      "task agent only",
			taskAgent: "sonnet-coder",
			want:      "sonnet-coder",
		},
		// Level 3: adapter/model
		{
			name:    "adapter and model",
			adapter: "codex",
			model:   "gpt-5.2",
			want:    "codex/gpt-5.2 (no named agent)",
		},
		{
			name:  "model only no adapter",
			model: "gpt-5.2",
			want:  "gpt-5.2 (no named agent)",
		},
		// Level 4: fallback sentinel
		{
			name: "all empty",
			want: "Worker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WorkerLabel(tt.stepAgent, tt.taskAgent, tt.adapter, tt.model)
			require.Equal(t, tt.want, got)
		})
	}
}
