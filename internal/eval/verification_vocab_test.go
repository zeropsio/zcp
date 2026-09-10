package eval

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// TestVerificationVocab_DerivedFromRegistries_RejectsUnknownName pins
// FM-33: every tool name / action value / error code a `never`/`askWhen`
// entry can use is derived from the server's own registries at test time —
// the same discipline internal/content/eval_scenario_drift_test.go already
// applies to scenario front matter — never a hand-maintained copy. A
// scenario naming something that doesn't exist in those registries fails
// this lint; the real corpus must pass.
func TestVerificationVocab_DerivedFromRegistries_RejectsUnknownName(t *testing.T) {
	repoRoot := vocabRepoRoot(t)
	vocab := loadVerificationVocab(t, repoRoot)

	t.Run("real corpus is clean", func(t *testing.T) {
		for _, dir := range []string{
			filepath.Join(repoRoot, "eval", "behavioral", "scenarios"),
			filepath.Join(repoRoot, "eval", "behavioral", "scenarios-local"),
		} {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue // scenarios-local may not exist on every checkout
			}
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
					continue
				}
				full := filepath.Join(dir, e.Name())
				sc, parseErr := ParseScenario(full)
				if parseErr != nil {
					// Scenarios with unrendered {{...}} placeholders or other
					// per-scenario concerns are out of this lint's scope —
					// this test only grades verification: vocab, not general
					// parseability (which other tests already cover).
					continue
				}
				if violations := vocab.lintScenario(sc); len(violations) != 0 {
					t.Errorf("%s: verification vocab drift:\n%s", full, strings.Join(violations, "\n"))
				}
			}
		}
	})

	t.Run("bogus tool name in never is rejected", func(t *testing.T) {
		dir := t.TempDir()
		bad := `---
id: vocab-lint-bogus-tool
seed: empty
verification:
  mode: observe
  never: [zerops_this_tool_does_not_exist]
---
Do the thing.
`
		path := filepath.Join(dir, "bad.md")
		if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		sc, err := ParseScenario(path)
		if err != nil {
			t.Fatalf("ParseScenario: %v", err)
		}
		violations := vocab.lintScenario(sc)
		if len(violations) == 0 {
			t.Fatal("expected a vocab violation for an unregistered tool name, got none")
		}
		if !strings.Contains(violations[0], "zerops_this_tool_does_not_exist") {
			t.Errorf("violation should name the offending tool, got %q", violations[0])
		}
	})

	t.Run("bogus error code in askWhen is rejected", func(t *testing.T) {
		dir := t.TempDir()
		bad := `---
id: vocab-lint-bogus-error-code
seed: empty
verification:
  mode: observe
  askWhen: [NOT_A_REAL_ERROR_CODE]
---
Do the thing.
`
		path := filepath.Join(dir, "bad.md")
		if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		sc, err := ParseScenario(path)
		if err != nil {
			t.Fatalf("ParseScenario: %v", err)
		}
		violations := vocab.lintScenario(sc)
		if len(violations) == 0 {
			t.Fatal("expected a vocab violation for an unregistered error code, got none")
		}
	})

	t.Run("bogus spec section is rejected", func(t *testing.T) {
		dir := t.TempDir()
		bad := `---
id: vocab-lint-bogus-spec-section
seed: empty
verification:
  mode: observe
  spec: spec-workflows.md §999.999 (does not exist)
---
Do the thing.
`
		path := filepath.Join(dir, "bad.md")
		if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		sc, err := ParseScenario(path)
		if err != nil {
			t.Fatalf("ParseScenario: %v", err)
		}
		violations := vocab.lintScenario(sc)
		if len(violations) == 0 {
			t.Fatal("expected a vocab violation for a nonexistent spec section, got none")
		}
	})
}

// verificationVocab is the set of names derived from the server's own
// registries at test time (FM-33): registered MCP tool names, `action`
// argument values found in every tool handler's own dispatch switch, and
// declared platform error codes.
type verificationVocab struct {
	repoRoot    string
	toolNames   map[string]bool
	actionNames map[string]bool
	errorCodes  map[string]bool
}

func loadVerificationVocab(t *testing.T, repoRoot string) verificationVocab {
	t.Helper()
	return verificationVocab{
		repoRoot:    repoRoot,
		toolNames:   deriveRegisteredToolNames(t),
		actionNames: deriveDispatchedActionValues(t, repoRoot),
		errorCodes:  deriveDeclaredErrorCodes(t, repoRoot),
	}
}

// lintScenario checks sc.Verification's never/askWhen/spec fields against
// the derived vocab and returns one violation message per offending entry.
func (v verificationVocab) lintScenario(sc *Scenario) []string {
	var violations []string
	if sc.Verification == nil {
		return nil
	}
	for _, expr := range sc.Verification.Never {
		shape, err := ParseCallShape(expr)
		if err != nil {
			violations = append(violations, fmt.Sprintf("never: unparseable expression %q: %v", expr, err))
			continue
		}
		if !v.toolNames[shape.Tool] {
			violations = append(violations, fmt.Sprintf("never: unregistered tool %q in expression %q", shape.Tool, expr))
		}
		if action, ok := shape.Args["action"]; ok && !v.actionNames[action] {
			violations = append(violations, fmt.Sprintf("never: unrecognized action %q in expression %q", action, expr))
		}
	}
	for _, code := range sc.Verification.AskWhen {
		if !v.errorCodes[code] {
			violations = append(violations, fmt.Sprintf("askWhen: unrecognized error code %q", code))
		}
	}
	if sc.Verification.Spec != "" {
		if err := v.checkSpecPointer(sc.Verification.Spec); err != nil {
			violations = append(violations, fmt.Sprintf("spec: %v", err))
		}
	}
	return violations
}

// specPointerPattern matches "<spec-file>.md §<section>" — the FM-27
// pointer shape. Anything trailing after the section number (parenthetical
// commentary) is ignored.
var specPointerPattern = regexp.MustCompile(`^(spec-[A-Za-z0-9_-]+\.md)\s+§(\d+(?:\.\d+)*)`)

// checkSpecPointer verifies the named spec file exists under docs/ and
// contains a "§<section>" anchor (FM-27: the drift lint checks only that
// the named section exists, never that assertions match its prose).
func (v verificationVocab) checkSpecPointer(pointer string) error {
	match := specPointerPattern.FindStringSubmatch(pointer)
	if match == nil {
		return fmt.Errorf("spec pointer %q doesn't match `<spec-file>.md §<section>`", pointer)
	}
	file, section := match[1], match[2]
	path := filepath.Join(v.repoRoot, "docs", file)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("spec file %q not found: %w", file, err)
	}
	if !strings.Contains(string(data), "§"+section) {
		return fmt.Errorf("docs/%s has no §%s anchor", file, section)
	}
	return nil
}

// vocabRepoRoot walks up from the working directory to find go.mod.
func vocabRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod) above " + dir)
		}
		dir = parent
	}
}

// vocabNopMounter/vocabNopSSH are inert stand-ins for ops.Mounter/
// ops.SSHDeployer — deriveRegisteredToolNames only needs a server that can
// enumerate its tools, never call them.
type vocabNopMounter struct{}

func (vocabNopMounter) CheckMount(context.Context, string) (platform.MountState, error) {
	return platform.MountStateNotMounted, nil
}
func (vocabNopMounter) Mount(context.Context, string, string) error        { return nil }
func (vocabNopMounter) Unmount(context.Context, string, string) error      { return nil }
func (vocabNopMounter) ForceUnmount(context.Context, string, string) error { return nil }
func (vocabNopMounter) IsWritable(context.Context, string) (bool, error)   { return false, nil }
func (vocabNopMounter) ListMountDirs(context.Context, string) ([]string, error) {
	return nil, nil
}
func (vocabNopMounter) HasUnit(context.Context, string) (bool, error) { return false, nil }
func (vocabNopMounter) CleanupUnit(context.Context, string) error     { return nil }

type vocabNopSSH struct{}

func (vocabNopSSH) ExecSSH(context.Context, string, string) ([]byte, error) { return nil, nil }
func (vocabNopSSH) ExecSSHBackground(context.Context, string, string, time.Duration) ([]byte, error) {
	return nil, nil
}

var _ ops.Mounter = vocabNopMounter{}
var _ ops.SSHDeployer = vocabNopSSH{}

// deriveRegisteredToolNames builds a real in-memory MCP server (default
// runtime.Info, not authoring/container-gated) and lists its tools — the
// actual registry an agent sees, never a hand-maintained copy.
func deriveRegisteredToolNames(t *testing.T) map[string]bool {
	t.Helper()
	mock := platform.NewMock().WithProject(&platform.Project{ID: "p1", Name: "test"}).WithServices(nil)
	authInfo := &auth.Info{ProjectID: "p1", Token: "test", APIHost: "localhost"}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	logFetcher := platform.NewMockLogFetcher()
	srv := server.New(context.Background(), mock, authInfo, store, logFetcher, vocabNopSSH{}, vocabNopMounter{}, runtime.Info{}, nil)

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.MCPServer().Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "vocab-lint", Version: "0.1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := make(map[string]bool, len(result.Tools))
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	return names
}

// deriveDispatchedActionValues walks every internal/tools/*.go source file
// and collects the string-literal case labels of any switch on an
// identifier/selector named "Action" or "action" — the actual dispatch
// vocabulary a tool handler recognizes, read from the code itself rather
// than hand-copied into a separate list.
func deriveDispatchedActionValues(t *testing.T, repoRoot string) map[string]bool {
	t.Helper()
	dir := filepath.Join(repoRoot, "internal", "tools")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	actions := make(map[string]bool)
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok || sw.Tag == nil || !switchesOnAction(sw.Tag) {
				return true
			}
			for _, stmt := range sw.Body.List {
				clause, ok := stmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, expr := range clause.List {
					lit, ok := expr.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					value, unquoteErr := strconv.Unquote(lit.Value)
					if unquoteErr == nil {
						actions[value] = true
					}
				}
			}
			return true
		})
	}
	return actions
}

// switchesOnAction reports whether a switch tag expression refers to an
// identifier or selector ending in "Action" or "action" (e.g.
// `input.Action`, `action`), the shape every actioned tool handler in this
// codebase dispatches on.
func switchesOnAction(tag ast.Expr) bool {
	switch t := tag.(type) {
	case *ast.Ident:
		return strings.EqualFold(t.Name, "action")
	case *ast.SelectorExpr:
		return strings.EqualFold(t.Sel.Name, "action")
	default:
		return false
	}
}

// deriveDeclaredErrorCodes parses internal/platform/errors.go's top const
// block and collects every string-literal value assigned to an Err*
// constant — the actual declared error-code catalog
// (internal/platform/errors.go, referenced by CLAUDE.md's map), read from
// source rather than hand-copied.
func deriveDeclaredErrorCodes(t *testing.T, repoRoot string) map[string]bool {
	t.Helper()
	path := filepath.Join(repoRoot, "internal", "platform", "errors.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	codes := make(map[string]bool)
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, value := range valueSpec.Values {
				lit, ok := value.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				code, unquoteErr := strconv.Unquote(lit.Value)
				if unquoteErr == nil {
					codes[code] = true
				}
			}
		}
	}
	return codes
}

// TestVerificationVocab_O6O7O8Fields_Accepted pins that the S8 oracle
// fields (launchShape, artifactPromotion, noFabricatedSecret) parse
// cleanly, while a misspelled field name is rejected
// (rejectUnknownVerificationFields, scenario.go).
func TestVerificationVocab_O6O7O8Fields_Accepted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	good := `---
id: vocab-o6o7o8-good
seed: empty
verification:
  mode: observe
  launchShape:
    prodProject: "zcp-farm-{{runId}}-prod"
  artifactPromotion:
    - from: appdev
      to: appstage
  noFabricatedSecret: true
---
Do the thing.
`
	goodPath := filepath.Join(dir, "good.md")
	if err := os.WriteFile(goodPath, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseScenario(goodPath); err != nil {
		t.Fatalf("expected launchShape/artifactPromotion/noFabricatedSecret to parse cleanly, got: %v", err)
	}

	bad := `---
id: vocab-o6o7o8-bad
seed: empty
verification:
  mode: observe
  launcShape:
    prodProject: "zcp-farm-r1-prod"
---
Do the thing.
`
	badPath := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(badPath, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseScenario(badPath); err == nil {
		t.Fatal("expected a misspelled verification field (launcShape) to be rejected")
	}
}
