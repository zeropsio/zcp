package service_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
	// Not parallel — mutates runFunc.
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
	t.Cleanup(func() { service.ResetRunFunc() })

	tests := []struct {
		name     string
		svc      string
		wantArgs []string
	}{
		{
			"nginx",
			"nginx",
			[]string{"nginx", "-g", "daemon off;"},
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
			// LookPath may fail if binary not installed — that's OK in CI.
			if err != nil {
				if strings.Contains(err.Error(), "find") {
					t.Skipf("binary not found (expected in CI): %v", err)
				}
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

func TestList_ReturnsAllServices(t *testing.T) {
	t.Parallel()
	names := service.List()

	want := map[string]bool{"nginx": false, "vscode": false, "z3": false}
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

// installFakeZ3Bundle lays down a bundle that looks exactly like an
// `npm install --prefix ~/.zcp/z3 t3@<version>` result, with a `t3` whose
// `serve --help` advertises (or hides) --base-path, and returns HOME.
func installFakeZ3Bundle(t *testing.T, advertisesBasePath bool) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	binDir := filepath.Join(home, ".zcp", "z3", "node_modules", ".bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	help := "  --base-dir   Data directory"
	if advertisesBasePath {
		help = "  --base-path   Public path prefix"
	}
	if err := os.WriteFile(filepath.Join(binDir, "t3"), []byte("#!/bin/sh\necho '"+help+"'\n"), 0o700); err != nil {
		t.Fatalf("write fake t3: %v", err)
	}
	return home
}

// TestStart_Z3_Argv locks the whole supervised command: the entry point is the
// bundle inside the prefix (never `npx`, never a PATH lookup), and --base-path
// is passed only when the installed bundle advertises it — an unknown flag is
// a fatal parse error for the z3 CLI, so passing it blind would crash-loop the
// unit at every container boot.
func TestStart_Z3_Argv(t *testing.T) {
	// Not parallel — mutates runFunc and HOME.
	tests := []struct {
		name         string
		advertises   bool
		wantBasePath bool
	}{
		{"bundle advertises --base-path", true, true},
		{"bundle predates --base-path", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := installFakeZ3Bundle(t, tt.advertises)
			var gotBinary string
			var gotArgs []string
			service.SetRunFunc(func(binary string, args []string, _ []string) error {
				gotBinary, gotArgs = binary, args
				return nil
			})
			t.Cleanup(func() { service.ResetRunFunc() })

			if err := service.Start("z3"); err != nil {
				t.Fatalf("Start(z3): %v", err)
			}

			wantBin := filepath.Join(home, ".zcp", "z3", "node_modules", ".bin", "t3")
			if gotBinary != wantBin {
				t.Errorf("binary: got %q, want %q", gotBinary, wantBin)
			}
			if slices.Contains(gotArgs, "npx") {
				t.Error("z3 must run the local bundle, never npx")
			}
			if hasBasePath := slices.Contains(gotArgs, "--base-path"); hasBasePath != tt.wantBasePath {
				t.Errorf("--base-path present = %v, want %v (argv %q)", hasBasePath, tt.wantBasePath, gotArgs)
			}
			for _, want := range []string{"serve", "--mode", "web", "--host", "127.0.0.1", "--no-browser", "--auto-bootstrap-project-from-cwd", "/var/www"} {
				if !slices.Contains(gotArgs, want) {
					t.Errorf("argv must contain %q, got %q", want, gotArgs)
				}
			}
		})
	}
}

// TestStart_Z3_MergesEnvFile locks the delivery of the Zerops identity
// contract to the supervised process: `zcp init` writes the file while the
// full container environment is present, and the unit — whose own environment
// is not guaranteed to carry `projectId` — gets it from there.
func TestStart_Z3_MergesEnvFile(t *testing.T) {
	// Not parallel — mutates runFunc and HOME.
	home := installFakeZ3Bundle(t, true)
	envFile := filepath.Join(home, ".zcp", "z3.env")
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

	if err := service.Start("z3"); err != nil {
		t.Fatalf("Start(z3): %v", err)
	}
	want := []string{"T3CODE_ZEROPS_PROJECT_ID=nTV3oMB2SS634ImDJnQckg", "T3CODE_ZEROPS_API_HOST=api.app-prg1.zerops.io"}
	if !slices.Equal(gotEnv, want) {
		t.Errorf("merged env:\n got %q\nwant %q", gotEnv, want)
	}
}

// TestStart_Z3_MissingEnvFile_StillStarts: a container whose env file did not
// get written (an init that degraded) must still bring z3 up — the server
// refuses the Zerops identity path on its own, which is a diagnosable state.
// A unit that refuses to launch is not.
func TestStart_Z3_MissingEnvFile_StillStarts(t *testing.T) {
	// Not parallel — mutates runFunc and HOME.
	installFakeZ3Bundle(t, true)
	called := false
	service.SetRunFunc(func(string, []string, []string) error {
		called = true
		return nil
	})
	t.Cleanup(func() { service.ResetRunFunc() })

	if err := service.Start("z3"); err != nil {
		t.Fatalf("Start(z3) with no env file: %v", err)
	}
	if !called {
		t.Error("z3 must start even without its env file")
	}
}
