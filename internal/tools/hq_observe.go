package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
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
func RegisterObserve(srv *mcp.Server, httpClient hq.Doer, projectID string) {
	registerObserve(srv, httpClient, hq.EnrollmentPath(), projectID)
}

const (
	observeEnvironments = "environments"
	observeStatus       = "status"
	observeLogs         = "logs"
)

var observationID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func registerObserve(srv *mcp.Server, httpClient hq.Doer, path, projectID string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_observe",
		Description: "Observe this project's stage and production through HQ. action=environments lists permitted environment project IDs; status reads service IDs, status and active versions; logs reads up to 100 entries for one service. Read only: no sibling Zerops grants or secrets. A failure ends the call; call again manually after fixing it.",
		Annotations: &mcp.ToolAnnotations{Title: "Observe stage and production", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ObserveInput) (*mcp.CallToolResult, any, error) {
		failure := func(message string) *mcp.CallToolResult {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message + "; fix it and call zerops_observe again."}}}
		}
		if in.Action == "" {
			in.Action = observeEnvironments
		}
		if in.Action != observeEnvironments && in.Action != observeStatus && in.Action != observeLogs {
			return failure("Unknown observation action"), nil, nil
		}
		if in.Action != observeEnvironments && !observationID.MatchString(in.ProjectID) {
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
		client, err := hq.Open(httpClient, path)
		if err != nil {
			return failure(fmt.Sprintf("HQ enrollment could not be opened: %v", err)), nil, nil
		}
		if client.ProjectID() != projectID {
			return failure("HQ enrollment belongs to another project"), nil, nil
		}
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		var body []byte
		switch in.Action {
		case observeEnvironments:
			out, readErr := client.Environments(ctx)
			if readErr != nil {
				return failure(readErr.Error()), nil, nil
			}
			body, err = json.Marshal(out)
		case observeStatus:
			out, readErr := client.EnvironmentStatus(ctx, in.ProjectID)
			if readErr != nil {
				return failure(readErr.Error()), nil, nil
			}
			body, err = json.Marshal(out)
		case observeLogs:
			out, readErr := client.EnvironmentLogs(ctx, in.ProjectID, in.ServiceID, in.Limit)
			if readErr != nil {
				return failure(readErr.Error()), nil, nil
			}
			body, err = json.Marshal(out)
		}
		if err != nil {
			return failure(fmt.Sprintf("Observation could not be rendered: %v", err)), nil, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
	})
}
