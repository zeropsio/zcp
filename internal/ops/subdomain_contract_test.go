package ops

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestSubdomainRobustnessContract pins the defensive invariants established by
// plans/archive/subdomain-robustness.md. Each rule corresponds to a root cause
// eliminated by the plan; regressing any of them reintroduces a real bug
// class (garbage FAILED processes, silent-success-on-timeout, discarded
// platform diagnostics, or L7 propagation race).
//
// Scans internal/ops/subdomain.go and internal/tools/subdomain.go. Whitelist
// is explicit — helpers for the contract itself and tests live in a
// different file (this one) and are not scanned.
func TestSubdomainRobustnessContract(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()

	opsSubdomain := parseGoFile(t, fset, "subdomain.go")
	toolsSubdomain := parseGoFile(t, fset, "../tools/subdomain.go")

	t.Run("ops.Subdomain calls GetService before EnableSubdomainAccess", func(t *testing.T) {
		t.Parallel()
		// GetService is the authoritative pre-check that prevents the
		// platform's garbage FAILED process pattern on redundant enable.
		// Removing it re-opens Bug #2 (plan §1.1).
		subdomainFn := findFunc(t, opsSubdomain, "Subdomain")
		getServiceIdx := firstCallIndex(subdomainFn, "GetService")
		enableIdx := firstCallIndex(subdomainFn, "EnableSubdomainAccess")
		if getServiceIdx < 0 {
			t.Fatal("Subdomain must call client.GetService (authoritative SubdomainAccess pre-check)")
		}
		if enableIdx < 0 {
			t.Fatal("Subdomain must still call EnableSubdomainAccess in the cold-enable path")
		}
		if getServiceIdx >= enableIdx {
			t.Errorf("GetService (index %d) must precede EnableSubdomainAccess (index %d)", getServiceIdx, enableIdx)
		}
	})

	t.Run("tools/subdomain.go captures pollManageProcess timeout", func(t *testing.T) {
		t.Parallel()
		// Regression pin for commit 4 — prior code discarded the timedOut
		// bool via `_ :=`, masking a 10-minute poll timeout as success.
		src := readFileAsString(t, "../tools/subdomain.go")
		if strings.Contains(src, ", _ := pollManageProcess(") {
			t.Error("pollManageProcess timedOut bool must not be discarded — surface it via Warnings")
		}
		if !strings.Contains(src, "pollManageProcess(ctx, client, result.Process, onProgress)") {
			t.Error("pollManageProcess call-site signature changed unexpectedly; update contract test")
		}
	})

	t.Run("SubdomainResult has Warnings field", func(t *testing.T) {
		t.Parallel()
		// Plan commit 3: Warnings is the diagnostic channel for non-fatal
		// anomalies (FAILED normalization, poll timeout, HTTP readiness
		// timeout). Removing the field deletes the provenance.
		if !hasStructField(opsSubdomain, "SubdomainResult", "Warnings") {
			t.Error("SubdomainResult.Warnings field missing — diagnostic provenance would be lost")
		}
	})

	t.Run("tools/subdomain.go calls WaitHTTPReady after enable", func(t *testing.T) {
		t.Parallel()
		// Plan commit 5: L7 propagation window means Process FINISHED ≠
		// HTTP reachable. Removing WaitHTTPReady re-opens Bug #1.
		src := readFileAsString(t, "../tools/subdomain.go")
		if !strings.Contains(src, "ops.WaitHTTPReady") {
			t.Error("tools/subdomain.go must call ops.WaitHTTPReady on post-enable URLs")
		}
	})

	t.Run("dead isAlready helpers are not re-introduced", func(t *testing.T) {
		t.Parallel()
		// Plan commit 6: removed isAlreadyEnabled/isAlreadyDisabled as dead
		// code. Platform doesn't emit those error codes.
		src := readFileAsString(t, "subdomain.go")
		for _, forbidden := range []string{"isAlreadyEnabled", "isAlreadyDisabled"} {
			if strings.Contains(src, forbidden) {
				t.Errorf("ops/subdomain.go must not reintroduce dead helper %q", forbidden)
			}
		}
	})

	_ = toolsSubdomain // reserved for future AST rules; readFileAsString is enough for string checks
}

func parseGoFile(t *testing.T, fset *token.FileSet, path string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return f
}

func readFileAsString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func findFunc(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("function %q not found", name)
	return nil
}

// firstCallIndex returns a pseudo-ordinal position of the first call to the
// named method within fn's body. Returns -1 when no call exists. The
// "position" is the token offset — reliable for "A before B" comparisons.
func firstCallIndex(fn *ast.FuncDecl, methodName string) int {
	if fn == nil || fn.Body == nil {
		return -1
	}
	idx := -1
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil {
			return true
		}
		if sel.Sel.Name == methodName {
			pos := int(call.Pos())
			if idx < 0 || pos < idx {
				idx = pos
			}
		}
		return true
	})
	return idx
}

func hasStructField(file *ast.File, typeName, fieldName string) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != typeName {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, f := range st.Fields.List {
			for _, name := range f.Names {
				if name.Name == fieldName {
					found = true
					return false
				}
			}
		}
		return false
	})
	return found
}
