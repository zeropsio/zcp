package topology_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// These tests pin the C3 state-namespace contract
// (docs/spec-authoring-boundary.md §3): under the shared `.zcp/state/`
// root, authoring owns the `port/` + `port-recipes/` namespaces and core
// owns everything else (`work/`, `sessions/`, `services/`, `develop`, …);
// neither side reads or writes the other's. The pin is mechanical: every
// `filepath.Join(stateDir, <component>, …)` call site is scanned and its
// first path component judged against the owning side. Joins whose first
// component is not statically resolvable (variable, expression) are out
// of scope — the namespace constants are how both sides build state
// paths in practice.

// authoringStateNamespaces enumerates the state-dir subtrees the
// authoring domain owns. Extending it is a deliberate contract change —
// update docs/spec-authoring-boundary.md C3 alongside.
var authoringStateNamespaces = map[string]bool{
	"port":         true, // PortSession sidecars (.zcp/state/port/{pid}.json)
	"port-recipes": true, // capture-stage emitted recipe output
}

func TestAuthoringBoundary_StateNamespaces(t *testing.T) {
	t.Parallel()

	// Authoring side: every stateDir join must land in an authoring-owned
	// namespace.
	authoringJoins, err := scanStateDirJoins("../authoring")
	if err != nil {
		t.Fatalf("scan authoring: %v", err)
	}
	if len(authoringJoins) == 0 {
		t.Error("expected at least one stateDir join in authoring (the port session sidecar) — scanner regression?")
	}
	for _, j := range authoringJoins {
		if !authoringStateNamespaces[j.Component] {
			t.Errorf("%s:%d joins stateDir with %q — authoring owns only %v (C3, docs/spec-authoring-boundary.md)",
				j.File, j.Line, j.Component, sortedKeys(authoringStateNamespaces))
		}
	}

	// Core side: no stateDir join may land in an authoring-owned namespace.
	coreJoins, err := scanStateDirJoinsExcluding("..", "authoring")
	if err != nil {
		t.Fatalf("scan core: %v", err)
	}
	if len(coreJoins) == 0 {
		t.Error("expected stateDir joins in core (work sessions, service metas) — scanner regression?")
	}
	for _, j := range coreJoins {
		if authoringStateNamespaces[j.Component] {
			t.Errorf("%s:%d joins stateDir with authoring-owned namespace %q — core must not touch it (C3, docs/spec-authoring-boundary.md)",
				j.File, j.Line, j.Component)
		}
	}
}

// stateDirJoin is one `filepath.Join(stateDir, <literal-or-const>, …)`
// call site with its resolved first path component.
type stateDirJoin struct {
	File      string
	Line      int
	Component string
}

// scanStateDirJoins walks root's production .go files (tests excluded —
// the contract is about production behavior) and returns every
// filepath.Join call whose first argument is an identifier named
// "stateDir" and whose second argument resolves to a string (literal, or
// a string const declared anywhere under the same root).
func scanStateDirJoins(root string) ([]stateDirJoin, error) {
	return scanStateDirJoinsExcluding(root, "")
}

func scanStateDirJoinsExcluding(root, excludeSubdir string) ([]stateDirJoin, error) {
	fset := token.NewFileSet()
	var files []*ast.File
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if excludeSubdir != "" && d.Name() == excludeSubdir && filepath.Dir(path) == filepath.Clean(root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		files = append(files, f)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// First pass: collect single-literal string consts so a namespace
	// constant (portSessionDirName, workSessionDirName) resolves.
	consts := map[string]string{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			decl, ok := n.(*ast.GenDecl)
			if !ok || decl.Tok != token.CONST {
				return true
			}
			for _, spec := range decl.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
					continue
				}
				lit, ok := vs.Values[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if v, err := strconv.Unquote(lit.Value); err == nil {
					consts[vs.Names[0].Name] = v
				}
			}
			return true
		})
	}

	// Second pass: collect the joins.
	var joins []stateDirJoin
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Join" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "filepath" {
				return true
			}
			arg0, ok := call.Args[0].(*ast.Ident)
			if !ok || arg0.Name != "stateDir" {
				return true
			}
			component := ""
			switch a1 := call.Args[1].(type) {
			case *ast.BasicLit:
				if a1.Kind == token.STRING {
					if v, err := strconv.Unquote(a1.Value); err == nil {
						component = v
					}
				}
			case *ast.Ident:
				component = consts[a1.Name]
			}
			if component == "" {
				return true // not statically resolvable — out of scope
			}
			pos := fset.Position(call.Pos())
			joins = append(joins, stateDirJoin{File: pos.Filename, Line: pos.Line, Component: component})
			return true
		})
	}
	return joins, nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// TestStateNamespaceScanner_FiresOnFixture — scanner self-test (house
// style: a lint test must prove it can fire). The fixture joins stateDir
// with a literal, a resolvable const, and an unresolvable variable; the
// scanner must report exactly the first two.
func TestStateNamespaceScanner_FiresOnFixture(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := `package fixture

import "path/filepath"

const fixtureDirName = "work"

func paths(stateDir, dynamic string) []string {
	return []string{
		filepath.Join(stateDir, "port"),
		filepath.Join(stateDir, fixtureDirName, "x.json"),
		filepath.Join(stateDir, dynamic),
		filepath.Join(dir2(), "port"),
	}
}

func dir2() string { return "" }
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	joins, err := scanStateDirJoins(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(joins) != 2 {
		t.Fatalf("scanner must flag exactly the literal + const joins, got %+v", joins)
	}
	if joins[0].Component != "port" || joins[1].Component != "work" {
		t.Fatalf("resolved components wrong: %+v", joins)
	}
}
