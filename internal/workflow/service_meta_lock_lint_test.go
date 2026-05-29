package workflow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestServiceMetaWriteIsLocked pins the XCUT-1 invariant: WriteServiceMeta — the
// low-level, UNLOCKED, atomic-rename writer — may only be called from
// service_meta.go. Every other ServiceMeta mutation must go through
// UpdateServiceMeta / UpsertServiceMeta, which hold the .services.lock flock so
// concurrent updates to orthogonal dimensions (close-mode / git-push /
// build-integration / first-deploy) cannot lost-update each other under the
// MCP go-sdk's async dispatch.
//
// It catches BOTH in-package unqualified calls (WriteServiceMeta, in package
// workflow) AND tools-layer qualified calls (workflow.WriteServiceMeta) — the
// existing topology selector-only scanner misses the unqualified form, which is
// how 7 of the 15 callers would have slipped a SelectorExpr-only lint. Tests
// seed metas via the raw writer directly and are exempt.
func TestServiceMetaWriteIsLocked(t *testing.T) {
	t.Parallel()
	// Relative to this package dir (go test runs with CWD = the package dir).
	dirs := []string{".", filepath.Join("..", "tools")}
	var violations []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read dir %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			// service_meta.go is the sanctioned home of the raw writer + the
			// locked Update/Upsert primitives that wrap it.
			if dir == "." && name == "service_meta.go" {
				continue
			}
			path := filepath.Join(dir, name)
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && callTargetName(call.Fun) == "WriteServiceMeta" {
					violations = append(violations, fset.Position(call.Pos()).String())
				}
				return true
			})
		}
	}
	if len(violations) > 0 {
		t.Errorf("WriteServiceMeta called outside service_meta.go — use UpdateServiceMeta/UpsertServiceMeta for locked read-modify-write (XCUT-1):\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// TestServiceMetaWriteScanner_FiresOnFixture proves the scanner detects both the
// unqualified and the package-qualified call forms (so a green
// TestServiceMetaWriteIsLocked means "no callers", not "scanner is blind").
func TestServiceMetaWriteScanner_FiresOnFixture(t *testing.T) {
	t.Parallel()
	const src = `package x
func f(d string, m *T) { WriteServiceMeta(d, m); pkg.WriteServiceMeta(d, m) }`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	count := 0
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && callTargetName(call.Fun) == "WriteServiceMeta" {
			count++
		}
		return true
	})
	if count != 2 {
		t.Errorf("scanner must detect both the unqualified and qualified WriteServiceMeta calls; got %d, want 2", count)
	}
}

// callTargetName returns the callee identifier for an unqualified call
// (WriteServiceMeta) or a selector call (pkg.WriteServiceMeta); "" otherwise.
func callTargetName(fun ast.Expr) string {
	switch t := fun.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}
