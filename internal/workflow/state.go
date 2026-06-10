// Package workflow provides the workflow engine for orchestrating
// multi-step Zerops operations with step-based progression and checkers.
package workflow

// WorkflowState is the persistent state stored at .zcp/state/sessions/{sessionID}.json.
type WorkflowState struct {
	Version   string          `json:"version"`
	SessionID string          `json:"sessionId"`
	PID       int             `json:"pid"`
	StartTime string          `json:"startTime,omitempty"` // (pid,startTime) identity for recycled-PID detection
	ProjectID string          `json:"projectId"`
	Workflow  string          `json:"workflow"`
	Iteration int             `json:"iteration"`
	Intent    string          `json:"intent"`
	CreatedAt string          `json:"createdAt"`
	UpdatedAt string          `json:"updatedAt"`
	Bootstrap *BootstrapState `json:"bootstrap,omitempty"`
	Recipe    *RecipeState    `json:"recipe,omitempty"`
}
