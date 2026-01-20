package main

import (
	"regexp"

	"github.com/blang/semver"
)

var reSemver = regexp.MustCompile(`\d+\.\d+\.\d+`)

func parseSemverVersion(s string) (semver.Version, bool) {
	m := reSemver.FindStringIndex(s)
	if m == nil {
		return semver.Version{}, false
	}
	v, err := semver.Make(s[m[0]:m[1]])
	if err != nil {
		return semver.Version{}, false
	}
	return v, true
}
