package main

import (
	"fmt"
	"os"

	"github.com/zippoxer/subtask/internal/homedir"
	"github.com/zippoxer/subtask/pkg/install"
	"github.com/zippoxer/subtask/pkg/task"
)

// InstallCmd implements 'subtask install'.
type InstallCmd struct {
	Guide    bool `help:"Print setup guidance and exit"`
	NoPrompt bool `help:"Non-interactive; use defaults"`
}

func (c *InstallCmd) Run() error {
	if c.Guide {
		printSetupGuide()
		return nil
	}

	homeDir, err := homedir.Dir()
	if err != nil {
		return err
	}

	once, err := install.RunLegacyClaudePluginMigrationOnce(homeDir)
	if err != nil {
		return err
	}
	if once.Ran && once.Migration.SkippedSettingsMalformed {
		printWarning(fmt.Sprintf("Skipped legacy settings cleanup (malformed JSON at %s)", abbreviatePath(once.Migration.SettingsPath)))
	}
	if once.Ran && (once.Migration.RemovedLegacyPluginDir || once.Migration.RemovedLegacySettingsKey) {
		printSuccess("Removed legacy Claude plugin install artifacts")
	}

	skillPath, updated, err := install.InstallTo(homeDir)
	if err != nil {
		return err
	}
	if updated {
		printSuccess(fmt.Sprintf("Installed skill to %s", abbreviatePath(skillPath)))
	} else {
		printSuccess(fmt.Sprintf("Skill already up to date at %s", abbreviatePath(skillPath)))
	}

	// If not configured yet, run the config wizard and write ~/.subtask/config.json.
	if _, err := os.Stat(task.ConfigPath()); os.IsNotExist(err) {
		cfg, _, err := runConfigWizard(configWizardParams{
			WritePath: task.ConfigPath(),
			Existing:  readConfigFileOrNil(task.ConfigPath()),
			NoPrompt:  c.NoPrompt,
		})
		if err != nil {
			return err
		}
		if cfg != nil {
			printSuccess("Configured subtask")
		}
	}

	return nil
}

func printSetupGuide() {
	fmt.Print(`# Setup Subtask

Subtask lets you (the lead agent) dispatch tasks to parallel AI workers. Each worker runs in an isolated git worktree, so they can work simultaneously without conflicts. You draft tasks, send prompts to workers, review their output, and merge when ready.

Setup does two things:
1. Installs the Subtask skill - this teaches you the commands and workflow for orchestrating workers.
2. Configures which AI harness to use for workers (e.g., Codex, Claude Code).

## Requirements

This must be run inside a Git repository. If not, tell the user and stop.

## Install

` + "```bash\nsubtask install\n```" + `

This installs the skill to ~/.claude/skills/subtask/ and runs a configuration wizard for ~/.subtask/config.json. The wizard asks which harness to use for workers.

## Optional: project overrides

` + "```bash\nsubtask config --project\n```" + `

Only needed if this repository needs different settings than global defaults (e.g., different harness or fewer workspaces).

## Done

Let the user know Subtask is ready. Give examples of what they can ask you to do, like:
- "fix the login bug with Subtask"
- "run these 3 features in parallel"
- "plan and implement the new API endpoint with Subtask"

Adapt these to their project context, tastefully (if relevant).
`)
}
