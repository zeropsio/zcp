# Discover `adoptionState` enum — replace mixed-semantics `managedByZcp`

**Status:** revised after Codex validation pass + Karel ultrathink review; ready for second Codex pass before implementation.
**Date:** 2026-05-27.
**Revision:** v2 — incorporates Codex findings (5-state, system filter, wire-leak fix) + Karel decisions (keep `adopted` name, plain drop `managedByZcp`).
**Predecessor:** `discover + validate: surface adopt-hint early` (commit `6069c88b`), `discover: replace adoptRecovery struct with directive Warnings string` (commit `f0842ec2`, released as `v9.101.3`).
**Codex review (v1):** `plans/discover-adoption-state-enum-2026-05-27.codex-review.md`.

---

## Problem statement

`ServiceInfo.ManagedByZCP bool` has mixed semantics: the value `false` means **four** different things depending on service kind and meta state.

| Service kind | IsInfrastructure | Type | Meta state | ManagedByZcp | What `false` actually means |
|---|---|---|---|---|---|
| Runtime adopted/bootstrapped | false | `nodejs@22` | complete | **true** | — |
| Runtime mid-bootstrap (resumable) | false | `nodejs@22` | incomplete + BootstrapSession | **false** | ← route=resume target (NOT route=adopt) |
| Runtime unadopted | false | `nodejs@22` | no meta / orphan | **false** | ← route=adopt candidate |
| ZCP self | false | `zcp@1` | n/a | **false** | never an adopt target (control-plane) |
| Managed dep | true | `postgresql@16` | n/a | **false** | API-authoritative, no ServiceMeta concept |

Agent must mentally aggregate 4 fields (`IsInfrastructure`, `Type == "zcp@1"`, meta presence, meta.IsComplete + meta.BootstrapSession) to derive one of 5 real states. Symptoms documented in `.zcp/manual/t2.txt` + `t3.txt` eval transcripts.

**Codex v1 review found a 4-state plan would mislabel resumable services as adoptable** → warning sends agent to `route=adopt` instead of `route=resume` → wrong recovery. The workflow layer already distinguishes via `workflow.adoptableServices` + `workflow.resumeOption` + `ComputeEnvelope.IdleIncomplete`. The single-truth `adoptionState` field must mirror that semantics.

## Goal

One `ServiceInfo.AdoptionState AdoptionState` field with explicit **5-state enum**. Removes mental aggregation, mirrors workflow-layer route semantics, makes warning derivation a trivial filter on a single field, and gives atom guidance a stable hook.

## Design

### Types

```go
// internal/ops/discover.go

type AdoptionState string

const (
    AdoptionAdopted    AdoptionState = "adopted"      // IsComplete ServiceMeta exists (greenfield OR brownfield)
    AdoptionResumable  AdoptionState = "resumable"    // meta exists, incomplete, BootstrapSession != "" → workflow route=resume
    AdoptionAdoptable  AdoptionState = "adoptable"    // runtime, no meta OR orphan meta with empty BootstrapSession → workflow route=adopt
    AdoptionManagedDep AdoptionState = "managed-dep"  // db/redis/storage — API-authoritative, no adoption concept
    AdoptionZCPSelf    AdoptionState = "zcp-self"     // control-plane container (zcp@1)
)

type ServiceInfo struct {
    ...
    AdoptionState AdoptionState `json:"adoptionState"`
    // ManagedByZCP REMOVED — plain drop per Karel decision.
    ...
}
```

**`adopted` name rationale (Karel decision):** kept despite Codex preference for `tracked`. Both greenfield bootstrap AND brownfield adopt-route produce a complete `ServiceMeta`; the enum value semantics is "ZCP-tracked via complete ServiceMeta, regardless of how it got there". Go-doc on the constant + atom guidance MUST spell out this definition explicitly so the term doesn't drift back to its narrower brownfield-only meaning.

### Classification logic

`enrichWithMetaStatus` rewrites to one switch — five mutually-exclusive branches, ordered for correct precedence:

```go
for i := range result.Services {
    s := &result.Services[i]
    meta := lookupMeta(idx, s.Hostname)  // pair-keyed
    switch {
    case s.Type == "zcp@1":
        s.AdoptionState = ops.AdoptionZCPSelf
    case s.IsInfrastructure:
        s.AdoptionState = ops.AdoptionManagedDep
    case meta != nil && meta.IsComplete():
        s.AdoptionState = ops.AdoptionAdopted
    case meta != nil && meta.BootstrapSession != "":
        // Incomplete meta tagged by a prior session — workflow.resumeOption
        // target. Note: meta != nil && incomplete && BootstrapSession == ""
        // is an ORPHAN meta → falls through to adoptable (matches
        // workflow.adoptableServices semantics in route.go).
        s.AdoptionState = ops.AdoptionResumable
    default:
        s.AdoptionState = ops.AdoptionAdoptable
    }
}
```

**Order rationale (per Codex Section 2):**
- `zcp@1` first — control-plane is USER category on platform, must not fall through.
- `IsInfrastructure` second — managed deps win even if stale meta exists.
- Complete meta third — pair-keyed adopted services on either half resolve.
- Incomplete + BootstrapSession fourth — resumable wins over default adoptable.
- Default — only true untracked runtimes (or orphan meta).

### Warning derivation

```go
var adoptCandidates, resumeCandidates []string
for _, s := range result.Services {
    switch s.AdoptionState {
    case ops.AdoptionAdoptable:
        adoptCandidates = append(adoptCandidates, s.Hostname)
    case ops.AdoptionResumable:
        resumeCandidates = append(resumeCandidates, s.Hostname)
    }
}
if len(adoptCandidates) > 0 {
    result.Warnings = append(result.Warnings, fmt.Sprintf(
        "Services with adoptionState=\"adoptable\" (live but not tracked by ZCP): %s. "+
        "Run `zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\"` "+
        "BEFORE any service-scoped work — those calls will reject with ADOPT_REQUIRED until adoption completes.",
        strings.Join(adoptCandidates, ", "),
    ))
}
if len(resumeCandidates) > 0 {
    result.Warnings = append(result.Warnings, fmt.Sprintf(
        "Services with adoptionState=\"resumable\" (mid-bootstrap, owned by a prior session): %s. "+
        "Run `zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"resume\"` "+
        "to continue the incomplete bootstrap — adopt route would reject these as ZCP-owned.",
        strings.Join(resumeCandidates, ", "),
    ))
}
```

Two distinct warnings ⇒ each points at the correct workflow route. Single warning conflating both states would force the agent to mentally split them again.

### System-service filter (Codex Section 1 secondary)

**Current state in code** (verified — `internal/ops/discover.go:102-138`):
- Unfiltered path (`hostname == ""`, line 128-138): skips `IsSystem()` services (line 130).
- Scoped path (`hostname != ""`, line 102-126): calls `FindService` (`helpers.go:19`) which iterates by hostname match only — **no IsSystem filter**.

This means `zerops_discover service="<CORE/BUILD/INTERNAL/PREPARE_RUNTIME/HTTP_L7_BALANCER hostname>"` returns the service through to the classifier. The 5-state switch's default branch would classify it as `adoptable` → warning suggests adopting an internal build container. Wrong.

**Fix:** in `ops.Discover` scoped path, after `FindService` succeeds, check `svc.IsSystem()` — return `ErrServiceNotFound` (matches unfiltered semantics: system services aren't user-facing). One added line; mirrors existing filter logic.

```go
if hostname != "" {
    svc, resolveErr := FindService(services, hostname)
    if resolveErr != nil {
        return nil, resolveErr
    }
    if svc.IsSystem() {
        return nil, platform.NewPlatformError(
            platform.ErrServiceNotFound,
            fmt.Sprintf("Service '%s' not found", hostname),
            "Available services: "+ListHostnames(filterUserVisible(services)),
        )
    }
    ...
}
```

Existing test coverage at `internal/ops/discover_test.go:655-705` pins the unfiltered filter; **new test for scoped path with system hostname required**.

### Wire-leak fix: refactor `env.go` → dedicated `EnvGetResponse`

**Diagnosis** (Codex Section 3 + verified):
- `ops.DiscoverResult` is returned by **6 call sites** (grep `ops.Discover(`):
  - `internal/tools/discover.go` — `zerops_discover` MCP tool. Runs `enrichWithMetaStatus`. **Agent-facing.**
  - `internal/tools/env.go:116` — `zerops_env action="get"`. **NO enrichment.** Result serialized to agent via `jsonResult`. **Agent-facing leak.**
  - `internal/tools/launch_source_read.go:68` — internal.
  - `internal/tools/launch_bundle_compose.go:52` — internal.
  - `internal/tools/workflow_export.go:97` — internal.
  - `internal/tools/workflow_export.go:314` — internal.
- Internal callers never serialize the result → no leak.
- `env.go` returns full `DiscoverResult` to agent — leaky abstraction (agent asked for env vars, gets project info + service list + notes + warnings + now would get adoption state).

**Three options considered:**

| Option | Approach | Why rejected/chosen |
|---|---|---|
| (1) `omitempty` on AdoptionState | `json:"adoptionState,omitempty"` | **Rejected.** Dual-contract: agent can't distinguish "classifier didn't run" vs "value is empty string". Every wire reader must memorize which call site enriches. |
| (2) Sidecar map `AdoptionStates map[string]AdoptionState` | Side-map keyed by hostname | **Rejected.** Per-service classification co-locates worse with other service fields; agent loses ergonomics. |
| (3) Two struct types `DiscoverResult` (internal) + `DiscoverResponse` (agent-facing) | Type-level separation | **Rejected.** Duplicates fields across two types; maintenance burden when adding new fields to ServiceInfo. |
| (4) Pass `stateDir` into `ops.Discover`, enrich inside ops | Single struct, opt-in enrichment | **Rejected.** Would require `ops/` to import `workflow/` (peer-layer violation per CLAUDE.md architecture rule). |
| (5) **Refactor `env.go` to return dedicated `EnvGetResponse`** | env get gets focused shape; DiscoverResult stays single-purpose | **Chosen.** Architectural fix — env get returning DiscoverResult was an implementation shortcut, not contract. Future-proof: any new DiscoverResult enrichment won't leak. |

**EnvGetResponse shape:**

```go
// internal/tools/env.go

type EnvGetResponse struct {
    Service *EnvGetServiceInfo `json:"service,omitempty"`  // omit for project=true
    Envs    []map[string]any   `json:"envs"`               // the asked-for vars
    Project *ops.ProjectInfo   `json:"project,omitempty"`  // when project=true
    Notes   []string           `json:"notes,omitempty"`    // env-ref hints (preserved from ops.Discover output)
}

type EnvGetServiceInfo struct {
    Hostname  string `json:"hostname"`
    ServiceID string `json:"serviceId"`
    Type      string `json:"type"`
    Status    string `json:"status"`
}
```

`env.go::env get` handler:
1. Call `ops.Discover` for env data fetch (unchanged).
2. Project the relevant fields into `EnvGetResponse` instead of returning `DiscoverResult` directly.
3. Return `jsonResult(envResp)` — adoption state not present in this shape AT ALL (structural impossibility, not omitempty trick).

**Migration safety — Karel decision pending:**

| Approach | Detail | Pro | Con |
|---|---|---|---|
| (a) Dual-emit one release | EnvGetResponse + deprecated `services[]` array carrying legacy shape | Backward-compat 1 release for external consumers | Wider scope; 2x serialize cost |
| (b) Clean break | Single EnvGetResponse shape; legacy consumers (e2e/instructions_test.go, possibly external scripts) update on this release | Cleaner long-term; less code | External consumers see field set shrink immediately |

**Plan default: (b) clean break** unless Karel says otherwise — consistent with ZCP rapid iteration approach; no production tooling depends on env get's full DiscoverResult shape per repo grep.

## Backward compat: `managedByZcp`

**Karel decision: plain drop.** No derived alias period.

Rationale: in-repo grep clean of atom references; `e2e/instructions_test.go` is the only external pin (will be updated as part of this commit). External MCP consumers reading our JSON output may break, but per Karel's call this is the accepted cost — `zerops_discover` JSON contract is treated as ZCP-internal until ZCP API stabilizes. Plan §"Engineering Priority" in `CLAUDE.local.md` puts "wide-and-long structural correction" above "preserve every wire field forever".

## Implementation phases

### P1 — types + classification + system filter

- `internal/ops/discover.go`:
  - Add `AdoptionState` type + 5 constants (`AdoptionAdopted`, `AdoptionResumable`, `AdoptionAdoptable`, `AdoptionManagedDep`, `AdoptionZCPSelf`).
  - Add `ServiceInfo.AdoptionState` field (no omitempty — every classifier path populates it).
  - **DROP** `ServiceInfo.ManagedByZCP` field.
  - Scoped `Discover` path: post-`FindService`, check `IsSystem()` → return `ErrServiceNotFound`.
- `internal/tools/discover.go::enrichWithMetaStatus`:
  - Rewrite to single switch, 5 mutually-exclusive branches in the order documented.
  - Warning derivation: two separate warnings (`adoptable` → route=adopt prose, `resumable` → route=resume prose).

### P2 — env.go wire-leak fix

- `internal/tools/env.go`:
  - Define `EnvGetResponse` + `EnvGetServiceInfo` types.
  - `env get` handler: project `DiscoverResult` → `EnvGetResponse`. Stop returning `DiscoverResult` directly.
- `internal/tools/env_test.go`:
  - Update existing env get assertions for new shape.
  - Add explicit regression test: `EnvGetResponse` JSON does NOT contain `adoptionState` key (structural impossibility check).

### P3 — tests

#### Per-state tests in `discover_adopt_hint_test.go`

- 5 enum constants → 5 minimum classification tests:
  - Adopted runtime (complete meta) → `AdoptionAdopted`
  - Resumable runtime (incomplete meta + `BootstrapSession != ""`) → `AdoptionResumable`
  - Adoptable runtime (no meta) → `AdoptionAdoptable`
  - Adoptable orphan (incomplete meta + empty `BootstrapSession`) → `AdoptionAdoptable`
  - Managed dep (`IsInfrastructure:true`) → `AdoptionManagedDep`
  - ZCP self (`Type:"zcp@1"`) → `AdoptionZCPSelf`
- Warning derivation tests:
  - Adoptable services → route=adopt warning fires, route=resume warning doesn't.
  - Resumable services → route=resume warning fires, route=adopt warning doesn't.
  - Mixed (adoptable + resumable in same project) → both warnings fire, each lists only its hostnames.
  - Fully adopted → no warning.

#### Wire-shape tests in `discover_test.go`

- `zerops_discover` JSON response includes non-empty `adoptionState` per service.
- `managedByZcp` JSON key is absent (post-drop regression pin).

#### Wire-shape tests in `env_test.go`

- `zerops_env action="get"` JSON response does NOT contain `adoptionState`, `services[]`, `unmanagedRuntimes`, `adoptRecovery` (structural separation).
- Existing env get assertions migrated to new `EnvGetResponse` shape.

#### Scoped system-service filter test in `discover_test.go`

- `zerops_discover service="<system-category-hostname>"` returns `ErrServiceNotFound` (not classifier output).

#### E2E test in `e2e/instructions_test.go`

- Migrate `managedByZcp` assertions → `adoptionState` assertions.

#### Parallel classifier parity tests

Codex Section 3 flagged 4 parallel classifiers; plan does NOT refactor them, but adds parity tests so the new discover enum stays consistent with workflow route semantics:
- `internal/workflow/route.go::adoptableServices` — hostname output must match discover's `AdoptionAdoptable` hostnames on the same fixture.
- `internal/workflow/route.go::resumeOption` — should fire iff discover surfaces any `AdoptionResumable` service.
- `internal/workflow/adopt.go::InferServicePairing` — zcp-filter parity check (prefix vs exact `zcp@1`).
- `internal/tools/launch_source_context.go::isZCPSelfService` — same fixture must classify the same service as zcp-self in both discover and launch.

Parity tests live in `internal/tools/discover_classifier_parity_test.go` (new file) — shared fixtures, multiple consumer asserts.

### P4 — atom guidance

Per Codex Section 6:

- **`internal/content/atoms/idle-adopt-entry.md`** (REQUIRED):
  - Add paragraph referencing `adoptionState` field. Example: "Per-service `adoptionState` in `zerops_discover` output classifies each service: `adopted` (ZCP-tracked, ready for develop/deploy), `adoptable` (live runtime without meta — call bootstrap route=adopt), `resumable` (mid-bootstrap, owned by prior session — call bootstrap route=resume), `managed-dep` (db/cache/storage, no adoption concept), `zcp-self` (control-plane, never adopted)."
  - Add `ops.ServiceInfo.AdoptionState` to `references-fields`.
- **`internal/content/atoms/bootstrap-adopt-discover.md`** (REQUIRED — currently has "Read every user (non-system, non-managed) service" prose that lines up exactly with the new enum):
  - Replace mental-aggregation prose with "use services where `adoptionState=\"adoptable\"`; explicitly ignore `managed-dep` and `zcp-self`".
  - Add `ops.ServiceInfo.AdoptionState` to `references-fields`.
- **`internal/content/atoms/bootstrap-resume.md`** (REQUIRED — connects resumable services to resume route):
  - Reference `adoptionState=\"resumable\"` as the structural signal pointing at this atom.
  - Add `ops.ServiceInfo.AdoptionState` to `references-fields`.
- **`internal/content/atoms/bootstrap-route-options.md`** (OPTIONAL — one short clause that `adoptServices[]` corresponds to adoptable runtimes; no need for full enum table here).
- **Goldens** regenerated via `ZCP_UPDATE_ATOM_GOLDENS=1 go test ./internal/workflow -run TestScenarios_GoldenComparison`. Expect updates at minimum in `idle/adopt-only.md`; possibly more if bootstrap-* atoms are referenced from envelope render paths.

### P5 — verification

- `go test ./... -short` green.
- `go test -race ./... -short` green.
- `make lint-local` 0 issues.
- Atom goldens regenerated + diff reviewed.
- Verify wire shapes manually via curl on `eval-zcp`:
  - `zerops_discover` returns `adoptionState` per service, no `managedByZcp`, warning prose matches state.
  - `zerops_env action=get serviceHostname="db"` returns `EnvGetResponse` shape, NO `adoptionState`, NO `services[]`, NO discover-style fields.

## Acceptance

Phase ships only when:

1. Per-item checklist walked + reported (Plan Fidelity rule).
2. `go test ./... -short -count=1` green.
3. `go test -race ./... -short` green.
4. `make lint-local` 0 issues.
5. All test files listed in P3 updated; new parity tests in `discover_classifier_parity_test.go` pass.
6. Atom edits + golden regen verified in same commit.
7. `e2e/instructions_test.go` migrated.
8. Live verification: discover + env get wire shapes match the documented contracts.

## Out of scope

- **Container-side auto-adopt** (Karel's separate ask #2 in current discussion) — separate plan; the 5-state enum here is a prerequisite (auto-adopt classifier would produce `AdoptionState` values).
- **Renaming `IsInfrastructure`** — orthogonal axis (managed-service-kind, not adoption); collapsing loses type information.
- **Refactoring parallel classifiers** (workflow_route.go, route.go::adoptableServices, etc.) — parity tests guard semantics; refactor lands as separate cleanup if needed.
- **Static `IsInfrastructure` from prefix-list drift** (Codex Section 7 note) — `topology.IsManagedService` is the existing source of truth; live-platform `IsInfrastructure` field would be a separate concern.

## Risks

- **Wire-shape breaking change for `env get` consumers** (clean break per Karel). Mitigation: `e2e/instructions_test.go` migration in same commit; flow-eval verification post-release.
- **Resumable-state semantics drift between discover classifier and workflow route logic.** Mitigation: parity tests in `discover_classifier_parity_test.go`.
- **Atom goldens churn — may surface unexpected diff in non-target atoms** (envelope render references the changed atoms). Mitigation: review golden diff in same commit; only target atom bodies change.

## Open questions for second Codex pass

1. **`env.go` migration approach:** clean break (default) vs dual-emit one release. Is there a third-party consumer or saved harness that depends on env get returning DiscoverResult shape?
2. **Resumable warning prose:** is `route=resume` the only path, or are there subroutes (e.g., resume-specific session ID) the warning should namespace?
3. **Orphan meta (incomplete + empty BootstrapSession) classification as `adoptable`:** does this match `workflow.adoptableServices` semantics exactly? `route.go:309` says "Incomplete meta with BootstrapSession tag is resumable, not adoptable" — implying empty BootstrapSession + incomplete = adoptable. Plan reflects this. Worth re-verifying against any other code path that might treat orphans differently.
4. **System-service filter in scoped path returning `ErrServiceNotFound`:** is there ANY legitimate user-facing case where targeting a system service makes sense? If yes, alternative behavior (e.g., return service info but flag it as `system` AdoptionState) might be safer.
5. **Parity tests location** (`discover_classifier_parity_test.go` in `internal/tools/`): or move to `internal/workflow/` where the parallel classifiers live? Layer ownership question.

Ship after second Codex pass clears these.
