package migrate

import (
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/kgruel/subtask/pkg/git"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/workflow"
)

const CurrentSchema = 1

type legacyState struct {
	Workspace       string    `json:"workspace,omitempty"`
	SessionID       string    `json:"session_id,omitempty"`
	Harness         string    `json:"harness,omitempty"`
	SupervisorPID   int       `json:"supervisor_pid,omitempty"`
	Status          string    `json:"status,omitempty"`
	Stage           string    `json:"stage,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	Merged          bool      `json:"merged,omitempty"`
	PromptDelivered bool      `json:"prompt_delivered,omitempty"`
	AgentReplied    bool      `json:"agent_replied,omitempty"`
}

func EnsureSchema(taskName string) error {
	t, err := task.Load(taskName)
	if err != nil {
		return err
	}
	if t.Schema >= CurrentSchema {
		return nil
	}

	// If history already exists, prefer it and just bump schema.
	if st, err := os.Stat(task.HistoryPath(taskName)); err == nil && st.Size() > 0 {
		t.Schema = CurrentSchema
		return t.Save()
	}

	events, err := buildSchema1History(taskName, t)
	if err != nil {
		return err
	}
	if err := history.WriteAll(taskName, events); err != nil {
		return err
	}

	t.Schema = CurrentSchema
	return t.Save()
}

func buildSchema1History(taskName string, t *task.Task) ([]history.Event, error) {
	openTS := fileModTimeUTC(task.Path(taskName))
	if openTS.IsZero() {
		openTS = time.Now().UTC()
	}

	var legacy *legacyState
	if b, err := os.ReadFile(task.StatePath(taskName)); err == nil {
		var st legacyState
		if err := json.Unmarshal(b, &st); err == nil {
			legacy = &st
		}
	}

	stageTo := ""
	if legacy != nil {
		stageTo = strings.TrimSpace(legacy.Stage)
	}
	if stageTo == "" {
		if wf, err := workflow.LoadFromTask(taskName); err == nil && wf != nil {
			stageTo = wf.FirstStage()
		}
	}
	if stageTo == "" {
		stageTo = "implement"
	}

	openData, _ := json.Marshal(map[string]any{
		"reason":      "draft",
		"branch":      taskName,
		"base_branch": t.BaseBranch,
		"title":       t.Title,
		"follow_up":   t.FollowUp,
		"model":       t.Model,
		"reasoning":   t.Reasoning,
	})

	stageData, _ := json.Marshal(map[string]any{
		"from": "",
		"to":   stageTo,
	})

	out := []history.Event{
		{TS: openTS, Type: "task.opened", Data: openData},
		{TS: openTS, Type: "stage.changed", Data: stageData},
	}

	// Best-effort session event (from legacy state).
	if legacy != nil && (strings.TrimSpace(legacy.Harness) != "" || strings.TrimSpace(legacy.SessionID) != "") {
		sessionTS := openTS
		if !legacy.StartedAt.IsZero() {
			sessionTS = legacy.StartedAt.UTC()
		} else if stTS := fileModTimeUTC(task.StatePath(taskName)); !stTS.IsZero() {
			sessionTS = stTS
		}
		data, _ := json.Marshal(map[string]any{
			"action":     "started",
			"harness":    strings.TrimSpace(legacy.Harness),
			"session_id": strings.TrimSpace(legacy.SessionID),
		})
		out = append(out, history.Event{TS: sessionTS, Type: "worker.session", Data: data})
	}

	// Close/merge inference from legacy state.
	if legacy != nil {
		closeTS := fileModTimeUTC(task.StatePath(taskName))
		if closeTS.IsZero() {
			closeTS = lastEventTime(out)
		}

		switch strings.TrimSpace(legacy.Status) {
		case "closed":
			if legacy.Merged {
				commit := inferMergedCommit(taskName)
				data, _ := json.Marshal(map[string]any{
					"commit":    commit,
					"into":      t.BaseBranch,
					"branch":    taskName,
					"inferred":  commit == "",
					"inference": "legacy state indicates merged",
				})
				out = append(out, history.Event{TS: closeTS, Type: "task.merged", Data: data})
			} else {
				data, _ := json.Marshal(map[string]any{"reason": "close"})
				out = append(out, history.Event{TS: closeTS, Type: "task.closed", Data: data})
			}
		case "error":
			if strings.TrimSpace(legacy.LastError) != "" {
				errMsg := strings.TrimSpace(legacy.LastError)
				data, _ := json.Marshal(map[string]any{
					"run_id":        "migrated",
					"outcome":       "error",
					"error":         errMsg,
					"error_message": errMsg,
					"inferred":      true,
					"tool_calls":    0,
				})
				out = append(out, history.Event{TS: closeTS, Type: "worker.finished", Data: data})
			}
		}
	}

	// Ensure chronological order for stable rendering.
	sortEventsStable(out)
	return out, nil
}

func inferMergedCommit(taskName string) string {
	repoDir := "."
	out, err := git.Output(repoDir, "log", "--grep", "Subtask-Task: "+taskName, "--format=%H", "-n", "1")
	if err == nil {
		return strings.TrimSpace(out)
	}
	return ""
}

func fileModTimeUTC(path string) time.Time {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime().UTC()
}

func lastEventTime(events []history.Event) time.Time {
	var last time.Time
	for _, e := range events {
		if e.TS.After(last) {
			last = e.TS
		}
	}
	if last.IsZero() {
		last = time.Now().UTC()
	}
	return last
}

func sortEventsStable(events []history.Event) {
	// tiny N; simple stable sort
	for i := 0; i < len(events); i++ {
		for j := i + 1; j < len(events); j++ {
			if events[j].TS.Before(events[i].TS) {
				events[i], events[j] = events[j], events[i]
			}
		}
	}
	// Ensure strictly stable order for equal timestamps by nudging with 1ns.
	for i := 1; i < len(events); i++ {
		if events[i].TS.Equal(events[i-1].TS) {
			events[i].TS = events[i].TS.Add(time.Nanosecond)
		}
	}
}
