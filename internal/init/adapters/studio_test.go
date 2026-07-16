package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/extension"
)

// TestStudioExtVersion_ParityWithPackageJSON pins the P0 / R-DRIFT-LOCAL
// invariant: the Go const studioExtVersion equals the Studio extension's
// package.json "version". Two version surfaces that drift mean a new
// extension.js may not reload (the install dir / extensions.json entry are
// keyed on the const, the manifest VS Code reads on the package.json). The
// parity is the whole point of P0.
func TestStudioExtVersion_ParityWithPackageJSON(t *testing.T) {
	pkg, err := extension.StudioPackageJSON()
	if err != nil {
		t.Fatalf("read studio package.json: %v", err)
	}
	var m struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(pkg), &m); err != nil {
		t.Fatalf("parse studio package.json: %v", err)
	}
	if m.Version != studioExtVersion {
		t.Errorf("version drift: studioExtVersion=%q but package.json version=%q — keep them equal (P0/R-DRIFT-LOCAL)", studioExtVersion, m.Version)
	}
}

// TestInstallStudioExtension_MaterializesAndRegisters proves the install path
// writes the extension tree into the desktop dir and registers a valid entry in
// the profile manifest.
func TestInstallStudioExtension_MaterializesAndRegisters(t *testing.T) {
	home := t.TempDir()
	if err := InstallStudioExtension(home); err != nil {
		t.Fatalf("install: %v", err)
	}

	extDir := filepath.Join(home, ".vscode", "extensions", studioExtDirName())
	for _, rel := range []string{
		"package.json", "extension.js", "logo.svg", "media/data.svg",
		"lib/discoverToUIMap.js", "lib/cards.js", "lib/handlers.js",
		"cards/managed.js",
	} {
		if _, err := os.Stat(filepath.Join(extDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected installed file %s: %v", rel, err)
		}
	}

	// No test artifacts shipped (the cards/ enumerator scans cards/*.js — a
	// stray *.test.js would try to register as a card).
	walkTestArtifacts(t, extDir)

	// extensions.json carries exactly our entry, well-formed.
	entries := readIndex(t, home)
	if got := countID(entries, studioExtID); got != 1 {
		t.Fatalf("want exactly 1 %s entry, got %d", studioExtID, got)
	}
	e := findID(entries, studioExtID)
	if e["relativeLocation"] != studioExtDirName() {
		t.Errorf("relativeLocation=%v, want %s", e["relativeLocation"], studioExtDirName())
	}
	if e["version"] != studioExtVersion {
		t.Errorf("index version=%v, want %s", e["version"], studioExtVersion)
	}
}

// TestInstallStudioExtension_Idempotent proves re-runs and version churn leave a
// single folder + single manifest entry (no orphans, no duplicates).
func TestInstallStudioExtension_Idempotent(t *testing.T) {
	home := t.TempDir()
	if err := InstallStudioExtension(home); err != nil {
		t.Fatalf("install 1: %v", err)
	}
	if err := InstallStudioExtension(home); err != nil {
		t.Fatalf("install 2: %v", err)
	}

	dirs, _ := filepath.Glob(filepath.Join(home, ".vscode", "extensions", studioExtID+"-*"))
	if len(dirs) != 1 {
		t.Errorf("want 1 studio version dir, got %d: %v", len(dirs), dirs)
	}
	if got := countID(readIndex(t, home), studioExtID); got != 1 {
		t.Errorf("want 1 manifest entry after re-install, got %d", got)
	}
}

// TestInstallStudioExtension_PreservesOtherExtensions is the critical guardrail:
// registering our entry must never disturb another extension's entry (we keep
// existing entries byte-for-byte).
func TestInstallStudioExtension_PreservesOtherExtensions(t *testing.T) {
	home := t.TempDir()
	extRoot := filepath.Join(home, ".vscode", "extensions")
	if err := os.MkdirAll(extRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `[{"identifier":{"id":"ms-python.python"},"version":"2024.1.0","relativeLocation":"ms-python.python-2024.1.0","location":{"$mid":1,"path":"/x/ms-python.python-2024.1.0","scheme":"file"}}]`
	if err := os.WriteFile(filepath.Join(extRoot, "extensions.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallStudioExtension(home); err != nil {
		t.Fatalf("install: %v", err)
	}

	entries := readIndex(t, home)
	if findID(entries, "ms-python.python") == nil {
		t.Error("pre-existing extension was dropped from extensions.json")
	}
	if findID(entries, studioExtID) == nil {
		t.Error("studio entry not added")
	}
}

// TestInstallStudioExtension_InvalidManifest_RefusesOverwrite proves a
// pre-existing corrupt extensions.json (the whole profile is already broken) is
// left untouched — we never own that blast radius — while the folder still
// installs and the call stays non-fatal.
func TestInstallStudioExtension_InvalidManifest_RefusesOverwrite(t *testing.T) {
	home := t.TempDir()
	extRoot := filepath.Join(home, ".vscode", "extensions")
	if err := os.MkdirAll(extRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := "{ this is not a json array"
	idx := filepath.Join(extRoot, "extensions.json")
	if err := os.WriteFile(idx, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallStudioExtension(home); err != nil {
		t.Fatalf("registration failure must be non-fatal (folder install is the gate): %v", err)
	}

	// Folder installed despite the manifest problem.
	if _, err := os.Stat(filepath.Join(extRoot, studioExtDirName(), "package.json")); err != nil {
		t.Errorf("extension folder should install regardless of manifest state: %v", err)
	}
	// Corrupt manifest left exactly as-is.
	got, _ := os.ReadFile(idx)
	if string(got) != bad {
		t.Errorf("invalid manifest was modified; it must be left untouched.\n got: %q", string(got))
	}
}

// --- container installer (code-server) ------------------------------------

// containerExtRoot is the code-server extensions dir the in-container installer
// targets — distinct from the desktop ~/.vscode/extensions the local one uses.
func containerExtRoot(home string) string {
	return filepath.Join(home, ".local", "share", "code-server", "extensions")
}

// TestInstallStudioExtensionContainer_MaterializesAndRegisters proves the
// in-container install path writes the extension tree into the CODE-SERVER
// extensions dir (not the desktop dir) and registers a valid entry in the
// code-server index using the code-server entry shape.
func TestInstallStudioExtensionContainer_MaterializesAndRegisters(t *testing.T) {
	home := t.TempDir()
	if err := InstallStudioExtensionContainer(home); err != nil {
		t.Fatalf("install: %v", err)
	}

	extDir := filepath.Join(containerExtRoot(home), studioExtDirName())
	for _, rel := range []string{
		"package.json", "extension.js", "logo.svg", "media/data.svg",
		"lib/discoverToUIMap.js", "lib/cards.js", "lib/handlers.js",
		"cards/managed.js",
	} {
		if _, err := os.Stat(filepath.Join(extDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected installed file %s: %v", rel, err)
		}
	}

	// The desktop dir is never touched by the container installer.
	if _, err := os.Stat(filepath.Join(home, ".vscode")); !os.IsNotExist(err) {
		t.Errorf("container installer must not create ~/.vscode (got err=%v)", err)
	}

	// No test artifacts shipped.
	walkTestArtifacts(t, extDir)

	// Index carries exactly our entry, in the CODE-SERVER shape.
	entries := readContainerIndex(t, home)
	if got := countID(entries, studioExtID); got != 1 {
		t.Fatalf("want exactly 1 %s entry, got %d", studioExtID, got)
	}
	e := findID(entries, studioExtID)
	if e["relativeLocation"] != studioExtDirName() {
		t.Errorf("relativeLocation=%v, want %s", e["relativeLocation"], studioExtDirName())
	}
	if e["version"] != studioExtVersion {
		t.Errorf("index version=%v, want %s", e["version"], studioExtVersion)
	}
	assertCodeServerEntryShape(t, e, extDir)
}

// TestInstallStudioExtensionContainer_Idempotent proves re-runs and version
// churn leave a single folder + single index entry and preserve the entry's
// installedTimestamp (so repeat container provisioning never churns it).
func TestInstallStudioExtensionContainer_Idempotent(t *testing.T) {
	home := t.TempDir()
	if err := InstallStudioExtensionContainer(home); err != nil {
		t.Fatalf("install 1: %v", err)
	}
	firstTS := studioTimestamp(t, readContainerIndex(t, home))
	if firstTS == 0 {
		t.Fatal("first run did not record installedTimestamp for studio")
	}

	if err := InstallStudioExtensionContainer(home); err != nil {
		t.Fatalf("install 2: %v", err)
	}

	dirs, _ := filepath.Glob(filepath.Join(containerExtRoot(home), studioExtID+"-*"))
	if len(dirs) != 1 {
		t.Errorf("want 1 studio version dir, got %d: %v", len(dirs), dirs)
	}
	entries := readContainerIndex(t, home)
	if got := countID(entries, studioExtID); got != 1 {
		t.Errorf("want 1 index entry after re-install, got %d", got)
	}
	if secondTS := studioTimestamp(t, entries); secondTS != firstTS {
		t.Errorf("installedTimestamp churned across re-runs: first=%d second=%d", firstTS, secondTS)
	}
}

// TestInstallStudioExtensionContainer_PreservesOtherExtensions proves
// registering our entry never disturbs another extension's entry — unknown
// fields on entries we did not author (e.g. code-server's --install-extension
// metadata) round-trip untouched.
func TestInstallStudioExtensionContainer_PreservesOtherExtensions(t *testing.T) {
	home := t.TempDir()
	extRoot := containerExtRoot(home)
	if err := os.MkdirAll(extRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `[{"identifier":{"id":"anthropic.claude-code"},"version":"2.1.120","relativeLocation":"anthropic.claude-code-2.1.120","metadata":{"installedTimestamp":1777170768624,"pinned":true,"source":"vsix","customField":"keep-me"}}]`
	if err := os.WriteFile(filepath.Join(extRoot, "extensions.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallStudioExtensionContainer(home); err != nil {
		t.Fatalf("install: %v", err)
	}

	entries := readContainerIndex(t, home)
	claude := findID(entries, "anthropic.claude-code")
	if claude == nil {
		t.Fatal("pre-existing extension was dropped from extensions.json")
	}
	md, ok := claude["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("claude-code metadata missing/malformed: %v", claude["metadata"])
	}
	if got, _ := md["customField"].(string); got != "keep-me" {
		t.Errorf("claude-code metadata.customField lost: got %q want keep-me", got)
	}
	if ts, _ := md["installedTimestamp"].(float64); int64(ts) != 1777170768624 {
		t.Errorf("claude-code installedTimestamp churned: got %v", md["installedTimestamp"])
	}
	if findID(entries, studioExtID) == nil {
		t.Error("studio entry not added")
	}
}

// TestInstallStudioExtensionContainer_InvalidManifest_RefusesOverwrite proves a
// pre-existing corrupt code-server index is left byte-for-byte untouched (we
// never own that blast radius) while the folder still installs and the call
// stays non-fatal.
func TestInstallStudioExtensionContainer_InvalidManifest_RefusesOverwrite(t *testing.T) {
	home := t.TempDir()
	extRoot := containerExtRoot(home)
	if err := os.MkdirAll(extRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := "{ this is not a json array"
	idx := filepath.Join(extRoot, "extensions.json")
	if err := os.WriteFile(idx, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallStudioExtensionContainer(home); err != nil {
		t.Fatalf("registration failure must be non-fatal (folder install is the gate): %v", err)
	}

	// Folder installed despite the manifest problem.
	if _, err := os.Stat(filepath.Join(extRoot, studioExtDirName(), "package.json")); err != nil {
		t.Errorf("extension folder should install regardless of manifest state: %v", err)
	}
	// Corrupt manifest left exactly as-is.
	got, _ := os.ReadFile(idx)
	if string(got) != bad {
		t.Errorf("invalid manifest was modified; it must be left untouched.\n got: %q", string(got))
	}
}

// --- helpers ---------------------------------------------------------------

func readIndex(t *testing.T, home string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, ".vscode", "extensions", "extensions.json"))
	if err != nil {
		t.Fatalf("read extensions.json: %v", err)
	}
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("parse extensions.json: %v\n%s", err, raw)
	}
	return entries
}

func readContainerIndex(t *testing.T, home string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(containerExtRoot(home), "extensions.json"))
	if err != nil {
		t.Fatalf("read code-server extensions.json: %v", err)
	}
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("parse code-server extensions.json: %v\n%s", err, raw)
	}
	return entries
}

// assertCodeServerEntryShape verifies an entry carries the code-server location
// shape (location.{fsPath,external,path,scheme,$mid} + metadata.installedTimestamp)
// — distinct from the leaner desktop shape (location.{$mid,path,scheme}, no
// metadata) InstallStudioExtension writes.
func assertCodeServerEntryShape(t *testing.T, e map[string]any, wantDir string) {
	t.Helper()
	loc, ok := e["location"].(map[string]any)
	if !ok {
		t.Fatalf("entry.location missing/malformed: %v", e["location"])
	}
	for _, k := range []string{"$mid", "fsPath", "external", "path", "scheme"} {
		if _, present := loc[k]; !present {
			t.Errorf("code-server location missing key %q; got %v", k, loc)
		}
	}
	if got, _ := loc["fsPath"].(string); got != wantDir {
		t.Errorf("location.fsPath=%q, want %q", got, wantDir)
	}
	if got, _ := loc["external"].(string); got != "file://"+wantDir {
		t.Errorf("location.external=%q, want %q", got, "file://"+wantDir)
	}
	md, ok := e["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("code-server entry missing metadata object: %v", e["metadata"])
	}
	if _, present := md["installedTimestamp"]; !present {
		t.Errorf("code-server metadata missing installedTimestamp; got %v", md)
	}
}

// studioTimestamp returns the studio entry's metadata.installedTimestamp (0 if
// absent).
func studioTimestamp(t *testing.T, entries []map[string]any) int64 {
	t.Helper()
	e := findID(entries, studioExtID)
	if e == nil {
		return 0
	}
	md, ok := e["metadata"].(map[string]any)
	if !ok {
		return 0
	}
	ts, _ := md["installedTimestamp"].(float64)
	return int64(ts)
}

func entryID(e map[string]any) string {
	id, _ := e["identifier"].(map[string]any)
	if id == nil {
		return ""
	}
	s, _ := id["id"].(string)
	return s
}

func countID(entries []map[string]any, id string) int {
	n := 0
	for _, e := range entries {
		if entryID(e) == id {
			n++
		}
	}
	return n
}

func findID(entries []map[string]any, id string) map[string]any {
	for _, e := range entries {
		if entryID(e) == id {
			return e
		}
	}
	return nil
}

func walkTestArtifacts(t *testing.T, root string) {
	t.Helper()
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(p) == ".js" && len(p) > len(".test.js") && p[len(p)-len(".test.js"):] == ".test.js" {
			t.Errorf("test artifact shipped into extension: %s", p)
		}
		return nil
	})
}
