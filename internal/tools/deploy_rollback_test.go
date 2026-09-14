// Tests for: tools/deploy_ssh.go + tools/deploy_rollback.go — the
// appVersion=<id> rollback dispatch (docs/spec-workflows.md §8 R2
// generalised, §12.6 GF-8): distinct from appVersion="latest" (R2
// never-activated recovery, deploy_appversion_tool_test.go).
package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// TestDeploySSH_AppVersionID_ReactivatesBackup pins the rollback dispatch:
// a non-"latest" appVersion value is treated as an id, routed to
// ops.ReactivateAppVersion (never ops.RedeployLastAppVersion), never
// touches SSH (no source/container needed), and the response reports
// DEPLOYED with the re-activated id.
func TestDeploySSH_AppVersionID_ReactivatesBackup(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "appstage", Status: platform.ServiceStatusActive}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-new", ServiceStackID: "s1", Status: platform.ServiceStatusActive, Sequence: 2},
			{ID: "av-old", ServiceStackID: "s1", Status: platform.BuildStatusBackup, Sequence: 1},
		}).
		WithRedeployAppVersionProcess(&platform.Process{ID: "proc-1", Status: platform.ProcessStatusPending, ActionName: "stack.deploy.backup"}).
		WithProcess(&platform.Process{ID: "proc-1", Status: platform.ProcessStatusFinished, ActionName: "stack.deploy.backup"})

	ssh := &countingSSH{}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, t.TempDir(), testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appstage",
		"appVersion":    "av-old",
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}
	if ssh.calls != 0 {
		t.Errorf("SSH exec calls = %d, want 0 — rollback has no source/container to exec against", ssh.calls)
	}

	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed.Status != statusDeployed {
		t.Errorf("status = %s, want %s", parsed.Status, statusDeployed)
	}
	if parsed.AppVersionID != "av-old" {
		t.Errorf("appVersionId = %q, want av-old", parsed.AppVersionID)
	}
	if parsed.TargetService != "appstage" {
		t.Errorf("targetService = %q, want appstage", parsed.TargetService)
	}

	if len(mock.CapturedRedeployAppVersion) != 1 {
		t.Fatalf("CapturedRedeployAppVersion len = %d, want 1", len(mock.CapturedRedeployAppVersion))
	}
	got := mock.CapturedRedeployAppVersion[0]
	if got.AppVersionID != "av-old" {
		t.Errorf("CapturedRedeployAppVersion.AppVersionID = %q, want av-old (never the newest av-new)", got.AppVersionID)
	}
}

// TestDeploySSH_AppVersionID_ActiveRefuses pins the no-op refusal: the id
// named is already the active appVersion — refused, zero platform
// mutations.
func TestDeploySSH_AppVersionID_ActiveRefuses(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "appstage", Status: platform.ServiceStatusActive}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-new", ServiceStackID: "s1", Status: platform.ServiceStatusActive, Sequence: 2},
			{ID: "av-old", ServiceStackID: "s1", Status: platform.BuildStatusBackup, Sequence: 1},
		})

	ssh := &countingSSH{}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, t.TempDir(), testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appstage",
		"appVersion":    "av-new",
	})
	if !result.IsError {
		t.Fatalf("expected IsError, got success: %s", getTextContent(t, result))
	}
	if len(mock.CapturedRedeployAppVersion) != 0 {
		t.Errorf("RedeployAppVersion must not be called on the active id: %+v", mock.CapturedRedeployAppVersion)
	}
}

// TestDeploySSH_AppVersionID_UnknownRefuses pins the not-found refusal for
// an id that isn't in the target's app-version history.
func TestDeploySSH_AppVersionID_UnknownRefuses(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "appstage", Status: platform.ServiceStatusActive}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-new", ServiceStackID: "s1", Status: platform.ServiceStatusActive, Sequence: 2},
		})

	ssh := &countingSSH{}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, t.TempDir(), testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appstage",
		"appVersion":    "av-nope",
	})
	if !result.IsError {
		t.Fatalf("expected IsError, got success: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "av-nope") {
		t.Errorf("error text = %q, want it to name the unknown id", text)
	}
	if len(mock.CapturedRedeployAppVersion) != 0 {
		t.Errorf("RedeployAppVersion must not be called on an unknown id: %+v", mock.CapturedRedeployAppVersion)
	}
}
