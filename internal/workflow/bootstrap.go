package workflow

import (
	"fmt"
	"time"
)

// Plan mode constants.
const (
	PlanModeStandard = "standard"
	PlanModeSimple   = "simple"
)

// StepCategory classifies bootstrap steps.
type StepCategory string

const (
	CategoryFixed     StepCategory = "fixed"
	CategoryCreative  StepCategory = "creative"
	CategoryBranching StepCategory = "branching"
)

// StepDetail defines a bootstrap step's metadata and guidance.
type StepDetail struct {
	Name         string       `json:"name"`
	Category     StepCategory `json:"category"`
	Guidance     string       `json:"guidance"`
	Tools        []string     `json:"tools"`
	Verification string       `json:"verification"`
	Skippable    bool         `json:"skippable"`
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
type BootstrapState struct {
	Active            bool                `json:"active"`
	CurrentStep       int                 `json:"currentStep"`
	Steps             []BootstrapStep     `json:"steps"`
	Plan              *ServicePlan        `json:"plan,omitempty"`
	DiscoveredEnvVars map[string][]string `json:"discoveredEnvVars,omitempty"`
	Context           *ContextDelivery    `json:"context,omitempty"`
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
	Name          string       `json:"name"`
	Index         int          `json:"index"`
	Category      string       `json:"category"`
	Guidance      string       `json:"-"`
	Tools         []string     `json:"tools"`
	Verification  string       `json:"verification"`
	DetailedGuide string       `json:"detailedGuide,omitempty"`
	PriorContext  *StepContext `json:"priorContext,omitempty"`
	PlanMode      string       `json:"planMode,omitempty"` // "standard" or "simple", set after plan submission
}

// NewBootstrapState creates a new bootstrap state with all 5 steps pending.
func NewBootstrapState() *BootstrapState {
	steps := make([]BootstrapStep, len(stepDetails))
	for i, d := range stepDetails {
		steps[i] = BootstrapStep{Name: d.Name, Status: stepPending}
	}
	return &BootstrapState{
		Active:      true,
		CurrentStep: 0,
		Steps:       steps,
		Context:     &ContextDelivery{GuideSentFor: make(map[string]int)},
	}
}

// CurrentStepName returns the name of the current step, or empty if done.
func (b *BootstrapState) CurrentStepName() string {
	if b.CurrentStep >= len(b.Steps) {
		return ""
	}
	return b.Steps[b.CurrentStep].Name
}

// Step status constants.
const (
	stepPending    = "pending"
	stepInProgress = "in_progress"
	stepComplete   = "complete"
	stepSkipped    = "skipped"
)

// Step name constants for conditional skip validation.
const (
	stepGenerate = "generate"
	stepDeploy   = "deploy"
)

const minAttestationLen = 10

// CompleteStep validates and completes the current step with an attestation.
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

	if b.CurrentStep >= len(b.Steps) {
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

	detail := lookupDetail(name)
	if !detail.Skippable {
		return fmt.Errorf("skip step: %q is mandatory and cannot be skipped", name)
	}

	if err := validateConditionalSkip(b.Plan, name); err != nil {
		return err
	}

	b.Steps[b.CurrentStep].Status = stepSkipped
	b.Steps[b.CurrentStep].SkipReason = reason
	b.Steps[b.CurrentStep].CompletedAt = time.Now().UTC().Format(time.RFC3339)
	b.CurrentStep++

	if b.CurrentStep >= len(b.Steps) {
		b.Active = false
	}
	return nil
}

// BuildResponse constructs a BootstrapResponse from the current state.
// iteration is the parent workflow iteration counter for guide delivery gating.
func (b *BootstrapState) BuildResponse(sessionID, intent string, iteration int) *BootstrapResponse {
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
			Category:     string(detail.Category),
			Guidance:     detail.Guidance,
			Tools:        detail.Tools,
			Verification: detail.Verification,
			PriorContext: b.buildPriorContext(),
			PlanMode:     b.planMode(),
		}
		resp.Current.DetailedGuide = b.resolveGuideWithGating(detail.Name, iteration)
		resp.Message = fmt.Sprintf("Step %d/%d: %s", b.CurrentStep+1, len(b.Steps), detail.Name)
	} else {
		resp.Message = "Bootstrap complete. All steps finished."
	}

	return resp
}

// resolveGuideWithGating handles guide delivery gating and iteration delta.
func (b *BootstrapState) resolveGuideWithGating(stepName string, iteration int) string {
	// Item 29: iteration delta overrides guide for deploy step.
	if iteration > 0 {
		lastAtt := b.lastAttestation()
		delta := BuildIterationDelta(stepName, iteration, b.Plan, lastAtt)
		if delta != "" {
			return delta
		}
	}

	// Item 27: guide delivery gating.
	if b.Context != nil && b.Context.GuideSentFor != nil {
		sentIter, sent := b.Context.GuideSentFor[stepName]
		if sent && sentIter >= iteration {
			// Already delivered this iteration — return stub.
			detail := lookupDetail(stepName)
			return fmt.Sprintf("[Guide for %s already delivered. Tools: %v. Verification: %s]",
				stepName, detail.Tools, detail.Verification)
		}
	}

	// Full guide delivery.
	guide := ResolveGuidance(stepName)
	if b.Context != nil {
		if b.Context.GuideSentFor == nil {
			b.Context.GuideSentFor = make(map[string]int)
		}
		b.Context.GuideSentFor[stepName] = iteration
	}
	return guide
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

// planMode returns "standard" or "simple" based on the plan targets.
// Returns empty if no plan has been submitted yet.
func (b *BootstrapState) planMode() string {
	if b.Plan == nil || len(b.Plan.Targets) == 0 {
		return ""
	}
	for _, t := range b.Plan.Targets {
		if t.Runtime.Simple {
			return PlanModeSimple
		}
	}
	return PlanModeStandard
}

// validateConditionalSkip prevents skipping steps that are required based on the plan.
func validateConditionalSkip(plan *ServicePlan, stepName string) error {
	if plan == nil {
		return nil
	}
	hasRuntimeServices := len(plan.Targets) > 0
	switch stepName {
	case stepGenerate, stepDeploy:
		if hasRuntimeServices {
			return fmt.Errorf("skip step: cannot skip %q — runtime services in plan require it", stepName)
		}
	}
	return nil
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
