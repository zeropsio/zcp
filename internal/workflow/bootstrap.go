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
	"plan":              "Identify required services from user's request: runtime types + frameworks, managed services + versions, environment mode (standard dev+stage or simple). Verify types against available stacks. If unclear, ask the user.",
	"recipe-search":     "Load stack-specific knowledge: zerops_knowledge runtime=\"{type}\" services=[...]. Then load infrastructure rules: zerops_knowledge scope=\"infrastructure\". Both are MANDATORY before generating YAML.",
	"generate-import":   "Generate import.yml following infrastructure rules. Standard mode: {app}dev (startWithoutCode: true, maxContainers: 1) + {app}stage + shared managed services. Validate hostnames, modes, ports.",
	"import-services":   "Import services: zerops_import content=\"<yaml>\". Then poll: zerops_process processId=\"<id>\" until complete.",
	"wait-services":     "Wait for dev services to reach RUNNING. Stage stays in READY_TO_DEPLOY (expected — starts on first deploy). Use zerops_discover to check status.",
	"mount-dev":         "Mount ONLY dev service filesystems: zerops_mount action=\"mount\" serviceHostname=\"{devHostname}\" for each runtime dev service. Stage services are NOT mounted.",
	"create-files":      "Write zerops.yml + application code + .gitignore to mount path (e.g., /var/www/appdev/). App MUST have /, /health, /status endpoints — /status MUST prove connectivity to each managed service (SELECT 1 for DB, PING for cache). Use dev vs prod deploy differentiation: dev deploys source (deployFiles: [.]), prod deploys build output. Use ONLY discovered env vars in envVariables. For 2+ service pairs: skip — subagents handle this in spawn-subagents step.",
	"discover-services": "Discover ALL services with env vars: zerops_discover service=\"{hostname}\" includeEnvs=true for EACH managed service. Record exact env var names (connectionString, host, port, user, password, dbName). This data MUST be available before creating files or spawning subagents.",
	"finalize":          "Prepare subagent context: combine discovered env vars, loaded runtime knowledge, app specification, and zerops.yml template into the Service Bootstrap Agent Prompt. For inline deployment (1 service pair): verify files are ready for deploy.",
	"spawn-subagents":   "Spawn one general-purpose agent per runtime service pair with the Service Bootstrap Agent Prompt from the workflow guide. Each agent handles FULL lifecycle: write code, deploy dev, verify (with iteration loop), deploy stage, verify. All agents run in parallel.",
	"aggregate-results": "Collect results from all subagents. Independently verify: zerops_discover for each runtime service (must be RUNNING). Check zeropsSubdomain URLs respond. Present final results: dev + stage URLs for each service pair.",
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
