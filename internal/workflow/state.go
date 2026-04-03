// Package workflow provides the workflow engine for orchestrating
// multi-step Zerops operations with step-based progression and checkers.
package workflow

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
	Recipe    *RecipeState    `json:"recipe,omitempty"`
	Export    *ExportState    `json:"export,omitempty"`
}

// ExportState holds state for the export workflow.
type ExportState struct {
	Step         string   `json:"step"`
	Intent       string   `json:"intent"`       // A=CI/CD, B=buildFromGit, C=both
	ServiceState string   `json:"serviceState"` // S0, S1, S2
	Hostname     string   `json:"hostname"`
	RepoURL      string   `json:"repoUrl,omitempty"`
	Branch       string   `json:"branch,omitempty"`
	Services     []string `json:"services,omitempty"`
}

// immediateWorkflows are stateless — no session, just guidance.
var immediateWorkflows = map[string]bool{
	"cicd": true,
}

// IsImmediateWorkflow returns true if the workflow is stateless (no session).
func IsImmediateWorkflow(name string) bool {
	return immediateWorkflows[name]
}
