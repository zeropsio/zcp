// Tests for: plans/analysis/ops.md § ops/manage.go
package ops

import (
	"context"
	"errors"
	"math"
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
