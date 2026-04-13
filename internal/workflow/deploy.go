package workflow

import (
	"fmt"
	"time"
)

// WorkflowDevelop is the workflow name for develop sessions.
const WorkflowDevelop = "develop"

// Deploy step constants.
const (
	DeployStepPrepare = "prepare"
	DeployStepExecute = "execute"
	DeployStepVerify  = "verify"
)

// Deploy target role constants.
const (
	DeployRoleDev    = "dev"
	DeployRoleStage  = "stage"
	DeployRoleSimple = "simple"
)

// deployTargetPending is the initial status for deploy targets.
const deployTargetPending = "pending"

// DeployState tracks progress through the develop workflow.
type DeployState struct {
	Active      bool           `json:"active"`
	CurrentStep int            `json:"currentStep"`
	Steps       []DeployStep   `json:"steps"`
	Targets     []DeployTarget `json:"targets"`
	Mode        string         `json:"mode"`
}

// DeployStep represents a step in the develop workflow.
type DeployStep struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Attestation string `json:"attestation,omitempty"`
	CompletedAt string `json:"completedAt,omitempty"`
}

// DeployTarget tracks per-service deploy progress.
type DeployTarget struct {
	Hostname    string `json:"hostname"`
	RuntimeType string `json:"runtimeType,omitempty"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	Strategy    string `json:"strategy,omitempty"`
	HTTPSupport bool   `json:"httpSupport,omitempty"`
}

// DeployResponse is returned from develop workflow actions.
type DeployResponse struct {
	SessionID   string            `json:"sessionId"`
	Intent      string            `json:"intent"`
	Iteration   int               `json:"iteration"`
	Message     string            `json:"message"`
	Progress    DeployProgress    `json:"progress"`
	Current     *DeployStepInfo   `json:"current,omitempty"`
	Targets     []DeployTargetOut `json:"targets"`
	CheckResult *StepCheckResult  `json:"checkResult,omitempty"`
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
	{DeployStepExecute, []string{"zerops_deploy", "zerops_subdomain", "zerops_logs", "zerops_verify", "zerops_manage"}},
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
// Returns targets and detected mode.
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
			Strategy: m.EffectiveStrategy(),
		})

		// Standard mode: add stage target after dev.
		if metaMode == PlanModeStandard && m.StageHostname != "" {
			targets = append(targets, DeployTarget{
				Hostname: m.StageHostname,
				Role:     DeployRoleStage,
				Status:   deployTargetPending,
				Strategy: m.EffectiveStrategy(),
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
// Prepare is mandatory (validates zerops.yaml before deploy) and cannot be skipped.
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
	if name == DeployStepPrepare {
		return fmt.Errorf("deploy skip: %q is mandatory and cannot be skipped", name)
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

// ResetForIteration resets deploy and verify steps for a new attempt.
func (d *DeployState) ResetForIteration() {
	if d == nil {
		return
	}
	firstReset := -1
	for i := range d.Steps {
		if d.Steps[i].Name == DeployStepExecute || d.Steps[i].Name == DeployStepVerify {
			d.Steps[i] = DeployStep{Name: d.Steps[i].Name, Status: stepPending}
			if firstReset < 0 {
				firstReset = i
			}
		}
	}
	for i := range d.Targets {
		d.Targets[i].Status = deployTargetPending
	}
	if firstReset >= 0 {
		d.CurrentStep = firstReset
		d.Steps[firstReset].Status = stepInProgress
	}
	d.Active = true
}

// BuildResponse constructs a DeployResponse.
func (d *DeployState) BuildResponse(sessionID, intent string, iteration int, env Environment, stateDir string) *DeployResponse {
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
		resp.Current.DetailedGuide = d.buildGuide(detail.Name, iteration, env, stateDir)
		resp.Message = fmt.Sprintf("Deploy step %d/%d: %s", d.CurrentStep+1, len(d.Steps), detail.Name)
	} else {
		resp.Message = "Deploy complete.\n\n" +
			"Start a new develop workflow for the next task:\n" +
			"  zerops_workflow action=\"start\" workflow=\"develop\"\n\n" +
			"Each workflow refreshes service state and provides current guidance."
	}

	return resp
}

// buildGuide generates personalized deploy step guidance from state.
// Deploy uses compact guidance with knowledge pointers — the agent pulls on demand.
func (d *DeployState) buildGuide(step string, iteration int, env Environment, stateDir string) string {
	switch step {
	case DeployStepPrepare:
		return buildPrepareGuide(d, env, stateDir)
	case DeployStepExecute:
		return buildDeployGuide(d, iteration, env, stateDir)
	case DeployStepVerify:
		return buildVerifyGuide(d)
	}
	return ""
}
