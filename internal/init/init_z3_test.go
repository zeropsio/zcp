// Tests for: the z3 (Zerops Code) step of `zcp init` and the init-complete
// marker nginx serves at /healthz.
//
// NOT parallel — every test redirects HOME, the command runner, the installer
// and the unit-file path, all package-level.
package init_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	zcpinit "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/z3"
)

// z3Rig is one container-mode init with every outside effect captured: the
// commands run, whether the bundle installer fired, and where the unit file
// would be.
type z3Rig struct {
	baseDir  string
	home     string
	unitPath string
	commands [][]string
	installs int
}

func newZ3Rig(t *testing.T) *z3Rig {
	t.Helper()
	rig := &z3Rig{
		baseDir: t.TempDir(),
		home:    t.TempDir(),
	}
	rig.unitPath = filepath.Join(t.TempDir(), "zerops@z3.service")

	t.Setenv("HOME", rig.home)
	zcpinit.SetVSCodeWorkDir(t.TempDir())
	zcpinit.SetCommandRunner(func(name string, args ...string) error {
		rig.commands = append(rig.commands, append([]string{name}, args...))
		return nil
	})
	zcpinit.SetZ3UnitFilePath(rig.unitPath)
	zcpinit.SetZ3Install(func() error {
		rig.installs++
		return errors.New("installer not stubbed for this test")
	})
	t.Cleanup(func() {
		zcpinit.ResetVSCodeWorkDir()
		zcpinit.ResetCommandRunner()
		zcpinit.ResetZ3UnitFilePath()
		zcpinit.ResetZ3Install()
	})
	return rig
}

// installBundle lays down what installing the verified release tarball leaves
// behind: an executable entry point whose `serve --help` advertises
// --base-path.
func (r *z3Rig) installBundle(t *testing.T) {
	t.Helper()
	bin := filepath.Join(r.home, ".zcp", "z3", "node_modules", ".bin", "t3")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho '  --base-path  Public path prefix'\n"), 0o700); err != nil {
		t.Fatalf("write fake t3: %v", err)
	}
}

func (r *z3Rig) unitCreateCalls() [][]string {
	var out [][]string
	for _, cmd := range r.commands {
		if len(cmd) >= 5 && cmd[len(cmd)-4] == "unit" && cmd[len(cmd)-3] == "create" {
			out = append(out, cmd)
		}
	}
	return out
}

func containerInfo() runtime.Info {
	return runtime.Info{InContainer: true, ProjectID: "nTV3oMB2SS634ImDJnQckg", ServiceID: "gt7tJZjDSk2zyH5XvNeAQQ"}
}

// TestRun_Z3_UsesExistingBundle_NoInstall is the local-bundle-first rule: a
// bundle already at the prefix is used as-is, with no version check and no
// network. That is what makes the hand-delivered dev build and the fetched
// release one code path, and what keeps a warm restart off the network.
func TestRun_Z3_UsesExistingBundle_NoInstall(t *testing.T) {
	rig := newZ3Rig(t)
	rig.installBundle(t)

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if rig.installs != 0 {
		t.Errorf("a present bundle must not be reinstalled, installer ran %d times", rig.installs)
	}
	if len(rig.unitCreateCalls()) != 1 {
		t.Fatalf("expected one unit create, got %d: %v", len(rig.unitCreateCalls()), rig.commands)
	}
}

// TestRun_Z3_InstallsPinnedBundle_WhenAbsent covers the fresh container: no
// bundle on disk, so exactly one install of the pinned version happens before
// the unit is registered.
func TestRun_Z3_InstallsPinnedBundle_WhenAbsent(t *testing.T) {
	rig := newZ3Rig(t)
	zcpinit.SetZ3Install(func() error {
		rig.installs++
		rig.installBundle(t)
		return nil
	})

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if rig.installs != 1 {
		t.Errorf("expected exactly one install, got %d", rig.installs)
	}
	if len(rig.unitCreateCalls()) != 1 {
		t.Errorf("expected one unit create, got %v", rig.commands)
	}
}

// TestRun_Z3_UnitCreateArgs locks the supervision primitive: the same
// `zsc unit create` the sshfs mounts use, with a bare ExecStart verb. A unit's
// command is frozen at creation and survives a container restart, so nothing
// that can change between releases may be baked into it.
func TestRun_Z3_UnitCreateArgs(t *testing.T) {
	rig := newZ3Rig(t)
	rig.installBundle(t)

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	calls := rig.unitCreateCalls()
	if len(calls) != 1 {
		t.Fatalf("expected one unit create, got %v", rig.commands)
	}
	want := []string{"sudo", "-E", "zsc", "unit", "create", z3.UnitName, z3.UnitCommand}
	got := calls[0]
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("unit create:\n got %q\nwant %q", got, want)
	}
}

// TestRun_Z3_SkipsUnitCreate_WhenAlreadyRegistered: `zsc unit` has only create
// and remove — no idempotent upsert — and a registered unit survives a restart.
// Every boot re-runs `zcp init`, so the unit file's presence is the check.
func TestRun_Z3_SkipsUnitCreate_WhenAlreadyRegistered(t *testing.T) {
	rig := newZ3Rig(t)
	rig.installBundle(t)
	if err := os.WriteFile(rig.unitPath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatalf("seed unit file: %v", err)
	}

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if calls := rig.unitCreateCalls(); len(calls) != 0 {
		t.Errorf("an already-registered unit must not be re-created, got %v", calls)
	}
}

// TestRun_Z3_InstallFailures_Degrade is the boot-path contract: every failure
// before a verified bundle exists names what broke, skips unit registration,
// and lets the run.init command finish successfully.
func TestRun_Z3_InstallFailures_Degrade(t *testing.T) {
	tests := []struct {
		name       string
		installErr string
		wantDetail string
	}{
		{"release download 404s", "download " + z3.ReleaseURL + ": HTTP 404 Not Found", "HTTP 404 Not Found"},
		{"download checksum mismatches", "SHA-256 mismatch for " + z3.ReleaseAssetName + ": expected aaa, got bbb", "expected aaa, got bbb"},
		{"npm dependency install fails", "npm install " + z3.ReleaseAssetName + ": exit status 1", "exit status 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rig := newZ3Rig(t)
			zcpinit.SetZ3Install(func() error {
				rig.installs++
				return errors.New(tt.installErr)
			})

			stderr := captureStderr(t, func() {
				if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
					t.Fatalf("a failed z3 install must not fail the container start: %v", err)
				}
			})

			if rig.installs != 1 {
				t.Errorf("expected one install attempt, got %d", rig.installs)
			}
			if calls := rig.unitCreateCalls(); len(calls) != 0 {
				t.Errorf("no verified bundle means no unit, got %v", calls)
			}
			for _, want := range []string{
				"Zerops Code",
				tt.wantDetail,
				"Zerops Code is unavailable on this container",
				"Init complete",
			} {
				if !strings.Contains(stderr, want) {
					t.Errorf("degraded init output must contain %q, got:\n%s", want, stderr)
				}
			}
			if strings.Contains(stderr, "t3@") {
				t.Errorf("degraded init must not name the removed npm source, got:\n%s", stderr)
			}
		})
	}
}

// TestRun_Z3_NoProjectID_Degrades: T3CODE_ZEROPS_PROJECT_ID set and non-empty
// is the whole "this is a Zerops environment" signal for the server. Without a
// projectId there is nothing to bind to, so the step degrades rather than
// registering a unit that would run as a plain upstream server.
func TestRun_Z3_NoProjectID_Degrades(t *testing.T) {
	rig := newZ3Rig(t)
	rig.installBundle(t)

	if err := zcpinit.Run(rig.baseDir, runtime.Info{InContainer: true}); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if calls := rig.unitCreateCalls(); len(calls) != 0 {
		t.Errorf("no projectId means no unit, got %v", calls)
	}
	if _, err := os.Stat(z3.EnvFilePath()); !os.IsNotExist(err) {
		t.Errorf("no projectId means no environment file, stat err=%v", err)
	}
}

// TestRun_Z3_WritesEnvContract locks the delivery of the identity contract:
// `zcp init` runs while the full container environment is present, the unit's
// own environment is not guaranteed to carry it, so the values are written to
// a file the supervisor merges at launch. Non-secret identifiers only.
func TestRun_Z3_WritesEnvContract(t *testing.T) {
	rig := newZ3Rig(t)
	rig.installBundle(t)
	t.Setenv("ZCP_API_HOST", "https://api.app-znojmo1.zerops.io/")

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}

	data, err := os.ReadFile(z3.EnvFilePath())
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	body := string(data)
	for _, want := range []string{
		"T3CODE_ZEROPS_PROJECT_ID=nTV3oMB2SS634ImDJnQckg",
		"T3CODE_ZEROPS_API_HOST=api.app-znojmo1.zerops.io",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("env file must carry %q, got:\n%s", want, body)
		}
	}
	// The optional keys stay unwritten so the server keeps its own defaults.
	for _, unwanted := range []string{"T3CODE_ZEROPS_ALLOWED_ORIGINS", "T3CODE_ZEROPS_MEMBERSHIP_TTL_SECONDS"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("env file must not carry an unconfigured %q, got:\n%s", unwanted, body)
		}
	}
}

func TestRun_Z3_WritesAllowedOrigins_WhenConfigured(t *testing.T) {
	rig := newZ3Rig(t)
	rig.installBundle(t)
	t.Setenv(z3.SourceAllowedOrigins, "https://code.example.com")

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	data, err := os.ReadFile(z3.EnvFilePath())
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if !strings.Contains(string(data), "T3CODE_ZEROPS_ALLOWED_ORIGINS=https://code.example.com") {
		t.Errorf("configured origins must reach the unit, got:\n%s", data)
	}
}

// TestRun_WritesInitCompleteMarker locks the /healthz body: the marker's
// CONTENT is what nginx serves, so a client can tell "still initializing" from
// "broken" before it holds any credential, and can watch initAt move to see
// that a restart re-initialized the container.
func TestRun_WritesInitCompleteMarker(t *testing.T) {
	rig := newZ3Rig(t)
	rig.installBundle(t)

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}

	data, err := os.ReadFile(filepath.Join(rig.baseDir, filepath.FromSlash(z3.InitMarkerRelPath)))
	if err != nil {
		t.Fatalf("init-complete marker should exist: %v", err)
	}
	var marker struct {
		InitComplete bool   `json:"initComplete"`
		InitAt       string `json:"initAt"`
	}
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatalf("the marker is served verbatim as JSON, so it must parse: %v (body %q)", err, data)
	}
	if !marker.InitComplete {
		t.Error("a run that reached the end of the step list must report initComplete")
	}
	if _, err := time.Parse(time.RFC3339, marker.InitAt); err != nil {
		t.Errorf("initAt must be RFC3339 so a client can compare boots: %v", err)
	}
}

// TestRun_Z3_DegradedStepStillMarksInitComplete: the marker records that the
// step LIST finished, not that every step succeeded — /healthz answering is
// how a client tells "still initializing" from "broken", and a degraded z3 is
// neither.
func TestRun_Z3_DegradedStepStillMarksInitComplete(t *testing.T) {
	rig := newZ3Rig(t)

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if _, err := os.Stat(filepath.Join(rig.baseDir, filepath.FromSlash(z3.InitMarkerRelPath))); err != nil {
		t.Errorf("marker must exist even when a step degraded: %v", err)
	}
}

// TestRun_NoZ3_OutsideContainer: a local `zcp init` has no nginx, no systemd
// and no Zerops project — it must not install a bundle, write a unit env file
// or leave a /healthz marker in the user's repository.
func TestRun_NoZ3_OutsideContainer(t *testing.T) {
	rig := newZ3Rig(t)

	if err := zcpinit.Run(rig.baseDir, runtime.Info{}); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if rig.installs != 0 {
		t.Errorf("local init must not install the z3 bundle, ran %d times", rig.installs)
	}
	if _, err := os.Stat(z3.EnvFilePath()); !os.IsNotExist(err) {
		t.Errorf("local init must not write the unit env file, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(rig.baseDir, filepath.FromSlash(z3.InitMarkerRelPath))); !os.IsNotExist(err) {
		t.Errorf("local init must not write the /healthz marker, stat err=%v", err)
	}
}
