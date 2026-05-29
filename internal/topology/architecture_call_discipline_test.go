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
// (ops.ListProjectServices / ops.LookupService / ops.FetchServiceEnv /
// inventory.FetchProjectEnvs) own caching, retries, classification, and
// instrumentation that would be lost if a caller goes around them.
//
// GetProjectEnv joined the set with RC2 (env-lifecycle): tools must read the
// project layer via inventory.FetchProjectEnvs so the single project-read path
// (and its future typed availability) can't be bypassed by a raw client call.
var forbiddenDirectClientMethods = map[string]bool{
	"ListServices":  true,
	"GetServiceEnv": true,
	"GetProjectEnv": true,
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
	return scanForMethodCalls(roots, forbiddenDirectClientMethods)
}

// scanForMethodCalls is the shared AST engine: it walks Go files under roots
// (skipping *_test.go) and returns one violation per <expr>.<method>(...) call
// where method ∈ methods. Used by scanForDirectClientCalls (tools/eval/cmd
// client-call discipline) and by the GetAppVersionUserData single-caller pin.
func scanForMethodCalls(roots []string, methods map[string]bool) ([]directClientCallViolation, error) {
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
				if !methods[sel.Sel.Name] {
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
func (fakeClient) GetProjectEnv(ctx context.Context, projectID string) (map[string]string, error) {
	return nil, nil
}

func use(ctx context.Context) {
	var c fakeClient
	_, _ = c.ListServices(ctx, "p1")
	_, _ = c.GetServiceEnv(ctx, "s1")
	_, _ = c.GetProjectEnv(ctx, "p1")
}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	violations, err := scanForDirectClientCalls([]string{dir})
	if err != nil {
		t.Fatalf("scanForDirectClientCalls: %v", err)
	}
	if len(violations) != 3 {
		t.Fatalf("expected 3 violations (ListServices + GetServiceEnv + GetProjectEnv), got %d: %+v",
			len(violations), violations)
	}

	saw := map[string]bool{}
	for _, v := range violations {
		saw[v.Method] = true
	}
	for _, want := range []string{"ListServices", "GetServiceEnv", "GetProjectEnv"} {
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

// TestGetAppVersionUserData_SingleCanonicalCaller pins the RC1 invariant: the
// raw app-version userData mapper (client.GetAppVersionUserData) — which
// classifies the SDK superset into genuine run.envVariables and derives
// Sensitive — must have EXACTLY ONE caller, ops.AppVersionEnvVars in
// env_effective.go. Any other caller would re-read the raw userDataList and
// bypass the classification (re-surfacing intrinsics/ZEROPS_YAML or losing the
// Sensitive derivation — the F7/E6 bug class). Consumers route through the
// gated+classified ops.AppVersionEnvVars, never the raw client method.
func TestGetAppVersionUserData_SingleCanonicalCaller(t *testing.T) {
	t.Parallel()

	roots := []string{"../ops", "../tools", "../workflow", "../eval", "../../cmd"}
	violations, err := scanForMethodCalls(roots, map[string]bool{"GetAppVersionUserData": true})
	if err != nil {
		t.Fatalf("scanForMethodCalls: %v", err)
	}
	for _, v := range violations {
		// The single canonical caller lives in ops/env_effective.go
		// (ops.AppVersionEnvVars). Everything else is forbidden.
		if strings.HasSuffix(v.File, "env_effective.go") {
			continue
		}
		t.Errorf(
			"forbidden raw GetAppVersionUserData call — %s:%d\n"+
				"\t→ route through ops.AppVersionEnvVars (gated + RC1-classified); never the raw client method\n"+
				"\t→ re-reading the raw userDataList bypasses classifyAppVersionUserData (F7/E6)",
			v.File, v.Line,
		)
	}
}

// TestGetAppVersionUserDataScanner_FiresOnFixture is the self-test for the
// single-caller pin's engine: a synthetic raw call must be flagged.
func TestGetAppVersionUserDataScanner_FiresOnFixture(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := `package fixture

import "context"

type fakeClient struct{}

func (fakeClient) GetAppVersionUserData(ctx context.Context, id string) ([]string, error) {
	return nil, nil
}

func use(ctx context.Context) {
	var c fakeClient
	_, _ = c.GetAppVersionUserData(ctx, "av1")
}
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	violations, err := scanForMethodCalls([]string{dir}, map[string]bool{"GetAppVersionUserData": true})
	if err != nil {
		t.Fatalf("scanForMethodCalls: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 GetAppVersionUserData violation, got %d: %+v", len(violations), violations)
	}
}
