// Tests for: plans/analysis/ops.md § delete
package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestDelete(t *testing.T) {
	t.Parallel()

	services := []platform.ServiceStack{
		{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		{ID: "svc-2", Name: "db", ProjectID: "proj-1"},
	}

	tests := []struct {
		name            string
		mock            *platform.Mock
		hostname        string
		confirmHostname string
		wantErr         string
	}{
		{
			name:            "Success",
			mock:            platform.NewMock().WithServices(services),
			hostname:        "api",
			confirmHostname: "api",
		},
		{
			name:            "NoConfirm",
			mock:            platform.NewMock().WithServices(services),
			hostname:        "api",
			confirmHostname: "",
			wantErr:         platform.ErrConfirmRequired,
		},
		{
			name:            "ConfirmMismatch",
			mock:            platform.NewMock().WithServices(services),
			hostname:        "api",
			confirmHostname: "db",
			wantErr:         platform.ErrConfirmRequired,
		},
		{
			name:            "ServiceNotFound",
			mock:            platform.NewMock().WithServices(services),
			hostname:        "missing",
			confirmHostname: "missing",
			wantErr:         platform.ErrServiceNotFound,
		},
		{
			name:            "EmptyHostname",
			mock:            platform.NewMock().WithServices(services),
			hostname:        "",
			confirmHostname: "",
			wantErr:         platform.ErrServiceRequired,
		},
		{
			name: "APIError",
			mock: platform.NewMock().WithServices(services).
				WithError("DeleteService", &platform.PlatformError{
					Code:    platform.ErrAPIError,
					Message: "delete failed",
				}),
			hostname:        "api",
			confirmHostname: "api",
			wantErr:         platform.ErrAPIError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Delete(context.Background(), tt.mock, "proj-1", tt.hostname, tt.confirmHostname)

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
			if result == nil {
				t.Fatal("expected non-nil process")
			}
			if result.ID == "" {
				t.Error("expected non-empty process ID")
			}
		})
	}
}
