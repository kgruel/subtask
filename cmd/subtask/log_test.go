package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zippoxer/subtask/pkg/task/history"
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
