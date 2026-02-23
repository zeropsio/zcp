// Tests for: port mapping, autoscaling JSON parsing through mappers.

package platform

import (
	"testing"

	"github.com/zeropsio/zerops-go/dto/output"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/enum"
)

func TestMapEsServiceStack_PortMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sdkPorts  output.EsServiceStackPorts
		wantPorts []Port
	}{
		{
			name:      "no_ports",
			sdkPorts:  nil,
			wantPorts: nil,
		},
		{
			name: "single_tcp_public_via_port_routing",
			sdkPorts: output.EsServiceStackPorts{
				{
					Port:        types.NewInt(8080),
					Protocol:    enum.ServicePortProtocolEnumTcp,
					PortRouting: types.NewBoolNull(true),
					HttpRouting: types.NewBoolNull(false),
				},
			},
			wantPorts: []Port{
				{Port: 8080, Protocol: "tcp", Public: true},
			},
		},
		{
			name: "public_via_http_routing",
			sdkPorts: output.EsServiceStackPorts{
				{
					Port:        types.NewInt(3000),
					Protocol:    enum.ServicePortProtocolEnumTcp,
					PortRouting: types.NewBoolNull(false),
					HttpRouting: types.NewBoolNull(true),
				},
			},
			wantPorts: []Port{
				{Port: 3000, Protocol: "tcp", Public: true},
			},
		},
		{
			name: "private_port",
			sdkPorts: output.EsServiceStackPorts{
				{
					Port:        types.NewInt(5432),
					Protocol:    enum.ServicePortProtocolEnumTcp,
					PortRouting: types.NewBoolNull(false),
					HttpRouting: types.NewBoolNull(false),
				},
			},
			wantPorts: []Port{
				{Port: 5432, Protocol: "tcp", Public: false},
			},
		},
		{
			name: "multiple_mixed_ports",
			sdkPorts: output.EsServiceStackPorts{
				{
					Port:        types.NewInt(80),
					Protocol:    enum.ServicePortProtocolEnumTcp,
					PortRouting: types.NewBoolNull(false),
					HttpRouting: types.NewBoolNull(true),
				},
				{
					Port:        types.NewInt(5432),
					Protocol:    enum.ServicePortProtocolEnumTcp,
					PortRouting: types.NewBoolNull(false),
					HttpRouting: types.NewBoolNull(false),
				},
			},
			wantPorts: []Port{
				{Port: 80, Protocol: "tcp", Public: true},
				{Port: 5432, Protocol: "tcp", Public: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			es := output.EsServiceStack{
				Ports: tt.sdkPorts,
			}
			result := mapEsServiceStack(es)

			if len(result.Ports) != len(tt.wantPorts) {
				t.Fatalf("Ports length = %d, want %d", len(result.Ports), len(tt.wantPorts))
			}
			for i, want := range tt.wantPorts {
				got := result.Ports[i]
				if got.Port != want.Port {
					t.Errorf("Ports[%d].Port = %d, want %d", i, got.Port, want.Port)
				}
				if got.Protocol != want.Protocol {
					t.Errorf("Ports[%d].Protocol = %q, want %q", i, got.Protocol, want.Protocol)
				}
				if got.Public != want.Public {
					t.Errorf("Ports[%d].Public = %v, want %v", i, got.Public, want.Public)
				}
			}
		})
	}
}

func TestMapFullServiceStack_PortMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sdkPorts  output.ServiceStackPorts
		wantPorts []Port
	}{
		{
			name:      "no_ports",
			sdkPorts:  nil,
			wantPorts: nil,
		},
		{
			name: "public_and_private_ports",
			sdkPorts: output.ServiceStackPorts{
				{
					Port:        types.NewInt(3000),
					Protocol:    enum.ServicePortProtocolEnumTcp,
					PortRouting: types.NewBoolNull(true),
					HttpRouting: types.NewBoolNull(true),
				},
				{
					Port:        types.NewInt(9090),
					Protocol:    enum.ServicePortProtocolEnumUdp,
					PortRouting: types.NewBoolNull(false),
					HttpRouting: types.NewBoolNull(false),
				},
			},
			wantPorts: []Port{
				{Port: 3000, Protocol: "tcp", Public: true},
				{Port: 9090, Protocol: "udp", Public: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ss := output.ServiceStack{
				Ports: tt.sdkPorts,
			}
			result := mapFullServiceStack(ss)

			if len(result.Ports) != len(tt.wantPorts) {
				t.Fatalf("Ports length = %d, want %d", len(result.Ports), len(tt.wantPorts))
			}
			for i, want := range tt.wantPorts {
				got := result.Ports[i]
				if got.Port != want.Port {
					t.Errorf("Ports[%d].Port = %d, want %d", i, got.Port, want.Port)
				}
				if got.Protocol != want.Protocol {
					t.Errorf("Ports[%d].Protocol = %q, want %q", i, got.Protocol, want.Protocol)
				}
				if got.Public != want.Public {
					t.Errorf("Ports[%d].Public = %v, want %v", i, got.Public, want.Public)
				}
			}
		})
	}
}

// Tests for: SDK workaround — raw JSON autoscaling parsing.
// The zerops-go SDK v1.0.16 has a JSON tag mismatch: API returns
// "verticalAutoscaling"/"horizontalAutoscaling" but SDK expects
// "verticalAutoscalingNullable"/"horizontalAutoscalingNullable".
// parseRawAutoscaling parses the real API JSON format directly.

func TestParseRawAutoscaling_FullResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		json    string
		want    *CustomAutoscaling
		wantErr bool
	}{
		{
			name: "complete_autoscaling",
			json: `{
				"currentAutoscaling": {
					"horizontalAutoscaling": {
						"maxContainerCount": 10,
						"minContainerCount": 1
					},
					"verticalAutoscaling": {
						"cpuMode": "SHARED",
						"maxResource": {
							"cpuCoreCount": 2,
							"diskGBytes": 250,
							"memoryGBytes": 2
						},
						"minResource": {
							"cpuCoreCount": 1,
							"diskGBytes": 1,
							"memoryGBytes": 0.5
						},
						"startCpuCoreCount": 2,
						"swapEnabled": true
					}
				},
				"customAutoscaling": {
					"horizontalAutoscaling": {
						"maxContainerCount": null,
						"minContainerCount": null
					},
					"verticalAutoscaling": {
						"cpuMode": "SHARED",
						"maxResource": {
							"cpuCoreCount": 2,
							"diskGBytes": null,
							"memoryGBytes": 2
						},
						"minResource": {
							"cpuCoreCount": 1,
							"diskGBytes": null,
							"memoryGBytes": 0.5
						},
						"startCpuCoreCount": null,
						"swapEnabled": null
					}
				}
			}`,
			want: &CustomAutoscaling{
				CPUMode:            "SHARED",
				MinCPU:             1,
				MaxCPU:             2,
				MinRAM:             0.5,
				MaxRAM:             2,
				MinDisk:            1,
				MaxDisk:            250,
				StartCPUCoreCount:  2,
				HorizontalMinCount: 1,
				HorizontalMaxCount: 10,
			},
		},
		{
			name: "dedicated_cpu",
			json: `{
				"currentAutoscaling": {
					"verticalAutoscaling": {
						"cpuMode": "DEDICATED",
						"maxResource": {"cpuCoreCount": 8, "diskGBytes": 100, "memoryGBytes": 32},
						"minResource": {"cpuCoreCount": 2, "diskGBytes": 5, "memoryGBytes": 4},
						"startCpuCoreCount": 4
					},
					"horizontalAutoscaling": {
						"maxContainerCount": 3,
						"minContainerCount": 1
					}
				}
			}`,
			want: &CustomAutoscaling{
				CPUMode:            "DEDICATED",
				MinCPU:             2,
				MaxCPU:             8,
				MinRAM:             4,
				MaxRAM:             32,
				MinDisk:            5,
				MaxDisk:            100,
				StartCPUCoreCount:  4,
				HorizontalMinCount: 1,
				HorizontalMaxCount: 3,
			},
		},
		{
			name: "null_autoscaling",
			json: `{
				"currentAutoscaling": null,
				"customAutoscaling": null
			}`,
			want: nil,
		},
		{
			name: "missing_autoscaling",
			json: `{
				"id": "some-service-id"
			}`,
			want: nil,
		},
		{
			name: "current_null_custom_present",
			json: `{
				"currentAutoscaling": null,
				"customAutoscaling": {
					"verticalAutoscaling": {
						"cpuMode": "SHARED",
						"maxResource": {"cpuCoreCount": 4, "diskGBytes": 50, "memoryGBytes": 8},
						"minResource": {"cpuCoreCount": 1, "diskGBytes": 1, "memoryGBytes": 1}
					},
					"horizontalAutoscaling": null
				}
			}`,
			want: &CustomAutoscaling{
				CPUMode: "SHARED",
				MinCPU:  1,
				MaxCPU:  4,
				MinRAM:  1,
				MaxRAM:  8,
				MinDisk: 1,
				MaxDisk: 50,
			},
		},
		{
			name: "null_inner_values_use_current",
			json: `{
				"currentAutoscaling": {
					"verticalAutoscaling": {
						"cpuMode": "SHARED",
						"maxResource": {"cpuCoreCount": 2, "diskGBytes": 250, "memoryGBytes": 2},
						"minResource": {"cpuCoreCount": 1, "diskGBytes": 1, "memoryGBytes": 0.5},
						"startCpuCoreCount": 2
					},
					"horizontalAutoscaling": {
						"maxContainerCount": 10,
						"minContainerCount": 1
					}
				},
				"customAutoscaling": {
					"verticalAutoscaling": {
						"cpuMode": "SHARED",
						"maxResource": {"cpuCoreCount": 2, "diskGBytes": null, "memoryGBytes": 2},
						"minResource": {"cpuCoreCount": 1, "diskGBytes": null, "memoryGBytes": 0.5},
						"startCpuCoreCount": null
					},
					"horizontalAutoscaling": {
						"maxContainerCount": null,
						"minContainerCount": null
					}
				}
			}`,
			want: &CustomAutoscaling{
				CPUMode:            "SHARED",
				MinCPU:             1,
				MaxCPU:             2,
				MinRAM:             0.5,
				MaxRAM:             2,
				MinDisk:            1,
				MaxDisk:            250,
				StartCPUCoreCount:  2,
				HorizontalMinCount: 1,
				HorizontalMaxCount: 10,
			},
		},
		{
			name:    "invalid_json",
			json:    `{not valid json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseRawAutoscaling([]byte(tt.json))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.want == nil {
				if got != nil {
					t.Fatalf("want nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("want non-nil, got nil")
			}

			if got.CPUMode != tt.want.CPUMode {
				t.Errorf("CPUMode = %q, want %q", got.CPUMode, tt.want.CPUMode)
			}
			if got.MinCPU != tt.want.MinCPU {
				t.Errorf("MinCPU = %d, want %d", got.MinCPU, tt.want.MinCPU)
			}
			if got.MaxCPU != tt.want.MaxCPU {
				t.Errorf("MaxCPU = %d, want %d", got.MaxCPU, tt.want.MaxCPU)
			}
			if got.MinRAM != tt.want.MinRAM {
				t.Errorf("MinRAM = %f, want %f", got.MinRAM, tt.want.MinRAM)
			}
			if got.MaxRAM != tt.want.MaxRAM {
				t.Errorf("MaxRAM = %f, want %f", got.MaxRAM, tt.want.MaxRAM)
			}
			if got.MinDisk != tt.want.MinDisk {
				t.Errorf("MinDisk = %f, want %f", got.MinDisk, tt.want.MinDisk)
			}
			if got.MaxDisk != tt.want.MaxDisk {
				t.Errorf("MaxDisk = %f, want %f", got.MaxDisk, tt.want.MaxDisk)
			}
			if got.StartCPUCoreCount != tt.want.StartCPUCoreCount {
				t.Errorf("StartCPUCoreCount = %d, want %d", got.StartCPUCoreCount, tt.want.StartCPUCoreCount)
			}
			if got.HorizontalMinCount != tt.want.HorizontalMinCount {
				t.Errorf("HorizontalMinCount = %d, want %d", got.HorizontalMinCount, tt.want.HorizontalMinCount)
			}
			if got.HorizontalMaxCount != tt.want.HorizontalMaxCount {
				t.Errorf("HorizontalMaxCount = %d, want %d", got.HorizontalMaxCount, tt.want.HorizontalMaxCount)
			}
		})
	}
}
