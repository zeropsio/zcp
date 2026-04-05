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
		if m.EffectiveStrategy() != workflow.StrategyPushGit {
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

	var b strings.Builder
	b.WriteString("## Your CI/CD Targets\n\nBased on project configuration:\n")
	b.WriteString(strings.Join(targets, "\n"))
	b.WriteString("\n\nGet service IDs: Zerops dashboard -> service -> three-dot menu -> Copy Service ID.")

	// Generate workflow YAML from targets (service IDs not available at this stage —
	// agent must resolve via zerops_discover and fill in placeholders).
	cicdTargets := buildCICDTargets(stateDir, nil)
	if len(cicdTargets) > 0 {
		yaml := generateGitHubActionsWorkflow(cicdTargets, "main")
		b.WriteString("\n\n## Generated Workflow (fill service IDs via zerops_discover)\n\n```yaml\n")
		b.WriteString(yaml)
		b.WriteString("```\n")
		b.WriteString("\nReplace `{SERVICE_ID}` placeholders with actual service IDs from `zerops_discover`.")
	}

	return b.String()
}
