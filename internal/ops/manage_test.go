// Tests for: plans/analysis/ops.md § ops/manage.go
package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestStart_Success(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "STOPPED"},
		})

	proc, err := Start(context.Background(), mock, "proj-1", "api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
	if proc.ID == "" {
		t.Error("expected non-empty process ID")
	}
}

func TestStop_Success(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING"},
		})

	proc, err := Stop(context.Background(), mock, "proj-1", "api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestRestart_Success(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING"},
		})

	proc, err := Restart(context.Background(), mock, "proj-1", "api")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proc == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestStart_ServiceNotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING"},
		})

	_, err := Start(context.Background(), mock, "proj-1", "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrServiceNotFound {
		t.Errorf("expected code %s, got %s", platform.ErrServiceNotFound, pe.Code)
	}
}

func TestScale_AllParams(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING"},
		})

	result, err := Scale(context.Background(), mock, "proj-1", "api", ScaleParams{
		CPUMode:       "SHARED",
		MinCPU:        1,
		MaxCPU:        4,
		MinRAM:        0.25,
		MaxRAM:        4,
		MinDisk:       1,
		MaxDisk:       10,
		MinContainers: 1,
		MaxContainers: 3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Hostname != "api" {
		t.Errorf("expected hostname=api, got %s", result.Hostname)
	}
	if result.ServiceID != "svc-1" {
		t.Errorf("expected serviceId=svc-1, got %s", result.ServiceID)
	}
}

func TestScale_NilProcess(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING"},
		})

	// Default mock SetAutoscaling returns nil process
	result, err := Scale(context.Background(), mock, "proj-1", "api", ScaleParams{
		MinCPU: 1,
		MaxCPU: 4,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Process != nil {
		t.Error("expected nil process (sync scaling)")
	}
	if result.Message == "" {
		t.Error("expected non-empty message for nil process")
	}
}

func TestScale_NoParams(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING"},
		})

	_, err := Scale(context.Background(), mock, "proj-1", "api", ScaleParams{})
	if err == nil {
		t.Fatal("expected error for empty params")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidScaling {
		t.Errorf("expected code %s, got %s", platform.ErrInvalidScaling, pe.Code)
	}
}

func TestScale_MinGtMax(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params ScaleParams
	}{
		{"CPU", ScaleParams{MinCPU: 4, MaxCPU: 2}},
		{"RAM", ScaleParams{MinRAM: 8, MaxRAM: 4}},
		{"Disk", ScaleParams{MinDisk: 20, MaxDisk: 10}},
		{"Containers", ScaleParams{MinContainers: 5, MaxContainers: 2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING"},
				})

			_, err := Scale(context.Background(), mock, "proj-1", "api", tt.params)
			if err == nil {
				t.Fatal("expected error for min > max")
			}
			pe, ok := err.(*platform.PlatformError)
			if !ok {
				t.Fatalf("expected *PlatformError, got %T: %v", err, err)
			}
			if pe.Code != platform.ErrInvalidScaling {
				t.Errorf("expected code %s, got %s", platform.ErrInvalidScaling, pe.Code)
			}
		})
	}
}

func TestScale_InvalidCPUMode(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING"},
		})

	_, err := Scale(context.Background(), mock, "proj-1", "api", ScaleParams{
		CPUMode: "INVALID",
		MinCPU:  1,
		MaxCPU:  2,
	})
	if err == nil {
		t.Fatal("expected error for invalid CPU mode")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidScaling {
		t.Errorf("expected code %s, got %s", platform.ErrInvalidScaling, pe.Code)
	}
}
