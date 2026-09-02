// Tests for: EnsureInstalled and the versioned-prefix bundle lifecycle it
// owns — InstalledVersion, DesiredRelease, IsDevVersion, migration from the
// legacy flat layout, staging/activation atomicity, and pruning.
//
// NOT parallel — every path here is derived from HOME (see runtime.HomeDir),
// and every test redirects it plus the package-level download/npm/smoke
// seams (see export_test.go).
package z3_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/z3"
)

// ensureRig gives each test a private HOME and counts how many times
// EnsureInstalled reached the download/npm/smoke seams, so "no network at
// all" assertions are exact rather than inferred from side effects.
type ensureRig struct {
	home string

	downloadCalls int
	npmCalls      int
	smokeCalls    int

	npmErr   error
	smokeErr error
}

func newEnsureRig(t *testing.T) *ensureRig {
	t.Helper()
	rig := &ensureRig{home: t.TempDir()}
	t.Setenv("HOME", rig.home)

	z3.SetDownloadVerified(func(_ context.Context, _ *http.Client, _, _ string) (string, func(), error) {
		rig.downloadCalls++
		f, err := os.CreateTemp(t.TempDir(), "fake-release-*.tgz")
		if err != nil {
			t.Fatalf("create fake tarball: %v", err)
		}
		_ = f.Close()
		path := f.Name()
		return path, func() { _ = os.Remove(path) }, nil
	})
	z3.SetNpmInstallTarball(func(_ context.Context, prefix, _ string) error {
		rig.npmCalls++
		if rig.npmErr != nil {
			// A real partial npm install leaves SOMETHING behind before
			// failing — write a marker so the cleanup assertion is testing
			// something real, not vacuously true over an empty directory.
			_ = os.MkdirAll(prefix, 0o755)
			_ = os.WriteFile(filepath.Join(prefix, "PARTIAL"), []byte("partial npm install"), 0o644)
			return rig.npmErr
		}
		writeFakePackage(t, prefix, z3.PinnedVersion)
		return nil
	})
	z3.SetSmokeTestInstall(func(_ context.Context, _ string) error {
		rig.smokeCalls++
		return rig.smokeErr
	})

	t.Cleanup(func() {
		z3.ResetDownloadVerified()
		z3.ResetNpmInstallTarball()
		z3.ResetSmokeTestInstall()
	})
	return rig
}

// writeFakePackage lays down what a completed `npm install --prefix dir`
// leaves behind for PackageName at the given version: a package.json
// InstalledVersion can read, and an entry-point script at the same relative
// spot BinPath() expects. The script is never executed by these tests
// (smokeTestInstall is stubbed), so its content is a placeholder.
func writeFakePackage(t *testing.T, dir, version string) {
	t.Helper()
	pkgDir := filepath.Join(dir, "node_modules", z3.PackageName)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", pkgDir, err)
	}
	body := `{"name":"` + z3.PackageName + `","version":"` + version + `"}`
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	bin := filepath.Join(dir, "node_modules", ".bin", "z3")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(bin), err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho fake z3\n"), 0o700); err != nil {
		t.Fatalf("write bin: %v", err)
	}
}

// seedInstalledVersion lays down a complete versioned install for version
// and points z3.CurrentLink() at it, matching what a prior EnsureInstalled
// (or a real npm install) would have left on disk.
func seedInstalledVersion(t *testing.T, version string) {
	t.Helper()
	writeFakePackage(t, z3.VersionDir(version), version)
	linkCurrent(t, version)
}

// linkCurrent (re)creates z3.CurrentLink() pointing at versions/<version>,
// relative — the same shape EnsureInstalled's own activation produces.
func linkCurrent(t *testing.T, version string) {
	t.Helper()
	current := z3.CurrentLink()
	_ = os.Remove(current)
	if err := os.MkdirAll(filepath.Dir(current), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(current), err)
	}
	if err := os.Symlink(filepath.Join("versions", version), current); err != nil {
		t.Fatalf("symlink current -> versions/%s: %v", version, err)
	}
}

// seedVersionDirOnly creates an on-disk version directory (a package.json at
// some arbitrary version) WITHOUT linking it live — used to seed extra
// entries for the pruning tests. Returns the directory path.
func seedVersionDirOnly(t *testing.T, version string) string {
	t.Helper()
	dir := z3.VersionDir(version)
	writeFakePackage(t, dir, version)
	return dir
}

// seedLegacyFlatInstall lays down a PRE-versioning install: a bundle straight
// at Prefix(), with no CurrentLink() at all.
func seedLegacyFlatInstall(t *testing.T, version string) {
	t.Helper()
	writeFakePackage(t, z3.Prefix(), version)
}

// seedLegacyFlatInstallWithoutVersion is a pre-versioning install whose
// entry point is present but whose package.json is not — the case where the
// migration cannot name what it found.
func seedLegacyFlatInstallWithoutVersion(t *testing.T) {
	t.Helper()
	writeFakePackage(t, z3.Prefix(), "0.0.9")
	pkgJSON := filepath.Join(z3.Prefix(), "node_modules", z3.PackageName, "package.json")
	if err := os.Remove(pkgJSON); err != nil {
		t.Fatalf("remove %s: %v", pkgJSON, err)
	}
}

func TestEnsureInstalled_SameVersion_NoNetwork_ResultNone(t *testing.T) {
	rig := newEnsureRig(t)
	seedInstalledVersion(t, z3.PinnedVersion)

	result, err := z3.EnsureInstalled(z3.EnsureOptions{})
	if err != nil {
		t.Fatalf("EnsureInstalled(): %v", err)
	}
	want := z3.Result{Action: z3.ActionNone, From: z3.PinnedVersion, To: z3.PinnedVersion}
	if result != want {
		t.Errorf("EnsureInstalled() = %+v, want %+v", result, want)
	}
	if rig.downloadCalls != 0 || rig.npmCalls != 0 || rig.smokeCalls != 0 {
		t.Errorf("same version must reach no network at all: download=%d npm=%d smoke=%d",
			rig.downloadCalls, rig.npmCalls, rig.smokeCalls)
	}
}

func TestEnsureInstalled_DifferentVersion_InstallsAndRepointsCurrent(t *testing.T) {
	rig := newEnsureRig(t)
	seedInstalledVersion(t, "0.0.9")

	result, err := z3.EnsureInstalled(z3.EnsureOptions{})
	if err != nil {
		t.Fatalf("EnsureInstalled(): %v", err)
	}
	want := z3.Result{Action: z3.ActionUpdated, From: "0.0.9", To: z3.PinnedVersion}
	if result != want {
		t.Errorf("EnsureInstalled() = %+v, want %+v", result, want)
	}
	if rig.downloadCalls != 1 || rig.npmCalls != 1 || rig.smokeCalls != 1 {
		t.Errorf("expected exactly one download/npm/smoke, got download=%d npm=%d smoke=%d",
			rig.downloadCalls, rig.npmCalls, rig.smokeCalls)
	}

	got, err := z3.InstalledVersion()
	if err != nil {
		t.Fatalf("InstalledVersion() after update: %v", err)
	}
	if got != z3.PinnedVersion {
		t.Errorf("current now names %q, want %q", got, z3.PinnedVersion)
	}
	if _, err := os.Stat(filepath.Join(z3.VersionDir("0.0.9"), "node_modules", z3.PackageName, "package.json")); err != nil {
		t.Errorf("the old version must still be on disk: %v", err)
	}
}

func TestEnsureInstalled_NothingInstalled_ResultInstalled(t *testing.T) {
	rig := newEnsureRig(t)

	result, err := z3.EnsureInstalled(z3.EnsureOptions{})
	if err != nil {
		t.Fatalf("EnsureInstalled(): %v", err)
	}
	want := z3.Result{Action: z3.ActionInstalled, From: "", To: z3.PinnedVersion}
	if result != want {
		t.Errorf("EnsureInstalled() = %+v, want %+v", result, want)
	}
	if rig.downloadCalls != 1 || rig.npmCalls != 1 || rig.smokeCalls != 1 {
		t.Errorf("expected exactly one download/npm/smoke, got download=%d npm=%d smoke=%d",
			rig.downloadCalls, rig.npmCalls, rig.smokeCalls)
	}
}

// TestEnsureInstalled_NpmFailure_LeavesCurrentUnchanged is the requirement
// the whole staged-versions design exists for: a failed npm install must
// never touch the version that was working.
func TestEnsureInstalled_NpmFailure_LeavesCurrentUnchanged(t *testing.T) {
	rig := newEnsureRig(t)
	rig.npmErr = errors.New("registry hiccup mid-install")
	seedInstalledVersion(t, "0.0.9")

	result, err := z3.EnsureInstalled(z3.EnsureOptions{})
	if err == nil {
		t.Fatal("EnsureInstalled(): expected the npm failure to surface")
	}
	if result != (z3.Result{}) {
		t.Errorf("a failed EnsureInstalled must return a zero Result, got %+v", result)
	}

	got, verErr := z3.InstalledVersion()
	if verErr != nil {
		t.Fatalf("InstalledVersion() after a failed update: %v", verErr)
	}
	if got != "0.0.9" {
		t.Errorf("current must still name the working version, got %q", got)
	}
	if _, statErr := os.Stat(z3.VersionDir(z3.PinnedVersion)); !os.IsNotExist(statErr) {
		t.Errorf("the half-built version directory must be cleaned up, stat err=%v", statErr)
	}
}

// TestEnsureInstalled_SmokeFailure_LeavesCurrentUnchanged is the same
// guarantee as the npm-failure case, for a bundle that installs but does not
// run.
func TestEnsureInstalled_SmokeFailure_LeavesCurrentUnchanged(t *testing.T) {
	rig := newEnsureRig(t)
	rig.smokeErr = errors.New("z3 --version: exit status 1")
	seedInstalledVersion(t, "0.0.9")

	result, err := z3.EnsureInstalled(z3.EnsureOptions{})
	if err == nil {
		t.Fatal("EnsureInstalled(): expected the smoke failure to surface")
	}
	if result != (z3.Result{}) {
		t.Errorf("a failed EnsureInstalled must return a zero Result, got %+v", result)
	}

	got, verErr := z3.InstalledVersion()
	if verErr != nil {
		t.Fatalf("InstalledVersion() after a failed update: %v", verErr)
	}
	if got != "0.0.9" {
		t.Errorf("current must still name the working version, got %q", got)
	}
	if _, statErr := os.Stat(z3.VersionDir(z3.PinnedVersion)); !os.IsNotExist(statErr) {
		t.Errorf("the half-built version directory must be cleaned up, stat err=%v", statErr)
	}
	if rig.npmCalls != 1 {
		t.Errorf("npm must still have run once before the smoke test, got %d", rig.npmCalls)
	}
}

func TestEnsureInstalled_DevVersionInstalled_KeptWithoutForce(t *testing.T) {
	rig := newEnsureRig(t)
	const devVersion = "0.1.0-dev.a1b2c3"
	seedInstalledVersion(t, devVersion)

	result, err := z3.EnsureInstalled(z3.EnsureOptions{})
	if err != nil {
		t.Fatalf("EnsureInstalled(): %v", err)
	}
	want := z3.Result{Action: z3.ActionNone, From: devVersion, To: devVersion}
	if result != want {
		t.Errorf("EnsureInstalled() = %+v, want %+v", result, want)
	}
	if rig.downloadCalls != 0 || rig.npmCalls != 0 || rig.smokeCalls != 0 {
		t.Errorf("keeping a dev build must reach no network at all: download=%d npm=%d smoke=%d",
			rig.downloadCalls, rig.npmCalls, rig.smokeCalls)
	}
}

func TestEnsureInstalled_DevVersionInstalled_ReplacedWithForce(t *testing.T) {
	newEnsureRig(t)
	const devVersion = "0.1.0-dev.a1b2c3"
	seedInstalledVersion(t, devVersion)

	result, err := z3.EnsureInstalled(z3.EnsureOptions{Force: true})
	if err != nil {
		t.Fatalf("EnsureInstalled(Force): %v", err)
	}
	want := z3.Result{Action: z3.ActionUpdated, From: devVersion, To: z3.PinnedVersion}
	if result != want {
		t.Errorf("EnsureInstalled(Force) = %+v, want %+v", result, want)
	}
}

// TestEnsureInstalled_LegacyFlatLayout_MigratesAndConverges covers a
// container that installed z3 before versioned prefixes existed: a bundle
// straight at Prefix(), no CurrentLink() at all. The migration only moves
// the bundle, so the SAME pass goes on to reconcile the version — a legacy
// container that is also behind must not need a second restart to catch up.
func TestEnsureInstalled_LegacyFlatLayout_MigratesAndConverges(t *testing.T) {
	newEnsureRig(t)
	const legacyVersion = "0.0.5"
	seedLegacyFlatInstall(t, legacyVersion)

	result, err := z3.EnsureInstalled(z3.EnsureOptions{})
	if err != nil {
		t.Fatalf("EnsureInstalled(): %v", err)
	}
	want := z3.Result{Action: z3.ActionUpdated, From: legacyVersion, To: z3.PinnedVersion}
	if result != want {
		t.Errorf("EnsureInstalled() = %+v, want %+v", result, want)
	}

	got, err := z3.InstalledVersion()
	if err != nil {
		t.Fatalf("InstalledVersion() after migration: %v", err)
	}
	if got != z3.PinnedVersion {
		t.Errorf("current names %q, want the pinned %q", got, z3.PinnedVersion)
	}
	if _, err := os.Stat(filepath.Join(z3.Prefix(), "node_modules")); !os.IsNotExist(err) {
		t.Errorf("the flat node_modules must be moved, not copied, stat err=%v", err)
	}
	if _, err := os.Stat(z3.VersionDir(legacyVersion)); err != nil {
		t.Errorf("the migrated legacy version must survive as a rollback target: %v", err)
	}
}

// TestEnsureInstalled_LegacyFlatLayout_AlreadyPinned_NoNetwork is the same
// migration when the flat install is ALREADY the pinned version: the layout
// changes, nothing is fetched, and the result names the migration rather
// than reporting "nothing happened".
func TestEnsureInstalled_LegacyFlatLayout_AlreadyPinned_NoNetwork(t *testing.T) {
	rig := newEnsureRig(t)
	seedLegacyFlatInstall(t, z3.PinnedVersion)

	result, err := z3.EnsureInstalled(z3.EnsureOptions{})
	if err != nil {
		t.Fatalf("EnsureInstalled(): %v", err)
	}
	want := z3.Result{Action: z3.ActionMigrated, From: z3.PinnedVersion, To: z3.PinnedVersion}
	if result != want {
		t.Errorf("EnsureInstalled() = %+v, want %+v", result, want)
	}
	if rig.downloadCalls != 0 || rig.npmCalls != 0 || rig.smokeCalls != 0 {
		t.Errorf("migrating an already-pinned install must reach no network at all: download=%d npm=%d smoke=%d",
			rig.downloadCalls, rig.npmCalls, rig.smokeCalls)
	}
	if _, err := os.Stat(filepath.Join(z3.Prefix(), "node_modules")); !os.IsNotExist(err) {
		t.Errorf("the flat node_modules must be moved, not copied, stat err=%v", err)
	}
}

// TestEnsureInstalled_LegacyUnreadableVersion_ConvergesAndKeepsTheOld covers
// a legacy flat install whose package.json cannot be read: the version is
// unknown, so the migration parks it under a placeholder name.
//
// Such an install IS converged to the pin rather than protected. The dev-build
// guard deliberately does not cover it — that guard is behind a SUCCESSFUL
// version read, because "I could not read the version" is not evidence that a
// dev build is there. Protecting it would strand the container on an unknown
// version forever, silently, needing a --force nobody knows to run. Nothing is
// destroyed either way: the old tree survives under its placeholder directory
// as a rollback target.
func TestEnsureInstalled_LegacyUnreadableVersion_ConvergesAndKeepsTheOld(t *testing.T) {
	rig := newEnsureRig(t)
	seedLegacyFlatInstallWithoutVersion(t)

	result, err := z3.EnsureInstalled(z3.EnsureOptions{})
	if err != nil {
		t.Fatalf("EnsureInstalled(): %v", err)
	}
	want := z3.Result{Action: z3.ActionInstalled, From: "", To: z3.PinnedVersion}
	if result != want {
		t.Errorf("EnsureInstalled() = %+v, want %+v", result, want)
	}
	if rig.downloadCalls != 1 {
		t.Errorf("an unreadable legacy install must converge to the pin, downloads=%d", rig.downloadCalls)
	}

	got, err := z3.InstalledVersion()
	if err != nil {
		t.Fatalf("InstalledVersion(): %v", err)
	}
	if got != z3.PinnedVersion {
		t.Errorf("current names %q, want the pinned %q", got, z3.PinnedVersion)
	}

	// The unknown-provenance tree is parked, not deleted — a rollback target.
	entries, err := os.ReadDir(z3.VersionsDir())
	if err != nil {
		t.Fatalf("read versions dir: %v", err)
	}
	var parked bool
	for _, e := range entries {
		if e.Name() != z3.PinnedVersion {
			parked = true
			// A placeholder must not read as a semver prerelease, or a later
			// reader would take it for a dev build worth protecting.
			if z3.IsDevVersion(e.Name()) {
				t.Errorf("placeholder %q reads as a dev version", e.Name())
			}
		}
	}
	if !parked {
		t.Error("the migrated legacy tree must survive as a rollback target")
	}
}

// TestEnsureInstalled_Pruning_KeepsTwoAndTheLiveVersion seeds several stale,
// unlinked version directories alongside the one currently live, then runs
// an update. Only the version just activated (always kept — see
// EnsureInstalled's doc comment) and the single most-recently-modified other
// entry survive.
func TestEnsureInstalled_Pruning_KeepsTwoAndTheLiveVersion(t *testing.T) {
	newEnsureRig(t)
	seedInstalledVersion(t, "0.0.5") // becomes the "From" version, itself prunable once superseded

	middle := seedVersionDirOnly(t, "0.0.3")
	newest := seedVersionDirOnly(t, "0.0.4")
	now := time.Now()
	// The previously-live version was seeded first, so it would otherwise
	// carry the newest mtime among the stale entries — backdate all three so
	// "0.0.4" is unambiguously the single newest non-live entry.
	if err := os.Chtimes(z3.VersionDir("0.0.5"), now.Add(-3*time.Hour), now.Add(-3*time.Hour)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := os.Chtimes(middle, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := os.Chtimes(newest, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	result, err := z3.EnsureInstalled(z3.EnsureOptions{})
	if err != nil {
		t.Fatalf("EnsureInstalled(): %v", err)
	}
	if result.Action != z3.ActionUpdated {
		t.Fatalf("expected ActionUpdated, got %+v", result)
	}

	entries, err := os.ReadDir(z3.VersionsDir())
	if err != nil {
		t.Fatalf("read versions dir: %v", err)
	}
	kept := make([]string, 0, len(entries))
	for _, e := range entries {
		kept = append(kept, e.Name())
	}
	wantKept := map[string]bool{z3.PinnedVersion: true, "0.0.4": true}
	if len(kept) != len(wantKept) {
		t.Fatalf("kept versions = %v, want exactly %v", kept, wantKept)
	}
	for _, k := range kept {
		if !wantKept[k] {
			t.Errorf("unexpected surviving version %q, want only %v", k, wantKept)
		}
	}
	for _, removed := range []string{"0.0.3", "0.0.5"} {
		if _, err := os.Stat(z3.VersionDir(removed)); !os.IsNotExist(err) {
			t.Errorf("version %q must have been pruned, stat err=%v", removed, err)
		}
	}
}

func TestInstalledVersion_ThroughCurrentLink(t *testing.T) {
	newEnsureRig(t)
	seedInstalledVersion(t, "1.2.3")

	got, err := z3.InstalledVersion()
	if err != nil {
		t.Fatalf("InstalledVersion(): %v", err)
	}
	if got != "1.2.3" {
		t.Errorf("InstalledVersion() = %q, want %q", got, "1.2.3")
	}
}

func TestInstalledVersion_NoCurrentLink_Errors(t *testing.T) {
	newEnsureRig(t)

	if _, err := z3.InstalledVersion(); err == nil {
		t.Error("InstalledVersion(): expected an error with no current link")
	}
}

func TestInstalledVersion_UnparsablePackageJSON_Errors(t *testing.T) {
	newEnsureRig(t)
	dir := z3.VersionDir("broken")
	pkgDir := filepath.Join(dir, "node_modules", z3.PackageName)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	linkCurrent(t, "broken")

	if _, err := z3.InstalledVersion(); err == nil {
		t.Error("InstalledVersion(): expected an error for unparsable package.json")
	}
}

func TestInstalledVersion_EmptyVersionField_Errors(t *testing.T) {
	newEnsureRig(t)
	dir := z3.VersionDir("empty")
	pkgDir := filepath.Join(dir, "node_modules", z3.PackageName)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"zerops-code","version":""}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	linkCurrent(t, "empty")

	if _, err := z3.InstalledVersion(); err == nil {
		t.Error("InstalledVersion(): expected an error for an empty version field")
	}
}

func TestBinPath_ResolvesThroughCurrentLink(t *testing.T) {
	newEnsureRig(t)
	seedInstalledVersion(t, "1.2.3")

	resolved, err := filepath.EvalSymlinks(z3.BinPath())
	if err != nil {
		t.Fatalf("resolve BinPath(): %v", err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(z3.VersionDir("1.2.3"), "node_modules", ".bin", "z3"))
	if err != nil {
		t.Fatalf("resolve expected path: %v", err)
	}
	if resolved != want {
		t.Errorf("BinPath() resolves to %q, want %q", resolved, want)
	}
}

func TestDesiredRelease_IsTheCompiledPin(t *testing.T) {
	got := z3.DesiredRelease()
	want := z3.Release{Version: z3.PinnedVersion, URL: z3.ReleaseURL, SHA256: z3.PinnedSHA256}
	if got != want {
		t.Errorf("DesiredRelease() = %+v, want %+v", got, want)
	}
}

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
			if got := z3.IsDevVersion(tt.version); got != tt.want {
				t.Errorf("IsDevVersion(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}
