package platform

import (
	"context"
	"errors"
	"testing"
)

// TestMock_GetServiceStackIntegrationStatus pins the Mock contract:
// seeded service IDs return the configured IntegrationStatus; unseeded
// IDs return IntegrationStatus{State: IntegrationNotConfigured} —
// matches the real wrapper's HTTP-400-as-state mapping so cascade
// tests can rely on "no seed = not configured".
func TestMock_GetServiceStackIntegrationStatus(t *testing.T) {
	t.Parallel()

	seeded := IntegrationStatus{
		State:           IntegrationConfigured,
		Provider:        IntegrationProviderGitHub,
		ZeropsYamlSetup: "appdev",
	}
	mock := NewMock().WithIntegrationStatus("svc-1", seeded)

	got, err := mock.GetServiceStackIntegrationStatus(context.Background(), "svc-1")
	if err != nil {
		t.Fatalf("seeded: %v", err)
	}
	if got.ZeropsYamlSetup != "appdev" {
		t.Errorf("seeded: ZeropsYamlSetup = %q, want appdev", got.ZeropsYamlSetup)
	}

	unseeded, err := mock.GetServiceStackIntegrationStatus(context.Background(), "svc-unseeded")
	if err != nil {
		t.Fatalf("unseeded: %v", err)
	}
	if unseeded.State != IntegrationNotConfigured {
		t.Errorf("unseeded state: want NotConfigured, got %q", unseeded.State)
	}

	wantErr := errors.New("boom")
	mockErr := NewMock().WithError("GetServiceStackIntegrationStatus", wantErr)
	if _, err := mockErr.GetServiceStackIntegrationStatus(context.Background(), "svc-x"); !errors.Is(err, wantErr) {
		t.Errorf("error override: want %v, got %v", wantErr, err)
	}
}

// TestMock_GetAppVersionAppCode pins the Mock contract: seeded IDs
// return the URL; unseeded returns empty string (cascade treats empty
// as miss).
func TestMock_GetAppVersionAppCode(t *testing.T) {
	t.Parallel()

	mock := NewMock().WithAppVersionAppCode("ver-1", "https://cdn.example/ver-1.tar.gz")

	got, err := mock.GetAppVersionAppCode(context.Background(), "ver-1")
	if err != nil {
		t.Fatalf("seeded: %v", err)
	}
	if got != "https://cdn.example/ver-1.tar.gz" {
		t.Errorf("seeded URL: got %q", got)
	}

	empty, _ := mock.GetAppVersionAppCode(context.Background(), "ver-unseeded")
	if empty != "" {
		t.Errorf("unseeded: want empty, got %q", empty)
	}
}
