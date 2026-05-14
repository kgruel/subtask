# Presets

An optional concept for projects that want their dispatch policy ("codex for backend, claude for UI, opus for review") in versioned config instead of CLAUDE.md prose, plus an extension to workflows for per-stage harness binding.

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

### Per-stage worker instructions

Stage `instructions` is rendered to the lead's terminal on `subtask stage` / `subtask draft`; it never reaches the worker prompt. When the worker's *role* changes per stage — most commonly a review stage where the worker must not modify files — set `worker_instructions` so the brief is appended to the worker prompt automatically:

```yaml
stages:
  - name: implement
    preset: gpt-5-high
    instructions: |
      Worker is implementing.
  - name: review
    preset: gemini-pro
    instructions: |
      Review code with `subtask diff <task>` and request changes via `subtask send`.
    worker_instructions: |
      Findings only — do NOT modify files.
      Write your full review to REVIEW.md using:
        Critical / Important / Minor / Out-of-scope
      Do not run tests. Do not commit.
```

Without `worker_instructions`, a `review`-stage worker sees the same prompt shape as the `implement` stage — task description, workflow-wide worker prose, and the lead's send prompt. Workspace ambient state (a `PLAN.md` left by the previous worker, prior commits) often gives the worker a "continue the work" prior strong enough to override a terse send like `"Review now."`. `worker_instructions` is what encodes the role swap once at the workflow level so the lead doesn't have to inline it on every send.

### Snapshot semantics

Workflows are copied into the task folder at draft time (`<task>/WORKFLOW.yaml` — `pkg/workflow/workflow.go:184`). Editing the project's workflow definition later does *not* change running tasks; they continue using whatever was current at draft. To change a running task's binding, edit the task-folder copy directly.

## Resolution order at draft

Each layer fills only fields not already set by an earlier layer:

1. Explicit flags (`--adapter`, `--provider`, `--model`, `--reasoning`, `--workflow`) win.
2. `--preset <name>` resolves the preset's fields.
3. The workflow loads. If its first stage has a `preset:` binding, that preset is the starting harness — only filling fields still unset.
4. Anything still unset falls through to project then user config defaults.

## What doesn't exist (and why)

We don't have a separate "flow" config concept. Per-stage harness binding lives in workflow YAML directly, since that's where stage definitions already live and they're already snapshotted into the task folder. Splitting "stage names" (workflow) from "stage→preset map" (a hypothetical flow config) would have been a parallel concept where one already covers the ground.
