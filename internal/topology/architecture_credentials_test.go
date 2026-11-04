package topology_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// credentialScanRoots are the production trees whose Zerops calls the
// credential guards below read: every Zerops call zcp makes lives under them.
var credentialScanRoots = []string{"../../internal", "../../cmd"}

// scanProductionGo parses every non-test Go file under credentialScanRoots
// and hands each to visit with its path and file set.
func scanProductionGo(t *testing.T, visit func(path string, fset *token.FileSet, f *ast.File)) {
	t.Helper()
	fset := token.NewFileSet()
	for _, root := range credentialScanRoots {
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
			visit(path, fset, f)
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", root, err)
		}
	}
}

// stringLiterals reports every string literal of f whose value contains sub.
func stringLiterals(fset *token.FileSet, f *ast.File, sub string) []token.Position {
	var at []token.Position
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if v, err := strconv.Unquote(lit.Value); err == nil && strings.Contains(v, sub) {
			at = append(at, fset.Position(lit.Pos()))
		}
		return true
	})
	return at
}

// TestNoOrgMemberListRead: zcp reads no organisation-wide member list.
// GET /client/{org}/user/list answers a project key 200 with no rows
// (ZEROPS-055); the persons a project key may read come from
// POST /client-user/search, and the Mate's server, not zcp, reads them for
// its access list (docs/spec-mate.md §3.9).
func TestNoOrgMemberListRead(t *testing.T) {
	t.Parallel()
	scanProductionGo(t, func(path string, fset *token.FileSet, f *ast.File) {
		for _, at := range stringLiterals(fset, f, "/user/list") {
			t.Errorf("%s:%d reads the organisation's member list (/user/list); a project key reads persons with POST /client-user/search", path, at.Line)
		}
	})
}

// integrationTokenBodyOwners are the only functions that may build an
// integration-token mint body (body.ClientIntegrationToken): projectKeyBody,
// the one shape of every project key zcp mints (organisation NO_ACCESS,
// BASIC_USER on exactly one project), and MintLaunchKey, the eval farm's
// stand-in for the token of the person who presses launch, minted with the
// farm operator's own token and revoked after the run.
var integrationTokenBodyOwners = map[string]bool{
	"projectKeyBody": true,
	"MintLaunchKey":  true,
}

// TestNoDelegatedLaunchToken: zcp mints no delegated launch token. A new
// production project is created with the token of the person who presses
// launch, used for that one call; zcp reads no token delegation and builds an
// integration-token mint body only where integrationTokenBodyOwners says.
func TestNoDelegatedLaunchToken(t *testing.T) {
	t.Parallel()
	scanProductionGo(t, func(path string, fset *token.FileSet, f *ast.File) {
		for _, at := range stringLiterals(fset, f, "/delegation") {
			t.Errorf("%s:%d reads a token delegation; zcp mints no delegated launch token", path, at.Line)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				sel, ok := lit.Type.(*ast.SelectorExpr)
				if ok && sel.Sel.Name == "ClientIntegrationToken" && !integrationTokenBodyOwners[fn.Name.Name] {
					t.Errorf("%s:%d %s builds an integration-token mint body; a project key comes from projectKeyBody (BASIC_USER on one project)",
						path, fset.Position(lit.Pos()).Line, fn.Name.Name)
				}
				return true
			})
		}
	})
}
