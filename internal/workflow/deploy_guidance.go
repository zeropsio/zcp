package workflow

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// strategyDescriptions provides one-line descriptions for strategy alternatives.
var strategyDescriptions = map[string]string{
	topology.StrategyPushDev: "SSH self-deploy from dev container",
	topology.StrategyPushGit: "push to git remote (optional CI/CD)",
	topology.StrategyManual:  "you manage deployments yourself",
}

func writeStrategyNote(sb *strings.Builder, current topology.DeployStrategy) {
	sb.WriteString("### Strategy\n")
	cur := string(current)
	if cur == "" {
		sb.WriteString("Not set. Before deploying, discuss with the user and choose:\n")
		for strategy, d := range strategyDescriptions {
			fmt.Fprintf(sb, "- %s (%s)\n", strategy, d)
		}
		sb.WriteString("Set via: `zerops_workflow action=\"strategy\" strategies={...}`\n\n")
		return
	}
	desc := strategyDescriptions[cur]
	fmt.Fprintf(sb, "Currently: %s (%s)\n", cur, desc)

	var alts []string
	for strategy, d := range strategyDescriptions {
		if strategy != cur {
			alts = append(alts, fmt.Sprintf("%s (%s)", strategy, d))
		}
	}
	fmt.Fprintf(sb, "Other options: %s\n", strings.Join(alts, ", "))
	sb.WriteString("Change: `zerops_workflow action=\"strategy\" strategies={...}`\n\n")
}
