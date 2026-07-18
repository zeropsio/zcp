# Schema as the single source of truth — collapse StackTypeCache, host-derive the fetch, make composite-aware

**Date:** 2026-06-02 (straightened from the original draft after a 13-agent ground-truth sweep + targeted live-schema probes)
**Status:** IMPLEMENTED — **all phases (1-6) shipped + green**: full `go test ./... -short` passes, `-race` on every touched package passes, `lint-fast` 0 issues, `schema check` live-fetch verified, `api-node-postgres-classic-dev` flow-eval clean (no regression). `StackTypeCache` + `client.ListServiceStackTypes` + the catalog `ServiceStackType`/`ServiceStackTypeVersion` structs are deleted; recipe path migrated (Aleš's `workflow_recipe.go`, Karel-authorized + verified no behavior change). Codex-verified `GO-WITH-REVISIONS` (§12, design); managed-detection (§8 = (b)) implemented. **Post-implementation Codex review: 5 rounds** — found + fixed composite-aware misses (`validateTargets`, `latestManagedVersion`, `FormatVersionCheck`), a self-introduced bogus-mode regression (root-caused: strip only KNOWN modes `{single,ha}`, mirroring known-OS-prefix), and the full mode-on-incapable-base class (catalog matcher + version-check both gated on `topology.ServiceSupportsMode`), each pinned by negative tests. One **accepted** residual (flagged, not silently skipped): `FormatVersionCheck` (an advisory briefing display, NOT a validation gate) is marginally more lenient than the strict catalog gate for a *single-mode managed type* — a data shape the schema never produces (both `:single`/`:ha` are always listed); closing it cleanly would regress the bare-name→latest resolution. Pending: plan archival (after commit decision). (One unrelated RED test — `TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap/dual-runtime-showcase/provision` — is a pre-existing parallel-store-mutation flake in Aleš's recipe-guidance domain, tipped over its 23 KB cap by a parallel-session corpus commit `a339a9d5` that landed mid-session; passes in isolation, unaffected by this refactor.)
**Scope owner:** krls2020
**Evidence appendix:** `plans/schema-single-source-of-truth-2026-06-02-impl-map.md` (per-file, per-field ground truth + adversarial verdicts; every file:line below traces to it).
**Absorbs:** `plans/schema-host-derivation-2026-06-02.md` (→ Phase 1 here; that plan is Codex-verified-ready and UNIMPLEMENTED).
**Builds on (SHIPPED):** `plans/schema-validation-final-2026-06-01.md` (15-min embedded-seeded poison-guarded cache, structure-only export validation, recipe live-base check).

---

## 0. What changed from the original draft (why this rewrite)

The original draft was written from a sound investigation but had **four load-bearing errors** the sweep caught, plus it under-specified the three things Karel actually wants. Corrections folded in:

1. **§1 was wrong about today's state.** It claimed `schema.Cache` is the host-correct source. It is **not** — the schema fetch still hardcodes `app-prg1` (`schema.go:14-17`, consumed `cache.go:102-110`). The source that DOES follow `ZCP_API_HOST` today is the very thing we delete (`StackTypeCache`, via the platform client). ⇒ **Phase ordering is correctness-load-bearing, not cosmetic** (§6).
2. **§5 row 1 contradicted the shipped predecessor.** "remove the recipe `liveTypes` fallback" directly opposes `schema-validation-final §3.3` which deliberately KEEPS it (pinned `recipe_validate_test.go:509,548`). Reconciled in §3.2.
3. **"topology covers Category, low risk" was a present-day defect.** 7 shipping storage enum values are misclassified managed→runtime (§3.3, verified by running the real predicates over the full embedded enum).
4. **"keep `LatestManagedVersion` as-is" was wrong.** It silently no-ops against the composite-only live schema (§3.3).

The original §5 migration map was also incomplete (omitted the cache OWNER, two `cache.Get` sites, and all signature plumbing — §5 here is the corrected, compile-complete list).

---

## 1. The single resolution policy (the target — zero overlap, no hidden fallback)

Every "what does this Zerops instance accept?" question gets **exactly one owner**. No question is answerable by two sources.

| Question | Single owner | Notes |
|---|---|---|
| Does service type / build base / run base exist? | **schema** (live, host-derived; embedded floor for cold-start only) | composite-aware matching (§3.3) |
| Latest managed version for a base | **schema** (`recipe_version_latest.go`, fed from `schemas.ImportYml.ServiceTypes`) | needs canonicalization fix (§3.3) |
| YAML structure (fields, required, stable enums) | **schema** (structure-only compiled — already shipped) | export/launch |
| Is type managed / utility / runtime? | **topology** (static Layer-2 vocabulary) | + coverage pin (§3.3, §8) |
| Is a real deploy / import valid? | **platform API** (`ValidateZeropsYaml` / `ImportServices`) | the AUTHORITY; no local fallback — confirmed `import.go:75-78`, `deploy_validate_api.go:69-75` |

```
DELETE entirely:  ops.StackTypeCache  +  client.ListServiceStackTypes  +  the catalog struct
                  platform.ServiceStackType{Name,Category,Versions[]{Name,IsBuild,Status}}
KEEP untouched:   per-running-service type metadata (ServiceTypeInfo on a ServiceStack,
                  BuildInfo.ServiceStackTypeVersionID, ValidateZeropsYamlInput.ServiceStackType*,
                  IsSystem()) — ~50 production readers across deploy/discover/verify/subdomain/env/adopt
DROP:             version Status / deprecation labels (the only API-unique consumed field; §2)
```

This collapses **5 overlapping sources → 2** (live schema + its embedded floor) + the platform authority. One host (`ZCP_API_HOST`), one TTL (15 min), one owner per question.

---

## 2. The API-need verdict (Karel's central question, answered to the field)

**Question:** *when do we still need the authenticated stack-types API, for what, and what does it uniquely bring at each current use site?*

**Answer: nowhere. Every field ZCP actually consumes is schema- or topology-derivable. Zero genuine blockers.** Per-field ledger (full evidence + every file:line in the impl-map appendix):

| API field | Consumed where (decision it drives) | Replacement | Verdict |
|---|---|---|---|
| `Versions[].Name` | version lists, base existence, latest-version | schema `run.base`/`build.base` enums + `ServiceTypes` | **schema** ✔ |
| `Category` (display grouping) | `FormatStackList/ServiceStacks/VersionCheck` 4-bucket grouping (`versions_format.go:31`, `briefing.go:37,102`) | static topology predicates (4 buckets) | **topology** ✔ |
| `Category` (managed-detection) | `ManagedBaseNames` (`versions.go:58-74`) → mode-defaulting + HA validation (`validate.go:253,396`) + adopt pairing (`adopt.go:156`) | `topology.IsManagedService` (already the fallback at `validate.go:235`) | **topology + fix + pin** (§3.3) |
| `Versions[].IsBuild` | nothing — **zero production reads** (only written `zerops_mappers.go:91`) | — | **dead, free delete** ✔ |
| `Versions[].Status` (ACTIVE/DISABLED/DEPRECATED) | display filter only (`versions.go:65,93`, `versions_format.go:58,106,176,208`); **never a gate** — no path blocks a plan/deploy/import on Status | live schema is **already curated** (deprecated versions absent: `nodejs@16`, `php@8.1`, `postgresql@14`, DISABLED `nodejs@18` all dropped); platform is the deprecation backstop | **drop** (§3 product decision) ✔ |
| `defaultServiceStackVersion(Id)`, `releaseDate`, `updateUrl` | **no ZCP consumer** (not even mapped) | — | **dead** ✔ |

Corollary the draft missed: the API **cannot** replace the schema for build bases — its BUILD category is only `*/build_runtime`, build bases live as version names brute-scanned across types (`recipe_validate.go:91-94,113-137`). The schema's dedicated `build.base` enum (191 embedded) is irreplaceable. So the direction is forced: **schema is the single source, the API is pure subtraction.**

**The real work is not sourcing data — it is three shape-mismatch fixes (§3.3) + honest fallbacks (§3.2) + host-correctness (§3.1).**

---

## 3. The three correctness requirements (Karel's goals as first-class design)

### 3.1 Goal 1 — everything follows `ZCP_API_HOST` (host-correctness)

Today the schema fetch is the **only** host that ignores `ZCP_API_HOST`. After this change, no client-side source hardcodes a host.

- **Add** `const CanonicalAPIHost = "api.app-prg1.zerops.io"` + `func URLs(apiHost string) (zeropsURL, importURL string)` mirroring `platform.resolveEndpoint` normalization (preserve explicit scheme, default https when none, keep port, trim trailing `/`, empty→canonical). **Byte-exact default:** `URLs("")` returns the two current const strings verbatim. Replicate `resolveEndpoint`'s ~6 lines in-package (schema must not import `platform`).
- **Thread the host:** `FetchSchemas(ctx, host)`, `FetchRawSchemas(ctx, host)`, `NewCache(ttl, host)`; `Cache.Get` fetches via `URLs(c.apiHost)`. Remove the hardcoded consts.
- **Server caller:** `server.go:140` → `schema.NewCache(DefaultCacheTTL, s.authInfo.APIHost)` (`authInfo` non-nil by construction; empty→canonical via the builder).
- **Recipe + workflow inherit for free** — `recipeStore.SetSchemaProvider` (`server.go:174`) and `RegisterWorkflow` (`server.go:187`) close over the SAME `schemaCache`. **Verified:** threading the host into `NewCache` alone propagates to recipe base-validation + workflow recipe-plan validation; no separate fetch bypasses it.
- **Split (load-bearing):** runtime cache = the user's `auth.APIHost`; `schema sync`/`check` (writing the COMMITTED embedded floor + `active_versions.json`) = **pinned `CanonicalAPIHost`** — committed artifacts are shared repo references, must not vary by whoever's `ZCP_API_HOST` ran the sync. Add the CLI testability seam (`schemaSync(host,paths) error` / `schemaCheck(host) (report,error)`) so the canonical pin is unit-testable without network/repo-writes.
- **Scope, stated honestly:** this regionalizes runtime **enum** validation. STRUCTURE stays canonical-embedded (a PRIVATE instance diverging *structurally* is out of scope — public regions share structure, only enum VALUES differ).

### 3.2 Goal 2 — no schizophrenia, no hidden fallback (single resolution, honest skips)

Three silent fallbacks exist today; each becomes explicit.

1. **Recipe `liveTypes` fallback (the §5-row-1 contradiction).** `ValidateRecipePlan(plan, liveTypes, schemas)` prefers `schemas`, falls back to `liveTypes`. In production `schemas` is **always non-nil** (embedded-seeded `cache.go:44`) ⇒ the `liveTypes` branch is **already dead in prod**, kept alive only by tests calling `(plan, liveTypes, nil)` (`recipe_validate_test.go:509,548`). **Resolution: drop the `liveTypes` parameter entirely** — `ValidateRecipePlan(plan, schemas)`. Rewrite the two tests to drive on `schemas` (the honest source). This removes the pretend-fallback AND satisfies the predecessor's intent (the predecessor kept it only because the cache work hadn't yet guaranteed non-nil — now it has, the reconciliation is "delete, the reason to keep it is gone").
2. **Embedded floor masquerading as live.** When the live fetch fails, the cache serves the embedded seed. Per the predecessor principle "schema down = platform down = nothing works," this only matters at cold-start before the first fetch. **Resolution:** the floor is legitimate for *existence* checks (the embedded enum is a real Zerops schema), so no per-call "this is stale" banner is warranted — BUT the **composite/bare divergence** (§3.3) is what made the floor give *different answers* than live. Fixing canonicalization makes embedded-floor and live produce **identical** accept/reject for any authored form ⇒ the schizophrenia disappears structurally, not by labeling.
3. **Bootstrap has NO schema source at all.** `ValidateBootstrapTargets` (`validate.go:247`) takes `liveTypes` ONLY — no `schemas` arg, no plumbing. The draft's §5 said "→ schema-derived" as if repointing; in reality Phase 3 must **ADD** a schema-catalog source to bootstrap, then delete the `liveTypes` feed. Not a repoint — new plumbing.

Net: one source per question (§1), the only "fallback" is the embedded floor which — once canonicalization lands — is answer-identical to live, so nothing is hidden.

### 3.3 Goal 3 — composite-aware, bare-fallback (the canonicalization contract)

**Ground truth.** Zerops moved to composite identifiers in the 2026-05-18 Sunday release: runtimes `<os>/<base>@<ver>` (`alpine/nodejs@22`), managed `<base>:<mode>@<ver>` (`postgresql:single@18`); legacy bare forms stay API-accepted. ZCP already has `topology.CanonicalBareForm` / `TypesAreEquivalent` (`type_equivalence.go`) for exactly this. The embedded `run.base` enum carries **both** forms (160 composite + 101 bare = 261); the **live curated schema is composite-only** for managed (`postgresql:single@18` present, bare `postgresql@18` absent).

**The problem.** The schema catalog sets do **plain `@`-split, not canonicalization** (`baseNameSet` `schema.go:135` → `strings.Cut(v,"@")`; `ServiceTypeSet` exact-match `schema.go:130`). So the SAME method returns different membership depending on which schema seeded it (`bun` vs `alpine/bun`). **Timing (Codex):** the cache already fetches live on first use (embedded seed is only the cold-start floor), so these exact-match bugs are **partly active TODAY on prg1** — Phase 1 host-derive only widens the exposure to every host; it does not introduce them. Five concrete match-sites break against the composite-only live schema:

- **`latestManagedVersion` (`recipe_version_latest.go:24`)** cuts at `@` then compares — `postgresql:single@18` → `postgresql:single` ≠ `postgresql` → **skipped**. Verified: returns `""` for composite input, silently disabling the use-latest rule.
- **`validateBuildBases` (`recipe_validate.go:106`)** checks authored bare `php` against `BuildBaseSet()` whose live keys are `alpine/php` (no bare `php`) → **false-reject**.
- **`schema.CheckZeropsBasesLive` (`validate_bases.go:28,53`)** — validates authored recipe `zerops.yaml` bases against the **exact full-value** run/build sets → bare authored base false-rejected against composite-only live. (Codex #5 — missed in the draft.)
- **`knowledge.FormatVersionCheck` (`versions.go:84,97,130`)** — builds `activeVersions`/`baseToVersions` from raw names then exact-checks the requested string → bare `nodejs@22` reports "unknown" when fed composite. (Codex #6 — missed in the draft.)
- **`CanonicalBareForm` bug (`type_equivalence.go:41-52`):** `stripModeSuffix` requires an `@` after the `:`, so `shared-storage:ha` (no version) stays unchanged; and it doesn't normalize the no-hyphen aliases (`objectstorage`, `sharedstorage`).

**The contract (apply everywhere a type/base is matched or version-derived):**

1. **Fix storage-alias handling at the ROOT (one normalization, not 4 parallel list edits).** The full-enum audit (333 types, real predicates — §8) shows the storage cluster fails in TWO ways: (i) `objectstorage`, `sharedstorage`, `seaweedfs` (+ `:ha`/`:single`) are not detected managed at all (`managedServicePrefixes` is hyphen-only); (ii) ALL `:mode` storage incl. the correctly-hyphenated `shared-storage:ha` canonicalize wrong (`stripModeSuffix` needs an `@` after the `:`, so `shared-storage:ha` → `shared-storage:ha` unchanged). The lazy fix (add aliases to `managedServicePrefixes`) is a trap: `IsObjectStorageType`/`IsSharedStorageType` ALSO prefix-match hyphen-only, so `objectstorage` would then pass `IsManagedService` but still fail `IsObjectStorageType` → `ServiceSupportsAutoscaling` wrongly returns `true`. Four parallel prefix lists = the exact drift this plan removes. **Fix (IMPLEMENTED):** a storage-specific normalizer `topology.canonicalStorageKind` (maps `objectstorage`→`object-storage`, `sharedstorage`/`seaweedfs`→`shared-storage`, stripping `@version`+`:mode` only to test membership against the closed storage-base set) that every storage predicate (`IsManagedService`, `IsObjectStorageType`, `IsSharedStorageType`) routes through, plus a symmetric `topology.CanonicalBaseName` for keying. NOTE — `CanonicalBareForm`/`stripModeSuffix` are deliberately LEFT conservative (NOT changed to strip `:mode` without `@` as an earlier draft proposed): `type_equivalence_test.go` pins `CanonicalBareForm("foo:bar")=="foo:bar"`, so generic stripping would regress unknown-shape handling. Storage knowledge lives in the storage normalizer, not the generic canonicalizer. Fixing it once corrects the second-order consumers for free (`RuntimeClassFor`, `ServiceSupportsMode`, `ServiceSupportsAutoscaling`, `IsDeferredStart`). (MANDATORY regardless of §8.) **Note — present-day bug, not only latent:** `RuntimeClassFor` is called directly (not via API `Category`) in envelope rendering / deploy classification / subdomain gating, so a running `shared-storage`/`seaweedfs` service is misclassified `RuntimeDynamic` TODAY. **Raw storage matchers OUTSIDE topology must also route through the shared normalization (Codex #7), not keep their own hyphen-only `HasPrefix`** — else the same drift persists in four more places: `workflow.serviceTypeKind` (`recipe_service_types.go:108`), `workflow.contractKindForType` (`symbol_contract.go:260`), `bundle.RulesForType` (`ops/bundle/rules.go:36`, longest-prefix on hyphenated keys — needs the aliases or to call `topology.IsObjectStorageType`/`IsSharedStorageType`), `tools.isManagedNonStorage` (`workflow_checks.go:260`). The full-enum audit ran only topology's own predicates and so did NOT surface these — they are real parallel storage-classifiers.
2. **Catalog membership is equivalence-based, not plain map lookup.** A `Catalog.HasServiceType(t)` / `HasRunBase(b)` / `HasBuildBase(b)` canonicalizes both the query and the schema keys via `CanonicalBareForm` before comparing (the API path already does this via `topology.TypesAreEquivalent`, `validate.go:466` — bring the schema path to parity). Accepts composite (new) AND bare (fallback) → identical answer on embedded floor and live. **`schema.CheckZeropsBasesLive` MUST route through `HasBuildBase`/`HasRunBase`** (Codex #5), not the raw exact-value sets it uses today.
3. **`latestManagedVersion` canonicalizes before the `@`-cut** + add a composite-form test case.
4. **`knowledge.FormatVersionCheck` canonicalizes both catalog values and the requested string** (or compares via `TypesAreEquivalent`) so bare authored input matches composite catalog values (Codex #6).

Result: an LLM authoring `nodejs@22` (bare) or `ubuntu/nodejs@22` (composite) is accepted identically, against either schema — goal #3 met, goal #2's schizophrenia removed at the root.

---

## 4. Target architecture — the schema-derived catalog

A thin catalog DERIVED from the parsed `*schema.Schemas` (already live-host + embedded-seeded), replacing everything `StackTypeCache` provided minus Status.

**Surface (equivalence-aware per §3.3):**
- `HasServiceType(t)` / `ServiceTypes()` — over `ImportYml.ServiceTypeSet()`.
- `HasRunBase(b)` / `HasBuildBase(b)` — over `RunBaseSet()`/`BuildBaseSet()`.
- `LatestManagedVersion(base)` — keep `recipe_version_latest.go`, + canonicalization fix.
- `ManagedBaseNames()` — schema type list filtered by `topology.IsManagedService` (§8).
- `GroupForBriefing()` — schema type list bucketed via static topology predicates (4 buckets: runtime / managed / shared-storage / object-storage).

**Placement (§9.1 resolved):** the existing `internal/catalog` package is **live and narrow** (~80 LOC, only the `active_versions.json` projection: `SnapshotFromSchemas`/`WriteSnapshot`, consumed by `cmd/zcp/schema.go`). The new catalog is a pure projection of `*schema.Schemas` and workflow consumers already import `schema`. ⇒ **Fold the new catalog into `internal/schema`** (methods on `*Schemas` or a `schema.Catalog` type). No collision; the `active_versions` projection stays where it is.

**Cleanup while here:** `ImportYmlSchema.Modes` (`schema.go:42,98`) is genuinely dead — the `services[].mode` node has no enum (`type:string`+description), so `extractEnum` returns nil → always empty. Delete the field + its `schema_test.go` reference (follow-the-chain). `location`/`cpuMode` enums exist in JSON but aren't extracted — leave unless a consumer needs them.

---

## 5. CORRECTED consumer migration map (compile-complete — the draft omitted the bold rows)

| Consumer / artifact | File:line | Today | Becomes |
|---|---|---|---|
| **catalog OWNER** | `ops/context_cache.go` (whole), `server.go:139,187,189`, `client.go:84` (iface+impl+mock), `types.go:394-406`, `zerops_mappers.go` mapper | `StackTypeCache` + `ListServiceStackTypes` | **DELETED** |
| recipe plan validation | `recipe_validate.go:20,76-137,192-210` | schema-preferred + `liveTypes` fallback | schema-only; drop `liveTypes` param (§3.2.1) |
| **bootstrap validation (no schema today)** | `validate.go:247,253,229-236,396-403,463-466` | `liveTypes` ONLY | **ADD** schema-catalog source, then drop `liveTypes` (§3.2.3) |
| adopt pairing | `adopt.go:156` | `ManagedBaseNames(liveTypes)` | schema-derived `ManagedBaseNames` |
| knowledge briefing | `versions.go:58-74,89`, `versions_format.go:19,31,102,116,176,203,208`, `briefing.go:37,102` | `Category`/`Status`/`IsBuild` filtering | schema type/version lists, topology grouping; drop `Status` (§7) |
| `ManagedBaseNames` | `versions.go:58-74` | API `Category`+`Status` filter | schema type list + `topology.IsManagedService` |
| **tools cache.Get sites (2 omitted)** | `workflow.go:421`, `workflow_recipe.go:93,97` (Aleš-adjacent) | `StackTypeCache.Get` / `schemaCache.Get` | catalog; `workflow_recipe.go` already dual-source |
| **tools `*ops.StackTypeCache` plumbing (Codex #2 — compile-blockers)** | `workflow.go:301,348,519,893,907,939`, `RegisterKnowledge` `knowledge.go:83` | `*ops.StackTypeCache` params/fields | remove the param/field; Phase 5 leaves dangling type refs otherwise |
| bootstrap handlers | `workflow_bootstrap.go:59,73,263` (`populateStacks`, `BootstrapCompletePlan`/`AdoptPlan`) | `liveTypes` | catalog |
| `zerops_knowledge` tool | `knowledge.go:157` | `FormatStackList(liveTypes)` | catalog |
| **signature plumbing** | `engine.go:540,560`, `engine_recipe.go:283`, knowledge `Provider.GetBriefing` `engine.go:28` | `liveTypes`-shaped params | decide: drop param vs feed schema-derived list (§8 sub-decision) |
| e2e knowledge-quality | `e2e/knowledge_quality_test.go:493` | `ListServiceStackTypes` + `activeVersionSet` | schema / `active_versions.json` |
| **test-fixture migration** | `versions_test.go:29`, `versions_bc_test.go`, `engine_briefing_test.go`, `validate_test.go`, `recipe_validate_test.go` | `[]ServiceStackType{Category,Status}` literals | Status-free schema-derived shape |
| deploy pre-validation | `deploy_validate_api.go:69-75` | live service's own metadata + platform | **UNCHANGED** (category-B metadata, not catalog) |
| stale comments (cleanup-on-delete) | `zerops_validate.go:29`, `import.go:75` | name `ListServiceStackTypes`/`StackTypeCache` | update/remove |

**No false-positive rows:** `import.go` (tools) does NOT consume the catalog struct (delegates to platform `import.go:75-78`) — correctly absent from the delete-internals list, present only for a stale comment.

---

## 6. Migration phases (corrected ordering — each compiles + all layers green; TDD)

**Phase 1 — host-derive the fetch (HARD GATE before Phase 5).** §3.1. Without it, deleting `StackTypeCache` (the only host-correct source today) regresses non-prg1 users to prg1 validation. RED: `TestURLs` (normalization matrix incl. `http://localhost:8080/` scheme-preserve, byte-exact empty default), `TestNewCache_UsesAPIHost`, `TestSchemaCLIPinsCanonical`. Pin: every fetch routes through `URLs` (no inline URL literal). **Do NOT release Phase 1 alone (Codex #3):** host-deriving the live fetch widens the composite/bare exact-match exposure (§3.3) to every host — Phase 2 canonicalization MUST land in the same release. Consider landing Phase 2 first (it's safe on the embedded floor and fixes the partly-present bug), then Phase 1.

**Phase 2 — schema-derived catalog + canonicalization fixes.** §3.3 + §4. **This is a correctness fix for a partly-present-today bug (§3.3 timing), not just catalog-prep.** Fix `CanonicalBareForm`/`stripModeSuffix` (storage suffix + no-hyphen aliases) + the four out-of-topology storage matchers (§3.3.1, Codex #7). Build the equivalence-aware catalog surface on `*schema.Schemas`; route `CheckZeropsBasesLive` (Codex #5) through it. Fix `latestManagedVersion` (canonicalize before `@`-cut). **Build `ManagedBaseNames` (schema+topology) here** so its consumers (Phase 3) repoint onto an existing symbol — the draft sequenced this backwards. RED: composite-form acceptance, bare-form acceptance, the 7 storage types classified managed, `latestManagedVersion` non-empty on composite input, `CheckZeropsBasesLive` accepts a bare base against a composite-only set. No consumer changes yet.

**Phase 3 — repoint validation consumers.** recipe (`recipe_validate.go`: drop `liveTypes` param), **bootstrap (ADD schema plumbing** to `validate.go` + `engine.go`/`workflow_bootstrap.go`), adopt. **Test budget (Codex #4): `recipe_validate_test.go` has MANY 3-arg `ValidateRecipePlan(plan, liveTypes, …)` direct calls, not just the two at `:509,:548`** — all must be rewritten to the 2-arg `(plan, schemas)` shape; budget the full sweep, not two edits. Recipe + bootstrap flow-evals as the no-break gate. Recipe is Aleš-adjacent (`workflow_recipe.go`) — coordinate.

**Phase 4 — repoint knowledge/briefing.** `ManagedBaseNames` consumers, `FormatStackList`/`ServiceStacks`/`VersionCheck` to schema+topology grouping; drop `Status`. **Canonicalize `FormatVersionCheck` lookups (Codex #6)** so bare requested input matches composite catalog values. **Migrate Status/Category test fixtures** (§5). Flag the [B]-marker resurrection (§7).

**Phase 5 — delete `StackTypeCache` + the API path.** Remove `ops.StackTypeCache`, `ListServiceStackTypes` (client iface + impl + mock + `zerops_search.go`), the `ServiceStackType{Name,Category,Versions}` catalog struct + its mapper. Confirm nothing imports them. Per-service `ServiceTypeInfo`/`*VersionID` metadata UNTOUCHED. Won't compile unless §5 plumbing rows are all done.

**Phase 6 — docs / invariant / cleanup.** Update the CLAUDE.md schema bullet + `docs/schema-integration.md` + the `cmd/zcp/check/yml_schema.go:19-21` comment. Add the topology coverage-pin (§8). Add an invariant pin: client-side catalog questions route through the schema catalog (no `StackTypeCache`). `git mv` this plan + the absorbed host plan + the impl-map to `plans/archive/`.

---

## 7. Genuine behavior changes (stated honestly — not silent)

1. **Dead `[B]` build-capable markers resurrect.** Today the `[B]` marker + "Build-only:" briefing section are **already dead** against the live API (BUILD category = only `*/build_runtime`, zero run-version matches). A schema `build.base`-driven re-derivation would emit **real** `[B]` markers — a net improvement but a visible change. **State as intended; flow-eval-check it.**
2. **Briefings lose ACTIVE/DEPRECATED labels.** Dropping `Status` means the schema's (already-curated) versions list without explicit deprecation labels. Mitigated: schema is pre-curated + "use latest" rule + platform backstop. Accepted per §3.
3. **Managed-detection lag for brand-new types.** Moving from live `Category` to static topology means a brand-new managed *service type* isn't detected as managed until a ZCP update — made HONEST + LOUD by the §8 coverage pin (a schema refresh adding an unclassified type fails the build).

---

## 8. Managed-detection approach — DECIDED (b), scope bounded by a full-enum audit

**Decision (krls2020): (b) — fix the topology classifier + add a coverage-pin test.** Not (c): a probe showed the schema carries no clean managed discriminator, only a fragile `allOf` branch grouping.

**Full-enum audit (real topology predicates over the embedded schema — authoritative):**

```
import types: 333  (managed=65  utility=0  runtime=268)   run.base: 261   build.base: 191
A. storage-ish NOT classified managed (BUG):  7  (objectstorage, sharedstorage[:ha|:single], seaweedfs[@3|:ha@3|:single@3])
   → each: RuntimeClassFor=Dynamic (should be Managed), supportsMode=false, supportsAutoscaling=true (all wrong)
B. CanonicalBareForm leaves ':' decoration (BUG): 4  (shared-storage:ha|:single, sharedstorage:ha|:single)
C. composite NOT equivalent to its bare form:   0   ← all 268 runtimes are FINE
D. managed type whose RuntimeClassFor != Managed: 0
F. run.base / build.base canonical leftover:     0   ← all bases FINE
```

**Conclusion — the ONLY topology-vs-schema gap is the storage cluster.** All 268 runtimes classify, canonicalize, and compare-equivalent correctly; both base enums are clean. `utility=0` because `mailpit` isn't in the import enum (utility is a recipe-app concept, not schema-anchored — the pin covers managed/runtime, not utility). So §3.3.1's one-normalization fix is the complete classification fix; there is no other hidden gap.

**The coverage pin = this audit turned into assertions.** It loops the embedded `services[].type` enum through the predicates and asserts the storage cluster is managed + every type's canonical form is decoration-free. **When it triggers:** it's a `go test` (not `go build`) — fires locally, in the commit/CI tier, and decisively right after `make schema-sync` pulls a new enum. A new managed type Zerops adds → red CI → forced topology update. The lag becomes loud, never silent (goal #2).

**Sub-decision (lean drop, confirm in Phase 3):** the engine signatures (`BootstrapCompletePlan`/…/`Provider.GetBriefing`) drop the `liveTypes`-shaped parameter rather than feed it a schema-derived list — one source, no re-divergence seam.

---

### Historical: the three options considered (the storage fix + §3.3.1 are needed under ALL):

| Option | What | Trade-off |
|---|---|---|
| **(a)** add `objectstorage`/`sharedstorage`/`seaweedfs` prefixes to `managedServicePrefixes` | minimal | brittle — re-breaks for the next storage backend; no self-healing |
| **(b)** topology classifier + **coverage-pin test** *(recommended)* | fix the lists (§3.3.1) + a test running `IsManagedService`/`IsRuntimeType` over the FULL embedded `services[].type` enum, asserting every type is classified; a schema refresh adding an unclassified type **fails the build** | self-healing-enough: lag becomes a loud build failure at `schema sync`, not a silent runtime wrong-answer (serves goal #2 directly). Keeps classification in the Layer-2 vocabulary where it belongs |
| **(c)** derive managed-ness **structurally from the schema** | classify from the `services.items.allOf` branch a type falls in (branch 1 = runtime/autoscaling, branches 3-9 = managed/storage) | **PROBED: feasible but couples ZCP to upstream allOf branch layout** — the branches group by scaling-knob applicability, not a clean managed required-field discriminator, so it's brittle in a *different* way than the list. Codex concurred: reject (c). |

---

## 9. Verification

- `go test ./... -short` + `-race` on touched packages; `make lint-local`.
- Per-phase flow-eval (container): a bootstrap scenario (`api-node-postgres-classic-dev`) + a recipe scenario — prove the schema-derived catalog drives validation + briefings with no `StackTypeCache`, and that composite + bare authored bases both validate.
- Live: `zcp schema check` (canonical) still works; a non-default `ZCP_API_HOST` makes runtime validation hit that host (Phase 1).
- Pin: catalog questions produce the same accept/reject as the old `StackTypeCache` path for current platform data (minus deprecation labels), for BOTH composite and bare input.
- Coverage pin (§8): `IsManagedService`/`IsRuntimeType` over the full embedded enum.

---

## 10. Risks

- **Sizeable refactor** across bootstrap + knowledge + workflow + recipe (Aleš-adjacent). Sequence so `internal/schema` (Phase 1-2) lands first; consumer repoints (Phase 3-4) are flow-eval-gated.
- **Composite/bare is the sharp edge** — §3.3 is the highest-risk work; the canonicalization contract must be applied at EVERY match site or a consumer silently no-ops/false-rejects. The Phase-2 RED tests must cover composite explicitly.
- **Private-instance structural divergence:** out of scope (§3.1) — structure stays canonical-embedded.

---

## 11. One-line summary

The **live host-derived schema (+ embedded floor) is the single client-side source** for type/base existence + structure + latest-version; **topology** is the single source for managed/runtime/utility classification (with a build-time coverage pin); the **platform API** remains the authority for real deploy/import; **`StackTypeCache` + the stack-types API are deleted** — their only consumed unique field (`Status`) is a nice-to-have the already-curated schema makes redundant — and the whole thing is made **composite-aware with bare fallback** by applying `CanonicalBareForm` equivalence at every match site.

---

## 12. Codex verifier verdict — `GO-WITH-REVISIONS` (2026-06-02, all folded)

Codex (adversarial, read the source) confirmed the architecture + every decision: API fully deletable with delete-scope correct (per-service metadata spared), Phase 1 a hard prerequisite, (b)-over-(c) agreed, `import.go` correctly excluded as a false-positive, deploy path unchanged, backward-compat safe (internal plumbing only — no on-disk user seam migrates). **Confirmed** facts: `IsBuild` mapped-never-read (`zerops_mappers.go:89`), `Status` filter/display-only, the deletable family at `types.go:394`.

Five revisions it surfaced — all folded above:
1. **§5 not compile-complete** → added the `*ops.StackTypeCache` tools plumbing rows (`workflow.go:301,348,519,893,907,939` + `RegisterKnowledge knowledge.go:83`).
2. **Composite contract missed `CheckZeropsBasesLive`** (`validate_bases.go:28,53`) → §3.3 + Phase 2 route it through `HasBuildBase`/`HasRunBase`.
3. **Composite contract missed `FormatVersionCheck`** (`versions.go:84,97,130`) → §3.3 item 4 + Phase 4 canonicalize lookups.
4. **Storage normalization under-scoped** → §3.3.1 now names the four out-of-topology storage matchers (`serviceTypeKind`, `contractKindForType`, `bundle.RulesForType`, `isManagedNonStorage`) that must route through the shared normalization.
5. **Recipe test budget understated** → Phase 3 notes the full `recipe_validate_test.go` 3-arg-call sweep, not two edits.
Plus the sequencing refinement: don't release Phase 1 alone — Phase 2 canonicalization ships with it (or first), since the live fetch makes the exact-match bugs partly active TODAY (§3.3 timing).
