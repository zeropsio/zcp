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
