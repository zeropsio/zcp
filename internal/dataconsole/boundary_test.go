package dataconsole

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestDataConsoleBoundary_CoreIsolated pins the extraction boundary: the
// console/ engine subtree imports NO zcp core package — only stdlib, 3rd-party
// drivers, and its own subtree. Anything core it needs comes through the
// console.Host seam implemented by zcpadapter. A violation here means the lift
// to a standalone repo would be a rewrite, not a `git mv`. Mirrors the depguard
// `dataconsole-core-isolated` rule (this test covers the call/import set the
// lint also enforces, so a config drift can't silently open the boundary).
func TestDataConsoleBoundary_CoreIsolated(t *testing.T) {
	t.Parallel()
	const (
		corePrefix = "github.com/zeropsio/zcp/internal/"
		ownPrefix  = "github.com/zeropsio/zcp/internal/dataconsole/console"
	)
	fset := token.NewFileSet()
	err := filepath.WalkDir("console", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(p, corePrefix) && !strings.HasPrefix(p, ownPrefix) {
				t.Errorf("%s imports %q — console/ must import zero zcp core (only its own subtree); route through zcpadapter", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk console/: %v", err)
	}
}

func TestDataConsoleBoundary_AdapterImportsAllowlistedOnly(t *testing.T) {
	t.Parallel()
	const internalPrefix = "github.com/zeropsio/zcp/internal/"
	allow := map[string]bool{
		"github.com/zeropsio/zcp/internal/auth":                         true,
		"github.com/zeropsio/zcp/internal/dataconsole/console":          true,
		"github.com/zeropsio/zcp/internal/dataconsole/console/provider": true,
		"github.com/zeropsio/zcp/internal/ops":                          true,
		"github.com/zeropsio/zcp/internal/platform":                     true,
		"github.com/zeropsio/zcp/internal/topology":                     true,
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir("zcpadapter", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(p, internalPrefix) && !allow[p] {
				t.Errorf("%s imports %q — not on the zcpadapter allowlist; adding a bridge edge is a deliberate boundary change", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk zcpadapter/: %v", err)
	}
}

// TestDataConsoleBoundary_CoreDoesNotImportSubsystem pins the OUTER island: no
// core package imports the Data Console subsystem (internal/dataconsole/...)
// except a handful of enumerated composition points. This is what lets the whole
// subsystem lift to its own repo without unpicking core — the two tests above
// pin the seam INSIDE the subsystem; this one pins the seam AROUND it. Mirrors
// the depguard `core-not-dataconsole` rule (this test covers the same import set
// the lint enforces, so a config drift can't silently open the boundary).
func TestDataConsoleBoundary_CoreDoesNotImportSubsystem(t *testing.T) {
	t.Parallel()
	const subsystemPrefix = "github.com/zeropsio/zcp/internal/dataconsole"

	// Composition points: the ONLY sites outside internal/dataconsole/ allowed to
	// import the subsystem (repo-root-relative, forward-slashed). Kept in lockstep
	// with the depguard `core-not-dataconsole` negations.
	allowedFile := map[string]bool{
		"cmd/zcp/studio.go":                     true,
		"cmd/zcp/studio_console.go":             true,
		"internal/init/adapters/studio.go":      true,
		"internal/init/adapters/studio_test.go": true, // version-parity test reads the embedded package.json
		"internal/init/vscode.go":               true,
	}
	// Whole subtrees exempt: the subsystem's own files, the dcseed seed CLI,
	// and the dclive live-lane config generator (S6: deliberately extended in
	// lockstep with the depguard core-not-dataconsole negations below).
	allowedPrefix := []string{
		"internal/dataconsole/",
		"cmd/dcseed/",
		"cmd/dclive/",
	}

	repoRoot := filepath.Join("..", "..")
	fset := token.NewFileSet()
	for _, root := range []string{"internal", "cmd"} {
		walkErr := filepath.WalkDir(filepath.Join(repoRoot, root), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			rel, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			if allowedFile[rel] {
				return nil
			}
			for _, p := range allowedPrefix {
				if strings.HasPrefix(rel, p) {
					return nil
				}
			}
			f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if perr != nil {
				return perr
			}
			for _, imp := range f.Imports {
				p := strings.Trim(imp.Path.Value, `"`)
				if strings.HasPrefix(p, subsystemPrefix) {
					t.Errorf("%s imports %q — core must not import the dataconsole subsystem; only the enumerated composition points may (see depguard core-not-dataconsole)", rel, p)
				}
			}
			return nil
		})
		if err := walkErr; err != nil {
			t.Fatalf("walk %s/: %v", root, err)
		}
	}
}
