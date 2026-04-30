package main

import (
	"github.com/kgruel/subtask/pkg/install"
	"github.com/kgruel/subtask/pkg/render"
)

// StatusCmd implements 'subtask status'.
type StatusCmd struct{}

func (c *StatusCmd) Run() error {
	st, err := install.GetSkillStatus()
	if err != nil {
		return err
	}

	skillInstalled := "no"
	skillUpToDate := "-"
	skillSHA := "-"
	if st.Installed {
		skillInstalled = "yes"
		skillUpToDate = yesNo(st.UpToDate)
		if st.InstalledSHA256 != "" {
			skillSHA = shortHash(st.InstalledSHA256)
		}
	}

	pst, _ := install.GetPluginStatus()
	pluginState := pluginStateLabel(pst)

	pairs := []render.KV{
		{Key: "Skill path", Value: abbreviatePath(st.Path)},
		{Key: "Skill installed", Value: skillInstalled},
		{Key: "Skill up-to-date", Value: skillUpToDate},
		{Key: "Skill embedded SHA256", Value: shortHash(st.EmbeddedSHA256)},
		{Key: "Skill installed SHA256", Value: skillSHA},
		{Key: "Plugin path", Value: abbreviatePath(pst.Path)},
		{Key: "Plugin state", Value: pluginState},
	}
	if pst.IsSymlink && pst.SymlinkTarget != "" {
		pairs = append(pairs, render.KV{Key: "Plugin link target", Value: abbreviatePath(pst.SymlinkTarget)})
	}

	kv := &render.KeyValueList{Pairs: pairs}
	kv.Print()
	return nil
}

func pluginStateLabel(st install.PluginStatus) string {
	switch {
	case !st.Exists:
		return "not installed"
	case st.IsSymlink && st.HasManifest:
		return "linked (dev)"
	case st.IsSymlink:
		return "broken symlink"
	case st.HasManifest:
		return "installed"
	default:
		return "present (no manifest)"
	}
}

func shortHash(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
