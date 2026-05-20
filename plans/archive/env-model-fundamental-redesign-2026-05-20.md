# Plan — Env-var lifecycle fundamental redesign

**Date:** 2026-05-20
**Status:** Proposed (pending Karel review)
**Prior work:** `plans/audit-env-vars-20260515/` (Phase A: dbName-completeness warning landed; reserved-name pinning landed; this plan extends to the structural axis)
**Trigger:** 39 behavioral evals 5/18-5/20 + transcript trace (60 discover/env calls) + SDK audit revealed ZCP discards 70%+ of platform env metadata at internal abstraction boundaries.

---

## 1. Why fundamental, not patches

Five prior fix attempts at the leaf (connectionString warning, atom rewrites, reserved-key list, platform-internal denylist, classifier bias internalization) treated each friction as independent. Transcript + SDK + atom audit show they're **one structural pattern**:

```
SDK output.*  (Type, Sensitive, Editable, Created, LastUpdate, UserData embedded,
   │           ConnectedStacks, HasUnsyncedUserDataRecord, ProjectEnvType USER/SYSTEM)
   │
   │ ─── DROP at platform/types.go mapEsServiceStack (UserData + ConnectedStacks + Unsynced* discarded)
   ↓
ops.ServiceStack  (10 fields kept from 26)
   │
   │ ─── DROP at ops/helpers.go envVarsToMaps via EnvAccessor (Type, Sensitive, Editable, Created stripped)
   ↓
discover JSON envs[] entries  (4 fields per env: key, value?, isReference?, isPlatformInjected?)
   │
   ↓ Agent sees keys only; reconstructs Type / Sensitive / origin from name pattern matching
```

**Every layer subtracts. None adds back.** Then atoms reinvent the dropped knowledge in prose ("zeropsSubdomainHost is infrastructure", "APP_KEY matches _KEY suffix → auto-secret bias", "connectionString omits dbName"). And ZCP's *own* internal classifier (`envclass.ClassifyProjectEnv`) computes the bias agents need but never emits it. Plus the hardcoded `platformInternalKeys` denylist in `env_generate.go` is a parallel reality to `Type=SYSTEM` — drift risk every time Zerops adds a system env.

Result of those subtractions, validated empirically across 60 transcript calls:

- 6 launch-production runs paid 2-call discover dance (service-scoped omits project envs)
- 100% of classify-prompt agents re-derived bias by name pattern that ZCP already computes
- 15/15 develop-loop agents pattern-matched managed-service prefix from hostname instead of platform truth
- `isReference: true` annotation: **0 explicit consumes** across 60 calls (annotations agents don't read are dead fields)
- 28/40 transcripts in dump-discover-noise (zcp service envs like `VSCODE_PASSWORD`, `ZCP_AGENT_TYPE` appear in classify-relevant queries)

**This plan removes the boundary drops, surfaces SDK truth all the way to discover response, and deletes the prose reconstructions that compensate.**

---

## 2. Mental model — Where envs live

```
                ┌──────────────────────────────────────────────┐
   PLATFORM     │  ProjectEnv  {Type=USER|SYSTEM, Sensitive,    │
   AUTHORITATIVE│                Editable, Created, LastUpdate} │
                │  ServiceEnv  {Type=READ_ONLY|EDITABLE|SECRET| │
                │                INTERNAL|ENV, Sensitive, ...}  │
                │  ServiceStack.UserData embedded               │
                │  ServiceStack.ConnectedStacks                 │
                │  ServiceStack.HasUnsyncedUserDataRecord       │
                └──────────────────────┬───────────────────────┘
                                       │ injected at container start
                                       ▼
                ┌──────────────────────────────────────────────┐
   RUNTIME      │  os.environ inside container                  │
   CONTAINER    │   ↑ project envs (auto-injected, every svc)   │
                │   ↑ service envs (this service's own)         │
                │   ↑ run.envVariables from setup block         │
                │   (service > yaml > project precedence)       │
                └──────────────────────────────────────────────┘

   USER-FACING SOURCES (write paths):
   ┌───────────────────────────────┬───────────────────────────────┐
   │  Project envs                  │  Set by:                      │
   │   • USER (Type=USER)           │   - zerops_env set proj=true  │
   │     auto-secret / external-    │   - import yaml project.envVariables│
   │     secret / plain-config      │   - launch-production at create│
   │   • SYSTEM (Type=SYSTEM)       │   - platform auto-emit          │
   │     CDN URLs, *Isolation,      │                                │
   │     zeropsSubdomain*           │                                │
   ├───────────────────────────────┼───────────────────────────────┤
   │  Service envs                  │  Set by:                      │
   │   • READ_ONLY/INTERNAL/ENV     │   - managed service generates  │
   │     db_user, db_password, ...  │   - platform injects (zerops*) │
   │   • EDITABLE/SECRET            │   - zerops_env set svcHost=X   │
   │                                │   - zerops.yaml run.envVariables│
   ├───────────────────────────────┼───────────────────────────────┤
   │  zerops.yaml setup blocks      │  Authored in repo:            │
   │   • build.envVariables         │     compile-time only          │
   │     (NPM_TOKEN, MAVEN_CREDS)   │     NOT visible at runtime!   │
   │   • run.envVariables           │     renames + per-setup literals│
   ├───────────────────────────────┼───────────────────────────────┤
   │  .env.local                    │  Per-developer (local only):  │
   │   highest precedence in        │   user-authored, gitignored,   │
   │   generate-dotenv output       │   ZCP never overwrites         │
   └───────────────────────────────┴───────────────────────────────┘
```

### Cross-service refs

`${hostname_varname}` in any `run.envVariables` value. Resolution timing:
- **Local dev** (`generate-dotenv`): resolved by ZCP via API calls per ref-target service
- **Deploy time** (container start): resolved by Zerops template interpolator
- **Build time**: NOT resolved by ZCP, deferred to Zerops builder

Hostname canonicalization: dashes ↔ underscores (`my-db` registers as `${my_db_*}`). Longest-prefix match.

### Self-shadow (`key: ${key}`)

Service-level `key: ${key}` shadows the auto-injected source; interpolator resolves to literal `"${key}"` 8-char string. Pre-flight check at `env_shadow.go::DetectSelfShadows`. Strict rule: value must be EXACTLY `${key}` (whitespace-trimmed). Substring (`postgres://${db_hostname}/app`) is legitimate.

### Reserved-key regimes

Three sets, currently in `deploy_validate.go`:

1. **Hard-reserved** (API rejects in any envVariables block): `hostname`, `PATH`, `serviceId`, `projectId`, `appVersionId`, `appVersionName`, `zeropsSubdomain`
2. **Run-scope crash** (passes API check, crashes runtime-init silently — BUILD_FAILED 4-5s zero logs): `HOSTNAME`, `Path`, `path`
3. **Platform-provided overridable** (silently shadows): `apiCdnUrl`, `staticCdnUrl`, `storageCdnUrl`, `envIsolation`, `sshIsolation`, `zeropsSubdomainHost`, `zeropsSubdomainString`

### Per-managed-service catalogs

Each managed-service type exposes a canonical env catalog via SDK (UserData on service stack):

- **Postgres/MariaDB/MySQL**: `connectionString` (no /dbName!), `hostname`, `port`, `user`, `password`, `dbName`, `superUser`, `superUserPassword`
- **Redis/Valkey**: hostname, port, password, connectionString
- **ClickHouse**: `portHttp`, `portMysql`, `portNative`, `portPostgresql` (multi-port), `superUser`, `superUserPassword`
- **Kafka**: hostname, port, no connectionString (broker URL composed)
- **Object storage (S3-compat)**: `apiUrl`, `accessKeyId`, `secretAccessKey`, `bucketName` (NO region)
- **Search (Meilisearch/Typesense/Qdrant)**: scoped API keys (never master)
- **Qdrant**: dual `connectionString` (HTTP) + `grpcConnectionString` (gRPC)

These come from SDK live. ZCP must read them, not duplicate as static knowledge.

### EnvPlan internals (generate-dotenv only)

`internal/ops/env_plan.go` tracks per-key:
- **Source**: `Project | YAMLSetup | LocalOverlay | BrownfieldImport` (precedence ascending)
- **Scope**: `Shared | DeployedRuntime | LocalOverride | ManagedRef`
- **Conflict**: `Clean | Overridden | Shadowed`

Currently INTERNAL — never surfaced in any tool response. Tooling for `.env` rendering only.

### 4-bucket classification (export + launch-production)

`internal/topology/types.go::SecretClassification`:
- **infrastructure** → drops; managed-service ref kept in zerops.yaml
- **auto-secret** → emits `<@generateRandomString(<32>)>` preprocessor
- **external-secret** → emits `<@pickRandom(["REPLACE_ME"])>` placeholder
- **plain-config** → literal value verbatim

`envclass.ClassifyProjectEnv` returns:
- `Drop` for `Type=SYSTEM` (filtered out before agent sees)
- `PromptUser` + `Bias` (`auto-secret` if key matches `(?i)(_KEY|_SECRET|_TOKEN|_PASS|APP_KEY)$`, else `plain-config`) for `Type=USER`

**Today's bias is computed but never surfaced** — agent sees `currentBucket: ""` and re-derives. Confirmed in `workflow_launch_production.go:966` (sets CurrentBucket from existing classifications map, not from bias).

---

## 3. Current gaps — Five dimensions

### Dim A: SDK metadata dropped at `envVarsToMaps`
- `EnvAccessor` interface (`platform/types.go:218`) exposes only GetID/GetKey/GetContent.
- `envVarsToMaps` (`ops/helpers.go:152`) is generic over EnvAccessor → can't see Type/Sensitive/Editable/Created/LastUpdate.
- Same for ServiceEnvVar.

### Dim B: Service-scoped query silently drops project envs
- `ops/discover.go:76-96` early-return when hostname filter set, skips `attachProjectEnvs()`.
- Test asserts as invariant (`discover_test.go:358-381`).
- Atoms (`launch-classify-prompt.md:34`, `export-classify-envs.md:22`) example uses `hostname=` (wrong param name) which silently makes call unscoped — accidentally compensates for the omission. Two bugs cancel.

### Dim C: Redundant per-service `GetServiceStackEnv` calls
- `ListServices` (PostServiceStackSearch) returns `EsServiceStack` with `UserData []UserDataLight` embedded.
- `mapEsServiceStack` (`platform/zerops_mappers.go`) drops UserData.
- `ops/discover.go:106` calls `attachEnvs` (`GetServiceStackEnv`) per service separately. 1+N calls instead of 1.
- ConnectedStacks, HasUnsynced*, IsSystem, ReloadAvailable, CdnEnabled — all dropped similarly.

### Dim D: Internal classifier bias never surfaced
- `envclass.ClassifyProjectEnv` computes `Bias: SecretClassAutoSecret | SecretClassPlainConfig`.
- `launchEnvsForClassifyPrompt` filters to USER-only — drops the bias.
- `workflow_launch_production.go:966` writes `CurrentBucket: classifications[env.Key]` (the agent's prior submission), zero when first seen.
- 13/13 launch-production agents re-derive identical bias by name pattern. Server work duplicated externally.

### Dim E: Parallel-reality denylists + reserved-key catalogs
- `platformInternalKeys` (`env_generate.go:83`) hardcoded list of 8 keys → duplicate of `Type=SYSTEM`.
- `hardReservedEnvKeys` + `runScopeReservedEnvKeys` (`deploy_validate.go:138-156`) → not in topology, not surfaced to agent except via deploy preflight.
- `envKeyZeropsSubdomain = "zeropsSubdomain"` (`helpers.go:136`) + `platformInjectedKeys` hardcoded map of length 1 → near-empty annotation set.
- `credentialPattern` (`envclass/classify.go:62`) regex used only for Bias computation, not exposed for agent's own validation of yaml literals.

Each list lives in a different file with no shared source-of-truth. Adding a new platform-internal env requires editing 3+ places. Multiple existing audit reports flag this drift.

---

## 4. Target shape

### 4.1 Per-env entry (unified across project + service scopes)

```json
{
  "key": "APP_KEY",
  "value": "gClchHmzX...",                  // includeValues=true; redacted if sensitive=true unless includeSensitiveValues=true
  "scope": "project",                        // "project" | "service:<hostname>"
  "origin": "user",                          // "user" | "system"  (collapsed from ProjectEnvType / UserDataTypeEnum)
  "sensitive": true,                         // server-marked (best-effort signal)
  "editable": true,                          // present only when scope=project
  "lastModified": "2026-04-12T08:21:00Z",    // from SDK LastUpdate
  "annotations": {                           // ZCP-derived
    "isReference": false,
    "refTargets": [],                        // ["db", "cache"] for refs
    "selfShadow": false,                     // value EXACTLY ${key}
    "reservedKey": null,                     // null | "hard" | "run-scope" | "overridable"
    "completenessFlags": {"includesDbName": false},  // db connectionString etc.
    "warning": "connectionString omits /${db_dbName}; compose explicitly..."
  },
  "suggestedClassification": {               // null for service envs and project SYSTEM
    "bucket": "auto-secret",
    "confidence": "name-pattern",            // "name-pattern" | "managed-ref" | "value-pattern"
    "rationale": "Key matches credentialPattern (_KEY suffix); auto-secret bias from envclass",
    "alternatives": ["plain-config"]
  }
}
```

### 4.2 envSummary at response root

```json
"envSummary": {
  "project": {
    "totalUser": 5,
    "totalSystem": 7,
    "totalSensitive": 4,
    "withReferences": 1,
    "reservedConflicts": []                  // [{key, regime}] if any project env hits a reserved key
  },
  "perService": {
    "appdev": {
      "totalUser": 2,
      "totalSystem": 12,
      "totalSensitive": 1,
      "withReferences": 3,
      "syncStatus": "in-sync",               // "in-sync" | "pending-apply" | "pending-restart"
      "selfShadowed": []                     // [] | ["db_hostname"]
    }
  }
}
```

### 4.3 Per-service ServiceInfo additions

```json
{
  "hostname": "appdev",
  "serviceId": "...",
  "type": "ubuntu/nodejs@22",
  // existing fields kept
  "connections": ["db", "cache"],            // NEW — from EsServiceStack.ConnectedStacks (ACTIVE only)
  "envSyncStatus": "pending-apply",          // NEW — from HasUnsyncedUserDataRecord
  "managedEnvCatalog": ["hostname", "port", ...]  // NEW — for managed services only; the canonical exposed keys
}
```

`connections` lets agents skip the "pattern-match the db hostname" step for first-deploy compose. `envSyncStatus` surfaces what auto-restart is silently fixing today. `managedEnvCatalog` makes the per-managed-type cheatsheet derivable from live data instead of static atom prose.

### 4.4 Workflow response shape (classify-prompt)

```json
"classifications": [
  {
    "key": "APP_KEY",
    "currentBucket": "",                                  // unchanged (set after agent submits)
    "suggestedBucket": "auto-secret",                     // NEW — from envclass Bias
    "sensitive": true,                                    // NEW
    "value": "gClchHmzX...",                              // NEW — currently agent must call discover separately
    "rationale": "credentialPattern: _KEY suffix on USER project env"
  }
]
```

Eliminates the 2-call discover dance entirely for launch-production + export.

---

## 5. Single source of truth — `internal/topology/env_classification.go` (new)

Consolidate scattered env knowledge:

```go
// Origin classifies env provenance into two LLM-actionable buckets.
type EnvOrigin string
const (
    EnvOriginUser   EnvOrigin = "user"
    EnvOriginSystem EnvOrigin = "system"
)

// FromProjectType maps SDK ProjectEnvType to EnvOrigin.
func FromProjectType(t platform.ProjectEnvType) EnvOrigin { ... }
// FromServiceType maps SDK UserDataTypeEnum to EnvOrigin (READ_ONLY/INTERNAL/ENV → system).
func FromServiceType(t platform.ServiceEnvType) EnvOrigin { ... }

// ReservedKeyRegime tells where the key is rejected/dangerous.
type ReservedKeyRegime string
const (
    ReservedKeyHard       ReservedKeyRegime = "hard"        // API rejects
    ReservedKeyRunScope   ReservedKeyRegime = "run-scope"   // silent runtime crash
    ReservedKeyOverridable ReservedKeyRegime = "overridable" // platform-provided, silently shadowed
)

func ReservedKey(key string) (ReservedKeyRegime, bool) { ... }
// Lists: HardReserved, RunScopeReserved, OverridableReserved (exported for tests)

// CredentialPattern is the regex shared between envclass Bias + agent-facing validation.
var CredentialPattern = regexp.MustCompile(`(?i)(_KEY|_SECRET|_TOKEN|_PASS|APP_KEY)$`)

// SuggestClassification returns the bucket bias for a project USER env.
func SuggestClassification(key string, value string, isManagedRef bool) (SecretClassification, string /*rationale*/) {
    switch {
    case isManagedRef:
        return SecretClassInfrastructure, "value contains ${managed_*} ref"
    case CredentialPattern.MatchString(key):
        return SecretClassAutoSecret, "key matches credentialPattern (_KEY|_SECRET|_TOKEN|_PASS suffix)"
    default:
        return SecretClassPlainConfig, "no credential-pattern match; defaulting to plain-config"
    }
}
```

After this:
- `envclass.ClassifyProjectEnv` becomes a thin wrapper calling topology helpers.
- `deploy_validate.go::hardReservedEnvKeys` + `runScopeReservedEnvKeys` move to topology.
- `env_generate.go::platformInternalKeys` deleted; consumers use `EnvOrigin == EnvOriginSystem`.
- `helpers.go::platformInjectedKeys` map (for `isPlatformInjected: true`) deleted; same `EnvOrigin == EnvOriginSystem` check.

---

## 6. Phased migration — Functional at each step

Each phase keeps the entire test suite green and the live tool surface working. No half-implemented states.

### Phase 1 — Surface platform metadata (additive)

**Files:**
- `internal/platform/types.go`: extend `EnvAccessor` with `GetType() string`, `GetSensitive() bool`, `GetEditable() (bool, bool /*present*/)`. Or **preferred:** drop EnvAccessor and split `envVarsToMaps` into `projectEnvsToMaps` + `serviceEnvsToMaps` (no abstraction, simpler).
- `internal/ops/helpers.go`: rewrite `envVarsToMaps` to emit new fields (`origin`, `sensitive`, `editable`, `lastModified`).
- `internal/topology/env_classification.go` (NEW): EnvOrigin enum + From*Type helpers.

**Tests updated:**
- `internal/ops/helpers_test.go::TestEnvVarsToMaps_*`: assert new fields present.
- `internal/ops/discover_test.go::TestDiscover_*`: assert new fields present in response.

**Result after phase:** Discover response carries metadata. Existing fields unchanged. Atoms unchanged. Agent CAN start reading new fields if it learns to; old behavior fully functional.

### Phase 2 — Surface annotations + envSummary

**Files:**
- `internal/ops/helpers.go`: add `annotations` block to env entries (selfShadow detect inline, reservedKey lookup, completenessFlags for db types — completenessFlags already exist, fold into annotations).
- `internal/ops/discover.go`: compute envSummary post-fetch, add to result.
- `internal/ops/discover.go`: pass live service list to envVarsToMaps so refTargets can be populated (cross-service ref classification per env).

**Tests:**
- New: `TestDiscover_Annotations_*` table covering selfShadow / reservedKey / refTargets.
- New: `TestDiscover_EnvSummary_*` table.

**Result after phase:** `annotations` populated. envSummary in response. Old fields kept for BC during transition.

### Phase 3 — Service-scoped query returns project envs too

**Files:**
- `internal/ops/discover.go:76-96`: remove early-return; `attachProjectEnvs()` called regardless of hostname filter.
- `internal/ops/discover_test.go:358-381`: invert test invariant. New name: `TestDiscover_ProjectEnvs_AlwaysIncludedWhenIncludeEnvs`.

**Result after phase:** Single `zerops_discover service=X includeEnvs=true` call returns BOTH project envs and that service's envs. Launch-classify 2-call dance gone.

**BC risk:** None — additive on response shape. Any consumer iterating service envs from `result.Services[0].Envs` still works.

### Phase 4 — Eliminate redundant per-service env fetch in unscoped discover

**Files:**
- `internal/platform/zerops_mappers.go::mapEsServiceStack`: preserve `UserData EsServiceStackUserData` → new field `ServiceStack.Envs []ServiceEnvVar`.
- `internal/platform/types.go::ServiceStack`: add `Envs []ServiceEnvVar` field.
- `internal/ops/discover.go`: when `hostname == ""` AND `includeEnvs == true`, skip per-service `GetServiceStackEnv` if `svc.Envs` already populated. Keep per-service fetch as fallback for scoped query (`hostname != ""`) for read-after-write freshness.

**Tests:**
- New: `TestDiscover_UnscopedUsesEmbeddedEnvs_*` — assert single API roundtrip.
- Mock: add UserData population in `WithServiceEnvs`.

**Result after phase:** Unscoped discover-with-envs makes 1 API call instead of 1+N. Service-scoped still uses dedicated `GetServiceStackEnv` for write-fresh reads.

### Phase 5 — Surface ConnectedStacks + HasUnsyncedUserDataRecord

**Files:**
- `internal/platform/zerops_mappers.go::mapEsServiceStack`: preserve `ConnectedStacks` (map to `Connections []string` with active filter) and `HasUnsyncedUserDataRecord` (→ `EnvsPendingApply bool`).
- `internal/platform/types.go::ServiceStack`: add `Connections []string`, `EnvsPendingApply bool`.
- `internal/ops/discover.go`: emit `connections` and `envSyncStatus` in ServiceInfo JSON.

**Tests:**
- New: `TestDiscover_Connections_PerService` — mock service with connected db, expect `connections: ["db"]` in response.
- New: `TestDiscover_EnvSyncStatus_PendingApply` — mock HasUnsynced=true, expect `envSyncStatus: "pending-apply"`.

**Result after phase:** Agents see explicit service connections + pending-apply state. First-deploy compose can derive managed-service refs from `connections`; debug "why is my env not set" surfaces `pending-apply`.

### Phase 6 — Managed-service env catalog field

**Files:**
- `internal/topology/managed_envs.go` (NEW): canonical catalog per managed service type (postgresql, mariadb, mysql, redis, valkey, clickhouse, kafka, meilisearch, typesense, qdrant, object-storage, shared-storage). Source = recipe knowledge files distilled.
- `internal/ops/discover.go`: for managed services, populate `managedEnvCatalog []string`.

**Tests:**
- `TestManagedEnvCatalog_*` per type.
- Optional: live API contract test against eval-zcp to confirm catalog matches real exposed envs.

**Result after phase:** `zerops_discover service=db` returns the canonical exposed-env catalog (`["hostname", "port", "user", "password", "dbName", "connectionString", "superUser", "superUserPassword"]` for postgres). Per-type cheatsheet atom becomes a lookup table consumers can reference, not memorize.

### Phase 7 — classify-prompt response carries suggestedBucket + value

**Files:**
- `internal/tools/workflow_launch_production.go::handleLaunchClassifyPrompt` (or wherever classifications are built around line 966): for each PromptUser env, populate:
  - `suggestedBucket` from `envclass.ClassifyProjectEnv(env).Bias`
  - `sensitive` from `env.Sensitive`
  - `value` from `env.Content` (only if `includeSensitiveValues` or default behavior — see open question §10.B)
  - `rationale` short text
- `internal/tools/workflow_export.go::handleExportClassifyPrompt`: identical changes.
- `internal/tools/launch_envs.go::launchClassifyRow` struct: add new fields.

**Tests:**
- Golden tests for `classify-prompt` response (`internal/workflow/testdata/atom-goldens/launch-production/classify-prompt.md` etc.): update to include new fields.
- Integration test for launch-production classify flow asserts agent doesn't need separate discover call.

**Result after phase:** Agent gets `{key, value, suggestedBucket, sensitive, rationale}` in classify-prompt. Can accept/override without separate discover roundtrip.

### Phase 8 — Atom rewrites

**Files (atom edits):**

8a. **DELETE entire pattern-matching paragraphs:**
- `internal/content/atoms/launch-classify-platform-envs.md`: hardcoded list of `ZCP_*` / `zerops*` / CDN keys to drop → replace with "envs with `origin=system` are auto-dropped server-side; you only see USER envs"
- `internal/content/atoms/launch-classify-prompt.md`: extensive name-pattern bucket guidance → simplify to "accept `suggestedBucket` unless you have specific reason to override; override-rationale must be stated"

8b. **FIX `hostname=` → `service=`:**
- `internal/content/atoms/launch-classify-prompt.md:34`
- `internal/content/atoms/export-classify-envs.md:22`
- `internal/content/atoms/scaffold-zerops-yaml.md:16`

8c. **REPLACE per-managed-type cheatsheet with live-data reference:**
- `internal/content/atoms/develop-first-deploy-env-vars.md`: replace inline cheatsheet with "see `managedEnvCatalog` per service in discover response; key shape gotchas (e.g. connectionString omits /dbName) surface via per-env `annotations.warning`"

8d. **ADD envSummary mention:**
- `internal/content/atoms/develop-env-var-model.md`: brief mention that envSummary tells you at-a-glance counts and pending-apply state.

8e. **TRIM duplication between export-classify-envs + launch-classify-prompt:**
- Both currently re-derive 4-bucket rules and traps. Move shared content to a single `env-classification-buckets.md` (id-shared atom; phases: `[launch-production-active, export-active]`); reference from both call-site atoms.

**Tests:**
- Atom lint catches axis violations.
- Golden tests regenerate for any atom rendering that changes.

**Result after phase:** Atom corpus net-smaller. Agents see less prose, more structured data.

### Phase 9 — Consolidation cleanup

**Files (deletions/migrations):**
- `internal/ops/env_generate.go::platformInternalKeys`: DELETE. Replace consumers:
  - `env_generate.go::EnvGenerateDotenv` filter loop: use `topology.EnvOriginFromKey(key) == EnvOriginSystem`  
  - `env_plan.go:367`: same
- `internal/ops/deploy_validate.go::hardReservedEnvKeys` + `runScopeReservedEnvKeys`: MOVE to `topology/env_classification.go` (exported).
- `internal/ops/helpers.go::platformInjectedKeys` + `envKeyZeropsSubdomain`: DELETE. The `isPlatformInjected` annotation now derives from `origin=system`.
- `internal/envclass/classify.go::credentialPattern`: MOVE to topology (exported as `topology.CredentialPattern`). envclass keeps the Decision/Bias logic but uses topology helper.

**Tests:**
- Drift-gate tests: `TestNoLegacyPlatformInternalKeys` — grep ensures no file references the deleted denylist.
- `TestEnvKeyClassification_*` consolidated in topology package.

**Result after phase:** Single source of truth for env taxonomy. Adding a new platform-internal env: add `Type=SYSTEM` server-side (or one line in topology if ZCP-side defense-in-depth needed). No 3-file edit dance.

### Phase 10 — Verification + atom regen + eval re-run

**Activities:**
- Run full `make lint-local` + `go test ./... -race`.
- Re-render atom goldens; visually diff in CI.
- Spawn `flow-eval` runs for: `develop-loop-after-bootstrap`, `launch-production-from-standard-pair`, `launch-production-pipeline-not-configured`, `recipe-laravel-showcase-fullstack`, `greenfield-node-postgres-dev-stage`.
- Compare transcript discover/env call counts pre/post — expect ~40% drop in launch-production (2-call dance gone) and qualitative drop in classify-prompt prose-burning.

---

## 7. File-by-file change inventory

| Path | Phase | Change |
|------|-------|--------|
| `internal/topology/env_classification.go` | 1, 9 | NEW: EnvOrigin enum + ReservedKey* + CredentialPattern + SuggestClassification + From*Type helpers |
| `internal/topology/managed_envs.go` | 6 | NEW: per-type managed-env catalog table |
| `internal/topology/env_classification_test.go` | 1, 9 | NEW: enum + lookup tables |
| `internal/topology/managed_envs_test.go` | 6 | NEW |
| `internal/platform/types.go` | 1 | Drop or expand `EnvAccessor`; add `ServiceStack.Envs/Connections/EnvsPendingApply` fields |
| `internal/platform/zerops_mappers.go` | 4, 5 | `mapEsServiceStack` preserves UserData/ConnectedStacks/HasUnsynced* |
| `internal/platform/mock.go` + `mock_methods.go` | 1-6 | Mock additions to mirror new SDK passthroughs |
| `internal/ops/helpers.go` | 1, 2 | Rewrite `envVarsToMaps`; fold `annotateConnectionStringShape` into `annotations`; DELETE `platformInjectedKeys` (after Phase 9) |
| `internal/ops/discover.go` | 2, 3, 4, 5, 6 | Remove early-return at L76; emit envSummary; use embedded UserData; emit connections/envSyncStatus/managedEnvCatalog |
| `internal/ops/discover_test.go` | 1-6 | Update + invert + add cases |
| `internal/ops/env_generate.go` | 9 | DELETE platformInternalKeys; switch filter to topology helper |
| `internal/ops/env_plan.go` | 9 | Same filter switch; consider exposing Source/Scope/Conflict in tool response for debug (followup, not blocking) |
| `internal/ops/deploy_validate.go` | 9 | Move reserved-key maps to topology; keep validator using them |
| `internal/envclass/classify.go` | 9 | Becomes thin wrapper; references topology |
| `internal/tools/workflow_launch_production.go` | 7 | classify-prompt response includes suggestedBucket/sensitive/value/rationale |
| `internal/tools/workflow_export.go` | 7 | Same |
| `internal/tools/launch_envs.go` | 7 | Extend `launchClassifyRow` struct |
| `internal/tools/discover.go` | 1-6 | Tool description update mentioning origin/sensitive/connections/envSummary |
| `internal/tools/env.go` | 1-6 | Tool description update; `get` delegates already, will inherit |
| `internal/content/atoms/launch-classify-prompt.md` | 8 | Trim |
| `internal/content/atoms/launch-classify-platform-envs.md` | 8 | Replace hardcoded list w/ origin-based filter |
| `internal/content/atoms/export-classify-envs.md` | 8 | Same trim |
| `internal/content/atoms/develop-first-deploy-env-vars.md` | 8 | Replace cheatsheet w/ live-catalog reference |
| `internal/content/atoms/develop-env-var-model.md` | 8 | Brief envSummary mention |
| `internal/content/atoms/env-classification-buckets.md` | 8 | NEW (shared) |
| `internal/content/atoms/scaffold-zerops-yaml.md` | 8 | hostname=→service= |
| `internal/workflow/testdata/atom-goldens/launch-production/*.md` | 7, 8 | Regen |
| `internal/workflow/testdata/atom-goldens/export/*.md` | 7, 8 | Regen |

**Estimated touch count:** ~30 source files + ~10 test files + ~12 atoms + golden regens. Most changes <30 lines.

---

## 8. Test invariant changes (what flips)

| Test | Phase | Current assertion | New assertion |
|------|-------|-------------------|---------------|
| `TestDiscover_ProjectEnvs_WithServiceFilter` | 3 | project envs nil when hostname set | project envs ALWAYS present when includeEnvs=true |
| `TestEnvVarsToMaps_PlatformInjected` | 1 | only `zeropsSubdomain` flagged isPlatformInjected | origin=system populated for all SYSTEM envs; legacy isPlatformInjected kept as alias for BC then removed |
| `TestEnvVarsToMaps_KeysOnly` | 1 | 3 fields per entry | 6+ fields per entry (key, scope, origin, sensitive, lastModified, annotations) |
| `TestAnnotateConnectionStringShape_*` | 2 | top-level `completenessFlags` + `warning` | nested under `annotations` |
| `TestCheckReservedEnvNames_*` | 9 | uses local maps in deploy_validate.go | uses topology.HardReservedKey() / RunScopeReservedKey() |
| `TestClassifyProjectEnv_*` | 9 | hits envclass.credentialPattern | hits topology.CredentialPattern; envclass keeps test for the Decision logic |

---

## 9. Risks

### 9.1 BC concerns
- Pre-production rule (CLAUDE.local.md): no BC shims unless required. This plan happily breaks existing JSON shape (renamed fields, deleted ones).
- Downstream consumers of `zerops_discover` JSON: LLM agents themselves. They adapt at next session start (atom guidance updates with them).
- Render goldens regenerate; CI catches any unaudited shape drift.

### 9.2 Eventual consistency on embedded UserData (Phase 4)
- `ListServices` returns ES-backed `EsServiceStack` which can lag a few seconds behind a recent `zerops_env set`.
- Mitigation: only use embedded UserData when `hostname == ""` (project-wide discover). Service-scoped (`hostname != ""`) keeps dedicated `GetServiceStackEnv` for read-after-write freshness. Tradeoff documented in `discover.go` comment.

### 9.3 Managed-env catalog drift (Phase 6)
- Catalog hardcoded in topology mirrors recipe-knowledge and could drift if Zerops adds keys.
- Mitigation: add a live API contract test (`go test -tags=live`) that compares the topology table against real eval-zcp managed services on demand. If drift detected, fail loudly.
- Alternative: derive catalog dynamically from `discover` response of a freshly-provisioned managed service (one-time at zcp startup or cached). Heavier; defer until Phase 6 lands and we see if drift is a real problem.

### 9.4 Sensitive value redaction policy (Phase 7 + design Q)
- See §10.B open question. Default redact is the right call but breaks debug-by-value workflows.

### 9.5 envclass package becoming vestigial
- After Phase 9, `envclass` is a thin wrapper around topology. Could be merged into topology.
- Don't merge in this plan — keep the Layer-3 SDK-driven name (`envclass`) as semantic marker of "this is the classifier" so consumers grep find it. Revisit after 6 months if vestigial nature is confirmed.

### 9.6 Phase 4 breaks ListServices contract
- Today `platform.ServiceStack` is the canonical projection; adding `Envs` makes it heavier.
- Callers that don't need envs (e.g., quick service-list-only callers) now carry the data anyway.
- Acceptable: SDK already returns it; ZCP currently throws it away. Keeping it is cheap.

---

## 10. Open questions for Karel

### A. Drop `EnvAccessor` interface entirely vs extend?
- **Drop (preferred):** Split `envVarsToMaps` into two type-specific functions. Simpler, no abstraction tax, callers see concrete types.
- **Extend:** Add `GetType/GetSensitive/GetEditable` methods. Keeps generic but the interface now leaks scope semantics (Editable is project-only).
- Recommendation: drop. EnvAccessor exists only for one helper; splitting is ~30 lines.

### B. Sensitive value redaction in `discover` response with `includeEnvValues=true`?
- **Today:** Full plaintext for all values including server-marked Sensitive.
- **Proposed A:** Default redact (`value: "[REDACTED — pass forceSensitive=true to read]"`) for sensitive=true entries. Adds `forceSensitive` (or similar) flag.
- **Proposed B:** Partial-redact (`github_pat_11AR...XE7`). Preserves pattern matching ability.
- **Proposed C:** Status quo (full plaintext).
- Tradeoff: launch-classify needs value patterns (`sk_test_*` detection); debug occasionally needs full value; daily develop-loop doesn't need values at all.
- Recommendation: **B for default**, with explicit `includeSensitiveValues=true` to get full. Matches eval evidence (7 of 60 calls actually need values).

### C. `connections` field: include just hostnames, or shapes with status?
- **A (simpler):** `connections: ["db", "cache"]` — list of ACTIVE-connected hostnames.
- **B (richer):** `connections: [{hostname: "db", status: "ACTIVE"}]` — includes status.
- Eval evidence shows agents pattern-match hostnames, not statuses. Recommendation: A.

### D. `managedEnvCatalog` placement: topology table vs live discover?
- **Topology table (Phase 6 as-written):** hardcoded; possible drift.
- **Live derivation:** discover the catalog by inspecting the actual managed service envs at runtime; cache per type.
- Live derivation eliminates drift but adds startup cost. Recommendation: topology + contract test in CI to catch drift.

### E. Should `zerops_env action="get"` get its own atom?
- Currently `get` delegates to `Discover` and the env tool description mentions it. Agents in transcripts use `zerops_discover` not `zerops_env get`. No friction — could drop `get` action and tell agents to use discover.
- Eval evidence: 0 `zerops_env action="get"` calls across 60 env-related calls. Action exists but unused.
- Recommendation: defer decision; small thing. Keep for now.

---

## 11. Definition of done

After all phases, the following are true:

1. **Single source of truth:** No env knowledge duplicated across files. `topology/env_classification.go` + `topology/managed_envs.go` are authoritative; everyone else references.

2. **Discover response is the env catalog:** A single `zerops_discover includeEnvs=true` call returns everything LLM needs across lifecycle phases — keys, values (controlled), Type, Sensitive, Editable, refs classified, reserved-key warnings, managed-service exposed catalogs, cross-service connections, envSyncStatus. No phase-specific atoms need to recompute.

3. **classify-prompt is one call:** No agent has to fetch values separately. `suggestedBucket` + `value` + `sensitive` are in the response.

4. **Atom corpus shrinks:** Inline cheatsheets / hardcoded blacklists / pattern-match instructions deleted in favor of "read the structured field". `launch-classify-platform-envs.md` deleted entirely (folded into `env-classification-buckets.md`). `develop-first-deploy-env-vars.md` cheatsheet section shrunk to 5 lines pointing at the discover response field.

5. **CLAUDE.md invariant added:** "Mapping functions (`map*`) must preserve all platform-exposed env-related fields by default; drops require explicit justification + test." Prevents the data-loss pattern from regrowing.

6. **Eval friction baseline:** Re-run the 5 env-heavy scenarios. Expectations:
   - Launch-production discover calls: 2-3 → 1.
   - Classify-prompt agent turns: 2-3 → 1 (accept bias).
   - Develop-loop "which db_*" pattern matching: gone (use `connections`).
   - Self-shadow surprises: surfaced in annotations pre-deploy.

7. **No regressions:** Existing scenarios (close-mode, deploy validation, .env generation, recipe import) work identically. Test suite green.

---

## 12. Out of scope (intentionally deferred)

- **EnvPlan Source/Scope/Conflict surfacing in tool responses.** Internal model is rich; debug workflows could benefit. Hold until eval evidence of need (today's evals don't surface debug-by-source-trace as a friction).
- **GitHub-style env "rotation" UX.** Multi-env-with-history workflows. No eval signal yet.
- **Per-env reference graph visualization.** Could be useful for understanding cross-service wiring but no agent flow needs it today.
- **`zerops_env` action overhaul.** Today's action enum (`get/set/delete/generate-dotenv`) is fine; deprecating `get` after evidence shows agents only use discover.
- **Brownfield import overlay** (`SourceBrownfieldImport`). Reserved Theme 3.

These get separate backlog entries if/when evidence appears.

---

## 13. Sequencing notes

- Phases 1+2+3 ship together (single PR or 2 PRs). They're the additive base.
- Phase 4+5 ship together (one PR — mapper changes).
- Phase 6 is independent (managed-env catalog table). Can ship anytime after Phase 1.
- Phase 7 (classify-prompt) requires Phase 1 (origin field) + Phase 3 (project envs in any discover call). Ship after 1+2+3.
- Phase 8 (atoms) requires 7. Atom golden regen happens in same PR as 7.
- Phase 9 (cleanup) is the last consolidation. Requires 1+2+3+4+5+6+7+8 done.
- Phase 10 = post-merge verification.

Estimated wall time: **5-7 working days** for one engineer focused. Phases 1-5 are ~50% of effort (foundation); Phases 6-9 are the long tail of consolidation.

---

## 14. Why this is the right shape

Going back to the meta-question Karel asked: "fundamentálně vylepšit pro LLM tu práci s envy."

The fundamental improvement is not adding fields, not rewriting atoms, not optimizing discover calls. It's: **eliminating the gap between what the platform knows and what the LLM sees.** Every env-related friction in the 39-eval corpus traces back to that gap. Closing it (Phases 1-7) makes the prose reconstructions (Phase 8 atom trimming) and the duplicated denylists (Phase 9 cleanup) collapse naturally.

The single invariant that prevents regression: **map functions don't drop fields without justification.** That goes in CLAUDE.md and pins the architectural lesson.

Everything else in this plan is mechanics for getting there phase-by-phase without breaking what works today.
