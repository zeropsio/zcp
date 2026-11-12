// Tests for: internal/update — Checker.Latest, the newest published release
// as `zcp mate status` reads it from its source, and
// Behind/IsRelease, which tell a release build that a restart would replace
// from a build nobody publishes.

package update

import (
	"net/http"
	"testing"
)

// A container whose shared egress spent GitHub's API quota still learns the
// newest release, so `zcp mate status` tells a behind zcp: the release is
// read from the releases/latest redirect install.sh follows, never the API.
// witness: internal/update/check.go | client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse } → client.CheckRedirect = nil
func TestLatest_RecordedRedirect_NamesTheNewestRelease(t *testing.T) {
	t.Parallel()

	c := &Checker{
		CurrentVersion: "v9.250.0",
		ReleasesURL:    defaultReleasesURL,
		RecordDir:      t.TempDir(),
		HTTPClient:     &http.Client{Transport: recordedReleasesPage(t, defaultReleasesURL)},
	}
	got, err := c.Latest(t.Context())
	if err != nil || got != "v9.251.1" {
		t.Fatalf("Latest() = %q, %v; want v9.251.1", got, err)
	}
	if !Behind(c.CurrentVersion, got) {
		t.Errorf("Behind(%q, %q) = false, want a behind zcp", c.CurrentVersion, got)
	}
}

func TestBehind_ReleaseAndDevBuilds_OnlyAnOlderReleaseIsBehind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		installed string
		latest    string
		want      bool
	}{
		{"older release is behind", "v9.220.0", "v9.226.0", true},
		{"equal release is not behind", "v9.226.0", "v9.226.0", false},
		{"newer release is not behind", "v9.227.0", "v9.226.0", false},
		{"v prefix is optional", "9.220.0", "v9.226.0", true},
		{"components compare as numbers", "v9.99.0", "v9.100.0", true},
		{"dev build is never behind", "dev", "v9.226.0", false},
		{"git-describe build is never behind", "v9.220.0-3-gabc1234-dirty", "v9.226.0", false},
		{"bare commit build is never behind", "abc1234", "v9.226.0", false},
		{"unknown latest is not behind", "v9.220.0", "", false},
		{"unparsable latest is not behind", "v9.220.0", "latest", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Behind(tt.installed, tt.latest); got != tt.want {
				t.Errorf("Behind(%q, %q) = %v, want %v", tt.installed, tt.latest, got, tt.want)
			}
		})
	}
}

func TestIsRelease_VersionShapes_OnlyMajorMinorPatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version string
		want    bool
	}{
		{"v9.226.0", true},
		{"9.226.0", true},
		{"dev", false},
		{"", false},
		{"v9.226", false},
		{"v9.226.0-rc.1", false},
		{"v9.220.0-3-gabc1234-dirty", false},
		{"abc1234", false},
		{"v9.x.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			t.Parallel()
			if got := IsRelease(tt.version); got != tt.want {
				t.Errorf("IsRelease(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}
