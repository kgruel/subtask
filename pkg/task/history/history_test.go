package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTailPathToleratesEventAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	line := `{"ts":"2026-01-01T00:00:00Z","type":"worker.finished","data":{"run_id":"r1","duration_ms":1234,"tool_calls":2,"outcome":"replied","agent":{"name":"codex-low","adapter":"codex","model":"gpt-5.5"}}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatalf("write history: %v", err)
	}

	tail, err := TailPath(path)
	if err != nil {
		t.Fatalf("TailPath: %v", err)
	}
	if tail.LastRunDurationMS != 1234 {
		t.Fatalf("duration = %d, want 1234", tail.LastRunDurationMS)
	}
	if tail.LastRunToolCalls != 2 {
		t.Fatalf("tool calls = %d, want 2", tail.LastRunToolCalls)
	}
	if tail.LastRunOutcome != "replied" {
		t.Fatalf("outcome = %q, want replied", tail.LastRunOutcome)
	}
	if tail.LastTS != time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("last ts = %s", tail.LastTS)
	}
}
