package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kgruel/subtask/pkg/task/history"
)

func TestFormatHistoryEvent_WorkerFinishedIncludesErrorMessage(t *testing.T) {
	data, err := json.Marshal(map[string]any{
		"run_id":        "abc123",
		"outcome":       "error",
		"error_message": "boom: something failed",
		"tool_calls":    3,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := formatHistoryEvent(history.Event{
		TS:   time.Date(2026, 1, 17, 12, 0, 0, 0, time.UTC),
		Type: "worker.finished",
		Data: data,
	})
	if !strings.Contains(got, "outcome=error") {
		t.Fatalf("expected outcome in %q", got)
	}
	if !strings.Contains(got, `error="boom: something failed"`) {
		t.Fatalf("expected error_message in %q", got)
	}
}

func TestFormatHistoryEvent_WorkerEventsIncludeAgent(t *testing.T) {
	agent := history.EventAgent{Name: "codex-low", Adapter: "codex", Model: "gpt-5.5"}
	startData, err := json.Marshal(map[string]any{
		"run_id": "abc123",
		"agent":  agent,
	})
	if err != nil {
		t.Fatalf("marshal start: %v", err)
	}
	finishData, err := json.Marshal(map[string]any{
		"run_id":      "abc123",
		"outcome":     "replied",
		"duration_ms": 1500,
		"tool_calls":  2,
		"agent":       agent,
	})
	if err != nil {
		t.Fatalf("marshal finish: %v", err)
	}

	started := formatHistoryEvent(history.Event{Type: "worker.started", Data: startData})
	if !strings.Contains(started, "agent=codex-low (codex/gpt-5.5)") {
		t.Fatalf("expected agent in %q", started)
	}
	finished := formatHistoryEvent(history.Event{Type: "worker.finished", Data: finishData})
	if !strings.Contains(finished, "outcome=replied agent=codex-low (codex/gpt-5.5)") {
		t.Fatalf("expected agent after outcome in %q", finished)
	}
}

func TestFormatHistoryEvent_EmptyMessageShowsPlaceholder(t *testing.T) {
	got := formatHistoryEvent(history.Event{
		TS:      time.Date(2026, 1, 17, 12, 0, 0, 0, time.UTC),
		Type:    "message",
		Role:    "worker",
		Content: "",
	})
	if !strings.Contains(got, "(empty)") {
		t.Fatalf("expected placeholder in %q", got)
	}
}

func TestFormatHistoryEvent_WorkerMessageIncludesAgentNameOnly(t *testing.T) {
	data, err := json.Marshal(map[string]any{
		"agent": history.EventAgent{Name: "codex-low", Adapter: "codex", Model: "gpt-5.5"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := formatHistoryEvent(history.Event{
		Type:    "message",
		Role:    "worker",
		Content: "done",
		Data:    data,
	})
	if !strings.Contains(got, "worker (codex-low): done") {
		t.Fatalf("expected named worker in %q", got)
	}
	if strings.Contains(got, "codex/gpt-5.5") {
		t.Fatalf("did not expect full label on worker message: %q", got)
	}
}

func TestFormatHistoryEvent_ReviewStarted(t *testing.T) {
	data, err := json.Marshal(map[string]any{
		"run_id":  "abc123",
		"kind":    "diff",
		"adapter": "builtin-mock",
		"model":   "gpt-5.2",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := formatHistoryEvent(history.Event{
		TS:   time.Date(2026, 1, 17, 12, 0, 0, 0, time.UTC),
		Type: "review.started",
		Data: data,
	})
	if !strings.Contains(got, "review started") {
		t.Fatalf("expected 'review started' in %q", got)
	}
	if !strings.Contains(got, "run=abc123") {
		t.Fatalf("expected run= in %q", got)
	}
	if !strings.Contains(got, "kind=diff") {
		t.Fatalf("expected kind= in %q", got)
	}
	if !strings.Contains(got, "adapter=builtin-mock") {
		t.Fatalf("expected adapter= in %q", got)
	}
}

func TestFormatHistoryEvent_ReviewStartedWithAgentKeepsMachineTokens(t *testing.T) {
	data, err := json.Marshal(map[string]any{
		"run_id":    "abc123",
		"kind":      "diff",
		"adapter":   "codex",
		"model":     "gpt-5.5",
		"reasoning": "high",
		"agent":     history.EventAgent{Name: "codex-high", Adapter: "codex", Model: "gpt-5.5", Reasoning: "high"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := formatHistoryEvent(history.Event{Type: "review.started", Data: data})
	for _, want := range []string{
		"agent=codex-high (codex/gpt-5.5)",
		"adapter=codex",
		"model=gpt-5.5",
		"reasoning=high",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
}

func TestFormatHistoryEvent_ReviewFinished_Success(t *testing.T) {
	data, err := json.Marshal(map[string]any{
		"run_id":      "abc123",
		"kind":        "diff",
		"outcome":     "success",
		"duration_ms": 1500,
		"file":        "reviews/20260117T120000Z-diff-builtin-mock.md",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := formatHistoryEvent(history.Event{
		TS:   time.Date(2026, 1, 17, 12, 0, 0, 0, time.UTC),
		Type: "review.finished",
		Data: data,
	})
	if !strings.Contains(got, "review finished") {
		t.Fatalf("expected 'review finished' in %q", got)
	}
	if !strings.Contains(got, "kind=diff") {
		t.Fatalf("expected kind= in %q", got)
	}
	if !strings.Contains(got, "outcome=success") {
		t.Fatalf("expected outcome= in %q", got)
	}
	if !strings.Contains(got, "duration=") {
		t.Fatalf("expected duration= in %q", got)
	}
	if !strings.Contains(got, "file=reviews/") {
		t.Fatalf("expected file= in %q", got)
	}
}

func TestFormatHistoryEvent_ReviewFinished_Error(t *testing.T) {
	data, err := json.Marshal(map[string]any{
		"run_id":  "abc123",
		"kind":    "plan",
		"outcome": "error",
		"error":   "harness failed: exit 1",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := formatHistoryEvent(history.Event{
		TS:   time.Date(2026, 1, 17, 12, 0, 0, 0, time.UTC),
		Type: "review.finished",
		Data: data,
	})
	if !strings.Contains(got, "review finished") {
		t.Fatalf("expected 'review finished' in %q", got)
	}
	if !strings.Contains(got, "kind=plan") {
		t.Fatalf("expected kind= in %q", got)
	}
	if !strings.Contains(got, "outcome=error") {
		t.Fatalf("expected outcome= in %q", got)
	}
	if !strings.Contains(got, `error="harness failed: exit 1"`) {
		t.Fatalf("expected error= in %q", got)
	}
}

func TestFormatHistoryEvent_ArtifactProduced(t *testing.T) {
	data, err := json.Marshal(map[string]any{
		"kind":  "review",
		"path":  "reviews/review.md",
		"agent": history.EventAgent{Name: "codex-high", Adapter: "codex", Model: "gpt-5.5"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := formatHistoryEvent(history.Event{Type: "artifact.produced", Data: data})
	for _, want := range []string{
		"artifact produced",
		"kind=review",
		"path=reviews/review.md",
		"by=codex-high (codex/gpt-5.5)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
}
