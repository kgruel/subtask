package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kgruel/subtask/internal/binaryupdate"
	"github.com/kgruel/subtask/pkg/render"
)

var newBinaryUpdateClient = func() *binaryupdate.Client { return binaryupdate.NewClient(nil) }

// runSyncPluginChild re-execs the just-updated binary at exe with
// internalSyncPluginEnvVar set, so it refreshes the binary-managed
// plugin/skill with its own (new) embedded assets. Stubbed in tests.
var runSyncPluginChild = func(exe string) error {
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), internalSyncPluginEnvVar+"=1")
	return cmd.Run()
}

// refreshPluginAfterSwap keeps the binary<->plugin version lockstep
// (CLAUDE.md Releasing section) from ever being observably broken: the
// process running this code still has the OLD binary's embedded assets, so it
// re-execs the NEW binary at exe to do the refresh. Best-effort — a failure
// here does not undo the already-successful binary swap; the plugin will
// still catch up on the next incidental subtask invocation via runAutoUpdate.
func refreshPluginAfterSwap(exe string) {
	if err := runSyncPluginChild(exe); err != nil {
		printWarning("Updated the binary but could not refresh the installed plugin/skill immediately; the next subtask command will sync them.")
	}
}

// UpdateCmd implements 'subtask update'.
type UpdateCmd struct {
	Check bool `help:"Only check for updates (do not install)"`
}

func (c *UpdateCmd) Run() error {
	printSection("Update")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := newBinaryUpdateClient()
	rel, err := client.LatestRelease(ctx)
	if err != nil {
		return err
	}

	latest, latestOK := parseSemverVersion(rel.TagName)
	cur, curOK := parseSemverVersion(version)

	var pairs []render.KV
	pairs = append(pairs,
		render.KV{Key: "Current", Value: version},
		render.KV{Key: "Latest", Value: rel.TagName},
	)
	if latestOK {
		pairs[len(pairs)-1] = render.KV{Key: "Latest", Value: fmt.Sprintf("v%s", latest)}
	}
	if rel.HTMLURL != "" {
		pairs = append(pairs, render.KV{Key: "Release", Value: rel.HTMLURL})
	}

	updateAvailable := false
	if curOK && latestOK {
		updateAvailable = latest.GT(cur)
		if updateAvailable {
			pairs = append(pairs, render.KV{Key: "Update available", Value: "yes"})
		} else {
			pairs = append(pairs, render.KV{Key: "Update available", Value: "no"})
		}
	} else {
		pairs = append(pairs, render.KV{Key: "Update available", Value: "unknown (non-semver build)"})
	}

	(&render.KeyValueList{Pairs: pairs}).Print()

	if c.Check {
		if !curOK {
			printWarning("Cannot compare versions for non-semver build; install an official release to enable self-update.")
		}
		return nil
	}

	if !curOK {
		return fmt.Errorf("cannot self-update non-semver build version %q; install an official release", version)
	}
	if !latestOK {
		return fmt.Errorf("cannot self-update to non-semver release %q", rel.TagName)
	}

	if !updateAvailable {
		printSuccess("Already up to date")
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	if isHomebrewManaged(exe) {
		return fmt.Errorf("subtask is managed by Homebrew; run `brew upgrade subtask`")
	}
	if !binaryupdate.CanWriteDir(exe) {
		return fmt.Errorf("subtask is not writable at %q; reinstall to a user-writable directory", exe)
	}

	archive, checksums, err := binaryupdate.SelectReleaseAssets(rel, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}

	checksumsBytes, err := client.Download(ctx, checksums.BrowserDownloadURL)
	if err != nil {
		return err
	}
	archiveBytes, err := client.Download(ctx, archive.BrowserDownloadURL)
	if err != nil {
		return err
	}
	if err := binaryupdate.VerifySHA256(checksumsBytes, archive.Name, archiveBytes); err != nil {
		return err
	}
	newBin, err := binaryupdate.ExtractSubtaskBinary(runtime.GOOS, archive.Name, archiveBytes)
	if err != nil {
		return err
	}

	if err := binaryupdate.Apply(exe, newBin); err != nil {
		if runtime.GOOS == "windows" {
			if err2 := binaryupdate.Stage(exe, newBin); err2 == nil {
				printWarning("Downloaded update but could not replace the running executable; update will apply on next run.")
				return nil
			}
		}
		return err
	}

	refreshPluginAfterSwap(exe)

	printSuccess(fmt.Sprintf("Updated to v%s", latest))
	return nil
}
