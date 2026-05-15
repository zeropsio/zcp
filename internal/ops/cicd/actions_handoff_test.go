package cicd_test

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/cicd"
)

// TestComposeActionsHandoff_Stage_RawZcli pins that the stage workflow
// YAML uses raw `zcli push --setup` (NOT zeropsio/actions@v1.0.2).
// The marketplace action doesn't accept a setup parameter; stage cicd
// needs explicit setup selection for pair-keyed builds.
func TestComposeActionsHandoff_Stage_RawZcli(t *testing.T) {
	t.Parallel()
	out := cicd.ComposeActionsHandoff(cicd.ActionsHandoffInput{
		Target:    cicd.TargetStage,
		OwnerRepo: "acme/app",
		ServiceID: "svc-stage-id",
		SetupName: "stage",
		Env:       cicd.EnvContainer,
	})
	if strings.Contains(out.WorkflowYAML, "zeropsio/actions") {
		t.Errorf("stage workflow uses zeropsio/actions marketplace action; expected raw zcli (the marketplace action doesn't accept --setup)\nYAML:\n%s", out.WorkflowYAML)
	}
	if !strings.Contains(out.WorkflowYAML, "zcli push --service-id") {
		t.Errorf("stage workflow missing `zcli push --service-id`:\n%s", out.WorkflowYAML)
	}
	if !strings.Contains(out.WorkflowYAML, "--setup") {
		t.Errorf("stage workflow missing --setup flag:\n%s", out.WorkflowYAML)
	}
}

// TestComposeActionsHandoff_Stage_BranchTrigger pins the stage trigger
// shape: push to main branch.
func TestComposeActionsHandoff_Stage_BranchTrigger(t *testing.T) {
	t.Parallel()
	out := cicd.ComposeActionsHandoff(cicd.ActionsHandoffInput{
		Target:    cicd.TargetStage,
		OwnerRepo: "acme/app",
		ServiceID: "svc-stage-id",
		SetupName: "stage",
	})
	if !strings.Contains(out.WorkflowYAML, "push:") {
		t.Errorf("stage workflow missing push trigger:\n%s", out.WorkflowYAML)
	}
	if !strings.Contains(out.WorkflowYAML, "branches: [main]") {
		t.Errorf("stage workflow trigger must be branches: [main]:\n%s", out.WorkflowYAML)
	}
	if strings.Contains(out.WorkflowYAML, "tags:") {
		t.Errorf("stage workflow must not trigger on tags (that's prod cicd's shape):\n%s", out.WorkflowYAML)
	}
}

// TestComposeActionsHandoff_Prod_TagTrigger pins the prod trigger
// shape: tag push matching v*.*.*.
func TestComposeActionsHandoff_Prod_TagTrigger(t *testing.T) {
	t.Parallel()
	out := cicd.ComposeActionsHandoff(cicd.ActionsHandoffInput{
		Target:    cicd.TargetProd,
		OwnerRepo: "acme/app",
		ServiceID: "svc-prod-id",
		SetupName: "stage",
	})
	if !strings.Contains(out.WorkflowYAML, "push:") {
		t.Errorf("prod workflow missing push trigger:\n%s", out.WorkflowYAML)
	}
	if !strings.Contains(out.WorkflowYAML, "tags: ['v*.*.*']") {
		t.Errorf("prod workflow trigger must be tags: ['v*.*.*']:\n%s", out.WorkflowYAML)
	}
	if strings.Contains(out.WorkflowYAML, "branches:") {
		t.Errorf("prod workflow must not trigger on branches (that's stage cicd's shape):\n%s", out.WorkflowYAML)
	}
}

// TestComposeActionsHandoff_DistinctSecretNames pins that stage and
// prod use different GitHub secret names so both can coexist in the
// same repo without overwriting each other.
func TestComposeActionsHandoff_DistinctSecretNames(t *testing.T) {
	t.Parallel()
	stage := cicd.ComposeActionsHandoff(cicd.ActionsHandoffInput{
		Target: cicd.TargetStage, OwnerRepo: "acme/app", ServiceID: "s", SetupName: "stage",
	})
	prod := cicd.ComposeActionsHandoff(cicd.ActionsHandoffInput{
		Target: cicd.TargetProd, OwnerRepo: "acme/app", ServiceID: "p", SetupName: "stage",
	})
	if stage.SecretName != "ZEROPS_TOKEN_STAGE" {
		t.Errorf("stage secret name: got %q want ZEROPS_TOKEN_STAGE", stage.SecretName)
	}
	if prod.SecretName != "ZEROPS_TOKEN_PROD" {
		t.Errorf("prod secret name: got %q want ZEROPS_TOKEN_PROD", prod.SecretName)
	}
	if stage.SecretName == prod.SecretName {
		t.Errorf("stage + prod must use distinct secret names; both got %q", stage.SecretName)
	}
}

// TestComposeActionsHandoff_TokenSourceExpr_ContainerVsLocal pins
// that the stage handoff's secret-set command sources the token
// from $ZCP_API_KEY in container mode and from jq+.mcp.json in
// local mode. The literal value never crosses the MCP wire either
// way — both forms use shell expansion / stdin form.
func TestComposeActionsHandoff_TokenSourceExpr_ContainerVsLocal(t *testing.T) {
	t.Parallel()
	container := cicd.ComposeActionsHandoff(cicd.ActionsHandoffInput{
		Target: cicd.TargetStage, OwnerRepo: "acme/app", ServiceID: "s", SetupName: "stage", Env: cicd.EnvContainer,
	})
	if !strings.Contains(container.SecretSetCommand, "$ZCP_API_KEY") {
		t.Errorf("container env: stage secret command should pipe $ZCP_API_KEY; got %q", container.SecretSetCommand)
	}
	if strings.Contains(container.SecretSetCommand, ".mcp.json") {
		t.Errorf("container env: stage secret command must not reference .mcp.json; got %q", container.SecretSetCommand)
	}

	local := cicd.ComposeActionsHandoff(cicd.ActionsHandoffInput{
		Target: cicd.TargetStage, OwnerRepo: "acme/app", ServiceID: "s", SetupName: "stage", Env: cicd.EnvLocal,
	})
	if !strings.Contains(local.SecretSetCommand, "jq -r '.mcpServers.zcp.env.ZCP_API_KEY' .mcp.json") {
		t.Errorf("local env: stage secret command should extract via jq from .mcp.json; got %q", local.SecretSetCommand)
	}
	if strings.Contains(local.SecretSetCommand, "$ZCP_API_KEY") {
		t.Errorf("local env: stage secret command must not reference bare $ZCP_API_KEY; got %q", local.SecretSetCommand)
	}
}

// TestComposeActionsHandoff_Prod_PastedToken pins that the prod
// secret-set command uses a `<paste-prod-token>` placeholder rather
// than reading from ZCP_API_KEY. The prod token is a SEPARATE
// credential generated on the prod project's dashboard — distinct
// from the source project's ZCP_API_KEY.
func TestComposeActionsHandoff_Prod_PastedToken(t *testing.T) {
	t.Parallel()
	out := cicd.ComposeActionsHandoff(cicd.ActionsHandoffInput{
		Target: cicd.TargetProd, OwnerRepo: "acme/app", ServiceID: "p", SetupName: "stage",
	})
	if !strings.Contains(out.SecretSetCommand, "<paste-prod-token>") {
		t.Errorf("prod secret command should carry <paste-prod-token> placeholder; got %q", out.SecretSetCommand)
	}
	if strings.Contains(out.SecretSetCommand, "$ZCP_API_KEY") {
		t.Errorf("prod secret command must not reference source ZCP_API_KEY (prod uses distinct token); got %q", out.SecretSetCommand)
	}
	if strings.Contains(out.SecretSetCommand, "jq") {
		t.Errorf("prod secret command must not extract from .mcp.json; got %q", out.SecretSetCommand)
	}
}

// TestComposeActionsHandoff_WorkflowFilePaths pins distinct file
// paths so stage + prod workflows can coexist in the same .github/
// directory.
func TestComposeActionsHandoff_WorkflowFilePaths(t *testing.T) {
	t.Parallel()
	stage := cicd.ComposeActionsHandoff(cicd.ActionsHandoffInput{Target: cicd.TargetStage, OwnerRepo: "x/y", ServiceID: "s", SetupName: "stage"})
	prod := cicd.ComposeActionsHandoff(cicd.ActionsHandoffInput{Target: cicd.TargetProd, OwnerRepo: "x/y", ServiceID: "p", SetupName: "stage"})
	if stage.WorkflowFilePath != ".github/workflows/zerops-stage.yml" {
		t.Errorf("stage path: got %q want .github/workflows/zerops-stage.yml", stage.WorkflowFilePath)
	}
	if prod.WorkflowFilePath != ".github/workflows/zerops-prod.yml" {
		t.Errorf("prod path: got %q want .github/workflows/zerops-prod.yml", prod.WorkflowFilePath)
	}
}

// TestComposeActionsHandoff_OwnerRepoInSecretCommand pins that the
// gh-secret-set command targets the right repo via the -R flag.
func TestComposeActionsHandoff_OwnerRepoInSecretCommand(t *testing.T) {
	t.Parallel()
	out := cicd.ComposeActionsHandoff(cicd.ActionsHandoffInput{
		Target: cicd.TargetStage, OwnerRepo: "myorg/myrepo", ServiceID: "s", SetupName: "stage",
	})
	if !strings.Contains(out.SecretSetCommand, "-R myorg/myrepo") {
		t.Errorf("secret command missing `-R myorg/myrepo`: %q", out.SecretSetCommand)
	}
}

// TestComposeActionsHandoff_PlaceholderWhenOwnerRepoEmpty pins that
// an empty OwnerRepo input falls back to a visible placeholder so the
// user notices to substitute.
func TestComposeActionsHandoff_PlaceholderWhenOwnerRepoEmpty(t *testing.T) {
	t.Parallel()
	out := cicd.ComposeActionsHandoff(cicd.ActionsHandoffInput{
		Target: cicd.TargetStage, OwnerRepo: "", ServiceID: "s", SetupName: "stage",
	})
	if !strings.Contains(out.SecretSetCommand, "<owner>/<repo>") {
		t.Errorf("empty OwnerRepo should yield <owner>/<repo> placeholder; got %q", out.SecretSetCommand)
	}
}

// --- DetectProvider tests ---

// TestDetectProvider_GitHubDotCom — github.com URLs classify as
// ProviderGitHub. Both HTTPS and SSH shapes recognized.
func TestDetectProvider_GitHubDotCom(t *testing.T) {
	t.Parallel()
	cases := []string{
		"https://github.com/owner/repo.git",
		"https://github.com/owner/repo",
		"git@github.com:owner/repo.git",
	}
	for _, url := range cases {
		if p := cicd.DetectProvider(url); p != cicd.ProviderGitHub {
			t.Errorf("URL %q: got %v want ProviderGitHub", url, p)
		}
	}
}

// TestDetectProvider_GitLabFallback — gitlab.com + self-hosted
// gitlab.* URLs classify as ProviderGitLab. Actions push-mode isn't
// natively supported there; callers route to the webhook OAuth flow.
func TestDetectProvider_GitLabFallback(t *testing.T) {
	t.Parallel()
	cases := []string{
		"https://gitlab.com/owner/repo.git",
		"git@gitlab.com:owner/repo.git",
		"https://gitlab.example.com/owner/repo",
	}
	for _, url := range cases {
		if p := cicd.DetectProvider(url); p != cicd.ProviderGitLab {
			t.Errorf("URL %q: got %v want ProviderGitLab", url, p)
		}
	}
}

// TestDetectProvider_Unknown — unrecognized hosts (Bitbucket,
// Gitea, custom) fall back to ProviderUnknown. Callers emit manual
// zcli push guidance.
func TestDetectProvider_Unknown(t *testing.T) {
	t.Parallel()
	cases := []string{
		"https://bitbucket.org/owner/repo.git",
		"https://gitea.example.com/owner/repo",
		"",
	}
	for _, url := range cases {
		if p := cicd.DetectProvider(url); p != cicd.ProviderUnknown {
			t.Errorf("URL %q: got %v want ProviderUnknown", url, p)
		}
	}
}
