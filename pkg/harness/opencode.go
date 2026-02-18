package harness

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// OpenCodeHarness implements Harness for the OpenCode CLI ("opencode").
type OpenCodeHarness struct {
	cli     cliSpec
	Model   string
	Variant string
	Agent   string
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


