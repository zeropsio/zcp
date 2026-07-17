package conformance

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
)

// This file is the OFFLINE coverage lint for the declared proof registry
// (proofs.go): docs/spec-dataconsole-testing.md §4 requires every registered
// profile's RequiredProofs to be traceable to a declared, real test — never
// silence. It is untagged (runs in plain `go test ./...`, no live engine
// needed) because every question it answers — "is this proof declared?",
// "does the declared test function exist?", "is the declared base type
// real?" — is answerable from source text and the profile registry alone.

// TestConformanceCoverage_DeclaredProofHasCase is the main lint: every
// registered profile's RequiredProofs must each be covered by at least one
// ConformanceCases row naming that proof and either the profile's own
// BaseType, or — for a profile with a ProvenBy equivalence — the referenced
// base type, after re-verifying (docs/spec-dataconsole-testing.md §2) that
// the referenced profile shares the SAME Family and Support (an equivalence
// is never allowed to silently launder a mismatched profile into "covered").
func TestConformanceCoverage_DeclaredProofHasCase(t *testing.T) {
	profiles := provider.ServiceProfiles()
	index := make(map[string]provider.ServiceProfile, len(profiles))
	for _, p := range profiles {
		index[p.BaseType] = p
	}

	// coveredBaseTypes[proof][baseType] records every (proof, baseType) pair
	// some declared case actually asserts.
	coveredBaseTypes := make(map[ProofID]map[string]bool)
	for _, c := range ConformanceCases {
		if coveredBaseTypes[c.Proof] == nil {
			coveredBaseTypes[c.Proof] = make(map[string]bool)
		}
		for _, bt := range c.BaseTypes {
			coveredBaseTypes[c.Proof][bt] = true
		}
	}

	var gaps []string
	for _, p := range profiles {
		for _, proof := range RequiredProofs(p) {
			if coveredBaseTypes[proof][p.BaseType] {
				continue
			}
			if p.ProvenBy != "" {
				target, ok := index[p.ProvenBy]
				if ok && target.Family == p.Family && target.Support == p.Support && coveredBaseTypes[proof][p.ProvenBy] {
					continue
				}
			}
			gaps = append(gaps, fmt.Sprintf("%s: missing case for proof %q (family=%s support=%s)", p.BaseType, proof, p.Family, p.Support))
		}
	}
	sort.Strings(gaps)
	for _, g := range gaps {
		t.Error(g)
	}
}

// TestConformanceCoverage_DeclaredTestNamesExist is the first anti-drift
// sub-lint: a CaseDecl naming a test function that does not (or no longer)
// exist in this package's source is a silent lie about coverage — parse
// every *_test.go file (including the //go:build e2e ones, which are still
// plain parseable Go source text; go/parser ignores build constraints) and
// require every declared TestName to resolve to a real `func TestXxx(t
// *testing.T)`.
func TestConformanceCoverage_DeclaredTestNamesExist(t *testing.T) {
	declared := make(map[string]bool)
	for _, c := range ConformanceCases {
		declared[c.TestName] = true
	}

	found, err := discoverTestFuncs(".")
	if err != nil {
		t.Fatalf("discoverTestFuncs: %v", err)
	}

	var missing []string
	for name := range declared {
		if !found[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		t.Errorf("CaseDecl names TestName %q, which does not exist as a func %s(t *testing.T) in this package", name, name)
	}
}

// TestConformanceCoverage_DeclaredBaseTypesRegistered is the second
// anti-drift sub-lint: a CaseDecl naming a base type absent from
// provider.ServiceProfiles is either a typo or a stale declaration outliving
// a registry change — either way it cannot actually satisfy coverage for a
// real profile.
func TestConformanceCoverage_DeclaredBaseTypesRegistered(t *testing.T) {
	registered := make(map[string]bool)
	for _, p := range provider.ServiceProfiles() {
		registered[p.BaseType] = true
	}

	var unregistered []string
	for _, c := range ConformanceCases {
		for _, bt := range c.BaseTypes {
			if !registered[bt] {
				unregistered = append(unregistered, fmt.Sprintf("%s: BaseTypes entry %q for proof %q is not a registered ServiceProfile base type", c.TestName, bt, c.Proof))
			}
		}
	}
	sort.Strings(unregistered)
	for _, u := range unregistered {
		t.Error(u)
	}
}

// discoverTestFuncs parses every *_test.go file in dir (regardless of build
// tag — go/parser works from source text, not a build-constrained
// compilation) and returns the set of top-level `func TestXxx(t
// *testing.T)` names declared anywhere in the package.
func discoverTestFuncs(dir string) (map[string]bool, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return nil, fmt.Errorf("glob: %w", err)
	}
	out := make(map[string]bool)
	fset := token.NewFileSet()
	for _, f := range files {
		node, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", f, err)
		}
		for _, decl := range node.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil { // no receiver — top-level funcs only
				continue
			}
			if isTestFunc(fn) {
				out[fn.Name.Name] = true
			}
		}
	}
	return out, nil
}

// isTestFunc reports whether fn has Go's test-function shape: name prefixed
// "Test", exactly one parameter, of type *testing.T.
func isTestFunc(fn *ast.FuncDecl) bool {
	if !isTestName(fn.Name.Name) {
		return false
	}
	params := fn.Type.Params
	if params == nil || len(params.List) != 1 || len(params.List[0].Names) != 1 {
		return false
	}
	star, ok := params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	return ok && pkgIdent.Name == "testing" && sel.Sel.Name == "T"
}

func isTestName(name string) bool {
	const prefix = "Test"
	if len(name) < len(prefix) || name[:len(prefix)] != prefix {
		return false
	}
	return true
}
