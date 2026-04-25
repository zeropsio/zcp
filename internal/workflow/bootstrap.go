package workflow

import (
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/topology"
)

// StepDetail defines a bootstrap step's metadata.
type StepDetail struct {
	Name         string   `json:"name"`
	Tools        []string `json:"tools"`
	Verification string   `json:"verification"`
}

// BootstrapStep represents a single step in the bootstrap subflow.
type BootstrapStep struct {
	Name        string `json:"name"`
	Status      string `json:"status"` // pending, in_progress, complete, skipped
	Attestation string `json:"attestation,omitempty"`
	SkipReason  string `json:"skipReason,omitempty"`
	CompletedAt string `json:"completedAt,omitempty"`
}

// BootstrapState tracks progress through the bootstrap subflow.
//
// Route records the path chosen at bootstrap start (recipe when the intent
// matches a viable recipe; classic otherwise). Adopt is NOT set here — the
// adopt path is inferred dynamically from the Plan once the discover step
// completes. Route is empty for bootstrap sessions created before the
// route-aware conductor shipped; the envelope synthesizer falls back to
// Plan-based inference when Route is empty.
type BootstrapState struct {
	Active            bool                `json:"active"`
	CurrentStep       int                 `json:"currentStep"`
	Steps             []BootstrapStep     `json:"steps"`
	Plan              *ServicePlan        `json:"plan,omitempty"`
	DiscoveredEnvVars map[string][]string `json:"discoveredEnvVars,omitempty"`
	Route             BootstrapRoute      `json:"route,omitempty"`
	RecipeMatch       *RecipeMatch        `json:"recipeMatch,omitempty"`
}

// BootstrapResponse is returned from conductor actions.
type BootstrapResponse struct {
	SessionID       string             `json:"sessionId"`
	Intent          string             `json:"intent"`
	Progress        BootstrapProgress  `json:"progress"`
	Current         *BootstrapStepInfo `json:"current,omitempty"`
	Message         string             `json:"message"`
	AvailableStacks string             `json:"availableStacks,omitempty"`
	CheckResult     *StepCheckResult   `json:"checkResult,omitempty"`
	AutoMounts      []AutoMountInfo    `json:"autoMounts,omitempty"`
}

// BootstrapDiscoveryResponse is returned from BootstrapDiscover. Discovery
// does not create a session — it inspects project state and returns a ranked
// list of route options for the LLM to pick from. The session is only
// committed when the LLM follows up with an explicit `route` parameter.
type BootstrapDiscoveryResponse struct {
	Intent       string                 `json:"intent,omitempty"`
	ProjectID    string                 `json:"projectId"`
	RouteOptions []BootstrapRouteOption `json:"routeOptions"`
	Message      string                 `json:"message"`
}

// AutoMountInfo reports the result of auto-mounting a service after provision.
type AutoMountInfo struct {
	Hostname  string `json:"hostname"`
	MountPath string `json:"mountPath,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

// BootstrapProgress summarizes overall bootstrap progress.
type BootstrapProgress struct {
	Total     int                    `json:"total"`
	Completed int                    `json:"completed"`
	Steps     []BootstrapStepSummary `json:"steps"`
}

// BootstrapStepSummary is a lightweight step summary for progress display.
type BootstrapStepSummary struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// StepContext provides context from prior bootstrap steps for the current step.
type StepContext struct {
	Plan         *ServicePlan      `json:"plan,omitempty"`
	Attestations map[string]string `json:"attestations,omitempty"`
}

// BootstrapStepInfo provides detailed info about the current step.
type BootstrapStepInfo struct {
	Name          string        `json:"name"`
	Index         int           `json:"index"`
	Tools         []string      `json:"tools"`
	Verification  string        `json:"verification"`
	DetailedGuide string        `json:"detailedGuide,omitempty"`
	PriorContext  *StepContext  `json:"priorContext,omitempty"`
	PlanMode      topology.Mode `json:"planMode,omitempty"` // "standard" or "simple", set after plan submission
}

// NewBootstrapState creates a new bootstrap state with all steps pending.
func NewBootstrapState() *BootstrapState {
	steps := make([]BootstrapStep, len(stepDetails))
	for i, d := range stepDetails {
		steps[i] = BootstrapStep{Name: d.Name, Status: stepPending}
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

// Step status constants. Exported so callers outside workflow/ can discriminate
// terminal states (StepStatusComplete, StepStatusSkipped) from in-progress ones
// without stringly-typed duplication.
const (
	StepStatusPending    = "pending"
	StepStatusInProgress = "in_progress"
	StepStatusComplete   = "complete"
	StepStatusSkipped    = "skipped"

	// Package-internal aliases. Callers inside workflow/ use the lowercase
	// names historically; the uppercase exports mirror them exactly.
	stepPending    = StepStatusPending
	stepInProgress = StepStatusInProgress
	stepComplete   = StepStatusComplete
	stepSkipped    = StepStatusSkipped
)

const minAttestationLen = 10

// CompleteStep validates and completes the current step with an attestation.
// Attestation is audit trail (iteration context + prior step history), not validation.
// Real enforcement is in StepChecker — see engine.BootstrapComplete.
func (b *BootstrapState) CompleteStep(name, attestation string) error {
	if !b.Active {
		return fmt.Errorf("complete step: bootstrap not active")
	}
	if b.CurrentStep >= len(b.Steps) {
		return fmt.Errorf("complete step: all steps already done")
	}
	current := b.Steps[b.CurrentStep].Name
	if name != current {
		return fmt.Errorf("complete step: expected %q (current), got %q", current, name)
	}
	if len(attestation) < minAttestationLen {
		return fmt.Errorf("complete step: attestation too short (min %d chars)", minAttestationLen)
	}

	b.Steps[b.CurrentStep].Status = stepComplete
	b.Steps[b.CurrentStep].Attestation = attestation
	b.Steps[b.CurrentStep].CompletedAt = time.Now().UTC().Format(time.RFC3339)
	b.CurrentStep++

	if b.CurrentStep < len(b.Steps) {
		b.Steps[b.CurrentStep].Status = stepInProgress
	} else {
		b.Active = false
	}
	return nil
}

// SkipStep validates and skips the current step with a reason.
func (b *BootstrapState) SkipStep(name, reason string) error {
	if !b.Active {
		return fmt.Errorf("skip step: bootstrap not active")
	}
	if b.CurrentStep >= len(b.Steps) {
		return fmt.Errorf("skip step: all steps already done")
	}
	current := b.Steps[b.CurrentStep].Name
	if name != current {
		return fmt.Errorf("skip step: expected %q (current), got %q", current, name)
	}

	if err := validateSkip(b.Plan, name); err != nil {
		return err
	}

	b.Steps[b.CurrentStep].Status = stepSkipped
	b.Steps[b.CurrentStep].SkipReason = reason
	b.Steps[b.CurrentStep].CompletedAt = time.Now().UTC().Format(time.RFC3339)
	b.CurrentStep++

	if b.CurrentStep < len(b.Steps) {
		b.Steps[b.CurrentStep].Status = stepInProgress
	} else {
		b.Active = false
	}
	return nil
}

// BuildResponse constructs a BootstrapResponse from the current state.
// iteration is the parent workflow iteration counter. env and kp enable knowledge injection.
func (b *BootstrapState) BuildResponse(sessionID, intent string, iteration int, env Environment, kp knowledge.Provider) *BootstrapResponse {
	completed := 0
	summaries := make([]BootstrapStepSummary, len(b.Steps))
	for i, s := range b.Steps {
		summaries[i] = BootstrapStepSummary{Name: s.Name, Status: s.Status}
		if s.Status == stepComplete || s.Status == stepSkipped {
			completed++
		}
	}

	resp := &BootstrapResponse{
		SessionID: sessionID,
		Intent:    intent,
		Progress: BootstrapProgress{
			Total:     len(b.Steps),
			Completed: completed,
			Steps:     summaries,
		},
	}

	if b.CurrentStep < len(b.Steps) {
		detail := lookupDetail(b.Steps[b.CurrentStep].Name)
		resp.Current = &BootstrapStepInfo{
			Name:         detail.Name,
			Index:        b.CurrentStep,
			Tools:        detail.Tools,
			Verification: detail.Verification,
			PriorContext: b.buildPriorContext(),
			PlanMode:     b.PlanMode(),
		}
		resp.Current.DetailedGuide = b.buildGuide(detail.Name, iteration, env, kp)
		resp.Message = fmt.Sprintf("Step %d/%d: %s", b.CurrentStep+1, len(b.Steps), detail.Name)
	} else {
		resp.Message = "Bootstrap complete. All steps finished."
	}

	return resp
}

// lastAttestation returns the attestation from the most recently completed step.
func (b *BootstrapState) lastAttestation() string {
	for i := b.CurrentStep - 1; i >= 0; i-- {
		if b.Steps[i].Attestation != "" {
			return b.Steps[i].Attestation
		}
	}
	return ""
}

// buildPriorContext collects attestations from completed prior steps and the plan.
// The most recent prior step (N-1) keeps its full attestation. Older steps are
// compressed to max 80 chars and wrapped in a status bracket.
// Returns nil if there is no prior context (first step, no attestations).
func (b *BootstrapState) buildPriorContext() *StepContext {
	attestations := make(map[string]string)
	for i := 0; i < b.CurrentStep && i < len(b.Steps); i++ {
		if b.Steps[i].Attestation == "" {
			continue
		}
		if i == b.CurrentStep-1 {
			attestations[b.Steps[i].Name] = b.Steps[i].Attestation
		} else {
			att := b.Steps[i].Attestation
			if len(att) > 80 {
				att = att[:77] + "..."
			}
			attestations[b.Steps[i].Name] = fmt.Sprintf("[%s: %s]", b.Steps[i].Status, att)
		}
	}

	if len(attestations) == 0 && b.Plan == nil {
		return nil
	}

	return &StepContext{
		Plan:         b.Plan,
		Attestations: attestations,
	}
}

// PlanMode returns the aggregate plan mode for gate decisions.
// Returns "standard" if ANY target uses standard mode (G4 required).
// Returns "dev", "simple", or "mixed" otherwise (G4 skipped).
// Returns empty if no plan has been submitted yet.
func (b *BootstrapState) PlanMode() topology.Mode {
	if b.Plan == nil || len(b.Plan.Targets) == 0 {
		return ""
	}
	modes := make(map[topology.Mode]bool)
	for _, t := range b.Plan.Targets {
		modes[t.Runtime.EffectiveMode()] = true
	}
	if modes[topology.PlanModeStandard] {
		return topology.PlanModeStandard
	}
	if len(modes) == 1 {
		for m := range modes {
			return m
		}
	}
	return "mixed"
}

// validateSkip checks whether a step can be skipped given the current plan.
// discover/provision are always mandatory. close is skippable when there are
// no runtime targets (managed-only) or all targets are adopted (pure adoption).
func validateSkip(plan *ServicePlan, name string) error {
	switch name {
	case StepDiscover, StepProvision:
		return fmt.Errorf("skip step: %q is mandatory and cannot be skipped", name)
	case StepClose:
		if plan != nil && len(plan.Targets) > 0 && !plan.IsAllExisting() {
			return fmt.Errorf("skip step: cannot skip %q — runtime services in plan require it", name)
		}
		return nil
	default:
		return fmt.Errorf("skip step: unknown step %q", name)
	}
}

// lookupDetail finds the StepDetail for a step name.
func lookupDetail(name string) StepDetail {
	for _, d := range stepDetails {
		if d.Name == name {
			return d
		}
	}
	return StepDetail{}
}
