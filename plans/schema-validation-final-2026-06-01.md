# Schema validation — final architecture (short-TTL live pre-check + structure-only export + offline build)

**Date:** 2026-06-01
**Status:** proposed (final design — pending Codex + agent-team review)
**Supersedes:** `schema-single-source-*` and `schema-validation-grounded-*` drafts (earlier floor+overlay / delete-fetch designs, overturned in discussion + a live platform probe).
**Scope owner:** krls2020. Recipe-scope touches greenlit by Karel **conditional on tests-green / no-break** (recipe unit + integration + flow-eval).
**Effort:** ~1.5 days; net negative LOC.

---

## 1. The principle (one paragraph)

The **Zerops platform is the single live authority** for real operations: a deploy is validated live by `client.ValidateZeropsYaml`, an import by the platform import API. Everything ZCP does client-side is a **pre-check** — early, friendlier feedback whose backstop is always the platform. The schema endpoint (`api.app-prg1.zerops.io/.../settings/...`) shares the platform's availability: if it is down, **nothing works** (no deploy, no import, no discover), so there is **no "schema-offline" state worth building fallbacks for**. From this: pre-checks that judge *authored* values must be **live** (short-TTL fetch); checks on *platform-sourced* values validate structure only and never re-litigate what the platform owns; the committed embedded schema exists for **build determinism + tests + cold-start bootstrap**, not as an outage fallback.

---

## 2. What's broken today (verified, incl. live platform probe)

1. **Export/launch false-rejects live types (bug shipping NOW).** The export gate validates the generated `import.yml` against an embedded schema whose `services[].type` is a **frozen 333-value enum** (`import_yml_schema.json:1370`). The types come from a **live `Discover` of the user's running services** — already valid on the platform — yet the frozen enum rejects any newer than the snapshot. **12 live types already missing**, incl. user-exportable `meilisearch:single@1.44` and the `zero` runtime → real users hit a **false `validation-failed`**. The platform re-validates at re-import anyway, so the enum is a **redundant** second gate that only produces false-negatives.
2. **The recipe live-fetch is barely "live" and unsafe.** `schema.Cache` TTL is **24h** (too stale to be live), trusts a poisoned `HTTP 200 {error:502}` body (empty enums → can flip every base to "invalid" or skip), and returns **nil on failure** → recipe field/base checks **silently skip**.
3. **Three mirrors drift; build hits the network.** Embedded (`testdata/*.json`, 2026-05-19), `active_versions.json` (2026-05-29), and the live fetch are refreshed independently and have measurably diverged; `make lint-local` runs `catalog-sync` which hits the live API. No CI drift sentinel exists.
4. **Dead code:** `format.go` (`FormatBothForLLM` + siblings) has no runtime caller (purpose removed in `641d7958`).

Live-probe corrections that killed the earlier designs: `StackTypeCache` (#3) does **not** carry build bases (only `build_runtime`); its types are **composite** (`ubuntu/nodejs@22`); it cannot replace the public schema for build-base/field-name validation. So "delete the live fetch / promote #3" was unsound — the live public schema is kept, just made properly live.

---

## 3. Final design — runtime

| Surface | Source | Freshness | Why |
|---|---|---|---|
| **Deploy / import** | **platform API** (live authority) | live | unchanged; the real gate |
| **Recipe pre-checks** (LLM-authored types/bases + field names) | **live schema fetch, SHORT TTL (5–60 min)**, poison-guarded, embedded-seeded | fresh ≤TTL | authored values can be wrong → must be checked live so a brand-new base isn't false-rejected and a hallucination is caught early |
| **Export / launch** | **structure-only compiled schema** (embedded, value-enums stripped) | structure pinned | types come from live `Discover` (running services) → definitionally valid → don't re-litigate; platform re-validates at import; preserves the pure-composition (no-I/O) property |
| **Build / CI / tests** | **embedded** (`go:embed`, committed) | build-time | deterministic, reproducible, offline |

### 3.1 The schema cache (recipe source) — short TTL + poison guard + embedded seed
`schema.Cache` stays a **threaded dependency** (constructed in `server.go`, no global mutable state). Changes to `cache.go`, with a **precise state machine** (per review):
- **TTL `24h → 15 min`** (§9 — decided; both reviews concur).
- **Embedded seed:** `NewCache` parses the already-`go:embed`'d bytes (`validate_jsonschema.go:16-20`) into `c.schemas` via `sync.Once`, **with `fetchedAt = zero`** — so the seed is the value-to-return-on-failure, NOT a "fresh" entry that suppresses the first fetch.
- **First `Get` fetches synchronously** (the existing bounded 10s, coalesced single-flight): seed is returned only if that fetch fails/poisons. This keeps the FIRST recipe pre-check on a fresh process actually live, not stale-seed.
- **Poison guard at the source:** `FetchSchemas` returns an **error** when the parsed body has empty enums (`len(BuildBases)==0 || len(ServiceTypes)==0` — the `{error:502}`-in-200 case) **before** the locked write (`cache.go:71`). On error, `Get` falls through to last-good (`cache.go:82-88`) — never overwrites good data with garbage. (Today empty enums only print to stderr and return success — `schema.go:61,92`.)

State machine: `seed(embedded, fetchedAt=0) → first Get: synchronous fetch → good ⇒ replace + fetchedAt=now; poison/error ⇒ keep last-good (seed on first failure), fetchedAt stays 0 so the next Get retries`. Net: `Get(ctx)` is never nil, live on first use, live-fresh ≤15 min thereafter, poison-immune. The recipe consumers (`workflow_recipe.go:93`, `workflow_checks_recipe.go:44`) need **no code change** — they stop receiving nil, so their checks stop silently skipping.

### 3.2 Export / launch — validate against a STRUCTURE-ONLY compiled schema
**Mechanism (revised after review — NOT an error-path filter).** Both Codex and the agent-team flagged that filtering jsonschema errors is fragile: `build.base` is `oneOf{string-enum, array-of-enum}`, so a new **string** base emits BOTH `"value must be one of …"` AND `"expected array, but got string"` at `/zerops/N/build/base` (path-only would drop a legit type error; message-only ("must be one of") leaves the "expected array" noise → the new string base **still rejects**), and a new **array** item emits the enum error at `/zerops/N/build/base/1` (a sub-path path-filtering misses).

Instead: at first use (`sync.Once`), derive a **structure-only compiled schema** from the embedded bytes by stripping the value-membership enums at exactly three nodes while **preserving the type contract**:
- `properties.services.items.properties.type` → drop `enum`, keep `type:string` (conditional `allOf[].if` discriminators stay — they don't fire for an unknown type)
- `properties.zerops.items.properties.build.properties.base` → drop the enum in BOTH `oneOf` branches, keep `oneOf{type:string, type:array items:{type:string}}`
- `properties.zerops.items.properties.run.properties.base` → drop `enum`, keep `type:string`

Export/launch (`bundle/export.go:45-46`, `bundle/launch.go:173`) validate against THIS schema. It keeps every structural guard (`additionalProperties:false` field-typo catch, `required`, the four stable enums `corePackage`/`location`/`objectStoragePolicy`/`cpuMode`, and the base string-or-array type contract) while never re-litigating a service type / base value that came from a live `Discover`. Robust to the `oneOf` error tree and to vendor message-wording changes (no string-matching). **Fixes the live bug** (a `meilisearch:single@1.44` export stops false-failing) and makes export immune to schema staleness. Pin: accepts unknown type + new string/array base; still rejects a typo'd field, a bad `objectStoragePolicy`, and a non-string base.

### 3.3 Recipe — keep `ValidateRecipePlan` nil-tolerant; fix never-nil at the cache only
**Revised after review.** The plan does NOT gut the `validateBuildBases` `liveTypes` fallback: `ValidateRecipePlan(plan, liveTypes, nil)` is called directly by unit tests (`recipe_validate_test.go:509,548`), and `validateTargets` / `validateManagedVersionLatest` **intentionally** skip when `schemas==nil`. So the function STAYS nil-tolerant. The never-nil guarantee is delivered solely by the **seeded cache** (§3.1) in production — the recipe handlers stop receiving nil, so their checks stop silently skipping, without touching the validator's defensive branches. Optional hygiene only: simplify the now-redundant outer `if schemaCache != nil` guard at the two call sites.

---

## 4. Why it is robust (the model, stress-walked)

| Condition | Behaviour |
|---|---|
| Platform up (the only state that matters) | recipe pre-checks live-fresh (≤TTL); export accepts running types; deploy/import authoritative |
| New platform type/version/base | deploy/import/export work **immediately**; recipe picks it up within ≤TTL — **no ZCP release** |
| Platform / schema endpoint down | fetch fails → cache returns embedded seed; but nothing deploys anyway → harmless, no useless fallback logic |
| Poison `HTTP 200 {error:502}` | rejected by the guard; last-good retained; can't poison validation or write a poisoned catalog |
| Airgapped / offline build | `go:embed` only; build deterministic + reproducible + offline |
| Two parallel dev sessions | `make schema-sync` is content-addressed/sorted → byte-identical → clean merge |

**A ZCP release is needed only for actual code/structure changes — never merely because the platform schema moved.**

---

## 5. Build / dev / ops

- **Build/CI:** `go:embed` a committed schema → deterministic, reproducible, **offline**. The live fetch is **runtime-only**; the build never touches the schema endpoint. Build-time staleness of the embedded copy does not affect runtime freshness (the short-TTL fetch refreshes within minutes; an old binary still pulls the current schema at runtime).
- **Decouple `lint-local` from the network:** drop `catalog-sync` from the `lint-local` dependency (`Makefile:60`) so full lint is offline.
- **`make schema-sync`** (the one refresh command): fetch live → **reject-on-empty-enum** → write embedded `testdata/*.json` → derive `active_versions.json` from the same fetch (one pass, kills the #2↔#4 drift). **Keep `make catalog-sync` / `zcp catalog sync` as aliases** (other references depend on them — review flagged). Harden `fetchURL` to reject an error-shaped/empty body.
- **CI drift sentinel:** a scheduled-daily + PR workflow runs `zcp schema check` (fetch-vs-committed); **advisory on PR** (endpoint flakes — 502-in-200 observed), **required on cron**; remediation = `make schema-sync`. Replace the toothless `TestEmbeddedSchemasMatchTestdata` (only `len()!=0`) with a real offline self-consistency test.

---

## 6. Migration — phases (each compiles + green)

1. **Export/launch structure-only compiled schema** (§3.2 — the revised mechanism, not an error filter). Build it in `internal/schema/`, wire into `bundle/export.go:45-46` + `bundle/launch.go:173`. Pin: accepts unknown type + new string/array base; rejects typo'd field + bad `objectStoragePolicy` + non-string base. **Independent of the cache work — ships FIRST and fixes the live bug.** Scope: mine.
2. **Delete `format.go`** + its 2 tests. Pure subtraction.
3. **Cache: 15-min TTL + embedded seed (fetchedAt=0) + synchronous-first-fetch + poison guard at `FetchSchemas`** (§3.1). RED first: poison→last-good (not overwritten), cold-start→first synchronous fetch (live) else seed (never nil), good fetch→live. Scope: `internal/schema/` (mine). **Blocks on nothing; Phase 1 does not depend on it.**
4. **Recipe no-break verify:** `ValidateRecipePlan` stays nil-tolerant (tests rely on it — do NOT gut the fallback); run recipe unit + integration + a recipe flow-eval to prove the now-always-firing checks don't break (seed = committed schema → findings are correct, not false). Optional hygiene: simplify the redundant `if schemaCache != nil` guards. Scope: recipe (greenlit, gated on flow-eval).
5. **Recipe v3 authoring gate** (`internal/recipe/validators_zerops_yaml_schema.go:53,79`): this gate validates LLM-authored YAML (CAN hallucinate, NOT platform-sourced) against the embedded schema → it would false-reject a brand-new base in authoring. Route it through the live short-TTL cache (fresh enums), OR explicitly defer as Aleš-scope. **Flag — do not silently leave on the frozen embedded.**
6. **`make schema-sync` (+ catalog-sync alias) + `schema check` + catalog-as-projection + decouple `lint-local` + `fetchURL` hardening.** Pin: `active_versions == projection of embedded`.
7. **CI drift sentinel + invariant pin + CLAUDE.md bullet.** `git mv` plan → archive.

---

## 7. Residual risks (honest)

- After the structure-only schema, a genuinely-invalid service type/base in an export passes ZCP and fails only at platform re-import (farther from the edit). Acceptable — export types come from live Discover (no typo path), and export already defers `buildFromGit` path + field-values to re-import (DM-4). (The structure-only schema still catches a malformed non-string base + every field-typo / required / stable-enum error.)
- Short TTL = more fetches; each is coalesced + bounded + poison-guarded, and recipe/export aren't latency-critical (deploy uses the platform API directly). MCP server is long-lived → cache warm.
- Recipe pre-check is non-deterministic across the TTL boundary (a brand-new base flips from reject→accept once refreshed). It is a pre-check; the platform is the authority; acceptable.
- A new **structural** field still needs a release (the composer must learn to emit it) — appropriate.

---

## 8. What we explicitly are NOT doing (and why)

| Rejected | Why |
|---|---|
| Delete the live fetch / embedded-only | recipe needs live build-base + field-name freshness; embedded-only forces a release for a new base |
| Promote `StackTypeCache` (#3) to primary | live probe: #3 lacks build bases; composite-form types; cannot supply field names |
| Floor + overlay / UNION / never-nil-for-offline | "schema down = platform down = nothing works" → no outage state to design for; short TTL + embedded seed is enough |
| Edit the committed embedded JSON to strip enums | breaks the next `make schema-sync` (would re-add them) + risks the conditional `allOf` discriminators; the in-memory structure-only derive (§3.2) is deterministic and self-healing |
| Error post-filter (path / message) for export | fragile vs the `build.base` `oneOf` error tree (both reviewers) — drops a legit type error or leaves "expected array" noise that still rejects a new string base; the structure-only compiled schema avoids it entirely |

---

## 9. Decisions (settled in review)

- **TTL = 15 min** (both Codex + agent-team concur). Short enough that a brand-new platform base stops false-rejecting within ≤15 min; every fetch is single-flight-coalesced + bounded + poison-guarded, so the higher frequency vs 1h is negligible and off any latency-critical path (deploy/import use the platform API directly). One constant at `cache.go:13`.
- **Reviews verdict:** Codex = *go after revising Phase 1 to the structure-only schema*; agent-team = *GO-WITH-REVISIONS, Phase 1 ready first*. Both revisions are now folded in (§3.1, §3.2, §3.3, §6). Phase 1 ships first and independently fixes the live bug.

---

## 10. Implementation status (2026-06-02)

Phases **1, 2, 3, 6, 7 implemented + green** (30/30 pkgs short, 10/10 race, lint-fast 0). Tooling verified live: `schema check` detected drift→exit 2, `schema sync` wrote embedded+catalog, post-sync `check` OK. Post-implementation Codex review: *faithful with caveats*; the `zcp catalog sync` CLI was a remaining independent-fetch path — **fixed** (CLI now delegates to `schema sync`; the standalone `catalog.Sync` orchestrator was deleted as dead).

**Phase 5 — IMPLEMENTED as variant (b), live-cache (Karel-authorized 2026-06-02):**
The recipe v3 authoring gate (`gateZeropsYamlSchema`) now validates in two parts:
structure via `ValidateZeropsYAMLStructure` (field-misplacement, the gate's real
purpose) + base existence via `schema.CheckZeropsBasesLive` against the LIVE
short-TTL `Schemas` threaded through `Store → Session → GateContext`. The live
cache is wired into `recipe.Store.SetSchemaProvider` from `server.go` (the
server's `schemaCache`); the sim/tests fall back to `schema.Embedded()` (no
regression). Result: a brand-new platform base is no longer false-rejected in
recipe authoring, while a hallucinated base still blocks. Pinned by
`TestGateZeropsYamlSchema_LiveBaseFreshness`, `TestCheckZeropsBasesLive`,
existing `TestGateZeropsYamlSchema_*` (field-misplacement unchanged). All recipe
+ server + schema tests green (incl. race); `make lint-local` clean.

**One explicit deferral (NOT a silent skip):**
2. **The embedded↔active_versions reconcile + the `active_versions == projection of embedded` pin**: deferred. Running `make schema-sync` refreshes the embedded copy to current (measured drift: 348 embedded-projected vs 240 committed active values), but that **reformats + refreshes the committed testdata, which breaks `recipe_validate` tests pinned to old schema content** (`php@8.4` as a build base, etc. — Aleš-adjacent). The pin can only land AFTER that reconcile + those test updates. The structure-only export validator (Phase 1) makes export immune to the staleness regardless, so this is a hygiene follow-up, not a correctness blocker.

**Not yet done (final step):** plan archival (`git mv` → `plans/archive/`) — pending the two deferrals above being resolved or accepted.
