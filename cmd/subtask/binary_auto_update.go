package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zippoxer/subtask/internal/binaryupdate"
	"github.com/zippoxer/subtask/pkg/task"
)

const (
	autoUpdateEnvVar     = "SUBTASK_NO_AUTO_UPDATE"
	autoUpdateLockName   = "binary-auto-update.lock"
	autoUpdateStateName  = "binary-auto-update.json"
	autoUpdateLockMaxAge = 10 * time.Minute
	autoUpdateInterval   = 24 * time.Hour
)

type autoUpdateState struct {
	LastChecked string `json:"last_checked"` // RFC3339
}

func startBinaryAutoUpdate() {
	if os.Getenv(autoUpdateEnvVar) == "1" {
		return
	}

	cur, curOK := parseSemverVersion(version)
	if !curOK {
		return
	}

	exe, err := os.Executable()
	if err != nil || exe == "" {
		return
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	if isHomebrewManaged(exe) {
		return
	}
	if !binaryupdate.CanWriteDir(exe) {
		return
	}

	// Best-effort: apply any staged update from a previous attempt.
	_, _ = binaryupdate.TryApplyStaged(exe)

	// Don't run the background checker during an explicit update command.
	if len(os.Args) > 1 && os.Args[1] == "update" {
		return
	}

	go func() {
		time.Sleep(500 * time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if !acquireAutoUpdateLock() {
			return
		}
		defer releaseAutoUpdateLock()

		st, ok := loadAutoUpdateState()
		if ok && time.Since(st) < autoUpdateInterval {
			return
		}

		client := newBinaryUpdateClient()
		rel, err := client.LatestRelease(ctx)
		if err != nil {
			return
		}
		_ = saveAutoUpdateState(time.Now())

		latest, latestOK := parseSemverVersion(rel.TagName)
		if !latestOK || !latest.GT(cur) {
			return
		}

		archive, checksums, err := binaryupdate.SelectReleaseAssets(rel, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return
		}
		checksumsBytes, err := client.Download(ctx, checksums.BrowserDownloadURL)
		if err != nil {
			return
		}
		archiveBytes, err := client.Download(ctx, archive.BrowserDownloadURL)
		if err != nil {
			return
		}
		if err := binaryupdate.VerifySHA256(checksumsBytes, archive.Name, archiveBytes); err != nil {
			return
		}
		newBin, err := binaryupdate.ExtractSubtaskBinary(runtime.GOOS, archive.Name, archiveBytes)
		if err != nil {
			return
		}

		if err := binaryupdate.Apply(exe, newBin); err != nil {
			// Simple Windows fallback: stage and retry on next run.
			if runtime.GOOS == "windows" {
				_ = binaryupdate.Stage(exe, newBin)
			}
			return
		}
	}()
}

func autoUpdateStatePath() string {
	dir := task.GlobalDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, autoUpdateStateName)
}

func autoUpdateLockPath() string {
	dir := task.GlobalDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, autoUpdateLockName)
}

func loadAutoUpdateState() (time.Time, bool) {
	path := autoUpdateStatePath()
	if path == "" {
		return time.Time{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, false
	}
	var st autoUpdateState
	if err := json.Unmarshal(b, &st); err != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, st.LastChecked)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func saveAutoUpdateState(t time.Time) error {
	dir := task.GlobalDir()
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := autoUpdateStatePath()
	if path == "" {
		return nil
	}
	tmp := path + ".tmp"
	b, err := json.Marshal(autoUpdateState{LastChecked: t.UTC().Format(time.RFC3339)})
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func acquireAutoUpdateLock() bool {
	lock := autoUpdateLockPath()
	if lock == "" {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		return false
	}

	for i := 0; i < 2; i++ {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(time.Now().UTC().Format(time.RFC3339) + "\n")
			_ = f.Close()
			return true
		}

		info, statErr := os.Stat(lock)
		if statErr != nil {
			continue
		}
		if time.Since(info.ModTime()) > autoUpdateLockMaxAge {
			_ = os.Remove(lock)
			continue
		}
		break
	}
	return false
}

func releaseAutoUpdateLock() {
	lock := autoUpdateLockPath()
	if lock == "" {
		return
	}
	_ = os.Remove(lock)
}

func isHomebrewManaged(exePath string) bool {
	p := filepath.ToSlash(exePath)
	return strings.Contains(p, "/Cellar/")
}
