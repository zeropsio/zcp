// Tests for: what `zcp mate status` says about a restart (cmd/zcp/mate.go) —
// the zcp release this container runs against the one install.sh installs,
// and whether a restart is needed: zcp behind, or a Mate without updater
// protocol 1 that a restart moves onto it (docs/spec-mate.md §2.1d, §2.9).
//
// NOT parallel — every test redirects HOME, ZCP_UPDATE_URL, the
// container-detection env vars and http.DefaultTransport, all process-global.
package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zeropsio/zcp/internal/mate"
)

// zcpReleaseServer is the zcp release source (GitHub's releases/latest, as
// ZCP_UPDATE_URL overrides it) answering tag; an empty tag answers 403, as a
// rate-limited GitHub does. It counts the asks.
func zcpReleaseServer(t *testing.T, tag string) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/zeropsio/zcp/releases/latest" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		if tag == "" {
			http.Error(w, "API rate limit exceeded", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": tag})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ZCP_UPDATE_URL", srv.URL)
	return &calls
}

// readinessTransport answers the Mate's loopback updater readiness the way
// one kind of Mate does — 404 for a Mate older than updater protocol 1 — and
// counts the asks; every other request goes through.
type readinessTransport struct {
	status   int
	body     string
	err      error
	calls    *atomic.Int32
	fallback http.RoundTripper
}

func (p readinessTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path != "/api/mate/update/readiness" {
		return p.fallback.RoundTrip(req)
	}
	p.calls.Add(1)
	if p.err != nil {
		return nil, p.err
	}
	return &http.Response{StatusCode: p.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(p.body)), Request: req}, nil
}

// mateStatusJSON is the part of `zcp mate status --json` these tests read, with
// the raw keys so an absent key is told from a zero one.
type mateStatusJSON struct {
	ZCP *struct {
		Installed string `json:"installed"`
		Latest    string `json:"latest"`
		Behind    bool   `json:"behind"`
	} `json:"zcp"`
	RestartNeeded bool   `json:"restartNeeded"`
	Why           string `json:"why"`
	raw           map[string]json.RawMessage
}

func decodeStatus(t *testing.T, stdout string) mateStatusJSON {
	t.Helper()
	var got mateStatusJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &got); err != nil {
		t.Fatalf("status JSON %q: %v", stdout, err)
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &got.raw); err != nil {
		t.Fatalf("status JSON %q: %v", stdout, err)
	}
	return got
}

// checkRestart: restartNeeded is always present, why only when it is true.
func checkRestart(t *testing.T, got mateStatusJSON, wantRestart bool, wantWhy string) {
	t.Helper()
	if _, ok := got.raw["restartNeeded"]; !ok {
		t.Error(`status has no "restartNeeded" key; it is always present`)
	}
	if got.RestartNeeded != wantRestart {
		t.Errorf("restartNeeded = %v, want %v", got.RestartNeeded, wantRestart)
	}
	_, hasWhy := got.raw["why"]
	switch {
	case wantRestart && got.Why != wantWhy:
		t.Errorf("why = %q, want %q", got.Why, wantWhy)
	case !wantRestart && hasWhy:
		t.Errorf("why = %q present while no restart is needed", got.Why)
	}
}

// TestRunMateStatus_ZCPRelease_BehindFromTheCachedLatest: status reports this
// zcp against the release install.sh installs, read through the same hourly
// cache the Mate manifest has — refreshed by --refresh, the only source for
// --local, and absent when unknown. A dev build is compared with nothing.
func TestRunMateStatus_ZCPRelease_BehindFromTheCachedLatest(t *testing.T) {
	type zcpWant struct {
		installed, latest string
		behind            bool
	}
	tests := []struct {
		name        string
		installed   string   // this zcp's version
		cached      string   // the release an earlier status cached ("" = none)
		source      string   // what the release source answers now ("" = it fails)
		args        []string // after "status"
		wantZCP     *zcpWant
		wantRestart bool
		wantWhy     string
		wantCalls   int32 // asks of the release source by this status
	}{
		{"cached latest above installed", "v9.220.0", "v9.226.0", "", []string{"--json"},
			&zcpWant{"v9.220.0", "v9.226.0", true}, true, "zcp v9.226.0 is out; a restart installs it", 0},
		{"cached latest equal to installed", "v9.226.0", "v9.226.0", "", []string{"--json"},
			&zcpWant{"v9.226.0", "v9.226.0", false}, false, "", 0},
		{"latest unknown", "v9.220.0", "", "", []string{"--json"},
			nil, false, "", 1},
		{"refresh asks the release source", "v9.220.0", "v9.226.0", "v9.227.0", []string{"--json", "--refresh"},
			&zcpWant{"v9.220.0", "v9.227.0", true}, true, "zcp v9.227.0 is out; a restart installs it", 1},
		{"local reads only the cache", "v9.220.0", "v9.226.0", "v9.227.0", []string{"--json", "--local"},
			&zcpWant{"v9.220.0", "v9.226.0", true}, true, "zcp v9.226.0 is out; a restart installs it", 0},
		{"local with nothing cached is unknown", "v9.220.0", "", "v9.227.0", []string{"--json", "--local"},
			nil, false, "", 0},
		{"dev build is compared with nothing", "dev", "v9.226.0", "v9.227.0", []string{"--json"},
			nil, false, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("serviceId", "")
			manifestAndTarballServer(t)
			if tt.cached != "" {
				zcpReleaseServer(t, tt.cached)
				captureStdout(t, func() { runMateStatusFor([]string{"--json"}, "v1.0.0") })
			}
			calls := zcpReleaseServer(t, tt.source)

			stdout := captureStdout(t, func() {
				if code := runMateStatusFor(tt.args, tt.installed); code != 0 {
					t.Errorf("exit %d, want 0", code)
				}
			})
			got := decodeStatus(t, stdout)
			switch {
			case tt.wantZCP == nil && got.ZCP != nil:
				t.Errorf("zcp = %+v, want the key absent", *got.ZCP)
			case tt.wantZCP != nil && got.ZCP == nil:
				t.Errorf("zcp absent, want %+v", *tt.wantZCP)
			case tt.wantZCP != nil && (got.ZCP.Installed != tt.wantZCP.installed || got.ZCP.Latest != tt.wantZCP.latest || got.ZCP.Behind != tt.wantZCP.behind):
				t.Errorf("zcp = %+v, want %+v", *got.ZCP, *tt.wantZCP)
			}
			if tt.wantZCP != nil {
				if _, ok := got.raw["zcp"]; ok && !strings.Contains(string(got.raw["zcp"]), `"behind":`) {
					t.Errorf(`zcp = %s, want "behind" always present`, got.raw["zcp"])
				}
			}
			checkRestart(t, got, tt.wantRestart, tt.wantWhy)
			if n := calls.Load(); n != tt.wantCalls {
				t.Errorf("release source asked %d times, want %d", n, tt.wantCalls)
			}
		})
	}
}

// TestRunMateStatus_RestartNeeded_ZCPBehindOrMateWithoutUpdater: a restart is
// needed when zcp is behind (the restart installs the latest) or when the
// running Mate answers its loopback updater readiness with 404 — older than
// protocol 1 — while a newer Mate is out, which the restart installs. Only an
// exact 404 says so; an unknown answer is no reason. The Mate is asked only
// in an enabled Mate container, and never by --local.
func TestRunMateStatus_RestartNeeded_ZCPBehindOrMateWithoutUpdater(t *testing.T) {
	tests := []struct {
		name        string
		zcp         string // this zcp's version
		release     string // the zcp release install.sh installs
		status      int    // the Mate's readiness answer
		body        string
		err         error
		mateEnabled bool
		args        []string // after "status"
		wantRestart bool
		wantWhy     string
		wantProbes  int32
	}{
		{"zcp behind", "v9.220.0", "v9.226.0", http.StatusOK, `{"protocol":1}`, nil, true, []string{"--json"},
			true, "zcp v9.226.0 is out; a restart installs it", 1},
		{"mate without updater protocol 1", "v9.226.0", "v9.226.0", http.StatusNotFound, "not found", nil, true, []string{"--json"},
			true, "mate 0.14.0 has no updater; a restart installs mate " + manifestVersion, 1},
		{"zcp behind and mate without updater", "v9.220.0", "v9.226.0", http.StatusNotFound, "not found", nil, true, []string{"--json"},
			true, "zcp v9.226.0 is out and mate 0.14.0 has no updater; a restart installs both", 1},
		{"mate with updater protocol 1, zcp current", "v9.226.0", "v9.226.0", http.StatusOK, `{"protocol":1}`, nil, true, []string{"--json"},
			false, "", 1},
		{"mate updater unknown is no reason", "v9.226.0", "v9.226.0", 0, "", context.DeadlineExceeded, true, []string{"--json"},
			false, "", 1},
		{"mate answering another status is no reason", "v9.226.0", "v9.226.0", http.StatusServiceUnavailable, "starting", nil, true, []string{"--json"},
			false, "", 1},
		{"mate gate off never asks the Mate", "v9.226.0", "v9.226.0", http.StatusNotFound, "not found", nil, false, []string{"--json"},
			false, "", 0},
		{"local never asks the Mate", "v9.226.0", "v9.226.0", http.StatusNotFound, "not found", nil, true, []string{"--json", "--local"},
			false, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.mateEnabled {
				if _, err := os.Stat(mate.LiveEnvStorePath); err == nil {
					t.Skip("this host's live env store decides the Mate gate")
				}
			}
			home := t.TempDir()
			t.Setenv("HOME", home)
			if tt.mateEnabled {
				containerEnv(t)
			} else {
				t.Setenv("serviceId", "")
				t.Setenv("ZCP_MATE_ENABLED", "")
			}
			seedInstalledBundle(t, home, "0.14.0")
			manifestAndTarballServer(t)
			zcpReleaseServer(t, tt.release)
			var probes atomic.Int32
			old := http.DefaultTransport
			http.DefaultTransport = readinessTransport{tt.status, tt.body, tt.err, &probes, old}
			t.Cleanup(func() { http.DefaultTransport = old })

			// A first status warms both caches, so --local has a manifest
			// and a release to read.
			captureStdout(t, func() { runMateStatusFor([]string{"--json"}, tt.zcp) })
			probes.Store(0)

			stdout := captureStdout(t, func() {
				if code := runMateStatusFor(tt.args, tt.zcp); code != 0 {
					t.Errorf("exit %d, want 0", code)
				}
			})
			checkRestart(t, decodeStatus(t, stdout), tt.wantRestart, tt.wantWhy)
			if n := probes.Load(); n != tt.wantProbes {
				t.Errorf("Mate readiness asked %d times, want %d", n, tt.wantProbes)
			}
		})
	}
}

// TestRunMateStatus_Text_ZCPBehind_OneAddedLine: the text status says what
// the JSON says — one line naming the newer zcp and the restart that installs
// it, and nothing about zcp when it is current.
func TestRunMateStatus_Text_ZCPBehind_OneAddedLine(t *testing.T) {
	tests := []struct {
		name      string
		installed string
		release   string
		want      string // the zcp line ("" = none)
	}{
		{"behind", "v9.220.0", "v9.226.0", "zcp: installed v9.220.0, v9.226.0 available; a restart installs it"},
		{"current", "v9.226.0", "v9.226.0", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("serviceId", "")
			manifestAndTarballServer(t)
			zcpReleaseServer(t, tt.release)
			stdout := captureStdout(t, func() {
				if code := runMateStatusFor(nil, tt.installed); code != 0 {
					t.Errorf("exit %d, want 0", code)
				}
			})
			var zcpLines []string
			for line := range strings.SplitSeq(strings.TrimSpace(stdout), "\n") {
				if strings.HasPrefix(line, "zcp") || strings.Contains(line, "restart") {
					zcpLines = append(zcpLines, line)
				}
			}
			switch {
			case tt.want == "" && len(zcpLines) != 0:
				t.Errorf("text status %q names zcp or a restart: %q", stdout, zcpLines)
			case tt.want != "" && (len(zcpLines) != 1 || zcpLines[0] != tt.want):
				t.Errorf("text status %q, want exactly one line %q", stdout, tt.want)
			}
		})
	}
}
