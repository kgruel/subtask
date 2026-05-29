package install

// AutoUpdateResult captures which installed components were updated to match embedded assets.
type AutoUpdateResult struct {
	UpdatedSkill  bool
	UpdatedPlugin bool
}

func AutoUpdateIfInstalled(baseDir, version string) (AutoUpdateResult, error) {
	var res AutoUpdateResult

	if isSkillInstalled(baseDir) {
		_, updated, err := InstallTo(baseDir)
		if err != nil {
			return AutoUpdateResult{}, err
		}
		res.UpdatedSkill = updated
	}

	// Keep the binary-installed plugin in lockstep with the binary version
	// (CLAUDE.md version-coupling invariant). InstallPluginBinaryTo leaves
	// dev symlinks and marketplace installs alone and content-compares, so
	// this is a cheap no-op when nothing changed.
	st, err := GetPluginStatusFor(baseDir)
	if err != nil {
		return AutoUpdateResult{}, err
	}
	if st.IsBinaryInstalled {
		pluginRes, err := InstallPluginBinaryTo(baseDir, version)
		if err != nil {
			return AutoUpdateResult{}, err
		}
		res.UpdatedPlugin = pluginRes.Action == "updated"
	}

	return res, nil
}
