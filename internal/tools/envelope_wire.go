package tools

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// Envelope on the wire — see docs/spec-mate.md.
//
// A workflow-aware tool result carries its lifecycle StateEnvelope as a
// fenced `json zcp-envelope` block at the end of the result TEXT, so the mate
// client can rebuild a thread's lifecycle state by reducing over the
// provider's tool-result stream. It rides in the text rather than in MCP's
// `structuredContent` because Claude Code replaces the model-facing result
// with `structuredContent` when present, which would strip the synthesized
// atom guidance the status renders. Guard:
// TestNoStructuredContentOnToolResults.

// statusResult renders a lifecycle Response and appends the envelope it was
// rendered from. Every handler that already owns an envelope returns through
// here, so the rendered markdown and the machine-readable state can never
// describe different moments.
func statusResult(resp workflow.Response) *mcp.CallToolResult {
	return textResult(workflow.AppendEnvelope(workflow.RenderStatus(resp), resp.Envelope))
}

// withFreshEnvelope appends a freshly computed StateEnvelope to a successful
// mutation's result, so the lifecycle strip advances on the mutation itself
// rather than waiting for the agent's next `action="status"` call.
//
// It is deliberately total: a nil/error result, a non-text payload, or a
// failed envelope computation returns the result UNCHANGED. The envelope is
// an addendum — it must never fail, truncate or reshape the tool's own
// answer. Computation failures go to stderr (JSON-only stdout).
//
// Error results are skipped by design: an error is a leaf payload
// (spec-workflows P4), and appending state to one would let the reducer read
// a failed call as fresh truth.
func withFreshEnvelope(
	ctx context.Context,
	result *mcp.CallToolResult,
	stateDir string,
	client platform.Client,
	projectID string,
	rt runtime.Info,
) *mcp.CallToolResult {
	if result == nil || result.IsError {
		return result
	}
	// Only a single-text-block result can carry the trailing fence; a JSON
	// payload would stop parsing as JSON and a multi-block result has no
	// unambiguous "end".
	if len(result.Content) != 1 {
		return result
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		return result
	}

	envelope := freshEnvelope(ctx, stateDir, client, projectID, rt)
	if envelope == nil {
		return result
	}
	return textResult(workflow.AppendEnvelope(text.Text, *envelope))
}

// freshEnvelope computes the post-mutation StateEnvelope a JSON-document
// result carries under its `envelope` key. It is the JSON-carrier twin of
// withFreshEnvelope and keeps the same discipline: on failure it returns nil,
// the `omitempty` field drops out, and the result stays byte-identical to
// what the tool produced before it carried an envelope at all. The tool's own
// answer is never altered by an envelope problem; the reason goes to stderr
// (JSON-only stdout).
//
// Call it only after a SUCCESSFUL mutation — the point is to describe the
// state the mutation produced.
func freshEnvelope(
	ctx context.Context,
	stateDir string,
	client platform.Client,
	projectID string,
	rt runtime.Info,
) *workflow.StateEnvelope {
	envelope, err := workflow.ComputeEnvelope(ctx, client, stateDir, projectID, rt, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "zcp: compute envelope for tool result: %v\n", err)
		return nil
	}
	return &envelope
}

// bootstrapResponse carries a BootstrapResponse verbatim (embedded, so every
// existing field keeps its name and shape) plus the post-step lifecycle
// envelope. docs/spec-mate.md §1.3.
type bootstrapResponse struct {
	*workflow.BootstrapResponse
	Envelope *workflow.StateEnvelope `json:"envelope,omitempty"`
}

// bootstrapDiscoveryResponse is bootstrapResponse's twin for the routeless
// discovery pass of action="start", which answers with a different shape.
type bootstrapDiscoveryResponse struct {
	*workflow.BootstrapDiscoveryResponse
	Envelope *workflow.StateEnvelope `json:"envelope,omitempty"`
}

// bootstrapResult renders a BootstrapResponse with a fresh envelope attached.
// The bootstrap handlers all hold an engine, so stateDir comes from it.
func bootstrapResult(
	ctx context.Context,
	resp *workflow.BootstrapResponse,
	engine *workflow.Engine,
	client platform.Client,
	projectID string,
	rt runtime.Info,
) *mcp.CallToolResult {
	return jsonResult(bootstrapResponse{
		BootstrapResponse: resp,
		Envelope:          freshEnvelope(ctx, engine.StateDir(), client, projectID, rt),
	})
}
