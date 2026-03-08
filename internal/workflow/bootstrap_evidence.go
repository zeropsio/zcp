package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// bootstrapEvidenceMap maps evidence types to the bootstrap steps that contribute to them.
var bootstrapEvidenceMap = map[string][]string{
	"recipe_review":   {"discover"},
	"discovery":       {"provision"},
	"dev_verify":      {"generate", "deploy", "verify"},
	"deploy_evidence": {"deploy"},
	"stage_verify":    {"verify"},
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

	// Best-effort: write service meta and reflog. Errors are logged but don't fail bootstrap.
	e.writeBootstrapOutputs(state)

	// Immediately unregister DONE sessions from the registry.
	if err := UnregisterSession(e.stateDir, state.SessionID); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: unregister completed session: %v\n", err)
	}

	return nil
}

// writeBootstrapOutputs writes service meta files and appends a reflog entry.
// Both are best-effort — errors are logged to stderr but don't fail bootstrap completion.
func (e *Engine) writeBootstrapOutputs(state *WorkflowState) {
	if state.Bootstrap == nil || state.Bootstrap.Plan == nil {
		return
	}

	plan := state.Bootstrap.Plan
	now := time.Now().UTC().Format("2006-01-02")

	// Write service meta for each target and its dependencies.
	for _, target := range plan.Targets {
		depHostnames := make([]string, 0, len(target.Dependencies))
		for _, dep := range target.Dependencies {
			depHostnames = append(depHostnames, dep.Hostname)

			// Skip meta overwrite for pre-existing or shared dependencies.
			if dep.Resolution == "EXISTS" || dep.Resolution == "SHARED" {
				continue
			}

			depMeta := &ServiceMeta{
				Hostname:         dep.Hostname,
				Type:             dep.Type,
				Mode:             dep.Mode,
				BootstrapSession: state.SessionID,
				BootstrappedAt:   now,
			}
			if err := WriteServiceMeta(e.stateDir, depMeta); err != nil {
				fmt.Fprintf(os.Stderr, "zcp: write service meta %s: %v\n", dep.Hostname, err)
			}
		}

		meta := &ServiceMeta{
			Hostname:         target.Runtime.DevHostname,
			Type:             target.Runtime.Type,
			StageHostname:    target.Runtime.StageHostname(),
			Dependencies:     depHostnames,
			BootstrapSession: state.SessionID,
			BootstrappedAt:   now,
		}
		if target.Runtime.Simple {
			meta.Mode = PlanModeSimple
		} else {
			meta.Mode = PlanModeStandard
		}
		if err := WriteServiceMeta(e.stateDir, meta); err != nil {
			fmt.Fprintf(os.Stderr, "zcp: write service meta %s: %v\n", target.Runtime.DevHostname, err)
		}
	}

	// Derive project root from stateDir (expected: {projectRoot}/.zcp/state/).
	projectRoot := filepath.Dir(filepath.Dir(e.stateDir))
	claudeMDPath := filepath.Join(projectRoot, "CLAUDE.md")

	if err := AppendReflogEntry(claudeMDPath, state.Intent, plan.Targets, state.SessionID, now); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: append reflog: %v\n", err)
	}
}
