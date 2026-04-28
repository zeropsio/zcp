package tools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// validCloseModes is the closed set of CloseDeployMode values the agent may
// pass via the close-mode action. CloseModeUnset is excluded — agents
// should not explicitly transition a service back to the unset sentinel
// (use the legacy reset workflow if a clean slate is needed).
//
//nolint:gochecknoglobals // immutable lookup table
var validCloseModes = map[topology.CloseDeployMode]bool{
	topology.CloseModeAuto:    true,
	topology.CloseModeGitPush: true,
	topology.CloseModeManual:  true,
}

// closeModeListEntry is one row in the listing-mode response: current
// close-mode + all options the agent may switch to.
type closeModeListEntry struct {
	Hostname         string                     `json:"hostname"`
	Current          topology.CloseDeployMode   `json:"current"`
	Confirmed        bool                       `json:"confirmed"`
	Options          []topology.CloseDeployMode `json:"options"`
	GitPushState     topology.GitPushState      `json:"gitPushState"`
	BuildIntegration topology.BuildIntegration  `json:"buildIntegration"`
	Hint             string                     `json:"hint"`
}

type closeModeListResponse struct {
	Status   string               `json:"status"`
	Services []closeModeListEntry `json:"services"`
}

// handleCloseMode is the central per-pair close-mode setter introduced by
// the deploy-strategy decomposition (Phase 5). Replaces the conflated
// action=strategy with a single-concern action targeting the
// CloseDeployMode dimension only — git-push-setup and build-integration
// land separately as their own actions.
//
// Three modes:
//
//   - Listing: empty input.CloseModes → returns current close-mode + the
//     other two orthogonal dimensions per service. No mutation.
//   - Update: input.CloseModes={hostname:auto|git-push|manual} → writes
//     meta.CloseDeployMode + CloseDeployModeConfirmed=true.
//   - Chained guidance: when switching to git-push and meta.GitPushState !=
//     GitPushConfigured, the response carries a guidance pointer at
//     action=git-push-setup (per §3.4 Scenario B — close-mode write
//     succeeds, capability setup is a separate explicit action).
func handleCloseMode(input WorkflowInput, stateDir string) (*mcp.CallToolResult, any, error) {
	if len(input.CloseModes) == 0 {
		return handleCloseModeList(stateDir)
	}

	closeModes := make(map[string]topology.CloseDeployMode, len(input.CloseModes))
	for hostname, raw := range input.CloseModes {
		cm := topology.CloseDeployMode(raw)
		if !validCloseModes[cm] {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Invalid closeMode %q for %q", raw, hostname),
				"Valid values: auto, git-push, manual"), WithRecoveryStatus()), nil, nil
		}
		closeModes[hostname] = cm
	}

	updated := make([]string, 0, len(closeModes))
	var setupPointers []string
	for hostname, cm := range closeModes {
		meta, err := workflow.FindServiceMeta(stateDir, hostname)
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceNotFound,
				fmt.Sprintf("Read service meta %q: %v", hostname, err),
				"Ensure the service was bootstrapped first"), WithRecoveryStatus()), nil, nil
		}
		if meta == nil || !meta.IsComplete() {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceNotFound,
				fmt.Sprintf("Service %q is not bootstrapped", hostname),
				"Run bootstrap first: zerops_workflow action=\"start\" workflow=\"bootstrap\""), WithRecoveryStatus()), nil, nil
		}

		updated = append(updated, fmt.Sprintf("%s=%s", hostname, cm))

		// No-op shortcut: same close-mode + already confirmed.
		if meta.CloseDeployMode == cm && meta.CloseDeployModeConfirmed {
			continue
		}
		meta.CloseDeployMode = cm
		meta.CloseDeployModeConfirmed = true
		if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceNotFound,
				fmt.Sprintf("Write service meta %q: %v", hostname, err),
				""), WithRecoveryStatus()), nil, nil
		}

		// Chained guidance: switching to git-push without a configured
		// GitPushState requires a follow-up action=git-push-setup (per
		// §3.4 Scenario B). Surface the pointer so the agent walks the
		// prereq chain without a status round-trip.
		if cm == topology.CloseModeGitPush && meta.GitPushState != topology.GitPushConfigured {
			setupPointers = append(setupPointers, fmt.Sprintf("Run zerops_workflow action=\"git-push-setup\" service=%q to set up GIT_TOKEN, .netrc, and remote URL.", hostname))
		}
	}

	result := map[string]any{
		"status":   "updated",
		"services": strings.Join(updated, ", "),
	}
	if len(setupPointers) > 0 {
		result["nextSteps"] = setupPointers
	}
	return jsonResult(result), nil, nil
}

// handleCloseModeList returns the per-pair close-mode state + the other
// two orthogonal dimensions for every bootstrapped service. Pure read.
func handleCloseModeList(stateDir string) (*mcp.CallToolResult, any, error) {
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("List service metas: %v", err),
			""), WithRecoveryStatus()), nil, nil
	}
	options := []topology.CloseDeployMode{topology.CloseModeAuto, topology.CloseModeGitPush, topology.CloseModeManual}

	entries := make([]closeModeListEntry, 0, len(metas))
	for _, m := range metas {
		if !m.IsComplete() {
			continue
		}
		current := m.CloseDeployMode
		if current == "" {
			current = topology.CloseModeUnset
		}
		entries = append(entries, closeModeListEntry{
			Hostname:         m.Hostname,
			Current:          current,
			Confirmed:        m.CloseDeployModeConfirmed,
			Options:          options,
			GitPushState:     m.GitPushState,
			BuildIntegration: m.BuildIntegration,
			Hint:             fmt.Sprintf(`zerops_workflow action="close-mode" closeMode={%q:%q}`, m.Hostname, topology.CloseModeAuto),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Hostname < entries[j].Hostname })

	return jsonResult(closeModeListResponse{
		Status:   "list",
		Services: entries,
	}), nil, nil
}
