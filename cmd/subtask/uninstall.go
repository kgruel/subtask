package main

import (
	"fmt"

	"github.com/zippoxer/subtask/pkg/install"
)

// UninstallCmd implements 'subtask uninstall'.
type UninstallCmd struct {
	Skill  bool   `help:"Uninstall only the skill"`
	Plugin bool   `help:"Uninstall only the plugin"`
	Scope  string `default:"user" enum:"user,project" help:"Installation scope"`
}

func (c *UninstallCmd) Run() error {
	scope, err := parseInstallScope(c.Scope)
	if err != nil {
		return err
	}

	removeSkill := c.Skill
	removePlugin := c.Plugin
	if !c.Skill && !c.Plugin {
		removeSkill = true
		removePlugin = true
	}

	baseDir, _, err := baseDirForScope(scope)
	if err != nil {
		return err
	}

	res, err := install.UninstallAll(install.UninstallRequest{
		Scope:   scope,
		BaseDir: baseDir,
		Skill:   removeSkill,
		Plugin:  removePlugin,
	})
	if err != nil {
		return err
	}

	if removeSkill {
		printSuccess(fmt.Sprintf("Removed skill from %s", abbreviatePath(res.SkillPath)))
	}
	if removePlugin {
		printSuccess(fmt.Sprintf("Removed plugin from %s", abbreviatePath(res.PluginDir)))
		if res.Settings.Rewrote && res.Settings.BackupTo != "" {
			printWarning(fmt.Sprintf("Rewrote malformed settings.json (backup at %s)", abbreviatePath(res.Settings.BackupTo)))
		}
	}

	return nil
}
