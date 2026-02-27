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

// Mode represents a workflow mode.
type Mode string

const (
	ModeFull    Mode = "full"
	ModeDevOnly Mode = "dev_only"
	ModeHotfix  Mode = "hotfix"
	ModeQuick   Mode = "quick"
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
	Mode      Mode                  `json:"mode"`
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

// phaseSequences defines the ordered phases for each mode.
var phaseSequences = map[Mode][]Phase{
	ModeFull:    {PhaseInit, PhaseDiscover, PhaseDevelop, PhaseDeploy, PhaseVerify, PhaseDone},
	ModeDevOnly: {PhaseInit, PhaseDiscover, PhaseDevelop, PhaseDone},
	ModeHotfix:  {PhaseInit, PhaseDevelop, PhaseDeploy, PhaseVerify, PhaseDone},
	ModeQuick:   {PhaseInit, PhaseDevelop, PhaseDeploy, PhaseVerify, PhaseDone},
}

// PhaseSequence returns the ordered phase sequence for a mode.
// Returns nil for ModeQuick (no phases).
func PhaseSequence(mode Mode) []Phase {
	seq, ok := phaseSequences[mode]
	if !ok {
		return nil
	}
	// Return a copy to prevent mutation.
	result := make([]Phase, len(seq))
	copy(result, seq)
	return result
}

// ValidNextPhase returns the valid next phases from the current phase in the given mode.
// Returns nil if the current phase is terminal or not in the mode's sequence.
func ValidNextPhase(current Phase, mode Mode) []Phase {
	seq := phaseSequences[mode]
	if len(seq) == 0 {
		return nil
	}
	for i, p := range seq {
		if p == current && i+1 < len(seq) {
			return []Phase{seq[i+1]}
		}
	}
	return nil
}

// IsValidTransition checks if a phase transition is valid for the given mode.
func IsValidTransition(from, to Phase, mode Mode) bool {
	return slices.Contains(ValidNextPhase(from, mode), to)
}
