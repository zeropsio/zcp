package tools

import (
	"context"
	"fmt"
	"strconv"
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
//     with the workflow YAML body, prefilled `gh secret set` snippets, and
//     the guidance for the ZEROPS_TOKEN the user mints for CI.
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
		guidance, err := workflow.SynthesizeStrategySetup(rt, []workflow.ServiceSnapshot{buildIntegrationSnapshot(meta, input.Service)})
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrNotImplemented,
				fmt.Sprintf("build-integration synthesis failed: %v", err),
				"Build-time defect — report it. Run `make lint-local` to verify the atom corpus."), WithRecoveryStatus()), nil, nil
		}
		decision := deliveryDecisionForMeta(meta)
		recommended := string(decision.Recommended)
		builds, btDecision := resolveBuildTarget(meta, input.Service, input.BuildTarget)
		buildHost, buildSetup := firstBuild(builds)
		body := map[string]any{
			"status":                    "walkthrough",
			"service":                   input.Service,
			"gitPushState":              meta.GitPushState,
			"buildIntegration":          meta.BuildIntegration,
			"buildTarget":               buildHost,
			"buildSetup":                buildSetup,
			"recommendedIntegration":    recommended,
			"recommendedIntegrationWhy": decision.Why,
			"options": []map[string]any{
				{
					"name":        "actions",
					"label":       "GitHub Actions (recommended for GitHub)",
					"description": "Workflow YAML + gh secret set commands. Zero manual Zerops dashboard step. Requires a fine-grained PAT with Contents + Workflows + Secrets: Read and write (+ Actions: Read and Checks: Read to watch the run). See ghPatRecommendation for the exact scopes + link.",
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
		addBuildTargets(body, builds, false)
		if btDecision != nil {
			body["buildTargetDecision"] = btDecision
		}
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
			return actionsConfirmResponse(ctx, client, projectID, input.Service, input.BuildTarget, meta, rt, stateDir,
				buildIntegrationRemoteDrift(ctx, sshDeployer, rt, meta)), nil, nil
		case topology.BuildIntegrationWebhook:
			return webhookConfirmResponse(ctx, client, projectID, input.Service, input.BuildTarget, meta, stateDir), nil, nil
		case topology.BuildIntegrationNone: // nothing to re-hand-off
			builds, btDecision := resolveBuildTarget(meta, input.Service, input.BuildTarget)
			buildHost, buildSetup := firstBuild(builds)
			body := map[string]any{
				"status":           "noop",
				"service":          input.Service,
				"buildIntegration": bi,
				"buildTarget":      buildHost,
				"buildSetup":       buildSetup,
			}
			addTopologyFields(body, meta, buildHost, buildSetup, "Re-call")
			addBuildTargets(body, builds, false)
			if btDecision != nil {
				body["buildTargetDecision"] = btDecision
			}
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
		return actionsConfirmResponse(ctx, client, projectID, input.Service, input.BuildTarget, meta, rt, stateDir,
			buildIntegrationRemoteDrift(ctx, sshDeployer, rt, meta)), nil, nil
	case topology.BuildIntegrationWebhook:
		return webhookConfirmResponse(ctx, client, projectID, input.Service, input.BuildTarget, meta, stateDir), nil, nil
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
// keyed by runtime env, and the mint guidance for the CI-only ZEROPS_TOKEN.
// ServiceID is looked up via ops.LookupService when client+projectID
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
	projectID, hostname, buildTargetOverride string,
	meta *workflow.ServiceMeta,
	rt runtime.Info,
	stateDir string,
	repoDriftWarning string,
) *mcp.CallToolResult {
	builds, btDecision := resolveBuildTarget(meta, hostname, buildTargetOverride)
	buildHost, buildSetup := firstBuild(builds)
	if buildHost == "" {
		buildHost = hostname // defensive fallback
	}
	if buildSetup == "" {
		buildSetup = buildHost
	}
	serviceID := actionsLookupServiceID(ctx, client, projectID, buildHost)
	// Every further stage gets its own push in the workflow, from its own
	// service-id secret (one commit, all parts — spec-mate §2.8a).
	further := make([]actionsPush, 0, len(builds))
	for _, b := range builds[min(1, len(builds)):] {
		further = append(further, actionsPush{
			Secret:    actionsServiceIDSecret(b.Hostname),
			Setup:     b.SetupName,
			ServiceID: actionsLookupServiceID(ctx, client, projectID, b.Hostname),
		})
	}
	owner, repo, repoOK := ops.ParseGitRemoteOwnerRepo(meta.RemoteURL)
	ownerRepo := ownerRepoPlaceholder
	if repoOK {
		ownerRepo = owner + "/" + repo
	}
	// Single .git-in-artifact predicate, shared with ZCP's own ssh deploys
	// (ops.SelfBuildTarget): the CI workflow for a self-targeting service
	// must ship .git (-g) or every CI build wipes the push source's git
	// state (prod.txt T2).
	selfTarget := ops.SelfBuildTarget(meta.Hostname, buildHost)
	// GF-7: the CI template builds off the recorded tracked ref, falling
	// back to "main" for a pre-existing meta (docs/spec-workflows.md §12.6).
	trackedRef := trackedRefOrDefault(meta)
	tokenHost := ciTokenHost(meta)

	workflowDescription := "Default workflow: installs zcli directly and passes --setup, so it works when zerops.yaml has multiple setups or the setup must be selected explicitly."
	if selfTarget {
		workflowDescription += " This service is its own build target, so the push carries -g (.git ships in the artifact — without it a CI build wipes /var/www/.git) and checkout disables persist-credentials (the job token must not ride into the runtime)."
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
			"description": workflowDescription,
			"content":     actionsWorkflowYAML(buildSetup, selfTarget, trackedRef, further...),
		},
		"secrets": []map[string]any{
			{
				"name":   "ZEROPS_TOKEN",
				"mint":   ciTokenMintGuidance,
				"source": ciTokenSourceHint,
				"command": ghSecretSetCommand(
					rt, tokenHost,
					"ZEROPS_TOKEN",
					ciTokenValueExpr(),
					ownerRepo,
				),
			},
			{
				"name":    "ZEROPS_SERVICE_ID",
				"value":   serviceID,
				"command": ghSecretSetCommand(rt, tokenHost, "ZEROPS_SERVICE_ID", quoteShellLiteral(serviceID), ownerRepo),
			},
		},
		"ghTokenConveyance": map[string]any{
			"description":    ghTokenConveyanceNote(rt, ownerRepo, tokenHost, meta),
			"failureSymptom": ghTokenFailureSymptom(rt, tokenHost),
		},
		"ghPatRecommendation": "Default to " + ghPATScopeRecommendation(ownerRepo, true) + " Set a calendar reminder to regenerate + re-run `gh secret set` before it lapses.",
		"nextStep":            "1) Write workflowFile.content at .github/workflows/zerops.yml. 2) Run the two `gh secret set` commands above — each conveys GH_TOKEN per invocation (no login step, no stored gh credential; a stale gh session can never act in place of the current PAT). 3) Push the workflow file. From then on every push to main triggers the GitHub Actions deploy. Keep the default setup-aware zcli workflow unless you are certain the repository has only one setup.",
	}
	secrets, _ := body["secrets"].([]map[string]any)
	for _, push := range further {
		secrets = append(secrets, map[string]any{
			"name":    push.Secret,
			"value":   push.ServiceID,
			"command": ghSecretSetCommand(rt, tokenHost, push.Secret, quoteShellLiteral(push.ServiceID), ownerRepo),
		})
	}
	body["secrets"] = secrets
	addBuildTargets(body, builds, true)
	if !selfTarget && len(further) == 0 {
		// The compact wrapper-action variant cannot express -g, so it is
		// only offered when the build target is NOT the push source — and
		// it pushes one service, so never for a pair with several stages.
		body["alternateWorkflowFiles"] = []map[string]any{
			{
				"path":        ".github/workflows/zerops.yml",
				"variant":     "single-setup-action",
				"description": "Use only when zerops.yaml has a single setup and no explicit --setup selection is required; zeropsio/actions exposes service-id/access-token only.",
				"content":     actionsSingleSetupWorkflowYAML(trackedRef),
			},
		}
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
	if btDecision != nil {
		body["buildTargetDecision"] = btDecision
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
	projectID, hostname, buildTargetOverride string,
	meta *workflow.ServiceMeta,
	stateDir string,
) *mcp.CallToolResult {
	builds, btDecision := resolveBuildTarget(meta, hostname, buildTargetOverride)
	buildHost, buildSetup := firstBuild(builds)
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
	if len(builds) > 1 {
		// Each stage holds its own webhook: the same dashboard steps on
		// every further stage's page (one commit, all parts).
		steps := webhookDashboardSteps(dashboardURL, buildHost, buildSetup, setupMandatory)
		for _, b := range builds[1:] {
			id := actionsLookupServiceID(ctx, client, projectID, b.Hostname)
			url := "https://app.zerops.io/dashboard/projects"
			if id != "" {
				url = fmt.Sprintf("https://app.zerops.io/service-stack/%s/deploy", id)
			}
			mandatory := b.SetupName != "" && b.SetupName != b.Hostname
			steps = append(steps, webhookDashboardSteps(url, b.Hostname, b.SetupName, mandatory)...)
		}
		body["dashboardSteps"] = steps
		body["nextStep"] = "The webhook choice is recorded; the integration EXISTS only after the dashboard OAuth completes on every stage: " + hostList(builds) + ". Once each pipeline trigger is activated, every push to the chosen branch builds each of them with its own setup. ZCP verifies the integrations against the platform at launch time (or on a re-call of this action)."
	}
	addBuildTargets(body, builds, false)
	if projectID == "" || serviceID == "" {
		body["dashboardLookupWarning"] = "Could not deep-link to the runtime page (missing serviceId). Open the Zerops dashboard, navigate to the project, then to the runtime service for " + buildHost + ", and switch to the Deploy tab."
	}
	if btDecision != nil {
		body["buildTargetDecision"] = btDecision
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
	if meta.DevIsPool() {
		body["topologyNote"] = fmt.Sprintf(
			"Pool-pair build-integration: the pair's dev half is the dev pool %q — its leases push their branches, and nothing builds the pool from a push; CI runs `zcli push --setup %s` so the build lands on the stage %q (build target). %s secrets/deep-links below reflect %q.",
			meta.Hostname, buildSetup, buildHost, biLabel, buildHost,
		)
		return
	}
	if meta.Hostname == buildHost {
		return
	}
	body["topologyNote"] = fmt.Sprintf(
		"Standard-pair build-integration: configured per-pair from the dev half %q (push source = meta-mutation target); CI runs `zcli push --setup %s` so the build lands on the stage half %q (build target). %s secrets/deep-links below reflect %q.",
		meta.Hostname, buildSetup, buildHost, biLabel, buildHost,
	)
}

// buildIntegrationSnapshot is the snapshot the build-integration walkthrough
// synthesizes its options atom from: the service as the meta records it,
// with close-mode auto.
func buildIntegrationSnapshot(meta *workflow.ServiceMeta, service string) workflow.ServiceSnapshot {
	return workflow.ServiceSnapshot{
		Hostname:         service,
		Mode:             meta.Mode,
		StageHostname:    meta.FirstStage().Hostname,
		Stages:           meta.Stages,
		Bootstrapped:     true,
		CloseDeployMode:  topology.CloseModeAuto,
		GitPushState:     meta.GitPushState,
		BuildIntegration: meta.BuildIntegration,
	}
}

// anticipatedBuildTarget returns (BuildTarget, BuildSetup) for the service
// receiving builds triggered by the CI integration the user is about to
// wire. Synthesizes a "post-integration" ServiceSnapshot (closeMode=git-push,
// GitPushConfigured) regardless of meta's CURRENT closeMode — the
// integration always sends builds to the same destination once it fires,
// and the user typically wires the integration BEFORE switching close-mode.
//
// For standard / local-stage pairs: every stage, each with its setup ("prod"
// by recipe convention when a one-stage pair records none). For simple/dev/
// single-runtime modes: self, setup "" (caller defaults to hostname). For
// local-only modes: none (caller's responsibility to refuse).
func anticipatedBuilds(meta *workflow.ServiceMeta) []topology.StageRef {
	if meta == nil {
		return nil
	}
	// Deployed=true forces past-first-deploy semantics: the integration the
	// agent is wiring will fire on subsequent pushes, never on the initial
	// commit. Without this override the FirstDeployBypass branch collapses
	// BuildTarget to self for a fresh meta whose FirstDeployedAt isn't
	// stamped yet.
	snap := workflow.ServiceSnapshot{
		Hostname:        meta.Hostname,
		Mode:            meta.ModeFor(meta.Hostname),
		CloseDeployMode: topology.CloseModeAuto,
		GitPushState:    topology.GitPushConfigured,
		StageHostname:   meta.FirstStage().Hostname,
		Stages:          meta.Stages,
		DevIsPool:       meta.DevIsPool(),
		Deployed:        true,
	}
	snaps := workflow.SnapshotsFromMetas([]*workflow.ServiceMeta{meta})
	if !snap.DevIsPool {
		return workflow.Resolve(snap, snaps).Builds()
	}
	// A pair keyed by its dev pool: no push builds the pool (its image is a
	// deploy of its own, DP-7), so the CI builds every stage, each as it
	// resolves (docs/spec-workflows.md §13 PM-10).
	var builds []topology.StageRef
	for _, s := range snaps {
		if _, ok := meta.StageFor(s.Hostname); ok {
			s.CloseDeployMode = topology.CloseModeAuto
			s.GitPushState = topology.GitPushConfigured
			s.Deployed = true
			builds = append(builds, workflow.Resolve(s, snaps).Builds()...)
		}
	}
	return builds
}

// firstBuild is the first runtime of builds and its setup; "" for none.
func firstBuild(builds []topology.StageRef) (string, string) {
	if len(builds) == 0 {
		return "", ""
	}
	return builds[0].Hostname, builds[0].SetupName
}

// resolveBuildTarget is the SINGLE owner of the CI build-target decision: it
// returns the (host, setup) the wired CI will build→deploy to, plus a non-nil
// `decision` map ONLY when the resolution is a flaggable MISMATCH the agent must
// surface — the user explicitly named the dev/push-source half of a standard
// pair, but the dev-builds/stage-receives convention redirects CI to the stage
// half. This is the CHECK the prior fix (7ddc7e47) missed: it echoed the
// topology as a settled fact (addTopologyFields) but never compared the user's
// stated target to the resolved one, so a user who meant CI-on-dev followed the
// silent stage redirect and had to redo the workflow + secret by hand.
//
// An explicit `override` that names a valid pair hostname is honored as the
// user's settled choice (no decision) — closing the seam the convention used to
// have none of. The common flows stay zero-friction: user names the stage half,
// or a simple/single-runtime service → silent convention, no decision.
//
// builds is every runtime the CI builds, the first being the one the
// single-target fields name: every stage of a pair, or the one service an
// override names.
func resolveBuildTarget(meta *workflow.ServiceMeta, requestedService, override string) (builds []topology.StageRef, decision map[string]any) {
	builds = anticipatedBuilds(meta)
	conventionHost, conventionSetup := firstBuild(builds)
	if meta == nil {
		return builds, nil
	}
	// A pair keyed by its dev pool builds only its stages: the pool is never
	// a CI target, so naming it is no override and asks nothing to confirm.
	if ov := strings.TrimSpace(override); ov != "" && (ov == meta.Hostname && !meta.DevIsPool() || isStageOf(meta, ov)) {
		s := meta.SetupNameFor(ov)
		if s == "" {
			s = conventionSetup
		}
		return []topology.StageRef{{Hostname: ov, SetupName: s}}, nil
	}
	if requestedService != "" && requestedService == meta.Hostname && !meta.DevIsPool() &&
		isStageOf(meta, conventionHost) && conventionHost != meta.Hostname {
		decision = map[string]any{
			"requested": requestedService,
			"resolved":  conventionHost,
			"reason":    fmt.Sprintf("standard-pair convention: the dev half %q iterates via git-push and the stage half %q receives the CI build, so CI was targeted at %q.", meta.Hostname, conventionHost, conventionHost),
			"confirm":   fmt.Sprintf("You named %q (the dev half). Confirm with the user that CI should build→deploy to the stage half %q (convention), OR re-call with buildTarget=%q to wire CI directly on %q instead.", requestedService, conventionHost, requestedService, requestedService),
		}
	}
	return builds, decision
}

// actionsWorkflowYAML returns the default .github/workflows/zerops.yml body.
// It installs zcli directly instead of using zeropsio/actions because the
// public action exposes only service-id/access-token inputs and cannot pass
// zcli's --setup selector. setupName defaults to the runtime hostname; if the
// user customized the setup block in zerops.yaml they can edit the setup name
// before committing.
//
// selfTarget (ops.SelfBuildTarget — the single .git-in-artifact predicate,
// shared with ZCP's own ssh deploys): when the CI build target IS the push
// source (simple/single topologies), the push must carry `-g` so the
// replacement container keeps /var/www/.git (origin + history the launch
// gate reads — without it every CI build wiped them; prod.txt T2), and
// checkout must not persist its job-scoped GitHub credential into the
// .git/config that -g ships into the runtime. Pair topologies (CI builds
// the stage half) stay git-less, mirroring ZCP's cross-deploy semantics.
//
// further are the pushes of a pair's further stages (spec-mate §2.8a), one
// zcli push each after the first; a self target never has any.
func actionsWorkflowYAML(setupName string, selfTarget bool, trackedRef string, further ...actionsPush) string {
	if selfTarget {
		return fmt.Sprintf(`name: Zerops deploy
on:
  push:
    branches: [%s]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          # The artifact ships .git into the runtime (-g below); the
          # job-scoped checkout token must not ride along in .git/config.
          persist-credentials: false
      - name: Install zcli
        run: |
          curl -sSL https://zerops.io/zcli/install.sh | sh
          echo "$HOME/.local/bin" >> "$GITHUB_PATH"
      - name: Deploy to Zerops
        shell: bash
        run: |
          zcli login "$ZEROPS_TOKEN"
          # -g ships .git in the artifact: this service is its own push
          # source, and a deploy without it would wipe /var/www/.git.
          # --version-name records the built commit (GF-10), "main 7e2d4c1":
          # the pushed branch and the commit's short sha (bash's ${v::7}).
          zcli push --service-id "${{ secrets.ZEROPS_SERVICE_ID }}" --setup %s -g --version-name "${GITHUB_REF_NAME} ${GITHUB_SHA::7}"
        env:
          ZEROPS_TOKEN: ${{ secrets.ZEROPS_TOKEN }}
`, trackedRef, quoteShellLiteral(setupName))
	}
	return fmt.Sprintf(`name: Zerops deploy
on:
  push:
    branches: [%s]
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
        shell: bash
        run: |
          zcli login "$ZEROPS_TOKEN"
          # --version-name records the built commit (GF-10), "main 7e2d4c1":
          # the pushed branch and the commit's short sha (bash's ${v::7}).
          zcli push --service-id "${{ secrets.ZEROPS_SERVICE_ID }}" --setup %s --version-name "${GITHUB_REF_NAME} ${GITHUB_SHA::7}"
%s        env:
          ZEROPS_TOKEN: ${{ secrets.ZEROPS_TOKEN }}
`, trackedRef, quoteShellLiteral(setupName), actionsFurtherPushes(further))
}

// actionsPush is one further stage's push in the Actions workflow: the
// repository secret holding its service id, its setup, and the id itself.
type actionsPush struct {
	Secret, Setup, ServiceID string
}

// actionsServiceIDSecret is the repository secret holding a further stage's
// service id: ZEROPS_SERVICE_ID_<HOSTNAME>. The first build target keeps
// ZEROPS_SERVICE_ID, so a pair that gains a part keeps its secret.
func actionsServiceIDSecret(hostname string) string {
	return "ZEROPS_SERVICE_ID_" + strings.ToUpper(hostname)
}

// actionsFurtherPushes are the zcli push lines of further, "" for none.
func actionsFurtherPushes(further []actionsPush) string {
	var b strings.Builder
	for _, p := range further {
		fmt.Fprintf(&b, "          zcli push --service-id \"${{ secrets.%s }}\" --setup %s --version-name \"${GITHUB_REF_NAME} ${GITHUB_SHA::7}\"\n", p.Secret, quoteShellLiteral(p.Setup))
	}
	return b.String()
}

// addBuildTargets names every runtime the CI builds on body, only when it
// is more than one: each with its setup, and with withSecrets the Actions
// secret holding its service id. A single build target is the buildTarget
// field alone.
func addBuildTargets(body map[string]any, builds []topology.StageRef, withSecrets bool) {
	if len(builds) < 2 {
		return
	}
	targets := make([]map[string]any, 0, len(builds))
	for i, b := range builds {
		target := map[string]any{"hostname": b.Hostname, "setup": b.SetupName}
		if withSecrets {
			target["serviceIdSecret"] = "ZEROPS_SERVICE_ID"
			if i > 0 {
				target["serviceIdSecret"] = actionsServiceIDSecret(b.Hostname)
			}
		}
		targets = append(targets, target)
	}
	body["buildTargets"] = targets
}

// hostList names the hostnames of builds for a sentence: "a", "a and b",
// "a, b and c".
func hostList(builds []topology.StageRef) string {
	return joinHosts(builds, func(h string) string { return h })
}

// quotedHostList is hostList with each hostname quoted: `"a"`, `"a" and "b"`.
func quotedHostList(builds []topology.StageRef) string {
	return joinHosts(builds, strconv.Quote)
}

func joinHosts(builds []topology.StageRef, name func(string) string) string {
	hosts := make([]string, 0, len(builds))
	for _, b := range builds {
		hosts = append(hosts, name(b.Hostname))
	}
	if len(hosts) < 2 {
		return strings.Join(hosts, "")
	}
	return strings.Join(hosts[:len(hosts)-1], ", ") + " and " + hosts[len(hosts)-1]
}

// actionsSingleSetupWorkflowYAML returns the compact wrapper-action variant.
// It intentionally does not support setup selection because zeropsio/actions
// currently exposes no setup input; for the same reason it cannot pass
// --version-name (GF-10 applies only to the setup-aware zcli variant, which
// runs `zcli push` directly).
func actionsSingleSetupWorkflowYAML(trackedRef string) string {
	return fmt.Sprintf(`name: Zerops deploy
on:
  push:
    branches: [%s]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: zeropsio/actions@v1.0.2
        with:
          access-token: ${{ secrets.ZEROPS_TOKEN }}
          service-id: ${{ secrets.ZEROPS_SERVICE_ID }}
`, trackedRef)
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

// ciTokenMintGuidance / ciTokenSourceHint own the CI Zerops credential TELL
// (guide 0.9). The key ZCP itself runs on is scoped to ZCP's own project and
// stays in the container: CI is a different actor, on a machine ZCP does not
// own, so it gets a token of the person's own, scoped to the one project it
// deploys. Naming the container's key here — as the old `reuse` hint did —
// walked the agent into pasting a project-wide credential into a repository
// secret, where it outlives every rotation ZCP can see.
const (
	ciTokenMintGuidance = "A Zerops access token the USER mints for CI, never ZCP's own key: `NO_ACCESS` at the organization, `BASIC_USER` on the target project only (app.zerops.io → Access Token Management). Single-project blast radius, revocable without touching ZCP."
	ciTokenSourceHint   = "The value comes from the user — paste it into the command below. NEVER substitute a container/env variable: ZCP's key belongs to ZCP's project and must not leave the container."
)

// ciTokenValueExpr is the `gh secret set -b` argument for ZEROPS_TOKEN: a
// placeholder the agent replaces with the value the user pasted. No shell
// substitution — there is deliberately nothing in the environment to read.
func ciTokenValueExpr() string {
	return `"<the CI token the user provides — collect via ` + credentialAskMechanism + `; NEVER generate one and NEVER read it from the container env>"`
}

// ciTokenHost is the push source whose session env the gh commands read
// GIT_TOKEN from over ssh, "" when there is none: a pair keyed by its dev
// pool has no container of its own — the pool's hostname reaches a random
// one of its containers (docs/spec-workflows.md §8 PS-3, §13 PM-10) — so
// the person gives the PAT.
func ciTokenHost(meta *workflow.ServiceMeta) string {
	if meta.DevIsPool() {
		return ""
	}
	return meta.Hostname
}

// ghSecretSetCommand assembles a `gh secret set <name> -b <valueExpr> -R
// <ownerRepo>` invocation with PER-INVOCATION GH_TOKEN conveyance
// (spec-git-delivery-target §4). GH_TOKEN takes precedence over any
// previously stored gh credential (live-verified, gh 2.4+), so a stale
// `gh auth login` from an earlier session can never act in place of the
// current PAT — the wrong-token 403 diagnosis class from the prod.txt
// session dies, and no hosts.yml copy is ever created.
//
// Container: the token is read over SSH from the push source's session
// env at command time (fresh sessions read the live platform value —
// rotation-proof), guarded against the empty-token case (an empty
// GH_TOKEN would silently fall back to stored credentials/prompt).
// Local, or a container with no push source to read it from (pushHost ""
// — ciTokenHost): ZCP holds no GitHub credential it can read — the
// user-provided PAT is substituted by the agent (collected via
// AskUserQuestion, NEVER generated).
func ghSecretSetCommand(rt runtime.Info, pushHost, name, valueExpr, ownerRepo string) string {
	ghCmd := fmt.Sprintf("gh secret set %s -b %s -R %s", name, valueExpr, ownerRepo)
	if rt.InContainer && pushHost != "" {
		return fmt.Sprintf(
			"_t=$(ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null %s 'printf %%s \"$%s\"') && [ -n \"$_t\" ] && GH_TOKEN=\"$_t\" %s || echo \"%s empty on %s — re-run git-push-setup first\"",
			pushHost, ops.GitTokenEnvKey, ghCmd, ops.GitTokenEnvKey, pushHost,
		)
	}
	return "GH_TOKEN='<PAT the user provides — collect via " + credentialAskMechanism + "; NEVER generate one>' " + ghCmd
}

// ghSecretSetFromStagedSecret builds a guarded `gh secret set` that conveys a
// value held as a STAGED service secret on pushHost, secret-to-secret — neither
// the GH_TOKEN auth (GIT_TOKEN) nor the staged VALUE (valueEnvKey) ever enters
// the conversation (both read over SSH at command time). BOTH are guarded for
// emptiness: an empty staged secret would otherwise write an EMPTY repo secret
// (silent broken delivery — the launch-prod twin of the empty-token fall-through
// ghSecretSetCommand already guards). Container-only: launch-prod always runs
// from the source push container. The empty case echoes a clear marker the agent
// keys on, never a silent fall-through to stored gh credentials.
func ghSecretSetFromStagedSecret(pushHost, name, valueEnvKey, ownerRepo string) string {
	const sshFlags = "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
	read := func(key string) string {
		return fmt.Sprintf("$(ssh %s %s 'printf %%s \"$%s\"')", sshFlags, pushHost, key)
	}
	// if/then/else (not `&&…||echo`) so a real gh failure (e.g. 403) propagates
	// its own exit code + stderr instead of being masked by the empty-token echo.
	return fmt.Sprintf(
		"_t=%s; _v=%s; if [ -n \"$_t\" ] && [ -n \"$_v\" ]; then GH_TOKEN=\"$_t\" gh secret set %s -b \"$_v\" -R %s; else echo \"%s or %s empty on %s — re-run git-push-setup / re-stage the launch token first\"; fi",
		read(ops.GitTokenEnvKey), read(valueEnvKey), name, ownerRepo, ops.GitTokenEnvKey, valueEnvKey, pushHost,
	)
}

// ghSecretSetFromStagedSecretWithPAT is ghSecretSetFromStagedSecret for a
// launched part whose pair is keyed by its dev pool (docs/spec-workflows.md
// §10 P-LP-21): the staged value is read over ssh from stageHost — a runtime
// of its own, never the pool, whose hostname reaches a random container —
// and GH_TOKEN is the PAT the user provides, as the pair's build-integration
// conveys it. The staged value is guarded for emptiness as there.
func ghSecretSetFromStagedSecretWithPAT(stageHost, name, valueEnvKey, ownerRepo string) string {
	const sshFlags = "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
	return fmt.Sprintf(
		"_v=$(ssh %s %s 'printf %%s \"$%s\"'); if [ -n \"$_v\" ]; then GH_TOKEN='<PAT the user provides — collect via %s; NEVER generate one>' gh secret set %s -b \"$_v\" -R %s; else echo \"%s empty on %s — re-stage the launch token first\"; fi",
		sshFlags, stageHost, valueEnvKey, credentialAskMechanism, name, ownerRepo, valueEnvKey, stageHost,
	)
}

// ghSecretSetFromOwnLaunchTokenWithPAT is ghSecretSetFromStagedSecretWithPAT
// for a launch token staged on zcp's own service — a part keyed by its dev
// pool (docs/spec-workflows.md §10 P-LP-17): the value is read in this very
// container by `zcp self launch-token`, no ssh, and goes secret-to-secret
// into the repo secret, never through the conversation. Same empty-value
// guard: nothing is written when no token is staged.
func ghSecretSetFromOwnLaunchTokenWithPAT(name, ownerRepo string) string {
	return fmt.Sprintf(
		"_v=$(zcp self launch-token); if [ -n \"$_v\" ]; then GH_TOKEN='<PAT the user provides — collect via %s; NEVER generate one>' gh secret set %s -b \"$_v\" -R %s; else echo \"%s empty on zcp's own service — re-stage the launch token first\"; fi",
		credentialAskMechanism, name, ownerRepo, ops.LaunchTokenEnvKey,
	)
}

// ghTokenConveyanceNote explains the per-invocation GH_TOKEN model that
// replaced the `gh auth login` + hosts.yml step. pushHost "" in a container
// is a pair keyed by its dev pool (meta), which has no container to read
// the token from.
func ghTokenConveyanceNote(rt runtime.Info, ownerRepo, pushHost string, meta *workflow.ServiceMeta) string {
	if rt.InContainer && pushHost == "" && meta != nil {
		return "Each `gh secret set` command above conveys GH_TOKEN per invocation. This pair's dev half is the dev pool `" + meta.Hostname + "`, which holds no container of its own to read GIT_TOKEN from over SSH (its hostname reaches a random one of its containers) — substitute the PAT the user provides (same scope as git-push-setup: `Secrets: Read and write` on " + ownerRepo + "). Collect it via " + credentialAskMechanism + "; NEVER generate a token and NEVER read it from a lease's env. No gh login step is needed or wanted."
	}
	if rt.InContainer {
		return "Each `gh secret set` command above conveys GH_TOKEN per invocation, read over SSH from the push-source container `" + pushHost + "`'s live session env at command time. There is NO login step and no stored gh credential — GH_TOKEN takes precedence over any hosts.yml left by earlier sessions, so the command always acts with the CURRENT git-push-setup PAT (needs `Secrets: Read and write` on " + ownerRepo + "; the recommended git-push-setup PAT scope already covers it)."
	}
	return "Each `gh secret set` command above conveys GH_TOKEN per invocation. In local mode ZCP holds no GitHub credential — substitute the PAT the user provides (same scope as git-push-setup: `Secrets: Read and write` on " + ownerRepo + "). Collect it via " + credentialAskMechanism + "; NEVER generate a token. No gh login step is needed or wanted."
}

// ghTokenFailureSymptom names the env-specific failure the agent will
// actually hit with per-invocation GH_TOKEN conveyance.
func ghTokenFailureSymptom(rt runtime.Info, pushHost string) string {
	if rt.InContainer && pushHost != "" {
		return "If $" + ops.GitTokenEnvKey + " is empty on `" + pushHost + "` the command prints \"" + ops.GitTokenEnvKey + " empty …\" and performs nothing — re-run git-push-setup with the PAT to (re)write the service secret. A 403 from `gh secret set` means the CURRENT PAT lacks `Secrets: Read and write` on the target repo (the command provably used it — there is no stored credential to blame)."
	}
	return "An empty GH_TOKEN makes gh fall back to stored credentials or an interactive prompt — always substitute a real PAT collected from the user before running."
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
	// A pair keyed by its dev pool has no checkout of its own to read an
	// origin from (docs/spec-workflows.md §13 PM-10): each lease clones the
	// recorded remote, which is what the commands are built from.
	if meta.RemoteURL == "" || meta.DevIsPool() {
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
