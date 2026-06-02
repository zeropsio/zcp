## Grounded implementation map — `plans/schema-single-source-of-truth-2026-06-02.md`

This is the corrected, file:line-grounded reality the plan must be iterated against. The plan is a DRAFT; one of its three constituent layers is shipped, one is unstarted, and the collapse itself is greenfield. Several §5 migration-map rows are incomplete or contradicted by the already-shipped predecessor.

---

### 1. Implementation status — what of the three plans is shipped vs unimplemented

| Plan layer | Status | Evidence |
|---|---|---|
| `schema-validation-final-2026-06-01.md` (predecessor — short-TTL cache, structure-only export, recipe live-base) | **SHIPPED + green** | 15-min TTL `cache.go:17`; embedded seed `embedded.go:25` + `cache.go:45`; poison guard `cache.go:136`; structure-only validators `validate_structure.go:131,154`; `CheckZeropsBasesLive`/`Embedded()` `validate_bases.go:24,10`; sync/check tooling `cmd/zcp/schema.go`; catalog-as-projection `catalog/sync.go`; standalone `catalog.Sync` orchestrator **deleted** (only no-I/O projection remains, `catalog/sync.go` doc-comment) |
| **Phase 1** — host-derivation (= absorbed `schema-host-derivation-2026-06-02.md`) | **NOT implemented** | `schema.go:14-17` still hardcodes `ZeropsYmlURL`/`ImportYmlURL` to `app-prg1`; no `URLs`/`CanonicalAPIHost` symbol exists anywhere; `NewCache(ttl)` `cache.go:44`, `FetchSchemas(ctx)` `cache.go:102`, `FetchRawSchemas(ctx)` `sync.go:33` take no host; `server.go:140` calls `schema.NewCache(schema.DefaultCacheTTL)`; CLI `cmd/zcp/schema.go:43,62` call `FetchRawSchemas(context.Background())`; no `cmd/zcp/schema_test.go`, no `TestURLs`/`TestNewCache_UsesAPIHost`/`TestSchemaCLIPinsCanonical` |
| **Phases 2-6** — schema-derived catalog + StackTypeCache collapse | **NOT started (greenfield)** | `ops.StackTypeCache` still constructed `server.go:139`, wired into `RegisterWorkflow` `server.go:187` + `RegisterKnowledge` `server.go:189`; `ListServiceStackTypes` live in `client.go:84` + impl + mock; no schema-derived catalog abstraction; `ManagedBaseNames` still API-`Category`-driven `knowledge/versions.go:58` |

Net: the plan's §1 table claim that `schema.Cache` is the host-correct single source is **false today** — the cache hardcodes prg1. Every "live from `ZCP_API_HOST`" assertion about the schema cache in §1/§4 contradicts the code. (The `StackTypeCache` *does* follow `ZCP_API_HOST` via the platform client — so deleting it before Phase 1 lands would REGRESS host-correctness for non-prg1 users; see §7.)

---

### 2. The 5 sources AS THEY EXIST IN CODE TODAY (corrected §1 table)

| Source | What it carries | Host **(real)** | TTL | Used for |
|---|---|---|---|---|
| `schema.Cache` (`cache.go`) | structure + enums (types, build/run bases, corePackage, storagePolicy) — **NOT modes (dead, see §3)** | **hardcoded `app-prg1`** (`schema.go:15-16`; consumed `cache.go:103,107`) | 15 min (`cache.go:17`) | recipe base validation, recipe-plan type/base, field-names |
| embedded schema (`testdata/*.json`) | same, frozen; `//go:embed` seed | canonical (binary) | static | export/launch structure, recipe structure floor, cache seed (`embedded.go:25`) |
| `StackTypeCache` (`ops/context_cache.go:15`) | `[]ServiceStackType{Name,Category,Versions[]{Name,IsBuild,Status}}` | **`ZCP_API_HOST`** (implicit via platform client; cache has NO host field) | **1 h** (`context_cache.go:12`) | bootstrap discovery, knowledge briefings, recipe/bootstrap/adopt validation feed, managed-detection |
| `active_versions.json` (`knowledge/testdata/`) | merged enum list (241 versions, projection of embedded) | canonical | static | **tests/dev-tooling only** — NOT read by the briefing layer at runtime |
| platform APIs (`ValidateZeropsYaml`, `ImportServices`) | everything, authoritatively | `ZCP_API_HOST` | — | the REAL authority for deploy/import — **confirmed no local fallback** |

---

### 3. Per-subsystem ground truth

**`internal/schema` package surface** (`schema.go`, `cache.go`, `validate_structure.go`, `validate_bases.go`)
- Already-exist accessors the §4.1 catalog would compose from: `ServiceTypeSet()` `schema.go:130`, `BuildBaseSet()` `schema.go:115`, `BuildBaseVersionSet()` `schema.go:120`, `RunBaseSet()` `schema.go:125`; public slices `ZeropsYmlSchema.BuildBases/RunBases` `schema.go:27-28`, `ImportYmlSchema.ServiceTypes` `schema.go:39`. **Confirmed populated** (built in `Parse*`).
- **`BuildBaseSet()`/`ServiceTypeSet()` are NOT semantically stable across embedded-vs-live.** `baseNameSet` cuts at `@`, so the embedded (bare-form) schema yields key `php`, while the live curated schema (OS-prefixed/composite) yields `alpine/php` with **no bare `php`**. `validateBuildBases` `recipe_validate.go:106` checks the authored bare base name against this set → **false-rejects against the live schema**. Same divergence shape on `ServiceTypeSet()` membership (live managed types are composite-only). The schema sets do plain map lookup; the API path's `typeAcceptedByCatalog` uses `topology.TypesAreEquivalent` (`validate.go:466`) — that equivalence is the gap any catalog set-membership replacement must preserve.
- **`ImportYmlSchema.Modes` is dead vs current schema.** `services[].mode` is `{type:string, description "Deprecated…"}` with NO enum (`testdata/import_yml_schema.json:1709`) → `extractEnum` returns nil → `Modes` always empty. `schema_test.go:138-147` documents this. Removable, but the test references it (follow-the-chain).
- **`location`/`cpuMode` enums exist in the JSON** (`testdata:50`, `testdata:1779`) and are kept by the structure-only schema, but are **NOT extracted** into typed fields. A catalog needing them must add extraction.
- No `URLs`/`CanonicalAPIHost`; no host param on any fetch/ctor (Phase 1 unstarted).

**`StackTypeCache` + the stack-types API** (`ops/context_cache.go`, `platform/zerops_search.go:18`, `platform/types.go`)
- `StackTypeCache.Get(ctx, client)` `context_cache.go:30` is the **ONLY production caller** of `client.ListServiceStackTypes`. 1h TTL. Cache has no host field — inherits `ZCP_API_HOST` purely from the client handed at `Get`-time.
- **Two same-prefix struct families — only ONE is deletable:** catalog `ServiceStackType{Name,Category,Versions}` + `ServiceStackTypeVersion{Name,IsBuild,Status}` (`types.go:394-406`, DELETE) vs per-running-service `ServiceTypeInfo{ServiceStackTypeID,VersionName,CategoryName}` on `ServiceStack` (`types.go:64-69,37`, **KEEP — ~50 production readers** across deploy/discover/verify/subdomain/env/adopt/route/launch; also `BuildInfo.ServiceStackTypeVersionID` `types.go:378` and `ValidateZeropsYamlInput.ServiceStackType*` `zerops_validate.go:28-33` and `IsSystem()` `types.go:81-83`).
- `IsBuild` is **genuinely dead** — only written at `zerops_mappers.go:91`, zero production reads. Build-capability is derived from `Category=="BUILD"` cross-reference (`versions_format.go:102,203`), not `IsBuild`.
- `Status` is read only as a **filter** (include/exclude ACTIVE), never as a gate/block — no code path rejects a deploy/import/plan because a version is non-ACTIVE.

**Workflow validation** (`recipe_validate.go`, `validate.go`, `adopt.go`)
- Three entry points, **asymmetric source-handling:**
  - `ValidateRecipePlan(plan, liveTypes, schemas)` `recipe_validate.go:20` — schema-PREFERRED-then-liveTypes-fallback, but only in `validateRuntimeType` (`:76-88`) and `validateBuildBases` (`:101-137`). `validateTargets` (`:141`) and `validateManagedVersionLatest` (`:192`) are **schema-ONLY** (skip when `schemas==nil`, no liveTypes path).
  - `ValidateBootstrapTargets(targets, liveTypes, liveServices)` `validate.go:247` — **liveTypes-ONLY, takes NO `schemas` arg.** Type accept via `typeAcceptedByCatalog` (`:463`, equivalence-based via `topology.TypesAreEquivalent`); managed-detection via `knowledge.ManagedBaseNames(liveTypes)` (`:253`) feeding `isManagedTypeWithLive` (`:229`, which **already** falls back to `topology.IsManagedService` when the live map is empty).
  - `BootstrapCompleteAdoptPlan` `adopt.go:105` — **takes liveTypes, no schemas.** Uses `topology.IsManagedService` DIRECTLY at `adopt.go:138` for dep-building AND `knowledge.ManagedBaseNames(liveTypes)` at `adopt.go:156` for `InferServicePairing` — TWO managed-detection paths.
- `latestManagedVersion` `recipe_version_latest.go:20` is **unexported, in workflow**, operates on `[]string` from `schemas.ImportYml.ServiceTypes`. It `strings.Cut(@)` WITHOUT canonicalization → on composite-only live types (`postgresql:single@18`) the `@`-cut yields `postgresql:single != postgresql` and the type is **SKIPPED** → the "use-latest-managed-version" rule silently no-ops the moment the live fetch succeeds. This is a **currently-latent defect** (the embedded seed is bare-form so it works today); Phase 1 host-derivation activates it for all users.

**Knowledge/briefing** (`knowledge/versions.go`, `versions_format.go`, `briefing.go`, `engine.go`)
- Four pure functions over `[]platform.ServiceStackType`: `FormatStackList` `versions_format.go:12`, `FormatServiceStacks` `versions_format.go:94`, `FormatVersionCheck` `versions.go:79`, `ManagedBaseNames` `versions.go:58`. `GetBriefing` `briefing.go:27` orchestrates; the `liveTypes` arg is also on the `knowledge.Provider` interface `engine.go:28` (a mock-pinned seam).
- `Category` does **THREE** jobs, not one: (1) hidden-category filtering (`hiddenVersionCategories` = CORE/INTERNAL/BUILD/PREPARE_RUNTIME/HTTP_L7_BALANCER, `versions.go:13-19`); (2) display grouping/ordering/labels (`versionCategoryOrder`/`versionCategoryDisplayName`); (3) the `Category=="BUILD"` cross-reference producing `[B]` markers + the "Build-only:" section. Topology has no predicate for CORE/INTERNAL/PREPARE_RUNTIME/HTTP_L7_BALANCER — a topology-substitution must reconstruct all three.
- **The `[B]` marker and "Build-only:" section are ALREADY DEAD in production against the live API** (adversarial verify): the live BUILD category contains only `alpine/build_runtime`/`ubuntu/build_runtime`; zero run-version names match → `[B]` never emitted, "Build-only:" never emitted. A schema-`build.base`-driven re-derivation would make them emit REAL markers — a behavior CHANGE (net improvement, but must be flagged + flow-eval-checked, not silent).
- `active_versions.json` is NOT read here at runtime — briefing source is `StackTypeCache.Get`.

**Tools/bootstrap** (`tools/knowledge.go`, `workflow_bootstrap.go`, `workflow.go`, `workflow_recipe.go`, `import.go`)
- **ONE** `StackTypeCache`, `server.go:139`, wired only into `RegisterWorkflow` (`:187`) + `RegisterKnowledge` (`:189`); `RegisterImport` (`:231`) gets none.
- **FIVE production `cache.Get` sites:** `workflow.go:421` (bootstrap-complete liveTypes), `workflow_recipe.go:97` (recipe liveTypes, alongside a separate `schemaCache.Get` at `:93` — a **dual-source** site), `knowledge.go:156` (scope=infrastructure `FormatStackList` prepend) AND `knowledge.go:185` (briefing), `workflow_bootstrap.go:262` (`populateStacks` → `resp.AvailableStacks`). Each is nil-tolerant.
- **Per bootstrap-complete call the catalog is fetched TWICE** for different purposes — presentation (`populateStacks` self-fetches) and validation (`handleWorkflowAction.complete` self-fetches liveTypes). A repoint must replace BOTH independently.
- `import.go` is a **false positive** — its only `StackTypeCache` token is the doc-comment `:75` recording removal; `RegisterImport` `:79` takes no cache; `server.go:231` passes none. Already platform-as-authority. (Trim the stale comment in Phase 5 so it doesn't dangle.)

**Topology currency** (`predicates.go`, `runtime_class.go`, `type_equivalence.go`)
- `managedServicePrefixes` `predicates.go:8-13` is a **14-entry hand-maintained list**: postgresql, mariadb, valkey, keydb, elasticsearch, meilisearch, rabbitmq, kafka, nats, clickhouse, qdrant, typesense, **object-storage, shared-storage** (hyphenated only).
- **7 currently-shipping import schema enum values are misclassified as runtime:** `objectstorage`, `sharedstorage`, `sharedstorage:ha`, `sharedstorage:single`, `seaweedfs@3`, `seaweedfs:ha@3`, `seaweedfs:single@3` — no `seaweedfs` prefix, no no-hyphen aliases. `IsManagedService`→false→`IsRuntimeType`→true→`RuntimeDynamic`. The live `Category` field (being deleted) classifies these correctly by category. This contradicts the plan's "Category largely covered by static topology / low risk" (§2, §7).
- `CanonicalBareForm` `type_equivalence.go:27` does NOT normalize no-hyphen aliases AND `stripModeSuffix` requires an `@` after the `:` mode — so `shared-storage:ha` canonicalizes to `shared-storage:ha` (unchanged), missing the `shared-storage` prefix bucket. Storage-suffix canonicalization is broken for the suffix-without-version form.
- For the core 11 DB/cache/search/queue managed bases the schema-derived and live-Category derivations AGREE.

**Catalog / sync / wiring** (`catalog/sync.go`, `cmd/zcp/schema.go`, `cmd/zcp/catalog.go`, `schema-drift.yml`)
- `internal/catalog` is a **live, narrow, used package** — `SnapshotFromSchemas` `sync.go:32` (no I/O) + `WriteSnapshot` `sync.go:66` (deterministic JSON). Consumed by `cmd/zcp/schema.go runSchemaSync`. The §9.1 naming-collision is **real**: a new `catalog` abstraction would collide.
- Single refresh path: `make schema-sync` → `zcp schema sync` (`runSchemaSync` `schema.go:42`) → one `FetchRawSchemas` → `WriteEmbedded` + `catalog.SnapshotFromSchemas` → both artifacts from one fetch. `zcp catalog sync` `catalog.go:12` is a thin alias.
- CLI handlers are **monolithic** (inline `log.Fatalf`/`os.Exit`, no extracted testable core, no canonical pin) — the host-derivation §3.2 testability seam is unstarted.
- `schema-drift.yml` is **"accidentally canonical"** — apples-to-apples only because `FetchRawSchemas` hardcodes prg1. Once Phase 1 threads a host, correctness depends on the unbuilt canonical-pin seam.

**Deploy/import authority (UNCHANGED — confirmed)**
- `ValidatePreDeployContent` `deploy_validate_api.go:56` reads `target.ServiceStackTypeInfo.{ID,VersionName}` (`:70-71`, category B) → `client.ValidateZeropsYaml` `zerops_validate.go:62`, **no local fallback**. `ImportServices` `zerops_search.go:41` is raw-YAML passthrough, platform validates server-side. Neither touches the catalog. (Minor: stale comment `zerops_validate.go:29` names `ListServiceStackTypes` — fix on delete.)

---

### 4. Corrected §5 consumer migration map — every real consumer with file:line

The plan's §5 has 8 rows. **It omits the catalog OWNER, two production cache.Get sites, and all signature-level plumbing; it has no false-positive rows** (every listed row is real; `import.go` is correctly absent).

| # | Consumer | Real file:line | Plan §5 says | Correction |
|---|---|---|---|---|
| 1 | recipe plan validation fallback | `recipe_validate.go:82-88` (`validateRuntimeType`), `:113-137` (`validateBuildBases`) | "remove fallback — schema preferred + never-nil seeded" | **CONTRADICTS shipped `schema-validation-final §3.3`** which explicitly KEEPS the nil-tolerant fallback. `recipe_validate_test.go:509,548` pass `(plan, liveTypes, nil)` and assert fallback behavior. Also: `validateTargets`/`validateManagedVersionLatest` are schema-ONLY (no fallback to remove). Decision needed. |
| 2 | bootstrap target validation | `validate.go:247` (`ValidateBootstrapTargets`), `:463` (`typeAcceptedByCatalog`), `:229` (`isManagedTypeWithLive`) | "→ schema-derived catalog type set + topology managed-detection" | **Takes NO `schemas` arg today.** Phase 3 must ADD a catalog parameter + thread from `workflow.go:421` and through `engine.go:540,560` — additive plumbing, not "remove a fallback." Catalog membership MUST do `TypesAreEquivalent` (legacy-bare↔composite BC, pinned by `validate_bc_test.go`) or BC tests regress. |
| 3 | adopt pairing | `adopt.go:105` (`BootstrapCompleteAdoptPlan`), `:138` (direct `IsManagedService`), `:156` (`ManagedBaseNames`), `InferServicePairing` `:45` | "→ schema-derived `ManagedBaseNames`" | Two managed-detection paths must stay consistent. `ManagedBaseNames` re-derivation hits the topology-currency gap (§7) for storage types. |
| 4 | knowledge briefing | `versions.go:58,79` + `versions_format.go:12,94` + `briefing.go:27` | "schema-derived lists, group via topology; drop Status" | Function-level enumeration incomplete: also `formatUnmatchedBuild` `versions_format.go:200`, `formatStackEntry`, `hiddenVersionCategories`, `managedCategories`, `versionStatusActive` const are collateral. `Category` does 3 jobs (§3). `[B]`/Build-only resurrection is a behavior change. |
| 5 | `zerops_knowledge` tool | `knowledge.go:156` AND `:185` (TWO sites) + `RegisterKnowledge` `:83` (`cache` param) | "→ schema catalog" | Cell hides two distinct `cache.Get` paths (scope-prepend + briefing). |
| 6 | bootstrap handlers | `workflow_bootstrap.go:258` (`populateStacks`), `:32/:152/:179/:185` (handlers carrying `cache`) | "→ schema catalog" | Plus the presentation `cache.Get` is separate from the validation `cache.Get` — both repointed. |
| 7 | e2e knowledge-quality | `e2e/knowledge_quality_test.go:189` (`ListServiceStackTypes`), `:489` (`activeVersionSet`), `:266` (`GetBriefing(...,liveTypes)`) | "→ schema / active_versions.json" | Right for the version set; **silent on the `GetBriefing` coupling** — once the Provider signature drops the catalog param, this call must change too. |
| 8 | deploy pre-validation | `deploy_validate_api.go:70-71` → `ValidateZeropsYaml` `zerops_validate.go:62` | "UNCHANGED — service's own metadata + platform validator" | **CONFIRMED correct.** Category-B read, no catalog, no fallback. |

**MISSING from §5 entirely (must be added or Phase 5 will not compile):**
- **Catalog owner:** `ops/context_cache.go` (whole file, Phase 5 delete); `server.go:139` (construction) + `:187,:189` (threading); `platform/client.go:84` (interface method) + `zerops_search.go:18` (impl) + `mock_methods.go:486` + `mock.go:30,257` (`WithServiceStackTypes`); `platform/types.go:394-406` (catalog structs) + `zerops_mappers.go:84` (`mapServiceStackTypes`).
- **Production cache.Get site:** `tools/workflow.go:421` (NOT in §5; feeds non-recipe bootstrap-complete) + `:1005` (`populateStacks` in `handleBootstrapStart`, unconditional). Cache param threaded through `workflow.go` handler signatures `:301,:348,:519,:893,:907,:939`.
- **Production cache.Get site:** `tools/workflow_recipe.go:97` (NOT in §5; feeds `RecipeCompletePlan` `engine_recipe.go:283`) — the **dual-source** (StackTypeCache + schema.Cache) site §1/§4 want collapsed.
- **Signature-level plumbing:** `engine.go:540` (`BootstrapCompletePlan`), `:560` (`completePlanWithTargets`), `engine_recipe.go:283` (`RecipeCompletePlan`), `knowledge/engine.go:28` (`Provider.GetBriefing` interface — pinned by mock at `recipe_knowledge_chain_test.go:22`) — all carry `liveTypes []platform.ServiceStackType`.

---

### 5. Discrepancy ledger (plan-says vs code-says, severity-ranked)

**BLOCKING**
1. **Host-derivation premise false.** §1/§4 treat the schema cache as host-correct/`ZCP_API_HOST`. Code: hardcoded `app-prg1` (`schema.go:15-16`), no host param anywhere. → Phase 1 is full-scope and a **hard prerequisite** for Phase 3/5 (deleting `StackTypeCache`, which *does* follow `ZCP_API_HOST`, before host-deriving the schema would regress non-prg1 users to prg1 validation).
2. **§5 row 1 contradicts the shipped predecessor.** §5 says remove the recipe `liveTypes` fallback; `schema-validation-final §3.3` explicitly decided to KEEP it, with tests pinned to `(plan, liveTypes, nil)` (`recipe_validate_test.go:509,548`). Removing it re-litigates a settled decision and breaks tests unless rewritten. **User must reconcile the two plans.**
3. **Topology managed-detection misses 7 currently-shipping storage types.** §2/§7 say "Category largely covered / low risk." Real predicates over the import enum misclassify `objectstorage`/`sharedstorage*`/`seaweedfs*` as runtime (`predicates.go:8-13` + `IsRuntimeType` `:71`). Dropping live `Category` for static topology is **NOT safe as the lists stand** — needs the missing prefixes OR (better) structural derivation from the schema's storage `allOf` discriminators, plus a `CanonicalBareForm` suffix fix.
4. **`typeAcceptedByCatalog` BC equivalence not preserved by a set-membership swap.** §4.1/§5 assume `ServiceTypeSet()` membership replaces accept logic. Bootstrap accepts legacy-bare↔composite via `topology.TypesAreEquivalent` (`validate.go:466`, pinned). Exact set-membership false-rejects those; the catalog method must do equivalence.

**NOTABLE**
5. **`BuildBaseSet()`/`ServiceTypeSet()` membership differs embedded-vs-live** (bare `php` vs `alpine/php`). `validateBuildBases` `recipe_validate.go:106` false-rejects authored bare bases against the live composite schema. Latent today (embedded seed is bare); Phase 1 activates it. Needs `CanonicalBareForm`-aware matching.
6. **`latestManagedVersion` silently no-ops on composite live types** (`recipe_version_latest.go:24` `@`-cut without canonicalization). "Keep as-is" (§4.1) is wrong; needs `CanonicalBareForm` before the cut + a composite test case.
7. **§5 omits `workflow.go` and `workflow_recipe.go` cache.Get sites** + all signature plumbing (§4 above). Treating §5 as the complete file list leaves dangling `cache.Get` + orphaned `cache` params → Phase 5 won't compile.
8. **CLI testability seam + canonical pin unstarted** (`cmd/zcp/schema.go` monolithic, no `schema_test.go`). Phase 1 §3.2 extraction is genuinely needed.

**MINOR**
9. `ImportYmlSchema.Modes` dead (always empty; `schema_test.go:138-147`) — removable, touches the test.
10. `IsBuild` confirmed dead (only `zerops_mappers.go:91`) — deletion free.
11. `import.go` §5 omission is CORRECT (false positive in the scout grep) — but trim the stale doc-comment `:75` + `zerops_validate.go:29` on delete.
12. `[B]`/Build-only sections already dead vs live API; schema re-derivation makes them emit (behavior change to flag).
13. Plan's `*/build_runtime` phrasing (§2) is not grounded in repo code (it filters `Category=="BUILD"` + `zbuild ` prefix `versions_format.go:203`); the substantive claim (build bases come from the schema) holds.
14. `active_versions.json` == projection-of-embedded pin still deferred (`schema-validation-final §10 #2`); no equality pin in `catalog/sync_test.go`.

---

### 6. §9 open items resolved with evidence

- **§9.1 catalog placement** — collision is REAL: `internal/catalog` is live + used (`catalog/sync.go`, consumed by `runSchemaSync`). But it's narrow (~80 LOC, only the version-snapshot projection). Options: (a) rename the existing package, (b) fold the new catalog into `internal/schema` (methods on `*Schemas`, or `schema.Catalog`), (c) name it differently. Folding into `internal/schema` is cleanest since the catalog is a pure projection of `*schema.Schemas` and consumers (workflow) already import `schema`.
- **§9.2 topology currency** — NOT current: 7 storage enum values misclassified (§5 #3). This is a name-prefix-list design flaw, not a stale-entry problem — re-creates for the next storage backend. Recommend structural derivation from the schema storage `allOf` discriminators + a pinning test running `IsManagedService` over the full embedded `services[].type` enum.
- **§9.3 deprecation dropped** — confirmed safe: `Status` is filter-only, never a gate (no deploy/import/plan rejection on non-ACTIVE). Live schema already curated (probe: 180 live run.base vs 261 embedded). Behavior change is briefings losing ACTIVE/DEPRECATED labels — accepted per §3.
- **§9.4 phasing** — Phase 1 is a HARD prerequisite, not optional sequencing: `StackTypeCache` follows `ZCP_API_HOST`, the schema cache hardcodes prg1, so deleting the former before host-deriving the latter regresses non-prg1 users. Land Phase 1 first (it's independently valuable + the predecessor already verified it).

---

### 7. Risks the plan under-weights

1. **Phase-ordering is correctness-load-bearing, not cosmetic** (§9.4 frames it as a free choice). Deleting `StackTypeCache` before Phase 1 = silent host regression.
2. **Topology storage-classification gap is present-day, not hypothetical** (plan rates it "low risk"). 7 shipped enum values flip managed→runtime, changing bootstrap/adopt validation + briefing grouping for storage services.
3. **Composite-vs-bare canonicalization is a cross-cutting trap** affecting `validateBuildBases`, `ServiceTypeSet` membership, `latestManagedVersion`, and `ManagedBaseNames` — all latent today (embedded bare seed) and all activated together by Phase 1's live composite fetch. The plan treats these as independent "keep" items.
4. **Phase 3/4 sequencing hazard inside the plan:** §6 repoints consumers onto a schema-derived `ManagedBaseNames` in Phase 3, but `ManagedBaseNames` itself is edited in Phase 4 — Phase 3 would repoint onto something not yet written.
5. **Blast radius of the interface delete** (`ListServiceStackTypes` from `client.go:84`) ripples to mock + ~20 test fixture files using `[]platform.ServiceStackType` — §5's table understates it.
6. **`Provider.GetBriefing` interface change** touches a mock-pinned seam (`recipe_knowledge_chain_test.go:22`) + every implementer/caller + the e2e call (`:266`).
7. **Test-fixture migration** (dropping `Status`/`Category`) is a large, un-budgeted Phase 4 cost: `versions_test.go:29` injects a DEPRECATED row to prove exclusion — that and many `ServiceStackType{Category,Status}` literals must be rewritten to a Status-free schema-derived shape.
