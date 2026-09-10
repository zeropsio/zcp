package eval

import (
	"context"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// These tests exercise evaluateArtifactPromotionRows against the current
// O7 row definition (docs/spec-eval-farm.md §4.4 O7): the target's ACTIVE
// appVersion must be created after run start, source "CLI", carry no
// publicGitSource, and the dev (From) service's ACTIVE appVersion id must
// be unchanged from the scenario baseline.

func TestVerification_ArtifactPromotion_CliTargetDevUnchanged_Passes(t *testing.T) {
	t.Parallel()
	runStart := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	entry := ArtifactPromotionEntry{From: "appdev", To: "appstage"}
	client := platform.NewMock().
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev"},
			{ID: "svc-stage", Name: "appstage"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			// Target's earlier startWithoutCode stamp, now backed up.
			{ID: "av-stage-1", ServiceStackID: "svc-stage", Status: "BACKUP", Source: "NONE", Created: "2026-09-09T00:00:00Z", Build: nil},
			// Target's cross-deployed appVersion: CLI source, no git source, created after run start.
			{ID: "av-stage-2", ServiceStackID: "svc-stage", Status: "ACTIVE", Source: "CLI", Created: "2026-09-10T01:00:00Z", Build: &platform.BuildInfo{}},
			// Dev service's active appVersion, unchanged from baseline.
			{ID: "av-dev-1", ServiceStackID: "svc-dev", Status: "ACTIVE", Source: "GIT", Created: "2026-09-09T00:00:00Z", Build: &platform.BuildInfo{}},
		})
	baseline := &ScenarioBaseline{AppVersions: map[string]string{"appdev": "av-dev-1"}}

	rows := evaluateArtifactPromotionRows(context.Background(), entry, client, "p1", runStart, baseline)
	assertRowResults(t, rows, map[string]CheckResult{
		"artifact_promotion/appstage/created_after_start": CheckPassed,
		"artifact_promotion/appstage/source_cli":          CheckPassed,
		"artifact_promotion/appstage/no_git_source":       CheckPassed,
		"artifact_promotion/appstage/dev_unchanged":       CheckPassed,
	})
}

func TestVerification_ArtifactPromotion_GitBuiltTarget_FailsSourceRow(t *testing.T) {
	t.Parallel()
	runStart := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	entry := ArtifactPromotionEntry{From: "appdev", To: "appstage"}
	client := platform.NewMock().
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev"},
			{ID: "svc-stage", Name: "appstage"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			// Target's ACTIVE appVersion is an independent GIT build, not a promotion.
			{
				ID: "av-stage-2", ServiceStackID: "svc-stage", Status: "ACTIVE", Source: "GIT",
				Created: "2026-09-10T01:00:00Z", Build: &platform.BuildInfo{},
				PublicGitSource: &platform.AppVersionGitSource{GitURL: "https://github.com/example/repo", BranchName: "main"},
			},
			{ID: "av-dev-1", ServiceStackID: "svc-dev", Status: "ACTIVE", Source: "GIT", Created: "2026-09-09T00:00:00Z", Build: &platform.BuildInfo{}},
		})
	baseline := &ScenarioBaseline{AppVersions: map[string]string{"appdev": "av-dev-1"}}

	rows := evaluateArtifactPromotionRows(context.Background(), entry, client, "p1", runStart, baseline)

	sourceRow := findRow(t, rows, "artifact_promotion/appstage/source_cli")
	if sourceRow.Result != CheckFailed {
		t.Fatalf("expected source_cli to fail for source=GIT, got %+v", sourceRow)
	}
	gitRow := findRow(t, rows, "artifact_promotion/appstage/no_git_source")
	if gitRow.Result != CheckFailed {
		t.Fatalf("expected no_git_source to fail when publicGitSource is set, got %+v", gitRow)
	}
}

func TestVerification_ArtifactPromotion_DevRebuilt_FailsUnchangedRow(t *testing.T) {
	t.Parallel()
	runStart := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	entry := ArtifactPromotionEntry{From: "appdev", To: "appstage"}
	client := platform.NewMock().
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev"},
			{ID: "svc-stage", Name: "appstage"},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-stage-2", ServiceStackID: "svc-stage", Status: "ACTIVE", Source: "CLI", Created: "2026-09-10T01:00:00Z", Build: &platform.BuildInfo{}},
			// Dev's active appVersion no longer matches the baseline id.
			{ID: "av-dev-2", ServiceStackID: "svc-dev", Status: "ACTIVE", Source: "GIT", Created: "2026-09-10T00:30:00Z", Build: &platform.BuildInfo{}},
		})
	baseline := &ScenarioBaseline{AppVersions: map[string]string{"appdev": "av-dev-1"}}

	rows := evaluateArtifactPromotionRows(context.Background(), entry, client, "p1", runStart, baseline)
	row := findRow(t, rows, "artifact_promotion/appstage/dev_unchanged")
	if row.Result != CheckFailed {
		t.Fatalf("expected dev_unchanged to fail when appdev's active appVersion no longer matches the baseline, got %+v", row)
	}
}

func TestVerification_ArtifactPromotion_SearchError_AllRowsBlocked(t *testing.T) {
	t.Parallel()
	runStart := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	entry := ArtifactPromotionEntry{From: "appdev", To: "appstage"}
	client := platform.NewMock().
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev"},
			{ID: "svc-stage", Name: "appstage"},
		}).
		WithError("SearchAppVersions", errBoomArtifactPromotionSearch)
	baseline := &ScenarioBaseline{AppVersions: map[string]string{"appdev": "av-dev-1"}}

	rows := evaluateArtifactPromotionRows(context.Background(), entry, client, "p1", runStart, baseline)
	assertRowResults(t, rows, map[string]CheckResult{
		"artifact_promotion/appstage/created_after_start": CheckBlocked,
		"artifact_promotion/appstage/source_cli":          CheckBlocked,
		"artifact_promotion/appstage/no_git_source":       CheckBlocked,
		"artifact_promotion/appstage/dev_unchanged":       CheckBlocked,
	})
}

var errBoomArtifactPromotionSearch = artifactPromotionSearchError{}

type artifactPromotionSearchError struct{}

func (artifactPromotionSearchError) Error() string { return "boom" }
