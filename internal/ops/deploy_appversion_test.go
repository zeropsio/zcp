// Tests for: ops/deploy_appversion.go — RedeployLastAppVersion, the ONLY
// in-place recovery for a never-activated buildFromGit service (docs/
// spec-workflows.md §8 R2).
package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestRedeployLastAppVersion_UserDataYaml_PollsToDeployed pins the happy
// path: the newest DEPLOY_FAILED appVersion's ZEROPS_YAML blob is sent
// verbatim (yaml source #1), the platform's returned stack.deploy process
// is polled to FINISHED, and the result reports DEPLOYED.
func TestRedeployLastAppVersion_UserDataYaml_PollsToDeployed(t *testing.T) {
	t.Parallel()

	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-2", ServiceStackID: "s1", Status: platform.BuildStatusDeployFailed, Source: "GIT", Sequence: 2},
		}).
		WithAppVersionZeropsYaml("av-2", "run:\n  start: npm start\n").
		WithRedeployAppVersionProcess(&platform.Process{ID: "proc-1", Status: platform.ProcessStatusPending, ActionName: "stack.deploy"}).
		WithProcess(&platform.Process{ID: "proc-1", Status: platform.ProcessStatusFinished, ActionName: "stack.deploy"})

	result, err := RedeployLastAppVersion(context.Background(), client, "p-1", "api", "prod")
	if err != nil {
		t.Fatalf("RedeployLastAppVersion: %v", err)
	}
	if result.Status != platform.BuildStatusDeployed {
		t.Errorf("Status = %q, want %q", result.Status, platform.BuildStatusDeployed)
	}
	if result.TargetService != "api" {
		t.Errorf("TargetService = %q, want api", result.TargetService)
	}
	if result.TargetServiceID != "s1" {
		t.Errorf("TargetServiceID = %q, want s1", result.TargetServiceID)
	}

	if len(client.CapturedRedeployAppVersion) != 1 {
		t.Fatalf("CapturedRedeployAppVersion len = %d, want 1", len(client.CapturedRedeployAppVersion))
	}
	got := client.CapturedRedeployAppVersion[0]
	if got.AppVersionID != "av-2" {
		t.Errorf("AppVersionID = %q, want av-2", got.AppVersionID)
	}
	if got.ZeropsYaml != "run:\n  start: npm start\n" {
		t.Errorf("ZeropsYaml = %q, want the ZEROPS_YAML blob verbatim", got.ZeropsYaml)
	}
	if got.Setup != "prod" {
		t.Errorf("Setup = %q, want prod", got.Setup)
	}
}

// TestRedeployLastAppVersion_ArchiveFallback pins the yaml cascade's second
// source: when the appVersion carries no ZEROPS_YAML blob, the app-code
// archive is fetched and its zerops.yaml extracted. archiveYamlFetcher is
// swapped for a stub so the test never performs real HTTP+zip — package-
// level var, not parallel-safe, restored via t.Cleanup.
func TestRedeployLastAppVersion_ArchiveFallback(t *testing.T) {
	orig := archiveYamlFetcher
	archiveYamlFetcher = func(_ context.Context, url string) (string, error) {
		if url != "https://example.test/archive.zip" {
			t.Errorf("archiveYamlFetcher got url %q", url)
		}
		return "run:\n  start: from-archive\n", nil
	}
	t.Cleanup(func() { archiveYamlFetcher = orig })

	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-2", ServiceStackID: "s1", Status: platform.BuildStatusDeployFailed, Source: "GIT", Sequence: 2},
		}).
		WithAppVersionAppCode("av-2", "https://example.test/archive.zip").
		WithRedeployAppVersionProcess(&platform.Process{ID: "proc-1", Status: platform.ProcessStatusPending, ActionName: "stack.deploy"}).
		WithProcess(&platform.Process{ID: "proc-1", Status: platform.ProcessStatusFinished, ActionName: "stack.deploy"})

	result, err := RedeployLastAppVersion(context.Background(), client, "p-1", "api", "prod")
	if err != nil {
		t.Fatalf("RedeployLastAppVersion: %v", err)
	}
	if result.Status != platform.BuildStatusDeployed {
		t.Errorf("Status = %q, want %q", result.Status, platform.BuildStatusDeployed)
	}
	if len(client.CapturedRedeployAppVersion) != 1 {
		t.Fatalf("CapturedRedeployAppVersion len = %d, want 1", len(client.CapturedRedeployAppVersion))
	}
	if got := client.CapturedRedeployAppVersion[0].ZeropsYaml; got != "run:\n  start: from-archive\n" {
		t.Errorf("ZeropsYaml = %q, want the archive-extracted text", got)
	}
}

// TestRedeployLastAppVersion_NoYaml_Errors pins the exhausted-cascade case:
// neither the ZEROPS_YAML blob nor the app-code archive yields yaml text —
// RedeployLastAppVersion errors naming both attempts, and never calls
// RedeployAppVersion (nothing to send).
func TestRedeployLastAppVersion_NoYaml_Errors(t *testing.T) {
	t.Parallel()

	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "api", Status: platform.ServiceStatusReadyToDeploy}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-2", ServiceStackID: "s1", Status: platform.BuildStatusDeployFailed, Source: "GIT", Sequence: 2},
		})
		// No WithAppVersionZeropsYaml, no WithAppVersionAppCode — both
		// cascade steps miss.

	_, err := RedeployLastAppVersion(context.Background(), client, "p-1", "api", "prod")
	if err == nil {
		t.Fatal("expected an error naming both yaml-source attempts, got nil")
	}
	if !strings.Contains(err.Error(), "ZEROPS_YAML") || !strings.Contains(err.Error(), "archive") {
		t.Errorf("error = %q, want it to name both the ZEROPS_YAML blob and the archive attempt", err.Error())
	}
	if len(client.CapturedRedeployAppVersion) != 0 {
		t.Errorf("RedeployAppVersion was called with no yaml resolved: %+v", client.CapturedRedeployAppVersion)
	}
}
