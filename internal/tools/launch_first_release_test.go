package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// firstReleaseTestState seeds the pair meta + a launched state for the
// first-release response surface tests. family selects the source pair's
// declared BuildIntegration.
func firstReleaseTestState(t *testing.T, family topology.BuildIntegration) (string, *launchState) {
	t.Helper()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "weather",
		Mode:             topology.PlanModeSimple,
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/krls2020/xy3",
		BuildIntegration: family,
		FirstDeployedAt:  "2026-06-10T09:00:00Z",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-06-10",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	state := &launchState{
		TargetServiceHostname: "weather",
		TargetProjectID:       "prod-1",
		TargetProjectName:     "weather-prod",
		Status:                topology.LaunchStatusLaunched,
		ImportedServices: []importedServiceEntry{
			{ID: "prod-svc-9", Name: "weather"},
		},
		RuntimeProds: []launchRuntimeProd{
			{ProdHostname: "weather", RepoURL: "https://github.com/krls2020/xy3", SetupName: "weather"},
		},
		PipelineCheckedAt: time.Now().UTC(),
		PipelineConfigurations: map[string]pipelineConfigEntry{
			"weather": {}, // probed, not configured, not skipped
		},
	}
	return stateDir, state
}

// TestLaunchedResponse_CarriesFirstReleaseBlock pins the pipeline-first
// launched semantics (plans/launch-pipeline-first-2026-06-11.md P2): the
// launched response ALWAYS carries the structured firstRelease block
// stating the truth — runtimes are ACTIVE with EMPTY containers
// (startWithoutCode) and the app arrives with the first release through
// the production pipeline.
func TestLaunchedResponse_CarriesFirstReleaseBlock(t *testing.T) {
	t.Parallel()
	stateDir, state := firstReleaseTestState(t, topology.BuildIntegrationActions)
	res := launchLaunchedResponse(nil, state, stateDir)
	body := getTextContent(t, res)
	for _, want := range []string{
		`"firstRelease"`,
		`"deliveryFamily":"actions"`,
		"EMPTY",
		"first release",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("launched response missing %q:\n%s", want, body)
		}
	}
}

// TestLaunchedResponse_FirstRelease_NoneFamilyAsksUser pins the undecided
// case: BuildIntegration=none (explicitly skipped on the source pair)
// must surface the family CHOICE to the user — never a silent default.
func TestLaunchedResponse_FirstRelease_NoneFamilyAsksUser(t *testing.T) {
	t.Parallel()
	stateDir, state := firstReleaseTestState(t, topology.BuildIntegrationNone)
	res := launchLaunchedResponse(nil, state, stateDir)
	body := getTextContent(t, res)
	for _, want := range []string{
		`"deliveryFamily":"none"`,
		"ASK THE USER",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("launched response missing %q:\n%s", want, body)
		}
	}
}

// TestPickPipelineAtom_FamilyAware pins the family split: the dashboard
// TAG atom belongs to the webhook/none families; an actions-family launch
// gets NO pipeline atom (the prodCD actions block owns its delivery
// story — emitting the dashboard walkthrough alongside it contradicted
// the actions track).
func TestPickPipelineAtom_FamilyAware(t *testing.T) {
	t.Parallel()
	_, state := firstReleaseTestState(t, topology.BuildIntegrationActions)
	if got := pickPipelineAtomID(state, topology.BuildIntegrationActions); got != "" {
		t.Errorf("actions family must render no pipeline atom; got %q", got)
	}
	if got := pickPipelineAtomID(state, topology.BuildIntegrationWebhook); got != launchPipelineConfigureDashboardAtom {
		t.Errorf("webhook family keeps the dashboard atom; got %q", got)
	}
	if got := pickPipelineAtomID(state, topology.BuildIntegrationNone); got != launchPipelineConfigureDashboardAtom {
		t.Errorf("none family keeps the dashboard atom; got %q", got)
	}
}

// TestPipelineBlockers_ActionsFamily_Suppressed pins the blocker split:
// the platform integration-status probe is EXPECTED not-configured for an
// actions-family launch (GitHub Actions registers no Zerops webhook
// integration), so the pipeline-not-configured blocker would mislead the
// agent into the dashboard for a delivery the actions track already owns.
func TestPipelineBlockers_ActionsFamily_Suppressed(t *testing.T) {
	t.Parallel()
	_, state := firstReleaseTestState(t, topology.BuildIntegrationActions)
	if got := pipelineBlockers(state, topology.BuildIntegrationActions); len(got) != 0 {
		t.Errorf("actions family must suppress pipeline blockers; got %+v", got)
	}
	webhook := pipelineBlockers(state, topology.BuildIntegrationWebhook)
	if len(webhook) != 1 || webhook[0].ID != "pipeline-not-configured-weather" {
		t.Errorf("webhook family keeps the dashboard blocker; got %+v", webhook)
	}
}
