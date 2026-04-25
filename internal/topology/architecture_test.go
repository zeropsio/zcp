package topology_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestArchitectureLayering pins the 4-layer dependency rules documented in
// CLAUDE.md (Architecture section) and docs/spec-architecture.md. Each rule
// has the same effect as the matching .golangci.yaml::depguard rule — the
// duplication is deliberate so a layering regression is caught even if
// depguard ever gets disabled or misconfigured.
//
//	Layer 2 (topology/) — stdlib + 3rd-party only.
//	Layer 1 (platform/) — stdlib + 3rd-party only.
//	Layer 3 peers (ops/, workflow/) — must not import each other or upper layers.
//
// Sub-package internal/ops/checks/ is exempt from the ops-not-workflow rule
// while recipe v2 still owns the legacy check types it imports from workflow.
func TestArchitectureLayering(t *testing.T) {
	t.Parallel()

	rules := []layerRule{
		{
			name:    "topology-stdlib-only",
			rootDir: "topology",
			deny: []string{
				"github.com/zeropsio/zcp/internal/",
			},
			reason: "topology/ is layer-2 vocabulary; imports stdlib + 3rd-party only",
		},
		{
			name:    "platform-stdlib-only",
			rootDir: "platform",
			deny: []string{
				"github.com/zeropsio/zcp/internal/",
			},
			reason: "platform/ is layer 1; no internal/ imports allowed",
		},
		{
			name:    "ops-not-workflow",
			rootDir: "ops",
			// Sub-package ops/checks/ keeps workflow access while recipe v2
			// still owns the StepCheck types it produces. The narrower
			// "ops-checks-legacy" rule below still pins the rest of the
			// deny list (tools/, recipe/) for that subtree.
			excludeSubdir: []string{"checks"},
			deny: []string{
				"github.com/zeropsio/zcp/internal/workflow",
				"github.com/zeropsio/zcp/internal/tools",
				"github.com/zeropsio/zcp/internal/recipe",
			},
			reason: "ops/ and workflow/ are peer layer-3 packages; share types via topology/",
		},
		{
			name:    "ops-checks-legacy",
			rootDir: "ops/checks",
			// workflow is intentionally permitted here — the StepCheck
			// types live in workflow/ and ops/checks/ produces them.
			// Once recipe v2 is deleted this exception goes away.
			deny: []string{
				"github.com/zeropsio/zcp/internal/tools",
				"github.com/zeropsio/zcp/internal/recipe",
			},
			reason: "ops/checks/ is layer 3 (recipe v2 legacy); tools/ and recipe/ remain forbidden",
		},
		{
			name:    "workflow-not-ops",
			rootDir: "workflow",
			deny: []string{
				"github.com/zeropsio/zcp/internal/ops",
				"github.com/zeropsio/zcp/internal/tools",
				"github.com/zeropsio/zcp/internal/recipe",
			},
			reason: "workflow/ is layer 3; must not depend on ops/, tools/, or recipe/",
		},
	}

	for _, rule := range rules {
		t.Run(rule.name, func(t *testing.T) {
			t.Parallel()
			rule.check(t)
		})
	}
}

type layerRule struct {
	name          string
	rootDir       string   // e.g. "topology" → ../topology relative to this file
	excludeSubdir []string // sub-directories under rootDir to skip
	deny          []string // import path prefixes that must not appear
	reason        string
}

func (r layerRule) check(t *testing.T) {
	t.Helper()
	root := filepath.Join("..", r.rootDir)
	excludes := make(map[string]bool, len(r.excludeSubdir))
	for _, d := range r.excludeSubdir {
		excludes[d] = true
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if rel != "." {
				// Match the FIRST path segment against excludes — that's
				// the immediate child of rootDir.
				first := rel
				if i := strings.IndexByte(first, filepath.Separator); i >= 0 {
					first = first[:i]
				}
				if excludes[first] {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		// Self-imports (a `_test.go` file with `package x_test` importing
		// its own production package or a sub-package of it) are always
		// allowed — they're standard Go test mechanics, not a layering
		// concern.
		ownRoot := "github.com/zeropsio/zcp/internal/" + r.rootDir
		for _, imp := range f.Imports {
			ipath := strings.Trim(imp.Path.Value, `"`)
			if ipath == ownRoot || strings.HasPrefix(ipath, ownRoot+"/") {
				continue
			}
			for _, denied := range r.deny {
				// Match if denied ends with "/" (prefix match) or matches exactly.
				if strings.HasSuffix(denied, "/") {
					if strings.HasPrefix(ipath, denied) {
						t.Errorf("%s: %s imports %q (forbidden by rule %q: %s)",
							r.name, path, ipath, r.name, r.reason)
					}
				} else if ipath == denied || strings.HasPrefix(ipath, denied+"/") {
					t.Errorf("%s: %s imports %q (forbidden by rule %q: %s)",
						r.name, path, ipath, r.name, r.reason)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}
