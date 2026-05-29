package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handleSetDefaultSetup writes the canonical zerops.yaml setup-block
// name(s) into a service's local ServiceMeta — the user-facing
// recovery for requiresSetupInput blockers (cascade miss) and
// staleMetaSetup blockers (meta drift vs live yaml).
//
// Pair-keyed write: input.Setup writes PrimarySetupName; optional
// input.StageSetup writes StageSetupName when the meta carries a
// StageHostname. Singleton metas (PlanModeDev / PlanModeSimple)
// silently ignore StageSetup so an agent that always passes both
// fields doesn't trip on shape-specific schemas.
//
// Validation: when a zerops.yaml is reachable via per-service mount
// (container) or workingDir (local) AND its parse yields a non-empty
// SetupNames list, the supplied value(s) MUST exist in that list.
// Mismatch returns a requiresSetupInput response with the actual
// availableSetups so the agent re-calls with a corrected value. When
// no yaml is reachable, the write proceeds — the meta cache is the
// canonical store and the next deploy preflight will catch any
// staleMetaSetup if the yaml later disagrees.
//
// No platform write happens at any point — local meta is the only
// canonical per plan §"Architectural decision".
func handleSetDefaultSetup(_ context.Context, _ platform.Client, _ string, input WorkflowInput, stateDir string) (*mcp.CallToolResult, any, error) {
	if input.TargetService == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"targetService is required for action=\"set-default-setup\"",
			"Pass targetService=<runtime-hostname>"), WithRecoveryStatus()), nil, nil
	}
	primary := strings.TrimSpace(input.Setup)
	if primary == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"setup is required for action=\"set-default-setup\"",
			"Pass setup=<zerops.yaml-setup-block-name>"), WithRecoveryStatus()), nil, nil
	}
	stage := strings.TrimSpace(input.StageSetup)

	meta, err := workflow.FindServiceMeta(stateDir, input.TargetService)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("read meta: %v", err),
			""), WithRecoveryStatus()), nil, nil
	}
	if meta == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("no ServiceMeta found for %q — run bootstrap or adopt-local first", input.TargetService),
			""), WithRecoveryStatus()), nil, nil
	}

	// Validate against any reachable yaml. Best-effort: missing yaml
	// is not a hard error — the cache write itself stands as the
	// canonical record, and the next deploy preflight catches drift.
	projectRoot := projectRootFromState(stateDir)
	sourceHostname := input.TargetService
	if doc, parseErr := findAndParseZeropsYml(projectRoot, sourceHostname, ""); parseErr == nil && doc != nil {
		availableSetups := doc.SetupNames()
		if len(availableSetups) > 0 {
			if !setupNameInList(primary, availableSetups) {
				return jsonResult(buildRequiresSetupInputResponse(input.TargetService, &workflow.ErrRequiresSetupInput{
					Service:         input.TargetService,
					TargetHostname:  input.TargetService,
					AvailableSetups: availableSetups,
					Reason:          fmt.Sprintf("supplied setup=%q not present in zerops.yaml — choose from availableSetups", primary),
				})), nil, nil
			}
			if stage != "" && !setupNameInList(stage, availableSetups) {
				return jsonResult(buildRequiresSetupInputResponse(input.TargetService, &workflow.ErrRequiresSetupInput{
					Service:         input.TargetService,
					TargetHostname:  input.TargetService,
					AvailableSetups: availableSetups,
					Reason:          fmt.Sprintf("supplied stageSetup=%q not present in zerops.yaml — choose from availableSetups", stage),
				})), nil, nil
			}
		}
	}

	// Locked RMW — pair-keyed; singleton metas silently ignore stage. Set only
	// the setup-name fields on the fresh meta (XCUT-1).
	if err := workflow.UpdateServiceMeta(stateDir, input.TargetService, func(m *workflow.ServiceMeta) error {
		m.PrimarySetupName = primary
		if stage != "" && m.StageHostname != "" {
			m.StageSetupName = stage
		}
		return nil
	}); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("write meta: %v", err),
			""), WithRecoveryStatus()), nil, nil
	}
	// Mirror onto the local copy for the echo-back response below.
	meta.PrimarySetupName = primary
	if stage != "" && meta.StageHostname != "" {
		meta.StageSetupName = stage
	}

	return jsonResult(setDefaultSetupResponse{
		Status:           "written",
		Service:          input.TargetService,
		PrimarySetupName: meta.PrimarySetupName,
		StageSetupName:   meta.StageSetupName,
	}), nil, nil
}

// setDefaultSetupResponse is the success-path wire shape — echoes back
// what got persisted so the agent can confirm without re-reading the
// meta file.
type setDefaultSetupResponse struct {
	Status           string `json:"status"`           // always "written"
	Service          string `json:"service"`          // input.TargetService
	PrimarySetupName string `json:"primarySetupName"` // value just written
	StageSetupName   string `json:"stageSetupName,omitempty"`
}

// setupNameInList reports whether name appears in available — extracted
// for table-pin clarity (a future caller might want a richer error,
// but bool keeps the validation rule readable at call sites).
func setupNameInList(name string, available []string) bool {
	return slices.Contains(available, name)
}
