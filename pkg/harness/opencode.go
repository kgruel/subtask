package harness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// OpenCodeHarness implements Harness for the OpenCode CLI ("opencode").
type OpenCodeHarness struct {
	cli     cliSpec
	Model   string
	Variant string
	Agent   string
}

type openCodeStreamEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionID,omitempty"`

	Part *struct {
		ID   string `json:"id,omitempty"`
		Type string `json:"type,omitempty"`
		Text string `json:"text,omitempty"`
	} `json:"part,omitempty"`
}

func (o *OpenCodeHarness) Run(ctx context.Context, cwd, prompt, continueFrom string, cb Callbacks) (*Result, error) {
	args := []string{"run", "--format", "json"}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.Variant != "" {
		args = append(args, "--variant", o.Variant)
	}
	if o.Agent != "" {
		args = append(args, "--agent", o.Agent)
	}
	if continueFrom != "" {
		args = append(args, "--session", continueFrom)
	}

	cmd, err := commandForCLI(ctx, o.effectiveCLI(), args)
	if err != nil {
		return nil, err
	}
	cmd.Dir = cwd

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// OpenCode re-quotes CLI args with spaces; to preserve formatting, provide the prompt via stdin.
	cmd.Stdin = strings.NewReader(prompt)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start opencode: %w", err)
	}

	result := &Result{
		SessionID:       continueFrom,
		PromptDelivered: continueFrom != "",
	}

	var stderrBuf strings.Builder
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(&stderrBuf, stderr)
	}()

	parseErr := parseOpenCodeStream(stdout, result, cb)

	cmdErr := cmd.Wait()
	<-stderrDone

	if parseErr != nil && result.Error == "" {
		result.Error = parseErr.Error()
	}
	if cmdErr != nil && result.Error == "" {
		result.Error = strings.TrimSpace(stderrBuf.String())
		if result.Error == "" {
			result.Error = cmdErr.Error()
		}
		return result, fmt.Errorf("opencode failed: %w", cmdErr)
	}
	if result.Error != "" {
		return result, fmt.Errorf("opencode error: %s", result.Error)
	}

	return result, nil
}

// Review runs a code review using the standard Run infrastructure.
func (o *OpenCodeHarness) Review(cwd string, target ReviewTarget, instructions string) (string, error) {
	prompt := buildReviewPrompt(cwd, target, instructions)
	result, err := o.Run(context.Background(), cwd, prompt, "", Callbacks{})
	if err != nil {
		return "", err
	}
	return result.Reply, nil
}

func (o *OpenCodeHarness) MigrateSession(sessionID, oldCwd, newCwd string) error {
	// OpenCode sessions are resumable across directories/worktrees.
	return nil
}

func (o *OpenCodeHarness) DuplicateSession(sessionID, oldCwd, newCwd string) (string, error) {
	if sessionID == "" {
		return "", nil
	}
	return "", fmt.Errorf("opencode does not support session duplication (falling back to prompt injection)")
}

func (o *OpenCodeHarness) effectiveCLI() cliSpec {
	if strings.TrimSpace(o.cli.Exec) == "" {
		return cliSpec{Exec: "opencode"}
	}
	return o.cli
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
