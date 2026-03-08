package workflow

import "github.com/zeropsio/zcp/internal/content"

// strategyToSection maps deploy strategy constants to deploy.md section names.
var strategyToSection = map[string]string{
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

	strategy := meta.Decisions[DecisionDeployStrategy]
	if strategy == "" {
		return ""
	}

	sectionName, ok := strategyToSection[strategy]
	if !ok {
		return ""
	}

	md, err := content.GetWorkflow("deploy")
	if err != nil {
		return ""
	}

	return extractSection(md, sectionName)
}
