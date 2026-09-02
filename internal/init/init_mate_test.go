// Tests for: the mate (Zerops Mate) step of `zcp init` and the init-complete
// marker nginx serves at {mate.BasePath}/healthz.
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
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/runtime"
)

// mateRig is one container-mode init with every outside effect captured: the
// commands run, whether the bundle installer fired, and where the unit file
// would be.
type mateRig struct {
	baseDir  string
	home     string
	unitPath string
	commands [][]string
	installs int
}

func newMateRig(t *testing.T) *mateRig {
	t.Helper()
	rig := &mateRig{
		baseDir: t.TempDir(),
		home:    t.TempDir(),
	}
	rig.unitPath = filepath.Join(t.TempDir(), "zerops@mate.service")

	t.Setenv("HOME", rig.home)
	zcpinit.SetVSCodeWorkDir(t.TempDir())
	zcpinit.SetCommandRunner(func(name string, args ...string) error {
		rig.commands = append(rig.commands, append([]string{name}, args...))
		return nil
	})
	zcpinit.SetMateUnitFilePath(rig.unitPath)
	zcpinit.SetMateEnsureInstalled(func(mate.EnsureOptions) (mate.Result, error) {
		rig.installs++
		return mate.Result{}, errors.New("installer not stubbed for this test")
	})
	t.Cleanup(func() {
		zcpinit.ResetVSCodeWorkDir()
		zcpinit.ResetCommandRunner()
		zcpinit.ResetMateUnitFilePath()
		zcpinit.ResetMateEnsureInstalled()
	})
	return rig
}

// writeBundleFiles lays down what installing the verified release tarball
// leaves behind, at mate.PinnedVersion: an executable entry point (reached
// through mate.CurrentLink()) whose `serve --help` advertises --base-path.
// Pure filesystem — it does not touch the mateEnsureInstalled stub, so a test
// that installs the bundle as a SIDE EFFECT of its own stub (see
// TestRun_Mate_InstallsPinnedBundle_WhenAbsent) can call this without
// recursively reconfiguring the seam it is itself running inside.
func (r *mateRig) writeBundleFiles(t *testing.T) {
	t.Helper()
	versionDir := filepath.Join(r.home, ".zcp", "mate", "versions", mate.PinnedVersion)
	bin := filepath.Join(versionDir, "node_modules", ".bin", mate.BinName)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho '  --base-path  Public path prefix'\n"), 0o700); err != nil {
		t.Fatalf("write fake mate: %v", err)
	}
	current := filepath.Join(r.home, ".zcp", "mate", "current")
	_ = os.Remove(current)
	if err := os.Symlink(filepath.Join("versions", mate.PinnedVersion), current); err != nil {
		t.Fatalf("symlink current -> versions/%s: %v", mate.PinnedVersion, err)
	}
}

// installBundle is writeBundleFiles plus the stub most tests actually want:
// mateEnsureInstalled reporting that mate.PinnedVersion is already live (a
// successful no-op ensure), so enableMate proceeds straight to the
// --base-path check, the env file and the unit. A test asserting something
// about the ensure-install call itself overrides the stub again afterward.
func (r *mateRig) installBundle(t *testing.T) {
	t.Helper()
	r.writeBundleFiles(t)
	zcpinit.SetMateEnsureInstalled(func(mate.EnsureOptions) (mate.Result, error) {
		r.installs++
		return mate.Result{Action: mate.ActionNone, From: mate.PinnedVersion, To: mate.PinnedVersion}, nil
	})
}

func (r *mateRig) unitCreateCalls() [][]string {
	var out [][]string
	for _, cmd := range r.commands {
		if len(cmd) >= 5 && cmd[len(cmd)-4] == "unit" && cmd[len(cmd)-3] == "create" {
			out = append(out, cmd)
		}
	}
	return out
}

func containerInfo() runtime.Info {
	return runtime.Info{InContainer: true, ProjectID: "nTV3oMB2SS634ImDJnQckg", ServiceID: "gt7tJZjDSk2zyH5XvNeAQQ", MateEnabled: true}
}

// TestRun_Mate_EnsureInstalledReportsNone_StillRegistersUnit covers the warm
// restart: mateEnsureInstalled reports nothing changed (the invariant that a
// warm restart never reaches the network belongs to mate.EnsureInstalled's own
// tests), and enableMate still registers the unit from the bundle left on disk.
func TestRun_Mate_EnsureInstalledReportsNone_StillRegistersUnit(t *testing.T) {
	rig := newMateRig(t)
	rig.installBundle(t)

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if rig.installs != 1 {
		t.Errorf("expected the ensure-installed seam to run exactly once, got %d", rig.installs)
	}
	if len(rig.unitCreateCalls()) != 1 {
		t.Fatalf("expected one unit create, got %d: %v", len(rig.unitCreateCalls()), rig.commands)
	}
}

// TestRun_Mate_InstallsPinnedBundle_WhenAbsent covers the fresh container: no
// bundle on disk, so exactly one call to mateEnsureInstalled installs the
// pinned version before the unit is registered.
func TestRun_Mate_InstallsPinnedBundle_WhenAbsent(t *testing.T) {
	rig := newMateRig(t)
	zcpinit.SetMateEnsureInstalled(func(mate.EnsureOptions) (mate.Result, error) {
		rig.installs++
		rig.writeBundleFiles(t)
		return mate.Result{Action: mate.ActionInstalled, To: mate.PinnedVersion}, nil
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

// TestRun_Mate_UnitCreateArgs locks the supervision primitive: the same
// `zsc unit create` the sshfs mounts use, with a bare ExecStart verb. A unit's
// command is frozen at creation and survives a container restart, so nothing
// that can change between releases may be baked into it.
func TestRun_Mate_UnitCreateArgs(t *testing.T) {
	rig := newMateRig(t)
	rig.installBundle(t)

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	calls := rig.unitCreateCalls()
	if len(calls) != 1 {
		t.Fatalf("expected one unit create, got %v", rig.commands)
	}
	want := []string{"sudo", "-E", "zsc", "unit", "create", mate.UnitName, mate.UnitCommand}
	got := calls[0]
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("unit create:\n got %q\nwant %q", got, want)
	}
}

// TestRun_Mate_SkipsUnitCreate_WhenAlreadyRegistered: `zsc unit` has only create
// and remove — no idempotent upsert — and a registered unit survives a restart.
// Every boot re-runs `zcp init`, so the unit file's presence is the check.
func TestRun_Mate_SkipsUnitCreate_WhenAlreadyRegistered(t *testing.T) {
	rig := newMateRig(t)
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

// TestRun_Mate_InstallFailures_Degrade is the boot-path contract: every failure
// before a verified bundle exists names what broke, skips unit registration,
// and lets the run.init command finish successfully.
func TestRun_Mate_InstallFailures_Degrade(t *testing.T) {
	tests := []struct {
		name       string
		installErr string
		wantDetail string
	}{
		{"release download 404s", "download " + mate.ReleaseURL + ": HTTP 404 Not Found", "HTTP 404 Not Found"},
		{"download checksum mismatches", "SHA-256 mismatch for " + mate.ReleaseAssetName + ": expected aaa, got bbb", "expected aaa, got bbb"},
		{"npm dependency install fails", "npm install " + mate.ReleaseAssetName + ": exit status 1", "exit status 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rig := newMateRig(t)
			zcpinit.SetMateEnsureInstalled(func(mate.EnsureOptions) (mate.Result, error) {
				rig.installs++
				return mate.Result{}, errors.New(tt.installErr)
			})

			stderr := captureStderr(t, func() {
				if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
					t.Fatalf("a failed mate install must not fail the container start: %v", err)
				}
			})

			if rig.installs != 1 {
				t.Errorf("expected one install attempt, got %d", rig.installs)
			}
			if calls := rig.unitCreateCalls(); len(calls) != 0 {
				t.Errorf("no verified bundle means no unit, got %v", calls)
			}
			for _, want := range []string{
				"Zerops Mate",
				tt.wantDetail,
				"ZCP_MATE_ENABLED",
				"Init complete",
			} {
				if !strings.Contains(stderr, want) {
					t.Errorf("degraded init output must contain %q, got:\n%s", want, stderr)
				}
			}
		})
	}
}

// TestRun_Mate_NoProjectID_Degrades: T3CODE_ZEROPS_PROJECT_ID set and non-empty
// is the whole "this is a Zerops environment" signal for the server. Without a
// projectId there is nothing to bind to, so the step degrades rather than
// registering a unit that would run as a plain upstream server.
func TestRun_Mate_NoProjectID_Degrades(t *testing.T) {
	rig := newMateRig(t)
	rig.installBundle(t)

	if err := zcpinit.Run(rig.baseDir, runtime.Info{InContainer: true, MateEnabled: true}); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if calls := rig.unitCreateCalls(); len(calls) != 0 {
		t.Errorf("no projectId means no unit, got %v", calls)
	}
	if _, err := os.Stat(mate.EnvFilePath()); !os.IsNotExist(err) {
		t.Errorf("no projectId means no environment file, stat err=%v", err)
	}
}

// TestRun_Mate_WritesEnvContract locks the delivery of the identity contract:
// `zcp init` runs while the full container environment is present, the unit's
// own environment is not guaranteed to carry it, so the values are written to
// a file the supervisor merges at launch. Non-secret identifiers only.
func TestRun_Mate_WritesEnvContract(t *testing.T) {
	rig := newMateRig(t)
	rig.installBundle(t)
	t.Setenv("ZCP_API_HOST", "https://api.app-znojmo1.zerops.io/")

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}

	data, err := os.ReadFile(mate.EnvFilePath())
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

func TestRun_Mate_WritesAllowedOrigins_WhenConfigured(t *testing.T) {
	rig := newMateRig(t)
	rig.installBundle(t)
	t.Setenv(mate.SourceAllowedOrigins, "https://code.example.com")

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	data, err := os.ReadFile(mate.EnvFilePath())
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if !strings.Contains(string(data), "T3CODE_ZEROPS_ALLOWED_ORIGINS=https://code.example.com") {
		t.Errorf("configured origins must reach the unit, got:\n%s", data)
	}
}

// TestRun_WritesInitCompleteMarker locks the readiness body: the marker's
// CONTENT is what nginx serves, so a client can tell "still initializing" from
// "broken" before it holds any credential, and can watch initAt move to see
// that a restart re-initialized the container.
func TestRun_WritesInitCompleteMarker(t *testing.T) {
	rig := newMateRig(t)
	rig.installBundle(t)

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}

	data, err := os.ReadFile(filepath.Join(rig.baseDir, filepath.FromSlash(mate.InitMarkerRelPath)))
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

// TestRun_Mate_DegradedStepStillMarksInitComplete: the marker records that the
// step LIST finished, not that every step succeeded — the route answering is
// how a client tells "still initializing" from "broken", and a degraded mate is
// neither.
func TestRun_Mate_DegradedStepStillMarksInitComplete(t *testing.T) {
	rig := newMateRig(t)

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if _, err := os.Stat(filepath.Join(rig.baseDir, filepath.FromSlash(mate.InitMarkerRelPath))); err != nil {
		t.Errorf("marker must exist even when a step degraded: %v", err)
	}
}

// TestRun_NoMate_OutsideContainer: a local `zcp init` has no nginx, no systemd
// and no Zerops project — it must not install a bundle, write a unit env file
// or leave a readiness marker in the user's repository.
func TestRun_NoMate_OutsideContainer(t *testing.T) {
	rig := newMateRig(t)

	if err := zcpinit.Run(rig.baseDir, runtime.Info{}); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if rig.installs != 0 {
		t.Errorf("local init must not install the mate bundle, ran %d times", rig.installs)
	}
	if _, err := os.Stat(mate.EnvFilePath()); !os.IsNotExist(err) {
		t.Errorf("local init must not write the unit env file, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(rig.baseDir, filepath.FromSlash(mate.InitMarkerRelPath))); !os.IsNotExist(err) {
		t.Errorf("local init must not write the readiness marker, stat err=%v", err)
	}
}

// TestRun_MateDisabled_NoUnitFile_NoOp is the common disabled case: a container
// that never had mate enabled has nothing to reconcile, so the step is not even
// registered — no install, no shell-out, no env file, no marker, and no step
// line in the output at all.
func TestRun_MateDisabled_NoUnitFile_NoOp(t *testing.T) {
	rig := newMateRig(t)

	info := containerInfo()
	info.MateEnabled = false

	var runErr error
	stderr := captureStderr(t, func() { runErr = zcpinit.Run(rig.baseDir, info) })
	if runErr != nil {
		t.Fatalf("Run(): %v", runErr)
	}

	if rig.installs != 0 {
		t.Errorf("flag off must never install, ran %d times", rig.installs)
	}
	if len(rig.commands) != 0 {
		t.Errorf("flag off with no unit file must run no commands, got %v", rig.commands)
	}
	if _, err := os.Stat(mate.EnvFilePath()); !os.IsNotExist(err) {
		t.Errorf("flag off must not write the env file, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(rig.baseDir, filepath.FromSlash(mate.InitMarkerRelPath))); !os.IsNotExist(err) {
		t.Errorf("flag off must not write the init-complete marker, stat err=%v", err)
	}
	if strings.Contains(stderr, "Zerops Mate") {
		t.Errorf("flag off with no unit file must print no mate step line at all, got:\n%s", stderr)
	}
}

// TestRun_MateDisabled_UnitFilePresent_StopsAndRemoves covers the reversal: a
// unit a prior enabled `zcp init` registered is stopped then removed, the
// identity contract file is dropped, and — the whole point of leaving
// mate.Prefix() alone — the installed bundle (its version directory and the
// mate.CurrentLink() symlink) survives so re-enabling later costs no network.
func TestRun_MateDisabled_UnitFilePresent_StopsAndRemoves(t *testing.T) {
	rig := newMateRig(t)
	rig.installBundle(t)
	if err := os.WriteFile(rig.unitPath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatalf("seed unit file: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(mate.EnvFilePath()), 0o755); err != nil {
		t.Fatalf("mkdir env file dir: %v", err)
	}
	if err := os.WriteFile(mate.EnvFilePath(), []byte("T3CODE_ZEROPS_PROJECT_ID=x\n"), 0o600); err != nil {
		t.Fatalf("seed env file: %v", err)
	}
	bundleBin := filepath.Join(rig.home, ".zcp", "mate", "versions", mate.PinnedVersion, "node_modules", ".bin", mate.BinName)
	currentLink := filepath.Join(rig.home, ".zcp", "mate", "current")

	info := containerInfo()
	info.MateEnabled = false
	if err := zcpinit.Run(rig.baseDir, info); err != nil {
		t.Fatalf("Run(): %v", err)
	}

	stopIdx, removeIdx := -1, -1
	for i, cmd := range rig.commands {
		joined := strings.Join(cmd, " ")
		if strings.Contains(joined, "systemctl stop") {
			stopIdx = i
		}
		if strings.Contains(joined, "zsc unit remove") {
			removeIdx = i
		}
	}
	if stopIdx < 0 {
		t.Errorf("expected a systemctl stop command, got %v", rig.commands)
	}
	if removeIdx < 0 {
		t.Errorf("expected a zsc unit remove command, got %v", rig.commands)
	}
	if stopIdx >= 0 && removeIdx >= 0 && stopIdx > removeIdx {
		t.Errorf("stop must precede remove, got %v", rig.commands)
	}

	if _, err := os.Stat(mate.EnvFilePath()); !os.IsNotExist(err) {
		t.Errorf("env file must be gone after disable, stat err=%v", err)
	}
	if _, err := os.Stat(bundleBin); err != nil {
		t.Errorf("the bundle under mate.Prefix() must survive disable: %v", err)
	}
	if _, err := os.Lstat(currentLink); err != nil {
		t.Errorf("the current-version symlink must survive disable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rig.baseDir, filepath.FromSlash(mate.InitMarkerRelPath))); !os.IsNotExist(err) {
		t.Errorf("flag off must not write the init-complete marker, stat err=%v", err)
	}
}

// TestRun_MateDisabled_UnitRemoveFails_Degrades: a real `zsc unit remove`
// failure is the one error this step returns — the step is still
// best-effort, so `zcp init` reports it and completes rather than failing
// the container start.
func TestRun_MateDisabled_UnitRemoveFails_Degrades(t *testing.T) {
	rig := newMateRig(t)
	if err := os.WriteFile(rig.unitPath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatalf("seed unit file: %v", err)
	}
	zcpinit.SetCommandRunner(func(name string, args ...string) error {
		full := append([]string{name}, args...)
		rig.commands = append(rig.commands, full)
		if strings.Contains(strings.Join(full, " "), "zsc unit remove") {
			return errors.New("unit is referenced and cannot be removed")
		}
		return nil
	})

	info := containerInfo()
	info.MateEnabled = false

	stderr := captureStderr(t, func() {
		if err := zcpinit.Run(rig.baseDir, info); err != nil {
			t.Fatalf("a failed unit remove must not fail the container start: %v", err)
		}
	})
	for _, want := range []string{"ZCP_MATE_ENABLED", "unit is referenced and cannot be removed", "Init complete"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("degraded output must contain %q, got:\n%s", want, stderr)
		}
	}
	// The unit file itself is untouched by this fake runner (it only stubs
	// the shell-out), so it remains — matching a real removal failure.
	if _, err := os.Stat(rig.unitPath); err != nil {
		t.Errorf("a failed removal must leave the unit file as found: %v", err)
	}
}

// restartCalls returns the systemctl restarts the rig's command runner saw.
func (r *mateRig) restartCalls() [][]string {
	var out [][]string
	for _, cmd := range r.commands {
		joined := strings.Join(cmd, " ")
		if strings.Contains(joined, "systemctl restart") {
			out = append(out, cmd)
		}
	}
	return out
}

// TestRun_Mate_UpdatedBundle_RestartsExistingUnit is the live finding from
// z3-eval: the unit starts at boot from WantedBy=multi-user.target, on its own,
// BEFORE `zcp init` runs (measured: unit active at 16:45:12, the zcp binary
// replaced at 16:45:14, init later still). So it serves whatever was on disk at
// boot, and a bundle this step just replaced reaches it only if init restarts
// the unit — otherwise a moved pin serves from the NEXT restart.
func TestRun_Mate_UpdatedBundle_RestartsExistingUnit(t *testing.T) {
	rig := newMateRig(t)
	rig.installBundle(t)
	if err := os.WriteFile(rig.unitPath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatalf("seed unit file: %v", err)
	}
	zcpinit.SetMateEnsureInstalled(func(mate.EnsureOptions) (mate.Result, error) {
		return mate.Result{Action: mate.ActionUpdated, From: "0.0.9", To: mate.PinnedVersion}, nil
	})

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if got := rig.restartCalls(); len(got) != 1 {
		t.Fatalf("an updated bundle must restart the already-running unit, got %v", rig.commands)
	}
}

// TestRun_Mate_UnchangedBundle_DoesNotRestart keeps the common warm restart
// cheap: nothing changed, so the running server is already the right one and
// bouncing it would drop live sessions for no reason.
func TestRun_Mate_UnchangedBundle_DoesNotRestart(t *testing.T) {
	rig := newMateRig(t)
	rig.installBundle(t)
	if err := os.WriteFile(rig.unitPath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatalf("seed unit file: %v", err)
	}
	// Seed the env file exactly as this run will write it, so the env is
	// unchanged too and only the bundle verdict is under test.
	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	rig.commands = nil

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if got := rig.restartCalls(); len(got) != 0 {
		t.Errorf("an unchanged bundle and env must not bounce the unit, got %v", got)
	}
}

// TestRun_Mate_ChangedEnvContract_RestartsExistingUnit: the supervisor merges
// ~/.zcp/mate.env at launch, so an operator's new ZCP_MATE_ALLOWED_ORIGINS reaches
// the running server no other way. Found by hand on z3-eval, where setting that
// env and re-running init left the old allowlist live until a manual restart.
func TestRun_Mate_ChangedEnvContract_RestartsExistingUnit(t *testing.T) {
	rig := newMateRig(t)
	rig.installBundle(t)
	if err := os.WriteFile(rig.unitPath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatalf("seed unit file: %v", err)
	}
	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	rig.commands = nil

	t.Setenv(mate.SourceAllowedOrigins, "https://mate.example.test")
	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if got := rig.restartCalls(); len(got) != 1 {
		t.Fatalf("a rewritten env contract must restart the unit, got %v", rig.commands)
	}
}

// TestRun_Mate_FirstBoot_DoesNotRestartFreshUnit: `zsc unit create` starts the
// unit itself, so restarting one this run just created costs a second boot for
// nothing.
func TestRun_Mate_FirstBoot_DoesNotRestartFreshUnit(t *testing.T) {
	rig := newMateRig(t)
	rig.installBundle(t)
	zcpinit.SetMateEnsureInstalled(func(mate.EnsureOptions) (mate.Result, error) {
		return mate.Result{Action: mate.ActionInstalled, To: mate.PinnedVersion}, nil
	})

	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if got := rig.restartCalls(); len(got) != 0 {
		t.Errorf("a unit created by this same run must not be restarted, got %v", got)
	}
	if len(rig.unitCreateCalls()) != 1 {
		t.Errorf("expected the unit to be created, got %v", rig.commands)
	}
}
