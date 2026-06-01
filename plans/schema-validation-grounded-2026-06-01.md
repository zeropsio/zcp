# Schema validation — grounded fix (export enum-strip + #1 discipline + hygiene)

**Date:** 2026-06-01
**Status:** proposed
**Supersedes:** the floor+overlay draft (`schema-single-source-2026-06-01.md`) — overturned by a live platform-API probe; see §3.
**Scope owner:** krls2020. Recipe-scope touches (Fix B consumers) greenlit by Karel **conditional on tests-green / no-break** (recipe unit + integration + flow-eval).
**Effort:** ~1.5–2 days; net negative LOC (delete `format.go` + dead guards; small additions).

---

## 1. What this fixes (one paragraph)

ZCP's export/launch publish gate validates a generated `import.yml` against an **embedded JSON schema whose `services[].type` is a frozen 333-value enum**. The service types in that bundle come from a **live `Discover` of the user's running services** — so they are definitionally valid — yet the frozen enum re-litigates them and **rejects any type newer than the last embedded snapshot**. This is a **live bug today**: 12 active platform types are already missing from the embedded enum, including user-exportable `meilisearch:single@1.44` and the `zero` runtime → a real user exporting such a project hits a **false `validation-failed` right now**. Separately, the live public-schema fetch (`schema.Cache`) that feeds recipe validation has two latent bugs: it trusts a poisoned `{"error":{"code":"502"}}` body (empty enums) and returns `nil` on failure (recipe checks silently no-op). The fix is three independent, mostly-subtractive changes — no new runtime machinery.

---

## 2. Verified validation map (live-probed ground truth)

| Surface | Real source (from the function body) | Phase | Hard/Advisory |
|---|---|---|---|
| Deploy field-level | **platform** `client.ValidateZeropsYaml` (no fallback) | deploy | hard |
| Deploy structure | parsed yaml: DM-2, reserved env names, env-ref preflight (live API) — **no schema** | deploy | hard / advisory |
| Import | **platform** validates fields/types/modes | import | hard |
| **Export/launch bundle** | **embedded jsonschema** (333-type enum + base enums) | export/launch | hard → `validation-failed` |
| Recipe research (types/bases) | **live `schema.Cache` #1** PRIMARY → StackTypeCache fallback | recipe (Aleš) | hard (skips when both nil) |
| Recipe field-names | **live `schema.Cache` #1** `ExtractValidFields` | recipe (Aleš) | hard (skips when nil) |
| Bootstrap types | embedded modes + StackTypeCache via `TypesAreEquivalent` | bootstrap | hard |

Sources, distinct:
- **#1 `schema.Cache`** (public schema fetch, live, 24h): the ONLY carrier of **build/run-base names** + **zerops.yaml field names**.
- **#2 embedded** (`testdata/*.json`, committed): structure + the same enums, frozen.
- **#3 `StackTypeCache`** (`ListServiceStackTypes`, public/global, 1h): service **types + versions only**, in **composite** form (`ubuntu/nodejs@22`).
- **#4 `active_versions.json`** (committed): merged enum list (types + bases), test-only today.

---

## 3. What the live probe overturned (why the earlier draft was wrong)

A probe of `ListServiceStackTypes` against the live platform (eval-zcp) killed the previous "delete #1 / promote #3 / floor+overlay" design:

| Earlier assumption | Live-verified reality |
|---|---|
| #3 carries build bases → can replace #1 | **REFUTED.** BUILD category has only `alpine/build_runtime` + `ubuntu/build_runtime`. No `go@1.25`/`php@8.4` anywhere. The `recipe_validate.go:113` fallback only "works" against in-repo test fixtures (bare form); against the live API (composite `alpine/php-nginx@8.4`) it finds nothing. |
| #3 covers types → naive set-membership | Types exist but in **composite** form; matching needs `TypesAreEquivalent`, not raw set lookup. |
| Delete #1 (live fetch) | **UNSOUND.** #1 is the ONLY source of build-base names + field names. Deleting it regresses `validateBuildBases` + `CheckZeropsYmlFields` to permissive no-ops. |
| Build a floor+overlay for the public schema | **Unnecessary.** Export types are live-sourced (Discover) → don't re-enum-check them; recipe already has #1 live for freshness. No overlay needed. |

**Conclusion:** keep #1 (it uniquely provides what recipe needs), fix its discipline; and fix export by *removing* a redundant gate, not adding machinery. The export problem and the recipe-freshness problem are **separate**.

---

## 4. The grounded design — three independent fixes

### Fix A — Export/launch: strip the volatile enums from the embedded schema (the real bug)
The embedded schema's load-bearing guards are `additionalProperties:false` (catches field-name typos — the single most valuable structural check), `required:[hostname,type]`, and four **small, stable** enums (`corePackage[2]`, `location[2]`, `objectStoragePolicy[6]`, `verticalAutoscaling.cpuMode[2]`) that DON'T drift with platform releases. Only `services[].type` (import) and `build.base`/`run.base` (zerops) drift on every runtime/version addition.

**Change:** in the committed embedded schemas, turn the volatile enums into plain `{"type":"string"}` — `services.items.properties.type` (`import_yml_schema.json:1370`), `run.base` (`zerops_yml_schema.json:610`), `build.base` (`zerops_yml_schema.json:22` oneOf). Leave the conditional `allOf[].if` discriminators intact (an unknown type just matches no `if`, applies no `then` — passes structure). Leave the 4 stable enums + `additionalProperties` + `required`.

**Why safe:** export/launch types come from live `Discover` (running services) → definitionally valid; the **platform re-validates at re-import** (the authoritative gate). The embedded enum is a redundant second gate that only produces false-negatives. Stripping loses nothing the platform won't re-check, keeps the structural guards the platform's error messages are worse at.

**Pin:** `TestValidateImportYAML` accepts an arbitrary unknown `type` string, STILL rejects a typo'd top-level field and a bad `objectStoragePolicy`. Add a case for a not-yet-embedded type (`ubuntu/nodejs@99`) → must pass.

**Scope:** `internal/schema/` (mine). **Biggest user-facing win — fixes the live 12-type bug.**

### Fix B — `schema.Cache` (#1) discipline: never-nil floor + sanity-gate (recipe-scope behavior)
Keep #1 (recipe needs it for bases + field names), fix its two latent bugs.

**Change (in `internal/schema/`):**
- Add `schema.Embedded() *Schemas` floor (sync.Once parse of the embedded bytes) + an `active_versions` enum-floor accessor.
- Make the cache read **never-nil**: on fetch failure/timeout → return the embedded/active_versions floor, not nil. **Sanity-gate** the live fetch: reject a parse that yields empty enums (kills the poison-200).

**Effect on the two recipe consumers (greenlit, no code change required):**
- `workflow_recipe.go:93` `schemas = schemaCache.Get(ctx)` → always non-nil → plan validation runs even when the API hiccups.
- `workflow_checks_recipe.go:44` `schemas != nil && ZeropsYml != nil` → always true → field-name check stops silently skipping.

**Behavior change to verify:** these recipe checks now FIRE on the floor where they previously SKIPPED (API down). The floor is the committed schema → findings are **correct**, not false → stricter, not breaking. **Prove no-break:** recipe unit + integration + a recipe flow-eval before landing.

**Optional cleanup (recipe files):** the now-dead `if schemas != nil` guards can be removed (hygiene). Allowed since no-break holds.

**Scope:** `internal/schema/` (mine) + behavior touches 2 recipe files (greenlit conditional on tests-green).

### Fix C — Hygiene: one freshness model, drift sentinel, dead-code
- Add `zcp schema sync` / `make schema-sync`: fetch live → **reject-on-empty-enum** → write embedded schemas **with Fix-A's volatile enums stripped** → derive `active_versions.json` from the same fetch (one pass). Fold in `catalog-sync`; **drop it from `lint-local`** (`Makefile:60`) so full lint is offline. Harden `fetchURL` to reject an error-shaped/empty body (so `zcp catalog sync` can't write a poisoned catalog).
- Add a **CI drift sentinel** (schedule daily + PR): `zcp schema check` fetch-vs-committed; reachability-gated self-skip; **advisory on PR** (endpoint flakes — observed 502-in-200), **required on cron**. Replace the toothless `TestEmbeddedSchemasMatchTestdata` (asserts only `len()!=0`) with a real offline self-consistency test (embed compiles + non-empty + `active_versions` == projection).
- **Delete `format.go`** + its 2 tests (dead since `641d7958`).
- `active_versions.json` promoted from test-only to the committed enum floor used by Fix B.

**Scope:** `internal/schema/`, `internal/catalog/`, `cmd/zcp/`, `Makefile`, `.github/` (mine).

---

## 5. Migration — phases, each compiles + green

1. **Fix A — export enum-strip.** Edit the 3 volatile enums in the committed embedded schemas; pin tests (accept unknown type, reject typo'd field + bad policy). Fixes the live bug. *Gate:* unit/tool/integration/e2e green.
2. **Fix C-1 — `format.go` deletion.** Pure subtraction. *Gate:* all layers green.
3. **Fix B — `schema.Embedded()` floor + never-nil + sanity-gate.** RED first: poison→floor, offline→floor (never nil), floor non-empty. *Gate:* green; recipe consumers now non-nil.
4. **Fix B verify — recipe no-break.** Run recipe unit + integration + a recipe flow-eval; confirm stricter-but-correct (no false findings). Optional: drop dead nil-guards in the 2 recipe files. *Gate:* recipe flow-eval green.
5. **Fix C-2 — `schema sync`/`check` + `make schema-sync` + catalog-as-projection + decouple `lint-local` + `fetchURL` hardening.** Pin: `active_versions == projection of embedded`. *Gate:* offline green.
6. **Fix C-3 — CI drift sentinel + invariant pin + CLAUDE.md bullet.** `git mv` this plan to `plans/archive/`.

---

## 6. Safety / stress

| Scenario | Result |
|---|---|
| Export project with a brand-new type (`meilisearch@1.44`, `zero`) | **FIXED.** Structure validates; type not enum-blocked; platform re-validates at import. (Today: false `validation-failed`.) |
| Typo'd type in an export bundle | types come from live Discover, not hand-authored → no typo path. Worst case (a since-removed type) surfaces at platform import — low impact. |
| Schema API down during recipe validation | **IMPROVED.** Floor (never-nil) → checks run on committed schema instead of silently skipping. |
| Poison `{error:502}` body | **FIXED.** Sanity-gate rejects empty-enum parse; floor stands; `zcp catalog sync` can't write a poisoned catalog. |
| Airgapped build/run | floor is embedded; everything offline; CI already schema-network-free. |
| Deploy / import | **untouched** — platform remains the authority. |
| Recipe behavior change | stricter (checks fire where they skipped); proven non-breaking by tests + flow-eval before landing. |

---

## 7. Residual risks (honest)

- After enum-strip, a genuinely-invalid service type in export passes ZCP and only fails at platform re-import (failure surfaces farther from the edit). Acceptable — export already defers `buildFromGit` path + field-values to re-import (DM-4); and export types come from Discover, so the path is rarely hit.
- `StackTypeCache` stays global/1h-TTL/stale-on-error — fine for its bootstrap/advisory use; we deliberately do NOT make it a sole hard gate.
- Build-base / field-name freshness still rides on #1 (24h live) → up-to-24h lag for a brand-new base; floor covers offline. A new structural field still needs a release (the composer must learn to emit it anyway).

---

## 8. Open questions (few; rest are defaults)

- **Q1 (build.base oneOf strip):** `build.base` is `oneOf` + dual enums (single OR array). Strip both enum branches to `string`/`array-of-string`, keeping the oneOf shape — confirm? *Rec: yes.*
- **Q2 (Fix A standalone first):** land Fix A before the schema-sync tooling exists (edit committed JSON by hand), so the live bug is fixed immediately, then Fix C's `schema-sync` preserves the strip on future refreshes — confirm sequencing? *Rec: yes, Fix A first.*
- **Q3 (active_versions as recipe floor):** promote `active_versions.json` to the runtime enum floor for Fix B (recipe existence checks when #1 is nil), or keep recipe floor = embedded enums (which Fix A strips)? Since Fix A strips the embedded enums, the floor MUST be `active_versions` (it retains the full list). *Rec: active_versions is the recipe enum floor.*
