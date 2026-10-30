package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/mate"
)

func TestReleasePaths_RequirePublishedMateBeforePublishing(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		path    string
		publish string
	}{
		{"../../Makefile", "git tag -a"},
		{"../../.github/workflows/release.yml", "gh release create"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			body, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			gate := strings.Index(string(body), "go run ./cmd/mate-release-check")
			publish := strings.Index(string(body), tt.publish)
			if gate < 0 || publish < 0 || gate > publish {
				t.Fatalf("release must check the published Mate before %q: gate=%d publish=%d", tt.publish, gate, publish)
			}
		})
	}
}

// non-parallel: HOME and the install manifest override are process-wide.
func TestRun_PublishedMate_DecidesWhetherZcpMayRelease(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ZCP_MATE_MANIFEST_URL", "https://example.invalid/private.json")
	for _, tt := range []struct {
		name       string
		version    string
		contract   int
		status     int
		brokenJSON bool
		fetchError bool
		wantExit   int
		wantText   string
	}{
		{"published Mate at the floor permits release", mate.MinimumMateVersion, 2, 200, false, false, 0, "Published Mate " + mate.MinimumMateVersion},
		{"published Mate without the Observe receiver refuses release", "0.16.39", 2, 200, false, false, 1, "older than the minimum"},
		{"Observe receiver prerelease below the stable floor refuses release", "0.16.40-rc.1", 2, 200, false, false, 1, "older than the minimum"},
		{"published Observe receiver permits release", "0.16.40", 2, 200, false, false, 0, "Published Mate 0.16.40"},
		{"newer published Mate permits release", "0.17.0", 2, 200, false, false, 0, "Published Mate 0.17.0"},
		{"published Mate below the floor refuses release", "0.15.999", 2, 200, false, false, 1, "older than the minimum"},
		{"published Mate with another contract refuses release", "0.16.0", 1, 200, false, false, 1, "declares contract 1"},
		{"invalid published version refuses release", "garbage", 2, 200, false, false, 1, "version"},
		{"unavailable published Mate refuses release", "0.16.0", 2, 404, false, false, 1, "HTTP 404"},
		{"invalid published JSON refuses release", "0.16.0", 2, 200, true, false, 1, "parse mate release manifest"},
		{"unreachable published Mate refuses release", "0.16.0", 2, 200, false, true, 1, "offline"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(mate.Manifest{Version: tt.version, Contract: tt.contract,
				URL: "https://example.invalid/mate.tgz", SHA256: strings.Repeat("a", 64)})
			if err != nil {
				t.Fatal(err)
			}
			if tt.brokenJSON {
				body = []byte("{")
			}
			calls := 0
			client := &http.Client{Transport: fakeFetcher(func(req *http.Request) (*http.Response, error) {
				calls++
				if got, want := req.URL.String(), "https://github.com/zeropsio/mate/releases/latest/download/stable.json"; got != want {
					t.Errorf("release must read the canonical published manifest: got %q, want %q", got, want)
				}
				if tt.fetchError {
					return nil, errors.New("offline")
				}
				return &http.Response{StatusCode: tt.status, Status: fmt.Sprint(tt.status), Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
			})}
			var output bytes.Buffer
			if got := run(t.Context(), client, &output); got != tt.wantExit {
				t.Fatalf("release exit = %d, want %d: %s", got, tt.wantExit, output.String())
			}
			if calls != 1 || !strings.Contains(output.String(), tt.wantText) {
				t.Fatalf("fetches=%d output=%q, want one fetch and %q", calls, output.String(), tt.wantText)
			}
		})
	}
}

type fakeFetcher func(*http.Request) (*http.Response, error)

func (f fakeFetcher) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// non-parallel: HOME determines the installation manifest cache.
func TestRun_UnreachablePublishedMate_CachedReleaseDoesNotAuthorizePublishing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	body, err := json.Marshal(mate.Manifest{Version: mate.MinimumMateVersion, Contract: 2,
		URL: "https://example.invalid/mate.tgz", SHA256: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: fakeFetcher(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})}
	if _, err := mate.DesiredRelease(t.Context(), client, mate.ManifestOptions{Refresh: true}); err != nil {
		t.Fatalf("populate installation cache: %v", err)
	}
	client.Transport = fakeFetcher(func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})
	var output bytes.Buffer
	if got := run(t.Context(), client, &output); got != 1 || !strings.Contains(output.String(), "offline") {
		t.Fatalf("cached install cannot authorize publishing: exit=%d output=%q", got, output.String())
	}
}
