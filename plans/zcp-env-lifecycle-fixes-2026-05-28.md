# ZCP env-lifecycle — verified fix plan (2026-05-28)

**Input:** `plans/zcp-env-lifecycle-review-findings-2026-05-28.md` (the review of 23 unpushed env-lifecycle commits).
**This doc:** every finding + every proposed fix **independently re-verified against the actual current code** (not trusting the review). 12/14 confirmed, 3 fix-shapes corrected, 1 partial-locator fixed. Three realization-critical corrections were additionally hand-verified by reading the code (F1 join-point, F3 shared-site, F3 continue-swallow).

**Tooling verified:** `./bin/golangci-lint` v2.8.0 reproduces BOTH lint blockers verbatim (`env.go:380`, `env_effective_test.go:10`); SDK checked in the v1.0.20 module cache.

> Push / release / `make install` are **separate explicit decisions** (CLAUDE.local.md) — NOT part of this plan. Land on a branch, run tests + targeted golangci-lint, then surface for Karel's release call.

---

## Corrections to the review doc (the point of re-verifying)

| Finding | Doc said | Verified reality → fix |
|---|---|---|
| **F1** | "hoist to ONE shared finalizer both paths pass through" | **No join point exists.** `executeLaunchMutation` and `executeExistingProjectMutation` are two independent funcs; each builds its own `state`+`launchBundle` and calls `launchLaunchedResponse(corpus, state)` separately. The renderer takes `state`, never `launchBundle` — it *cannot* be the finalizer. → **one-liner** at the existing path + pin BOTH paths. |
| **F3** | "propagate `RefResolveTransientError` from both consumers (mirror F4 one-liner)" | The preflight consumer **swallows via `continue` at `deploy_preflight.go:228`, not an error-return** — a one-liner can't reach it. AND a **second, pre-existing** error surface (slim `FetchServiceEnv`) mistranslates transients identically; the doc missed it. → wrap at the **single shared site** `ServiceHigherLayers` (`env_effective.go`), which returns raw errors from *both* fetches → unifies F3+F4 and fixes the slim surface for free. |
| **F7** | "derive `Sensitive: ud.Type.String()==\"SECRET\"`; add a mapper test (optional)" | Stringly compare is inferior to **typed `ud.Type == enum.UserDataTypeEnumSecret`**. And the existing green ops/tools redaction tests are an **over-capable-mock seam** (they seed `Sensitive:true` the real client physically can't produce) → they pass regardless of the prod fix, so a **NEW platform-layer mapper test is load-bearing, not optional**. |
| **NIT-2** | "1-line fix at §2 ~line 52" | Locator **wrong** (line 52 is a GUI-category table). Real `MAIL_MAILER`→mailpit misattributions are at `spec-zerops-env-lifecycle.md:69` AND `:72` → **two edits**. |
| F2 | "21 `TestDeployPreFlight_*`" | Actually **26** (21 + 5 GateB). No bearing on the fix. |
| NIT-1 | "remove the `$HOME` path" | Also drops the **stale `@v1.0.17` pin** (go.mod is v1.0.20; the factual claim still holds). |

All other locations were **exact** (line drift ≤ 2 lines everywhere). C1, C2 (cleared) stand.

---

## Shared root cause (one cause behind F1/F3/F4/F7)

The env-lifecycle work added **a new app-version fetch** (`AppVersionEnvVars` → `GetAppVersionUserData`). The happy path is sound, but its **edge surfaces** (transient errors, the existing-project launch path, secret sensitivity) weren't brought to parity with the pre-existing slim path, and the **test seams that would catch it are missing or use over-capable mocks.** Fix root-cause (parity at the shared sites + the seams), not per-symptom.

---

## Fix plan (5 tiers, ordered)

### Tier 1 — CI-unblock: lint blockers  *(~9 lines, no runtime change)*
- **LINT-1** `internal/tools/env.go:380` — add explicit `case ops.EnvLayerProject:` before `default:`, returning the same generic message, with a one-line comment restating spec §2 (project is the lowest layer, can never be the WINNING layer; present only to satisfy `exhaustive`). **KEEP** the `default` branch (`EnvLayer` is `type EnvLayer string` → out-of-enum values representable; default is the defensive fallback). *Reject the `//nolint` option the doc offered — the explicit case keeps exhaustiveness live for a future enum member.*
- **LINT-2** `internal/ops/env_effective_test.go:10` — drop the unused `name` param from `runtimeSvc(id, name, appVersionID)`, hardcode `Name: "app"` inside; update the 5 call sites (L46/51/82/115/146).
- **Tests:** none new — LINT-1 unreachability already pinned by `TestDetectLayeredShadows`; LINT-2 is a helper refactor. Confirm `./bin/golangci-lint run ./internal/tools/ ./internal/ops/` goes green.
- **How it works after:** CI green, unblocking the branch. `formatLayeredShadow` identical at runtime (project was already unreachable as a winner).

### Tier 2 — Root-cause parity: launch warning drop (F1) + transient misclassification (F3+F4)  *(~25 prod + ~120 test lines, medium risk)*
- **F1** `internal/tools/launch_existing.go` — add `state.Warnings = launchBundle.Warnings` in `executeExistingProjectMutation` right after `state.ImportedServices = importedServices` (≈L361), **before** `writeLaunchState`. Mirrors the new-path assignment at `workflow_launch_production.go:673`; persists warnings so both the live `launchLaunchedResponse` (L376) and a later resume (`launchResumeResponse`) surface them. 1 line. *(Optional hardening: extract `finalizeLaunchedState(state, launchBundle)` called by both paths — only if a 3rd mutation path is ever added.)*
- **F3+F4 (one transient-parity change)** `internal/ops/env_effective.go` — in `ServiceHigherLayers`, wrap genuine fetch failures as `&ops.RefResolveTransientError{...}` for **both** the `FetchServiceEnv` and `AppVersionEnvVars` returns (it currently returns both raw). The lifecycle gate guarantees `AppVersionEnvVars` returns `(nil,nil)` for managed + never-deployed, so only a **live** runtime's genuine API failure yields a non-nil err = exactly the transient class → the never-deployed graceful path is untouched. Once the shared site wraps, F4's `env_generate.go:228` (which discards `ybErr`) just propagates correctly, and the pre-existing slim surface is fixed for free.
- **F3 preflight consumer** `internal/tools/deploy_preflight.go:228` — the blanket `if effErr != nil { continue }` must not swallow a transient. Surface it up `preflightEnvRefs`' error return so the deploy handlers (`deploy_local.go` ~L118 / `deploy_ssh.go` ~L178) `errors.As(*ops.RefResolveTransientError)` and emit a **VPN-retry recovery** message (parallel to `ErrRequiresSetupInput` and the generate-dotenv `LocalDotenvVPNDown` contract at `workflow_checks_local_env.go:100`) instead of a hard typo-FAIL. *(Open decision: this matching-contract route vs the smaller-surface "classify at L228 → WARN/statusPass".)*
- **Tests (RED→GREEN):**
  - `TestLaunchExistingProject_LaunchedResponse_SurfacesWarnings` — drive `handleLaunchProduction` (existing-project mock) with a non-empty `launchBundle.Warnings` (promoted-but-unreferenced managed dep / external-secret `REPLACE_ME`); assert rendered `warnings[]` non-empty. **Fails today**, passes after the L361 one-liner.
  - `TestLaunchNewProject_LaunchedResponse_SurfacesWarnings` — locks the new path (L673) which is **also currently untested**.
  - F3: `deploy_preflight_test.go` — live runtime sibling (`ActiveAppVersion.ID` set) + `WithAppVersionUserData`, inject `mock.WithError("GetAppVersionUserData", <transient>)`, target `entry.Run.EnvVariables` with `${sibling_VAR}`; assert NOT statusFail/typo but a transient/VPN signal. Second case injects the **slim** `GetServiceEnv` error. **Trap:** the sibling MUST be in `WithServices` (a liveHostname) or `ValidateEnvReferences` treats the ref as lone and emits nothing.
  - F4: `env_generate_test.go` — live sibling, `${sibling_VAR}` cross-ref, inject the transient; assert `errors.As(*ops.RefResolveTransientError)`, NOT `ErrInvalidParameter` typo-class. Companion test asserting `LocalDotenvVPNDown` pins the downstream contract.
- **Risk:** the shared wrap touches a helper used by env-get / shadow-detect / preflight / generate-dotenv — wider blast radius (see open decision a/b). Verified safe against the never-deployed path. F1 is low-risk (copies an already-computed `[]string`).
- **How it works after:** Launching INTO an existing project now surfaces the same bundle warnings the new-project path already does (live + resume) — the highest-severity advisories stop vanishing. A VPN/API blip while fetching a live sibling's app-version env during deploy-preflight OR `.env` generation no longer masquerades as a "typo/unknown variable" hard FAIL; the agent gets `zcli vpn up`+retry guidance on both paths.

### Tier 3 — Test-hardening: pin already-shipped correct behavior  *(~150 test lines, tests only)*
> **Plan-fidelity note:** F2/F5/F6 production code is already correct + complete. These are **GREEN-only characterization** pins closing test gaps that `plans/zcp-env-appversion-2026-05-28.md:99-103` enumerated but shipped partial (silent scope-cut) — NOT RED→GREEN. Flagged as post-hoc pins.
- **F2** `deploy_preflight_test.go` — 3 cases: managed-typo→FAIL, live-runtime→resolves, never-deployed→WARN-as-pass (detail contains "not yet deployed"). Same liveHostname trap as Tier 2.
- **F5** `env_generate_test.go` — `TestEnvGenerateDotenv_NeverDeployedRuntimeSibling_KeepsLiteralNoError`: ref to a runtime sibling with `ActiveAppVersion==nil` → kept literal in `.env`, no error, not counted unresolved. **Accounting trap:** assert `Variables==1`, and `Services==1` (the sibling IS slim-fetched → lands in `TouchedServiceHostnames`) — do NOT assert `Services==0`.
- **F6** `internal/platform/zerops_mappers_test.go` — add a `Scheme` assertion + a Scheme-bearing fixture row to BOTH `TestMapEsServiceStack_PortMapping` and `TestMapFullServiceStack_PortMapping` (mailpit 2-port shape: 1025 tcp + 8025 http). Pins that the platform layer populates `Port.Scheme` from `output.ServicePort.Scheme` (`zerops_mappers.go:229`) — the single point of failure since the cross-port probe was deleted by `06a82501`.
- **How it works after:** No runtime change — locks the lifecycle partition, the keep-literal branch, and the scheme wire so a future regression is caught by CI. F6 specifically guards the wire that the probe-deletion made load-bearing for subdomain URL build + `verify http_root`.

### Tier 4 — Sensitive-contract: yaml-baked secret redaction reaches production (F7)  *(~10 prod + ~40 test lines, low risk)*
- `internal/platform/zerops_appversion.go` — in `GetAppVersionUserData`'s struct literal (L51-55), add `Sensitive: ud.Type == enum.UserDataTypeEnumSecret` (typed compare; `enum.UserDataTypeEnumSecret = "SECRET"` at `userDataTypeEnum.go:10`; the `AppVersionUserData` DTO has **no** `Sensitive` field, only Key/Content/Type-deprecated). Import `github.com/zeropsio/zerops-go/types/enum`. Extract the `UserDataList→[]ServiceEnvVar` loop into a **pure func** so it's unit-testable without a live `GetAppVersion` handler. Comment that Type is the only sensitivity signal and it's SDK-deprecated; an empty wire `type` → `Sensitive=false` (under-redact = safe direction).
- **Test (NEW, load-bearing):** platform-layer mapper test — `Type=SECRET → Sensitive:true`, `EDITABLE/empty → false`. This is the ONLY test proving prod marks yaml-baked secrets sensitive; the existing ops/tools redaction tests pass regardless of the fix (over-capable mock).
- **How it works after:** A SECRET-typed var baked in a service's `zerops.yaml` `run.envVariables` that wins a cross-layer shadow now renders `<redacted>` in the env-shadow guidance (`env.go:382`), matching the already-working service-layer redaction. The documented `WinningSensitive` contract becomes true in production for **both** layers, not just service. `GetAppVersionUserData` is the single construction site → the fix propagates to shadow-detect AND env-get with no second mapper.
- **Scope (open decision):** real but narrow (yaml-baked vars are overwhelmingly `${ref}` templates, not literal secret material). Cheap + contract is documented → recommend fixing this branch.

### Tier 5 — Hygiene + flag-only  *(~6 lines, no behavior change)*
- **C1** — commit the uncommitted `internal/knowledge/testdata/active_versions.json` refresh (`+mysql:ha@5.7` at L113; valid JSON, content-addressed, idempotent) **separately** as a catalog refresh; do NOT fold into env commits. No test depends on the `mysql:ha`-vs-`single` asymmetry.
- **NIT-1** `internal/tools/deploy_subdomain.go:56-58` — drop the machine-specific `$HOME` path AND the stale `@v1.0.17` pin; replace with a module-relative reference. Factual claim still true in v1.0.20.
- **NIT-2** `docs/spec-zerops-env-lifecycle.md` — **two** edits (L72 and L69): re-attribute `MAIL_MAILER` from mailpit to laravel-showcase (it lives in `laravel-showcase.md`; mailpit has none). Verify which var the live precedence test at L69 actually used before editing.
- **CheckEnvRefs orphan** `internal/ops/checks/env_refs.go:42` — dead-FAIL path (only caller passes `nil liveHostnames`). **Recipe-engine / Aleš scope** → FLAG only, no action, no backlog.

---

## Open decisions (Karel)

1. **F3 surface (a/b):** (a) root-cause wrap at `ServiceHigherLayers` — cleanest, unifies F3+F4, fixes the pre-existing slim surface, but wider blast radius (shared helper) **[recommended]**; vs (b) smaller-surface: classify `effErr` at `deploy_preflight.go:228` → WARN, leave F4 separate. → is the wider blast radius acceptable this branch?
2. **F3 deploy-handler UX:** preflight transient → typed error the deploy handlers `errors.As` → VPN-retry recovery (matches the generate-dotenv `LocalDotenvVPNDown` contract) **[recommended]**, vs a non-blocking WARN/statusPass detail.
3. **F1 extraction:** ship the one-liner + per-path pins **[recommended]**, vs pre-emptively extract `finalizeLaunchedState`. → only worth it if a 3rd mutation path is coming.
4. **F7 in this branch vs backlog:** narrow exploit surface but cheap + documented contract → **recommend fix now**.
5. **C1 timing:** commit the catalog refresh separately now vs stash. Test-safe either way.
6. **TDD framing for F2/F5/F6:** these are GREEN-only post-hoc pins (plan §99-103 silent scope-cut), not RED→GREEN — acknowledged, not hidden.

---

## Invariants held by every tier
- `run.envVariables` canonical — all reads via `entry.Run.EnvVariables` (Tier 2/3 tests use `run:`/`envVariables:` yaml).
- Arch dep rule — Tier 2 stays intra-ops + tools (no new cross-import); Tier 4 is platform-layer only (`enum` is the SDK pkg); no direct `client.ListServices`/`GetServiceEnv` introduced.
- Every behavior change pinned (Tier 2 RED→GREEN; Tier 3 GREEN-only back-fill flagged as such).
- Remove (don't disable) — LINT-2 drops the dead param; no code disabled.
