package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/content"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// validStrategies is the set of allowed strategy values.
var validStrategies = map[string]bool{
	workflow.StrategyPushDev: true,
	workflow.StrategyCICD:    true,
	workflow.StrategyManual:  true,
}

// handleStrategy handles post-bootstrap strategy updates for individual services.
func handleStrategy(_ *workflow.Engine, input WorkflowInput, stateDir string) (*mcp.CallToolResult, any, error) {
	if len(input.Strategies) == 0 {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Strategies map is required for strategy action",
			"Provide strategies: {\"hostname\": \"push-dev|ci-cd|manual\"}")), nil, nil
	}

	// Validate all strategy values first.
	for hostname, strategy := range input.Strategies {
		if !validStrategies[strategy] {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Invalid strategy %q for %q", strategy, hostname),
				"Valid strategies: push-dev, ci-cd, manual")), nil, nil
		}
	}

	// Update each service meta.
	var updated []string
	for hostname, strategy := range input.Strategies {
		meta, err := workflow.ReadServiceMeta(stateDir, hostname)
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceNotFound,
				fmt.Sprintf("Read service meta %q: %v", hostname, err),
				"Ensure the service was bootstrapped first")), nil, nil
		}
		if meta == nil {
			meta = &workflow.ServiceMeta{
				Hostname: hostname,
			}
		}
		meta.DeployStrategy = strategy
		if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrServiceNotFound,
				fmt.Sprintf("Write service meta %q: %v", hostname, err),
				"")), nil, nil
		}
		updated = append(updated, fmt.Sprintf("%s=%s", hostname, strategy))
	}

	// Build guidance from deploy.md sections.
	guidance := buildStrategyGuidance(input.Strategies)

	result := map[string]string{
		"status":   "updated",
		"services": strings.Join(updated, ", "),
		"next":     `When code is ready: zerops_workflow action="start" workflow="deploy"`,
	}
	if guidance != "" {
		result["guidance"] = guidance
	}
	return jsonResult(result), nil, nil
}

// buildStrategyGuidance extracts strategy-specific guidance from deploy.md.
func buildStrategyGuidance(strategies map[string]string) string {
	md, err := content.GetWorkflow("deploy")
	if err != nil {
		return ""
	}

	seen := make(map[string]bool)
	var parts []string
	for _, strategy := range strategies {
		section, ok := workflow.StrategyToSection[strategy]
		if !ok || seen[section] {
			continue
		}
		seen[section] = true
		extracted := workflow.ExtractSection(md, section)
		if extracted != "" {
			parts = append(parts, extracted)
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// strategySelectionResponse is returned when deploy is attempted without strategies set.
type strategySelectionResponse struct {
	Action   string   `json:"action"`
	Services []string `json:"services"`
	Guidance string   `json:"guidance"`
}

// buildStrategySelectionResponse creates conversational guidance for strategy selection.
func buildStrategySelectionResponse(metas []*workflow.ServiceMeta) strategySelectionResponse {
	hostnames := make([]string, 0, len(metas))
	var sb strings.Builder

	sb.WriteString("## How should your services be deployed?\n\n")
	sb.WriteString("Before deploying, choose a strategy for each service:\n\n")

	for _, m := range metas {
		hostnames = append(hostnames, m.Hostname)
		sb.WriteString(fmt.Sprintf("### %s (%s, %s mode)\n\n", m.Hostname, m.Mode, m.Mode))
	}

	sb.WriteString("### push-dev\n")
	sb.WriteString("You deploy by pushing code from a dev container via SSH.\n")
	sb.WriteString("- **How it works**: Edit code on the dev container, then `zcli push` deploys it. Fast feedback loop.\n")
	sb.WriteString("- **Good for**: Prototyping, experimenting, quick iterations.\n")
	sb.WriteString("- **Trade-off**: Manual process — you trigger each deploy yourself.\n\n")

	sb.WriteString("### ci-cd\n")
	sb.WriteString("Deploys happen automatically when you push to a git repository.\n")
	sb.WriteString("- **How it works**: Connect a GitHub/GitLab repo. Every push triggers a build and deploy via webhook.\n")
	sb.WriteString("- **Good for**: Team development, production workflows, deploys tied to git history.\n")
	sb.WriteString("- **Trade-off**: Requires initial pipeline setup (I can help with that).\n\n")

	sb.WriteString("### manual\n")
	sb.WriteString("No automated deployment. You manage the process yourself.\n")
	sb.WriteString("- **How it works**: Zerops runs your service, but you handle deploys with your own tools.\n")
	sb.WriteString("- **Good for**: Existing CI/CD pipelines, custom deployment workflows.\n")
	sb.WriteString("- **Trade-off**: ZCP won't manage or guide your deploys.\n\n")

	// Build example command.
	parts := make([]string, 0, len(hostnames))
	for _, h := range hostnames {
		parts = append(parts, fmt.Sprintf("%q:\"push-dev\"", h))
	}
	sb.WriteString(fmt.Sprintf("→ `zerops_workflow action=\"strategy\" strategies={%s}`\n\n", strings.Join(parts, ",")))
	sb.WriteString("After choosing, re-run: `zerops_workflow action=\"start\" workflow=\"deploy\"`\n")

	return strategySelectionResponse{
		Action:   "strategy_required",
		Services: hostnames,
		Guidance: sb.String(),
	}
}

// handleRoute gathers router input from live API + local state and returns flow offerings.
func handleRoute(ctx context.Context, _ *workflow.Engine, client platform.Client, projectID, stateDir, selfHostname string) (*mcp.CallToolResult, any, error) {
	projState := workflow.StateUnknown
	var liveHostnames []string
	if client != nil && projectID != "" {
		if ps, err := workflow.DetectProjectState(ctx, client, projectID, selfHostname); err == nil {
			projState = ps
		}
		if svcs, err := client.ListServices(ctx, projectID); err == nil {
			for _, s := range svcs {
				if !s.IsSystem() {
					liveHostnames = append(liveHostnames, s.Name)
				}
			}
		}
	}
	metas, _ := workflow.ListServiceMetas(stateDir)
	sessions, _ := workflow.ListSessions(stateDir)
	return jsonResult(workflow.Route(workflow.RouterInput{
		ProjectState:   projState,
		ServiceMetas:   metas,
		ActiveSessions: sessions,
		LiveServices:   liveHostnames,
	})), nil, nil
}
