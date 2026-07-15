package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	petname "github.com/dustinkirkland/golang-petname"
	"github.com/kgruel/subtask/pkg/agent"
	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/workspace"
)

// AskCmd implements 'subtask ask'.
type AskCmd struct {
	Prompt   string `arg:"" optional:"" help:"Question or prompt (or use stdin)"`
	FollowUp string `name:"follow-up" help:"Continue from task name, session name, or session ID"`
	Adapter  string `help:"Override adapter for this prompt (does not persist)"`
	Provider string `help:"Override provider for this prompt (adapter-dependent; does not persist)"`
	Model    string `help:"Override model for this prompt (does not persist)"`
	// Reasoning is adapter-dependent (e.g. codex, pi); not persisted.
	Reasoning string `help:"Override reasoning for this prompt (adapter-dependent; does not persist)"`
	Agent     string `help:"Agent override for adapter/model/reasoning (does not persist)"`

	// Internal: injected harness for testing
	testHarness harness.Harness
}

// WithHarness returns a copy with injected harness for testing.
func (c *AskCmd) WithHarness(h harness.Harness) *AskCmd {
	c.testHarness = h
	return c
}

// Run executes the ask command.
func (c *AskCmd) Run() error {
	// Read prompt from stdin if not provided
	prompt := c.Prompt
	if prompt == "" {
		prompt = readStdinIfAvailable()
	}

	if prompt == "" {
		return fmt.Errorf("prompt is required\n\n" +
			"Provide a prompt as argument or via stdin (heredoc/pipe)")
	}

	// Load config for harness
	cfg, err := workspace.LoadConfig()
	if err != nil {
		return err
	}

	// If --follow-up resolves to a task, load it for adapter/model resolution.
	var followUpTask *task.Task
	if c.FollowUp != "" {
		if t, loadErr := task.Load(c.FollowUp); loadErr == nil {
			followUpTask = t
		}
	}

	// Resolve adapter/provider/model/reasoning (task snapshot takes
	// precedence over project default when --follow-up names a task).
	var agentOverride *workspace.AgentSpec
	if c.Agent != "" {
		ag, agErr := agent.LoadByName(c.Agent)
		if agErr != nil {
			return agErr
		}
		spec := ag.AgentSpec()
		agentOverride = &spec
	}
	r, err := workspace.Resolve(cfg, followUpTask, workspace.ResolveOverrides{
		Adapter:   c.Adapter,
		Provider:  c.Provider,
		Model:     c.Model,
		Reasoning: c.Reasoning,
		Agent:     agentOverride,
	})
	if err != nil {
		return err
	}

	// Get cwd
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Resolve --follow-up to session ID and petname
	var continueFrom string
	var sessionName string // petname for this session
	if c.FollowUp != "" {
		continueFrom, sessionName, err = resolveAskContext(c.FollowUp, r.Adapter)
		if err != nil {
			return err
		}
	}

	// Build prompt with safety prefix
	fullPrompt := "The following is just a question. Do NOT make any modifications or take any actions unless explicitly requested.\n\n" + prompt

	// Create harness and run
	var h harness.Harness
	if c.testHarness != nil {
		h = c.testHarness
	} else {
		h, err = harness.New(workspace.ConfigWithOverrides(cfg, r.Adapter, r.Provider, r.Model, r.Reasoning))
		if err != nil {
			return err
		}
	}

	printInfo("[Waiting for reply...]")

	result, err := h.Run(context.Background(), cwd, fullPrompt, continueFrom, harness.Callbacks{})
	if err != nil {
		return err
	}

	// Generate petname for new sessions
	if sessionName == "" {
		sessionName = generateSessionName()
	}

	// Save conversation and UUID mapping
	convPath, err := saveAskConversation(sessionName, result.SessionID, prompt, result.Reply)
	if err != nil {
		printWarning(fmt.Sprintf("failed to save conversation: %v", err))
	}

	// Print reply
	fmt.Println()
	fmt.Println(result.Reply)
	fmt.Println()

	// Print session info for resuming
	fmt.Printf("Session: %s\n", sessionName)
	fmt.Printf("Resume:  subtask ask --follow-up %s \"...\"\n", sessionName)
	if convPath != "" {
		fmt.Printf("Log:     %s\n", convPath)
	}

	return nil
}

// ConversationsDir returns ~/.subtask/conversations.
func ConversationsDir() string {
	return filepath.Join(task.GlobalDir(), "conversations")
}

// resolveAskContext resolves a context string to (sessionID, sessionName).
// Checks: task state → task history (portable fallback, used when state.json is
// gone — after merge/close cleanup, or when a task folder was synced to another
// machine since internal/ is machine-local) → petname → raw session ID.
func resolveAskContext(ctx, projectAdapter string) (sessionID, sessionName string, err error) {
	// Try as task name via state first (fast path, common case).
	if st, stErr := task.LoadState(ctx); stErr == nil && st != nil && st.SessionID != "" {
		if err := enforceTaskHarnessMatch(ctx, st, projectAdapter); err != nil {
			return "", "", err
		}
		return st.SessionID, "", nil
	}

	// state.json missing or empty. If ctx is a real task, recover the last
	// session from the portable history instead of falling through to
	// raw-session-ID (which would pass the task name itself as a session ID).
	if _, loadErr := task.Load(ctx); loadErr == nil {
		sid, h := lastSessionFromHistory(ctx)
		if sid == "" {
			return "", "", fmt.Errorf("task %q has no session to continue\n\n"+
				"Tip: run 'subtask send %s \"...\"' to dispatch it first, or pass a session ID or petname directly.",
				ctx, ctx)
		}
		if h != "" && projectAdapter != "" && h != projectAdapter {
			return "", "", fmt.Errorf("task %q was last run with adapter %q, but this project is configured for %q\n\n"+
				"Sessions are not compatible across adapters.\n"+
				"Tip: run without --follow-up to start a fresh session.",
				ctx, h, projectAdapter)
		}
		return sid, "", nil
	}

	// Try as petname (check for .uuid file)
	uuidPath := filepath.Join(ConversationsDir(), ctx+".uuid")
	if data, readErr := os.ReadFile(uuidPath); readErr == nil {
		return strings.TrimSpace(string(data)), ctx, nil
	}

	// Assume it's a raw session ID
	return ctx, "", nil
}

// generateSessionName generates a unique petname for a session.
func generateSessionName() string {
	dir := ConversationsDir()
	os.MkdirAll(dir, 0755)

	for i := 0; i < 100; i++ { // Max retries
		name := petname.Generate(3, "-")
		convPath := filepath.Join(dir, name+".txt")
		if _, err := os.Stat(convPath); os.IsNotExist(err) {
			return name
		}
	}

	// Fallback: add random suffix (shouldn't happen in practice)
	return petname.Generate(4, "-")
}

// saveAskConversation saves the conversation and UUID mapping.
// Returns the conversation file path.
func saveAskConversation(sessionName, sessionID, prompt, reply string) (string, error) {
	dir := ConversationsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	convPath := filepath.Join(dir, sessionName+".txt")
	uuidPath := filepath.Join(dir, sessionName+".uuid")

	// Save UUID mapping (only if new)
	if _, err := os.Stat(uuidPath); os.IsNotExist(err) {
		if err := os.WriteFile(uuidPath, []byte(sessionID+"\n"), 0644); err != nil {
			return "", err
		}
	}

	// Append to conversation
	f, err := os.OpenFile(convPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}
	defer f.Close()

	fmt.Fprintf(f, "<user>\n%s\n</user>\n\n<assistant>\n%s\n</assistant>\n\n", prompt, reply)
	return convPath, nil
}
