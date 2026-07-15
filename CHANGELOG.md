## [0.6.0] - 2026-07-15

### Added
- `subtask wait <task>...` blocks until named tasks finish (completion barrier), so a lead can fan out several `send --detach` calls and rejoin without polling.
- `--detach` flag on `subtask send` dispatches a detached supervisor process and returns as soon as it claims the task; retrieve the reply later with `subtask wait` + `subtask reply`.
- `consumes:` on routine steps renders a `## Inputs` block into the worker prompt from task-relative paths, existence-checking each and flagging missing ones.
- `--follow-up` now injects the parent task's artifacts (TASK.md/PLAN.md/PROGRESS.json and produced files) as a read-only `## Parent Context` block even when the parent's session can't be resumed (merged/closed, never dispatched, or a different harness adapter) — continuity degrades to artifact-only instead of failing outright.

### Fixed
- `subtask wait` / `subtask send`: a live supervisor claim now outranks a stale `merged`/`closed` status read during the race window, a routine-reload failure during auto-advance is recorded in `LastError` instead of silently dropped, and the supervisor claim is held across the auto-advance window so a fast follow-on `wait` can't observe a false idle.
- `draft --follow-up` / `send`: a cross-adapter parent now always degrades to artifact-only continuity per the documented contract — even a LIVE parent, which previously hard-failed instead of degrading — and a stale or deleted parent workspace path is now treated as not-live rather than tripping a cwd-keyed session duplication.
- Adapter subprocess failures now surface the adapter's stderr instead of a bare exit-code error.
- `subtask ask --follow-up` recovers the parent's session from portable history instead of passing the task name itself as a session ID, so cross-machine and cross-clone follow-ups resolve correctly.
- `git diff -z` output (null-delimited) is now parsed directly, removing the `=>` rename-notation ambiguity that could misattribute renamed files in diff stats.
- Routine `consumes:`/`produces:` artifact paths are validated with slash-only semantics instead of `filepath`, so nested paths (e.g. `notes/spec.md`) work on Windows; Windows drive-letter paths (`C:/...`) are now rejected portably on every platform.
- Release CI now runs `go test` across the OS matrix (Windows/macOS/Linux) before goreleaser, catching platform-specific regressions before they ship.
- `subtask update` refreshes the binary-managed plugin in the same invocation as the binary swap, instead of leaving the binary↔plugin version lockstep (documented in CLAUDE.md's Releasing section) broken until some later, incidental `subtask` invocation happened to run the new binary.
- `stale-workers.sh` is now tracked as executable (`100755`); marketplace/source installs no longer hit permission denied when the `UserPromptSubmit` hook shells out to it.

### Changed
- `/release` pushes the current branch before creating and pushing the release tag, instead of pushing the tag alone — a tag-only push could publish a release built from commits that never made it to `origin`.
- `.serena/` and `data/` (local serena MCP state and local DuckDB files) are now gitignored; they must never enter the release tree.

## [0.5.1] - Unreleased (version bumped on main 2026-07-12, never tagged; these changes first ship in 0.6.0)

### Added
- Free-text model input and reasoning-level support for the `claude` adapter in `subtask config`.
- TASK.md/PLAN.md surfaced in the TUI's Artifacts tab, with show/render polish.

### Changed
- `pkg/task/gather` (detail/list assembly) dissolved into `pkg/task/store`, unifying the read/list layer used by both the CLI and the TUI.
- `subtask install` refreshes the binary-managed plugin as part of a binary self-update, keeping the binary↔plugin version lockstep documented in CLAUDE.md.
- TUI/render internals standardized: width math and status rendering unified, unused `Table`/`Box`/dispatcher render stack deleted, `model_override` overlay semantics unified in `pkg/workspace`.

### Fixed
- `subtask merge`/`git`: no-op merges and raw rebase failures now surface actionable errors instead of failing silently (#7).
- `subtask draft` validates the task name as a git branch ref up front, rejecting invalid names before a workspace is created (#7).
- Numstat rename paths are resolved correctly in the git diff parser.
- `EnsureSchema` migration now locks against concurrent history truncation.
- Frozen merge stats are preserved across a no-op-finalize recompute in the task index.
- Reasoning level is validated at agent load instead of failing later at dispatch.
- `SUBTASK_DIR` is honored for user adapter overrides in the harness.
- `subtask install` warns instead of silently dropping unknown config flags.
- Cross-day log timestamps in `subtask log`/`trace` now include the date.
- `subtask update` only treats a pure `X.Y.Z` tag as a release and drops unextractable `.gz` assets, instead of misidentifying betas/malformed tags.

### Removed
- `pkg/task/gather` package (dissolved into `pkg/task/store` — see Changed).

## [0.5.0] - 2026-05-17

### Added
- Routine and Agent file formats (`.subtask/routines/*.yaml`, `.subtask/agents/*.yaml`) replace the legacy workflow YAML + WORKER.md system. Canonical routines (`default`, `they-plan`, `you-plan`) ship built-in; both are project-extensible.
- `subtask routines` and `subtask agents` discovery commands.
- `subtask stage` supports `advance: auto`, `produces:`/`consumes:` on routine steps, and a routine diagram surfacing gates, branches, terminals, and loopbacks.
- `subtask review --plan` reviews PLAN.md against the TASK.md spec.
- `subtask review --task` is event-sourced into the task's `history.jsonl`.
- `notify: false` on a routine step silences unread-reply nudges while a task is in that step.
- `worker_context:` on routine steps injects passive per-step context without triggering dispatch.
- `subtask list --json` output; parent→children task index (SQLite) with a `child.drafted` history event.
- `artifact.produced` history event.
- TUI: Artifacts tab (list mode, view mode, clipboard actions, count badge), `s`/`>`/`m` write-action keys (send/stage-advance/merge), onboarding via `subtask quickstart` and `subtask next <task>`.
- `subtask draft --base-branch` defaults to the current branch.

### Changed
- The `Type` concept is removed; tasks are now purely routine + agent driven.
- `Preset` is dissolved into `Agent` — agent files now carry what presets used to (adapter/model/reasoning), eliminating the peer-mutex between the two concepts.
- `subtask list` hides merged/closed tasks by default (`--all` shows everything); TUI is pinned to show all tasks.
- CLI surface renamed "Stage" to "Flow" in several render paths for clarity against the routine-step "stage" verb.
- SKILL.md rewritten primitive-first (verb-first previously), documenting the Routine+Agent model end to end.

### Removed
- Legacy workflow YAML format and `WORKER.md` (superseded by Routine + Agent files).
- `Preset` type and the `presets` project-config surface (dissolved into `Agent`, see Changed).

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
