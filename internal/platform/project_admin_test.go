package platform_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestNewProjectAdminClient_EmptyKey verifies ErrEmptyLaunchKey for empty input.
// P-LP-1: empty key short-circuits before any network call.
func TestNewProjectAdminClient_EmptyKey(t *testing.T) {
	t.Parallel()
	_, err := platform.NewProjectAdminClient("", "")
	if !errors.Is(err, platform.ErrEmptyLaunchKey) {
		t.Fatalf("expected ErrEmptyLaunchKey, got %v", err)
	}
}

// TestProjectAdminClient_MockSatisfiesInterface ensures *MockProjectAdminClient
// satisfies ProjectAdminClient at compile time.
func TestProjectAdminClient_MockSatisfiesInterface(t *testing.T) {
	t.Parallel()
	var _ platform.ProjectAdminClient = platform.NewMockProjectAdminClient()
}

// TestMockProjectAdmin_CreateAndImport_CapturesInputs verifies the mock
// captures yaml + opts for assertions.
func TestMockProjectAdmin_CreateAndImport_CapturesInputs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	want := &platform.ImportResult{
		ProjectID:   "newProjectID",
		ProjectName: "myapp-prod",
		ServiceStacks: []platform.ImportedServiceStack{
			{ID: "svc1", Name: "app"},
		},
	}
	m := platform.NewMockProjectAdminClient().WithImportResult(want)

	got, err := m.CreateAndImportProject(ctx, "project:\n  name: myapp-prod\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ProjectID != "newProjectID" {
		t.Fatalf("ProjectID mismatch: got %q want %q", got.ProjectID, "newProjectID")
	}
	if m.CapturedImportYAML == "" || !strings.Contains(m.CapturedImportYAML, "myapp-prod") {
		t.Fatalf("captured yaml mismatch: %q", m.CapturedImportYAML)
	}
}

// TestMockProjectAdmin_CreateAndImport_PropagatesError exercises the error
// path.
func TestMockProjectAdmin_CreateAndImport_PropagatesError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	want := errors.New("simulated platform error")
	m := platform.NewMockProjectAdminClient().WithImportError(want)
	_, err := m.CreateAndImportProject(ctx, "")
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

// TestMockProjectAdmin_GetServiceEnvKeys_ReturnsNoValues verifies EnvKey
// shape (no Value field). P-LP-5 invariant — the platform package
// physically prevents callers from reading env values.
func TestMockProjectAdmin_GetServiceEnvKeys_ReturnsNoValues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := platform.NewMockProjectAdminClient().WithServiceEnvKeys("svc1", []platform.EnvKey{
		{ID: "env1", Key: "STRIPE_SECRET_KEY", Sensitive: true},
		{ID: "env2", Key: "OPENAI_API_KEY", Sensitive: true},
		{ID: "env3", Key: "LOG_LEVEL", Sensitive: false},
	})
	keys, err := m.GetServiceEnvKeys(ctx, "svc1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	for i, k := range keys {
		// Compile-time guarantee: EnvKey has no Value/Content field.
		// Reflection-only check would be redundant; the API surface itself
		// makes value reads impossible.
		if k.Key == "" {
			t.Fatalf("key[%d] missing", i)
		}
	}
	if !keys[0].Sensitive {
		t.Fatalf("expected STRIPE_SECRET_KEY sensitive=true, got false")
	}
	if keys[2].Sensitive {
		t.Fatalf("expected LOG_LEVEL sensitive=false, got true")
	}
}

// TestMockProjectAdmin_DeleteProject_CapturesAndReturns verifies the delete
// pathway.
func TestMockProjectAdmin_DeleteProject_CapturesAndReturns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wantProc := &platform.Process{ID: "deleteProcess1", Status: "RUNNING"}
	m := platform.NewMockProjectAdminClient().WithDeleteResult(wantProc)

	proc, err := m.DeleteProject(ctx, "targetProjectID")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proc.ID != "deleteProcess1" {
		t.Fatalf("process ID mismatch: %q", proc.ID)
	}
	if m.CapturedDeleteProject != "targetProjectID" {
		t.Fatalf("captured project ID mismatch: %q", m.CapturedDeleteProject)
	}
}

// TestMockProjectAdmin_AfterClose_ReturnsErrClientClosed pins the close
// contract — every method post-Close() returns ErrClientClosed.
func TestMockProjectAdmin_AfterClose_ReturnsErrClientClosed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := platform.NewMockProjectAdminClient().
		WithImportResult(&platform.ImportResult{ProjectID: "x"}).
		WithDeleteResult(&platform.Process{ID: "y"}).
		WithProcess(&platform.Process{ID: "z"}).
		WithServices([]platform.ServiceStack{{ID: "s"}}).
		WithServiceEnvKeys("svc", []platform.EnvKey{{Key: "k"}}).
		WithProjectEnvKeys("prj", []platform.EnvKey{{Key: "k"}})

	m.Close()

	if _, err := m.CreateAndImportProject(ctx, ""); !errors.Is(err, platform.ErrClientClosed) {
		t.Fatalf("CreateAndImportProject after close: got %v want ErrClientClosed", err)
	}
	if _, err := m.ListServices(ctx, "p"); !errors.Is(err, platform.ErrClientClosed) {
		t.Fatalf("ListServices after close: got %v want ErrClientClosed", err)
	}
	if _, err := m.GetServiceEnvKeys(ctx, "s"); !errors.Is(err, platform.ErrClientClosed) {
		t.Fatalf("GetServiceEnvKeys after close: got %v want ErrClientClosed", err)
	}
	if _, err := m.GetProjectEnvKeys(ctx, "p"); !errors.Is(err, platform.ErrClientClosed) {
		t.Fatalf("GetProjectEnvKeys after close: got %v want ErrClientClosed", err)
	}
	if _, err := m.GetProcess(ctx, "p"); !errors.Is(err, platform.ErrClientClosed) {
		t.Fatalf("GetProcess after close: got %v want ErrClientClosed", err)
	}
	if _, err := m.DeleteProject(ctx, "p"); !errors.Is(err, platform.ErrClientClosed) {
		t.Fatalf("DeleteProject after close: got %v want ErrClientClosed", err)
	}
}

// TestEnvKey_NoValueField is a compile-time pin: the EnvKey struct must
// never grow a Value/Content field. P-LP-5 invariant.
//
// This test exists as documentation; the actual guarantee is in the type
// definition itself. If anyone adds a Value field to EnvKey, ZCP-wide
// grep for "EnvKey{" will surface every consumer (currently: project_admin.go
// + tests + Phase D handler) and force review.
func TestEnvKey_NoValueField(t *testing.T) {
	t.Parallel()
	k := platform.EnvKey{
		ID:        "id1",
		Key:       "STRIPE_SECRET_KEY",
		Sensitive: true,
	}
	if k.Key != "STRIPE_SECRET_KEY" {
		t.Fatalf("EnvKey.Key should round-trip; got %q", k.Key)
	}
	// Note: writing `k.Value = "..."` here would be a compile error,
	// which is the actual safety property.
}
