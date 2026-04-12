package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/workflow"
)

// GuidanceInput is the input type for zerops_guidance.
type GuidanceInput struct {
	Topic string `json:"topic" jsonschema:"required,Guidance topic ID (e.g. 'zerops-yaml-rules', 'smoke-test', 'deploy-flow'). See skeleton for available topics."`
}

// RegisterGuidance registers the zerops_guidance MCP tool. It provides
// on-demand access to recipe workflow guidance blocks, filtered through
// the active plan's predicates. Phase C: records access and expands
// related topics.
func RegisterGuidance(srv *mcp.Server, engine *workflow.Engine) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_guidance",
		Description: "Fetch detailed recipe guidance for a specific topic. Returns rules and instructions filtered to your recipe's shape. Call before each sub-task (zerops.yaml writing, smoke test, deploy, etc.) for focused, timely guidance instead of reading the full guide.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Recipe Guidance",
			ReadOnlyHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GuidanceInput) (*mcp.CallToolResult, any, error) {
		if input.Topic == "" {
			return textResult("Error: topic is required"), nil, nil
		}

		topic := workflow.LookupTopic(input.Topic)
		if topic == nil {
			return textResult(fmt.Sprintf("Error: unknown guidance topic %q — check the skeleton for valid topic IDs", input.Topic)), nil, nil
		}

		// Get the active recipe plan from the engine.
		var plan *workflow.RecipePlan
		if engine != nil {
			state, err := engine.GetState()
			if err == nil && state.Recipe != nil && state.Recipe.Plan != nil {
				plan = state.Recipe.Plan
			}
		}

		content, err := workflow.ResolveTopic(input.Topic, plan)
		if err != nil {
			return textResult(fmt.Sprintf("Error resolving topic %q: %v", input.Topic, err)), nil, nil
		}

		if content == "" {
			return textResult(fmt.Sprintf("Topic %q does not apply to your recipe shape.", input.Topic)), nil, nil
		}

		// Phase C: record access for adaptive delivery.
		if engine != nil {
			engine.RecordGuidanceAccess(input.Topic, topic.Step)
		}

		// Phase C: expand related topics that the agent likely needs.
		if plan != nil && engine != nil {
			expanded := workflow.ExpandTopic(input.Topic, plan, guidanceAccessSet(engine))
			if len(expanded) > 0 {
				var sb strings.Builder
				sb.WriteString(content)
				sb.WriteString("\n\n---\n\n## Related topics\n\n")
				sb.WriteString("Based on your recipe shape, you may also need:\n")
				for _, t := range expanded {
					sb.WriteString(fmt.Sprintf("- `zerops_guidance topic=%q` — %s\n", t.ID, t.Description))
				}
				content = sb.String()
			}
		}

		return textResult(content), nil, nil
	})
}

// guidanceAccessSet returns the set of topic IDs the agent has already fetched.
func guidanceAccessSet(engine *workflow.Engine) map[string]bool {
	state, err := engine.GetState()
	if err != nil || state.Recipe == nil {
		return nil
	}
	set := make(map[string]bool, len(state.Recipe.GuidanceAccess))
	for _, entry := range state.Recipe.GuidanceAccess {
		set[entry.TopicID] = true
	}
	return set
}
