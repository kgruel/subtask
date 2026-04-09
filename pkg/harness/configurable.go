package harness

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// templateVars holds values that are substituted into adapter config arg templates.
type templateVars struct {
	Model          string
	Provider       string
	Prompt         string
	SessionID      string
	CWD            string
	Reasoning      string
	PermissionMode string
	Tools          string
	Variant        string
	Agent          string
}

// ConfigurableAdapter is a generic Harness implementation driven by AdapterConfig.
// It builds CLI args from templates, delegates output parsing to named or generic
// parsers, and dispatches session operations to the session handler registry.
type ConfigurableAdapter struct {
	config *AdapterConfig
	vars   templateVars
}

// NewConfigurableAdapter creates a ConfigurableAdapter from a parsed AdapterConfig
// and template variables (model, reasoning, etc.).
func NewConfigurableAdapter(cfg *AdapterConfig, vars templateVars) (*ConfigurableAdapter, error) {
	if cfg == nil {
		return nil, fmt.Errorf("adapter config is nil")
	}
	return &ConfigurableAdapter{config: cfg, vars: vars}, nil
}

// Run implements Harness.Run. It builds CLI args, spawns the subprocess,
// parses output, and returns the result.
func (a *ConfigurableAdapter) Run(ctx context.Context, cwd, prompt, continueFrom string, cb Callbacks) (*Result, error) {
	continuing := continueFrom != ""
	if continuing {
		a.vars.SessionID = continueFrom
	}

	args := a.buildArgs(prompt, continuing)

	spec := cliSpec{Exec: a.config.CLI}
	cmd, err := commandForCLI(ctx, spec, args)
	if err != nil {
		return nil, err
	}
	cmd.Dir = cwd

	// Apply adapter-specific environment variables.
	if len(a.config.Env) > 0 {
		cmd.Env = append(os.Environ(), a.envSlice()...)
	}

	// If prompt_via is stdin, pipe the prompt to the process.
	if a.config.PromptVia == "stdin" {
		cmd.Stdin = strings.NewReader(prompt)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start %s: %w", a.config.Name, err)
	}

	result := &Result{}

	// Collect stderr concurrently.
	var stderrBuf strings.Builder
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(&stderrBuf, stderr)
	}()

	parseErr := a.parseOutput(stdout, result, cb)

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
		return result, fmt.Errorf("%s failed: %w", a.config.Name, cmdErr)
	}
	if result.Error != "" {
		return result, fmt.Errorf("%s error: %s", a.config.Name, result.Error)
	}

	return result, nil
}

// Review implements Harness.Review.
func (a *ConfigurableAdapter) Review(cwd string, target ReviewTarget, instructions string) (string, error) {
	if !a.config.Capabilities.Review {
		return "", fmt.Errorf("%s adapter does not support review", a.config.Name)
	}
	prompt := buildReviewPrompt(cwd, target, instructions)
	result, err := a.Run(context.Background(), cwd, prompt, "", Callbacks{})
	if err != nil {
		return "", err
	}
	return result.Reply, nil
}

// MigrateSession implements Harness.MigrateSession.
func (a *ConfigurableAdapter) MigrateSession(sessionID, oldCwd, newCwd string) error {
	return migrateSessionByHandler(a.config.SessionHandler, sessionID, oldCwd, newCwd)
}

// DuplicateSession implements Harness.DuplicateSession.
func (a *ConfigurableAdapter) DuplicateSession(sessionID, oldCwd, newCwd string) (string, error) {
	return duplicateSessionByHandler(a.config.SessionHandler, sessionID, oldCwd, newCwd)
}

// buildArgs constructs the CLI argument list from the adapter config templates.
//
// Template variables (e.g. {{model}}) are replaced with their values. If a template
// variable expands to empty AND the arg is purely that template variable, both the
// arg and the preceding flag arg (if it looks like a flag) are skipped.
//
// When continuing, continue_args are inserted before the prompt positional arg.
// When prompt_via is "arg" and {{prompt}} does not already appear in the args,
// the prompt is appended as the last positional arg.
func (a *ConfigurableAdapter) buildArgs(prompt string, continuing bool) []string {
	promptInArgs := argListContainsTemplate(a.config.Args, "{{prompt}}")

	// Template the base args, skipping empty template-only args and their flag predecessors.
	templated := a.templateArgList(a.config.Args, prompt)

	var result []string
	result = append(result, templated...)

	// Insert continue_args before prompt.
	if continuing && len(a.config.ContinueArgs) > 0 {
		contArgs := a.templateArgList(a.config.ContinueArgs, prompt)
		result = append(result, contArgs...)
	}

	// Append prompt as positional arg if prompt_via=arg and {{prompt}} not in args.
	if a.config.PromptVia != "stdin" && !promptInArgs {
		result = append(result, prompt)
	}

	return result
}

// templateArgList processes a list of args, performing template substitution and
// skipping empty template-only args along with their preceding flag.
func (a *ConfigurableAdapter) templateArgList(args []string, prompt string) []string {
	var result []string

	for i := 0; i < len(args); i++ {
		raw := args[i]
		expanded := a.templateArg(raw, prompt)

		// If this arg is purely a template variable and it expanded to empty,
		// skip it. Also skip the preceding arg if it looks like a flag.
		if isPureTemplateVar(raw) && expanded == "" {
			// Remove the preceding flag arg if we already added one.
			if len(result) > 0 && looksLikeFlag(result[len(result)-1]) {
				result = result[:len(result)-1]
			}
			continue
		}

		result = append(result, expanded)
	}

	return result
}

// templateArg replaces known template variables in a single arg string.
func (a *ConfigurableAdapter) templateArg(arg, prompt string) string {
	r := strings.NewReplacer(
		"{{model}}", a.vars.Model,
		"{{provider}}", a.vars.Provider,
		"{{prompt}}", prompt,
		"{{session_id}}", a.vars.SessionID,
		"{{cwd}}", a.vars.CWD,
		"{{reasoning}}", a.vars.Reasoning,
		"{{permission_mode}}", a.vars.PermissionMode,
		"{{tools}}", a.vars.Tools,
		"{{variant}}", a.vars.Variant,
		"{{agent}}", a.vars.Agent,
	)
	return r.Replace(arg)
}

// parseOutput dispatches to the appropriate parser based on the adapter config.
func (a *ConfigurableAdapter) parseOutput(r io.Reader, result *Result, cb Callbacks) error {
	parser := a.config.OutputParser

	// Named parsers.
	switch parser {
	case "claude", "codex", "opencode":
		return ParseByName(parser, r, result, cb)
	case "generic-jsonl":
		rules := GenericJSONLRules{
			SessionID:       a.config.Parse.SessionID,
			Reply:           a.config.Parse.Reply,
			ReplyAccumulate: a.config.Parse.ReplyAccumulate,
		}
		if len(a.config.Parse.ToolCallMatch) > 0 {
			rules.ToolCall = &ToolCallRule{Match: a.config.Parse.ToolCallMatch}
		}
		return ParseGenericJSONL(r, result, cb, rules)
	case "text", "":
		return ParseText(r, result)
	default:
		return fmt.Errorf("unknown output parser: %q", parser)
	}
}

// envSlice converts the adapter's Env map to a slice of KEY=VALUE strings.
func (a *ConfigurableAdapter) envSlice() []string {
	s := make([]string, 0, len(a.config.Env))
	for k, v := range a.config.Env {
		s = append(s, k+"="+v)
	}
	return s
}

// isPureTemplateVar returns true if the arg is exactly a single template variable
// (e.g. "{{model}}"), with no other text.
func isPureTemplateVar(arg string) bool {
	return strings.HasPrefix(arg, "{{") && strings.HasSuffix(arg, "}}") && strings.Count(arg, "{{") == 1
}

// looksLikeFlag returns true if the arg starts with "-".
func looksLikeFlag(arg string) bool {
	return strings.HasPrefix(arg, "-")
}

// argListContainsTemplate checks if any arg in the list contains the given template var.
func argListContainsTemplate(args []string, tmpl string) bool {
	for _, arg := range args {
		if strings.Contains(arg, tmpl) {
			return true
		}
	}
	return false
}
