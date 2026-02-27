package workflow

import (
	"fmt"
	"time"
)

// GateResult holds the result of a gate check.
type GateResult struct {
	Passed   bool     `json:"passed"`
	Gate     string   `json:"gate"`               // "G0", "G1", etc. Empty if no gate applies.
	Missing  []string `json:"missing"`            // missing evidence types
	Failures []string `json:"failures,omitempty"` // content/session/freshness failures
}

// gateDefinition maps a gate to its required evidence types.
type gateDefinition struct {
	name      string
	from      Phase
	to        Phase
	required  []string
	freshness time.Duration // 0 means no freshness check (G0 has its own via isDiscoveryFresh)
}

// gates defines the gate requirements for each phase transition.
var gates = []gateDefinition{
	{"G0", PhaseInit, PhaseDiscover, []string{"recipe_review"}, 0},
	{"G1", PhaseDiscover, PhaseDevelop, []string{"discovery"}, 24 * time.Hour},
	{"G2", PhaseDevelop, PhaseDeploy, []string{"dev_verify"}, 24 * time.Hour},
	{"G3", PhaseDeploy, PhaseVerify, []string{"deploy_evidence"}, 24 * time.Hour},
	{"G4", PhaseVerify, PhaseDone, []string{"stage_verify"}, 24 * time.Hour},
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
	var failures []string
	for _, req := range gate.required {
		ev, err := LoadEvidence(evidenceDir, sessionID, req)
		if err != nil {
			missing = append(missing, req)
			continue
		}
		// Session binding: evidence must belong to current session.
		if ev.SessionID != "" && ev.SessionID != sessionID {
			failures = append(failures, fmt.Sprintf("%s: session mismatch (want %s, got %s)", req, sessionID, ev.SessionID))
			continue
		}
		// Freshness check: evidence must not be stale.
		if gate.freshness > 0 && ev.Timestamp != "" {
			if ts, err := time.Parse(time.RFC3339, ev.Timestamp); err == nil {
				if time.Since(ts) > gate.freshness {
					failures = append(failures, fmt.Sprintf("%s: stale (age %s, max %s)", req, time.Since(ts).Truncate(time.Minute), gate.freshness))
					continue
				}
			}
		}
		// Content validation.
		if err := ValidateEvidence(ev); err != nil {
			failures = append(failures, err.Error())
			continue
		}
		// Multi-service result validation.
		if errs := validateServiceResults(ev); len(errs) > 0 {
			failures = append(failures, errs...)
		}
	}

	return &GateResult{
		Passed:   len(missing) == 0 && len(failures) == 0,
		Gate:     gate.name,
		Missing:  missing,
		Failures: failures,
	}, nil
}

// ValidateEvidence checks that evidence content is structurally valid:
// no failures and non-empty attestation. Passed==0 && Failed==0 is acceptable
// (vacuous evidence from auto-complete of skipped steps).
func ValidateEvidence(ev *Evidence) error {
	if ev.Failed > 0 {
		return fmt.Errorf("evidence %s: has %d failure(s)", ev.Type, ev.Failed)
	}
	if ev.Attestation == "" {
		return fmt.Errorf("evidence %s: empty attestation", ev.Type)
	}
	return nil
}

// validateServiceResults checks per-service results within evidence.
// Returns a list of failure messages for any service with status "fail".
func validateServiceResults(ev *Evidence) []string {
	if len(ev.ServiceResults) == 0 {
		return nil
	}
	var errs []string
	for _, sr := range ev.ServiceResults {
		if sr.Status == "fail" {
			detail := sr.Hostname + ": failed"
			if sr.Detail != "" {
				detail = sr.Hostname + ": " + sr.Detail
			}
			errs = append(errs, fmt.Sprintf("evidence %s service %s", ev.Type, detail))
		}
	}
	return errs
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
