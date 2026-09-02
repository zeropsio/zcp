package tools

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoStructuredContentOnToolResults pins the trap that decided the
// mate envelope-on-the-wire design.
//
// The Go MCP SDK marshals a non-nil typed handler output (the second
// return value of a tool handler) into the JSON-RPC result's
// `structuredContent` field, alongside — not instead of — the text
// content. Claude Code, however, REPLACES the model-facing tool result
// with `structuredContent` whenever it is present: the text block never
// reaches the model. For ZCP that means every atom of synthesized
// guidance a workflow result renders would silently vanish the moment a
// handler populated the slot.
//
// So the typed-output slot stays empty at every handler, and machine
// state (the lifecycle `workflow.StateEnvelope`) rides INSIDE the text
// as a fenced `json zcp-envelope` block — see docs/spec-mate.md,
// "Envelope on the wire", and workflow.AppendEnvelope.
//
// Test files are exempt: a test may construct any result shape it likes
// to assert on it.
func TestNoStructuredContentOnToolResults(t *testing.T) {
	t.Parallel()

	files, err := goSourceFiles(".")
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}

	fset := token.NewFileSet()
	for _, path := range files {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		for _, h := range toolHandlers(file) {
			for _, ret := range handlerReturns(h.body) {
				if len(ret.Results) != 3 || isNilIdent(ret.Results[1]) {
					continue
				}
				t.Errorf("%s:%d: handler %s returns a non-nil typed output — "+
					"Claude Code replaces the model-facing text with structuredContent; "+
					"put machine state in the text via workflow.AppendEnvelope instead",
					path, fset.Position(ret.Pos()).Line, h.name)
			}
		}
	}
}

// TestNoStructuredContentField backs the handler check with a source
// scan: nothing in internal/tools may set the SDK's StructuredContent
// field on a result it builds by hand either.
func TestNoStructuredContentField(t *testing.T) {
	t.Parallel()

	files, err := goSourceFiles(".")
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	fset := token.NewFileSet()
	for _, path := range files {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		// Idents only — a doc comment naming the field (this contract has
		// to be explainable at its call sites) is prose, not a write.
		ast.Inspect(file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if ok && id.Name == "StructuredContent" {
				t.Errorf("%s:%d: StructuredContent is forbidden — Claude Code replaces "+
					"the model-facing text with it (docs/spec-mate.md)",
					path, fset.Position(id.Pos()).Line)
			}
			return true
		})
	}
}

// goSourceFiles lists the non-test Go files under root, recursively.
func goSourceFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

// toolHandler is one function with the MCP tool-handler result shape.
type toolHandler struct {
	name string
	body *ast.BlockStmt
}

// toolHandlers collects every tool handler in file — both named
// functions and the anonymous closures handed to mcp.AddTool, which are
// where most handlers actually live.
func toolHandlers(file *ast.File) []toolHandler {
	var out []toolHandler
	ast.Inspect(file, func(n ast.Node) bool {
		switch fn := n.(type) {
		case *ast.FuncDecl:
			if fn.Body != nil && isToolHandlerSignature(fn.Type) {
				out = append(out, toolHandler{fn.Name.Name, fn.Body})
			}
		case *ast.FuncLit:
			if fn.Body != nil && isToolHandlerSignature(fn.Type) {
				out = append(out, toolHandler{"<closure>", fn.Body})
			}
		}
		return true
	})
	return out
}

// isToolHandlerSignature reports whether ft has the MCP tool-handler
// result shape `(*mcp.CallToolResult, T, error)` — three results whose
// first is a pointer to mcp.CallToolResult and whose last is error.
func isToolHandlerSignature(ft *ast.FuncType) bool {
	if ft.Results == nil {
		return false
	}
	var count int
	for _, f := range ft.Results.List {
		n := len(f.Names)
		if n == 0 {
			n = 1
		}
		count += n
	}
	if count != 3 {
		return false
	}
	// The third result must be `error` — helpers that merely happen to
	// return a result plus two values (e.g. a pre-deploy validator
	// returning (result, msg, FailureClass)) are not tool handlers and own
	// no structured-output slot.
	last := ft.Results.List[len(ft.Results.List)-1].Type
	if id, ok := last.(*ast.Ident); !ok || id.Name != "error" {
		return false
	}
	star, ok := ft.Results.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "mcp" && sel.Sel.Name == "CallToolResult"
}

// handlerReturns collects the return statements belonging to body itself,
// skipping any nested function literal (a closure's returns answer to its
// own signature, not the handler's).
func handlerReturns(body *ast.BlockStmt) []*ast.ReturnStmt {
	var out []*ast.ReturnStmt
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			out = append(out, node)
		}
		return true
	})
	return out
}

// isNilIdent reports whether expr is the bare identifier `nil`.
func isNilIdent(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "nil"
}
