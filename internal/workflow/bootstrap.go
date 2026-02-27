package workflow

import (
	"fmt"
	"time"
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
	Active      bool            `json:"active"`
	CurrentStep int             `json:"currentStep"`
	Steps       []BootstrapStep `json:"steps"`
	Plan        *ServicePlan    `json:"plan,omitempty"`
}

// BootstrapResponse is returned from conductor actions.
type BootstrapResponse struct {
	SessionID string             `json:"sessionId"`
	Mode      Mode               `json:"mode"`
	Intent    string             `json:"intent"`
	Progress  BootstrapProgress  `json:"progress"`
	Current   *BootstrapStepInfo `json:"current,omitempty"`
	Message   string             `json:"message"`
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

// BootstrapStepInfo provides detailed info about the current step.
type BootstrapStepInfo struct {
	Name         string   `json:"name"`
	Index        int      `json:"index"`
	Category     string   `json:"category"`
	Guidance     string   `json:"guidance"`
	Tools        []string `json:"tools"`
	Verification string   `json:"verification"`
}

// NewBootstrapState creates a new bootstrap state with all 10 steps pending.
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

// Step status constants.
const (
	stepPending    = "pending"
	stepInProgress = "in_progress"
	stepComplete   = "complete"
	stepSkipped    = "skipped"
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
func (b *BootstrapState) BuildResponse(sessionID string, mode Mode, intent string) *BootstrapResponse {
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
		Mode:      mode,
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
		}
		resp.Message = fmt.Sprintf("Step %d/%d: %s", b.CurrentStep+1, len(b.Steps), detail.Name)
	} else {
		resp.Message = "Bootstrap complete. All steps finished."
	}

	return resp
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
