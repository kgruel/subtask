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
