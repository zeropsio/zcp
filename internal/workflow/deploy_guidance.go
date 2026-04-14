package workflow

import (
	"fmt"
	"strings"
)

// StrategyToSection maps deploy strategy constants to deploy.md section names.
var StrategyToSection = map[string]string{
	StrategyPushDev: "deploy-push-dev",
	StrategyPushGit: "deploy-push-git",
	StrategyManual:  "deploy-manual",
}

// strategyDescriptions provides one-line descriptions for strategy alternatives.
var strategyDescriptions = map[string]string{
	StrategyPushDev: "SSH self-deploy from dev container",
	StrategyPushGit: "push to git remote (optional CI/CD)",
	StrategyManual:  "you manage deployments yourself",
}

func writeStrategyNote(sb *strings.Builder, current string) {
	sb.WriteString("### Strategy\n")
	if current == "" {
		sb.WriteString("Not set. Before deploying, discuss with the user and choose:\n")
		for strategy, d := range strategyDescriptions {
			fmt.Fprintf(sb, "- %s (%s)\n", strategy, d)
		}
		sb.WriteString("Set via: `zerops_workflow action=\"strategy\" strategies={...}`\n\n")
		return
	}
	desc := strategyDescriptions[current]
	fmt.Fprintf(sb, "Currently: %s (%s)\n", current, desc)

	var alts []string
	for strategy, d := range strategyDescriptions {
		if strategy != current {
			alts = append(alts, fmt.Sprintf("%s (%s)", strategy, d))
		}
	}
	fmt.Fprintf(sb, "Other options: %s\n", strings.Join(alts, ", "))
	sb.WriteString("Change: `zerops_workflow action=\"strategy\" strategies={...}`\n\n")
}
