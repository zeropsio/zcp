// Package tools — handleLaunchConfirmProduction closes the launch
// window (single-token lifecycle T3).
//
// The window stays open through CI/CD setup, the first releases, and
// any recovery (problems can surface there and ZCP must still be able
// to fix them or reset the whole project) — it closes only when the
// USER confirms the production project is fully functional. The close
// is PHYSICAL: deleting the staged ZCP_LAUNCH_TOKEN secret leaves ZCP
// nowhere to read the credential from, so "never works with it again"
// is enforced by absence, not policy. WindowClosedAt is stamped for
// honest status messages only.
//
// The integration token itself STAYS VALID: GitHub Actions keeps using
// its repo-secret copy, and the final response recommends regenerating
// the token in the dashboard — regeneration preserves settings,
// invalidates the old value everywhere (including every copy the
// conversation ever saw), and the user then refreshes the repo secret
// with the new value in their own terminal. What WAS one-time is the
// platform delegation that may have minted this token in the first
// place (plans/token-delegation-implementation-spec-2026-07-10.md) —
// once spent, minting another token needs a fresh delegation or the
// manual dashboard path; the token itself has no expiry.
package tools

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// launchTokenDashboardURL is the Zerops dashboard page where access
// tokens are managed (create / regenerate / revoke). Same host as the
// pipeline deep-links.
const launchTokenDashboardURL = "https://app.zerops.io/settings/token-management"

// handleLaunchConfirmProduction dispatches action="confirm-production".
// client is the SOURCE-project session client (staged-secret read +
// delete); the prod-side liveness read constructs a per-call admin
// client from the resolved token, best-effort.
func handleLaunchConfirmProduction(
	ctx context.Context,
	projectID string,
	client platform.Client,
	input WorkflowInput,
	stateDir string,
	apiHost string,
) (*mcp.CallToolResult, any, error) {
	if input.ProductionProjectName == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"confirm-production requires productionProjectName (locates the launch state for the target production project)",
			`Re-call with productionProjectName="<name>" confirmFunctional=true after the user confirms the production project is fully functional.`,
		), WithRecoveryStatus()), nil, nil
	}
	launchID := generateLaunchID(projectID, input.ProductionProjectName)
	state, err := readLaunchState(stateDir, launchID)
	if err != nil || state == nil {
		//nolint:nilerr // structured refusal IS the response; nil error is the MCP-success boundary contract
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("no launch state for %q — confirm-production closes the window of a launch this ZCP performed", input.ProductionProjectName),
			"Check productionProjectName spelling; the launch state lives under .zcp/state/launch-production/.",
		), WithRecoveryStatus()), nil, nil
	}
	if state.Status != topology.LaunchStatusLaunched {
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("confirm-production applies to a launched project; %q is %q", input.ProductionProjectName, state.Status),
			`Reach launched first (the mutation pipeline / resume), verify the first release, then re-call. A failed launch is cleaned up via action="reset" instead.`,
		), WithRecoveryStatus()), nil, nil
	}

	// Idempotent echo: the window is already closed — report the
	// original close, mutate nothing (compaction-recovery friendly).
	if !state.WindowClosedAt.IsZero() {
		return jsonResult(map[string]any{
			"workflow":       workflowLaunchProduction,
			"action":         "confirm-production",
			"status":         "window-closed",
			"windowClosedAt": state.WindowClosedAt.UTC().Format(time.RFC3339),
			"note":           "The launch window was already closed — the staged " + ops.LaunchTokenEnvKey + " secret is gone. Production management belongs to the dashboard or a project-scoped token.",
			"tokenLifecycle": launchTokenLifecycleBlock(nil, state, stateDir),
		}), nil, nil
	}

	// Resolve the launch-window token (staged secret, explicit launchKey
	// fallback) for the best-effort prod liveness + token-list reads.
	// Unresolvable token degrades to a warning — liveness is advisory,
	// the close itself needs only the source-side env delete.
	launchKey, tokErr := resolveLaunchWindowToken(ctx, client, projectID, state, input.LaunchKey)
	if tokErr != nil {
		// Best-effort: a stage-read failure only means prod-liveness can't be
		// checked. The close itself uses a separate source-side env delete, so
		// it does not depend on this read — degrade to "" → liveness warning.
		launchKey = ""
	}
	var warnings []string
	var liveness []map[string]any
	var tokens []platform.IntegrationTokenInfo
	if launchKey == "" {
		warnings = append(warnings, "liveness unverified: no staged "+ops.LaunchTokenEnvKey+" secret and no launchKey — confirm the production services in the dashboard")
	} else {
		admin, adminErr := projectAdminClientFactory(launchKey, apiHost)
		if adminErr != nil {
			warnings = append(warnings, fmt.Sprintf("liveness unverified: launch token rejected (%v)", adminErr))
		} else {
			liveness, warnings = launchConfirmLiveness(ctx, admin, state, warnings)
			if listed, tokErr := admin.ListIntegrationTokens(ctx); tokErr == nil {
				tokens = listed
			}
			admin.Close()
		}
	}

	// Consent gate: the user (not the agent) owns the "fully functional"
	// verdict. Without the ack, prompt with the prefilled retry call —
	// nothing is deleted or stamped.
	if !input.ConfirmFunctional.Bool() {
		return jsonResult(map[string]any{
			"workflow": workflowLaunchProduction,
			"action":   "confirm-production",
			"status":   "confirm-required",
			"closes":   "Closing the launch window DELETES the staged " + ops.LaunchTokenEnvKey + " secret on " + state.TargetServiceHostname + " — after that, CI/CD fixes through ZCP and reset-with-orphan-delete need a re-supplied token. The GitHub repo secret (actions family) is untouched and keeps delivering releases.",
			"preconditions": []string{
				"first release is LIVE on the production runtimes (prod-ops status / the pipeline run)",
				"HTTP exposure established + smoke check passed",
				"the USER explicitly confirms the project is fully functional",
			},
			"liveness": liveness,
			"warnings": warnings,
			"retryCall": map[string]any{
				"tool": "zerops_workflow",
				"args": map[string]any{
					"action":                "confirm-production",
					"workflow":              workflowLaunchProduction,
					"productionProjectName": input.ProductionProjectName,
					"confirmFunctional":     true,
				},
			},
		}), nil, nil
	}

	// Physical close — delete FIRST, stamp after: a failed delete leaves
	// the window honestly open (no stamp claiming otherwise).
	svc, lookupErr := ops.LookupService(ctx, client, projectID, state.TargetServiceHostname)
	if lookupErr != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrAPIError,
			fmt.Sprintf("confirm-production: locate stage service %q: %v — window NOT closed", state.TargetServiceHostname, lookupErr),
			"Fix the lookup cause and re-call; the staged secret must be deleted before the close is recorded.",
		), WithRecoveryStatus()), nil, nil
	}
	stagedDeleted, delErr := ops.EnvDeleteServiceKeyIfPresent(ctx, client, svc.ID, ops.LaunchTokenEnvKey)
	if delErr != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrAPIError,
			fmt.Sprintf("confirm-production: delete staged %s on %q: %v — window NOT closed", ops.LaunchTokenEnvKey, state.TargetServiceHostname, delErr),
			"Re-call once the env delete can succeed; the close is recorded only after the staged secret is gone.",
		), WithRecoveryStatus()), nil, nil
	}
	if !stagedDeleted {
		warnings = append(warnings, "staged "+ops.LaunchTokenEnvKey+" secret was already absent — recording the close for honest status")
	}

	state.WindowClosedAt = time.Now().UTC()
	if writeErr := writeLaunchState(stateDir, state); writeErr != nil {
		warnings = append(warnings, fmt.Sprintf("write launch state: %v (the staged secret IS deleted; status surfaces may not show the close)", writeErr))
	}
	_ = appendAuditLog(stateDir, launchAuditEntry{
		LaunchID:          state.LaunchID,
		Action:            "confirm-production",
		SourceProjectID:   state.SourceProjectID,
		TargetProjectID:   state.TargetProjectID,
		TargetProjectName: state.TargetProjectName,
		Result:            "success",
	})

	return jsonResult(map[string]any{
		"workflow":            workflowLaunchProduction,
		"action":              "confirm-production",
		"status":              "window-closed",
		"windowClosedAt":      state.WindowClosedAt.Format(time.RFC3339),
		"stagedSecretDeleted": stagedDeleted,
		"liveness":            liveness,
		"warnings":            warnings,
		"tokenLifecycle":      launchTokenLifecycleBlock(tokens, state, stateDir),
		"nextStep":            "The launch lifecycle is complete. For ongoing production iteration, generate a project-scoped key for the prod project and run a separate ZCP session against it; releases keep shipping via the tag pipeline.",
	}), nil, nil
}

// launchConfirmLiveness reads the prod services best-effort and reports
// each promoted runtime's status. Non-ACTIVE runtimes and read failures
// become warnings — never blocks (the user's confirmation is the gate).
func launchConfirmLiveness(ctx context.Context, admin platform.ProjectAdminClient, state *launchState, warnings []string) ([]map[string]any, []string) {
	services, err := admin.ListServices(ctx, state.TargetProjectID)
	if err != nil {
		return nil, append(warnings, fmt.Sprintf("liveness unverified: list prod services: %v", err))
	}
	statusByName := make(map[string]string, len(services))
	for _, s := range services {
		statusByName[s.Name] = s.Status
	}
	rows := make([]map[string]any, 0, len(state.RuntimeProds))
	for _, rt := range state.RuntimeProds {
		status, found := statusByName[rt.ProdHostname]
		if !found {
			status = "MISSING"
		}
		rows = append(rows, map[string]any{"hostname": rt.ProdHostname, "status": status})
		if status != "ACTIVE" {
			warnings = append(warnings, fmt.Sprintf("prod runtime %q is %s (expected ACTIVE) — confirm with the user before closing", rt.ProdHostname, status))
		}
	}
	return rows, warnings
}

// launchTokenLifecycleBlock renders the regenerate recommendation. The
// token-id match is best-effort: integration tokens that can create
// projects, preferring ones whose project access lists the launched
// project; exactly one candidate → named, else the generic dashboard
// pointer stands.
func launchTokenLifecycleBlock(tokens []platform.IntegrationTokenInfo, state *launchState, stateDir string) map[string]any {
	block := map[string]any{
		"truth":          "The integration token itself STAYS VALID — regeneration, not expiry, is what invalidates it. The staged copy is deleted, so ZCP has nowhere left to read it from; the GitHub repo secret (actions family) remains the only working copy in the delivery chain.",
		"recommendation": "Recommended hygiene: regenerate the token in the Zerops dashboard — regeneration keeps all settings and immediately invalidates the old value everywhere, including every copy this conversation ever saw — then update the GitHub repo secret with the new value.",
		"dashboard":      launchTokenDashboardURL,
	}
	if matched := matchLaunchToken(tokens, state.TargetProjectID); matched != nil {
		block["token"] = map[string]any{"id": matched.ID, "name": matched.Name}
	}
	if cmd := launchSecretRefreshCommand(stateDir, state); cmd != "" {
		block["refreshSecretCommand"] = cmd
	}
	return block
}

// matchLaunchToken picks the single integration token that plausibly is
// the launch token: canCreateProjects + access to the launched project
// (creator access), falling back to canCreateProjects alone. Ambiguity
// (0 or 2+ candidates) yields nil — the generic dashboard link stands.
func matchLaunchToken(tokens []platform.IntegrationTokenInfo, prodProjectID string) *platform.IntegrationTokenInfo {
	var creators, withAccess []platform.IntegrationTokenInfo
	for _, tk := range tokens {
		if !tk.CanCreateProjects {
			continue
		}
		creators = append(creators, tk)
		if slices.Contains(tk.ProjectIDs, prodProjectID) {
			withAccess = append(withAccess, tk)
		}
	}
	if len(withAccess) == 1 {
		return &withAccess[0]
	}
	if len(creators) == 1 {
		return &creators[0]
	}
	return nil
}

// launchSecretRefreshCommand prepares the post-regenerate repo-secret
// update for the actions family. USER-RUN by design: the regenerated
// value must not enter the conversation, so the user pastes it into the
// command in their OWN terminal (gh auth already present there).
func launchSecretRefreshCommand(stateDir string, state *launchState) string {
	if stateDir == "" || state == nil || state.TargetServiceHostname == "" {
		return ""
	}
	meta, err := workflow.FindServiceMeta(stateDir, state.TargetServiceHostname)
	if err != nil || meta == nil || meta.BuildIntegration != topology.BuildIntegrationActions {
		return ""
	}
	owner, repo, ok := ops.ParseGitRemoteOwnerRepo(meta.RemoteURL)
	if !ok {
		return ""
	}
	return fmt.Sprintf("gh secret set %s -b \"<the regenerated token value>\" -R %s/%s  # user-run in their own terminal — the value must not enter this conversation",
		launchProdSecretName, owner, repo)
}
