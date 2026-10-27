package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zeropsio/zcp/internal/mate"
)

// non-parallel: the CLI reads process environment and writes os.Stdout.
func TestMateStatus_LocalStartup_CapabilityWithoutNetwork(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	t.Setenv("ZCP_MATE_MANIFEST_URL", server.URL)
	text := captureStdout(t, func() {
		if code := runMateStatus([]string{"--local", "--json"}); code != 0 {
			t.Fatalf("exit=%d", code)
		}
	})
	var result struct {
		Updater struct {
			Protocol           int    `json:"protocol"`
			Phase              string `json:"phase"`
			RollbackCompatible bool   `json:"rollbackCompatible"`
		} `json:"updater"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("startup made %d network calls", calls.Load())
	}
	if result.Updater.Protocol != 1 || result.Updater.Phase != "idle" || result.Updater.RollbackCompatible {
		t.Fatalf("startup capability=%+v", result.Updater)
	}
}

type capabilityTransport struct {
	status   int
	body     string
	err      error
	fallback http.RoundTripper
}

func (p capabilityTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path != "/api/mate/update/readiness" {
		return p.fallback.RoundTrip(req)
	}
	if p.err != nil {
		return nil, p.err
	}
	return &http.Response{StatusCode: p.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(p.body))}, nil
}
func TestMateUpdate_ManualUnknownCapabilityCannotReplaceRunningMate(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		body        string
		err         error
		wantCode    int
		wantVersion string
		started     bool
	}{
		{"exact protocol starts independent worker", 200, `{"protocol":1}`, nil, 0, "0.14.0", true},
		{"explicit missing endpoint keeps attended legacy path", 404, "not found", nil, 0, manifestVersion, false},
		{"transport unknown postpones", 0, "", context.DeadlineExceeded, 1, "0.14.0", false},
		{"unavailable status postpones", 503, "unavailable", nil, 1, "0.14.0", false},
		{"redirect is unknown", 302, "", nil, 1, "0.14.0", false},
		{"invalid json is unknown", 200, "<html></html>", nil, 1, "0.14.0", false},
		{"unknown protocol postpones", 200, `{"protocol":2}`, nil, 1, "0.14.0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			containerEnv(t)
			seedInstalledBundle(t, home, "0.14.0")
			manifestAndTarballServer(t)
			writeFakeInstallTools(t)
			bin := t.TempDir()
			writeFakeBin(t, filepath.Join(bin, "sudo"), "#!/bin/sh\nexit 0\n")
			t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
			old := http.DefaultTransport
			http.DefaultTransport = capabilityTransport{tc.status, tc.body, tc.err, old}
			t.Cleanup(func() { http.DefaultTransport = old })
			stdout := captureStdout(t, func() {
				if code := runMateUpdate([]string{"--json"}); code != tc.wantCode {
					t.Errorf("exit=%d want%d", code, tc.wantCode)
				}
			})
			var result struct {
				Started bool `json:"started"`
			}
			if err := json.Unmarshal([]byte(stdout), &result); err != nil {
				t.Fatal(err)
			}
			version, err := mate.InstalledVersion()
			if err != nil || version != tc.wantVersion {
				t.Errorf("installed=%q error=%v want%q", version, err, tc.wantVersion)
			}
			if result.Started != tc.started {
				t.Errorf("started=%v want%v", result.Started, tc.started)
			}
		})
	}
}

// non-parallel: the command reads process environment and writes os.Stderr.
func TestMateUpdate_WorkerStarted_ReportsPendingDecision(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	containerEnv(t)
	bin := t.TempDir()
	writeFakeBin(t, filepath.Join(bin, "sudo"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	_, stderr := captureOutput(t, func() {
		if code := runMateUpdate([]string{"--automatic"}); code != 0 {
			t.Fatalf("exit = %d, want 0", code)
		}
	})
	if want := "mate: update worker started; outcome pending (zcp mate status)\n"; stderr != want {
		t.Errorf("update output = %q, want %q", stderr, want)
	}
}

// A help request or a mistyped flag is answered before any action: neither the
// independent worker nor the attended install path runs, so the running Mate is
// never drained, replaced or restarted by `zcp mate update --help`.
func TestMateUpdate_HelpOrUnknownFlag_ChangesNothing(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		status   int
		body     string
		wantCode int
		wantHelp bool
	}{
		{"--help with an updater-capable Mate", []string{"update", "--help"}, 200, `{"protocol":1}`, 0, true},
		{"-h with an updater-capable Mate", []string{"update", "-h"}, 200, `{"protocol":1}`, 0, true},
		{"unknown flag with an updater-capable Mate", []string{"update", "--bogus"}, 200, `{"protocol":1}`, 2, false},
		{"--help on the attended legacy path", []string{"update", "--help"}, 404, "not found", 0, true},
		{"unknown flag on the attended legacy path", []string{"update", "--bogus"}, 404, "not found", 2, false},
		{"unknown flag after a known one", []string{"update", "--json", "--bogus"}, 200, `{"protocol":1}`, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			containerEnv(t)
			seedInstalledBundle(t, home, "0.14.0")
			manifestAndTarballServer(t)
			writeFakeInstallTools(t)
			bin := t.TempDir()
			launched := filepath.Join(t.TempDir(), "sudo-ran")
			writeFakeBin(t, filepath.Join(bin, "sudo"), "#!/bin/sh\necho \"$@\" > '"+launched+"'\n")
			t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
			old := http.DefaultTransport
			http.DefaultTransport = capabilityTransport{tc.status, tc.body, nil, old}
			t.Cleanup(func() { http.DefaultTransport = old })
			restarted := false
			origRunner := mateRestartUnit
			mateRestartUnit = func(string) error { restarted = true; return nil }
			t.Cleanup(func() { mateRestartUnit = origRunner })

			stdout, _ := captureOutput(t, func() {
				if code := runMateCmd(tc.args); code != tc.wantCode {
					t.Errorf("exit = %d, want %d", code, tc.wantCode)
				}
			})
			if _, err := os.Stat(launched); err == nil {
				t.Error("no update worker unit may be launched")
			}
			if version, err := mate.InstalledVersion(); err != nil || version != "0.14.0" {
				t.Errorf("installed = %q, %v; want 0.14.0 untouched", version, err)
			}
			if _, err := os.Stat(filepath.Join(home, ".zcp", "mate", "versions", manifestVersion)); err == nil {
				t.Errorf("version %s was staged", manifestVersion)
			}
			if _, err := os.Stat(mate.UpdateStatePath()); err == nil {
				t.Error("an update state was recorded")
			}
			if restarted {
				t.Error("the Mate unit may not be restarted")
			}
			if gotHelp := strings.Contains(stdout, "usage: zcp mate update"); gotHelp != tc.wantHelp {
				t.Errorf("stdout = %q, usage printed = %v, want %v", stdout, gotHelp, tc.wantHelp)
			}
		})
	}
}
