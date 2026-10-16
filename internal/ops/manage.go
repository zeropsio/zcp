package ops

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// ScaleParams holds scaling configuration for a service.
// Pointer fields distinguish "not set" (nil) from zero values.
type ScaleParams struct {
	CPUMode       *string  // SHARED or DEDICATED
	MinCPU        *int     // min CPU cores
	MaxCPU        *int     // max CPU cores
	StartCPU      *int     // initial CPU cores on container start
	MinRAM        *float64 // min RAM in GB
	MaxRAM        *float64 // max RAM in GB
	MinDisk       *float64 // min disk in GB
	MaxDisk       *float64 // max disk in GB
	MinContainers *int     // min container count
	MaxContainers *int     // max container count

	MinFreeRAMGB      *float64 // min free RAM in GB before scale-up
	MinFreeRAMPercent *float64 // min free RAM as % of granted
	MinFreeCPUCores   *float64 // min free CPU fraction (0.0-1.0) before scale-up
	MinFreeCPUPercent *float64 // min free CPU % across all cores
}

// ScaleResult contains the result of a scale operation.
type ScaleResult struct {
	Process     *platform.Process `json:"process,omitempty"`
	Message     string            `json:"message,omitempty"`
	Hostname    string            `json:"serviceHostname"`
	ServiceID   string            `json:"serviceId"`
	TimedOut    bool              `json:"timedOut,omitempty"`
	NextActions string            `json:"nextActions,omitempty"`
	// Effective is the service's autoscaling read back once the change
	// landed; Clamped names each requested field the platform set to
	// another value.
	Effective *platform.CustomAutoscaling `json:"effective,omitempty"`
	Clamped   []string                    `json:"clamped,omitempty"`

	requested ScaleParams
}

// Start starts a stopped service.
func Start(ctx context.Context, client platform.Client, projectID, hostname string) (*platform.Process, error) {
	svc, err := resolveUserVisibleService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}
	return client.StartService(ctx, svc.ID)
}

// Stop stops a running service.
func Stop(ctx context.Context, client platform.Client, projectID, hostname string) (*platform.Process, error) {
	svc, err := resolveUserVisibleService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}
	return client.StopService(ctx, svc.ID)
}

// Restart restarts a running service.
func Restart(ctx context.Context, client platform.Client, projectID, hostname string) (*platform.Process, error) {
	svc, err := resolveUserVisibleService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}
	return client.RestartService(ctx, svc.ID)
}

// Reload reloads a running service (graceful config/code reload). Faster than
// restart (~4s vs ~14s), but does NOT re-read env for the running process — the
// runtime keeps its boot env and PHP-FPM keeps its boot config (live-verified
// 2026-05-28; spec §5). Use restart for env-var changes to take effect.
func Reload(ctx context.Context, client platform.Client, projectID, hostname string) (*platform.Process, error) {
	svc, err := resolveUserVisibleService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}
	return client.ReloadService(ctx, svc.ID)
}

// ConnectStorage connects a seaweedfs service to a runtime service through the
// platform-managed mount. That mount is the deprecated Shared Storage mechanism,
// kept best-effort by the platform for already-connected services; new work
// mounts the filer from run.startCommands instead.
func ConnectStorage(ctx context.Context, client platform.Client, projectID, hostname, storageHostname string) (*platform.Process, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, err
	}
	svc, err := FindUserVisibleService(services, hostname)
	if err != nil {
		return nil, err
	}
	storage, err := FindService(services, storageHostname)
	if err != nil {
		return nil, err
	}
	return client.ConnectSharedStorage(ctx, svc.ID, storage.ID)
}

// DisconnectStorage detaches a platform-managed seaweedfs mount from a runtime
// service — the unwind half of ConnectStorage's deprecated mechanism.
func DisconnectStorage(ctx context.Context, client platform.Client, projectID, hostname, storageHostname string) (*platform.Process, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, err
	}
	svc, err := FindUserVisibleService(services, hostname)
	if err != nil {
		return nil, err
	}
	storage, err := FindService(services, storageHostname)
	if err != nil {
		return nil, err
	}
	return client.DisconnectSharedStorage(ctx, svc.ID, storage.ID)
}

// apiCodeInvalidAutoscaling is the platform's code for autoscaling values it
// refuses, e.g. a minRam above maxRam (seen live 2026-10-16).
const apiCodeInvalidAutoscaling = "invalidCustomAutoscalingValue"

// Scale updates the autoscaling parameters for a service. The platform
// replaces the whole autoscaling with the body it gets, so every field the
// caller leaves out is filled from the service's customAutoscaling — a
// RAM-only call keeps the CPU and container bounds it had. A field the
// service never set stays out and keeps its platform default.
func Scale(ctx context.Context, client platform.Client, projectID, hostname string, params ScaleParams) (*ScaleResult, error) {
	if err := validateScaleParams(params); err != nil {
		return nil, err
	}

	svc, err := resolveUserVisibleService(ctx, client, projectID, hostname)
	if err != nil {
		return nil, err
	}

	// The search behind resolveUserVisibleService lags writes; the direct
	// read carries the autoscaling a previous scale just set. Its
	// currentAutoscaling also carries platform defaults, which the platform
	// refuses as input (minFreeRamGB 0.0625), so the fill reads only the
	// user's own values.
	current, err := client.GetService(ctx, svc.ID)
	if err != nil {
		return nil, fmt.Errorf("scale: read current autoscaling: %w", err)
	}
	apiParams := buildAutoscalingParams(params)
	fillOmittedAutoscaling(&apiParams, current.CustomAutoscaling,
		topology.IsManagedService(current.ServiceStackTypeInfo.ServiceStackTypeVersionName))
	apiParams.ServiceMode = svc.Mode
	proc, err := client.SetAutoscaling(ctx, svc.ID, apiParams)
	if err != nil {
		var pe *platform.PlatformError
		if errors.As(err, &pe) && pe.APICode == apiCodeInvalidAutoscaling {
			if conflict := scaleBoundsConflict(params, apiParams); conflict != nil {
				return nil, conflict
			}
		}
		return nil, err
	}

	result := &ScaleResult{
		Process:   proc,
		Hostname:  svc.Name,
		ServiceID: svc.ID,
		requested: params,
	}
	if proc == nil {
		result.Message = "Scaling parameters updated"
	}

	return result, nil
}

// ReadBackScale reads the service once a scale landed: Effective is what
// the service runs with, Clamped names each requested field the platform set
// to another value (an account or plan ceiling).
func ReadBackScale(ctx context.Context, client platform.Client, result *ScaleResult) error {
	svc, err := client.GetService(ctx, result.ServiceID)
	if err != nil {
		return fmt.Errorf("read back scaling of %s: %w", result.Hostname, err)
	}
	result.Effective = effectiveAutoscaling(svc)
	if result.Effective != nil {
		result.Clamped = clampedFields(result.requested, result.Effective)
	}
	return nil
}

// effectiveAutoscaling is the active config (CurrentAutoscaling) over the
// user overrides (CustomAutoscaling) — the same preference discover shows.
func effectiveAutoscaling(svc *platform.ServiceStack) *platform.CustomAutoscaling {
	if svc.CurrentAutoscaling != nil {
		return svc.CurrentAutoscaling
	}
	return svc.CustomAutoscaling
}

// fillOmittedAutoscaling fills each field the request leaves out from cur.
// A zero in cur is a field the platform did not return, so it stays out and
// the platform default applies. A memory size the platform would refuse as
// input (more than 3 decimal places) can only be its own default, so it
// stays out too. A managed service's container count is fixed by its
// variant, so none is sent for it.
func fillOmittedAutoscaling(p *platform.AutoscalingParams, cur *platform.CustomAutoscaling, managed bool) {
	if cur == nil {
		return
	}
	fillInt := func(dst **int32, v int32) {
		if *dst == nil && v != 0 {
			*dst = &v
		}
	}
	fillFloat := func(dst **float64, v float64) {
		if *dst == nil && v != 0 {
			*dst = &v
		}
	}
	if p.VerticalCPUMode == nil && cur.CPUMode != "" {
		mode := cur.CPUMode
		p.VerticalCPUMode = &mode
	}
	fillInt(&p.VerticalMinCPU, cur.MinCPU)
	fillInt(&p.VerticalMaxCPU, cur.MaxCPU)
	fillInt(&p.VerticalStartCPU, cur.StartCPUCoreCount)
	fillMemoryGB := func(dst **float64, v float64) {
		if math.Abs(v*1000-math.Round(v*1000)) < 1e-9 {
			fillFloat(dst, v)
		}
	}
	fillMemoryGB(&p.VerticalMinRAM, cur.MinRAM)
	fillMemoryGB(&p.VerticalMaxRAM, cur.MaxRAM)
	fillFloat(&p.VerticalMinDisk, cur.MinDisk)
	fillFloat(&p.VerticalMaxDisk, cur.MaxDisk)
	fillMemoryGB(&p.VerticalMinFreeRAMGB, cur.MinFreeRAMGB)
	fillFloat(&p.VerticalMinFreeRAMPct, cur.MinFreeRAMPercent)
	fillFloat(&p.VerticalMinFreeCPUCores, cur.MinFreeCPUCores)
	fillFloat(&p.VerticalMinFreeCPUPct, cur.MinFreeCPUPercent)
	if p.VerticalSwapEnabled == nil && cur.SwapEnabled {
		swap := true
		p.VerticalSwapEnabled = &swap
	}
	if !managed {
		fillInt(&p.HorizontalMinCount, cur.HorizontalMinCount)
		fillInt(&p.HorizontalMaxCount, cur.HorizontalMaxCount)
	}
}

// clampedFields names each requested field whose effective value differs.
func clampedFields(p ScaleParams, eff *platform.CustomAutoscaling) []string {
	var out []string
	note := func(name string, asked, got any) {
		out = append(out, fmt.Sprintf("%s: asked %v, the platform set %v", name, asked, got))
	}
	if p.CPUMode != nil && *p.CPUMode != eff.CPUMode {
		note("cpuMode", *p.CPUMode, eff.CPUMode)
	}
	ints := []struct {
		name  string
		asked *int
		got   int32
	}{
		{"minCpu", p.MinCPU, eff.MinCPU},
		{"maxCpu", p.MaxCPU, eff.MaxCPU},
		{"startCpu", p.StartCPU, eff.StartCPUCoreCount},
		{"minContainers", p.MinContainers, eff.HorizontalMinCount},
		{"maxContainers", p.MaxContainers, eff.HorizontalMaxCount},
	}
	for _, f := range ints {
		if f.asked != nil && int32(*f.asked) != f.got {
			note(f.name, *f.asked, f.got)
		}
	}
	floats := []struct {
		name  string
		asked *float64
		got   float64
	}{
		{"minRam", p.MinRAM, eff.MinRAM},
		{"maxRam", p.MaxRAM, eff.MaxRAM},
		{"minDisk", p.MinDisk, eff.MinDisk},
		{"maxDisk", p.MaxDisk, eff.MaxDisk},
		{"minFreeRamGB", p.MinFreeRAMGB, eff.MinFreeRAMGB},
		{"minFreeRamPercent", p.MinFreeRAMPercent, eff.MinFreeRAMPercent},
		{"minFreeCpuCores", p.MinFreeCPUCores, eff.MinFreeCPUCores},
		{"minFreeCpuPercent", p.MinFreeCPUPercent, eff.MinFreeCPUPercent},
	}
	for _, f := range floats {
		if f.asked != nil && *f.asked != f.got {
			note(f.name, *f.asked, f.got)
		}
	}
	return out
}

// resolveService is a convenience wrapper that fetches services and resolves
// hostname. Use for READ paths (logs, env get) that may legitimately resolve
// a system service. Mutating paths use resolveUserVisibleService.
func resolveService(ctx context.Context, client platform.Client, projectID, hostname string) (*platform.ServiceStack, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return FindService(services, hostname)
}

// resolveUserVisibleService is resolveService for MUTATING paths: it refuses a
// hostname that resolves to a system-category service (core, build*, L7) so a
// typo'd or copied system hostname is rejected at ZCP instead of reaching the
// platform's own guard.
func resolveUserVisibleService(ctx context.Context, client platform.Client, projectID, hostname string) (*platform.ServiceStack, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return FindUserVisibleService(services, hostname)
}

func validateScaleParams(p ScaleParams) error {
	if !hasAnyScaleParam(p) {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			"At least one scaling parameter must be provided",
			"Provide cpuMode, minCpu/maxCpu, minRam/maxRam, minDisk/maxDisk, or minContainers/maxContainers")
	}

	if p.CPUMode != nil && *p.CPUMode != "SHARED" && *p.CPUMode != "DEDICATED" {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			fmt.Sprintf("Invalid cpuMode '%s'", *p.CPUMode),
			"Use SHARED or DEDICATED")
	}

	if p.MinCPU != nil && p.MaxCPU != nil && *p.MinCPU > *p.MaxCPU {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			"minCpu must be <= maxCpu", "")
	}
	if p.MinRAM != nil && p.MaxRAM != nil && *p.MinRAM > *p.MaxRAM {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			"minRam must be <= maxRam", "")
	}
	if p.MinDisk != nil && p.MaxDisk != nil && *p.MinDisk > *p.MaxDisk {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			"minDisk must be <= maxDisk", "")
	}
	if p.MinContainers != nil && p.MaxContainers != nil && *p.MinContainers > *p.MaxContainers {
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			"minContainers must be <= maxContainers", "")
	}

	return nil
}

// scaleBoundsConflict names a minimum above its maximum in what Scale sent
// — the call's values over the service's current ones — with both values and
// the one the call did not set, so a platform rejection of a partial call
// reads as the conflict it is, not as a field error to fix in YAML. nil when
// no pair conflicts. a minimum above its maximum in what Scale would
// send — the call's values over the service's current ones — naming both
// values and the one the call did not set, so the agent sees the conflict a
// partial call creates instead of the platform's field error.
func scaleBoundsConflict(asked ScaleParams, sent platform.AutoscalingParams) error {
	type bound struct {
		name             string
		value            float64
		set, asked, isGB bool
	}
	ofInt := func(name string, v *int32, a *int) bound {
		if v == nil {
			return bound{name: name}
		}
		return bound{name: name, value: float64(*v), set: true, asked: a != nil}
	}
	ofGB := func(name string, v *float64, a *float64) bound {
		if v == nil {
			return bound{name: name}
		}
		return bound{name: name, value: *v, set: true, asked: a != nil, isGB: true}
	}
	pairs := [][2]bound{
		{ofInt("minCpu", sent.VerticalMinCPU, asked.MinCPU), ofInt("maxCpu", sent.VerticalMaxCPU, asked.MaxCPU)},
		{ofGB("minRam", sent.VerticalMinRAM, asked.MinRAM), ofGB("maxRam", sent.VerticalMaxRAM, asked.MaxRAM)},
		{ofGB("minDisk", sent.VerticalMinDisk, asked.MinDisk), ofGB("maxDisk", sent.VerticalMaxDisk, asked.MaxDisk)},
		{ofInt("minContainers", sent.HorizontalMinCount, asked.MinContainers), ofInt("maxContainers", sent.HorizontalMaxCount, asked.MaxContainers)},
	}
	describe := func(b bound) string {
		text := fmt.Sprintf("%s %g", b.name, b.value)
		if b.isGB {
			text += " GB"
		}
		if !b.asked {
			text += " (the service's current value; this call did not set it)"
		}
		return text
	}
	for _, pair := range pairs {
		lo, hi := pair[0], pair[1]
		if !lo.set || !hi.set || lo.value <= hi.value {
			continue
		}
		suggestion := fmt.Sprintf("Pass %s no lower than %g, or %s no higher than %g, in the same call.", hi.name, lo.value, lo.name, hi.value)
		return platform.NewPlatformError(platform.ErrInvalidScaling,
			fmt.Sprintf("%s is greater than %s", describe(lo), describe(hi)), suggestion)
	}
	return nil
}

func hasAnyScaleParam(p ScaleParams) bool {
	return p.CPUMode != nil || p.MinCPU != nil || p.MaxCPU != nil || p.StartCPU != nil ||
		p.MinRAM != nil || p.MaxRAM != nil || p.MinDisk != nil || p.MaxDisk != nil ||
		p.MinContainers != nil || p.MaxContainers != nil ||
		p.MinFreeRAMGB != nil || p.MinFreeRAMPercent != nil ||
		p.MinFreeCPUCores != nil || p.MinFreeCPUPercent != nil
}

func buildAutoscalingParams(p ScaleParams) platform.AutoscalingParams {
	params := platform.AutoscalingParams{}

	if p.CPUMode != nil {
		params.VerticalCPUMode = p.CPUMode
	}
	if p.MinCPU != nil {
		v := int32(*p.MinCPU)
		params.VerticalMinCPU = &v
	}
	if p.MaxCPU != nil {
		v := int32(*p.MaxCPU)
		params.VerticalMaxCPU = &v
	}
	if p.StartCPU != nil {
		v := int32(*p.StartCPU)
		params.VerticalStartCPU = &v
	}
	if p.MinRAM != nil {
		params.VerticalMinRAM = p.MinRAM
	}
	if p.MaxRAM != nil {
		params.VerticalMaxRAM = p.MaxRAM
	}
	if p.MinDisk != nil {
		params.VerticalMinDisk = p.MinDisk
	}
	if p.MaxDisk != nil {
		params.VerticalMaxDisk = p.MaxDisk
	}
	if p.MinContainers != nil {
		v := int32(*p.MinContainers)
		params.HorizontalMinCount = &v
	}
	if p.MaxContainers != nil {
		v := int32(*p.MaxContainers)
		params.HorizontalMaxCount = &v
	}

	if p.MinFreeRAMGB != nil {
		params.VerticalMinFreeRAMGB = p.MinFreeRAMGB
	}
	if p.MinFreeRAMPercent != nil {
		params.VerticalMinFreeRAMPct = p.MinFreeRAMPercent
	}
	if p.MinFreeCPUCores != nil {
		params.VerticalMinFreeCPUCores = p.MinFreeCPUCores
	}
	if p.MinFreeCPUPercent != nil {
		params.VerticalMinFreeCPUPct = p.MinFreeCPUPercent
	}

	return params
}
