package farm

import (
	"bytes"
	"debug/buildinfo"
	"reflect"
	"runtime/debug"
	"testing"
)

// TestCandidateInfoFromBuildInfo_MapsVCSSettings pins docs/spec-eval-farm.md
// §3.3: vcs.revision -> Revision, vcs.modified == "true" -> Modified,
// vcs.time -> Time, and the top-level Go version -> GoVersion. A binary
// with no vcs.revision setting (never VCS-stamped) or a nil *BuildInfo
// (debug/buildinfo.ReadFile found nothing to read) maps to nil — "without
// VCS info it uploads nothing" (§3.3).
func TestCandidateInfoFromBuildInfo_MapsVCSSettings(t *testing.T) {
	tests := []struct {
		name string
		bi   *buildinfo.BuildInfo
		want *CandidateInfo
	}{
		{
			name: "nil build info",
			bi:   nil,
			want: nil,
		},
		{
			name: "no VCS settings at all",
			bi:   &buildinfo.BuildInfo{GoVersion: "go1.25.0"},
			want: nil,
		},
		{
			name: "settings present but no vcs.revision",
			bi: &buildinfo.BuildInfo{
				GoVersion: "go1.25.0",
				Settings: []debug.BuildSetting{
					{Key: "-buildmode", Value: "exe"},
					{Key: "CGO_ENABLED", Value: "0"},
				},
			},
			want: nil,
		},
		{
			name: "clean revision",
			bi: &buildinfo.BuildInfo{
				GoVersion: "go1.25.0",
				Settings: []debug.BuildSetting{
					{Key: "vcs", Value: "git"},
					{Key: "vcs.revision", Value: "10365ad9eafaeb935d225b76b6ad90363181f262"},
					{Key: "vcs.time", Value: "2026-02-01T19:55:50Z"},
					{Key: "vcs.modified", Value: "false"},
				},
			},
			want: &CandidateInfo{
				Revision:  "10365ad9eafaeb935d225b76b6ad90363181f262",
				Modified:  false,
				Time:      "2026-02-01T19:55:50Z",
				GoVersion: "go1.25.0",
			},
		},
		{
			name: "modified working tree",
			bi: &buildinfo.BuildInfo{
				GoVersion: "go1.25.0",
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "abc123"},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			want: &CandidateInfo{
				Revision:  "abc123",
				Modified:  true,
				GoVersion: "go1.25.0",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CandidateInfoFromBuildInfo(tt.bi)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CandidateInfoFromBuildInfo(%+v) = %+v, want %+v", tt.bi, got, tt.want)
			}
		})
	}
}

// TestCandidateInfoKey_Layout pins §3.3's literal key: sibling to (never
// nested under) the candidate's own "candidates/<sha256>/zcp" object.
func TestCandidateInfoKey_Layout(t *testing.T) {
	got := CandidateInfoKey("abc123")
	want := "candidates/abc123.info.json"
	if got != want {
		t.Errorf("CandidateInfoKey(%q) = %q, want %q", "abc123", got, want)
	}
}

// TestCorpusMarkerCount_EmbeddedAndEmpty_Table pins docs/spec-eval-farm.md
// FM-66: a candidate binary carries the recipe corpus embedded from disk at
// build time, and each embedded recipe's frontmatter contributes one
// `guiSlug: "` marker to the binary's bytes. MinCorpusMarkers is the
// refusal threshold (§3.3).
func TestCorpusMarkerCount_EmbeddedAndEmpty_Table(t *testing.T) {
	if MinCorpusMarkers != 20 {
		t.Fatalf("MinCorpusMarkers = %d, want 20", MinCorpusMarkers)
	}

	repeat := func(n int) []byte {
		return bytes.Repeat([]byte(`guiSlug: "`), n)
	}

	tests := []struct {
		name string
		data []byte
		want int
	}{
		{name: "no markers", data: []byte("pretend candidate binary"), want: 0},
		{name: "19 markers, below threshold", data: repeat(19), want: 19},
		{name: "20 markers, at threshold", data: repeat(20), want: 20},
		{name: "47 markers, a real local build", data: repeat(47), want: 47},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CorpusMarkerCount(tt.data)
			if got != tt.want {
				t.Errorf("CorpusMarkerCount(%d bytes) = %d, want %d", len(tt.data), got, tt.want)
			}
		})
	}
}
