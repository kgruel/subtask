package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kgruel/subtask/internal/homedir"
	"github.com/kgruel/subtask/pkg/install"
	"github.com/kgruel/subtask/pkg/task"
)

// internalSyncPluginEnvVar gates a hidden re-exec path (see UpdateCmd.Run):
// after `subtask update` swaps the binary on disk, the still-running OLD
// process has the OLD version's embedded plugin/skill assets baked in, so it
// cannot refresh them itself. It re-execs the just-written new binary with
// this env var set; the new process runs only the sync below and exits,
// closing the binary<->plugin lockstep gap immediately instead of leaving it
// to the next incidental subtask invocation.
const internalSyncPluginEnvVar = "SUBTASK_INTERNAL_SYNC_PLUGIN"

func runInternalPluginSync() error {
	homeDir, err := homedir.Dir()
	if err != nil {
		return err
	}
	if homeDir == "" {
		return fmt.Errorf("could not resolve home directory")
	}
	_, err = install.AutoUpdateIfInstalled(homeDir, version)
	return err
}

func runAutoUpdate() {
	if os.Getenv(autoUpdateEnvVar) == "1" {
		return
	}

	// When the install command is running it manages the skill itself —
	// skip the auto-update write to avoid emitting a success line before
	// install's own message. Assumes no global flags precede the subcommand
	// name (true for this CLI today; revisit if a global flag is added).
	if !(len(os.Args) > 1 && os.Args[1] == "install") {
		homeDir, err := homedir.Dir()
		if err == nil && homeDir != "" {
			res, err := install.AutoUpdateIfInstalled(homeDir, version)
			if err == nil {
				// Stderr, not stdout: this is meta-status that fires before the
				// user's command runs. It must never contaminate stdout consumed
				// by pipes, hooks, or `subtask reply` / `subtask unread`.
				if res.UpdatedSkill {
					fmt.Fprintln(os.Stderr, "✓ Updated skill to latest version")
				}
				if res.UpdatedPlugin {
					fmt.Fprintln(os.Stderr, "✓ Updated plugin to latest version")
				}
			}
		}
	}

	repoRoot, err := task.GitRootAbs()
	if err != nil || repoRoot == "" {
		return
	}

	// Don't warn about a stale project skill when the user is running install —
	// the install command writes the updated skill itself, so the warning would
	// appear before the success line and confuse the user.
	if len(os.Args) > 1 && os.Args[1] == "install" {
		return
	}

	st, err := install.GetSkillStatusFor(repoRoot)
	if err != nil {
		return
	}
	if st.Installed && !st.UpToDate {
		printWarning("Project skill at " + filepath.Join(".claude", "skills", "subtask", "SKILL.md") + " is outdated. Run `subtask install --scope project` to update.")
	}
}
