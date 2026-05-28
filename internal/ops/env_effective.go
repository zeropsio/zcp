package ops

import (
	"context"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// EnvLayer labels which platform layer an effective env var came from.
// Order reflects container precedence for the BARE key (lowest first):
// project < service userData/secret < yaml-baked run.envVariables (system
// vars sit above all but ZCP never sets them). Spec:
// docs/spec-zerops-env-lifecycle.md §2.
type EnvLayer string

const (
	EnvLayerProject   EnvLayer = "project"     // project envVariables (GetProjectEnv)
	EnvLayerService   EnvLayer = "service"     // slim service /env: user-set userData + intrinsic
	EnvLayerYamlBaked EnvLayer = "zerops.yaml" // app-version run.envVariables (GUI "from master")
)

// EffectiveEnvVar is one key as seen on a layer, with its source.
type EffectiveEnvVar struct {
	Key       string
	Value     string
	Layer     EnvLayer
	Sensitive bool
}

// EffectiveEnv is the layered, API-reconstructed view of a service's env.
// It is NOT the resolved container env (cross-service refs stay as
// templates in the yaml-baked layer); it is the assembly of the three
// API-readable layers, source-labeled, which the slim /env alone cannot
// provide. Spec: docs/spec-zerops-env-lifecycle.md §6.
type EffectiveEnv struct {
	Hostname  string
	Project   []EffectiveEnvVar // inherited by every service (bare + PROJECT_)
	Service   []EffectiveEnvVar // this service's own user-set + intrinsic
	YamlBaked []EffectiveEnvVar // run.envVariables of the active app version (live runtime only)
}

// Keys returns the de-duplicated set of every env key visible to the
// service across all three layers — the universe a cross-service
// ${host_var} reference can legitimately resolve against.
func (e *EffectiveEnv) Keys() []string {
	seen := make(map[string]struct{})
	keys := make([]string, 0, len(e.Project)+len(e.Service)+len(e.YamlBaked))
	add := func(vars []EffectiveEnvVar) {
		for _, v := range vars {
			if _, ok := seen[v.Key]; ok {
				continue
			}
			seen[v.Key] = struct{}{}
			keys = append(keys, v.Key)
		}
	}
	add(e.Service)
	add(e.YamlBaked)
	add(e.Project)
	return keys
}

// AppVersionEnvVars returns a service's yaml-baked run.envVariables (plus
// intrinsic vars + ZEROPS_YAML) from the app-version userDataList — the
// only API surface that exposes them (the slim /env omits them).
//
// LIFECYCLE-AWARE (spec §1): returns nil for
//   - managed deps (postgres, valkey…): not built from yaml, no app
//     version; their connection vars live in the slim /env already.
//   - never-deployed runtime services (bootstrap / startWithoutCode): no
//     active app version yet.
//
// Only a LIVE runtime service (deployed ≥1×, ActiveAppVersion.ID set)
// returns yaml-baked vars. Callers branch on nil to fall back (local
// zerops.yaml for a candidate deploy, or WARN "not yet deployed").
func AppVersionEnvVars(ctx context.Context, client platform.Client, svc platform.ServiceStack) ([]platform.ServiceEnvVar, error) {
	if topology.IsManagedService(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
		return nil, nil
	}
	if svc.ActiveAppVersion == nil || svc.ActiveAppVersion.ID == "" {
		return nil, nil
	}
	return client.GetAppVersionUserData(ctx, svc.ActiveAppVersion.ID)
}

// IsRuntimeNeverDeployed reports a runtime service that has no active app
// version yet (bootstrap / startWithoutCode). Its yaml-baked
// run.envVariables are NOT on the platform, so a cross-service ref to it
// cannot be confirmed — callers WARN rather than FAIL. Managed deps are
// excluded (they're never "deployed" but their vars ARE in the slim /env).
func IsRuntimeNeverDeployed(svc platform.ServiceStack) bool {
	if topology.IsManagedService(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
		return false
	}
	return svc.ActiveAppVersion == nil || svc.ActiveAppVersion.ID == ""
}

// EffectiveServiceEnv assembles the three API-readable env layers for a
// service (project + slim service + yaml-baked-when-live). Lifecycle
// states are handled by AppVersionEnvVars (managed / never-deployed yield
// an empty yaml-baked layer). projectEnvs may be passed pre-fetched (it
// is identical for every service in a project) to avoid N project reads;
// pass nil to fetch here.
func EffectiveServiceEnv(ctx context.Context, client platform.Client, projectID string, svc platform.ServiceStack, projectEnvs []platform.ProjectEnvVar) (*EffectiveEnv, error) {
	eff := &EffectiveEnv{Hostname: svc.Name}

	if projectEnvs == nil {
		var err error
		projectEnvs, err = client.GetProjectEnv(ctx, projectID)
		if err != nil {
			return nil, err
		}
	}
	for _, e := range projectEnvs {
		eff.Project = append(eff.Project, EffectiveEnvVar{Key: e.Key, Value: e.Content, Layer: EnvLayerProject, Sensitive: e.Sensitive})
	}

	svcEnvs, err := FetchServiceEnv(ctx, client, svc.ID)
	if err != nil {
		return nil, err
	}
	for _, e := range svcEnvs {
		eff.Service = append(eff.Service, EffectiveEnvVar{Key: e.Key, Value: e.Content, Layer: EnvLayerService, Sensitive: e.Sensitive})
	}

	yamlBaked, err := AppVersionEnvVars(ctx, client, svc)
	if err != nil {
		return nil, err
	}
	for _, e := range yamlBaked {
		eff.YamlBaked = append(eff.YamlBaked, EffectiveEnvVar{Key: e.Key, Value: e.Content, Layer: EnvLayerYamlBaked, Sensitive: e.Sensitive})
	}

	return eff, nil
}
