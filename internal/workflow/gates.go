package workflow

import (
	"fmt"
	"time"
)

// GateResult holds the result of a gate check.
type GateResult struct {
	Passed  bool     `json:"passed"`
	Gate    string   `json:"gate"`    // "G0", "G1", etc. Empty if no gate applies.
	Missing []string `json:"missing"` // missing evidence types
}

// gateDefinition maps a gate to its required evidence types.
type gateDefinition struct {
	name     string
	from     Phase
	to       Phase
	required []string
}

// gates defines the gate requirements for each phase transition.
var gates = []gateDefinition{
	{"G0", PhaseInit, PhaseDiscover, []string{"recipe_review"}},
	{"G1", PhaseDiscover, PhaseDevelop, []string{"discovery"}},
	{"G2", PhaseDevelop, PhaseDeploy, []string{"dev_verify"}},
	{"G3", PhaseDeploy, PhaseVerify, []string{"deploy_evidence"}},
	{"G4", PhaseVerify, PhaseDone, []string{"stage_verify"}},
}

const discoveryFreshDuration = 24 * time.Hour

// GateName returns the gate name for a transition, or empty if no gate applies.
func GateName(from, to Phase) string {
	for _, g := range gates {
		if g.from == from && g.to == to {
			return g.name
		}
	}
	return ""
}

// CheckGate checks whether a phase transition is allowed based on evidence.
// Returns an error for invalid transitions in the given mode.
func CheckGate(from, to Phase, mode Mode, evidenceDir, sessionID string) (*GateResult, error) {
	// Quick mode has no gates.
	if mode == ModeQuick {
		return &GateResult{Passed: true}, nil
	}

	// Validate the transition is valid for this mode.
	if !IsValidTransition(from, to, mode) {
		return nil, fmt.Errorf("check gate: invalid transition %s → %s in mode %s", from, to, mode)
	}

	// Find the gate for this transition.
	var gate *gateDefinition
	for i := range gates {
		if gates[i].from == from && gates[i].to == to {
			gate = &gates[i]
			break
		}
	}

	// No gate defined for this transition — pass.
	if gate == nil {
		return &GateResult{Passed: true}, nil
	}

	// G0 special case: skip if discovery.json exists and is fresh (<24h).
	if gate.name == "G0" {
		if isDiscoveryFresh(evidenceDir, sessionID) {
			return &GateResult{Passed: true, Gate: "G0"}, nil
		}
	}

	// Check required evidence.
	var missing []string
	for _, req := range gate.required {
		_, err := LoadEvidence(evidenceDir, sessionID, req)
		if err != nil {
			missing = append(missing, req)
		}
	}

	return &GateResult{
		Passed:  len(missing) == 0,
		Gate:    gate.name,
		Missing: missing,
	}, nil
}

// isDiscoveryFresh checks if discovery.json exists and was created within 24h.
func isDiscoveryFresh(evidenceDir, sessionID string) bool {
	ev, err := LoadEvidence(evidenceDir, sessionID, "discovery")
	if err != nil {
		return false
	}
	ts, err := time.Parse(time.RFC3339, ev.Timestamp)
	if err != nil {
		return false
	}
	return time.Since(ts) < discoveryFreshDuration
}
