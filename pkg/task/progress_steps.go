package task

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ProgressStep represents a step in PROGRESS.json (task folder).
type ProgressStep struct {
	Step string `json:"step"`
	Done bool   `json:"done"`
}

// LoadProgressSteps reads .subtask/tasks/<task>/PROGRESS.json.
//
// This is best-effort and returns nil on missing/invalid files, matching the
// CLI's historical behavior (progress is informational).
func LoadProgressSteps(taskName string) []ProgressStep {
	path := filepath.Join(Dir(taskName), "PROGRESS.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var steps []ProgressStep
	if err := json.Unmarshal(data, &steps); err != nil {
		return nil
	}
	return steps
}

// CountProgressSteps returns (done,total) for a set of steps.
func CountProgressSteps(steps []ProgressStep) (done, total int) {
	total = len(steps)
	for _, s := range steps {
		if s.Done {
			done++
		}
	}
	return done, total
}
