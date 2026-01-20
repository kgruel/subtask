package main

import (
	"os"

	"github.com/zippoxer/subtask/pkg/install"
)

func runAutoUpdate() {
	if os.Getenv(autoUpdateEnvVar) == "1" {
		return
	}

	userBase, _, err := baseDirForScope(install.ScopeUser)
	if err != nil || userBase == "" {
		return
	}
	projectBase, _, err := baseDirForScope(install.ScopeProject)
	if err != nil || projectBase == "" {
		return
	}

	userRes, err := install.AutoUpdateIfInstalled(install.ScopeUser, userBase)
	if err != nil {
		return
	}
	projectRes, err := install.AutoUpdateIfInstalled(install.ScopeProject, projectBase)
	if err != nil {
		return
	}

	skillUpdated := userRes.UpdatedSkill || projectRes.UpdatedSkill
	pluginUpdated := userRes.UpdatedPlugin || projectRes.UpdatedPlugin

	if skillUpdated {
		printSuccess("Updated skill to latest version")
	}
	if pluginUpdated {
		printSuccess("Updated plugin to latest version")
	}
}
