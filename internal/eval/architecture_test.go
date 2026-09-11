package eval

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestEvalIsolation_CoreDoesNotImportEval and
// TestEvalIsolation_EvalDoesNotImportUpperLayers pin the eval subsystem's
// isolation (docs/spec-eval-farm.md §3.1 FM-17: "the same discipline as
// ZCP_AUTHORING") in Go, alongside the matching .golangci.yaml::depguard
// rules (core-not-eval, eval-no-upper-layers) — duplicated deliberately so
// a regression is caught even where depguard is skipped or misconfigured.
// Mirrors internal/topology/architecture_test.go (the 4-layer rules) and
// internal/captureinspector/architecture_test.go (the cold-tool boundary).

const (
	evalModulePath = "github.com/zeropsio/zcp"
	evalImportPath = evalModulePath + "/internal/eval"
)

// TestEvalIsolation_CoreDoesNotImportEval is core-not-eval: only
// internal/eval/** and cmd/zcp/eval*.go may compose the eval subsystem
// (behavioral verdicts, the farm controller, observer, console —
// docs/spec-eval-farm.md). Test files are exempt — they are not part of
// the composition contract.
func TestEvalIsolation_CoreDoesNotImportEval(t *testing.T) {
	t.Parallel()
	root := evalRepositoryRoot(t)
	violations, err := scanEvalImports(root, func(relative, imported string) bool {
		if imported != evalImportPath && !strings.HasPrefix(imported, evalImportPath+"/") {
			return false
		}
		if strings.HasSuffix(relative, "_test.go") {
			return false
		}
		if strings.HasPrefix(relative, "internal/eval/") {
			return false
		}
		if filepath.Dir(relative) == "cmd/zcp" && strings.HasPrefix(filepath.Base(relative), "eval") {
			return false
		}
		return true
	})
	if err != nil {
		t.Fatalf("scan eval importers: %v", err)
	}
	for _, v := range violations {
		t.Errorf("%s imports %q; only internal/eval/** and cmd/zcp/eval*.go may import the eval subsystem (.golangci.yaml core-not-eval)", v.file, v.imported)
	}
}

// TestEvalIsolation_EvalDoesNotImportUpperLayers is eval-no-upper-layers:
// non-test files under internal/eval/** must not import internal/tools,
// internal/server, internal/authoring or internal/captureinspector — a
// deny list of the upper layers, deliberately not a strict allow-list (the
// subsystem legitimately uses many internal and external packages). Test
// files are exempt: internal/eval/verification_vocab_test.go imports
// internal/server to derive the tool vocabulary.
func TestEvalIsolation_EvalDoesNotImportUpperLayers(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve eval root: %v", err)
	}
	upperLayers := []string{
		evalModulePath + "/internal/tools",
		evalModulePath + "/internal/server",
		evalModulePath + "/internal/authoring",
		evalModulePath + "/internal/captureinspector",
	}
	violations, err := scanEvalImports(root, func(relative, imported string) bool {
		if strings.HasSuffix(relative, "_test.go") {
			return false
		}
		for _, upper := range upperLayers {
			if imported == upper || strings.HasPrefix(imported, upper+"/") {
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatalf("scan eval dependencies: %v", err)
	}
	for _, v := range violations {
		t.Errorf("%s imports %q; internal/eval/** must not import upper layers (.golangci.yaml eval-no-upper-layers)", v.file, v.imported)
	}
}

type evalImportViolation struct {
	file     string
	imported string
}

// scanEvalImports walks root and reports every import forbidden reports
// true for, as (file relative to root, imported path). ImportsOnly parsing
// keeps this cheap even over the whole repository.
func scanEvalImports(root string, forbidden func(relative, imported string) bool) ([]evalImportViolation, error) {
	violations := []evalImportViolation{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && shouldSkipEvalScanDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		parsed, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		for _, spec := range parsed.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if forbidden(relative, imported) {
				violations = append(violations, evalImportViolation{file: relative, imported: imported})
			}
		}
		return nil
	})
	return violations, err
}

func shouldSkipEvalScanDir(name string) bool {
	switch name {
	case ".git", ".cache", ".claude", "node_modules", "tmp", "vendor", "bin":
		return true
	default:
		return false
	}
}

func evalRepositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}
