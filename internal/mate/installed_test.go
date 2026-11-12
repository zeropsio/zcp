// Tests for: the versioned prefix the start job keeps — InstalledVersion and
// BinPath through the current link, IsDevVersion, pruning old version
// directories, and StageRelease never replacing what last-good names.
//
// NOT parallel — every path here is derived from HOME (see runtime.HomeDir).
package mate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
)

// writeFakePackage lays down what a completed `npm install --prefix dir`
// leaves behind for PackageName at the given version: a package.json
// InstalledVersion can read, and an entry-point script at the spot BinPath()
// expects. withBin false leaves the entry point out, so a smoke test of the
// directory fails without starting a process.
func writeFakePackage(t *testing.T, dir, version string, withBin bool) {
	t.Helper()
	pkgDir := filepath.Join(dir, "node_modules", mate.PackageName)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", pkgDir, err)
	}
	body := `{"name":"` + mate.PackageName + `","version":"` + version + `"}`
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	if !withBin {
		return
	}
	bin := filepath.Join(dir, "node_modules", ".bin", mate.BinName)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(bin), err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho fake mate\n"), 0o700); err != nil {
		t.Fatalf("write bin: %v", err)
	}
}

// linkVersion (re)creates link pointing at versions/<version>, relative — the
// shape the start job's own links have.
func linkVersion(t *testing.T, link, version string) {
	t.Helper()
	_ = os.Remove(link)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(link), err)
	}
	if err := os.Symlink(filepath.Join("versions", version), link); err != nil {
		t.Fatalf("symlink %s -> versions/%s: %v", link, version, err)
	}
}

// seedInstalledVersion lays down a complete versioned install for version and
// points current at it.
func seedInstalledVersion(t *testing.T, version string) {
	t.Helper()
	writeFakePackage(t, mate.VersionDir(version), version, true)
	linkVersion(t, mate.CurrentLink(), version)
}

// TestInstalledVersion_ThroughCurrentLink: the version is npm's own record in
// the directory current names.
// witness: internal/mate/mate.go | func InstalledVersion() (string, error) { return installedVersionIn(CurrentLink()) } → func InstalledVersion() (string, error) { return installedVersionIn(VersionsDir()) }
func TestInstalledVersion_ThroughCurrentLink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seedInstalledVersion(t, "1.2.3")

	got, err := mate.InstalledVersion()
	if err != nil {
		t.Fatalf("InstalledVersion(): %v", err)
	}
	if got != "1.2.3" {
		t.Errorf("InstalledVersion() = %q, want %q", got, "1.2.3")
	}
}

// TestInstalledVersion_NoCurrentLink_Errors: nothing linked is nothing
// installed, never a version.
// witness: internal/mate/mate.go | 		return pkg, path, fmt.Errorf("read %s: %w", path, err) → 		return installedPackage{Version: MinimumMateVersion}, path, nil
func TestInstalledVersion_NoCurrentLink_Errors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := mate.InstalledVersion(); err == nil {
		t.Error("InstalledVersion(): expected an error with no current link")
	}
}

// TestInstalledVersion_UnparsablePackageJSON_Errors: a damaged record is no
// usable install.
// witness: internal/mate/mate.go | 		return pkg, path, fmt.Errorf("parse %s: %w", path, err) → 		return installedPackage{Version: MinimumMateVersion}, path, nil
func TestInstalledVersion_UnparsablePackageJSON_Errors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pkgDir := filepath.Join(mate.VersionDir("broken"), "node_modules", mate.PackageName)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	linkVersion(t, mate.CurrentLink(), "broken")

	if _, err := mate.InstalledVersion(); err == nil {
		t.Error("InstalledVersion(): expected an error for unparsable package.json")
	}
}

// TestInstalledVersion_EmptyVersionField_Errors: a record without a version
// is no usable install.
// witness: internal/mate/mate.go | 	if pkg.Version == "" { → 	if pkg.Version == "never" {
func TestInstalledVersion_EmptyVersionField_Errors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pkgDir := filepath.Join(mate.VersionDir("empty"), "node_modules", mate.PackageName)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"zerops-mate","version":""}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	linkVersion(t, mate.CurrentLink(), "empty")

	if _, err := mate.InstalledVersion(); err == nil {
		t.Error("InstalledVersion(): expected an error for an empty version field")
	}
}

// TestBinPath_ResolvesThroughCurrentLink: the unit runs whichever version
// current names.
// witness: internal/mate/mate.go | func BinPath() string { return binIn(CurrentLink()) } → func BinPath() string { return binIn(Prefix()) }
func TestBinPath_ResolvesThroughCurrentLink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seedInstalledVersion(t, "1.2.3")

	resolved, err := filepath.EvalSymlinks(mate.BinPath())
	if err != nil {
		t.Fatalf("resolve BinPath(): %v", err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(mate.VersionDir("1.2.3"), "node_modules", ".bin", mate.BinName))
	if err != nil {
		t.Fatalf("resolve expected path: %v", err)
	}
	if resolved != want {
		t.Errorf("BinPath() resolves to %q, want %q", resolved, want)
	}
}

// TestIsDevVersion: any prerelease is a dev build, which the version to run
// keeps and last-good never names.
// witness: internal/mate/mate.go | func IsDevVersion(v string) bool { return strings.Contains(v, "-") } → func IsDevVersion(v string) bool { return strings.Contains(v, "-dev") }
func TestIsDevVersion(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"0.1.0", false},
		{"1.2.3", false},
		{"0.1.0-dev.a1b2c3", true},
		{"1.0.0-rc.1", true},
		{"legacy-pre-versioning", true}, // any "-" reads as a prerelease, by design
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := mate.IsDevVersion(tt.version); got != tt.want {
				t.Errorf("IsDevVersion(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

// TestEnsureInstalled_Pruning_KeepsTwoAndTheLiveVersion: after a version came
// up, only it and the single most recently modified other directory stay.
// witness: internal/mate/mate.go | 		if len(keep) >= 2 { → 		if len(keep) >= 3 {
func TestEnsureInstalled_Pruning_KeepsTwoAndTheLiveVersion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seedInstalledVersion(t, "0.0.6") // came up: the live version
	now := time.Now()
	for i, v := range []string{"0.0.5", "0.0.3", "0.0.4"} {
		writeFakePackage(t, mate.VersionDir(v), v, true)
		// 0.0.4 is unambiguously the newest of the others.
		age := map[string]time.Duration{"0.0.5": 3 * time.Hour, "0.0.3": 2 * time.Hour, "0.0.4": time.Hour}[v]
		if err := os.Chtimes(mate.VersionDir(v), now.Add(-age), now.Add(-age)); err != nil {
			t.Fatalf("chtimes %d: %v", i, err)
		}
	}
	if err := os.Chtimes(mate.VersionDir("0.0.6"), now.Add(-4*time.Hour), now.Add(-4*time.Hour)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	mate.PruneOldVersions("0.0.6")

	entries, err := os.ReadDir(mate.VersionsDir())
	if err != nil {
		t.Fatalf("read versions dir: %v", err)
	}
	kept := make([]string, 0, len(entries))
	for _, e := range entries {
		kept = append(kept, e.Name())
	}
	if strings.Join(kept, " ") != "0.0.4 0.0.6" {
		t.Errorf("kept versions = %v, want [0.0.4 0.0.6]", kept)
	}
}

// TestStageRelease_LastGoodCandidate_PreservesVerifiedInstall: the directory
// last-good names is never deleted to download it again, even when its smoke
// test fails.
// witness: internal/mate/mate.go | 	for _, link := range []string{CurrentLink(), LastGoodLink()} { → 	for _, link := range []string{CurrentLink()} {
func TestStageRelease_LastGoodCandidate_PreservesVerifiedInstall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	seedInstalledVersion(t, "0.16.203")
	const lastGood = "0.16.180"
	writeFakePackage(t, mate.VersionDir(lastGood), lastGood, false)
	linkVersion(t, mate.LastGoodLink(), lastGood)

	err := mate.StageRelease(context.Background(), mate.Manifest{Version: lastGood, URL: "https://127.0.0.1:1/mate.tgz"}, mate.ReplaceFailing)

	if err == nil || !strings.Contains(err.Error(), "not replaced") {
		t.Fatalf("StageRelease() = %v, want a refusal to replace what last-good names", err)
	}
	if got, err := os.ReadFile(filepath.Join(mate.VersionDir(lastGood), "node_modules", mate.PackageName, "package.json")); err != nil || !strings.Contains(string(got), lastGood) {
		t.Errorf("last-good's directory was replaced: %q, %v", got, err)
	}
}
