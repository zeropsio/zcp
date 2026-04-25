package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestLookupService_NotFound(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s-app", Name: "appdev"},
	})

	_, err := LookupService(context.Background(), mock, "p1", "missing")
	if err == nil {
		t.Fatal("expected error for missing hostname")
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *platform.PlatformError, got %T (%v)", err, err)
	}
	if pe.Code != platform.ErrServiceNotFound {
		t.Errorf("code = %q, want %q", pe.Code, platform.ErrServiceNotFound)
	}
	// Suggestion text must list the actual project hostnames — that's
	// the wording every caller relies on for parity with FindService.
	if pe.Suggestion == "" {
		t.Errorf("suggestion is empty; expected 'Available services: ...'")
	}
}

func TestLookupService_Found(t *testing.T) {
	t.Parallel()
	want := platform.ServiceStack{ID: "s-app", Name: "appdev"}
	mock := platform.NewMock().WithServices([]platform.ServiceStack{want})

	got, err := LookupService(context.Background(), mock, "p1", "appdev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.ID != want.ID {
		t.Errorf("got %+v, want service ID %q", got, want.ID)
	}
}

func TestListProjectServices_Passthrough(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s-app", Name: "appdev"},
		{ID: "s-db", Name: "db"},
	})

	services, err := ListProjectServices(context.Background(), mock, "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(services) != 2 {
		t.Errorf("expected 2 services, got %d", len(services))
	}
}
