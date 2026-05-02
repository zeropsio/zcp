package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// ExportEnvelopeOpts carries the upstream-data accessors BuildExportEnvelope
// needs to populate the Services slice for a known target. Caller is
// expected to be the export tool handler, which already holds the
// platform client + projectID + stateDir from its own setup.
type ExportEnvelopeOpts struct {
	Client    platform.Client
	ProjectID string
	StateDir  string
}

// BuildExportEnvelope returns the typed StateEnvelope for the export
// workflow's per-call render. Single-entry Services semantics per audit
// in synthesize_export_audit.md — no atom in the corpus needs to reason
// about non-target services, so the envelope carries either an empty
// Services slice (scope-prompt: target unknown) or one snapshot for the
// chosen targetServiceHostname.
//
// Service-scoped axes (runtimes:, serviceStatus:, closeDeployModes:,
// gitPushStates:, buildIntegrations:, modes:) on export atoms therefore
// fire on the SAME service the agent is exporting — no silent-non-firing
// problem the way SynthesizeImmediatePhase had (which passed no service
// context at all).
//
// The empty-target branch deliberately skips the ListServices call —
// scope-prompt is the very response telling the agent which runtime is
// available, so re-fetching mid-render would be redundant; the handler
// runs Discover anyway to assemble the runtime list for the response
// payload.
//
// Errors:
//   - opts.Client == nil with a non-empty target → "client unavailable"
//   - service not found in project → identifies the missing hostname
//   - ListServices network/auth failures → wrapped through.
func BuildExportEnvelope(
	ctx context.Context,
	targetServiceHostname string,
	status topology.ExportStatus,
	opts ExportEnvelopeOpts,
) (StateEnvelope, error) {
	env := StateEnvelope{
		Phase:        PhaseExportActive,
		ExportStatus: status,
	}
	if targetServiceHostname == "" {
		return env, nil
	}
	if opts.Client == nil {
		return StateEnvelope{}, fmt.Errorf("BuildExportEnvelope: opts.Client is required for non-empty target %q", targetServiceHostname)
	}
	services, err := opts.Client.ListServices(ctx, opts.ProjectID)
	if err != nil {
		return StateEnvelope{}, fmt.Errorf("BuildExportEnvelope: ListServices: %w", err)
	}
	for _, svc := range services {
		if svc.Name != targetServiceHostname {
			continue
		}
		meta, err := FindServiceMeta(opts.StateDir, targetServiceHostname)
		if err != nil {
			return StateEnvelope{}, fmt.Errorf("BuildExportEnvelope: FindServiceMeta: %w", err)
		}
		env.Services = []ServiceSnapshot{buildOneSnapshot(svc, meta, nil)}
		return env, nil
	}
	return StateEnvelope{}, fmt.Errorf("BuildExportEnvelope: service %q not found in project %q", targetServiceHostname, opts.ProjectID)
}

// RenderExportGuidance composes the agent-facing guidance body by
// running Synthesize over the export-active corpus and joining matched
// atoms with the same separator SynthesizeImmediateWorkflow uses. The
// caller (handleExport) chains this output into the response `guidance`
// field; structural data (nextSteps URLs, hostnames) stays as inline
// JSON the handler builds separately.
//
// Returns an empty string when no atom matches — the caller decides
// whether that's an error (it usually is for export-active, since
// export-intro has no exportStatus filter and should always fire).
func RenderExportGuidance(env StateEnvelope, corpus []KnowledgeAtom) (string, error) {
	matches, err := Synthesize(env, corpus)
	if err != nil {
		return "", err
	}
	return strings.Join(BodiesOf(matches), "\n\n---\n\n"), nil
}
