package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/topology"
)

const invocationMetaKey = "io.zerops.mate/invocation"
const invocationTaskScope = "task"

func invocationCapabilities(managed bool) *mcp.ServerCapabilities {
	if !managed {
		return nil
	}
	return &mcp.ServerCapabilities{Logging: &mcp.LoggingCapabilities{}, Experimental: map[string]any{invocationMetaKey: 1}}
}

// This boundary precedes all tool side effects. Ordinary MCP callers cannot
// opt into engine authority by adding model-supplied metadata. The managed
// process is a private child of the trusted bridge, not an authenticated socket.
func invocationIngress(managed bool) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != methodCallTool {
				return next(ctx, method, req)
			}
			value, present := req.GetParams().GetMeta()[invocationMetaKey]
			invocation := topology.Invocation{Scope: topology.InvocationScope{Kind: "manual"}}
			valid := !present
			if managed {
				var err error
				invocation, err = decodeInvocation(value)
				valid = present && err == nil
			}
			if !valid {
				// Fixed error text never reflects credentials, invocation IDs or model data.
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: `{"code":"INVOCATION_REQUIRED","error":"The original managed invocation is missing or invalid; run this call through its owning Mate bridge."}`}}}, nil
			}
			callCtx, err := hq.WithInvocation(ctx, invocation)
			if err != nil {
				return nil, fmt.Errorf("capture original invocation: %w", err)
			}
			return next(callCtx, method, req)
		}
	}
}

func decodeInvocation(value any) (topology.Invocation, error) {
	invalid := errors.New("invalid managed invocation")
	raw, err := json.Marshal(value)
	if err != nil {
		return topology.Invocation{}, invalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 3 || fields["v"] == nil || fields["id"] == nil || fields["scope"] == nil {
		return topology.Invocation{}, invalid
	}
	var envelope struct {
		Version int             `json:"v"`
		ID      string          `json:"id"`
		Scope   json.RawMessage `json:"scope"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || envelope.Version != 1 || envelope.ID == "" {
		return topology.Invocation{}, invalid
	}
	var scope map[string]json.RawMessage
	if json.Unmarshal(envelope.Scope, &scope) != nil {
		return topology.Invocation{}, invalid
	}
	var kind string
	if json.Unmarshal(scope["kind"], &kind) != nil {
		return topology.Invocation{}, invalid
	}
	expected := 1
	if kind == invocationTaskScope {
		expected = 3
	}
	if len(scope) != expected || scope["kind"] == nil || kind == invocationTaskScope && (scope["taskId"] == nil || scope["assignment"] == nil) {
		return topology.Invocation{}, invalid
	}
	invocation := topology.Invocation{ID: envelope.ID}
	decoder = json.NewDecoder(bytes.NewReader(envelope.Scope))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&invocation.Scope) != nil || invocation.Validate() != nil {
		return topology.Invocation{}, invalid
	}
	return invocation, nil
}
