package platform

// Raw JSON autoscaling parsing — workaround for zerops-go SDK v1.0.16.
//
// The SDK has a JSON tag mismatch for autoscaling fields:
//   API returns:  "verticalAutoscaling" / "horizontalAutoscaling"
//   SDK expects:  "verticalAutoscalingNullable" / "horizontalAutoscalingNullable"
// This causes the SDK to silently drop all autoscaling data during JSON decode.
// parseRawAutoscaling bypasses the SDK and parses the real API JSON directly.
//
// TODO: remove this file when zerops-go SDK fixes the JSON tags.

import (
	"encoding/json"
	"fmt"
)

// rawServiceResponse is a minimal representation of the API response for autoscaling extraction.
type rawServiceResponse struct {
	CurrentAutoscaling *rawAutoscaling `json:"currentAutoscaling"`
	CustomAutoscaling  *rawAutoscaling `json:"customAutoscaling"`
}

type rawAutoscaling struct {
	VerticalAutoscaling   *rawVerticalAutoscaling   `json:"verticalAutoscaling"`
	HorizontalAutoscaling *rawHorizontalAutoscaling `json:"horizontalAutoscaling"`
}

type rawVerticalAutoscaling struct {
	CPUMode           *string      `json:"cpuMode"`
	MinResource       *rawResource `json:"minResource"`
	MaxResource       *rawResource `json:"maxResource"`
	StartCPUCoreCount *float64     `json:"startCpuCoreCount"`
}

type rawResource struct {
	CPUCoreCount *float64 `json:"cpuCoreCount"`
	MemoryGBytes *float64 `json:"memoryGBytes"`
	DiskGBytes   *float64 `json:"diskGBytes"`
}

type rawHorizontalAutoscaling struct {
	MinContainerCount *float64 `json:"minContainerCount"`
	MaxContainerCount *float64 `json:"maxContainerCount"`
}

// parseRawAutoscaling extracts autoscaling config from raw API JSON bytes.
// Prefers currentAutoscaling (active platform config), falls back to customAutoscaling (user overrides).
func parseRawAutoscaling(data []byte) (*CustomAutoscaling, error) {
	var resp rawServiceResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse autoscaling JSON: %w", err)
	}

	// Prefer currentAutoscaling (what the platform is actually using).
	primary := resp.CurrentAutoscaling
	fallback := resp.CustomAutoscaling

	if primary == nil && fallback == nil {
		return nil, nil //nolint:nilnil // intentional: nil means no autoscaling data in API response
	}

	// If only one exists, use it.
	if primary == nil {
		primary = fallback
		fallback = nil
	}

	result := &CustomAutoscaling{}

	// Map vertical autoscaling.
	if v := primary.VerticalAutoscaling; v != nil {
		mapRawVertical(result, v)
	} else if fallback != nil && fallback.VerticalAutoscaling != nil {
		mapRawVertical(result, fallback.VerticalAutoscaling)
	}

	// Map horizontal autoscaling.
	if h := primary.HorizontalAutoscaling; h != nil {
		mapRawHorizontal(result, h)
	} else if fallback != nil && fallback.HorizontalAutoscaling != nil {
		mapRawHorizontal(result, fallback.HorizontalAutoscaling)
	}

	if *result == (CustomAutoscaling{}) {
		return nil, nil //nolint:nilnil // intentional: all fields zero means no usable data
	}
	return result, nil
}

func mapRawVertical(dst *CustomAutoscaling, v *rawVerticalAutoscaling) {
	if v.CPUMode != nil {
		dst.CPUMode = *v.CPUMode
	}
	if v.StartCPUCoreCount != nil {
		dst.StartCPUCoreCount = int32(*v.StartCPUCoreCount)
	}
	if v.MinResource != nil {
		if v.MinResource.CPUCoreCount != nil {
			dst.MinCPU = int32(*v.MinResource.CPUCoreCount)
		}
		if v.MinResource.MemoryGBytes != nil {
			dst.MinRAM = *v.MinResource.MemoryGBytes
		}
		if v.MinResource.DiskGBytes != nil {
			dst.MinDisk = *v.MinResource.DiskGBytes
		}
	}
	if v.MaxResource != nil {
		if v.MaxResource.CPUCoreCount != nil {
			dst.MaxCPU = int32(*v.MaxResource.CPUCoreCount)
		}
		if v.MaxResource.MemoryGBytes != nil {
			dst.MaxRAM = *v.MaxResource.MemoryGBytes
		}
		if v.MaxResource.DiskGBytes != nil {
			dst.MaxDisk = *v.MaxResource.DiskGBytes
		}
	}
}

func mapRawHorizontal(dst *CustomAutoscaling, h *rawHorizontalAutoscaling) {
	if h.MinContainerCount != nil {
		dst.HorizontalMinCount = int32(*h.MinContainerCount)
	}
	if h.MaxContainerCount != nil {
		dst.HorizontalMaxCount = int32(*h.MaxContainerCount)
	}
}
