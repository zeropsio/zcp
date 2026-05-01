package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// validBuildIntegrations is the closed set of BuildIntegration values the
// agent may pass via the build-integration action.
//
//nolint:gochecknoglobals // immutable lookup table
var validBuildIntegrations = map[topology.BuildIntegration]bool{
	topology.BuildIntegrationNone:    true,
	topology.BuildIntegrationWebhook: true,
	topology.BuildIntegrationActions: true,
}

// handleBuildIntegration configures the per-pair ZCP-managed CI integration
// that responds to git pushes hitting the remote. Introduced by
// deploy-strategy decomposition Phase 5.
//
// UTILITY framing: BuildIntegration is one specific CI integration ZCP
// helps wire (webhook OAuth or GitHub Actions); users may keep independent
// CI/CD that ZCP does not track. Setting BuildIntegration=none does NOT
// mean "no build will fire" — it means "no ZCP-managed integration is
// configured."
//
// Prerequisite chain (handler-side composition per plan §3.4 Scenario C):
// when GitPushState != GitPushConfigured the response composes git-push-setup
// guidance THEN build-integration setup atoms in a single response. The
// agent walks both prereqs without a status round-trip.
//
// Modes:
//
//   - Walkthrough (input.Integration empty): synthesize options atom; no
//     mutation.
//   - Confirm (input.Integration ∈ {webhook, actions, none}): pre-check
//     GitPushState; if unconfigured return chained guidance pointer; on
//     pass write meta.BuildIntegration AND for `actions` enrich the response
//     with the workflow YAML body, prefilled `gh secret set` snippets
//     (env-aware: container reads $ZCP_API_KEY, local extracts from
//     .mcp.json), and the explicit ZEROPS_TOKEN=ZCP_API_KEY reuse hint.
//     The enrichment closes the gap surfaced in live agent feedback
//     2026-04-29 where the terse `status:configured` response left the
//     agent guessing what to do next on the GitHub side.
func handleBuildIntegration(
	ctx context.Context,
	client platform.Client,
	projectID string,
	input WorkflowInput,
	stateDir string,
	rt runtime.Info,
) (*mcp.CallToolResult, any, error) {
	if input.Service == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"service is required for build-integration",
			"Pass service=<hostname> identifying the runtime to configure"), WithRecoveryStatus()), nil, nil
	}

	meta, err := workflow.FindServiceMeta(stateDir, input.Service)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Read service meta %q: %v", input.Service, err),
			""), WithRecoveryStatus()), nil, nil
	}
	if meta == nil || !meta.IsComplete() {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Service %q is not bootstrapped", input.Service),
			"Run bootstrap first: zerops_workflow action=\"start\" workflow=\"bootstrap\""), WithRecoveryStatus()), nil, nil
	}

	// Walkthrough mode: synthesize options atom (PhaseStrategySetup).
	if input.Integration == "" {
		snap := workflow.ServiceSnapshot{
			Hostname:         input.Service,
			Mode:             meta.Mode,
			StageHostname:    meta.StageHostname,
			Bootstrapped:     true,
			CloseDeployMode:  topology.CloseModeGitPush,
			GitPushState:     meta.GitPushState,
			BuildIntegration: meta.BuildIntegration,
		}
		guidance, err := workflow.SynthesizeStrategySetup(rt, []workflow.ServiceSnapshot{snap})
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrNotImplemented,
				fmt.Sprintf("build-integration synthesis failed: %v", err),
				"Build-time defect — report it. Run `make lint-local` to verify the atom corpus."), WithRecoveryStatus()), nil, nil
		}
		return jsonResult(map[string]any{
			"status":           "walkthrough",
			"service":          input.Service,
			"gitPushState":     meta.GitPushState,
			"buildIntegration": meta.BuildIntegration,
			"guidance":         guidance,
			"nextStep":         fmt.Sprintf("Pick an integration and re-call: zerops_workflow action=\"build-integration\" service=%q integration=\"webhook|actions|none\".", input.Service),
		}), nil, nil
	}

	bi := topology.BuildIntegration(input.Integration)
	if !validBuildIntegrations[bi] {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid integration %q", input.Integration),
			"Valid values: none, webhook, actions"), WithRecoveryStatus()), nil, nil
	}

	// Pre-check the prereq chain. Setting BuildIntegration to anything other
	// than 'none' requires git-push capability — the integration fires on
	// remote pushes, which need GitPushConfigured to land in the first place.
	// 'none' is a valid no-prereq target (clears any prior integration).
	if bi != topology.BuildIntegrationNone && meta.GitPushState != topology.GitPushConfigured {
		return jsonResult(map[string]any{
			"status":   "needsGitPushSetup",
			"service":  input.Service,
			"reason":   fmt.Sprintf("Build integration %q requires git-push capability (current state: %s).", bi, meta.GitPushState),
			"nextStep": fmt.Sprintf("Run zerops_workflow action=\"git-push-setup\" service=%q first; then re-run this build-integration call.", input.Service),
		}), nil, nil
	}

	if meta.BuildIntegration == bi {
		return jsonResult(map[string]any{
			"status":           "noop",
			"service":          input.Service,
			"buildIntegration": bi,
		}), nil, nil
	}
	meta.BuildIntegration = bi
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Write service meta %q: %v", input.Service, err),
			""), WithRecoveryStatus()), nil, nil
	}

	switch bi {
	case topology.BuildIntegrationActions:
		return actionsConfirmResponse(ctx, client, projectID, input.Service, meta, rt), nil, nil
	case topology.BuildIntegrationWebhook:
		return webhookConfirmResponse(ctx, client, projectID, input.Service), nil, nil
	case topology.BuildIntegrationNone:
		return jsonResult(map[string]any{
			"status":           "configured",
			"service":          input.Service,
			"buildIntegration": bi,
			"nextStep":         "BuildIntegration cleared. Pushes to the remote will no longer trigger any ZCP-managed CI integration; any independent CI/CD you may have continues unchanged.",
		}), nil, nil
	}
	// validBuildIntegrations gate above ensures bi is one of the three
	// known values; this point is unreachable. The defensive return keeps
	// the compiler + linter happy when a future BuildIntegration variant
	// lands and this switch hasn't been updated yet.
	return convertError(platform.NewPlatformError(
		platform.ErrInvalidParameter,
		fmt.Sprintf("unhandled BuildIntegration variant %q — please file a bug", bi),
		"Run zerops_workflow action=\"status\" to recover.",
	), WithRecoveryStatus()), nil, nil
}

// actionsConfirmResponse builds the enriched confirm body for the GitHub
// Actions integration: workflow YAML, prefilled `gh secret set` snippets
// keyed by runtime env, and the explicit ZEROPS_TOKEN=ZCP_API_KEY reuse
// hint. ServiceID is looked up via ops.LookupService when client+projectID
// are available; on miss (e.g. handler called from a unit test without
// mock platform), the placeholder `<run zerops_discover>` falls in so the
// response is still self-describing.
func actionsConfirmResponse(
	ctx context.Context,
	client platform.Client,
	projectID, hostname string,
	meta *workflow.ServiceMeta,
	rt runtime.Info,
) *mcp.CallToolResult {
	serviceID := actionsLookupServiceID(ctx, client, projectID, hostname)
	owner, repo, repoOK := ops.ParseGitRemoteOwnerRepo(meta.RemoteURL)
	ownerRepo := "<owner>/<repo>"
	if repoOK {
		ownerRepo = owner + "/" + repo
	}

	body := map[string]any{
		"status":           "configured",
		"service":          hostname,
		"buildIntegration": topology.BuildIntegrationActions,
		"workflowFile": map[string]any{
			"path":    ".github/workflows/zerops.yml",
			"content": actionsWorkflowYAML(serviceID, hostname),
		},
		"secrets": []map[string]any{
			{
				"name":   "ZEROPS_TOKEN",
				"reuse":  "Same Zerops PAT as ZCP_API_KEY — DON'T generate a new token. ZCP already holds the value; reuse it as the GitHub secret to keep one credential, one rotation surface.",
				"source": ghSecretSourceHint(rt),
				"command": ghSecretSetCommand(
					"ZEROPS_TOKEN",
					ghSecretValueExpr(rt),
					ownerRepo,
				),
			},
			{
				"name":    "ZEROPS_SERVICE_ID",
				"value":   serviceID,
				"command": ghSecretSetCommand("ZEROPS_SERVICE_ID", quoteShellLiteral(serviceID), ownerRepo),
			},
		},
		"ghPatRecommendation": "Default to a fine-grained GitHub PAT scoped ONLY to " + ownerRepo + " with `Secrets: Read and write` (single-repo blast radius). GitHub PATs require an expiration — pick the longest you're comfortable with (max 1 year); set a calendar reminder to regenerate + re-run `gh secret set` before it lapses.",
		"nextStep":            "1) Write the workflow file at .github/workflows/zerops.yml. 2) Run the two `gh secret set` commands above. 3) Push the workflow file. From then on every push to main triggers the GitHub Actions deploy.",
	}
	if !repoOK {
		body["repoParseWarning"] = fmt.Sprintf(
			"Could not parse owner/repo from meta.RemoteURL=%q. Replace `<owner>/<repo>` in the commands above before running.",
			meta.RemoteURL,
		)
	}
	if serviceID == "" {
		body["serviceIDLookupWarning"] = "Could not resolve serviceId via Discover — run `zerops_discover service=" + hostname + "` and paste the numeric ID into the ZEROPS_SERVICE_ID command."
	}
	return jsonResult(body)
}

// webhookConfirmResponse builds the confirm body for the dashboard-OAuth
// webhook integration. No secrets to wire on the GitHub side (Zerops owns
// the pull), so the response points the agent at the dashboard with the
// project + service IDs prefilled — they form the deep-link path so the
// agent (or the user) lands on the exact runtime page rather than having
// to navigate from the project root.
func webhookConfirmResponse(
	ctx context.Context,
	client platform.Client,
	projectID, hostname string,
) *mcp.CallToolResult {
	serviceID := actionsLookupServiceID(ctx, client, projectID, hostname)
	dashboardURL := "https://app.zerops.io/dashboard/projects"
	if projectID != "" && serviceID != "" {
		dashboardURL = fmt.Sprintf(
			"https://app.zerops.io/dashboard/project/%s/service-stack/%s/service-stack-source-code",
			projectID, serviceID,
		)
	}
	body := map[string]any{
		"status":           "configured",
		"service":          hostname,
		"buildIntegration": topology.BuildIntegrationWebhook,
		"projectId":        projectID,
		"serviceId":        serviceID,
		"dashboardUrl":     dashboardURL,
		"dashboardSteps": []string{
			"Open " + dashboardURL + " — this deep-links straight to the runtime's source-code panel.",
			"Click the GitHub/GitLab OAuth connect button. Authorize Zerops to access the repository.",
			"Pick the repository the service should pull from, then the branch (typically main). Save.",
			"The dashboard installs the webhook on the remote with the right permissions automatically — no manual secret wiring on the GitHub side.",
		},
		"nextStep": "Once the OAuth flow is saved, every push to the chosen branch triggers a Zerops build automatically — including pushes from other contributors, not just yours.",
	}
	if projectID == "" || serviceID == "" {
		body["dashboardLookupWarning"] = "Could not deep-link to the runtime page (missing projectId and/or serviceId). Open the Zerops dashboard, navigate to the project, then to the runtime service for " + hostname + " manually."
	}
	return jsonResult(body)
}

// actionsWorkflowYAML returns the .github/workflows/zerops.yml body with
// serviceId and setup-name prefilled. setup-name defaults to the runtime
// hostname; if the user customized the setup block in zerops.yaml they can
// edit the rendered file before committing.
func actionsWorkflowYAML(serviceID, hostname string) string {
	idExpr := "${{ secrets.ZEROPS_SERVICE_ID }}"
	if serviceID == "" {
		// Fall back to the secret reference; the user must still set
		// ZEROPS_SERVICE_ID via gh secret set, but the workflow is valid.
		_ = idExpr
	}
	return fmt.Sprintf(`name: Zerops deploy
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: zeropsio/actions-setup-zcli@v1
      - run: zcli push --serviceId %s --setup %s
        env:
          ZEROPS_TOKEN: ${{ secrets.ZEROPS_TOKEN }}
`, idExpr, hostname)
}

// actionsLookupServiceID is a thin wrapper around ops.LookupService that
// returns "" when the lookup is impossible (nil client / empty projectID /
// not-found / API error). The handler degrades gracefully in those cases —
// the response still carries every other piece of guidance, just with a
// `serviceIDLookupWarning` directing the agent to run zerops_discover.
func actionsLookupServiceID(ctx context.Context, client platform.Client, projectID, hostname string) string {
	if client == nil || projectID == "" {
		return ""
	}
	svc, err := ops.LookupService(ctx, client, projectID, hostname)
	if err != nil || svc == nil {
		return ""
	}
	return svc.ID
}

// ghSecretSourceHint describes WHERE the agent should read ZCP_API_KEY from
// in the current runtime env. Container: $ZCP_API_KEY is injected. Local:
// ZCP runs from the user's machine and ZCP_API_KEY lives in .mcp.json
// alongside the MCP server config.
func ghSecretSourceHint(rt runtime.Info) string {
	if rt.InContainer {
		return "ZCP runs in a Zerops container; ZCP_API_KEY is in the container env. The command below substitutes via $ZCP_API_KEY at shell-expansion time — the literal value never crosses the MCP wire."
	}
	return "ZCP runs locally; ZCP_API_KEY lives in .mcp.json (env block of the zcp server). The command below extracts via jq at shell-expansion time — the literal value never crosses the MCP wire."
}

// ghSecretValueExpr returns the env-aware shell expression for the
// ZCP_API_KEY value. Container reads the env var directly; local extracts
// from .mcp.json via jq.
func ghSecretValueExpr(rt runtime.Info) string {
	if rt.InContainer {
		return `"$ZCP_API_KEY"`
	}
	return `"$(jq -r '.mcpServers.zcp.env.ZCP_API_KEY' .mcp.json)"`
}

// ghSecretSetCommand assembles a `gh secret set <name> -b <valueExpr> -R
// <ownerRepo>` invocation. valueExpr is already shell-quoted; ownerRepo is
// not (it's a literal owner/repo string with no shell metacharacters in
// practice — gh would fail on anything weird anyway).
func ghSecretSetCommand(name, valueExpr, ownerRepo string) string {
	return fmt.Sprintf("gh secret set %s -b %s -R %s", name, valueExpr, ownerRepo)
}

// quoteShellLiteral wraps a literal string in double quotes for safe use as
// a `gh secret set -b` argument. The values we splice (numeric serviceId)
// are tame, but consistent quoting makes the rendered commands copy-paste
// safe across any future serviceId format change.
func quoteShellLiteral(s string) string {
	if s == "" {
		return `"<run zerops_discover>"`
	}
	return `"` + s + `"`
}
