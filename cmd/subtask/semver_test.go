package main

import "testing"

func TestParseSemverVersion(t *testing.T) {
	cases := []struct {
		in     string
		wantOK bool
		want   string
	}{
		{"0.5.0", true, "0.5.0"},
		{"v0.5.0", true, "0.5.0"},
		{"subtask 0.5.0", true, "0.5.0"}, // embedded release version
		{"0.4.0-dev", false, ""},         // prerelease/dev — rejected
		{"0.4.0-5-gabc123", false, ""},   // git-describe — rejected
		{"0.4.0+build.7", false, ""},     // build metadata — rejected
		{"dev", false, ""},
		{"not a version", false, ""},
	}
	for _, c := range cases {
		v, ok := parseSemverVersion(c.in)
		if ok != c.wantOK {
			t.Errorf("parseSemverVersion(%q) ok=%v, want %v", c.in, ok, c.wantOK)
			continue
		}
		if ok && v.String() != c.want {
			t.Errorf("parseSemverVersion(%q) = %q, want %q", c.in, v.String(), c.want)
		}
	}
}
