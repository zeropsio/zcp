package workflow

import (
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/knowledge"
)

// WorkflowCICD is the workflow name for CI/CD setup sessions.
const WorkflowCICD = "cicd"

// CI/CD step constants.
const (
	CICDStepChoose    = "choose"
	CICDStepConfigure = "configure"
	CICDStepVerify    = "verify"
)

// CI/CD provider constants.
const (
	CICDProviderGitHub  = "github"
	CICDProviderGitLab  = "gitlab"
	CICDProviderWebhook = "webhook"
	CICDProviderGeneric = "generic"
)

// CICDState tracks progress through the CI/CD setup workflow.
type CICDState struct {
	Active      bool       `json:"active"`
	CurrentStep int        `json:"currentStep"`
	Steps       []CICDStep `json:"steps"`
	Provider    string     `json:"provider,omitempty"`
	Hostnames   []string   `json:"hostnames,omitempty"`
}

// CICDStep represents a step in the CI/CD workflow.
type CICDStep struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Attestation string `json:"attestation,omitempty"`
	CompletedAt string `json:"completedAt,omitempty"`
}

// CICDResponse is returned from CI/CD workflow actions.
type CICDResponse struct {
	SessionID string        `json:"sessionId"`
	Intent    string        `json:"intent"`
	Message   string        `json:"message"`
	Progress  CICDProgress  `json:"progress"`
	Current   *CICDStepInfo `json:"current,omitempty"`
	Provider  string        `json:"provider,omitempty"`
	Hostnames []string      `json:"hostnames,omitempty"`
}

// CICDProgress summarizes CI/CD progress.
type CICDProgress struct {
	Total     int              `json:"total"`
	Completed int              `json:"completed"`
	Steps     []CICDStepOutSum `json:"steps"`
}

// CICDStepOutSum is a step summary.
type CICDStepOutSum struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// CICDStepInfo provides info about the current CI/CD step.
type CICDStepInfo struct {
	Name          string   `json:"name"`
	DetailedGuide string   `json:"detailedGuide,omitempty"`
	Tools         []string `json:"tools,omitempty"`
}

// cicdStepDetails defines the 3 CI/CD steps.
var cicdStepDetails = []struct {
	Name  string
	Tools []string
}{
	{CICDStepChoose, []string{"zerops_knowledge"}},
	{CICDStepConfigure, []string{"zerops_knowledge"}},
	{CICDStepVerify, []string{"zerops_verify", "zerops_discover", "zerops_process"}},
}

// NewCICDState creates a CI/CD state.
func NewCICDState(hostnames []string) *CICDState {
	steps := make([]CICDStep, len(cicdStepDetails))
	for i, d := range cicdStepDetails {
		steps[i] = CICDStep{Name: d.Name, Status: stepPending}
	}
	return &CICDState{
		Active:      true,
		CurrentStep: 0,
		Steps:       steps,
		Hostnames:   hostnames,
	}
}

// CurrentStepName returns the name of the current step.
func (c *CICDState) CurrentStepName() string {
	if c.CurrentStep >= len(c.Steps) {
		return ""
	}
	return c.Steps[c.CurrentStep].Name
}

// CompleteStep advances to the next step.
func (c *CICDState) CompleteStep(name, attestation string) error {
	if !c.Active {
		return fmt.Errorf("cicd complete: not active")
	}
	if c.CurrentStep >= len(c.Steps) {
		return fmt.Errorf("cicd complete: all steps done")
	}
	if c.Steps[c.CurrentStep].Name != name {
		return fmt.Errorf("cicd complete: expected %q, got %q", c.Steps[c.CurrentStep].Name, name)
	}
	if len(attestation) < minAttestationLen {
		return fmt.Errorf("cicd complete: attestation too short (min %d chars)", minAttestationLen)
	}

	c.Steps[c.CurrentStep].Status = stepComplete
	c.Steps[c.CurrentStep].Attestation = attestation
	c.Steps[c.CurrentStep].CompletedAt = time.Now().UTC().Format(time.RFC3339)
	c.CurrentStep++

	if c.CurrentStep < len(c.Steps) {
		c.Steps[c.CurrentStep].Status = stepInProgress
	} else {
		c.Active = false
	}
	return nil
}

// SetProvider records the chosen CI/CD provider.
func (c *CICDState) SetProvider(provider string) error {
	switch provider {
	case CICDProviderGitHub, CICDProviderGitLab, CICDProviderWebhook, CICDProviderGeneric:
		c.Provider = provider
		return nil
	default:
		return fmt.Errorf("cicd: unknown provider %q (valid: github, gitlab, webhook, generic)", provider)
	}
}

// BuildResponse constructs a CICDResponse.
func (c *CICDState) BuildResponse(sessionID, intent string, _ Environment, kp knowledge.Provider) *CICDResponse {
	completed := 0
	summaries := make([]CICDStepOutSum, len(c.Steps))
	for i, s := range c.Steps {
		summaries[i] = CICDStepOutSum{Name: s.Name, Status: s.Status}
		if s.Status == stepComplete || s.Status == stepSkipped {
			completed++
		}
	}

	resp := &CICDResponse{
		SessionID: sessionID,
		Intent:    intent,
		Provider:  c.Provider,
		Hostnames: c.Hostnames,
		Progress: CICDProgress{
			Total:     len(c.Steps),
			Completed: completed,
			Steps:     summaries,
		},
	}

	if c.CurrentStep < len(c.Steps) {
		detail := cicdStepDetails[c.CurrentStep]
		resp.Current = &CICDStepInfo{
			Name:  detail.Name,
			Tools: detail.Tools,
		}
		resp.Current.DetailedGuide = resolveCICDGuidance(detail.Name, c.Provider)
		resp.Message = fmt.Sprintf("CI/CD step %d/%d: %s", c.CurrentStep+1, len(c.Steps), detail.Name)
	} else {
		resp.Message = "CI/CD setup complete."
	}

	return resp
}
