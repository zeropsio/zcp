package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/runtime"
)

// ObserveInput is HQ's fixed read contract; credentials never enter model input.
type ObserveInput = hq.ObservationRequest

// RegisterObserve needs no SSH. HQ authorizes each request with the profile
// child, current native parent proof and current target permissions.
func RegisterObserve(srv *mcp.Server, httpClient hq.Doer, projectID, stateDir string, rt runtime.Info) {
	// Availability is not authorization. Keep the container flag surface, and
	// allow a standalone profile with an explicitly configured HQ and source.
	if !rt.InContainer || !rt.MateEnabled {
		id := hqIdentity(rt)
		if rt.InContainer || id.Origin == "" || id.ProjectID == "" {
			return
		}
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_observe",
		Description: "Read stage/production status, processes, versions and complete native logs through HQ with automatic ZCP profile authorization and current target permissions. For failed builds, follow process.versionId to build_logs for build/preparation; active runtime is separate. Log from/till accept record IDs or ISO timestamps. environments includes known changes; absent/changesAsLastRead means unknown, so refresh before merge advice. Errors distinguish input, unavailability and refusal.",
		Annotations: &mcp.ToolAnnotations{Title: "Observe stage and production", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false)},
		InputSchema: observeInputSchema(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ObserveInput) (*mcp.CallToolResult, any, error) {
		failure := func(message string) *mcp.CallToolResult {
			return observeFailure(message + "; fix it and call zerops_observe again.")
		}
		in, err := in.Normalize()
		if err != nil {
			message := err.Error()
			return failure(strings.ToUpper(message[:1]) + message[1:]), nil, nil
		}
		id := hqIdentity(rt)
		if id.ProjectID != projectID {
			return failure("Configured HQ source belongs to another project"), nil, nil
		}
		client, err := hq.OpenObservation(httpClient, id)
		if err != nil {
			return failure(fmt.Sprintf("HQ could not be addressed: %v", err)), nil, nil
		}

		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		body, err := client.Observe(ctx, in)
		if err != nil {
			return readFailure(err, failure), nil, nil
		}
		if in.Action == "environments" {
			// Existing own-project change metadata retains its native authority.
			// This runs only AFTER Observe succeeds; it can never authorize an
			// observation or substitute for the profile credential.
			changes, asLastRead, _ := statusHQChanges(ctx, httpClient, rt, stateDir, hqAddress(rt), time.Now(), false)
			var facts map[string]json.RawMessage
			if err = json.Unmarshal(body, &facts); err == nil {
				facts["changes"], err = json.Marshal(changes)
				if err == nil {
					facts["changesAsLastRead"], err = json.Marshal(asLastRead)
				}
				if err == nil {
					body, err = json.Marshal(facts)
				}
			}
		}

		if err != nil {
			return failure(fmt.Sprintf("Observation could not be rendered: %v", err)), nil, nil
		}
		if len(body) > hq.ObserveOutputLimit {
			return failure("Observation exceeds the output limit"), nil, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
	})
}

// Derive from HQ's request tags, but keep optional boundaries non-nullable.
// Pointers distinguish an omitted boundary from an explicitly empty string;
// schema inference otherwise also admits null, which HQ's contract rejects.
func observeInputSchema() *jsonschema.Schema {
	const stringType = "string"
	s, err := jsonschema.For[ObserveInput](nil)
	if err != nil {
		return nil
	}
	for _, name := range []string{"from", "till"} {
		if property := s.Properties[name]; property != nil {
			property.Type = stringType
			property.Types = nil
		}
	}
	return s
}

func observeFailure(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// readFailure distinguishes an unavailable HQ from a refused read or output bound.
// Neither path retries; authorization and input failures remain visible.
func readFailure(err error, failure func(string) *mcp.CallToolResult) *mcp.CallToolResult {
	var unavailable *hq.UnavailableError
	if errors.As(err, &unavailable) {
		if unavailable.Code == "zerops_unavailable" {
			return observeFailure("HQ could not read Zerops right now (zerops_unavailable); try again later — nothing to fix on your side.")
		}
		detail := unavailable.Code
		if detail == "" && unavailable.Err != nil {
			detail = unavailable.Err.Error()
		}
		return observeFailure(fmt.Sprintf("HQ is not serving right now (%s); try again later — nothing to fix on your side.", detail))
	}
	var refused *hq.RefusedError
	if errors.As(err, &refused) {
		if refused.Code == "observation_output_too_large" {
			return observeFailure(fmt.Sprintf("HQ refused the observation: %d observation_output_too_large; reduce limit or narrow from/till and try zerops_observe again. A single record over the output limit cannot be returned.", refused.Status))
		}
		refusal := fmt.Sprintf("%d %s", refused.Status, refused.Code)
		if refused.Reason != "" && refused.Reason != refused.Code {
			refusal += " (" + refused.Reason + ")"
		}
		return observeFailure("HQ refused the observation: " + refusal + ".")
	}
	return failure(err.Error())
}
