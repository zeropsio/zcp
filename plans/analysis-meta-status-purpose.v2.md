# ServiceMeta: ZCP's Bootstrap Decision Store

**Date**: 2026-03-20
**Source**: Deep review of meta Status field usage and meta's role

---

## What Meta Is

ServiceMeta is ZCP's **persistent record of bootstrap decisions** — information the Zerops API doesn't track. It answers: "how was this service set up by ZCP?"

The API is the source of truth for operational state (is it running? what resources? what env vars?). Meta is the source of truth for bootstrap decisions (what mode? what pairing? what dependencies? what deploy strategy?).

These two are orthogonal. Meta is NOT a second source of truth — it stores what the API cannot.

## Schema (after Status removal)

```go
// ServiceMeta records bootstrap decisions for a service.
// ZCP's persistent knowledge — API doesn't track mode, pairing, or strategy.
// API is source of truth for operational state (running, resources, envs).
type ServiceMeta struct {
    Hostname         string            `json:"hostname"`                  // Identity key
    Type             string            `json:"type"`                      // Cached for deploy-time knowledge injection
    Mode             string            `json:"mode,omitempty"`            // dev/standard/simple (ZCP-only)
    StageHostname    string            `json:"stageHostname,omitempty"`   // Dev-stage pairing (ZCP-only)
    Dependencies     []string          `json:"dependencies,omitempty"`    // Co-created services
    BootstrapSession string            `json:"bootstrapSession"`          // Audit: which session
    BootstrappedAt   string            `json:"bootstrappedAt"`            // Audit: when completed
    Decisions        map[string]string `json:"decisions,omitempty"`       // Deploy strategy + future decisions
}
```

### Removed: Status field

**Why**: Written 3 times during bootstrap (planned, provisioned, bootstrapped) but never read for any decision. Intermediate values were overwritten before they could be useful. `MetaStatusDeployed` was defined but never written. The Decisions map already provides the decision-driving mechanism.

### Removed: Intermediate meta writes

**Why**: `writeServiceMetas()` was called at plan completion (planned) and provision completion (provisioned), but `writeBootstrapOutputs()` unconditionally overwrites all metas with final values at bootstrap end. No code reads intermediate metas for crash recovery. Session state tracks step progress.

**New policy**: Meta is written ONCE at bootstrap completion. Post-bootstrap updates allowed only for Decisions (e.g., strategy changes).

## Field Justification

| Field | Read by | Purpose | Why not API? |
|-------|---------|---------|--------------|
| Hostname | Router, deploy, delete, context | Identity/lookup key | API has it, but meta needs it as primary key |
| Type | deploy.go:163 (RuntimeType), deploy.go:184 (DependencyTypes) | Knowledge injection in deploy guidance | API has it, but deploy is stateless per-call — meta is the offline cache |
| Mode | deploy.go:154 (role assignment), service_context.go:36 (classification) | Dev/standard/simple routing | API has no mode concept |
| StageHostname | deploy.go:174 (stage target), service_context.go:27 (display) | Dev-stage pairing | API has no pairing concept |
| Dependencies | deploy.go:183 (dependency type collection) | Knowledge injection (what DBs/caches exist) | API has no dependency graph |
| Decisions | router.go:197 (routing), deploy_guidance.go:26 (guidance), strategy tool | Primary decision driver | API has no strategy concept |
| BootstrapSession | (audit only) | Traceability | Not in API |
| BootstrappedAt | (audit only) | Traceability | Not in API |

## What Meta is NOT

- **NOT operational state** — don't add isRunning, lastDeployTime, containerCount (API provides these)
- **NOT session state** — don't write intermediate progress (session files track step completion)
- **NOT a general cache** — Type is the one exception, explicitly cached for offline deploy-time use

## Extending Meta

New ZCP-specific decisions go in the `Decisions` map with new constant keys:

```go
const (
    DecisionDeployStrategy = "deployStrategy"  // existing
    DecisionSomeNewThing   = "someNewThing"    // future example
)
```

Only add new top-level fields if the information is structurally different from a key-value decision (e.g., a list like Dependencies).

## Implementation Changes

1. Remove `MetaStatus*` constants from `service_meta.go:20-26`
2. Remove `Status` field from `ServiceMeta` struct
3. Remove `normalizeServiceMeta()` function
4. Remove Status rendering from `service_context.go:28-30`
5. Remove `writeServiceMetas()` function from `bootstrap_outputs.go:80-117`
6. Remove intermediate write calls from `engine.go:164` and `engine.go:240`
7. Update `writeBootstrapOutputs()` to not set Status field (it won't exist)
8. Update tests: remove Status assertions, remove `TestWriteServiceMetas_PlannedStatus`, `TestWriteServiceMetas_ProvisionedStatus`, `TestMetaStatusConstants`, backward-compat tests
9. Update ServiceMeta comment to document purpose clearly
