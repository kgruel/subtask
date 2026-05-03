# Types and presets

Two optional concepts for projects that want their dispatch policy ("codex for backend, claude for UI, opus for review") in versioned config instead of CLAUDE.md prose, plus an extension to workflows for per-stage harness binding.

If you don't define any of them, subtask works exactly as before.

## The two concepts

| Layer | What it names | Example |
|---|---|---|
| **Preset** | adapter + model + reasoning bundle | `sonnet-medium`, `opus-high`, `gpt-5-low` |
| **Type** | purpose of a task | `implement`, `review`, `docs`, `explore` |

A type can bind a default workflow and/or a default preset; both fall through to project / user config defaults when unset. Per-stage harness binding lives in workflow YAML — see "Per-stage presets in workflows" below.

## Quick start

`.subtask/config.json`:

```json
{
  "adapter": "claude",
  "max_workspaces": 20,

  "presets": {
    "sonnet-medium": { "adapter": "claude", "model": "sonnet", "reasoning": "medium" },
    "opus-high":     { "adapter": "claude", "model": "opus",   "reasoning": "high" },
    "gpt-5-low":     { "adapter": "codex",  "model": "gpt-5",  "reasoning": "low" }
  },

  "types": {
    "implement": {
      "default_workflow": "they-plan",
      "default_preset":   "sonnet-medium",
      "description":      "Backend or feature implementation"
    },
    "review": {
      "default_preset": "opus-high",
      "description":    "Audit task — worker reviews code in scope"
    },
    "explore": {
      "default_preset": "sonnet-medium",
      "description":    "Investigation, no plan needed"
    }
  }
}
```

Listing commands:

```
subtask presets    # show available presets
subtask types      # show available types
```

## Usage

### Presets — shorthand for adapter/model/reasoning

```
subtask draft fix-bug --preset gpt-5-low --base-branch main --title "Fix bug"
subtask send  fix-bug --preset opus-high "Take another look"
```

`--preset` on `send` is a one-off override that does not change the task's locked harness.

### Types — name the kind of work

```
subtask draft fix-bug --type implement --base-branch main --title "Fix bug"
```

The type's default_workflow + default_preset resolve. The type label is recorded in TASK.md frontmatter.

Follow-up tasks (`--follow-up <parent>`) inherit the parent's type when no `--type` is given. The inherited type's defaults then apply normally.

## Per-stage presets in workflows

Workflow YAML stages gain an optional `preset` field. When a task enters a stage with a binding, the harness automatically swaps to that preset for the next send.

```yaml
# .subtask/workflows/plan-impl-opus-review/WORKFLOW.yaml
name: plan-impl-opus-review
description: Plan and implement on sonnet, review on opus
stages:
  - name: plan
    preset: sonnet-medium
    instructions: |
      Plan the work in PLAN.md. ...
  - name: implement
    preset: sonnet-medium
    instructions: |
      Implement per PLAN.md. ...
  - name: review
    preset: opus-high
    instructions: |
      Review the implementation against PLAN.md. ...
  - name: ready
    instructions: |
      Task is ready for human review.
```

A stage without a `preset` field stays on the last-used harness. The `ready` stage typically has no worker activity, so leaving it unbound is the right call.

### Cross-adapter swaps

When the swap crosses adapters (e.g., `gpt-5-low` for impl, then `opus-high` for review), the previous session is cleared and the new adapter starts fresh. Cross-stage context comes from files, not from session history:

- The workspace contains the code committed by the previous worker.
- `PLAN.md` (if any) carries the plan.
- `PROGRESS.json` (if any) tracks completion state.
- `TASK.md` carries the description and current stage.

A reviewer doesn't need the implementer's chat log; they need the code and the plan. This is consistent with subtask's file-based collaboration model.

### Snapshot semantics

Workflows are copied into the task folder at draft time (`<task>/WORKFLOW.yaml` — `pkg/workflow/workflow.go:184`). Editing the project's workflow definition later does *not* change running tasks; they continue using whatever was current at draft. To change a running task's binding, edit the task-folder copy directly.

## Resolution order at draft

Each layer fills only fields not already set by an earlier layer:

1. Explicit flags (`--adapter`, `--provider`, `--model`, `--reasoning`, `--workflow`) win.
2. `--preset <name>` resolves the preset's fields.
3. `--type <name>` (or inherited from parent on follow-up) resolves the type's `default_workflow` + `default_preset`.
4. The workflow loads. If its first stage has a `preset:` binding, that preset is the starting harness — only filling fields still unset.
5. Anything still unset falls through to project then user config defaults.

## Validation

`.subtask/config.json` is validated at load time:

- A type's `default_preset` references a preset that doesn't exist → error with the available alternatives.

Workflow `preset:` references are validated at draft time when the workflow is loaded (workspace can't validate them at config load — workflows live on disk and may be project-overridden).

## Disambiguation: `review` task type vs. `review` workflow stage

Different things at different layers:

- A `review` **task type** is "worker reviews code as a subtask" — a whole-task audit, typically with an `opus-high`-style preset.
- A workflow's `review` **stage** is the in-task review pass after implementation, where the harness swaps to a stronger preset for the same task.

A project can have both. They don't conflict.

## What doesn't exist (and why)

We don't have a separate "flow" config concept. Per-stage harness binding lives in workflow YAML directly, since that's where stage definitions already live and they're already snapshotted into the task folder. Splitting "stage names" (workflow) from "stage→preset map" (a hypothetical flow config) would have been a parallel concept where one already covers the ground. See `docs/dev/issues/task-types-and-flows.md` for the design history.
