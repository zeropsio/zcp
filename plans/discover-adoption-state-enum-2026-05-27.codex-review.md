# Codex validation: discover `adoptionState` enum

Verdict: revise before implementation. The direction is right, but the
current plan is not safe to ship as written.

## 1. Architectural soundness

The 4-state enum fixes the immediate `managedByZcp:false` ambiguity for
ordinary live services, but it is not exhaustive for the existing ZCP state
machine.

Blocking flaw: runtime services with an incomplete `ServiceMeta` carrying a
non-empty `BootstrapSession` are neither `adopted` nor `adoptable`. The
workflow layer already treats those as resumable:

- `workflow.adoptableServices` excludes incomplete metas with
  `BootstrapSession != ""`.
- `workflow.resumeOption` routes them to `route="resume"`.
- `ComputeEnvelope` exposes the same condition as `Resumable` and
  `IdleIncomplete`.

The proposed switch classifies that service as `adoptable`, because the meta
is not complete and every non-infra non-zcp runtime falls through to default.
Then the warning tells the agent to run `route="adopt"`, which is the wrong
recovery for a resumable bootstrap. This preserves an existing edge-case bug
and makes it a documented enum value.

Fix: add a fifth runtime state, e.g. `resumable` or `bootstrap-incomplete`, or
explicitly exclude resumable metas from `adoptionState="adoptable"` and expose
their state somewhere else. If `adoptionState` is the single truth agents read,
it needs the fifth state.

Secondary naming issue: `adopted` is overloaded. The implementation comment
means "complete `ServiceMeta` exists", which includes greenfield bootstraps,
not only brownfield adoption. I would prefer `tracked` / `zcp-tracked`. If
Karel wants to keep `adopted`, the atom and Go comments must define it as
"ZCP-tracked via complete ServiceMeta", not "created by the adopt route".

Scoped system services are another edge case. Unfiltered discover omits
`ServiceStack.IsSystem()` services, but `zerops_discover service="<host>"`
uses `FindService` and can return a system service. The proposed default would
mark a scoped CORE/BUILD/PREPARE_RUNTIME service as `adoptable`. Either reject
or filter scoped system services in `ops.Discover`, or add a `system` state.
Do not let the default branch cover them.

## 2. Classification ordering

For the four ordinary states, the order is correct:

1. `Type == "zcp@1"` first: ZCP self is a USER-category service and must not
   fall through to adoptable.
2. `IsInfrastructure` second: managed deps should win even if a stale meta file
   somehow exists.
3. complete meta third: pair-keyed complete metas make dev and stage halves
   tracked.
4. default: only true untracked runtimes should land here.

Required revised order:

1. Filter/reject system services, or classify `system` before default.
2. `zcp@1`.
3. managed dep.
4. complete meta.
5. incomplete meta with `BootstrapSession != ""` -> `resumable` /
   `bootstrap-incomplete`.
6. default -> `adoptable`.

Also note that current `ServiceInfo.IsInfrastructure` is derived from
`topology.IsManagedService(typeVersion)`, not from a platform-modeled
`IsInfrastructure` field. If the plan relies on a live platform boolean, that
field is not currently modeled in `platform.ServiceStack`. The enum will
inherit the static prefix-list drift unless that is addressed.

## 3. Implementation gotchas / call sites

Source grep result:

- `ManagedByZCP` source uses are only in:
  - `internal/ops/discover.go`
  - `internal/tools/discover.go`
  - `internal/tools/discover_adopt_hint_test.go`
  - `e2e/instructions_test.go`
- Atom and knowledge grep is clean for `managedByZcp` / `ManagedByZCP`.
- `unmanagedRuntimes` still exists in `internal/tools/workflow_route.go` as
  route-action input. That is separate from the reverted `DiscoverResult`
  field, but it is the same conceptual classifier and needs parity tests.

The plan misses a public-output gotcha: `ops.DiscoverResult` is not returned
only by `zerops_discover`.

- `internal/tools/env.go` returns `ops.Discover(...)` directly for
  `zerops_env action="get"`.
- `ops.Discover` is also used by export and launch internals.
- Only `zerops_discover` calls `enrichWithMetaStatus`.

If `ServiceInfo.AdoptionState` is added with `json:"adoptionState"` and no
`omitempty`, `zerops_env get` will serialize `adoptionState:""` because it has
no stateDir and does not run enrichment. That is worse than omitting the field.

Fix one of these explicitly:

- Move classification to a helper that every public `DiscoverResult` producer
  calls, and pass stateDir where required; or
- Make `adoptionState` `omitempty` and document that only `zerops_discover`
  guarantees it; or
- Stop returning the full `DiscoverResult` shape from `zerops_env get`.

The plan should not leave empty enum values on the wire.

Parallel classifier drift to account for:

- `internal/tools/workflow_route.go` computes `unmanagedRuntimes`.
- `internal/workflow/route.go::adoptableServices` computes adopt route
  candidates and handles resumable metas differently from the proposed switch.
- `internal/workflow/adopt.go::InferServicePairing` filters `zcp` types via
  prefix, not exact `zcp@1`.
- `internal/tools/launch_source_context.go::isZCPSelfService` filters zcp self
  by exact type plus hostname fallback.

The plan does not need to refactor all of them, but it must add parity tests
for the discover classifier against the workflow route semantics, especially
for `zcp@1` and resumable metas.

## 4. Backward compatibility

Pick option (b): keep `managedByZcp` as a derived deprecated alias for at least
1-2 releases.

Reason: `zerops_discover` is a published MCP wire surface. In-repo grep only
proves ZCP itself does not consume the field. It does not prove external MCP
clients, agent harnesses, user scripts, or saved eval tools do not parse it.
There is no schema version and no negotiated feature flag. Dropping a JSON key
from a released tool response is a breaking change.

Derived alias shape:

```go
// Deprecated: use AdoptionState. True only when AdoptionState is adopted/tracked.
ManagedByZCP bool `json:"managedByZcp"`
```

Set it from the enum, not from a second classifier. If the enum value is
renamed to `tracked`, derive `managedByZcp = adoptionState == "tracked"`.

Third-party consumers identified from the repo: none by source import. But MCP
tool JSON is itself the external contract, and `e2e/instructions_test.go`
currently pins `managedByZcp` as live discover shape.

## 5. Test surface

The per-state tests in the plan are necessary but not sufficient.

Add or update:

- `internal/tools/discover_adopt_hint_test.go`
  - all planned state cases
  - warning lists only `adoptionState="adoptable"`
  - resumable incomplete meta does not get adopt warning and has the correct
    enum state or no adoptable state
  - scoped/system service cannot be classified as adoptable
- `internal/tools/discover_test.go`
  - JSON wire response includes non-empty `adoptionState` for
    `zerops_discover`
  - deprecated `managedByZcp` alias remains while compatibility window is open
- `internal/tools/env_test.go`
  - `zerops_env action=get` does not emit `adoptionState:""`; it is either
    enriched or omitted by contract
- `e2e/instructions_test.go`
  - replace the `managedByZcp`-only assertions with adoptionState assertions
    and, during compatibility, alias assertions
- workflow parity tests
  - incomplete meta with `BootstrapSession` maps to resume in workflow and
    does not appear as discover `adoptable`
  - zcp self stays excluded/classified consistently
- atom goldens
  - regenerate via `ZCP_UPDATE_ATOM_GOLDENS=1 go test ./internal/workflow -run TestScenarios_GoldenComparison`
  - expect updates at least in `idle/adopt-only.md`; more if
    `bootstrap-adopt-discover.md` is edited

The proposed `internal/ops/discover_state_exhaustive_test.go` is not enough if
the switch remains in `internal/tools/discover.go`. Put the consumer test next
to the consumer, or extract a classifier helper and test that helper directly.

## 6. Atom guidance scope

The plan should edit more than `idle-adopt-entry.md`.

Must update:

- `internal/content/atoms/idle-adopt-entry.md`
  - mention `adoptionState` because this is the entry point from the discover
    warning to bootstrap adoption.
  - add `ops.ServiceInfo.AdoptionState` to `references-fields`.
- `internal/content/atoms/bootstrap-adopt-discover.md`
  - it currently says "Read every user (non-system, non-managed) service."
    Replace that mental aggregation with "use services with
    `adoptionState=\"adoptable\"`" and explicitly ignore `managed-dep` and
    `zcp-self`.
  - add `ops.ServiceInfo.AdoptionState` to `references-fields`.

Should update if the fifth state is added:

- `internal/content/atoms/bootstrap-resume.md`
  - connect resumable/incomplete services to resume, not adopt.
- `internal/content/atoms/bootstrap-route-options.md`
  - one short clause that `adoptServices[]` corresponds to adoptable runtimes
    is enough; do not duplicate the full enum table here.

Optional non-atom follow-up:

- `internal/content/templates/agents_shared.md`
  - it is the global "discover before service-scoped work" instruction. A
    one-line rule saying `adoptionState="adoptable"` is not a valid
    develop/deploy scope would reinforce the new wire shape.

## 7. Additional risks not in the plan

- Empty enum values leaking from non-enriched `ops.Discover` callers.
- Existing resumable bootstrap state being mislabeled as adoptable.
- Scoped system services being mislabeled as adoptable.
- Static managed-service prefix drift if `IsInfrastructure` remains derived
  from `topology.IsManagedService` instead of a live platform field/catalog.
- Exact `Type == "zcp@1"` is enough for the current platform-known type, but
  other classifiers use broader/self-aware rules. Keep a test that pins the
  exact `zcp@1` case and do not assume this replaces all self filtering.
- Removing `managedByZcp` immediately violates the repo's own published-surface
  compatibility policy.

## 8. Concrete revision items

1. Decide the real enum shape. I recommend:
   `tracked` (or documented `adopted`), `adoptable`, `resumable`,
   `managed-dep`, `zcp-self`. Separately filter/reject scoped system services
   or add `system`.
2. Keep `managedByZcp` as a deprecated derived alias for the compatibility
   window.
3. Define where adoptionState is guaranteed. Either enrich every public
   `DiscoverResult` producer or make the field omitted outside
   `zerops_discover`.
4. Add parity tests against workflow resume/adopt semantics.
5. Update `e2e/instructions_test.go`, `internal/tools/discover_test.go`, and
   `internal/tools/env_test.go`; do not limit tests to
   `discover_adopt_hint_test.go`.
6. Update at least `idle-adopt-entry.md` and `bootstrap-adopt-discover.md`,
   plus `bootstrap-resume.md` if the resumable state is added.

Ship after those revisions. Do not implement the current 4-state switch as-is.
