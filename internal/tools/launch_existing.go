package tools

import (
	"context"
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
	// source-project client (sourceClient), not the target client.
	source, blocker := readAndValidateSourceState(ctx, sourceClient, sshDeployer, rt, corpus, input, sourceProjectID, stateDir, launchID)
	if blocker != nil {
		return blocker, nil, nil
	}

	bundleInputs := ops.LaunchBundleInputs{
		SourceProjectID:   sourceProjectID,
		TargetProjectName: input.ProductionProjectName,
		TargetHostname:    input.TargetService,
		ServiceType:       source.ServiceType,
		SetupName:         "prod",
		RepoURL:           source.RepoURL,
		ZeropsYAMLBody:    source.ZeropsYAMLBody,
		GitCommitSHA:      source.GitCommitSHA,
		ProjectEnvs:       launchBundleProjectEnvs(sourceEnvs),
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

	// 5. Per-env mutation. Composer-supplied envs only — Drop-decision
	// envs (envclass SYSTEM) never reach launchBundleProjectEnvs.
	composerEnvs := launchBundleProjectEnvs(sourceEnvs)
	for _, env := range composerEnvs {
		sensitive := classificationsSensitive(classifications, env.Key)
		if _, envErr := target.CreateProjectEnv(ctx, input.ExistingProjectID, env.Key, env.Value, sensitive); envErr != nil {
			_ = appendAuditLog(stateDir, launchAuditEntry{
				LaunchID:          launchID,
				Action:            "create-project-env-failed",
				SourceProjectID:   sourceProjectID,
				TargetProjectName: input.ProductionProjectName,
				Result:            "failure",
				ErrorMessage:      fmt.Sprintf("CreateProjectEnv %s: %v", env.Key, envErr),
			})
			return convertError(fmt.Errorf("existing-project env mutation %s: %w", env.Key, envErr), WithRecoveryStatus()), nil, nil
		}
	}

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

// classificationsSensitive returns whether a classified env should be
// marked Sensitive on CreateProjectEnv. Conservative default: any
// classification other than plain-config is sensitive (auto-secret,
// external-secret, infrastructure). Plain-config is treated as
// non-sensitive (the user opted into the "literal copy" bucket).
func classificationsSensitive(classifications map[string]topology.SecretClassification, key string) bool {
	c, ok := classifications[key]
	if !ok {
		return false
	}
	return c != topology.SecretClassPlainConfig
}

// ensureNoExistingProdTokenInState is a defensive helper that traces
// uses of input.ExistingProdToken inside any string that could end up
// in state or response. Currently a no-op stub the AST sentinel (Phase
// 5) will replace with a serialization-fixture check. Marked _ to
// avoid unused-variable warnings; will gain a body when Phase 5 lands.
var _ = errors.New
