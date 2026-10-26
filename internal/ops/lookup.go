package ops

import (
	"context"
	"fmt"
	"slices"

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

// ListProjectServicesDirect is ListProjectServices read lag-free: the
// project's service stacks by direct GET, never the Elasticsearch search that
// trails writes — for a caller judging state right after an import or a
// mutation, as ops.Discover does.
func ListProjectServicesDirect(ctx context.Context, client platform.Client, projectID string) ([]platform.ServiceStack, error) {
	return client.ListServicesDirect(ctx, projectID)
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
// upper layers don't reach into platform.Client directly. It is also the one
// place that decides a row is masked: a value the platform answers with
// platform.MaskedEnvValue is marked Masked and its Content blanked, so no
// consumer can use the placeholder as the secret. It marks a copy, never the
// rows the client returned, which a client may share between callers.
func FetchServiceEnv(ctx context.Context, client platform.Client, serviceID string) ([]platform.ServiceEnvVar, error) {
	answered, err := client.GetServiceEnv(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	envs := slices.Clone(answered)
	for i := range envs {
		if envs[i].Content == platform.MaskedEnvValue {
			envs[i].Masked, envs[i].Content = true, ""
		}
	}
	return envs, nil
}

// EnvScalars flattens env rows into a key→value map for consumers that build a
// connection from them, and lists the keys whose value the platform withheld —
// those never enter the map.
func EnvScalars(envs []platform.ServiceEnvVar) (scalars map[string]string, masked []string) {
	scalars = make(map[string]string, len(envs))
	for _, e := range envs {
		if e.Masked {
			masked = append(masked, e.Key)
			continue
		}
		scalars[e.Key] = e.Content
	}
	return scalars, masked
}

// FetchServiceUserEnvs returns a service's user-set env layer — the slim
// /env USER entries (an empty Type is kept too — D4: never silently drop a
// possibly user-set var; the wire always sends a type) MINUS the keys the
// active app-version's yaml-baked USER mirror also carries. Since 2026-08
// the yaml-baked run.envVariables layer is ALSO mirrored read-only on the
// slim /env as USER (same Type as a user-set var), so Type alone no longer
// tells them apart (spec docs/spec-zerops-env-lifecycle.md §1/§6) — the
// user-set layer is derived by subtraction: exactly what
// `zerops_env set serviceHostname=X KEY=val` / GUI / an import's service vault
// write. A live runtime whose yaml-baked read FAILS returns the error —
// NEVER an empty slice with nil error, which would let the yaml-baked
// mirror's keys leak into export/launch's service vault (GAP0-1 regression
// class). Managed deps and never-deployed runtimes have no yaml layer
// (AppVersionEnvVars' lifecycle gate returns nil, nil) — no subtraction,
// every USER/empty-typed slim entry passes through untouched. Never logs
// values.
func FetchServiceUserEnvs(ctx context.Context, client platform.Client, serviceID string) ([]platform.ServiceEnvVar, error) {
	layers, err := fetchServiceEnvLayers(ctx, client, serviceID)
	if err != nil {
		return nil, err
	}
	return layers.user(), nil
}

// serviceEnvLayers is a service's slim env read (masked rows marked, as
// FetchServiceEnv leaves them) beside the keys its active app-version's
// zerops.yaml owns.
type serviceEnvLayers struct {
	all       []platform.ServiceEnvVar
	yamlOwned map[string]struct{}
}

// fetchServiceEnvLayers reads the two surfaces FetchServiceUserEnvs
// subtracts. A failed yaml-baked read is an error, never an empty layer.
func fetchServiceEnvLayers(ctx context.Context, client platform.Client, serviceID string) (serviceEnvLayers, error) {
	all, err := FetchServiceEnv(ctx, client, serviceID)
	if err != nil {
		return serviceEnvLayers{}, fmt.Errorf("fetch service env %s: %w", serviceID, err)
	}

	svc, err := client.GetService(ctx, serviceID)
	if err != nil {
		return serviceEnvLayers{}, fmt.Errorf("get service %s: %w", serviceID, err)
	}

	yamlBaked, err := AppVersionEnvVars(ctx, client, *svc)
	if err != nil {
		return serviceEnvLayers{}, fmt.Errorf("yaml-baked env vars for %s: %w", serviceID, err)
	}
	yamlKeys := make(map[string]struct{}, len(yamlBaked))
	for _, v := range yamlBaked {
		yamlKeys[v.Key] = struct{}{}
	}
	return serviceEnvLayers{all: all, yamlOwned: yamlKeys}, nil
}

// user is the user-set layer: USER (or untyped) rows whose key the
// zerops.yaml does not own.
func (l serviceEnvLayers) user() []platform.ServiceEnvVar {
	out := make([]platform.ServiceEnvVar, 0, len(l.all))
	for _, e := range l.all {
		if e.Type != platform.ServiceEnvUser && e.Type != "" {
			continue
		}
		if _, isYamlBaked := l.yamlOwned[e.Key]; isYamlBaked {
			continue
		}
		out = append(out, e)
	}
	return out
}
