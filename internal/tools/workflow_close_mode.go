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

// preflightCloseModeDualHalves rejects input maps carrying divergent
// closeMode values for both halves of a pair. ServiceMeta is pair-keyed —
// FindServiceMeta resolves either dev or stage hostname to the canonical
// dev meta — so writing the same pair twice with conflicting values
// would silently last-write-wins by Go's undefined map iteration order.
//
// Hostnames whose meta cannot be loaded (missing / incomplete) are skipped
// here; the main per-hostname loop produces the proper ErrAdoptRequired /
// ErrServiceNotFound diagnostic. Same-value duals are allowed: the
// per-hostname loop writes the canonical meta twice and the no-op
// shortcut absorbs the second pass.
func preflightCloseModeDualHalves(stateDir string, closeModes map[string]topology.CloseDeployMode) error {
	canonicalToInputs := make(map[string]map[string]topology.CloseDeployMode)
	for hostname, cm := range closeModes {
		meta, err := workflow.FindServiceMeta(stateDir, hostname)
		if err != nil || meta == nil || !meta.IsComplete() {
			continue
		}
		if canonicalToInputs[meta.Hostname] == nil {
			canonicalToInputs[meta.Hostname] = make(map[string]topology.CloseDeployMode)
		}
		canonicalToInputs[meta.Hostname][hostname] = cm
	}
	canonicals := make([]string, 0, len(canonicalToInputs))
	for c := range canonicalToInputs {
		canonicals = append(canonicals, c)
	}
	sort.Strings(canonicals)
	for _, canonical := range canonicals {
		inputs := canonicalToInputs[canonical]
		if len(inputs) <= 1 {
			continue
		}
		var first topology.CloseDeployMode
		divergent := false
		pairs := make([]string, 0, len(inputs))
		for h, v := range inputs {
			pairs = append(pairs, fmt.Sprintf("%s=%s", h, v))
			if first == "" {
				first = v
			} else if v != first {
				divergent = true
			}
		}
		if !divergent {
			continue
		}
		sort.Strings(pairs)
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("close-mode conflict for pair %q: input map carries divergent values across both halves (%s)",
				canonical, strings.Join(pairs, ", ")),
			fmt.Sprintf("Pass a single value for the pair: closeMode={%q:\"<auto|git-push|manual>\"}. Both halves of a pair share one canonical close-mode written to the pair's dev-half meta.", canonical))
	}
	return nil
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
	RemoteURL        string                     `json:"remoteUrl,omitempty"`
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

	// Preflight: reject divergent dual-half input. ServiceMeta is pair-keyed
	// (one file per pair, FindServiceMeta resolves either hostname to the
	// same dev meta). Without this guard, an input map carrying conflicting
	// values for both halves silently last-write-wins by Go's undefined map
	// iteration order. Same-value dual-halves stay accepted: the per-hostname
	// loop below idempotently writes the canonical meta twice and the no-op
	// shortcut absorbs the second pass.
	if err := preflightCloseModeDualHalves(stateDir, closeModes); err != nil {
		return convertError(err, WithRecoveryStatus()), nil, nil
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
			// H2 sweep: same shape as workflow_develop.go — point at
			// bootstrap+adopt, not generic status. Code is ErrAdoptRequired
			// (not ErrServiceNotFound — the service IS found in Zerops, it
			// just lacks ZCP bootstrap metadata). Self-evident from the code
			// name what Recovery should look like; pinned by
			// TestErrAdoptRequiredCarriesAdoptRecovery.
			return convertError(platform.NewPlatformError(
				platform.ErrAdoptRequired,
				fmt.Sprintf("Service %q is not bootstrapped", hostname),
				"Run bootstrap first: zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\""),
				WithRecovery(&RecoveryHint{
					Tool:   "zerops_workflow",
					Action: "start",
					Args:   map[string]string{"workflow": "bootstrap", "route": "adopt"},
				})), nil, nil
		}

		// Local-only services have no Zerops runtime target — closeMode=auto
		// (zcli push direct) makes no sense, mirroring the pre-decomp deploy
		// gate at deploy_strategy_gate.go that rejected push-dev for
		// PlanModeLocalOnly. Surface the same redirection here so the
		// invalid choice is caught at intent-set time, not at deploy time.
		if cm == topology.CloseModeAuto && meta.Mode == topology.PlanModeLocalOnly {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Service %q is local-only — closeMode=auto needs a Zerops runtime target to push to", hostname),
				"Link a Zerops runtime first: zerops_workflow action=\"adopt-local\" targetService=<runtime-hostname>. Or pick git-push / manual, which work without a stage."), WithRecoveryStatus()), nil, nil
		}

		// O3 fix (round-3 audit): close-mode=git-push only makes sense for
		// modes that can act as a push source. ModeDev (legacy dev-only) and
		// ModeStage (stage half — build target, never push source) cannot
		// run `zerops_deploy strategy="git-push"` (handler rejects with
		// PushSourceModeUnsupported). Without this gate, an agent can set
		// closeMode=git-push for ModeDev, walk the git-push-setup chain,
		// configure GIT_TOKEN/remote — then hit a hard rejection at deploy
		// time with mode-expansion guidance. Catch the invalid combination
		// at intent-set time, mirroring the local-only gate above.
		if cm == topology.CloseModeGitPush && !topology.IsPushSource(meta.Mode) {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Service %q is in mode %q which cannot push (only Standard/Simple/LocalStage/LocalOnly can act as push source)", hostname, meta.Mode),
				"Either pick closeMode=auto/manual, or expand the service to standard mode first (re-run bootstrap with route=adopt + isExisting=true + bootstrapMode=\"standard\" + an explicit stageHostname). See develop-mode-expansion atom for the plan shape."), WithRecoveryStatus()), nil, nil
		}

		updated = append(updated, fmt.Sprintf("%s=%s", hostname, cm))

		// No-op shortcut: same close-mode + already confirmed.
		if meta.CloseDeployMode == cm && meta.CloseDeployModeConfirmed {
			continue
		}
		// Locked read-modify-write: set only this dimension on the fresh meta so
		// a concurrent git-push-setup / build-integration on the same pair can't
		// be lost-updated (XCUT-1).
		if err := workflow.UpdateServiceMeta(stateDir, hostname, func(m *workflow.ServiceMeta) error {
			m.CloseDeployMode = cm
			m.CloseDeployModeConfirmed = true
			return nil
		}); err != nil {
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
			setupPointers = append(setupPointers, fmt.Sprintf("Run zerops_workflow action=\"git-push-setup\" service=%q remoteUrl=<url> gitToken=<PAT> (container) or remoteUrl=<url> (local) — the handler probes the remote BEFORE writing project state.", hostname))
		}
	}

	result := map[string]any{
		"status":   "updated",
		"services": strings.Join(updated, ", "),
	}
	if len(setupPointers) > 0 {
		result["nextSteps"] = setupPointers
	}
	// Attach WorkSessionState so the agent observes the lifecycle effect
	// of the close-mode write in the same response. Auto-close is DERIVED
	// (DeriveCloseState), not stamped: if this write tipped the gate (scope
	// green + every meta now auto/git-push), sessionAnnotations recomputes the
	// close state and the response carries status="auto-closed" instead of
	// leaving the agent to learn via a follow-up action="status" call. Spec
	// §1.3 invariant: every state transition observable in the next MCP response.
	return jsonResult(attachWorkSessionState(result, stateDir)), nil, nil
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
			RemoteURL:        m.RemoteURL,
			Hint:             fmt.Sprintf(`zerops_workflow action="close-mode" closeMode={%q:%q}`, m.Hostname, topology.CloseModeAuto),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Hostname < entries[j].Hostname })

	return jsonResult(closeModeListResponse{
		Status:   "list",
		Services: entries,
	}), nil, nil
}
