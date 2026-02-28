// Tests for: plans/analysis/ops.md § ops/manage.go
package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func scaleIntPtr(v int) *int           { return &v }
func scaleFloatPtr(v float64) *float64 { return &v }
func scaleCPUMode(v string) *string    { return &v }

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
		CPUMode:       scaleCPUMode("SHARED"),
		MinCPU:        scaleIntPtr(1),
		MaxCPU:        scaleIntPtr(4),
		MinRAM:        scaleFloatPtr(0.25),
		MaxRAM:        scaleFloatPtr(4),
		MinDisk:       scaleFloatPtr(1),
		MaxDisk:       scaleFloatPtr(10),
		MinContainers: scaleIntPtr(1),
		MaxContainers: scaleIntPtr(3),
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
		MinCPU: scaleIntPtr(1),
		MaxCPU: scaleIntPtr(4),
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
		{"CPU", ScaleParams{MinCPU: scaleIntPtr(4), MaxCPU: scaleIntPtr(2)}},
		{"RAM", ScaleParams{MinRAM: scaleFloatPtr(8), MaxRAM: scaleFloatPtr(4)}},
		{"Disk", ScaleParams{MinDisk: scaleFloatPtr(20), MaxDisk: scaleFloatPtr(10)}},
		{"Containers", ScaleParams{MinContainers: scaleIntPtr(5), MaxContainers: scaleIntPtr(2)}},
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

func TestConnectStorage_Success(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: "RUNNING"},
			{ID: "svc-2", Name: "storage", ProjectID: "proj-1", Status: "RUNNING"},
		})

	proc, err := ConnectStorage(context.Background(), mock, "proj-1", "appdev", "storage")
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

func TestConnectStorage_ServiceNotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-2", Name: "storage", ProjectID: "proj-1", Status: "RUNNING"},
		})

	_, err := ConnectStorage(context.Background(), mock, "proj-1", "missing", "storage")
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

func TestConnectStorage_StorageNotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: "RUNNING"},
		})

	_, err := ConnectStorage(context.Background(), mock, "proj-1", "appdev", "missing")
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

func TestDisconnectStorage_Success(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: "RUNNING"},
			{ID: "svc-2", Name: "storage", ProjectID: "proj-1", Status: "RUNNING"},
		})

	proc, err := DisconnectStorage(context.Background(), mock, "proj-1", "appdev", "storage")
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

func TestScale_StartCPU(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING"},
		})

	result, err := Scale(context.Background(), mock, "proj-1", "api", ScaleParams{
		StartCPU: scaleIntPtr(2),
		MinCPU:   scaleIntPtr(1),
		MaxCPU:   scaleIntPtr(4),
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
}

func TestBuildAutoscalingParams_StartCPU(t *testing.T) {
	t.Parallel()

	startCPU := 2
	params := buildAutoscalingParams(ScaleParams{
		StartCPU: &startCPU,
	})

	if params.VerticalStartCPU == nil {
		t.Fatal("VerticalStartCPU should be set")
	}
	if *params.VerticalStartCPU != 2 {
		t.Errorf("VerticalStartCPU = %d, want 2", *params.VerticalStartCPU)
	}
}

func TestScale_InvalidCPUMode(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING"},
		})

	_, err := Scale(context.Background(), mock, "proj-1", "api", ScaleParams{
		CPUMode: scaleCPUMode("INVALID"),
		MinCPU:  scaleIntPtr(1),
		MaxCPU:  scaleIntPtr(2),
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
