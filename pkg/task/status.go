package task

import "strings"

// TaskStatus is the durable, syncable status of a task (stored in history.jsonl).
type TaskStatus string

const (
	TaskStatusOpen   TaskStatus = "open"
	TaskStatusMerged TaskStatus = "merged"
	TaskStatusClosed TaskStatus = "closed"
)

// WorkerStatus is the ephemeral status of the local worker process.
type WorkerStatus string

const (
	// WorkerStatusNotStarted means the worker has never been invoked for this task yet.
	// This is represented as the empty string so it can be omitted easily in UIs.
	WorkerStatusNotStarted WorkerStatus = ""

	// WorkerStatusRunning means the worker is currently executing.
	//
	// Note: this used to be serialized as "running". We now use "working" but
	// accept both on read for backwards compatibility.
	WorkerStatusRunning WorkerStatus = "working"
	WorkerStatusReplied WorkerStatus = "replied"
	WorkerStatusError   WorkerStatus = "error"
)

// UserStatus is the simplified status shown to users (derived from task+worker state).
type UserStatus string

const (
	UserStatusDraft   UserStatus = "draft" // open, worker never started
	UserStatusRunning UserStatus = "working"
	UserStatusReplied UserStatus = "replied"
	UserStatusError   UserStatus = "error"
	UserStatusMerged  UserStatus = "merged"
	UserStatusClosed  UserStatus = "closed"
)

func ParseWorkerStatus(s string) WorkerStatus {
	return NormalizeWorkerStatus(WorkerStatus(strings.TrimSpace(s)))
}

// NormalizeWorkerStatus maps legacy serialized values to their current equivalents.
func NormalizeWorkerStatus(ws WorkerStatus) WorkerStatus {
	switch strings.TrimSpace(string(ws)) {
	case "":
		return WorkerStatusNotStarted
	case "idle":
		return WorkerStatusNotStarted
	case "running":
		return WorkerStatusRunning
	case "working":
		return WorkerStatusRunning
	case "replied":
		return WorkerStatusReplied
	case "error":
		return WorkerStatusError
	default:
		return ws
	}
}

// UserStatusFor derives the user-visible status from the internal task+worker state.
func UserStatusFor(ts TaskStatus, ws WorkerStatus) UserStatus {
	switch ts {
	case TaskStatusMerged:
		return UserStatusMerged
	case TaskStatusClosed:
		return UserStatusClosed
	}

	switch NormalizeWorkerStatus(ws) {
	case WorkerStatusRunning:
		return UserStatusRunning
	case WorkerStatusReplied:
		return UserStatusReplied
	case WorkerStatusError:
		return UserStatusError
	default:
		return UserStatusDraft
	}
}
