// Tests for: port mapping through mapEsServiceStack and mapFullServiceStack.

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
