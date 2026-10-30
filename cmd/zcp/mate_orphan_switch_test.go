package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/mate"
)

func orphanSwitchVersions(t *testing.T) (installed, candidate, newer string) {
	t.Helper()
	var major, minor, patch int
	if _, err := fmt.Sscanf(manifestVersion, "%d.%d.%d", &major, &minor, &patch); err != nil {
		t.Fatalf("parse orphan-switch fixture version: %v", err)
	}
	return manifestVersion, fmt.Sprintf("%d.%d.%d", major, minor, patch+2), fmt.Sprintf("%d.%d.%d", major, minor, patch+4)
}

// non-parallel: the status command reads HOME and uses the default HTTP clients.
func TestMateStatus_DeadSwitchWorker_LateReadiness(t *testing.T) {
	installedVersion, candidateVersion, newerVersion := orphanSwitchVersions(t)
	if mode := os.Getenv("ZCP_TEST_ORPHAN_SWITCH_WORKER"); mode != "" {
		runTimedOutSwitchWorker(t, mode)
		return
	}
	t.Run("A dead update worker's switch settles when last-good becomes ready, so a newer release can update.", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		containerEnv(t)
		seedInstalledBundle(t, home, installedVersion)
		pkg := filepath.Join(mate.VersionDir(candidateVersion), "node_modules", mate.PackageName)
		if err := os.MkdirAll(pkg, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"version":"`+candidateVersion+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		binary := zcpBinary(t)
		for _, mode := range []string{"switch", "recovery"} {
			cmd := exec.CommandContext(t.Context(), binary, "-test.run=^TestMateStatus_DeadSwitchWorker_LateReadiness$")
			cmd.Env = append(os.Environ(), "ZCP_TEST_ORPHAN_SWITCH_WORKER="+mode)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s worker: %v\n%s", mode, err, output)
			}
		}
		before, err := mate.ReadUpdateState()
		if err != nil {
			t.Fatal(err)
		}
		if before.Phase != "switching" || before.FailedVersion != candidateVersion || before.WorkerPID == 0 {
			t.Fatalf("recovery timeout lost unfinished switch: %+v", before)
		}
		manifestServerForVersion(t, candidateVersion)
		transport := &lateSwitchTransport{fallback: http.DefaultClient.Transport, installedVersion: installedVersion}
		http.DefaultTransport = transport
		client := http.DefaultClient
		http.DefaultClient = &http.Client{Transport: transport}
		t.Cleanup(func() { http.DefaultClient = client })
		readStatus := func() mateStatusResult {
			t.Helper()
			stdout := captureStdout(t, func() {
				if code := runMateStatusFor([]string{"--json"}, "dev"); code != 0 {
					t.Fatalf("status exit=%d", code)
				}
			})
			var status mateStatusResult
			if err := json.Unmarshal([]byte(stdout), &status); err != nil {
				t.Fatal(err)
			}
			return status
		}
		if status := readStatus(); status.Updater.Phase != "switching" {
			t.Fatalf("unready restored process settled: %+v", status.Updater)
		}
		transport.ready = true
		status := readStatus()
		if status.Error != "" || status.Installed != installedVersion || status.Updater.Phase != "postponed" || status.Updater.RunningVersion != installedVersion || status.Updater.FailedVersion != candidateVersion {
			t.Fatalf("ASSERTION: ordinary status after late readiness = %+v, updater=%+v; want postponed on %s retaining failed %s", status, status.Updater, installedVersion, candidateVersion)
		}
		state, err := mate.ReadUpdateState()
		if err != nil {
			t.Fatal(err)
		}
		if state.WorkerPID != 0 || state.WorkerStart != "" || state.Candidate != candidateVersion || state.OldBoot != "old-boot" {
			t.Fatalf("settlement lost receipt or retained ownership: %+v", state)
		}
		if transport.environmentCalls != 1 {
			t.Fatalf("C-4 proof calls=%d, want 1", transport.environmentCalls)
		}
		for _, tc := range []struct {
			version string
			allowed bool
		}{{candidateVersion, false}, {newerVersion, true}} {
			candidate := mate.Manifest{Version: tc.version, RollbackCompatible: true, CompatibleFrom: installedVersion}
			if allowed := mate.AutomaticUpdateAllowed(status.Installed, status.Updater.FailedVersion, candidate); allowed != tc.allowed {
				t.Fatalf("automatic admission for %s=%t, want %t", tc.version, allowed, tc.allowed)
			}
		}
	})
}

// These are real, separate worker processes. Only supervisor/timeout effects are
// supplied at SwitchHooks; activation, persisted ownership and recovery are real.
func runTimedOutSwitchWorker(t *testing.T, mode string) {
	t.Helper()
	installedVersion, candidateVersion, _ := orphanSwitchVersions(t)
	var restarts []string
	hooks := mate.SwitchHooks{
		Restart: func() error {
			version, err := mate.InstalledVersion()
			if err != nil {
				return err
			}
			restarts = append(restarts, version)
			return nil
		},
		Ready:  func(string, string) error { return context.DeadlineExceeded },
		Commit: func() error { t.Error("unready process committed"); return nil },
		Cancel: func() error { t.Error("switch unexpectedly cancelled"); return nil },
	}
	var err error
	var wantRestarts []string
	if mode == "switch" {
		err = mate.SwitchUpdate(candidateVersion, "old-boot", hooks)
		wantRestarts = []string{candidateVersion, installedVersion}
	} else {
		err = mate.RecoverStoppedUpdate(hooks)
		wantRestarts = []string{installedVersion}
	}
	if !errors.Is(err, context.DeadlineExceeded) || !reflect.DeepEqual(restarts, wantRestarts) {
		t.Fatalf("%s: restarts=%v, error=%v; want %v and readiness timeout", mode, restarts, err, wantRestarts)
	}
	state, err := mate.ReadUpdateState()
	if err != nil {
		t.Fatal(err)
	}
	if state.WorkerPID != os.Getpid() {
		t.Fatalf("worker did not own switch: %+v", state)
	}
	// Production is Linux; preserve a recorded identity in this cross-platform
	// fixture when the host running the test has no /proc process start ticks.
	if state.WorkerStart == "" {
		state.WorkerStart = "exited-worker-without-procfs"
		if err := mate.WriteUpdateState(state); err != nil {
			t.Fatal(err)
		}
	}
}

type lateSwitchTransport struct {
	fallback         http.RoundTripper
	installedVersion string
	ready            bool
	environmentCalls int
}

func (p *lateSwitchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != "127.0.0.1:3773" {
		return p.fallback.RoundTrip(req)
	}
	body := `{"protocol":1,"version":"` + p.installedVersion + `","bootId":"restored-boot","ready":false}`
	if p.ready {
		body = `{"protocol":1,"version":"` + p.installedVersion + `","bootId":"restored-boot","ready":true}`
	}
	if req.URL.Path == "/.well-known/t3/environment" {
		p.environmentCalls++
		body = `{"basePath":"/mate"}`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}
