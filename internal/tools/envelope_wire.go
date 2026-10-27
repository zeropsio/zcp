package tools

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
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
// describe different moments. It is action="status"'s carrier, so the
// envelope keeps the whole attempt history.
func statusResult(resp workflow.Response) *mcp.CallToolResult {
	return textResult(workflow.AppendEnvelope(workflow.RenderStatus(resp), wireEnvelope(resp.Envelope)))
}

// briefingResult is statusResult for the develop briefing, a reply other than
// action="status": the markdown renders from the whole envelope, the fence
// carries replyEnvelope.
func briefingResult(resp workflow.Response) *mcp.CallToolResult {
	return textResult(workflow.AppendEnvelope(workflow.RenderStatus(resp), replyEnvelope(resp.Envelope)))
}

// wireEnvelope is the envelope as this zcp sends it: from field-order step
// (3) of docs/spec-mate.md §2.8a.3 a pair's stages ride stages[] in place of
// stageHostname/stageSetupName (mate.EnvelopeCarriesParts).
func wireEnvelope(env workflow.StateEnvelope) workflow.StateEnvelope {
	if !mate.EnvelopeCarriesParts() {
		return env
	}
	return env.WithParts(nil)
}

// replyEnvelope is the envelope a reply other than action="status" carries:
// wireEnvelope with each host's deploy and verify history cut to its last
// attempt, every host kept. Mate reads only the last attempt and which hosts
// have one (docs/spec-mate.md §1.4); action="status", the recovery primitive,
// keeps the whole history.
func replyEnvelope(env workflow.StateEnvelope) workflow.StateEnvelope {
	env = wireEnvelope(env)
	if env.WorkSession == nil {
		return env
	}
	ws := *env.WorkSession
	ws.Deploys = lastAttempts(ws.Deploys)
	ws.Verifies = lastAttempts(ws.Verifies)
	env.WorkSession = &ws
	return env
}

func lastAttempts(byHost map[string][]workflow.AttemptInfo) map[string][]workflow.AttemptInfo {
	if byHost == nil {
		return nil
	}
	out := make(map[string][]workflow.AttemptInfo, len(byHost))
	for host, attempts := range byHost {
		out[host] = attempts[max(len(attempts)-1, 0):]
	}
	return out
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
// The envelope carries the project's pool-container leases (leases[], docs/spec-mate.md
// §2.8a.3) like action="status" does: the default lease reader asks the pool
// orchestrators and checks a held lease's alias and mount on this host
// (envelopeLeaseHost), an extra option (the lease tool's own orchestrator
// factory and host side) replaces it.
//
// Call it only after a SUCCESSFUL mutation — the point is to describe the
// state the mutation produced.
func freshEnvelope(
	ctx context.Context,
	stateDir string,
	client platform.Client,
	projectID string,
	rt runtime.Info,
	extra ...workflow.EnvelopeOption,
) *workflow.StateEnvelope {
	envelope, err := workflow.ComputeEnvelope(ctx, client, stateDir, projectID, rt, time.Now(),
		leaseEnvelopeOptions(client, projectID, stateDir, rt, envelopeLeaseTimeout, extra...)...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zcp: compute envelope for tool result: %v\n", err)
		return nil
	}
	envelope = replyEnvelope(envelope)
	return &envelope
}

// leaseEnvelopeOptions are the options every envelope zcp computes starts
// from: the default lease reader — the project's leases reconciled against
// the pool orchestrators within timeout, a held lease's alias and mount
// checked on this host (envelopeLeaseHost) — then extra, a later reader
// replacing it. A stage whose pair's dev half is a pool deploys from a held
// lease of it, so an envelope without the leases plans a lease that is
// already held (docs/spec-workflows.md §13 PM-10).
func leaseEnvelopeOptions(client platform.Client, projectID, stateDir string, rt runtime.Info, timeout time.Duration, extra ...workflow.EnvelopeOption) []workflow.EnvelopeOption {
	return append([]workflow.EnvelopeOption{workflow.WithLeaseReader(leaseReader(client, projectID, stateDir, envelopeLeaseHost(rt), timeout))}, extra...)
}

// envelopeLeaseHost is the host side an envelope's lease reader checks a held
// lease's ssh alias and mount on, so leases[] reads `unmounted` wherever
// zerops_lease action=status does (DP-2): inside a container the system
// mounter (what cmd/zcp hands the server there) and the alias file; elsewhere
// none, as a lease has neither there. It only reads — /proc/mounts and the
// alias file.
func envelopeLeaseHost(rt runtime.Info) ops.LeaseHost {
	if !rt.InContainer {
		return ops.LeaseHost{}
	}
	return ops.NewLeaseHost(platform.NewSystemMounter())
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
	// A Mate's classic discover says the pair rule up front (hq_pair.go).
	appendHQPairDiscoverNote(resp, engine, rt.MateEnabled)
	return jsonResult(bootstrapResponse{
		BootstrapResponse: resp,
		Envelope:          freshEnvelope(ctx, engine.StateDir(), client, projectID, rt),
	})
}

// bootstrapResultWithRepoStatus is bootstrapResult's twin for the one call
// site that must carry services[].repo in the SAME response that promises
// the agent to check it: the adopt route's provision-complete response
// (buildAdoptionTransitionMessage, the bootstrap-adopt-baseline-commit atom
// — docs/spec-workflows.md §8 GLC-7). It attaches a live repo read via
// attachRepoStatus to the freshly computed envelope's Services before
// embedding it — the same tools-layer helper handleLifecycleStatus uses for
// action="status" — so the promise and the payload can never describe
// different moments. Every other bootstrap response routes through
// bootstrapResult and stays byte-identical; only this call site pays the
// per-service SSH round trips (bounded, concurrent — attachRepoStatusConcurrency).
func bootstrapResultWithRepoStatus(
	ctx context.Context,
	resp *workflow.BootstrapResponse,
	engine *workflow.Engine,
	client platform.Client,
	projectID string,
	rt runtime.Info,
	sshDeployer ops.SSHDeployer,
) *mcp.CallToolResult {
	envelope := freshEnvelope(ctx, engine.StateDir(), client, projectID, rt)
	if envelope != nil {
		attachRepoStatus(ctx, envelope.Services, sshDeployer, rt, engine.StateDir())
	}
	return jsonResult(bootstrapResponse{
		BootstrapResponse: resp,
		Envelope:          envelope,
	})
}
