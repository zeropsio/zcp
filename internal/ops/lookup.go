package ops

import (
	"context"
	"fmt"

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

// FetchServiceUserEnvs returns a service's user-set env layer — the slim
// /env USER entries (an empty Type is kept too — D4: never silently drop a
// possibly user-set var; the wire always sends a type) MINUS the keys the
// active app-version's yaml-baked USER mirror also carries. Since 2026-08
// the yaml-baked run.envVariables layer is ALSO mirrored read-only on the
// slim /env as USER (same Type as a user-set var), so Type alone no longer
// tells them apart (spec docs/spec-zerops-env-lifecycle.md §1/§6) — the
// user-set layer is derived by subtraction: exactly what
// `zerops_env set serviceHostname=X KEY=val` / GUI / import envSecrets
// write. A live runtime whose yaml-baked read FAILS returns the error —
// NEVER an empty slice with nil error, which would let the yaml-baked
// mirror's keys leak into export/launch's envSecrets (GAP0-1 regression
// class). Managed deps and never-deployed runtimes have no yaml layer
// (AppVersionEnvVars' lifecycle gate returns nil, nil) — no subtraction,
// every USER/empty-typed slim entry passes through untouched. Never logs
// values.
func FetchServiceUserEnvs(ctx context.Context, client platform.Client, serviceID string) ([]platform.ServiceEnvVar, error) {
	all, err := FetchServiceEnv(ctx, client, serviceID)
	if err != nil {
		return nil, fmt.Errorf("fetch service env %s: %w", serviceID, err)
	}

	svc, err := client.GetService(ctx, serviceID)
	if err != nil {
		return nil, fmt.Errorf("get service %s: %w", serviceID, err)
	}

	yamlBaked, err := AppVersionEnvVars(ctx, client, *svc)
	if err != nil {
		return nil, fmt.Errorf("yaml-baked env vars for %s: %w", serviceID, err)
	}
	yamlKeys := make(map[string]struct{}, len(yamlBaked))
	for _, v := range yamlBaked {
		yamlKeys[v.Key] = struct{}{}
	}

	out := make([]platform.ServiceEnvVar, 0, len(all))
	for _, e := range all {
		if e.Type != platform.ServiceEnvUser && e.Type != "" {
			continue
		}
		if _, isYamlBaked := yamlKeys[e.Key]; isYamlBaked {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
