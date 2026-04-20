package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// cicdTarget represents a single deploy target for CI/CD workflow generation.
type cicdTarget struct {
	ServiceID   string // Zerops service ID (from API)
	Hostname    string // deploy target hostname
	DevHostname string // source dev hostname (for display; same as Hostname when no stage)
	Setup       string // zerops.yaml setup name ("prod" or "dev")
}

// generateGitHubActionsWorkflow creates a ready-to-use GitHub Actions workflow
// YAML for deploying to Zerops via zcli. Returns empty string if no targets provided.
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
	b.WriteString("      - name: Install zcli\n")
	b.WriteString("        run: |\n")
	b.WriteString("          curl -sSL https://zerops.io/zcli/install.sh | sh\n")
	b.WriteString("          echo \"$HOME/.local/bin\" >> $GITHUB_PATH\n")

	for _, t := range targets {
		svcID := t.ServiceID
		if svcID == "" {
			svcID = "{SERVICE_ID}" // placeholder when ID not available
		}
		setup := t.Setup
		if setup == "" {
			setup = workflow.RecipeSetupProd
		}
		b.WriteString(fmt.Sprintf("      - name: Deploy to %s\n", t.Hostname))
		b.WriteString(fmt.Sprintf("        run: zcli push --serviceId %s --setup %s\n", svcID, setup))
		b.WriteString("        env:\n")
		b.WriteString("          ZEROPS_TOKEN: ${{ secrets.ZEROPS_TOKEN }}\n")
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
		if m.DeployStrategy != workflow.StrategyPushGit {
			continue
		}
		// Stage is the deploy target in standard mode; dev hostname otherwise.
		hostname := m.StageHostname
		setup := workflow.RecipeSetupProd
		if hostname == "" {
			hostname = m.Hostname
			if m.Mode == workflow.PlanModeDev {
				setup = workflow.RecipeSetupDev
			}
		}
		targets = append(targets, cicdTarget{
			ServiceID:   services[hostname],
			Hostname:    hostname,
			DevHostname: m.Hostname,
			Setup:       setup,
		})
	}
	return targets
}
