// Package logs provides session log parsing for AI agents (Codex, Claude).
package logs

import (
	"time"
)

// EntryKind categorizes log entries for display.
type EntryKind string

const (
	KindUserMessage  EntryKind = "user"
	KindAgentMessage EntryKind = "agent"
	KindReasoning    EntryKind = "reasoning"
	KindToolCall     EntryKind = "tool_call"
	KindToolOutput   EntryKind = "tool_output"
	KindError        EntryKind = "error"
	KindSessionStart EntryKind = "session_start"
	KindTurnContext  EntryKind = "turn_context"
)

// LogEntry is a normalized log entry for display.
type LogEntry struct {
	Time    time.Time
	Kind    EntryKind
	Summary string // Short, truncated summary for display

	// Tool-specific fields
	ToolName   string
	ToolCallID string
	ExitCode   *int // For command outputs
}

// SessionInfo holds metadata about a session.
type SessionInfo struct {
	ID         string
	StartTime  time.Time
	Model      string
	CWD        string
	CLIVersion string
}

// Parser can parse session logs from a specific harness.
type Parser interface {
	// ParseFile parses a session file and emits entries via the callback.
	// Streams efficiently without loading everything into memory.
	// Returns SessionInfo if found.
	ParseFile(path string, cb func(LogEntry)) (*SessionInfo, error)
}

// Locator can find the session file for a given session ID.
type Locator interface {
	FindSessionFile(sessionID string) (string, error)
}

// TruncateLimit is the default character limit for truncation.
// When NoTrunc is true, this limit is not applied.
const TruncateLimit = 200
