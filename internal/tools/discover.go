package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// DiscoverInput is the input type for zerops_discover.
//
// IncludeEnvs and IncludeEnvValues are FlexBool so the MCP schema
// accepts both JSON booleans and their stringified forms — some LLM
// agents serialize tool arguments with quoted primitives and the
// earlier bool-only schema rejected them with a non-actionable error
// (seen in the v7 post-mortem log, attempt 1 of discover failed with
// `includeEnvs has type "string", want "boolean"`). The explicit
// InputSchema below publishes a matching `oneOf: [boolean, string]`
// so the protocol-level validator agrees.
type DiscoverInput struct {
	Service          string   `json:"service,omitempty"`
	IncludeEnvs      FlexBool `json:"includeEnvs,omitempty"`
	IncludeEnvValues FlexBool `json:"includeEnvValues,omitempty"`
}

// discoverInputSchema is the explicit InputSchema for zerops_discover.
// Hand-written so we can mark the two env toggles as `oneOf: [boolean,
// string-enum]` — the reflection-based schema would have rejected the
// stringified form used by some agents. Field descriptions live here
// rather than on struct tags so the two are co-located with the schema
// types they describe.
func discoverInputSchema() *jsonschema.Schema {
	return objectSchema(map[string]*jsonschema.Schema{
		"service": {
			Type:        "string",
			Description: "Filter by service hostname. Omit to list all services in the project. When discovering env vars for multiple services, omit this parameter — one call returns all.",
		},
		"includeEnvs":      flexBoolSchema("Include env var keys (service-level and project-level), plus a live runtime's yaml-baked run.envVariables tagged source=\"zerops.yaml\" (the GUI \"from master\" layer the slim API omits). Returns keys and annotations only — no values."),
		"includeEnvValues": flexBoolSchema("Also include actual env var values. Use only for troubleshooting when keys-only is insufficient (e.g. empty values, wrong formats, unresolved refs). For .env generation use zerops_env generate-dotenv instead."),
	})
}

// RegisterDiscover registers the zerops_discover tool.
func RegisterDiscover(srv *mcp.Server, client platform.Client, projectID, stateDir string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_discover",
		Description: "Discover project and service information. Filter by service hostname or list all. Use includeEnvs=true to read env var keys. Add includeEnvValues=true only when you need actual secret values (troubleshooting).",
		InputSchema: discoverInputSchema(),
		Annotations: &mcp.ToolAnnotations{
			Title:          "Discover project and services",
			ReadOnlyHint:   true,
			IdempotentHint: true,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DiscoverInput) (*mcp.CallToolResult, any, error) {
		// includeProjectEnvs=true: the discover tool always surfaces project
		// envs alongside scoped service envs so agents can read launch /
		// debug / env-wiring context in one call. The ops layer defaults
		// this false so zerops_env action="get" (which delegates to Discover)
		// keeps its narrow service-only contract — see env.go.
		result, err := ops.Discover(ctx, client, projectID, input.Service, input.IncludeEnvs.Bool(), input.IncludeEnvValues.Bool(), true)
		if err != nil {
			return convertError(err), nil, nil
		}
		enrichWithMetaStatus(result, stateDir)
		return jsonResult(result), nil, nil
	})
}

// enrichWithMetaStatus classifies each service into one of six
// AdoptionState values (adopted / resumable / adoptable / managed-dep
// / zcp-self / bootstrapping), detects SSHFS mount paths, and appends directive
// warnings to result.Warnings for adoptable and/or resumable services
// (separate warnings — each points at the correct workflow route).
//
// Plan: plans/discover-adoption-state-enum-2026-05-27.md.
//
// Classification order matches the documented precedence in the plan
// + workflow.adoptableServices / workflow.resumeOption semantics
// (internal/workflow/route.go). Each branch is mutually exclusive.
func enrichWithMetaStatus(result *ops.DiscoverResult, stateDir string) {
	// Detect mounts regardless of stateDir.
	for i := range result.Services {
		path := "/var/www/" + result.Services[i].Hostname
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			result.Services[i].MountPath = path
		}
	}

	// Pair-keyed meta index: both halves of a standard-mode pair
	// resolve to the shared meta (spec-workflows.md §8 E8).
	var idx map[string]*workflow.ServiceMeta
	if stateDir != "" {
		metas, err := workflow.ListServiceMetas(stateDir)
		if err == nil && len(metas) > 0 {
			idx = workflow.ManagedRuntimeIndex(metas)
		}
	}

	// Services planned by an alive bootstrap session but not yet meta-stamped
	// (the import→provision-complete window) are mid-bootstrap, not adoptable.
	inFlight := workflow.InFlightBootstrapHostnames(stateDir)

	// Classify every service into exactly one AdoptionState.
	var adoptCandidates []string
	var adoptTypes []string // parallel to adoptCandidates — live Type per candidate (D pairing predicate)
	var resumeCandidates []resumeCandidate
	for i := range result.Services {
		s := &result.Services[i]
		state, sessionID := classifyAdoptionState(s, idx, inFlight)
		s.AdoptionState = state
		switch state {
		case ops.AdoptionAdoptable:
			adoptCandidates = append(adoptCandidates, s.Hostname)
			adoptTypes = append(adoptTypes, s.Type)
		case ops.AdoptionResumable:
			resumeCandidates = append(resumeCandidates, resumeCandidate{
				Hostname:  s.Hostname,
				SessionID: sessionID,
			})
		case ops.AdoptionAdopted, ops.AdoptionManagedDep, ops.AdoptionZCPSelf, ops.AdoptionBootstrapping:
			// No warning needed; per-service AdoptionState already
			// carries the agent-actionable signal. AdoptionBootstrapping
			// is silent on purpose — the agent's own alive session is
			// mid-provision on it, so neither adopt nor resume applies.
		}
	}

	if len(adoptCandidates) > 0 {
		result.Warnings = append(result.Warnings, adoptableServicesWarning(adoptCandidates, adoptTypes))
	}
	if len(resumeCandidates) > 0 {
		result.Warnings = append(result.Warnings, formatResumeWarning(resumeCandidates))
	}
}

// adoptableServicesWarning builds the adopt-route steer for live-but-untracked
// services. Both the op CLASS it names and the same-stack-pair pre-steer DERIVE
// from the facts the adopt CHECK uses, so the TELL cannot drift from it:
//   - B-fix: the op class is "every service-scoped mutate/promote call" — deploy,
//     develop, build-integration, AND launch-production — all reject with
//     ADOPT_REQUIRED. An earlier enumerated list dropped launch-production, so an
//     agent promoting to prod walked into a service-not-bootstrapped bounce with
//     no up-front warning that launch is adoption-gated too.
//   - D-fix: when exactly two adoptable runtimes share a deployment stack, a bare
//     scope=[...] adopt is GUARANTEED to reject — the adopt handler refuses to
//     guess a standard dev/stage pair vs two independent devs (adopt.go,
//     topology.CanonicalBareForm equality). Pre-steer to the explicit plan shape
//     the handler accepts instead of the scope path it will bounce.
func adoptableServicesWarning(hostnames, types []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Services with adoptionState=\"adoptable\" (live but not tracked by ZCP): %s. ", strings.Join(hostnames, ", "))
	b.WriteString("Run `zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\"` before any service-scoped MUTATING or PROMOTING call — `zerops_deploy`, the `develop` / `build-integration` workflows, AND `launch-production` (promote-to-prod) all reject with ADOPT_REQUIRED until adoption completes. ")
	if len(hostnames) == 2 && topology.CanonicalBareForm(types[0]) == topology.CanonicalBareForm(types[1]) {
		fmt.Fprintf(&b, "NOTE: %s and %s share the same runtime stack (%s) — a bare `scope=[...]` adopt can't tell a standard dev/stage pair from two independent dev containers and WILL reject; on the discover-complete call submit an explicit `plan=[...]` (one entry for a standard pair, two for independent devs), not `scope`. ", hostnames[0], hostnames[1], topology.CanonicalBareForm(types[0]))
	}
	b.WriteString("Read-only diagnostics work pre-adopt — for a reported URL/HTTP problem run `zerops_verify` FIRST (it carries the exact Recovery call), and `zerops_discover` / `zerops_events` / `zerops_logs` are all usable before adopting.")
	return b.String()
}

// resumeCandidate carries the per-host BootstrapSession ID so the
// resume-route warning can name the exact `sessionId=<...>` value the
// `zerops_workflow action=start workflow=bootstrap route=resume`
// handler requires (`internal/tools/workflow.go:961` rejects route=
// resume without sessionId). Without naming the session ID up front,
// the agent would hit INVALID_PARAMETER on the obvious follow-up call.
type resumeCandidate struct {
	Hostname  string
	SessionID string
}

// classifyAdoptionState returns the AdoptionState bucket for a service
// plus the BootstrapSession ID when the state is Resumable (empty
// otherwise). Precedence: zcp-self first (USER category, would slip past
// IsInfrastructure), managed-dep second, complete meta → adopted third,
// then `inFlight` (an alive provision-reached session owns this hostname)
// → bootstrapping — checked BEFORE incomplete-with-session → resumable so an
// alive session's own service is never sent down the route=resume dead end;
// resumable is reserved for a DEAD session's incomplete meta. Default
// adoptable last (matches workflow.adoptableServices semantics in
// internal/workflow/route.go for orphan metas).
func classifyAdoptionState(s *ops.ServiceInfo, idx map[string]*workflow.ServiceMeta, inFlight map[string]bool) (ops.AdoptionState, string) {
	if s.Type == "zcp@1" {
		return ops.AdoptionZCPSelf, ""
	}
	if s.IsInfrastructure {
		return ops.AdoptionManagedDep, ""
	}
	meta := idx[s.Hostname]
	if meta != nil && meta.IsComplete() {
		return ops.AdoptionAdopted, ""
	}
	// Not a completed bootstrap (no meta, orphan meta, or partial meta with a
	// BootstrapSession). If an ALIVE bootstrap session that has reached
	// provision is creating this hostname, it's mid-bootstrap on the agent's
	// OWN alive session — silent (AdoptionBootstrapping), regardless of whether
	// the provision-step partial meta has been stamped yet. This MUST be
	// checked before the meta.BootstrapSession→resumable branch: that branch
	// is route=resume territory only for a DEAD session's incomplete bootstrap
	// (resume is rejected for an alive session, so emitting it here would loop
	// the agent into a dead end).
	if inFlight[s.Hostname] {
		return ops.AdoptionBootstrapping, ""
	}
	if meta != nil && meta.BootstrapSession != "" {
		// A prior, no-longer-alive session left an incomplete bootstrap.
		return ops.AdoptionResumable, meta.BootstrapSession
	}
	// No meta, or an orphan meta with empty BootstrapSession.
	return ops.AdoptionAdoptable, ""
}

// formatResumeWarning composes the resume-route warning prose. When
// every resumable service shares one BootstrapSession ID, name it
// directly. When multiple session IDs are present, list each
// hostname=sessionId pair so the agent can call route=resume
// per-session.
func formatResumeWarning(candidates []resumeCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	sessions := map[string]bool{}
	for _, c := range candidates {
		sessions[c.SessionID] = true
	}
	hosts := make([]string, len(candidates))
	for i, c := range candidates {
		hosts[i] = c.Hostname
	}
	if len(sessions) == 1 {
		// Single session — name it once.
		one := candidates[0].SessionID
		return fmt.Sprintf(
			"Services with adoptionState=\"resumable\" (mid-bootstrap, owned by a prior session): %s. "+
				"Run `zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"resume\" sessionId=\"%s\"` "+
				"to continue the incomplete bootstrap — route=\"adopt\" would reject these as ZCP-owned, and route=\"resume\" without sessionId rejects with INVALID_PARAMETER.",
			strings.Join(hosts, ", "), one,
		)
	}
	// Multiple sessions — enumerate.
	pairs := make([]string, len(candidates))
	for i, c := range candidates {
		pairs[i] = fmt.Sprintf("%s (sessionId=%s)", c.Hostname, c.SessionID)
	}
	return fmt.Sprintf(
		"Services with adoptionState=\"resumable\" (mid-bootstrap, owned by prior sessions): %s. "+
			"Run `zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"resume\" sessionId=\"<sessionId>\"` "+
			"once per session — route=\"adopt\" would reject these as ZCP-owned.",
		strings.Join(pairs, ", "),
	)
}
