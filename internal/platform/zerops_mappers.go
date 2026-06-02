package platform

import (
	"time"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/output"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/enum"
)

// statusCancelled is the API status string for cancelled operations.
const statusCancelled = "CANCELLED"

// stringNullValue extracts the underlying string from a types.StringNull,
// returning "" when the value is null. SDK v1.0.20 changed several scalar
// fields (e.g. ServiceStack.Mode) from pointer-to-enum to types.StringNull;
// this helper keeps consumer sites terse without repeating the Get pattern.
func stringNullValue(s types.StringNull) string {
	v, ok := s.Get()
	if !ok {
		return ""
	}
	return v.Native()
}

// ---------------------------------------------------------------------------
// Mapping helpers
// ---------------------------------------------------------------------------

func mapProcess(p output.Process) Process {
	status := p.Status.String()
	switch status {
	case "DONE":
		status = "FINISHED"
	case statusCancelled:
		status = "CANCELED"
	}

	serviceStacks := make([]ServiceStackRef, 0, len(p.ServiceStacks))
	for _, ss := range p.ServiceStacks {
		serviceStacks = append(serviceStacks, ServiceStackRef{
			ID:   ss.Id.TypedString().String(),
			Name: ss.Name.String(),
		})
	}

	created := p.Created.Format(time.RFC3339Nano)

	var started *string
	if s, ok := p.Started.Get(); ok {
		v := s.Format(time.RFC3339Nano)
		started = &v
	}
	var finished *string
	if f, ok := p.Finished.Get(); ok {
		v := f.Format(time.RFC3339Nano)
		finished = &v
	}

	// Map FailReason from PublicMeta if present (PRD section 5.4).
	var failReason *string
	if m, ok := p.PublicMeta.Get(); ok {
		raw := map[string]any(m)
		if fr, ok := raw["failReason"]; ok {
			if s, ok := fr.(string); ok && s != "" {
				failReason = &s
			}
		}
	}

	return Process{
		ID:            p.Id.TypedString().String(),
		Status:        status,
		ActionName:    p.ActionName.String(),
		ServiceStacks: serviceStacks,
		Created:       created,
		Started:       started,
		Finished:      finished,
		FailReason:    failReason,
	}
}

func mapEsServiceStack(s output.EsServiceStack) ServiceStack {
	var autoscaling *CustomAutoscaling
	if s.CustomAutoscaling != nil {
		autoscaling = mapOutputCustomAutoscaling(s.CustomAutoscaling)
	}

	mode := ""
	if v, ok := s.Mode.Get(); ok {
		mode = v.Native()
	}

	return ServiceStack{
		ID:        s.Id.TypedString().String(),
		Name:      s.Name.String(),
		ProjectID: s.ProjectId.TypedString().String(),
		ServiceStackTypeInfo: ServiceTypeInfo{
			ServiceStackTypeID:           s.ServiceStackTypeId.Native(),
			ServiceStackTypeVersionName:  s.ServiceStackTypeInfo.ServiceStackTypeVersionName.String(),
			ServiceStackTypeCategoryName: s.ServiceStackTypeInfo.ServiceStackTypeCategory.String(),
		},
		Status:            s.Status.String(),
		Mode:              mode,
		SubdomainAccess:   s.SubdomainAccess.Native(),
		Ports:             mapServicePorts(s.Ports),
		CustomAutoscaling: autoscaling,
		Created:           s.Created.Format(time.RFC3339Nano),
		LastUpdate:        s.LastUpdate.Format(time.RFC3339Nano),
		// ListServices carries activeAppVersion as a lighter shape than
		// GetService; map its ID so callers can fetch the app-version
		// userDataList (yaml-baked vars) without an extra GetService.
		ActiveAppVersion: mapAppVersionLight(s.ActiveAppVersion),
	}
}

// mapAppVersionLight projects the ES list's AppVersionLight onto the
// ActiveAppVersionDigest, carrying only the ID (the list shape has no
// GH-integration / public-git-source fields). Returns nil when there's
// no active app version (never-deployed service) so callers can branch
// on lifecycle state.
func mapAppVersionLight(av *output.AppVersionLight) *ActiveAppVersionDigest {
	if av == nil {
		return nil
	}
	id := av.Id.TypedString().String()
	if id == "" {
		return nil
	}
	return &ActiveAppVersionDigest{ID: id}
}

func mapFullServiceStack(s output.ServiceStack) ServiceStack {
	var customAutoscaling *CustomAutoscaling
	if s.CustomAutoscaling != nil {
		customAutoscaling = mapOutputCustomAutoscaling(s.CustomAutoscaling)
	}

	var currentAutoscaling *CustomAutoscaling
	if s.CurrentAutoscaling != nil {
		currentAutoscaling = mapOutputCustomAutoscaling(s.CurrentAutoscaling)
	}

	return ServiceStack{
		ID:        s.Id.TypedString().String(),
		Name:      s.Name.String(),
		ProjectID: s.ProjectId.TypedString().String(),
		ServiceStackTypeInfo: ServiceTypeInfo{
			ServiceStackTypeID:           s.ServiceStackTypeId.Native(),
			ServiceStackTypeVersionName:  s.ServiceStackTypeInfo.ServiceStackTypeVersionName.String(),
			ServiceStackTypeCategoryName: s.ServiceStackTypeInfo.ServiceStackTypeCategory.String(),
		},
		Status:             s.Status.String(),
		Mode:               stringNullValue(s.Mode),
		SubdomainAccess:    s.SubdomainAccess.Native(),
		Ports:              mapServicePorts(s.Ports),
		CustomAutoscaling:  customAutoscaling,
		CurrentAutoscaling: currentAutoscaling,
		Created:            s.Created.Format(time.RFC3339Nano),
		LastUpdate:         s.LastUpdate.Format(time.RFC3339Nano),
		ActiveAppVersion:   mapActiveAppVersion(s.ActiveAppVersion),
	}
}

// mapActiveAppVersion projects the SDK's GetAppVersion (when populated
// on ServiceStack.ActiveAppVersion) onto the minimal ActiveAppVersionDigest
// ZCP uses for setup-name cascade. Returns nil when there's no active
// app version OR no useful field was readable. Plan: plans/setup-name-
// local-canonical-2026-05-27.md §SDK surface.
func mapActiveAppVersion(av *output.GetAppVersion) *ActiveAppVersionDigest {
	if av == nil {
		return nil
	}
	out := &ActiveAppVersionDigest{
		ID: av.Id.TypedString().String(),
	}
	if av.GithubIntegration != nil {
		if setup, ok := av.GithubIntegration.ZeropsYamlSetup.Get(); ok {
			out.GithubIntegrationSetup = setup.Native()
		}
	}
	if av.PublicGitSource != nil {
		if explicit, ok := av.PublicGitSource.ExplicitSetup.Get(); ok {
			b := explicit.Native()
			out.PublicGitSourceExplicitSet = &b
		}
	}
	if out.ID == "" && out.GithubIntegrationSetup == "" && out.PublicGitSourceExplicitSet == nil {
		return nil
	}
	return out
}

func mapServicePorts(sdkPorts []output.ServicePort) []Port {
	if len(sdkPorts) == 0 {
		return nil
	}
	ports := make([]Port, 0, len(sdkPorts))
	for _, p := range sdkPorts {
		portRouting, prFilled := p.PortRouting.Get()
		httpRouting, hrFilled := p.HttpRouting.Get()
		public := (prFilled && portRouting.Native()) || (hrFilled && httpRouting.Native())
		ports = append(ports, Port{
			Port:        int(p.Port),
			Protocol:    p.Protocol.String(),
			Public:      public,
			HTTPSupport: hrFilled && httpRouting.Native(),
			Scheme:      p.Scheme.String(),
		})
	}
	return ports
}

func mapOutputCustomAutoscaling(ca *output.CustomAutoscaling) *CustomAutoscaling {
	result := &CustomAutoscaling{}
	if v := ca.VerticalAutoscaling; v != nil {
		if v.CpuMode != nil {
			result.CPUMode = v.CpuMode.String()
		}
		if v.MinResource != nil {
			if val, ok := v.MinResource.CpuCoreCount.Get(); ok {
				result.MinCPU = int32(val)
			}
			if val, ok := v.MinResource.MemoryGBytes.Get(); ok {
				result.MinRAM = float64(val)
			}
			if val, ok := v.MinResource.DiskGBytes.Get(); ok {
				result.MinDisk = float64(val)
			}
		}
		if v.MaxResource != nil {
			if val, ok := v.MaxResource.CpuCoreCount.Get(); ok {
				result.MaxCPU = int32(val)
			}
			if val, ok := v.MaxResource.MemoryGBytes.Get(); ok {
				result.MaxRAM = float64(val)
			}
			if val, ok := v.MaxResource.DiskGBytes.Get(); ok {
				result.MaxDisk = float64(val)
			}
		}
		if val, ok := v.StartCpuCoreCount.Get(); ok {
			result.StartCPUCoreCount = int32(val)
		}
		if val, ok := v.SwapEnabled.Get(); ok {
			result.SwapEnabled = bool(val)
		}
		if v.MinFreeResource != nil {
			if val, ok := v.MinFreeResource.CpuCoreCount.Get(); ok {
				result.MinFreeCPUCores = float64(val)
			}
			if val, ok := v.MinFreeResource.CpuCorePercent.Get(); ok {
				result.MinFreeCPUPercent = float64(val)
			}
			if val, ok := v.MinFreeResource.MemoryGBytes.Get(); ok {
				result.MinFreeRAMGB = float64(val)
			}
			if val, ok := v.MinFreeResource.MemoryPercent.Get(); ok {
				result.MinFreeRAMPercent = float64(val)
			}
		}
	}
	if h := ca.HorizontalAutoscaling; h != nil {
		if val, ok := h.MinContainerCount.Get(); ok {
			result.HorizontalMinCount = int32(val)
		}
		if val, ok := h.MaxContainerCount.Get(); ok {
			result.HorizontalMaxCount = int32(val)
		}
	}
	return result
}

func buildAutoscalingBody(params AutoscalingParams) body.Autoscaling {
	// SDK v1.0.20 ("refactor of the service base and scaling") removed
	// body.Autoscaling.Mode + enum.ServiceStackModeEnum. Mode (HA/NON_HA)
	// is no longer part of the autoscaling body — Zerops moved it to
	// service-stack-creation / pipeline-trigger endpoints (marked
	// `Deprecated` there pending its own dedicated endpoint).
	// The old defensive resend ("nil mode causes 'mode update forbidden'")
	// is no longer needed: the field doesn't exist to be nulled.
	result := body.Autoscaling{}
	_ = params.ServiceMode // ServiceMode is now ignored at this site; see scale handler if explicit Mode change is needed.

	var vert *body.VerticalAutoscalingNullable
	var horiz *body.HorizontalAutoscalingNullable

	needsVert := params.VerticalCPUMode != nil || params.VerticalMinCPU != nil ||
		params.VerticalMaxCPU != nil || params.VerticalMinRAM != nil ||
		params.VerticalMaxRAM != nil || params.VerticalMinDisk != nil ||
		params.VerticalMaxDisk != nil || params.VerticalStartCPU != nil ||
		params.VerticalSwapEnabled != nil ||
		params.VerticalMinFreeRAMGB != nil || params.VerticalMinFreeRAMPct != nil ||
		params.VerticalMinFreeCPUCores != nil || params.VerticalMinFreeCPUPct != nil

	if needsVert {
		vert = &body.VerticalAutoscalingNullable{}
		if params.VerticalCPUMode != nil {
			mode := enum.VerticalAutoscalingCpuModeEnum(*params.VerticalCPUMode)
			vert.CpuMode = &mode
		}
		minRes := &body.ScalingResourceNullable{}
		hasMinRes := false
		if params.VerticalMinCPU != nil {
			minRes.CpuCoreCount = types.NewIntNull(int(*params.VerticalMinCPU))
			hasMinRes = true
		}
		if params.VerticalMinRAM != nil {
			minRes.MemoryGBytes = types.NewFloatNull(*params.VerticalMinRAM)
			hasMinRes = true
		}
		if params.VerticalMinDisk != nil {
			minRes.DiskGBytes = types.NewFloatNull(*params.VerticalMinDisk)
			hasMinRes = true
		}
		if hasMinRes {
			vert.MinResource = minRes
		}

		maxRes := &body.ScalingResourceNullable{}
		hasMaxRes := false
		if params.VerticalMaxCPU != nil {
			maxRes.CpuCoreCount = types.NewIntNull(int(*params.VerticalMaxCPU))
			hasMaxRes = true
		}
		if params.VerticalMaxRAM != nil {
			maxRes.MemoryGBytes = types.NewFloatNull(*params.VerticalMaxRAM)
			hasMaxRes = true
		}
		if params.VerticalMaxDisk != nil {
			maxRes.DiskGBytes = types.NewFloatNull(*params.VerticalMaxDisk)
			hasMaxRes = true
		}
		if hasMaxRes {
			vert.MaxResource = maxRes
		}

		minFreeRes := &body.ScalingMinFreeResourceNullable{}
		hasMinFreeRes := false
		if params.VerticalMinFreeCPUCores != nil {
			minFreeRes.CpuCoreCount = types.NewFloatNull(*params.VerticalMinFreeCPUCores)
			hasMinFreeRes = true
		}
		if params.VerticalMinFreeCPUPct != nil {
			minFreeRes.CpuCorePercent = types.NewFloatNull(*params.VerticalMinFreeCPUPct)
			hasMinFreeRes = true
		}
		if params.VerticalMinFreeRAMGB != nil {
			minFreeRes.MemoryGBytes = types.NewFloatNull(*params.VerticalMinFreeRAMGB)
			hasMinFreeRes = true
		}
		if params.VerticalMinFreeRAMPct != nil {
			minFreeRes.MemoryPercent = types.NewFloatNull(*params.VerticalMinFreeRAMPct)
			hasMinFreeRes = true
		}
		if hasMinFreeRes {
			vert.MinFreeResource = minFreeRes
		}

		if params.VerticalStartCPU != nil {
			vert.StartCpuCoreCount = types.NewIntNull(int(*params.VerticalStartCPU))
		}
		if params.VerticalSwapEnabled != nil {
			vert.SwapEnabled = types.NewBoolNull(*params.VerticalSwapEnabled)
		}
	}

	needsHoriz := params.HorizontalMinCount != nil || params.HorizontalMaxCount != nil
	if needsHoriz {
		horiz = &body.HorizontalAutoscalingNullable{}
		if params.HorizontalMinCount != nil {
			horiz.MinContainerCount = types.NewIntNull(int(*params.HorizontalMinCount))
		}
		if params.HorizontalMaxCount != nil {
			horiz.MaxContainerCount = types.NewIntNull(int(*params.HorizontalMaxCount))
		}
	}

	if vert != nil || horiz != nil {
		result.CustomAutoscaling = &body.CustomAutoscaling{
			VerticalAutoscaling:   vert,
			HorizontalAutoscaling: horiz,
		}
	}

	return result
}
