package main

import (
	"regexp"

	"github.com/blang/semver"
)

// Capture the optional prerelease/build suffix too, so dev builds like
// "0.4.0-dev" or git-describe "0.4.0-5-gabc123" are recognized as non-release
// and rejected below (rather than silently parsing as a clean "0.4.0" and
// defeating the non-release self-update guards).
var reSemver = regexp.MustCompile(`\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?`)

func parseSemverVersion(s string) (semver.Version, bool) {
	m := reSemver.FindString(s)
	if m == "" {
		return semver.Version{}, false
	}
	v, err := semver.Make(m)
	if err != nil {
		return semver.Version{}, false
	}
	// Only clean release versions (pure X.Y.Z) drive self-update; reject
	// prerelease/dev (-suffix) and build-metadata (+suffix) builds.
	if len(v.Pre) > 0 || len(v.Build) > 0 {
		return semver.Version{}, false
	}
	return v, true
}
