package homedir

import (
	"os"
	"path/filepath"
)

// Dir returns the user's home directory.
//
// On Windows, os.UserHomeDir() prefers USERPROFILE/HOMEDRIVE+HOMEPATH and does
// not consistently honor HOME. Some environments (and our tests) use HOME, so we
// prefer HOME when it is set.
func Dir() (string, error) {
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Clean(h), nil
	}
	return os.UserHomeDir()
}
