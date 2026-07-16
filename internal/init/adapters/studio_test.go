package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/content"
)

// TestStudioExtVersion_ParityWithPackageJSON pins the P0 / R-DRIFT-LOCAL
// invariant: the Go const studioExtVersion equals the Studio extension's
// package.json "version". Two version surfaces that drift mean a new
// extension.js may not reload (the install dir / extensions.json entry are
// keyed on the const, the manifest VS Code reads on the package.json). The
// parity is the whole point of P0.
func TestStudioExtVersion_ParityWithPackageJSON(t *testing.T) {
	pkg, err := content.StudioPackageJSON()
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
		"package.json", "extension.js", "logo.svg",
		"lib/discoverToUIMap.js", "lib/cards.js", "lib/handlers.js",
		"cards/runtime.js", "cards/managed.js",
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
