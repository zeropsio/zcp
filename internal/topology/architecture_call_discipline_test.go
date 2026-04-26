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

// directClientCallViolation describes one call expression that bypasses
// the ops/ helpers and reaches the platform client directly.
type directClientCallViolation struct {
	File   string
	Line   int
	Method string
}

// forbiddenDirectClientMethods are the platform.Client methods that
// upper layers (tools/, eval/, cmd/) MUST reach through ops/ helpers
// instead of calling on the client directly. The convention is documented
// in CLAUDE.md ("tools/eval reach platform via ops"); the helpers
// (ops.ListProjectServices / ops.LookupService / ops.FetchServiceEnv)
// own caching, retries, and instrumentation that would be lost if a
// caller goes around them.
var forbiddenDirectClientMethods = map[string]bool{
	"ListServices":  true,
	"GetServiceEnv": true,
}

// scanForDirectClientCalls walks Go files under roots looking for call
// expressions of the form <expr>.<method>(...) where method is a member
// of forbiddenDirectClientMethods. Returns one violation per call site.
//
// Test files (*_test.go) are exempt by design: direct platform setup
// in test helpers is legal (e.g., e2e/helpers_test.go:78 calls
// ListServices to verify probe state).
//
// Production code under the allowed layers (ops/, platform/, workflow/)
// is also legal — those layers OWN the convention. The caller specifies
// roots; a typical call passes only `internal/tools`, `internal/eval`,
// and `cmd` to scan.
func scanForDirectClientCalls(roots []string) ([]directClientCallViolation, error) {
	var violations []directClientCallViolation
	fset := token.NewFileSet()
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if os.IsNotExist(walkErr) {
					return nil // root may not exist (e.g., eval/ before it lands); skip silently
				}
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel == nil {
					return true
				}
				if !forbiddenDirectClientMethods[sel.Sel.Name] {
					return true
				}
				pos := fset.Position(call.Pos())
				violations = append(violations, directClientCallViolation{
					File:   path,
					Line:   pos.Line,
					Method: sel.Sel.Name,
				})
				return true
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return violations, nil
}

// TestNoDirectClientCallsInToolsEvalCmd pins the CLAUDE.md
// "tools/eval reach platform via ops" convention. tools/, eval/, and
// cmd/ MUST NOT call client.ListServices or client.GetServiceEnv
// directly; they go through ops.{ListProjectServices,LookupService,
// FetchServiceEnv} so caching, retries, and instrumentation land at
// one site.
//
// Allowed callers (not scanned): internal/ops/, internal/platform/,
// internal/workflow/. Test files in any layer are exempt.
//
// The matching depguard rule covers IMPORTS; this test covers CALLS,
// catching the "import the package via the workflow allowlist but
// reach a forbidden method" scenario the import lint cannot see.
func TestNoDirectClientCallsInToolsEvalCmd(t *testing.T) {
	t.Parallel()

	// Test file lives in internal/topology/. ../tools, ../eval are
	// siblings; ../../cmd is two levels up from topology.
	roots := []string{
		"../tools",
		"../eval",
		"../../cmd",
	}

	violations, err := scanForDirectClientCalls(roots)
	if err != nil {
		t.Fatalf("scanForDirectClientCalls: %v", err)
	}
	for _, v := range violations {
		t.Errorf(
			"forbidden direct client call — %s:%d uses %q\n"+
				"\t→ route through ops.{ListProjectServices,LookupService,FetchServiceEnv}\n"+
				"\t→ see CLAUDE.md Conventions: \"tools/eval reach platform via ops\"",
			v.File, v.Line, v.Method,
		)
	}
}

// TestNoDirectClientCallsScanner_FiresOnFixture is the lint engine's
// self-test: TestNoDirectClientCallsInToolsEvalCmd above only proves
// the production tree is clean today. If the AST inspector is broken
// (wrong selector pattern, wrong method name list, miscounted nodes),
// the production scan would silently return zero violations and the
// regression-floor would pass — leaving every future violation
// undetected. This fixture asserts the scanner DOES flag a synthetic
// violation, so the scanner itself has coverage.
func TestNoDirectClientCallsScanner_FiresOnFixture(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package fixture

import "context"

type fakeClient struct{}

func (fakeClient) ListServices(ctx context.Context, projectID string) ([]string, error) {
	return nil, nil
}
func (fakeClient) GetServiceEnv(ctx context.Context, serviceID string) (map[string]string, error) {
	return nil, nil
}

func use(ctx context.Context) {
	var c fakeClient
	_, _ = c.ListServices(ctx, "p1")
	_, _ = c.GetServiceEnv(ctx, "s1")
}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	violations, err := scanForDirectClientCalls([]string{dir})
	if err != nil {
		t.Fatalf("scanForDirectClientCalls: %v", err)
	}
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations (ListServices + GetServiceEnv), got %d: %+v",
			len(violations), violations)
	}

	saw := map[string]bool{}
	for _, v := range violations {
		saw[v.Method] = true
	}
	for _, want := range []string{"ListServices", "GetServiceEnv"} {
		if !saw[want] {
			t.Errorf("scanner did not flag method %q in fixture", want)
		}
	}
}

// TestNoDirectClientCallsScanner_TestFilesExempt proves the scanner
// skips *_test.go files. Test setup legitimately uses direct platform
// access (e.g., e2e/helpers_test.go), so the lint exempts them.
func TestNoDirectClientCallsScanner_TestFilesExempt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package fixture

import "context"

type fakeClient struct{}

func (fakeClient) ListServices(ctx context.Context, projectID string) ([]string, error) {
	return nil, nil
}

func TestSomething(_ context.Context) {
	var c fakeClient
	_, _ = c.ListServices(context.Background(), "p1")
}
`
	if err := os.WriteFile(filepath.Join(dir, "scanner_exempt_test.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	violations, err := scanForDirectClientCalls([]string{dir})
	if err != nil {
		t.Fatalf("scanForDirectClientCalls: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected zero violations from *_test.go file, got %d: %+v",
			len(violations), violations)
	}
}

// TestNoDirectClientCallsScanner_NoMatchInCleanFixture asserts the
// scanner does NOT spuriously match clean prose / unrelated method
// names that happen to be similar.
func TestNoDirectClientCallsScanner_NoMatchInCleanFixture(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package fixture

import "context"

type otherClient struct{}

// Methods named close-but-not-exact must not trip the scanner.
func (otherClient) ListServiceStacks(ctx context.Context) ([]string, error) {
	return nil, nil
}
func (otherClient) GetService(ctx context.Context, id string) (string, error) {
	return "", nil
}
func (otherClient) ServicesEnv(ctx context.Context) error {
	return nil
}

func use(ctx context.Context) {
	var c otherClient
	_, _ = c.ListServiceStacks(ctx)
	_, _ = c.GetService(ctx, "s1")
	_ = c.ServicesEnv(ctx)
}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	violations, err := scanForDirectClientCalls([]string{dir})
	if err != nil {
		t.Fatalf("scanForDirectClientCalls: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected zero violations from clean fixture, got %d: %+v",
			len(violations), violations)
	}
}
