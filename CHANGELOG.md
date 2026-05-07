## [0.4.2] - 2026-05-07

### Added
- `--preset` and `--adapter` flags on `subtask review`. Mirrors the override surface on `send` so the reviewer adapter/model can be swapped per call without editing TASK.md.
- `--preset` and `--adapter` flags on `subtask ask`.
- `subtask stage` auto-dispatches the worker when the new stage has `worker_instructions:` defined in the workflow YAML, blocking until reply (same semantics as `subtask send` — run with `run_in_background: true` from the lead). Eliminates the manual `subtask send` follow-up that was previously required for workflow-driven pipelines.
- `--no-send` flag on `subtask stage` to opt out of auto-dispatch.
- Optional positional prompt on `subtask stage` that extends `worker_instructions` (or dispatches alone when no `worker_instructions:` is set).
- `subtask install` now writes the plugin (hooks + scripts) in addition to the skill, eliminating the previous two-step setup. Marketplace-installed and dev-symlinked plugins are detected and left alone via an ownership marker (`.subtask-binary-installed`).
- `subtask uninstall` now removes both the skill and the binary-installed plugin. Marketplace installs are left alone with a printed note.
- `--skill-only` flag on `subtask install` and `subtask uninstall` to opt out of plugin install/removal.
- `stale-workers.sh` hook wired into `UserPromptSubmit`. Surfaces tasks whose worker has been running without history activity longer than `SUBTASK_STALE_THRESHOLD_MIN` (default 30 minutes), so the lead doesn't lose track of a hung or unproductive worker.

### Changed
- `subtask review --task <task>` now resolves adapter/provider/model/reasoning from the task's TASK.md snapshot (matching `send`'s long-standing behavior). Previously it ignored the task and used the project default, which broke workflows where the task ran under a different preset than the project default.
- `subtask ask --follow-up <task>` now resolves adapter/provider/model/reasoning from the task. Harness-match validation runs against the resolved adapter, not the project default — preventing a session ID from being sent into the wrong harness.
- All adapters (including `pi`) can now invoke `subtask review`. The `Capabilities.Review` and `Capabilities.ContinueSession` flags were no-op gates that didn't actually test anything technical — `Review()` was always just `buildReviewPrompt + Run`. The flags are removed.
- `marketplace.json` description now reflects all supported adapters (Claude / Codex / OpenCode / Gemini / Pi).

### Fixed
- `subtask review --task` no longer fails with "adapter does not support review" when the project default is `pi` but the task was drafted with a `claude`-based preset.
- `subtask ask --follow-up <task>` no longer silently sends a task's session ID into the project-default harness when the task ran under a different adapter.
- `subtask stage` auto-dispatch no longer injects `worker_instructions` into the worker prompt twice. `BuildPrompt` already emits them in a `## Stage:` block; the auto-dispatch path now passes only the lead's positional prompt (or a short stage trigger when none is given). The dispatch print message also correctly distinguishes the three sources (`worker_instructions` / `prompt` / both).
- `subtask uninstall` no longer silently removes dev symlinks (`subtask install --plugin-dev`) or pre-existing stray symlinks at the plugin path. Both are preserved with a printed note pointing at the manual `rm` command, mirroring the install-side asymmetry. Defaults preserve (design principle #8).

### Removed
- `Capabilities.Review` and `Capabilities.ContinueSession` from the adapter YAML schema. Both were declared but never gated real behavior. Adapter YAMLs no longer carry a `capabilities:` block.

### Documentation
- `CLAUDE.md` documents snapshot semantics for TASK.md (resolved adapter/model/reasoning are captured at draft time and don't auto-update from `.subtask/config.json` edits) and the recovery path via `--preset` on `send`/`review`/`ask --follow-up`.
- `CLAUDE.md` documents `subtask stage`'s auto-dispatch behavior, `--no-send` opt-out, and the optional positional prompt.
- `CLAUDE.md` clarifies that `stage`'s preset mechanism is the workflow YAML `preset:` binding, not a `--preset` CLI flag; `--preset` is reserved for the one-off send/review/ask paths.
- `SKILL.md` updated for the new override surface (`--preset` / `--adapter` across `draft` / `send` / `review` / `ask`), snapshot semantics, and stage auto-dispatch (including explicit `run_in_background: true` guidance for the lead).
- `docs/adding-an-adapter.md` example YAML drops the `capabilities:` block now that the field is gone from `AdapterConfig`.
