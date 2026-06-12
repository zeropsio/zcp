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

// These tests pin the authoring boundary (docs/spec-authoring-boundary.md):
// internal/authoring/ is the maintainer-only authoring domain (recipe
// engine, recipe-repo publish lifecycle, run-analysis harness), env-gated
// behind ZCP_AUTHORING and structurally severable from the end-user
// product. Three laws, each with the same effect as a .golangci.yaml
// depguard rule — the duplication is deliberate (same rationale as
// TestArchitectureLayering): a boundary regression is caught even if
// depguard is disabled or misconfigured.
//
//	L1 — core never imports authoring (composition root: internal/server).
//	L2 — authoring imports core only through the enumerated allowlist.
//	L3 — the authoring→workflow edge is identifier-pinned (workflow is
//	     the most-refactored core package; every identifier authoring
//	     consumes from it is a deliberate, visible contract entry).

// TestAuthoringBoundary_CoreDoesNotImportAuthoring — L1. Walks all of
// internal/ except the authoring subtree itself and the composition
// root (internal/server, which constructs the store and registers the
// gated tools), plus the repo-root test harnesses (integration/, e2e/)
// — they sit outside internal/ so the depguard glob misses them, but a
// direct authoring import there would couple the harness to the domain
// and break its severability; both exercise authoring only through the
// composed server (gate env), never by import.
func TestAuthoringBoundary_CoreDoesNotImportAuthoring(t *testing.T) {
	t.Parallel()
	for _, rule := range []layerRule{
		{
			name:          "core-not-authoring",
			rootDir:       "", // ".." relative to internal/topology/ = all of internal/
			excludeSubdir: []string{"authoring", "server"},
			deny: []string{
				"github.com/zeropsio/zcp/internal/authoring",
			},
			reason: "core must not import the authoring domain; only internal/server composes it (L1, docs/spec-authoring-boundary.md)",
		},
		{
			name:    "integration-not-authoring",
			rootDir: "../integration",
			deny: []string{
				"github.com/zeropsio/zcp/internal/authoring",
			},
			reason: "the integration harness reaches authoring only through the composed server (L1, docs/spec-authoring-boundary.md)",
		},
		{
			name:    "e2e-not-authoring",
			rootDir: "../e2e",
			deny: []string{
				"github.com/zeropsio/zcp/internal/authoring",
			},
			reason: "the e2e harness reaches authoring only through the composed server (L1, docs/spec-authoring-boundary.md)",
		},
	} {
		rule.check(t)
	}
}

// authoringImportAllowlist enumerates every import prefix the authoring
// domain may use (L2). Extending it is a deliberate contract change —
// update docs/spec-authoring-boundary.md and the matching depguard
// rule (.golangci.yaml::authoring-allowlist) alongside.
var authoringImportAllowlist = []string{
	"github.com/zeropsio/zcp/internal/authoring", // self + sub-packages
	"github.com/zeropsio/zcp/internal/topology",
	"github.com/zeropsio/zcp/internal/schema",
	"github.com/zeropsio/zcp/internal/knowledge",
	"github.com/zeropsio/zcp/internal/platform",
	"github.com/zeropsio/zcp/internal/workflow",
	"github.com/zeropsio/zcp/internal/sync",
}

// TestAuthoringBoundary_AuthoringImportsAllowlistedOnly — L2. The
// inverse direction of L1: collect every zcp-internal import under
// internal/authoring/ and fail any that is not on the allowlist.
// Third-party and stdlib imports are out of scope here (depguard's
// strict list-mode covers them).
func TestAuthoringBoundary_AuthoringImportsAllowlistedOnly(t *testing.T) {
	t.Parallel()
	violations, err := scanImportsOutsideAllowlist("../authoring", "github.com/zeropsio/zcp/", authoringImportAllowlist)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, v := range violations {
		t.Errorf("%s imports %q — not on the authoring allowlist (L2, docs/spec-authoring-boundary.md); adding an edge is a deliberate contract change", v.File, v.Import)
	}
}

// authoringWorkflowIdentifierAllowlist enumerates every identifier the
// authoring domain may reference from internal/workflow (L3).
// Production files only — test fixtures may use session-setup helpers.
var authoringWorkflowIdentifierAllowlist = map[string]bool{
	"CanonicalEnvFolders": true, // analyze: ghost-env-folder detection (single owner)
	"RecipeState":         true, // publish: v2 close-gate session shape
	"LoadSessionByID":     true, // publish: close-gate session load
	"ListSessions":        true, // publish: live-session discovery
}

// TestAuthoringBoundary_WorkflowIdentifierAllowlist — L3. Workflow is
// the most-refactored core package; this pin makes "does my workflow
// refactor break authoring?" answerable by diffing one allowlist.
func TestAuthoringBoundary_WorkflowIdentifierAllowlist(t *testing.T) {
	t.Parallel()
	violations, err := scanPackageSelectorsOutsideAllowlist(
		"../authoring",
		"github.com/zeropsio/zcp/internal/workflow",
		"workflow",
		authoringWorkflowIdentifierAllowlist,
	)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, v := range violations {
		t.Errorf("%s uses workflow.%s — not on the authoring→workflow identifier allowlist (L3, docs/spec-authoring-boundary.md)", v.File, v.Import)
	}
}

// boundaryViolation is one offending import or selector reference.
type boundaryViolation struct {
	File   string
	Import string
}

// scanImportsOutsideAllowlist walks root and returns every import that
// starts with modulePrefix but matches no allowlist entry (exact or
// "<entry>/" prefix).
func scanImportsOutsideAllowlist(root, modulePrefix string, allowlist []string) ([]boundaryViolation, error) {
	var violations []boundaryViolation
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			ipath := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(ipath, modulePrefix) {
				continue
			}
			allowed := false
			for _, a := range allowlist {
				if ipath == a || strings.HasPrefix(ipath, a+"/") {
					allowed = true
					break
				}
			}
			if !allowed {
				violations = append(violations, boundaryViolation{File: path, Import: ipath})
			}
		}
		return nil
	})
	return violations, err
}

// scanPackageSelectorsOutsideAllowlist walks root's PRODUCTION files
// (skips _test.go), and for every file importing pkgPath under alias
// (or defaultAlias when unaliased) collects selector references
// `alias.Identifier` not present in the allowlist.
func scanPackageSelectorsOutsideAllowlist(root, pkgPath, defaultAlias string, allowlist map[string]bool) ([]boundaryViolation, error) {
	var violations []boundaryViolation
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		alias := ""
		for _, imp := range f.Imports {
			if strings.Trim(imp.Path.Value, `"`) != pkgPath {
				continue
			}
			alias = defaultAlias
			if imp.Name != nil {
				alias = imp.Name.Name
			}
		}
		if alias == "" {
			return nil // file does not import the pinned package
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != alias {
				return true
			}
			if !allowlist[sel.Sel.Name] {
				violations = append(violations, boundaryViolation{File: path, Import: sel.Sel.Name})
			}
			return true
		})
		return nil
	})
	return violations, err
}

// TestBoundaryImportScanner_FiresOnFixture — scanner self-test (house
// style: a lint test must prove it can fire). Parses an in-memory
// fixture importing a non-allowlisted core package and asserts the
// matcher logic flags it.
func TestBoundaryImportScanner_FiresOnFixture(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := `package fixture

import (
	_ "github.com/zeropsio/zcp/internal/ops"
	_ "github.com/zeropsio/zcp/internal/topology"
)
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	violations, err := scanImportsOutsideAllowlist(dir, "github.com/zeropsio/zcp/", authoringImportAllowlist)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(violations) != 1 || violations[0].Import != "github.com/zeropsio/zcp/internal/ops" {
		t.Fatalf("scanner must flag exactly the ops import, got %+v", violations)
	}
}

// TestBoundarySelectorScanner_FiresOnFixture — self-test for the L3
// identifier scanner: an off-allowlist selector fires, an allowlisted
// one and a _test.go file do not.
func TestBoundarySelectorScanner_FiresOnFixture(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := `package fixture

import "github.com/zeropsio/zcp/internal/workflow"

var _ = workflow.RecipeState{}
var _ = workflow.Engine{}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// Same content in a _test.go file must be skipped.
	if err := os.WriteFile(filepath.Join(dir, "fixture_test.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	violations, err := scanPackageSelectorsOutsideAllowlist(dir, "github.com/zeropsio/zcp/internal/workflow", "workflow", authoringWorkflowIdentifierAllowlist)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(violations) != 1 || violations[0].Import != "Engine" {
		t.Fatalf("scanner must flag exactly workflow.Engine in the production file, got %+v", violations)
	}
}
