package workflow

import "github.com/zeropsio/zcp/internal/content"

// resolveCICDGuidance returns guidance for a CI/CD workflow step.
// For the configure step, returns provider-specific guidance.
func resolveCICDGuidance(step, provider string) string {
	md, err := content.GetWorkflow("cicd")
	if err != nil {
		return ""
	}

	switch step {
	case CICDStepChoose:
		return ExtractSection(md, "cicd-choose")
	case CICDStepConfigure:
		if provider != "" {
			return ExtractSection(md, "cicd-configure-"+provider)
		}
		return ExtractSection(md, "cicd-configure-generic")
	case CICDStepVerify:
		return ExtractSection(md, "cicd-verify")
	}
	return ""
}
