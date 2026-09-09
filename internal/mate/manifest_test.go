// Tests for: internal/mate's release manifest resolver (DesiredRelease) —
// the fork's stable.json, fetched, validated and cached, replacing a version
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
		"version":     "0.9.0",
		"asset":       "zerops-mate-0.9.0.tgz",
		"url":         "https://github.com/zeropsio/mate/releases/download/v0.9.0/zerops-mate-0.9.0.tgz",
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

func TestDesiredRelease_FetchesValidManifest_AndCaches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var requests int
	client := manifestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write(validManifestJSON(t, nil))
	})

	got, err := mate.DesiredRelease(context.Background(), client, mate.ManifestOptions{})
	if err != nil {
		t.Fatalf("DesiredRelease(): %v", err)
	}
	if got.Version != "0.9.0" || got.Contract != mate.SupportedContract {
		t.Errorf("DesiredRelease() = %+v, want version 0.9.0 contract %d", got, mate.SupportedContract)
	}
	if requests != 1 {
		t.Fatalf("expected exactly one fetch, got %d", requests)
	}

	// A second call within the TTL must answer from the cache alone.
	again, err := mate.DesiredRelease(context.Background(), client, mate.ManifestOptions{})
	if err != nil {
		t.Fatalf("DesiredRelease() (cached): %v", err)
	}
	if again != got {
		t.Errorf("cached DesiredRelease() = %+v, want %+v", again, got)
	}
	if requests != 1 {
		t.Errorf("a cache hit must reach no network at all, got %d requests", requests)
	}
}

func TestDesiredRelease_Refresh_BypassesCache(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var requests int
	client := manifestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write(validManifestJSON(t, nil))
	})

	if _, err := mate.DesiredRelease(context.Background(), client, mate.ManifestOptions{}); err != nil {
		t.Fatalf("DesiredRelease(): %v", err)
	}
	if _, err := mate.DesiredRelease(context.Background(), client, mate.ManifestOptions{Refresh: true}); err != nil {
		t.Fatalf("DesiredRelease(Refresh): %v", err)
	}
	if requests != 2 {
		t.Errorf("Refresh must bypass the cache, got %d requests, want 2", requests)
	}
}

func TestDesiredRelease_UnreachableManifest_ReturnsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client := manifestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	if _, err := mate.DesiredRelease(context.Background(), client, mate.ManifestOptions{}); err == nil {
		t.Fatal("DesiredRelease(): expected an error for an unreachable manifest")
	}
}

// TestDesiredRelease_RefusesInvalidManifest is MD-10's validation table:
// every field DesiredRelease checks before trusting or caching a manifest.
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
			name:      "version below the minimum",
			overrides: map[string]any{"version": "0.0.1"},
			wantIn:    "0.0.1",
		},
		{
			name:      "sha256 not 64 hex characters",
			overrides: map[string]any{"sha256": "not-a-digest"},
			wantIn:    "sha256",
		},
		{
			name:      "url is not https",
			overrides: map[string]any{"url": "http://github.com/zeropsio/mate/releases/download/v0.9.0/zerops-mate-0.9.0.tgz"},
			wantIn:    "https",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			client := manifestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(validManifestJSON(t, tt.overrides))
			})

			_, err := mate.DesiredRelease(context.Background(), client, mate.ManifestOptions{})
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
		_, _ = w.Write(validManifestJSON(t, map[string]any{"contract": 2}))
	})

	_, err := mate.DesiredRelease(context.Background(), client, mate.ManifestOptions{})
	if err == nil {
		t.Fatal("DesiredRelease(): expected a contract-mismatch error")
	}
	var mismatch *mate.ContractMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("DesiredRelease() error = %v, want a *mate.ContractMismatchError", err)
	}
	if mismatch.Manifest != 2 || mismatch.Supported != mate.SupportedContract {
		t.Errorf("ContractMismatchError = %+v, want Manifest=2 Supported=%d", mismatch, mate.SupportedContract)
	}
}

// TestDesiredRelease_InvalidManifestNeverCached: a manifest that fails
// validation must not poison the cache — the next call, even without
// Refresh, must fetch again rather than serve the refused payload.
func TestDesiredRelease_InvalidManifestNeverCached(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var requests int
	client := manifestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write(validManifestJSON(t, map[string]any{"contract": 99}))
	})

	if _, err := mate.DesiredRelease(context.Background(), client, mate.ManifestOptions{}); err == nil {
		t.Fatal("DesiredRelease(): expected the first call to refuse")
	}
	if _, err := mate.DesiredRelease(context.Background(), client, mate.ManifestOptions{}); err == nil {
		t.Fatal("DesiredRelease(): expected the second call to refuse too")
	}
	if requests != 2 {
		t.Errorf("a refused manifest must not be cached, got %d requests, want 2", requests)
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
		{"0.8.1", "0.9.0", true},
		{"0.9.0", "0.9.0", false},
		{"0.9.0", "0.8.1", false},
		{"0.9.0", "0.9.1", true},
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
