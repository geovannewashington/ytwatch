package main

import "testing"

func TestVersionStringPrefersTheReleaseVersion(t *testing.T) {
	old := version
	defer func() { version = old }()

	version = "1.2.3"
	if got := versionString(); got != "1.2.3" {
		t.Errorf("versionString() = %q, want the version set at build time", got)
	}

	// Tests are built from a checkout, so there is no module version to find.
	version = ""
	if got := versionString(); got != "dev" {
		t.Errorf("versionString() = %q, want dev for a build from a checkout", got)
	}
}
