// Helpers for zerops_lease tests in a given container (runtime.Info) and the
// variables a pool service holds on the mock platform.
package tools

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// leaseToolServerIn is leaseToolServerSSH for the runtime rt, answering the
// mock platform too.
func leaseToolServerIn(t *testing.T, rt runtime.Info, ssh ops.SSHDeployer) (srv *mcp.Server, stateDir string, mock *platform.Mock) {
	t.Helper()
	hs := httptest.NewServer(&fakeOrchestrator{})
	t.Cleanup(hs.Close)
	stateDir = t.TempDir()
	mock = platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "proj"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: leaseTestToken, Sensitive: true}})
	factory := func(_, token string) ops.SwarmAPI { return platform.NewSwarmClient(hs.URL, token, hs.Client()) }
	srv = mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	registerLeaseTool(t, srv, mock, "proj-1", stateDir, factory, nil, ssh, rt)
	return srv, stateDir, mock
}

// serviceEnvVar is the variable key serviceID holds on mock.
func serviceEnvVar(t *testing.T, mock *platform.Mock, serviceID, key string) (platform.ServiceEnvVar, bool) {
	t.Helper()
	envs, err := mock.GetServiceEnv(context.Background(), serviceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range envs {
		if e.Key == key {
			return e, true
		}
	}
	return platform.ServiceEnvVar{}, false
}
