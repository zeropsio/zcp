package hq

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/topology"
)

type invocationKey struct{}

// WithInvocation copies provenance from the owned MCP ingress. It is not a
// model input, runtime setting, current-task lookup or ambient process state.
func WithInvocation(ctx context.Context, invocation topology.Invocation) (context.Context, error) {
	if err := invocation.Validate(); err != nil {
		return nil, fmt.Errorf("call origin: %w", err)
	}
	return context.WithValue(ctx, invocationKey{}, invocation), nil
}

func InvocationFromContext(ctx context.Context) (topology.Invocation, error) {
	if value := ctx.Value(invocationKey{}); value != nil {
		invocation, ok := value.(topology.Invocation)
		if !ok {
			return topology.Invocation{}, fmt.Errorf("call origin: invalid context")
		}
		if err := invocation.Validate(); err != nil {
			return topology.Invocation{}, fmt.Errorf("call origin: %w", err)
		}
		return invocation, nil
	}
	return topology.Invocation{Scope: topology.InvocationScope{Kind: "manual"}}, nil
}

type taskOrigin struct {
	TaskID     string `json:"taskId"`
	Assignment string `json:"assignment"`
}

func originatingTask(ctx context.Context) (taskOrigin, bool, error) {
	invocation, err := InvocationFromContext(ctx)
	if err != nil {
		return taskOrigin{}, false, err
	}
	if invocation.Scope.Kind == "task" {
		return taskOrigin{invocation.Scope.TaskID, invocation.Scope.Assignment}, true, nil
	}
	return taskOrigin{}, false, nil
}
