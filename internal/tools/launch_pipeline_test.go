package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
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
// 2026-05-19 against eval-zcp dashboard).
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

// prodRuntime is a one-runtime Runtimes slice for the common single-
// runtime test case (prod hostname "app").
func prodRuntime(setup string) []launchRuntimeProd {
	return []launchRuntimeProd{{ProdHostname: "app", RepoURL: "https://github.com/krls2020/myapp", SetupName: setup}}
}

// TestExecuteLaunchPipelineCheck_NoPutCallsByZCP pins P-LP-7: the
// pipeline check never mutates platform state (ProjectAdminClient lacks
// any Put*Integration). The test pins the read counter only.
func TestExecuteLaunchPipelineCheck_NoPutCallsByZCP(t *testing.T) {
	t.Parallel()
	mock := platform.NewMockProjectAdminClient()
	state := newPipelineTestState()
	executeLaunchPipelineCheck(context.Background(), mock, state, pipelineCheckInputs{
		Runtimes: prodRuntime(""),
	})
	if len(mock.CapturedIntegrationStatusServices) != 1 {
		t.Errorf("expected 1 GetStatus call; got %d", len(mock.CapturedIntegrationStatusServices))
	}
	if mock.CapturedIntegrationStatusServices[0] != "svc-app" {
		t.Errorf("GetStatus called with wrong serviceID: %q", mock.CapturedIntegrationStatusServices[0])
	}
}

// TestExecuteLaunchPipelineCheck_SourceNotEqualProd is the LAUNCH-1
// regression pin. The promoted runtime's source hostname (`appdev`)
// differs from the prod hostname the platform assigned (`app`). The
// check MUST probe the prod service (keyed on prod hostname) — the old
// code keyed on the source hostname, matched nothing, and silently
// reported "configured" with zero probes.
func TestExecuteLaunchPipelineCheck_SourceNotEqualProd(t *testing.T) {
	t.Parallel()
	mock := platform.NewMockProjectAdminClient() // returns NotConfigured
	state := &launchState{
		LaunchID:              "lp1",
		TargetServiceHostname: "appdev", // SOURCE hostname (mode-suffixed)
		ImportedServices: []importedServiceEntry{
			{ID: "svc-app", Name: "app"}, // PROD runtime
			{ID: "svc-db", Name: "db"},   // managed dep (no buildFromGit)
		},
		RuntimeProds: []launchRuntimeProd{
			{ProdHostname: "app", RepoURL: "https://github.com/krls2020/myapp", SetupName: "prod"},
		},
	}
	executeLaunchPipelineCheck(context.Background(), mock, state, pipelineCheckInputs{
		Runtimes: state.RuntimeProds,
	})
	if len(mock.CapturedIntegrationStatusServices) != 1 || mock.CapturedIntegrationStatusServices[0] != "svc-app" {
		t.Fatalf("expected exactly 1 probe of the PROD runtime svc-app; got %v", mock.CapturedIntegrationStatusServices)
	}
	entry, ok := state.PipelineConfigurations["app"]
	if !ok {
		t.Fatal("expected an entry keyed by PROD hostname 'app'")
	}
	if entry.Configured {
		t.Error("NotConfigured runtime must not be reported configured")
	}
	if pickPipelineAtomID(state, topology.BuildIntegrationWebhook) != launchPipelineConfigureDashboardAtom {
		t.Errorf("expected the dashboard-nag atom, got %q (the LAUNCH-1 false-positive was 'launch-pipeline-configured')", pickPipelineAtomID(state, topology.BuildIntegrationWebhook))
	}
	if len(pipelineBlockers(state, topology.BuildIntegrationWebhook)) != 1 {
		t.Errorf("expected 1 warn blocker surfacing the unconfigured prod runtime; got %d", len(pipelineBlockers(state, topology.BuildIntegrationWebhook)))
	}
}

// TestExecuteLaunchPipelineCheck_RuntimeNotInImport_NotSilent pins the
// no-silent-pass property: a promoted runtime whose prod hostname is
// absent from the import result becomes a pending (loud) entry, never a
// silent "configured".
func TestExecuteLaunchPipelineCheck_RuntimeNotInImport_NotSilent(t *testing.T) {
	t.Parallel()
	mock := platform.NewMockProjectAdminClient()
	state := &launchState{
		ImportedServices: []importedServiceEntry{{ID: "svc-app", Name: "app"}},
	}
	executeLaunchPipelineCheck(context.Background(), mock, state, pipelineCheckInputs{
		Runtimes: []launchRuntimeProd{
			{ProdHostname: "worker", RepoURL: "https://github.com/krls2020/myapp", SetupName: "prod"},
		},
	})
	if len(mock.CapturedIntegrationStatusServices) != 0 {
		t.Errorf("expected 0 probes (runtime not imported); got %v", mock.CapturedIntegrationStatusServices)
	}
	entry, ok := state.PipelineConfigurations["worker"]
	if !ok || entry.Configured {
		t.Fatalf("expected a pending (unconfigured) entry for the missing runtime; got %+v ok=%v", entry, ok)
	}
	if pickPipelineAtomID(state, topology.BuildIntegrationWebhook) == "launch-pipeline-configured" {
		t.Error("a runtime missing from the import must NOT yield a 'configured' atom")
	}
}

// TestExecuteLaunchPipelineCheck_MultiRuntime probes every promoted
// runtime (LAUNCH-3/4: multi-runtime is live).
func TestExecuteLaunchPipelineCheck_MultiRuntime(t *testing.T) {
	t.Parallel()
	mock := platform.NewMockProjectAdminClient().WithIntegrationStatus("svc-app", platform.IntegrationStatus{
		State: platform.IntegrationConfigured, Provider: platform.IntegrationProviderGitHub,
		RepositoryFullName: "krls2020/app", EventType: platform.IntegrationEventTag, IsActive: true,
	})
	state := &launchState{
		ImportedServices: []importedServiceEntry{
			{ID: "svc-app", Name: "app"},
			{ID: "svc-worker", Name: "worker"},
			{ID: "svc-db", Name: "db"},
		},
		RuntimeProds: []launchRuntimeProd{
			{ProdHostname: "app", RepoURL: "https://github.com/krls2020/app", SetupName: "prod"},
			{ProdHostname: "worker", RepoURL: "https://github.com/krls2020/worker", SetupName: "worker"},
		},
	}
	executeLaunchPipelineCheck(context.Background(), mock, state, pipelineCheckInputs{Runtimes: state.RuntimeProds})
	if len(mock.CapturedIntegrationStatusServices) != 2 {
		t.Fatalf("expected 2 probes (one per runtime, db skipped); got %v", mock.CapturedIntegrationStatusServices)
	}
	if !state.PipelineConfigurations["app"].Configured {
		t.Error("app should be configured")
	}
	if state.PipelineConfigurations["worker"].Configured {
		t.Error("worker should be unconfigured (default mock)")
	}
	// app configured + worker not → still pending overall → nag atom + 1 blocker for worker.
	if pickPipelineAtomID(state, topology.BuildIntegrationWebhook) != launchPipelineConfigureDashboardAtom {
		t.Errorf("mixed state should nag; got %q", pickPipelineAtomID(state, topology.BuildIntegrationWebhook))
	}
	if len(pipelineBlockers(state, topology.BuildIntegrationWebhook)) != 1 {
		t.Errorf("expected 1 blocker (worker only); got %d", len(pipelineBlockers(state, topology.BuildIntegrationWebhook)))
	}
}

// TestExecuteLaunchPipelineCheck_NotConfigured_PopulatesBlocker pins
// P-LP-8: a NotConfigured runtime produces an entry with DeepLink +
// Recommendation; pipelineBlockers turns it into a warn blocker.
func TestExecuteLaunchPipelineCheck_NotConfigured_PopulatesBlocker(t *testing.T) {
	t.Parallel()
	mock := platform.NewMockProjectAdminClient() // default returns NotConfigured
	state := newPipelineTestState()
	executeLaunchPipelineCheck(context.Background(), mock, state, pipelineCheckInputs{
		Runtimes: prodRuntime("prod"),
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

	blockers := pipelineBlockers(state, topology.BuildIntegrationWebhook)
	if len(blockers) != 1 {
		t.Fatalf("expected 1 blocker; got %d", len(blockers))
	}
	if blockers[0].Severity != topology.BlockerSeverityWarn {
		t.Errorf("blocker severity: got %q want warn (P-LP-8)", blockers[0].Severity)
	}
	if !strings.Contains(blockers[0].Message, "krls2020/myapp") {
		t.Errorf("expected blocker message to surface recommendation; got %q", blockers[0].Message)
	}
}

// TestExecuteLaunchPipelineCheck_Configured_NoBlocker pins the "already
// configured" path.
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
		Runtimes: prodRuntime(""),
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

	if len(pipelineBlockers(state, topology.BuildIntegrationWebhook)) != 0 {
		t.Errorf("expected no blockers when all runtimes configured")
	}
}

// TestExecuteLaunchPipelineCheck_SkipFlagBypassesCheck verifies that
// SkipPipelineSetup=true skips the GetStatus call entirely.
func TestExecuteLaunchPipelineCheck_SkipFlagBypassesCheck(t *testing.T) {
	t.Parallel()
	mock := platform.NewMockProjectAdminClient()
	state := newPipelineTestState()
	executeLaunchPipelineCheck(context.Background(), mock, state, pipelineCheckInputs{
		SkipPipelineSetup: true,
		Runtimes:          prodRuntime(""),
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
		t.Error("expected empty DeepLink when skipped")
	}

	if len(pipelineBlockers(state, topology.BuildIntegrationWebhook)) != 0 {
		t.Errorf("expected no blockers when explicitly skipped; got %v", pipelineBlockers(state, topology.BuildIntegrationWebhook))
	}
}

// TestExecuteLaunchPipelineCheck_LookupFailed_RecordsSkipReason verifies
// a GetStatus error becomes a SkipReason rather than aborting. P-LP-8.
func TestExecuteLaunchPipelineCheck_LookupFailed_RecordsSkipReason(t *testing.T) {
	t.Parallel()
	mock := platform.NewMockProjectAdminClient().WithIntegrationStatusError(errors.New("transient platform glitch"))
	state := newPipelineTestState()
	executeLaunchPipelineCheck(context.Background(), mock, state, pipelineCheckInputs{
		Runtimes: prodRuntime(""),
	})
	entry := state.PipelineConfigurations["app"]
	if !strings.HasPrefix(entry.SkipReason, "lookup-failed:") {
		t.Errorf("SkipReason: got %q want prefix 'lookup-failed:'", entry.SkipReason)
	}
	if entry.Configured {
		t.Error("expected Configured=false on lookup failure")
	}
}

// TestRuntimeProdsFromBundleInputs pins the source→prod mapping that
// closes LAUNCH-1: the persisted runtime list carries the PROD hostname
// (the bundle's ProdHostname), not the source.
func TestRuntimeProdsFromBundleInputs(t *testing.T) {
	t.Parallel()
	in := ops.LaunchBundleInputs{
		Runtimes: []bundle.LaunchRuntimeInput{
			{ProdHostname: "app", RepoURL: "https://github.com/krls2020/app", SetupName: "prod"},
			{ProdHostname: "worker", RepoURL: "https://github.com/krls2020/worker", SetupName: "worker"},
		},
	}
	got := runtimeProdsFromBundleInputs(in)
	if len(got) != 2 || got[0].ProdHostname != "app" || got[1].ProdHostname != "worker" {
		t.Fatalf("expected [app worker] prod hostnames; got %+v", got)
	}
	if got[0].RepoURL != "https://github.com/krls2020/app" || got[1].SetupName != "worker" {
		t.Errorf("per-runtime RepoURL/SetupName not carried: %+v", got)
	}
}

// TestPipelineSkipRecorded_DetectsOptOut covers the resume helper.
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
		{"no check run yet", &launchState{}, ""},
		{"configured", stateWithEntry("app", pipelineConfigEntry{Configured: true}), "launch-pipeline-configured"},
		{"pending", stateWithEntry("app", pipelineConfigEntry{}), launchPipelineConfigureDashboardAtom},
		{"skipped", stateWithEntry("app", pipelineConfigEntry{SkipReason: "user-opted-out"}), "launch-pipeline-skipped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := pickPipelineAtomID(tc.give, topology.BuildIntegrationWebhook)
			if got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

// newPipelineTestState builds a launchState with one PROD runtime ("app",
// id "svc-app") imported + a matching RuntimeProds entry. Source and prod
// hostnames coincide here (simple-mode shape); the source≠prod case is
// pinned separately by TestExecuteLaunchPipelineCheck_SourceNotEqualProd.
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
		RuntimeProds: []launchRuntimeProd{{ProdHostname: "app"}},
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
