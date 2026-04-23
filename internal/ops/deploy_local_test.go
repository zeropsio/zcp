// Tests for: ops/deploy_local.go — DeployLocal via zcli push.
package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
)

// mockRunner is a test mock for commandRunner.
type mockRunner struct {
	lookPathErr error
	runResults  []runResult // consumed in order
	runCalls    []runCall
	callIdx     int
}

type runResult struct {
	stdout string
	stderr string
	err    error
}

type runCall struct {
	name string
	args []string
}

func (m *mockRunner) LookPath(_ string) (string, error) {
	if m.lookPathErr != nil {
		return "", m.lookPathErr
	}
	return "/usr/local/bin/zcli", nil
}

func (m *mockRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	m.runCalls = append(m.runCalls, runCall{name: name, args: args})
	if m.callIdx >= len(m.runResults) {
		return "", "", nil
	}
	r := m.runResults[m.callIdx]
	m.callIdx++
	return r.stdout, r.stderr, r.err
}

func localTestAuth() auth.Info {
	return auth.Info{
		Token:   "test-token",
		APIHost: "api.app-prg1.zerops.io",
		Region:  "prg1",
	}
}

// tmpWithZeropsYml creates a temp dir with a minimal zerops.yaml (uses .yml for fallback testing).
func tmpWithZeropsYml(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte("zerops:\n  - setup: app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Note: tests that use OverrideRunnerForTest must NOT use t.Parallel()
// because they mutate the global runner variable.

func TestDeployLocal_Success(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{
				ID:   "svc-1",
				Name: "appstage",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName: "nodejs@22",
				},
			},
		})

	mr := &mockRunner{runResults: []runResult{{}, {}}}
	restore := OverrideRunnerForTest(mr)
	defer restore()

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte("zerops:\n  - setup: appstage\n    build:\n      base: nodejs@22\n"), 0o644)

	result, err := DeployLocal(context.Background(), mock, "proj-1", localTestAuth(),
		"appstage", "", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "BUILD_TRIGGERED" {
		t.Errorf("status = %s, want BUILD_TRIGGERED", result.Status)
	}
	if result.Mode != "local" {
		t.Errorf("mode = %s, want local", result.Mode)
	}
	if result.TargetService != "appstage" {
		t.Errorf("targetService = %s, want appstage", result.TargetService)
	}
	if result.TargetServiceID != "svc-1" {
		t.Errorf("targetServiceID = %s, want svc-1", result.TargetServiceID)
	}
	if result.TargetServiceType != "nodejs@22" {
		t.Errorf("targetServiceType = %s, want nodejs@22", result.TargetServiceType)
	}

	// Verify zcli commands.
	if len(mr.runCalls) != 2 {
		t.Fatalf("expected 2 run calls, got %d", len(mr.runCalls))
	}
	if mr.runCalls[0].name != "zcli" || mr.runCalls[0].args[0] != "login" {
		t.Errorf("call[0] = %s %v, want zcli login ...", mr.runCalls[0].name, mr.runCalls[0].args)
	}
	pushArgs := strings.Join(mr.runCalls[1].args, " ")
	if !strings.Contains(pushArgs, "push") {
		t.Errorf("push args should contain push, got: %s", pushArgs)
	}
	if !strings.Contains(pushArgs, "--service-id") || !strings.Contains(pushArgs, "--project-id") {
		t.Errorf("push args should contain --service-id and --project-id for non-interactive mode, got: %s", pushArgs)
	}
	if !strings.Contains(pushArgs, "--no-git") {
		t.Errorf("push args should contain --no-git by default, got: %s", pushArgs)
	}
}

func TestDeployLocal_ZcliNotFound(t *testing.T) {
	mock := platform.NewMock()
	mr := &mockRunner{lookPathErr: fmt.Errorf("not found")}
	restore := OverrideRunnerForTest(mr)
	defer restore()

	_, err := DeployLocal(context.Background(), mock, "proj-1", localTestAuth(),
		"app", "", ".")
	if err == nil {
		t.Fatal("expected error for missing zcli")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrPrerequisiteMissing {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrPrerequisiteMissing)
	}
}

func TestDeployLocal_MissingZeropsYml(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "app"}})

	mr := &mockRunner{}
	restore := OverrideRunnerForTest(mr)
	defer restore()

	dir := t.TempDir() // empty dir

	_, err := DeployLocal(context.Background(), mock, "proj-1", localTestAuth(),
		"app", "", dir)
	if err == nil {
		t.Fatal("expected error for missing zerops.yaml")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidParameter {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrInvalidParameter)
	}
}

func TestDeployLocal_LoginFailed(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "app"}})

	mr := &mockRunner{
		runResults: []runResult{
			{stderr: "Error: login failed\n", err: fmt.Errorf("exit status 1")},
		},
	}
	restore := OverrideRunnerForTest(mr)
	defer restore()

	dir := tmpWithZeropsYml(t)

	_, err := DeployLocal(context.Background(), mock, "proj-1", localTestAuth(),
		"app", "", dir)
	if err == nil {
		t.Fatal("expected error for login failure")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrDeployFailed {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrDeployFailed)
	}
}

func TestDeployLocal_PushFailed(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "app"}})

	mr := &mockRunner{
		runResults: []runResult{
			{}, // login ok
			{stderr: "Error: push failed\nbuild error\n", err: fmt.Errorf("exit status 1")},
		},
	}
	restore := OverrideRunnerForTest(mr)
	defer restore()

	dir := tmpWithZeropsYml(t)

	_, err := DeployLocal(context.Background(), mock, "proj-1", localTestAuth(),
		"app", "", dir)
	if err == nil {
		t.Fatal("expected error for push failure")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrDeployFailed {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrDeployFailed)
	}
}

func TestDeployLocal_NoTargetService(t *testing.T) {
	mock := platform.NewMock()
	mr := &mockRunner{}
	restore := OverrideRunnerForTest(mr)
	defer restore()

	_, err := DeployLocal(context.Background(), mock, "proj-1", localTestAuth(),
		"", "", ".")
	if err == nil {
		t.Fatal("expected error for empty targetService")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidParameter {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrInvalidParameter)
	}
}

func TestDeployLocal_ServiceNotFound(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "other"}})

	mr := &mockRunner{}
	restore := OverrideRunnerForTest(mr)
	defer restore()

	_, err := DeployLocal(context.Background(), mock, "proj-1", localTestAuth(),
		"nonexistent", "", ".")
	if err == nil {
		t.Fatal("expected error for nonexistent service")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrServiceNotFound {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrServiceNotFound)
	}
}
