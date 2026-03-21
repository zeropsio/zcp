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

// resolveDeployStepGuidance returns guidance for a deploy workflow step.
// Mode-specific and strategy-specific sections are assembled for the deploy step.
func resolveDeployStepGuidance(step, mode, strategy string) string {
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
		// Strategy-specific guidance for deploy step.
		if strategy != "" {
			if sectionName, ok := StrategyToSection[strategy]; ok {
				sections = append(sections, ExtractSection(md, sectionName))
			}
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
