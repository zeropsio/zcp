package workflow

import (
	"fmt"
	"time"
)

// Evidence type constants used in gate definitions and evidence files.
const (
	EvidenceRecipeReview   = "recipe_review"
	EvidenceDiscovery      = "discovery"
	EvidenceDevVerify      = "dev_verify"
	EvidenceDeployEvidence = "deploy_evidence"
	EvidenceStageVerify    = "stage_verify"
)

// RemediationStep provides actionable guidance for resolving a gate failure.
type RemediationStep struct {
	Action      string `json:"action"`
	Tool        string `json:"tool"`
	Params      string `json:"params"`
	Explanation string `json:"explanation"`
}

// GateResult holds the result of a gate check.
type GateResult struct {
	Passed      bool              `json:"passed"`
	Gate        string            `json:"gate"`                  // "G0", "G1", etc. Empty if no gate applies.
	Missing     []string          `json:"missing"`               // missing evidence types
	Failures    []string          `json:"failures,omitempty"`    // content/session/freshness failures
	Remediation []RemediationStep `json:"remediation,omitempty"` // actionable steps to resolve failures
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
	{"G0", PhaseInit, PhaseDiscover, []string{EvidenceRecipeReview}, 0},
	{"G1", PhaseDiscover, PhaseDevelop, []string{EvidenceDiscovery}, 24 * time.Hour},
	{"G2", PhaseDevelop, PhaseDeploy, []string{EvidenceDevVerify}, 24 * time.Hour},
	{"G3", PhaseDeploy, PhaseVerify, []string{EvidenceDeployEvidence}, 24 * time.Hour},
	{"G4", PhaseVerify, PhaseDone, []string{EvidenceStageVerify}, 24 * time.Hour},
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
// mode is the aggregate plan mode (empty or "standard" = all gates apply;
// "dev", "simple", or "mixed" = G4 skipped since no stage services exist).
func CheckGate(from, to Phase, evidenceDir, sessionID, mode string) (*GateResult, error) {
	// Validate the transition is valid.
	if !IsValidTransition(from, to) {
		return nil, fmt.Errorf("check gate: invalid transition %s → %s", from, to)
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

	// G4 skip for non-standard modes: dev/simple/mixed have no stage services.
	// Positive allowlist — unknown/corrupt mode strings fall through to evidence check.
	if gate.name == "G4" && (mode == PlanModeDev || mode == PlanModeSimple || mode == "mixed") {
		return &GateResult{Passed: true, Gate: "G4"}, nil
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

	passed := len(missing) == 0 && len(failures) == 0
	var remediation []RemediationStep
	if !passed {
		for _, m := range missing {
			remediation = append(remediation, remediationForEvidence(m))
		}
	}

	return &GateResult{
		Passed:      passed,
		Gate:        gate.name,
		Missing:     missing,
		Failures:    failures,
		Remediation: remediation,
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

// remediationForEvidence returns a remediation step for a missing evidence type.
func remediationForEvidence(evidenceType string) RemediationStep {
	switch evidenceType {
	case EvidenceRecipeReview:
		return RemediationStep{
			Action:      "record_evidence",
			Tool:        "zerops_workflow",
			Params:      `action="evidence" type="recipe_review"`,
			Explanation: "Review relevant recipes via zerops_knowledge, then record evidence",
		}
	case EvidenceDiscovery:
		return RemediationStep{
			Action:      "run_discovery",
			Tool:        "zerops_discover",
			Params:      `includeEnvs=true`,
			Explanation: "Discover project services and environment variables",
		}
	case EvidenceDevVerify:
		return RemediationStep{
			Action:      "verify_dev",
			Tool:        "zerops_verify",
			Params:      `serviceHostname="{hostname}"`,
			Explanation: "Verify dev service is healthy and responding",
		}
	case EvidenceDeployEvidence:
		return RemediationStep{
			Action:      "deploy_services",
			Tool:        "zerops_deploy",
			Params:      `targetService="{hostname}"`,
			Explanation: "Deploy services to stage environment",
		}
	case EvidenceStageVerify:
		return RemediationStep{
			Action:      "verify_stage",
			Tool:        "zerops_verify",
			Params:      `serviceHostname="{hostname}"`,
			Explanation: "Verify stage service is healthy and responding",
		}
	default:
		return RemediationStep{
			Action:      "record_evidence",
			Tool:        "zerops_workflow",
			Params:      fmt.Sprintf(`action="evidence" type="%s"`, evidenceType),
			Explanation: fmt.Sprintf("Record %s evidence", evidenceType),
		}
	}
}

// isDiscoveryFresh checks if discovery.json exists and was created within 24h.
func isDiscoveryFresh(evidenceDir, sessionID string) bool {
	ev, err := LoadEvidence(evidenceDir, sessionID, EvidenceDiscovery)
	if err != nil {
		return false
	}
	ts, err := time.Parse(time.RFC3339, ev.Timestamp)
	if err != nil {
		return false
	}
	return time.Since(ts) < discoveryFreshDuration
}
