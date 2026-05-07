package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	petname "github.com/dustinkirkland/golang-petname"
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
	Preset    string `help:"Preset shorthand for adapter/model/reasoning (does not persist)"`

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

	// Apply preset then resolve adapter/provider/model/reasoning (task snapshot takes
	// precedence over project default when --follow-up names a task).
	adapterFlag := c.Adapter
	providerFlag := c.Provider
	modelFlag := c.Model
	reasoningFlag := c.Reasoning
	if c.Preset != "" {
		p, ok := cfg.Presets[c.Preset]
		if !ok {
			return fmt.Errorf("unknown preset %q\n\nAvailable: %s", c.Preset, presetNames(cfg))
		}
		applyPreset(p, &adapterFlag, &providerFlag, &modelFlag, &reasoningFlag)
	}
	adapter := workspace.ResolveAdapter(cfg, followUpTask, adapterFlag)
	provider := workspace.ResolveProvider(cfg, followUpTask, providerFlag)
	model := workspace.ResolveModel(cfg, followUpTask, modelFlag)
	reasoning := workspace.ResolveReasoning(cfg, followUpTask, reasoningFlag)

	if err := workspace.ValidateReasoningFlag(adapter, reasoningFlag); err != nil {
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
		// Validate harness match against the resolved adapter (not the project default).
		if st, stErr := task.LoadState(c.FollowUp); stErr == nil && st != nil && st.SessionID != "" {
			if err := enforceTaskHarnessMatch(c.FollowUp, st, adapter); err != nil {
				return err
			}
		}
		continueFrom, sessionName = resolveAskContext(c.FollowUp)
	}

	// Build prompt with safety prefix
	fullPrompt := "The following is just a question. Do NOT make any modifications or take any actions unless explicitly requested.\n\n" + prompt

	// Create harness and run
	var h harness.Harness
	if c.testHarness != nil {
		h = c.testHarness
	} else {
		h, err = harness.New(workspace.ConfigWithOverrides(cfg, adapter, provider, model, reasoning))
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
// Checks: task name → petname → raw session ID
func resolveAskContext(ctx string) (sessionID, sessionName string) {
	// Try as task name first
	if state, err := task.LoadState(ctx); err == nil && state != nil && state.SessionID != "" {
		return state.SessionID, ""
	}

	// Try as petname (check for .uuid file)
	uuidPath := filepath.Join(ConversationsDir(), ctx+".uuid")
	if data, err := os.ReadFile(uuidPath); err == nil {
		return strings.TrimSpace(string(data)), ctx
	}

	// Assume it's a raw session ID
	return ctx, ""
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
