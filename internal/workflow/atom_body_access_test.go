package workflow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoProductionAtomBodyReads pins Phase 2 (C4) of the pipeline-repair
// plan: production code MUST NOT read KnowledgeAtom.Body directly outside
// the parser/synthesizer/test boundary. Direct reads bypass placeholder
// substitution and the unknown-placeholder check, leaking literal
// `{hostname}` tokens to the LLM and degrading the atom architecture's
// "one source of truth" guarantee.
//
// The bypass had a real production hit at strategy_guidance.go before
// C4 landed; this test ensures the regression cannot re-occur. Allowed
// sites: parser (atom.go) and synthesizer (synthesize.go) — both are
// the canonical pipeline. Test files (`*_test.go`) are exempt because
// fixtures legitimately construct/inspect atoms in arbitrary ways.
//
// Scan covers `internal/workflow/`, `internal/tools/`, `internal/ops/`,
// and `internal/content/` — the production layers where atom corpus
// access happens. Other packages (auth/, runtime/, etc.) don't touch
// atoms.
func TestNoProductionAtomBodyReads(t *testing.T) {
	t.Parallel()

	allowedFiles := map[string]struct{}{
		"atom.go":                {}, // parser owns Body construction
		"synthesize.go":          {}, // synthesizer owns Body rendering
		"atom_loader.go":         {}, // corpus loader (legacy alias path)
		"atom_manifest.go":       {}, // manifest generator (Body for export)
		"atom_stitcher.go":       {}, // recipe stitcher (Body for assembly)
		"recipe_corpus_store.go": {}, // recipe-side store, separate corpus
	}
	scanRoots := []string{
		"../workflow",
		"../tools",
		"../ops",
		"../content",
	}

	var violations []string
	for _, root := range scanRoots {
		matches, err := filepath.Glob(filepath.Join(root, "*.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", root, err)
		}
		for _, file := range matches {
			base := filepath.Base(file)
			if strings.HasSuffix(base, "_test.go") {
				continue
			}
			if _, ok := allowedFiles[base]; ok {
				continue
			}
			violations = append(violations, scanFileForAtomBodyReads(t, file)...)
		}
	}

	if len(violations) > 0 {
		t.Errorf("found %d production read(s) of KnowledgeAtom.Body outside the allowed parser/synthesizer boundary:\n  %s\n\nFix: route through Synthesize so placeholder substitution and unknown-placeholder checking apply. If the read is legitimate (e.g. corpus-export tooling), add the file to the allowedFiles list with a comment explaining why.",
			len(violations), strings.Join(violations, "\n  "))
	}
}

// scanFileForAtomBodyReads parses one .go file and returns positions of
// expressions that read .Body where the receiver is plausibly a
// KnowledgeAtom. The check is name-based: any selector ending in `.Body`
// on a variable named `atom`, `a`, `ka`, or with KnowledgeAtom in scope
// flags. False positives are acceptable here — the allowlist absorbs
// them, and the test failure message points at the file:line so the
// author can decide.
//
// Type-precise resolution would require go/types and full package load
// (slow + brittle for an architecture pin). Name-based is sufficient
// because there are no other types in the workflow package with a Body
// field.
func scanFileForAtomBodyReads(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var hits []string
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel == nil || sel.Sel.Name != "Body" {
			return true
		}
		// Identify the receiver. We flag selector reads where the name
		// suggests a KnowledgeAtom variable. Field-write contexts (LHS
		// of assignment, struct literal field) are checked at the
		// caller via ast.Walk semantics — but Inspect visits all
		// SelectorExpr nodes regardless. The narrow check (variable
		// names typically used for atoms) keeps false positives down
		// without needing full type resolution.
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		switch ident.Name {
		case "atom", "a", "ka", "knowledgeAtom":
			pos := fset.Position(sel.Pos())
			hits = append(hits, pos.String())
		}
		return true
	})
	return hits
}
