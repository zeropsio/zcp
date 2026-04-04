package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// buildCICDContext reads ServiceMeta and builds a context header for CI/CD guidance.
// Returns empty string if no ServiceMeta exists (agent uses zerops_discover instead).
func buildCICDContext(stateDir string) string {
	if stateDir == "" {
		return ""
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil || len(metas) == 0 {
		return ""
	}

	var targets []string
	for _, m := range metas {
		if m.DeployStrategy != workflow.StrategyPushGit {
			continue
		}
		if m.StageHostname != "" {
			targets = append(targets, fmt.Sprintf("- %s -> %s (stage deploy target)", m.Hostname, m.StageHostname))
		} else {
			targets = append(targets, fmt.Sprintf("- %s (direct deploy target)", m.Hostname))
		}
	}
	if len(targets) == 0 {
		return ""
	}
	return "## Your CI/CD Targets\n\nBased on project configuration:\n" +
		strings.Join(targets, "\n") +
		"\n\nGet service IDs: Zerops dashboard -> service -> three-dot menu -> Copy Service ID."
}
