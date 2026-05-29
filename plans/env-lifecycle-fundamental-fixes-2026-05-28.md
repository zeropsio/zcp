# ZCP env-lifecycle — FUNDAMENTAL fix plan with a structural completeness guarantee (2026-05-28)

**What this is.** The root-cause remediation for the env-lifecycle subsystem, synthesized from every finding gathered this session: the env-lifecycle review (F1–F7), my independent verification (12/14 confirmed, 3 fix-shapes corrected), the verified merge, and the Codex adversarial round (E1–E7 + 3 deepest root causes). Organized **by root cause**, not by symptom. Every claim a fix depends on was verified against current code; line numbers are anchors — re-confirm before editing.

**Why it exists.** Karel's documented pain: plans ship partial (setup-name P0-P12 shipped 7/12, invisible until manual audit). This plan is built so **partial shipping is mechanically impossible** — see Part 0 §B. "Tests green" is not "plan complete"; the fidelity ledger + the RED-first anti-fragmentation pins are the separate completeness gate.

**Provenance:** 8-agent census/design/architect workflow (60 consumer sites enumerated) on top of the prior verification passes. **Do NOT `make release` / `make install`** — separate explicit Karel decisions (CLAUDE.local.md).

**Sibling docs:** `plans/zcp-env-lifecycle-review-findings-2026-05-28.md` (raw findings), `plans/zcp-env-lifecycle-fixes-2026-05-28.md` (my first fix plan — **superseded by this on F4: Tier 2's shared-wrap is wrong, see E1**), `plans/workflow-family-remediation-verified-2026-05-28.md` (Track B + the meta-pattern).

---

## Part 0 — The disease and the cure

### A. The meta-pattern (one cause, many faces)

> **A canonical foundation gets built (`EffectiveServiceEnv`/`ServiceHigherLayers`/`AppVersionEnvVars`; `ResolveCanonicalSetup`; `ServiceMeta` canonical fields) but consumers are NOT routed through it and the old fragmented paths are NOT deleted — because the last "collapse consumers + delete fallback" phase is the hardest and gets deferred-as-safety-net.**

In env-lifecycle it compounds three ways:

| RC | Root cause | Gathers | The "looks-functional-but-not-real" symptom |
|---|---|---|---|
| **RC1** | App-version userData abstraction is too **raw** — the mapper returns the SDK superset (`run.envVariables` ∪ ~119 intrinsics ∪ `ZEROPS_YAML`) with no classification and no `Sensitive` (the DTO has none) | F7, E5, E6, E7 | A baked SECRET prints verbatim in shadow warnings; the redaction signal is **fabricated only in the mock**; intrinsics/`ZEROPS_YAML` are labeled `source=zerops.yaml` |
| **RC2** | Env-layer fetch lacks **error semantics** — absent / empty / unavailable collapse into nil/empty, then code `continue`s | F3, F4, E1, E2, E4 | A transient VPN/API blip becomes a confident-wrong **typo-FAIL** / false "env values are live" / "unresolved refs → fix your yaml" |
| **RC3** | Env resolution is **fragmented** — 4 read-for-validation consumers hand-assemble layers with divergent error handling; `ServiceHigherLayers` is the choke point for only 2 of them | the fragmentation behind RC2 | `env_generate.go:228` bypasses `ServiceHigherLayers` entirely → **my first plan's shared-wrap fix could never reach it (E1)** |

Track B (workflow-family: setup-name resolvers, ServiceMeta writers) is the **identical disease**, owner-gated, cross-referenced in Part 5.

### B. The completeness contract — five interlocking structural guarantees

Designed so a partial ship fails CI, not review.

1. **RED-FIRST anti-fragmentation pin per fundamental phase.** Each phase lands its pin **failing**, in the same commit that introduces the new canonical API, **before** any consumer is migrated. The centerpiece `TestEnvLayerUnavailability_NeverProducesFalseFail` is one table over all **four** class-A consumers, each injecting `mock.WithError` on the relevant fetch and asserting the outcome is transient/WARN/unverified — **never** `statusFail`/"unknown variable"/"env values are live". It is RED today (every site collapses) and cannot go green until **every** consumer branches on typed availability. **"Phase done" ≡ "pin green"**, not reviewer judgment.
2. **Exhaustive checklist.** Every consumer site (from the 60-site census) is an explicit row with a concrete `doneWhen` (a named test or a grep that must return zero). **CLASS B** (read-before-write mutation) and **CLASS C** (single-key intrinsic reads) and already-canonical sites are listed as explicit **NO-MIGRATION** rows so a "route everything" pass can't wrongly funnel them.
3. **Four-condition exit gate per phase:** (a) new behavior tests green + (b) anti-fragmentation pin green + (c) deletion-grep returns zero (the collapse idioms are *gone*, not flagged) + (d) every checklist row marked done-or-deferred-with-a-Karel-question.
4. **Final completeness audit (Phase A-AUDIT).** Re-runs the census programmatically: `client.GetAppVersionUserData` has exactly ONE caller; `client.GetServiceEnv`/`GetProjectEnv` appear in `tools/` only via ops/inventory helpers; no collapse idiom survives in the four files. Fails the build on any surviving bypass. This is the structural backstop against the P0-P12 failure mode.
5. **Fidelity ledger** (Part 3) — the implementer fills one row per finding + per Track-A consumer. A deferred row MUST carry the explicit question raised to Karel.

Each new scanner ships its **own RED self-test fixture** (mirroring the existing `TestNoDirectClientCallsScanner_FiresOnFixture`) so a broken scanner is itself caught.

---

## Part 1 — The three new abstractions (API surfaces)

### RC1 — classified app-version userData domain (platform→ops boundary)

- **Where:** `internal/platform/zerops_appversion.go` (Layer 1, the single SDK-decode owner; uses `enum.UserDataTypeEnumSecret`, a 3rd-party pkg — allowed).
- **New:** pure func `classifyAppVersionUserData(key, typeStr string) (kind, sensitive)`, `kind ∈ {RunEnvVariable, Intrinsic, ZeropsYaml}`. `GetAppVersionUserData` maps each `UserDataList` record, returns **only** `RunEnvVariable`-kind, `Sensitive := Type==SECRET`. Signature unchanged. `ops.AppVersionEnvVars` (its sole caller, `env_effective.go:84`) inherits the narrowing — the structural choke point.
- **✅ LINCHPIN — RESOLVED 2026-05-28 (live-verified): use the Type-allowlist (keep `ENV`+`SECRET`; drop `READ_ONLY`/`INTERNAL`/`EDITABLE` as intrinsic).** A throwaway probe against the live `zcp@1` app-version `UserDataList` (eval-zcp) returned 17 intrinsic records, **all `READ_ONLY`/`INTERNAL`/`EDITABLE`, zero `ENV`** (`zeropsSubdomain`/`hostname`/`projectId`=READ_ONLY; `ZEROPS_RUN_*`/`workingDir`/`initCommands`=INTERNAL; `PATH`=EDITABLE; **`ZEROPS_YAML`=INTERNAL**) — this **refutes RC3's "intrinsics can be `ENV`"** claim. The `ENV` side is confirmed `[LIVE 05-28]` in `spec-zerops-env-lifecycle.md:41-46` (`FOO=fromyaml`, `DBREF=${db_hostname}` observed as `ENV`-type yaml-baked records). So `Type` DOES discriminate: run.envVariables → `ENV` (+ `SECRET` for envSecrets/dotEnvSecrets); intrinsics → `READ_ONLY`/`INTERNAL`/`EDITABLE`. The allowlist excludes all observed intrinsics AND captures genuine run vars; its only failure mode is under-inclusion (a real run var typed non-`ENV/SECRET` → false "unknown variable", **not a leak**) — the safe direction, and no platform mechanism produces such a record. `ZEROPS_YAML` is `INTERNAL` so the allowlist already excludes it; the literal-key drop is **optional defense** (cheap insurance against a future re-typing), not required. Fail-safe: empty/unknown Type → `Sensitive=false` AND non-run-var. **Evidence boundary:** the intrinsic side is independently probe-confirmed today; the `ENV` side rests on spec-`[LIVE]` + the platform model (no app with user run.envVariables was live in eval-zcp to re-observe directly) — deploy a one-var service if a fully-independent `ENV` observation is wanted before P-RC1.
- **Type fork (open decision #2):** reuse `platform.ServiceEnvVar{Key,Content,Type,Sensitive}` (architect's choice — no new type, filter at boundary) vs a dedicated `platform.AppVersionUserRecord{...,Kind}` (RC3 designer's choice — run-var-only contract type-enforced). Recommend **reuse ServiceEnvVar** unless the Kind needs to survive past the ops gate.

### RC2 — typed per-layer availability (`internal/ops/env_effective.go`)

```go
type LayerAvailability int
const ( LayerPresent LayerAvailability = iota // fetched OK (may be known-empty)
        LayerAbsent      // legit no layer: managed dep / never-deployed runtime
        LayerUnavailable // transient fetch failure — DO NOT treat as empty )
type LayerState struct { Availability LayerAvailability; Cause error }
```
`EffectiveEnv` grows `ProjectState/ServiceState/YamlBakedState LayerState` (no field removed; the `[]EffectiveEnvVar` slices stay, state annotates them) + `func (e *EffectiveEnv) Confirmable() bool` (true iff no consulted layer is Unavailable) — the single predicate consumers branch on instead of `err != nil`. `EffectiveServiceEnv`/`ServiceHigherLayers` stop folding a layer-fetch error into a bare return; they return the struct with the failing layer marked `Unavailable{Cause}`. `(nil, err)` reserved **only** for precondition errors (e.g. nil client). The existing `ops.RefResolveTransientError` (`env_plan.go:258`, consumed at `workflow_checks_local_env.go:100`) **is** the Unavailable case for the generate-dotenv resolver family — no new type there.

### RC3 — one lifecycle-aware resolver, all class-A consumers routed through it

`internal/ops/env_effective.go` becomes the **sole** assembly path. Consumers call `EffectiveServiceEnv`/`ServiceHigherLayers` (+ optional `ResolveServiceRefUniverse(...) (knownKeys []string, avail)` for what preflight/generate-dotenv actually need) and branch on availability. **Three site-classes — only CLASS A migrates:**
- **CLASS A (migrate):** preflight, generate-dotenv ref-resolver, discover `attachEnvs`, env-set shadow scan.
- **CLASS B (NO-MIGRATION, allowlist by function):** `ops/env.go:117/153/226/263/288` read-before-write ID lookups — hard-fail on read error is *correct*.
- **CLASS C (route slim read through `ops.FetchServiceEnv` only, not the resolver):** `discover.go:408`, `subdomain.go:258` single-key intrinsic reads.
- **generate-dotenv impedance (E1):** `env_generate.go` builds a flat `map[string][]ServiceEnvVar` cache, NOT the `EffectiveEnv` shape, and calls `AppVersionEnvVars` **directly** at `:228`. Fix the slim read at `:216` through the canonical service-layer fetch, but **keep** the direct `AppVersionEnvVars` enrich at `:228` and wrap *its* error as `*RefResolveTransientError` there (NOT folded into `ServiceHigherLayers`). Preserve the flat-cache shape.
- **Open decision #3:** `ops.FetchServiceEnv` (`lookup.go:36`) vs `ops/inventory.FetchServiceEnvs` (`envs.go:39`) — two canonical-named wrappers over `client.GetServiceEnv`. Pick ONE (or document the split) before pinning the ops-scoped lint, else "single canonical slim read" is unenforceable.

All new types stay in `ops`/`platform` (in-memory, never serialized → no `.zcp/state` migration). The AST lint lives in `internal/topology/architecture_call_discipline_test.go` (existing call-discipline home; stdlib-only test scanning sibling dirs — the established pattern).

---

## Part 2 — Phases

Order: **P0 first** (push-unblock + regression floor) → **P-RC1** → **P-RC2-RC3** → **P-E3**; **P-E7-SPEC** and **A-AUDIT** ride their dependencies. RC1 before RC2-RC3 because its classification narrows `AppVersionEnvVars` (which RC3's discover labels + preflight `eff.Keys()` consume) and its `Sensitive`-from-Type is the redaction signal RC2's shadow path needs. RC2+RC3 are **one phase** — separating them creates a half-migrated window where the resolver exists but consumers still collapse.

### P0-PRECURSOR — push-blocker cleanup (NOT fundamental) · deps: none
Unblocks the 23-commit branch; F2/F5 GREEN pins are a regression floor for functions the fundamental phases reshape.

| Site | Action | doneWhen |
|---|---|---|
| `launch_existing.go:362` (+`:307`) | Add `state.Warnings = launchBundle.Warnings` before `writeLaunchState` (mirror new-path `workflow_launch_production.go:673`). Verified: `executeExistingProjectMutation` never sets `state.Warnings`; advisories appended at `:202/:308/:322/:363` are dropped (F1) | `TestExecuteExistingProjectMutation_SurfacesBundleWarnings` green |
| `deploy_preflight.go:243-262` | GREEN-only `TestPreflightEnvRefs_Partition_*` pinning **lifecycle-only** behavior: managed-miss→FAIL, never-deployed-miss→WARN/pass, mixed→FAIL wins. **Do NOT pin the transient-unavailable case — RC2 changes it** (F2) | 3 cases green vs current code |
| `env_generate.go:240-251` | GREEN-only `TestEnvGenerateDotenv_NeverDeployedRuntimeSibling_RefKeptLiteralNotUnresolved` (F5) | green vs current code |
| `zerops_mappers.go:229` | GREEN-only `TestMapServicePorts_Scheme` (http/https/tcp passthrough) — single point of failure since probe deletion `06a82501` (F6) | green; Scheme mapped per protocol |

**Exit:** (a) all four green; (b)(c) N/A; (d) rows marked. **Must land before P-RC1** reshapes `preflightEnvRefs`/`expandRefs`/`AppVersionEnvVars`. **BC:** internal-only; launch warnings appearing is strictly additive.

### P-RC1 — classify userData at the mapper boundary · deps: P0 · covers F7, E5, E6
**Pins (RED-first):** (1) `TestGetAppVersionUserData_ClassifiesAndDerivesSensitive` (ENV+SECRET run vars kept, READ_ONLY intrinsic + `ZEROPS_YAML` dropped, Sensitive only for SECRET); (2) `TestAppVersionEnvVars_NoFabricatedSensitive` **(load-bearing)** + a parity sub-test feeding identical raw `{Key,Content,Type}` to prod classifier and mock — they MUST agree; (3) flip `env_test.go:571` seed `Sensitive:true`→`Type:"SECRET"`, still assert no leak; (4) AST grep guard: FAIL on any `Sensitive: true` literal on a `WithAppVersionUserData` arg.

| Site | Action | doneWhen |
|---|---|---|
| `zerops_appversion.go:39-58` `GetAppVersionUserData` | Add classifier; return only `RunEnvVariable` w/ derived `Sensitive`. No DTO change | pin (1) green; no consumer re-derives Sensitive |
| `env_effective.go:77-85` `AppVersionEnvVars` | No call-shape change (inherits narrowing); rewrite docstring (drop "intrinsics + ZEROPS_YAML"); keep §1 gate | `TestAppVersionEnvVars_FiltersIntrinsicAndZeropsYaml` green; managed/never-deployed cases stay green |
| `mock_methods.go:95-103` + `mock.go:131` | Mock runs the SAME classifier (or stores SDK-shaped records); fix never-deployed docstring (gated *before* the mock) | pin (2) parity green; no `WithAppVersionUserData` caller passes Sensitive |
| `discover.go:362-371` `attachEnvs` | Inherits narrowing → intrinsics/`ZEROPS_YAML` stop being `source=zerops.yaml` | discover_test: seeded intrinsic+`ZEROPS_YAML` not in `Services[].Envs` source=zerops.yaml |
| `deploy_preflight.go:227-237` via `eff.Keys()` **(easy-to-miss transitive)** | Inherits narrowing | preflight test: `${app_ZEROPS_YAML}` ref FAILs env-ref validation |
| `env_generate.go:228` direct `AppVersionEnvVars` **(easy-to-miss, E1)** | Inherits narrowing (RC2 fixes the swallow) | env_generate_test: ref to a sibling intrinsic key doesn't resolve from yaml-baked cache |
| `env_effective.go:106-154` `ServiceHigherLayers`/`EffectiveServiceEnv` | `Sensitive` propagation at `:120` becomes correct | env_effective_test: SECRET yaml var → YamlBaked `EffectiveEnvVar{Sensitive:true}` |
| 6 `WithAppVersionUserData` callers (`env_test.go:571`, `env_effective_test.go:60/61/80/111`, `env_generate_test.go:534`, `discover_test.go:421/459`) | Convert SECRET intent `Sensitive:true`→`Type:"SECRET"`; recount before edit | pin (4) grep guard green; pin (3) green |

**Deletions:** unfiltered verbatim map (`zerops_appversion.go:49-56`); "plus intrinsic vars + ZEROPS_YAML" docstrings (`env_effective.go:64-66`, `discover.go:337-344`); mock "unseeded→never-deployed" framing (`mock.go:127-130`); `Sensitive:true` seed (`env_test.go:571`).
**Exit:** (a) classify+filter+redaction green; (b) pins (2)+(4) green; (c) zero `Sensitive: true` on userData seeds, zero stale docstrings; (d) 8 rows marked. **E7 is NOT here** (different data path — rides P-E7-SPEC). **Risk MED:** classification direction (open #1); `Type` SDK-Deprecated (open: accept "Type vanishing → no-yaml-baked-layer" fail-safe?); mock de-fabrication touches 6 seeds (pin (4) catches a miss). **BC:** no on-disk change; `Type`/`Sensitive` pre-exist; the mock-shape seam is locked one-way by pins (2)+(4).
**How it works after:** secrets redact using the same signal the real client produces; intrinsics/`ZEROPS_YAML` stop masquerading as run.envVariables; the mock can no longer fabricate an impossible Sensitive.

### P-RC2-RC3 — one resolver, typed availability, all four class-A consumers routed · deps: P-RC1 · covers F3, F4, E1, E2, E4
**Pins (RED-first):** **CENTERPIECE** `TestEnvLayerUnavailability_NeverProducesFalseFail` — table over (a) preflight project-env `WithError(GetProjectEnv)`+`${api_SHARED_KEY}` → NOT statusFail; (b) preflight sibling-env `WithError(GetServiceEnv)`+`${api_KEY}` → NOT statusFail + **never-deployed-under-WithError companion** still WARN (F3+F2 coupling guard); (c) generate-dotenv `WithError(GetAppVersionUserData)`+`${app_API_URL}` → `errors.As(*RefResolveTransientError)`, NOT "unresolved refs"; (d) env-set shadow `WithError(GetServiceEnv)` → NextActions NOT "env values are live". PLUS the **ops-scoped AST scanner** (forbids direct `GetServiceEnv`/`GetAppVersionUserData`/`AppVersionEnvVars` outside `env_effective.go` + the env.go CLASS-B allowlist; forbids the collapse idioms in the four files); PLUS `GetProjectEnv` added to the existing tools/eval/cmd scanner (matches by method name → catches `target.GetProjectEnv` at `launch_existing.go:437`); PLUS **E1 anti-unify guard (C)** (assert `env_generate.go` STILL calls `AppVersionEnvVars` directly AND wraps its error).

| Site | Action | doneWhen |
|---|---|---|
| `env_effective.go` core | Add `LayerAvailability`/`LayerState` + `EffectiveEnv` state fields + `Confirmable()`; return Unavailable-marked layers instead of `(nil,err)` for layer failures (precondition errors still `(nil,err)`) | `TestEffectiveServiceEnv_TypedAvailability_*` green |
| `deploy_preflight.go:213-216` (project, E2) | Route via `inventory.FetchProjectEnvs`; on Unavailable emit ONE `_env_refs StepCheck{statusPass, "project env unavailable — refs unverified; retry after zcli vpn up"}` + early return. DELETE sentinel-empty | centerpiece (a); grep: no `= []platform.ProjectEnvVar{}` |
| `deploy_preflight.go:225-235` (sibling, F3+F2) | Branch on availability; set `unconfirmable` map **before** any early-out (the `continue` also skips `neverDeployed` set at `:232-234`); route unconfirmable → WARN bucket at partition. DELETE bare continue | centerpiece (b)+companion; grep: no `if effErr != nil { continue }` |
| all project-env reads: `deploy_preflight.go:213`, `workflow_export_probe.go:207`, `launch_existing.go:437`, `env_plan.go:362`, `discover.go:324`, `env_effective.go:137` | Canonicalize to `inventory.FetchProjectEnvs`. Preserve per-site behavior (export hard-fail; launch graceful-warn; env_plan hard-fail; discover warn) | GetProjectEnv pin green; existing export/launch/discover/env_plan tests stay green |
| `env_generate.go:216`+`:228` (F4/E1) | Route slim `:216` through canonical fetch; KEEP direct `AppVersionEnvVars` at `:228` but wrap `ybErr` as `*RefResolveTransientError` + return; DELETE `ybErr == nil` swallow; preserve flat cache | centerpiece (c); guard (C); grep: no `ybErr == nil` |
| `env.go:360-363` `detectSetShadows` + `:329` | On Unavailable record "shadow unverified for <svc>" + gate the `:329` "env values are live" → "live where verified; N unverified". DELETE bare continue | centerpiece (d); grep: no `if err != nil { continue }` in detectSetShadows |
| `discover.go:346`+`:362` `attachEnvs` | Derive slim+yaml-baked from canonical assembly; keep warn-on-error UX; adopt `YamlBakedState` (Unavailable≠Absent); don't regress Warnings; don't hard-fail | discover_test: warning still fires AND yaml-baked marked unavailable; ops lint clears attachEnvs |
| `discover.go:408`, `subdomain.go:258` (CLASS C) | Route single-key slim read through `ops.FetchServiceEnv` (NOT the resolver) | no behavior change; ExtractSubdomainURL test green; lint allows FetchServiceEnv |
| `ops/env.go:117/153/226/263/288` (CLASS B) | **NO-MIGRATION** — allowlist by function in the ops lint; hard-fail correct | `TestEnvSet_*`/`TestEnvDelete_*` green; documented CLASS B |
| `workflow_checks.go:119`, `import.go:148`, `workflow_launch_production.go:177` | **NO-MIGRATION** — confirm already-canonical | existing tests green; listed already-canonical |

**Deletions:** the four collapse idioms above; the hand-rolled parallel slim+yaml-baked assembly in `discover.go`/`env_generate.go` (slim reads routed through canonical; `:228` enrich kept per E1).
**Exit:** (a) four centerpiece rows + typed-availability tests green; (b) all anti-fragmentation pins + self-test fixtures green; (c) deletion-grep zero across the four files; (d) every row marked done-or-NO-MIGRATION. **Deliberately-preserved asymmetry:** `env_plan.go:362` hard-fails GetProjectEnv while preflight WARNs — **correct-by-context** (generate writes a file → refuse; preflight only validates → must not block). A reviewer must NOT unify preflight to hard-fail.
**Risk HIGH** (most surface): F3+F2 coupling (set unconfirmable before early-out); E1 anti-unify (don't fold `:228` into `ServiceHigherLayers`); discover already soft-visible (annotate, don't hard-fail); `:329` string and `:361` swallow move together; `(nil,err)`→struct-with-Unavailable touches every caller's error handling (keep precondition errors as `(nil,err)`); resolve the FetchServiceEnv naming (open #3) before pinning the lint. **BC:** in-memory only; preflight/generate/env-set get LOOSER on transients (safe); `checksAllPassed` blocks only on `statusFail` so statusPass-transient needs no new constant; deploy-handler `errors.As` wiring untouched.
**How it works after:** every read-for-validation consumer branches on typed availability — a transient read surfaces as WARN/skip/unverified, never a typo-FAIL or false "live"; slim+yaml-baked assembly lives in one place; the AST lint structurally forbids any new bypass.

### P-E3 — overlay candidate target's own local run.envVariables in preflight · deps: P-RC2-RC3 · covers E3
**Pin (RED-first):** `TestPreflightEnvRefs_SelfIntroducedVar_DoesNotFail` (live target whose prior app-version lacks FOO; `entry.Run.EnvVariables` introduces FOO + a `${app_FOO}` ref → statusPass, not "unknown variable FOO").

| Site | Action | doneWhen |
|---|---|---|
| `deploy_preflight.go:225-237` (before `ValidateEnvReferences`) | Union the target's own `entry.Run.EnvVariables` keys (read `entry.Run.EnvVariables` — canonical-location invariant) into `discoveredEnvVars[targetHostname]` (union, not replace; target host only — siblings validate against platform) | the pin green; existing preflight tests stay green |

**Exit:** (a) the pin green; (b) the behavior test IS the pin (absence → no AST guard); (c)(d) N/A/marked. **Owner-relevant:** changes self-deploy semantics (a self-introduced var no longer false-FAILs) — confirm with Karel before landing (open #5). **BC:** internal; looser/correctness-restoring.

### P-E7-SPEC — launch-existing project-scope Sensitive (OWNER DECISION) · deps: P-RC1 · covers E7
Platform does **not** persist project-level sensitivity (`spec-zerops-env-lifecycle.md:153`), but `launch_existing_test.go:352` asserts `Sensitive:true` was sent → test proves intent, not reality.

| Site | Action | doneWhen |
|---|---|---|
| `launch_existing.go` `applyClassificationToProjectEnvs` + `launch_existing_test.go:367-368/:380-381` | **OWNER DECISION (a/b):** (a) keep sending `Sensitive=true` as best-effort intent + change the test to assert INTENT-only with a `spec §153` comment; OR (b) stop sending on project-scope + warn that project envs can't carry true secrets. **STOP and ask Karel** | test no longer asserts unpersisted Sensitive as persisted |

**Risk:** scope-creep — do NOT fold into the RC1 classifier (different data path: `SecretClassification`, not userData). Can land before/after the RC phases.

### A-AUDIT — final completeness audit · deps: P-RC2-RC3 · covers all
| Item | Action | doneWhen |
|---|---|---|
| `architecture_call_discipline_test.go` | Confirm all 5 audit assertions green + the centerpiece table fully green: `GetAppVersionUserData` exactly ONE caller; `GetServiceEnv`/`GetProjectEnv` in `tools/` only via helpers; no direct `GetServiceEnv`/`GetAppVersionUserData`/`AppVersionEnvVars` outside `env_effective.go`+allowlist; no collapse idiom in the four files; no `Sensitive:true` on a `WithAppVersionUserData` arg. Each ships a self-test fixture | every assertion green; CI fails on any future re-introduction |
| Fidelity ledger (Part 3) | Walk EACH finding + each Track-A consumer; fill the table. Tests-green ≠ plan-complete | every row done-or-deferred-with-Karel-question |

---

## Part 3 — Fidelity ledger (the implementer fills this)

One row per finding (F1–F7, E1–E7, RC1–RC3) + per Track-A consumer site.

| Finding | Site (file:line) | Phase | Anti-fragmentation pin | Behavior test | DeletionGrep (idiom → expect 0) | Status (done/deferred/skipped/NO-MIGRATION) | KarelDecision (a/b or blank) | Verified-at-audit (line) |
|---|---|---|---|---|---|---|---|---|
| _… one row per item; a `deferred` row MUST carry the explicit question raised to Karel …_ | | | | | | | | |

---

## Part 4 — Open decisions (Karel)

1. ~~**RC1 classification direction (LINCHPIN — blocks P-RC1).**~~ **✅ RESOLVED 2026-05-28 (live-verified): Type-allowlist (keep ENV+SECRET).** Live probe of `zcp@1` userDataList: 17 intrinsics, all READ_ONLY/INTERNAL/EDITABLE, zero ENV (`ZEROPS_YAML`=INTERNAL) — refutes "intrinsics can be ENV"; `ENV`=run.envVariables confirmed `[LIVE]` in spec §1. `ZEROPS_YAML` literal-drop downgraded to optional defense. P-RC1 unblocked. (Evidence boundary: ENV side rests on spec-[LIVE]+model, not today's probe — deploy a one-var service for a fully-independent ENV observation if wanted.)
2. **RC1 type fork.** Reuse `platform.ServiceEnvVar` (recommended) vs new `AppVersionUserRecord{Kind}`.
3. **`Type` is SDK-Deprecated.** Accept fail-safe "Type vanishing → degrade to no-yaml-baked-layer" vs need a new signal then.
4. **FetchServiceEnv naming (blocks the ops lint).** `ops.FetchServiceEnv` vs `ops/inventory.FetchServiceEnvs` — pick one canonical slim read or document the split.
5. **E3 self-overlay** changes self-deploy preflight semantics — confirm before P-E3.
6. **E7 (a/b)** — keep-intent+document vs stop-sending+warn.
7. **Track B** (Part 5) — spin into its own plan now (with its two AST guards) or defer.
8. **Push timing.** P0 push-before (get the branch out) vs hold the branch until the RC phases land. Recommend: **P0 + push; RC phases as a separate sequenced refactor track** (do not rush them to unblock the push).

---

## Part 5 — Track B (workflow-family) — same disease, OWNER-GATED, cross-reference only

Identical meta-pattern; **nothing here is zero-gate** (every change touches a deploy/launch/export gate outcome or atom selection). Not proposed for change in this plan — listed so the whole picture is visible. Full detail: `plans/workflow-family-remediation-verified-2026-05-28.md`.

- **setup-name:** `ResolveCanonicalSetup` exists but 5+ resolvers bypass it (`resolveSetupEntry` `deploy_preflight.go:183`; `resolveLaunchSetupName` `launch_promotables.go:152`; `launchTargetSetupName` `workflow_launch_production.go:893`; `pickSetupName` `workflow_export_probe.go:71`; `legacyDefaultSetupName` `ops/bundle/launch.go:77`; bare `setupName==""→hostname` at `deploy_git_push.go:318`/`deploy_local_git.go:89`). **Note:** OMITTING the setup arg is **unsafe** (`deploy_local.go:127` defaults empty→hostname → breaks recipe yamls) — must fall back to recipe convention, not omit. Pin: AST guard "resolution only via `ResolveCanonicalSetup`/`PickSetupNameFromNames`".
- **ServiceMeta:** no `NewServiceMeta` constructor; 5 divergent literal writers (`adopt_local.go:119/:153` omit GitPushState+BuildIntegration); `compute_envelope.go` normalizes only CloseDeployMode (`:231`) while copying the other two raw (`:234-235`) → empty GitPushState silently fails to match an atom filtering on `unconfigured` (`synthesize.go:378`). Pin: AST guard "no fresh `&ServiceMeta{}` literal outside `NewServiceMeta`".
- **Scope boundary:** `ops/bundle/launch.go` is the LAUNCH composer (Track B, owner-gated), **NOT** Aleš recipe scope. `workflow_recipe.go` / `workflow_checks_recipe.go` / `internal/recipe/` / recipe atoms / `deploy_preflight.go:191-198` RecipeSetup resolution = **Aleš — flag only, never touch**.

---

## Part 6 — Invariants (every fix holds these)

- `run.envVariables` canonical — read `entry.Run.EnvVariables`, never top-level `entry.EnvVariables`.
- Arch dep rule: `ops`/`workflow` peers (no mutual import); `platform` imports no internal pkgs (SDK `enum` OK); `tools`/`eval` reach platform via `ops`. New types stay in `ops`/`platform` (in-memory) — no `topology` promotion needed (not peer-shared vocabulary).
- Every behavior change pinned (RED→GREEN for fundamentals; P0 F2/F5/F6 are GREEN-only back-fill, flagged as such). Remove (don't disable) dead code.
- No `.zcp/state`/State-Version change in this plan → no migration. (Track B's ServiceMeta normalization WOULD touch the on-disk seam → needs a one-way idempotent tested migration when it's done.)
- **Never `make release` / `make install`** — separate explicit Karel decisions.

---

## Part 7 — IMPLEMENTATION STATUS + FIDELITY LEDGER (as of 2026-05-29)

Branch `env-lifecycle-fundamental-fixes`. Commits (newest first): `ad6571a6` E3 ·
`5b3d1042` RC2-RC3b · `1bbc766b` RC2-RC3 core · `01b04b87` RC1 · `c6e719cd` C1 ·
`5abe1931` P0. Full repo `go test ./... -short`: 31 packages green; lint 0 issues.

| Finding | Phase | Pin test | Status |
|---|---|---|---|
| F1 launch-existing drops warnings | P0 | TestLaunchExistingProject_SurfacesBundleWarnings | ✅ done (RED→GREEN) |
| F2 preflight lifecycle partition | P0 | TestPreflightEnvRefs_Partition | ✅ done (GREEN-only back-fill) |
| F5 generate-dotenv keep-literal | P0 | TestEnvGenerateDotenv_NeverDeployedRuntimeSibling_… | ✅ done (GREEN-only) |
| F6 Port.Scheme mapper | P0 | TestMap{Es,Full}ServiceStack_PortMapping (Scheme) | ✅ done (GREEN-only) |
| LINT-1 exhaustive / LINT-2 unparam / goconst | P0 | exhaustive/unparam/goconst | ✅ done |
| F7 yaml-baked secret redaction unreachable | RC1 | TestClassifyAppVersionUserData + redaction real-path | ✅ done |
| E5 mock fabricates Sensitive | RC1 | TestGetAppVersionUserData_MockFiltersAndDerives | ✅ done |
| E6 userData superset mislabeled | RC1 | discover E6 filtering + classifier table | ✅ done |
| F3 preflight sibling transient → FAIL | RC2-RC3 | TestPreflightEnvRefs_LayerUnavailable (sibling) | ✅ done (RED→GREEN) |
| F4 generate-dotenv transient swallow | RC2-RC3 | TestEnvGenerateDotenv_AppVersionTransient_… | ✅ done (RED→GREEN) |
| E1 generate bypasses ServiceHigherLayers | RC2-RC3 | kept direct + wrapped; AST single-caller guard | ✅ done |
| E2 preflight project collapse | RC2-RC3 | TestPreflightEnvRefs_LayerUnavailable (project) | ✅ done (RED→GREEN) |
| E4 shadow swallow → false "live" | RC2-RC3 | TestEnvSet_ProjectScope_ShadowScanUnavailable_… | ✅ done (RED→GREEN) |
| RC1/RC3 enforcement (anti-fragmentation) | RC2-RC3b | TestGetAppVersionUserData_SingleCanonicalCaller; GetProjectEnv in scanner | ✅ done |
| E3 preflight self-introduced var | E3 | TestPreflightEnvRefs_SelfIntroducedVar_DoesNotFail | ✅ done (RED→GREEN; owner-relevant, isolated commit) |
| **E7 launch project-scope Sensitive** | P-E7-SPEC | — | ⏸ **DEFERRED — blocked on Karel a/b** (keep-intent+doc vs stop-sending+warn) |
| discover typed YamlBakedState | RC2-RC3b | — | ⏸ **DEFERRED — display-richness, NOT correctness** (discover already warns-on-error, never collapses to false-"live", reads via gated ops.AppVersionEnvVars) |

**No silent scope-cuts:** the two ⏸ items are explicitly deferred with reasons — E7 is a genuine owner a/b fork; discover-state is output-richness, not a correctness gap.

**Live verification (Karel's mandate):** RC1 linchpin live-verified 2026-05-28 (real
`zcp@1` userDataList probe). Targeted env e2e against eval-zcp (LaunchBaseline
DTO Type/Sensitive, env_generate ProjectOnly, Discover_MetaFields) + flow-eval are
the remaining live proof — in progress.

**Codex verification:** RC1 reviewed SAFE (2 LOW fixes folded); RC2-RC3 design
pressure-tested before implementation (struct returns, LayerUnknown zero,
lifecycle-based Absent/Unavailable, ReadComplete, typed ProjectEnvLayer — all adopted).
