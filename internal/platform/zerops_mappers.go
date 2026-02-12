package platform

import (
	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/output"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/enum"
)

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

	created := p.Created.String()

	var started *string
	if s, ok := p.Started.Get(); ok {
		v := s.String()
		started = &v
	}
	var finished *string
	if f, ok := p.Finished.Get(); ok {
		v := f.String()
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

func mapServiceStackTypes(items output.EsServiceStackTypeResponseItems) []ServiceStackType {
	result := make([]ServiceStackType, 0, len(items))
	for _, item := range items {
		versions := make([]ServiceStackTypeVersion, 0, len(item.ServiceStackTypeVersionList))
		for _, v := range item.ServiceStackTypeVersionList {
			versions = append(versions, ServiceStackTypeVersion{
				Name:    v.Name.String(),
				IsBuild: v.IsBuild.Native(),
				Status:  v.Status.String(),
			})
		}
		result = append(result, ServiceStackType{
			Name:     item.Name.String(),
			Category: item.Category.String(),
			Versions: versions,
		})
	}
	return result
}

func mapEsServiceStack(s output.EsServiceStack) ServiceStack {
	var autoscaling *CustomAutoscaling
	if s.CustomAutoscaling != nil {
		autoscaling = mapOutputCustomAutoscaling(s.CustomAutoscaling)
	}

	mode := ""
	if s.Mode != nil {
		mode = s.Mode.String()
	}

	return ServiceStack{
		ID:        s.Id.TypedString().String(),
		Name:      s.Name.String(),
		ProjectID: s.ProjectId.TypedString().String(),
		ServiceStackTypeInfo: ServiceTypeInfo{
			ServiceStackTypeVersionName: s.ServiceStackTypeInfo.ServiceStackTypeVersionName.String(),
		},
		Status:            s.Status.String(),
		Mode:              mode,
		Ports:             mapServicePorts(s.Ports),
		CustomAutoscaling: autoscaling,
		Created:           s.Created.String(),
		LastUpdate:        s.LastUpdate.String(),
	}
}

func mapFullServiceStack(s output.ServiceStack) ServiceStack {
	var autoscaling *CustomAutoscaling
	if s.CustomAutoscaling != nil {
		autoscaling = mapOutputCustomAutoscaling(s.CustomAutoscaling)
	}

	return ServiceStack{
		ID:        s.Id.TypedString().String(),
		Name:      s.Name.String(),
		ProjectID: s.ProjectId.TypedString().String(),
		ServiceStackTypeInfo: ServiceTypeInfo{
			ServiceStackTypeVersionName: s.ServiceStackTypeInfo.ServiceStackTypeVersionName.String(),
		},
		Status:            s.Status.String(),
		Mode:              s.Mode.String(),
		Ports:             mapServicePorts(s.Ports),
		CustomAutoscaling: autoscaling,
		Created:           s.Created.String(),
		LastUpdate:        s.LastUpdate.String(),
	}
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
			Port:     int(p.Port),
			Protocol: p.Protocol.String(),
			Public:   public,
		})
	}
	return ports
}

func mapOutputCustomAutoscaling(ca *output.CustomAutoscaling) *CustomAutoscaling {
	result := &CustomAutoscaling{}
	if v := ca.VerticalAutoscalingNullable; v != nil {
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
	}
	if h := ca.HorizontalAutoscalingNullable; h != nil {
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
	result := body.Autoscaling{}

	// Set service mode to preserve current HA/NON_HA — nil mode causes "mode update forbidden".
	if params.ServiceMode != "" {
		mode := enum.ServiceStackModeEnum(params.ServiceMode)
		result.Mode = &mode
	}

	var vert *body.VerticalAutoscalingNullable
	var horiz *body.HorizontalAutoscalingNullable

	needsVert := params.VerticalCPUMode != nil || params.VerticalMinCPU != nil ||
		params.VerticalMaxCPU != nil || params.VerticalMinRAM != nil ||
		params.VerticalMaxRAM != nil || params.VerticalMinDisk != nil ||
		params.VerticalMaxDisk != nil || params.VerticalStartCPU != nil

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
