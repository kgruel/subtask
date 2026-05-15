package install

// AutoUpdateResult captures which installed components were updated to match embedded assets.
type AutoUpdateResult struct {
	UpdatedSkill bool
}

func AutoUpdateIfInstalled(baseDir string) (AutoUpdateResult, error) {
	var res AutoUpdateResult

	if isSkillInstalled(baseDir) {
		_, updated, err := InstallTo(baseDir)
		if err != nil {
			return AutoUpdateResult{}, err
		}
		res.UpdatedSkill = updated
	}

	return res, nil
}
