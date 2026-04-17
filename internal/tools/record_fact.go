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
}

// RegisterRecordFact registers the zerops_record_fact MCP tool.
func RegisterRecordFact(srv *mcp.Server, engine *workflow.Engine) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_record_fact",
		Description: "Record a structured fact discovered during deploy for the readmes sub-step writer to consume. Call when you encounter and fix a non-trivial issue, verify a non-obvious platform behavior, or establish a cross-codebase contract binding. The writer subagent at the end of deploy reads the accumulated log as pre-organized input — write facts at the moment of freshest knowledge, not in retrospect.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Record deploy-time fact",
			ReadOnlyHint:   false,
			IdempotentHint: false,
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, input RecordFactInput) (*mcp.CallToolResult, any, error) {
		if engine == nil {
			return textResult("Error: workflow engine not initialized"), nil, nil
		}
		sessionID := engine.SessionID()
		if sessionID == "" {
			return textResult("Error: no active workflow session — zerops_record_fact is only meaningful during an active recipe session"), nil, nil
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
		}
		path := ops.FactLogPath(sessionID)
		if err := ops.AppendFact(path, rec); err != nil {
			return textResult(fmt.Sprintf("Error recording fact: %v", err)), nil, nil
		}
		return textResult(fmt.Sprintf("Recorded %s fact: %q (session %s)", rec.Type, rec.Title, sessionID)), nil, nil
	})
}
