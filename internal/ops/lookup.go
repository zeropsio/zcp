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
func FetchServiceEnv(ctx context.Context, client platform.Client, serviceID string) ([]platform.ServiceEnvVar, error) {
	return client.GetServiceEnv(ctx, serviceID)
}

// FetchServiceSecretEnvs returns a service's USER-set env layer — the
// Type=SECRET entries of the slim service /env, which is exactly what
// `zerops_env set serviceHostname=X KEY=val` writes (verified live:
// CreateServiceEnvVar → Type=SECRET). It EXCLUDES platform intrinsics
// (READ_ONLY: hostname, projectId, zeropsSubdomain, …), editable platform
// defaults (EDITABLE: PATH), and INTERNAL entries. These SECRET entries
// are the only service-level env the platform stores as user data and
// that buildFromGit does NOT rebuild (they are not in zerops.yaml), so
// export/launch must carry them or silently lose the key on re-import
// (GAP0-1).
func FetchServiceSecretEnvs(ctx context.Context, client platform.Client, serviceID string) ([]platform.ServiceEnvVar, error) {
	all, err := FetchServiceEnv(ctx, client, serviceID)
	if err != nil {
		return nil, err
	}
	out := make([]platform.ServiceEnvVar, 0, len(all))
	for _, e := range all {
		if e.Type == platform.ServiceEnvSecret {
			out = append(out, e)
		}
	}
	return out, nil
}
