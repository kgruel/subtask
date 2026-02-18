# Config-Driven Adapters Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace hardcoded Go harness structs with a single generic adapter that loads CLI invocation config from YAML files, so adding a new worker backend is a config file instead of Go code.

**Architecture:** The adapter config defines how to invoke a CLI (args, model flags, prompt delivery, session continuation) while output parsing uses named strategies (built-in parsers for claude/codex/opencode, generic JSONL/text parsers for user-defined adapters). Session operations (migrate/duplicate) use registered Go handlers since the complex cases (Claude path rewriting, Codex JSONL mutation) can't be cleanly expressed in config.

**Tech Stack:** Go 1.24, gopkg.in/yaml.v3 (already in deps), embed.FS, stretchr/testify

---

### Task 1: Extract output parsers into parse.go

Move the three existing stream parsers out of their harness files into a shared `parse.go` with a registry function. This is purely additive — the old files still call the functions, they just live in a new location.

**Files:**
- Create: `pkg/harness/parse.go`
- Create: `pkg/harness/parse_test.go`
- Modify: `pkg/harness/codex.go` (remove `parseCodexExecJSONL`, `processCodexJSONLLine`)
- Modify: `pkg/harness/claude.go` (remove `parseClaudeStream`)
- Modify: `pkg/harness/opencode.go` (remove `parseOpenCodeStream`)

**Step 1: Write the test**

```go
// pkg/harness/parse_test.go
package harness

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseByName_Claude(t *testing.T) {
	input := `{"type":"system","subtype":"init","session_id":"sess-abc"}
{"type":"result","result":"hello world","session_id":"sess-abc"}
`
	result := &Result{}
	err := ParseByName("claude", strings.NewReader(input), result, Callbacks{})
	require.NoError(t, err)
	require.Equal(t, "sess-abc", result.SessionID)
	require.Equal(t, "hello world", result.Reply)
	require.True(t, result.AgentReplied)
}

func TestParseByName_GenericJSONL(t *testing.T) {
	input := `{"session_id":"s1","result":"partial"}
{"session_id":"s2","result":"final answer"}
`
	rules := GenericJSONLRules{
		SessionIDPath: ".session_id",
		ReplyPath:     ".result",
	}
	result := &Result{}
	err := ParseGenericJSONL(strings.NewReader(input), result, Callbacks{}, rules)
	require.NoError(t, err)
	require.Equal(t, "s2", result.SessionID)
	require.Equal(t, "final answer", result.Reply)
}

func TestParseByName_GenericJSONL_Accumulate(t *testing.T) {
	input := `{"part":{"text":"hello "}}
{"part":{"text":"world"}}
`
	rules := GenericJSONLRules{
		ReplyPath:       ".part.text",
		ReplyAccumulate: true,
	}
	result := &Result{}
	err := ParseGenericJSONL(strings.NewReader(input), result, Callbacks{}, rules)
	require.NoError(t, err)
	require.Equal(t, "hello world", result.Reply)
}

func TestParseByName_Text(t *testing.T) {
	input := "line 1\nline 2\nline 3\n"
	result := &Result{}
	err := ParseText(strings.NewReader(input), result)
	require.NoError(t, err)
	require.Equal(t, "line 1\nline 2\nline 3", result.Reply)
	require.True(t, result.AgentReplied)
}

func TestParseByName_Unknown(t *testing.T) {
	result := &Result{}
	err := ParseByName("nonexistent", strings.NewReader(""), result, Callbacks{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown parser")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/harness/ -run TestParseByName -v`
Expected: FAIL — `ParseByName`, `ParseGenericJSONL`, `ParseText` not defined

**Step 3: Implement parse.go**

```go
// pkg/harness/parse.go
package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ParseByName selects a named parser and runs it.
// Built-in names: "claude", "codex", "opencode".
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

// GenericJSONLRules configures the generic JSONL parser.
type GenericJSONLRules struct {
	SessionIDPath   string // dot-path, e.g. ".session_id"
	ReplyPath       string // dot-path, e.g. ".result"
	ReplyAccumulate bool   // true = concatenate all matches; false = last match wins
	ToolCallMatch   map[string]string // field→value filter for tool call detection
}

// ParseGenericJSONL parses JSONL output using dot-path extraction rules.
func ParseGenericJSONL(r io.Reader, result *Result, cb Callbacks, rules GenericJSONLRules) error {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var replyBuf strings.Builder
	seenSessionStart := false

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var obj map[string]any
		if err := json.Unmarshal(line, &obj); err != nil {
			continue
		}

		// Session ID extraction
		if rules.SessionIDPath != "" {
			if v := extractDotPath(obj, rules.SessionIDPath); v != "" {
				result.SessionID = v
				if !seenSessionStart {
					seenSessionStart = true
					result.PromptDelivered = true
					if cb.OnSessionStart != nil {
						cb.OnSessionStart(v)
					}
				}
			}
		}

		// Reply extraction
		if rules.ReplyPath != "" {
			if v := extractDotPath(obj, rules.ReplyPath); v != "" {
				if rules.ReplyAccumulate {
					replyBuf.WriteString(v)
					result.Reply = replyBuf.String()
				} else {
					result.Reply = v
				}
				result.AgentReplied = true
			}
		}

		// Tool call detection
		if len(rules.ToolCallMatch) > 0 && matchFields(obj, rules.ToolCallMatch) {
			if cb.OnToolCall != nil {
				cb.OnToolCall(time.Now())
			}
		}
	}

	return scanner.Err()
}

// ParseText captures all stdout as the reply.
func ParseText(r io.Reader, result *Result) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	result.Reply = strings.TrimRight(string(data), "\n")
	if result.Reply != "" {
		result.AgentReplied = true
	}
	return nil
}

// extractDotPath extracts a value from a JSON object using dot notation.
// e.g. ".session_id" extracts obj["session_id"]
// e.g. ".item.text" extracts obj["item"]["text"]
func extractDotPath(obj map[string]any, path string) string {
	path = strings.TrimPrefix(path, ".")
	parts := strings.Split(path, ".")

	var current any = obj
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

	switch v := current.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%g", v)
	default:
		return ""
	}
}

// matchFields returns true if all key→value pairs match in the JSON object.
func matchFields(obj map[string]any, match map[string]string) bool {
	for k, want := range match {
		got := extractDotPath(obj, "."+k)
		if got != want {
			return false
		}
	}
	return true
}
```

Note: add `"time"` to imports for the `time.Now()` call in tool call detection.

**Step 4: Move existing parser functions**

Move `parseClaudeStream` from `claude.go` to `parse.go` (cut from claude.go, paste into parse.go).
Move `parseCodexExecJSONL` and `processCodexJSONLLine` from `codex.go` to `parse.go`.
Move `parseOpenCodeStream` from `opencode.go` to `parse.go`.

Keep all the existing types they use (`claudeStreamEvent`, `claudeMessagePart`, `CodexEvent`, `openCodeStreamEvent`) in their respective files for now — they're used by the Run() methods too.

Actually, move the event types to parse.go as well since they're parsing concerns. The Run() methods in the old harness files just call the parse functions.

**Step 5: Run all tests to verify nothing broke**

Run: `go test ./pkg/harness/ -v`
Expected: All existing tests PASS, new tests PASS

**Step 6: Commit**

```bash
git add pkg/harness/parse.go pkg/harness/parse_test.go pkg/harness/codex.go pkg/harness/claude.go pkg/harness/opencode.go
git commit -m "refactor: extract output parsers into parse.go with generic JSONL parser"
```

---

### Task 2: Define adapter config schema and YAML loader

**Files:**
- Create: `pkg/harness/adapter_config.go`
- Create: `pkg/harness/adapter_config_test.go`

**Step 1: Write the test**

```go
// pkg/harness/adapter_config_test.go
package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadAdapterConfig(t *testing.T) {
	dir := t.TempDir()
	yaml := `
name: test-cli
cli: my-tool
args:
  - "--json"
  - "--model"
  - "{{model}}"
prompt_via: arg
continue_args:
  - "--resume"
  - "{{session_id}}"
output_parser: generic-jsonl
parse:
  session_id: ".session_id"
  reply: ".result"
  reply_accumulate: false
capabilities:
  continue_session: true
  review: true
session_handler: none
`
	err := os.WriteFile(filepath.Join(dir, "test-cli.yaml"), []byte(yaml), 0644)
	require.NoError(t, err)

	cfg, err := LoadAdapterConfigFromDir(dir, "test-cli")
	require.NoError(t, err)
	require.Equal(t, "test-cli", cfg.Name)
	require.Equal(t, "my-tool", cfg.CLI)
	require.Equal(t, []string{"--json", "--model", "{{model}}"}, cfg.Args)
	require.Equal(t, "arg", cfg.PromptVia)
	require.Equal(t, []string{"--resume", "{{session_id}}"}, cfg.ContinueArgs)
	require.Equal(t, "generic-jsonl", cfg.OutputParser)
	require.Equal(t, ".session_id", cfg.Parse.SessionID)
	require.Equal(t, ".result", cfg.Parse.Reply)
	require.False(t, cfg.Parse.ReplyAccumulate)
	require.True(t, cfg.Capabilities.ContinueSession)
	require.True(t, cfg.Capabilities.Review)
	require.Equal(t, "none", cfg.SessionHandler)
}

func TestLoadAdapterConfig_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadAdapterConfigFromDir(dir, "nonexistent")
	require.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/harness/ -run TestLoadAdapterConfig -v`
Expected: FAIL — types not defined

**Step 3: Implement adapter_config.go**

```go
// pkg/harness/adapter_config.go
package harness

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// AdapterConfig defines how to invoke a CLI worker backend.
type AdapterConfig struct {
	Name         string            `yaml:"name"`
	CLI          string            `yaml:"cli"`
	Args         []string          `yaml:"args"`
	PromptVia    string            `yaml:"prompt_via"`    // "arg" (default) or "stdin"
	ContinueArgs []string          `yaml:"continue_args"` // inserted before prompt on continuation
	OutputParser string            `yaml:"output_parser"` // "claude", "codex", "opencode", "generic-jsonl", "text"
	Parse        AdapterParseRules `yaml:"parse"`
	Capabilities AdapterCaps       `yaml:"capabilities"`
	SessionHandler string          `yaml:"session_handler"` // "claude", "codex", "none" (default)
	Env          map[string]string `yaml:"env,omitempty"`   // extra env vars
}

// AdapterParseRules configures the generic JSONL parser (used when OutputParser is "generic-jsonl").
type AdapterParseRules struct {
	SessionID       string            `yaml:"session_id"`
	Reply           string            `yaml:"reply"`
	ReplyAccumulate bool              `yaml:"reply_accumulate"`
	ToolCallMatch   map[string]string `yaml:"tool_call_match,omitempty"`
}

// AdapterCaps declares what the adapter supports.
type AdapterCaps struct {
	ContinueSession bool `yaml:"continue_session"`
	Review          bool `yaml:"review"`
}

// LoadAdapterConfigFromDir loads an adapter config YAML from a directory.
func LoadAdapterConfigFromDir(dir, name string) (*AdapterConfig, error) {
	path := filepath.Join(dir, name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("adapter %q not found in %s: %w", name, dir, err)
	}
	return parseAdapterConfig(data)
}

func parseAdapterConfig(data []byte) (*AdapterConfig, error) {
	var cfg AdapterConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid adapter config: %w", err)
	}
	if cfg.Name == "" {
		return nil, fmt.Errorf("adapter config missing 'name'")
	}
	if cfg.CLI == "" {
		return nil, fmt.Errorf("adapter config missing 'cli'")
	}
	// Defaults
	if cfg.PromptVia == "" {
		cfg.PromptVia = "arg"
	}
	if cfg.OutputParser == "" {
		cfg.OutputParser = "text"
	}
	if cfg.SessionHandler == "" {
		cfg.SessionHandler = "none"
	}
	return &cfg, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/harness/ -run TestLoadAdapterConfig -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/harness/adapter_config.go pkg/harness/adapter_config_test.go
git commit -m "feat: add adapter config YAML schema and loader"
```

---

### Task 3: Create built-in adapter YAML files and embed them

**Files:**
- Create: `pkg/harness/adapters/claude.yaml`
- Create: `pkg/harness/adapters/codex.yaml`
- Create: `pkg/harness/adapters/opencode.yaml`
- Create: `pkg/harness/adapter_embed.go`
- Create: `pkg/harness/adapter_embed_test.go`

**Step 1: Write the test**

```go
// pkg/harness/adapter_embed_test.go
package harness

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadBuiltinAdapter_Claude(t *testing.T) {
	cfg, err := LoadBuiltinAdapter("claude")
	require.NoError(t, err)
	require.Equal(t, "claude", cfg.Name)
	require.Equal(t, "claude", cfg.CLI)
	require.Equal(t, "claude", cfg.OutputParser)
	require.True(t, cfg.Capabilities.ContinueSession)
	require.True(t, cfg.Capabilities.Review)
	require.Equal(t, "claude", cfg.SessionHandler)
}

func TestLoadBuiltinAdapter_Codex(t *testing.T) {
	cfg, err := LoadBuiltinAdapter("codex")
	require.NoError(t, err)
	require.Equal(t, "codex", cfg.Name)
	require.Equal(t, "codex", cfg.CLI)
	require.Equal(t, "codex", cfg.OutputParser)
	require.True(t, cfg.Capabilities.ContinueSession)
	require.True(t, cfg.Capabilities.Review)
	require.Equal(t, "codex", cfg.SessionHandler)
}

func TestLoadBuiltinAdapter_OpenCode(t *testing.T) {
	cfg, err := LoadBuiltinAdapter("opencode")
	require.NoError(t, err)
	require.Equal(t, "opencode", cfg.Name)
	require.Equal(t, "opencode", cfg.CLI)
	require.Equal(t, "stdin", cfg.PromptVia)
	require.Equal(t, "opencode", cfg.OutputParser)
	require.Equal(t, "none", cfg.SessionHandler)
}

func TestLoadBuiltinAdapter_NotFound(t *testing.T) {
	_, err := LoadBuiltinAdapter("nonexistent")
	require.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/harness/ -run TestLoadBuiltinAdapter -v`
Expected: FAIL — `LoadBuiltinAdapter` not defined

**Step 3: Write adapter YAML files**

`pkg/harness/adapters/claude.yaml`:
```yaml
name: claude
cli: claude
args:
  - "--print"
  - "--verbose"
  - "--output-format=stream-json"
  - "--permission-mode"
  - "{{permission_mode}}"
  - "--model"
  - "{{model}}"
prompt_via: arg
continue_args:
  - "--resume"
  - "{{session_id}}"
output_parser: claude
capabilities:
  continue_session: true
  review: true
session_handler: claude
```

`pkg/harness/adapters/codex.yaml`:
```yaml
name: codex
cli: codex
args:
  - "exec"
  - "--json"
  - "--dangerously-bypass-approvals-and-sandbox"
  - "--enable"
  - "web_search_request"
  - "-m"
  - "{{model}}"
prompt_via: arg
continue_args:
  - "resume"
  - "{{session_id}}"
output_parser: codex
capabilities:
  continue_session: true
  review: true
session_handler: codex
```

`pkg/harness/adapters/opencode.yaml`:
```yaml
name: opencode
cli: opencode
args:
  - "run"
  - "--format"
  - "json"
  - "--model"
  - "{{model}}"
prompt_via: stdin
continue_args:
  - "--session"
  - "{{session_id}}"
output_parser: opencode
capabilities:
  continue_session: true
  review: true
session_handler: none
```

**Step 4: Implement embed loader**

```go
// pkg/harness/adapter_embed.go
package harness

import (
	"embed"
	"fmt"
)

//go:embed adapters/*.yaml
var embeddedAdapters embed.FS

// LoadBuiltinAdapter loads a built-in adapter by name from embedded YAML.
func LoadBuiltinAdapter(name string) (*AdapterConfig, error) {
	path := fmt.Sprintf("adapters/%s.yaml", name)
	data, err := embeddedAdapters.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("built-in adapter %q not found: %w", name, err)
	}
	return parseAdapterConfig(data)
}

// LoadAdapter loads an adapter config with precedence:
// 1. User directory (~/.subtask/adapters/<name>.yaml)
// 2. Built-in embedded default
func LoadAdapter(userDir, name string) (*AdapterConfig, error) {
	// Try user override first
	if userDir != "" {
		cfg, err := LoadAdapterConfigFromDir(userDir, name)
		if err == nil {
			return cfg, nil
		}
	}
	// Fall back to built-in
	return LoadBuiltinAdapter(name)
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./pkg/harness/ -run TestLoadBuiltinAdapter -v`
Expected: PASS

**Step 6: Commit**

```bash
git add pkg/harness/adapters/ pkg/harness/adapter_embed.go pkg/harness/adapter_embed_test.go
git commit -m "feat: add built-in adapter YAML configs for claude, codex, opencode"
```

---

### Task 4: Implement ConfigurableAdapter

The core: a generic `Harness` implementation that uses `AdapterConfig` for invocation and delegates parsing to named parsers.

**Files:**
- Create: `pkg/harness/configurable.go`
- Create: `pkg/harness/configurable_test.go`

**Step 1: Write the test**

```go
// pkg/harness/configurable_test.go
package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigurableAdapter_TemplateArgs(t *testing.T) {
	cfg := &AdapterConfig{
		Name: "test",
		CLI:  "echo",
		Args: []string{"--model", "{{model}}", "--flag"},
		ContinueArgs: []string{"--resume", "{{session_id}}"},
		PromptVia: "arg",
	}

	a := &ConfigurableAdapter{config: cfg}

	vars := templateVars{
		Model:     "opus",
		SessionID: "sess-123",
		Prompt:    "hello",
	}

	// Fresh invocation (no continuation)
	args := a.buildArgs(vars, false)
	require.Equal(t, []string{"--model", "opus", "--flag", "hello"}, args)

	// Continuation
	args = a.buildArgs(vars, true)
	require.Equal(t, []string{"--model", "opus", "--flag", "--resume", "sess-123", "hello"}, args)
}

func TestConfigurableAdapter_TemplateArgs_EmptyModel(t *testing.T) {
	cfg := &AdapterConfig{
		Name: "test",
		CLI:  "echo",
		Args: []string{"--model", "{{model}}", "--flag"},
		PromptVia: "arg",
	}

	a := &ConfigurableAdapter{config: cfg}
	vars := templateVars{Prompt: "hello"}

	// When model is empty, omit the --model and its value
	args := a.buildArgs(vars, false)
	require.Equal(t, []string{"--flag", "hello"}, args)
}

func TestConfigurableAdapter_Review_Unsupported(t *testing.T) {
	cfg := &AdapterConfig{
		Name: "test",
		CLI:  "echo",
		Capabilities: AdapterCaps{Review: false},
	}
	a := &ConfigurableAdapter{config: cfg}
	_, err := a.Review(".", ReviewTarget{Uncommitted: true}, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not support review")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/harness/ -run TestConfigurableAdapter -v`
Expected: FAIL — `ConfigurableAdapter` not defined

**Step 3: Implement configurable.go**

```go
// pkg/harness/configurable.go
package harness

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// templateVars holds the values available for arg templating.
type templateVars struct {
	Model          string
	Prompt         string
	SessionID      string
	CWD            string
	Reasoning      string
	PermissionMode string
	Tools          string
	Variant        string
	Agent          string
}

// ConfigurableAdapter implements Harness using an AdapterConfig.
type ConfigurableAdapter struct {
	config    *AdapterConfig
	cliSpec   cliSpec // resolved CLI executable
}

// NewConfigurableAdapter creates a ConfigurableAdapter from config.
func NewConfigurableAdapter(cfg *AdapterConfig, vars templateVars) (*ConfigurableAdapter, error) {
	cliExec := cfg.CLI
	spec := cliSpec{Exec: cliExec}

	return &ConfigurableAdapter{
		config:  cfg,
		cliSpec: spec,
	}, nil
}

func (a *ConfigurableAdapter) Run(ctx context.Context, cwd, prompt, continueFrom string, cb Callbacks) (*Result, error) {
	continuing := continueFrom != ""

	vars := a.varsFromContext(cwd, prompt, continueFrom)
	args := a.buildArgs(vars, continuing)

	cmd, err := commandForCLI(ctx, a.cliSpec, args)
	if err != nil {
		return nil, err
	}
	cmd.Dir = cwd

	// Set extra env vars
	if len(a.config.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range a.config.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	// Prompt via stdin if configured
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
		return nil, fmt.Errorf("failed to start %s: %w", a.config.CLI, err)
	}

	result := &Result{}

	var stderrBuf strings.Builder
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(&stderrBuf, stderr)
	}()

	// Parse output using the configured parser
	var parseErr error
	switch a.config.OutputParser {
	case "claude", "codex", "opencode":
		parseErr = ParseByName(a.config.OutputParser, stdout, result, cb)
	case "generic-jsonl":
		rules := GenericJSONLRules{
			SessionIDPath:   a.config.Parse.SessionID,
			ReplyPath:       a.config.Parse.Reply,
			ReplyAccumulate: a.config.Parse.ReplyAccumulate,
			ToolCallMatch:   a.config.Parse.ToolCallMatch,
		}
		parseErr = ParseGenericJSONL(stdout, result, cb, rules)
	case "text":
		parseErr = ParseText(stdout, result)
	default:
		parseErr = fmt.Errorf("unknown output parser: %q", a.config.OutputParser)
	}

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

func (a *ConfigurableAdapter) Review(cwd string, target ReviewTarget, instructions string) (string, error) {
	if !a.config.Capabilities.Review {
		return "", fmt.Errorf("adapter %q does not support review", a.config.Name)
	}
	prompt := buildReviewPrompt(cwd, target, instructions)
	result, err := a.Run(context.Background(), cwd, prompt, "", Callbacks{})
	if err != nil {
		return "", err
	}
	return result.Reply, nil
}

func (a *ConfigurableAdapter) MigrateSession(sessionID, oldCwd, newCwd string) error {
	return migrateSessionByHandler(a.config.SessionHandler, sessionID, oldCwd, newCwd)
}

func (a *ConfigurableAdapter) DuplicateSession(sessionID, oldCwd, newCwd string) (string, error) {
	return duplicateSessionByHandler(a.config.SessionHandler, sessionID, oldCwd, newCwd)
}

// buildArgs templates the CLI arguments, handling continuation and prompt delivery.
func (a *ConfigurableAdapter) buildArgs(vars templateVars, continuing bool) []string {
	var args []string

	for i := 0; i < len(a.config.Args); i++ {
		arg := a.config.Args[i]
		expanded := templateArg(arg, vars)

		// If this is a flag (--model) followed by a template that expanded to empty, skip both
		if expanded == "" && isTemplateVar(arg) {
			// Check if previous arg was a flag for this value
			if len(args) > 0 && strings.HasPrefix(args[len(args)-1], "-") {
				args = args[:len(args)-1] // remove the flag too
			}
			continue
		}
		if expanded != "" || !isTemplateVar(arg) {
			args = append(args, expanded)
		}
	}

	// Insert continuation args before prompt
	if continuing && len(a.config.ContinueArgs) > 0 {
		for _, arg := range a.config.ContinueArgs {
			args = append(args, templateArg(arg, vars))
		}
	}

	// Append prompt as positional arg
	if a.config.PromptVia != "stdin" && vars.Prompt != "" {
		args = append(args, vars.Prompt)
	}

	return args
}

func (a *ConfigurableAdapter) varsFromContext(cwd, prompt, sessionID string) templateVars {
	// These are populated by the caller via adapter-specific options.
	// For now, return what we have.
	return templateVars{
		CWD:       cwd,
		Prompt:    prompt,
		SessionID: sessionID,
	}
}

func templateArg(arg string, vars templateVars) string {
	replacements := map[string]string{
		"{{model}}":           vars.Model,
		"{{prompt}}":          vars.Prompt,
		"{{session_id}}":      vars.SessionID,
		"{{cwd}}":             vars.CWD,
		"{{reasoning}}":       vars.Reasoning,
		"{{permission_mode}}": vars.PermissionMode,
		"{{tools}}":           vars.Tools,
		"{{variant}}":         vars.Variant,
		"{{agent}}":           vars.Agent,
	}
	result := arg
	for k, v := range replacements {
		result = strings.ReplaceAll(result, k, v)
	}
	return result
}

func isTemplateVar(s string) bool {
	return strings.HasPrefix(s, "{{") && strings.HasSuffix(s, "}}")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/harness/ -run TestConfigurableAdapter -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/harness/configurable.go pkg/harness/configurable_test.go
git commit -m "feat: implement ConfigurableAdapter with arg templating and parser dispatch"
```

---

### Task 5: Session handler registry

Move the Claude and Codex session operations into a registry so they can be selected by adapter config name.

**Files:**
- Create: `pkg/harness/session_handlers.go`
- Create: `pkg/harness/session_handlers_test.go`

**Step 1: Write the test**

```go
// pkg/harness/session_handlers_test.go
package harness

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSessionHandler_None(t *testing.T) {
	err := migrateSessionByHandler("none", "sess-1", "/old", "/new")
	require.NoError(t, err)

	_, err = duplicateSessionByHandler("none", "sess-1", "/old", "/new")
	require.Error(t, err) // "none" handler can't duplicate
}

func TestSessionHandler_Unknown(t *testing.T) {
	err := migrateSessionByHandler("nonexistent", "sess-1", "/old", "/new")
	require.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/harness/ -run TestSessionHandler -v`
Expected: FAIL — functions not defined

**Step 3: Implement session_handlers.go**

```go
// pkg/harness/session_handlers.go
package harness

import "fmt"

// migrateSessionByHandler dispatches session migration to the appropriate handler.
func migrateSessionByHandler(handler, sessionID, oldCwd, newCwd string) error {
	switch handler {
	case "none", "":
		return nil // no-op
	case "codex":
		h := &CodexHarness{}
		return h.MigrateSession(sessionID, oldCwd, newCwd)
	case "claude":
		h := &ClaudeHarness{}
		return h.MigrateSession(sessionID, oldCwd, newCwd)
	default:
		return fmt.Errorf("unknown session handler: %q", handler)
	}
}

// duplicateSessionByHandler dispatches session duplication to the appropriate handler.
func duplicateSessionByHandler(handler, sessionID, oldCwd, newCwd string) (string, error) {
	switch handler {
	case "none", "":
		return "", fmt.Errorf("adapter does not support session duplication")
	case "codex":
		h := &CodexHarness{}
		return h.DuplicateSession(sessionID, oldCwd, newCwd)
	case "claude":
		h := &ClaudeHarness{}
		return h.DuplicateSession(sessionID, oldCwd, newCwd)
	default:
		return "", fmt.Errorf("unknown session handler: %q", handler)
	}
}
```

Note: This temporarily references the old `CodexHarness` and `ClaudeHarness` types. When we delete those files later (Task 8), we'll move the session operation functions directly into this file.

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/harness/ -run TestSessionHandler -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/harness/session_handlers.go pkg/harness/session_handlers_test.go
git commit -m "feat: add session handler registry for adapter-driven session operations"
```

---

### Task 6: Update Config struct

Rename `Harness` → `Adapter`, promote `model`/`reasoning` from the options map to top-level fields. Support reading both old and new formats for migration.

**Files:**
- Modify: `pkg/workspace/config.go`
- Modify: `pkg/workspace/model_override.go`
- Create: `pkg/workspace/config_test.go` (if not exists, add migration test)

**Step 1: Write the test**

```go
// Add to pkg/workspace/config_test.go (or create it)

func TestLoadConfig_MigrateLegacyFormat(t *testing.T) {
	// Legacy format: {"harness":"claude","options":{"model":"opus"}}
	// New format:    {"adapter":"claude","model":"opus"}
	legacy := `{"harness":"claude","max_workspaces":10,"options":{"model":"opus","reasoning":"high"}}`

	var cfg Config
	err := json.Unmarshal([]byte(legacy), &cfg)
	require.NoError(t, err)
	cfg.migrateLegacy()

	require.Equal(t, "claude", cfg.Adapter)
	require.Equal(t, "opus", cfg.Model)
	require.Equal(t, "high", cfg.Reasoning)
	require.Equal(t, 10, cfg.MaxWorkspaces)
}

func TestLoadConfig_NewFormat(t *testing.T) {
	newFmt := `{"adapter":"claude","model":"opus","max_workspaces":10}`

	var cfg Config
	err := json.Unmarshal([]byte(newFmt), &cfg)
	require.NoError(t, err)
	cfg.migrateLegacy()

	require.Equal(t, "claude", cfg.Adapter)
	require.Equal(t, "opus", cfg.Model)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/workspace/ -run TestLoadConfig_Migrate -v`
Expected: FAIL — `Adapter`, `Model`, `Reasoning` fields and `migrateLegacy` not defined

**Step 3: Update config.go**

Change the `Config` struct:

```go
type Config struct {
	Adapter       string         `json:"adapter"`
	Model         string         `json:"model,omitempty"`
	Reasoning     string         `json:"reasoning,omitempty"`
	MaxWorkspaces int            `json:"max_workspaces"`

	// Legacy fields (read-only, for migration)
	LegacyHarness string         `json:"harness,omitempty"`
	LegacyOptions map[string]any `json:"options,omitempty"`
}

func (c *Config) migrateLegacy() {
	// Migrate harness → adapter
	if c.Adapter == "" && c.LegacyHarness != "" {
		c.Adapter = c.LegacyHarness
	}
	// Migrate options.model → model
	if c.Model == "" && c.LegacyOptions != nil {
		if m, ok := c.LegacyOptions["model"].(string); ok && strings.TrimSpace(m) != "" {
			c.Model = strings.TrimSpace(m)
		}
	}
	// Migrate options.reasoning → reasoning
	if c.Reasoning == "" && c.LegacyOptions != nil {
		if r, ok := c.LegacyOptions["reasoning"].(string); ok && strings.TrimSpace(r) != "" {
			c.Reasoning = strings.TrimSpace(r)
		}
	}
	// Clear legacy fields after migration
	c.LegacyHarness = ""
	c.LegacyOptions = nil
}
```

Update `LoadConfig` to call `migrateLegacy()` after unmarshaling.
Update `mergeConfig` to use `Adapter` instead of `Harness`.
Update `ResolveModel` and `ResolveReasoning` in `model_override.go` to read from `cfg.Model` / `cfg.Reasoning` directly instead of `cfg.Options`.

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/workspace/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/workspace/config.go pkg/workspace/model_override.go pkg/workspace/config_test.go
git commit -m "refactor: rename Harness to Adapter in config, promote model/reasoning to top-level"
```

---

### Task 7: Update harness.New() factory and callers

Wire up the new adapter system: `New()` loads adapter YAML, callers reference `cfg.Adapter` instead of `cfg.Harness`.

**Files:**
- Modify: `pkg/harness/harness.go` — update `New()` to load adapter config
- Modify: `cmd/subtask/send.go` — `cfg.Harness` → `cfg.Adapter`
- Modify: `cmd/subtask/ask.go` — same
- Modify: `cmd/subtask/harness_match.go` — rename to `adapter_match.go`
- Modify: `cmd/subtask/session_harness.go` — rename to `session_adapter.go`
- Modify: `cmd/subtask/config_wizard.go` — update validation and defaults

**Step 1: Update harness.New()**

```go
func New(cfg *workspace.Config) (Harness, error) {
	adapterName := cfg.Adapter
	if adapterName == "" {
		return nil, fmt.Errorf("no adapter configured")
	}

	// Special cases for test mocks (keep as-is)
	if adapterName == "mock" || adapterName == "builtin-mock" {
		return newMockHarness(adapterName, cfg)
	}

	// Load adapter config (user override → built-in)
	userDir := filepath.Join(task.SubtaskDir(), "adapters")
	adapterCfg, err := LoadAdapter(userDir, adapterName)
	if err != nil {
		return nil, fmt.Errorf("failed to load adapter %q: %w", adapterName, err)
	}

	// Build template vars from config
	vars := templateVars{
		Model:          cfg.Model,
		Reasoning:      cfg.Reasoning,
		PermissionMode: "bypassPermissions", // default for claude
	}

	return NewConfigurableAdapter(adapterCfg, vars)
}
```

**Step 2: Update all callers**

In `send.go`, `ask.go`, and other command files, replace:
- `cfg.Harness` → `cfg.Adapter`
- `workspace.ConfigWithModelReasoning(cfg, model, reasoning)` → update to set `cfg.Model` and `cfg.Reasoning` directly

In `harness_match.go` (rename to `adapter_match.go`):
- `projectHarness` → `projectAdapter`
- `st.Harness` → `st.Adapter` (also update `task.State` struct)

In `config_wizard.go`:
- `validateConfigValues` — accept any adapter name, not just hardcoded list
- Discovery: list available adapters from `~/.subtask/adapters/` + built-in
- Remove harness-specific defaults (adapter YAML defines its own defaults)

**Step 3: Update task.State**

In `pkg/task/state.go`, rename `Harness` field to `Adapter`:
```go
type State struct {
	Workspace      string    `json:"workspace,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
	Adapter        string    `json:"adapter,omitempty"`  // was: Harness
	// ... rest unchanged
}
```

Add legacy migration: if `json:"harness"` is present, read into Adapter.

**Step 4: Run full test suite**

Run: `go test ./... -short`
Expected: All tests PASS (may need to fix remaining references)

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: wire up config-driven adapter system, update all callers"
```

---

### Task 8: Delete old harness files and consolidate

Now that everything goes through `ConfigurableAdapter`, remove the old bespoke harness structs. Move their session operation code into `session_handlers.go`.

**Files:**
- Delete: `pkg/harness/codex.go` (keep session ops, move to session_handlers.go)
- Delete: `pkg/harness/claude.go` (keep session ops, move to session_handlers.go)
- Delete: `pkg/harness/opencode.go`
- Delete: `pkg/harness/cli_resolution.go` (keep `commandForCLI`, `CanResolveCLI`, move to configurable.go or a small cli.go)
- Modify: `pkg/harness/session_handlers.go` — inline the session functions instead of delegating to old structs
- Update/delete old tests that reference removed types

**Step 1: Move session operations**

Move `ClaudeHarness.MigrateSession`, `ClaudeHarness.DuplicateSession`, `CodexHarness.DuplicateSession` (and their helper functions: `findCodexSessionFile`, `copyCodexSessionWithNewID`, `escapeClaudeProjectDir`, `copyFile`, `copyDir`, `replaceAllInFile`, `replaceAllInDir`, `fileContains`, `dirContains`) into `session_handlers.go`.

Update `migrateSessionByHandler` and `duplicateSessionByHandler` to call these functions directly instead of through the old struct methods.

**Step 2: Move CLI resolution**

Keep `commandForCLI`, `CanResolveCLI`, `findCLIInCommonLocations`, `commonCandidatePaths`, `isExecutableFile` and related functions. Move them to a small `pkg/harness/cli.go` file. These are still needed by `ConfigurableAdapter.Run()`.

**Step 3: Delete the old harness files**

Remove `codex.go`, `claude.go`, `opencode.go` (the struct definitions and Run/Review methods — session ops and CLI helpers have been moved).

**Step 4: Clean up tests**

- `codex_stream_test.go` — references `parseCodexExecJSONL` which is now in `parse.go`. Update import path or rename.
- `claude_test.go` — same for `parseClaudeStream`
- `codex_test.go`, `opencode_test.go` — if they test Run() with the old struct, update to use `ConfigurableAdapter` or remove
- `cli_resolution_test.go` — tests for `commandForCLI` etc. Move to `cli_test.go`
- `codex_run_test.go`, `codex_review_args_test.go` — assess if they test adapter-specific logic that should be covered by configurable adapter tests instead

**Step 5: Run full test suite**

Run: `go test ./... -short`
Expected: All tests PASS

**Step 6: Commit**

```bash
git add -A
git commit -m "refactor: remove bespoke harness structs, consolidate into config-driven adapter"
```

---

### Task 9: E2E test with built-in mock

Verify the full flow works end-to-end with the mock adapter.

**Files:**
- Modify: `e2e/` (add or update an e2e test)

**Step 1: Verify existing e2e tests pass**

Run: `go test ./e2e/ -v -count=1`

If tests reference `harness` in config.json, update them to use `adapter`.

**Step 2: Run the full build**

```bash
go build ./cmd/subtask
```

**Step 3: Manual smoke test**

```bash
./subtask config --user  # verify wizard works with adapter terminology
./subtask list           # verify it loads config properly
```

**Step 4: Commit any e2e fixes**

```bash
git add -A
git commit -m "test: update e2e tests for config-driven adapter system"
```

---

### Task 10: Write a user-defined adapter to validate the workflow

Create a sample `aider.yaml` adapter to verify that user-defined adapters work end-to-end.

**Files:**
- Create: example adapter YAML (not embedded — just documentation/testing)

**Step 1: Write a sample aider adapter**

```yaml
# Example: save to ~/.subtask/adapters/aider.yaml
name: aider
cli: aider
args:
  - "--model"
  - "{{model}}"
  - "--yes-always"
  - "--no-auto-commits"
  - "--message"
  - "{{prompt}}"
prompt_via: arg
output_parser: text
capabilities:
  continue_session: false
  review: false
session_handler: none
```

Note: aider uses `--message` for non-interactive mode. The `{{prompt}}` template in the args handles this (prompt is in the flag value, not a trailing positional).

For this adapter, set `prompt_via: arg` but the prompt is already embedded in the args via `{{prompt}}`. The adapter should detect that `{{prompt}}` appears in args and NOT append it again as a positional. Update `buildArgs` in Task 4 to check for this:

```go
// In buildArgs: don't append prompt as positional if it already appears in args
promptInArgs := false
for _, arg := range a.config.Args {
    if strings.Contains(arg, "{{prompt}}") {
        promptInArgs = true
        break
    }
}
if a.config.PromptVia != "stdin" && !promptInArgs && vars.Prompt != "" {
    args = append(args, vars.Prompt)
}
```

**Step 2: Verify it loads**

```bash
./subtask config --user  # select aider if available, or manually set adapter: aider
```

**Step 3: Commit the example and the buildArgs fix**

```bash
git add -A
git commit -m "feat: handle prompt-in-args pattern for user-defined adapters"
```
