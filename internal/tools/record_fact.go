package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// RecordFactInput is the input for zerops_record_fact. v8.86 §3.1 — the agent
// calls this mid-deploy at the moment of freshest knowledge (when a fix is
// applied, when a platform behavior is verified, when a cross-codebase contract
// is established). The readmes sub-step's writer subagent reads the accumulated
// records as structured input instead of doing session-log archaeology.
type RecordFactInput struct {
	Type        string `json:"type"                  jsonschema:"required,One of: gotcha_candidate, ig_item_candidate, verified_behavior, platform_observation, fix_applied, cross_codebase_contract. gotcha_candidate for non-obvious failure modes the reader would trip on; ig_item_candidate for a concrete application-code change that belongs in the integration-guide; verified_behavior for a platform behavior confirmed by observation (e.g. execOnce lock resets per deploy); platform_observation for load-bearing platform facts (e.g. L7 balancer terminates SSL); fix_applied for the remedy applied to a non-obvious issue (pairs with a gotcha_candidate); cross_codebase_contract for shape bindings that must stay in sync across codebases (DB schema, NATS subject + queue group, HTTP response shape)."`
	Title       string `json:"title"                 jsonschema:"required,Short (< 100 chars) summary of the fact. Prefer mechanism + failure_mode format: 'execOnce: first-deploy no-op when ts-node module resolution fails silently'."`
	Substep     string `json:"substep,omitempty"     jsonschema:"The deploy sub-step where the fact emerged (deploy-dev, init-commands, subagent, browser-walk, cross-deploy). Optional but strongly recommended — the writer subagent uses it to decide which README codebase to attribute the fact to."`
	Codebase    string `json:"codebase,omitempty"    jsonschema:"The codebase hostname the fact belongs to (apidev, appdev, workerdev). Optional for cross-codebase facts."`
	Mechanism   string `json:"mechanism,omitempty"   jsonschema:"Named Zerops platform mechanism or framework-x-platform intersection that causes the behavior (execOnce, L7 balancer, ${db_hostname}, httpSupport, readinessCheck, SSHFS mount, advisory lock)."`
	FailureMode string `json:"failureMode,omitempty" jsonschema:"Concrete failure symptom: HTTP code, quoted error message, or strong-symptom verb (rejects, deadlocks, drops, crashes, times out, throws, returns 5xx, returns wrong content-type, hangs, silent no-op)."`
	FixApplied  string `json:"fixApplied,omitempty"  jsonschema:"Remedy applied. Include the exact config flip / file path / command when applicable."`
	Evidence    string `json:"evidence,omitempty"    jsonschema:"Log-line timestamps, deploy IDs, or curl output snippets that prove the behavior. Keeps the writer subagent from inventing evidence to fit the gotcha."`
	Scope       string `json:"scope,omitempty"       jsonschema:"One of: content, downstream, both. Defaults to 'content' (writer-only, the pre-v8.96 behavior). Set 'downstream' for framework/tooling discoveries that don't belong in published content but would waste downstream subagents' time if re-investigated (e.g. Meilisearch v0.57 renamed class from MeiliSearch to Meilisearch; svelte-check@4 incompatible with typescript@6 — $state shows untyped errors). Set 'both' sparingly when a fact is load-bearing on both lanes."`
	RouteTo     string `json:"routeTo,omitempty"     jsonschema:"Optional at record time — the published surface this fact belongs on. One of: content_gotcha, content_intro, content_ig, content_env_comment, claude_md, zerops_yaml_comment, scaffold_preamble, feature_preamble, discarded. When set, the writer subagent adopts the route by default and documents any override; when empty, the response includes a nudge inferring a likely route from 'type' so the caller can confirm or override. See docs/spec-content-surfaces.md §7 routing matrix."`
}

// inferLikelyRouteTo returns the default route the fact-recorder nudge
// suggests when the caller leaves RouteTo empty. Mapping is conservative —
// we match against FactRecord.Type since that's the strongest signal the
// caller already supplied. A nudge is not a refusal: the fact is recorded
// either way, and the writer subagent can always override.
//
// v39 Commit 4 — reduces the "all recorded facts arrive at the writer
// with empty RouteTo" class the facts log exhibited in v38 (writer had
// to re-classify every fact at dispatch time, re-doing work the recorder
// could have done at record time when classification is freshest).
func inferLikelyRouteTo(factType string) string {
	switch factType {
	case ops.FactTypeGotchaCandidate:
		return ops.FactRouteToContentGotcha
	case ops.FactTypeIGItemCandidate:
		return ops.FactRouteToContentIG
	case ops.FactTypeCrossCodebaseContract:
		return ops.FactRouteToContentIG
	case ops.FactTypeFixApplied:
		// fix_applied pairs with a gotcha; default to gotcha surface.
		return ops.FactRouteToContentGotcha
	case ops.FactTypeVerifiedBehavior, ops.FactTypePlatformObservation:
		// Platform-level observations most often surface as zerops.yaml
		// comments or gotchas; the writer has better context so suggest
		// zerops_yaml_comment as a conservative default.
		return ops.FactRouteToZeropsYAMLComment
	}
	return ""
}

// RegisterRecordFact registers the zerops_record_fact MCP tool.
//
// recipeProbe may be nil in tests that don't exercise the recipe path. When
// the v2 workflow engine has no active session but exactly one v3 recipe
// session is open, the tool routes the v2-shaped record into
// <outputRoot>/legacy-facts.jsonl so recipe sub-agents using this tool land
// inside the recipe run dir — the v3 structured facts log at
// <outputRoot>/facts.jsonl stays reserved for zerops_recipe action=record-fact.
func RegisterRecordFact(srv *mcp.Server, engine *workflow.Engine, recipeProbe RecipeSessionProbe) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_record_fact",
		Description: "v2 fact tool — use zerops_recipe action=record-fact for v3 recipe sessions; this tool is for the legacy bootstrap/develop workflow only. Records a structured fact discovered during deploy for the readmes sub-step writer to consume. Call when you encounter and fix a non-trivial issue, verify a non-obvious platform behavior, or establish a cross-codebase contract binding. The writer subagent at the end of deploy reads the accumulated log as pre-organized input — write facts at the moment of freshest knowledge, not in retrospect.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Record deploy-time fact",
			ReadOnlyHint:   false,
			IdempotentHint: false,
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, input RecordFactInput) (*mcp.CallToolResult, any, error) {
		sessionLabel, path, routeErr := resolveFactLogPath(engine, recipeProbe)
		if routeErr != "" {
			return textResult(routeErr), nil, nil
		}

		rec := ops.FactRecord{
			Type:        input.Type,
			Title:       input.Title,
			Substep:     input.Substep,
			Codebase:    input.Codebase,
			Mechanism:   input.Mechanism,
			FailureMode: input.FailureMode,
			FixApplied:  input.FixApplied,
			Evidence:    input.Evidence,
			Scope:       input.Scope,
			RouteTo:     input.RouteTo,
		}
		if err := ops.AppendFact(path, rec); err != nil {
			return textResult(fmt.Sprintf("Error recording fact: %v", err)), nil, nil
		}
		// Cx-ITERATE-GUARD: a recorded fact is the canonical "new evidence"
		// touchpoint that clears the post-iterate substep-complete gate.
		// Best-effort; a failure to flip the flag is non-fatal (the fact
		// itself landed) so don't escalate past the log. The flag only
		// exists on the v2 engine; skip when routing to a recipe session.
		if engine != nil && engine.SessionID() != "" {
			_ = engine.ClearAwaitingEvidenceAfterIterate()
		}

		// v39 Commit 4 — nudge (not refusal) when the caller leaves RouteTo
		// empty. The fact is already appended; the nudge surfaces the
		// inferred default so the caller can reinforce or override on the
		// next record_fact call. Infers from rec.Type because that's the
		// strongest signal the caller already supplied.
		msg := fmt.Sprintf("Recorded %s fact: %q (session %s)", rec.Type, rec.Title, sessionLabel)
		if rec.RouteTo == "" {
			if inferred := inferLikelyRouteTo(rec.Type); inferred != "" {
				msg += fmt.Sprintf(
					". Nudge: you didn't set routeTo. Based on type=%s the likely route is %q — if that fits, pass routeTo=%q on future records of this class. Route is advisory at record time; the writer subagent re-classifies with override_reason at manifest time.",
					rec.Type, inferred, inferred,
				)
			}
		}
		return textResult(msg), nil, nil
	})
}

// resolveFactLogPath returns (sessionLabel, path, "") when a destination
// for the fact was resolved — the v2 engine's /tmp log. When a v3 recipe
// session is open, the legacy "courtesy route" to <outputRoot>/legacy-facts.jsonl
// is gone (run-11 gap U-1): the silent route hid 5 hard-won discoveries
// from the v3 stitch pipeline in run 10 because nothing reads
// legacy-facts.jsonl. We refuse with a redirect naming the v3 action +
// schema so the sub-agent re-records via zerops_recipe action=record-fact.
func resolveFactLogPath(engine *workflow.Engine, recipeProbe RecipeSessionProbe) (string, string, string) {
	if engine != nil {
		if sid := engine.SessionID(); sid != "" {
			return sid, ops.FactLogPath(sid), ""
		}
	}
	if recipeProbe != nil {
		if slug, _, _, ok := recipeProbe.CurrentSingleSession(); ok {
			return "", "", fmt.Sprintf(
				"Error: zerops_record_fact is the v2 fact tool. A v3 recipe session is open (slug=%s) — use zerops_recipe action=record-fact slug=%s instead. v3 schema: topic, symptom, mechanism, failureMode, fixApplied, evidence, scope, surfaceHint, citation.",
				slug, slug,
			)
		}
		if recipeProbe.HasAnySession() {
			return "", "", "Error: multiple recipe sessions open — zerops_record_fact cannot infer the target; use zerops_recipe action=record-fact with an explicit slug"
		}
	}
	return "", "", "Error: no active workflow session — zerops_record_fact is only meaningful during an active recipe session"
}
