package tools

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// evalEnvVarMarkers are env-var names that exist ONLY in the eval/live-test
// harness. They must never appear in a STRING LITERAL in production code,
// because such a literal becomes an agent-facing tell (a shell command the
// agent runs) that resolves to nothing on every real-user machine. This is the
// B1 regression class: 8201d826 lifted `echo "$ZCP_E2E_GITHUB_PAT" | gh auth
// login` — the eval agent's improvised recovery command, with the harness-only
// env var — verbatim into build-integration's confirm response, and eval-env
// circularity (the only env that defines the var is the one it shipped in)
// certified it green.
//
// Comments are intentionally NOT scanned (an AST string-literal walk excludes
// them) — documenting the regression by name, like the doc comment on
// ghAuthSetupCommand, is correct and must stay allowed. The live-test harness
// (live_test_harness.go — `api` build tag) is skipped by FILENAME: that is the
// var's home, and a filename predicate survives build-tag retags (the harness
// moved `live`→`api`; a build-tag-string skip would silently stop firing).
var evalEnvVarMarkers = []string{
	"ZCP_E2E_",
}

// evalHarnessFile is the one production file in internal/tools where the eval
// env vars legitimately live as string literals (the live-test harness builds
// platform.Client from ZCP_API_KEY etc.). Skipped by base name so the
// exemption is anchored to identity, not to a build tag that can move.
const evalHarnessFile = "live_test_harness.go"

// evalismsLintDirs are the production source trees whose string literals reach
// the agent (handler responses, ops-built commands, rendered guidance).
var evalismsLintDirs = []string{
	".",
	filepath.Join("..", "ops"),
	filepath.Join("..", "workflow"),
	filepath.Join("..", "topology"),
	filepath.Join("..", "envclass"),
}

// TestNoEvalEnvVarsInAgentFacingStrings pins the B1 class: no production string
// literal may name an eval-harness-only env var. An AST walk (not grep) is the
// right tool — it ships the check at the exact granularity of the defect (a
// string the agent executes) while leaving regression-documenting comments
// untouched.
func TestNoEvalEnvVarsInAgentFacingStrings(t *testing.T) {
	t.Parallel()
	var violations []string
	for _, dir := range evalismsLintDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			// envclass / topology may not exist as siblings in every layout;
			// skip a missing dir rather than fail the whole lint.
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("read dir %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			// Skip the live-test harness by FILENAME — the eval var
			// legitimately lives there. Matching the base name (not a build
			// tag) keeps the exemption stable across tag retags.
			if name == evalHarnessFile {
				continue
			}
			path := filepath.Join(dir, name)
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, src, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				for _, marker := range evalEnvVarMarkers {
					if strings.Contains(lit.Value, marker) {
						violations = append(violations,
							fset.Position(lit.Pos()).String()+": string literal names eval-only env var "+marker)
					}
				}
				return true
			})
		}
	}
	if len(violations) > 0 {
		t.Errorf("eval-harness env var(s) in agent-facing string literals (use a derived, env-aware command instead — see ghAuthSetupCommand):\n%s",
			strings.Join(violations, "\n"))
	}
}
