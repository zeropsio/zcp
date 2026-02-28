// Package workflow provides the workflow engine for orchestrating
// multi-step Zerops operations with phases, gates, and evidence.
package workflow

import "slices"

// Phase represents a workflow phase.
type Phase string

const (
	PhaseInit     Phase = "INIT"
	PhaseDiscover Phase = "DISCOVER"
	PhaseDevelop  Phase = "DEVELOP"
	PhaseDeploy   Phase = "DEPLOY"
	PhaseVerify   Phase = "VERIFY"
	PhaseDone     Phase = "DONE"
)

// ProjectState represents detected project state.
type ProjectState string

const (
	StateFresh         ProjectState = "FRESH"
	StateNonConformant ProjectState = "NON_CONFORMANT"
	StateConformant    ProjectState = "CONFORMANT"
)

// WorkflowState is the persistent state stored at .zcp/state/zcp_state.json.
type WorkflowState struct {
	Version   string                `json:"version"`
	SessionID string                `json:"sessionId"`
	ProjectID string                `json:"projectId"`
	Workflow  string                `json:"workflow"`
	Phase     Phase                 `json:"phase"`
	Iteration int                   `json:"iteration"`
	Intent    string                `json:"intent"`
	CreatedAt string                `json:"createdAt"`
	UpdatedAt string                `json:"updatedAt"`
	Services  map[string]ServiceRef `json:"services,omitempty"`
	History   []PhaseTransition     `json:"history"`
	Bootstrap *BootstrapState       `json:"bootstrap,omitempty"`
}

// ServiceRef is a lightweight service reference in workflow state.
type ServiceRef struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
}

// PhaseTransition records a phase change.
type PhaseTransition struct {
	From Phase  `json:"from"`
	To   Phase  `json:"to"`
	At   string `json:"at"`
}

// orchestratedPhases is the fixed phase sequence for orchestrated workflows (bootstrap, deploy).
var orchestratedPhases = []Phase{PhaseInit, PhaseDiscover, PhaseDevelop, PhaseDeploy, PhaseVerify, PhaseDone}

// PhaseSequence returns a copy of the orchestrated phase sequence.
func PhaseSequence() []Phase {
	result := make([]Phase, len(orchestratedPhases))
	copy(result, orchestratedPhases)
	return result
}

// ValidNextPhase returns the valid next phase from the current phase.
func ValidNextPhase(current Phase) []Phase {
	for i, p := range orchestratedPhases {
		if p == current && i+1 < len(orchestratedPhases) {
			return []Phase{orchestratedPhases[i+1]}
		}
	}
	return nil
}

// IsValidTransition checks if a phase transition is valid.
func IsValidTransition(from, to Phase) bool {
	return slices.Contains(ValidNextPhase(from), to)
}

// immediateWorkflows are stateless — no session, no phases, just guidance.
var immediateWorkflows = map[string]bool{
	"debug": true, "scale": true, "configure": true,
}

// IsImmediateWorkflow returns true if the workflow is stateless (no session, no phases).
func IsImmediateWorkflow(name string) bool {
	return immediateWorkflows[name]
}
