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

// strategySectionMap is the canonical map from workflow package.
var strategySectionMap = workflow.StrategyToSection

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
		section, ok := strategySectionMap[strategy]
		if !ok || seen[section] {
			continue
		}
		seen[section] = true
		extracted := extractDeploySection(md, section)
		if extracted != "" {
			parts = append(parts, extracted)
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// extractDeploySection finds a <section name="{name}">...</section> block.
func extractDeploySection(md, name string) string {
	openTag := "<section name=\"" + name + "\">"
	closeTag := "</section>"
	start := strings.Index(md, openTag)
	if start < 0 {
		return ""
	}
	start += len(openTag)
	end := strings.Index(md[start:], closeTag)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(md[start : start+end])
}

// handleRoute gathers router input from live API + local state and returns flow offerings.
func handleRoute(ctx context.Context, _ *workflow.Engine, client platform.Client, projectID, stateDir string) (*mcp.CallToolResult, any, error) {
	projState := workflow.StateUnknown
	var liveHostnames []string
	if client != nil && projectID != "" {
		if ps, err := workflow.DetectProjectState(ctx, client, projectID); err == nil {
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
