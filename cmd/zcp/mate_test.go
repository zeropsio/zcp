// Tests for: `zcp mate status` and `zcp mate update` (cmd/zcp/mate.go) —
// the two CLI entry points that share mate.DesiredRelease/EnsureInstalled
// with the init step (spec-mate.md §2.1a, §2.9, invariant MD-17).
//
// NOT parallel — every test redirects HOME, PATH, ZCP_MATE_MANIFEST_URL and
// the container-detection env vars, all process-global via t.Setenv.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/mate"
)

// captureStdout redirects os.Stdout for the duration of fn and returns what
// was written to it — the --json subcommands print their result there.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()
	fn()
	os.Stdout = orig
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

// writeFakeBin writes an executable script at path, mirroring the helper
// internal/mate's own tests use.
func writeFakeBin(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
}

// manifestVersion is the release every test in this file resolves toward —
// arbitrary, standing in for whatever the real release manifest would name.
const manifestVersion = "0.9.0"

// manifestAndTarballServer serves a valid stable.json at /stable.json for
// manifestVersion, plus the tarball body it names, both real loopback HTTPS
// (mate.DesiredRelease and InstallRelease always use http.DefaultClient —
// there is no client injection seam at the CLI layer, and a manifest's "url"
// must be https — see validateManifest) so ZCP_MATE_MANIFEST_URL is the only
// wiring a test needs. http.DefaultClient is swapped for the test server's
// own cert-trusting client for the duration of the test, restored on
// cleanup — safe since this file's tests are not parallel (see the package
// doc comment).
func manifestAndTarballServer(t *testing.T) {
	t.Helper()
	manifestServerForVersion(t, manifestVersion)
}

// manifestServerForVersion is manifestAndTarballServer generalized to an
// arbitrary version, so a test can seed the on-disk manifest cache at one
// version and then point ZCP_MATE_MANIFEST_URL at a server answering a
// different one — proving --refresh bypasses the cache rather than merely
// exercising the happy path.
func manifestServerForVersion(t *testing.T, version string) {
	t.Helper()
	body := []byte("fake mate release tarball for " + version)
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	asset := "zerops-mate-" + version + ".tgz"

	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/stable.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version":     version,
			"asset":       asset,
			"url":         server.URL + "/" + asset,
			"sha256":      digest,
			"size":        len(body),
			"contract":    mate.SupportedContract,
			"publishedAt": "2026-09-09T00:00:00Z",
		})
	})
	mux.HandleFunc("/"+asset, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	})

	server = httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	t.Setenv("ZCP_MATE_MANIFEST_URL", server.URL+"/stable.json")

	origClient := http.DefaultClient
	http.DefaultClient = server.Client()
	t.Cleanup(func() { http.DefaultClient = origClient })
}

// seedInstalledBundle lays down what a completed `npm install --prefix` at
// version leaves behind, and activates it — matching what EnsureInstalled
// itself produces, so a test can start from "already installed" without
// running a real install.
func seedInstalledBundle(t *testing.T, home, version string) {
	t.Helper()
	dir := filepath.Join(home, ".zcp", "mate", "versions", version)
	pkgDir := filepath.Join(dir, "node_modules", mate.PackageName)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", pkgDir, err)
	}
	body := `{"name":"` + mate.PackageName + `","version":"` + version + `"}`
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	current := filepath.Join(home, ".zcp", "mate", "current")
	if err := os.Symlink(filepath.Join("versions", version), current); err != nil {
		t.Fatalf("symlink current: %v", err)
	}
}

// writeFakeInstallTools puts a fake npm and node on PATH so a real
// EnsureInstalled pass (download real, over loopback; npm install and the
// native-addon smoke probe fake) can stage and activate toVersion without a
// real npm registry or Node runtime. npm writes exactly what
// mate.InstalledVersion()/BinPath() expect to find; node exits 0
// unconditionally, satisfying the native-addon probe.
func writeFakeInstallTools(t *testing.T, toVersion string) {
	t.Helper()
	binDir := t.TempDir()
	// npm's fake script below shells out to mkdir/chmod; keep the system
	// PATH available for those while still resolving "npm"/"node" to the
	// fakes ahead of any real ones.
	t.Setenv("PATH", binDir+":/bin:/usr/bin")

	writeFakeBin(t, filepath.Join(binDir, "npm"), fmt.Sprintf(`#!/bin/sh
prefix=
prev=
for arg do
  if [ "$prev" = "--prefix" ]; then prefix=$arg; fi
  prev=$arg
done
mkdir -p "$prefix/node_modules/%s"
mkdir -p "$prefix/node_modules/.bin"
printf '{"name":"%s","version":"%s"}' > "$prefix/node_modules/%s/package.json"
printf '#!/bin/sh\necho mate %s\n' > "$prefix/node_modules/.bin/%s"
chmod +x "$prefix/node_modules/.bin/%s"
`, mate.PackageName, mate.PackageName, toVersion, mate.PackageName, toVersion, mate.BinName, mate.BinName))
	writeFakeBin(t, filepath.Join(binDir, "node"), "#!/bin/sh\nexit 0\n")
}

// containerEnv makes runtime.Detect() report an enabled Zerops container —
// the gate `zcp mate update` refuses without.
func containerEnv(t *testing.T) {
	t.Helper()
	t.Setenv("serviceId", "gt7tJZjDSk2zyH5XvNeAQQ")
	t.Setenv("projectId", "nTV3oMB2SS634ImDJnQckg")
	t.Setenv("ZCP_MATE_ENABLED", "1")
}

func TestRunMateCmd_UnknownSubcommand_Fails(t *testing.T) {
	tests := [][]string{nil, {}, {"bogus"}, {"--force"}}
	for _, args := range tests {
		if got := runMateCmd(args); got != 1 {
			t.Errorf("runMateCmd(%v) = %d, want 1", args, got)
		}
	}
}

func TestRunMateStatus_NothingInstalled_ReportsLatest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manifestAndTarballServer(t)

	if got := runMateCmd([]string{"status", "--json"}); got != 0 {
		t.Fatalf("runMateCmd(status --json) = %d, want 0", got)
	}
}

func TestRunMateStatus_JSON_ReportsUpdateAvailable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	seedInstalledBundle(t, home, "0.8.1")
	manifestAndTarballServer(t)

	stdout := captureStdout(t, func() {
		if got := runMateCmd([]string{"status", "--json"}); got != 0 {
			t.Fatalf("runMateCmd(status --json) = %d, want 0", got)
		}
	})

	var got struct {
		Installed       string `json:"installed"`
		Latest          string `json:"latest"`
		Contract        int    `json:"contract"`
		UpdateAvailable bool   `json:"updateAvailable"`
		CheckedAt       string `json:"checkedAt"`
		Error           string `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &got); err != nil {
		t.Fatalf("unmarshal status JSON %q: %v", stdout, err)
	}
	if got.Installed != "0.8.1" || got.Latest != manifestVersion || !got.UpdateAvailable {
		t.Errorf("status = %+v, want installed=0.8.1 latest=0.9.0 updateAvailable=true", got)
	}
	if got.Contract != mate.SupportedContract {
		t.Errorf("status.Contract = %d, want %d", got.Contract, mate.SupportedContract)
	}
	if got.CheckedAt == "" {
		t.Error("status.CheckedAt must be set")
	}
	if got.Error != "" {
		t.Errorf("status.Error = %q, want empty", got.Error)
	}
}

func TestRunMateStatus_Refresh_BypassesManifestCache(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantLatest string
	}{
		{"no refresh: reports the warm cache", []string{"status", "--json"}, "0.8.5"},
		{"--refresh: bypasses the cache", []string{"status", "--json", "--refresh"}, "0.9.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			seedInstalledBundle(t, home, "0.8.1")

			// Warm the manifest cache at 0.8.5.
			manifestServerForVersion(t, "0.8.5")
			if got := runMateCmd([]string{"status", "--json"}); got != 0 {
				t.Fatalf("warm cache: runMateCmd(status --json) = %d, want 0", got)
			}

			// Point the manifest URL at a server answering a newer version —
			// only --refresh should reach it; a warm cache answers 0.8.5.
			manifestServerForVersion(t, "0.9.0")

			stdout := captureStdout(t, func() {
				if got := runMateCmd(tt.args); got != 0 {
					t.Fatalf("runMateCmd(%v) = %d, want 0", tt.args, got)
				}
			})

			var got struct {
				Latest string `json:"latest"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &got); err != nil {
				t.Fatalf("unmarshal status JSON %q: %v", stdout, err)
			}
			if got.Latest != tt.wantLatest {
				t.Errorf("status.Latest = %q, want %q", got.Latest, tt.wantLatest)
			}
		})
	}
}

func TestRunMateStatus_ManifestUnreachable_ExitsZeroWithError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	seedInstalledBundle(t, home, "0.8.1")
	t.Setenv("ZCP_MATE_MANIFEST_URL", "http://127.0.0.1:1/stable.json") // nothing listens here

	stdout := captureStdout(t, func() {
		if got := runMateCmd([]string{"status", "--json"}); got != 0 {
			t.Fatalf("runMateCmd(status --json) = %d, want 0 even when the manifest is unreachable", got)
		}
	})

	var got struct {
		Installed       string `json:"installed"`
		UpdateAvailable bool   `json:"updateAvailable"`
		Error           string `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &got); err != nil {
		t.Fatalf("unmarshal status JSON %q: %v", stdout, err)
	}
	if got.Installed != "0.8.1" {
		t.Errorf("status.Installed = %q, want 0.8.1", got.Installed)
	}
	if got.UpdateAvailable {
		t.Error("status.UpdateAvailable must be false when the manifest is unreachable")
	}
	if got.Error == "" {
		t.Error("status.Error must name why latest is unknown")
	}
}

func TestRunMateUpdate_RefusesOutsideContainer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("serviceId", "")

	if got := runMateCmd([]string{"update"}); got != 1 {
		t.Errorf("runMateCmd(update) outside a container = %d, want 1", got)
	}
}

func TestRunMateUpdate_RefusesWhenMateDisabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("serviceId", "gt7tJZjDSk2zyH5XvNeAQQ")
	t.Setenv("ZCP_MATE_ENABLED", "")

	if got := runMateCmd([]string{"update"}); got != 1 {
		t.Errorf("runMateCmd(update) with mate disabled = %d, want 1", got)
	}
}

func TestRunMateUpdate_AlreadyUpToDate_NoRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	containerEnv(t)
	seedInstalledBundle(t, home, manifestVersion)
	manifestAndTarballServer(t)

	unitPath := filepath.Join(t.TempDir(), "zerops@mate.service")
	if err := os.WriteFile(unitPath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatalf("seed unit file: %v", err)
	}
	origUnitPath := mateUnitFilePath
	mateUnitFilePath = unitPath
	t.Cleanup(func() { mateUnitFilePath = origUnitPath })

	restarted := false
	origRunner := mateRestartUnit
	mateRestartUnit = func(string) error { restarted = true; return nil }
	t.Cleanup(func() { mateRestartUnit = origRunner })

	if got := runMateCmd([]string{"update"}); got != 0 {
		t.Fatalf("runMateCmd(update) = %d, want 0", got)
	}
	if restarted {
		t.Error("nothing changed, so no restart must be attempted")
	}
}

func TestRunMateUpdate_InstallsAndRestartsWhenUnitPresent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	containerEnv(t)
	manifestAndTarballServer(t)
	writeFakeInstallTools(t, manifestVersion)
	// ZCP_MATE_MANIFEST_URL and PATH are both set via t.Setenv above; the
	// container env vars must survive the PATH overwrite, which they do
	// since containerEnv used t.Setenv itself (independent keys).

	unitPath := filepath.Join(t.TempDir(), "zerops@mate.service")
	if err := os.WriteFile(unitPath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatalf("seed unit file: %v", err)
	}
	origUnitPath := mateUnitFilePath
	mateUnitFilePath = unitPath
	t.Cleanup(func() { mateUnitFilePath = origUnitPath })

	var restartedUnit string
	origRunner := mateRestartUnit
	mateRestartUnit = func(unit string) error { restartedUnit = unit; return nil }
	t.Cleanup(func() { mateRestartUnit = origRunner })

	stdout := captureStdout(t, func() {
		if got := runMateCmd([]string{"update", "--json"}); got != 0 {
			t.Fatalf("runMateCmd(update --json) = %d, want 0", got)
		}
	})

	if want := "zerops@mate.service"; restartedUnit != want {
		t.Errorf("restarted unit = %q, want %q", restartedUnit, want)
	}
	var got struct {
		Action    string `json:"action"`
		To        string `json:"to"`
		Restarted bool   `json:"restarted"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &got); err != nil {
		t.Fatalf("unmarshal update JSON %q: %v", stdout, err)
	}
	if got.Action != string(mate.ActionInstalled) || got.To != manifestVersion || !got.Restarted {
		t.Errorf("update result = %+v, want action=installed to=0.9.0 restarted=true", got)
	}
}

func TestRunMateUpdate_NoUnitFile_SkipsRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	containerEnv(t)
	manifestAndTarballServer(t)
	writeFakeInstallTools(t, manifestVersion)

	origUnitPath := mateUnitFilePath
	mateUnitFilePath = filepath.Join(t.TempDir(), "no-such-unit.service")
	t.Cleanup(func() { mateUnitFilePath = origUnitPath })

	restarted := false
	origRunner := mateRestartUnit
	mateRestartUnit = func(string) error { restarted = true; return nil }
	t.Cleanup(func() { mateRestartUnit = origRunner })

	if got := runMateCmd([]string{"update"}); got != 0 {
		t.Fatalf("runMateCmd(update) = %d, want 0", got)
	}
	if restarted {
		t.Error("no unit file present must mean no restart attempt")
	}
}

func TestRunMateUpdate_RestartFailure_ReturnsNonZero(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	containerEnv(t)
	manifestAndTarballServer(t)
	writeFakeInstallTools(t, manifestVersion)

	unitPath := filepath.Join(t.TempDir(), "zerops@mate.service")
	if err := os.WriteFile(unitPath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatalf("seed unit file: %v", err)
	}
	origUnitPath := mateUnitFilePath
	mateUnitFilePath = unitPath
	t.Cleanup(func() { mateUnitFilePath = origUnitPath })

	origRunner := mateRestartUnit
	mateRestartUnit = func(string) error { return os.ErrPermission }
	t.Cleanup(func() { mateRestartUnit = origRunner })

	if got := runMateCmd([]string{"update"}); got != 1 {
		t.Errorf("runMateCmd(update) = %d, want 1 when the restart fails", got)
	}
}
