# Adding a Worker Adapter

Adapters teach Subtask how to invoke a specific AI coding CLI as a worker. They are declarative (YAML) where possible and require Go code only for parsing the CLI's streaming output and (optionally) migrating session state when workspaces move.

This guide walks through what's required, using the existing `gemini` adapter as the worked example.

## What an adapter has to do

For each task message, Subtask spawns the worker CLI and needs to:

1. **Invoke** it non-interactively with a prompt, model, and any auto-approval flags.
2. **Parse** its streaming output to extract: the session ID (so we can resume), the final reply text, and a count of tool calls (for progress visibility).
3. **Resume** a previous turn so multi-step conversations work.
4. **(Optional) Migrate sessions** if the CLI's session storage is keyed on workspace path and the user revives a closed/merged task in a new workspace.

## Files involved

| File | What it holds |
|---|---|
| `pkg/harness/adapters/<name>.yaml` | Declarative adapter config. Auto-discovered via `embed.FS`. |
| `pkg/harness/parse.go` | Stream parsers. Add a parser function and register it in `ParseByName`. |
| `pkg/harness/configurable.go` | `parseOutput` switch — must list your parser name to dispatch to `ParseByName`. |
| `pkg/harness/session_handlers.go` | Optional: session migration/duplication handlers. |
| `cmd/subtask/install.go`, `config.go`, `config_wizard.go` | Help text strings and the install-guide template that mentions known adapters. |

## Step 1: Write the YAML

Create `pkg/harness/adapters/<name>.yaml`. Schema is defined in `pkg/harness/adapter_config.go`.

```yaml
name: gemini
cli: gemini
args:
  - "--prompt"
  - "{{prompt}}"
  - "-o"
  - "stream-json"
  - "--approval-mode"
  - "yolo"
  - "-m"
  - "{{model}}"
prompt_via: arg
continue_args:
  - "--resume"
  - "{{session_id}}"
output_parser: gemini
capabilities:
  continue_session: true
  review: true
session_handler: none
env:
  OTEL_SDK_DISABLED: "true"
```

### Field notes

- **`args`**: built every invocation. Template variables (`{{model}}`, `{{prompt}}`, `{{session_id}}`, `{{provider}}`, `{{reasoning}}`, etc.) are substituted. If a `{{var}}` expands to empty *and* the arg is purely the template, both that arg and the preceding flag arg are dropped — so `-m {{model}}` with empty model becomes nothing.
- **`prompt_via`**: `arg` (default) or `stdin`. With `arg`, if `{{prompt}}` is in `args` it's substituted there; otherwise the prompt is appended as the last positional arg.
- **`continue_args`**: inserted *before* the prompt arg when resuming. `{{session_id}}` is filled with the ID captured by the parser on the prior turn.
- **`output_parser`**: one of `claude`, `codex`, `opencode`, `gemini`, `generic-jsonl`, `text`. To add a new one, see Step 2.
- **`session_handler`**: `none` (default), `claude`, `codex`, or your new one. Only matters when workspaces move (see Step 3).
- **`env`**: extra environment variables for the spawned CLI. Useful for silencing telemetry or setting non-interactive flags the CLI only honors via env.

The adapter is auto-discovered via `embed.FS` — no registration needed beyond the file existing.

## Step 2: Write a stream parser (if needed)

If your CLI emits a unique JSON-per-line format (most do), add a parser in `pkg/harness/parse.go`:

```go
func parseGeminiStream(r io.Reader, result *Result, cb Callbacks) error {
    scanner := bufio.NewScanner(r)
    buf := make([]byte, 0, 64*1024)
    scanner.Buffer(buf, 10*1024*1024) // CLIs can emit large lines

    for scanner.Scan() {
        var ev geminiStreamEvent
        if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
            continue // skip non-JSON / banner lines
        }

        // 1. On session start: set result.SessionID, fire OnSessionStart.
        // 2. On tool call: fire OnToolCall(time.Now()).
        // 3. On reply text: accumulate into result.Reply, set AgentReplied=true.
        // 4. On error: set result.Error.
    }
    return scanner.Err()
}
```

Then register it in two places:

```go
// pkg/harness/parse.go - ParseByName
case "gemini":
    return parseGeminiStream(r, result, cb)

// pkg/harness/configurable.go - parseOutput
case "claude", "codex", "opencode", "gemini":
    return ParseByName(parser, r, result, cb)
```

### What the parser must populate

| Field | Set from | Used for |
|---|---|---|
| `result.SessionID` | first session-start event | Resume on next turn |
| `result.PromptDelivered` | true once you've seen any output | "the worker received our prompt" |
| `result.AgentReplied` | true once final/assistant text arrives | Distinguishes replied from errored |
| `result.Reply` | accumulate assistant content | Shown to lead in `subtask show` |
| `result.Error` | error events | Surfaced to lead on failure |
| `cb.OnToolCall(t)` | each tool-use event | Tool counts in progress |
| `cb.OnSessionStart(id)` | first session-start event | Persists session ID for resume |

### If your CLI's format is too simple to need code

You can use the `generic-jsonl` parser with dot-path rules in the YAML. Useful when sessions and replies live at fixed JSON paths and there's no role-filtering needed. See `pkg/harness/adapters/pi.yaml` for an example.

## Step 3: Session migration (optional)

Most CLIs persist sessions to disk keyed on the workspace path (or a hash of it). When Subtask reuses a workspace ID for a revived task in a *different* path, `--resume <id>` will fail unless we move the session file.

Look at how Claude does it in `pkg/harness/session_handlers.go`:

- `migrateClaudeSession`: copies the session file from `~/.claude/projects/<old-escaped>/<id>.jsonl` to the new path, rewrites occurrences of the old cwd inside the file, deletes the source.
- `duplicateClaudeSession`: same but generates a new session ID, leaves the source.

Then dispatch them by adding a case in `migrateSessionByHandler` and `duplicateSessionByHandler`.

**You can ship without this** by setting `session_handler: none`. Tasks will resume fine within their original workspace; only revivals across workspaces will silently fail. Document the limitation.

### Known: Gemini session migration is unimplemented

Gemini stores sessions at `~/.gemini/tmp/<dir>/chats/<sessionId>.json` where `<dir>` is a path-derived slug whose exact algorithm is undocumented (it's not `sha256(cwd)` despite the `projectHash` field inside the JSON being exactly that). Reverse-engineering the slug is brittle without a Gemini API guarantee. Until that's resolved, the Gemini adapter ships with `session_handler: none` and reviving a Gemini task into a moved workspace breaks resume. If you pick this up, the JSON shape and `projectHash` semantics are documented in the siftd adapter at `~/Code/siftd/src/siftd/adapters/gemini_cli.py`.

## Step 4: Wire it into the install/config UX

Three small touchups:

1. **`cmd/subtask/config.go`** + **`install.go`** — add your adapter name to the `--adapter` help text.
2. **`cmd/subtask/install.go`** — add an availability probe (`isCommandAvailable("<cli>")`) and a line to the install-guide template under "Available worker adapters".
3. **`cmd/subtask/config_wizard.go`** — add your display name to the `displayNames` map, and (optionally) add an `else if` branch to the model-selection step if your CLI has a fixed list of recommended models. Add an install URL hint to `validateAdapterAvailable`.

## Step 5: Test

A few patterns from the existing adapters:

- **Parser unit test** in `pkg/harness/<name>_test.go` covering: session start, accumulated/streaming reply, tool-call detection, error status. See `gemini_test.go` and `opencode_test.go`.
- **End-to-end smoke**: `go build -o ./subtask ./cmd/subtask`, configure a throwaway repo with `subtask config --project --adapter <name> --no-prompt`, draft a task, send a real prompt that exercises tool use, verify reply and `"tool_calls":N` in `history.jsonl`.

## Checklist

- [ ] `pkg/harness/adapters/<name>.yaml` written
- [ ] Stream parser added to `parse.go` and registered in `ParseByName` + `parseOutput`
- [ ] (Optional) Session handler added and registered
- [ ] Help text and install guide updated
- [ ] Wizard display name + (optional) model branch added
- [ ] Parser unit test
- [ ] End-to-end smoke run
- [ ] README + AGENTS.md/CLAUDE.md adapter list updated if needed
