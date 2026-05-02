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
// on WorkflowInput (TargetService, Variant, EnvClassifications).
//
//   - Phase A — probe + variant choice. When TargetService is empty, the
//     handler returns a scope-prompt listing project runtimes. When the
//     chosen runtime is part of a pair (ModeStandard / ModeLocalStage)
//     and Variant is unset, the handler returns a variant-prompt.
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
	discover, err := ops.Discover(ctx, client, projectID, "", false, false)
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
	sourceMode := meta.Mode

	variant, prompt := resolveExportVariant(ctx, input, sourceMode, envOpts, corpus)
	if prompt != nil {
		return prompt, nil, nil
	}

	if sshDeployer == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"SSH access unavailable — export Phase A reads /var/www/zerops.yaml + git remote from the chosen container",
			"Run zcp from a Zerops container (recommended) or configure local SSH credentials before invoking export."), WithRecoveryStatus()), nil, nil
	}

	zeropsYAMLBody, err := readZeropsYAMLBody(ctx, sshDeployer, input.TargetService)
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	if strings.TrimSpace(zeropsYAMLBody) == "" {
		return scaffoldChainResponse(ctx, input.TargetService, envOpts, corpus), nil, nil
	}

	repoURL, err := readGitRemoteURL(ctx, sshDeployer, input.TargetService)
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	if strings.TrimSpace(repoURL) == "" {
		return gitPushSetupChainResponse(ctx, input.TargetService, nil, "no git remote configured in /var/www", envOpts, corpus), nil, nil
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
			input.TargetService, refreshErr))
	}

	setupName, err := pickSetupName(zeropsYAMLBody, input.TargetService, sourceMode)
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}

	projectEnvs, err := readProjectEnvs(ctx, client, projectID)
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}

	managedServices := collectManagedServices(discover, input.TargetService)

	inputs := ops.BundleInputs{
		ProjectName:      discover.Project.Name,
		TargetHostname:   input.TargetService,
		SourceMode:       sourceMode,
		ServiceType:      svc.Type,
		SubdomainEnabled: svc.SubdomainEnabled,
		SetupName:        setupName,
		ZeropsYAMLBody:   zeropsYAMLBody,
		RepoURL:          repoURL,
		ProjectEnvs:      projectEnvs,
		ManagedServices:  managedServices,
	}

	classifications := convertClassificationsInput(input.EnvClassifications)
	bundle, err := ops.BuildBundle(inputs, variant, classifications)
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	bundle.Warnings = append(bundle.Warnings, remoteWarnings...)

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

	if meta.GitPushState != topology.GitPushConfigured {
		return gitPushSetupChainResponse(ctx, input.TargetService, bundle, "GitPushState != configured", envOpts, corpus), nil, nil
	}

	return publishGuidanceResponse(ctx, bundle, envOpts, corpus), nil, nil
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

// resolveExportVariant returns the chosen variant + nil prompt when the
// source mode resolves to a forced single half OR when the agent has
// supplied a Variant. For pair modes (ModeStandard / ModeLocalStage)
// with no Variant supplied, returns the variant-prompt response built
// from the export-variant-prompt atom.
func resolveExportVariant(
	ctx context.Context,
	input WorkflowInput,
	sourceMode topology.Mode,
	opts workflow.ExportEnvelopeOpts,
	corpus []workflow.KnowledgeAtom,
) (topology.ExportVariant, *mcp.CallToolResult) {
	supplied := topology.ExportVariant(input.Variant)

	switch sourceMode {
	case topology.ModeDev, topology.ModeSimple, topology.ModeLocalOnly:
		// Single-half source modes — variant is forced; ignore agent input.
		return topology.ExportVariantUnset, nil
	case topology.ModeStandard, topology.ModeLocalStage:
		// Dev half of a pair — variant is "dev" by default; if agent
		// supplied "stage", that's a mismatch with the chosen hostname.
		if supplied == topology.ExportVariantStage {
			return topology.ExportVariantUnset, convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Variant=stage but targetService=%q is the dev half of the pair", input.TargetService),
				"Either pass the stage hostname as targetService OR set variant=\"dev\". For ModeStandard pairs, the chosen hostname's mode determines the variant."), WithRecoveryStatus())
		}
		if supplied == topology.ExportVariantUnset {
			return topology.ExportVariantUnset, variantPromptResponse(ctx, input.TargetService, sourceMode, opts, corpus)
		}
		return supplied, nil
	case topology.ModeStage:
		// Stage half — variant must be "stage" or unset.
		if supplied == topology.ExportVariantDev {
			return topology.ExportVariantUnset, convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Variant=dev but targetService=%q is the stage half of the pair", input.TargetService),
				"Either pass the dev hostname as targetService OR set variant=\"stage\"."), WithRecoveryStatus())
		}
		if supplied == topology.ExportVariantUnset {
			return topology.ExportVariantUnset, variantPromptResponse(ctx, input.TargetService, sourceMode, opts, corpus)
		}
		return supplied, nil
	default:
		// Single-half modes — variant is forced; ignore agent input.
		return topology.ExportVariantUnset, nil
	}
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
	discover, err := ops.Discover(ctx, client, projectID, "", false, false)
	if err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
	}
	var runtimes []string
	for _, svc := range discover.Services {
		if !svc.IsInfrastructure {
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

// variantPromptResponse asks the agent to pick which half of a pair
// to package. ModeStandard / ModeLocalStage / ModeStage trigger this;
// the chosen hostname's mode resolves the variant on the next call.
// Atom-rendered guidance composes export-intro + export-variant-prompt.
func variantPromptResponse(
	ctx context.Context,
	targetService string,
	sourceMode topology.Mode,
	opts workflow.ExportEnvelopeOpts,
	corpus []workflow.KnowledgeAtom,
) *mcp.CallToolResult {
	guidance, err := renderExportStatusGuidance(ctx, targetService, topology.ExportStatusVariantPrompt, opts, corpus)
	if err != nil {
		return convertError(err, WithRecoveryStatus())
	}
	return jsonResult(map[string]any{
		"status":        "variant-prompt",
		"phase":         "export-active",
		"targetService": targetService,
		"sourceMode":    sourceMode,
		"guidance":      guidance,
		"options":       []topology.ExportVariant{topology.ExportVariantDev, topology.ExportVariantStage},
	})
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
			"After confirmation, re-call: zerops_workflow workflow=\"export\" targetService=\"" + targetService + "\" with the same inputs.",
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
// The live zerops.yaml body and the bundle warnings are still passed
// through — zerops.yaml is the upstream source the agent already has
// SSH access to (no incremental leak), and warnings carry only env
// names + structural notes (no raw values).
func classifyPromptResponse(
	ctx context.Context,
	bundle *ops.ExportBundle,
	envs []ops.ProjectEnvVar,
	classifications map[string]topology.SecretClassification,
	opts workflow.ExportEnvelopeOpts,
	corpus []workflow.KnowledgeAtom,
) *mcp.CallToolResult {
	rows := make([]map[string]any, 0, len(envs))
	for _, env := range envs {
		bucket := classifications[env.Key]
		row := map[string]any{
			"key":           env.Key,
			"currentBucket": bucket,
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
		"variant":                bundle.Variant,
		"zeropsYaml":             bundle.ZeropsYAML,
		"warnings":               bundle.Warnings,
		"envClassificationTable": rows,
		"guidance":               guidance,
		"fetchValuesVia":         fmt.Sprintf("zerops_discover hostname=%q includeEnvs=true includeEnvValues=true", bundle.TargetHostname),
		"nextSteps": []string{
			fmt.Sprintf("Re-call zerops_workflow workflow=\"export\" targetService=%q variant=%q envClassifications={key:bucket,...}", bundle.TargetHostname, bundle.Variant),
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
		"variant":       bundle.Variant,
		"errors":        formatBundleErrors(bundle.Errors),
		"preview":       bundlePreview(bundle),
		"guidance":      guidance,
		"nextSteps": []string{
			"Read each validation error and fix at its source (project envs, zerops.yaml, or service shape).",
			fmt.Sprintf("Re-call zerops_workflow workflow=\"export\" targetService=%q variant=%q envClassifications=<your same map> after fixes.", bundle.TargetHostname, bundle.Variant),
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
		"variant":       bundle.Variant,
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
			fmt.Sprintf("ssh %s 'cat > %s/zerops.yaml' <<'EOF'\n%s\nEOF (skip if zerops.yaml already matches)", bundle.TargetHostname, repoRoot, bundle.ZeropsYAML),
			fmt.Sprintf("ssh %s 'cd %s && git add -A && git commit -m \"export: zerops-project-import.yaml + zerops.yaml for buildFromGit re-import\"'", bundle.TargetHostname, repoRoot),
			fmt.Sprintf("zerops_deploy targetService=%q strategy=\"git-push\"", bundle.TargetHostname),
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

// needsClassifyPrompt returns true when the project has any env that
// has not yet been classified. Walks the projectEnvs slice and checks
// each key against the classifications input — partial maps (some
// envs classified, others missing) still trigger a re-prompt so the
// agent can complete the table per Codex Phase 3 POST-WORK
// amendment 3.
//
// Extra keys in EnvClassifications that don't map to any project env
// do NOT suppress the prompt — they're informational noise that the
// composer simply ignores (no env to apply them to).
func needsClassifyPrompt(envClassifications map[string]string, envs []ops.ProjectEnvVar) bool {
	if len(envs) == 0 {
		return false
	}
	for _, env := range envs {
		if _, ok := envClassifications[env.Key]; !ok {
			return true
		}
	}
	return false
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
