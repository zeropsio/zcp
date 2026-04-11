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
	RecipeTierHelloWorld = "hello-world" // type 1-2 (runtime and frontend hello-worlds)
	RecipeTierMinimal    = "minimal"     // type 3
	RecipeTierShowcase   = "showcase"    // type 4
)

const recipeDBNone = "none"

// RecipeState tracks progress through the recipe workflow.
type RecipeState struct {
	Active            bool                `json:"active"`
	Tier              string              `json:"tier"`
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
	// Agent-authored comments baked into import.yaml at generate-finalize time.
	// Keyed by env index as string ("0".."5"). Each env carries its own service
	// and project comments — envs differ (dev workspace vs small-prod vs HA prod)
	// and the commentary has to match, so the agent writes one set per env.
	EnvComments map[string]EnvComments `json:"envComments,omitempty"`
	// Agent-authored project-level env vars baked into each env's import.yaml
	// project.envVariables block at generate-finalize time. Keyed by env index
	// ("0".."5"), value is a flat map of env var name → value. Values are
	// emitted verbatim — interpolation markers like ${zeropsSubdomainHost} are
	// preserved so the platform resolves them at project import time.
	//
	// Different envs can carry different maps: envs 0-1 (dev/stage pair)
	// typically carry DEV_* and STAGE_* URL constants; envs 2-5 (single-slot)
	// carry STAGE_* only. The agent owns the per-env shape — the template
	// renders each env's map verbatim in sorted key order.
	ProjectEnvVariables map[string]map[string]string `json:"projectEnvVariables,omitempty"`
}

// EnvComments holds the agent-authored comments for a single environment's
// import.yaml file. Service keys match the service entries that appear in
// THAT file — in envs 0-1 the runtime pair gives two keys ("appdev", "appstage"),
// in envs 2-5 it collapses to one ("app").
type EnvComments struct {
	// Service maps a service key (as it appears in this env's import.yaml) to
	// the comment block emitted above that service entry.
	Service map[string]string `json:"service,omitempty"`
	// Project is the comment emitted above the project: block. Can differ per
	// env (e.g. env 5 explains corePackage: SERIOUS alongside the shared secret
	// rationale, other envs only carry the secret rationale).
	Project string `json:"project,omitempty"`
}

// RecipeTarget defines a service in the recipe workspace. Template dispatch
// uses type-capability predicates (IsRuntimeType, IsManagedService, etc.).
// The Role field is for repo routing and comment generation only — it does NOT
// affect template dispatch.
type RecipeTarget struct {
	Hostname string `json:"hostname"           jsonschema:"Service hostname — lowercase alphanumeric, no hyphens or underscores (e.g. 'app', 'db', 'cache')."`
	Type     string `json:"type"               jsonschema:"Zerops service type with version — pick the highest available version from the live catalog for each stack. Must exist in the live catalog."`
	IsWorker bool   `json:"isWorker,omitempty" jsonschema:"Only meaningful for runtime types — set true for background/queue workers, false (default) for the HTTP-serving primary app. Ignored for managed/utility types (their rendering is fully determined by type)."`
	Role     string `json:"role,omitempty"     jsonschema:"Service role for repo routing: 'app' (frontend/default), 'api' (backend API), 'worker' (background processor). Empty for managed/utility services. Does NOT affect template dispatch — type predicates remain authoritative."`
	// SharesCodebaseWith names another (non-worker) runtime target whose
	// codebase this worker shares — one app, two processes. Only meaningful
	// for workers. Empty (DEFAULT) means the worker is a SEPARATE codebase:
	// its own repo, its own zerops.yaml, its own dev+stage pair. Non-empty
	// means shared: no workerdev, the worker runs as a `setup: worker`
	// block in the host target's zerops.yaml, buildFromGit inherits the
	// host target's repo. Validation enforces: the referenced hostname
	// exists, is a non-worker runtime target, and has a base runtime that
	// matches this worker's base runtime (cross-language sharing is invalid).
	//
	// Worker codebase decision is a first-class research-step decision:
	// separate is the default because most mature architectures deploy
	// workers from their own repo; opt into sharing ONLY when the framework's
	// queue library is tightly bound to the app boundary (Laravel Horizon,
	// Rails Sidekiq, Django Celery in-process).
	SharesCodebaseWith string   `json:"sharesCodebaseWith,omitempty" jsonschema:"OPTIONAL — only for workers (isWorker=true). Hostname of the non-worker runtime target whose codebase this worker shares (one repo, two processes). Empty (default) = separate codebase, own repo, own zerops.yaml, own dev+stage pair. Set to the app/api hostname ONLY when the framework's queue library runs as an in-process entry point of the app (Laravel Horizon, Rails Sidekiq, Django+Celery). Must reference an existing non-worker runtime target whose base runtime matches this worker's base runtime."`
	Environments       []string `json:"environments,omitempty"` // ignored — all targets appear in all environments
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
	AppSecretKey   string `json:"appSecretKey,omitempty"` // e.g. "APP_KEY", "SECRET_KEY_BASE" — framework-specific name
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
	PrettyName      string           `json:"prettyName,omitempty"`
	Progress        RecipeProgress   `json:"progress"`
	Current         *RecipeStepInfo  `json:"current,omitempty"`
	CheckResult     *StepCheckResult `json:"checkResult,omitempty"`
	OutputDir       string           `json:"outputDir,omitempty"`
	AvailableStacks string           `json:"availableStacks,omitempty"`
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

	// Expose derived pretty name so the agent can use it in titles and READMEs.
	if r.Plan != nil {
		resp.PrettyName = recipePrettyName(r.Plan.Slug, r.Plan.Framework)
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

		// Finalize step: files were auto-generated when deploy completed.
		if detail.Name == RecipeStepFinalize && r.OutputDir != "" {
			resp.Message += fmt.Sprintf(". Recipe files auto-generated in %s — add framework-specific comments to each import.yaml (structure/scaling are final, 30%% comment ratio check will enforce)", r.OutputDir)
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
