package task

import "time"

// ConversationHeader is derived metadata about a task's activity history.
// It is populated from history events (e.g. worker.session).
type ConversationHeader struct {
	Harness string
	Session string
}

type ConversationRole string

const (
	ConversationRoleLead   ConversationRole = "lead"
	ConversationRoleWorker ConversationRole = "worker"
)

type ConversationMessage struct {
	Role ConversationRole
	Body string
	Time time.Time
}

// ConversationItem represents either a message or a lifecycle event in the timeline.
type ConversationItem struct {
	IsEvent bool
	Message ConversationMessage
	Event   ConversationEvent
}

// ConversationEvent represents a lifecycle event (task opened, stage changed, etc.).
type ConversationEvent struct {
	Type string
	Text string
	Time time.Time
}
