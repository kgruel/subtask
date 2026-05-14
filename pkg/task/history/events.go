package history

// Event type constants for history.jsonl.
const (
	EventTypeTaskOpened    = "task.opened"
	EventTypeTaskMerged    = "task.merged"
	EventTypeTaskClosed    = "task.closed"
	EventTypeStageChanged  = "stage.changed"
	EventTypeWorkerStarted = "worker.started"
	EventTypeWorkerFinished = "worker.finished"
	EventTypeReviewStarted  = "review.started"
	EventTypeReviewFinished = "review.finished"
	EventTypeArtifactProduced = "artifact.produced"
)

// ArtifactProducedData is the JSON payload for an artifact.produced event.
// Name is the filename, Path is relative to the task folder, Kind identifies
// the producing step (e.g. "review").
type ArtifactProducedData struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"`
}
