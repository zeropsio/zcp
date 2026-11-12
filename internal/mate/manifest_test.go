// Tests for: internal/mate's release manifest resolver (DesiredRelease) —
// the fork's stable.json, fetched and validated on every call, replacing a version
// pinned into zcp (spec-mate.md §2.1c, invariant MD-10).
//
// NOT parallel — every path here is derived from HOME (see runtime.HomeDir,
// mate.Prefix), and ZCP_MATE_MANIFEST_URL is set with t.Setenv.
package mate_test

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/mate"
)

// validManifestJSON renders a well-formed stable.json body for version,
// overridable per field by the caller for the validation-refusal table.
func validManifestJSON(t *testing.T, overrides map[string]any) []byte {
	t.Helper()
	body := map[string]any{
		"version":     mate.MinimumMateVersion,
		"asset":       "zerops-mate-" + mate.MinimumMateVersion + ".tgz",
		"url":         "https://github.com/zeropsio/mate/releases/download/v" + mate.MinimumMateVersion + "/zerops-mate-" + mate.MinimumMateVersion + ".tgz",
		"sha256":      strings.Repeat("a", 64),
		"size":        21690443,
		"contract":    mate.SupportedContract,
		"publishedAt": "2026-09-09T07:23:00Z",
	}
	maps.Copy(body, overrides)
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal manifest fixture: %v", err)
	}
	return data
}

// manifestServer wires ZCP_MATE_MANIFEST_URL at a pipe-backed httptest
// server serving handler, and returns the client DesiredRelease must be
// called with.
func manifestServer(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	server, client := newPipeHTTPTestServer(t, handler)
	t.Setenv("ZCP_MATE_MANIFEST_URL", server.URL+"/stable.json")
	return client
}

func TestDesiredRelease_SemanticVersions_PreserveTheReleaseFloor(t *testing.T) {
	// Preserve the regression titles while arranging valid candidates around
	// the current floor. TestVersionOlder keeps the original numeric controls.
	for _, tt := range []struct {
		name    string
		version string
		allowed bool
	}{
		{"0.16.38-dev.1e84132e76", "0.17.0-dev.1e84132e76", true},
		{"0.16.1-alpha.1+build.42", "0.17.0-alpha.1+build.42", true},
		{"0.16.0+build.42", mate.MinimumMateVersion + "+build.42", true},
		{"0.16.0-rc.1", mate.MinimumMateVersion + "-rc.1", false},
		{"0.15.999-dev.1", "0.15.999-dev.1", false},
		{"0.16.38-dev..sha", "0.16.38-dev..sha", false},
		{"0.16.38-dev.01", "0.16.38-dev.01", false},
		{"0.16.38-", "0.16.38-", false},
		{"0.16.38+", "0.16.38+", false},
		{"00.16.38", "00.16.38", false},
		{"v0.16.38", "v0.16.38", false},
		{"0.16", "0.16", false},
		{"0.16.38-dev/escape", "0.16.38-dev/escape", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			client := manifestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(validManifestJSON(t, map[string]any{"version": tt.version}))
			})
			got, err := mate.DesiredRelease(context.Background(), client)
			if (err == nil) != tt.allowed {
				t.Fatalf("DesiredRelease(%q) error = %v, allowed = %v", tt.version, err, tt.allowed)
			}
			if tt.allowed && got.Version != tt.version {
				t.Fatalf("manifest version = %q, want exact %q", got.Version, tt.version)
			}
		})
	}
}

func TestDesiredRelease_UnreachableManifest_ReturnsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client := manifestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	if _, err := mate.DesiredRelease(context.Background(), client); err == nil {
		t.Fatal("DesiredRelease(): expected an error for an unreachable manifest")
	}
}

// TestDesiredRelease_RefusesInvalidManifest is MD-10's validation table:
// every field DesiredRelease checks before trusting or recording a manifest.
func TestDesiredRelease_RefusesInvalidManifest(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]any
		wantIn    string
	}{
		{
			name:      "contract zcp does not know",
			overrides: map[string]any{"contract": mate.SupportedContract + 1},
			wantIn:    "contract",
		},
		{
			// Contract 1 is the enrollment Mate: it links to HQ with a
			// credential this zcp no longer issues.
			name:      "the enrollment contract",
			overrides: map[string]any{"contract": 1},
			wantIn:    "contract",
		},
		{
			name:      "version below the minimum",
			overrides: map[string]any{"version": "0.0.1"},
			wantIn:    "0.0.1",
		},
		{
			// 0.13.0 is the first release that keeps a link to HQ
			// (HQ): one before it cannot be a Mate.
			name:      "a release before HQ",
			overrides: map[string]any{"version": "0.12.3"},
			wantIn:    "0.12.3",
		},
		{
			// 0.14.0 is the first release that judges a stand-up by its zcp
			// process, the heartbeat this zcp no longer writes.
			name:      "a release before process liveness",
			overrides: map[string]any{"version": "0.13.11"},
			wantIn:    "0.13.11",
		},
		{
			// 0.15.153 is the first release that draws zerops_origin's ask:
			// one before it shows a plain call and never says how it ended.
			name:      "a release before the origin ask",
			overrides: map[string]any{"version": "0.15.152"},
			wantIn:    "0.15.152",
		},
		{
			name:      "sha256 not 64 hex characters",
			overrides: map[string]any{"sha256": "not-a-digest"},
			wantIn:    "sha256",
		},
		{
			name:      "url is not https",
			overrides: map[string]any{"url": "http://github.com/zeropsio/mate/releases/download/v0.16.0/zerops-mate-0.16.0.tgz"},
			wantIn:    "https",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			client := manifestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(validManifestJSON(t, tt.overrides))
			})

			_, err := mate.DesiredRelease(context.Background(), client)
			if err == nil {
				t.Fatalf("DesiredRelease(): expected a validation error for %s", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantIn) {
				t.Errorf("DesiredRelease() error = %q, want it to mention %q", err, tt.wantIn)
			}
		})
	}
}

func TestDesiredRelease_ContractMismatch_NamesBothNumbers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client := manifestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(validManifestJSON(t, map[string]any{"contract": 1}))
	})

	_, err := mate.DesiredRelease(context.Background(), client)
	if err == nil {
		t.Fatal("DesiredRelease(): expected a contract-mismatch error")
	}
	var mismatch *mate.ContractMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("DesiredRelease() error = %v, want a *mate.ContractMismatchError", err)
	}
	if mismatch.Manifest != 1 || mismatch.Supported != mate.SupportedContract {
		t.Errorf("ContractMismatchError = %+v, want Manifest=1 Supported=%d", mismatch, mate.SupportedContract)
	}
}

func TestManifestURL_DefaultsToGitHubLatestRelease(t *testing.T) {
	t.Setenv("ZCP_MATE_MANIFEST_URL", "")
	want := "https://github.com/zeropsio/mate/releases/latest/download/stable.json"
	if got := mate.ManifestURL(); got != want {
		t.Errorf("ManifestURL() = %q, want %q", got, want)
	}
}

func TestManifestURL_EnvOverride(t *testing.T) {
	t.Setenv("ZCP_MATE_MANIFEST_URL", "https://example.com/private/stable.json")
	if got, want := mate.ManifestURL(), "https://example.com/private/stable.json"; got != want {
		t.Errorf("ManifestURL() = %q, want %q", got, want)
	}
}

// TestVersionOlder covers the comparison `zcp mate status` uses to answer
// updateAvailable: installed strictly older than latest.
func TestVersionOlder(t *testing.T) {
	tests := []struct {
		installed, latest string
		want              bool
	}{
		{"0.16.38-dev.1e84132e76", "0.16.0", false},
		{"0.16.1-alpha.1+build.42", "0.16.0", false},
		{"0.16.0+build.42", "0.16.0", false},
		{"0.8.1", "0.9.0", true},
		{"0.9.0", "0.9.0", false},
		{"0.9.0", "0.8.1", false},
		{"0.9.0", "0.9.1", true},
		{"0.16.38-dev.2", "0.16.38-dev.10", true},
		{"0.16.38-dev.10", "0.16.38-dev.2", false},
		{"0.16.38-alpha", "0.16.38-alpha.1", true},
		{"0.16.38-rc.1", "0.16.38", true},
		{"0.16.38", "0.16.38-rc.1", false},
		{"0.16.38+build.1", "0.16.38+build.2", false},
		{"not-a-version", "0.9.0", false},
		{"0.9.0", "not-a-version", false},
	}
	for _, tt := range tests {
		t.Run(tt.installed+"_vs_"+tt.latest, func(t *testing.T) {
			if got := mate.VersionOlder(tt.installed, tt.latest); got != tt.want {
				t.Errorf("VersionOlder(%q, %q) = %v, want %v", tt.installed, tt.latest, got, tt.want)
			}
		})
	}
}

// TestEnvelopeCarriesParts (TEST-PLAN E1 #16): zcp sends per-server state
// only to a Mate at or above the minimum version that accepts it — so only
// once MinimumMateVersion reaches the first release that accepts it, and
// never while no release does.
func TestEnvelopeCarriesParts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, minimum, accepting string
		want                     bool
	}{
		{name: "no mate release accepts it yet", minimum: "0.14.0", accepting: "", want: false},
		{name: "a release accepts it, the minimum is older", minimum: "0.14.0", accepting: "0.15.0", want: false},
		{name: "the minimum is that release", minimum: "0.15.0", accepting: "0.15.0", want: true},
		{name: "the minimum is past it", minimum: "0.16.2", accepting: "0.15.0", want: true},
		{name: "an unreadable version", minimum: "0.16", accepting: "0.15.0", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mate.CarriesParts(tt.minimum, tt.accepting); got != tt.want {
				t.Errorf("CarriesParts(%q, %q) = %v, want %v", tt.minimum, tt.accepting, got, tt.want)
			}
		})
	}
	if mate.EnvelopeCarriesParts() != mate.CarriesParts(mate.MinimumMateVersion, mate.PartsMateVersion) {
		t.Error("EnvelopeCarriesParts does not read MinimumMateVersion and PartsMateVersion")
	}
	if mate.PartsMateVersion == "" && mate.EnvelopeCarriesParts() {
		t.Error("zcp sends stages/devServers while no mate release accepts them")
	}
}
