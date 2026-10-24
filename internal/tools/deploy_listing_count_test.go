// Tests for: the service listings zerops_deploy, zerops_deploy_batch and
// zerops_manage make. ListServices is an ES-backed search (CLAUDE.md, §3.5):
// the pool and Box checks the dev-pool work added read a service's type from
// one list the call already holds, so a call in a project without a pool
// lists no more than it did before pools existed.
package tools

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// Listings each call made before the pool and Box checks (measured on
// origin/main with these same tests).
const (
	wantSelfDeployListings  = 3
	wantCrossDeployListings = 3
	wantBatchListings       = 6
	wantManageListings      = 1
)

func listingCountServices() []platform.ServiceStack {
	return []platform.ServiceStack{
		{ID: "svc-dev", Name: "appdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{ID: "svc-stage", Name: "appstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{ID: "svc-api", Name: "apistage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}
}

func listingCountMock() *platform.Mock {
	return platform.NewMock().
		WithServices(listingCountServices()).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-dev", ServiceStackID: "svc-dev", Status: statusActive, Sequence: 1},
			{ID: "av-stage", ServiceStackID: "svc-stage", Status: statusActive, Sequence: 1},
			{ID: "av-api", ServiceStackID: "svc-api", Status: statusActive, Sequence: 1},
		}).
		WithProcess(&platform.Process{ID: "proc-stop-svc-stage", Status: statusFinished}).
		WithProcess(&platform.Process{ID: "proc-restart-svc-stage", Status: statusFinished})
}

func TestDeployTool_NoPoolProject_ListsAsBeforePools(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args map[string]any
		want int
	}{
		{name: "self-deploy", args: map[string]any{"targetService": "appdev"}, want: wantSelfDeployListings},
		{name: "cross-deploy", args: map[string]any{"sourceService": "appdev", "targetService": "appstage"}, want: wantCrossDeployListings},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := listingCountMock()
			authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDeploySSH(srv, mock, okHTTP, "proj-1", &stubSSH{output: []byte("ok")}, authInfo, nil, runtime.Info{}, "", testDeployEngine(t), nil, DevsupSource{})

			result := callTool(t, srv, "zerops_deploy", tt.args)
			if result.IsError {
				t.Fatalf("deploy: %s", getTextContent(t, result))
			}
			if n := mock.CallCount("ListServices"); n != tt.want {
				t.Errorf("ListServices calls = %d, want %d (the count before the pool checks)", n, tt.want)
			}
		})
	}
}

func TestDeployBatchTool_NoPoolProject_ListsAsBeforePools(t *testing.T) {
	t.Parallel()
	mock := listingCountMock()
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeployBatch(srv, mock, okHTTP, "proj-1", &stubSSH{output: []byte("ok")}, authInfo, nil, runtime.Info{InContainer: true}, "", testDeployEngine(t), nil, DevsupSource{})

	result := callTool(t, srv, "zerops_deploy_batch", map[string]any{
		"targets": []map[string]any{
			{"sourceService": "appdev", "targetService": "appstage", "setup": "prod"},
			{"sourceService": "appdev", "targetService": "apistage", "setup": "prod"},
		},
	})
	if result.IsError {
		t.Fatalf("batch: %s", getTextContent(t, result))
	}
	if n := mock.CallCount("ListServices"); n != wantBatchListings {
		t.Errorf("ListServices calls = %d, want %d (the count before the pool checks)", n, wantBatchListings)
	}
}

func TestManageTool_StopRestart_ListsTheServicesOnce(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"stop", "restart"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			mock := listingCountMock()
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterManage(srv, mock, "proj-1")

			result := callTool(t, srv, "zerops_manage", map[string]any{"action": action, "serviceHostname": "appstage"})
			if result.IsError {
				t.Fatalf("%s: %s", action, getTextContent(t, result))
			}
			if n := mock.CallCount("ListServices"); n != wantManageListings {
				t.Errorf("ListServices calls = %d, want %d: the Box check reads the service the %s resolved", n, wantManageListings, action)
			}
		})
	}
}
