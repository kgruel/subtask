//go:build windows

package task

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func canonicalPath(p string) string {
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	p = filepath.Clean(p)

	// Expand any 8.3 short names (e.g. RUNNER~1) to long paths when possible.
	if long, err := getLongPathName(p); err == nil && long != "" {
		p = long
	}
	return p
}

func getLongPathName(path string) (string, error) {
	in, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}

	// Try a reasonable initial buffer, then resize if needed.
	buf := make([]uint16, 260)
	n, err := windows.GetLongPathName(in, &buf[0], uint32(len(buf)))
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	if n > uint32(len(buf)) {
		buf = make([]uint16, n+1)
		n, err = windows.GetLongPathName(in, &buf[0], uint32(len(buf)))
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "", nil
		}
	}

	return windows.UTF16ToString(buf[:n]), nil
}
