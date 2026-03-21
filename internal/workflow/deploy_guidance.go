package workflow

import (
	"strings"

	"github.com/zeropsio/zcp/internal/content"
)

// StrategyToSection maps deploy strategy constants to deploy.md section names.
var StrategyToSection = map[string]string{
	StrategyPushDev: "deploy-push-dev",
	StrategyCICD:    "deploy-ci-cd",
	StrategyManual:  "deploy-manual",
}

// ResolveDeployGuidance reads the ServiceMeta for hostname,
// maps its strategy to the corresponding deploy.md section,
// and returns the strategy-specific guidance.
// Falls back to empty string if no strategy is set or meta not found.
func ResolveDeployGuidance(stateDir, hostname string) string {
	meta, err := ReadServiceMeta(stateDir, hostname)
	if err != nil || meta == nil {
		return ""
	}

	strategy := meta.DeployStrategy
	if strategy == "" {
		return ""
	}

	sectionName, ok := StrategyToSection[strategy]
	if !ok {
		return ""
	}

	md, err := content.GetWorkflow("deploy")
	if err != nil {
		return ""
	}

	return ExtractSection(md, sectionName)
}

// resolveDeployStepGuidance returns guidance for a deploy workflow step.
// Mode-specific sections are assembled for the deploy step.
func resolveDeployStepGuidance(step, mode string) string {
	md, err := content.GetWorkflow("deploy")
	if err != nil {
		return ""
	}

	switch step {
	case DeployStepPrepare:
		return ExtractSection(md, "deploy-prepare")
	case DeployStepDeploy:
		var sections []string
		sections = append(sections, ExtractSection(md, "deploy-execute-overview"))
		switch mode {
		case PlanModeStandard:
			sections = append(sections, ExtractSection(md, "deploy-execute-standard"))
		case PlanModeDev:
			sections = append(sections, ExtractSection(md, "deploy-execute-dev"))
		case PlanModeSimple:
			sections = append(sections, ExtractSection(md, "deploy-execute-simple"))
		default:
			sections = append(sections, ExtractSection(md, "deploy-execute-standard"))
		}
		// Iteration guidance for standard and dev modes (not simple — auto-starts).
		if mode != PlanModeSimple {
			sections = append(sections, ExtractSection(md, "deploy-iteration"))
		}
		var parts []string
		for _, s := range sections {
			if s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) == 0 {
			return ""
		}
		return strings.Join(parts, "\n\n---\n\n")
	case DeployStepVerify:
		return ExtractSection(md, "deploy-verify")
	}
	return ""
}
