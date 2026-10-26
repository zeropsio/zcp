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
