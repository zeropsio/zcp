package content

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// markerConstNames are the ZCP-managed-section / REFLOG / shell marker
// constants declared across content/ and init/. Matching any of these
// with a raw substring search treats a mid-line prose MENTION as a
// structural boundary and truncates user content at the mention —
// content.IndexMarkerLine is the single owner of line-anchored marker
// matching and MUST be used instead.
var markerConstNames = map[string]bool{
	"agentMarkerBegin": true, "agentMarkerEnd": true,
	"mdMarkerBegin": true, "mdMarkerEnd": true,
	"reflogMarker": true, "reflogMarkerEnd": true,
	"shellMarkerBegin": true, "shellMarkerEnd": true,
}

// markerLiteralSignatures are substrings that identify a marker string
// literal passed inline (rather than via a named constant).
var markerLiteralSignatures = []string{"ZCP:BEGIN", "ZCP:END", "ZEROPS:REFLOG"}

// rawSubstringFuncs are the strings.* helpers that perform unanchored
// matching — using any of them against a marker is the regression class.
var rawSubstringFuncs = map[string]bool{
	"Index": true, "LastIndex": true, "Contains": true, "Count": true,
	"Split": true, "SplitN": true, "SplitAfter": true,
	"HasPrefix": true, "HasSuffix": true, "ReplaceAll": true, "Replace": true,
}

// TestNoRawMarkerMatching pins the single-owner contract for marker
// matching: no non-test code in content/ or init/ may pass a ZCP marker
// (named constant or inline literal) to a raw strings.* substring
// helper. All marker lookups route through content.IndexMarkerLine,
// which anchors to whole lines so a mid-line mention in user prose is
// treated as content, not a section boundary. Regression class: the
// real-user CLAUDE.md corruption (lines truncated at `-->`, 2026-07-04)
// and the AGENTS.md data-loss the first cut of the anchoring fix left in
// the reflog-drop branch. markers.go (the owner) is exempt.
func TestNoRawMarkerMatching(t *testing.T) {
	t.Parallel()
	dirs := []string{".", filepath.Join("..", "init")}
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
			if dir == "." && name == "markers.go" {
				continue // the single owner
			}
			path := filepath.Join(dir, name)
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if callMatchesMarkerRawly(n) {
					violations = append(violations, fset.Position(n.Pos()).String())
				}
				return true
			})
		}
	}
	if len(violations) > 0 {
		t.Errorf("raw strings.* substring match on a ZCP marker — use content.IndexMarkerLine (line-anchored; a mid-line mention is prose, not a boundary):\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// TestMarkerLintScanner_FiresOnFixture proves the scanner detects both a
// named-constant arg and an inline-literal arg, so a green
// TestNoRawMarkerMatching means "no raw matchers", not "scanner blind".
func TestMarkerLintScanner_FiresOnFixture(t *testing.T) {
	t.Parallel()
	const src = `package x
import "strings"
func f(text, mdMarkerBegin string) {
	_ = strings.Index(text, mdMarkerBegin)
	_ = strings.Contains(text, "<!-- ZEROPS:REFLOG -->")
	_ = strings.Contains(text, "unrelated")
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	count := 0
	ast.Inspect(f, func(n ast.Node) bool {
		if callMatchesMarkerRawly(n) {
			count++
		}
		return true
	})
	if count != 2 {
		t.Errorf("scanner must flag the constant-arg and literal-arg calls (not the unrelated one); got %d, want 2", count)
	}
}

// callMatchesMarkerRawly reports whether n is a strings.<rawFunc>(...)
// call with any argument that is a marker constant identifier or a
// string literal carrying a marker signature.
func callMatchesMarkerRawly(n ast.Node) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "strings" || !rawSubstringFuncs[sel.Sel.Name] {
		return false
	}
	return slices.ContainsFunc(call.Args, argIsMarker)
}

func argIsMarker(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return markerConstNames[v.Name]
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return false
		}
		for _, sig := range markerLiteralSignatures {
			if strings.Contains(v.Value, sig) {
				return true
			}
		}
	}
	return false
}
