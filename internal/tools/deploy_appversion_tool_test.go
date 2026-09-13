// Tests for: tools/deploy_ssh.go + tools/deploy_local.go — the appVersion
// input dispatch (docs/spec-workflows.md §8 R2 in-place recovery).
package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// countingSSH is a SSHDeployer double that records how many exec calls it
// received — used to assert the appVersion redeploy path never touches SSH
// at all (no source, no container to exec against).
type countingSSH struct {
	calls int
}

func (s *countingSSH) ExecSSH(_ context.Context, _, _ string) ([]byte, error) {
	s.calls++
	return nil, nil
}

func (s *countingSSH) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	s.calls++
	return nil, nil
}

// TestDeploySSH_AppVersionLatest_SkipsSourceAndRedeploys pins the R2
// in-place recovery dispatch (docs/spec-workflows.md §8 R2): appVersion=
// "latest" never touches SSH (no source resolution, no adoption gate — the
// target need not even carry a ServiceMeta), calls
// ops.RedeployLastAppVersion, and the response reports DEPLOYED.
func TestDeploySSH_AppVersionLatest_SkipsSourceAndRedeploys(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-2", ServiceStackID: "s1", Status: platform.BuildStatusDeployFailed, Source: "GIT", Sequence: 2},
		}).
		WithAppVersionZeropsYaml("av-2", "run:\n  start: npm start\n").
		WithRedeployAppVersionProcess(&platform.Process{ID: "proc-1", Status: platform.ProcessStatusPending, ActionName: "stack.deploy"}).
		WithProcess(&platform.Process{ID: "proc-1", Status: platform.ProcessStatusFinished, ActionName: "stack.deploy"})

	ssh := &countingSSH{}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	// No ServiceMeta written for "api" — the appVersion path must not need
	// adoption at all, unlike the normal source-resolving deploy path.
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, t.TempDir(), testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "api",
		"appVersion":    "latest",
		"setup":         "prod",
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}
	if ssh.calls != 0 {
		t.Errorf("SSH exec calls = %d, want 0 — the appVersion path has no source/container to exec against", ssh.calls)
	}

	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed.Status != statusDeployed {
		t.Errorf("status = %s, want %s", parsed.Status, statusDeployed)
	}
	if parsed.TargetService != "api" {
		t.Errorf("targetService = %q, want api", parsed.TargetService)
	}

	if len(mock.CapturedRedeployAppVersion) != 1 {
		t.Fatalf("CapturedRedeployAppVersion len = %d, want 1", len(mock.CapturedRedeployAppVersion))
	}
	got := mock.CapturedRedeployAppVersion[0]
	if got.AppVersionID != "av-2" || got.Setup != "prod" {
		t.Errorf("CapturedRedeployAppVersion = %+v, want AppVersionID=av-2 Setup=prod", got)
	}
}
