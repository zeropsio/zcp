package ops

import (
	"context"

	"github.com/zeropsio/zcp/internal/platform"
)

// ListProjectServices is the canonical entry point for upper layers
// (tools, eval) that need every service in a project. It exists so the
// dozens of tools/eval sites previously calling client.ListServices
// directly converge on one ops.* surface — caching, auth fingerprint,
// retries, or instrumentation can land here without touching every
// caller. Behavior today is a passthrough.
func ListProjectServices(ctx context.Context, client platform.Client, projectID string) ([]platform.ServiceStack, error) {
	return client.ListServices(ctx, projectID)
}

// LookupService combines ListProjectServices + FindService into one
// call. Returns the canonical ErrServiceNotFound + "Available: ..."
// suggestion when the hostname is absent in the project.
func LookupService(ctx context.Context, client platform.Client, projectID, hostname string) (*platform.ServiceStack, error) {
	services, err := ListProjectServices(ctx, client, projectID)
	if err != nil {
		return nil, err
	}
	return FindService(services, hostname)
}

// FetchServiceEnv is the canonical entry point for callers that need a
// service's full env-var list. Like ListProjectServices, it exists so
// upper layers don't reach into platform.Client directly. Today this is
// a thin passthrough, but caching / batching / retries can land here
// without touching every site.
func FetchServiceEnv(ctx context.Context, client platform.Client, serviceID string) ([]platform.EnvVar, error) {
	return client.GetServiceEnv(ctx, serviceID)
}
