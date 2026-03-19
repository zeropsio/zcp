// Package workflow provides the workflow engine for orchestrating
// multi-step Zerops operations with step-based progression and checkers.
package workflow

// ProjectState represents detected project state.
type ProjectState string

const (
	StateFresh         ProjectState = "FRESH"
	StateNonConformant ProjectState = "NON_CONFORMANT"
	StateConformant    ProjectState = "CONFORMANT"
	StateUnknown       ProjectState = "UNKNOWN"
)

// WorkflowState is the persistent state stored at .zcp/state/sessions/{sessionID}.json.
type WorkflowState struct {
	Version   string          `json:"version"`
	SessionID string          `json:"sessionId"`
	PID       int             `json:"pid"`
	ProjectID string          `json:"projectId"`
	Workflow  string          `json:"workflow"`
	Iteration int             `json:"iteration"`
	Intent    string          `json:"intent"`
	CreatedAt string          `json:"createdAt"`
	UpdatedAt string          `json:"updatedAt"`
	Bootstrap *BootstrapState `json:"bootstrap,omitempty"`
	Deploy    *DeployState    `json:"deploy,omitempty"`
	CICD      *CICDState      `json:"cicd,omitempty"`
}

// immediateWorkflows are stateless — no session, just guidance.
var immediateWorkflows = map[string]bool{
	"debug": true, "scale": true, "configure": true,
}

// IsImmediateWorkflow returns true if the workflow is stateless (no session).
func IsImmediateWorkflow(name string) bool {
	return immediateWorkflows[name]
}
