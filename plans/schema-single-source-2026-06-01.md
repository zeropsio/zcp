# Schema single-source-of-truth — collapse the three drifting schema mirrors

**Date:** 2026-06-01
**Status:** proposed
**Scope owner:** krls2020 (schema/server/build) + Aleš (2 recipe call-sites, Phase 5)
**Effort:** ~2 days; roughly LOC-neutral (delete `format.go` ~250; refactor cache→resolver + add `Resolve`/`Embedded`/sanity-gate/UNION + tooling + tests ~net even)

---

## 1. Problem in one paragraph

Zerops publishes two JSON schemas (`zerops.yaml` + `import.yml`) at two public URLs —
the **live source of truth**. ZCP mirrors that one upstream into **three
independently-refreshed copies** that demonstrably drift, and its runtime live fetch is
**undisciplined**: it trusts a poisoned `{error:502}` body (empty enums → can disable
validation for 24h), returns nil on failure (three soft gates silently vanish), and is
treated as a competing source rather than a freshness layer over a deterministic floor.

The fix is **not** to delete the live fetch — the platform schema must reach an installed
ZCP **without forcing a release**. The fix is to make the embedded copy an honest
**floor** (deterministic, offline, never-nil) and the live fetch a **disciplined overlay**
that raises the floor when reachable-and-valid, and to collapse the third mirror
(`active_versions.json`) into a pure projection of the floor.

---

## 2. The validation tiers (two were already right; the third needs discipline, not deletion)

ZCP validates user config across **three tiers**:

| Tier | Who is authoritative | Where it fires | Source | Verdict |
|------|---------------------|----------------|--------|---------|
| **Real mutating operations** | **Zerops platform API** (live, final) | deploy, import | `client.ValidateZeropsYaml` (`POST /service-stack/zerops-yaml-validation`), `client.ImportServices` | ✅ correct — keep |
| **Pre-publish artifacts** | **client-side schema** | export/launch bundle, recipe yaml | `//go:embed testdata/*.json` → `ValidateImportYAML`/`ValidateZeropsYAML` | ✅ right place; gains the floor+overlay resolution |
| **Advisory recipe-plan checks** | **client-side schema** | recipe-plan pre-check | today `schema.Cache.Get(ctx)` (undisciplined) | ⚠️ right idea, broken impl → fix into the disciplined overlay |

**Principle:** *the platform is the final authority for real operations; for everything
client-side (pre-publish artifacts + advisory pre-checks) the schema is the source of
truth — resolved as a deterministic embedded **floor** raised by a validate-before-trust
live **overlay**.* The current live fetch isn't wrong to exist (it provides
freshness-without-release); it is wrong because it lacks discipline (poison-trust,
nil-on-fail, no floor) and because its dead LLM-injection sibling (`format.go`) was never
cleaned up.

Key file anchors:
- Deploy gate (platform-authoritative, no fallback): `internal/ops/deploy_validate_api.go:27,56` → `internal/platform/zerops_validate.go:50`
- Import gate (platform-authoritative; schema dep explicitly removed): `internal/tools/import.go:75` → `internal/ops/import.go:185`
- Pre-publish embedded gate: `internal/schema/validate_jsonschema.go:16-20,85,110`; callers `internal/ops/bundle/export.go:45-46`, `internal/ops/bundle/launch.go:173`, `internal/recipe/validators_zerops_yaml_schema.go:53,79`
- Live fetch (to be disciplined into the overlay): `internal/schema/cache.go`; consumers `internal/tools/workflow_recipe.go:93`, `internal/tools/workflow_checks_recipe.go:44`

---

## 3. The four schema-derived artifacts today (one upstream → four copies)

| # | Artifact | Source | Where | Freshness model | Fate |
|---|----------|--------|-------|-----------------|------|
| 1 | `schema.Cache` (live fetch) | public URLs at runtime | in-mem, 24h TTL | auto-refresh per process | **REFACTOR → disciplined overlay** |
| 2 | embedded schemas | `internal/schema/testdata/*.json` | compiled into binary | hand-committed (last `4de510c2`, 2026-05-19) | **become the FLOOR** |
| 3 | `StackTypeCache` | **authenticated** platform API | in-mem, 1h TTL | live | **keep — different upstream** |
| 4 | `active_versions.json` | independent live fetch via `zcp catalog sync` | `internal/knowledge/testdata/`, git-committed | `make catalog-sync` (`980298a2`, 2026-05-29) | **keep, but derive from FLOOR (#2)** |

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

## 5. Target architecture — embedded FLOOR + disciplined live OVERLAY

**Design requirement (Karel, 2026-06-01):** when Zerops ships a new schema version,
ZCP must pick it up at runtime **without forcing a release**. The live schema is the
real source of truth; the embedded copy is an honest offline stand-in. So the model is
NOT "embedded only" — it is a **floor + overlay** where the live schema raises the floor
when reachable, and the floor catches every failure deterministically.

```
            ┌──────────────────────────────────────────────┐
            │  Zerops public schema URLs (ONE upstream,     │
            │  the LIVE source of truth)                    │
            └──────────┬───────────────────────┬───────────┘
        dev/CI:        │                runtime:│ disciplined overlay fetch
   `make schema-sync`  │                        │ (lazy, TTL-cached, bounded,
   `zcp schema check`  ▼                        │  validate-before-trust)
   ┌────────────────────────────────────┐       ▼
   │ internal/schema/testdata/*.json     │   ┌──────────────────────────────┐
   │  ← the committed FLOOR (go:embed)   │   │  live overlay (in-mem, TTL)  │
   └───────┬──────────────────┬──────────┘   │  trusted ONLY if non-empty   │
           │ parse            │ derive        │  enums + compiles; else      │
           ▼                  ▼               │  discarded (→ floor stands)  │
   embedded *Schemas    active_versions.json  └──────────────┬───────────────┘
   (never-nil)          (pure projection of floor;           │
           │             deterministic, tests/CI)            │
           └───────────────┬──────────────────────────────────┘
                           ▼
         schema.Resolve(ctx) *Schemas   ← NEVER nil. = floor, RAISED by live overlay.
                           │                enum allowlists: UNION(floor, live)  → floor can only GROW
                           │                structural jsonschema: live-if-trusted else floor
       ┌───────────────────┼───────────────────────┐
       ▼                   ▼                        ▼
  build/run-base +   ExtractValidFields    ValidateImport/ZeropsYAML
  service-type enums (field-name check)    (export/launch bundle gate)

   UNCHANGED, platform-authoritative (online by nature — the FINAL authority):
     deploy → client.ValidateZeropsYaml     import → client.ImportServices
     live per-project runtime types → StackTypeCache (1h TTL, authenticated)
```

### 5.1 Runtime resolution — the one rule

`schema.Resolve(ctx) *Schemas` returns the **effective** schema for every client-side
validator. **Never nil.** Resolution:

1. **Floor = embedded** (parsed once via `sync.Once`). Always present, offline, the
   guaranteed baseline of everything valid at build time.
2. **Overlay = live fetch**, but trusted ONLY if it passes a **sanity gate**: build
   bases, run bases AND service types all non-empty AND both schemas compile. A
   `{"error":{"code":"502"}}`-body (HTTP 200) fails this → **discarded**, floor stands.
   (Kills 4b at the runtime layer.)
3. **Enum allowlists** (service types, build/run bases) = **UNION(floor, trusted-live)**.
   The floor can only ever **grow** at runtime — never reject what the binary shipped as
   valid, always accept what the fresh platform schema adds. Offline → exactly the floor.
4. **Structural jsonschema** (export/launch gate) = trusted-live-if-available **else**
   floor. Structure rarely changes; preferring live lets a brand-new optional field pass
   when online; the floor guarantees it never breaks offline; the **platform re-validates
   at the actual import**, so this gate is a courtesy pre-check, not the final authority.
5. **Fetch hygiene:** lazy + TTL-cached + coalesced (today's machinery, FIXED).
   Warmed in the background on server start so the first tool call never blocks; bounded
   timeout; any failure/timeout/garbage → floor. **No call ever hangs on the network,
   and no call ever returns nil.**

### 5.2 What this buys (the requirement, met by design)

| New platform schema event after install | How it reaches the user — NO release |
|---|---|
| New **service type** (`foo@2`) | live overlay UNION + `StackTypeCache` (authenticated per-project types) both surface it |
| New **version** (`postgresql@18`) | live overlay UNION (and `StackTypeCache` for the project's own) |
| New **build/run base** (`bun@2`, `go@1.25`) | live overlay UNION — *only* the public schema carries these (not StackTypeCache) |
| New **optional field** | structural jsonschema prefers trusted-live → accepted online |
| Live endpoint down / garbage / airgap | floor stands — deterministic, never worse than build time |

**A release is needed only for actual ZCP CODE changes (new validation logic, handler
behavior) — never merely because the platform schema moved.**

**Net effects:**
- **Freshness without release** — the overlay raises the floor live (Karel's requirement).
- **Floor can only grow** — a flaky/partial/poisoned live response can never make
  validation *stricter* than the shipped binary (UNION semantics). Kills the
  502-poison-rejects-everything regression (4b) and the silent-skip (4c): `Resolve` is
  never nil, never below floor.
- **Catalog skew structurally impossible** — `active_versions.json` (#4) is a pure
  projection of the **floor** (not the overlay, not an independent fetch). Deterministic
  for tests/CI.
- **One committed artifact, one git-observable freshness fact + CI sentinel** — the floor
  is refreshed by `make schema-sync`; drift between floor and live is what the overlay
  closes at runtime and the cron sentinel surfaces at dev time.

---

## 6. What we keep, change, delete

| Component | Action | Reason |
|-----------|--------|--------|
| `schema.Cache` struct + `Get` (`cache.go`) | **REFACTOR into the overlay** | keep the TTL/coalesce machinery, ADD validate-before-trust + floor-fallback + UNION; it becomes the live overlay behind `Resolve`, not an independent source |
| `schema.Resolve(ctx)` | **ADD** (never-nil) | floor (embedded) RAISED by trusted live overlay; the single entry point for all client-side validators; replaces every `schemaCache.Get(ctx)` |
| `schema.Embedded()` | **ADD** (never-nil, `sync.Once`) | the FLOOR — parsed embedded bytes; also the offline/CI/test path and the `Resolve` fallback |
| embedded `testdata/*.json` (#2) | **PROMOTE to the floor + catalog source** | feeds the floor, field names, and the catalog projection |
| `FetchSchemas` + `fetchURL` | **KEEP + HARDEN** | backs the overlay AND `schema sync`/`check`; add reject-on-empty/error-body (kills the poison at the fetch layer) |
| `format.go` + 2 tests | **DELETE** | dead since `641d7958` |
| `active_versions.json` (#4) | **KEEP, derive from the FLOOR** | pure projection of embedded; deterministic for tests; refresh folds into `schema-sync` |
| `catalog.Sync` | **REFACTOR** to derive from embedded floor | kills the independent live fetch (the #2↔#4 drift mechanism) |
| `StackTypeCache` (#3) | **KEEP unchanged** | authenticated live per-project types — freshest source for "does this type exist"; complements the overlay |
| platform deploy/import validators | **KEEP unchanged** | the FINAL authority for mutations |
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
changes; this is the FLOOR the overlay (Phase 4) builds on top of.
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

### Phase 4 — rebuild the live fetch into a disciplined overlay + add `Resolve` (NON-recipe)
This is the heart of the operations model. The live fetch is **not deleted** — it is
fixed into the overlay that raises the floor.
- Refactor `cache.go` → `resolver.go`: keep the TTL/coalesce machinery, but make the
  cached value an **overlay** that is accepted ONLY if it passes the **sanity gate**
  (build/run bases + service types non-empty AND both compile); otherwise discarded.
  Rename `*schema.Cache` → `*schema.Resolver` (constructed in `server.go`, **stays a
  threaded dependency** — NOT a package global; complies with the no-global-mutable-state
  rule). Warm in the background on startup; bounded timeout; any failure → floor.
- Add `Resolver.Resolve(ctx) *Schemas` — **never nil**. Enum sets = **UNION(floor,
  trusted-overlay)**; structural schema = trusted-overlay-if-present else floor. (This is
  §5.1.)
- Keep `FetchSchemas`/`fetchURL` (move to `schema/fetch.go`); the overlay AND
  `schema sync`/`check` share them.
- **RED tests:** (a) poison body `{error:502}` → discarded → `Resolve` == floor;
  (b) offline / fetch-fail → `Resolve` == floor, never nil; (c) overlay adds a new type
  → `Resolve` enum set == floor ∪ {new} (floor only grows); (d) overlay missing a
  floor value → floor value still present (UNION guarantee).
- Replace toothless `TestEmbeddedSchemasMatchTestdata` with a real **offline
  self-consistency** test (embed compiles + enum-extracts non-empty + catalog == projection).
- Sequencing: do the **type-rename + server wiring + resolver internals here**; the two
  recipe-owned call-site *bodies* (`workflow_recipe.go:93`, `workflow_checks_recipe.go:44`)
  still compile against the renamed param — their `Get`→`Resolve` body swap is Phase 5.
*Gate:* unit/tool/integration green.

### Phase 5 — RECIPE-SCOPE (ISOLATED + FLAGGED for Aleš)
Swap the two recipe-generate sources from the raw cache read to the disciplined resolver:
- `workflow_recipe.go:93`: `schemas = schemaCache.Get(ctx)` → `schemas = resolver.Resolve(ctx)`
- `workflow_checks_recipe.go:44`: `schemaCache.Get(ctx)` → `resolver.Resolve(ctx)`
`recipe_validate.go` / `engine_recipe.go` unchanged (already `*schema.Schemas`-param).
Add tests: (a) offline recipe field/enum validation works on the floor (closes 4c — no
more silent skip); (b) with a stubbed-fresh overlay, a brand-new build base / type is
accepted **without a release** (proves the requirement).
**FLAG to Aleš before merge.**
*Gate:* tool + integration + recipe flow-eval.

### Phase 6 — CI drift sentinel + invariant pin
- Add a **schema-drift workflow** (`schedule` daily + `pull_request`) running
  `zcp schema check`: reachability-gated self-skip, **advisory/continue-on-error on PR**,
  **required on cron**, remediation message = literal `make schema-sync`. (At runtime the
  overlay already absorbs drift; this surfaces it at dev time so the committed FLOOR +
  catalog stay current for offline/CI.)
- The Phase-4 offline self-consistency test stays the **required PR gate** (no network).
- Add a CLAUDE.md bullet ("client-side schema validation resolves through
  `schema.Resolve` = embedded FLOOR raised by a validate-before-trust live OVERLAY,
  never nil, floor-only-grows; the committed floor is refreshed by `make schema-sync`;
  `active_versions.json` is a projection of the floor; deploy/import stay
  platform-authoritative") **pinned by a test** asserting every client-side validator
  goes through `Resolve` (no raw `Get`/`FetchSchemas` outside the resolver + sync/check).
- `git mv` this plan to `plans/archive/`.

---

## 8. Stress-test (verified against the actual repo)

| Scenario | Result |
|----------|--------|
| Airgapped build / run, no network | **SURVIVES** — overlay fetch fails → `Resolve` returns the floor; every validator runs offline on embedded bytes, never nil. *Stronger than today* (today airgapped recipe-validate loses enum/field checks entirely on a nil cache). |
| Schema API down mid deploy/export | **SURVIVES.** Overlay discarded → floor stands; export/launch gate + recipe-plan validate on the floor; deploy/import stay platform-authoritative (online by nature). The 502-poison can no longer make validation *stricter* than the floor (UNION + sanity gate). |
| **Platform adds a new service type / base / version tomorrow** | **SURVIVES — and this is the requirement, met.** The live overlay UNION surfaces the new value at runtime **with no release**; `StackTypeCache` also surfaces new per-project types live. The committed floor + catalog lag until `make schema-sync`, but that only affects offline/CI determinism, never a user's live validation. Release is needed only for ZCP *code* changes. |
| New value but overlay flaky/partial | **SURVIVES.** UNION means the floor can only grow — a partial live response never drops a floor value; a poison body fails the sanity gate and is discarded. Worst case = exactly the floor (build-time correctness). |
| Fork PR, no API secrets | **SURVIVES, no flake.** Endpoint is unauthenticated public; `zcp schema check` needs no secret but is advisory/reachability-gated and **never gates merge**. Required PR gate is the offline self-consistency test. |
| Two parallel dev sessions | **SURVIVES.** `Embedded()` floor is `sync.Once` over immutable bytes; the overlay is a per-`Resolver` (per-process) struct — no shared global state. `make schema-sync` is content-addressed/sorted/timestamp-free → byte-identical → clean merge. |
| Recipe-scope ownership (Aleš) | **SURVIVES, isolated.** Exactly 2 call-site bodies change (Phase 5, `Get`→`Resolve`); `recipe_validate.go`/`engine_recipe.go` need zero change. Flagged, not silent. |

---

## 9. Risks / trade-offs

| Decision | Rejected alternative | Trade-off |
|----------|---------------------|-----------|
| **Floor + disciplined overlay** | (a) Embedded-only / delete live fetch | Embedded-only forces a ZCP release for every schema change — **rejected by the operations requirement**. The overlay buys "fresh without release"; the discipline (sanity-gate + UNION + floor-fallback) removes the reasons the *current* live fetch was bad. Cost: keeps a bounded runtime fetch + two-layer resolution logic — worth it for the requirement. |
| | (b) Live-only as source (≈ today) | Non-deterministic, nil-on-failure (silent skip), 502-poisonable, no offline floor. The overlay keeps live's freshness but never lets it drop below the deterministic floor. |
| **UNION enums (floor can only grow)** | Prefer-live-else-floor for enums | Prefer-live would let a partial/stale-but-valid live response *reject* a floor value (false-negative regression). UNION guarantees no runtime regression below build-time; the cost (accepting a since-removed value) is a harmless false-positive the platform catches authoritatively. |
| Structural schema prefers live | Floor-only structural | A new optional field must pass when online without a release; floor guarantees offline; platform re-validates at import. The export/launch gate is a courtesy pre-check, not the final authority. |
| Keep platform deploy/import validators | Reimplement validation locally | Platform is the only authority for mutating ops + cross-field behavior. |
| Keep `StackTypeCache` | Collapse type checks into the schema overlay | Authenticated per-project types are the freshest "does this type exist" source; complements (doesn't duplicate) the public-schema overlay, which uniquely carries build/run bases. |
| `active_versions` derived from the floor | Derive from the live overlay | Catalog must be deterministic for tests/CI → project the committed floor, not the runtime-variable overlay. |
| CI drift: advisory-PR / required-cron | Required-on-PR | Endpoint flakes (502-in-200 observed); required-on-PR would block merges on an upstream outage. |

---

## 10. Backward compatibility

**No user-facing config compatibility break.** Nothing here is user-authored config.
Behavior changes are net improvements: validation never silently skips (floor is
never-nil), the 502-poison can no longer make checks stricter than the floor, and a
brand-new platform enum is auto-accepted at runtime via the overlay (UNION) **without a
ZCP release** — strictly more permissive than today, never less. `make catalog-sync`
folds into `make schema-sync` (dev-facing; alias if any external script depends on it).

---

## 11. Open questions for Karel

- **Q1 (Phase 0):** embedded import schema is ~90KB vs the ~62KB live copy — pure
  staleness, or were embedded schemas hand-edited? Want a manual 3-way review of the
  Phase-0 diff, and any deliberate edit preserved as a documented patch-on-top?
- **Q2 (502 hardening scope):** fix in `schema sync`/`check` is in-plan — also harden
  `fetchURL` itself (it backs the user-facing `zcp catalog sync` today)? *Rec: yes.*
- **Q3 (overlay merge semantics):** enum allowlists = **UNION(floor, live)** so the floor
  can only grow (never reject a floor value on a flaky live response) — confirm? And
  structural jsonschema = prefer-trusted-live-else-floor — confirm? *Rec: UNION enums +
  prefer-live structural.*
- **Q3b (overlay warm strategy / TTL):** warm the overlay in the background on server
  start (freshness from the first real validation, never blocks) vs purely lazy on first
  `Resolve` (first call uses floor). And TTL — keep 24h, or shorter? *Rec: background-warm
  on start + bounded timeout; 24h TTL.*
- **Q4 (`active_versions` source):** derive from just-written embedded bytes vs in-memory
  live `*Schemas`. *Rec: from written embed (the FLOOR).*
- **Q5 (cron required-ness):** required-on-nightly-cron goes red on a multi-day endpoint
  outage (maintenance signal, validation still works). Paging, or just a red check? And
  PR: advisory/continue-on-error, or fully off on PR (cron-only)? *Rec: advisory-PR + required-cron, non-paging.*
- **Q6 (recipe sequencing):** split signature-plumbing (Phase 4) from the 2 body-swaps
  (Phase 5, Aleš), or hand Aleš the whole recipe-validation-sourcing slice end-to-end?
  *Rec: minimal flagged final touch.*
