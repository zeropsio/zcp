// Tests for: port mapping and timestamp formatting through mappers.

package platform

import (
	"regexp"
	"testing"
	"time"

	"github.com/zeropsio/zerops-go/dto/output"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/enum"
)

// rfc3339MapperRe matches RFC3339/RFC3339Nano timestamps.
var rfc3339MapperRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)

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

func TestMapProcess_TimestampsRFC3339(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 23, 14, 30, 0, 0, time.UTC)
	started := now.Add(1 * time.Second)
	finished := now.Add(5 * time.Second)

	p := output.Process{
		Created:  types.NewDateTime(now),
		Started:  types.NewDateTimeNull(started),
		Finished: types.NewDateTimeNull(finished),
		Status:   enum.ProcessStatusEnumRunning,
	}
	result := mapProcess(p)

	if !rfc3339MapperRe.MatchString(result.Created) {
		t.Errorf("Created not RFC3339: %q", result.Created)
	}
	if result.Created != now.Format(time.RFC3339Nano) {
		t.Errorf("Created = %q, want %q", result.Created, now.Format(time.RFC3339Nano))
	}
	if result.Started == nil {
		t.Fatal("Started is nil")
	}
	if !rfc3339MapperRe.MatchString(*result.Started) {
		t.Errorf("Started not RFC3339: %q", *result.Started)
	}
	if result.Finished == nil {
		t.Fatal("Finished is nil")
	}
	if !rfc3339MapperRe.MatchString(*result.Finished) {
		t.Errorf("Finished not RFC3339: %q", *result.Finished)
	}
}

func TestMapEsServiceStack_TimestampsRFC3339(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 23, 14, 30, 0, 0, time.UTC)
	later := now.Add(10 * time.Second)

	es := output.EsServiceStack{
		Created:    types.NewDateTime(now),
		LastUpdate: types.NewDateTime(later),
	}
	result := mapEsServiceStack(es)

	if !rfc3339MapperRe.MatchString(result.Created) {
		t.Errorf("Created not RFC3339: %q", result.Created)
	}
	if !rfc3339MapperRe.MatchString(result.LastUpdate) {
		t.Errorf("LastUpdate not RFC3339: %q", result.LastUpdate)
	}
}

func TestMapFullServiceStack_TimestampsRFC3339(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 23, 14, 30, 0, 0, time.UTC)
	later := now.Add(10 * time.Second)

	ss := output.ServiceStack{
		Created:    types.NewDateTime(now),
		LastUpdate: types.NewDateTime(later),
	}
	result := mapFullServiceStack(ss)

	if !rfc3339MapperRe.MatchString(result.Created) {
		t.Errorf("Created not RFC3339: %q", result.Created)
	}
	if !rfc3339MapperRe.MatchString(result.LastUpdate) {
		t.Errorf("LastUpdate not RFC3339: %q", result.LastUpdate)
	}
}
