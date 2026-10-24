// Tests for: plans/analysis/ops.md § ops/manage.go
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
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

func TestConnectStorage_SingleListServicesCall(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: "RUNNING"},
			{ID: "svc-2", Name: "storage", ProjectID: "proj-1", Status: "RUNNING"},
		})

	_, err := ConnectStorage(context.Background(), mock, "proj-1", "appdev", "storage")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := mock.CallCounts["ListServices"]
	if calls != 1 {
		t.Errorf("ListServices called %d times, want 1", calls)
	}
}

func TestDisconnectStorage_SingleListServicesCall(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: "RUNNING"},
			{ID: "svc-2", Name: "storage", ProjectID: "proj-1", Status: "RUNNING"},
		})

	_, err := DisconnectStorage(context.Background(), mock, "proj-1", "appdev", "storage")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := mock.CallCounts["ListServices"]
	if calls != 1 {
		t.Errorf("ListServices called %d times, want 1", calls)
	}
}

func TestScale_MinFreeRamParams_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params ScaleParams
	}{
		{
			"both RAM thresholds",
			ScaleParams{
				MinFreeRAMGB:      scaleFloatPtr(0.5),
				MinFreeRAMPercent: scaleFloatPtr(20),
			},
		},
		{
			"only minFreeRamGB",
			ScaleParams{MinFreeRAMGB: scaleFloatPtr(0.5)},
		},
		{
			"only minFreeRamPercent",
			ScaleParams{MinFreeRAMPercent: scaleFloatPtr(20)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING"},
				})

			result, err := Scale(context.Background(), mock, "proj-1", "api", tt.params)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result.Hostname != "api" {
				t.Errorf("expected hostname=api, got %s", result.Hostname)
			}
		})
	}
}

func TestScale_MinFreeCpuParams_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params ScaleParams
	}{
		{
			"both CPU thresholds",
			ScaleParams{
				MinFreeCPUCores:   scaleFloatPtr(0.2),
				MinFreeCPUPercent: scaleFloatPtr(10),
			},
		},
		{
			"only minFreeCpuCores",
			ScaleParams{MinFreeCPUCores: scaleFloatPtr(0.2)},
		},
		{
			"only minFreeCpuPercent",
			ScaleParams{MinFreeCPUPercent: scaleFloatPtr(10)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING"},
				})

			result, err := Scale(context.Background(), mock, "proj-1", "api", tt.params)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result.Hostname != "api" {
				t.Errorf("expected hostname=api, got %s", result.Hostname)
			}
		})
	}
}

func TestScale_ThresholdOnly_HasParam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params ScaleParams
	}{
		{"minFreeRamGB only", ScaleParams{MinFreeRAMGB: scaleFloatPtr(0.5)}},
		{"minFreeRamPercent only", ScaleParams{MinFreeRAMPercent: scaleFloatPtr(20)}},
		{"minFreeCpuCores only", ScaleParams{MinFreeCPUCores: scaleFloatPtr(0.2)}},
		{"minFreeCpuPercent only", ScaleParams{MinFreeCPUPercent: scaleFloatPtr(10)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !hasAnyScaleParam(tt.params) {
				t.Error("hasAnyScaleParam should return true for threshold-only params")
			}
		})
	}
}

func TestBuildAutoscalingParams_MinFreeResource(t *testing.T) {
	t.Parallel()

	ramGB := 0.5
	ramPct := 20.0
	cpuCores := 0.2
	cpuPct := 10.0

	params := buildAutoscalingParams(ScaleParams{
		MinFreeRAMGB:      &ramGB,
		MinFreeRAMPercent: &ramPct,
		MinFreeCPUCores:   &cpuCores,
		MinFreeCPUPercent: &cpuPct,
	})

	if params.VerticalMinFreeRAMGB == nil || *params.VerticalMinFreeRAMGB != 0.5 {
		t.Errorf("VerticalMinFreeRAMGB = %v, want 0.5", params.VerticalMinFreeRAMGB)
	}
	if params.VerticalMinFreeRAMPct == nil || *params.VerticalMinFreeRAMPct != 20.0 {
		t.Errorf("VerticalMinFreeRAMPct = %v, want 20.0", params.VerticalMinFreeRAMPct)
	}
	if params.VerticalMinFreeCPUCores == nil || *params.VerticalMinFreeCPUCores != 0.2 {
		t.Errorf("VerticalMinFreeCPUCores = %v, want 0.2", params.VerticalMinFreeCPUCores)
	}
	if params.VerticalMinFreeCPUPct == nil || *params.VerticalMinFreeCPUPct != 10.0 {
		t.Errorf("VerticalMinFreeCPUPct = %v, want 10.0", params.VerticalMinFreeCPUPct)
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

// scaleCaptureClient records the autoscaling body Scale sends.
type scaleCaptureClient struct {
	*platform.Mock
	sent *platform.AutoscalingParams
}

func (c *scaleCaptureClient) SetAutoscaling(ctx context.Context, serviceID string, p platform.AutoscalingParams) (*platform.Process, error) {
	c.sent = &p
	return c.Mock.SetAutoscaling(ctx, serviceID, p)
}

func TestScale_OmittedFieldsKeepTheServiceCurrentScaling(t *testing.T) {
	t.Parallel()
	const dedicated = "DEDICATED"

	client := &scaleCaptureClient{Mock: platform.NewMock().
		WithServices([]platform.ServiceStack{{
			ID: "svc-1", Name: "apidev", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			CustomAutoscaling: &platform.CustomAutoscaling{
				CPUMode: dedicated, MinCPU: 2, MaxCPU: 4, StartCPUCoreCount: 2,
				MinRAM: 1, MaxRAM: 4, MinDisk: 1, MaxDisk: 10,
				HorizontalMinCount: 1, HorizontalMaxCount: 1,
				MinFreeRAMGB: 0.25, MinFreeCPUCores: 0.2, SwapEnabled: true,
			},
		}})}

	if _, err := Scale(context.Background(), client, "proj-1", "apidev", ScaleParams{
		MinRAM: scaleFloatPtr(2),
		MaxRAM: scaleFloatPtr(8),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := client.sent
	if got == nil {
		t.Fatal("SetAutoscaling was not called")
	}
	wantInt := map[string]struct {
		got  *int32
		want int32
	}{
		"minCpu": {got.VerticalMinCPU, 2}, "maxCpu": {got.VerticalMaxCPU, 4}, "startCpu": {got.VerticalStartCPU, 2},
		"minContainers": {got.HorizontalMinCount, 1}, "maxContainers": {got.HorizontalMaxCount, 1},
	}
	for name, f := range wantInt {
		if f.got == nil || *f.got != f.want {
			t.Errorf("%s sent = %v, want %d", name, f.got, f.want)
		}
	}
	wantFloat := map[string]struct {
		got  *float64
		want float64
	}{
		"minRam": {got.VerticalMinRAM, 2}, "maxRam": {got.VerticalMaxRAM, 8},
		"minDisk": {got.VerticalMinDisk, 1}, "maxDisk": {got.VerticalMaxDisk, 10},
		"minFreeRamGB": {got.VerticalMinFreeRAMGB, 0.25}, "minFreeCpuCores": {got.VerticalMinFreeCPUCores, 0.2},
	}
	for name, f := range wantFloat {
		if f.got == nil || *f.got != f.want {
			t.Errorf("%s sent = %v, want %g", name, f.got, f.want)
		}
	}
	if got.VerticalCPUMode == nil || *got.VerticalCPUMode != dedicated {
		t.Errorf("cpuMode sent = %v, want DEDICATED", got.VerticalCPUMode)
	}
	if got.VerticalSwapEnabled == nil || !*got.VerticalSwapEnabled {
		t.Errorf("swapEnabled sent = %v, want true", got.VerticalSwapEnabled)
	}
}

// A partial scale on a service still on platform defaults sends a body the
// platform accepts: the default minFreeRamGB 0.0625 was copied into the
// request and the platform refused it ("minFreeResource.memoryGBytes … no more
// than 3 decimal places", seen live 2026-10-16). Omitted fields come from the
// service's customAutoscaling, or stay out when it has none.
func TestScale_APartialScaleOnAServiceStillOnDefaultsSendsABodyThePlatformAccepts(t *testing.T) {
	t.Parallel()
	defaults := &platform.CustomAutoscaling{
		CPUMode: "SHARED", MinCPU: 1, MaxCPU: 8, StartCPUCoreCount: 2,
		MinRAM: 0.125, MaxRAM: 48, MinDisk: 1, MaxDisk: 250,
		HorizontalMinCount: 1, HorizontalMaxCount: 1,
		MinFreeRAMGB: 0.0625, MinFreeCPUCores: 0.1,
	}
	tests := []struct {
		name   string
		custom *platform.CustomAutoscaling
		// wantMinCPU is the minCpu the body carries, nil when it stays out.
		wantMinCPU *int32
	}{
		{name: "no customAutoscaling", custom: nil},
		{
			name:       "customAutoscaling carries the default minFreeRamGB",
			custom:     &platform.CustomAutoscaling{MinCPU: 2, MaxCPU: 4, MinFreeRAMGB: 0.0625},
			wantMinCPU: func() *int32 { v := int32(2); return &v }(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &scaleCaptureClient{Mock: platform.NewMock().
				WithServices([]platform.ServiceStack{{
					ID: "svc-1", Name: "vetsstage", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "java@21"},
					CurrentAutoscaling:   defaults,
					CustomAutoscaling:    tt.custom,
				}})}

			if _, err := Scale(context.Background(), client, "proj-1", "vetsstage", ScaleParams{MaxRAM: scaleFloatPtr(4)}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := client.sent
			if got == nil {
				t.Fatal("SetAutoscaling was not called")
			}
			for name, v := range map[string]*float64{
				"minRam": got.VerticalMinRAM, "maxRam": got.VerticalMaxRAM, "minFreeRamGB": got.VerticalMinFreeRAMGB,
			} {
				if v != nil && math.Abs(*v*1000-math.Round(*v*1000)) > 1e-9 {
					t.Errorf("%s sent = %g, the platform refuses more than 3 decimal places", name, *v)
				}
			}
			if got.VerticalMaxRAM == nil || *got.VerticalMaxRAM != 4 {
				t.Errorf("maxRam sent = %v, want 4", got.VerticalMaxRAM)
			}
			if got.VerticalMinFreeRAMGB != nil {
				t.Errorf("minFreeRamGB sent = %g, want it left out so the platform default stays", *got.VerticalMinFreeRAMGB)
			}
			if got.HorizontalMinCount != nil || got.VerticalMaxDisk != nil {
				t.Errorf("a platform default was sent: minContainers=%v maxDisk=%v", got.HorizontalMinCount, got.VerticalMaxDisk)
			}
			switch {
			case tt.wantMinCPU == nil && got.VerticalMinCPU != nil:
				t.Errorf("minCpu sent = %d, want it left out", *got.VerticalMinCPU)
			case tt.wantMinCPU != nil && (got.VerticalMinCPU == nil || *got.VerticalMinCPU != *tt.wantMinCPU):
				t.Errorf("minCpu sent = %v, want %d from customAutoscaling", got.VerticalMinCPU, *tt.wantMinCPU)
			}
		})
	}
}

// A scale call that raises minRam above the maxRam the service keeps (the
// call did not set maxRam) is refused by the platform with a field error whose
// suggestion says to fix YAML (seen live 2026-10-16). The error names both
// values and which one the call left as it was instead.
func TestScale_ARefusedMinAboveTheMaxTheServiceKeepsNamesBothValues(t *testing.T) {
	t.Parallel()

	refused := platform.NewPlatformError(platform.ErrAPIError, "Invalid custom autoscaling value.",
		"Field 'minResource.memoryGBytes' (Value must not be greater than maxResource.memoryGBytes) rejected. Fix in YAML and retry.")
	refused.APICode = "invalidCustomAutoscalingValue"
	client := &scaleCaptureClient{Mock: platform.NewMock().
		WithServices([]platform.ServiceStack{{
			ID: "svc-1", Name: "difydev", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/python@3.12"},
			CustomAutoscaling: &platform.CustomAutoscaling{
				MinCPU: 1, MaxCPU: 3, MinRAM: 4, MaxRAM: 6, MinDisk: 1, MaxDisk: 100,
				HorizontalMinCount: 1, HorizontalMaxCount: 1,
			},
		}}).WithError("SetAutoscaling", refused)}

	_, err := Scale(context.Background(), client, "proj-1", "difydev", ScaleParams{
		MinRAM: scaleFloatPtr(8),
		MinCPU: scaleIntPtr(2),
	})
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidScaling {
		t.Fatalf("err = %v, want an %s PlatformError", err, platform.ErrInvalidScaling)
	}
	for _, want := range []string{"minRam 8 GB", "maxRam 6 GB", "this call did not set it"} {
		if !strings.Contains(pe.Message, want) {
			t.Errorf("message = %q, want it to contain %q", pe.Message, want)
		}
	}
	if !strings.Contains(pe.Suggestion, "maxRam") || strings.Contains(pe.Suggestion, "YAML") {
		t.Errorf("suggestion = %q, want it to name maxRam and not YAML", pe.Suggestion)
	}
}

func TestScale_ManagedServiceNeverSendsAContainerCount(t *testing.T) {
	t.Parallel()

	client := &scaleCaptureClient{Mock: platform.NewMock().
		WithServices([]platform.ServiceStack{{
			ID: "svc-db", Name: "db", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"},
			CustomAutoscaling: &platform.CustomAutoscaling{
				MinCPU: 1, MaxCPU: 2, MinRAM: 0.25, MaxRAM: 2,
				HorizontalMinCount: 1, HorizontalMaxCount: 1,
			},
		}})}

	if _, err := Scale(context.Background(), client, "proj-1", "db", ScaleParams{MaxRAM: scaleFloatPtr(4)}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.sent.HorizontalMinCount != nil || client.sent.HorizontalMaxCount != nil {
		t.Errorf("container count sent for a managed service: min=%v max=%v", client.sent.HorizontalMinCount, client.sent.HorizontalMaxCount)
	}
	if client.sent.VerticalMinCPU == nil || *client.sent.VerticalMinCPU != 1 {
		t.Errorf("minCpu sent = %v, want 1", client.sent.VerticalMinCPU)
	}
}

func TestReadBackScale_NamesEveryFieldThePlatformSetDifferently(t *testing.T) {
	t.Parallel()

	effective := &platform.CustomAutoscaling{
		CPUMode: "SHARED", MinCPU: 3, MaxCPU: 3, MinRAM: 6, MaxRAM: 6,
		HorizontalMinCount: 1, HorizontalMaxCount: 1,
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{
			ID: "svc-1", Name: "webdev", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"},
			CurrentAutoscaling:   effective,
		}})

	result, err := Scale(context.Background(), mock, "proj-1", "webdev", ScaleParams{
		MinCPU: scaleIntPtr(3), MaxCPU: scaleIntPtr(8),
		MinRAM: scaleFloatPtr(16), MaxRAM: scaleFloatPtr(24),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ReadBackScale(context.Background(), mock, result); err != nil {
		t.Fatalf("read back: %v", err)
	}

	if result.Effective == nil || *result.Effective != *effective {
		t.Errorf("effective = %+v, want %+v", result.Effective, effective)
	}
	want := []string{
		"maxCpu: asked 8, the platform set 3",
		"minRam: asked 16, the platform set 6",
		"maxRam: asked 24, the platform set 6",
	}
	if len(result.Clamped) != len(want) {
		t.Fatalf("clamped = %q, want %q", result.Clamped, want)
	}
	for i := range want {
		if result.Clamped[i] != want[i] {
			t.Errorf("clamped[%d] = %q, want %q", i, result.Clamped[i], want[i])
		}
	}
}

func TestReadBackScale_NothingClampedWhenTheServiceTookEveryValue(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{
			ID: "svc-1", Name: "api", ProjectID: "proj-1", Status: "RUNNING",
			CurrentAutoscaling: &platform.CustomAutoscaling{MinRAM: 0.5, MaxRAM: 2, HorizontalMinCount: 1, HorizontalMaxCount: 2},
		}})

	result, err := Scale(context.Background(), mock, "proj-1", "api", ScaleParams{
		MinRAM: scaleFloatPtr(0.5), MaxRAM: scaleFloatPtr(2), MaxContainers: scaleIntPtr(2),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ReadBackScale(context.Background(), mock, result); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if result.Effective == nil || result.Effective.MaxRAM != 2 {
		t.Errorf("effective = %+v, want maxRam 2", result.Effective)
	}
	if len(result.Clamped) != 0 {
		t.Errorf("clamped = %q, want none", result.Clamped)
	}
}

// swarmSchemaContainerBounds reads a type's minContainers/maxContainers
// bounds straight from the vendored import schema — the independent oracle
// for what a pool may be scaled to (spec-workflows.md §8 PS-4).
func swarmSchemaContainerBounds(t *testing.T, typeName string) (lo, hi int) {
	t.Helper()
	raw, err := os.ReadFile("../schema/testdata/import_yml_schema.json")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var doc struct {
		Properties struct {
			Services struct {
				Items struct {
					AllOf []struct {
						If struct {
							Properties struct {
								Type struct {
									Enum []string `json:"enum"`
								} `json:"type"`
							} `json:"properties"`
						} `json:"if"`
						Then struct {
							Properties struct {
								Min json.RawMessage `json:"minContainers"`
							} `json:"properties"`
						} `json:"then"`
					} `json:"allOf"`
				} `json:"items"`
			} `json:"services"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	for _, rule := range doc.Properties.Services.Items.AllOf {
		for _, name := range rule.If.Properties.Type.Enum {
			if name != typeName {
				continue
			}
			// A stanza that forbids the field carries `false`, not bounds.
			var bounds struct {
				Minimum *int `json:"minimum"`
				Maximum *int `json:"maximum"`
			}
			if err := json.Unmarshal(rule.Then.Properties.Min, &bounds); err == nil && bounds.Minimum != nil && bounds.Maximum != nil {
				return *bounds.Minimum, *bounds.Maximum
			}
		}
	}
	t.Fatalf("schema has no minContainers bounds for %s", typeName)
	return 0, 0
}

func TestScale_Pool_ZeroMinContainers_Accepted(t *testing.T) {
	t.Parallel()

	for _, poolType := range []string{"swarm@1", "swarm-vm@1"} {
		lo, hi := swarmSchemaContainerBounds(t, poolType)
		if lo != 0 || hi != 10 {
			t.Fatalf("%s schema bounds = %d..%d, spec PS-4 expects 0..10", poolType, lo, hi)
		}
		tests := []struct {
			name     string
			params   ScaleParams
			wantSent [2]*int32 // min, max
			wantErr  bool
		}{
			{"zero min", ScaleParams{MinContainers: scaleIntPtr(lo)}, [2]*int32{int32Ptr(int32(lo)), nil}, false},
			{"zero min, top max", ScaleParams{MinContainers: scaleIntPtr(lo), MaxContainers: scaleIntPtr(hi)}, [2]*int32{int32Ptr(int32(lo)), int32Ptr(int32(hi))}, false},
			{"max above schema", ScaleParams{MaxContainers: scaleIntPtr(hi + 1)}, [2]*int32{}, true},
			{"min below schema", ScaleParams{MinContainers: scaleIntPtr(lo - 1)}, [2]*int32{}, true},
		}
		for _, tt := range tests {
			t.Run(poolType+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				client := &scaleCaptureClient{Mock: platform.NewMock().
					WithServices([]platform.ServiceStack{{
						ID: "svc-pool", Name: "pool", ProjectID: "proj-1", Status: "RUNNING",
						ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: poolType},
					}})}

				res, err := Scale(context.Background(), client, "proj-1", "pool", tt.params)
				if tt.wantErr {
					var pe *platform.PlatformError
					if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidScaling {
						t.Fatalf("err = %v, want an %s PlatformError", err, platform.ErrInvalidScaling)
					}
					if client.sent != nil {
						t.Errorf("a refused scale reached the platform: %+v", client.sent)
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				got := [2]*int32{client.sent.HorizontalMinCount, client.sent.HorizontalMaxCount}
				for i, name := range []string{"minContainers", "maxContainers"} {
					switch {
					case tt.wantSent[i] == nil && got[i] != nil:
						t.Errorf("%s sent = %d, want it left out", name, *got[i])
					case tt.wantSent[i] != nil && (got[i] == nil || *got[i] != *tt.wantSent[i]):
						t.Errorf("%s sent = %v, want %d", name, got[i], *tt.wantSent[i])
					}
				}
				for _, want := range []string{"orchestrator", "maxContainers"} {
					if !strings.Contains(res.Notice, want) {
						t.Errorf("notice = %q, want it to contain %q", res.Notice, want)
					}
				}
			})
		}
	}
}

func int32Ptr(v int32) *int32 { return &v }

// TestScale_PoolMinContainersAboveZero_NoticeSaysUnresettable: a scale that
// sets a pool's minContainers above 0 says, in its notice, that the
// containers the platform creates for that minimum cannot be reset (a
// release deletes them) and that a warm container through the orchestrator
// is the way to have one ready (spec-workflows.md §8 DP-15, DP-27). A
// minimum of 0, or a call that leaves the minimum alone, carries no such
// sentence.
func TestScale_PoolMinContainersAboveZero_NoticeSaysUnresettable(t *testing.T) {
	t.Parallel()

	unresettable := []string{"no primary snapshot", "cannot be reset", "deletes such a container", "minContainers at 0", "warm container", "POST /container"}
	tests := []struct {
		name     string
		poolType string
		params   ScaleParams
		want     bool
	}{
		{"min raised to 1", "swarm@1", ScaleParams{MinContainers: scaleIntPtr(1)}, true},
		{"min raised with max", "swarm-vm@1", ScaleParams{MinContainers: scaleIntPtr(2), MaxContainers: scaleIntPtr(4)}, true},
		{"min kept at 0", "swarm@1", ScaleParams{MinContainers: scaleIntPtr(0), MaxContainers: scaleIntPtr(4)}, false},
		{"max only", "swarm@1", ScaleParams{MaxContainers: scaleIntPtr(4)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &scaleCaptureClient{Mock: platform.NewMock().
				WithServices([]platform.ServiceStack{{
					ID: "svc-pool", Name: "pool", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: tt.poolType},
					CustomAutoscaling:    &platform.CustomAutoscaling{HorizontalMinCount: 0, HorizontalMaxCount: 4},
				}})}

			res, err := Scale(context.Background(), client, "proj-1", "pool", tt.params)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(res.Notice, "maxContainers caps") {
				t.Errorf("notice = %q, want the container-count notice kept", res.Notice)
			}
			for _, want := range unresettable {
				if got := strings.Contains(res.Notice, want); got != tt.want {
					t.Errorf("notice contains %q = %v, want %v; notice = %q", want, got, tt.want, res.Notice)
				}
			}
		})
	}
}

func TestScale_Box_ContainerCounts_Refused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		params    ScaleParams
		wantFirst string
	}{
		{"min only", ScaleParams{MinContainers: scaleIntPtr(1)}, "minContainers"},
		{"max only", ScaleParams{MaxContainers: scaleIntPtr(2)}, "maxContainers"},
		{"both with vertical", ScaleParams{MinContainers: scaleIntPtr(1), MaxContainers: scaleIntPtr(1), MaxRAM: scaleFloatPtr(4)}, "minContainers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &scaleCaptureClient{Mock: platform.NewMock().
				WithServices([]platform.ServiceStack{{
					ID: "svc-box", Name: "box", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/zbox@1"},
				}})}

			_, err := Scale(context.Background(), client, "proj-1", "box", tt.params)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidScaling {
				t.Fatalf("err = %v, want an %s PlatformError", err, platform.ErrInvalidScaling)
			}
			for _, want := range []string{"Zerops Box", tt.wantFirst, "single VM"} {
				if !strings.Contains(pe.Message, want) {
					t.Errorf("message = %q, want it to contain %q", pe.Message, want)
				}
			}
			if client.sent != nil {
				t.Errorf("a refused scale reached the platform: %+v", client.sent)
			}
		})
	}
}

func TestScale_Box_VerticalScaling_StillAllowed(t *testing.T) {
	t.Parallel()

	client := &scaleCaptureClient{Mock: platform.NewMock().
		WithServices([]platform.ServiceStack{{
			ID: "svc-box", Name: "box", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/zbox@1"},
			CustomAutoscaling: &platform.CustomAutoscaling{
				MinCPU: 2, MaxCPU: 2, MinRAM: 2, MaxRAM: 2, MinDisk: 10, MaxDisk: 10,
				HorizontalMinCount: 1, HorizontalMaxCount: 1,
			},
		}})}

	if _, err := Scale(context.Background(), client, "proj-1", "box", ScaleParams{MaxRAM: scaleFloatPtr(8)}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.sent.VerticalMaxRAM == nil || *client.sent.VerticalMaxRAM != 8 {
		t.Errorf("maxRam sent = %v, want 8", client.sent.VerticalMaxRAM)
	}
	if client.sent.HorizontalMinCount != nil || client.sent.HorizontalMaxCount != nil {
		t.Errorf("container count sent for a Box: min=%v max=%v", client.sent.HorizontalMinCount, client.sent.HorizontalMaxCount)
	}
}

func TestScale_Pool_PartialScaleKeepsZeroMinContainers(t *testing.T) {
	t.Parallel()

	client := &scaleCaptureClient{Mock: platform.NewMock().
		WithServices([]platform.ServiceStack{{
			ID: "svc-pool", Name: "pool", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"},
			CustomAutoscaling: &platform.CustomAutoscaling{
				MinCPU: 1, MaxCPU: 2, MinRAM: 0.5, MaxRAM: 2,
				HorizontalMinCount: 0, HorizontalMaxCount: 5,
			},
		}})}

	if _, err := Scale(context.Background(), client, "proj-1", "pool", ScaleParams{MaxContainers: scaleIntPtr(3)}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.sent.HorizontalMinCount == nil || *client.sent.HorizontalMinCount != 0 {
		t.Errorf("minContainers sent = %v, want the pool's 0 kept", client.sent.HorizontalMinCount)
	}
	if client.sent.HorizontalMaxCount == nil || *client.sent.HorizontalMaxCount != 3 {
		t.Errorf("maxContainers sent = %v, want 3", client.sent.HorizontalMaxCount)
	}
}

// TestScale_Box_ResizeNotice — a Box is one VM: every resize stops and starts
// it, CPU and RAM as well as disk, so its running sandboxes come back PARKED
// without their memory, and its disk can only grow (zerops-docs
// zbox/workspace-service.mdx, Resizing). A Box scale carries that notice; a
// runtime's carries none (spec-workflows.md §8 PS-8).
func TestScale_Box_ResizeNotice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		typeName   string
		params     ScaleParams
		wantNotice bool
	}{
		{"Box RAM", "ubuntu/zbox@1", ScaleParams{MaxRAM: scaleFloatPtr(8)}, true},
		{"Box disk, bare type", "zbox@1", ScaleParams{MaxDisk: scaleFloatPtr(20)}, true},
		{"runtime RAM", "nodejs@22", ScaleParams{MaxRAM: scaleFloatPtr(8)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &scaleCaptureClient{Mock: platform.NewMock().
				WithServices([]platform.ServiceStack{{
					ID: "svc-1", Name: "svc", ProjectID: "proj-1", Status: "RUNNING",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: tt.typeName},
				}})}
			result, err := Scale(context.Background(), client, "proj-1", "svc", tt.params)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range []string{"single VM", "every resize", "PARKED", "without their memory", "park", "disk can only grow"} {
				if got := strings.Contains(result.Notice, want); got != tt.wantNotice {
					t.Errorf("notice contains %q = %v, want %v: %q", want, got, tt.wantNotice, result.Notice)
				}
			}
		})
	}
}

// TestScale_PoolNotice_SaysWhatScaleDoesWithAHigherMax: the pool notice says
// what zerops_scale does with a maxContainers above a pool's 10: it refuses
// it before anything is sent (the platform itself would silently cap it at
// 10, but zcp never sends one) — never that it is capped.
func TestScale_PoolNotice_SaysWhatScaleDoesWithAHigherMax(t *testing.T) {
	t.Parallel()
	client := &scaleCaptureClient{Mock: platform.NewMock().
		WithServices([]platform.ServiceStack{{
			ID: "svc-pool", Name: "pool", ProjectID: "proj-1", Status: "RUNNING",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"},
			CustomAutoscaling:    &platform.CustomAutoscaling{HorizontalMinCount: 0, HorizontalMaxCount: 4},
		}})}

	res, err := Scale(context.Background(), client, "proj-1", "pool", ScaleParams{MaxContainers: scaleIntPtr(6)})
	if err != nil {
		t.Fatalf("scale to 6: %v", err)
	}
	if strings.Contains(res.Notice, "capped") || !strings.Contains(res.Notice, "at most 10 per pool") || !strings.Contains(res.Notice, "refuses a higher value") {
		t.Errorf("notice = %q, want at most 10 per pool and that a higher value is refused, never capped", res.Notice)
	}
	_, err = Scale(context.Background(), client, "proj-1", "pool", ScaleParams{MaxContainers: scaleIntPtr(11)})
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrInvalidScaling {
		t.Fatalf("scale to 11: err = %v, want INVALID_SCALING before anything is sent", err)
	}
}
