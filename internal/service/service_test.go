package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/service"
)

func TestStart_UnknownService(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		svc     string
		wantErr string
	}{
		{"unknown name", "redis", "unknown service"},
		{"empty name", "", "unknown service"},
		{"typo", "ngnix", "unknown service"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := service.Start(tt.svc)
			if err == nil {
				t.Fatal("expected error for unknown service")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error should contain %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestStart_KnownService_ArgsCorrect(t *testing.T) {
	// Not parallel — mutates runFunc + tuneFunc and PATH.
	// LookPath needs executable files, but the captured runner never executes
	// them. Keep the launch contract covered even without nginx in CI.
	binDir := t.TempDir()
	for _, name := range []string{"nginx", "code-server"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("seed %s executable: %v", name, err)
		}
	}
	t.Setenv("PATH", binDir)
	type captured struct {
		binary string
		args   []string
	}
	var got captured

	service.SetRunFunc(func(binary string, args []string, _ []string) error {
		got.binary = binary
		got.args = args
		return nil
	})
	// vscode declares a TasksMax tune; stub it so this test never shells out
	// to `sudo systemctl set-property` (docs: TestServiceStart_TasksMaxTunerStubbed_NoSudo).
	service.SetTuneFunc(func(string, int) error { return nil })
	t.Cleanup(func() { service.ResetRunFunc(); service.ResetTuneFunc() })

	tests := []struct {
		name     string
		svc      string
		wantArgs []string
	}{
		{
			"nginx",
			"nginx",
			[]string{"nginx", "-e", "stderr", "-g", "daemon off;"},
		},
		{
			"vscode",
			"vscode",
			[]string{"code-server", "--auth", "none", "--bind-addr", "0.0.0.0:8081", "--disable-workspace-trust", "/var/www"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got = captured{}
			err := service.Start(tt.svc)
			if err != nil {
				t.Fatalf("Start(%q) error: %v", tt.svc, err)
			}
			if len(got.args) != len(tt.wantArgs) {
				t.Fatalf("args length: got %d, want %d", len(got.args), len(tt.wantArgs))
			}
			for i, arg := range tt.wantArgs {
				if got.args[i] != arg {
					t.Errorf("args[%d]: got %q, want %q", i, got.args[i], arg)
				}
			}
		})
	}
}

func TestStart_VSCode_RaisesTasksMax(t *testing.T) {
	// Not parallel — mutates runFunc + tuneFunc.
	var tuned bool
	var tunedUnit string
	var tunedMax int
	service.SetRunFunc(func(string, []string, []string) error { return nil })
	service.SetTuneFunc(func(unit string, tasksMax int) error {
		tuned, tunedUnit, tunedMax = true, unit, tasksMax
		return nil
	})
	t.Cleanup(func() { service.ResetRunFunc(); service.ResetTuneFunc() })

	// vscode runs as the ExecStart of zerops@vscode.service; code-server +
	// in-container AI agents spawn many subprocesses and hit the default
	// TasksMax (300 observed live). Start must raise it on the unit before
	// launching. The tune runs before binary resolution, so this asserts even
	// when code-server isn't installed (CI / dev box). 1600 = 80% of the
	// container's 2000 shared pid budget, reserving ~400 for the rest.
	_ = service.Start("vscode")
	if !tuned {
		t.Fatal("Start(vscode) must tune TasksMax")
	}
	if tunedUnit != "zerops@vscode.service" {
		t.Errorf("tuned unit: got %q, want zerops@vscode.service", tunedUnit)
	}
	if tunedMax != 1600 {
		t.Errorf("tuned TasksMax: got %d, want 1600", tunedMax)
	}

	// nginx declares no tasksMax → no tuning.
	tuned = false
	_ = service.Start("nginx")
	if tuned {
		t.Error("Start(nginx) must NOT tune TasksMax (none declared)")
	}
}

// TestServiceStart_TasksMaxTunerStubbed_NoSudo pins that TestStart_VSCode_
// RaisesTasksMax's stub-tuneFunc pattern is the only path any test in this
// package takes to a vscode TasksMax tune: the two other tests exercising
// Start("vscode") (TestStart_KnownService_ArgsCorrect and
// TestStart_OtherServices_UnaffectedByMateGuard) now stub tuneFunc too, so
// no test in this package shells out to `sudo systemctl set-property`. This
// test proves the stub itself still receives the real call the production
// code makes — it does not merely assert absence.
func TestServiceStart_TasksMaxTunerStubbed_NoSudo(t *testing.T) {
	// Not parallel — mutates runFunc + tuneFunc.
	var tunedUnit string
	var tunedMax int
	service.SetRunFunc(func(string, []string, []string) error { return nil })
	service.SetTuneFunc(func(unit string, tasksMax int) error {
		tunedUnit, tunedMax = unit, tasksMax
		return nil
	})
	t.Cleanup(func() { service.ResetRunFunc(); service.ResetTuneFunc() })

	// The tune runs before binary resolution (see TestStart_VSCode_RaisesTasksMax),
	// so this assertion is meaningful even when code-server isn't installed
	// (CI / dev box) — only a LookPath failure ("find ...") is tolerated.
	if err := service.Start("vscode"); err != nil && !strings.Contains(err.Error(), "find") {
		t.Fatalf("Start(vscode): %v", err)
	}
	if tunedUnit != "zerops@vscode.service" || tunedMax != 1600 {
		t.Fatalf("tuneFunc called with (%q, %d), want (zerops@vscode.service, 1600)", tunedUnit, tunedMax)
	}
}

func TestList_ReturnsAllServices(t *testing.T) {
	t.Parallel()
	names := service.List()

	want := map[string]bool{"nginx": false, "vscode": false, "mate": false}
	for _, name := range names {
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("List() should include %q", name)
		}
	}
}

// installFakeMateBundle lays down a bundle that looks exactly like an
// `npm install --prefix ~/.zcp/mate/versions/<v> zerops-mate@<version>` result —
// activated via mate.CurrentLink() the way mate.EnsureInstalled leaves it — with a
// `mate` that leaves a help-ran marker in HOME if anything runs it. Returns HOME.
func installFakeMateBundle(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	const version = "0.1.0"
	binDir := filepath.Join(home, ".zcp", "mate", "versions", version, "node_modules", ".bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, mate.BinName), []byte("#!/bin/sh\n: > '"+filepath.Join(home, "help-ran")+"'\n"), 0o700); err != nil {
		t.Fatalf("write fake mate: %v", err)
	}
	current := filepath.Join(home, ".zcp", "mate", "current")
	if err := os.Symlink(filepath.Join("versions", version), current); err != nil {
		t.Fatalf("symlink current: %v", err)
	}
	// The release is what is installed: no network, nothing to change.
	service.SetMateEnsureFunc(func(mate.EnsureOptions) (mate.Result, error) {
		return mate.Result{Action: mate.ActionNone, From: version, To: version}, nil
	})
	t.Cleanup(service.ResetMateEnsureFunc)
	return home
}

// TestStart_Mate_Argv locks the whole supervised command: the entry point is the
// bundle inside the prefix (never `npx`, never a PATH lookup), and the argv
// carries no --base-path — whatever the bundle's `serve --help` would say, and
// without running it. The prefix rides the launch environment
// (TestStart_Mate_BasePathRidesTheEnv); a flag the bundle might reject is a
// fatal parse error for the mate CLI, and would crash-loop the unit at boot.
func TestStart_Mate_Argv(t *testing.T) {
	// Not parallel — mutates runFunc, HOME and ZCP_MATE_ENABLED.
	t.Setenv("ZCP_MATE_ENABLED", "1")
	home := installFakeMateBundle(t)
	var gotBinary string
	var gotArgs []string
	service.SetRunFunc(func(binary string, args []string, _ []string) error {
		gotBinary, gotArgs = binary, args
		return nil
	})
	t.Cleanup(func() { service.ResetRunFunc() })

	if err := service.Start("mate"); err != nil {
		t.Fatalf("Start(mate): %v", err)
	}

	wantBin := filepath.Join(home, ".zcp", "mate", "current", "node_modules", ".bin", mate.BinName)
	if gotBinary != wantBin {
		t.Errorf("binary: got %q, want %q", gotBinary, wantBin)
	}
	if slices.Contains(gotArgs, "npx") {
		t.Error("mate must run the local bundle, never npx")
	}
	if slices.Contains(gotArgs, "--base-path") {
		t.Errorf("argv must not carry --base-path, the prefix rides the env: %q", gotArgs)
	}
	for _, want := range []string{"serve", "--mode", "web", "--host", "127.0.0.1", "--no-browser", "--auto-bootstrap-project-from-cwd", "/var/www"} {
		if !slices.Contains(gotArgs, want) {
			t.Errorf("argv must contain %q, got %q", want, gotArgs)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "help-ran")); err == nil {
		t.Error("a launch must not run the bundle's serve --help")
	}
}

// TestStart_Mate_MergesEnvFile locks the delivery of the Zerops identity
// contract to the supervised process: `zcp init` writes the file while the
// full container environment is present, and the unit — whose own environment
// is not guaranteed to carry `projectId` — gets it from there.
func TestStart_Mate_MergesEnvFile(t *testing.T) {
	// Not parallel — mutates runFunc, HOME and ZCP_MATE_ENABLED.
	t.Setenv("ZCP_MATE_ENABLED", "1")
	home := installFakeMateBundle(t)
	envFile := filepath.Join(home, ".zcp", "mate.env")
	body := "T3CODE_ZEROPS_PROJECT_ID=nTV3oMB2SS634ImDJnQckg\nT3CODE_ZEROPS_API_HOST=api.app-prg1.zerops.io\n"
	if err := os.WriteFile(envFile, []byte(body), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	var gotEnv []string
	service.SetRunFunc(func(_ string, _ []string, extraEnv []string) error {
		gotEnv = extraEnv
		return nil
	})
	t.Cleanup(func() { service.ResetRunFunc() })

	if err := service.Start("mate"); err != nil {
		t.Fatalf("Start(mate): %v", err)
	}
	want := []string{"T3CODE_ZEROPS_PROJECT_ID=nTV3oMB2SS634ImDJnQckg", "T3CODE_ZEROPS_API_HOST=api.app-prg1.zerops.io", "T3CODE_BASE_PATH=/mate",
		"ZCP_MATE_UPDATE_STATE_FILE=" + mate.UpdateStatePath(),
		"ZCP_STATUS_FILE=" + filepath.Join(home, ".zcp", "state", "mate-status.json"),
		"T3CODE_ZEROPS_HQ_ENROLLMENT=" + filepath.Join(home, ".zcp", "hq", "enrollment.json")}
	if !slices.Equal(gotEnv, want) {
		t.Errorf("merged env:\n got %q\nwant %q", gotEnv, want)
	}
}

// TestStart_Mate_BasePathRidesTheEnv: the server learns its public prefix
// from its environment on every launch — the one way every mate release since
// v0.1.0 reads it, and one no bundle can reject.
func TestStart_Mate_BasePathRidesTheEnv(t *testing.T) {
	// Not parallel — mutates runFunc, HOME and ZCP_MATE_ENABLED.
	t.Setenv("ZCP_MATE_ENABLED", "1")
	installFakeMateBundle(t)
	var gotEnv []string
	service.SetRunFunc(func(_ string, _ []string, extraEnv []string) error {
		gotEnv = extraEnv
		return nil
	})
	t.Cleanup(func() { service.ResetRunFunc() })

	if err := service.Start("mate"); err != nil {
		t.Fatalf("Start(mate): %v", err)
	}
	if !slices.Contains(gotEnv, "T3CODE_BASE_PATH=/mate") {
		t.Errorf("launch env %q must carry T3CODE_BASE_PATH=/mate", gotEnv)
	}
}

// TestStart_Mate_LaunchStartsTheSetup: every launch names the status file to
// the server and writes it before the server starts, and a launch whose
// container carries a runtimes plan starts the boot import beside the server
// — never before it.
func TestStart_Mate_LaunchStartsTheSetup(t *testing.T) {
	// Not parallel — mutates runFunc, HOME, the store path and the boot seam.
	t.Setenv("ZCP_MATE_ENABLED", "1")
	tests := []struct {
		name         string
		store        map[string]string
		wantRuntimes string
		wantBoot     bool
	}{
		{"no runtimes plan", map[string]string{"PATH": "/usr/bin"}, mate.RuntimesNone, false},
		{"a runtimes plan", map[string]string{"PATH": "/usr/bin", "MATE_SETUP_RUNTIMES": "c2VydmljZXM6IFtd"}, mate.RuntimesPending, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := installFakeMateBundle(t)
			service.SetMateStorePath(writeLiveEnvStore(t, tt.store))
			t.Cleanup(service.ResetMateStorePath)
			// The boot import runs until the server is up: one that held the
			// launch would never let it start.
			booted := make(chan string, 1)
			serverUp := make(chan struct{})
			service.SetMateSetupBoot(func(_ context.Context, path string, env func() func(string) string) {
				booted <- path + " " + env()("MATE_SETUP_RUNTIMES")
				<-serverUp
			})
			t.Cleanup(service.ResetMateSetupBoot)
			// Every launch keeps the Mate enrolled with its HQ, beside the
			// server, over the live env store as it is at each attempt.
			kept := make(chan string, 1)
			service.SetMateHQKeep(func(_ context.Context, env func() func(string) string) {
				kept <- env()("PATH")
			})
			t.Cleanup(func() { service.SetMateHQKeep(keepNothing) })
			// Enrollment finishes before the seed, over the same live store.
			enrolled := false
			service.SetMateHQPrepare(func(_ context.Context, lookup func(string) string) error {
				if lookup("PATH") != tt.store["PATH"] {
					t.Error("launch enrollment did not read the live store")
				}
				enrolled = true
				return nil
			})
			t.Cleanup(func() { service.SetMateHQPrepare(prepareNothing) })
			// The seed is ready before the server reads its signer store once.
			seeded := ""
			service.SetMateSeedSignIns(func(_ context.Context, lookup func(string) string) {
				if !enrolled {
					t.Error("seed ran before enrollment finished")
				}
				seeded = lookup("PATH")
			})
			t.Cleanup(func() { service.SetMateSeedSignIns(seedNothing) })
			statusPath := filepath.Join(home, ".zcp", "state", "mate-status.json")
			var gotEnv []string
			var atLaunch mate.Status
			var seededAtLaunch string
			service.SetRunFunc(func(_ string, _ []string, extraEnv []string) error {
				gotEnv = extraEnv
				seededAtLaunch = seeded
				atLaunch, _ = mate.ReadStatus(statusPath)
				close(serverUp)
				return nil
			})
			t.Cleanup(func() { service.ResetRunFunc() })

			started := make(chan error, 1)
			go func() { started <- service.Start("mate") }()
			select {
			case err := <-started:
				if err != nil {
					t.Fatalf("Start(mate): %v", err)
				}
			case <-time.After(5 * time.Second):
				close(serverUp)
				t.Fatal("the server never started: the launch waited on the boot import")
			}
			if seededAtLaunch != tt.store["PATH"] {
				t.Errorf("the sign-in seed ran over %q before the server started, want the live store's", seededAtLaunch)
			}
			if !slices.Contains(gotEnv, "ZCP_STATUS_FILE="+statusPath) {
				t.Errorf("launch env %q must name the status file %s", gotEnv, statusPath)
			}
			if atLaunch.Runtimes.State != tt.wantRuntimes {
				t.Errorf("runtimes at launch = %q, want %q", atLaunch.Runtimes.State, tt.wantRuntimes)
			}
			select {
			case got := <-booted:
				if !tt.wantBoot {
					t.Errorf("boot import started without a plan: %s", got)
				} else if got != statusPath+" "+tt.store["MATE_SETUP_RUNTIMES"] {
					t.Errorf("boot import got %q", got)
				}
			case <-time.After(2 * time.Second):
				if tt.wantBoot {
					t.Error("boot import never started")
				}
			}
			select {
			case got := <-kept:
				if got != tt.store["PATH"] {
					t.Errorf("HQ enrollment read the env %q, want the live store's", got)
				}
			case <-time.After(2 * time.Second):
				t.Error("the launch never started keeping the Mate enrolled with HQ")
			}
			if !slices.Contains(gotEnv, "T3CODE_ZEROPS_HQ_ENROLLMENT="+filepath.Join(home, ".zcp", "hq", "enrollment.json")) {
				t.Errorf("launch env %q must name the HQ enrollment the server links with", gotEnv)
			}
		})
	}
}

func TestStart_Mate_EnrollmentFailureStillStartsTheServer(t *testing.T) {
	// non-parallel: HOME and the launch seams are process-wide.
	t.Setenv("ZCP_MATE_ENABLED", "1")
	installFakeMateBundle(t)
	service.SetMateHQPrepare(func(context.Context, func(string) string) error { return errors.New("HQ unavailable") })
	t.Cleanup(func() { service.SetMateHQPrepare(prepareNothing) })
	seeded := false
	service.SetMateSeedSignIns(func(context.Context, func(string) string) { seeded = true })
	t.Cleanup(func() { service.SetMateSeedSignIns(seedNothing) })
	started := false
	service.SetRunFunc(func(string, []string, []string) error { started = true; return nil })
	t.Cleanup(service.ResetRunFunc)
	if err := service.Start("mate"); err != nil {
		t.Fatal(err)
	}
	if !started || seeded {
		t.Fatalf("started = %v, seeded without enrollment = %v", started, seeded)
	}
}

// TestStart_Mate_MissingEnvFile_StillStarts: a container whose env file did not
// get written (an init that degraded) must still bring mate up — the server
// refuses the Zerops identity path on its own, which is a diagnosable state.
// A unit that refuses to launch is not.
func TestStart_Mate_MissingEnvFile_StillStarts(t *testing.T) {
	// Not parallel — mutates runFunc, HOME and ZCP_MATE_ENABLED.
	t.Setenv("ZCP_MATE_ENABLED", "1")
	installFakeMateBundle(t)
	called := false
	service.SetRunFunc(func(string, []string, []string) error {
		called = true
		return nil
	})
	t.Cleanup(func() { service.ResetRunFunc() })

	if err := service.Start("mate"); err != nil {
		t.Fatalf("Start(mate) with no env file: %v", err)
	}
	if !called {
		t.Error("mate must start even without its env file")
	}
}

// TestStart_Mate_GuardRefusesWhenDisabled: a unit that outlived a failed
// removal (init_mate.go's disableMate, on a real `zsc unit remove` failure) must
// not resurrect the server — every launch re-checks the flag itself,
// independent of whatever `zcp init` last did.
//
// The store is pointed at an explicit "flag absent" file rather than left to
// the real one: this process's own environment never carries the flag under
// systemd, so without a store to read the guard would fall back to its
// fail-open branch and this test would pass or fail on whether the machine
// running it happens to have /etc/zerops-zembed/env.json.
func TestStart_Mate_GuardRefusesWhenDisabled(t *testing.T) {
	// Not parallel — mutates runFunc, HOME, the store path and ZCP_MATE_ENABLED.
	t.Setenv("ZCP_MATE_ENABLED", "")
	installFakeMateBundle(t)
	service.SetMateStorePath(writeLiveEnvStore(t, map[string]string{"PATH": "/usr/bin"}))
	t.Cleanup(service.ResetMateStorePath)
	called := false
	service.SetRunFunc(func(string, []string, []string) error {
		called = true
		return nil
	})
	t.Cleanup(func() { service.ResetRunFunc() })

	err := service.Start("mate")
	if err == nil {
		t.Fatal("expected an error when ZCP_MATE_ENABLED is off")
	}
	if !strings.Contains(err.Error(), "ZCP_MATE_ENABLED") {
		t.Errorf("error must name ZCP_MATE_ENABLED, got: %v", err)
	}
	if called {
		t.Error("the run function must not be called when the guard refuses")
	}
}

// TestStart_Mate_GuardAllowsWhenEnabled is the flip side: with the flag on,
// Start behaves exactly as before the guard existed.
func TestStart_Mate_GuardAllowsWhenEnabled(t *testing.T) {
	// Not parallel — mutates runFunc, HOME and ZCP_MATE_ENABLED.
	t.Setenv("ZCP_MATE_ENABLED", "1")
	installFakeMateBundle(t)
	called := false
	service.SetRunFunc(func(string, []string, []string) error {
		called = true
		return nil
	})
	t.Cleanup(func() { service.ResetRunFunc() })

	if err := service.Start("mate"); err != nil {
		t.Fatalf("Start(mate) with the flag on: %v", err)
	}
	if !called {
		t.Error("the run function must be called when the guard allows")
	}
}

// TestStart_OtherServices_UnaffectedByMateGuard: nginx and vscode declare no
// guard, so they must launch regardless of ZCP_MATE_ENABLED.
func TestStart_OtherServices_UnaffectedByMateGuard(t *testing.T) {
	// Not parallel — mutates runFunc, tuneFunc, and ZCP_MATE_ENABLED.
	t.Setenv("ZCP_MATE_ENABLED", "")
	service.SetRunFunc(func(string, []string, []string) error { return nil })
	// vscode declares a TasksMax tune; stub it so this test never shells out
	// to `sudo systemctl set-property` (docs: TestServiceStart_TasksMaxTunerStubbed_NoSudo).
	service.SetTuneFunc(func(string, int) error { return nil })
	t.Cleanup(func() { service.ResetRunFunc(); service.ResetTuneFunc() })

	for _, name := range []string{"nginx", "vscode"} {
		if err := service.Start(name); err != nil {
			if strings.Contains(err.Error(), "find") {
				t.Skipf("binary not found (expected in CI): %v", err)
			}
			t.Errorf("Start(%q) with ZCP_MATE_ENABLED off: %v", name, err)
		}
	}
}

// writeLiveEnvStore lays down the container's live service-env snapshot —
// the root-owned JSON the platform rewrites on every env change, and the
// source a login shell is populated from.
func writeLiveEnvStore(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "env.json")
	body, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal store: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write store: %v", err)
	}
	return path
}

// TestStart_Mate_GuardReadsLiveEnvStore_NotOnlyProcessEnv is the regression for
// a defect found live on z3-eval, not in this suite: `zcp service start mate` IS
// the systemd unit's ExecStart, and a unit inherits almost nothing — HOME and
// PATH — so ZCP_MATE_ENABLED is NOT in this process's environment even on a
// container where it is set. A guard reading only os.Environ refused every
// start and crash-looped the unit (restart counter reached 13 before it was
// caught). The flag has to come from the same live env store the supervisor
// already merges into the child.
func TestStart_Mate_GuardReadsLiveEnvStore_NotOnlyProcessEnv(t *testing.T) {
	// Not parallel — package-level run/store hooks and HOME.
	installFakeMateBundle(t)
	t.Setenv("ZCP_MATE_ENABLED", "") // exactly what systemd hands the unit

	service.SetMateStorePath(writeLiveEnvStore(t, map[string]string{
		"PATH":             "/usr/bin",
		"ZCP_MATE_ENABLED": "1",
		"VSCODE_PASSWORD":  "irrelevant",
	}))
	t.Cleanup(service.ResetMateStorePath)

	var ran bool
	service.SetRunFunc(func(_ string, _, _ []string) error { ran = true; return nil })
	t.Cleanup(service.ResetRunFunc)

	if err := service.Start("mate"); err != nil {
		t.Fatalf("the store says enabled, so mate must start: %v", err)
	}
	if !ran {
		t.Error("mate must actually launch when the live env store says it is enabled")
	}
}

// TestStart_Mate_GuardRefusesWhenStoreSaysDisabled keeps the guard's reason for
// existing: a unit that outlived a failed `zsc unit remove` must not resurrect
// the server. The store is what `zcp init` read when it tried to remove it.
func TestStart_Mate_GuardRefusesWhenStoreSaysDisabled(t *testing.T) {
	installFakeMateBundle(t)
	t.Setenv("ZCP_MATE_ENABLED", "")

	service.SetMateStorePath(writeLiveEnvStore(t, map[string]string{"PATH": "/usr/bin"}))
	t.Cleanup(service.ResetMateStorePath)

	var ran bool
	service.SetRunFunc(func(_ string, _, _ []string) error { ran = true; return nil })
	t.Cleanup(service.ResetRunFunc)

	err := service.Start("mate")
	if err == nil {
		t.Fatal("a store without the flag must refuse the start")
	}
	if !strings.Contains(err.Error(), "ZCP_MATE_ENABLED") {
		t.Errorf("the refusal must name the gate, got %v", err)
	}
	if ran {
		t.Error("mate must not launch when the store says it is disabled")
	}
}

// TestStart_Mate_GuardFailsOpenOnUnreadableStore: the unit exists only because a
// `zcp init` that saw the flag on created it, and init is also what removes it
// — so on a container whose env store is broken, starting beats crash-looping.
func TestStart_Mate_GuardFailsOpenOnUnreadableStore(t *testing.T) {
	installFakeMateBundle(t)
	t.Setenv("ZCP_MATE_ENABLED", "")

	service.SetMateStorePath(filepath.Join(t.TempDir(), "absent.json"))
	t.Cleanup(service.ResetMateStorePath)

	var ran bool
	service.SetRunFunc(func(_ string, _, _ []string) error { ran = true; return nil })
	t.Cleanup(service.ResetRunFunc)

	if err := service.Start("mate"); err != nil {
		t.Fatalf("an unreadable store must not block the start: %v", err)
	}
	if !ran {
		t.Error("mate must launch when the store cannot be read")
	}
}

// TestStart_Mate_InstallsBeforeItStarts: a Mate's unit starts at boot before
// `zcp init` updates its bundle, so it served the old release and was
// restarted onto the new one — two starts, ~10 s down each. It now brings the
// bundle to the release under the install lock `zcp init` shares, then starts
// what is installed: whichever of the two runs first installs and the other
// finds nothing to do. Nothing it meets ever keeps the server down: a failed
// install or a lock held past the wait starts what is installed.
func TestStart_Mate_InstallsBeforeItStarts(t *testing.T) {
	// Not parallel — mutates runFunc, HOME and ZCP_MATE_ENABLED.
	t.Setenv("ZCP_MATE_ENABLED", "1")
	tests := []struct {
		name       string
		ensureErr  error
		lockedAway bool
		wantEnsure bool
	}{
		{name: "the release is installed under the lock, then started", wantEnsure: true},
		{name: "an install that fails still starts what is installed", ensureErr: errors.New("registry down"), wantEnsure: true},
		{name: "a lock held past the wait starts what is installed", lockedAway: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installFakeMateBundle(t)
			service.SetMateLockWait(200 * time.Millisecond)
			t.Cleanup(service.ResetMateLockWait)
			if tt.lockedAway {
				release, err := mate.LockInstall(0)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(release)
			}
			var order []string
			service.SetMateEnsureFunc(func(opts mate.EnsureOptions) (mate.Result, error) {
				if _, err := mate.LockInstall(0); !errors.Is(err, mate.ErrInstallLockBusy) {
					t.Errorf("the install ran without the lock held: %v", err)
				}
				if !opts.Refresh {
					t.Error("a start reads the release manifest afresh, as a boot does")
				}
				order = append(order, "ensure")
				return mate.Result{Action: mate.ActionUpdated, From: "0.0.9", To: "0.1.0"}, tt.ensureErr
			})
			service.SetRunFunc(func(string, []string, []string) error {
				if release, err := mate.LockInstall(0); err == nil {
					release()
				} else if !tt.lockedAway {
					t.Error("the server started with the install lock still held")
				}
				order = append(order, "run")
				return nil
			})
			t.Cleanup(service.ResetRunFunc)

			if err := service.Start("mate"); err != nil {
				t.Fatalf("Start(mate): %v", err)
			}
			want := []string{"run"}
			if tt.wantEnsure {
				want = []string{"ensure", "run"}
			}
			if !slices.Equal(order, want) {
				t.Errorf("order = %v, want %v", order, want)
			}
		})
	}
}
