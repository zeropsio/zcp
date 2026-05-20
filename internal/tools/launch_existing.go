package tools

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// existingProdTokenClientFactory constructs the platform.Client used by
// the existing-project launch path. Token is the user-supplied
// project-scoped Zerops token (WorkflowInput.ExistingProdToken).
//
// Production default: wraps platform.NewZeropsClient. Tests inject a
// mock via setExistingProdTokenClientFactory (defer cleanup).
//
// P-LP-1 invariant: the token is held inside the SDK's authenticated
// transport, never copied to a separately-addressable field on the
// returned client.
//
//nolint:gochecknoglobals // test-injection point for the existing-project surface
var existingProdTokenClientFactory = func(token, apiHost string) (platform.Client, error) {
	c, err := platform.NewZeropsClient(token, apiHost)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// setExistingProdTokenClientFactory swaps the factory for tests.
// Restore with the returned cleanup func via defer.
func setExistingProdTokenClientFactory(f func(token, apiHost string) (platform.Client, error)) func() {
	prev := existingProdTokenClientFactory
	existingProdTokenClientFactory = f
	return func() { existingProdTokenClientFactory = prev }
}

// validateExistingProdTokenScope confirms that a project-scoped token
// authenticates AND resolves to exactly one project AND that project's
// ID matches the user-supplied expectedProjectID. Returns nil on
// success; returns a structured PlatformError on every failure mode:
//
//   - GetUserInfo failure → wraps with ErrAuthRequired equivalent
//   - ListProjects returns 0 → ErrTokenNoProject
//   - ListProjects returns >1 → ErrTokenMultiProject
//   - ListProjects returns 1, mismatched ID → ErrTokenScopeMismatch
//
// Caller wraps the response via convertError so the LLM sees a
// recovery-friendly status.
func validateExistingProdTokenScope(ctx context.Context, c platform.Client, expectedProjectID string) error {
	info, err := c.GetUserInfo(ctx)
	if err != nil {
		return fmt.Errorf("existing-project token: validate user info: %w", err)
	}
	if info == nil || info.ID == "" {
		return platform.NewPlatformError(
			platform.ErrAuthRequired,
			"Existing-project token did not resolve to a Zerops client; cannot continue",
			"Regenerate the project-scoped token on the target project's dashboard.",
		)
	}
	projects, err := c.ListProjects(ctx, info.ID)
	if err != nil {
		return fmt.Errorf("existing-project token: list projects: %w", err)
	}
	switch len(projects) {
	case 0:
		return platform.NewPlatformError(
			platform.ErrTokenNoProject,
			"Existing-project token has no project access; cannot import into target",
			"Regenerate a project-scoped token on the target project's dashboard (Settings → Access Tokens Management).",
		)
	case 1:
		if projects[0].ID != expectedProjectID {
			return platform.NewPlatformError(
				platform.ErrTokenScopeMismatch,
				fmt.Sprintf("Existing-project token scoped to project %q but workflow expects %q",
					projects[0].ID, expectedProjectID),
				"Regenerate the project-scoped token on the EXPECTED target project's dashboard (Settings → Access Tokens Management). The current token's project does not match existingProjectId.",
			)
		}
		return nil
	default:
		return platform.NewPlatformError(
			platform.ErrTokenMultiProject,
			fmt.Sprintf("Existing-project token resolves to %d projects; ZCP requires single-project scope", len(projects)),
			"Regenerate the project-scoped token on the target project's dashboard (Settings → Access Tokens Management). Account-wide tokens are not accepted for the existing-project import path.",
		)
	}
}

// detectHostnameConflicts returns the subset of about-to-import
// hostnames that already exist as services in the target project.
// Comparison is case-sensitive (platform treats hostnames as
// case-sensitive identifiers). Order preserved from `incoming` for
// deterministic test output.
//
// platform.ServiceStack.Name carries the hostname per ServiceStack
// struct docs (line 35: `Name string `json:"name"` // hostname`).
func detectHostnameConflicts(existing []platform.ServiceStack, incoming []string) []string {
	if len(existing) == 0 || len(incoming) == 0 {
		return nil
	}
	taken := make(map[string]bool, len(existing))
	for _, s := range existing {
		taken[s.Name] = true
	}
	var conflicts []string
	for _, h := range incoming {
		if taken[h] {
			conflicts = append(conflicts, h)
		}
	}
	return conflicts
}

// importHostnamesFromInputs returns the hostnames the launch bundle
// would create on the target. Used by the hostname-conflict preflight
// to compare against the existing service list. Order matches the
// composer's services array order (runtime first, then managed deps).
func importHostnamesFromInputs(inputs ops.LaunchBundleInputs) []string {
	hostnames := make([]string, 0, 1+len(inputs.ManagedServices))
	hostnames = append(hostnames, inputs.TargetHostname)
	for _, m := range inputs.ManagedServices {
		hostnames = append(hostnames, m.Hostname)
	}
	return hostnames
}

// executeExistingProjectMutation runs the launch-production existing-
// project mutation path per plan §6.6:
//
//  1. Construct project-scoped client from ExistingProdToken.
//  2. Validate token scope (single project + matches ExistingProjectID).
//  3. P-LP-3 source snapshot compare (re-uses the same gate as the
//     new-project path; baseline persisted at ready-to-launch).
//  4. Hostname conflict preflight against target's current services.
//  5. CreateProjectEnv per classified USER-scope env.
//  6. ImportServices with services-only yaml (VariantLaunchExisting).
//  7. Audit log entry; emit launched response.
//
// On any structured failure between step 1 and step 4, no mutation
// occurs (read-only checks). Mutation begins at step 5 — partial-
// success states (some envs created, some failed; envs created but
// ImportServices failed) surface via the audit log + last-error
// fields. Resume idempotency is deferred to Phase 4b state promotion.
func executeExistingProjectMutation(
	ctx context.Context,
	sourceProjectID string,
	sourceClient platform.Client,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	input WorkflowInput,
	sourceEnvs []platform.ProjectEnvVar,
	classifications map[string]topology.SecretClassification,
	corpus []workflow.KnowledgeAtom,
	stateDir string,
	launchID string,
) (*mcp.CallToolResult, any, error) {
	// 1. Construct project-scoped client from the user-supplied token.
	target, err := existingProdTokenClientFactory(input.ExistingProdToken, "")
	if err != nil {
		return launchFailedAuthResponse(corpus, err), nil, nil
	}

	// 2. Token scope validation. Errors return structured codes that
	// the convertError boundary surfaces with recovery hints.
	if scopeErr := validateExistingProdTokenScope(ctx, target, input.ExistingProjectID); scopeErr != nil {
		_ = appendAuditLog(stateDir, launchAuditEntry{
			LaunchID:          launchID,
			Action:            "publish-rejected",
			SourceProjectID:   sourceProjectID,
			TargetProjectName: input.ProductionProjectName,
			Result:            "failure",
			ErrorMessage:      "existing-project token scope validation: " + scopeErr.Error(),
		})
		return convertError(scopeErr, WithRecoveryStatus()), nil, nil
	}

	// 3. Read + validate source state, compute the bundle (carries
	// SourceSnapshot for P-LP-3 compare). Source reads use the
	// source-project client (sourceClient), not the target client. This
	// is the existing-project mutation path's hard-read — failures land
	// in launch-audit-log.json (writeAudit=true).
	source, blocker := readAndValidateSourceState(ctx, sourceClient, sshDeployer, rt, corpus, input, sourceProjectID, stateDir, launchID, true)
	if blocker != nil {
		return blocker, nil, nil
	}

	// Publish-side source-control gate (P-LP-10 hard re-check) — shared
	// helper with executeLaunchMutation.
	gateResult := runPublishSideSourceControlGate(
		ctx, corpus, sourceClient, sshDeployer, rt, input,
		sourceProjectID, stateDir, launchID, source.RepoURL,
	)
	if gateResult.Response != nil {
		return gateResult.Response, nil, nil
	}

	bundleInputs := ops.LaunchBundleInputs{
		SourceProjectID:   sourceProjectID,
		TargetProjectName: input.ProductionProjectName,
		TargetHostname:    input.TargetService,
		ServiceType:       source.ServiceType,
		SetupName:         effectiveProdSetupName(input),
		RepoURL:           gateResult.RepoURL,
		ZeropsYAMLBody:    source.ZeropsYAMLBody,
		GitCommitSHA:      source.GitCommitSHA,
		ProjectEnvs:       bundleProjectEnvsFromSource(sourceEnvs),
		ManagedServices:   source.ManagedServices,
		KeepNonHA:         input.KeepNonHA,
		Variant:           bundle.VariantLaunchExisting,
	}
	launchBundle, err := ops.BuildLaunchBundle(bundleInputs, classifications)
	if err != nil {
		_ = appendAuditLog(stateDir, launchAuditEntry{
			LaunchID:          launchID,
			Action:            "publish-rejected",
			SourceProjectID:   sourceProjectID,
			TargetProjectName: input.ProductionProjectName,
			Result:            "failure",
			ErrorMessage:      "bundle compose: " + err.Error(),
		})
		// Graceful refusal — bundle-compose error is packaged into the
		// MCP tool response (third return is success at the boundary
		// so the structured payload reaches the client). Mirrors the
		// new-project path's executeLaunchMutation pattern.
		//nolint:nilerr // err is surfaced via the structured response
		return launchFailedResponse(corpus, topology.BlockerCategoryOther,
			"bundle-compose-failed",
			"Launch bundle composition failed: "+err.Error()), nil, nil
	}
	if len(launchBundle.Errors) > 0 {
		_ = appendAuditLog(stateDir, launchAuditEntry{
			LaunchID:          launchID,
			Action:            "publish-rejected",
			SourceProjectID:   sourceProjectID,
			TargetProjectName: input.ProductionProjectName,
			Result:            "failure",
			ErrorMessage:      "schema validation failed",
		})
		return launchFailedResponse(corpus, topology.BlockerCategorySchema,
			"schema-validation-failed",
			fmt.Sprintf("Import yaml schema validation failed: %v", launchBundle.Errors)), nil, nil
	}

	// 4. Hostname conflict preflight against target's current services.
	// Routed through ops.ListProjectServices to satisfy the
	// architecture invariant (CLAUDE.md "tools/eval reach platform
	// via ops" — single cache/retry/instrumentation site).
	existingServices, err := ops.ListProjectServices(ctx, target, input.ExistingProjectID)
	if err != nil {
		return convertError(fmt.Errorf("existing-project preflight: list services: %w", err), WithRecoveryStatus()), nil, nil
	}
	if conflicts := detectHostnameConflicts(existingServices, importHostnamesFromInputs(bundleInputs)); len(conflicts) > 0 {
		_ = appendAuditLog(stateDir, launchAuditEntry{
			LaunchID:          launchID,
			Action:            "publish-rejected",
			SourceProjectID:   sourceProjectID,
			TargetProjectName: input.ProductionProjectName,
			Result:            "failure",
			ErrorMessage:      "hostname conflicts: " + strings.Join(conflicts, ","),
		})
		return launchFailedResponse(corpus, topology.BlockerCategoryOther,
			"hostname-conflict",
			fmt.Sprintf("Target project already has services with these hostnames: %s. Rename source services or delete the target collisions before retrying.",
				strings.Join(conflicts, ", "))), nil, nil
	}

	// Pre-mutation state persistence — same shape as new-project path
	// so resume primitives behave identically. ExistingProjectID lives
	// on the state file from the moment the mutation starts.
	state := &launchState{
		LaunchID:              launchID,
		SourceProjectID:       sourceProjectID,
		SourceRepoURL:         source.RepoURL,
		TargetProjectName:     input.ProductionProjectName,
		TargetProjectID:       input.ExistingProjectID,
		TargetServiceHostname: input.TargetService,
		SourceSnapshot:        launchBundle.SourceSnapshot,
		Classifications:       classifications,
		Status:                topology.LaunchStatusLaunching,
	}
	if writeErr := writeLaunchState(stateDir, state); writeErr != nil {
		launchBundle.Warnings = append(launchBundle.Warnings,
			fmt.Sprintf("write launch state: %v (proceeding; resume after restart may not work)", writeErr))
	}

	// 5. Per-env mutation — apply classifications to composer envs and
	// drive CreateProjectEnv per emission. Extracted to keep this
	// function under the maintainability-index ceiling.
	composerEnvs := bundleProjectEnvsFromSource(sourceEnvs)
	emitWarnings, mutationResp := mutateProjectEnvs(
		ctx, target, stateDir, launchID, sourceProjectID, input, composerEnvs, classifications,
	)
	if mutationResp != nil {
		return mutationResp, nil, nil
	}
	launchBundle.Warnings = append(launchBundle.Warnings, emitWarnings...)

	// 6. Services-only import. VariantLaunchExisting omits the project
	// block; the API rejects yaml with project: blocks on this endpoint.
	importResult, err := target.ImportServices(ctx, input.ExistingProjectID, launchBundle.ImportYAML)
	if err != nil {
		_ = appendAuditLog(stateDir, launchAuditEntry{
			LaunchID:          launchID,
			Action:            "import-services-failed",
			SourceProjectID:   sourceProjectID,
			TargetProjectName: input.ProductionProjectName,
			Result:            "failure",
			ErrorMessage:      fmt.Sprintf("ImportServices: %v", err),
		})
		return convertError(fmt.Errorf("existing-project services import: %w", err), WithRecoveryStatus()), nil, nil
	}
	if importResult == nil {
		return launchFailedResponse(corpus, topology.BlockerCategoryOther,
			"import-services-empty",
			"Existing-project import returned nil result — platform import failed silently."), nil, nil
	}

	// 7. Persist launched state + audit log. ExistingProjectID is the
	// TargetProjectID for the audit trail.
	importedServices := make([]importedServiceEntry, 0, len(importResult.ServiceStacks))
	for _, svc := range importResult.ServiceStacks {
		entry := importedServiceEntry{
			ID:   svc.ID,
			Name: svc.Name,
		}
		for _, p := range svc.Processes {
			entry.ProcessIDs = append(entry.ProcessIDs, p.ID)
		}
		if svc.Error != nil {
			entry.ImportError = svc.Error.Message
		}
		importedServices = append(importedServices, entry)
	}
	state.Status = topology.LaunchStatusLaunched
	state.ImportedServices = importedServices
	if writeErr := writeLaunchState(stateDir, state); writeErr != nil {
		launchBundle.Warnings = append(launchBundle.Warnings,
			fmt.Sprintf("write launched state: %v", writeErr))
	}

	_ = appendAuditLog(stateDir, launchAuditEntry{
		LaunchID:          launchID,
		Action:            "publish-success",
		SourceProjectID:   sourceProjectID,
		TargetProjectName: input.ProductionProjectName,
		TargetProjectID:   input.ExistingProjectID,
		Result:            "success",
	})

	return launchLaunchedResponse(corpus, state), nil, nil
}

// projectEnvEmission is one (Key, Value, Sensitive) tuple that the
// existing-project mutation path feeds to CreateProjectEnv. Output of
// applyClassificationToProjectEnvs; intentionally distinct from
// platform.ProjectEnvVar so the per-env-API path doesn't accidentally
// carry the source-side `Content` field through unmodified.
type projectEnvEmission struct {
	Key       string
	Value     string
	Sensitive bool
}

// mutateProjectEnvs runs step 5 of executeExistingProjectMutation:
// apply classifications to the composer envs, then drive
// CreateProjectEnv for each emission. Returns (warnings, nil) on
// success and (nil, response) on any failure — caller surfaces the
// response to the MCP boundary and aborts. Extracted from
// executeExistingProjectMutation so that function stays under the
// maintainability-index ceiling; mirrors mutate* helper shapes
// elsewhere in this package.
//
// applyClassificationToProjectEnvs handles the bucket semantics
// (Infrastructure → drop, AutoSecret → fresh random, ExternalSecret →
// REPLACE_ME, PlainConfig → verbatim). The transform is necessary
// because CreateProjectEnv bypasses the platform preprocessor — the
// new-project path's `<@generateRandomString(<32>)>` directive in
// project.envVariables would land here as a literal string. Without
// the transform, dev/stage secrets leaked verbatim to prod.
func mutateProjectEnvs(
	ctx context.Context,
	target platform.Client,
	stateDir string,
	launchID string,
	sourceProjectID string,
	input WorkflowInput,
	composerEnvs []bundle.ProjectEnvVar,
	classifications map[string]topology.SecretClassification,
) ([]string, *mcp.CallToolResult) {
	emissions, emissionWarnings, applyErr := applyClassificationToProjectEnvs(composerEnvs, classifications)
	if applyErr != nil {
		_ = appendAuditLog(stateDir, launchAuditEntry{
			LaunchID:          launchID,
			Action:            "apply-classification-failed",
			SourceProjectID:   sourceProjectID,
			TargetProjectName: input.ProductionProjectName,
			Result:            "failure",
			ErrorMessage:      applyErr.Error(),
		})
		return nil, convertError(fmt.Errorf("existing-project env classification: %w", applyErr), WithRecoveryStatus())
	}

	// FIX 1 PR 3 — preflight existing envs so a partial-success retry
	// doesn't hit `projectEnvDuplicateKey` from the platform side.
	// Eval evidence (launch-to-existing-prod-project 20260517-185653):
	// first call created SESSION_SECRET etc.; second call hit dup-key on
	// the same env; agent deleted services thinking they were stale, but
	// project-level envs persist independently of service deletion, so
	// the retry loop continued failing. Pre-reading the live env set and
	// skipping already-present keys turns the mutation idempotent.
	existing, existingErr := target.GetProjectEnv(ctx, input.ExistingProjectID)
	existingKeys := make(map[string]bool, len(existing))
	if existingErr == nil {
		for _, e := range existing {
			existingKeys[e.Key] = true
		}
	}
	// Failure to read existing envs degrades gracefully — the mutation
	// proceeds with no skip set; CreateProjectEnv will surface
	// projectEnvDuplicateKey as before. We surface a warning so the
	// operator knows preflight didn't run.
	if existingErr != nil {
		emissionWarnings = append(emissionWarnings,
			fmt.Sprintf("preflight GetProjectEnv failed (%v) — idempotency check skipped; dup-key errors will surface from the platform", existingErr))
	}

	for _, e := range emissions {
		if existingKeys[e.Key] {
			// Already present in the target — skip to keep the mutation
			// idempotent. Surfaces in warnings so the operator can audit
			// whether the existing value matches their intent. Note: we
			// do NOT compare values (CreateProjectEnv returns sensitive
			// values as a discriminator placeholder, not the literal),
			// so the warning emphasizes "verify don't overwrite".
			emissionWarnings = append(emissionWarnings,
				fmt.Sprintf("env %q already exists in target project — skipped to avoid duplicate-key; verify the existing value in Zerops dashboard matches intent", e.Key))
			continue
		}
		if _, envErr := target.CreateProjectEnv(ctx, input.ExistingProjectID, e.Key, e.Value, e.Sensitive); envErr != nil {
			_ = appendAuditLog(stateDir, launchAuditEntry{
				LaunchID:          launchID,
				Action:            "create-project-env-failed",
				SourceProjectID:   sourceProjectID,
				TargetProjectName: input.ProductionProjectName,
				Result:            "failure",
				ErrorMessage:      fmt.Sprintf("CreateProjectEnv %s: %v", e.Key, envErr),
			})
			return nil, convertError(fmt.Errorf("existing-project env mutation %s: %w", e.Key, envErr), WithRecoveryStatus())
		}
	}
	return emissionWarnings, nil
}

// applyClassificationToProjectEnvs walks composer-supplied envs and
// produces the CreateProjectEnv-shaped emissions for the existing-
// project mutation path. Mirrors composeProjectEnvVariables (which
// emits preprocessor directives into the project.envVariables yaml
// block for the new-project path) but with one critical difference:
// CreateProjectEnv bypasses the platform preprocessor, so the
// auto-secret directive `<@generateRandomString(<32>)>` would land as
// a literal string. Auto-secret values are therefore generated in-tool
// from crypto/rand.
//
// Bucket semantics:
//   - Infrastructure → entry dropped (managed services regenerate at
//     re-import; the ${db_*} ref still resolves against the target's
//     own managed service).
//   - AutoSecret     → fresh 32-char base64-url-safe random; entry
//     emitted with Sensitive=true.
//   - ExternalSecret → bundle.ExternalSecretPlaceholder literal;
//     Sensitive=true; warning instructs the operator to replace in
//     dashboard before runtime depends on it.
//   - PlainConfig    → source value verbatim; Sensitive=false (the
//     "literal copy" bucket is opt-in non-sensitive by design).
//   - Unset / unknown → source value verbatim + warning.
//
// Returns the emissions, the per-env warnings, and an error if random
// generation itself fails (crypto/rand exhaustion — practically
// unreachable, but propagated rather than swallowed).
func applyClassificationToProjectEnvs(
	envs []bundle.ProjectEnvVar,
	classifications map[string]topology.SecretClassification,
) ([]projectEnvEmission, []string, error) {
	out := make([]projectEnvEmission, 0, len(envs))
	var warnings []string
	for _, env := range envs {
		bucket := classifications[env.Key]
		switch bucket {
		case topology.SecretClassInfrastructure:
			continue
		case topology.SecretClassAutoSecret:
			v, err := generateAutoSecretValue()
			if err != nil {
				return nil, warnings, fmt.Errorf("env %s: %w", env.Key, err)
			}
			out = append(out, projectEnvEmission{Key: env.Key, Value: v, Sensitive: true})
		case topology.SecretClassExternalSecret:
			out = append(out, projectEnvEmission{
				Key:       env.Key,
				Value:     bundle.ExternalSecretPlaceholder,
				Sensitive: true,
			})
			warnings = append(warnings, fmt.Sprintf(
				"env %q: external-secret bucket — emitted as %q on the target; replace in Zerops dashboard (or via `zerops_env action=set`) before the runtime depends on it",
				env.Key, bundle.ExternalSecretPlaceholder))
		case topology.SecretClassPlainConfig:
			out = append(out, projectEnvEmission{Key: env.Key, Value: env.Value, Sensitive: false})
		case topology.SecretClassUnset:
			out = append(out, projectEnvEmission{Key: env.Key, Value: env.Value, Sensitive: false})
			warnings = append(warnings, fmt.Sprintf(
				"env %q: not classified — emitted as plain-config; classify before publish", env.Key))
		default:
			out = append(out, projectEnvEmission{Key: env.Key, Value: env.Value, Sensitive: false})
			warnings = append(warnings, fmt.Sprintf(
				"env %q: unknown classification %q — emitted as plain-config", env.Key, bucket))
		}
	}
	return out, warnings, nil
}

// generateAutoSecretValue returns a fresh cryptographically random
// 32-character secret using base64 URL-safe encoding without padding
// (24 random bytes → 32 chars). Matches the platform preprocessor's
// generateRandomString(<32>) visible length so AutoSecret values look
// the same regardless of whether the new-project (preprocessor) or
// existing-project (CreateProjectEnv) mutation path produced them.
func generateAutoSecretValue() (string, error) {
	const rawBytes = 24
	b := make([]byte, rawBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate auto-secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ensureNoExistingProdTokenInState is a defensive helper that traces
// uses of input.ExistingProdToken inside any string that could end up
// in state or response. Currently a no-op stub the AST sentinel (Phase
// 5) will replace with a serialization-fixture check. Marked _ to
// avoid unused-variable warnings; will gain a body when Phase 5 lands.
var _ = errors.New
