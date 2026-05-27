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
		"includeEnvs":      flexBoolSchema("Include env var keys (service-level and project-level). Returns keys and annotations only — no values. Sufficient for bootstrap, deploy, recipe validation."),
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

// enrichWithMetaStatus classifies each service into one of five
// AdoptionState values (adopted / resumable / adoptable / managed-dep
// / zcp-self), detects SSHFS mount paths, and appends directive
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

	// Classify every service into exactly one AdoptionState.
	var adoptCandidates []string
	var resumeCandidates []resumeCandidate
	for i := range result.Services {
		s := &result.Services[i]
		state, sessionID := classifyAdoptionState(s, idx)
		s.AdoptionState = state
		switch state {
		case ops.AdoptionAdoptable:
			adoptCandidates = append(adoptCandidates, s.Hostname)
		case ops.AdoptionResumable:
			resumeCandidates = append(resumeCandidates, resumeCandidate{
				Hostname:  s.Hostname,
				SessionID: sessionID,
			})
		case ops.AdoptionAdopted, ops.AdoptionManagedDep, ops.AdoptionZCPSelf:
			// No warning needed; per-service AdoptionState already
			// carries the agent-actionable signal.
		}
	}

	if len(adoptCandidates) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"Services with adoptionState=\"adoptable\" (live but not tracked by ZCP): %s. "+
				"Run `zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\"` "+
				"BEFORE any service-scoped work — those calls will reject with ADOPT_REQUIRED until adoption completes.",
			strings.Join(adoptCandidates, ", "),
		))
	}
	if len(resumeCandidates) > 0 {
		result.Warnings = append(result.Warnings, formatResumeWarning(resumeCandidates))
	}
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
// otherwise). Order matches the plan's documented precedence: zcp-self
// first (USER category, would slip past IsInfrastructure), managed-dep
// second, complete meta third, incomplete-with-session fourth,
// default adoptable last (matches workflow.adoptableServices semantics
// in internal/workflow/route.go for orphan metas).
func classifyAdoptionState(s *ops.ServiceInfo, idx map[string]*workflow.ServiceMeta) (ops.AdoptionState, string) {
	if s.Type == "zcp@1" {
		return ops.AdoptionZCPSelf, ""
	}
	if s.IsInfrastructure {
		return ops.AdoptionManagedDep, ""
	}
	meta, ok := idx[s.Hostname]
	if !ok || meta == nil {
		return ops.AdoptionAdoptable, ""
	}
	if meta.IsComplete() {
		return ops.AdoptionAdopted, ""
	}
	if meta.BootstrapSession != "" {
		return ops.AdoptionResumable, meta.BootstrapSession
	}
	// Incomplete meta with empty BootstrapSession is an orphan —
	// workflow.adoptableServices treats it as adoptable (route.go:309
	// "Incomplete meta with BootstrapSession tag is resumable, not
	// adoptable" — empty session falls through).
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
