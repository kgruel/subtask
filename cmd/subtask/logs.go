package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kgruel/subtask/pkg/logs"
	"github.com/kgruel/subtask/pkg/render"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/store"
	"github.com/kgruel/subtask/pkg/workspace"
)

// LogsCmd implements 'subtask logs'.
type LogsCmd struct {
	TaskOrSession string `arg:"" help:"Task name or session ID"`
	Limit         int    `short:"n" help:"Show only the last N entries" default:"0"`
	Since         string `help:"Show entries since duration or timestamp (e.g., '5m', '1h', '1d', '2024-01-01T10:00:00Z')"`
	Follow        bool   `short:"f" help:"Follow log output (stream new entries)"`
	Timestamps    bool   `short:"t" help:"Show timestamps"`
	NoTrunc       bool   `help:"Don't truncate output"`
}

type harnessLogBackend struct {
	name    string
	parser  logs.Parser
	locator logs.Locator
}

// Run executes the logs command.
func (c *LogsCmd) Run() error {
	res, err := preflightProject()
	if err != nil {
		return err
	}

	backends := []harnessLogBackend{
		{name: "codex", parser: &logs.CodexParser{}, locator: &logs.CodexParser{}},
		{name: "claude", parser: &logs.ClaudeParser{}, locator: &logs.ClaudeParser{}},
	}

	// Try to find session file - could be task name or session ID
	sessionFile, backend, sessionID, taskName, err := c.resolveSession(backends)
	if err != nil {
		return err
	}
	_ = sessionID // Available for future use (e.g., header display)

	// Parse since filter
	var sinceTime time.Time
	if c.Since != "" {
		sinceTime, err = parseSince(c.Since)
		if err != nil {
			return fmt.Errorf("invalid --since value: %w", err)
		}
	}

	// Set up formatter
	formatter := &logs.Formatter{
		Pretty:     render.Pretty,
		Timestamps: c.Timestamps,
		NoTrunc:    c.NoTrunc,
	}

	if c.Follow {
		if taskName != "" {
			if err := printTraceHeader(taskName, res.Config); err != nil {
				return err
			}
		}
		return c.streamLogs(sessionFile, backend, formatter, sinceTime)
	}

	if taskName != "" {
		if err := printTraceHeader(taskName, res.Config); err != nil {
			return err
		}
	}
	return c.showLogs(sessionFile, backend, formatter, sinceTime)
}

// showLogs displays logs from the session file.
func (c *LogsCmd) showLogs(path string, backend harnessLogBackend, formatter *logs.Formatter, since time.Time) error {
	// Collect entries (for limit support we need to buffer)
	var entries []logs.LogEntry
	var sessionInfo *logs.SessionInfo

	info, err := backend.parser.ParseFile(path, func(e logs.LogEntry) {
		// Apply since filter
		if !since.IsZero() && e.Time.Before(since) {
			return
		}
		entries = append(entries, e)
	})
	if err != nil {
		return err
	}
	sessionInfo = info

	// Apply limit (take last N)
	if c.Limit > 0 && len(entries) > c.Limit {
		entries = entries[len(entries)-c.Limit:]
	}

	// Set start time for relative timestamps
	if sessionInfo != nil {
		formatter.StartTime = sessionInfo.StartTime
	} else if len(entries) > 0 {
		formatter.StartTime = entries[0].Time
	}

	// Print entries
	for _, e := range entries {
		line := formatter.Format(e)
		if line == "" {
			continue // Skip empty formatted lines
		}
		fmt.Println(line)
	}

	return nil
}

// streamLogs follows the log file for new entries.
func (c *LogsCmd) streamLogs(path string, backend harnessLogBackend, formatter *logs.Formatter, since time.Time) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// First, show existing entries (respecting limit and since)
	var entries []logs.LogEntry
	var sessionInfo *logs.SessionInfo

	info, err := backend.parser.ParseFile(path, func(e logs.LogEntry) {
		if !since.IsZero() && e.Time.Before(since) {
			return
		}
		entries = append(entries, e)
	})
	if err != nil {
		return err
	}
	sessionInfo = info

	// Apply limit to initial display
	if c.Limit > 0 && len(entries) > c.Limit {
		entries = entries[len(entries)-c.Limit:]
	}

	// Set start time
	if sessionInfo != nil {
		formatter.StartTime = sessionInfo.StartTime
	} else if len(entries) > 0 {
		formatter.StartTime = entries[0].Time
	}

	// Print initial entries
	for _, e := range entries {
		line := formatter.Format(e)
		if line == "" {
			continue
		}
		fmt.Println(line)
	}

	// Now tail the file for new entries
	// Seek to end
	f.Seek(0, io.SeekEnd)

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	// Match the largest parser's file-mode buffer (claude assistant lines can
	// exceed 1MB — see ClaudeParser.ParseFile) so a large line never aborts the
	// follow with ErrTooLong, keeping -f consistent with file mode.
	scanner.Buffer(buf, 10*1024*1024)

	for {
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			// Parse the new line via the canonical parser so streamed entries
			// render identically to file mode. ParseLine omits the session
			// header, so it is not reprinted on the tail.
			backend.parser.ParseLine(line, func(e logs.LogEntry) {
				if !since.IsZero() && e.Time.Before(since) {
					return
				}
				if formatted := formatter.Format(e); formatted != "" {
					fmt.Println(formatted)
				}
			})
		}

		if err := scanner.Err(); err != nil {
			return err
		}

		// Wait a bit before checking for more
		time.Sleep(100 * time.Millisecond)

		// Re-check file for new content
		// Reset scanner to continue from current position
		scanner = bufio.NewScanner(f)
		scanner.Buffer(buf, 10*1024*1024)
	}
}

// parseSince parses a duration or timestamp string.
func parseSince(s string) (time.Time, error) {
	// Try relative duration first (e.g., "5m", "1h", "30s")
	if matched, _ := regexp.MatchString(`^\d+[smhd]$`, s); matched {
		unit := s[len(s)-1]
		val, _ := strconv.Atoi(s[:len(s)-1])
		var dur time.Duration
		switch unit {
		case 's':
			dur = time.Duration(val) * time.Second
		case 'm':
			dur = time.Duration(val) * time.Minute
		case 'h':
			dur = time.Duration(val) * time.Hour
		case 'd':
			dur = time.Duration(val) * 24 * time.Hour
		}
		return time.Now().Add(-dur), nil
	}

	// Try ISO timestamp
	for _, layout := range []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("cannot parse %q as duration or timestamp", s)
}

// resolveSession resolves a task name or session ID to a session file path.
// Returns (sessionFile, backend, sessionID, taskName, error). taskName is empty
// when the argument resolves only as a raw session ID.
func (c *LogsCmd) resolveSession(backends []harnessLogBackend) (string, harnessLogBackend, string, string, error) {
	arg := c.TaskOrSession

	// First, try as a task name
	state, err := task.LoadState(arg)
	if err == nil && state != nil && state.SessionID != "" {
		preferred := sessionHarnessForTask(arg, state)
		candidates := orderBackends(backends, preferred)
		for _, b := range candidates {
			sessionFile, err := b.locator.FindSessionFile(state.SessionID)
			if err == nil {
				return sessionFile, b, state.SessionID, arg, nil
			}
			if !os.IsNotExist(err) {
				return "", harnessLogBackend{}, "", "", err
			}
		}
		// Session file not found - fall through to try as session ID
	}

	// Try as a session ID directly
	// Session IDs look like UUIDs: 019a48ac-f230-7f23-b587-d7e38f2669cd
	for _, b := range backends {
		sessionFile, err := b.locator.FindSessionFile(arg)
		if err == nil {
			return sessionFile, b, arg, "", nil
		}
		if !os.IsNotExist(err) {
			return "", harnessLogBackend{}, "", "", err
		}
	}

	// Neither worked - give helpful error
	if state != nil && state.SessionID != "" {
		return "", harnessLogBackend{}, "", "", fmt.Errorf("session file not found for task %q\n\nSession ID: %s\nThe session file may have been deleted or moved.", arg, state.SessionID)
	}

	// Check if task exists at all
	if _, taskErr := task.Load(arg); taskErr == nil {
		return "", harnessLogBackend{}, "", "", fmt.Errorf("task %q has no session (never run?)", arg)
	}

	return "", harnessLogBackend{}, "", "", fmt.Errorf("no task or session found for %q", arg)
}

func printTraceHeader(taskName string, cfg *workspace.Config) error {
	view, err := store.BuildView(context.Background(), taskName, cfg, store.BuildViewOptions{})
	if err != nil {
		return err
	}
	parts := []string{view.Name, view.Agent.Label()}
	if view.Routine != nil && strings.TrimSpace(view.Routine.CurrentStep) != "" {
		parts = append(parts, "stage: "+strings.TrimSpace(view.Routine.CurrentStep))
	}
	if strings.TrimSpace(view.StatusText) != "" {
		parts = append(parts, view.StatusText)
	}
	fmt.Println(strings.Join(parts, " | "))
	return nil
}

func orderBackends(backends []harnessLogBackend, preferred string) []harnessLogBackend {
	if preferred == "" {
		return backends
	}
	var out []harnessLogBackend
	for _, b := range backends {
		if b.name == preferred {
			out = append(out, b)
		}
	}
	for _, b := range backends {
		if b.name != preferred {
			out = append(out, b)
		}
	}
	if len(out) == 0 {
		return backends
	}
	return out
}
