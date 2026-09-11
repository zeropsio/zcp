// Tests for: port mapping and timestamp formatting through mappers.

package platform

import (
	"regexp"
	"testing"
	"time"

	"github.com/zeropsio/zerops-go/dto/output"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/enum"
	"github.com/zeropsio/zerops-go/types/uuid"
)

// rfc3339MapperRe matches RFC3339/RFC3339Nano timestamps.
var rfc3339MapperRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)

// mustAppVersionID builds a uuid.AppVersionId from a plain string for test
// fixtures, failing the test on parse error.
func mustAppVersionID(t *testing.T, id string) uuid.AppVersionId {
	t.Helper()
	v, err := uuid.NewAppVersionIdFromString(id)
	if err != nil {
		t.Fatalf("NewAppVersionIdFromString(%q) failed: %v", id, err)
	}
	return v
}

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
				{Port: 8080, Protocol: "tcp", Public: true, HTTPSupport: false},
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
					Scheme:      enum.ServicePortSchemeEnumHttp,
				},
			},
			wantPorts: []Port{
				{Port: 3000, Protocol: "tcp", Public: true, HTTPSupport: true, Scheme: "http"},
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
				{Port: 5432, Protocol: "tcp", Public: false, HTTPSupport: false},
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
				{Port: 80, Protocol: "tcp", Public: true, HTTPSupport: true},
				{Port: 5432, Protocol: "tcp", Public: false, HTTPSupport: false},
			},
		},
		{
			name: "http_support_null_routing",
			sdkPorts: output.EsServiceStackPorts{
				{
					Port:     types.NewInt(9000),
					Protocol: enum.ServicePortProtocolEnumTcp,
					// HttpRouting not set (null) — HTTPSupport should be false
				},
			},
			wantPorts: []Port{
				{Port: 9000, Protocol: "tcp", Public: false, HTTPSupport: false},
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
				if got.HTTPSupport != want.HTTPSupport {
					t.Errorf("Ports[%d].HTTPSupport = %v, want %v", i, got.HTTPSupport, want.HTTPSupport)
				}
				if got.Scheme != want.Scheme {
					t.Errorf("Ports[%d].Scheme = %q, want %q", i, got.Scheme, want.Scheme)
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
					Scheme:      enum.ServicePortSchemeEnumHttp,
				},
				{
					Port:        types.NewInt(9090),
					Protocol:    enum.ServicePortProtocolEnumUdp,
					PortRouting: types.NewBoolNull(false),
					HttpRouting: types.NewBoolNull(false),
				},
			},
			wantPorts: []Port{
				{Port: 3000, Protocol: "tcp", Public: true, HTTPSupport: true, Scheme: "http"},
				{Port: 9090, Protocol: "udp", Public: false, HTTPSupport: false},
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
				if got.HTTPSupport != want.HTTPSupport {
					t.Errorf("Ports[%d].HTTPSupport = %v, want %v", i, got.HTTPSupport, want.HTTPSupport)
				}
				if got.Scheme != want.Scheme {
					t.Errorf("Ports[%d].Scheme = %q, want %q", i, got.Scheme, want.Scheme)
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

// TestMapActiveAppVersion_CreatedSourcePublicGitSource pins the digest
// fields the O7 artifact-promotion oracle reads directly off
// ListServicesDirect's ActiveAppVersion (docs/spec-eval-farm.md §4.4 O7,
// finding E2) — Created, Source and PublicGitSource, populated straight
// from the SDK's full GetAppVersion DTO, never through the ES-backed
// SearchAppVersions index.
func TestMapActiveAppVersion_CreatedSourcePublicGitSource(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		av         output.GetAppVersion
		wantSource string
		wantGitURL string
		wantHasGit bool
	}{
		{
			name: "cli_source_no_git_source",
			av: output.GetAppVersion{
				Id:      mustAppVersionID(t, "av-cli"),
				Created: types.NewDateTime(now),
				Source:  enum.AppVersionSourceEnumCli,
			},
			wantSource: "CLI",
			wantHasGit: false,
		},
		{
			name: "git_source_carries_public_git_source",
			av: output.GetAppVersion{
				Id:      mustAppVersionID(t, "av-git"),
				Created: types.NewDateTime(now),
				Source:  enum.AppVersionSourceEnumGit,
				PublicGitSource: &output.AppVersionPublicGitSource{
					GitUrl:     types.NewString("https://github.com/example/repo"),
					BranchName: types.NewString("main"),
				},
			},
			wantSource: "GIT",
			wantGitURL: "https://github.com/example/repo",
			wantHasGit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := mapActiveAppVersion(&tt.av)
			if result == nil {
				t.Fatal("mapActiveAppVersion returned nil")
			}
			if !rfc3339MapperRe.MatchString(result.Created) {
				t.Errorf("Created not RFC3339: %q", result.Created)
			}
			if result.Source != tt.wantSource {
				t.Errorf("Source = %q, want %q", result.Source, tt.wantSource)
			}
			if tt.wantHasGit {
				if result.PublicGitSource == nil {
					t.Fatal("PublicGitSource is nil, want set")
				}
				if result.PublicGitSource.GitURL != tt.wantGitURL {
					t.Errorf("PublicGitSource.GitURL = %q, want %q", result.PublicGitSource.GitURL, tt.wantGitURL)
				}
			} else if result.PublicGitSource != nil {
				t.Errorf("PublicGitSource = %+v, want nil", result.PublicGitSource)
			}
		})
	}
}
