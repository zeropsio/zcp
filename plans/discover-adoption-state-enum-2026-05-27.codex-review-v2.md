# Codex validation v2: discover `adoptionState` enum

Verdict: revise before implementation. V2 fixes the main enum-shape flaw from
v1, but it introduces/retains three material gaps: resumable warning prose is
not directly executable, the planned parity-test location cannot directly call
the workflow helpers it names, and `workflow_route.go::unmanagedRuntimes` is
still omitted even though it is one of the drifted classifiers called out in
v1.

## Section A — v1 findings addressed item-by-item

### 1. Architectural soundness

Mostly addressed.

- **Resumable state:** fixed. V2 adds the fifth enum state in `## Design /
  Types`, wires it into `### Classification logic`, and adds P3 test coverage
  for incomplete meta + non-empty `BootstrapSession` → `AdoptionResumable`.
  This addresses the v1 blocker where a resumable bootstrap would have been
  mislabeled as adoptable.
- **`adopted` naming:** addressed by explicit decision. V2 keeps `adopted`,
  but adds the required definition in `## Design / Types`: "ZCP-tracked via
  complete ServiceMeta, regardless of how it got there", plus a P4 atom-guidance
  requirement. That is enough if the naming decision is fixed.
- **Scoped system services:** addressed. V2 adds a scoped `ops.Discover` filter
  in `### System-service filter` and a P1 implementation item. Returning
  `ErrServiceNotFound` matches unfiltered discover semantics.
- **Remaining gap:** the resumable warning in `### Warning derivation` says to
  run `zerops_workflow action="start" workflow="bootstrap" route="resume"` but
  the tool handler rejects `route=resume` without `sessionId`
  (`internal/tools/workflow.go:961-967`). This is not a state-model flaw, but it
  means the recovery prose in the warning is incomplete.

### 2. Classification ordering

Addressed for the discover classifier.

V2's switch order matches the required order from v1:

1. `zcp@1`
2. managed dependency / `IsInfrastructure`
3. complete meta
4. incomplete meta with `BootstrapSession != ""`
5. default adoptable

The `adoptable`/`resumable` boundary matches
`workflow.adoptableServices` for live runtime services:

- `route.go:304-310` excludes complete metas and incomplete metas with
  `BootstrapSession`.
- `route.go:312` appends everything else, so incomplete meta + empty
  `BootstrapSession` is adoptable.
- `compute_envelope_test.go:358-395` pins the same orphan behavior:
  orphan incomplete meta is not resumable and falls to `IdleAdopt`.

Two caveats remain:

- Workflow's adoptable filter also excludes the current ZCP host via
  `isSelf` (`route.go:301-302`), while discover can only identify ZCP self by
  `Type == "zcp@1"`. That is acceptable for the current platform-known self
  type, but it is not exact parity with workflow semantics.
- The static managed-service prefix drift risk is explicitly moved out of
  scope in V2. That is a conscious scope decision, not a fix.

### 3. Implementation gotchas / call sites

Partially addressed.

- **Env wire leak:** addressed in principle. P2 replaces `env.go`'s direct
  `jsonResult(ops.DiscoverResult)` return with a dedicated `EnvGetResponse`,
  which makes `adoptionState` structurally absent from `zerops_env action=get`.
  This is better than `omitempty`.
- **Internal `ops.Discover` callers:** addressed. V2 correctly identifies the
  six callers and distinguishes the single public leak (`internal/tools/env.go`)
  from internal launch/export reads.
- **Parallel classifier drift:** only partially addressed. V2 adds parity tests
  for `workflow/route.go`, `workflow/adopt.go`, and
  `tools/launch_source_context.go`, but it omits
  `internal/tools/workflow_route.go::unmanagedRuntimes`, which v1 explicitly
  called out. That path currently treats any non-managed runtime without a
  complete meta as unmanaged (`workflow_route.go:41-43`), including incomplete
  metas with `BootstrapSession`. That can surface an adopt hint for the same
  service the workflow route layer treats as resumable.
- **New EnvGetResponse gap:** V2 preserves `notes` but not `warnings`. Raw
  `ops.Discover` uses `Warnings` for env fetch failures
  (`internal/ops/discover.go:262-279`). Dropping warnings from env-get would
  silently hide partial env-read failures.

### 4. Backward compatibility

Resolved by operator decision, not by the review recommendation.

V1 recommended keeping `managedByZcp` as a deprecated alias for a compatibility
window. V2 instead records Karel's decision to drop it plainly, and adds P3/P5
checks that `managedByZcp` is absent and `e2e/instructions_test.go` is migrated.

That addresses the plan's chosen policy. I still would not justify the removal
by saying `CLAUDE.local.md` prefers structural correction over wire stability:
the local policy says published user-facing surfaces must stay compatible unless
the operator explicitly overrides that. The plan has that explicit override, so
this is not a blocker.

### 5. Test surface

Mostly addressed, but with two needed revisions.

Good additions in V2:

- Per-state classification tests in `discover_adopt_hint_test.go`.
- Warning tests for adoptable, resumable, mixed, and fully adopted cases.
- Wire-shape tests in `discover_test.go`.
- `env_test.go` regression that env-get does not contain `adoptionState`.
- `e2e/instructions_test.go` migration.
- Scoped system-service test.
- Atom golden regeneration.

Needed revisions:

- Put the scoped system-service unit test in `internal/ops/discover_test.go`
  because the filter is in `ops.Discover`. A tool-level test in
  `internal/tools/discover_test.go` is optional coverage for `convertError`,
  but it does not replace the ops-layer test.
- The proposed `internal/tools/discover_classifier_parity_test.go` cannot
  directly call `workflow.adoptableServices` or `workflow.resumeOption`; both
  are unexported. Either test parity through the exported
  `workflow.BuildBootstrapRouteOptions` black-box from `internal/tools`, or put
  workflow-only parity tests in `internal/workflow` and keep discover classifier
  tests in `internal/tools`.

### 6. Atom guidance scope

Addressed.

V2 expands P4 to the required atoms:

- `idle-adopt-entry.md`
- `bootstrap-adopt-discover.md`
- `bootstrap-resume.md`

It also correctly keeps `bootstrap-route-options.md` optional and requires
`references-fields` updates plus golden regeneration.

One wording adjustment: `bootstrap-resume.md` already teaches that resume needs
`resumeSession`/`sessionId`. The discover warning should either include the
session ID or explicitly say "call bootstrap discovery first, read
`routeOptions[].resumeSession`, then call `route=resume sessionId=...`".

### 7. Additional risks

Partially addressed.

- Empty `adoptionState` leak from env-get: addressed by dedicated
  `EnvGetResponse`.
- Resumable mislabeled as adoptable: addressed in the discover classifier.
- Scoped system service mislabeled as adoptable: addressed for
  `ops.Discover`.
- Static managed-service prefix drift: explicitly out of scope.
- Exact `zcp@1` self filtering: accepted but should be pinned only as exact
  current behavior.
- Immediate `managedByZcp` removal: operator decision accepted.
- Remaining risk from v1: `workflow_route.go::unmanagedRuntimes` remains a
  drifted classifier and is not in the V2 parity list.

## Section B — new findings in v2

### B1. Resumable warning points to an invalid direct call

V2's resumable warning says:

```text
Run `zerops_workflow action="start" workflow="bootstrap" route="resume"`
to continue the incomplete bootstrap
```

That call is rejected unless `sessionId` is also supplied. The handler says:

- `route=resume requires sessionId` in `internal/tools/workflow.go:961-967`.
- The handler comment says the LLM must pass `sessionId` from the discovery
  response's `resumeSession` field (`workflow.go:916-918`).

Recommendation: change the planned warning to one of these two shapes:

- Include session IDs in the warning if `enrichWithMetaStatus` has the meta:
  `... route="resume" sessionId="<bootstrapSession>" ...`.
- Or use a two-step recovery: call bootstrap discovery with no route, read the
  `resume` route option's `resumeSession`, then call `route="resume"`
  with `sessionId`.

Do not ship a warning that sends agents into an avoidable
`INVALID_PARAMETER` detour.

### B2. Parity test location/shape is currently not executable as written

The plan says `internal/tools/discover_classifier_parity_test.go` should assert
against:

- `internal/workflow/route.go::adoptableServices`
- `internal/workflow/route.go::resumeOption`

Those functions are unexported. A test in `internal/tools` cannot call them.

Recommendation: keep the test in `internal/tools` if the primary target is the
discover classifier, but compare against exported workflow behavior:

- Build the same fixture.
- Run `enrichWithMetaStatus`.
- Run `workflow.BuildBootstrapRouteOptions`.
- Compare discover `adoptable` hostnames with the `adopt` option's
  `AdoptServices`.
- Compare discover `resumable` presence/session with the `resume` option's
  `ResumeServices`/`ResumeSession`.

If the plan really needs direct tests for `adoptableServices` or
`resumeOption`, those tests belong in `internal/workflow`, not `internal/tools`.

### B3. `workflow_route.go::unmanagedRuntimes` is still omitted and currently diverges

V1 explicitly listed `internal/tools/workflow_route.go` as a parallel
classifier. V2's P3 parity list does not include it.

Current code:

```go
if !topology.IsManagedService(typeName) {
    if m, ok := metaIdx[s.Name]; !ok || !m.IsComplete() {
        unmanagedRuntimes = append(unmanagedRuntimes, s.Name)
    }
}
```

This treats incomplete meta + non-empty `BootstrapSession` as unmanaged, while
`workflow.adoptableServices` treats the same state as resumable. The result can
be a route surface that offers both resume and adopt-like guidance for one
service.

Recommendation: add `workflow_route.go` to the parity plan. Either align its
`unmanagedRuntimes` semantics with `AdoptionAdoptable`, or explicitly rename
the concept if it intentionally means "not complete" rather than "adoptable".
As written, it is the same semantic trap this plan is trying to remove.

### B4. `EnvGetResponse` must preserve warnings and define project scope

V2's `EnvGetResponse` includes:

```go
Service *EnvGetServiceInfo
Envs    []map[string]any
Project *ops.ProjectInfo
Notes   []string
```

It misses `Warnings []string`. That loses env-fetch diagnostics currently
emitted by `ops.Discover` when `GetServiceEnv` or `GetProjectEnv` fails
(`internal/ops/discover.go:262-279`).

The plan also needs to define `project=true` explicitly. Today
`zerops_env action=get project=true` calls unscoped `ops.Discover`, so it
returns `project.envs` plus all service entries. V2's new shape says `Envs`
contains "the asked-for vars" and `Project` is present for `project=true`, but
does not say whether project env vars live in top-level `envs` or
`project.envs`.

Recommendation:

- Add `Warnings []string json:"warnings,omitempty"` to `EnvGetResponse`.
- Define one canonical env list location for both scopes. I recommend
  top-level `envs` as the "asked-for vars" list, with `project` and `service`
  carrying only identity/status metadata.
- Add `env_test.go` cases for both service-scoped and `project=true` response
  shape, including warning preservation on env-fetch error.

### B5. `managedByZcp` removal rationale should be framed as an explicit override

The plan says the plain drop is justified by the local engineering priority.
That is too broad: `zerops_discover` is an MCP wire surface. The safe framing is
"Karel explicitly accepted the breaking change", not "the policy endorses
drops of released JSON fields".

Recommendation: keep the chosen behavior if that decision is final, but revise
the rationale so future readers do not generalize it into a standing rule.

## Section C — answers to v2 open questions

### 1. `env.go` migration: clean break vs dual-emit?

Recommendation: clean break is acceptable only because Karel explicitly chose
plain wire cleanup for this plan. I would not dual-emit `services[]`; that
keeps the leaky DiscoverResult shape alive and undermines the structural fix.

But do not ship the current proposed shape unchanged. Add `warnings`, define
`project=true` shape, and pin both service/project scopes in `env_test.go`.
Repo grep shows tests currently expect `services[0].envs` in
`internal/tools/env_test.go:86-94` and `150-156`. Atom grep does not show an
atom parsing env-get `services[]`; `bootstrap-recipe-import-local.md` only says
to call `zerops_env action="get" project=true`.

### 2. Resumable warning prose: route only or session ID?

`route=resume` is not enough. The warning must include or lead to
`sessionId`.

Best shape: include the `BootstrapSession` value directly when all resumable
services share one session. If multiple session IDs are present, tell the agent
to run bootstrap discovery first and choose the `resume` option it returns.

Minimum acceptable wording:

```text
Services with adoptionState="resumable": appdev. Call
`zerops_workflow action="start" workflow="bootstrap"` first, read the
`routeOptions[]` entry where `route="resume"` and its `resumeSession`, then call
`zerops_workflow action="start" workflow="bootstrap" route="resume"
sessionId="<resumeSession>"`.
```

### 3. Orphan meta classification as `adoptable`

Yes, for live runtime services this matches workflow semantics.

Evidence:

- `workflow.adoptableServices` appends a service when meta is nil or incomplete
  with empty `BootstrapSession` (`route.go:304-312`).
- `workflow.resumeOption` only considers incomplete metas with non-empty
  `BootstrapSession` (`route.go:330-354`).
- `TestBuildBootstrapRouteOptions_IncompleteMetaOrphan_AdoptNotResume` pins
  orphan → adopt (`route_test.go:240-258`).
- `TestComputeEnvelope_OrphanIncompleteMeta` pins orphan → `IdleAdopt`
  (`compute_envelope_test.go:358-395`).

The only qualifier is self filtering: workflow can exclude the current ZCP host
by runtime identity, while discover uses exact type `zcp@1`.

### 4. System-service filter returning `ErrServiceNotFound`

Recommendation: keep `ErrServiceNotFound` for `zerops_discover
service="<system-host>"`.

Discover is the user-facing inventory. Unfiltered discover already hides
system services, so scoped discover should not reveal them. Diagnostic use
cases are not blocked by this change: `zerops_logs` resolves hostnames through
`ops.FindService` and does not use `ops.Discover`, so a user who already has a
system/build hostname can still fetch logs. Build/deploy diagnostics also have
dedicated build-log/event paths.

One implementation detail for the plan: ensure the "Available services"
suggestion lists only user-visible services. `ops.ListHostnames(services)` would
include the system services unless the filtered slice is passed.

### 5. Parity tests location

Recommendation: put the discover-classifier parity test in `internal/tools`,
but compare against exported workflow APIs, not unexported helpers.

Use `workflow.BuildBootstrapRouteOptions` as the black-box authority for
adopt/resume route semantics. That avoids exporting workflow internals and
keeps the test next to the discover classifier (`enrichWithMetaStatus`) it is
validating.

Add separate `internal/workflow` tests only if you need direct coverage of
unexported helpers. Do not place a test in `internal/tools` that claims to test
`workflow.adoptableServices` directly; Go cannot do that without changing
visibility or using brittle indirection.

## Section D — verdict

Revise before implementation.

Concrete required revisions:

1. Fix resumable warning prose so it includes `sessionId` or instructs the
   bootstrap-discovery → `resumeSession` → resume flow.
2. Add `workflow_route.go::unmanagedRuntimes` to the parity scope and resolve
   its current resumable/adoptable drift.
3. Rewrite the parity-test plan so tests in `internal/tools` use exported
   workflow behavior (`BuildBootstrapRouteOptions`) or move direct helper tests
   to `internal/workflow`.
4. Add `warnings` to `EnvGetResponse`, define `project=true` response shape,
   and test service-scope, project-scope, no discover-field leakage, and
   warning preservation.
5. Pin the scoped system-service filter in `internal/ops/discover_test.go`
   specifically; add tool-level coverage only as extra.
6. Reframe the `managedByZcp` removal as an explicit Karel-approved breaking
   change, not as a general compatibility-policy precedent.

After those changes, the plan is structurally sound enough to implement.
