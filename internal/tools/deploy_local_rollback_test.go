// Tests for: tools/deploy_local.go — the appVersion=<id> rollback dispatch
// in LOCAL mode (docs/spec-workflows.md §12.6 GF-8), mirroring the SSH
// dispatch pinned by deploy_rollback_test.go. Closes the gap: local mode
// shares validateAppVersionParam (which accepts any id, not just
// "latest") but, before this, still routed EVERY non-empty appVersion to
// runAppVersionRedeploy (ops.RedeployLastAppVersion, newest-only) — a
// specific id would have been silently ignored in favor of the newest
// appVersion.
package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// TestDeployLocal_AppVersionID_ReactivatesBackup pins the local-mode
// rollback dispatch: a non-"latest" appVersion value is routed to
// ops.ReactivateAppVersion with THAT id — never ops.RedeployLastAppVersion
// (which would silently redeploy the newest instead).
func TestDeployLocal_AppVersionID_ReactivatesBackup(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "appstage", Status: platform.ServiceStatusActive}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-new", ServiceStackID: "s1", Status: platform.ServiceStatusActive, Sequence: 2},
			{ID: "av-old", ServiceStackID: "s1", Status: platform.BuildStatusBackup, Sequence: 1},
		}).
		WithRedeployAppVersionProcess(&platform.Process{ID: "proc-1", Status: platform.ProcessStatusPending, ActionName: "stack.deploy.backup"}).
		WithProcess(&platform.Process{ID: "proc-1", Status: platform.ProcessStatusFinished, ActionName: "stack.deploy.backup"})

	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeployLocal(srv, mock, okHTTP, "proj-1", authInfo, nil, t.TempDir(), testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appstage",
		"appVersion":    "av-old",
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
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

	if len(mock.CapturedRedeployAppVersion) != 1 {
		t.Fatalf("CapturedRedeployAppVersion len = %d, want 1", len(mock.CapturedRedeployAppVersion))
	}
	got := mock.CapturedRedeployAppVersion[0]
	if got.AppVersionID != "av-old" {
		t.Errorf("CapturedRedeployAppVersion.AppVersionID = %q, want av-old (never the newest av-new)", got.AppVersionID)
	}
}

// TestDeployLocal_AppVersionLatest_Unchanged pins that "latest" keeps
// dispatching to the R2 newest-only recovery (runAppVersionRedeploy /
// ops.RedeployLastAppVersion) in local mode, unaffected by the id-routing
// branch added alongside it.
func TestDeployLocal_AppVersionLatest_Unchanged(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-2", ServiceStackID: "s1", Status: platform.BuildStatusDeployFailed, Source: "GIT", Sequence: 2},
		}).
		WithAppVersionZeropsYaml("av-2", "run:\n  start: npm start\n").
		WithRedeployAppVersionProcess(&platform.Process{ID: "proc-1", Status: platform.ProcessStatusPending, ActionName: "stack.deploy"}).
		WithProcess(&platform.Process{ID: "proc-1", Status: platform.ProcessStatusFinished, ActionName: "stack.deploy"})

	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeployLocal(srv, mock, okHTTP, "proj-1", authInfo, nil, t.TempDir(), testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "api",
		"appVersion":    "latest",
		"setup":         "prod",
	})
	if result.IsError {
		t.Fatalf("unexpected IsError: %s", getTextContent(t, result))
	}

	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed.Status != statusDeployed {
		t.Errorf("status = %s, want %s", parsed.Status, statusDeployed)
	}

	if len(mock.CapturedRedeployAppVersion) != 1 {
		t.Fatalf("CapturedRedeployAppVersion len = %d, want 1", len(mock.CapturedRedeployAppVersion))
	}
	got := mock.CapturedRedeployAppVersion[0]
	if got.AppVersionID != "av-2" || got.Setup != "prod" {
		t.Errorf("CapturedRedeployAppVersion = %+v, want AppVersionID=av-2 Setup=prod (unchanged R2 newest-only path)", got)
	}
}

// TestDeployLocal_AppVersionID_ActiveRefuses pins the no-op refusal in
// local mode too.
func TestDeployLocal_AppVersionID_ActiveRefuses(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "appstage", Status: platform.ServiceStatusActive}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-new", ServiceStackID: "s1", Status: platform.ServiceStatusActive, Sequence: 2},
			{ID: "av-old", ServiceStackID: "s1", Status: platform.BuildStatusBackup, Sequence: 1},
		})

	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeployLocal(srv, mock, okHTTP, "proj-1", authInfo, nil, t.TempDir(), testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appstage",
		"appVersion":    "av-new",
	})
	if !result.IsError {
		t.Fatalf("expected IsError, got success: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "already active") {
		t.Errorf("error text = %q, want it to say already active", text)
	}
	if len(mock.CapturedRedeployAppVersion) != 0 {
		t.Errorf("RedeployAppVersion must not be called on the active id: %+v", mock.CapturedRedeployAppVersion)
	}
}
