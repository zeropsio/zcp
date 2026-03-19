package workflow

import (
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/knowledge"
)

// WorkflowDeploy is the workflow name for deploy sessions.
const WorkflowDeploy = "deploy"

// Deploy step constants.
const (
	DeployStepPrepare = "prepare"
	DeployStepDeploy  = "deploy"
	DeployStepVerify  = "verify"
)

// Deploy target role constants.
const (
	DeployRoleDev    = "dev"
	DeployRoleStage  = "stage"
	DeployRoleSimple = "simple"
)

// Deploy target status constants.
const (
	deployTargetPending  = "pending"
	deployTargetDeployed = "deployed"
	deployTargetVerified = "verified"
	deployTargetFailed   = "failed"
	deployTargetSkipped  = "skipped"
)

// DeployState tracks progress through the deploy workflow.
type DeployState struct {
	Active      bool           `json:"active"`
	CurrentStep int            `json:"currentStep"`
	Steps       []DeployStep   `json:"steps"`
	Targets     []DeployTarget `json:"targets"`
	Mode        string         `json:"mode"`
}

// DeployStep represents a step in the deploy workflow.
type DeployStep struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Attestation string `json:"attestation,omitempty"`
	CompletedAt string `json:"completedAt,omitempty"`
}

// DeployTarget tracks per-service deploy progress.
type DeployTarget struct {
	Hostname        string `json:"hostname"`
	Role            string `json:"role"`
	Status          string `json:"status"`
	Error           string `json:"error,omitempty"`
	LastAttestation string `json:"lastAttestation,omitempty"`
}

// DeployResponse is returned from deploy workflow actions.
type DeployResponse struct {
	SessionID string            `json:"sessionId"`
	Intent    string            `json:"intent"`
	Iteration int               `json:"iteration"`
	Message   string            `json:"message"`
	Progress  DeployProgress    `json:"progress"`
	Current   *DeployStepInfo   `json:"current,omitempty"`
	Targets   []DeployTargetOut `json:"targets"`
}

// DeployProgress summarizes overall deploy progress.
type DeployProgress struct {
	Total     int                `json:"total"`
	Completed int                `json:"completed"`
	Steps     []DeployStepOutSum `json:"steps"`
}

// DeployStepOutSum is a step summary for progress display.
type DeployStepOutSum struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// DeployStepInfo provides info about the current deploy step.
type DeployStepInfo struct {
	Name          string   `json:"name"`
	DetailedGuide string   `json:"detailedGuide,omitempty"`
	Tools         []string `json:"tools,omitempty"`
}

// DeployTargetOut is a target status for the response.
type DeployTargetOut struct {
	Hostname string `json:"hostname"`
	Role     string `json:"role"`
	Status   string `json:"status"`
}

// deployStepDetails defines the 3 deploy steps.
var deployStepDetails = []struct {
	Name  string
	Tools []string
}{
	{DeployStepPrepare, []string{"zerops_discover", "zerops_knowledge"}},
	{DeployStepDeploy, []string{"zerops_deploy", "zerops_subdomain", "zerops_logs", "zerops_verify", "zerops_manage"}},
	{DeployStepVerify, []string{"zerops_verify", "zerops_discover"}},
}

// NewDeployState creates a deploy state with ordered targets.
func NewDeployState(targets []DeployTarget, mode string) *DeployState {
	steps := make([]DeployStep, len(deployStepDetails))
	for i, d := range deployStepDetails {
		steps[i] = DeployStep{Name: d.Name, Status: stepPending}
	}
	return &DeployState{
		Active:      true,
		CurrentStep: 0,
		Steps:       steps,
		Targets:     targets,
		Mode:        mode,
	}
}

// BuildDeployTargets constructs ordered targets from service metas.
// Returns targets and detected mode. Standard mode orders dev before stage.
func BuildDeployTargets(metas []*ServiceMeta) ([]DeployTarget, string) {
	if len(metas) == 0 {
		return nil, ""
	}

	var targets []DeployTarget
	mode := ""

	for _, m := range metas {
		metaMode := m.Mode
		if metaMode == "" {
			metaMode = PlanModeStandard
		}
		if mode == "" {
			mode = metaMode
		}

		targets = append(targets, DeployTarget{
			Hostname: m.Hostname,
			Role:     deployRoleFromMode(metaMode, m.Hostname, m.StageHostname),
			Status:   deployTargetPending,
		})

		// Standard mode: add stage target after dev.
		if metaMode == PlanModeStandard && m.StageHostname != "" {
			targets = append(targets, DeployTarget{
				Hostname: m.StageHostname,
				Role:     DeployRoleStage,
				Status:   deployTargetPending,
			})
		}
	}

	return targets, mode
}

func deployRoleFromMode(mode, _, stageHostname string) string {
	switch mode {
	case PlanModeSimple:
		return DeployRoleSimple
	case PlanModeDev:
		return DeployRoleDev
	default:
		if stageHostname != "" {
			return DeployRoleDev
		}
		return DeployRoleSimple
	}
}

// CurrentStepName returns the name of the current step.
func (d *DeployState) CurrentStepName() string {
	if d.CurrentStep >= len(d.Steps) {
		return ""
	}
	return d.Steps[d.CurrentStep].Name
}

// CompleteStep advances to the next step.
func (d *DeployState) CompleteStep(name, attestation string) error {
	if !d.Active {
		return fmt.Errorf("deploy complete: not active")
	}
	if d.CurrentStep >= len(d.Steps) {
		return fmt.Errorf("deploy complete: all steps done")
	}
	if d.Steps[d.CurrentStep].Name != name {
		return fmt.Errorf("deploy complete: expected %q, got %q", d.Steps[d.CurrentStep].Name, name)
	}
	if len(attestation) < minAttestationLen {
		return fmt.Errorf("deploy complete: attestation too short (min %d chars)", minAttestationLen)
	}

	d.Steps[d.CurrentStep].Status = stepComplete
	d.Steps[d.CurrentStep].Attestation = attestation
	d.Steps[d.CurrentStep].CompletedAt = time.Now().UTC().Format(time.RFC3339)
	d.CurrentStep++

	if d.CurrentStep < len(d.Steps) {
		d.Steps[d.CurrentStep].Status = stepInProgress
	} else {
		d.Active = false
	}
	return nil
}

// SkipStep skips the current step.
func (d *DeployState) SkipStep(name, reason string) error {
	if !d.Active {
		return fmt.Errorf("deploy skip: not active")
	}
	if d.CurrentStep >= len(d.Steps) {
		return fmt.Errorf("deploy skip: all steps done")
	}
	if d.Steps[d.CurrentStep].Name != name {
		return fmt.Errorf("deploy skip: expected %q, got %q", d.Steps[d.CurrentStep].Name, name)
	}

	d.Steps[d.CurrentStep].Status = stepSkipped
	d.Steps[d.CurrentStep].Attestation = reason
	d.Steps[d.CurrentStep].CompletedAt = time.Now().UTC().Format(time.RFC3339)
	d.CurrentStep++

	if d.CurrentStep < len(d.Steps) {
		d.Steps[d.CurrentStep].Status = stepInProgress
	} else {
		d.Active = false
	}
	return nil
}

// UpdateTarget updates a target's status and attestation.
func (d *DeployState) UpdateTarget(hostname, status, attestation string) error {
	for i, t := range d.Targets {
		if t.Hostname == hostname {
			d.Targets[i].Status = status
			d.Targets[i].LastAttestation = attestation
			if status == deployTargetFailed {
				d.Targets[i].Error = attestation
			}
			return nil
		}
	}
	return fmt.Errorf("deploy update target: hostname %q not found", hostname)
}

// ResetForIteration resets deploy and verify steps for a new attempt.
func (d *DeployState) ResetForIteration() {
	if d == nil {
		return
	}
	// Reset steps 1 (deploy) and 2 (verify) to pending.
	for i := 1; i < len(d.Steps); i++ {
		d.Steps[i] = DeployStep{Name: d.Steps[i].Name, Status: stepPending}
	}
	// Reset all target statuses to pending.
	for i := range d.Targets {
		d.Targets[i].Status = deployTargetPending
		d.Targets[i].Error = ""
	}
	d.CurrentStep = 1
	if d.CurrentStep < len(d.Steps) {
		d.Steps[d.CurrentStep].Status = stepInProgress
	}
	d.Active = true
}

// DevFailed returns true if any dev target has failed (blocks stage deployment).
func (d *DeployState) DevFailed() bool {
	for _, t := range d.Targets {
		if t.Role == DeployRoleDev && t.Status == deployTargetFailed {
			return true
		}
	}
	return false
}

// BuildResponse constructs a DeployResponse.
func (d *DeployState) BuildResponse(sessionID, intent string, iteration int, env Environment, kp knowledge.Provider) *DeployResponse {
	completed := 0
	summaries := make([]DeployStepOutSum, len(d.Steps))
	for i, s := range d.Steps {
		summaries[i] = DeployStepOutSum{Name: s.Name, Status: s.Status}
		if s.Status == stepComplete || s.Status == stepSkipped {
			completed++
		}
	}

	targetOuts := make([]DeployTargetOut, len(d.Targets))
	for i, t := range d.Targets {
		targetOuts[i] = DeployTargetOut{
			Hostname: t.Hostname,
			Role:     t.Role,
			Status:   t.Status,
		}
	}

	resp := &DeployResponse{
		SessionID: sessionID,
		Intent:    intent,
		Iteration: iteration,
		Progress: DeployProgress{
			Total:     len(d.Steps),
			Completed: completed,
			Steps:     summaries,
		},
		Targets: targetOuts,
	}

	if d.CurrentStep < len(d.Steps) {
		detail := deployStepDetails[d.CurrentStep]
		resp.Current = &DeployStepInfo{
			Name:  detail.Name,
			Tools: detail.Tools,
		}
		resp.Current.DetailedGuide = d.buildGuide(detail.Name, iteration, env, kp)
		resp.Message = fmt.Sprintf("Deploy step %d/%d: %s", d.CurrentStep+1, len(d.Steps), detail.Name)
	} else {
		resp.Message = "Deploy complete. All steps finished."
	}

	return resp
}

// buildGuide assembles a deploy step guide with knowledge injection.
func (d *DeployState) buildGuide(step string, _ int, _ Environment, kp knowledge.Provider) string {
	guide := resolveDeployStepGuidance(step, d.Mode)

	if extra := d.assembleDeployKnowledge(step, kp); extra != "" {
		guide += "\n\n---\n\n" + extra
	}

	return guide
}

// assembleDeployKnowledge injects relevant knowledge for deploy steps.
func (d *DeployState) assembleDeployKnowledge(step string, kp knowledge.Provider) string {
	if kp == nil || len(d.Targets) == 0 {
		return ""
	}

	switch step {
	case DeployStepPrepare:
		// Inject zerops.yml schema for config checking.
		if doc, err := kp.Get("zerops://themes/core"); err == nil {
			sections := doc.H2Sections()
			if s, ok := sections["zerops.yml Schema"]; ok && s != "" {
				return "## zerops.yml Schema\n\n" + s
			}
		}
	case DeployStepDeploy:
		// Inject schema rules for deploy constraints.
		if doc, err := kp.Get("zerops://themes/core"); err == nil {
			sections := doc.H2Sections()
			if s, ok := sections["Schema Rules"]; ok && s != "" {
				return "## Deploy Rules\n\n" + s
			}
		}
	}
	return ""
}
