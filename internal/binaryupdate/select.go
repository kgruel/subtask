package binaryupdate

import (
	"errors"
	"fmt"
	"strings"
)

func SelectReleaseAssets(rel Release, goos, goarch string) (archive Asset, checksums Asset, err error) {
	if rel.TagName == "" {
		return Asset{}, Asset{}, errors.New("invalid release")
	}
	if goos == "" || goarch == "" {
		return Asset{}, Asset{}, errors.New("invalid platform")
	}

	for _, a := range rel.Assets {
		if a.Name == "checksums.txt" {
			checksums = a
			break
		}
	}
	if checksums.BrowserDownloadURL == "" {
		return Asset{}, Asset{}, errors.New("checksums.txt not found in release assets")
	}

	var suffixes []string
	if goos == "windows" {
		suffixes = []string{
			fmt.Sprintf("_%s_%s.zip", goos, goarch),
			fmt.Sprintf("_%s_%s.exe.zip", goos, goarch),
		}
	} else {
		suffixes = []string{
			fmt.Sprintf("_%s_%s.tar.gz", goos, goarch),
			fmt.Sprintf("_%s_%s.tgz", goos, goarch),
		}
	}

	for _, suf := range suffixes {
		for _, a := range rel.Assets {
			if strings.HasSuffix(a.Name, suf) {
				return a, checksums, nil
			}
		}
	}

	// Last resort: accept any archive type if naming differs.
	fallbackSuffixes := []string{
		fmt.Sprintf("_%s_%s.zip", goos, goarch),
		fmt.Sprintf("_%s_%s.tar.gz", goos, goarch),
		fmt.Sprintf("_%s_%s.tgz", goos, goarch),
		fmt.Sprintf("_%s_%s.gz", goos, goarch),
	}
	for _, suf := range fallbackSuffixes {
		for _, a := range rel.Assets {
			if strings.HasSuffix(a.Name, suf) {
				return a, checksums, nil
			}
		}
	}

	return Asset{}, Asset{}, fmt.Errorf("no release asset found for %s/%s", goos, goarch)
}
