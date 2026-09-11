package eval

import (
	"context"
	"strings"
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
		// ActiveAppVersion is the direct-read id evaluateArtifactPromotionRows
		// now resolves against SearchAppVersions (finding E2) — it must match
		// the ACTIVE entry each fixture's SearchAppVersions response carries.
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-dev-1"}},
			{ID: "svc-stage", Name: "appstage", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-stage-2"}},
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
		// ActiveAppVersion is the direct-read id evaluateArtifactPromotionRows
		// now resolves against SearchAppVersions (finding E2).
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-dev-1"}},
			{ID: "svc-stage", Name: "appstage", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-stage-2"}},
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
		// ActiveAppVersion is the direct-read id evaluateArtifactPromotionRows
		// now resolves against SearchAppVersions (finding E2); svc-dev's
		// deliberately does NOT match the baseline id below.
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-dev-2"}},
			{ID: "svc-stage", Name: "appstage", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-stage-2"}},
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

// TestVerification_ArtifactPromotion_SearchError_TargetFieldsBlocked pins
// finding E2's dev_unchanged/SearchAppVersions split: dev_unchanged is graded
// straight from the direct-read service list and the scenario baseline, so a
// SearchAppVersions failure blocks only the three target-scoped fields —
// dev_unchanged still evaluates (and here, since the direct-read id matches
// the baseline, passes). Renamed from …_AllRowsBlocked: under the old
// contract dev_unchanged also depended on SearchAppVersions succeeding.
func TestVerification_ArtifactPromotion_SearchError_TargetFieldsBlocked(t *testing.T) {
	t.Parallel()
	runStart := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	entry := ArtifactPromotionEntry{From: "appdev", To: "appstage"}
	client := platform.NewMock().
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-dev-1"}},
			{ID: "svc-stage", Name: "appstage", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-stage-2"}},
		}).
		WithError("SearchAppVersions", errBoomArtifactPromotionSearch)
	baseline := &ScenarioBaseline{AppVersions: map[string]string{"appdev": "av-dev-1"}}

	rows := evaluateArtifactPromotionRows(context.Background(), entry, client, "p1", runStart, baseline)
	assertRowResults(t, rows, map[string]CheckResult{
		"artifact_promotion/appstage/created_after_start": CheckBlocked,
		"artifact_promotion/appstage/source_cli":          CheckBlocked,
		"artifact_promotion/appstage/no_git_source":       CheckBlocked,
		"artifact_promotion/appstage/dev_unchanged":       CheckPassed,
	})
}

var errBoomArtifactPromotionSearch = artifactPromotionSearchError{}

type artifactPromotionSearchError struct{}

func (artifactPromotionSearchError) Error() string { return "boom" }

// TestArtifactPromotion_ActiveIDNotIndexed_Blocked pins finding E2 (live
// gate8): when the target's direct-read active appVersion id has not yet
// appeared in the ES-backed SearchAppVersions index, the three
// target-scoped rows block (after retrying) — never "no ACTIVE appVersion
// found", which would misreport a target that IS genuinely active as if it
// had none at all.
func TestArtifactPromotion_ActiveIDNotIndexed_Blocked(t *testing.T) {
	defer OverrideArtifactPromotionIndexRetryForTest([]time.Duration{time.Millisecond, time.Millisecond})()
	runStart := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	entry := ArtifactPromotionEntry{From: "appdev", To: "appstage"}
	mock := platform.NewMock().
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-dev-1"}},
			{ID: "svc-stage", Name: "appstage", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-stage-not-indexed-yet"}},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			// Present in the index, but never the id the direct read reports
			// as active — the ES index simply hasn't caught up yet.
			{ID: "av-stage-old", ServiceStackID: "svc-stage", Status: "ACTIVE", Source: "NONE", Created: "2026-09-09T00:00:00Z"},
		})
	client := &searchAppVersionsCountingClient{Mock: mock}
	baseline := &ScenarioBaseline{AppVersions: map[string]string{"appdev": "av-dev-1"}}

	rows := evaluateArtifactPromotionRows(context.Background(), entry, client, "p1", runStart, baseline)
	assertRowResults(t, rows, map[string]CheckResult{
		"artifact_promotion/appstage/created_after_start": CheckBlocked,
		"artifact_promotion/appstage/source_cli":          CheckBlocked,
		"artifact_promotion/appstage/no_git_source":       CheckBlocked,
	})
	row := findRow(t, rows, "artifact_promotion/appstage/created_after_start")
	if !strings.Contains(row.Message, "av-stage-not-indexed-yet") || !strings.Contains(row.Message, "not yet indexed") {
		t.Errorf("Message = %q, want it to name the id and say not yet indexed", row.Message)
	}
	if got := client.calls; got < 2 {
		t.Errorf("SearchAppVersions calls = %d, want at least 2 (initial + at least one retry)", got)
	}
}

// searchAppVersionsCountingClient wraps platform.Mock to count
// SearchAppVersions calls directly — platform.Mock.SearchAppVersions does
// not call trackCall, so Mock.CallCounts can't observe the retry loop.
type searchAppVersionsCountingClient struct {
	*platform.Mock
	calls int
}

func (c *searchAppVersionsCountingClient) SearchAppVersions(ctx context.Context, projectID string, limit int) ([]platform.AppVersionEvent, error) {
	c.calls++
	return c.Mock.SearchAppVersions(ctx, projectID, limit)
}

// TestArtifactPromotion_SeedOnlyTarget_FailedCreatedBeforeStart pins finding
// E2's other half: once the target's direct-read active id IS found inside
// SearchAppVersions (a seed-only target that was never promoted has had
// time to index), a created timestamp before run start is real evidence —
// created_after_start fails with that timestamp in Observed, never "no
// ACTIVE appVersion found" (the lookup matches by id alone, regardless of
// the search entry's own Status field).
func TestArtifactPromotion_SeedOnlyTarget_FailedCreatedBeforeStart(t *testing.T) {
	t.Parallel()
	runStart := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	entry := ArtifactPromotionEntry{From: "appdev", To: "appstage"}
	client := platform.NewMock().
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-dev-1"}},
			{ID: "svc-stage", Name: "appstage", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-stage-seed"}},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-stage-seed", ServiceStackID: "svc-stage", Status: "BACKUP", Source: "NONE", Created: "2026-09-09T00:00:00Z"},
		})
	baseline := &ScenarioBaseline{AppVersions: map[string]string{"appdev": "av-dev-1"}}

	rows := evaluateArtifactPromotionRows(context.Background(), entry, client, "p1", runStart, baseline)
	row := findRow(t, rows, "artifact_promotion/appstage/created_after_start")
	if row.Result != CheckFailed {
		t.Fatalf("created_after_start = %+v, want failed (true evidence), not blocked/no-active-found", row)
	}
	if row.Observed != "2026-09-09T00:00:00Z" {
		t.Errorf("Observed = %q, want the seed's created timestamp", row.Observed)
	}
}
