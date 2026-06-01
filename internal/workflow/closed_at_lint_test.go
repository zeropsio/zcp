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

// closedAtAllowlist names the ONLY functions permitted to compare a work
// session's raw .ClosedAt field against "". Auto-close is DERIVED, not stamped
// (P5): a derived auto-complete session keeps ClosedAt=="" on disk, so a raw
// `ws.ClosedAt == ""` read elsewhere treats a done session as open and stuck-
// loops the agent. Every other caller MUST go through IsOpen / DeriveCloseState.
//
//   - DeriveCloseState   — the single derive site (ClosedAt!="" => explicit/cap close).
//   - ResolveLifecycle   — open OR auto-complete (incl. old-binary stamped) => FocusWork.
//   - closeWorkSessionOnCap — explicit iteration-cap double-stamp guard.
var closedAtAllowlist = map[string]bool{
	"DeriveCloseState":      true,
	"ResolveLifecycle":      true,
	"closeWorkSessionOnCap": true,
}

// TestNoRawClosedAtReads pins the derived-close invariant: no function outside
// the allowlist may branch on `<ws>.ClosedAt == ""` / `!= ""`. Catches the
// regression class the P7 review found (workflow_develop.go, guard.go,
// instructions.go all read raw ClosedAt and read a derived-closed session as
// open). Tests are exempt (they seed ClosedAt directly).
func TestNoRawClosedAtReads(t *testing.T) {
	t.Parallel()
	dirs := []string{".", filepath.Join("..", "tools"), filepath.Join("..", "server")}
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
			path := filepath.Join(dir, name)
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil || closedAtAllowlist[fn.Name.Name] {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if comparesClosedAtToEmpty(n) {
						violations = append(violations, fset.Position(n.Pos()).String()+" (in "+fn.Name.Name+")")
					}
					return true
				})
			}
		}
	}
	if len(violations) > 0 {
		t.Errorf("raw `.ClosedAt ==/!= \"\"` read outside {DeriveCloseState, ResolveLifecycle, closeWorkSessionOnCap} — use workflow.IsOpen / DeriveCloseState (auto-complete is derived, ClosedAt stays \"\" on disk):\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// TestClosedAtScanner_FiresOnFixture proves the scanner detects both comparison
// directions (so a green TestNoRawClosedAtReads means "no raw readers", not
// "scanner is blind").
func TestClosedAtScanner_FiresOnFixture(t *testing.T) {
	t.Parallel()
	const src = `package x
func f(ws *T) bool { return ws.ClosedAt == "" || ws.ClosedAt != "" }`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	count := 0
	ast.Inspect(f, func(n ast.Node) bool {
		if comparesClosedAtToEmpty(n) {
			count++
		}
		return true
	})
	if count != 2 {
		t.Errorf("scanner must detect both `== \"\"` and `!= \"\"`; got %d, want 2", count)
	}
}

// comparesClosedAtToEmpty reports whether n is a binary == / != expression with
// a `.ClosedAt` selector on one side and an empty string literal on the other.
// Pointer-nil checks (`ClosedAt != nil`) and assignments (`ClosedAt = x`) do not
// match — only the string-field open/closed branch this invariant governs.
func comparesClosedAtToEmpty(n ast.Node) bool {
	be, ok := n.(*ast.BinaryExpr)
	if !ok || (be.Op != token.EQL && be.Op != token.NEQ) {
		return false
	}
	return (isClosedAtSelector(be.X) && isEmptyStringLit(be.Y)) ||
		(isClosedAtSelector(be.Y) && isEmptyStringLit(be.X))
}

func isClosedAtSelector(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "ClosedAt"
}

func isEmptyStringLit(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && (lit.Value == `""` || lit.Value == "``")
}
