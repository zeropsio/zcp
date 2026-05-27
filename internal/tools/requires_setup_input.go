package tools

import (
	"github.com/zeropsio/zcp/internal/workflow"
)

// RequiresSetupInputResponse is the canonical wire shape returned when a
// gate or lifecycle read cannot determine a setup-name + has no explicit
// override. Named struct (not inline map) so the JSON schema is
// test-pinnable across the call sites that emit it: adoption cascade
// miss, classic-bootstrap first-deploy cascade miss, set-default-setup
// without live yaml + explicit input. See plan
// `plans/setup-name-local-canonical-2026-05-27.md` §requiresSetupInput.
type RequiresSetupInputResponse struct {
	Status          string                     `json:"status"`                    // always "blocked"
	Reason          string                     `json:"reason"`                    // always "requiresSetupInput"
	Service         string                     `json:"service"`                   // hostname for which the cascade ran
	TargetHostname  string                     `json:"targetHostname"`            // pair-half or singleton hostname
	AvailableSetups []string                   `json:"availableSetups,omitempty"` // populated when yaml was readable
	AmbiguityReason string                     `json:"ambiguityReason"`           // cascade-step prose
	Recovery        RequiresSetupInputRecovery `json:"recovery"`                  // exact tool+action the agent should call next
}

// RequiresSetupInputRecovery names the tool+action the agent should call
// to resolve the blocker. Tool + Action MUST reference an existing
// surface (no typos that ship) — pinned by `TestRequiresSetupInputShape`.
type RequiresSetupInputRecovery struct {
	Tool   string                         `json:"tool"`
	Action string                         `json:"action"`
	Args   RequiresSetupInputRecoveryArgs `json:"args"`
}

// RequiresSetupInputRecoveryArgs spells out the set-default-setup
// invocation. Setup is the placeholder the agent fills in from
// AvailableSetups; StageSetup is optional and only relevant for pair
// shapes (omitempty drops it from singleton responses).
type RequiresSetupInputRecoveryArgs struct {
	Service    string `json:"service"`
	Setup      string `json:"setup"`
	StageSetup string `json:"stageSetup,omitempty"`
}

// buildRequiresSetupInputResponse projects the cascade's
// `*workflow.ErrRequiresSetupInput` blocker into the wire shape. The
// service argument is the user-facing hostname (which may differ from
// the in-blocker TargetHostname when adoption was driven by a project
// alias — caller decides which name to surface).
func buildRequiresSetupInputResponse(service string, blocker *workflow.ErrRequiresSetupInput) RequiresSetupInputResponse {
	if blocker == nil {
		return RequiresSetupInputResponse{}
	}
	args := RequiresSetupInputRecoveryArgs{
		Service: service,
		Setup:   "<choose-from-availableSetups>",
	}
	return RequiresSetupInputResponse{
		Status:          "blocked",
		Reason:          "requiresSetupInput",
		Service:         service,
		TargetHostname:  blocker.TargetHostname,
		AvailableSetups: blocker.AvailableSetups,
		AmbiguityReason: blocker.Reason,
		Recovery: RequiresSetupInputRecovery{
			Tool:   "zerops_workflow",
			Action: "set-default-setup",
			Args:   args,
		},
	}
}
