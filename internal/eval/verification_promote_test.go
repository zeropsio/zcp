package eval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// These tests exercise evaluateArtifactPromotionRows against the current
// O7 row definition (docs/spec-eval-farm.md §4.4 O7): the target's ACTIVE
// appVersion must be created after run start, source "CLI", carry no
// publicGitSource, and the dev (From) service's ACTIVE appVersion id must
// be unchanged from the scenario baseline.
//
// Finding T4: every field is read straight off ListServicesDirect's
// ActiveAppVersion digest — never through the ES-backed SearchAppVersions
// index (which can lag long enough after a finished deploy to report a
// genuinely active target's appVersion as "not yet indexed").

func TestVerification_ArtifactPromotion_CliTargetDevUnchanged_Passes(t *testing.T) {
	t.Parallel()
	runStart := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	entry := ArtifactPromotionEntry{From: "appdev", To: "appstage"}
	client := platform.NewMock().
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev", ActiveAppVersion: &platform.ActiveAppVersionDigest{
				ID: "av-dev-1", Source: "GIT", Created: "2026-09-09T00:00:00Z",
			}},
			{ID: "svc-stage", Name: "appstage", ActiveAppVersion: &platform.ActiveAppVersionDigest{
				// Target's cross-deployed appVersion: CLI source, no git source, created after run start.
				ID: "av-stage-2", Source: "CLI", Created: "2026-09-10T01:00:00Z",
			}},
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
			{ID: "svc-dev", Name: "appdev", ActiveAppVersion: &platform.ActiveAppVersionDigest{
				ID: "av-dev-1", Source: "GIT", Created: "2026-09-09T00:00:00Z",
			}},
			{ID: "svc-stage", Name: "appstage", ActiveAppVersion: &platform.ActiveAppVersionDigest{
				// Target's ACTIVE appVersion is an independent GIT build, not a promotion.
				ID: "av-stage-2", Source: "GIT", Created: "2026-09-10T01:00:00Z",
				PublicGitSource: &platform.AppVersionGitSource{GitURL: "https://github.com/example/repo", BranchName: "main"},
			}},
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
			// Dev's active appVersion no longer matches the baseline id below.
			{ID: "svc-dev", Name: "appdev", ActiveAppVersion: &platform.ActiveAppVersionDigest{
				ID: "av-dev-2", Source: "GIT", Created: "2026-09-10T00:30:00Z",
			}},
			{ID: "svc-stage", Name: "appstage", ActiveAppVersion: &platform.ActiveAppVersionDigest{
				ID: "av-stage-2", Source: "CLI", Created: "2026-09-10T01:00:00Z",
			}},
		})
	baseline := &ScenarioBaseline{AppVersions: map[string]string{"appdev": "av-dev-1"}}

	rows := evaluateArtifactPromotionRows(context.Background(), entry, client, "p1", runStart, baseline)
	row := findRow(t, rows, "artifact_promotion/appstage/dev_unchanged")
	if row.Result != CheckFailed {
		t.Fatalf("expected dev_unchanged to fail when appdev's active appVersion no longer matches the baseline, got %+v", row)
	}
}

// TestVerification_ArtifactPromotion_NoActiveAppVersion_TargetFieldsFail pins
// the "target service has no ACTIVE appVersion at all" branch (distinct from
// a target whose ACTIVE appVersion just isn't a CLI cross-deploy): the three
// target-scoped fields fail with "no ACTIVE appVersion found", and
// dev_unchanged still evaluates independently against the baseline.
func TestVerification_ArtifactPromotion_NoActiveAppVersion_TargetFieldsFail(t *testing.T) {
	t.Parallel()
	runStart := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	entry := ArtifactPromotionEntry{From: "appdev", To: "appstage"}
	client := platform.NewMock().
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-dev-1"}},
			{ID: "svc-stage", Name: "appstage", ActiveAppVersion: nil},
		})
	baseline := &ScenarioBaseline{AppVersions: map[string]string{"appdev": "av-dev-1"}}

	rows := evaluateArtifactPromotionRows(context.Background(), entry, client, "p1", runStart, baseline)
	assertRowResults(t, rows, map[string]CheckResult{
		"artifact_promotion/appstage/created_after_start": CheckFailed,
		"artifact_promotion/appstage/source_cli":          CheckFailed,
		"artifact_promotion/appstage/no_git_source":       CheckFailed,
		"artifact_promotion/appstage/dev_unchanged":       CheckPassed,
	})
}

// TestArtifactPromotion_DirectReadOnly_NoSearchCall pins finding T4: the O7
// artifact-promotion oracle never calls SearchAppVersions — every field
// comes from the direct-read ListServicesDirect digest. A platform mock
// whose SearchAppVersions call fails the test proves no ES-backed lookup
// happens on either a passing or a target-inactive path.
func TestArtifactPromotion_DirectReadOnly_NoSearchCall(t *testing.T) {
	t.Parallel()
	runStart := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	entry := ArtifactPromotionEntry{From: "appdev", To: "appstage"}
	baseline := &ScenarioBaseline{AppVersions: map[string]string{"appdev": "av-dev-1"}}

	tests := []struct {
		name     string
		services []platform.ServiceStack
	}{
		{
			name: "target_active_cli",
			services: []platform.ServiceStack{
				{ID: "svc-dev", Name: "appdev", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-dev-1", Source: "GIT", Created: "2026-09-09T00:00:00Z"}},
				{ID: "svc-stage", Name: "appstage", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-stage-2", Source: "CLI", Created: "2026-09-10T01:00:00Z"}},
			},
		},
		{
			name: "target_never_deployed",
			services: []platform.ServiceStack{
				{ID: "svc-dev", Name: "appdev", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-dev-1", Source: "GIT", Created: "2026-09-09T00:00:00Z"}},
				{ID: "svc-stage", Name: "appstage", ActiveAppVersion: nil},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &searchAppVersionsFailingClient{Mock: platform.NewMock().WithServicesDirect(tt.services), t: t}
			rows := evaluateArtifactPromotionRows(context.Background(), entry, client, "p1", runStart, baseline)
			if len(rows) != len(artifactPromotionFields) {
				t.Fatalf("got %d rows, want %d", len(rows), len(artifactPromotionFields))
			}
			for _, row := range rows {
				if row.Result != CheckPassed && row.Result != CheckFailed {
					t.Errorf("row %s = %+v, want resolved passed/failed (never blocked)", row.ID, row)
				}
			}
		})
	}
}

// searchAppVersionsFailingClient wraps platform.Mock and fails the test if
// SearchAppVersions is ever called — the O7 artifact-promotion oracle must
// resolve entirely from the direct-read digest.
type searchAppVersionsFailingClient struct {
	*platform.Mock
	t *testing.T
}

func (c *searchAppVersionsFailingClient) SearchAppVersions(ctx context.Context, projectID string, limit int) ([]platform.AppVersionEvent, error) {
	c.t.Fatal("SearchAppVersions must not be called by the O7 artifact-promotion oracle (finding T4)")
	return nil, errors.New("unreachable")
}

// TestArtifactPromotion_CreatedAfterStart_Table covers created_after_start
// true/false against runStart, resolved straight from the digest's Created
// field (no SearchAppVersions involved).
func TestArtifactPromotion_CreatedAfterStart_Table(t *testing.T) {
	t.Parallel()
	runStart := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		created string
		want    CheckResult
	}{
		{name: "created_after_run_start", created: "2026-09-10T01:00:00Z", want: CheckPassed},
		{name: "created_before_run_start", created: "2026-09-09T00:00:00Z", want: CheckFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			row := artifactPromotionCreatedAfterStartRow("appstage", platform.ActiveAppVersionDigest{
				ID: "av-stage-2", Source: "CLI", Created: tt.created,
			}, runStart)
			if row.Result != tt.want {
				t.Errorf("Result = %v, want %v (row: %+v)", row.Result, tt.want, row)
			}
		})
	}
}

// TestArtifactPromotion_SourceCli_Table covers source CLI vs GIT.
func TestArtifactPromotion_SourceCli_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   CheckResult
	}{
		{name: "source_cli_passes", source: "CLI", want: CheckPassed},
		{name: "source_git_fails", source: "GIT", want: CheckFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			row := artifactPromotionSourceCliRow("appstage", platform.ActiveAppVersionDigest{
				ID: "av-stage-2", Source: tt.source,
			})
			if row.Result != tt.want {
				t.Errorf("Result = %v, want %v (row: %+v)", row.Result, tt.want, row)
			}
		})
	}
}

// TestArtifactPromotion_NoGitSource_Table covers public git source present/absent.
func TestArtifactPromotion_NoGitSource_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		publicGitSource *platform.AppVersionGitSource
		want            CheckResult
	}{
		{name: "no_git_source_absent_passes", publicGitSource: nil, want: CheckPassed},
		{name: "git_source_present_fails", publicGitSource: &platform.AppVersionGitSource{GitURL: "https://github.com/example/repo", BranchName: "main"}, want: CheckFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			row := artifactPromotionNoGitSourceRow("appstage", platform.ActiveAppVersionDigest{
				ID: "av-stage-2", PublicGitSource: tt.publicGitSource,
			})
			if row.Result != tt.want {
				t.Errorf("Result = %v, want %v (row: %+v)", row.Result, tt.want, row)
			}
		})
	}
}
