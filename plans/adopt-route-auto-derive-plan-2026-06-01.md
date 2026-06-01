# Adopt route — auto-derive the bootstrap plan (kill the 14-failure hand-authoring slog)

**Date:** 2026-06-01
**Status:** approved (Codex: GO-WITH-REVISIONS — all 4 findings folded into §3.1/§3.2/§4 below)
**Scope owner:** krls2020
**Effort:** ~0.5 day; small net-positive LOC (re-wires one orphaned function + one engine method + one atom edit).

### Codex revisions applied (2026-06-01)
1. **Candidate filter = the canonical adoptable classifier** — reuse
   `adoptableServices(existing, metas, self)` (`route.go:291`), not an ad-hoc
   status/meta filter, so empty-plan adopt covers EXACTLY what discover labels
   `adoptable` (no status filter; orphan-meta services included; self-exclusion via
   `runtime.Info`). (§3.1)
2. **Dispatch on `len(input.Plan)==0`, not `!= nil`** — `plan:[]` (empty array) must
   trigger auto-derive too, else it advances discover with zero targets and writes no
   metas. Pin both omitted and `[]`. (§3.2)
3. **Explicit adopt plan gets live services too** — for `route=adopt`, fetch live
   services and pass to BOTH `BootstrapCompletePlan` (explicit) and auto-derive, so the
   "explicit standard-pair" escape hatch is validated against live state (catches a
   missing stage runtime / dep). Classic/recipe keep `nil` (out of scope). (§3.2)
4. **Pairing: refuse-and-prompt BEFORE commit, never silently commit a guess** — the
   post-commit "re-run with explicit plan" hatch was unusable (discover already advanced
   → needs a reset). Instead, when exactly two adoptable runtimes share the SAME type
   (the canonical dev/stage shape), auto-derive REFUSES and returns copy-pasteable
   standard-pair + independent-dev templates; one runtime or mixed types commit
   frictionlessly. Type-based detection — no revived hostname-suffix heuristic. (§4)

---

## 1. The problem (verified against code + a live agent transcript)

A live Codex session ("create a weather dashboard") tried to adopt two pre-existing
runtime services (`appdev` + `appstage`, both `alpine/php-nginx@8.4`) via
`zerops_workflow action=start workflow=bootstrap route=adopt`. It then spent
**~14 consecutive validation failures** reverse-engineering the `complete
step=discover` plan shape by fuzzing:

```
{hostname,type,resolution}        → unexpected additional properties
{targets:[...]}                   → type object, want array
{serviceHostname,serviceStack,…}  → unexpected additional properties
(no plan)                         → attestation required        (misleading)
["appdev","appstage"]             → string, want object
{target:…} {service:…} {name:…}   → unexpected additional properties  (blind fuzzing)
{},{}                             → required: missing ["runtime"]      (first useful signal)
{runtime:"appdev"}                → runtime needs devHostname,type,bootstrapMode
bootstrapMode:"adopt"             → must be standard|dev|simple         (route name misleads)
bootstrapMode:"standard"          → requires explicit stageHostname
resolution:"EXISTING"             → must be CREATE|EXISTS|SHARED
resolution:"EXISTS"               → SUCCESS (15th attempt)
```

This is **route=adopt only**. For adoption the plan is **100 % mechanically
derivable** from what `zerops_discover` already returned: every service with
`adoptionState="adoptable"` becomes a target with `isExisting=true`; every
`managed-dep` becomes an `EXISTS` dependency. The agent is hand-typing data the
tool already has.

## 2. Root cause (a regression with a paper trail)

| Commit | Effect |
|---|---|
| `41ddde57` (8 Apr) | Added **auto-adopt**: `adoptUnmanagedServices` → `workflow.InferServicePairing(candidates)` → ran the bootstrap adoption flow automatically when no metas existed. **The agent authored no plan.** |
| `cb63bf32` (21 Apr) | Removed auto-adopt from develop-start — *"adoption belongs in bootstrap route='adopt', not a hidden side-effect of develop start."* Architecturally correct. **But the move was incomplete:** `InferServicePairing` was never wired into the `route=adopt` discover step. |
| today | `InferServicePairing` (`internal/workflow/adopt.go:39`) is **orphaned exported code** — its only callers are in `adopt_test.go`. `route=adopt` makes the agent hand-author the full nested plan (`engine.go:338`: *"the discover step's plan is expected to mirror them"*). |

So the fix is **not new behavior** — it is finishing the `cb63bf32` move:
re-connect the derivation that already exists, scoped to the explicit adopt route
(honoring the "adoption is explicit" decision, not reviving the implicit
develop-start side-effect).

### Why the hand-authoring is so brutal (compounding friction, all bypassed by the fix)

These exist but become irrelevant for adopt once the agent never authors the plan
(they still affect classic/recipe authoring — see §6 Out of scope):

- **F1** The MCP SDK validates the generated JSON schema (`additionalProperties:false`,
  `required`) **before** the handler runs, so the actionable flatten diagnostic in
  `BootstrapTarget.UnmarshalJSON` (`validate.go:58`) is structurally unreachable —
  the agent gets the terse SDK error, never the "must nest inside runtime" message.
  (Same mechanism that rejected `launchKey` pre-F1, per `workflow_schema_test.go:108`.)
- **F2** Nested `RuntimeTarget`/`Dependency` struct fields carry no `jsonschema`
  descriptions — only the giant `Plan` field wall-of-text (`workflow.go:51`) documents
  the shape, and it evidently didn't reach / wasn't parsed by the agent.
- **F3** Route is named "adopt" but adoption is signaled by `isExisting:true` on a
  normal mode — there is no `bootstrapMode:adopt`. The vocabulary actively misleads.

## 3. Design — runtime

**Principle:** for `route=adopt`, an empty/omitted plan means *"derive it for me."*
An explicit plan is still honored (power-user override, e.g. adopt a dev/stage pair
as `standard` mode). All other routes are untouched.

### 3.1 Engine — new method + extracted core (no duplication)

Extract the validate-store body of `BootstrapCompletePlan` (`engine.go:540`) into a
private `completePlanWithTargets(state, targets, liveTypes, liveServices)` and have
two public entrypoints call it:

- `BootstrapCompletePlan(targets, liveTypes, liveServices)` — unchanged signature,
  explicit-plan path (all routes).
- **new** `BootstrapCompleteAdoptPlan(existing []platform.ServiceStack, self runtime.Info, liveTypes []platform.ServiceStackType)`:
  1. load state; require `state.Bootstrap.Route == BootstrapRouteAdopt`, else
     `fmt.Errorf("auto-derive plan is adopt-route only; submit an explicit plan")`;
     require current step `discover`.
  2. `metas := ListServiceMetas(e.stateDir)`;
     `adoptable := adoptableServices(existing, metas, self)` — **the canonical
     classifier** (`route.go:291`, Codex rev 1). It already excludes system, managed,
     self (`runtime.Info`), complete metas, and resumable (incomplete meta +
     `BootstrapSession`). No ad-hoc status filter.
  3. if `len(adoptable)==0` → `fmt.Errorf("no adoptable runtime services found — nothing to adopt")`.
  4. **pairing guard (Codex rev 4):** if `len(adoptable)==2` and both share the same
     `ServiceStackTypeVersionName` → return `ErrAdoptPairingChoice` carrying two
     copy-pasteable templates (standard-pair + independent-dev). Do NOT commit — state
     stays at `discover` in_progress; the agent resubmits an explicit plan.
  5. build `[]AdoptCandidate` = the adoptable runtimes (hostname+type) **plus** every
     managed `existing` service (so `InferServicePairing` attaches them as shared
     `EXISTS` deps); `targets := InferServicePairing(candidates, knowledge.ManagedBaseNames(liveTypes))`.
  6. `completePlanWithTargets(state, targets, liveTypes, existing)` — same validation,
     hostname-lock check, plan storage, step advance as the explicit path; sets a
     response message naming the adopted hosts + deps. The pure-adoption fast-path
     (`IsAllExisting` → skip close, `engine.go:509`) fires for free.

`InferServicePairing` already sets `IsExisting=true`, `BootstrapMode=PlanModeDev`,
and shares managed deps as `EXISTS` (`adopt.go:60-78`) — no change to it.

### 3.2 Tool handler — dispatch on empty plan + adopt route

In `handleBootstrapComplete` (`workflow_bootstrap.go:31`), the `step=="discover"`
branch becomes (route read once via `engine.GetState().Bootstrap.Route`):

```
route == adopt:
    existing := ops.ListProjectServices(ctx, client, projectID)   // fetched ONCE
    len(input.Plan) == 0  → BootstrapCompleteAdoptPlan(existing, rt, liveTypes)   // auto-derive, no attestation
    len(input.Plan) >  0  → BootstrapCompletePlan(input.Plan, liveTypes, existing) // explicit, live-validated (Codex rev 3)
route != adopt:
    input.Plan != nil     → BootstrapCompletePlan(input.Plan, liveTypes, nil)      // unchanged
    else                  → attestation fall-through                               // unchanged
```

Dispatch keys on `len(input.Plan)==0` so `plan:[]` triggers auto-derive too (Codex
rev 2). Live services fetched through `ops.ListProjectServices(ctx, client, projectID)`
(layer-2 rule — tools never call `client.ListServices` directly) and reused for both
adopt paths. `rt runtime.Info` is threaded into `handleBootstrapComplete` (new param;
the dispatch site at `workflow.go:423` already holds `rt`). The authoritative
`route==adopt` check stays in the engine method; the handler pre-read only selects the
dispatch (Codex note). The auto-commit response names what was adopted; the pairing
choice surfaces as a pre-commit refusal (§4), not a post-commit message.

### 3.3 Guidance — the adopt discover atom tells the agent it authors nothing

Edit the `bootstrap-adopt-discover` atom (rendered into golden
`testdata/atom-goldens/bootstrap/adopt/discover-existing-pair.md`, currently stops at
*"note the hostname, type, ports"* and never shows a plan shape). New contract:

> Adoption needs no hand-authored plan. After `route=adopt`, call
> `zerops_workflow action="complete" step="discover"` with **no `plan`** — ZCP derives
> the adoption plan from every `adoptionState="adoptable"` service (each adopted as an
> independent dev container; managed deps attached as `EXISTS`). To adopt a dev/stage
> pair as a single `standard`-mode pair instead, submit an explicit plan.

Trim the now-obsolete "note hostname/type/ports for the plan" steps. Tighten the
`Plan` field jsonschema (`workflow.go:51`) with one clause: *"route=adopt: omit plan —
ZCP derives it from adoptable services; submit a plan only to override the derived shape."*

## 4. Pairing — refuse-and-prompt before commit (Codex rev 4, settled)

`InferServicePairing` does **no** dev/stage pairing — each runtime becomes an
independent `PlanModeDev` container. Auto-committing that for `appdev`+`appstage` would
**silently lose** the dev→stage pair relationship that later promote/launch flows
depend on, and the post-commit "submit an explicit plan instead" hatch is unusable
(discover already advanced to provision → the agent would have to `reset` first). So
auto-derive **refuses and prompts before committing** when the shape is ambiguous:

| Adoptable runtimes | Behaviour |
|---|---|
| 0 | error: *"no adoptable runtime services found — nothing to adopt"* |
| 1 | **auto-commit** as a dev container (frictionless); message names it + its deps |
| exactly 2, **same type** | **refuse-and-prompt** (`ErrAdoptPairingChoice`): return two copy-pasteable templates — a `standard`-mode dev/stage pair AND independent-dev — agent resubmits ONE as an explicit plan (one round-trip, exact paste, no fuzzing) |
| 2 mixed types, or 3+ | **auto-commit** independent dev containers (pairing across types is nonsensical; N replicas are deterministically independent); message names them |

- **Why type-based detection, not hostname suffix:** the `{base}dev`→`{base}stage`
  suffix heuristic was deleted in `16f2640a` (misclassified `frontend-app`+
  `frontend-app-prod`, overrode author intent). "Exactly two runtimes of the same type"
  is the canonical dev/stage shape and never inspects hostname structure — no revived
  heuristic. We use it only to decide whether to **ask**, never to commit a guess.
- **Cost:** a genuine two-same-type-but-independent case (e.g. two separate workers)
  costs one extra round-trip (paste the independent template). Acceptable; never wrong.
- **Trade-off / alternative considered:** auto-commit independent-dev always + a strong
  response message — **rejected** (Codex): the message lands *after* the commit/advance,
  so the correction needs a `reset`; refusing pre-commit keeps the state recoverable and
  surfaces the real choice when it's still cheap.

## 5. Backward compatibility

- Explicit-plan adopt sessions (anyone already submitting a full plan) — **unchanged**;
  `BootstrapCompletePlan` keeps its signature and behavior.
- Classic / recipe routes — **untouched** (empty-plan handling for them is unchanged;
  auto-derive is gated on `route==adopt`).
- `route=adopt` + empty plan previously fell through to the attestation path and left
  `Plan==nil` → broke `provision` (`engine.go:468`). That path was never a successful
  flow; replacing it with auto-derive only turns a dead-end into success. No user has a
  working empty-plan-adopt flow to preserve.
- State files / atoms / `mcp__zerops__*` permissions — no surface change.

## 6. Out of scope (explicitly deferred, NOT silently dropped)

F1–F3 (§2) still hurt **classic/recipe plan authoring** (where the agent genuinely
must author). They are a **separate root cause** (SDK validates schema before the
handler diagnostic) with wider blast radius. Per the triage rule they are backlogged,
not folded in here:

- **Backlog entry to create:** `plans/backlog/plan-schema-author-friction.md` —
  F1 (make the flatten diagnostic reachable, e.g. relax `additionalProperties` on
  `BootstrapTarget` so `UnmarshalJSON` runs, or normalize the flat shape), F2 (per-field
  `jsonschema` tags on `RuntimeTarget`/`Dependency`), F3 (accept `EXISTING`→`EXISTS`
  normalization + a clearer route-vocab error). Trigger to promote: next classic/recipe
  flow-eval that shows plan-authoring fuzzing.

This plan fully resolves the **reported** pain (adopt), which is the only route in the
transcript. Flagging F1–F3 as deferred rather than shipping them silently.

## 7. Migration — phases (each compiles + green, RED first)

1. **RED** — engine tests against the new method (fixtures mirror `engine_test.go:638`
   adoption shapes):
   - `TestBootstrapCompleteAdoptPlan_SingleRuntime_AutoCommits` — adopt route, live
     `appdev`+`db` → 1 `isExisting` dev target + `db` `EXISTS` dep; plan stored; step
     advances; close fast-path skipped via `IsAllExisting`; message names `appdev`.
   - `TestBootstrapCompleteAdoptPlan_TwoSameType_RefusesWithTemplates` — `appdev`+
     `appstage` same type → `ErrAdoptPairingChoice`; state NOT advanced (still discover);
     error string contains both a `standard`-pair and an independent-dev template.
   - `TestBootstrapCompleteAdoptPlan_MixedTypes_AutoCommitsIndependent` — `appdev`(php)+
     `api`(go) → 2 independent dev targets committed.
   - `TestBootstrapCompleteAdoptPlan_RejectsNonAdoptRoute` — classic route → error.
   - `TestBootstrapCompleteAdoptPlan_UsesCanonicalAdoptable` — a hostname with a complete
     meta (already adopted) and a resumable (incomplete meta + BootstrapSession) are both
     excluded; an orphan-meta (incomplete, no session) IS included — parity with
     `adoptableServices`.
   - `TestBootstrapCompleteAdoptPlan_NoRuntimes_Errors` — only managed live → error.
2. **GREEN** — extract `completePlanWithTargets`; add `BootstrapCompleteAdoptPlan`
   (§3.1) + `ErrAdoptPairingChoice`. Verify `BootstrapCompletePlan` behavior
   byte-identical (pure refactor of its core — no RED, all engine tests stay green).
3. **Handler** — dispatch in `handleBootstrapComplete` (§3.2). Tool tests:
   - `TestHandleBootstrapComplete_AdoptEmptyPlan_AutoDerives` — `route=adopt`, complete
     `step=discover` with no plan → derives (no "attestation required", plan present,
     advances).
   - `TestHandleBootstrapComplete_AdoptEmptyArray_AutoDerives` — `plan:[]` (Codex rev 2)
     takes the same auto-derive path, NOT the zero-target commit.
   - `TestHandleBootstrapComplete_AdoptExplicitPlan_PassesLiveServices` — explicit adopt
     plan naming a non-existent stage runtime is rejected (Codex rev 3: live services
     reach the validator).
4. **Guidance** — edit `bootstrap-adopt-discover` atom + regenerate the adopt golden;
   tighten `Plan` jsonschema clause (§3.3). Atom-lint + golden test green.
5. **Backlog + CLAUDE.md** — write the F1–F3 backlog file (§6); add a one-line
   convention bullet ("adopt route auto-derives the plan from adoptable discovery;
   `InferServicePairing` is its single consumer — pinned by
   `TestBootstrapCompleteAdoptPlan_*`"). `git mv` this plan → `plans/archive/` after ship.

Eval gate: a `route=adopt` flow-eval (or the next behavioral run) should show
`complete step=discover` succeeding in **one** call with no plan.

## 8. Residual risks (honest)

- Independent-dev default loses pairing for `appdev`/`appstage` (§4) — mitigated by the
  response-message escape hatch + explicit-plan override. Accept for v1; Codex may
  prefer refuse-and-prompt-on-likely-pair.
- Auto-derive includes a service the user did *not* want adopted (e.g. an unrelated live
  runtime). Mitigation: it only ever attaches ZCP tracking (`isExisting=true`, no infra
  change) and only to `adoptable` (no-meta, non-managed, ACTIVE) services; close is the
  cheap undo. Acceptable.
- Extra `ListServices` call on the empty-plan adopt path — one bounded call, off any
  latency-critical loop. Negligible.
</content>
</invoke>
