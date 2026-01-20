package logs

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zippoxer/subtask/internal/homedir"
)

// codexEvent represents a raw Codex JSONL event.
type codexEvent struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// codexSessionMeta is the payload of session_meta events.
type codexSessionMeta struct {
	ID         string `json:"id"`
	CWD        string `json:"cwd"`
	CLIVersion string `json:"cli_version"`
	Model      string `json:"model,omitempty"`
}

// codexTurnContext is the payload of turn_context events.
type codexTurnContext struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
	CWD    string `json:"cwd"`
}

// codexResponseItem is the payload of response_item events.
type codexResponseItem struct {
	Type      string          `json:"type"`
	Name      string          `json:"name,omitempty"`      // function_call
	Arguments string          `json:"arguments,omitempty"` // function_call (JSON string)
	CallID    string          `json:"call_id,omitempty"`   // function_call / function_call_output
	Output    string          `json:"output,omitempty"`    // function_call_output
	Summary   string          `json:"summary,omitempty"`   // reasoning
	Role      string          `json:"role,omitempty"`      // message
	Content   json.RawMessage `json:"content,omitempty"`   // message
}

// codexEventMsg is the payload of event_msg events.
type codexEventMsg struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
	Text    string `json:"text,omitempty"`
}

// CodexParser parses Codex session JSONL files.
type CodexParser struct{}

// FindSessionFile finds the session file for a given session ID.
// Searches ~/.codex/sessions/ recursively.
func (p *CodexParser) FindSessionFile(sessionID string) (string, error) {
	home, err := homedir.Dir()
	if err != nil {
		return "", err
	}

	sessionsDir := filepath.Join(home, ".codex", "sessions")

	var found string
	err = filepath.WalkDir(sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		// Check if session ID is in the filename
		if strings.Contains(filepath.Base(path), sessionID) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return "", err
	}

	if found == "" {
		return "", os.ErrNotExist
	}
	return found, nil
}

// ParseFile parses a Codex session file and emits entries via callback.
func (p *CodexParser) ParseFile(path string, cb func(LogEntry)) (*SessionInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var info *SessionInfo
	scanner := bufio.NewScanner(f)
	// Increase buffer for large lines (some events have huge payloads)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event codexEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue // Skip malformed lines
		}

		ts := parseTimestamp(event.Timestamp)

		switch event.Type {
		case "session_meta":
			var meta codexSessionMeta
			if err := json.Unmarshal(event.Payload, &meta); err == nil {
				info = &SessionInfo{
					ID:         meta.ID,
					StartTime:  ts,
					CWD:        meta.CWD,
					CLIVersion: meta.CLIVersion,
					Model:      meta.Model,
				}
				cb(LogEntry{
					Time:    ts,
					Kind:    KindSessionStart,
					Summary: "Session started",
				})
			}

		case "turn_context":
			var ctx codexTurnContext
			if err := json.Unmarshal(event.Payload, &ctx); err == nil {
				if info != nil && ctx.Model != "" {
					info.Model = ctx.Model
				}
			}

		case "response_item":
			var item codexResponseItem
			if err := json.Unmarshal(event.Payload, &item); err != nil {
				continue
			}

			switch item.Type {
			case "function_call":
				summary := formatToolCall(item.Name, item.Arguments)
				if summary == "" {
					continue // Skip noisy/uninteresting tool calls
				}
				cb(LogEntry{
					Time:       ts,
					Kind:       KindToolCall,
					Summary:    summary,
					ToolName:   item.Name,
					ToolCallID: item.CallID,
				})

			case "function_call_output":
				summary, exitCode := formatToolOutput(item.Output)
				cb(LogEntry{
					Time:       ts,
					Kind:       KindToolOutput,
					Summary:    summary,
					ToolCallID: item.CallID,
					ExitCode:   exitCode,
				})

			case "message":
				if item.Role == "user" || item.Role == "assistant" {
					text := extractMessageText(item.Content)
					if text != "" {
						kind := KindUserMessage
						if item.Role == "assistant" {
							kind = KindAgentMessage
						}
						cb(LogEntry{
							Time:    ts,
							Kind:    kind,
							Summary: normalizeText(text),
						})
					}
				}

			case "reasoning":
				if item.Summary != "" {
					cb(LogEntry{
						Time:    ts,
						Kind:    KindReasoning,
						Summary: normalizeText(item.Summary),
					})
				}
			}

		case "event_msg":
			var msg codexEventMsg
			if err := json.Unmarshal(event.Payload, &msg); err != nil {
				continue
			}

			switch msg.Type {
			case "user_message":
				if msg.Message != "" {
					cb(LogEntry{
						Time:    ts,
						Kind:    KindUserMessage,
						Summary: normalizeText(msg.Message),
					})
				}
			case "agent_message":
				if msg.Message != "" {
					cb(LogEntry{
						Time:    ts,
						Kind:    KindAgentMessage,
						Summary: normalizeText(msg.Message),
					})
				}
			case "agent_reasoning":
				if msg.Text != "" {
					cb(LogEntry{
						Time:    ts,
						Kind:    KindReasoning,
						Summary: normalizeText(msg.Text),
					})
				}
			}

		case "error":
			var msg codexEventMsg
			if err := json.Unmarshal(event.Payload, &msg); err == nil && msg.Message != "" {
				cb(LogEntry{
					Time:    ts,
					Kind:    KindError,
					Summary: msg.Message,
				})
			}
		}
	}

	return info, scanner.Err()
}

func parseTimestamp(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

func formatToolCall(name, argsJSON string) string {
	// Parse arguments to extract the important bits
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return name
	}

	switch name {
	case "shell", "exec_command":
		if cmd, ok := args["command"]; ok {
			return formatShellCommand(cmd)
		}
		// Try "cmd" as alternative key
		if cmd, ok := args["cmd"]; ok {
			return formatShellCommand(cmd)
		}
	case "apply_patch":
		if input, ok := args["input"].(string); ok {
			return formatPatch(input)
		}
	case "read_file":
		if path, ok := args["path"].(string); ok {
			return "read " + truncatePath(path)
		}
	case "write_file":
		if path, ok := args["path"].(string); ok {
			return "write " + truncatePath(path)
		}
	case "list_dir":
		if path, ok := args["path"].(string); ok {
			return "ls " + truncatePath(path)
		}
	case "write_stdin":
		// These are typically just continuation signals or input to interactive processes
		// They're noisy and not very informative - skip them
		return ""
	}

	// Generic fallback - show name
	return name
}

func formatShellCommand(cmd any) string {
	switch v := cmd.(type) {
	case string:
		return "$ " + normalizeText(v)
	case []any:
		if len(v) >= 3 {
			// ["bash", "-lc", "actual command"]
			if actual, ok := v[len(v)-1].(string); ok {
				return "$ " + normalizeText(actual)
			}
		}
		// Just show first element
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				return "$ " + s
			}
		}
	}
	return "$ (command)"
}

func formatPatch(input string) string {
	// Count files in patch
	files := 0
	for _, line := range strings.Split(input, "\n") {
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			files++
		}
	}
	files = files / 2
	if files == 0 {
		files = 1
	}
	if files == 1 {
		return "patch (1 file)"
	}
	return "patch (" + itoa(files) + " files)"
}

func formatToolOutput(output string) (string, *int) {
	// Try to parse as JSON with metadata
	var result struct {
		Output   string `json:"output"`
		Metadata struct {
			ExitCode int `json:"exit_code"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(output), &result); err == nil {
		exitCode := result.Metadata.ExitCode
		summary := firstLine(result.Output)
		// Filter out unhelpful internal metadata
		if isUnhelpfulOutput(summary) {
			summary = ""
		}
		return summary, &exitCode
	}

	// Plain output - also filter unhelpful content
	summary := firstLine(output)
	if isUnhelpfulOutput(summary) {
		return "", nil
	}
	return summary, nil
}

// isUnhelpfulOutput returns true if the output line is internal metadata
// that doesn't help understand what happened.
func isUnhelpfulOutput(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	// Codex internal chunk IDs
	if strings.HasPrefix(s, "Chunk ID:") {
		return true
	}
	// Other internal prefixes to filter
	if strings.HasPrefix(s, "chunk_id:") {
		return true
	}
	return false
}

func extractMessageText(content json.RawMessage) string {
	// Content can be a string or array of parts
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		return text
	}

	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &parts); err == nil {
		for _, p := range parts {
			if p.Type == "input_text" || p.Type == "text" {
				if text != "" {
					text += " "
				}
				text += p.Text
			}
		}
	}
	return text
}

// normalizeText collapses whitespace in a string.
func normalizeText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncatePath(path string) string {
	if len(path) <= 50 {
		return path
	}
	// Show last 47 chars
	return "..." + path[len(path)-47:]
}

func firstLine(s string) string {
	if idx := strings.Index(s, "\n"); idx != -1 {
		s = s[:idx]
	}
	return normalizeText(s)
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
