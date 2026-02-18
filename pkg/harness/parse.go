package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

// ParseByName dispatches to a named stream parser.
// Known names: "claude", "codex", "opencode".
func ParseByName(name string, r io.Reader, result *Result, cb Callbacks) error {
	switch name {
	case "claude":
		return parseClaudeStream(r, result, cb)
	case "codex":
		return parseCodexExecJSONL(r, result, cb, codexMaxJSONLLineBytes)
	case "opencode":
		return parseOpenCodeStream(r, result, cb)
	default:
		return fmt.Errorf("unknown parser: %q", name)
	}
}

// ---------------------------------------------------------------------------
// Generic JSONL parser
// ---------------------------------------------------------------------------

// ToolCallRule defines how to detect tool calls in a JSONL stream.
type ToolCallRule struct {
	// Match requires all specified field=value pairs to be present.
	Match map[string]string
}

// GenericJSONLRules configures the generic JSONL parser.
type GenericJSONLRules struct {
	// SessionID is a dot-path (e.g. ".session_id") to extract the session ID.
	SessionID string

	// Reply is a dot-path to extract the reply text.
	Reply string

	// ReplyAccumulate concatenates all matched reply values instead of last-match-wins.
	ReplyAccumulate bool

	// ToolCall defines how to detect tool call events.
	ToolCall *ToolCallRule
}

// ParseGenericJSONL parses a JSONL stream using dot-path extraction rules.
func ParseGenericJSONL(r io.Reader, result *Result, cb Callbacks, rules GenericJSONLRules) error {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	seenSessionStart := false
	var replyBuilder strings.Builder

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var obj map[string]any
		if err := json.Unmarshal(line, &obj); err != nil {
			continue
		}

		// Session ID extraction.
		if rules.SessionID != "" && !seenSessionStart {
			if id := extractDotPath(obj, rules.SessionID); id != "" {
				seenSessionStart = true
				result.SessionID = id
				result.PromptDelivered = true
				if cb.OnSessionStart != nil {
					cb.OnSessionStart(id)
				}
			}
		}

		// Tool call detection.
		if rules.ToolCall != nil && matchFields(obj, rules.ToolCall.Match) {
			if cb.OnToolCall != nil {
				cb.OnToolCall(time.Now())
			}
		}

		// Reply extraction.
		if rules.Reply != "" {
			if text := extractDotPath(obj, rules.Reply); text != "" {
				if rules.ReplyAccumulate {
					replyBuilder.WriteString(text)
					result.Reply = replyBuilder.String()
				} else {
					result.Reply = text
				}
				result.AgentReplied = true
			}
		}
	}

	return scanner.Err()
}

// ---------------------------------------------------------------------------
// Text parser
// ---------------------------------------------------------------------------

// ParseText reads all of r as plain text and stores it as the reply.
func ParseText(r io.Reader, result *Result) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if len(data) > 0 {
		result.Reply = string(data)
		result.AgentReplied = true
	}
	return nil
}

// ---------------------------------------------------------------------------
// Dot-path extraction and field matching
// ---------------------------------------------------------------------------

// extractDotPath traverses a map using a dot-separated path like ".item.text".
// Returns the string value at the path, or "" if not found or not a string.
func extractDotPath(obj map[string]any, path string) string {
	if path == "" {
		return ""
	}
	// Strip leading dot.
	path = strings.TrimPrefix(path, ".")
	if path == "" {
		return ""
	}

	parts := strings.Split(path, ".")
	current := any(obj)

	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = m[part]
		if !ok {
			return ""
		}
	}

	s, ok := current.(string)
	if !ok {
		return ""
	}
	return s
}

// matchFields returns true if every key=value pair in match is present in obj.
// An empty match always returns true.
func matchFields(obj map[string]any, match map[string]string) bool {
	for key, want := range match {
		val, ok := obj[key]
		if !ok {
			return false
		}
		s, ok := val.(string)
		if !ok || s != want {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Claude stream parser (moved from claude.go)
// ---------------------------------------------------------------------------

type claudeStreamEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	SessionID string `json:"session_id,omitempty"`

	// assistant events
	Message *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message,omitempty"`

	// result event
	Result string `json:"result,omitempty"`
}

type claudeMessagePart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`

	// tool_use
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

func parseClaudeStream(r io.Reader, result *Result, cb Callbacks) error {
	scanner := bufio.NewScanner(r)
	// Claude can emit large JSON lines.
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	seenSessionStart := false

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var ev claudeStreamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // ignore non-JSON / noise
		}

		// Session start
		if ev.Type == "system" && ev.Subtype == "init" {
			if ev.SessionID != "" && !seenSessionStart {
				seenSessionStart = true
				result.SessionID = ev.SessionID
				result.PromptDelivered = true
				if cb.OnSessionStart != nil {
					cb.OnSessionStart(ev.SessionID)
				}
			}
			continue
		}

		// Tool call detection
		if ev.Type == "assistant" && ev.Message != nil && len(ev.Message.Content) > 0 {
			var parts []claudeMessagePart
			if err := json.Unmarshal(ev.Message.Content, &parts); err == nil {
				for _, p := range parts {
					if p.Type == "tool_use" {
						if cb.OnToolCall != nil {
							cb.OnToolCall(time.Now())
						}
					}
				}
			}
		}

		// Final result
		if ev.Type == "result" {
			if ev.SessionID != "" && result.SessionID == "" {
				result.SessionID = ev.SessionID
			}
			if ev.Result != "" {
				result.Reply = ev.Result
				result.AgentReplied = true
			}
		}
	}

	return scanner.Err()
}

// ---------------------------------------------------------------------------
// Codex stream parser (moved from codex.go)
// ---------------------------------------------------------------------------

// CodexEvent represents a JSONL event from codex exec --json.
type CodexEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id,omitempty"` // in thread.started
	Message  string `json:"message,omitempty"`   // in error event
	Item     *struct {
		ID      string `json:"id,omitempty"`
		Type    string `json:"type,omitempty"` // command_execution, agent_message, reasoning
		Text    string `json:"text,omitempty"`
		Command string `json:"command,omitempty"`
	} `json:"item,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"` // in turn.failed
}

const codexMaxJSONLLineBytes = 32 * 1024 * 1024 // 32MB

func processCodexJSONLLine(line []byte, result *Result, cb Callbacks) {
	if len(bytes.TrimSpace(line)) == 0 {
		return
	}

	var event CodexEvent
	if err := json.Unmarshal(line, &event); err != nil {
		// Not JSON, skip.
		return
	}

	switch event.Type {
	case "thread.started":
		result.SessionID = event.ThreadID
		result.PromptDelivered = true
		if cb.OnSessionStart != nil {
			cb.OnSessionStart(event.ThreadID)
		}

	case "item.started":
		if event.Item != nil && event.Item.Type == "command_execution" {
			if cb.OnToolCall != nil {
				cb.OnToolCall(time.Now())
			}
		}

	case "item.completed":
		if event.Item != nil && event.Item.Type == "agent_message" {
			result.AgentReplied = true
			// Note: We also read from -o file, but capture here too.
			if event.Item.Text != "" {
				result.Reply = event.Item.Text
			}
		}

	case "error":
		result.Error = event.Message

	case "turn.completed":
		// Codex may emit transient "error" events (e.g. brief network failures)
		// even when the overall turn succeeds. If the turn completed, treat any
		// prior stream error as recovered.
		result.Error = ""
		result.TurnFailed = false

	case "turn.failed":
		if event.Error != nil {
			result.Error = event.Error.Message
		}
		result.TurnFailed = true
	}
}

func parseCodexExecJSONL(r io.Reader, result *Result, cb Callbacks, maxLineBytes int) error {
	// Codex can emit large JSONL lines (reasoning and/or aggregated tool output).
	//
	// bufio.Scanner has a hard token limit; exceeding it can silently stop parsing and can
	// deadlock the process (stdout pipe fills, codex blocks on write, subtask blocks on Wait()).
	//
	// Use bufio.Reader + ReadSlice to keep draining stdout even if a line is unexpectedly huge.
	if maxLineBytes <= 0 {
		maxLineBytes = codexMaxJSONLLineBytes
	}

	br := bufio.NewReaderSize(r, 256*1024)

	var (
		firstErr error

		accum   []byte
		tooLong bool
	)

	for {
		frag, err := br.ReadSlice('\n')
		switch {
		case err == nil:
			if tooLong {
				// Discard the remainder of an overlong line; reset at newline boundary.
				tooLong = false
				accum = accum[:0]
				continue
			}
			if len(accum)+len(frag) > maxLineBytes {
				if firstErr == nil {
					firstErr = fmt.Errorf("codex json stream line exceeded %d bytes", maxLineBytes)
				}
				accum = accum[:0]
				continue
			}
			accum = append(accum, frag...)
			processCodexJSONLLine(bytes.TrimSpace(accum), result, cb)
			accum = accum[:0]
			continue

		case errors.Is(err, bufio.ErrBufferFull):
			if tooLong {
				// Keep draining until newline.
				continue
			}
			if len(accum)+len(frag) > maxLineBytes {
				if firstErr == nil {
					firstErr = fmt.Errorf("codex json stream line exceeded %d bytes", maxLineBytes)
				}
				tooLong = true
				accum = accum[:0]
				continue
			}
			accum = append(accum, frag...)
			continue

		case errors.Is(err, io.EOF):
			// Process any trailing data (may be a partial last line without newline).
			if len(frag) > 0 && !tooLong {
				if len(accum)+len(frag) > maxLineBytes {
					if firstErr == nil {
						firstErr = fmt.Errorf("codex json stream line exceeded %d bytes", maxLineBytes)
					}
				} else {
					accum = append(accum, frag...)
					processCodexJSONLLine(bytes.TrimSpace(accum), result, cb)
				}
			}
			return firstErr

		default:
			// A real read error.
			if firstErr == nil {
				firstErr = err
			}
			return firstErr
		}
	}
}

// ---------------------------------------------------------------------------
// OpenCode stream parser (moved from opencode.go)
// ---------------------------------------------------------------------------

type openCodeStreamEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionID,omitempty"`

	Part *struct {
		ID   string `json:"id,omitempty"`
		Type string `json:"type,omitempty"`
		Text string `json:"text,omitempty"`
	} `json:"part,omitempty"`
}

func parseOpenCodeStream(r io.Reader, result *Result, cb Callbacks) error {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	seenSessionStart := false
	var reply strings.Builder

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var ev openCodeStreamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}

		if ev.SessionID != "" && !seenSessionStart {
			seenSessionStart = true
			result.SessionID = ev.SessionID
			result.PromptDelivered = true
			if cb.OnSessionStart != nil {
				cb.OnSessionStart(ev.SessionID)
			}
		}

		if ev.Type == "tool_use" {
			if cb.OnToolCall != nil {
				cb.OnToolCall(time.Now())
			}
			continue
		}

		if ev.Type == "text" && ev.Part != nil && ev.Part.Text != "" {
			reply.WriteString(ev.Part.Text)
			result.Reply = reply.String()
			result.AgentReplied = result.Reply != ""
		}
	}

	return scanner.Err()
}
