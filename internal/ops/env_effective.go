package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// LayerAvailability is the read-state of one env layer. The zero value is
// LayerUnknown by design, so an unset state never silently reads as "present
// data present" — the confident-wrong default (a failed read masquerading as
// "the layer is empty") this whole change removes. Spec §RC2.
type LayerAvailability int

const (
	LayerUnknown     LayerAvailability = iota // not yet determined (zero value — never trust as data)
	LayerPresent                              // read OK (may be legitimately empty)
	LayerAbsent                               // legitimately no layer (managed dep / never-deployed runtime has no yaml-baked)
	LayerUnavailable                          // a read was attempted and FAILED (transient/API/auth) — NOT empty
)

// LayerState carries a layer's availability + the cause when Unavailable.
type LayerState struct {
	Availability LayerAvailability
	Cause        error
}

// Unavailable reports whether a read was attempted and failed.
func (s LayerState) Unavailable() bool { return s.Availability == LayerUnavailable }

// ProjectEnvLayer is the project env layer passed into EffectiveServiceEnv,
// carrying the fetched vars PLUS their read-state — so a caller passes "project
// layer unavailable" explicitly instead of smuggling a failed read as an empty
// slice (which reads as "no project vars" = a confident-wrong default).
type ProjectEnvLayer struct {
	Vars  []platform.ProjectEnvVar
	State LayerState
}

// HigherEnvLayers is a service's env layers ABOVE project (slim service + yaml-
// baked), each with its read-state. Bundled into a struct (not a multi-return)
// so callers cannot transpose the layers or their states.
type HigherEnvLayers struct {
	Service        []EffectiveEnvVar
	YamlBaked      []EffectiveEnvVar
	ServiceState   LayerState
	YamlBakedState LayerState
}

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
	Key   string
	Value string
	Layer EnvLayer
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

	// Per-layer read-state. A consumer MUST branch on these instead of
	// treating an empty slice as authoritative "absent" — a failed read is
	// LayerUnavailable, not empty. Spec §RC2.
	ProjectState   LayerState
	ServiceState   LayerState
	YamlBakedState LayerState
}

// ReadComplete reports whether every consulted layer was read successfully
// (Present or Absent) — i.e. no layer is Unavailable. It does NOT assert that a
// never-deployed runtime's future yaml-baked keys are confirmable (those are a
// complete read of "no layer yet" = Absent); it asserts the read itself held.
func (e *EffectiveEnv) ReadComplete() bool {
	return !e.ProjectState.Unavailable() &&
		!e.ServiceState.Unavailable() &&
		!e.YamlBakedState.Unavailable()
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

// AppVersionEnvVars returns a service's yaml-baked run.envVariables from the
// app-version userDataList — the unambiguous API surface for them (since
// 2026-08 they are ALSO mirrored read-only on the slim /env, but Type alone
// can't tell that mirror apart from a user-set var there — spec §1). The
// mapper (platform.GetAppVersionUserData) classifies the raw userDataList
// superset and returns ONLY genuine run.envVariables (Type USER,
// editable:false), Sensitive always false — SYSTEM intrinsic vars and the
// ZEROPS_YAML blob are filtered out at that boundary, so this returns
// run-layer vars only.
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

// ServiceHigherLayers returns a service's env layers ABOVE project — the slim
// service userData (always present) and the yaml-baked run.envVariables (live
// runtime only; AppVersionEnvVars enforces the §1 lifecycle gate, so managed
// and never-deployed services yield an empty yaml-baked layer). It is the
// extracted helper EffectiveServiceEnv composes on top of the project layer,
// and the direct source for project-set shadow detection (which compares the
// just-set project values against these higher layers, not a re-read project).
func ServiceHigherLayers(ctx context.Context, client platform.Client, svc platform.ServiceStack) (HigherEnvLayers, error) {
	if client == nil {
		return HigherEnvLayers{}, fmt.Errorf("ServiceHigherLayers: nil client")
	}
	var out HigherEnvLayers

	// Slim service layer — present on every service (managed deps included:
	// their connection vars live here). A fetch failure is Unavailable, NOT
	// "the service has no env" — the caller must not treat it as empty.
	svcEnvs, err := FetchServiceEnv(ctx, client, svc.ID)
	if err != nil {
		out.ServiceState = LayerState{Availability: LayerUnavailable, Cause: err}
	} else {
		out.ServiceState = LayerState{Availability: LayerPresent}
		for _, e := range svcEnvs {
			out.Service = append(out.Service, EffectiveEnvVar{Key: e.Key, Value: e.Content, Layer: EnvLayerService})
		}
	}

	// Yaml-baked layer — classify Absent vs fetch from the svc LIFECYCLE, not
	// from returned-slice nilness (a live runtime with zero run.envVariables is
	// Present-but-empty, NOT Absent). Managed deps + never-deployed runtimes
	// legitimately have no app version → Absent, no fetch attempted.
	switch {
	case topology.IsManagedService(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName), IsRuntimeNeverDeployed(svc):
		out.YamlBakedState = LayerState{Availability: LayerAbsent}
	default: // live runtime with an active app version
		yb, ybErr := AppVersionEnvVars(ctx, client, svc)
		if ybErr != nil {
			out.YamlBakedState = LayerState{Availability: LayerUnavailable, Cause: ybErr}
		} else {
			out.YamlBakedState = LayerState{Availability: LayerPresent}
			for _, e := range yb {
				out.YamlBaked = append(out.YamlBaked, EffectiveEnvVar{Key: e.Key, Value: e.Content, Layer: EnvLayerYamlBaked})
			}
		}
	}
	return out, nil
}

// EffectiveServiceEnv assembles the three API-readable env layers for a
// service (project + slim service + yaml-baked-when-live), each annotated with
// its read-state. The caller fetches the project layer once (it is identical
// for every service in a project — avoids N reads) and passes it as a typed
// ProjectEnvLayer carrying its own availability. err is reserved for precondition
// failures (nil client); a layer FETCH failure surfaces as LayerUnavailable on
// the returned *EffectiveEnv, never as (nil, err) — so callers branch on state
// instead of collapsing a transient into "the layer is empty". Spec §RC2/§RC3.
func EffectiveServiceEnv(ctx context.Context, client platform.Client, svc platform.ServiceStack, project ProjectEnvLayer) (*EffectiveEnv, error) {
	eff := &EffectiveEnv{Hostname: svc.Name, ProjectState: project.State}
	for _, e := range project.Vars {
		eff.Project = append(eff.Project, EffectiveEnvVar{Key: e.Key, Value: e.Content, Layer: EnvLayerProject})
	}

	higher, err := ServiceHigherLayers(ctx, client, svc)
	if err != nil {
		return nil, err
	}
	eff.Service = higher.Service
	eff.ServiceState = higher.ServiceState
	eff.YamlBaked = higher.YamlBaked
	eff.YamlBakedState = higher.YamlBakedState

	return eff, nil
}
