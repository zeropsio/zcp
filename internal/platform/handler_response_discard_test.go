package platform_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// handlerDiscardViolation describes an assignment that discards the response
// value of a zerops-go SDK handler call.
type handlerDiscardViolation struct {
	File   string
	Line   int
	Method string
}

// scanHandlerResponseDiscards finds assignments of the form
//
//	_, err = <expr>.handler.<Method>(...)
//
// where the SDK response (the first return) is discarded with `_`. In
// zerops-go every handler method reports API-level failures (HTTP 4xx/5xx)
// ONLY through the response's Output()/Err() — the function's error return
// covers transport failures alone. Discarding the response therefore
// swallows every API error and turns a rejected call into a silent success
// (the P0-2 GrantSelfRole bug). The response must be captured and its
// Output()/Err() checked.
func scanHandlerResponseDiscards(roots []string) ([]handlerDiscardViolation, error) {
	var violations []handlerDiscardViolation
	fset := token.NewFileSet()
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if os.IsNotExist(walkErr) {
					return nil
				}
				return walkErr
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				assign, ok := n.(*ast.AssignStmt)
				if !ok || len(assign.Lhs) == 0 || len(assign.Rhs) != 1 {
					return true
				}
				if !isBlankIdent(assign.Lhs[0]) {
					return true
				}
				method, ok := handlerCallMethod(assign.Rhs[0])
				if !ok {
					return true
				}
				pos := fset.Position(assign.Pos())
				violations = append(violations, handlerDiscardViolation{
					File:   path,
					Line:   pos.Line,
					Method: method,
				})
				return true
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return violations, nil
}

// isBlankIdent reports whether the expression is the blank identifier `_`.
func isBlankIdent(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "_"
}

// handlerCallMethod returns the method name when the expression is a call on
// a selector chain containing `.handler.` (i.e. `<expr>.handler.<Method>(...)`).
func handlerCallMethod(e ast.Expr) (string, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return "", false
	}
	// The receiver of the method call must itself be a `.handler` selector.
	recv, ok := sel.X.(*ast.SelectorExpr)
	if !ok || recv.Sel == nil || recv.Sel.Name != "handler" {
		return "", false
	}
	return sel.Sel.Name, true
}

// TestNoHandlerResponseDiscards pins the single-owner rule that every
// zerops-go handler call must capture and inspect its response (Output()/
// Err()), never discard it with `_`. This is the class-prevention pin for
// P0-2: the GrantSelfRole bug was the only one of 30+ call sites that
// dropped the response, and nothing structurally stopped the next one.
func TestNoHandlerResponseDiscards(t *testing.T) {
	t.Parallel()

	violations, err := scanHandlerResponseDiscards([]string{"."})
	if err != nil {
		t.Fatalf("scanHandlerResponseDiscards: %v", err)
	}
	for _, v := range violations {
		t.Errorf(
			"discarded SDK handler response — %s:%d `_, ... = ....handler.%s(...)`\n"+
				"\t→ zerops-go reports API errors (4xx/5xx) only via the response Output()/Err()\n"+
				"\t→ capture the response and check `if _, err := resp.Output(); err != nil { ... }`\n"+
				"\t→ see plans/audit-fixes-plan-2026-06-10.md Phase 2 (P0-2)",
			v.File, v.Line, v.Method,
		)
	}
}

// TestHandlerResponseDiscardScanner_FiresOnFixture is the lint's self-test:
// it proves the scanner flags the forbidden form (and does not flag the
// correct capture-and-check form), so a future AST-walk regression cannot
// silently let the production scan return zero.
func TestHandlerResponseDiscardScanner_FiresOnFixture(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package fixture

type sdk struct{ handler hdl }
type hdl struct{}
type resp struct{}

func (resp) Output() (any, error) { return nil, nil }
func (hdl) PutClientUserRoles() (resp, error) { return resp{}, nil }
func (hdl) GetClientUserRoles() (resp, error) { return resp{}, nil }

func bad(p sdk) error {
	_, err := p.handler.PutClientUserRoles() // FLAG: response discarded
	return err
}

func good(p sdk) error {
	r, err := p.handler.GetClientUserRoles() // OK: captured
	if err != nil {
		return err
	}
	_, err = r.Output() // OK: not a .handler call
	return err
}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	violations, err := scanHandlerResponseDiscards([]string{dir})
	if err != nil {
		t.Fatalf("scanHandlerResponseDiscards: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected exactly 1 violation (the discarded PutClientUserRoles), got %d: %+v",
			len(violations), violations)
	}
	if violations[0].Method != "PutClientUserRoles" {
		t.Errorf("flagged method = %q, want PutClientUserRoles", violations[0].Method)
	}
}
