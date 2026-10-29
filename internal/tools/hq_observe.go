package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// ObserveInput uses HQ's project and service IDs, learned from environments
// and status. No Zerops credential or application ID comes from the model.
type ObserveInput struct {
	Action    string `json:"action,omitempty"    jsonschema:"environments (default), status, or logs"`
	ProjectID string `json:"projectId,omitempty" jsonschema:"Environment project ID from environments; required for status and logs"`
	ServiceID string `json:"serviceId,omitempty" jsonschema:"Service ID from status; required for logs"`
	Limit     int    `json:"limit,omitempty"     jsonschema:"Log entries: 1 to 100; defaults to 100"`
}

// RegisterObserve is Mate-gated by the server. Observation needs no SSH:
// HQ reads Zerops with each environment's deploy key and exposes only facts.
func RegisterObserve(srv *mcp.Server, httpClient hq.Doer, projectID, stateDir string, rt runtime.Info) {
	const (
		observeEnvironments = "environments"
		observeStatus       = "status"
		observeLogs         = "logs"
	)

	var observationID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_observe",
		Description: "Observe stage and production through HQ. environments lists project IDs and known change status; changesAsLastRead marks unrefreshed status. Merge advice requires current status; absent changes or changesAsLastRead mean unknown. status reads services and versions; logs reads up to 100 entries. Read only, without secrets. Failures say fix input, retry later, or accept HQ's refusal.",
		Annotations: &mcp.ToolAnnotations{Title: "Observe stage and production", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ObserveInput) (*mcp.CallToolResult, any, error) {
		failure := func(message string) *mcp.CallToolResult {
			return observeFailure(message + "; fix it and call zerops_observe again.")
		}
		if in.Action == "" {
			in.Action = observeEnvironments
		}
		if in.Action != observeEnvironments && in.Action != observeStatus && in.Action != observeLogs {
			return failure("Unknown observation action"), nil, nil
		}
		if (in.Action == observeStatus || in.Action == observeLogs) && !observationID.MatchString(in.ProjectID) {
			return failure("A valid environment projectId from environments is required"), nil, nil
		}
		if in.Action == observeLogs && !observationID.MatchString(in.ServiceID) {
			return failure("A valid serviceId from status is required for logs"), nil, nil
		}
		if in.Limit == 0 {
			in.Limit = 100
		}
		if in.Limit < 1 || in.Limit > 100 {
			return failure("Log limit must be 1 to 100"), nil, nil
		}
		client, err := hq.Open(httpClient, liveHQIdentity())
		if err != nil {
			return failure(fmt.Sprintf("HQ could not be addressed: %v", err)), nil, nil
		}
		if client.ProjectID() != projectID {
			return failure("This Mate's key belongs to another project"), nil, nil
		}
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		var body []byte
		switch in.Action {
		case observeEnvironments:
			out, readErr := client.Environments(ctx)
			if readErr != nil {
				return readFailure(readErr, failure), nil, nil
			}
			changes, asLastRead, _ := statusHQChanges(ctx, httpClient, rt, stateDir, hqAddress(rt), time.Now(), false)
			body, err = json.Marshal(struct {
				hq.ObservedEnvironments
				Changes           []workflow.HQRepoChanges `json:"changes"`
				ChangesAsLastRead bool                     `json:"changesAsLastRead"`
			}{out, changes, asLastRead})
		case observeStatus:
			out, readErr := client.EnvironmentStatus(ctx, in.ProjectID)
			if readErr != nil {
				return readFailure(readErr, failure), nil, nil
			}
			body, err = json.Marshal(out)
		case observeLogs:
			out, readErr := client.EnvironmentLogs(ctx, in.ProjectID, in.ServiceID, in.Limit)
			if readErr != nil {
				return readFailure(readErr, failure), nil, nil
			}
			body, err = json.Marshal(out)
		}
		if err != nil {
			return failure(fmt.Sprintf("Observation could not be rendered: %v", err)), nil, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
	})
}

func observeFailure(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// readFailure says what the agent can do about HQ's answer: an unavailable
// HQ is waited out, a refusal is HQ's answer to the same call; anything else
// stays the input failure.
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
		refusal := fmt.Sprintf("%d %s", refused.Status, refused.Code)
		if refused.Reason != "" && refused.Reason != refused.Code {
			refusal += " (" + refused.Reason + ")"
		}
		return observeFailure("HQ refused the observation: " + refusal + "; the same call gets the same answer.")
	}
	return failure(err.Error())
}
