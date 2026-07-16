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
