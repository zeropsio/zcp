package platform_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// newUserInfoServer serves /api/rest/public/user/info with a
// clientUserList carrying one entry per (clientUserID, clientID) pair, in
// the order given — the shape D11's tests need to prove the configured
// client is targeted regardless of its position in the list.
func newUserInfoServer(t *testing.T, memberships [][2]string) *httptest.Server {
	t.Helper()
	entries := make([]string, 0, len(memberships))
	for _, m := range memberships {
		entries = append(entries, fmt.Sprintf(`{"id":%q,"clientId":%q,"userId":"user-1"}`, m[0], m[1]))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/rest/public/user/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"user-1","email":"a@b.com","fullName":"ZCP","clientUserList":[%s]}`, strings.Join(entries, ","))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestAccountClient_UsesConfiguredClientID_NotFirstMembership pins D11:
// NewProjectAdminClientForClient targets the caller-supplied clientID even
// when it is not ClientUserList[0] — the bug that made
// platform.NewProjectAdminClient (via ClientUserList[0]) post to the wrong
// org for a token that is OWNER in two.
func TestAccountClient_UsesConfiguredClientID_NotFirstMembership(t *testing.T) {
	t.Parallel()
	const wrongOrg, configuredOrg = "org-first-wrong", "org-second-configured"
	srv := newUserInfoServer(t, [][2]string{
		{"cu-wrong", wrongOrg},
		{"cu-configured", configuredOrg},
	})

	client, err := platform.NewProjectAdminClientForClient("token", srv.URL, configuredOrg)
	if err != nil {
		t.Fatalf("NewProjectAdminClientForClient: %v", err)
	}
	t.Cleanup(client.Close)

	if got := client.ClientUserID(); got != "cu-configured" {
		t.Errorf("ClientUserID() = %q, want %q (the configured org's clientUserId, not ClientUserList[0]'s)", got, "cu-configured")
	}
}

// TestAccountClient_TokenNotMemberOfConfiguredOrg_RefusesBeforeWrite pins
// D11's refusal: a token that authenticates but is not a member of the
// configured clientID must be refused (ErrClientNotMember) before the
// caller can make any write with it — no ProjectAdminClient is returned.
func TestAccountClient_TokenNotMemberOfConfiguredOrg_RefusesBeforeWrite(t *testing.T) {
	t.Parallel()
	srv := newUserInfoServer(t, [][2]string{
		{"cu-a", "org-a"},
		{"cu-b", "org-b"},
	})

	_, err := platform.NewProjectAdminClientForClient("token", srv.URL, "org-not-a-member")
	if !errors.Is(err, platform.ErrClientNotMember) {
		t.Fatalf("NewProjectAdminClientForClient error = %v, want ErrClientNotMember", err)
	}
	if !strings.Contains(err.Error(), "org-not-a-member") || !strings.Contains(err.Error(), "org-a") || !strings.Contains(err.Error(), "org-b") {
		t.Errorf("error %q should name the configured org and the orgs the token can see", err.Error())
	}
}
