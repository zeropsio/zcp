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

// handlerStateViolation describes one package-level var declaration in
// the handler layer (internal/tools/) that lacks an initializer —
// i.e., a zero-valued mutable global that will be assigned at runtime,
// which is the exact shape "stateless STDIO tools" forbids.
type handlerStateViolation struct {
	File  string
	Line  int
	Names []string
}

// scanForHandlerState walks Go files under roots looking for top-level
// `var` declarations that have no initializer (`var x int`,
// `var m map[string]int`, etc.). These are zero-value globals that
// can only be useful if they're MUTATED at runtime — exactly the
// per-call state leak pattern CLAUDE.md "Stateless STDIO tools"
// forbids.
//
// Exempt forms (each has an initializer or is a compile-time check):
//   - `var _ Interface = (*T)(nil)` — interface satisfaction assertion
//   - `var x = regexp.MustCompile(...)` — compiled regex (effectively immutable)
//   - `var x = map[K]V{...}` / `var x = []T{...}` — initialised lookup tables
//   - `var x = "literal"` / `var x = 42` — basic literals
//
// Test files (`*_test.go`) are exempt — fixtures and helpers may
// legitimately keep mutable test state.
//
// Caveat: `var x = make(map[K]V)` has an initializer (the `make` call)
// and is therefore allowed by this lint. It is still a smell — make()
// returns a mutable container that callers will populate later. If
// such a pattern appears we tighten the rule then; for now the lint
// targets the most common manifestation (zero-value declarations) and
// accepts the make() blind spot.
func scanForHandlerState(roots []string) ([]handlerStateViolation, error) {
	var violations []handlerStateViolation
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
			for _, decl := range f.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.VAR {
					continue
				}
				for _, spec := range gen.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					// Initializer present → effectively constant or table; allowed.
					if len(vs.Values) > 0 {
						continue
					}
					// All names are blank `_` → interface assertion (rare without
					// a value, but covered for completeness).
					allBlank := true
					for _, n := range vs.Names {
						if n.Name != "_" {
							allBlank = false
							break
						}
					}
					if allBlank {
						continue
					}
					names := make([]string, len(vs.Names))
					for i, n := range vs.Names {
						names[i] = n.Name
					}
					pos := fset.Position(spec.Pos())
					violations = append(violations, handlerStateViolation{
						File:  path,
						Line:  pos.Line,
						Names: names,
					})
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return violations, nil
}

// TestNoCrossCallHandlerState pins CLAUDE.md "Stateless STDIO tools —
// each MCP call is a fresh operation." Package-level mutable state in
// handler files lets one MCP call's data leak into the next, breaking
// the canonical recovery contract (every call computes its envelope
// from scratch).
//
// Scope: internal/tools/ only — that's the MCP handler package. Other
// packages (workflow/, ops/) have their own state-management contracts
// and are not handlers.
//
// The lint flags zero-value var declarations (`var x int`, `var m
// map[K]V`); initialized vars (regex, lookup tables, interface
// assertions, literals) are allowed because they don't accumulate
// per-call state.
func TestNoCrossCallHandlerState(t *testing.T) {
	t.Parallel()

	roots := []string{"../tools"}

	violations, err := scanForHandlerState(roots)
	if err != nil {
		t.Fatalf("scanForHandlerState: %v", err)
	}
	for _, v := range violations {
		t.Errorf(
			"package-level zero-value var in handler — %s:%d %v\n"+
				"\t→ MCP handlers must be stateless across calls\n"+
				"\t→ move the field into the handler context (Engine, request input)\n"+
				"\t→ see CLAUDE.md Conventions: \"Stateless STDIO tools\"",
			v.File, v.Line, v.Names,
		)
	}
}

// TestHandlerStateScanner_FiresOnFixture is the lint engine's self-test:
// TestNoCrossCallHandlerState only proves the production tree is clean.
// If the AST walker misses ValueSpec, miscounts initializer presence, or
// errs on multi-name declarations, a future regression would slip
// silently. This fixture asserts every forbidden form is flagged.
func TestHandlerStateScanner_FiresOnFixture(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package fixture

// Zero-value scalar — forbidden.
var counter int

// Zero-value pointer — forbidden.
var lastReq *Request

// Zero-value map — forbidden (will be assigned at runtime).
var cache map[string]int

// Multi-name zero-value — forbidden, both names.
var first, second int

// Var block with a forbidden zero-value spec.
var (
	pending []string
)

type Request struct{}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	violations, err := scanForHandlerState([]string{dir})
	if err != nil {
		t.Fatalf("scanForHandlerState: %v", err)
	}
	// Expect 5 violations: counter, lastReq, cache, (first,second), pending.
	// `var first, second int` is one ValueSpec with two names → one violation.
	if len(violations) != 5 {
		t.Fatalf("expected 5 violations, got %d: %+v", len(violations), violations)
	}

	// Each declared name must show up across the violations.
	wantNames := map[string]bool{
		"counter": false, "lastReq": false, "cache": false,
		"first": false, "second": false, "pending": false,
	}
	for _, v := range violations {
		for _, n := range v.Names {
			if _, ok := wantNames[n]; ok {
				wantNames[n] = true
			}
		}
	}
	for n, fired := range wantNames {
		if !fired {
			t.Errorf("scanner did not flag name %q in fixture; got: %+v", n, violations)
		}
	}
}

// TestHandlerStateScanner_AllowedFormsClean proves vars that ARE
// effectively constants / lookup tables / interface assertions /
// literals do NOT trip the lint. These are the patterns the existing
// internal/tools/ code uses today.
func TestHandlerStateScanner_AllowedFormsClean(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package fixture

import (
	"encoding/json"
	"regexp"
)

// Compile-time interface assertion — allowed.
var _ json.Marshaler = (*FlexBool)(nil)

// Var block of interface assertions — allowed.
var (
	_ json.Marshaler   = (*FlexBool)(nil)
	_ json.Unmarshaler = (*FlexBool)(nil)
)

// Compiled regex — allowed (initialised, effectively immutable).
var pathRe = regexp.MustCompile(` + "`" + `^/api/[a-z]+$` + "`" + `)

// Lookup-table map literal — allowed (initialised).
var validStatuses = map[string]bool{
	"PENDING": true,
	"RUNNING": true,
	"DONE":    true,
}

// String slice literal — allowed.
var requiredFields = []string{"id", "name", "status"}

// String literal constant — allowed.
var defaultMessage = "ready"

// Numeric literal — allowed.
var maxItems = 100

type FlexBool bool
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	violations, err := scanForHandlerState([]string{dir})
	if err != nil {
		t.Fatalf("scanForHandlerState: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected zero violations from initialized-only fixture, got %d: %+v",
			len(violations), violations)
	}
}
