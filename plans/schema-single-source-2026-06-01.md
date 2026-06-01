# Schema single-source-of-truth — collapse the three drifting schema mirrors

**Date:** 2026-06-01
**Status:** proposed
**Scope owner:** krls2020 (schema/server/build) + Aleš (2 recipe call-sites, Phase 5)
**Effort:** ~1–2 days; net negative LOC (~390 deleted, ~190 added)

---

## 1. Problem in one paragraph

Zerops publishes two JSON schemas (`zerops.yaml` + `import.yml`) at two public URLs.
ZCP mirrors that **one** upstream into **three independently-refreshed artifacts**
that demonstrably drift, and uses a runtime network fetch for the *weakest* checks
while the *authoritative* checks run on a hand-frozen copy. The freshness model is
inverted, the three mirrors have measurably diverged, and the live-fetch path
carries a latent "poisoned 200" bug that can silently disable validation for 24h.

This is not a systemic mess — it is **one legacy path that predates a principle the
rest of the codebase already follows**, plus one latent input-validation hole.

---

## 2. The principle that already governs validation (and the one violator)

ZCP validates user config across **three tiers**, and two of the three are correct:

| Tier | Who is authoritative | Where it fires | Source | Verdict |
|------|---------------------|----------------|--------|---------|
| **Real mutating operations** | **Zerops platform API** | deploy, import | `client.ValidateZeropsYaml` (`POST /service-stack/zerops-yaml-validation`), `client.ImportServices` | ✅ correct |
| **Pre-publish artifacts** | **embedded schema** | export/launch bundle, recipe yaml | `//go:embed testdata/*.json` → `ValidateImportYAML`/`ValidateZeropsYAML` | ✅ correct (offline + deterministic; no service to POST to yet) |
| **Advisory recipe-plan checks** | **live 24h-TTL fetch** ❌ | recipe-plan pre-check | `schema.Cache.Get(ctx)` → public URLs | ❌ the only violator |

**Principle:** *ask the platform for real operations; use an embedded snapshot only
for pre-publish artifacts where no platform call is possible.* The live fetch (#1)
violates it — a runtime network call for advisory checks whose original purpose
(LLM knowledge injection) was already deleted.

Key file anchors:
- Deploy gate (platform-authoritative, no fallback): `internal/ops/deploy_validate_api.go:27,56` → `internal/platform/zerops_validate.go:50`
- Import gate (platform-authoritative; schema dep explicitly removed): `internal/tools/import.go:75` → `internal/ops/import.go:185`
- Pre-publish embedded gate: `internal/schema/validate_jsonschema.go:16-20,85,110`; callers `internal/ops/bundle/export.go:45-46`, `internal/ops/bundle/launch.go:173`, `internal/recipe/validators_zerops_yaml_schema.go:53,79`
- Live fetch (the violator): `internal/schema/cache.go`; consumers `internal/tools/workflow_recipe.go:93`, `internal/tools/workflow_checks_recipe.go:44`

---

## 3. The four schema-derived artifacts today (one upstream → four copies)

| # | Artifact | Source | Where | Freshness model | Fate |
|---|----------|--------|-------|-----------------|------|
| 1 | `schema.Cache` (live fetch) | public URLs at runtime | in-mem, 24h TTL | auto-refresh per process | **DELETE** |
| 2 | embedded schemas | `internal/schema/testdata/*.json` | compiled into binary | hand-committed (last `4de510c2`, 2026-05-19) | **become the single source** |
| 3 | `StackTypeCache` | **authenticated** platform API | in-mem, 1h TTL | live | **keep — different upstream** |
| 4 | `active_versions.json` | independent live fetch via `zcp catalog sync` | `internal/knowledge/testdata/`, git-committed | `make catalog-sync` (`980298a2`, 2026-05-29) | **keep, but derive from #2** |

Plus: `format.go` (`FormatBothForLLM` + 2 siblings) — LLM knowledge injection, **zero
runtime callers** (only 2 test callers), purpose removed in `641d7958`. **DELETE.**

---

## 4. Evidence this is a real defect, not a smell

**4a. Measured drift (bidirectional, 10 days).** Embedded #2 (2026-05-19) vs
`active_versions.json` #4 (2026-05-29):
- `active_versions` has **8** types embedded lacks: `mysql:ha@5.7`, `meilisearch:single@1.44`, `alpine/zero@{0.1,latest,nightly}`, `ubuntu/zero@{0.1,latest,nightly}`.
- embedded has **116** entries `active_versions` lacks: `bun@*`, `deno@*`, `dotnet@*`, `clickhouse@25.3`, `docker@*`, …

Root mechanism: `active_versions.json` is **not** derived from the embedded bytes —
`catalog.Sync` (`internal/catalog/sync.go:30`) does its **own independent
`schema.FetchSchemas` live call**. Two artifacts, two fetch lifecycles → guaranteed skew.

**4b. Latent "poisoned 200" bug.** `fetchURL` checks only `StatusCode != 200`
(`cache.go:134`). The live endpoint was observed returning **HTTP 200 with body
`{"error":{"code":"502"}}`**. `ParseImportYmlSchema` accepts that (navigatePath→nil→
empty enums) and only `Fprintln`s to stderr (`schema.go:62,68,93`) — **never errors**.
Consequence: `zcp catalog sync` can silently write an empty catalog; the live cache can
poison validation for 24h (`validateBuildBases` enters the schema branch when
`schemas.ZeropsYml` is non-nil-but-empty, then flags *every* base invalid).

**4c. Silent validation skip (offline).** When `schema.Cache.Get` returns nil
(first-fetch failure / API down), three soft gates **silently no-op**, no signal:
- `CheckZeropsYmlFields` (unknown field names) — `internal/ops/checks/yml_schema.go:24`
- `validateManagedVersionLatest` ("use latest managed version") — `internal/workflow/recipe_validate.go:193` (no liveTypes fallback)
- recipe-plan enum checks degrade (fall back to #3, or skip)

**4d. No drift sentinel exists.** `TestEmbeddedSchemasMatchTestdata`
(`validate_jsonschema_test.go:158-166`) only asserts `len()!=0`. No CI workflow touches
the schema endpoint. The embedded copy and the catalog can rot unbounded and nothing fails.

**4e. Build coupling.** `lint-local: catalog-sync …` (`Makefile:60`) makes the **local
full lint hit the live API** (and implicitly mutate `active_versions.json`). CI does
*not* run this, so CI is already schema-network-free — but local lint isn't, and breaks
if the endpoint is down.

---

## 5. Target architecture

**SINGLE embedded source of truth for all client-side validation + one make-refresh +
a CI drift sentinel; the live network call leaves the runtime entirely and survives only
as a dev/CI sync helper.**

```
            ┌──────────────────────────────────────────────┐
            │  Zerops public schema URLs (ONE upstream)     │
            └───────────────────────┬──────────────────────┘
                                    │  dev/CI ONLY: `make schema-sync` / `zcp schema check`
                                    ▼
        ┌────────────────────────────────────────────────────────┐
        │  internal/schema/testdata/{import,zerops}_yml_schema.json│  ← THE source (go:embed)
        └───────────────┬───────────────────────┬─────────────────┘
                        │ parse                  │ derive (same bytes)
                        ▼                        ▼
   schema.Embedded() *Schemas          active_versions.json (pure projection)
   (never-nil, sync.Once)              (catalog == merge of embedded enums)
       │            │
       │            └─ jsonschema.Compile → ValidateImportYAML/ValidateZeropsYAML (unchanged)
       │
       ├─ build/run-base + service-type enums  (was schema.Cache → now Embedded)
       └─ ExtractValidFields (field-name check) (was schema.Cache → now Embedded)

   UNCHANGED, platform-authoritative (online by nature):
     deploy → client.ValidateZeropsYaml     import → client.ImportServices
     live runtime types → StackTypeCache (1h TTL, authenticated)
```

**Net effects:**
- One upstream → one committed artifact → one derivation. Catalog-vs-schema skew is
  **structurally impossible** (projection, not independent fetch).
- Every client-side validator is **offline + deterministic + never-nil**. The
  silent-skip bug (4c) is closed: `schema.Embedded()` never returns nil.
- The 502-poison class (4b) is eliminated from the runtime; `schema sync`/`check`
  reject-on-empty-enum so it can't poison the committed artifacts either.
- Freshness becomes a single **git-observable** fact + a CI gate, mirroring the model
  the team already trusts for `active_versions.json`.

---

## 6. What we keep, change, delete

| Component | Action | Reason |
|-----------|--------|--------|
| `schema.Cache` struct + `Get` coalescing (`cache.go`) | **DELETE** | runtime network for advisory checks; bounded 6-site removal (4 sigs / 2 files + 2 server sites) |
| `FetchSchemas` + `fetchURL` | **KEEP**, move to `schema/fetch.go` | used only by `schema sync`/`check` (dev/CI) |
| embedded `testdata/*.json` (#2) | **PROMOTE to single source** | now also feeds enums + field names + catalog |
| `schema.Embedded()` | **ADD** (never-nil, `sync.Once`) | replaces every `schemaCache.Get(ctx)` |
| `format.go` + 2 tests | **DELETE** | dead since `641d7958` |
| `active_versions.json` (#4) | **KEEP, derive from #2** | becomes pure projection; refresh folds into `schema-sync` |
| `catalog.Sync` | **REFACTOR** to take `*Schemas` / read embedded | kills the independent live fetch (drift mechanism) |
| `StackTypeCache` (#3) | **KEEP unchanged** | authenticated live runtime types — different question |
| platform deploy/import validators | **KEEP unchanged** | authoritative for mutations |
| `make catalog-sync` | **FOLD into `make schema-sync`** | one refresh path; drop from `lint-local` dep |

---

## 7. Migration — 7 phases, each compiles + all affected layers green

Ordering keeps shared `internal/schema/` work first; the only recipe-scope (Aleš) touch
is isolated and flagged in Phase 5.

### Phase 0 — one-time reconcile (REVIEW REQUIRED, big data diff)
Regenerate embedded schemas + `active_versions.json` from one **clean** live fetch
(abort if extracted enums are empty — guards the 502-body). This is the only large diff
and it is **pure data**. The embedded import schema is ~90KB and structurally differs
from the live copy — **before clobbering, 3-way review** whether any deliberate local
edit exists (open question Q1). Commit separately.
*Gate:* compiles + all layers green.

### Phase 1 — delete dead weight (no behavior change)
Delete `format.go` + `TestFormatZeropsYmlForLLM` + `TestFormatImportYmlForLLM`. Pure
subtraction; verified zero runtime callers.
*Gate:* unit/tool/integration/e2e green.

### Phase 2 — never-nil embedded loader (additive)
Add `schema.Embedded() *Schemas` — `sync.Once` parse of `embeddedImportSchema` /
`embeddedZeropsSchema` via the existing `ParseImportYmlSchema` / `ParseZeropsYmlSchema`,
returning a **never-nil** `*Schemas`. **RED first:** test asserts non-nil with non-empty
`BuildBaseSet`/`RunBaseSet`/`ServiceTypeSet` matching the embedded JSON. No caller
changes; Cache still exists.
*Gate:* green at every layer.

### Phase 3 — build/freshness tooling + the 502 hardening (no runtime behavior change)
- Add `zcp schema sync`: fetch both URLs → **reject-on-empty-enum** → canonicalize →
  overwrite `internal/schema/testdata/*.json` → **derive** `active_versions.json` from
  the just-written embedded bytes (one atomic pass).
- Add `zcp schema check`: fetch → canonical-diff vs committed → nonzero on drift;
  **SKIP-with-log** when unreachable OR non-schema/empty-enum body.
- Harden `fetchURL` to detect an error-shaped / empty-enum body (closes 4b at the
  fetch layer too — `zcp catalog sync` can't write a poisoned catalog).
- Refactor `catalog.Sync` to derive from embedded bytes / accept `*Schemas`
  (`cmd/zcp/catalog.go` + `sync_test.go` — shared scope).
- Add `make schema-sync`; fold/remove `catalog-sync`; **drop it from `lint-local`**
  (`Makefile:60`) → full lint offline.
- **Pin:** `active_versions == merge of embedded enums`.
*Gate:* green offline.

### Phase 4 — switch non-recipe runtime consumers off Cache + delete it
- Remove `*schema.Cache` from `RegisterWorkflow` (`workflow.go:301`),
  `handleWorkflowAction` (`348`), `handleRecipeComplete` (`workflow_recipe.go:25`,
  signature), `handleRecipeCompletePlan` (`77`, signature), `server.go:140/177`.
- Delete `schema.Cache` struct + `Get` from `cache.go`; **keep** `FetchSchemas`/
  `fetchURL` (move to `schema/fetch.go`).
- Replace toothless `TestEmbeddedSchemasMatchTestdata` with a real **offline
  self-consistency** test (embed compiles + enum-extracts non-empty + catalog ==
  projection).
- Sequencing: do **signature plumbing here**, leave the two recipe-owned call-site
  *bodies* (`workflow_recipe.go:93`, `workflow_checks_recipe.go:44`) calling
  `schema.Embedded()` as the Phase-5 recipe touch (keep them compiling).
*Gate:* unit/tool/integration green.

### Phase 5 — RECIPE-SCOPE (ISOLATED + FLAGGED for Aleš)
Swap the two recipe-generate sources:
- `workflow_recipe.go:93`: `schemas = schemaCache.Get(ctx)` → `schemas = schema.Embedded()`
- `workflow_checks_recipe.go:44`: `schemaCache.Get(ctx)` → `schema.Embedded()`
`recipe_validate.go` / `engine_recipe.go` unchanged (already `*schema.Schemas`-param).
Add a test: offline recipe field/enum validation works with no cache (closes 4c).
**FLAG to Aleš before merge.**
*Gate:* tool + integration + recipe flow-eval.

### Phase 6 — CI drift sentinel + invariant pin
- Add a **schema-drift workflow** (`schedule` daily + `pull_request`) running
  `zcp schema check`: reachability-gated self-skip, **advisory/continue-on-error on PR**,
  **required on cron**, remediation message = literal `make schema-sync`.
- The Phase-4 offline self-consistency test stays the **required PR gate** (no network).
- Add a CLAUDE.md bullet ("embedded schema is the single source of truth for all
  client-side validation; refresh via `make schema-sync`; drift is a CI gate; live
  network is dev/CI-only") **pinned by a test** asserting no validator consumes a live
  fetch.
- `git mv` this plan to `plans/archive/`.

---

## 8. Stress-test (verified against the actual repo)

| Scenario | Result |
|----------|--------|
| Airgapped build, no network | **SURVIVES** — only net/http in the schema build path was `cache.go`; after deletion, zero. Validators read embedded bytes. *Stronger than today* (today airgapped recipe-validate loses enum/field checks). |
| Schema API down mid deploy/export | **SURVIVES — headline win.** Export/launch gate already embedded-only; recipe-plan switches to embedded; deploy/import stay platform-authoritative (online by nature). Eliminates the 502-poison class. |
| Platform adds a new service type tomorrow | **SURVIVES, visible trade.** Stale embedded pre-flight warns until `make schema-sync` + commit + release; nightly cron turns it RED within ~1 day with a named fix; the **live import/deploy API still ACCEPTS** the new type (user never hard-blocked). Net strictly better than today (same staleness, but today zero sentinel + 502-poison risk). |
| Fork PR, no API secrets | **SURVIVES, no flake.** Endpoint is unauthenticated public; `zcp schema check` needs no secret but is advisory/reachability-gated and **never gates merge**. Required PR gate is the offline self-consistency test. |
| Two parallel dev sessions | **SURVIVES.** `schema.Embedded()` is pure `sync.Once` over immutable bytes — no shared mutable state, no runtime writes. `make schema-sync` is content-addressed/sorted/timestamp-free → byte-identical output → clean merge, never a rebase conflict. |
| Recipe-scope ownership (Aleš) | **SURVIVES, isolated.** Exactly 2 call-sites change (Phase 5); `recipe_validate.go`/`engine_recipe.go` need zero change. Param-drop flagged, not silently dropped. |

---

## 9. Risks / trade-offs

| Decision | Rejected alternative | Trade-off |
|----------|---------------------|-----------|
| Delete live `schema.Cache` | Keep "new schema works without a ZCP release" | Already false for hard gates; deploy/import validate via live platform API regardless. Real loss ≈ advisory recipe-plan pre-check lagging until next release — cosmetic, and `make schema-sync` + nightly cron bound it. |
| Embedded as single source | Make everything live-fetch | Live-fetch is non-deterministic + network-in-hot-path + 502-poisonable; wrong for a published binary that must pin validation to a reviewed commit. |
| Keep platform deploy/import validators | Reimplement validation locally | Platform is the only authority for mutating ops + cross-field behavior. |
| Keep `StackTypeCache` | Collapse type checks into embedded | Stack types are operationally current (per-project, authenticated); schema enums are a publish-time contract. Different questions. |
| Full DELETE Cache | Demote to stderr drift-telemetry | Demote = backward-compat shim for an internal refactor (forbidden by CLAUDE.local.md); duplicates the CI sentinel; re-adds a runtime network call. |
| `active_versions` derived from written embed | Derive from in-memory live `*Schemas` | Derive-from-embed = catalog is literally a projection of what the binary ships; strongest determinism. |
| CI drift: advisory-PR / required-cron | Required-on-PR | Endpoint flakes (502-in-200 observed); required-on-PR would block merges on an upstream outage. |

---

## 10. Backward compatibility

**No user-facing config compatibility break.** Nothing here is user-authored config.
The only behavior change is internal: legacy recipe validation no longer auto-accepts a
brand-new platform schema enum before a ZCP refresh — and the authoritative live
import/deploy API still accepts it, so users are never hard-blocked. `make catalog-sync`
folds into `make schema-sync` (dev-facing; alias if any external script depends on it).

---

## 11. Open questions for Karel

- **Q1 (Phase 0):** embedded import schema is ~90KB vs the ~62KB live copy — pure
  staleness, or were embedded schemas hand-edited? Want a manual 3-way review of the
  Phase-0 diff, and any deliberate edit preserved as a documented patch-on-top?
- **Q2 (502 hardening scope):** fix in `schema sync`/`check` is in-plan — also harden
  `fetchURL` itself (it backs the user-facing `zcp catalog sync` today)? *Rec: yes.*
- **Q3 (Cache delete vs demote):** any runtime drift-WARNING wanted (stderr "embedded
  schema is N days stale"), or is the CI cron sentinel sufficient? *Rec: CI-only, pure-offline runtime.*
- **Q4 (`active_versions` source):** derive from just-written embedded bytes vs in-memory
  live `*Schemas`. *Rec: from written embed.*
- **Q5 (cron required-ness):** required-on-nightly-cron goes red on a multi-day endpoint
  outage (maintenance signal, validation still works). Paging, or just a red check? And
  PR: advisory/continue-on-error, or fully off on PR (cron-only)? *Rec: advisory-PR + required-cron, non-paging.*
- **Q6 (recipe sequencing):** split signature-plumbing (Phase 4) from the 2 body-swaps
  (Phase 5, Aleš), or hand Aleš the whole recipe-validation-sourcing slice end-to-end?
  *Rec: minimal flagged final touch.*
