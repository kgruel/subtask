package history

import "github.com/kgruel/subtask/pkg/task"

// Event type constants for history.jsonl.
const (
	EventTypeTaskOpened       = "task.opened"
	EventTypeTaskMerged       = "task.merged"
	EventTypeTaskClosed       = "task.closed"
	EventTypeStageChanged     = "stage.changed"
	EventTypeWorkerStarted    = "worker.started"
	EventTypeWorkerFinished   = "worker.finished"
	EventTypeReviewStarted    = "review.started"
	EventTypeReviewFinished   = "review.finished"
	EventTypeArtifactProduced = "artifact.produced"
)

// ArtifactProducedData is the JSON payload for an artifact.produced event.
// Name is the filename, Path is relative to the task folder, Kind identifies
// the producing step (e.g. "review").
type ArtifactProducedData struct {
	Name  string     `json:"name"`
	Path  string     `json:"path"`
	Kind  string     `json:"kind"`
	Agent EventAgent `json:"agent,omitempty"`
}

// EventAgent captures who emitted or owned a runtime event at write time.
// Event payloads embed it as a top-level "agent" field.
type EventAgent struct {
	Name      string `json:"name,omitempty"`
	Adapter   string `json:"adapter,omitempty"`
	Model     string `json:"model,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}

// ToAgentView converts event-local identity into the shared render model.
func (a EventAgent) ToAgentView() task.AgentView {
	return task.AgentView{
		Name:      a.Name,
		Adapter:   a.Adapter,
		Model:     a.Model,
		Reasoning: a.Reasoning,
	}
}
