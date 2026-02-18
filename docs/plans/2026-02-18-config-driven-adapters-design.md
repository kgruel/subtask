# Config-Driven Adapters

Replace hardcoded Go harness structs with a single generic adapter that reads YAML config. Adding support for a new CLI tool becomes a config file, not Go code.

## Context

The current harness layer has three bespoke Go structs (`codex.go`, `claude.go`, `opencode.go`), each implementing the `Harness` interface with CLI-specific flag construction, output parsing, and session management. Adding a new CLI tool requires writing Go, implementing 4 methods, and recompiling.

## Decision

Fork the Go codebase. Surgically replace the harness layer with config-driven adapters. Everything else stays.

## Adapter Config Format

```yaml
# ~/.subtask/adapters/claude.yaml
name: claude
cli: claude
args:
  - "--print"
  - "--output-format"
  - "stream-json"
  - "--model"
  - "{{model}}"
  - "--verbose"
args_continue:
  - "--resume"
  - "{{session_id}}"

output_format: jsonl                 # jsonl | text
parse:
  session_id: ".session_id"          # dot-path into JSON lines
  reply: ".result"
  cost: ".total_cost_usd"           # optional
  turns: ".num_turns"               # optional

capabilities:
  continue_session: true
  migrate_session: true
  review: true

session_migrate:
  command: "cp"
  args: ["-r", "{{old_cwd}}/.claude/projects/*/{{session_id}}*", "{{new_cwd}}/.claude/projects/*/"]
```

### Template Variables

| Variable | Source |
|----------|--------|
| `{{model}}` | Resolved model (CLI flag > task > config) |
| `{{prompt}}` | Full built prompt |
| `{{session_id}}` | Previous session ID (for continuation) |
| `{{cwd}}` | Workspace path |
| `{{reasoning}}` | Reasoning effort level |

## Adapter Discovery

```
~/.subtask/
├── config.json              # "adapter": "claude", "model": "claude-opus-4-6"
├── adapters/
│   ├── claude.yaml          # built-in (embedded, written on install)
│   ├── codex.yaml           # built-in
│   ├── opencode.yaml        # built-in
│   └── aider.yaml           # user-defined
```

Built-in adapters ship embedded in the binary via `embed.FS`. Written to `~/.subtask/adapters/` on install. User edits take precedence over embedded defaults.

## Config.json Changes

```json
{
  "adapter": "claude",
  "max_workspaces": 20,
  "model": "claude-opus-4-6"
}
```

- `harness` renamed to `adapter`
- `options.model` promoted to top-level `model`
- `options.reasoning` promoted to top-level `reasoning`
- `options` map removed (adapter-specific knobs live in adapter YAML)

## Generic Adapter Implementation

### Core Struct

```go
type Adapter struct {
    Name       string
    CLI        string
    Args       []string
    ArgsCont   []string
    Format     string         // "jsonl" | "text"
    Parse      ParseRules
    Caps       Capabilities
    Migrate    *MigrateConfig
}

type ParseRules struct {
    SessionID  string  // dot-path or null
    Reply      string  // dot-path or "stdout"
    Cost       string  // optional
    Turns      string  // optional
}

type Capabilities struct {
    ContinueSession  bool
    MigrateSession   bool
    Review           bool
}
```

### Run()

1. Template args with resolved values
2. Append `ArgsCont` if continuing a session
3. Spawn subprocess with stdout/stderr pipes
4. Parse output by format:
   - **jsonl**: scan lines, extract fields via dot-paths, last non-empty match wins
   - **text**: accumulate stdout as reply, no session ID
5. Return `Result{Reply, SessionID, Metrics}`

### Review()

If `Caps.Review` is true, build review prompt and call `Run()`. Otherwise error.

### MigrateSession() / DuplicateSession()

If `Migrate` config exists, template and execute migration command. Otherwise no-op with warning.

### JSONL Parsing

Simple dot-notation traversal (no full jq). Split path on `.`, traverse JSON object. Scan every line, keep last non-empty match per field.

## Impact

### Deleted
- `pkg/harness/codex.go`
- `pkg/harness/claude.go`
- `pkg/harness/opencode.go`
- `pkg/harness/cli_resolution.go`

### Added
- `pkg/harness/adapter.go` — generic adapter
- `pkg/harness/adapter_test.go`
- `pkg/harness/parse.go` — output parsing
- `workflow/adapters/{claude,codex,opencode}.yaml` — embedded defaults

### Modified
- `pkg/harness/harness.go` — `New()` loads adapter YAML
- `pkg/workspace/config.go` — field renames
- `pkg/workspace/model_override.go` — field access changes
- `cmd/subtask/send.go`, `ask.go` — `Harness` -> `Adapter` references
- `cmd/subtask/config_wizard.go` — discover adapters from directory
- `cmd/subtask/harness_match.go` — rename
- `cmd/subtask/install.go` — write embedded adapter YAMLs

### Untouched
Task management, workspace allocation, history, git ops, plugin/skill layer, TUI, render, workflow stages.

## Deferred

- **Dynamic adapter/model selection at dispatch time** — pick adapter + model per-send interactively, not from static config. Config-driven adapters are the foundation for this.
