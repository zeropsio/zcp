package tools

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestEnrichTargetRuntimeTypes_HTTPSupport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		services    []platform.ServiceStack
		targets     []workflow.DeployTarget
		wantHTTP    map[string]bool
		wantRuntime map[string]string
	}{
		{
			name: "web_app_with_http_support",
			services: []platform.ServiceStack{
				{
					Name: "app",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName: "nodejs@22",
					},
					Ports: []platform.Port{
						{Port: 3000, HTTPSupport: true},
					},
				},
			},
			targets:     []workflow.DeployTarget{{Hostname: "app"}},
			wantHTTP:    map[string]bool{"app": true},
			wantRuntime: map[string]string{"app": "nodejs@22"},
		},
		{
			name: "managed_service_no_ports",
			services: []platform.ServiceStack{
				{
					Name: "db",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName: "postgresql@16",
					},
				},
			},
			targets:     []workflow.DeployTarget{{Hostname: "db"}},
			wantHTTP:    map[string]bool{"db": false},
			wantRuntime: map[string]string{"db": "postgresql@16"},
		},
		{
			name: "mixed_web_and_managed",
			services: []platform.ServiceStack{
				{
					Name: "app",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName: "nodejs@22",
					},
					Ports: []platform.Port{
						{Port: 3000, HTTPSupport: true},
					},
				},
				{
					Name: "db",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName: "postgresql@16",
					},
				},
			},
			targets: []workflow.DeployTarget{
				{Hostname: "app"},
				{Hostname: "db"},
			},
			wantHTTP:    map[string]bool{"app": true, "db": false},
			wantRuntime: map[string]string{"app": "nodejs@22", "db": "postgresql@16"},
		},
		{
			name: "multiple_ports_one_http",
			services: []platform.ServiceStack{
				{
					Name: "api",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName: "go@1",
					},
					Ports: []platform.Port{
						{Port: 8080, HTTPSupport: true},
						{Port: 9090, HTTPSupport: false},
					},
				},
			},
			targets:     []workflow.DeployTarget{{Hostname: "api"}},
			wantHTTP:    map[string]bool{"api": true},
			wantRuntime: map[string]string{"api": "go@1"},
		},
		{
			name: "all_ports_no_http",
			services: []platform.ServiceStack{
				{
					Name: "grpc",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{
						ServiceStackTypeVersionName: "go@1",
					},
					Ports: []platform.Port{
						{Port: 50051, HTTPSupport: false},
					},
				},
			},
			targets:     []workflow.DeployTarget{{Hostname: "grpc"}},
			wantHTTP:    map[string]bool{"grpc": false},
			wantRuntime: map[string]string{"grpc": "go@1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().WithServices(tt.services)
			targets := make([]workflow.DeployTarget, len(tt.targets))
			copy(targets, tt.targets)

			enrichTargetRuntimeTypes(context.Background(), mock, "proj-1", targets)

			for _, tgt := range targets {
				wantHTTP := tt.wantHTTP[tgt.Hostname]
				if tgt.HTTPSupport != wantHTTP {
					t.Errorf("%s: HTTPSupport = %v, want %v", tgt.Hostname, tgt.HTTPSupport, wantHTTP)
				}
				wantRT := tt.wantRuntime[tgt.Hostname]
				if tgt.RuntimeType != wantRT {
					t.Errorf("%s: RuntimeType = %q, want %q", tgt.Hostname, tgt.RuntimeType, wantRT)
				}
			}
		})
	}
}
