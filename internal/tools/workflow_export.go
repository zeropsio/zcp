package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handleExport orchestrates the three-call export-buildFromGit workflow
// per plan §3.5. Stateless multi-call narrowing via per-request inputs
// on WorkflowInput (TargetService, EnvClassifications).
//
//   - Phase A — probe. When TargetService is empty, the handler returns
//     a scope-prompt listing project runtimes. The chosen hostname alone
//     selects the half of a pair (appdev → dev, appstage → stage) — there
//     is no separate variant choice.
//   - Phase B — generate. Reads /var/www/zerops.yaml + git remote +
//     project envs + managed services upstream, calls ops.BuildBundle,
//     and returns the preview + per-env classify-prompt when
//     EnvClassifications is empty.
//   - Phase C — publish. Chains to setup-git-push when
//     GitPushState != GitPushConfigured. Otherwise returns the bundle
//     plus the SSH write + commit + zerops_deploy strategy="git-push"
//     instruction set the agent executes.
//
// Atom synthesis with service context: each per-status response
// constructs a typed StateEnvelope via workflow.BuildExportEnvelope —
// single-entry Services slice carrying the targetService snapshot for
// known-target statuses, empty Services for scope-prompt — and renders
// the matching atom(s) into the `guidance` field via
// workflow.RenderExportGuidance. The new `exportStatus:` axis on each
// export atom plus the `phases: [export-active]` gate disambiguates
// per-status fire sets so atoms no longer overmatch (six-atom together
// render fixed in 0b.7). Service-scoped axes paired with `exportStatus:`
// (e.g. `runtimes: [implicit-webserver]` + `exportStatus:
// [scaffold-required]`) fire on the targetService's snapshot — the
// silent-non-firing problem SynthesizeImmediatePhase had is gone. Plan
// `plans/atom-corpus-verification-2026-05-02.md` Phase 0b.
//
// sshDeployer may be nil when zcp runs outside a Zerops container —
// the handler returns a clear error pointing the user to a container
// or configured SSH host since Phase A requires SSH-read access.
//
//nolint:maintidx // linear three-call narrowing (scope-prompt → classify-prompt → publish); the maintainability index is dominated by Halstead volume (sequential guard + bundle-input plumbing), not nested control flow.
func handleExport(
	ctx context.Context,
	projectID string,
	_ *workflow.Engine,
	client platform.Client,
	input WorkflowInput,
	sshDeployer ops.SSHDeployer,
	stateDir string,
	rt runtime.Info,
) (*mcp.CallToolResult, any, error) {
	if client == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Platform client unavailable — export requires API access for Discover and project envs",
			"Ensure ZCP is configured with a Zerops API key (ZCP_API_KEY) before invoking export."), WithRecoveryStatus()), nil, nil
	}
	if projectID == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Project ID unavailable — export requires a configured project context",
			"Ensure ZCP is bound to a Zerops project (ZCP_PROJECT_ID or zcp config)."), WithRecoveryStatus()), nil, nil
	}

	corpus, err := workflow.LoadAtomCorpus()
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	envOpts := workflow.ExportEnvelopeOpts{
		Client:      client,
		ProjectID:   projectID,
		StateDir:    stateDir,
		Environment: workflow.DetectEnvironment(rt),
	}

	if input.TargetService == "" {
		return scopePromptResponse(ctx, client, projectID, envOpts, corpus)
	}

	// Discover with NO hostname filter — we need ALL project services so
	// collectManagedServices can include managed deps in the bundle. Per
	// Phase 8 eval finding (2026-04-29): passing input.TargetService as the
	// hostname filter limited Discover to a single service, leaving the
	// managed-services collector empty and producing bundles missing the
	// db/redis/etc. entries plan §3.4 requires for `${db_*}` reference
	// resolution at re-import.
	discover, err := ops.Discover(ctx, client, projectID, "", false, false, false)
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	var svc ops.ServiceInfo
	found := false
	for _, s := range discover.Services {
		if s.Hostname == input.TargetService {
			svc = s
			found = true
			break
		}
	}
	if !found {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Service %q not found in project", input.TargetService),
			"Pass targetService=<runtime-hostname>. Discover the project's runtimes via zerops_discover."), WithRecoveryStatus()), nil, nil
	}
	// PS-3: a pool or Box is not the app — refuse before any SSH read (a read
	// by a pool's hostname reaches a random one of its containers).
	if err := refuseNonRuntimeKind(svc.Hostname, svc.Type, "export"); err != nil {
		return convertError(exportPoolKeyingRefusal(stateDir, svc, err), WithRecoveryStatus()), nil, nil
	}
	if svc.IsInfrastructure {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Service %q is a managed service — export targets runtime services only", input.TargetService),
			"Pick a runtime hostname (the buildFromGit-bearing source) — managed services come along automatically."), WithRecoveryStatus()), nil, nil
	}

	// Resolve topology.Mode from ServiceMeta (state-dir authoritative for
	// the deploy-mode dimension; svc.Mode from Discover is the platform's
	// HA / NON_HA scaling mode, not the bootstrap-assigned topology.Mode).
	meta, _ := workflow.FindServiceMeta(stateDir, input.TargetService)
	if meta == nil || !meta.IsComplete() {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Service %q has no bootstrapped meta — export needs the topology.Mode (dev / standard / stage / simple / local-stage / local-only) to resolve variant", input.TargetService),
			"Run bootstrap first: zerops_workflow action=\"start\" workflow=\"bootstrap\". Or adopt the service via adopt-local."), WithRecoveryStatus()), nil, nil
	}
	// EXPORT-1: project the mode for the CHOSEN hostname, not the pair's
	// dev-half mode. meta.Mode is the dev-half mode (ModeStandard for a
	// standard pair); exporting the STAGE half must resolve as ModeStage
	// so setup-candidate selection + the runtime import mode key off the
	// right half. meta.ModeFor(hostname) returns ModeStage for a stage
	// hostname, ModeStandard/ModeDev/ModeSimple for the dev half. The
	// chosen hostname alone determines the half — there is no separate
	// dev/stage `variant` choice (it was inert: the composer discarded it).
	sourceMode := meta.ModeFor(input.TargetService)
	if sourceMode == "" {
		// Fallback for project-keyed local metas where ModeFor short-
		// circuits on the project name — use the meta's recorded Mode.
		sourceMode = meta.Mode
	}

	// A part whose pair is keyed by its dev pool has no checkout: it is read
	// from its origin, never a container (§9 E10); every other runtime from
	// its own checkout over ssh.
	var src exportSource
	var early *mcp.CallToolResult
	if meta.DevIsPool() {
		src, early = exportPoolPartSource(ctx, client, rt, projectID, meta, input.TargetService)
	} else {
		src, early = exportCheckoutSource(ctx, sshDeployer, stateDir, meta, input.TargetService, envOpts, corpus)
	}
	if early != nil {
		return early, nil, nil
	}
	zeropsYAMLBody, repoURL, remoteWarnings := src.ZeropsYAMLBody, src.RepoURL, src.Warnings

	setupName := ""
	if src.Origin != nil {
		// The stage's recorded setup, when the origin's zerops.yaml has it.
		if stage, ok := meta.StageFor(input.TargetService); ok && hasSetupNamed(zeropsYAMLBody, stage.SetupName) {
			setupName = stage.SetupName
		}
	}
	if setupName == "" {
		if setupName, err = pickSetupName(zeropsYAMLBody, input.TargetService, sourceMode); err != nil {
			return convertError(err, WithRecoveryStatus()), nil, nil
		}
	}

	projectEnvs, err := readProjectEnvs(ctx, client, projectID)
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}

	managedServices := collectManagedServices(discover, input.TargetService)
	// R7 identity snapshot: carry each profile-bearing dep's live tier so a
	// re-import keeps it instead of reverting to the platform default. Reads
	// the FULL service stack (the Discover list omits autoscalingProfileId);
	// non-fatal — an unread profile is simply omitted.
	enrichManagedProfiles(ctx, client, discover, managedServices)

	// GAP0-1: carry the runtime's USER-SET service env (slim /env USER
	// minus yaml-baked) so a key set via `zerops_env set
	// serviceHostname=X` survives re-import. A read failure (slim /env, or
	// the yaml-layer fetch on a live runtime) is non-fatal — warn + omit,
	// never block the export; the warning is the agent-facing signal that
	// the bundle's service vault may be incomplete (a silent omission is the
	// regression this layer closes).
	serviceUserEnvs, secErr := ops.FetchServiceUserEnvs(ctx, client, svc.ServiceID)
	if secErr != nil {
		remoteWarnings = append(remoteWarnings, fmt.Sprintf("read service user envs for %q: %v (the service vault is left out of the bundle)", svc.Hostname, secErr))
	}

	// R7: read the live autoscaling shape so the bundle reproduces the deployed
	// scaling on re-import. Non-fatal — a read failure yields nil and the
	// composer emits a "scaling unread" warning rather than blocking export, so
	// the error is intentionally discarded (the warning is the agent-facing signal).
	scaling, _ := ops.FetchServiceScaling(ctx, client, svc.ServiceID)

	inputs := ops.BundleInputs{
		ProjectName:      discover.Project.Name,
		TargetHostname:   input.TargetService,
		ServiceType:      svc.Type,
		SubdomainEnabled: svc.SubdomainEnabled,
		SetupName:        setupName,
		ZeropsYAMLBody:   zeropsYAMLBody,
		RepoURL:          repoURL,
		ProjectEnvs:      bundleProjectEnvsFromSource(projectEnvs),
		ServiceEnvs:      serviceUserEnvsToBundleSecrets(serviceUserEnvs),
		ManagedServices:  managedServices,
		Scaling:          scaling,
	}

	if err := validateEnvClassifications(input.EnvClassifications); err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	classifications := convertClassificationsInput(input.EnvClassifications)
	bundle, err := ops.BuildBundle(inputs, classifications)
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	bundle.Warnings = append(bundle.Warnings, remoteWarnings...)
	if src.Origin != nil {
		bundle.ZeropsYAMLSource = exportZeropsYAMLSourceOrigin
	}

	if needsClassifyPrompt(input.EnvClassifications, projectEnvs) {
		return classifyPromptResponse(ctx, bundle, projectEnvs, classifications, envOpts, corpus), nil, nil
	}

	// Validation gates publish ahead of git-push-setup. Per Codex Phase 5
	// POST-WORK amendment 4: validation-failed must outrank
	// git-push-setup-required because a schema-invalid bundle would fail
	// at re-import even after setup completes — surfacing the publish
	// prereq first would mask the real blocker. The git-push-setup chain
	// includes `preview.errors` either way (via bundlePreview), so the
	// agent doesn't lose visibility on validation issues while resolving
	// setup separately.
	if len(bundle.Errors) > 0 {
		return validationFailedResponse(ctx, bundle, envOpts, corpus), nil, nil
	}

	// A pool-keyed part publishes through a lease of its pool, never the
	// stage's ssh writes and git-push publish-ready names (§9 E10).
	if src.Origin != nil {
		return poolPartComposeReadyResponse(ctx, bundle, src.Origin, meta, envOpts, corpus), nil, nil
	}

	if meta.GitPushState != topology.GitPushConfigured {
		// Compose-ready (R5/R7): the validated bundle IS the deliverable —
		// hand it back now rather than blocking behind git-push setup. The
		// earlier git-push-setup gate withheld a fully-composed, schema-clean
		// bundle from a user who simply hadn't configured git-push (the common
		// case). Publishing via git-push is an OPTIONAL follow-on surfaced in
		// nextSteps, not a precondition for generation.
		return composeReadyResponse(ctx, bundle, envOpts, corpus), nil, nil
	}

	return publishGuidanceResponse(ctx, bundle, envOpts, corpus), nil, nil
}

// exportZeropsYAMLSourceOrigin is the bundle's zeropsYamlSource for a
// zerops.yaml read from a pool-keyed part's origin (§9 E10).
const exportZeropsYAMLSourceOrigin = "origin"

// exportSource is what export packages a runtime from: the zerops.yaml, the
// repository its buildFromGit names and the read's warnings; Origin is the
// origin read of a part whose pair is keyed by its dev pool (§9 E10), nil
// for a runtime read from its own checkout.
type exportSource struct {
	ZeropsYAMLBody string
	RepoURL        string
	Warnings       []string
	Origin         *poolPartSource
}

// exportCheckoutSource reads the runtime target from its own checkout over
// ssh: its zerops.yaml and live origin, refreshing the meta's RemoteURL
// cache from the live origin (E5). The response is set when export answers
// before a bundle: no ssh, a scaffold or a git-push setup to do first.
func exportCheckoutSource(
	ctx context.Context,
	sshDeployer ops.SSHDeployer,
	stateDir string,
	meta *workflow.ServiceMeta,
	target string,
	envOpts workflow.ExportEnvelopeOpts,
	corpus []workflow.KnowledgeAtom,
) (exportSource, *mcp.CallToolResult) {
	if sshDeployer == nil {
		return exportSource{}, convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"SSH access unavailable — export Phase A reads /var/www/zerops.yaml + git remote from the chosen container",
			"Run zcp from a Zerops container (recommended) or configure local SSH credentials before invoking export."), WithRecoveryStatus())
	}

	zeropsYAMLBody, err := readZeropsYAMLBody(ctx, sshDeployer, target)
	if err != nil {
		return exportSource{}, convertError(err, WithRecoveryStatus())
	}
	if strings.TrimSpace(zeropsYAMLBody) == "" {
		return exportSource{}, scaffoldChainResponse(ctx, target, envOpts, corpus)
	}

	repoURL, err := readGitRemoteURL(ctx, sshDeployer, target)
	if err != nil {
		return exportSource{}, convertError(err, WithRecoveryStatus())
	}
	if strings.TrimSpace(repoURL) == "" {
		return exportSource{}, gitPushSetupChainResponse(ctx, target, nil, "no git remote configured in /var/www", envOpts, corpus)
	}

	// Phase 6 — refresh ServiceMeta.RemoteURL cache from the live remote.
	// The live value always wins for the bundle (handler passed it as
	// repoURL above); the cache is just a hint for unrelated tooling
	// that doesn't SSH-read on every invocation. When live differs from
	// cache, surface a warning so the agent knows the cache was stale.
	remoteWarnings, refreshErr := refreshRemoteURLCache(stateDir, meta, repoURL)
	if refreshErr != nil {
		// Cache write failures are non-fatal — the bundle is still
		// publish-ready against the live remote, agents just lose the
		// cache freshness benefit. Surface the failure as a warning.
		remoteWarnings = append(remoteWarnings, fmt.Sprintf(
			"refresh ServiceMeta.RemoteURL cache for %q: %v (non-fatal — bundle uses live remote regardless)",
			target, refreshErr))
	}
	return exportSource{ZeropsYAMLBody: zeropsYAMLBody, RepoURL: repoURL, Warnings: remoteWarnings}, nil
}

// exportPoolPartSource reads target, the stage of a pair keyed by its dev
// pool, from the pair's origin (§9 E10, §10 P-LP-19) — never the pool, a
// lease or the stage. An origin it cannot read, or whose ref holds no
// zerops.yaml, is refused naming what to fix; E5's cache refresh does not
// apply: the recorded origin is what the pool's leases clone.
func exportPoolPartSource(
	ctx context.Context,
	client platform.Client,
	rt runtime.Info,
	projectID string,
	meta *workflow.ServiceMeta,
	target string,
) (exportSource, *mcp.CallToolResult) {
	origin, err := readPoolPartOrigin(ctx, poolPartOriginDeps{client: client, rt: rt, projectID: projectID}, meta)
	if err != nil {
		return exportSource{}, convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Export reads %q from its origin and the origin could not be read: %v", target, err),
			"Fix what the message names, then re-call export."), WithRecoveryStatus())
	}
	if strings.TrimSpace(origin.ZeropsYAMLBody) == "" {
		return exportSource{}, convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Export reads %q from its origin, and %s at %s (commit %s) holds no zerops.yaml — the pair is keyed by the dev pool %q, which holds no checkout of its own to scaffold one in",
				target, topology.RedactRepoURLCredentials(origin.RepoURL), origin.Ref, origin.CommitSHA, meta.Hostname),
			fmt.Sprintf("Commit a zerops.yaml with the stage's setup in a lease of the pool (zerops_lease action=\"acquire\" pool=%q), push it so it reaches %s, then re-call export.", meta.Hostname, origin.Ref)), WithRecoveryStatus())
	}
	return exportSource{ZeropsYAMLBody: origin.ZeropsYAMLBody, RepoURL: origin.RepoURL, Origin: origin}, nil
}

// exportPoolKeyingRefusal is the PS-3 refusal of a pool named as the export
// target; when the pool keys a pair it names the stage to export instead
// (§9 E10). Every other refusal is answered as it is.
func exportPoolKeyingRefusal(stateDir string, svc ops.ServiceInfo, refusal error) error {
	meta, _ := workflow.FindServiceMeta(stateDir, svc.Hostname)
	if !meta.DevIsPool() || meta.Hostname != svc.Hostname || !meta.HasStage() {
		return refusal
	}
	stage := meta.FirstStage().Hostname
	return platform.NewPlatformError(
		platform.ErrInvalidParameter,
		fmt.Sprintf("Service %q is a Swarm pool (%s) — the dev pool the pair of stage %q is keyed by, not the app's runtime, so export cannot use it as the source (reading its files by hostname would reach a random container)", svc.Hostname, svc.Type, stage),
		fmt.Sprintf("Export the part by its stage: re-call export with targetService=%q — it is read from the pair's origin, never the pool.", stage))
}

// poolPartComposeReadyResponse is export's terminal for the stage of a pair
// keyed by its dev pool (§9 E10): the bundle is the deliverable, composed
// from the origin read source names, and it reaches the repository through a
// lease of the pool — the pair has no checkout for publish-ready's ssh writes
// and git-push. The zerops.yaml is already the origin's; only the import
// file is new.
func poolPartComposeReadyResponse(
	ctx context.Context,
	bundle *ops.ExportBundle,
	source *poolPartSource,
	meta *workflow.ServiceMeta,
	opts workflow.ExportEnvelopeOpts,
	corpus []workflow.KnowledgeAtom,
) *mcp.CallToolResult {
	const importFile = "zerops-project-import.yaml"
	guidance, err := renderExportStatusGuidance(ctx, bundle.TargetHostname, topology.ExportStatusComposeReady, opts, corpus)
	if err != nil {
		return convertError(err, WithRecoveryStatus())
	}
	pool := meta.Hostname
	origin := topology.RedactRepoURLCredentials(source.RepoURL)
	return jsonResult(map[string]any{
		"status":        "compose-ready",
		"phase":         "export-active",
		"targetService": bundle.TargetHostname,
		"bundle": map[string]any{
			"importYaml":       bundle.ImportYAML,
			"zeropsYaml":       bundle.ZeropsYAML,
			"zeropsYamlSource": bundle.ZeropsYAMLSource,
			"setupName":        bundle.SetupName,
			"repoUrl":          bundle.RepoURL,
			"warnings":         bundle.Warnings,
			"importFile":       importFile,
			"zeropsFile":       "zerops.yaml",
		},
		"source": map[string]any{
			"origin":  origin,
			"ref":     source.Ref,
			"commit":  source.CommitSHA,
			"devPool": pool,
		},
		"guidance": guidance,
		"nextSteps": []string{
			fmt.Sprintf("zerops_lease action=\"acquire\" pool=%q branch=\"<branch>\"", pool),
			fmt.Sprintf("ssh <lease alias> 'cat > /var/www/%s' <<'EOF'\n%s\nEOF", importFile, bundle.ImportYAML),
			fmt.Sprintf("ssh <lease alias> 'cd /var/www && git add %s && git commit -m \"export bundle\"'", importFile),
			fmt.Sprintf("Push the branch from the lease; it reaches %s like any change of the part.", source.Ref),
		},
	})
}

// renderExportStatusGuidance builds the per-status export envelope and
// returns the atom-rendered guidance body. Single-entry Services
// semantics (one snapshot for a known target, empty for scope-prompt)
// per the audit decision in
// internal/workflow/synthesize_export_audit.md. Caller is one of the
// per-status response builders below.
func renderExportStatusGuidance(
	ctx context.Context,
	targetService string,
	status topology.ExportStatus,
	opts workflow.ExportEnvelopeOpts,
	corpus []workflow.KnowledgeAtom,
) (string, error) {
	env, err := workflow.BuildExportEnvelope(ctx, targetService, status, opts)
	if err != nil {
		return "", err
	}
	return workflow.RenderExportGuidance(env, corpus)
}

// scopePromptResponse returns a list of runtime hostnames in the
// project so the agent can pick a TargetService. Phase A.1 entry point
// when the agent calls workflow="export" without targetService. Atom-
// rendered guidance composes export-intro (universal framing) + export-
// scope-prompt (status-specific imperative).
func scopePromptResponse(
	ctx context.Context,
	client platform.Client,
	projectID string,
	opts workflow.ExportEnvelopeOpts,
	corpus []workflow.KnowledgeAtom,
) (*mcp.CallToolResult, any, error) {
	discover, err := ops.Discover(ctx, client, projectID, "", false, false, false)
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	var runtimes []string
	for _, svc := range discover.Services {
		if !svc.IsInfrastructure && !topology.IsPoolOrSandboxType(svc.Type) {
			runtimes = append(runtimes, svc.Hostname)
		}
	}
	sort.Strings(runtimes)
	guidance, err := renderExportStatusGuidance(ctx, "", topology.ExportStatusScopePrompt, opts, corpus)
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	return jsonResult(map[string]any{
		"status":   "scope-prompt",
		"phase":    "export-active",
		"guidance": guidance,
		"runtimes": runtimes,
	}), nil, nil
}

// scaffoldChainResponse fires when /var/www/zerops.yaml is absent or
// empty. Atom-rendered guidance is the scaffold-zerops-yaml body
// (filtered by exportStatus: [scaffold-required]) plus universal export-
// intro. The structural nextSteps array carries the dynamic targetService
// substitution since atoms don't yet template runtime values.
func scaffoldChainResponse(
	ctx context.Context,
	targetService string,
	opts workflow.ExportEnvelopeOpts,
	corpus []workflow.KnowledgeAtom,
) *mcp.CallToolResult {
	guidance, err := renderExportStatusGuidance(ctx, targetService, topology.ExportStatusScaffoldRequired, opts, corpus)
	if err != nil {
		return convertError(err, WithRecoveryStatus())
	}
	return jsonResult(map[string]any{
		"status":        "scaffold-required",
		"phase":         "export-active",
		"targetService": targetService,
		"guidance":      guidance,
		"nextSteps": []string{
			"After scaffolding, re-call: zerops_workflow workflow=\"export\" targetService=\"" + targetService + "\".",
		},
	})
}

// gitPushSetupChainResponse fires for either of two cases: (a) the
// chosen container has no git remote configured, OR (b) ServiceMeta
// records GitPushState != GitPushConfigured. Both require the
// git-push-setup action to land before publish. The bundle (when
// available) is included so the agent can preview the YAMLs while
// the user resolves the prereq. Atom-rendered guidance composes export-
// intro + export-publish-needs-setup; the dynamic `reason` and dynamic
// targetService substitutions stay in the response top-level + nextSteps.
func gitPushSetupChainResponse(
	ctx context.Context,
	targetService string,
	bundle *ops.ExportBundle,
	reason string,
	opts workflow.ExportEnvelopeOpts,
	corpus []workflow.KnowledgeAtom,
) *mcp.CallToolResult {
	guidance, err := renderExportStatusGuidance(ctx, targetService, topology.ExportStatusGitPushSetupRequired, opts, corpus)
	if err != nil {
		return convertError(err, WithRecoveryStatus())
	}
	body := map[string]any{
		"status":        "git-push-setup-required",
		"phase":         "export-active",
		"targetService": targetService,
		"reason":        reason,
		"guidance":      guidance,
		"nextSteps": []string{
			fmt.Sprintf("Run zerops_workflow action=\"git-push-setup\" service=%q remoteUrl=<URL>", targetService),
			fmt.Sprintf("After confirm, re-call: zerops_workflow workflow=\"export\" targetService=%q", targetService),
		},
	}
	if bundle != nil {
		body["preview"] = bundlePreview(bundle)
	}
	return jsonResult(body)
}

// classifyPromptResponse returns the per-env review table for the agent
// to classify (or accept) before publish. Per plan §14.2 amendment 2
// (Phase 3 clarification): the handler MUST redact unclassified env
// values in the agent-facing preview. The rendered import.yaml is
// withheld — values appear in the rendered body when classifications
// are incomplete, so the body itself is the leak vector. The agent
// re-fetches values via zerops_discover if it needs them for grep-
// driven classification.
//
// Rows carry server-side `suggestedBucket` + `rationale` derived from
// the env key NAME only (envclass + topology.IsClassifyInfrastructure);
// the value never enters that computation, preserving the no-leak
// invariant pinned by TestHandleExport_ClassifyPromptDoesNotLeakValues.
// SYSTEM envs are filtered upstream so the agent only sees keys that
// genuinely require its judgment.
//
// The live zerops.yaml body and the bundle warnings are still passed
// through — zerops.yaml is the upstream source the agent already has
// SSH access to (no incremental leak), and warnings carry only env
// names + structural notes (no raw values).
func classifyPromptResponse(
	ctx context.Context,
	bundle *ops.ExportBundle,
	envs []platform.ProjectEnvVar,
	classifications map[string]topology.SecretClassification,
	opts workflow.ExportEnvelopeOpts,
	corpus []workflow.KnowledgeAtom,
) *mcp.CallToolResult {
	userEnvs := envsForClassifyPrompt(envs)
	rows := make([]map[string]any, 0, len(userEnvs))
	for _, env := range userEnvs {
		bucket := classifications[env.Key]
		suggested, rationale := suggestBucketForKey(env)
		row := map[string]any{
			"key":             env.Key,
			"currentBucket":   bucket,
			"suggestedBucket": suggested,
			"rationale":       rationale,
		}
		if bucket != topology.SecretClassUnset {
			row["classified"] = true
		}
		rows = append(rows, row)
	}
	guidance, err := renderExportStatusGuidance(ctx, bundle.TargetHostname, topology.ExportStatusClassifyPrompt, opts, corpus)
	if err != nil {
		return convertError(err, WithRecoveryStatus())
	}
	return jsonResult(map[string]any{
		"status":                 "classify-prompt",
		"phase":                  "export-active",
		"targetService":          bundle.TargetHostname,
		"zeropsYaml":             bundle.ZeropsYAML,
		"warnings":               bundle.Warnings,
		"envClassificationTable": rows,
		"guidance":               guidance,
		"fetchValuesVia":         fmt.Sprintf("zerops_discover service=%q includeEnvs=true includeEnvValues=true", bundle.TargetHostname),
		"nextSteps": []string{
			fmt.Sprintf("Re-call: zerops_workflow workflow=\"export\" targetService=%q", bundle.TargetHostname),
			"        envClassifications={key:bucket,...}",
		},
	})
}

// validationFailedResponse blocks publish when Phase 5 schema validation
// surfaces blocking errors. The agent inspects `errors` (path + message
// per failure) and either re-classifies envs (e.g., reclassify a
// dropped infrastructure env to plain-config), edits the live
// zerops.yaml (e.g., add a missing required field), or scaffolds when
// the body is structurally absent. Re-call export with the same inputs
// after fixing — if the validators clear, the response moves to
// publish-ready.
func validationFailedResponse(
	ctx context.Context,
	bundle *ops.ExportBundle,
	opts workflow.ExportEnvelopeOpts,
	corpus []workflow.KnowledgeAtom,
) *mcp.CallToolResult {
	guidance, err := renderExportStatusGuidance(ctx, bundle.TargetHostname, topology.ExportStatusValidationFailed, opts, corpus)
	if err != nil {
		return convertError(err, WithRecoveryStatus())
	}
	return jsonResult(map[string]any{
		"status":        "validation-failed",
		"phase":         "export-active",
		"targetService": bundle.TargetHostname,
		"errors":        formatBundleErrors(bundle.Errors),
		"preview":       bundlePreview(bundle),
		"guidance":      guidance,
		"nextSteps": []string{
			"Fix each validation error at its source.",
			fmt.Sprintf("Re-call: zerops_workflow workflow=\"export\" targetService=%q", bundle.TargetHostname),
			"        envClassifications=<your same map>",
		},
	})
}

// publishGuidanceResponse is the Phase C success body: bundle ready,
// agent executes the SSH writes + commit + zerops_deploy.
func publishGuidanceResponse(
	ctx context.Context,
	bundle *ops.ExportBundle,
	opts workflow.ExportEnvelopeOpts,
	corpus []workflow.KnowledgeAtom,
) *mcp.CallToolResult {
	const importFile = "zerops-project-import.yaml"
	const repoRoot = "/var/www"

	guidance, err := renderExportStatusGuidance(ctx, bundle.TargetHostname, topology.ExportStatusPublishReady, opts, corpus)
	if err != nil {
		return convertError(err, WithRecoveryStatus())
	}
	return jsonResult(map[string]any{
		"status":        "publish-ready",
		"phase":         "export-active",
		"targetService": bundle.TargetHostname,
		"bundle": map[string]any{
			"importYaml": bundle.ImportYAML,
			"zeropsYaml": bundle.ZeropsYAML,
			"setupName":  bundle.SetupName,
			"repoUrl":    bundle.RepoURL,
			"warnings":   bundle.Warnings,
			"importFile": importFile,
			"zeropsFile": "zerops.yaml",
		},
		"guidance": guidance,
		"nextSteps": []string{
			fmt.Sprintf("ssh %s 'cat > %s/%s' <<'EOF'\n%s\nEOF", bundle.TargetHostname, repoRoot, importFile, bundle.ImportYAML),
			fmt.Sprintf("ssh %s 'cat > %s/zerops.yaml' <<'EOF'\n%s\nEOF", bundle.TargetHostname, repoRoot, bundle.ZeropsYAML),
			fmt.Sprintf("ssh %s 'cd %s && git add -A && git commit -m \"export bundle\"'", bundle.TargetHostname, repoRoot),
			fmt.Sprintf("zerops_deploy targetService=%q strategy=\"git-push\"", bundle.TargetHostname),
		},
	})
}

// composeReadyResponse is the deliverable terminal (R5/R7): the bundle is
// composed, classifications accepted, and schema-clean, but git-push is NOT
// configured. It hands back the importYaml + zeropsYaml as the deliverable —
// decoupling generation from publication — and offers publishing via git-push
// as an OPTIONAL follow-on. The earlier flow blocked here with
// git-push-setup-required, withholding a finished bundle from a user who just
// hadn't set up git-push (the common case).
func composeReadyResponse(
	ctx context.Context,
	bundle *ops.ExportBundle,
	opts workflow.ExportEnvelopeOpts,
	corpus []workflow.KnowledgeAtom,
) *mcp.CallToolResult {
	const importFile = "zerops-project-import.yaml"
	guidance, err := renderExportStatusGuidance(ctx, bundle.TargetHostname, topology.ExportStatusComposeReady, opts, corpus)
	if err != nil {
		return convertError(err, WithRecoveryStatus())
	}
	if strings.TrimSpace(guidance) == "" {
		guidance = "The export bundle is composed and schema-valid — the importYaml + zeropsYaml below are the deliverable. Write them into the repo and commit. Publishing the repo to Zerops via git-push is OPTIONAL: configure it with git-push-setup, then re-call export for the push instructions."
	}
	return jsonResult(map[string]any{
		"status":        "compose-ready",
		"phase":         "export-active",
		"targetService": bundle.TargetHostname,
		"bundle": map[string]any{
			"importYaml": bundle.ImportYAML,
			"zeropsYaml": bundle.ZeropsYAML,
			"setupName":  bundle.SetupName,
			"repoUrl":    bundle.RepoURL,
			"warnings":   bundle.Warnings,
			"importFile": importFile,
			"zeropsFile": "zerops.yaml",
		},
		"guidance": guidance,
		"nextSteps": []string{
			fmt.Sprintf("Write %s + zerops.yaml into the repo and commit.", importFile),
			"OPTIONAL — to publish via git-push:",
			fmt.Sprintf("  zerops_workflow action=\"git-push-setup\" service=%q remoteUrl=<URL>", bundle.TargetHostname),
			fmt.Sprintf("  then re-call export targetService=%q (→ publish-ready)", bundle.TargetHostname),
		},
	})
}

// bundlePreview is the agent-facing summary of an ExportBundle —
// includes the YAML bodies but trims internal fields like
// Classifications (which the agent already supplied).
//
// Phase 5 lands schema validation; Errors propagates through the
// preview so agents can surface blocking failures alongside the
// rendered YAMLs without re-running validation in the handler.
func bundlePreview(bundle *ops.ExportBundle) map[string]any {
	preview := map[string]any{
		"importYaml":       bundle.ImportYAML,
		"zeropsYaml":       bundle.ZeropsYAML,
		"zeropsYamlSource": bundle.ZeropsYAMLSource,
		"setupName":        bundle.SetupName,
		"repoUrl":          bundle.RepoURL,
		"warnings":         bundle.Warnings,
	}
	if len(bundle.Errors) > 0 {
		preview["errors"] = formatBundleErrors(bundle.Errors)
	}
	return preview
}

// formatBundleErrors renders schema.ValidationError slices into the
// agent-facing JSON shape. Each entry carries `path` (JSON pointer to
// the failing field) + `message` (schema validator output). Empty
// path means "root-level" error (parse failure, missing required
// section, schema-compile failure).
func formatBundleErrors(errs []schema.ValidationError) []map[string]any {
	out := make([]map[string]any, 0, len(errs))
	for _, e := range errs {
		out = append(out, map[string]any{
			"path":    e.Path,
			"message": e.Message,
		})
	}
	return out
}

// convertClassificationsInput coerces the JSON-input string map into
// the typed topology.SecretClassification map BuildBundle expects.
// Unknown values pass through — the composer surfaces a warning when
// the bucket is unrecognized.
func convertClassificationsInput(in map[string]string) map[string]topology.SecretClassification {
	out := make(map[string]topology.SecretClassification, len(in))
	for k, v := range in {
		out[k] = topology.SecretClassification(v)
	}
	return out
}

// refuseNonRuntimeKind returns a named refusal when the service is a Swarm
// pool or a Zerops Box — neither is an app runtime, and a file read by a
// pool's service hostname lands on a random one of its containers
// (spec-workflows.md §8 PS-3). verb names the workflow in the message
// ("export", "launch"). Nil for every other service type.
func refuseNonRuntimeKind(hostname, serviceType, verb string) error {
	switch {
	case topology.IsPoolType(serviceType):
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Service %q is a Swarm pool (%s) — a pool is not the app's runtime, so %s cannot use it as the source (reading its files by hostname would reach a random container)", hostname, serviceType, verb),
			"Pass the repo-backed app runtime hostname instead. A pool is infrastructure the app uses; recreate it in the new project's import yourself.")
	case topology.IsSandboxType(serviceType):
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Service %q is a Zerops Box (%s) — a Box hosts sandboxes and is not the app's runtime, so %s cannot use it as the source", hostname, serviceType, verb),
			"Pass the repo-backed app runtime hostname instead. A Box is infrastructure the app uses; recreate it in the new project's import yourself.")
	}
	return nil
}
