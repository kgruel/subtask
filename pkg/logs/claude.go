package logs

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kgruel/subtask/internal/homedir"
)

// ClaudeParser parses Claude Code session JSONL files stored under ~/.claude/projects.
type ClaudeParser struct{}

type claudeLogEvent struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	Version   string `json:"version,omitempty"`

	Message json.RawMessage `json:"message,omitempty"`
}

// FindSessionFile finds the session file for a given session ID.
// Searches ~/.claude/projects/ recursively.
func (p *ClaudeParser) FindSessionFile(sessionID string) (string, error) {
	home, err := homedir.Dir()
	if err != nil {
		return "", err
	}
	projectsDir := filepath.Join(home, ".claude", "projects")

	var found string
	err = filepath.WalkDir(projectsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) == sessionID+".jsonl" {
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

// ParseFile parses a Claude session file and emits entries via callback.
func (p *ClaudeParser) ParseFile(path string, cb func(LogEntry)) (*SessionInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var info *SessionInfo

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var ev claudeLogEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}

		ts := parseTimestampLoose(ev.Timestamp)

		// Initialize session info from first event that contains it.
		if info == nil && ev.SessionID != "" {
			info = &SessionInfo{
				ID:         ev.SessionID,
				StartTime:  ts,
				CWD:        ev.CWD,
				CLIVersion: ev.Version,
			}
			cb(LogEntry{Time: ts, Kind: KindSessionStart, Summary: "Session started"})
		} else if info != nil {
			if info.CWD == "" && ev.CWD != "" {
				info.CWD = ev.CWD
			}
			if info.CLIVersion == "" && ev.Version != "" {
				info.CLIVersion = ev.Version
			}
		}

		switch ev.Type {
		case "user":
			text := extractClaudeMessageText(ev.Message)
			if text != "" {
				cb(LogEntry{Time: ts, Kind: KindUserMessage, Summary: normalizeText(text)})
			}
			// toolUseResult is in the top-level event (not in Message) for some formats,
			// but we keep this simple; most output is sufficiently covered by tool calls.

		case "assistant":
			parts := extractClaudeMessageParts(ev.Message)
			for _, part := range parts {
				switch part.Type {
				case "text":
					if part.Text != "" {
						cb(LogEntry{Time: ts, Kind: KindAgentMessage, Summary: normalizeText(part.Text)})
					}
				case "tool_use":
					summary := formatClaudeToolCall(part.Name, part.Input)
					if summary == "" {
						continue
					}
					cb(LogEntry{
						Time:     ts,
						Kind:     KindToolCall,
						Summary:  summary,
						ToolName: part.Name,
					})
				}
			}
		}
	}

	return info, scanner.Err()
}

func parseTimestampLoose(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

type claudeMsg struct {
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
}

type claudePart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"`
	// tool_use input
	Input json.RawMessage `json:"input,omitempty"`
}

func extractClaudeMessageParts(raw json.RawMessage) []claudePart {
	if len(raw) == 0 {
		return nil
	}
	var m claudeMsg
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	if len(m.Content) == 0 {
		return nil
	}

	// Content may be a string (rare) or an array of parts.
	var text string
	if err := json.Unmarshal(m.Content, &text); err == nil {
		return []claudePart{{Type: "text", Text: text}}
	}

	var parts []claudePart
	if err := json.Unmarshal(m.Content, &parts); err == nil {
		return parts
	}
	return nil
}

func extractClaudeMessageText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m claudeMsg
	if err := json.Unmarshal(raw, &m); err == nil {
		// The "user" event sometimes stores content as a plain string.
		var s string
		if err := json.Unmarshal(m.Content, &s); err == nil {
			return s
		}
		// Or an array of parts.
		var parts []struct {
			Type    string `json:"type"`
			Text    string `json:"text,omitempty"`
			Content string `json:"content,omitempty"`
		}
		if err := json.Unmarshal(m.Content, &parts); err == nil {
			var out []string
			for _, p := range parts {
				if p.Text != "" {
					out = append(out, p.Text)
				} else if p.Content != "" {
					out = append(out, p.Content)
				}
			}
			return strings.Join(out, " ")
		}
	}
	return ""
}

func formatClaudeToolCall(name string, input json.RawMessage) string {
	switch name {
	case "Bash":
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(input, &args); err == nil && args.Command != "" {
			return "$ " + normalizeText(args.Command)
		}
		return "$ (command)"
	case "Write", "Edit", "Read":
		var args struct {
			FilePath string `json:"file_path"`
		}
		_ = json.Unmarshal(input, &args)
		if args.FilePath == "" {
			return name
		}
		verb := strings.ToLower(name)
		return verb + " " + truncatePath(args.FilePath)
	default:
		return name
	}
}
