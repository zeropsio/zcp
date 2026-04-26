package topology_test

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

// stdoutWriteViolation describes one expression that writes to stdout
// from within MCP-server-session code paths.
type stdoutWriteViolation struct {
	File string
	Line int
	Form string // "fmt.Print*", "fmt.Fprint*(os.Stdout, ...)", "os.Stdout.Write*", "println"
}

// scanForStdoutWrites walks Go files under roots looking for any of:
//   - fmt.Print, fmt.Println, fmt.Printf
//   - fmt.Fprint(os.Stdout, ...), fmt.Fprintln(os.Stdout, ...), fmt.Fprintf(os.Stdout, ...)
//   - os.Stdout.Write(...), os.Stdout.WriteString(...)
//   - println(...) (builtin)
//
// Test files (*_test.go) are exempt — debug prints in tests are fine.
// CLI entrypoints under cmd/ are NOT in scope here; this scanner is
// designed to be called against `internal/` only.
//
// `fmt.Fprint*(os.Stderr, ...)`, `log.*`, `os.Stderr.Write*`, and any
// other stderr-bound output are explicitly allowed.
func scanForStdoutWrites(roots []string) ([]stdoutWriteViolation, error) {
	var violations []stdoutWriteViolation
	fset := token.NewFileSet()
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if os.IsNotExist(walkErr) {
					return nil
				}
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				form := classifyStdoutCall(call)
				if form == "" {
					return true
				}
				pos := fset.Position(call.Pos())
				violations = append(violations, stdoutWriteViolation{
					File: path,
					Line: pos.Line,
					Form: form,
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

// classifyStdoutCall returns a non-empty form name when the call is one
// of the forbidden stdout-write expressions. Returns "" otherwise.
func classifyStdoutCall(call *ast.CallExpr) string {
	// Builtin println(...).
	if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "println" {
		return "println"
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return ""
	}
	method := sel.Sel.Name

	// fmt.Print, fmt.Println, fmt.Printf.
	if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "fmt" {
		switch method {
		case "Print", "Println", "Printf":
			return "fmt." + method
		case "Fprint", "Fprintln", "Fprintf":
			// Only flag when first arg is os.Stdout. fmt.Fprint(os.Stderr, ...)
			// or fmt.Fprint(buf, ...) is fine.
			if len(call.Args) > 0 && isOSStdout(call.Args[0]) {
				return "fmt." + method + "(os.Stdout, ...)"
			}
			return ""
		}
	}

	// os.Stdout.Write / os.Stdout.WriteString.
	if isOSStdout(sel.X) {
		switch method {
		case "Write", "WriteString":
			return "os.Stdout." + method
		}
	}

	return ""
}

// isOSStdout returns true when the expression is the literal `os.Stdout`.
func isOSStdout(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkg.Name == "os" && sel.Sel.Name == "Stdout"
}

// TestNoStdoutOutsideJSONPath pins the CLAUDE.md "JSON-only stdout"
// convention. The MCP STDIO protocol requires stdout to carry only the
// JSON-RPC framing; any stray fmt.Println, os.Stdout.Write, or println
// in the request-handling path silently breaks Claude Desktop and
// every other MCP STDIO client.
//
// Scope: internal/ — every package that runs DURING an MCP server
// session. cmd/zcp/ is intentionally NOT scanned: CLI entrypoints
// (zcp --version, zcp analyze, etc.) legitimately write to stdout for
// human users and never coexist with a running server in the same
// process invocation.
//
// Stderr writes (os.Stderr, fmt.Fprint(os.Stderr, ...), log.*) are
// allowed — they don't share the JSON channel.
func TestNoStdoutOutsideJSONPath(t *testing.T) {
	t.Parallel()

	// Test file lives in internal/topology/. ".." is internal/.
	roots := []string{".."}

	violations, err := scanForStdoutWrites(roots)
	if err != nil {
		t.Fatalf("scanForStdoutWrites: %v", err)
	}
	for _, v := range violations {
		t.Errorf(
			"forbidden stdout write — %s:%d %s\n"+
				"\t→ MCP STDIO protocol requires stdout to carry only JSON-RPC framing\n"+
				"\t→ debug output goes to stderr (os.Stderr, log.*)\n"+
				"\t→ see CLAUDE.md Conventions: \"JSON-only stdout\"",
			v.File, v.Line, v.Form,
		)
	}
}

// TestStdoutPurityScanner_FiresOnFixture is the lint engine's self-test:
// TestNoStdoutOutsideJSONPath above only proves the production tree is
// clean today. If classifyStdoutCall is broken (wrong selector match,
// wrong method-name list, AST-walk miss), the production scan would
// silently return zero and no future violation would be caught. This
// fixture asserts the scanner DOES flag every forbidden form.
func TestStdoutPurityScanner_FiresOnFixture(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package fixture

import (
	"fmt"
	"os"
)

func main() {
	fmt.Print("a")
	fmt.Println("b")
	fmt.Printf("c %d\n", 1)
	fmt.Fprint(os.Stdout, "d")
	fmt.Fprintln(os.Stdout, "e")
	fmt.Fprintf(os.Stdout, "f %d\n", 2)
	os.Stdout.Write([]byte("g"))
	os.Stdout.WriteString("h")
	println("i")
}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	violations, err := scanForStdoutWrites([]string{dir})
	if err != nil {
		t.Fatalf("scanForStdoutWrites: %v", err)
	}

	// Expected: 9 forbidden forms (3 fmt.Print*, 3 fmt.Fprint*(os.Stdout, ...),
	// 2 os.Stdout.Write*, 1 println).
	if len(violations) != 9 {
		t.Fatalf("expected 9 violations from fixture, got %d: %+v",
			len(violations), violations)
	}

	wantForms := map[string]bool{
		"fmt.Print":                    false,
		"fmt.Println":                  false,
		"fmt.Printf":                   false,
		"fmt.Fprint(os.Stdout, ...)":   false,
		"fmt.Fprintln(os.Stdout, ...)": false,
		"fmt.Fprintf(os.Stdout, ...)":  false,
		"os.Stdout.Write":              false,
		"os.Stdout.WriteString":        false,
		"println":                      false,
	}
	for _, v := range violations {
		if _, ok := wantForms[v.Form]; ok {
			wantForms[v.Form] = true
		}
	}
	for form, fired := range wantForms {
		if !fired {
			t.Errorf("scanner did not flag form %q in fixture; got: %+v", form, violations)
		}
	}
}

// TestStdoutPurityScanner_StderrAllowed proves stderr writes are NOT
// flagged. The MCP STDIO contract is about stdout specifically; stderr
// is the right channel for debug/error output.
func TestStdoutPurityScanner_StderrAllowed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package fixture

import (
	"fmt"
	"log"
	"os"
)

func main() {
	fmt.Fprint(os.Stderr, "a")
	fmt.Fprintln(os.Stderr, "b")
	fmt.Fprintf(os.Stderr, "c %d\n", 1)
	os.Stderr.Write([]byte("d"))
	os.Stderr.WriteString("e")
	log.Print("f")
	log.Println("g")
	log.Printf("h %d\n", 2)
}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	violations, err := scanForStdoutWrites([]string{dir})
	if err != nil {
		t.Fatalf("scanForStdoutWrites: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected zero violations from stderr-only fixture, got %d: %+v",
			len(violations), violations)
	}
}

// TestStdoutPurityScanner_BufferWritesAllowed proves writes to non-os.Stdout
// io.Writer instances (bytes.Buffer, strings.Builder, etc.) are not flagged.
// fmt.Fprint(buf, ...) is a common idiom that has nothing to do with the
// MCP protocol channel.
func TestStdoutPurityScanner_BufferWritesAllowed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package fixture

import (
	"bytes"
	"fmt"
	"strings"
)

func main() {
	var buf bytes.Buffer
	fmt.Fprint(&buf, "a")
	fmt.Fprintln(&buf, "b")
	fmt.Fprintf(&buf, "c %d\n", 1)
	buf.WriteString("d")

	var sb strings.Builder
	fmt.Fprint(&sb, "e")
}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	violations, err := scanForStdoutWrites([]string{dir})
	if err != nil {
		t.Fatalf("scanForStdoutWrites: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected zero violations from buffer-write fixture, got %d: %+v",
			len(violations), violations)
	}
}
