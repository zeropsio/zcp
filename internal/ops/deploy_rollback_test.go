// Tests for: ops/deploy_rollback.go — ReactivateAppVersion, the GF-8
// rollback path (docs/spec-workflows.md §8 R2 generalised / §12.6 GF-8):
// re-activate a specific recorded appVersion by id, no build.
package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestReactivateAppVersion_Backup_Reactivates pins the happy path: a
// BACKUP appVersion id is re-deployed with an EMPTY body (the platform
// ignores the body for a BACKUP target — live-verified 2026-09-14), the
// returned process is polled to FINISHED, and the result names the
// re-activated id with no-build wording.
func TestReactivateAppVersion_Backup_Reactivates(t *testing.T) {
	t.Parallel()

	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "appstage", Status: platform.ServiceStatusActive}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-new", ServiceStackID: "s1", Status: platform.ServiceStatusActive, Sequence: 2},
			{ID: "av-old", ServiceStackID: "s1", Status: platform.BuildStatusBackup, Sequence: 1},
		}).
		WithRedeployAppVersionProcess(&platform.Process{ID: "proc-1", Status: platform.ProcessStatusPending, ActionName: "stack.deploy.backup"}).
		WithProcess(&platform.Process{ID: "proc-1", Status: platform.ProcessStatusFinished, ActionName: "stack.deploy.backup"})

	result, err := ReactivateAppVersion(context.Background(), client, "p-1", "appstage", "av-old")
	if err != nil {
		t.Fatalf("ReactivateAppVersion: %v", err)
	}
	if result.Status != platform.BuildStatusDeployed {
		t.Errorf("Status = %q, want %q", result.Status, platform.BuildStatusDeployed)
	}
	if result.AppVersionID != "av-old" {
		t.Errorf("AppVersionID = %q, want av-old", result.AppVersionID)
	}
	if result.TargetService != "appstage" {
		t.Errorf("TargetService = %q, want appstage", result.TargetService)
	}
	if !strings.Contains(result.Message, "av-old") || !strings.Contains(result.Message, "appstage") || !strings.Contains(result.Message, "no build") {
		t.Errorf("Message = %q, want it to name the appVersion, target, and no-build", result.Message)
	}

	if len(client.CapturedRedeployAppVersion) != 1 {
		t.Fatalf("CapturedRedeployAppVersion len = %d, want 1", len(client.CapturedRedeployAppVersion))
	}
	got := client.CapturedRedeployAppVersion[0]
	if got.AppVersionID != "av-old" {
		t.Errorf("AppVersionID sent = %q, want av-old", got.AppVersionID)
	}
	if got.ZeropsYaml != "" || got.Setup != "" {
		t.Errorf("body = (yaml=%q, setup=%q), want both empty — the client sends them as JSON null, which the platform ignores for a BACKUP target (empty strings are rejected)", got.ZeropsYaml, got.Setup)
	}
}

// TestReactivateAppVersion_Active_RefusesNoOp pins the "already active"
// refusal: the target id resolves to the CURRENTLY active appVersion —
// refuse with a clear message, zero platform mutations.
func TestReactivateAppVersion_Active_RefusesNoOp(t *testing.T) {
	t.Parallel()

	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "appstage", Status: platform.ServiceStatusActive}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-new", ServiceStackID: "s1", Status: platform.ServiceStatusActive, Sequence: 2},
			{ID: "av-old", ServiceStackID: "s1", Status: platform.BuildStatusBackup, Sequence: 1},
		})

	_, err := ReactivateAppVersion(context.Background(), client, "p-1", "appstage", "av-new")
	if err == nil {
		t.Fatal("expected an error refusing the already-active id, got nil")
	}
	if !strings.Contains(err.Error(), "already active") {
		t.Errorf("err = %q, want it to say already active", err.Error())
	}
	if len(client.CapturedRedeployAppVersion) != 0 {
		t.Errorf("RedeployAppVersion must not be called on the active id: %+v", client.CapturedRedeployAppVersion)
	}
}

// TestReactivateAppVersion_UnknownID_RefusesWithCandidates pins the
// not-found refusal: an id that isn't in hostname's app-version history
// refuses naming the target, and lists the ids/statuses/sequence the
// caller can choose from instead, newest first, capped at 5.
func TestReactivateAppVersion_UnknownID_RefusesWithCandidates(t *testing.T) {
	t.Parallel()

	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "appstage", Status: platform.ServiceStatusActive}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-new", ServiceStackID: "s1", Status: platform.ServiceStatusActive, Sequence: 2},
			{ID: "av-old", ServiceStackID: "s1", Status: platform.BuildStatusBackup, Sequence: 1},
		})

	_, err := ReactivateAppVersion(context.Background(), client, "p-1", "appstage", "av-nope")
	if err == nil {
		t.Fatal("expected an error naming the unknown id, got nil")
	}
	if !strings.Contains(err.Error(), "av-nope") || !strings.Contains(err.Error(), "appstage") {
		t.Errorf("err = %q, want it to name the id and the hostname", err.Error())
	}
	if !strings.Contains(err.Error(), "av-new") || !strings.Contains(err.Error(), "av-old") {
		t.Errorf("err = %q, want it to list the candidate ids", err.Error())
	}
	if len(client.CapturedRedeployAppVersion) != 0 {
		t.Errorf("RedeployAppVersion must not be called on an unknown id: %+v", client.CapturedRedeployAppVersion)
	}
}

// TestReactivateAppVersion_WrongStatus_Refuses pins the third refusal
// shape: an id that exists but is neither ACTIVE nor BACKUP (e.g. still
// DEPLOY_FAILED) names the status and never calls the platform.
func TestReactivateAppVersion_WrongStatus_Refuses(t *testing.T) {
	t.Parallel()

	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "s1", Name: "appstage", Status: platform.ServiceStatusActive}}).
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-new", ServiceStackID: "s1", Status: platform.ServiceStatusActive, Sequence: 2},
			{ID: "av-failed", ServiceStackID: "s1", Status: platform.BuildStatusDeployFailed, Sequence: 1},
		})

	_, err := ReactivateAppVersion(context.Background(), client, "p-1", "appstage", "av-failed")
	if err == nil {
		t.Fatal("expected an error naming the status, got nil")
	}
	if !strings.Contains(err.Error(), platform.BuildStatusDeployFailed) {
		t.Errorf("err = %q, want it to name %s", err.Error(), platform.BuildStatusDeployFailed)
	}
	if len(client.CapturedRedeployAppVersion) != 0 {
		t.Errorf("RedeployAppVersion must not be called on a non-BACKUP id: %+v", client.CapturedRedeployAppVersion)
	}
}
