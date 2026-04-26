package tools

import (
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// classifyResponse is the JSON payload returned by zerops_workflow
// action=classify. Mirrors the Go-side classification taxonomy +
// routing matrix so the writer sub-agent can make per-item override
// decisions at runtime instead of carrying the full 11KB atoms in
// its dispatch brief.
type classifyResponse struct {
	// Classification is one of the six taxonomy classes.
	Classification string `json:"classification"`
	// DefaultRouteTo is the route a fact of this classification
	// defaults to — e.g. framework-invariant → content_gotcha;
	// framework-quirk → discarded.
	DefaultRouteTo string `json:"defaultRouteTo"`
	// RequiresCitation is true when the default route is one where
	// the citations-present gate fires (content_gotcha + content_ig
	// as of v39 Commit 4).
	RequiresCitation bool `json:"requiresCitation"`
	// RequiresOverrideReason is true when a non-default routing
	// requires a non-empty override_reason in the manifest (classes
	// whose default is "discarded").
	RequiresOverrideReason bool `json:"requiresOverrideReason"`
	// Guidance carries a short prose explanation the writer can use
	// to resolve override situations — e.g. "framework-quirk + self-
	// inflicted may re-route away from discarded only with a non-empty
	// override_reason reframing the fact as porter-facing".
	Guidance string `json:"guidance"`
}

// handleRecipeClassify dispatches the zerops_workflow action=classify
// runtime lookup. The handler is stateless — it doesn't need the
// engine or an active session. The writer sub-agent calls it per
// fact when classification is non-obvious (the classification-pointer
// atom in the writer brief directs it here).
func handleRecipeClassify(input WorkflowInput) (*mcp.CallToolResult, any, error) {
	factType := strings.TrimSpace(input.FactType)
	if factType == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"factType is required for action=classify",
			"Pass factType=<one of gotcha_candidate, ig_item_candidate, verified_behavior, platform_observation, fix_applied, cross_codebase_contract>. The type comes from the fact record the writer sub-agent is classifying.",
		), WithRecoveryStatus()), nil, nil
	}

	keywords := strings.ToLower(input.TitleKeywords)
	resp := classifyFactRuntime(factType, keywords)
	return jsonResult(resp), nil, nil
}

// classifyFactRuntime is the pure-function core of the classify
// handler — exposed so tests can exercise the classification logic
// without a full MCP dispatch. Input is factType (from ops.FactType*
// constants) and keywords (lowercased title tokens). Output is the
// classifyResponse the main handler wraps.
func classifyFactRuntime(factType, lowerKeywords string) classifyResponse {
	// Keyword-based overrides for the common "default route is wrong
	// for THIS fact" patterns. Order matters: self-inflicted checks
	// come before framework-quirk because a v38 CRIT-worthy self-
	// inflicted bug (silent seed exit) could otherwise slip through
	// as framework-quirk.

	if hasAnyKeyword(lowerKeywords,
		"silent seed", "silently exited", "exited 0 with no",
		"our code had", "fixed it to do", "our script",
	) {
		return classifyResponse{
			Classification:         "self-inflicted",
			DefaultRouteTo:         ops.FactRouteToDiscarded,
			RequiresCitation:       false,
			RequiresOverrideReason: true,
			Guidance:               "Self-inflicted: our code had a bug; a reasonable porter would not hit it because their code doesn't have that specific bug. Default route is discarded. Routing elsewhere requires a non-empty override_reason reframing the fact as porter-facing (a concrete failure mode tied to a platform mechanism). See spec-content-surfaces.md §7.",
		}
	}

	if hasAnyKeyword(lowerKeywords,
		"setglobalprefix", "peer-dep", "peerinvalid",
		"framework doc", "package.json", "controller decorator",
	) {
		return classifyResponse{
			Classification:         "framework-quirk",
			DefaultRouteTo:         ops.FactRouteToDiscarded,
			RequiresCitation:       false,
			RequiresOverrideReason: true,
			Guidance:               "Framework-quirk: the framework's own behavior with no Zerops involvement. Default route is discarded — framework docs or code comments are the right home. Override requires reframing so the Zerops side becomes material.",
		}
	}

	if hasAnyKeyword(lowerKeywords,
		"claude", "dev loop", "sshfs", "truncate", "reset dev",
		"iterate locally", "test locally",
	) {
		return classifyResponse{
			Classification:         "operational",
			DefaultRouteTo:         ops.FactRouteToClaudeMD,
			RequiresCitation:       false,
			RequiresOverrideReason: false,
			Guidance:               "Operational: how to iterate on, test, or reset THIS specific repo locally. Default route is claude_md — the per-codebase operational guide.",
		}
	}

	if hasAnyKeyword(lowerKeywords,
		"deployfiles", "httpsupport", "readinesscheck", "zeropssetup",
		"zerops.yaml", "initcommands", "execonce",
	) {
		// Scaffold decision in YAML is the most common case when
		// these keywords surface. Code-level principles ship as IG
		// items — but code-level phrasing usually names the app
		// code changes, not these config-field names.
		return classifyResponse{
			Classification:         "scaffold-decision",
			DefaultRouteTo:         ops.FactRouteToZeropsYAMLComment,
			RequiresCitation:       false,
			RequiresOverrideReason: false,
			Guidance:               "Scaffold-decision (YAML choice): a Zerops-config trade-off in the recipe's own zerops.yaml. Default route is zerops_yaml_comment. If the fact is a principle a porter should know in their own code (not our scaffold), reroute to content_ig with the principle-level rewrite. If it's operational (iteration-only), reroute to claude_md.",
		}
	}

	// Type-indexed defaults — matches inferLikelyRouteTo in record_fact.go
	// but returns the full classifyResponse shape.
	switch factType {
	case ops.FactTypeGotchaCandidate:
		return classifyResponse{
			Classification:         "framework-invariant",
			DefaultRouteTo:         ops.FactRouteToContentGotcha,
			RequiresCitation:       true,
			RequiresOverrideReason: false,
			Guidance:               "Framework-invariant: the fact is true of Zerops regardless of this recipe's framework or scaffold. Default route is content_gotcha (published as a knowledge-base gotcha). Citation required — every gotcha entry must carry at least one citation with a non-empty guide_fetched_at timestamp, OR the completion gate at complete substep=readmes refuses.",
		}
	case ops.FactTypeIGItemCandidate:
		return classifyResponse{
			Classification:         "framework-invariant",
			DefaultRouteTo:         ops.FactRouteToContentIG,
			RequiresCitation:       true,
			RequiresOverrideReason: false,
			Guidance:               "IG-item candidate: a concrete application-code change a porter must make in their own codebase. Default route is content_ig (published as an H3 integration-guide item). Citation required — every IG entry whose topic matches the Citation Map must carry at least one citation with a non-empty guide_fetched_at timestamp.",
		}
	case ops.FactTypeCrossCodebaseContract:
		return classifyResponse{
			Classification:         "framework-invariant",
			DefaultRouteTo:         ops.FactRouteToContentIG,
			RequiresCitation:       true,
			RequiresOverrideReason: false,
			Guidance:               "Cross-codebase contract: a shape binding (DB schema, NATS subject + queue group, HTTP response shape) that must stay in sync across codebases. Default route is content_ig; the host codebase's IG owns the contract and other codebases cross-reference.",
		}
	case ops.FactTypeFixApplied:
		return classifyResponse{
			Classification:         "framework-invariant",
			DefaultRouteTo:         ops.FactRouteToContentGotcha,
			RequiresCitation:       true,
			RequiresOverrideReason: false,
			Guidance:               "Fix-applied: a remedy applied to a non-obvious issue. Pairs with a gotcha_candidate (the symptom) — publish both together on the gotcha surface. If the fix was to a self-inflicted scaffold bug, reclassify as self-inflicted and discard.",
		}
	case ops.FactTypeVerifiedBehavior, ops.FactTypePlatformObservation:
		return classifyResponse{
			Classification:         "framework-invariant",
			DefaultRouteTo:         ops.FactRouteToZeropsYAMLComment,
			RequiresCitation:       false,
			RequiresOverrideReason: false,
			Guidance:               "Platform observation / verified behavior: a load-bearing platform fact that informs scaffold-decision-level content. Default route is zerops_yaml_comment (where the decision the observation informs lands). Reroute to content_gotcha if the observation surfaces a trap a porter would hit.",
		}
	}

	return classifyResponse{
		Classification:   "",
		DefaultRouteTo:   "",
		RequiresCitation: false,
		Guidance:         "Unknown fact type. Valid types: gotcha_candidate, ig_item_candidate, verified_behavior, platform_observation, fix_applied, cross_codebase_contract.",
	}
}

// hasAnyKeyword returns true iff at least one of the supplied
// substrings is present in haystack. Case-sensitive — callers
// pre-lowercase both sides.
func hasAnyKeyword(haystack string, substrings ...string) bool {
	for _, s := range substrings {
		if strings.Contains(haystack, s) {
			return true
		}
	}
	return false
}
