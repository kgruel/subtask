package main

import (
	"github.com/zippoxer/subtask/pkg/install"
	"github.com/zippoxer/subtask/pkg/render"
)

// StatusCmd implements 'subtask status'.
type StatusCmd struct{}

func (c *StatusCmd) Run() error {
	userBase, _, err := baseDirForScope(install.ScopeUser)
	if err != nil {
		return err
	}
	projectBase, _, err := baseDirForScope(install.ScopeProject)
	if err != nil {
		return err
	}

	user, err := install.GetScopeStatus(install.ScopeUser, userBase)
	if err != nil {
		return err
	}
	project, err := install.GetScopeStatus(install.ScopeProject, projectBase)
	if err != nil {
		return err
	}

	printScopeStatus("User", user)
	printScopeStatus("Project", project)
	return nil
}

func printScopeStatus(title string, st install.ScopeStatus) {
	printSection(title)

	skillInstalled := "no"
	skillUpToDate := "-"
	skillSHA := "-"
	if st.Skill.Installed {
		skillInstalled = "yes"
		skillUpToDate = yesNo(st.Skill.UpToDate)
		if st.Skill.InstalledSHA256 != "" {
			skillSHA = shortHash(st.Skill.InstalledSHA256)
		}
	}

	pluginInstalled := "no"
	pluginUpToDate := "-"
	pluginSHA := "-"
	if st.Plugin.Installed {
		pluginInstalled = "yes"
		pluginUpToDate = yesNo(st.Plugin.UpToDate)
		if st.Plugin.InstalledSHA256 != "" {
			pluginSHA = shortHash(st.Plugin.InstalledSHA256)
		}
	}

	settingsExists := "no"
	settingsEnabled := "-"
	settingsErr := "-"
	if st.Settings.Exists {
		settingsExists = "yes"
		settingsEnabled = yesNo(st.Settings.PluginEnabled)
	}
	if st.Settings.Error != "" {
		settingsErr = st.Settings.Error
	}

	kv := &render.KeyValueList{
		Pairs: []render.KV{
			{Key: "Skill path", Value: abbreviatePath(st.Skill.Path)},
			{Key: "Skill installed", Value: skillInstalled},
			{Key: "Skill up-to-date", Value: skillUpToDate},
			{Key: "Skill embedded SHA256", Value: shortHash(st.Skill.EmbeddedSHA256)},
			{Key: "Skill installed SHA256", Value: skillSHA},
			{Key: "Plugin dir", Value: abbreviatePath(st.Plugin.Dir)},
			{Key: "Plugin installed", Value: pluginInstalled},
			{Key: "Plugin up-to-date", Value: pluginUpToDate},
			{Key: "Plugin embedded SHA256", Value: shortHash(st.Plugin.EmbeddedSHA256)},
			{Key: "Plugin installed SHA256", Value: pluginSHA},
			{Key: "Settings path", Value: abbreviatePath(st.Settings.Path)},
			{Key: "Settings exists", Value: settingsExists},
			{Key: "Plugin enabled", Value: settingsEnabled},
			{Key: "Settings error", Value: settingsErr},
		},
	}
	kv.Print()
}

func shortHash(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}
