package main

import (
	"fmt"

	"github.com/kgruel/subtask/internal/homedir"
	"github.com/kgruel/subtask/pkg/install"
)

// UninstallCmd implements 'subtask uninstall'.
type UninstallCmd struct {
	SkillOnly bool `help:"Remove the skill only; skip removing the plugin. Use if the plugin was installed via the marketplace and you want to keep it."`
}

func (c *UninstallCmd) Run() error {
	homeDir, err := homedir.Dir()
	if err != nil {
		return err
	}

	path, err := install.UninstallFrom(homeDir)
	if err != nil {
		return err
	}
	printSuccess(fmt.Sprintf("Removed skill from %s", abbreviatePath(path)))

	if c.SkillOnly {
		return nil
	}

	pluginRes, err := install.UninstallPluginBinaryFrom(homeDir)
	if err != nil {
		return err
	}
	switch pluginRes.Action {
	case "removed":
		printSuccess(fmt.Sprintf("Removed plugin from %s", abbreviatePath(pluginRes.Path)))
	case "nothing":
		// Plugin was never installed — no message needed.
	case "marketplace":
		fmt.Println()
		fmt.Printf("Plugin at %s was not removed (marketplace-installed).\n", abbreviatePath(pluginRes.Path))
		fmt.Println("To remove it: /plugin uninstall subtask")
		fmt.Printf("Or manually:  rm -rf %s\n", pluginRes.Path)
	case "dev_link":
		fmt.Println()
		fmt.Printf("Plugin at %s is a dev symlink (--plugin-dev); preserved.\n", abbreviatePath(pluginRes.Path))
		fmt.Printf("Remove manually with: rm %s\n", pluginRes.Path)
	case "stray":
		fmt.Println()
		fmt.Printf("Plugin at %s is a symlink with no valid target; preserved.\n", abbreviatePath(pluginRes.Path))
		fmt.Printf("Remove manually with: rm %s\n", pluginRes.Path)
	}
	return nil
}
