package tools

import (
	"context"
	"fmt"
	"strings"

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
	sshDeployer ops.SSHDeployer,
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
		// Mirrors workflow_close_mode.go — point at bootstrap+adopt, not
		// generic status. Code is ErrAdoptRequired (not ErrServiceNotFound —
		// the service IS found in Zerops, it just lacks ZCP bootstrap
		// metadata). Pinned by TestErrAdoptRequiredCarriesAdoptRecovery.
		return convertError(platform.NewPlatformError(
			platform.ErrAdoptRequired,
			fmt.Sprintf("Service %q is not bootstrapped", input.Service),
			"Run bootstrap first: zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\""),
			WithRecovery(&RecoveryHint{
				Tool:   "zerops_workflow",
				Action: actionStart,
				Args:   map[string]string{"workflow": "bootstrap", "route": "adopt"},
			})), nil, nil
	}

	// Walkthrough mode: synthesize options atom (PhaseStrategySetup) and
	// emit the structured choice prompt. The `options` list is ordered with
	// the recommended choice first so agent harnesses with AskUserQuestion
	// surface Actions as the default for GitHub remotes (zero manual
	// dashboard step) and webhook as the fallback for GitLab / policy-
	// constrained setups.
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
		recommended := recommendIntegrationForRemoteURL(meta.RemoteURL)
		buildHost, buildSetup := anticipatedBuildTarget(meta)
		body := map[string]any{
			"status":                 "walkthrough",
			"service":                input.Service,
			"gitPushState":           meta.GitPushState,
			"buildIntegration":       meta.BuildIntegration,
			"buildTarget":            buildHost,
			"buildSetup":             buildSetup,
			"recommendedIntegration": recommended,
			"options": []map[string]any{
				{
					"name":        "actions",
					"label":       "GitHub Actions (recommended for GitHub)",
					"description": "Workflow YAML + gh secret set commands. Zero manual Zerops dashboard step. Requires fine-grained PAT with Contents+Secrets+Workflows scope.",
				},
				{
					"name":        "webhook",
					"label":       "Zerops dashboard webhook (fallback)",
					"description": "OAuth in Zerops dashboard authorizes pull. Required for GitLab and policy-constrained repos. Includes one manual dashboard step.",
				},
				{
					"name":        "none",
					"label":       "No ZCP-managed CI",
					"description": "Keep your existing CI/CD; ZCP does not wire anything. Pushes still land at the remote.",
				},
			},
			"inputField": "integration",
			"prompt":     "Pick a CI integration for " + input.Service + " (build target: " + buildHost + ", setup: " + buildSetup + "). Default: " + recommended + ".",
			"guidance":   guidance,
			"nextStep":   fmt.Sprintf("Pick an integration and re-call: zerops_workflow action=\"build-integration\" service=%q integration=\"actions|webhook|none\".", input.Service),
		}
		addTopologyFields(body, meta, buildHost, buildSetup, "Walkthrough")
		return jsonResult(attachWorkSessionState(body, stateDir)), nil, nil
	}

	bi := topology.BuildIntegration(input.Integration)
	if !validBuildIntegrations[bi] {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid integration %q", input.Integration),
			"Valid values: none, webhook, actions"), WithRecoveryStatus()), nil, nil
	}

	// Local-only projects have no Zerops runtime to receive builds. Refuse
	// actions/webhook with adopt-local recovery so the agent links a stage
	// first; ZEROPS_SERVICE_ID and webhook deep-link have no target without
	// it. 'none' is allowed (clears integration; no build target needed).
	if bi != topology.BuildIntegrationNone && meta.Mode == topology.PlanModeLocalOnly {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("Build integration %q requires a linked Zerops runtime; %q is local-only", bi, input.Service),
			"Link a Zerops runtime as stage first: zerops_workflow action=\"adopt-local\" targetService=\"<runtime-hostname>\". Then re-run build-integration.",
		), WithRecovery(&RecoveryHint{
			Tool:   "zerops_workflow",
			Action: "adopt-local",
		})), nil, nil
	}

	// Pre-check the prereq chain. Setting BuildIntegration to anything other
	// than 'none' requires git-push capability — the integration fires on
	// remote pushes, which need GitPushConfigured to land in the first place.
	// 'none' is a valid no-prereq target (clears any prior integration).
	if bi != topology.BuildIntegrationNone && meta.GitPushState != topology.GitPushConfigured {
		return jsonResult(attachWorkSessionState(map[string]any{
			"status":   "needsGitPushSetup",
			"service":  input.Service,
			"reason":   fmt.Sprintf("Build integration %q requires git-push capability (current state: %s).", bi, meta.GitPushState),
			"nextStep": fmt.Sprintf("Run zerops_workflow action=\"git-push-setup\" service=%q first; then re-run this build-integration call.", input.Service),
			"recovery": map[string]any{
				"tool":   "zerops_workflow",
				"action": "git-push-setup",
				"args":   map[string]string{"service": input.Service},
			},
		}, stateDir)), nil, nil
	}

	if meta.BuildIntegration == bi {
		// Stateless recompute (BI-NOOP-1): a re-call of the already-declared
		// integration returns the FULL enriched handoff — workflow file,
		// secret commands, dashboard steps — not a terse noop. Post-
		// compaction this is the only way the agent re-fetches the setup
		// instructions it lost (the declaration stamp landed; the GitHub/
		// dashboard side may still be undone).
		switch bi {
		case topology.BuildIntegrationActions:
			return actionsConfirmResponse(ctx, client, projectID, input.Service, meta, rt, stateDir,
				buildIntegrationRemoteDrift(ctx, sshDeployer, rt, meta)), nil, nil
		case topology.BuildIntegrationWebhook:
			return webhookConfirmResponse(ctx, client, projectID, input.Service, meta, stateDir), nil, nil
		default: // none — nothing to re-hand-off
			buildHost, buildSetup := anticipatedBuildTarget(meta)
			body := map[string]any{
				"status":           "noop",
				"service":          input.Service,
				"buildIntegration": bi,
				"buildTarget":      buildHost,
				"buildSetup":       buildSetup,
			}
			addTopologyFields(body, meta, buildHost, buildSetup, "Re-call")
			return jsonResult(attachWorkSessionState(body, stateDir)), nil, nil
		}
	}
	// Locked RMW: set only BuildIntegration on the fresh meta (XCUT-1).
	if err := workflow.UpdateServiceMeta(stateDir, input.Service, func(m *workflow.ServiceMeta) error {
		m.BuildIntegration = bi
		// Changing the declared integration invalidates any prior earned
		// verification — the evidence belonged to the previous choice.
		m.BuildIntegrationVerifiedAt = ""
		return nil
	}); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Write service meta %q: %v", input.Service, err),
			""), WithRecoveryStatus()), nil, nil
	}
	meta.BuildIntegration = bi // mirror onto the local copy for the handoff responses below
	meta.BuildIntegrationVerifiedAt = ""

	switch bi {
	case topology.BuildIntegrationActions:
		return actionsConfirmResponse(ctx, client, projectID, input.Service, meta, rt, stateDir,
			buildIntegrationRemoteDrift(ctx, sshDeployer, rt, meta)), nil, nil
	case topology.BuildIntegrationWebhook:
		return webhookConfirmResponse(ctx, client, projectID, input.Service, meta, stateDir), nil, nil
	case topology.BuildIntegrationNone:
		return jsonResult(attachWorkSessionState(map[string]any{
			"status":           "cleared",
			"service":          input.Service,
			"buildIntegration": bi,
			"nextStep":         "BuildIntegration cleared. Pushes to the remote will no longer trigger any ZCP-managed CI integration; any independent CI/CD you may have continues unchanged.",
		}, stateDir)), nil, nil
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
//
// Build target resolution: for a standard pair the Actions workflow must
// `zcli push` to the STAGE half (build target) with `--setup prod`, NOT to
// the dev half passed as `service=` (which is the push source for the meta
// configuration). DeployIntent.Resolve flips the snapshot's closeMode to
// git-push (anticipated state once the integration fires) and reads the
// resolved BuildTarget + BuildSetup. For simple/single-runtime modes
// BuildTarget falls back to the input hostname.
func actionsConfirmResponse(
	ctx context.Context,
	client platform.Client,
	projectID, hostname string,
	meta *workflow.ServiceMeta,
	rt runtime.Info,
	stateDir string,
	repoDriftWarning string,
) *mcp.CallToolResult {
	buildHost, buildSetup := anticipatedBuildTarget(meta)
	if buildHost == "" {
		buildHost = hostname // defensive fallback
	}
	if buildSetup == "" {
		buildSetup = buildHost
	}
	serviceID := actionsLookupServiceID(ctx, client, projectID, buildHost)
	owner, repo, repoOK := ops.ParseGitRemoteOwnerRepo(meta.RemoteURL)
	ownerRepo := "<owner>/<repo>"
	if repoOK {
		ownerRepo = owner + "/" + repo
	}

	body := map[string]any{
		"status":           "declared",
		"service":          hostname,
		"buildTarget":      buildHost,
		"buildSetup":       buildSetup,
		"buildIntegration": topology.BuildIntegrationActions,
		"verified":         meta.BuildIntegrationVerifiedAt != "",
		"verification":     buildIntegrationVerificationNote(meta),
		"workflowFile": map[string]any{
			"path":        ".github/workflows/zerops.yml",
			"variant":     "setup-aware-zcli",
			"setup":       buildSetup,
			"description": "Default workflow: installs zcli directly and passes --setup, so it works when zerops.yaml has multiple setups or the setup must be selected explicitly.",
			"content":     actionsWorkflowYAML(buildSetup),
		},
		"alternateWorkflowFiles": []map[string]any{
			{
				"path":        ".github/workflows/zerops.yml",
				"variant":     "single-setup-action",
				"description": "Use only when zerops.yaml has a single setup and no explicit --setup selection is required; zeropsio/actions exposes service-id/access-token only.",
				"content":     actionsSingleSetupWorkflowYAML(),
			},
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
		"ghAuthPrecondition": map[string]any{
			"required":       true,
			"description":    ghAuthDescription(rt, ownerRepo, meta.Hostname),
			"setupCommand":   ghAuthSetupCommand(rt, meta.Hostname),
			"verifyCommand":  "gh auth status",
			"failureSymptom": ghAuthFailureSymptom(rt, meta.Hostname),
		},
		"ghPatRecommendation": "Default to a fine-grained GitHub PAT scoped ONLY to " + ownerRepo + " with `Secrets: Read and write` (single-repo blast radius). GitHub PATs require an expiration — pick the longest you're comfortable with (max 1 year); set a calendar reminder to regenerate + re-run `gh secret set` before it lapses.",
		"nextStep":            "1) Authenticate `gh` (see ghAuthPrecondition.setupCommand). 2) Write workflowFile.content at .github/workflows/zerops.yml. 3) Run the two `gh secret set` commands above. 4) Push the workflow file. From then on every push to main triggers the GitHub Actions deploy. Keep the default setup-aware zcli workflow unless you are certain the repository has only one setup.",
	}
	addTopologyFields(body, meta, buildHost, buildSetup, "Actions")
	if repoDriftWarning != "" {
		body["repoDriftWarning"] = repoDriftWarning
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
	return jsonResult(attachWorkSessionState(body, stateDir))
}

// webhookConfirmResponse builds the confirm body for the dashboard-OAuth
// webhook integration. No secrets to wire on the GitHub side (Zerops owns
// the pull), so the response points the agent at the dashboard with the
// project + service IDs prefilled — they form the deep-link path so the
// agent (or the user) lands on the exact runtime page rather than having
// to navigate from the project root.
//
// Build target resolution: deep-link points at the BUILD TARGET service
// page, not the push source. For a standard pair the user must connect
// OAuth on the stage half so Zerops rebuilds the stage runtime from
// pushed code (matches the actions integration's `--setup prod`
// targeting). For simple/single-runtime modes the build target equals
// the input hostname. Deep-link URL shape verified live 2026-05-19:
// `/service-stack/<id>/deploy` (no project segment); the earlier
// `/dashboard/project/<proj>/service-stack/<id>/service-stack-source-code`
// slug 404s. The fallback service-stack-detail page is included in
// dashboardSteps so the agent navigates manually if the live shape
// later changes again.
func webhookConfirmResponse(
	ctx context.Context,
	client platform.Client,
	projectID, hostname string,
	meta *workflow.ServiceMeta,
	stateDir string,
) *mcp.CallToolResult {
	buildHost, buildSetup := anticipatedBuildTarget(meta)
	if buildHost == "" {
		buildHost = hostname
	}
	if buildSetup == "" {
		buildSetup = buildHost
	}
	serviceID := actionsLookupServiceID(ctx, client, projectID, buildHost)
	dashboardURL := "https://app.zerops.io/dashboard/projects"
	if serviceID != "" {
		dashboardURL = fmt.Sprintf("https://app.zerops.io/service-stack/%s/deploy", serviceID)
	}
	setupMandatory := buildSetup != "" && buildSetup != buildHost
	body := map[string]any{
		"status":              "declared",
		"service":             hostname,
		"buildTarget":         buildHost,
		"buildSetup":          buildSetup,
		"buildIntegration":    topology.BuildIntegrationWebhook,
		"verified":            meta.BuildIntegrationVerifiedAt != "",
		"verification":        buildIntegrationVerificationNote(meta),
		"projectId":           projectID,
		"serviceId":           serviceID,
		"dashboardUrl":        dashboardURL,
		"setupFieldMandatory": setupMandatory,
		"dashboardSteps":      webhookDashboardSteps(dashboardURL, buildHost, buildSetup, setupMandatory),
		"nextStep":            "The webhook choice is recorded; the integration EXISTS only after the dashboard OAuth completes. Once the pipeline trigger is activated, every push to the chosen branch triggers a build of " + buildHost + " using setup=" + buildSetup + ". Tick \"Trigger once after the activation?\" to also run an immediate build of the current branch state. ZCP verifies the integration against the platform at launch time (or on a re-call of this action).",
	}
	addTopologyFields(body, meta, buildHost, buildSetup, "Webhook")
	if projectID == "" || serviceID == "" {
		body["dashboardLookupWarning"] = "Could not deep-link to the runtime page (missing serviceId). Open the Zerops dashboard, navigate to the project, then to the runtime service for " + buildHost + ", and switch to the Deploy tab."
	}
	return jsonResult(attachWorkSessionState(body, stateDir))
}

// webhookDashboardSteps builds the per-step dashboard instructions for the
// webhook integration. Captures the empirical UI shape verified on
// 2026-05-19 (eval-zcp live test, service-stack/<id>/deploy panel):
//
//   - The integration page lives at `/service-stack/<serviceId>/deploy`,
//     NOT `/service-stack-source-code` (legacy slug that 404s).
//   - The trigger dialog offers New Tag or Push to Branch — branch flow
//     is the default happy path; tag flow is for production-grade CD.
//   - The "setup from zerops.yml" field is labelled optional in the UI but
//     dashboard maps hostname → setup with NO fallback, so for any service
//     whose hostname doesn't match a setup block name (e.g. recipe-style
//     standard pair: stage hostname `appstage` vs setup name `prod`), the
//     field IS mandatory — leaving it blank surfaces a "The setup was not
//     found" error.
//   - "Trigger once after the activation?" runs an immediate build of the
//     current branch state without waiting for the next push.
func webhookDashboardSteps(dashboardURL, buildHost, buildSetup string, setupMandatory bool) []string {
	setupStep := "In the `setup` field, leave blank (the dashboard maps the service hostname `" + buildHost + "` to a matching setup block in zerops.yaml)."
	if setupMandatory {
		setupStep = "In the `setup` field, type `" + buildSetup + "`. MANDATORY despite the optional label: dashboard maps service hostname → setup block, and `" + buildHost + "` does not match a setup block in zerops.yaml — leaving blank surfaces \"The setup was not found\"."
	}
	return []string{
		"Open " + dashboardURL + " — the Deploy tab on the build target service. Click the GitHub/GitLab integration button to start OAuth.",
		"Authorize Zerops to access the repository. Pick the repo, then choose trigger type: Push to Branch (typical) or New Tag (production-grade CD with tag regex).",
		"For Push to Branch: select the branch (typically `main`).",
		setupStep,
		"Optional: tick \"Trigger once after the activation?\" to run an immediate build of the current branch state right after save.",
		"Click Activate pipeline trigger. Zerops installs the webhook on the remote automatically — no manual secret wiring on the GitHub side.",
	}
}

// addTopologyFields enriches a build-integration response body with the
// canonical push source + a human-readable topology note for standard pairs.
// Roles:
//   - pushSource: meta.Hostname (the canonical dev half). Computed from
//     canonical meta, NOT from input echo — pair-keyed FindServiceMeta
//     resolves both halves to the same dev meta, so an agent passing the
//     stage hostname would receive a lying input-echoed pushSource.
//   - topologyNote: only emitted when meta.Hostname != buildHost (standard /
//     local-stage pairs). Simple / dev / single-runtime modes have
//     pushSource == buildTarget so the note would be noise.
//
// biLabel is the integration name ("Actions" / "Webhook") used inside the note.
func addTopologyFields(body map[string]any, meta *workflow.ServiceMeta, buildHost, buildSetup string, biLabel string) {
	if meta == nil {
		return
	}
	body["pushSource"] = meta.Hostname
	if meta.Hostname == buildHost {
		return
	}
	body["topologyNote"] = fmt.Sprintf(
		"Standard-pair build-integration: configured per-pair from the dev half %q (push source = meta-mutation target); CI runs `zcli push --setup %s` so the build lands on the stage half %q (build target). %s secrets/deep-links below reflect %q.",
		meta.Hostname, buildSetup, buildHost, biLabel, buildHost,
	)
}

// anticipatedBuildTarget returns (BuildTarget, BuildSetup) for the service
// receiving builds triggered by the CI integration the user is about to
// wire. Synthesizes a "post-integration" ServiceSnapshot (closeMode=git-push,
// GitPushConfigured) regardless of meta's CURRENT closeMode — the
// integration always sends builds to the same destination once it fires,
// and the user typically wires the integration BEFORE switching close-mode.
//
// For standard / local-stage pairs: BuildTarget = StageHostname, BuildSetup
// = "prod" (matches recipe convention). For simple/dev/single-runtime modes:
// BuildTarget = self, BuildSetup = "" (caller defaults to hostname). For
// local-only modes: BuildTarget = "" (caller's responsibility to refuse).
func anticipatedBuildTarget(meta *workflow.ServiceMeta) (string, string) {
	if meta == nil {
		return "", ""
	}
	// Deployed=true forces past-first-deploy semantics: the integration the
	// agent is wiring will fire on subsequent pushes, never on the initial
	// commit. Without this override the FirstDeployBypass branch collapses
	// BuildTarget to self for a fresh meta whose FirstDeployedAt isn't
	// stamped yet.
	snap := workflow.ServiceSnapshot{
		Hostname:        meta.Hostname,
		Mode:            meta.ModeFor(meta.Hostname),
		CloseDeployMode: topology.CloseModeGitPush,
		GitPushState:    topology.GitPushConfigured,
		StageHostname:   meta.StageHostname,
		Deployed:        true,
	}
	snaps := workflow.SnapshotsFromMetas([]*workflow.ServiceMeta{meta})
	intent := workflow.Resolve(snap, snaps)
	return intent.BuildTarget, intent.BuildSetup
}

// actionsWorkflowYAML returns the default .github/workflows/zerops.yml body.
// It installs zcli directly instead of using zeropsio/actions because the
// public action exposes only service-id/access-token inputs and cannot pass
// zcli's --setup selector. setupName defaults to the runtime hostname; if the
// user customized the setup block in zerops.yaml they can edit the setup name
// before committing.
func actionsWorkflowYAML(setupName string) string {
	return fmt.Sprintf(`name: Zerops deploy
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install zcli
        run: |
          curl -sSL https://zerops.io/zcli/install.sh | sh
          echo "$HOME/.local/bin" >> "$GITHUB_PATH"
      - name: Deploy to Zerops
        run: |
          zcli login "$ZEROPS_TOKEN"
          zcli push --service-id "${{ secrets.ZEROPS_SERVICE_ID }}" --setup %s
        env:
          ZEROPS_TOKEN: ${{ secrets.ZEROPS_TOKEN }}
`, quoteShellLiteral(setupName))
}

// actionsSingleSetupWorkflowYAML returns the compact wrapper-action variant.
// It intentionally does not support setup selection because zeropsio/actions
// currently exposes no setup input.
func actionsSingleSetupWorkflowYAML() string {
	return `name: Zerops deploy
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: zeropsio/actions@v1.0.2
        with:
          access-token: ${{ secrets.ZEROPS_TOKEN }}
          service-id: ${{ secrets.ZEROPS_SERVICE_ID }}
`
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
	return "ZCP runs locally; ZCP_API_KEY lives in .mcp.json (env block of the zerops server). The command below extracts via jq at shell-expansion time — the literal value never crosses the MCP wire."
}

// ghSecretValueExpr returns the env-aware shell expression for the
// ZCP_API_KEY value. Container reads the env var directly; local extracts
// from .mcp.json via jq.
func ghSecretValueExpr(rt runtime.Info) string {
	if rt.InContainer {
		return `"$ZCP_API_KEY"`
	}
	return `"$(jq -r '.mcpServers.zerops.env.ZCP_API_KEY' .mcp.json)"`
}

// ghSecretSetCommand assembles a `gh secret set <name> -b <valueExpr> -R
// <ownerRepo>` invocation. valueExpr is already shell-quoted; ownerRepo is
// not (it's a literal owner/repo string with no shell metacharacters in
// practice — gh would fail on anything weird anyway).
func ghSecretSetCommand(name, valueExpr, ownerRepo string) string {
	return fmt.Sprintf("gh secret set %s -b %s -R %s", name, valueExpr, ownerRepo)
}

// ghAuthSetupCommand returns the env-aware command that authenticates the
// agent's `gh` CLI for the upcoming `gh secret set` calls. It is DERIVED from
// the credential's real owner, never an authored literal (the eval-only
// $ZCP_E2E_GITHUB_PAT regression, born in 8201d826, is exactly the failure of
// authoring this string by hand).
//
// Container: the PAT git-push-setup collected lives as $GIT_TOKEN on the
// push-source container (meta.Hostname) — git-push-setup wrote it to project
// env and restarted that container so the value is live in ITS shell, not in
// the agent's (the agent runs on the separate zcp control-plane container,
// which is never restarted and so never sees the project env). The command
// reads the token over SSH from the push source and pipes it into a LOCAL
// `gh auth login` (where `gh secret set` then runs). Guards: `gh auth status`
// short-circuits an already-authed CLI (idempotent); `[ -n "$_t" ]` blocks the
// empty-token path that makes `gh` silently fall back to its interactive
// device-code flow and hang the whole tool call.
//
// Local: ZCP holds NO GitHub credential in local mode (confirmGitPushSetupLocal
// collects none by design). The only honest tell is to authenticate gh with a
// PAT the user supplies — collected via AskUserQuestion, NEVER generated.
func ghAuthSetupCommand(rt runtime.Info, pushHost string) string {
	if rt.InContainer {
		remoteRead := fmt.Sprintf(
			"ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null %s 'printf %%s \"$%s\"'",
			pushHost, ops.GitTokenEnvKey,
		)
		return fmt.Sprintf(
			"gh auth status >/dev/null 2>&1 || { _t=$(%s); [ -n \"$_t\" ] && printf %%s \"$_t\" | gh auth login --with-token || echo \"%s empty on %s — re-run git-push-setup first\"; }",
			remoteRead, ops.GitTokenEnvKey, pushHost,
		)
	}
	return "printf %s '<PAT the user provides — collect via AskUserQuestion; NEVER generate one>' | gh auth login --with-token"
}

// ghAuthDescription explains WHERE the gh-auth credential comes from in the
// current runtime env, mirroring ghSecretSourceHint for the auth step.
func ghAuthDescription(rt runtime.Info, ownerRepo, pushHost string) string {
	if rt.InContainer {
		return "The `gh secret set` commands below require an authenticated `gh` CLI, which fresh containers do NOT have. The PAT you set in git-push-setup lives as $" + ops.GitTokenEnvKey + " on the push-source container `" + pushHost + "` (not in this shell). setupCommand reads it over SSH and authenticates `gh` here; it needs `Secrets: Read and write` on " + ownerRepo + " (the git-push-setup PAT already covers this when scoped Secrets+Workflows)."
	}
	return "The `gh secret set` commands below require an authenticated `gh` CLI. In local mode ZCP holds no GitHub credential — authenticate `gh` with the PAT the user provides (same scope as git-push-setup: `Secrets: Read and write` on " + ownerRepo + "). Collect it via AskUserQuestion; NEVER generate a token."
}

// ghAuthFailureSymptom names the env-specific failure the agent will actually
// hit, so the diagnosis points at the real cause instead of a phantom 401.
func ghAuthFailureSymptom(rt runtime.Info, pushHost string) string {
	if rt.InContainer {
		return "If $" + ops.GitTokenEnvKey + " is empty on `" + pushHost + "` the setupCommand prints \"" + ops.GitTokenEnvKey + " empty …\" and gh stays unauthenticated — re-run git-push-setup to (re)write the token and restart `" + pushHost + "`. A bare `gh auth login --with-token` with no piped token falls back to the interactive device-code flow and hangs."
	}
	return "An empty or omitted token makes `gh auth login --with-token` fall back to the interactive device-code flow and hang — always pipe a real PAT collected from the user."
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

// buildIntegrationRemoteDrift live-reads the push source's origin and
// compares repo identity against the recorded meta.RemoteURL — the value
// every owner/repo-bearing command in the actions confirm derives from.
// A stale cache would emit `gh secret set ... -R <wrong-repo>` commands;
// the warning names both URLs so the agent re-runs git-push-setup first.
// Read failures return "" (the confirm proceeds on the recorded value —
// the launch gate re-verifies with a hard block later).
func buildIntegrationRemoteDrift(ctx context.Context, sshDeployer ops.SSHDeployer, rt runtime.Info, meta *workflow.ServiceMeta) string {
	if meta.RemoteURL == "" {
		return ""
	}
	if rt.InContainer && sshDeployer == nil {
		return ""
	}
	live, err := launchLiveRemoteReader(ctx, sshDeployer, rt, meta.Hostname)
	if err != nil {
		return ""
	}
	live = strings.TrimSpace(live)
	if live == "" || topology.CanonicalRepoURL(live) == topology.CanonicalRepoURL(meta.RemoteURL) {
		return ""
	}
	return fmt.Sprintf(
		"Live `git remote get-url origin` on %s (%s) differs from the recorded RemoteURL (%s) the commands below are built from. Re-run zerops_workflow action=\"git-push-setup\" service=%q remoteUrl=<correct-url> to realign before running the gh commands — otherwise the secrets land on the wrong repository.",
		meta.Hostname,
		topology.RedactRepoURLCredentials(live),
		topology.RedactRepoURLCredentials(meta.RemoteURL),
		meta.Hostname,
	)
}
