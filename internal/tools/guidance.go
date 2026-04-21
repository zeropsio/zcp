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
		var planSubmitted bool
		var recipeTier string
		if engine != nil {
			state, err := engine.GetState()
			if err == nil && state.Recipe != nil {
				recipeTier = state.Recipe.Tier
				if state.Recipe.Plan != nil {
					plan = state.Recipe.Plan
					planSubmitted = true
				}
			}
		}

		// Cx-GUIDANCE-PLAN-NIL (v36 F-7 close): research-step topic
		// pulls happen before the agent submits recipePlan via
		// complete=research, so state.Recipe.Plan is nil. Predicates
		// returning false for nil made every tier-gated topic surface
		// a misleading "does not apply" message (v36 snippet:
		// showcase-service-keys, dashboard-skeleton, recipe-types all
		// rejected despite state.Recipe.Tier=showcase). Build a
		// tier-only synthetic plan so tier-gated predicates (isShowcase)
		// resolve correctly; shape-dependent predicates (hasWorker,
		// isDualRuntime) still false → distinct "plan not submitted"
		// message below.
		evalPlan := plan
		if evalPlan == nil && recipeTier != "" {
			evalPlan = &workflow.RecipePlan{Tier: recipeTier}
		}

		content, err := workflow.ResolveTopic(input.Topic, evalPlan)
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
			if topic.Predicate != nil && !topic.Predicate(evalPlan) {
				if !planSubmitted && recipeTier != "" {
					// Cx-GUIDANCE-PLAN-NIL: predicate needs more than
					// tier (targets, framework) but plan hasn't been
					// submitted yet. Different class from "shape
					// mismatch" — must be distinguishable so the agent
					// knows to retry after research-complete.
					return textResult(fmt.Sprintf(
						"Topic %q requires plan shape (targets/framework) that isn't available yet — "+
							"state.Recipe.Plan is nil because action=complete step=research hasn't run. "+
							"Submit your recipePlan via action=complete step=research first, then re-fetch. "+
							"(Tier-only topics resolve at research; this one needs more than tier=%q.)",
						input.Topic, recipeTier)), nil, nil
				}
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
