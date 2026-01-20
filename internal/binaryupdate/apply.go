package binaryupdate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	update "github.com/inconshreveable/go-update"
)

const stagedSuffix = ".staged"

func StagedPath(exePath string) string {
	return exePath + stagedSuffix
}

func Stage(exePath string, bin []byte) error {
	if exePath == "" {
		return errors.New("empty exe path")
	}
	stagePath := StagedPath(exePath)
	tmpPath := stagePath + ".tmp"

	if err := os.WriteFile(tmpPath, bin, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, stagePath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func TryApplyStaged(exePath string) (bool, error) {
	stagePath := StagedPath(exePath)
	staged, err := os.ReadFile(stagePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := Apply(exePath, staged); err != nil {
		return false, err
	}
	_ = os.Remove(stagePath)
	return true, nil
}

func Apply(exePath string, bin []byte) error {
	if exePath == "" {
		return errors.New("empty exe path")
	}

	mode := os.FileMode(0o755)
	if st, err := os.Stat(exePath); err == nil {
		mode = st.Mode() & 0o777
	}

	return update.Apply(bytes.NewReader(bin), update.Options{
		TargetPath: exePath,
		TargetMode: mode,
	})
}

func CanWriteDir(path string) bool {
	if path == "" {
		return false
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".subtask-write-*")
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return true
}
