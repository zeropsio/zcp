package tools

import (
	"fmt"
	"strings"
)

// buildCICDContext reads ServiceMeta and builds a context header for CI/CD guidance.
// Returns empty string if no ServiceMeta exists (agent uses zerops_discover instead).
func buildCICDContext(stateDir string) string {
	targets := buildCICDTargets(stateDir, nil)
	if len(targets) == 0 {
		return ""
	}

	var lines []string
	for _, t := range targets {
		if t.DevHostname != t.Hostname {
			lines = append(lines, fmt.Sprintf("- %s -> %s (setup=%s)", t.DevHostname, t.Hostname, t.Setup))
		} else {
			lines = append(lines, fmt.Sprintf("- %s (setup=%s)", t.Hostname, t.Setup))
		}
	}

	var b strings.Builder
	b.WriteString("## Your CI/CD Targets\n\nBased on project configuration:\n")
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("\n\nGet service IDs: Zerops dashboard -> service -> three-dot menu -> Copy Service ID.")

	yaml := generateGitHubActionsWorkflow(targets, "main")
	if yaml != "" {
		b.WriteString("\n\n## Generated Workflow (fill service IDs via zerops_discover)\n\n```yaml\n")
		b.WriteString(yaml)
		b.WriteString("```\n")
		b.WriteString("\nReplace `{SERVICE_ID}` placeholders with actual service IDs from `zerops_discover`.")
	}

	return b.String()
}
