package workflow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

// loadAtomReferenceFieldIndex scans the response/envelope packages and
// returns a set of "pkg.Type.Field" strings (e.g. "ops.DeployResult.Status")
// that atom references-fields entries can resolve to. Called by
// TestAtomReferenceFieldIntegrity to prove every cited field exists as a
// named struct field.
//
// Embedded fields and type aliases are NOT included — an atom that cites
// an embedded field will fail, prompting the author to either rename the
// reference or refactor the type. This is deliberate: the set of resolvable
// references should be exactly what an agent sees in response JSON.
func loadAtomReferenceFieldIndex(roots []string) (map[string]bool, error) {
	out := map[string]bool{}
	fset := token.NewFileSet()

	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return parseErr
			}
			pkgName := file.Name.Name
			ast.Inspect(file, func(n ast.Node) bool {
				ts, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok || st.Fields == nil {
					return true
				}
				for _, fld := range st.Fields.List {
					for _, name := range fld.Names {
						out[pkgName+"."+ts.Name.Name+"."+name.Name] = true
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return out, nil
}

// atomReferenceFieldRoots is the canonical list of source roots scanned
// by loadAtomReferenceFieldIndex. Kept as a package var so tests can
// override when exercising partial scans.
var atomReferenceFieldRoots = []string{
	"../../internal/ops",
	"../../internal/tools",
	"../../internal/platform",
	"../../internal/workflow",
}
