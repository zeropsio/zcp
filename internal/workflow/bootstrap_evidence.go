package workflow

import (
	"fmt"
	"strings"
	"time"
)

// bootstrapEvidenceMap maps evidence types to the bootstrap steps that contribute to them.
var bootstrapEvidenceMap = map[string][]string{
	"recipe_review":   {"detect", "plan", "load-knowledge"},
	"discovery":       {"discover-envs"},
	"dev_verify":      {"generate-code", "deploy", "verify"},
	"deploy_evidence": {"deploy"},
	"stage_verify":    {"verify", "report"},
}

// autoCompleteBootstrap records evidence and transitions through all phases to DONE.
func (e *Engine) autoCompleteBootstrap(state *WorkflowState) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// Collect attestations and step statuses for evidence generation.
	attestations := make(map[string]string)
	stepStatus := make(map[string]string)
	for _, step := range state.Bootstrap.Steps {
		if step.Attestation != "" {
			attestations[step.Name] = step.Attestation
		}
		stepStatus[step.Name] = step.Status
	}

	for evType, steps := range bootstrapEvidenceMap {
		var parts []string
		passed := 0
		for _, s := range steps {
			if a, ok := attestations[s]; ok {
				parts = append(parts, s+": "+a)
			}
			if stepStatus[s] == stepComplete {
				passed++
			}
		}
		attestation := "auto-recorded from bootstrap steps"
		if len(parts) > 0 {
			attestation = strings.Join(parts, "; ")
		}
		ev := &Evidence{
			SessionID:        state.SessionID,
			Timestamp:        now,
			VerificationType: "attestation",
			Attestation:      attestation,
			Type:             evType,
			Passed:           passed,
		}

		if err := SaveEvidence(e.evidenceDir, state.SessionID, ev); err != nil {
			return fmt.Errorf("auto-evidence %s: %w", evType, err)
		}
	}

	// Transition through all phases, checking gates at each step.
	seq := PhaseSequence()
	for i := 1; i < len(seq); i++ {
		result, err := CheckGate(seq[i-1], seq[i], e.evidenceDir, state.SessionID)
		if err != nil {
			return fmt.Errorf("auto-complete gate %s→%s: %w", seq[i-1], seq[i], err)
		}
		if !result.Passed {
			return fmt.Errorf("auto-complete blocked at gate %s: missing=%v failures=%v",
				result.Gate, result.Missing, result.Failures)
		}
		state.History = append(state.History, PhaseTransition{
			From: seq[i-1],
			To:   seq[i],
			At:   now,
		})
	}
	if len(seq) > 0 {
		state.Phase = seq[len(seq)-1]
	}

	return nil
}
