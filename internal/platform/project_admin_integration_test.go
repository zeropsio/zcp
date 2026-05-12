package platform_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestMockProjectAdmin_GetIntegrationStatus_DefaultsToNotConfigured ensures
// uninhabited serviceIDs return IntegrationNotConfigured — the canonical
// default state per Phase A B.1 finding.
func TestMockProjectAdmin_GetIntegrationStatus_DefaultsToNotConfigured(t *testing.T) {
	t.Parallel()
	m := platform.NewMockProjectAdminClient()
	got, err := m.GetServiceStackIntegrationStatus(context.Background(), "svc-unknown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.State != platform.IntegrationNotConfigured {
		t.Errorf("state: got %q want %q", got.State, platform.IntegrationNotConfigured)
	}
}

// TestMockProjectAdmin_GetIntegrationStatus_ReturnsConfigured verifies the
// mock surfaces configured statuses for serviceIDs it was seeded with.
func TestMockProjectAdmin_GetIntegrationStatus_ReturnsConfigured(t *testing.T) {
	t.Parallel()
	want := platform.IntegrationStatus{
		State:              platform.IntegrationConfigured,
		Provider:           platform.IntegrationProviderGitHub,
		RepositoryFullName: "krls2020/myapp",
		EventType:          platform.IntegrationEventTag,
		TagRegex:           "^v\\d+\\.\\d+\\.\\d+$",
		ZeropsYamlSetup:    "prod",
		IsActive:           true,
	}
	m := platform.NewMockProjectAdminClient().WithIntegrationStatus("svc-app", want)
	got, err := m.GetServiceStackIntegrationStatus(context.Background(), "svc-app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %+v want %+v", got, want)
	}
}

// TestMockProjectAdmin_GetIntegrationStatus_CapturesServiceIDs verifies
// per-call serviceID capture used by handler tests asserting call order.
func TestMockProjectAdmin_GetIntegrationStatus_CapturesServiceIDs(t *testing.T) {
	t.Parallel()
	m := platform.NewMockProjectAdminClient()
	ctx := context.Background()
	_, _ = m.GetServiceStackIntegrationStatus(ctx, "svc-app")
	_, _ = m.GetServiceStackIntegrationStatus(ctx, "svc-api")
	_, _ = m.GetServiceStackIntegrationStatus(ctx, "svc-app")
	want := []string{"svc-app", "svc-api", "svc-app"}
	if len(m.CapturedIntegrationStatusServices) != len(want) {
		t.Fatalf("capture length: got %d want %d", len(m.CapturedIntegrationStatusServices), len(want))
	}
	for i, v := range m.CapturedIntegrationStatusServices {
		if v != want[i] {
			t.Errorf("capture[%d]: got %q want %q", i, v, want[i])
		}
	}
}

// TestMockProjectAdmin_GetIntegrationStatus_AfterClose enforces the
// ProjectAdminClient.Close() contract — calls after Close MUST return
// ErrClientClosed (matches the real client per project_admin.go).
func TestMockProjectAdmin_GetIntegrationStatus_AfterClose(t *testing.T) {
	t.Parallel()
	m := platform.NewMockProjectAdminClient()
	m.Close()
	_, err := m.GetServiceStackIntegrationStatus(context.Background(), "svc-app")
	if !errors.Is(err, platform.ErrClientClosed) {
		t.Errorf("got %v want ErrClientClosed", err)
	}
}

// TestMockProjectAdmin_GetIntegrationStatus_ErrorInjection verifies
// WithIntegrationStatusError surfaces the configured error.
func TestMockProjectAdmin_GetIntegrationStatus_ErrorInjection(t *testing.T) {
	t.Parallel()
	want := errors.New("boom")
	m := platform.NewMockProjectAdminClient().WithIntegrationStatusError(want)
	_, err := m.GetServiceStackIntegrationStatus(context.Background(), "svc-app")
	if !errors.Is(err, want) {
		t.Errorf("got %v want %v", err, want)
	}
}

// TestIntegrationState_Constants pins the canonical state string values
// surfaced over the MCP wire — agents may branch on these in atom logic
// and changes need to be intentional.
func TestIntegrationState_Constants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"not-configured", string(platform.IntegrationNotConfigured), "not-configured"},
		{"configured", string(platform.IntegrationConfigured), "configured"},
		{"provider-github", string(platform.IntegrationProviderGitHub), "github"},
		{"provider-gitlab", string(platform.IntegrationProviderGitLab), "gitlab"},
		{"event-tag", string(platform.IntegrationEventTag), "TAG"},
		{"event-branch", string(platform.IntegrationEventBranch), "BRANCH"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.got != tc.want {
				t.Errorf("got %q want %q", tc.got, tc.want)
			}
		})
	}
}
