# Presets

An optional concept for projects that want their dispatch policy ("codex for backend, claude for UI, opus for review") in versioned config instead of CLAUDE.md prose, plus an extension to routines for per-step harness binding.

If you don't define any of them, subtask works exactly as before.

## Presets — shorthand for adapter/model/reasoning

A **Preset** names an `adapter + model + reasoning` bundle. Examples: `sonnet-medium`, `opus-high`, `gpt-5-low`.

`.subtask/config.json`:

```json
{
  "adapter": "claude",
  "max_workspaces": 20,

  "presets": {
    "sonnet-medium": { "adapter": "claude", "model": "sonnet", "reasoning": "medium" },
    "opus-high":     { "adapter": "claude", "model": "opus",   "reasoning": "high" },
    "gpt-5-low":     { "adapter": "codex",  "model": "gpt-5",  "reasoning": "low" }
  }
}
```

Listing command:

```
subtask presets    # show available presets
```

## Usage

```
subtask draft fix-bug --preset gpt-5-low --base-branch main --title "Fix bug"
subtask send  fix-bug --preset opus-high "Take another look"
```

`--preset` on `send` is a one-off override that does not change the task's locked harness.

## Per-step presets in routines

Routine YAML steps gain an optional `preset` field. When a task enters a step with a binding, the harness automatically swaps to that preset for the next send.

```yaml
# .subtask/routines/plan-impl-opus-review.yaml
name: plan-impl-opus-review
description: Plan and implement on sonnet, review on opus
steps:
  - id: plan
    preset: sonnet-medium
    instructions: |
      Plan the work in PLAN.md. ...
  - id: implement
    preset: sonnet-medium
    instructions: |
      Implement per PLAN.md. ...
  - id: review
    preset: opus-high
    instructions: |
      Review the implementation against PLAN.md. ...
  - id: ready
    kind: terminal
    instructions: |
      Task is ready for human review.
```

A step without a `preset` field stays on the last-used harness. The `ready` step (terminal) typically has no worker activity, so leaving it unbound is the right call.

### Cross-adapter swaps

When the swap crosses adapters (e.g., `gpt-5-low` for impl, then `opus-high` for review), the previous session is cleared and the new adapter starts fresh. Cross-step context comes from files, not from session history:

- The workspace contains the code committed by the previous worker.
- `PLAN.md` (if any) carries the plan.
- `PROGRESS.json` (if any) tracks completion state.
- `TASK.md` carries the description and current step.

A reviewer doesn't need the implementer's chat log; they need the code and the plan. This is consistent with subtask's file-based collaboration model.

### Per-step worker instructions

Step `instructions` is rendered to the lead's terminal on `subtask stage` / `subtask draft`; it never reaches the worker prompt. When the worker's *role* changes per step — most commonly a review step where the worker must not modify files — set `worker_instructions` so the brief is appended to the worker prompt automatically:

```yaml
steps:
  - id: implement
    preset: gpt-5-high
    instructions: |
      Worker is implementing.
  - id: review
    preset: gemini-pro
    instructions: |
      Review code with `subtask diff <task>` and request changes via `subtask send`.
    worker_instructions: |
      Findings only — do NOT modify files.
      Write your full review to REVIEW.md using:
        Critical / Important / Minor / Out-of-scope
      Do not run tests. Do not commit.
```

Without `worker_instructions`, a `review`-step worker sees the same prompt shape as the `implement` step — task description, routine-wide worker prose, and the lead's send prompt. Workspace ambient state (a `PLAN.md` left by the previous worker, prior commits) often gives the worker a "continue the work" prior strong enough to override a terse send like `"Review now."`. `worker_instructions` is what encodes the role swap once at the routine level so the lead doesn't have to inline it on every send.

### Snapshot semantics

Routines are resolved at draft time; the routine name is stored in TASK.md frontmatter. The routine YAML itself is read from `.subtask/routines/<name>.yaml` (or the built-in canonical) at runtime, so edits to the project's routine definition do take effect for running tasks. To lock the routine for a task, copy the YAML to `.subtask/routines/<name>.yaml` before drafting.

## Resolution order at draft

Each layer fills only fields not already set by an earlier layer:

1. Explicit flags (`--adapter`, `--provider`, `--model`, `--reasoning`) win.
2. `--preset <name>` resolves the preset's fields.
3. The routine loads. If its entry step has a `preset:` binding, that preset is the starting harness — only filling fields still unset.
4. Anything still unset falls through to project then user config defaults.

## What doesn't exist (and why)

We don't have a separate "flow" config concept. Per-step harness binding lives in routine YAML directly, since that's where step definitions already live. Splitting "step names" (routine) from "step→preset map" (a hypothetical flow config) would have been a parallel concept where one already covers the ground.
