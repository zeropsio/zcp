# Subdomain auto-enable foundation fix

> **Status**: PLAN, awaiting approval before execution.
> **Date**: 2026-05-03
> **Predecessor commits**: `fef12f3e` (R-13-12), `82b89639` (R-14-1), `0fc2534e` (record-deploy P7), `6a246201` (F8 unified predicate), `b92deb91` + `3f7e28bf` (B12 atom guidance), `537a3190` (verify Recovery field).
> **Related backlog**: `plans/backlog/cross-deploy-stage-subdomain-auto-enable-suspect.md` (closes out as part of this plan).
> **Live verification**: 2026-05-03, probe service on `eval-zcp` (project `i6HLVWoiQeeLv8tV0ZZ0EQ`) confirmed root cause empirically.

---

## 1. Why this exists

User report (2026-05-03, notes-dashboard session, fresh Claude Code instance against ZCP main):

> `enableSubdomainAccess: true` in the import YAML didn't actually provision the subdomain on first deploy. Both `appdev` and `appstage` deployed successfully and reported ACTIVE, but `zerops_verify` then failed with `http_root: fail` — subdomain access not enabled. I had to manually call `zerops_subdomain action="enable"` on each service.

The verify recovery hint (`537a3190`) makes the agent self-heal, but every standard-mode first deploy burns an extra round-trip (`verify → recovery → enable → re-verify`). The same symptom was already SUSPECTed in `cross-deploy-stage-subdomain-auto-enable-suspect.md` on 2026-04-30 — this plan resolves that suspect.

Per `CLAUDE.local.md`: **fix the foundation, not the symptom.** The atom guidance (B12) and verify recovery are compensations for a broken predicate.

---

## 2. Root cause — verified live

### 2.1 The broken predicate

`internal/tools/deploy_subdomain.go:150-190` — `serviceEligibleForSubdomain`:

```go
if detail.SubdomainAccess { return true }       // POST-enable state
for _, port := range detail.Ports {
    if port.HTTPSupport { return true }          // POST-enable state (mapped from HttpRouting)
}
return false
```

Both signals are claimed (commit `82b89639` body) to reflect plan-declared intent. **They do not.** Both flip true only AFTER a successful `EnableSubdomainAccess` call.

### 2.2 Live evidence — `eval-zcp` probe service (2026-05-03)

Imported probe (nodejs@22) with `enableSubdomainAccess: true` + `ports[{port:3000, httpSupport:true}]`:

```
Step 1 (post-import, no deploy):
  zerops_discover → ports field omitted, no subdomainEnabled key
  zerops_subdomain enable → ✗ "Service stack is not http or https" (serviceStackIsNotHttp)

Step 2 (post-deploy with run.ports[].httpSupport: true):
  zerops_discover → ports[0] = {port: 3000, httpSupport: FALSE, protocol: tcp}
  zerops_subdomain enable → ✓ FINISHED, URL https://probe-21ca-3000.prg1.zerops.app

Step 3 (post-enable):
  zerops_discover → subdomainEnabled: true (now correctly visible)
                  → ports[0].httpSupport STILL FALSE
```

### 2.3 Smoking gun — platform DTO

`/Users/macbook/go/pkg/mod/github.com/zeropsio/zerops-go@v1.0.17/dto/output/servicePort.go:15-23`:

```go
type ServicePort struct {
    Protocol    enum.ServicePortProtocolEnum
    Port        types.Int
    Description types.EmptyString
    PortRouting types.BoolNull
    HttpRouting types.BoolNull         // ← what we read
    Scheme      enum.ServicePortSchemeEnum
    ServiceId   uuid.ServiceIdNull
}
```

**No `httpSupport` field on the platform DTO.** Our `Port.HTTPSupport` (`internal/platform/zerops_mappers.go:166`) is `HttpRouting`, the post-enable routing flag. `detail.SubdomainAccess` exhibits the same semantics.

### 2.4 Consequence

For any service that has not yet had subdomain enabled:
- `detail.SubdomainAccess = false`
- `detail.Ports[].HTTPSupport = false`

→ predicate returns `false` → `maybeAutoEnableSubdomain` silently `return`s → agent must manually call `zerops_subdomain enable` for every fresh service. The chain of fixes (R-13-12, R-14-1, F8) all operated on this misunderstanding. Tests passed because mock fixtures pre-set `SubdomainAccess: true` (modeling unreachable platform state).

---

## 3. Design — let the platform answer

The wrong question was "should we attempt enable?" (predicting eligibility from broken signals). The right question is "is subdomain enabled? if not, attempt — platform tells us in one call whether the service is HTTP-shaped."

### 3.1 The algorithm

```
After deploy success:
  if meta != nil && meta.Mode != "" && !modeAllowsSubdomain(meta.Mode):
    return                                    # production / unknown mode opt-out
  
  result = ops.Subdomain.Enable(service)
    
  switch:
    err == nil && result.Status == AlreadyEnabled:
      copy URLs (no probe — service was already serving traffic)
    err == nil:
      copy URLs + WaitHTTPReady on subdomain
    isServiceStackIsNotHttp(err):
      return                                  # platform: "no HTTP shape" → benign skip
    err != nil:
      result.Warnings += "auto-enable subdomain: <err>"
```

### 3.2 What this replaces

- `serviceEligibleForSubdomain`: collapses to mode-allowlist + `IsSystem()` check. No `GetService` REST call. No `detail.SubdomainAccess` check. No `detail.Ports[].HTTPSupport` check.

`IsSystem()` defensive check is **kept** (`deploy_subdomain.go:174`). Codex's third review caught that the upstream filters (`discover.go:76`, `deploy_local.go:81`, `deploy_ssh.go:87`, `guard.go:61`) do NOT enforce service-category filtering on explicit `targetService` resolution paths — `FindService`/`GetService` accept any hostname. Dropping `IsSystem()` based on "five upstream filters" was insufficient proof. Keeping the guard:

- Costs one `ListServices` RT (~50-100ms) — half what the old predicate did (which also called `GetService`).
- Defends against future caller paths that may not yet exist.
- Maintains the spec invariant that L7 routing is never auto-enabled on platform-internal stacks.

Net REST-call change vs current code: **2 RTs → 1 RT** (removes the `GetService` call used for the broken DTO checks).

### 3.3 Why `ops.Subdomain.Enable` already does most of the work

- Check-before-enable lives there (`internal/ops/subdomain.go:176-183`): if `detail.SubdomainAccess` is true, returns `SubdomainStatusAlreadyEnabled` without an API call. Idempotency is built in. `SubdomainAccess` is honestly used here as "is currently enabled" — that semantic is correct for already-enabled detection.
- Bounded retry on `noSubdomainPorts` (`enableSubdomainAccessWithRetry`): handles L7 propagation race added in 82b89639. Stays.
- Belt-and-suspenders TOCTOU normalization at tool layer: stays (different concern).

### 3.4 What we add

A single helper, **placed in `internal/tools/deploy_subdomain.go`** (caller side, not ops):

```go
func isServiceStackIsNotHttpErr(err error) bool {
    var pe *platform.PlatformError
    if !errors.As(err, &pe) { return false }
    return pe.APICode == "serviceStackIsNotHttp"
}
```

`errors.As` on `*platform.PlatformError` works across packages, so the helper does not need to live in ops. Keeping it caller-side preserves Codex review §3 invariant: `ops.Subdomain.Enable` stays unchanged and untouched, so explicit `zerops_subdomain enable` recovery callers still see `serviceStackIsNotHttp` as a real error. The downgrade is contextual (auto-enable proves benign), not structural.

### 3.5 What we deliberately do NOT add

Per `CLAUDE.local.md` "Single path over multiple variants":

- **No** new `ServiceMeta.SubdomainIntent` field
- **No** zerops.yaml parser for `httpSupport` intent
- **No** plumbing of `workingDir`/`setup` into `maybeAutoEnableSubdomain`
- **No** intent restamp on import
- **No** `ZeropsYmlPort.HTTPSupport` field extension
- **No** `BenignSkip` status in `ops.Subdomain` (keeps it honest for explicit recovery callers)
- **No** new `Notes` field on `DeployResult`
- **No** classification of `noSubdomainPorts` as benign (already retried with bounded backoff in `enableSubdomainAccessWithRetry`; surfaces as real warning after exhaustion — defense-in-depth via verify Recovery field 537a3190)

The platform's `serviceStackIsNotHttp` response IS the intent signal for "non-HTTP shape". Having multiple sources for the same answer is the variant-bug-vector pattern we're avoiding.

---

## 4. Scenario verification

All four bootstrap routes audited against the new design, no gaps found.

### 4.1 Classic (bootstrap workflow → import → deploy)

```
zerops_workflow workflow=bootstrap → plan → complete=discover → import yaml constructed
zerops_import content=<yaml>      → services created
bootstrap close                   → ServiceMeta written: Mode=PlanModeStandard (or Dev/Simple)
zerops_deploy targetService=appdev → success → maybeAutoEnableSubdomain runs
  → meta.Mode allow-list pass (PlanModeStandard ∈ {dev, standard, stage, simple, local-stage})
  → ops.Subdomain.Enable
    → HTTP service: success, URL set
    → Worker (mode=PlanModeStandard but role=worker, no HTTP port): serviceStackIsNotHttp swallow
zerops_deploy targetService=appstage source=appdev → cross-deploy → maybeAutoEnableSubdomain
  → FindServiceMeta("appstage") returns dev-half meta (pair-keyed) → Mode allow-list pass
  → enable on appstage (separate platform stack from appdev)
zerops_verify → http_root pass
```

Result: **no manual `zerops_subdomain enable` in happy path.** ✓

### 4.2 Recipe-authoring (`zerops_recipe` → workspace yaml → import → deploy)

Recipe v3 engine doesn't write ServiceMeta (confirmed: `grep WriteServiceMeta internal/recipe/` returns zero hits).

```
zerops_recipe action=...        → emits workspace yaml (non-workers carry enableSubdomainAccess: true)
zerops_import content=<yaml>    → services created, NO meta written
zerops_deploy targetService=appdev → success → maybeAutoEnableSubdomain runs
  → FindServiceMeta returns nil
  → predicate: meta == nil, mode check skipped → continue
  → ops.Subdomain.Enable
    → HTTP service (yaml emitter sets enableSubdomainAccess: true, deployed run.ports has httpSupport): success
    → Worker (recipe yaml emitter omits enableSubdomainAccess for workers; deployed run.ports has no httpSupport): serviceStackIsNotHttp swallow
```

Result: **happy path unchanged from user perspective.** ✓

### 4.3 Manual import (`zerops_import` outside any workflow)

```
zerops_import content=<user-crafted yaml>  → services created, NO meta
zerops_deploy targetService=foo            → success → maybeAutoEnableSubdomain runs
  → meta nil → mode check skipped → enable attempt
    → HTTP service: success
    → non-HTTP: serviceStackIsNotHttp swallow
```

Behavior change vs current broken state: meta-less HTTP services now auto-enable. Today they require manual `zerops_subdomain enable`. The change matches what we already do for bootstrap-managed services. ✓

### 4.4 Adopt (`zerops_workflow workflow=bootstrap action=start` on existing services)

`internal/workflow/adopt.go:74`: every adopted runtime gets `BootstrapMode: topology.PlanModeDev` (which is in the allow-list).

```
zerops_workflow workflow=bootstrap action=start → adopt route detected
zerops_workflow action=complete step=adopt      → ServiceMeta written: Mode=PlanModeDev,
                                                  IsExisting=true, FirstDeployedAt stamped
                                                  if platform Status=ACTIVE
zerops_deploy targetService=adopted             → success → maybeAutoEnableSubdomain runs
  → meta.Mode = PlanModeDev → allow-list pass
  → ops.Subdomain.Enable
    → If subdomain was already on: returns AlreadyEnabled, copies cached URLs
    → If subdomain was off but service is HTTP: enables, gets URL
    → If service has no HTTP shape: serviceStackIsNotHttp swallow
```

Result: **adopt path works without changes.** Includes the case where user adopted a service that previously had subdomain off — they now get auto-enable on next deploy. Same trade-off as manual import; symmetric with the bootstrap-managed path. ✓

### 4.5 F8 (dev runtime with `zsc noop` deferred dev-server start)

```
zerops_deploy targetService=appdev → success (noop is a valid start)
maybeAutoEnableSubdomain
  → meta.Mode = PlanModeDev → allow-list pass
  → ops.Subdomain.Enable
    → platform: serviceStackIsNotHttp (no HTTP listener, no HTTP-shaped port)
    → caller: silent skip
  → No warning. No spurious "Service stack is not http or https" leak.

(later) zerops_dev_server start → dev server listens on HTTP port → service becomes HTTP-shaped
(next deploy or explicit zerops_subdomain enable) → enable now succeeds
```

F8 fixed via the same swallow path that handles workers. No special case. ✓

### 4.6 Override re-import

```
zerops_import content=<new-yaml> override=true  → spec replaced; meta untouched (this is fine)
zerops_deploy targetService=foo                 → success → maybeAutoEnableSubdomain runs
  → meta unchanged; Mode still allow-list pass
  → ops.Subdomain.Enable
    → If new spec is HTTP-shaped: enable succeeds (or already_enabled)
    → If new spec deliberately non-HTTP: serviceStackIsNotHttp swallow
```

The b92deb91 finding ("preserve enableSubdomainAccess: true on override re-import") becomes a USER-side concern (their spec choice), not a predicate concern. Auto-enable doesn't depend on the import yaml's `enableSubdomainAccess` flag. ✓

---

## 5. Phase plan

Four phases. Net code change is small (~150 lines, mostly deletions in tests).

### Phase 0 — RED: e2e proof + corrected mocks

- New e2e `TestE2E_StandardPairFirstDeploy_AutoEnablesSubdomain` (e2e build tag): provision standard pair on `eval-zcp`, deploy code with `httpSupport: true` ports, assert `zerops_discover` shows `subdomainEnabled: true` on both `appdev` + `appstage` WITHOUT explicit `zerops_subdomain` call. Cleanup deletes services.
- Update `autoEnableTestMock` helper in `internal/tools/deploy_subdomain_test.go`: default `SubdomainAccess: false`, default port `HTTPSupport: false` (matches real platform pre-enable).
- Tests asserting auto-enable based on `SubdomainAccess: true` at import (lines 145-185, 415-460+) marked `t.Skip("phase-1 RED: predicate rewrite")`.

**Files**: `e2e/`, `internal/tools/deploy_subdomain_test.go`.

**Verification**: e2e RED (assertion fails — current bug); affected unit tests skipped with phase-1 reference; remaining suite green.

---

### Phase 1 — Replace predicate; classify `serviceStackIsNotHttp`

**The whole fix.** Single commit.

- `internal/tools/deploy_subdomain.go`:
  - `serviceEligibleForSubdomain`: collapses to:
    ```go
    func serviceEligibleForSubdomain(
        ctx context.Context,
        client platform.Client,
        meta *workflow.ServiceMeta,
        projectID, targetService string,
    ) bool {
        if meta != nil && meta.Mode != "" && !modeAllowsSubdomain(meta.Mode) {
            return false
        }
        // System-stack defense (kept per §3.2). One ListServices RT.
        svc, err := ops.LookupService(ctx, client, projectID, targetService)
        if err != nil || svc == nil {
            return false
        }
        if svc.IsSystem() {
            return false
        }
        return true
    }
    ```
  - `maybeAutoEnableSubdomain`:
    - Keeps the `LookupService` call (one RT, for `IsSystem()`).
    - Drops the `GetService` call (the broken DTO checks are gone).
    - On `ops.Subdomain.Enable` error, classifies via new helper:
      ```go
      if isServiceStackIsNotHttpErr(err) {
          return  // benign: not HTTP-shaped (worker, F8 deferred-start)
      }
      ```
    - Other errors → `result.Warnings`.
  - Add `isServiceStackIsNotHttpErr(err error) bool` helper (file-local, see §3.4).
  - `httpClient` and `projectID` parameters retained (URL-ready probe still uses them).
  - Top-of-file doc-comment + per-function docs rewritten. Smoking-gun reference inline so a future reader doesn't re-introduce the broken predicate.

- `internal/ops/subdomain.go`: **UNCHANGED.** `ops.Subdomain.Enable` keeps returning `serviceStackIsNotHttp` as a regular error so explicit `zerops_subdomain enable` recovery calls still see it. The downgrade lives only in the caller, where context proves benign.

- `internal/tools/deploy_subdomain_test.go`:
  - Delete tests that asserted the old DTO-driven predicate branches.
  - Update `autoEnableTestMock` defaults (already done in phase 0).
  - New tests (truth table):
    - `TestServiceEligible_NoMeta_NonSystem_True`
    - `TestServiceEligible_NoMeta_SystemStack_False` (defensive guard pinned)
    - `TestServiceEligible_MetaMode_AllowList_NonSystem_True` (cases for dev / standard / stage / simple / local-stage)
    - `TestServiceEligible_LocalOnly_False`
    - `TestServiceEligible_UnknownMode_False`
    - `TestServiceEligible_LookupFails_False` (soft fail)
    - `TestMaybeAutoEnable_Success_SetsURL`
    - `TestMaybeAutoEnable_AlreadyEnabled_SetsURL_NoProbe`
    - `TestMaybeAutoEnable_ServiceStackIsNotHttp_SilentSkip` (replaces F8 + worker concerns)
    - `TestMaybeAutoEnable_OtherError_AddsWarning`
    - `TestMaybeAutoEnable_LocalOnlyMode_NoEnableCall`
    - `TestIsServiceStackIsNotHttpErr_*` (positive + negative classification)

- **Atom test pin update IN THE SAME COMMIT** (Codex review point #4):
  - `internal/content/atoms_test.go::TestDevelopReadyToDeployAtom_ManualSubdomainFallback` (line 74) currently pins the string `zerops_subdomain action="enable"` in `develop-ready-to-deploy.md`. Phase 2 removes that line from the atom; the test must be deleted or rewritten in phase 1's commit so the build doesn't break between phases.

**Files**: `internal/tools/deploy_subdomain.go`, `internal/tools/deploy_subdomain_test.go`, `internal/content/atoms/develop-ready-to-deploy.md` (B12 line removal pre-empted from phase 2 to keep this commit atomic), `internal/content/atoms_test.go`.

**Verification**: e2e from phase 0 passes; new unit suite green; `make lint-fast` + `make lint-local` clean; atom-corpus pin-density holds.

**Codex round?**: **YES — POST-WORK mandatory.** This is the load-bearing change.

---

### Phase 2 — Atom + doc + spec ripple

Note: B12 reactive manual-fallback line in `develop-ready-to-deploy.md` was already removed in phase 1's commit (to keep that commit's atom-pin test consistent). Phase 2 covers everything else.

**Atoms** (`internal/content/atoms/`):
- `develop-first-deploy-execute.md` lines 31-33 — promise about `subdomainAccessEnabled: true` on first-deploy success: KEEP (now actually true). **ADD** a one-line opt-out hint near this section: "If you imported a service that you deliberately want to keep without a public subdomain, call `zerops_subdomain action="disable"` after the deploy."
- `develop-ready-to-deploy.md` (B12 preventive line from `b92deb91`): preventive guidance ("preserve `enableSubdomainAccess: true` on override re-import") becomes USER-intent guidance (so the new spec carries their intent), not predicate-related: REPHRASE. (Reactive line already gone in phase 1.)
- `develop-record-external-deploy.md` line 12: rephrase eligibility clause — "auto-enables when mode is in the allow-list; the platform's `serviceStackIsNotHttp` response signals non-HTTP shape and is treated as benign."
- `develop-verify-matrix.md` (Recovery field from `537a3190`): KEEP — defense-in-depth for genuine post-deploy misses (network glitches, propagation delays unrelated to predicate). **ADD** the same opt-out hint near `develop-verify-matrix.md:13` (Codex round 3 catch — `develop-first-deploy-execute.md` only fires for `deployStates: [never-deployed]`, so adopted ACTIVE services with `Deployed=true` would never see the opt-out hint there). The verify atom fires post-deploy regardless of state.

**Recipe / knowledge content** (Codex round 2 + 3 catches — six files, multiple sections):
- `internal/recipe/content/phase_entry/scaffold.md:185` — currently repeats the broken `detail.SubdomainAccess OR detail.Ports[].HTTPSupport` predicate. REWRITE to mode-allowlist + platform-classifies.
- `internal/recipe/content/workflows/recipe/.../verify-dev.md:17` and `verify-stage.md:7` — currently instruct agent to manually enable. REMOVE manual-enable step from happy path; keep only as recovery if `zerops_verify` reports failure.
- `internal/recipe/content/workflows/recipe/.../deploy-dev.md:39` — currently instructs manual enable post-deploy. REMOVE.
- `internal/content/workflows/recipe.md:1552`, `recipe.md:1624`, `recipe.md:2121`, `recipe.md:3393` — monolithic recipe doc, four sections instructing manual enable (Codex round 3 added :1624 + :3393). REMOVE manual-enable steps; keep only `zerops_verify` reactive guidance.
- `internal/knowledge/themes/core.md:120` — knowledge theme prose currently says always call manual enable after first deploy. REWRITE.
- `e2e/subdomain_test.go:5` — stale top-of-file comment under the new invariant. UPDATE comment.

**Doc-comments** (`internal/tools/deploy_subdomain.go`):
- Top-of-file 13-45 + per-function 121-189: complete rewrite. Remove R-14-1 / "plan-declared intent gate" narrative. Add explicit smoking-gun reference (HttpRouting != httpSupport, link to plan archive) so future readers don't re-introduce.

**Spec** (`docs/spec-workflows.md`):
- §4.8 "Subdomain L7 activation is the deploy handler's concern": rewrite — "after deploy, ops.Subdomain.Enable is called for services in the mode allow-list; platform classifies via response (success / already_enabled / serviceStackIsNotHttp benign skip)."
- O3 invariant: re-state with the corrected source.

**CLAUDE.md**:
- Update "Subdomain L7 activation is the deploy handler's concern" bullet — mention the platform-as-classifier design.

**Files**: 4 atom files, 6 recipe/knowledge content files (`scaffold.md`, `verify-dev.md`, `verify-stage.md`, `deploy-dev.md`, `recipe.md`, `themes/core.md`), 1 e2e comment, `internal/tools/deploy_subdomain.go`, `docs/spec-workflows.md`, `CLAUDE.md`.

**Verification**: full test suite green (`go test ./... -short`); atom-corpus pin-density holds; lint clean.

**Codex round?**: Optional — narrow doc/atom work.

---

### Phase 3 — E2E pojistka + cleanup

- E2E coverage for all five scenarios:
  - Phase 0 e2e (standard pair) — already passes after phase 1.
  - `TestE2E_RecipeAuthoringDeploy_AutoEnablesSubdomain` — recipe-authoring path (no meta), assert subdomain enabled without manual call.
  - `TestE2E_OverrideReimport_AutoEnablesSubdomain` — import → enable → override re-import → re-deploy → subdomain auto-re-enables.
  - Worker scenario split into TWO tests (Codex round 3 — event-log assertion is unreliable when the API rejects pre-process):
    - **Unit**: `TestMaybeAutoEnable_Worker_CallIssued` — mock asserts `EnableSubdomainAccess` invoked exactly once even though it returns `serviceStackIsNotHttp`. Pins the §6.1 design choice (workers eat the wasted RT).
    - **Live E2E**: `TestE2E_WorkerService_BenignSkip` — deploy worker, assert `result.SubdomainAccessEnabled == false`, `result.Warnings` has no subdomain entry, `result.SubdomainURL == ""`. Do NOT assert on `zerops_events` log content (the platform may reject before creating a process event).
  - `TestE2E_AdoptedService_AutoEnablesOnDeploy` — adopt + re-deploy → enable lands.
- Delete `plans/backlog/cross-deploy-stage-subdomain-auto-enable-suspect.md` (resolved).
- `git mv plans/subdomain-auto-enable-foundation-fix-2026-05-03.md plans/archive/`.

**Files**: `e2e/`, `plans/backlog/`, `plans/`.

**Verification**: e2e suite green on real `eval-zcp`; worker BenignSkip e2e asserts only `result.SubdomainAccessEnabled == false` + no subdomain warning entry (the `EnableSubdomainAccess` invocation itself is pinned at the unit level — `zerops_events` may not carry a `stack.enableSubdomainAccess` event when the platform rejects pre-process).

---

## 6. Risks + open questions

### 6.1 Worker first-deploy eats three wasted REST calls

Workers (mode in allow-list, role=worker, no HTTP port) attempt enable on first deploy → platform rejects with `serviceStackIsNotHttp` → silent swallow. The cost is **three round-trips per worker per first deploy**, not one (Codex review correction): `ops.Subdomain.Enable` runs `ListServices` (`subdomain.go:152`) + `GetService` (`subdomain.go:164`) + `EnableSubdomainAccess` (`subdomain.go:184`) on a cold enable path. Three workers in a recipe = nine serialized REST calls after batch completion (`deploy_batch.go:129`).

Acceptable: workers are infrequent (recipes typically have 0-2), this happens once-per-lifetime per worker, and total worst-case latency stays well under one second on top of an already-multi-second deploy. The alternative (re-introducing intent tracking to filter workers pre-enable) re-introduces the variant-bug-vector the design just eliminated. Worker cost is the explicit trade for "no fallbacks" purity.

### 6.2 Manual import / adopt of "internal-only" HTTP service

Edge case: user manually imports/adopts a service that has HTTP shape but they deliberately don't want public subdomain. Today: never auto-enables (broken predicate). New: auto-enables.

Mitigation: explicit opt-out via `zerops_subdomain action=disable` after deploy, surfaced to the agent via opt-out hints added to BOTH `develop-first-deploy-execute.md` (covers fresh deploys, `deployStates: [never-deployed]`) AND `develop-verify-matrix.md` (covers adopted ACTIVE services with `Deployed=true` post-deploy) in phase 2. Same as if user wanted to disable a bootstrap-managed subdomain. This is a 5%-scenario where the 95%-scenario (subdomain wanted on HTTP service) wins.

### 6.3 `serviceStackIsNotHttp` in explicit recovery path

Codex review §4: if a user explicitly calls `zerops_subdomain enable` and gets `serviceStackIsNotHttp`, that's a meaningful diagnostic — they need to know their yaml is missing `httpSupport: true` on the port. We must NOT swallow this in the explicit-recovery codepath.

Resolution: the swallow lives only in `maybeAutoEnableSubdomain` (auto-enable context). `ops.Subdomain.Enable` returns the error normally; explicit `zerops_subdomain enable` callers see it; recovery atom guidance can reference `serviceStackIsNotHttp` as a diagnostic.

### 6.4 Test fixture audit beyond the named files

Phase 1 includes a grep audit of `internal/tools/` for any other test fixtures that pre-set `SubdomainAccess: true` at import. If found, rewrite or delete consistently.

### 6.5 What if `noSubdomainPorts` retry is actually masking another race?

`noSubdomainPorts` retry was added in 82b89639 to absorb L7 port-registration propagation. With the new design, every first deploy of an HTTP service hits this retry — verify retry exhaustion isn't a new failure mode under load. If so, increase backoff or surface as warning explicitly. **Do not classify `noSubdomainPorts` as benign in the caller** (Codex review §2): after retry exhaustion it represents a real failure that the agent / user needs to see; the verify Recovery field (537a3190) catches it as defense-in-depth.

### 6.6 `IsSystem()` defensive check kept

Initial design dropped this guard on the argument that five upstream filters (`discover.go:101`, `route.go:210/238/276`, `compute_envelope.go:186`, `adopt_local.go:75`, `workflow_adopt_local.go:91`) make system stacks unreachable. Codex's third review correctly identified the gap: those filters cover route/envelope/adopt surfaces, NOT explicit `targetService` resolution paths (`discover.go:76`, `deploy_local.go:81`, `deploy_ssh.go:87`, `guard.go:61`) which accept any hostname via `FindService`/`GetService` without category filtering.

Decision: **keep `IsSystem()` as defensive check.** Cost is one `ListServices` RT (~50-100ms) — half of what the old broken predicate consumed. The check defends against future caller paths that may not yet exist and maintains the spec invariant that L7 routing is never auto-enabled on platform-internal stacks. The benefit (provable safety without depending on five upstream filters staying complete forever) outweighs the latency.

---

## 7. Test strategy

| Layer | New | Removed / Updated |
|---|---|---|
| Unit (`deploy_subdomain_test.go`) | 9 truth-table tests for new predicate + 4 enable-result classification tests | Delete: predicate-fallthrough tests assuming `SubdomainAccess: true` at import (~12 tests); Update: `autoEnableTestMock` defaults |
| Unit (`subdomain_test.go` ops side) | `TestIsServiceStackIsNotHttpErr_*` predicate parallel | None |
| Integration | None new | Audit pass for similar fixture issues |
| E2E (`e2e/`) | Standard pair, recipe-authoring, override re-import, worker, adopt — 5 scenarios | None |

---

## 8. What's NOT changing

- `ServiceMeta` schema (no new field)
- Bootstrap path (`bootstrap_outputs.go` writeProvisionMetas etc.)
- Recipe v3 engine
- `internal/tools/import.go`
- Platform DTO mappers
- `ops.Subdomain.Enable` signature or behavior (only adds an error-classification helper next to it)
- `ops.zeropsYmlPort` struct
- `WorkflowInput` schema
- `deploy_batch` ops-layer plumbing
- Verify Recovery field (537a3190 — still useful as defense-in-depth for genuine misses)

---

## 9. Codex review hooks

**Pre-execution review history** — three Codex rounds in session 2026-05-03:

1. **Round 1**: SHIP-WITH-CHANGES on the original Option-B intent-stamp design (6 changes).
2. **Round 2**: SHIP-WITH-CHANGES on the simplified "platform-classifies" design (6 mechanical changes).
3. **Round 3**: SHIP-WITH-CHANGES with 4 residual items: (a) restore `IsSystem()` defensive guard — round 2 dropped it on five-filter argument that didn't cover explicit-hostname `FindService`/`GetService` paths; (b) atom-pin claim confirmed; (c) recipe ripple expanded from 5 to 6 surface files; (d) worker E2E split into unit + live test (event-log assertion was over-specified); (e) opt-out hint added to `develop-verify-matrix.md` IN ADDITION to `develop-first-deploy-execute.md` (the latter only fires for `deployStates: [never-deployed]`, missing adopted ACTIVE services); plus §3.5 explicit no-classification of `noSubdomainPorts`.

All five round-3 items integrated into this version. Round 4 confirmed consistency. **Plan is ready to execute.**

**Post-phase-1 review** (after the predicate rewrite lands):

> Review the predicate rewrite in `internal/tools/deploy_subdomain.go` against plan §5 phase 1. Confirm: (a) predicate matches the truth table in the new tests; (b) `isServiceStackIsNotHttpErr` lives in tools/ and is correctly applied only in `maybeAutoEnableSubdomain` (not in `ops.Subdomain.Enable`); (c) doc-comment smoking-gun reference is clear enough to prevent re-introducing the broken DTO-driven predicate; (d) all four caller sites (deploy_local, deploy_batch, deploy_ssh, workflow_record_deploy) work with the simplified signature; (e) atom-pin test for B12 was deleted/rewritten in the same commit so the build stays atomic.
