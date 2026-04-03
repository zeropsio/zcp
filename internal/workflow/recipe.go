package workflow

import (
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/knowledge"
)

// WorkflowRecipe is the workflow name for recipe sessions.
const WorkflowRecipe = "recipe"

// Recipe tier constants.
const (
	RecipeTierMinimal  = "minimal"  // type 3
	RecipeTierShowcase = "showcase" // type 4
)

// RecipeState tracks progress through the recipe workflow.
type RecipeState struct {
	Active            bool                `json:"active"`
	CurrentStep       int                 `json:"currentStep"`
	Steps             []RecipeStep        `json:"steps"`
	Plan              *RecipePlan         `json:"plan,omitempty"`
	DiscoveredEnvVars map[string][]string `json:"discoveredEnvVars,omitempty"`
	OutputDir         string              `json:"outputDir,omitempty"`
}

// RecipeStep represents a single step in the recipe workflow.
type RecipeStep struct {
	Name        string `json:"name"`
	Status      string `json:"status"` // pending, in_progress, complete, skipped
	Attestation string `json:"attestation,omitempty"`
	SkipReason  string `json:"skipReason,omitempty"`
	CompletedAt string `json:"completedAt,omitempty"`
}

// RecipePlan holds the structured research output for recipe creation.
type RecipePlan struct {
	Framework   string          `json:"framework"`
	Tier        string          `json:"tier"`
	Slug        string          `json:"slug"`
	RuntimeType string          `json:"runtimeType"`
	BuildBases  []string        `json:"buildBases"`
	Decisions   DecisionResults `json:"decisions"`
	Research    ResearchData    `json:"research"`
	Targets     []RecipeTarget  `json:"targets"`
	CreatedAt   string          `json:"createdAt,omitempty"`
}

// RecipeTarget defines a service in the recipe workspace.
type RecipeTarget struct {
	Hostname     string   `json:"hostname"`
	Type         string   `json:"type"`
	Role         string   `json:"role"`         // app, worker, db, cache, etc.
	Environments []string `json:"environments"` // which envs include this service
}

// DecisionResults holds the 4 recipe decision tree outputs.
type DecisionResults struct {
	WebServer  string `json:"webServer"`  // builtin, nginx-sidecar, nginx-proxy
	BuildBase  string `json:"buildBase"`  // runtime type for build phase
	OS         string `json:"os"`         // ubuntu-22, alpine
	DevTooling string `json:"devTooling"` // hot-reload, watch, manual
}

// ResearchData holds the framework research fields.
type ResearchData struct {
	// Framework identity.
	ServiceType    string `json:"serviceType"`
	PackageManager string `json:"packageManager"`
	HTTPPort       int    `json:"httpPort"`
	// Build & deploy pipeline.
	BuildCommands []string `json:"buildCommands"`
	DeployFiles   []string `json:"deployFiles"`
	StartCommand  string   `json:"startCommand"`
	CacheStrategy []string `json:"cacheStrategy"`
	// Database & migration.
	DBDriver     string `json:"dbDriver"`
	MigrationCmd string `json:"migrationCmd"`
	SeedCmd      string `json:"seedCmd,omitempty"`
	// Environment & secrets.
	NeedsAppSecret bool   `json:"needsAppSecret"`
	LoggingDriver  string `json:"loggingDriver"`
	// Showcase-only fields.
	CacheLib      string `json:"cacheLib,omitempty"`
	SessionDriver string `json:"sessionDriver,omitempty"`
	QueueDriver   string `json:"queueDriver,omitempty"`
	StorageDriver string `json:"storageDriver,omitempty"`
	SearchLib     string `json:"searchLib,omitempty"`
	MailLib       string `json:"mailLib,omitempty"`
}

// RecipeResponse is returned from recipe workflow actions.
type RecipeResponse struct {
	SessionID       string           `json:"sessionId"`
	Intent          string           `json:"intent"`
	Iteration       int              `json:"iteration"`
	Message         string           `json:"message"`
	Progress        RecipeProgress   `json:"progress"`
	Current         *RecipeStepInfo  `json:"current,omitempty"`
	CheckResult     *StepCheckResult `json:"checkResult,omitempty"`
	OutputDir       string           `json:"outputDir,omitempty"`
	AvailableStacks string           `json:"availableStacks,omitempty"`
	SchemaKnowledge string           `json:"schemaKnowledge,omitempty"`
}

// RecipeProgress summarizes overall recipe progress.
type RecipeProgress struct {
	Total     int                 `json:"total"`
	Completed int                 `json:"completed"`
	Steps     []RecipeStepSummary `json:"steps"`
}

// RecipeStepSummary is a lightweight step summary for progress display.
type RecipeStepSummary struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// RecipeStepInfo provides detailed info about the current recipe step.
type RecipeStepInfo struct {
	Name          string       `json:"name"`
	Index         int          `json:"index"`
	Tools         []string     `json:"tools"`
	Verification  string       `json:"verification"`
	DetailedGuide string       `json:"detailedGuide,omitempty"`
	PriorContext  *StepContext `json:"priorContext,omitempty"`
}

// NewRecipeState creates a new recipe state with all 6 steps pending.
func NewRecipeState() *RecipeState {
	steps := make([]RecipeStep, len(recipeStepDetails))
	for i, d := range recipeStepDetails {
		steps[i] = RecipeStep{Name: d.Name, Status: stepPending}
	}
	return &RecipeState{
		Active:      true,
		CurrentStep: 0,
		Steps:       steps,
	}
}

// CurrentStepName returns the name of the current step, or empty if done.
func (r *RecipeState) CurrentStepName() string {
	if r.CurrentStep >= len(r.Steps) {
		return ""
	}
	return r.Steps[r.CurrentStep].Name
}

// CompleteStep validates and completes the current step with an attestation.
func (r *RecipeState) CompleteStep(name, attestation string) error {
	if !r.Active {
		return fmt.Errorf("recipe complete step: not active")
	}
	if r.CurrentStep >= len(r.Steps) {
		return fmt.Errorf("recipe complete step: all steps done")
	}
	current := r.Steps[r.CurrentStep].Name
	if name != current {
		return fmt.Errorf("recipe complete step: expected %q (current), got %q", current, name)
	}
	if len(attestation) < minAttestationLen {
		return fmt.Errorf("recipe complete step: attestation too short (min %d chars)", minAttestationLen)
	}

	r.Steps[r.CurrentStep].Status = stepComplete
	r.Steps[r.CurrentStep].Attestation = attestation
	r.Steps[r.CurrentStep].CompletedAt = time.Now().UTC().Format(time.RFC3339)
	r.CurrentStep++

	if r.CurrentStep < len(r.Steps) {
		r.Steps[r.CurrentStep].Status = stepInProgress
	} else {
		r.Active = false
	}
	return nil
}

// SkipStep validates and skips the current step with a reason.
// Only the close step is skippable in recipe workflow.
func (r *RecipeState) SkipStep(name, reason string) error {
	if !r.Active {
		return fmt.Errorf("recipe skip step: not active")
	}
	if r.CurrentStep >= len(r.Steps) {
		return fmt.Errorf("recipe skip step: all steps done")
	}
	current := r.Steps[r.CurrentStep].Name
	if name != current {
		return fmt.Errorf("recipe skip step: expected %q (current), got %q", current, name)
	}
	if name != RecipeStepClose {
		return fmt.Errorf("recipe skip step: %q is mandatory and cannot be skipped", name)
	}

	r.Steps[r.CurrentStep].Status = stepSkipped
	r.Steps[r.CurrentStep].SkipReason = reason
	r.Steps[r.CurrentStep].CompletedAt = time.Now().UTC().Format(time.RFC3339)
	r.CurrentStep++

	if r.CurrentStep < len(r.Steps) {
		r.Steps[r.CurrentStep].Status = stepInProgress
	} else {
		r.Active = false
	}
	return nil
}

// ResetForIteration resets generate+deploy+finalize steps for a new iteration cycle.
// Preserves: research, provision, close.
func (r *RecipeState) ResetForIteration() {
	if r == nil {
		return
	}
	firstReset := -1
	for i := range r.Steps {
		name := r.Steps[i].Name
		if name == RecipeStepGenerate || name == RecipeStepDeploy || name == RecipeStepFinalize {
			r.Steps[i] = RecipeStep{Name: name, Status: stepPending}
			if firstReset < 0 {
				firstReset = i
			}
		}
	}
	if firstReset >= 0 {
		r.CurrentStep = firstReset
		r.Steps[firstReset].Status = stepInProgress
	}
	r.Active = true
}

// BuildResponse constructs a RecipeResponse from the current state.
func (r *RecipeState) BuildResponse(sessionID, intent string, iteration int, env Environment, kp knowledge.Provider) *RecipeResponse {
	completed := 0
	summaries := make([]RecipeStepSummary, len(r.Steps))
	for i, s := range r.Steps {
		summaries[i] = RecipeStepSummary{Name: s.Name, Status: s.Status}
		if s.Status == stepComplete || s.Status == stepSkipped {
			completed++
		}
	}

	resp := &RecipeResponse{
		SessionID: sessionID,
		Intent:    intent,
		Iteration: iteration,
		Progress: RecipeProgress{
			Total:     len(r.Steps),
			Completed: completed,
			Steps:     summaries,
		},
	}

	if r.CurrentStep < len(r.Steps) {
		detail := lookupRecipeDetail(r.Steps[r.CurrentStep].Name)
		resp.Current = &RecipeStepInfo{
			Name:         detail.Name,
			Index:        r.CurrentStep,
			Tools:        detail.Tools,
			Verification: detail.Verification,
			PriorContext: r.buildPriorContext(),
		}
		resp.Current.DetailedGuide = r.buildGuide(detail.Name, iteration, kp)
		resp.Message = fmt.Sprintf("Recipe step %d/%d: %s", r.CurrentStep+1, len(r.Steps), detail.Name)

		// Only expose outputDir during finalize/close — earlier steps write to
		// mounted service filesystems, not the recipe output directory.
		if detail.Name == RecipeStepFinalize || detail.Name == RecipeStepClose {
			resp.OutputDir = r.OutputDir
		}
	} else {
		// Recipe complete — include outputDir for post-completion reference.
		resp.OutputDir = r.OutputDir
		resp.Message = "Recipe complete. All steps finished."
	}

	return resp
}

// buildPriorContext collects attestations from completed prior steps and the plan.
func (r *RecipeState) buildPriorContext() *StepContext {
	attestations := make(map[string]string)
	for i := 0; i < r.CurrentStep && i < len(r.Steps); i++ {
		if r.Steps[i].Attestation == "" {
			continue
		}
		if i == r.CurrentStep-1 {
			attestations[r.Steps[i].Name] = r.Steps[i].Attestation
		} else {
			att := r.Steps[i].Attestation
			if len([]rune(att)) > 80 {
				att = string([]rune(att)[:77]) + "..."
			}
			attestations[r.Steps[i].Name] = fmt.Sprintf("[%s: %s]", r.Steps[i].Status, att)
		}
	}

	if len(attestations) == 0 && r.Plan == nil {
		return nil
	}

	return &StepContext{
		Attestations: attestations,
	}
}

// lookupRecipeDetail finds the recipe StepDetail for a step name.
func lookupRecipeDetail(name string) StepDetail {
	for _, d := range recipeStepDetails {
		if d.Name == name {
			return d
		}
	}
	return StepDetail{}
}
