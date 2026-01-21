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

Subtask is a CLI for orchestrating parallel AI workers. You (the lead agent) use it to dispatch tasks to workers running in isolated git worktrees. Each worker gets its own workspace so they can't conflict with each other.

Install the Subtask skill and configure it for use in any Git repository.

## Requirements

Check if we're inside a Git repository. If not, let the user know that Subtask requires a Git repository and stop.

## Install + configure (global)

` + "```bash\nsubtask install\n```" + `

This installs the Subtask skill to ~/.claude/skills/subtask/ and runs a configuration wizard to set up ~/.subtask/config.json.

## Optional: project overrides

` + "```bash\nsubtask config --project\n```" + `

Use this only if the current repository needs different settings than your global defaults.

## Done

Tell the user:

> Subtask is ready!
>
> Example usage:
> - "fix the login bug with Subtask"
> - "run these 3 features in parallel"
> - "plan and implement the new API endpoint with Subtask"
>
> I'll draft tasks, dispatch workers in isolated workspaces and let you know when they're done.
`)
}
