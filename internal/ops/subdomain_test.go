// Tests for: plans/analysis/ops.md § subdomain
package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestSubdomain(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		{ID: "svc-2", Name: "db", ProjectID: "proj-1"},
	}

	tests := []struct {
		name       string
		mock       *platform.Mock
		hostname   string
		action     string
		wantStatus string
		wantProc   bool
		wantErr    string
	}{
		{
			name:     "Enable_Success",
			mock:     platform.NewMock().WithServices(services),
			hostname: "api",
			action:   "enable",
			wantProc: true,
		},
		{
			name:     "Disable_Success",
			mock:     platform.NewMock().WithServices(services),
			hostname: "api",
			action:   "disable",
			wantProc: true,
		},
		{
			name: "Enable_AlreadyEnabled",
			mock: platform.NewMock().WithServices(services).
				WithError("EnableSubdomainAccess", &platform.PlatformError{
					Code:    "SUBDOMAIN_ALREADY_ENABLED",
					Message: "subdomain already enabled",
				}),
			hostname:   "api",
			action:     "enable",
			wantStatus: "already_enabled",
		},
		{
			name: "Disable_AlreadyDisabled",
			mock: platform.NewMock().WithServices(services).
				WithError("DisableSubdomainAccess", &platform.PlatformError{
					Code:    "SUBDOMAIN_ALREADY_DISABLED",
					Message: "subdomain already disabled",
				}),
			hostname:   "api",
			action:     "disable",
			wantStatus: "already_disabled",
		},
		{
			name:     "InvalidAction",
			mock:     platform.NewMock().WithServices(services),
			hostname: "api",
			action:   "toggle",
			wantErr:  platform.ErrInvalidParameter,
		},
		{
			name:     "ServiceNotFound",
			mock:     platform.NewMock().WithServices(services),
			hostname: "missing",
			action:   "enable",
			wantErr:  platform.ErrServiceNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Subdomain(context.Background(), tt.mock, "proj-1", tt.hostname, tt.action)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				pe, ok := err.(*platform.PlatformError)
				if !ok {
					t.Fatalf("expected *PlatformError, got %T: %v", err, err)
				}
				if pe.Code != tt.wantErr {
					t.Fatalf("expected code %s, got %s", tt.wantErr, pe.Code)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Hostname != tt.hostname {
				t.Errorf("expected hostname=%s, got %s", tt.hostname, result.Hostname)
			}
			if result.Action != tt.action {
				t.Errorf("expected action=%s, got %s", tt.action, result.Action)
			}
			if tt.wantStatus != "" && result.Status != tt.wantStatus {
				t.Errorf("expected status=%s, got %s", tt.wantStatus, result.Status)
			}
			if tt.wantProc && result.Process == nil {
				t.Error("expected non-nil process")
			}
		})
	}
}
