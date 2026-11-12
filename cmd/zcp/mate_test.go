// Tests for: `zcp mate status` and `zcp mate update` (cmd/zcp/mate.go) —
// the CLI entry points over mate.DesiredRelease and the versions the
// container holds (spec-mate.md §2.1a, §2.9, invariant MD-17).
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
	installFakeExecutable(t, path, body)
}

// manifestVersion is the release every test in this file resolves toward —
// arbitrary, standing in for whatever the real release manifest would name.
const manifestVersion = mate.MinimumMateVersion

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
	version := manifestVersion
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
// version leaves behind, and activates it — matching what the start job
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
// install (download real, over loopback; npm install and the
// native-addon smoke probe fake) can stage and activate manifestVersion without a
// real npm registry or Node runtime. npm writes exactly what
// mate.InstalledVersion()/BinPath() expect to find; node exits 0
// unconditionally, satisfying the native-addon probe.
func writeFakeInstallTools(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	// npm's fake script below shells out to mkdir/ln; keep the system
	// PATH available for those while still resolving "npm"/"node" to the
	// fakes ahead of any real ones.
	t.Setenv("PATH", binDir+":/bin:/usr/bin")

	// The installed mate is a link to one shared script, so its --version
	// probe skips the first-exec assessment installFakeExecutable avoids.
	mateBin := sharedFakeExecutable(t, fmt.Sprintf("#!/bin/sh\necho mate %s\n", manifestVersion))
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
ln "%s" "$prefix/node_modules/.bin/%s"
`, mate.PackageName, mate.PackageName, manifestVersion, mate.PackageName, mateBin, mate.BinName))
	writeFakeBin(t, filepath.Join(binDir, "node"), "#!/bin/sh\nexit 0\n")
}

// containerEnv makes runtime.Detect() report an enabled Zerops container —
// the gate `zcp mate update` refuses without.
func containerEnv(t *testing.T) {
	t.Helper()
	t.Setenv("serviceId", "gt7tJZjDSk2zyH5XvNeAQQ")
	t.Setenv("projectId", "nTV3oMB2SS634ImDJnQckg")
	t.Setenv("ZCP_MATE_ENABLED", "1")
	// This legacy CLI fixture explicitly answers that the updater endpoint is absent.
	old := http.DefaultTransport
	http.DefaultTransport = capabilityTransport{status: http.StatusNotFound, body: "not found", fallback: old}
	t.Cleanup(func() { http.DefaultTransport = old })
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
	seedInstalledBundle(t, home, "0.14.0")
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
	if got.Installed != "0.14.0" || got.Latest != manifestVersion || !got.UpdateAvailable {
		t.Errorf("status = %+v, want installed=0.14.0 latest=%s updateAvailable=true", got, manifestVersion)
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

func TestRunMateStatus_ManifestUnreachable_ExitsZeroWithError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	seedInstalledBundle(t, home, "0.14.0")
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
	if got.Installed != "0.14.0" {
		t.Errorf("status.Installed = %q, want 0.14.0", got.Installed)
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

// Milo, 2026-10-09: after a manual update the descriptor still described the
// automatic updater's postponement on a version two releases back.
func TestRunMateStatus_AfterAManualUpdate_ReportsNoOlderAttempt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	seedInstalledBundle(t, home, manifestVersion)
	manifestAndTarballServer(t)
	if err := os.WriteFile(mate.FailedVersionPath(), []byte("0.13.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout := captureStdout(t, func() {
		if runMateCmd([]string{"status", "--json"}) != 0 {
			t.Fatal("status failed")
		}
	})
	var got struct {
		Updater mateUpdaterStatus `json:"updater"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &got); err != nil {
		t.Fatal(err)
	}
	if got.Updater.Phase != "idle" || got.Updater.RunningVersion != manifestVersion || got.Updater.FailedVersion != "" {
		t.Fatalf("updater=%+v, want idle on %s with no failed version", got.Updater, manifestVersion)
	}
}

// TestRunMateStatus_JSON_ReportsTheFloor: the receipt for the version floor —
// the minimum this zcp drives, and whether the installed bundle is below it.
func TestRunMateStatus_JSON_ReportsTheFloor(t *testing.T) {
	tests := []struct {
		name      string
		installed string
		wantBelow bool
	}{
		{name: "below", installed: "0.16.40", wantBelow: true},
		{name: "at", installed: mate.MinimumMateVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			seedInstalledBundle(t, home, tt.installed)
			manifestAndTarballServer(t)

			stdout := captureStdout(t, func() {
				if got := runMateCmd([]string{"status", "--json"}); got != 0 {
					t.Fatalf("runMateCmd(status --json) = %d, want 0", got)
				}
			})

			var got struct {
				Minimum    string `json:"minimum"`
				BelowFloor bool   `json:"belowFloor"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &got); err != nil {
				t.Fatalf("unmarshal status JSON %q: %v", stdout, err)
			}
			if got.Minimum != mate.MinimumMateVersion || got.BelowFloor != tt.wantBelow {
				t.Errorf("status = %+v, want minimum %s belowFloor %v", got, mate.MinimumMateVersion, tt.wantBelow)
			}
		})
	}
}
