package tools

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// launchFailedFromPlatformError builds a failed-status response that
// preserves the structured detail of a platform.PlatformError instead
// of collapsing it through err.Error() — which, for the Zerops API's
// errorList shape, yields the placeholder "See metadata" and drops
// APICode + APIMeta + the expanded Suggestion entirely.
//
// Behaviour:
//   - errors.As(err, &pe) extracts the typed error. The Blocker carries
//     pe.Suggestion (already expanded inline by formatAPIMetaActionable
//     in zerops_errors.go), pe.APICode, pe.APIMeta.
//   - BlockerCategory is derived from pe.Code via blockerCategoryFromPlatformCode.
//     Hardcoding Auth on every CreateAndImportProject failure (the pre-F5
//     shape) misled agents into token-regeneration loops when the actual
//     failure was schema validation or project-name collision.
//   - When err is not a *PlatformError (rare — wrapped generic Go errors),
//     falls back to fallbackCategory + err.Error() in the message.
//
// fallbackCategory is the category used when no typed error is present
// OR when the typed error's code maps to a non-specific bucket.
// fallbackMessageFmt is the per-site prefix (e.g. "CreateAndImportProject
// failed: %v"); err is the only argument expected — verbatim if the
// caller has already composed it.
func launchFailedFromPlatformError(
	_ []workflow.KnowledgeAtom,
	err error,
	fallbackCategory topology.BlockerCategory,
	blockerID string,
	fallbackMessageFmt string,
) *mcp.CallToolResult {
	blocker := topology.Blocker{
		ID:       blockerID,
		Severity: topology.BlockerSeverityBlock,
		Category: fallbackCategory,
		Message:  fmt.Sprintf(fallbackMessageFmt, err),
	}

	var pe *platform.PlatformError
	if errors.As(err, &pe) {
		blocker.Suggestion = pe.Suggestion
		blocker.APICode = pe.APICode
		blocker.APIMeta = convertPlatformAPIMeta(pe.APIMeta)
		if category := blockerCategoryFromPlatformCode(pe.Code); category != "" {
			blocker.Category = category
		}
		// Re-author the message so the agent reads the typed Message
		// (often "See metadata" placeholder) plus the expanded Suggestion
		// in one line, instead of just the placeholder.
		blocker.Message = composeBlockerMessage(fallbackMessageFmt, pe)
	}

	return jsonResult(launchProductionResponse{
		Workflow: workflowLaunchProduction,
		Status:   topology.LaunchStatusFailed,
		Phase:    workflow.PhaseLaunchProductionActive,
		Guidance: blocker.Message,
		Blockers: []topology.Blocker{blocker},
	})
}

// composeBlockerMessage builds the agent-facing message from a typed
// PlatformError. Format: "<site prefix>: <Message> [<APICode>]. <Suggestion>"
// — collapses the agent-relevant fields into one readable line while
// preserving the structured Blocker.APIMeta for programmatic reads.
func composeBlockerMessage(fallbackFmt string, pe *platform.PlatformError) string {
	base := fmt.Sprintf(fallbackFmt, errSummary(pe))
	if pe.Suggestion != "" && !strings.Contains(base, pe.Suggestion) {
		base = base + " " + pe.Suggestion
	}
	return base
}

// errSummary returns a one-line summary of a PlatformError suitable for
// embedding in a parent error message. Includes APICode in brackets
// when the Message would otherwise be a generic placeholder
// ("See metadata", empty, etc.) — without the code, the agent has
// nothing actionable to type-discriminate on.
func errSummary(pe *platform.PlatformError) string {
	msg := strings.TrimSpace(pe.Message)
	if pe.APICode != "" && (msg == "" || msg == "See metadata") {
		return fmt.Sprintf("%s [%s]", msg, pe.APICode)
	}
	if pe.APICode != "" {
		return fmt.Sprintf("%s [%s]", msg, pe.APICode)
	}
	return msg
}

// blockerCategoryFromPlatformCode maps a typed platform error code to
// the BlockerCategory that best represents the user-visible failure
// class. Returns "" when no specific mapping applies — caller falls
// back to its per-site default.
//
// Codes that don't map specifically:
//   - ErrAPIError (generic 4xx without further classification) — caller
//     decides (often Schema for input-validation; Other otherwise).
//   - ErrUnknown (wrapped Go error) — caller decides.
func blockerCategoryFromPlatformCode(code string) topology.BlockerCategory {
	switch code {
	case platform.ErrAuthTokenExpired, platform.ErrAuthRequired, platform.ErrTokenNoProject, platform.ErrTokenMultiProject:
		return topology.BlockerCategoryAuth
	case platform.ErrPermissionDenied:
		return topology.BlockerCategoryAuth
	case platform.ErrNetworkError, platform.ErrAPITimeout:
		return topology.BlockerCategoryOther
	case platform.ErrAPIRateLimited:
		return topology.BlockerCategoryOther
	case platform.ErrInvalidImportYml, platform.ErrInvalidZeropsYml, platform.ErrInvalidParameter:
		return topology.BlockerCategorySchema
	}
	return ""
}

// convertPlatformAPIMeta promotes platform.APIMetaItem entries into
// topology.APIMetaItem. Same shape, different layer — topology stays
// stdlib-only per architecture rules, so Blocker carries the topology
// type instead of importing platform upward.
func convertPlatformAPIMeta(in []platform.APIMetaItem) []topology.APIMetaItem {
	if len(in) == 0 {
		return nil
	}
	out := make([]topology.APIMetaItem, 0, len(in))
	for _, item := range in {
		out = append(out, topology.APIMetaItem{
			Code:     item.Code,
			Error:    item.Error,
			Metadata: item.Metadata,
		})
	}
	return out
}

// formatPlatformErrorForAudit produces a one-line forensic string
// suitable for state.LastError and launchAuditEntry.ErrorMessage when
// the upstream error carries a typed PlatformError. Captures Message +
// APICode + the per-field rejections compacted from APIMeta. Without
// this, the audit trail records only "See metadata" — useless for
// post-hoc debugging.
//
// Non-platform errors fall back to err.Error() verbatim. nil err
// returns the empty string so callers can write straight into the
// state/audit fields without nil-guarding.
func formatPlatformErrorForAudit(err error) string {
	if err == nil {
		return ""
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		return err.Error()
	}
	parts := []string{}
	if pe.Message != "" {
		parts = append(parts, pe.Message)
	}
	if pe.APICode != "" {
		parts = append(parts, fmt.Sprintf("apiCode=%s", pe.APICode))
	}
	if pe.Suggestion != "" {
		parts = append(parts, fmt.Sprintf("suggestion=%s", pe.Suggestion))
	}
	for _, item := range pe.APIMeta {
		for field, reasons := range item.Metadata {
			parts = append(parts, fmt.Sprintf("%s=%s", field, strings.Join(reasons, "; ")))
		}
	}
	if len(parts) == 0 {
		return err.Error()
	}
	return strings.Join(parts, " | ")
}

// launchFirstDeployFailedResponse builds the failure response for the
// "target project created + services imported successfully, but first
// deploy hit a FAILED terminal process" case. Per
// plans/backlog/launch-first-deploy-failed-recovery-hint.md: this is
// typically platform-side (build queue, clone preflight, quota — Karel's
// 2026-05-16 reproducer showed `WAITING_TO_BUILD` with null
// pipelineStart). The agent's recoveries — retry-via-push, dashboard
// inspection, delete + republish — are user actions, not in-tool calls,
// so the response embeds them as explicit guidance in the message
// instead of a structured topology.Recovery (no in-tool target is
// reachable across projects from the source-bound ZCP instance).
//
// state.ImportedServices lands inline so the agent can map state.LastError
// (which names the offending service + process ID) to the per-service
// record without a second tool call.
func launchFirstDeployFailedResponse(state *launchState, projectID string) *mcp.CallToolResult {
	var dashboardURL string
	if projectID != "" {
		dashboardURL = "https://app.zerops.io/project/" + projectID
	}
	msg := fmt.Sprintf(
		"Target project %s created and services imported, but the first deploy did not complete cleanly: %s.\n\n"+
			"This usually indicates a platform-side condition (build queue, clone preflight, quota). Recovery options:\n\n"+
			"1. Retry by pushing a fresh commit to the source repo — Zerops picks up the new ref and re-triggers build:\n\n"+
			"       git -C <source-repo> commit --allow-empty -m \"retry build\" && git push origin <main-branch>\n\n"+
			"2. Inspect the target project in the Zerops dashboard: %s\n"+
			"3. If the failure persists across retries, delete the target project (Zerops dashboard) and re-call publish after fixing the underlying source issue.\n\n"+
			"Per-service import + first-deploy outcomes are listed inline under `importedServices` — the failing service's process ID appears in the message above.",
		projectID, state.LastError, dashboardURL,
	)
	return jsonResult(launchProductionResponse{
		Workflow:         workflowLaunchProduction,
		Status:           topology.LaunchStatusFailed,
		Phase:            workflow.PhaseLaunchProductionActive,
		Guidance:         msg,
		ImportedServices: state.ImportedServices,
		Blockers: []topology.Blocker{{
			ID:       "first-deploy-failed",
			Severity: topology.BlockerSeverityBlock,
			Category: topology.BlockerCategoryOther,
			Message:  msg,
		}},
	})
}

// launchOrphanProjectResponse builds the failure response for the
// "project created, one or more services rejected" case. Pre-F5 this
// emitted just a blocker pointing at the state file ("inspect
// imported-services") — agents can't read state files directly. Now
// the response carries state.ImportedServices inline so each per-service
// ImportError (code + message) is visible to the agent at decision time.
func launchOrphanProjectResponse(state *launchState, projectID string) *mcp.CallToolResult {
	failingNames := make([]string, 0, len(state.ImportedServices))
	for _, svc := range state.ImportedServices {
		if svc.ImportError != "" {
			failingNames = append(failingNames, svc.Name)
		}
	}
	failed := strings.Join(failingNames, ", ")
	message := fmt.Sprintf("Target project %s created but services failed import: %s. Inspect per-service ImportError on importedServices in this response; delete the project via Zerops dashboard or retry with corrected inputs.", projectID, failed)
	return jsonResult(launchProductionResponse{
		Workflow:         workflowLaunchProduction,
		Status:           topology.LaunchStatusFailed,
		Phase:            workflow.PhaseLaunchProductionActive,
		Guidance:         message,
		ImportedServices: state.ImportedServices,
		Blockers: []topology.Blocker{{
			ID:       "orphan-project",
			Severity: topology.BlockerSeverityBlock,
			Category: topology.BlockerCategoryOrphan,
			Message:  message,
		}},
	})
}

// httpStatusFromPlatformCode is reserved for callers that need to drive
// retry policy from the upstream HTTP status. Currently used only for
// reference in test fixtures.
//
//nolint:unused // forward-compatibility helper for retry-policy callers
func httpStatusFromPlatformCode(code string) int {
	switch code {
	case platform.ErrAuthTokenExpired, platform.ErrAuthRequired:
		return http.StatusUnauthorized
	case platform.ErrPermissionDenied:
		return http.StatusForbidden
	case platform.ErrAPIRateLimited:
		return http.StatusTooManyRequests
	}
	return 0
}
