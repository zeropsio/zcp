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
	if !strings.Contains(result.Message, "av-old") || !strings.Contains(result.Message, "appstage") || !strings.Contains(result.Message, "without a rebuild") {
		t.Errorf("Message = %q, want it to name the appVersion, target, and without-a-rebuild", result.Message)
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

// TestAppVersionCandidates_MarksActiveAndBackup pins the exported
// candidate reader zerops_events and the status envelope both render
// from (docs/spec-workflows.md §8 R2 / §12.6 GF-8): the currently ACTIVE
// row is flagged Active, a BACKUP (rollback-eligible) row is flagged
// Backup, and a row in neither status carries both flags false.
func TestAppVersionCandidates_MarksActiveAndBackup(t *testing.T) {
	t.Parallel()

	client := platform.NewMock().
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-new", ServiceStackID: "s1", Status: platform.ServiceStatusActive, Sequence: 3, Created: "2026-09-14T10:00:00Z"},
			{ID: "av-old", ServiceStackID: "s1", Status: platform.BuildStatusBackup, Sequence: 2, Created: "2026-09-13T10:00:00Z"},
			{ID: "av-failed", ServiceStackID: "s1", Status: platform.BuildStatusDeployFailed, Sequence: 1, Created: "2026-09-12T10:00:00Z"},
		})

	candidates, err := AppVersionCandidates(context.Background(), client, "s1")
	if err != nil {
		t.Fatalf("AppVersionCandidates: %v", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("len(candidates) = %d, want 3", len(candidates))
	}

	byID := make(map[string]AppVersionCandidate, len(candidates))
	for _, c := range candidates {
		byID[c.ID] = c
	}

	active := byID["av-new"]
	if !active.Active || active.Backup {
		t.Errorf("av-new = %+v, want Active=true Backup=false", active)
	}
	backup := byID["av-old"]
	if backup.Active || !backup.Backup {
		t.Errorf("av-old = %+v, want Active=false Backup=true", backup)
	}
	failed := byID["av-failed"]
	if failed.Active || failed.Backup {
		t.Errorf("av-failed = %+v, want Active=false Backup=false", failed)
	}
	if failed.Created != "2026-09-12T10:00:00Z" {
		t.Errorf("av-failed.Created = %q, want the seeded timestamp passed through", failed.Created)
	}
}

// TestAppVersionCandidates_OrderedNewestFirst pins that the candidate
// list preserves ListServiceAppVersions' own newest-first (Sequence
// descending) ordering — callers (the rollback error, zerops_events, the
// envelope) all rely on index 0 being the newest without re-sorting.
func TestAppVersionCandidates_OrderedNewestFirst(t *testing.T) {
	t.Parallel()

	client := platform.NewMock().
		WithServiceAppVersions("s1", []platform.AppVersionEvent{
			{ID: "av-3", ServiceStackID: "s1", Status: platform.ServiceStatusActive, Sequence: 3},
			{ID: "av-2", ServiceStackID: "s1", Status: platform.BuildStatusBackup, Sequence: 2},
			{ID: "av-1", ServiceStackID: "s1", Status: platform.BuildStatusBackup, Sequence: 1},
		})

	candidates, err := AppVersionCandidates(context.Background(), client, "s1")
	if err != nil {
		t.Fatalf("AppVersionCandidates: %v", err)
	}
	want := []string{"av-3", "av-2", "av-1"}
	got := make([]string, len(candidates))
	for i, c := range candidates {
		got[i] = c.ID
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidates[%d].ID = %q, want %q (order = %v)", i, got[i], want[i], got)
		}
	}
}

// TestAppVersionCandidates_PropagatesError pins that a platform error
// from ListServiceAppVersions is wrapped and returned, not swallowed.
func TestAppVersionCandidates_PropagatesError(t *testing.T) {
	t.Parallel()

	client := platform.NewMock().
		WithError("ListServiceAppVersions", platform.NewPlatformError(platform.ErrAPIError, "boom", ""))

	_, err := AppVersionCandidates(context.Background(), client, "s1")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %q, want it to wrap the underlying platform error", err.Error())
	}
}
