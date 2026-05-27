package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestDeriveRepositoryFullName_TableDriven pins the supported input
// shapes for the recommendation payload. HTTPS / HTTPS+.git / SSH
// (git@) all collapse to `owner/repo`; unknown inputs pass through.
func TestDeriveRepositoryFullName_TableDriven(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"https-github", "https://github.com/krls2020/myapp", "krls2020/myapp"},
		{"https-github-dotgit", "https://github.com/krls2020/myapp.git", "krls2020/myapp"},
		{"https-gitlab", "https://gitlab.com/group/proj", "group/proj"},
		{"ssh-github", "git@github.com:krls2020/myapp.git", "krls2020/myapp"},
		{"ssh-no-dotgit", "git@github.com:krls2020/myapp", "krls2020/myapp"},
		{"empty", "", ""},
		{"weird", "not-a-url", "not-a-url"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := deriveRepositoryFullName(tc.in)
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

// TestDeriveDashboardDeepLink_NonEmptyShape verifies the URL composition.
// Pins the live URL shape (`/service-stack/<id>/deploy`, verified
// 2026-05-19 against eval-zcp dashboard) — earlier
// `/dashboard/project/<proj>/service-stack/<id>/service-stack-source-code`
// slug 404s on the current frontend.
func TestDeriveDashboardDeepLink_NonEmptyShape(t *testing.T) {
	t.Parallel()
	got := deriveDashboardDeepLink("svc-456")
	want := "https://app.zerops.io/service-stack/svc-456/deploy"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if deriveDashboardDeepLink("") != "" {
		t.Error("empty serviceID should produce empty link")
	}
}

// TestPipelineRecommendation_DefaultsAndOverride pins the recommendation
// payload shape. ZeropsYamlSetup is now an explicit caller-supplied
// argument (plan §P5 — no "prod" default in pipelineRecommendation);
// empty zeropsYamlSetup OR empty repoURL collapse the return to nil.
func TestPipelineRecommendation_DefaultsAndOverride(t *testing.T) {
	t.Parallel()
	t.Run("default-regex", func(t *testing.T) {
		t.Parallel()
		rec := pipelineRecommendation("https://github.com/krls2020/myapp", "", "prod")
		if rec == nil {
			t.Fatal("expected non-nil recommendation")
		}
		if rec.TagRegex != defaultPipelineTagRegex {
			t.Errorf("TagRegex: got %q want %q", rec.TagRegex, defaultPipelineTagRegex)
		}
		if rec.RepositoryFullName != "krls2020/myapp" {
			t.Errorf("RepositoryFullName: got %q", rec.RepositoryFullName)
		}
		if rec.EventType != defaultPipelineEventType {
			t.Errorf("EventType: got %q want %q", rec.EventType, defaultPipelineEventType)
		}
		if rec.ZeropsYamlSetup != "prod" {
			t.Errorf("ZeropsYamlSetup: got %q want %q", rec.ZeropsYamlSetup, "prod")
		}
	})
	t.Run("explicit-non-prod-setup-name", func(t *testing.T) {
		t.Parallel()
		rec := pipelineRecommendation("https://github.com/krls2020/myapp", "", "release-train")
		if rec == nil || rec.ZeropsYamlSetup != "release-train" {
			t.Errorf("non-conventional setup name should pass through; got %+v", rec)
		}
	})
	t.Run("override-regex", func(t *testing.T) {
		t.Parallel()
		rec := pipelineRecommendation("https://github.com/krls2020/myapp", "^release-.*$", "prod")
		if rec.TagRegex != "^release-.*$" {
			t.Errorf("TagRegex: got %q want override", rec.TagRegex)
		}
	})
	t.Run("empty-repo-url", func(t *testing.T) {
		t.Parallel()
		if pipelineRecommendation("", "", "prod") != nil {
			t.Error("expected nil for empty repo URL")
		}
	})
	t.Run("empty-zeropsYamlSetup-blocks-recommendation", func(t *testing.T) {
		t.Parallel()
		if pipelineRecommendation("https://github.com/krls2020/myapp", "", "") != nil {
			t.Error("expected nil when zeropsYamlSetup is empty — no \"prod\" default")
		}
	})
}

// TestExecuteLaunchPipelineCheck_NoPutCallsByZCP pins P-LP-7: the
// pipeline check function never calls anything that mutates platform
// state. ProjectAdminClient interface intentionally lacks
// PutServiceStackIntegration — this guarantees compile-time, but the
// test pins runtime behavior by asserting the mock's read counter only.
func TestExecuteLaunchPipelineCheck_NoPutCallsByZCP(t *testing.T) {
	t.Parallel()
	mock := platform.NewMockProjectAdminClient()
	state := newPipelineTestState()
	executeLaunchPipelineCheck(context.Background(), mock, state, pipelineCheckInputs{
		RuntimeHostname: "app",
		RepoURL:         "https://github.com/krls2020/myapp",
	})
	if len(mock.CapturedIntegrationStatusServices) != 1 {
		t.Errorf("expected 1 GetStatus call; got %d", len(mock.CapturedIntegrationStatusServices))
	}
	if mock.CapturedIntegrationStatusServices[0] != "svc-app" {
		t.Errorf("GetStatus called with wrong serviceID: %q", mock.CapturedIntegrationStatusServices[0])
	}
}

// TestExecuteLaunchPipelineCheck_NotConfigured_PopulatesBlocker pins
// P-LP-8: a runtime that comes back as NotConfigured produces a
// pipelineConfigEntry with DeepLink + Recommendation set; subsequent
// pipelineBlockers() turns this into a warn-severity blocker.
func TestExecuteLaunchPipelineCheck_NotConfigured_PopulatesBlocker(t *testing.T) {
	t.Parallel()
	mock := platform.NewMockProjectAdminClient() // default returns NotConfigured
	state := newPipelineTestState()
	executeLaunchPipelineCheck(context.Background(), mock, state, pipelineCheckInputs{
		RuntimeHostname: "app",
		RepoURL:         "https://github.com/krls2020/myapp",
		ZeropsYamlSetup: "prod", // plan §P5 — explicit caller-supplied; no internal default
	})
	entry, ok := state.PipelineConfigurations["app"]
	if !ok {
		t.Fatal("expected app entry in PipelineConfigurations")
	}
	if entry.Configured {
		t.Error("expected Configured=false for NotConfigured state")
	}
	if entry.SkipReason != "" {
		t.Errorf("expected empty SkipReason; got %q", entry.SkipReason)
	}
	if entry.DeepLink == "" {
		t.Error("expected non-empty DeepLink for not-configured entry")
	}
	if entry.Recommendation == nil {
		t.Fatal("expected non-nil Recommendation")
	}
	if entry.Recommendation.RepositoryFullName != "krls2020/myapp" {
		t.Errorf("Recommendation.RepositoryFullName: got %q", entry.Recommendation.RepositoryFullName)
	}

	blockers := pipelineBlockers(state)
	if len(blockers) != 1 {
		t.Fatalf("expected 1 blocker; got %d", len(blockers))
	}
	if blockers[0].Severity != topology.BlockerSeverityWarn {
		t.Errorf("blocker severity: got %q want warn (P-LP-8 — pipeline never blocks launched)", blockers[0].Severity)
	}
	if !strings.Contains(blockers[0].Message, "krls2020/myapp") {
		t.Errorf("expected blocker message to surface recommendation; got %q", blockers[0].Message)
	}
}

// TestExecuteLaunchPipelineCheck_Configured_NoBlocker pins the "already
// configured" path: GetStatus returns IntegrationConfigured →
// pipelineConfigEntry has Configured=true + CurrentConfig populated.
// pipelineBlockers returns nothing for the configured entry.
func TestExecuteLaunchPipelineCheck_Configured_NoBlocker(t *testing.T) {
	t.Parallel()
	mock := platform.NewMockProjectAdminClient().WithIntegrationStatus("svc-app", platform.IntegrationStatus{
		State:              platform.IntegrationConfigured,
		Provider:           platform.IntegrationProviderGitHub,
		RepositoryFullName: "krls2020/myapp",
		EventType:          platform.IntegrationEventTag,
		TagRegex:           "^v\\d+\\.\\d+\\.\\d+$",
		ZeropsYamlSetup:    "prod",
		IsActive:           true,
	})
	state := newPipelineTestState()
	executeLaunchPipelineCheck(context.Background(), mock, state, pipelineCheckInputs{
		RuntimeHostname: "app",
		RepoURL:         "https://github.com/krls2020/myapp",
	})
	entry := state.PipelineConfigurations["app"]
	if !entry.Configured {
		t.Error("expected Configured=true")
	}
	if entry.Recommendation != nil {
		t.Error("expected Recommendation=nil for configured entry (no nag)")
	}
	if entry.CurrentConfig == nil {
		t.Fatal("expected non-nil CurrentConfig for configured entry")
	}
	if entry.CurrentConfig.RepositoryFullName != "krls2020/myapp" {
		t.Errorf("CurrentConfig.RepositoryFullName: got %q", entry.CurrentConfig.RepositoryFullName)
	}

	if len(pipelineBlockers(state)) != 0 {
		t.Errorf("expected no blockers when all runtimes configured")
	}
}

// TestExecuteLaunchPipelineCheck_SkipFlagBypassesCheck verifies that
// SkipPipelineSetup=true skips the GetStatus call entirely and records
// the skip reason.
func TestExecuteLaunchPipelineCheck_SkipFlagBypassesCheck(t *testing.T) {
	t.Parallel()
	mock := platform.NewMockProjectAdminClient()
	state := newPipelineTestState()
	executeLaunchPipelineCheck(context.Background(), mock, state, pipelineCheckInputs{
		SkipPipelineSetup: true,
		RuntimeHostname:   "app",
		RepoURL:           "https://github.com/krls2020/myapp",
	})
	if len(mock.CapturedIntegrationStatusServices) != 0 {
		t.Errorf("expected 0 GetStatus calls when skipping; got %d", len(mock.CapturedIntegrationStatusServices))
	}
	entry := state.PipelineConfigurations["app"]
	if entry.SkipReason != "user-opted-out" {
		t.Errorf("SkipReason: got %q want user-opted-out", entry.SkipReason)
	}
	if entry.Configured {
		t.Error("expected Configured=false when skipped")
	}
	if entry.DeepLink != "" {
		t.Error("expected empty DeepLink when skipped (no actionable info to surface)")
	}

	if len(pipelineBlockers(state)) != 0 {
		t.Errorf("expected no blockers when explicitly skipped; got %v", pipelineBlockers(state))
	}
}

// TestExecuteLaunchPipelineCheck_LookupFailed_RecordsSkipReason verifies
// that a GetStatus error becomes a SkipReason rather than aborting the
// loop. P-LP-8 — pipeline issues never fail the launch.
func TestExecuteLaunchPipelineCheck_LookupFailed_RecordsSkipReason(t *testing.T) {
	t.Parallel()
	mock := platform.NewMockProjectAdminClient().WithIntegrationStatusError(errors.New("transient platform glitch"))
	state := newPipelineTestState()
	executeLaunchPipelineCheck(context.Background(), mock, state, pipelineCheckInputs{
		RuntimeHostname: "app",
		RepoURL:         "https://github.com/krls2020/myapp",
	})
	entry := state.PipelineConfigurations["app"]
	if !strings.HasPrefix(entry.SkipReason, "lookup-failed:") {
		t.Errorf("SkipReason: got %q want prefix 'lookup-failed:'", entry.SkipReason)
	}
	if entry.Configured {
		t.Error("expected Configured=false on lookup failure")
	}
}

// TestPipelineSkipRecorded_DetectsOptOut covers the helper used by the
// resume path to avoid re-running the check on every refresh.
func TestPipelineSkipRecorded_DetectsOptOut(t *testing.T) {
	t.Parallel()
	state := &launchState{
		PipelineConfigurations: map[string]pipelineConfigEntry{
			"app": {SkipReason: "user-opted-out"},
		},
	}
	if !pipelineSkipRecorded(state) {
		t.Error("expected true for user-opted-out entry")
	}
	state2 := &launchState{
		PipelineConfigurations: map[string]pipelineConfigEntry{
			"app": {SkipReason: "lookup-failed: boom"},
		},
	}
	if pipelineSkipRecorded(state2) {
		t.Error("lookup-failed should NOT count as a recorded skip")
	}
	empty := &launchState{}
	if pipelineSkipRecorded(empty) {
		t.Error("empty state should not look skip-recorded")
	}
}

// TestPickPipelineAtomID_Branches pins which atom the launched response
// surfaces based on observed pipeline state.
func TestPickPipelineAtomID_Branches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		give *launchState
		want string
	}{
		{
			name: "no check run yet",
			give: &launchState{},
			want: "",
		},
		{
			name: "configured",
			give: stateWithEntry("app", pipelineConfigEntry{Configured: true}),
			want: "launch-pipeline-configured",
		},
		{
			name: "pending",
			give: stateWithEntry("app", pipelineConfigEntry{}),
			want: "launch-pipeline-configure-dashboard",
		},
		{
			name: "skipped",
			give: stateWithEntry("app", pipelineConfigEntry{SkipReason: "user-opted-out"}),
			want: "launch-pipeline-skipped",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := pickPipelineAtomID(tc.give)
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

// newPipelineTestState builds a launchState with one runtime ("app",
// id "svc-app") captured. v1 launch-production is single-runtime;
// multi-runtime expansion (multi-repo prod) is a scope cut per
// plans/production-lifecycle-part2-2026-05-12.md §10. The helper exists
// so callers don't have to repeat the boilerplate.
func newPipelineTestState() *launchState {
	return &launchState{
		LaunchID:              "test-launch",
		SourceProjectID:       "src-proj",
		TargetProjectID:       "tgt-proj",
		TargetProjectName:     "myapp-prod",
		TargetServiceHostname: "app",
		ImportedServices: []importedServiceEntry{
			{ID: "svc-app", Name: "app"},
		},
	}
}

// stateWithEntry returns a *launchState pre-populated with one
// PipelineCheckedAt + one entry. Used by atom-id branch table tests.
func stateWithEntry(name string, entry pipelineConfigEntry) *launchState {
	return &launchState{
		PipelineCheckedAt: time.Now().UTC(),
		PipelineConfigurations: map[string]pipelineConfigEntry{
			name: entry,
		},
	}
}
