package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// cicdTarget represents a single deploy target for CI/CD workflow generation.
type cicdTarget struct {
	ServiceID string // Zerops service ID (from API)
	Hostname  string // service hostname (for comments)
}

// generateGitHubActionsWorkflow creates a ready-to-use GitHub Actions workflow
// YAML for deploying to Zerops. Returns empty string if no targets provided.
func generateGitHubActionsWorkflow(targets []cicdTarget, branch string) string {
	if len(targets) == 0 {
		return ""
	}
	if branch == "" {
		branch = "main"
	}

	var b strings.Builder
	b.WriteString("name: Deploy to Zerops\n")
	b.WriteString("on:\n")
	b.WriteString("  push:\n")
	b.WriteString(fmt.Sprintf("    branches: [%s]\n", branch))
	b.WriteString("jobs:\n")
	b.WriteString("  deploy:\n")
	b.WriteString("    runs-on: ubuntu-latest\n")
	b.WriteString("    steps:\n")
	b.WriteString("      - uses: actions/checkout@v4\n")

	for _, t := range targets {
		svcID := t.ServiceID
		if svcID == "" {
			svcID = "{SERVICE_ID}" // placeholder when ID not available
		}
		b.WriteString(fmt.Sprintf("      - uses: zeropsio/actions@main # %s\n", t.Hostname))
		b.WriteString("        with:\n")
		b.WriteString("          access-token: ${{ secrets.ZEROPS_TOKEN }}\n")
		b.WriteString(fmt.Sprintf("          service-id: %s\n", svcID))
	}

	return b.String()
}

// buildCICDTargets reads ServiceMeta and builds deploy targets for CI/CD.
// services maps hostname → serviceID (from zerops_discover).
func buildCICDTargets(stateDir string, services map[string]string) []cicdTarget {
	if stateDir == "" {
		return nil
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil || len(metas) == 0 {
		return nil
	}

	var targets []cicdTarget
	for _, m := range metas {
		if m.EffectiveStrategy() != workflow.StrategyPushGit {
			continue
		}
		// Stage is the deploy target in standard mode; dev hostname otherwise.
		hostname := m.StageHostname
		if hostname == "" {
			hostname = m.Hostname
		}
		targets = append(targets, cicdTarget{
			ServiceID: services[hostname],
			Hostname:  hostname,
		})
	}
	return targets
}
