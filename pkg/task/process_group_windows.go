//go:build windows

package task

// SelfProcessGroupID returns the current process group ID.
// Windows does not have Unix-style process groups, so this returns 0.
func SelfProcessGroupID() int {
	return 0
}

// EnsureOwnProcessGroup is a no-op on Windows.
func EnsureOwnProcessGroup() {}
