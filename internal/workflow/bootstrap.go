package workflow

import "fmt"

// BootstrapStep represents a single step in the bootstrap subflow.
type BootstrapStep struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pending, in_progress, complete, skipped
}

// BootstrapState tracks progress through the bootstrap subflow.
type BootstrapState struct {
	Active      bool            `json:"active"`
	CurrentStep int             `json:"currentStep"`
	Steps       []BootstrapStep `json:"steps"`
}

// bootstrapStepNames defines the 11 bootstrap steps in order.
var bootstrapStepNames = []string{
	"plan",
	"recipe-search",
	"generate-import",
	"import-services",
	"wait-services",
	"mount-dev",
	"create-files",
	"discover-services",
	"finalize",
	"spawn-subagents",
	"aggregate-results",
}

// stepGuidance provides guidance text for each bootstrap step.
var stepGuidance = map[string]string{
	"plan":              "Create a deployment plan based on the user's intent. Identify required services, runtimes, and configurations.",
	"recipe-search":     "Search the knowledge base for matching recipes and runtime configurations using zerops_knowledge.",
	"generate-import":   "Generate import.yml YAML based on the plan and recipe findings. Validate against runtime rules.",
	"import-services":   "Call zerops_import to create services from the generated YAML.",
	"wait-services":     "Wait for dev services to reach RUNNING. Stage services will be in READY_TO_DEPLOY — this is expected (they start on first deploy). Use zerops_process to check status.",
	"mount-dev":         "Mount only dev service filesystems using zerops_mount for code deployment. Stage services are not mounted.",
	"create-files":      "Create zerops.yml and application source files on the mounted dev service filesystem. Write files to the mount path (e.g., /var/www/appdev/). Use deployFiles: ./ in zerops.yml for dev services. Required files: zerops.yml (setup: entries must match ALL service hostnames), application source code, .gitignore. The deploy tool auto-initializes a git repo if missing — no manual git init needed.",
	"discover-services": "Run zerops_discover to verify all services are running and collect their details.",
	"finalize":          "Validate the deployment matches the plan. Record discovery evidence.",
	"spawn-subagents":   "STUBBED: Use the Task tool to create subagent tasks for parallel service configuration. Each service should get its own task with specific setup instructions.",
	"aggregate-results": "STUBBED: Use the Task tool to collect results from all subagent tasks. Verify all services are configured correctly and record final evidence.",
}

// NewBootstrapState creates a new bootstrap state with all 11 steps pending.
func NewBootstrapState() *BootstrapState {
	steps := make([]BootstrapStep, len(bootstrapStepNames))
	for i, name := range bootstrapStepNames {
		steps[i] = BootstrapStep{Name: name, Status: "pending"}
	}
	return &BootstrapState{
		Active:      true,
		CurrentStep: 0,
		Steps:       steps,
	}
}

// CurrentStepName returns the name of the current step, or empty if done.
func (b *BootstrapState) CurrentStepName() string {
	if b.CurrentStep >= len(b.Steps) {
		return ""
	}
	return b.Steps[b.CurrentStep].Name
}

// AdvanceStep returns the current step name, guidance, and whether the bootstrap is done.
// It marks the current step as in_progress.
func (b *BootstrapState) AdvanceStep() (stepName string, guidance string, done bool) {
	if !b.Active || b.CurrentStep >= len(b.Steps) {
		return "", "", true
	}

	step := &b.Steps[b.CurrentStep]
	step.Status = "in_progress"
	return step.Name, stepGuidance[step.Name], false
}

// MarkStepComplete marks a step as complete by name and advances CurrentStep.
func (b *BootstrapState) MarkStepComplete(name string) error {
	for i := range b.Steps {
		if b.Steps[i].Name == name {
			b.Steps[i].Status = "complete"
			// Advance to next step if this was the current step.
			if i == b.CurrentStep {
				b.CurrentStep++
			}
			// Deactivate if all steps are done.
			if b.CurrentStep >= len(b.Steps) {
				b.Active = false
			}
			return nil
		}
	}
	return fmt.Errorf("mark step complete: step %q not found", name)
}
