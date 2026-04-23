package workflow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoInlineManagedRuntimeIndex enforces the pair-keyed meta invariant
// (docs/spec-workflows.md §8 E8). Any code that builds a hostname→value map
// indexed by `m.StageHostname` is reimplementing what workflow.ManagedRuntimeIndex
// already provides. Inline copies drift — before the consolidation, four
// such copies existed with divergent nuances (some filtered by IsComplete,
// some didn't; some stored bool, some stored the meta pointer).
//
// This test scans internal/workflow and internal/tools for the forbidden
// pattern: an assignment statement of the shape
//
//	<something>[<identifier>.StageHostname] = <value>
//
// and fails with a message citing E8 plus the forbidden location. The only
// whitelisted files are this package's service_meta.go (the canonical
// helper lives there) and its tests (which exercise the helper with literal
// meta fixtures).
//
// To add a new legitimate use: call ManagedRuntimeIndex(metas) or (for a
// single hostname resolution) FindServiceMeta(stateDir, hostname). If you
// believe you truly need a new kind of inline pattern, amend this test
// deliberately and document the exemption — E8 violations cascade into
// scope / auto-close / strategy bugs that are hard to debug downstream.
func TestNoInlineManagedRuntimeIndex(t *testing.T) {
	t.Parallel()

	// Absolute paths are brittle; walk from repo root found via the module cache.
	roots := []string{
		"../../internal/workflow",
		"../../internal/tools",
	}

	// Whitelist files that legitimately reference .StageHostname for index
	// construction: the helper itself and anything inside service_meta.go,
	// plus tests that assert on the helper's behavior with literal fixtures.
	whitelist := map[string]bool{
		filepath.Clean("../../internal/workflow/service_meta.go"):              true,
		filepath.Clean("../../internal/workflow/service_meta_test.go"):         true,
		filepath.Clean("../../internal/workflow/pair_keyed_contract_test.go"):  true,
	}

	var violations []string
	fset := token.NewFileSet()

	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if whitelist[filepath.Clean(path)] {
				return nil
			}
			file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				t.Fatalf("parse %s: %v", path, parseErr)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				assign, ok := n.(*ast.AssignStmt)
				if !ok || len(assign.Lhs) != 1 {
					return true
				}
				idx, ok := assign.Lhs[0].(*ast.IndexExpr)
				if !ok {
					return true
				}
				sel, ok := idx.Index.(*ast.SelectorExpr)
				if !ok || sel.Sel == nil {
					return true
				}
				if sel.Sel.Name != "StageHostname" {
					return true
				}
				pos := fset.Position(assign.Pos())
				violations = append(violations, pos.String())
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	if len(violations) > 0 {
		t.Errorf(
			"pair-keyed meta invariant (spec-workflows.md §8 E8) violated — "+
				"inline ManagedRuntimeIndex reimplementation detected at %d site(s):\n\t%s\n\n"+
				"Fix: call workflow.ManagedRuntimeIndex(metas) for slice→map construction, "+
				"or workflow.FindServiceMeta(stateDir, hostname) for disk lookup.",
			len(violations),
			strings.Join(violations, "\n\t"),
		)
	}
}
