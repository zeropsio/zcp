package checks_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// forbiddenGoStructs are internal Go type names whose exported fields
// MUST NOT appear in user-facing check Detail strings. The manifest
// writer (and the main agent reading check failures) authors JSON — so
// error text must name wire contracts by their JSON key, not by the Go
// struct.field reader-side projection. See
// docs/zcprecipator2/05-regression/defect-class-registry.md §16.2 and
// HANDOFF-to-I6 Cx-CHECK-WIRE-NOTATION.
//
// Canonical v35 smoking gun: CheckManifestCompleteness emitted
// "manifest missing entries for N distinct FactRecord.Title values" —
// FactRecord is a Go struct, Title is a Go field; the JSON key the
// author must write is `fact_title`. The agent never guessed the JSON
// key because the error text pointed at Go notation.
var forbiddenGoStructs = []string{
	"FactRecord",
	"ContentManifest",
	"ContentManifestFact",
	"ManifestFact",
	"StepCheck",
}

// TestCheckDetailStrings_UseJSONKeyNotation_NoGoStructFieldDotNotation
// walks every non-test Go source file under internal/ops/checks/ and
// internal/tools/workflow_checks_*.go, locates every
// workflow.StepCheck composite literal (or the local type alias used
// inside internal/workflow itself), and fails if any Detail string
// literal — direct or inside a fmt.Sprintf/Errorf format arg — matches
// a forbidden Go-struct dot-notation reference.
//
// Passing this test is the calibration bar B-10 from
// runs/v35/verdict.md §4.
func TestCheckDetailStrings_UseJSONKeyNotation_NoGoStructFieldDotNotation(t *testing.T) {
	t.Parallel()

	pattern := regexp.MustCompile(
		`\b(` + strings.Join(forbiddenGoStructs, "|") + `)\.[A-Z]\w+\b`,
	)

	dirs := []string{
		".",
		filepath.Join("..", "..", "tools"),
	}

	var violations []detailViolation

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
			// In internal/tools, only lint the workflow_checks_*.go
			// family — the handoff scope. Other tools files author
			// their own messages and are out of scope.
			if dir != "." && !strings.HasPrefix(name, "workflow_checks") {
				continue
			}
			path := filepath.Join(dir, name)
			violations = append(violations,
				inspectFileForGoNotationInCheckDetails(t, path, pattern)...,
			)
		}
	}

	if len(violations) > 0 {
		var b strings.Builder
		b.WriteString("check Detail strings must name wire contracts by JSON key, not Go struct.field (see §16.2 / Cx-CHECK-WIRE-NOTATION):\n")
		for _, v := range violations {
			b.WriteString("  ")
			b.WriteString(v.file)
			b.WriteString(":")
			b.WriteString(strconv.Itoa(v.line))
			b.WriteString("  matched ")
			b.WriteString(strconv.Quote(v.text))
			b.WriteString("\n")
		}
		t.Fatal(b.String())
	}
}

type detailViolation struct {
	file string
	line int
	text string
}

func inspectFileForGoNotationInCheckDetails(t *testing.T, path string, pattern *regexp.Regexp) []detailViolation {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var out []detailViolation
	walkForStepCheckDetails(f, func(stringLit *ast.BasicLit) {
		if stringLit.Kind != token.STRING {
			return
		}
		val, err := strconv.Unquote(stringLit.Value)
		if err != nil {
			return
		}
		if m := pattern.FindString(val); m != "" {
			pos := fset.Position(stringLit.Pos())
			out = append(out, detailViolation{pos.Filename, pos.Line, m})
		}
	})
	return out
}

// walkForStepCheckDetails invokes fn for every string literal that
// appears in the Detail-field value of a workflow.StepCheck composite
// literal. Handles:
//   - StepCheck{Detail: ...}
//   - workflow.StepCheck{Detail: ...}
//   - []StepCheck{{Detail: ...}, ...} (inner composite lit with nil Type)
//   - []workflow.StepCheck{{Detail: ...}, ...}
func walkForStepCheckDetails(root ast.Node, fn func(*ast.BasicLit)) {
	var visit func(n ast.Node, inStepCheckSlice bool)
	visit = func(n ast.Node, inStepCheckSlice bool) {
		if n == nil {
			return
		}
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			ast.Inspect(n, func(nn ast.Node) bool {
				if nn == n {
					return true
				}
				if child, ok := nn.(*ast.CompositeLit); ok {
					visit(child, false)
					return false
				}
				return true
			})
			return
		}
		// Determine this CompositeLit's effective type.
		typeExpr := cl.Type
		isStepCheck := isStepCheckType(typeExpr) || (typeExpr == nil && inStepCheckSlice)

		// Is this a slice/array of StepCheck? Then inner composite
		// literals with nil Type should be treated as StepCheck.
		nextIsStepCheckSlice := false
		if arr, ok := typeExpr.(*ast.ArrayType); ok {
			if isStepCheckType(arr.Elt) {
				nextIsStepCheckSlice = true
			}
		}

		if isStepCheck {
			for _, elt := range cl.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Detail" {
						collectStringLiterals(kv.Value, fn)
					}
				}
			}
		}

		for _, elt := range cl.Elts {
			visit(elt, nextIsStepCheckSlice)
		}
	}
	ast.Inspect(root, func(n ast.Node) bool {
		if cl, ok := n.(*ast.CompositeLit); ok {
			visit(cl, false)
			return false
		}
		return true
	})
}

// isStepCheckType reports whether the composite-literal type expression
// names workflow.StepCheck (or a bare StepCheck used from within the
// internal/workflow package itself).
func isStepCheckType(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name == "StepCheck"
	case *ast.SelectorExpr:
		return t.Sel != nil && t.Sel.Name == "StepCheck"
	}
	return false
}

// collectStringLiterals walks an expression tree and invokes fn for
// every *ast.BasicLit encountered. Captures both direct string values
// (Detail: "..."), fmt.Sprintf format strings + format args, and
// deeper call chains.
func collectStringLiterals(expr ast.Expr, fn func(*ast.BasicLit)) {
	ast.Inspect(expr, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok {
			fn(lit)
		}
		return true
	})
}
