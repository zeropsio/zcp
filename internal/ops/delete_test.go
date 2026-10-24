// Tests for: plans/analysis/ops.md § delete
package ops

import (
	"context"
	"errors"
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
		name     string
		mock     *platform.Mock
		hostname string
		wantErr  string
	}{
		{
			name:     "Success",
			mock:     platform.NewMock().WithServices(services),
			hostname: "api",
		},
		{
			name:     "ServiceNotFound",
			mock:     platform.NewMock().WithServices(services),
			hostname: "missing",
			wantErr:  platform.ErrServiceNotFound,
		},
		{
			name:     "EmptyHostname",
			mock:     platform.NewMock().WithServices(services),
			hostname: "",
			wantErr:  platform.ErrServiceRequired,
		},
		{
			name: "APIError",
			mock: platform.NewMock().WithServices(services).
				WithError("DeleteService", &platform.PlatformError{
					Code:    platform.ErrAPIError,
					Message: "delete failed",
				}),
			hostname: "api",
			wantErr:  platform.ErrAPIError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Delete(context.Background(), tt.mock, "proj-1", tt.hostname)

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

// TestDelete_ByID_NeverResolvesTheName pins that a delete by id deletes
// exactly the id it is handed and reads no service list: the lagging search
// could name another service by the hostname, or none (docs/spec-workflows.md
// §3.5), and a service that later took the hostname has another id.
func TestDelete_ByID_NeverResolvesTheName(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		// The search already lists a new service under the old hostname.
		{ID: "svc-new", Name: "appdev", ProjectID: "proj-1"},
	})

	proc, err := DeleteByID(context.Background(), mock, "svc-old")
	if err != nil {
		t.Fatalf("DeleteByID: %v", err)
	}
	if proc == nil || len(proc.ServiceStacks) != 1 || proc.ServiceStacks[0].ID != "svc-old" {
		t.Fatalf("process = %+v, want the delete of svc-old", proc)
	}
	if n := mock.CallCounts["DeleteService"]; n != 1 {
		t.Errorf("DeleteService calls = %d, want 1", n)
	}
	for _, read := range []string{"ListServices", "ListServicesDirect", "GetService"} {
		if n := mock.CallCounts[read]; n > 0 {
			t.Errorf("%s called %d times: a delete by id resolves nothing", read, n)
		}
	}

	if _, err := DeleteByID(context.Background(), mock, ""); err == nil {
		t.Error("DeleteByID with no id: want an error, never a delete")
	}
	var pe *platform.PlatformError
	failing := platform.NewMock().WithError("DeleteService", &platform.PlatformError{Code: platform.ErrAPIError, Message: "delete failed"})
	if _, err := DeleteByID(context.Background(), failing, "svc-old"); !errors.As(err, &pe) || pe.Code != platform.ErrAPIError {
		t.Errorf("DeleteByID on a failing API = %v, want the API error", err)
	}
}
