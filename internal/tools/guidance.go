package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
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
			// Cx-GUIDANCE-TOPIC-REGISTRY (v35 F-5 close): bare
			// "unknown topic" errors let the main agent keep
			// guessing plausible-sounding IDs (v35 at 07:29:50-51).
			// Returning the top-3 nearest matches short-circuits
			// the guess loop.
			suggestions := workflow.NearestTopicIDs(input.Topic, 3)
			msg := fmt.Sprintf("Error: unknown guidance topic %q.", input.Topic)
			if len(suggestions) > 0 {
				msg += " Did you mean: " + strings.Join(suggestions, ", ") + "?"
			}
			msg += " (The full topic-ID list was returned in the response to action=start workflow=recipe as guidanceTopicIds.)"
			return textResult(msg), nil, nil
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
			// Cx-GUIDANCE-TOPIC-REGISTRY (v35 F-5 close): distinguish
			// predicate-filtered-empty (topic doesn't apply to this
			// plan shape, legitimate) from block-missing-empty
			// (registry references a block that doesn't exist in
			// recipe.md, server bug). The latter is a hard error so
			// the main agent doesn't silently skip and miss required
			// guidance.
			if topic.Predicate != nil && !topic.Predicate(plan) {
				return textResult(fmt.Sprintf("Topic %q does not apply to your recipe shape.", input.Topic)), nil, nil
			}
			return convertError(platform.NewPlatformError(
				platform.ErrTopicEmpty,
				fmt.Sprintf("guidance topic %q resolved to zero bytes despite predicate match", input.Topic),
				"Server-side registry bug — topic references blocks missing from recipe.md. Report via flow-main.md; do not assume the topic is empty by design.")), nil, nil
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
