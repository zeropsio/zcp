package eval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestDeleteServices_ServiceAlreadyGone_Succeeds(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithError("DeleteService", platform.NewPlatformError(
		platform.ErrServiceNotFound,
		"Service stack not found",
		"",
	))
	services := []platform.ServiceStack{{ID: "svc-app", Name: "app"}}

	if err := deleteServices(context.Background(), mock, services); err != nil {
		t.Fatalf("deleteServices: %v", err)
	}
	if got := mock.CallCounts["DeleteService"]; got != 1 {
		t.Fatalf("DeleteService calls: got %d, want 1", got)
	}
}

func TestDeleteServices_DeleteError_Fails(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithError("DeleteService", errors.New("api unavailable"))
	services := []platform.ServiceStack{{ID: "svc-app", Name: "app"}}

	err := deleteServices(context.Background(), mock, services)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `delete "app": api unavailable`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := mock.CallCounts["DeleteService"]; got != 1 {
		t.Fatalf("DeleteService calls: got %d, want 1", got)
	}
}

// Regression for flow-eval suite 20260503-173119: cleanup aborted on
// "Service stack not found" because the API returned the error via APICode
// rather than HTTP 404 → ErrServiceNotFound. Cleanup must tolerate ALL
// "not found"-class errors so a service that vanished between list and
// delete (concurrent scenarios, retries) doesn't kill the rest of the run.

func TestDeleteServices_NotFoundViaAPICode_Skips(t *testing.T) {
	t.Parallel()
	pe := platform.NewPlatformError(platform.ErrAPIError, "Service stack not found.", "")
	pe.APICode = "serviceStackNotFound"
	mock := platform.NewMock().WithError("DeleteService", pe)
	services := []platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}}

	if err := deleteServices(context.Background(), mock, services); err != nil {
		t.Fatalf("APICode=serviceStackNotFound must be tolerated: %v", err)
	}
}

func TestDeleteServices_NotFoundViaMessage_Skips(t *testing.T) {
	t.Parallel()
	// Some platform paths leave APICode empty and only put "not found" in the
	// message. The message-substring fallback covers that case.
	pe := platform.NewPlatformError(platform.ErrAPIError, "Service stack not found.", "")
	mock := platform.NewMock().WithError("DeleteService", pe)
	services := []platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}}

	if err := deleteServices(context.Background(), mock, services); err != nil {
		t.Fatalf("message-only 'not found' must be tolerated: %v", err)
	}
}

func TestIsServiceAlreadyGone_AllChannels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"err_service_not_found_code", platform.NewPlatformError(platform.ErrServiceNotFound, "x", ""), true},
		{"api_code_serviceStackNotFound", func() error {
			pe := platform.NewPlatformError(platform.ErrAPIError, "x", "")
			pe.APICode = "serviceStackNotFound"
			return pe
		}(), true},
		{"api_code_serviceNotFound", func() error {
			pe := platform.NewPlatformError(platform.ErrAPIError, "x", "")
			pe.APICode = "serviceNotFound"
			return pe
		}(), true},
		{"message_only", platform.NewPlatformError(platform.ErrAPIError, "Service stack not found.", ""), true},
		{"unrelated_error", errors.New("api unavailable"), false},
		{"unrelated_platform_error", platform.NewPlatformError(platform.ErrAPIError, "rate limited", ""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isServiceAlreadyGone(tt.err); got != tt.want {
				t.Errorf("isServiceAlreadyGone(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
