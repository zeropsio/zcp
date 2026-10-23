package tools

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// One commit per level, all parts (spec-mate §2.8a): the CI a pair's push
// triggers builds every stage, each with its own setup.

func writeThreeStageGitPushPair(t *testing.T, integration topology.BuildIntegration) string {
	t.Helper()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		PrimarySetupName: "dev",
		Stages: []topology.StageRef{
			{Hostname: "webstage", SetupName: "web"},
			{Hostname: "apistage", SetupName: "api"},
			{Hostname: "workerstage"},
		},
		GitPushState:     topology.GitPushConfigured,
		BuildIntegration: integration,
		RemoteURL:        "https://github.com/example/shop.git",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-10-14",
		FirstDeployedAt:  "2026-10-14",
	}); err != nil {
		t.Fatal(err)
	}
	return stateDir
}

func threeStageServices() *platform.Mock {
	return platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-dev", Name: "appdev"},
		{ID: "svc-web", Name: "webstage"},
		{ID: "svc-api", Name: "apistage"},
		{ID: "svc-worker", Name: "workerstage"},
	})
}

func TestBuildIntegration_ActionsForAPairWithThreeStages_PushesEveryStage(t *testing.T) {
	t.Parallel()
	stateDir := writeThreeStageGitPushPair(t, topology.BuildIntegrationNone)
	result, _, _ := handleBuildIntegration(context.Background(), threeStageServices(), nil, "proj-1", WorkflowInput{
		Service:     "appdev",
		BuildTarget: "",
		Integration: string(topology.BuildIntegrationActions),
	}, stateDir, runtime.Info{InContainer: true})
	var body struct {
		BuildTarget  string `json:"buildTarget"`
		WorkflowFile struct {
			Content string `json:"content"`
		} `json:"workflowFile"`
		Secrets []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"secrets"`
		Alternates   []any `json:"alternateWorkflowFiles"`
		BuildTargets []struct {
			Hostname        string `json:"hostname"`
			Setup           string `json:"setup"`
			ServiceIDSecret string `json:"serviceIdSecret"`
		} `json:"buildTargets"`
	}
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &body); err != nil {
		t.Fatal(err)
	}
	for _, push := range []string{
		`zcli push --service-id "${{ secrets.ZEROPS_SERVICE_ID }}" --setup "web" `,
		`zcli push --service-id "${{ secrets.ZEROPS_SERVICE_ID_APISTAGE }}" --setup "api" `,
		`zcli push --service-id "${{ secrets.ZEROPS_SERVICE_ID_WORKERSTAGE }}" --setup "prod" `,
	} {
		if !strings.Contains(body.WorkflowFile.Content, push) {
			t.Errorf("workflow lacks %q:\n%s", push, body.WorkflowFile.Content)
		}
	}
	if got := strings.Count(body.WorkflowFile.Content, "zcli push"); got != 3 {
		t.Errorf("zcli pushes = %d, want 3", got)
	}
	secrets := map[string]string{}
	for _, s := range body.Secrets {
		secrets[s.Name] = s.Value
	}
	want := map[string]string{"ZEROPS_SERVICE_ID": "svc-web", "ZEROPS_SERVICE_ID_APISTAGE": "svc-api", "ZEROPS_SERVICE_ID_WORKERSTAGE": "svc-worker"}
	for name, id := range want {
		if secrets[name] != id {
			t.Errorf("secret %s = %q, want %q", name, secrets[name], id)
		}
	}
	if body.BuildTarget != "webstage" {
		t.Errorf("buildTarget = %q, want the first stage webstage", body.BuildTarget)
	}
	if len(body.Alternates) != 0 {
		t.Errorf("the one-service wrapper variant is offered for a pair with three stages: %v", body.Alternates)
	}
	hosts := make([]string, 0, len(body.BuildTargets))
	for _, bt := range body.BuildTargets {
		hosts = append(hosts, bt.Hostname+"/"+bt.Setup+"/"+bt.ServiceIDSecret)
	}
	if wantHosts := []string{"webstage/web/ZEROPS_SERVICE_ID", "apistage/api/ZEROPS_SERVICE_ID_APISTAGE", "workerstage/prod/ZEROPS_SERVICE_ID_WORKERSTAGE"}; !slices.Equal(hosts, wantHosts) {
		t.Errorf("buildTargets = %v, want %v", hosts, wantHosts)
	}
}

func TestBuildIntegration_WebhookForAPairWithThreeStages_StepsOnEveryStage(t *testing.T) {
	t.Parallel()
	stateDir := writeThreeStageGitPushPair(t, topology.BuildIntegrationNone)
	result, _, _ := handleBuildIntegration(context.Background(), threeStageServices(), nil, "proj-1", WorkflowInput{
		Service:     "appdev",
		Integration: string(topology.BuildIntegrationWebhook),
	}, stateDir, runtime.Info{InContainer: true})
	var body struct {
		DashboardSteps []string `json:"dashboardSteps"`
		NextStep       string   `json:"nextStep"`
	}
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &body); err != nil {
		t.Fatal(err)
	}
	steps := strings.Join(body.DashboardSteps, "\n")
	for _, page := range []string{"service-stack/svc-web/deploy", "service-stack/svc-api/deploy", "service-stack/svc-worker/deploy"} {
		if !strings.Contains(steps, page) {
			t.Errorf("dashboard steps never open %s:\n%s", page, steps)
		}
	}
	for _, setup := range []string{"`web`", "`api`", "`prod`"} {
		if !strings.Contains(steps, "type "+setup) {
			t.Errorf("dashboard steps never type setup %s:\n%s", setup, steps)
		}
	}
	if !strings.Contains(body.NextStep, "webstage, apistage and workerstage") {
		t.Errorf("nextStep does not name every stage: %s", body.NextStep)
	}
}

func TestGitPushBuildWatch_PairWithThreeStages_DeliveredOnlyWhenEveryStageLands(t *testing.T) {
	t.Parallel()
	pushedAt := time.Now().Add(-time.Minute)
	built := pushedAt.Add(time.Second).Format(time.RFC3339)
	tests := []struct {
		name          string
		statuses      map[string]string // service id → build status
		wantStatus    string
		wantActions   string
		wantBuildStat string
	}{
		{
			name:          "every stage built ACTIVE: delivered, every stage named",
			statuses:      map[string]string{"svc-web": "ACTIVE", "svc-api": "ACTIVE", "svc-worker": "ACTIVE"},
			wantStatus:    "DELIVERED",
			wantActions:   "Run zerops_verify on each of webstage, apistage and workerstage",
			wantBuildStat: "ACTIVE",
		},
		{
			name:          "a stage whose build failed keeps the push undelivered and names what landed",
			statuses:      map[string]string{"svc-web": "ACTIVE", "svc-api": "FAILED", "svc-worker": "ACTIVE"},
			wantStatus:    "PUSHED",
			wantActions:   `Landed and recorded on webstage and workerstage (ACTIVE). The integration build on "apistage" finished FAILED.`,
			wantBuildStat: "FAILED",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := writeThreeStageGitPushPair(t, topology.BuildIntegrationActions)
			events := make([]platform.AppVersionEvent, 0, len(tt.statuses))
			for id, status := range tt.statuses {
				events = append(events, platform.AppVersionEvent{ID: "av-" + id, ProjectID: "proj-1", ServiceStackID: id, Status: status, Created: built})
			}
			result := &ops.GitPushResult{Status: "PUSHED"}
			finishGitPushWithBuildWatch(context.Background(), threeStageServices().WithAppVersionEvents(events), "proj-1", nil, nil,
				DeploySSHInput{TargetService: "appdev", Strategy: "git-push"}, stateDir, "", pushedAt, result)
			if result.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", result.Status, tt.wantStatus)
			}
			if result.BuildStatus != tt.wantBuildStat {
				t.Errorf("buildStatus = %q, want %q", result.BuildStatus, tt.wantBuildStat)
			}
			if !strings.HasPrefix(result.NextActions, tt.wantActions) {
				t.Errorf("nextActions = %q, want it to start %q", result.NextActions, tt.wantActions)
			}
			if want := []string{"webstage", "apistage", "workerstage"}; !slices.Equal(result.BuildTargets, want) {
				t.Errorf("buildTargets = %v, want %v", result.BuildTargets, want)
			}
		})
	}
}

func TestGitPushBuildWatch_OneStagePair_AsBefore(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard,
		Stages:       []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
		GitPushState: topology.GitPushConfigured, BuildIntegration: topology.BuildIntegrationActions,
		BootstrapSession: "test", BootstrappedAt: "2026-10-14", FirstDeployedAt: "2026-10-14",
	}); err != nil {
		t.Fatal(err)
	}
	pushedAt := time.Now().Add(-time.Minute)
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-dev", Name: "appdev"}, {ID: "svc-stage", Name: "appstage"}}).
		WithAppVersionEvents([]platform.AppVersionEvent{{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-stage", Status: "ACTIVE", Created: pushedAt.Add(time.Second).Format(time.RFC3339)}})
	result := &ops.GitPushResult{Status: "PUSHED"}
	finishGitPushWithBuildWatch(context.Background(), client, "proj-1", nil, nil,
		DeploySSHInput{TargetService: "appdev", Strategy: "git-push"}, stateDir, "", pushedAt, result)
	if result.Status != "DELIVERED" || result.BuildTarget != "appstage" || result.VerifyTarget != "appstage" || result.BuildTargets != nil {
		t.Errorf("result = %+v, want DELIVERED on appstage alone", result)
	}
	if want := "Code pushed from appdev and the integration build landed on appstage (ACTIVE). Deploy auto-recorded — no record-deploy ack needed."; result.Message != want {
		t.Errorf("message = %q, want %q", result.Message, want)
	}
	if want := `Run zerops_verify serviceHostname="appstage" for runtime state.`; result.NextActions != want {
		t.Errorf("nextActions = %q, want %q", result.NextActions, want)
	}
}

func TestEarnWebhook_PairWithThreeStages_EarnedOnlyWhenEveryStageHoldsOne(t *testing.T) {
	t.Parallel()
	gh := platform.IntegrationStatus{State: platform.IntegrationConfigured, Provider: platform.IntegrationProviderGitHub}
	tests := []struct {
		name       string
		configured []string
		wantEarned bool
		wantDetail string
	}{
		{"every stage holds a webhook", []string{"svc-web", "svc-api", "svc-worker"}, true,
			"platform reports a configured github integration on webstage, a configured github integration on apistage, a configured github integration on workerstage"},
		{"a stage without one keeps it unearned", []string{"svc-web", "svc-worker"}, false,
			"platform reports no repository integration on apistage — the dashboard OAuth step has not completed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := writeThreeStageGitPushPair(t, topology.BuildIntegrationWebhook)
			meta, err := workflow.ReadServiceMeta(stateDir, "appdev")
			if err != nil {
				t.Fatal(err)
			}
			client := threeStageServices()
			for _, id := range tt.configured {
				client.WithIntegrationStatus(id, gh)
			}
			earned, detail := earnWebhookIntegrationStatus(context.Background(), earnProbeDeps{client: client, projectID: "proj-1"}, meta)
			if earned != tt.wantEarned || detail != tt.wantDetail {
				t.Errorf("earned, detail = %v, %q; want %v, %q", earned, detail, tt.wantEarned, tt.wantDetail)
			}
		})
	}
}
