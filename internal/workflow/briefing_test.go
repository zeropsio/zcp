package workflow

import (
	"strings"
	"testing"
)

func TestBuildDevelopBriefing_PushDev_HasDeployCommands(t *testing.T) {
	t.Parallel()

	targets := []BriefingTarget{
		{Hostname: "appdev", Role: DeployRoleDev, RuntimeType: "nodejs@22", Strategy: StrategyPushDev},
	}
	briefing := BuildDevelopBriefing(targets, StrategyPushDev, PlanModeDev, EnvContainer, "")

	if !strings.Contains(briefing, "zerops_deploy") {
		t.Error("push-dev briefing should contain zerops_deploy commands")
	}
	if !strings.Contains(briefing, "zerops_verify") {
		t.Error("push-dev briefing should contain zerops_verify commands")
	}
	if !strings.Contains(briefing, "Closing the task") {
		t.Error("briefing should contain close instructions section")
	}
}

func TestBuildDevelopBriefing_PushGit_HasGitPushCommands(t *testing.T) {
	t.Parallel()

	targets := []BriefingTarget{
		{Hostname: "appdev", Role: DeployRoleDev, RuntimeType: "nodejs@22", Strategy: StrategyPushGit},
	}
	briefing := BuildDevelopBriefing(targets, StrategyPushGit, PlanModeDev, EnvContainer, "")

	if !strings.Contains(briefing, "git-push") {
		t.Error("push-git briefing should contain git-push strategy")
	}
	if !strings.Contains(briefing, "git add") {
		t.Error("push-git briefing should contain git commit instructions")
	}
	// Hostnames must be interpolated, not literal {dev} placeholders.
	if strings.Contains(briefing, "{dev}") {
		t.Error("push-git briefing must not contain literal {dev} placeholder — use real hostname")
	}
	if !strings.Contains(briefing, "ssh appdev") {
		t.Error("push-git workflow should reference real hostname 'appdev' in SSH command")
	}
	if !strings.Contains(briefing, `targetService="appdev"`) {
		t.Error("push-git workflow should reference real hostname 'appdev' in deploy command")
	}
}

func TestBuildDevelopBriefing_PushGit_DecisionQuestion(t *testing.T) {
	t.Parallel()

	targets := []BriefingTarget{
		{Hostname: "appdev", Role: DeployRoleDev, RuntimeType: "nodejs@22", Strategy: StrategyPushGit},
	}
	briefing := BuildDevelopBriefing(targets, StrategyPushGit, PlanModeDev, EnvContainer, "")

	wantParts := []string{
		"Ask the user",        // must ask, not assume
		"push code to remote", // option A
		"CI/CD",               // option B
		"GIT_TOKEN",           // prerequisite for push
		".netrc",              // auth mechanism
		`workflow="cicd"`,     // route to CI/CD workflow
	}
	for _, part := range wantParts {
		if !strings.Contains(briefing, part) {
			t.Errorf("push-git briefing should contain %q", part)
		}
	}
}

func TestBuildDevelopBriefing_PushGit_Standard_SkipsStageInCommit(t *testing.T) {
	t.Parallel()

	targets := []BriefingTarget{
		{Hostname: "appdev", Role: DeployRoleDev, RuntimeType: "nodejs@22", Strategy: StrategyPushGit},
		{Hostname: "appstage", Role: DeployRoleStage, RuntimeType: "nodejs@22", Strategy: StrategyPushGit},
	}
	briefing := BuildDevelopBriefing(targets, StrategyPushGit, PlanModeStandard, EnvContainer, "")

	// Only dev services should appear in commit/push steps (code lives on dev)
	if !strings.Contains(briefing, "ssh appdev") {
		t.Error("should reference dev hostname for commit")
	}
	if strings.Contains(briefing, "ssh appstage") {
		t.Error("should NOT reference stage hostname for commit — code lives on dev")
	}
}

func TestBuildDevelopBriefing_PushGit_MultiTarget_AllListed(t *testing.T) {
	t.Parallel()

	targets := []BriefingTarget{
		{Hostname: "apidev", Role: DeployRoleDev, RuntimeType: "nodejs@22", Strategy: StrategyPushGit},
		{Hostname: "appdev", Role: DeployRoleDev, RuntimeType: "nodejs@22", Strategy: StrategyPushGit},
	}
	briefing := BuildDevelopBriefing(targets, StrategyPushGit, PlanModeDev, EnvContainer, "")

	if !strings.Contains(briefing, `targetService="apidev"`) {
		t.Error("close instructions should include apidev")
	}
	if !strings.Contains(briefing, `targetService="appdev"`) {
		t.Error("close instructions should include appdev")
	}
}

func TestBuildDevelopBriefing_Manual_InformsUser(t *testing.T) {
	t.Parallel()

	targets := []BriefingTarget{
		{Hostname: "app", Role: DeployRoleSimple, RuntimeType: "nodejs@22", Strategy: StrategyManual},
	}
	briefing := BuildDevelopBriefing(targets, StrategyManual, PlanModeSimple, EnvContainer, "")

	if !strings.Contains(briefing, "Inform the user") {
		t.Error("manual strategy briefing should instruct to inform user")
	}
	if !strings.Contains(briefing, "User controls") {
		t.Error("manual strategy briefing should mention user controls deployment")
	}
}

func TestBuildDevelopBriefing_Standard_DevThenStage(t *testing.T) {
	t.Parallel()

	targets := []BriefingTarget{
		{Hostname: "appdev", Role: DeployRoleDev, RuntimeType: "nodejs@22"},
		{Hostname: "appstage", Role: DeployRoleStage, RuntimeType: "nodejs@22"},
	}
	briefing := BuildDevelopBriefing(targets, StrategyPushDev, PlanModeStandard, EnvContainer, "")

	devIdx := strings.Index(briefing, `targetService="appdev"`)
	stageIdx := strings.Index(briefing, `targetService="appstage"`)
	if devIdx < 0 || stageIdx < 0 {
		t.Fatal("briefing should contain both dev and stage deploy commands")
	}
	if devIdx > stageIdx {
		t.Error("dev deploy should come before stage deploy")
	}
	if !strings.Contains(briefing, `sourceService="appdev"`) {
		t.Error("stage deploy should use dev as sourceService for cross-deploy")
	}
}

func TestBuildDevelopBriefing_Simple_SingleDeploy(t *testing.T) {
	t.Parallel()

	targets := []BriefingTarget{
		{Hostname: "app", Role: DeployRoleSimple, RuntimeType: "nodejs@22"},
	}
	briefing := BuildDevelopBriefing(targets, StrategyPushDev, PlanModeSimple, EnvContainer, "")

	if !strings.Contains(briefing, `targetService="app"`) {
		t.Error("simple mode should have single deploy command")
	}
	if strings.Contains(briefing, "sourceService") {
		t.Error("simple mode should NOT have cross-deploy")
	}
}

func TestBuildDevelopBriefing_Local_VPNMentioned(t *testing.T) {
	t.Parallel()

	targets := []BriefingTarget{
		{Hostname: "app", Role: DeployRoleSimple, RuntimeType: "nodejs@22"},
	}
	briefing := BuildDevelopBriefing(targets, StrategyPushDev, PlanModeSimple, EnvLocal, "")

	if !strings.Contains(briefing, "VPN") {
		t.Error("local env briefing should mention VPN")
	}
	if strings.Contains(briefing, "SSHFS") {
		t.Error("local env briefing should NOT mention SSHFS mount")
	}
}

func TestBuildDevelopBriefing_KnowledgeMap_RuntimePointers(t *testing.T) {
	t.Parallel()

	targets := []BriefingTarget{
		{Hostname: "app", Role: DeployRoleSimple, RuntimeType: "nodejs@22"},
	}
	briefing := BuildDevelopBriefing(targets, "", PlanModeSimple, EnvContainer, "")

	if !strings.Contains(briefing, `zerops_knowledge query="nodejs"`) {
		t.Error("briefing should have runtime knowledge pointer for nodejs")
	}
}

func TestBuildBriefingTargets_Simple(t *testing.T) {
	t.Parallel()

	metas := []*ServiceMeta{
		{Hostname: "app", Mode: PlanModeSimple, BootstrappedAt: "2026-01-01"},
	}
	targets, mode := BuildBriefingTargets(metas)

	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Hostname != "app" {
		t.Errorf("hostname = %q, want %q", targets[0].Hostname, "app")
	}
	if targets[0].Role != DeployRoleSimple {
		t.Errorf("role = %q, want %q", targets[0].Role, DeployRoleSimple)
	}
	if mode != PlanModeSimple {
		t.Errorf("mode = %q, want %q", mode, PlanModeSimple)
	}
}

// Local+standard meta stores the stage hostname at m.Hostname with
// StageHostname empty (dev doesn't exist locally). PrimaryRole must
// recognize this and return stage, not simple.
func TestBuildBriefingTargets_LocalStandard_ReturnsStageRole(t *testing.T) {
	t.Parallel()

	metas := []*ServiceMeta{
		{Hostname: "appstage", Mode: PlanModeStandard, Environment: string(EnvLocal), BootstrappedAt: "2026-01-01"},
	}
	targets, _ := BuildBriefingTargets(metas)

	if len(targets) != 1 {
		t.Fatalf("expected 1 target (no dev in local+standard), got %d", len(targets))
	}
	if targets[0].Role != DeployRoleStage {
		t.Errorf("role = %q, want %q (local+standard primary is stage, not simple)", targets[0].Role, DeployRoleStage)
	}
}

func TestBuildBriefingTargets_Standard_DevPlusStage(t *testing.T) {
	t.Parallel()

	metas := []*ServiceMeta{
		{Hostname: "appdev", Mode: PlanModeStandard, StageHostname: "appstage", BootstrappedAt: "2026-01-01"},
	}
	targets, mode := BuildBriefingTargets(metas)

	if len(targets) != 2 {
		t.Fatalf("expected 2 targets (dev+stage), got %d", len(targets))
	}
	if targets[0].Role != DeployRoleDev {
		t.Errorf("first target role = %q, want %q", targets[0].Role, DeployRoleDev)
	}
	if targets[1].Role != DeployRoleStage {
		t.Errorf("second target role = %q, want %q", targets[1].Role, DeployRoleStage)
	}
	if targets[1].Hostname != "appstage" {
		t.Errorf("second target hostname = %q, want %q", targets[1].Hostname, "appstage")
	}
	if mode != PlanModeStandard {
		t.Errorf("mode = %q, want %q", mode, PlanModeStandard)
	}
}

func TestEnrichBriefingTargets(t *testing.T) {
	t.Parallel()

	targets := []BriefingTarget{
		{Hostname: "app"},
		{Hostname: "db"},
	}
	typeMap := map[string]string{"app": "nodejs@22", "db": "postgresql@16"}
	httpMap := map[string]bool{"app": true}

	EnrichBriefingTargets(targets, typeMap, httpMap)

	if targets[0].RuntimeType != "nodejs@22" {
		t.Errorf("app RuntimeType = %q, want %q", targets[0].RuntimeType, "nodejs@22")
	}
	if !targets[0].HTTPSupport {
		t.Error("app should have HTTPSupport=true")
	}
	if targets[1].RuntimeType != "postgresql@16" {
		t.Errorf("db RuntimeType = %q, want %q", targets[1].RuntimeType, "postgresql@16")
	}
	if targets[1].HTTPSupport {
		t.Error("db should have HTTPSupport=false")
	}
}
