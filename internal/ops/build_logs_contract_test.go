package ops

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestBuildLogsContract_UsesTagIdentityAndApplicationFacility pins the
// invariants I-LOG-2 (tag identity) and I-LOG-3 (application facility) for
// the FetchBuildWarnings and FetchBuildLogs functions.
//
// Regression scenario this test catches:
//  1. Someone deletes the Tags line from FetchBuildWarnings to "simplify".
//     Stale warnings from previous builds on the same build stack start
//     leaking into successful deploy results. The eval scenario and the
//     api-tagged contract tests pin the visible symptom; this test pins
//     the structural cause so the failure is discoverable without running
//     live-API tests.
//  2. Someone removes the Facility="application" line to "return more".
//     Daemon noise (sshfs mount errors, systemd timeouts) starts appearing
//     alongside genuine build warnings.
//
// The scan targets the composite literal of platform.LogFetchParams inside
// the two functions. For each occurrence, it requires:
//   - A Tags: field with a non-nil value.
//   - A Facility: field set to "application".
//
// If you have a legitimate reason to call the backend with a wider scope
// (e.g. a future "fetch everything for debugging" mode), do it via a new
// function with its own invariant, not by weakening these.
//
// Background: plans/logging-refactor.md §4.7 I-LOG-2, I-LOG-3.
func TestBuildLogsContract_UsesTagIdentityAndApplicationFacility(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "build_logs.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse build_logs.go: %v", err)
	}

	requireContract := map[string]bool{
		"FetchBuildWarnings": false,
		"FetchBuildLogs":     false,
	}

	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		if _, want := requireContract[fn.Name.Name]; !want {
			return true
		}
		requireContract[fn.Name.Name] = true

		found := struct {
			tags, facility, logFetchParamsLit bool
		}{}

		ast.Inspect(fn, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			// Identify LogFetchParams (either platform.LogFetchParams or
			// bare LogFetchParams if imported as package).
			if !isLogFetchParamsLit(cl) {
				return true
			}
			found.logFetchParamsLit = true
			for _, elt := range cl.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				ident, ok := kv.Key.(*ast.Ident)
				if !ok {
					continue
				}
				switch ident.Name {
				case "Tags":
					// Non-nil, non-empty composite literal.
					if !isNilOrEmpty(kv.Value) {
						found.tags = true
					}
				case "Facility":
					// Must be exactly "application" string literal.
					if lit, ok := kv.Value.(*ast.BasicLit); ok && lit.Value == `"application"` {
						found.facility = true
					}
				}
			}
			return false
		})

		if !found.logFetchParamsLit {
			t.Errorf("%s: no platform.LogFetchParams composite literal found — did the caller-side fetch wrapping get refactored out?", fn.Name.Name)
		}
		if !found.tags {
			t.Errorf("%s: LogFetchParams missing Tags field (I-LOG-2 regression — per-build tag identity dropped). See plans/logging-refactor.md §4.7.", fn.Name.Name)
		}
		if !found.facility {
			t.Errorf("%s: LogFetchParams missing Facility=\"application\" (I-LOG-3 regression — daemon noise will leak). See plans/logging-refactor.md §4.7.", fn.Name.Name)
		}
		return false
	})

	for name, seen := range requireContract {
		if !seen {
			t.Errorf("expected to find function %s in build_logs.go — signature changed?", name)
		}
	}
}

func isLogFetchParamsLit(cl *ast.CompositeLit) bool {
	switch t := cl.Type.(type) {
	case *ast.SelectorExpr:
		// platform.LogFetchParams
		if t.Sel != nil && t.Sel.Name == "LogFetchParams" {
			return true
		}
	case *ast.Ident:
		if t.Name == "LogFetchParams" {
			return true
		}
	}
	return false
}

func isNilOrEmpty(expr ast.Expr) bool {
	if ident, ok := expr.(*ast.Ident); ok && ident.Name == "nil" {
		return true
	}
	if cl, ok := expr.(*ast.CompositeLit); ok && len(cl.Elts) == 0 {
		return true
	}
	// Check string values too — `Tags: []string{}` or nil equivalents.
	_ = strings.HasPrefix
	return false
}
