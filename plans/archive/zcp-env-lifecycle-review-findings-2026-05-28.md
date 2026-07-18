# ZCP env-lifecycle — review of unpushed changes: findings + fix proposal (2026-05-28)

## Purpose / how to use this doc

This is a **handoff for an investigating agent**. It captures a multi-agent review of
the **23 unpushed commits** on `main` (env-lifecycle correctness work) plus a
deterministic build/test/lint baseline. Every confirmed finding was **adversarially
re-verified** against the actual code (a second agent tried to refute it). Two findings
were **refuted** — they are listed under "Cleared" so you do not re-chase them.

For each finding you get: location (`file:line`), the bug chain (evidence), why it is
real (the verification), and a suggested fix. Treat the locations as approximate
line anchors — open the file and confirm before editing (the branch may move).

Raw review data (full agent output, ~400 lines JSON) is at:
`/private/tmp/claude-501/.../tasks/wezpkfbey.output` (ephemeral; this doc is the durable record).

**Authoritative sources to read first** (intent the work was implementing):
- `plans/zcp-env-appversion-2026-05-28.md` — AUTHORITATIVE plan (Phases 0–7). Subsumes the two below.
- `plans/zcp-env-correctness-fixes-2026-05-27.md` — older plan (Phases A–F + defect table).
- `plans/env-shadow-httpport-utility-recipe-2026-05-27.md` — F1/F2/F3 source.
- `CLAUDE.md` — invariants (esp. "run.envVariables canonical", architecture dep rules, "check parallel paths", "every behavior change pinned").

---

## Scope reviewed

Branch `main`, 23 commits ahead of `origin/main` (~2577 insertions / 260 deletions, 64 files).
Dominant theme: **env-lifecycle correctness**. Unifying root cause being fixed: ZCP read
service env only from the slim `service-stack/{id}/env` API (~2% of container env) and was
blind to **yaml-baked `run.envVariables`**. The correct source is the **app-version
userDataList**, wired once into `ops.EffectiveServiceEnv`, then consumed by the slim-blind
sites (env-ref validation, shadow detection, discover/env-get).

**Load-bearing lifecycle constraint** every consumer must honor — app-version userDataList
exists ONLY for a runtime service deployed ≥1×. Three states:
- **Managed dep** (postgres/valkey): no app-version; connection vars are intrinsic, already in slim. Slim authoritative.
- **Runtime never-deployed**: no app-version; only local `zerops.yaml`. MUST NOT hard-fail → WARN / keep-literal.
- **Runtime live**: has app-version; primary path.
Gate: fetch app-version only when `IsManagedService==false && ActiveAppVersion.ID != ""`
(centralized in `ops.AppVersionEnvVars`, complement `ops.IsRuntimeNeverDeployed`).

### Commit → phase map
| Phase | Commits |
|---|---|
| P0 enabler (app-version source, lifecycle gate) | `614f9d97` |
| groundwork spec | `5159e329` |
| P1 env-ref validation A1+A2 | `1379450d` (+ Stage-2: `9c88d413`, `66161a75`, `6d406364`) |
| P2 shadow + dup-key | `e5d6e77a`, `150affdf` |
| P3 discover/env-get yaml-baked | `648cd6ea` |
| P4 production-readiness corpus | `cf5d5d34` (+ Stage-2: `0b9f83fc`, `bf30c3bd`) |
| P5 launch gate PR-4 | `58d719b6`, `f2e21c40` |
| P6 small bugs | `93cc62b7`, `4309cf5b` |
| P7 docs/spec | `df817477` |
| F2 http-port (older plan) | `09ca6a28` (added probe) → `06a82501` (REVERTED to scheme-only) |
| H1 service-env replace→per-var merge | `726867db` |
| F3 mailpit in-repo bundle | `930f2079` |
| e2e+eval | `62c01e17`; backlog | `49176ef3` |

---

## Overall verdict

**Happy-path integration is sound and well-woven across all four layers** (platform → ops →
tools → atoms/docs/tests). The lifecycle gate is centralized (no copy-paste drift), no
slim-blind validator survives on any live deploy path, the H1 mock was made genuinely
faithful, the F2 scheme-only revert is fully clean (zero orphans), atoms are cross-consistent
+ lint-clean, and the specs accurately map the shipped code.

**Not pushable as-is.** Recurring weak spot: the *new* app-version fetch added an edge
surface (transient errors, the existing-project launch path, secret sensitivity) that the
edge paths don't handle as completely as the happy path, and the test seams that would catch
it are missing or use over-capable mocks. Plus 2 CI-blocking lint failures.

---

## Health baseline (deterministic — `make lint-local`)

`go build ./...` ✅ · `go vet ./internal/...` ✅ · `go test ./internal/... -short` ✅ ·
**`make lint-local` ❌ (CI blocker)**:

- **LINT-1 — `internal/tools/env.go:380`** — `exhaustive`: switch on `s.WinningLayer` is
  missing `case ops.EnvLayerProject`. **NOT a runtime bug**: `DetectLayeredShadows`
  (`env_shadow.go:98-112`) only ever sets `WinningLayer` to `EnvLayerYamlBaked` or
  `EnvLayerService` (project is the lowest layer, can't shadow itself), and the `default`
  case (env.go:387) handles it safely. But it fails `make lint-local` + CI.
  Fix: explicit `case ops.EnvLayerProject:` falling to default behavior **with a comment**
  that project is the lowest layer so never wins (documents the invariant), or
  `//nolint:exhaustive` + rationale.
- **LINT-2 — `internal/ops/env_effective_test.go:10`** — `unparam`: test helper
  `runtimeSvc(id, name, appVersionID)` — `name` always receives `"app"`. Cosmetic; drop the
  param (hardcode) or vary it. CI-blocking.

**Note on `active_versions.json` (uncommitted):** `make lint-local` runs `zcp catalog sync`,
which regenerated it. The added `mysql:ha@5.7` line is **legit catalog-sync output** (see
Cleared §1), not a hand-edit. It is sitting uncommitted, unrelated to this changeset.

---

## Confirmed findings (verified real)

### F1 — [HIGH] Launch into an *existing* project drops ALL bundle warnings
**Location:** `internal/tools/launch_existing.go:122-377` (`executeExistingProjectMutation`).
**Chain:** `f2e21c40` fixed warning-surfacing by adding `state.Warnings = launchBundle.Warnings`
ONLY to the new-project path (`workflow_launch_production.go:673`). The parallel
existing-project mutation path builds `launchBundle` (runs PR-4's
`detectUnreferencedManagedDeps`), accumulates `composeWarnings`, external-secret `REPLACE_ME`
advisories (`emitWarnings`, line ~322), and state-write fallbacks onto `launchBundle.Warnings`
— but **never sets `state.Warnings`** (grep over the function returns nothing). It then
returns `launchLaunchedResponse(corpus, state)` (line 376), which reads `state.Warnings`
(`workflow_launch_production.go:1303`) = nil → every warning vanishes.
**Verified:** `git show --stat f2e21c40` touched only `launch_state.go`, `launch_warnings_test.go`,
and the new path in `workflow_launch_production.go`; never `launch_existing.go`. Routing at
`workflow_launch_production.go:~349/397/402` confirms two live parallel mutation paths; one fixed.
**Impact:** `zerops_workflow workflow="launch-production"` into an existing project ships a
promoted-but-unreferenced (unreachable under default `service` isolation) managed db, and
unreplaced external-secret placeholders, **silently** — the exact bug class f2e21c40 set out
to kill, alive on the sibling path. Textbook "check parallel paths" violation (CLAUDE.md).
**Suggested fix (root-cause, preferred):** hoist `state.Warnings = bundle.Warnings` into ONE
shared finalizer both mutation paths pass through right before `writeLaunchState` / the
launched response, so a future third path can't reintroduce the drop. Symptom-parity
alternative = add the one line to the existing path (faster, more fragile). Pair with F1b.

### F1b — [MED] Warnings-surfacing test doesn't pin either mutation path
**Location:** `internal/tools/launch_warnings_test.go:17-42`.
**Chain:** `TestLaunchLaunchedResponse_SurfacesWarnings` hand-builds `launchState` with
`Warnings` pre-populated (lines 19-26) and calls `launchLaunchedResponse` directly — it pins
only the renderer reading `state.Warnings`, never exercises EITHER mutation path populating
`state.Warnings` from `launchBundle.Warnings`. Existing-project tests
(`launch_existing_env_idempotency_test.go`) assert `mutateProjectEnvs` *returns* warnings but
stop before the response. So the mutation→state→response seam — exactly where F1 lives — is
uncrossed by any test, which is why F1 compiles and passes CI.
**Suggested fix:** end-to-end regression pin per mutation path asserting `warnings[]` in the
rendered launched response.

### F2 — [HIGH] A1 deploy-preflight lifecycle partition (managed→FAIL vs never-deployed→WARN) has no test
**Location:** `internal/tools/deploy_preflight.go:243-262` (`preflightEnvRefs`).
**Chain:** the partition `if neverDeployed[e.Host]` → `statusPass` + "not yet deployed; its
run.envVariables can't be confirmed" WARN; else (managed/live) → `statusFail` (blocks deploy).
`neverDeployed` populated via `ops.IsRuntimeNeverDeployed`. This is the load-bearing A1 change
(was a blanket FAIL). Repo-wide grep across `*_test.go` for `preflightEnvRefs` /
`not yet deployed` / `neverDeployed` / `_env_refs` → zero hits exercising this branch. The 21
`TestDeployPreFlight_*` functions only test yaml-location/mount/setup resolution; none
register a sibling service with `ActiveAppVersion` or a `${sibling_VAR}` cross-ref. The pure
primitive `ValidateEnvReferences` IS tested; the caller-side partition is not.
**Verified:** `plans/zcp-env-appversion-2026-05-28.md:102-103` **explicitly enumerated** this
test ("new deploy_preflight_test.go (managed-FAIL / live-runtime-resolves / never-deployed-WARN)").
Literal silent scope-cut vs the authoritative plan. Only coverage is the observation-only eval
`eval/behavioral/scenarios/env-cross-runtime-ref-lifecycle.md`, which per CLAUDE.md is NOT a CI gate.
**Impact:** the false-blocker-fix-but-don't-overcorrect logic (managed typo still FAILs,
never-deployed sibling now WARNs) can silently regress to re-blocking valid first-deploys or
leaking real managed typos. **Suggested fix:** unit test for both branches (this is pure test
addition — locks behavior that already works).

### F3 — [MED] Transient app-version fetch error → false deploy-block (preflight)
**Location:** `internal/tools/deploy_preflight.go:226-235` → `ops/deploy_validate.go:426-433` → `deploy_preflight.go:244-256`.
**Chain:** the loop appends `svc.Name` to `liveHostnames` UNCONDITIONALLY (line 226), then
calls `ops.EffectiveServiceEnv` (line 227); on error it `continue`s (line 229) → for that
sibling neither `discoveredEnvVars[svc.Name]` nor `neverDeployed[svc.Name]` is set.
`ValidateEnvReferences` then sees `knownVars = nil` → emits `EnvRefError{Reason:"unknown
variable"}`; back in preflight `neverDeployed[e.Host]` is false → routes to `failDetails` →
`statusFail` (blocks deploy). Commit `1379450d` replaced the old `FetchServiceEnv` (slim, one
call) with `EffectiveServiceEnv`, which for a LIVE runtime ADDS a `GetAppVersionUserData` API
call with **no transient-retry wrapper** → the pre-existing continue-on-error now also trips
on app-version transient failures (wider failure surface than the old slim-only path).
**Verified:** `AppVersionEnvVars` returns `(nil,nil)` for managed/never-deployed, so a non-nil
err means a live runtime whose actual API call failed (transient class, same as
`*RefResolveTransientError`). The new lifecycle graceful-degradation only covers the
never-deployed state, not the transient-fetch-failure-on-a-live-sibling state.
**Impact:** a VPN/API blip on a valid cross-ref to a live sibling converts a legit deploy into
a hard "fix your yaml" FAIL during a window where retry is correct. Unpinned.

### F4 — [MED] Same transient error misclassified in generate-dotenv (parallel to F3)
**Location:** `internal/ops/env_generate.go:228` (swallow) → `:248-250` (unresolved++) →
`env_plan.go:469-473` → `env_generate.go:337-343`.
**Chain:** `if yb, ybErr := AppVersionEnvVars(ctx, r.client, svc); ybErr == nil && len(yb) > 0`
— a non-nil `ybErr` is silently discarded. The slim fetch 12 lines up (`:216-221`) correctly
returns a `*RefResolveTransientError` on failure. When a live sibling's app-version fetch
fails, the cache holds only slim /env (no yaml-baked), `findEnvValue` misses, and because the
svc is NOT `IsRuntimeNeverDeployed` the miss is counted `unresolved++` → bubbles to
"unresolved ${} refs in:" → `ErrInvalidParameter` "could not resolve env vars … Check that
referenced services are running and have the expected env var keys" (typo class).
**Verified downstream:** `workflow_checks_local_env.go:100-111` branches on
`errors.As(*RefResolveTransientError)` → "run `zcli vpn up` and retry" + retry hint; the plain
"unresolved refs" error does NOT match → falls to generic "could not build env plan". Same root
cause (transient blip), two different failure contracts.
**Impact:** transient blip on `.env` generation fails with typo-class guidance, no VPN-retry
framing. Unpinned. **Suggested fix (F3+F4 together — parallel paths to parity):** treat a
non-nil app-version fetch error like the slim fetch → propagate `RefResolveTransientError`.

### F5 — [MED] A2 generate-dotenv never-deployed keep-literal branch implemented but untested
**Location:** `internal/ops/env_generate.go:240-251`.
**Chain:** `if svc, ok := r.serviceIndex[svcHost]; !ok || !IsRuntimeNeverDeployed(svc) { unresolved++ }`
— a ref to a never-deployed RUNTIME sibling is kept literal AND not counted unresolved (so
`.env` is still written, no hard error). This is the A2 false-block fix. The live-runtime case
is pinned (`TestEnvGenerateDotenv_RuntimeSiblingYamlBaked`), but every referenced sibling in
the test tables is `postgresql@16` (managed) or the live `api` — none creates a referenced
RUNTIME sibling with `ActiveAppVersion==nil` to assert the ref stays literal without error.
**Impact:** companion to F2 — a regression dropping the `IsRuntimeNeverDeployed` guard
re-introduces the false hard-error blocking local `.env` during bootstrap, no failing test.
Both never-deployed lifecycle branches (F2 preflight-WARN, F5 keep-literal) are unpinned.

### F6 — [MED] `Port.Scheme` mapping (the load-bearing wire for the F2 revert) has no test pin
**Location:** `internal/platform/zerops_mappers.go:229` (`Scheme: p.Scheme.String()`).
**Chain:** scheme-based HTTP-port selection — the entire justification for the `06a82501`
revert (which DELETED the cross-port probe safety net) — rests on this one mapper line. But the
mapper tests (`zerops_mappers_test.go`) assert only Port/Protocol/Public/HTTPSupport and NO
input fixture sets `ServicePort.Scheme` (`grep -c Scheme zerops_mappers_test.go` == 0), so the
line is never exercised. The ops tests (`port_select_test.go`) hand-build
`platform.Port{Scheme:"http"}` literals — they pin `PreferredHTTPPort`'s branching but NOT that
the platform layer populates `Scheme` from real API data.
**Impact:** if the mapping regresses (e.g. SDK field rename on a version bump), `Scheme` is `""`
on all real ports, `PreferredHTTPPort` falls through to HTTPSupport/80/first, multi-port
services (mailpit SMTP 1025 + HTTP 8025) re-acquire the original wrong-URL bug, and the suite
stays green. With the probe net removed this is now a single point of failure.
**Suggested fix:** mapper test with an SDK fixture whose `ServicePort.Scheme` is set, asserting
mapped `Port.Scheme`.

### F7 — [MED] Yaml-baked secret redaction is structurally unreachable in production; the mock hides it
**Location:** `internal/platform/zerops_appversion.go:49-56` (real client) vs
`internal/platform/mock_methods.go:95-103` + `internal/tools/env.go:377-383`.
**Chain:** `formatLayeredShadow` redacts `WinningValue` only when `WinningSensitive` is true,
including the `EnvLayerYamlBaked` branch. `WinningSensitive` flows from
`EffectiveEnvVar.Sensitive` (`env_effective.go:120`). But the real
`GetAppVersionUserData` builds `ServiceEnvVar{Key, Content, Type}` and **never sets
`Sensitive`** — the SDK `AppVersionUserData` DTO (zerops-go v1.0.20) has only Key/Content/Type,
no `Sensitive` field (contrast `serviceStackEnv.go` which HAS `Sensitive`). So in production
every yaml-baked var has `Sensitive=false` → a literal secret baked in `run.envVariables`
prints **verbatim** in the shadow warning. The mock returns the seeded slice verbatim,
preserving a `Sensitive:true` the real client physically cannot produce, so
`TestEnvSet_ProjectScope_ShadowedBySensitive_Redacts` (`env_test.go:568`) passes — the mock is
strictly more faithful than the real client.
**Verified:** `enum.UserDataTypeEnumSecret = "SECRET"` exists and the `Type` field IS carried
(`zerops_appversion.go:54`), so this is a **fixable wiring gap, not a hard SDK limit**.
**Impact:** narrow (yaml-baked secrets are usually `${ref}` templates, whose literal form isn't
secret material), but the explicit `WinningSensitive` redaction contract silently doesn't hold
for one of its two layers while tests claim it does. Leak target = agent/transcript/log.
**Suggested fix:** derive `Sensitive: ud.Type.String() == "SECRET"` in `GetAppVersionUserData`;
add a platform-mapper test for Type=SECRET → Sensitive=true.

---

## Low / nit (confirmed; backlog or fold into a cleanup commit)

- **[low] `CheckEnvRefs` is a dead slim-blind orphan** — `internal/ops/checks/env_refs.go:42`.
  No live deploy caller (only an unreachable-path caller at `cmd/zcp/check/env_refs.go:48`
  passing empty `liveHostnames`, so the FAIL branch never fires). It is the one slim-blind ref
  primitive the plan named that wasn't brought to parity → latent reintroduction trap if ever
  wired. **Recipe-engine scope (Aleš)** per `docs/zcprecipator*/` — FLAG, do not act.
- **[nit] Personal `$HOME` path in committed comment** — `internal/tools/deploy_subdomain.go:57`
  hard-codes `/Users/macbook/go/pkg/mod/.../zerops-go@v1.0.17/...` (stale; repo is on v1.0.20,
  claim still true). Remove the machine-specific path, keep the factual claim. Quick clean.
- **[low] Project-path env mock is a no-op** — `internal/platform/mock_methods.go:273-302`
  (`CreateProjectEnv`/`DeleteProjectEnv` don't mutate `m.projectEnv`). Asymmetric with the
  now-faithful service-path mock (H1) → a future project-path data-loss bug would slip past
  discover-after-set integration coverage exactly as the service-path bug did pre-H1. Backlog.
- **[low] PR-4 lacks hyphen-canon + shared-dep dedupe tests** — `internal/ops/bundle/launch_test.go`
  only covers single non-hyphen `db` + single runtime; `detectUnreferencedManagedDeps` does
  `strings.ReplaceAll(hostname,"-","_")` and dedupes shared deps, both untested. Backlog.
- **[low] Spec recipe-name misattribution** — `docs/spec-zerops-env-lifecycle.md:52` (§2) cites
  `MAIL_MAILER: log` as mailpit's; that var lives in `laravel-showcase.md` (mailpit has none).
  Pattern is correct, only the recipe name is wrong. 1-line doc fix.
- **[nit] preflight reads project env via direct `client.GetProjectEnv`** —
  `internal/tools/deploy_preflight.go:213`. NOT an invariant violation (`GetProjectEnv` is not
  in the forbidden set; `TestNoDirectClientCallsInToolsEvalCmd` only forbids
  `ListServices`/`GetServiceEnv`) and is a deliberate N→1 fetch optimization. Defensible as-is.

---

## Cleared — investigated, NOT real (do not re-chase)

### C1 — `active_versions.json` `mysql:ha@5.7` is NOT a hand-edit
Claimed as a stray hand-inserted fixture. **Refuted:** the verifier ran the real
`make catalog-sync` (`go run ./cmd/zcp catalog sync`) → output diff is **byte-identical**
(`+ "mysql:ha@5.7"`, nothing else), idempotent (content-addressed). The live import schema
genuinely has only `mysql:ha@5.7` (no `mysql:single`) — the asymmetry vs other managed types
is schema-driven, not evidence of editing. No committed test depends on it; recipe lint passes
with and without. **Residue = process hygiene only:** an unrelated catalog refresh sitting
uncommitted. Action: commit it on its own (`catalog: refresh active versions`) or stash —
**not** part of the env-lifecycle fixes.

### C2 — `CheckEnvRefs` "not given the lifecycle partition" is NOT a scope-cut
Claimed as a half-done plan item (plan named it as a FAIL-site parity target). **Refuted:** the
cited line is from the SUPERSEDED `zcp-env-correctness-fixes-2026-05-27.md`. The authoritative
`zcp-env-appversion-2026-05-28.md` deliberately re-scoped `CheckEnvRefs` to "caller passes
app-version-enhanced keys (doc + contract)" — NOT a FAIL/WARN partition — and its A1 location
row lists only `deploy_preflight.go` + `deploy_validate.go`. `git diff/log` show `env_refs.go`
was never touched in the branch. The FAIL branch is structurally unreachable on its only caller
(empty `liveHostnames`). It's also Aleš's recipe-engine scope. Shipped state is correct per the
binding plan. (Note: C2 overlaps the F-list's "dead orphan" low item — same file, different
claim; the *cleanup/parity* angle is a low/flag, the *scope-cut* angle is refuted.)

---

## Proposed fix plan (4 commits + 1, in order)

| Tier | Commit | Contents | Size |
|---|---|---|---|
| 1 | lint blockers | LINT-1 (`EnvLayerProject` case) + LINT-2 (unparam) — unblocks CI | ~5 ln |
| 2 | launch warnings | **F1** (hoist warning→state to one shared finalizer) + **F1b** (per-path e2e pin) | ~30 ln |
| 3 | transient parity | **F3 + F4** (propagate `RefResolveTransientError` from both consumers) + pin | ~15 ln |
| 4 | test hardening | **F2** + **F5** + **F6** pins (+ fold the `$HOME`-path nit + spec MAIL_MAILER nit) | ~120 ln tests |
| 5 | sensitive contract | **F7** (derive `Sensitive` from `Type=="SECRET"` + re-pin) | ~40 ln |

**Shared root** of F2/F3/F4/F5/F6/F7: the new app-version fetch added an edge surface that the
edge paths under-handle and the test seams don't cross. Fix root-cause (parity of paths + the
seam), not per-symptom. After Tiers 1–5 the branch is clean (push/release is a SEPARATE,
explicit decision — see CLAUDE.local.md prohibitions; do NOT `make release`/`make install`).

---

## Open decisions (need Karel)

1. **F1 fix shape:** hoist `bundle.Warnings → state.Warnings` to ONE shared finalizer both
   mutation paths pass (robust against a future 3rd path — recommended) vs symptom-parity one
   line into the existing path (faster, fragile). → recommend **hoist**.
2. **F7 (sensitive):** fix now (small, well-understood — `Type` already carried) vs backlog
   (exposure narrow). → recommend **now**.
3. **`active_versions.json`:** separate `catalog: refresh` commit (recommended) vs stash.
4. **`CheckEnvRefs` orphan + `ig_one_mechanism.md`:** Aleš's recipe-engine scope → FLAG, do not act.

---

## Invariants any fix must keep (from CLAUDE.md)
- `run.envVariables` is the canonical setup-entry env location; never read top-level
  `entry.EnvVariables` (use `entry.Run.EnvVariables`).
- Architecture dep rule: `ops/` and `workflow/` are peers (no mutual import); `platform/`
  imports no internal pkgs; `tools/eval` reach platform via `ops` (no direct
  `client.ListServices`/`GetServiceEnv`).
- Every behavior change pinned by a test; no silent scope cuts; remove (don't disable) dead code.
- TDD RED→GREEN; verify four layers (unit/tool/integration/e2e) where the change reaches them.
