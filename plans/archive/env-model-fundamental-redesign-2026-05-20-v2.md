# Plan v2 — Env-var lifecycle redesign (corrected after multi-perspective review)

**Date:** 2026-05-20
**Status:** Supersedes v1 (`plans/env-model-fundamental-redesign-2026-05-20.md`); v1 remains as historical reference
**Triggers for v2:** Karel review feedback ("rozšíř na multi-service, by-design safe + clean architecture, znovu reviduj"); independent codex review; 3 parallel agent investigations (multi-service stress test, architecture audit, safety audit)

---

## 1. Why v2 — what v1 got wrong

v1 was structurally **half-right and dangerously incomplete**. The direction (don't drop SDK metadata) is correct, but the execution had several issues that independent review caught:

| Issue from v1 | Severity | Source |
|---------------|----------|--------|
| Proposed `platformInternalKeys` denylist deletion based on "duplicates Type=SYSTEM" — **FALSE**. `ZCP_API_KEY` is platform-marked `Type=USER, Sensitive=false` (live-verified by `envclass/classify_test.go:177`). Deleting denylist would leak control-plane secrets into local `.env`. | 🔴 SAFETY | Codex |
| Proposed adding `value` to classify-prompt response — **VIOLATES PINNED SAFETY INVARIANT**. `TestHandleExport_ClassifyPromptDoesNotLeakValues` pins "rows must NOT include the raw env value field"; `workflow_export.go:425-432` doc-comment explicitly forbids it. Plan v1 §4.4 silently proposed a policy reversal without flagging it. | 🔴 SAFETY | Codex |
| Proposed `FromProjectType(t platform.ProjectEnvType) EnvOrigin` in `topology/` — **VIOLATES stdlib-only invariant** pinned by `architecture_test.go:29`. Phase 1 would fail CI gate immediately. | 🔴 ARCH | Codex + Architecture audit (Q3) |
| Kept `[]map[string]any` response model. That `any`-map is **the same abstraction that caused the data-loss pattern** v1 set out to fix. Adding fields to `any` doesn't fix the type-safety problem. | 🟠 ARCH | Codex |
| `envSyncStatus="in-sync"` overpromises. `HasUnsyncedUserDataRecord=false` does NOT mean all containers restarted with new value — only that ES list-service reflects it. | 🟠 SAFETY | Codex |
| `reservedKey` annotation not scope-aware. `hostname` is hard-reserved in *authored* yaml but legitimate as *discovered* managed-service env key. Single-flag annotation would teach agents to "fix" valid platform output. | 🟠 SAFETY | Codex |
| Single-service shaped: `envSummary.perService` summarizes the wrong unit. Multi-setup zerops.yaml has different envs per setup-block (`dev`/`prod`/`worker`). Per-service rollup loses setup-axis. | 🟠 SCOPE | Codex + Multi-service stress test |
| `managedEnvCatalog: ["hostname","port"]` is a string list — pushes join work onto LLM. Should expose fully-qualified refs (`{provider:"db", key:"hostname", ref:"${db_hostname}"}`). | 🟡 UX | Codex + Multi-service stress test |
| `connections []string` filters to ACTIVE only — CREATING managed services invisible to agent. | 🟡 SCOPE | Codex |
| `suggestedBucket` as single value lacks context-awareness. `API_URL` has 4+ valid interpretations. | 🟡 UX | Codex |
| Missing `actionability` axis — verb-oriented signal of what agent should DO (read-only-info / safe-to-use-ref / needs-classification / control-plane-omit / pending-restart). v1 surfaces property-oriented metadata; the cognitive bridge to action is left to the LLM. | 🟢 MISS | Codex (Section 5 — angle I missed) |
| Missing Created/LastUpdate at platform layer. v1 assumed `LastUpdate` was available; it's parsed by SDK but dropped at `platform/zerops_env.go:27-37` (no field on `ProjectEnvVar`/`ServiceEnvVar`). Prerequisite step not in v1. | 🟡 BUG | Codex |
| `hostname=` bug also in CODE, not just atoms. `workflow_export.go:471` emits `zerops_discover hostname=...` instruction (wrong param name). v1 only listed atom fixes. | 🟡 BUG | Codex |

Net assessment: v1 is **drectionally correct but structurally insufficient**. v2 rewrites Sections 4, 5, 6, 7, 9 of v1; carries §1, §2, §3, §8, §10-§14 with minor amendments.

---

## 2. Mental model v2 — Env lifecycle as a multi-dimensional graph

v1 treated envs as flat per-service lists. **They're not.** Five orthogonal dimensions:

### Dimension 1 — Source layer (where the value comes from)
```
1. Platform-auto-injected SYSTEM project envs    (Type=SYSTEM)
2. Platform-internal USER project envs           (Type=USER + ControlPlanePolicy match: ZCP_API_KEY)
3. User-authored project envs                    (Type=USER, not in ControlPlanePolicy)
4. Managed-service auto-emitted service envs     (Type=READ_ONLY|INTERNAL on managed)
5. User-authored zerops.yaml run.envVariables    (per setup-block)
6. Service-level direct overrides                (zerops_env set serviceHostname=X)
7. .env.local                                     (per-developer local override)
8. Container OS env                               (HOSTNAME, PATH from kernel)
```

### Dimension 2 — Setup-block axis (zerops.yaml multi-setup)
Multi-setup yaml exposes different envs per setup. Laravel showcase has `setup: dev` (full deps), `setup: prod` (no-dev + assets), `setup: worker` (queue worker, no HTTP). Each setup block carries its own `run.envVariables` map.

```
appdev   deployed via setup=dev    → DEV  envVariables
appstage deployed via setup=prod   → PROD envVariables
workerstage deployed via setup=worker → WORKER envVariables
```

Same managed services (db, redis) but each setup may rename refs differently. Discover sees deployed state, not all setup blocks. New v2 surface: `zeropsYamlEnvGraph` parsed from zerops.yaml when accessible (mount or SSH read).

### Dimension 3 — Service-role axis
```
runtime services       — consume envs, may compose refs
managed services       — emit envs (db_*, redis_*, etc.)
worker / migrate roles — runtime semantics but no HTTP probe
utility services       — own buildFromGit (e.g. mailpit)
```

### Dimension 4 — Sync-state axis (per-service)
```
in-platform                  — env in API state
in-platform-pending-restart  — set but containers don't have it yet
in-platform-pending-sync     — ES list-service hasn't reflected it
```

### Dimension 5 — Trust-boundary axis (cross-project)
```
this-project (current ZCP) — full read/write
source-project              — launch-production source (read only via launchKey)
target-project              — launch-production target (write via launchKey, read via existingProdToken)
foreign                     — other projects, no access
```

**v1 collapsed dimensions 2–5 onto dimension 1.** v2 keeps them orthogonal.

---

## 3. Architecture corrections

### A. Topology stays stdlib-only

**v1 error:** `topology.FromProjectType(platform.ProjectEnvType)` imports platform → violates `topology/architecture_test.go::TestArchitectureContract`.

**v2 fix:**
- Topology exports the `EnvOrigin` enum (`user|system`) + `EnvOriginFromString(s string) EnvOrigin` helper that takes RAW STRING (Layer-3 caller passes `string(platform.ProjectEnvType)`).
- Mapping from SDK enum to topology enum lives at `internal/ops/inventory/envs.go` (Layer 2 bridge that already imports platform):
  ```go
  func ProjectEnvOrigin(t platform.ProjectEnvType) topology.EnvOrigin {
      return topology.EnvOriginFromString(string(t))
  }
  func ServiceEnvOrigin(t platform.ServiceEnvType) topology.EnvOrigin {
      // READ_ONLY, INTERNAL, ENV → system; EDITABLE, SECRET → user
      switch t {
      case platform.ServiceEnvReadOnly, platform.ServiceEnvInternal, platform.ServiceEnvEnv:
          return topology.EnvOriginSystem
      default:
          return topology.EnvOriginUser
      }
  }
  ```

### B. Topology owns vocabulary, ops owns policy + helpers, envclass owns Decision logic

```
topology/env_classification.go
  - EnvOrigin enum + EnvOriginFromString
  - ReservedKey* enums + maps (HardReservedKeys, RunScopeReservedKeys, OverridableReservedKeys)
  - ControlPlaneEnvKeys map (ZCP_API_KEY, ZCP_AGENT_TYPE, ...)  ← NEW (codex finding)
  - CredentialPattern regex
  - SecretClassification enum (existing)
  - Actionability enum (NEW: read-only-info | safe-to-use-ref | needs-classification | invalid-authored-yaml | control-plane-omit | pending-restart)

ops/inventory/envs.go (Layer 2 bridge)
  - ProjectEnvVar / ServiceEnvVar type aliases (existing)
  - ProjectEnvOrigin(t) / ServiceEnvOrigin(t) helpers (NEW)
  - FetchProjectEnvs / FetchServiceEnvs (existing)

ops/env_classify.go (NEW, Layer 3)
  - PerEnvActionability(env, ctx) []Actionability  — context-aware classifier
  - SuggestClassification(env) (SecretClassification, rationale)  — moved here from v1 topology placement
  - IsControlPlane(key) bool  — strict allowlist; renamed from platformInternalKeys

envclass/classify.go (existing, becomes thinner but NOT vestigial)
  - ClassifyServiceEnv / ClassifyProjectEnv decision tree using topology + ops helpers
  - The package boundary stays as documented "Layer 3 SDK-driven policy"
  - Note: heuristics MIGHT move here from ops/env_classify.go if Karel prefers — codex argued envclass should keep policy. Decision deferred to Karel (open Q below).
```

### C. Replace `[]map[string]any` with typed structs

**v1 error:** Plan kept `envVarsToMaps[T EnvAccessor] []map[string]any`. That `any`-map is what caused the data loss originally. Bolting fields onto it doesn't fix the abstraction.

**v2 fix:** Typed structs in `internal/ops/env_view.go` (NEW):

```go
package ops

// EnvView is the typed representation surfaced to LLM agents via discover.
type EnvView struct {
    Key                     string                          `json:"key"`
    Scope                   EnvScope                        `json:"scope"`                     // project | service:<host>
    Origin                  topology.EnvOrigin              `json:"origin"`                    // user | system
    Sensitive               bool                            `json:"sensitive,omitempty"`
    Editable                *bool                           `json:"editable,omitempty"`        // nil for service scope
    LastModified            string                          `json:"lastModified,omitempty"`    // RFC3339; from platform LastUpdate
    Value                   *EnvValueView                   `json:"value,omitempty"`           // structured value w/ redaction
    Annotations             EnvAnnotations                  `json:"annotations,omitzero"`
    Actionability           []topology.Actionability        `json:"actionability,omitempty"`   // computed per context
    SuggestedClassification *SuggestedClassificationHint    `json:"suggestedClassification,omitempty"`
}

type EnvValueView struct {
    Redacted    bool   `json:"redacted"`           // true when sensitive AND not explicitly unredacted
    Preview     string `json:"preview,omitempty"`  // first 6 + last 3 chars when redacted (avoid middle leakage on short tokens)
    Hash        string `json:"hash,omitempty"`     // sha256:hex(first16) when redacted; lets agent diff without value
    Length      int    `json:"length,omitempty"`
    Kind        string `json:"kind,omitempty"`     // "literal" | "reference" | "reference-composed" | "preprocessor-directive" | "empty"
    Shape       string `json:"shape,omitempty"`    // pattern discriminator: "stripe-live-key" | "github-pat" | "jwt" | "managed-ref" | "hex32" | etc.
    Full        string `json:"full,omitempty"`     // populated only when includeSensitiveValues=true OR not sensitive
    Raw         string `json:"raw,omitempty"`      // template form ("${db_connectionString}") — present when Kind=reference and resolveRefs=true

    // resolveRefs=true population (Phase 5a):
    Resolved         string                  `json:"resolved,omitempty"`          // fully-resolved chain output; redaction follows Full rules per leaf sensitivity
    ResolutionDepth  int                     `json:"resolutionDepth,omitempty"`   // number of hops walked
    ResolutionChain  []EnvResolutionStep     `json:"resolutionChain,omitempty"`   // provenance trail; empty when resolveRefs=false
    ResolutionStatus string                  `json:"resolutionStatus,omitempty"`  // "complete" | "partial-cycle" | "partial-max-depth" | "partial-unknown-ref"

    // Edge case: when env exists but value is "", do NOT render "[REDACTED]" —
    // render value: "" (or omit the EnvValueView entirely) so agent can
    // distinguish "set-but-hidden" from "set-but-empty". Pinned by
    // TestEnvValueView_EmptySensitiveValue.
}

// EnvResolutionStep traces one hop in nested-ref expansion.
type EnvResolutionStep struct {
    Step      string `json:"step"`                // qualified key, e.g. "db.connectionString"
    Service   string `json:"service,omitempty"`   // owning service hostname
    Expands   string `json:"expands,omitempty"`   // template at this step (when intermediate)
    Leaf      string `json:"leaf,omitempty"`      // terminal value at this step (when leaf); redacted if sensitive unless includeSensitiveValues=true
    Sensitive bool   `json:"sensitive,omitempty"` // leaf-level sensitivity (drives redaction)
}

type EnvAnnotations struct {
    IsReference        bool                    `json:"isReference,omitempty"`
    RefTargets         []EnvRef                `json:"refTargets,omitempty"`        // per-env, list of resolved refs
    StaleRefTargets    []string                `json:"staleRefTargets,omitempty"`   // refs to renamed/deleted services
    SelfShadow         bool                    `json:"selfShadow,omitempty"`        // value == ${key}
    ReservedKey        *ReservedKeyContext     `json:"reservedKey,omitempty"`       // scope-aware: see below
    CompletenessFlags  map[string]bool         `json:"completenessFlags,omitempty"` // e.g. {"includesDbName": false}
    Warning            string                  `json:"warning,omitempty"`
}

type EnvRef struct {
    Provider string   `json:"provider"`  // hostname
    Keys     []string `json:"keys"`      // ["user","password","hostname","port","dbName"] for compound URL
    Raw      []string `json:"raw"`       // ["${db_user}", "${db_password}", ...] verbatim
}

type ReservedKeyContext struct {
    Regime topology.ReservedKeyRegime `json:"regime"` // hard | run-scope | overridable
    Scope  string                     `json:"scope"`  // "authored-yaml" | "discovered-managed-env" | "discovered-runtime-env"
    Action string                     `json:"action"` // "forbidden" | "advisory" | "informational"
}

type SuggestedClassificationHint struct {
    Bucket       topology.SecretClassification `json:"bucket"`
    Confidence   string                        `json:"confidence"`   // "name-pattern" | "managed-ref" | "value-pattern" | "low"
    Rationale    string                        `json:"rationale"`
    Alternatives []topology.SecretClassification `json:"alternatives,omitempty"`
    ContextNote  string                        `json:"contextNote,omitempty"`   // e.g. "in greenfield prod project; user override expected if state continuity needed"
}
```

**ENV-WIDE SCHEMA: typed, JSON-friendly, evolution-friendly.** No `any`-maps. Adding a new field is a Go struct edit + a test golden regen, not a discovery surface change.

### D. ControlPlane policy is explicit allowlist (Codex critical correction)

**v1 error:** Plan §9 proposed deleting `platformInternalKeys` and relying on `Type=SYSTEM`. But `ZCP_API_KEY` is `Type=USER` + `Sensitive=false` — would leak into `.env`.

**v2 fix:**
```go
// internal/topology/env_classification.go
//
// ControlPlaneEnvKeys lists keys ZCP injects into the source project for
// the MCP server's own operation. They are NOT user application data
// regardless of platform Type classification (some are Type=USER).
// Policy: omit from .env, drop from launch-production / export bundles,
// never include in agent classify-prompt (auto-bucket as infrastructure).
var ControlPlaneEnvKeys = map[string]bool{
    "ZCP_API_KEY":     true,
    "ZCP_AGENT_TYPE":  true,
    "ZCP_BASE_HOST":   true,
    "ZCP_BUILTINS_DIR": true,
    "ZCP_PROJECT_DIR": true,
    // ... plus prefix rule: any key starting with "ZCP_"
}

func IsControlPlane(key string) bool {
    if ControlPlaneEnvKeys[key] {
        return true
    }
    return strings.HasPrefix(key, "ZCP_")
}
```

This is **separate from `Type=SYSTEM` filtering** — both gates fire independently. `env_generate.go` filter becomes:

```go
if env.Origin == topology.EnvOriginSystem || topology.IsControlPlane(env.Key) {
    continue  // omit from .env
}
```

### E. Managed-env catalog: live-derive first, static fallback in knowledge

**v1 error:** `topology/managed_envs.go` proposed as static table inside topology. Codex argued: live SDK returns it embedded in `ServiceStack.UserData`; static table risks drift.

**v2 fix:**
- Primary path: `mapEsServiceStack` preserves `UserData` (already proposed Phase 4 in v1). discover surfaces real exposed keys from live service.
- Fallback path: `internal/knowledge/managed_envs.go` (NOT topology) carries hardcoded catalogs for recipe-authoring contexts where no live service exists. Imported only by `zerops_knowledge` tool.
- Contract test (live, `+build live`) compares topology fallback against real managed-service envs on eval-zcp. Drift triggers CI fail in `-tags=live` mode + manual update workflow.

### F. EnvAccessor interface — drop, don't extend

**Q1 from architecture audit, Codex agreement:**

Drop `EnvAccessor` interface (`platform/types.go:218`). Split `envVarsToMaps` into two type-specific functions:
- `projectEnvViewsFrom(envs []platform.ProjectEnvVar, opts ViewOpts) []EnvView`
- `serviceEnvViewsFrom(envs []platform.ServiceEnvVar, scope string, opts ViewOpts) []EnvView`

Each function sees full SDK shape (Type, Sensitive, Editable, Created, LastUpdate). No fields lost at interface boundary.

---

## 4. Multi-service awareness — what v2 adds

### A. zeropsYamlEnvGraph (NEW surface)

Discover with `includeYamlEnvGraph=true` parses zerops.yaml (via SSH mount or local CWD) and surfaces:

```json
"zeropsYamlEnvGraph": {
  "setups": {
    "dev":    {"runEnvVariables": {"APP_ENV": "development", "DB_HOST": "${db_hostname}"}, "buildEnvVariables": {}},
    "prod":   {"runEnvVariables": {"APP_ENV": "production", "DB_HOST": "${db_hostname}"}, "buildEnvVariables": {}},
    "worker": {"runEnvVariables": {"APP_ENV": "production", "QUEUE_DRIVER": "redis", "DB_HOST": "${db_hostname}"}}
  },
  "warnings": [
    {"setup": "dev", "key": "DB_HOST", "type": "rename-not-self-shadow"}
  ]
}
```

**Why:** Multi-setup recipes (Laravel showcase) have different envs per setup. v1's per-service rollup couldn't expose this. With graph, agent debugging "why does worker behave differently" has direct path.

**When to fetch:** Optional — default off (yaml read is SSH/mount-dependent). Agent passes `includeYamlEnvGraph=true` when they care.

### B. Connections expanded (status-tagged + bidirectional)

Per Safety audit S7: ACTIVE-only filter loses signal during first-deploy-during-provisioning. Flatten list with status discriminator:

```json
{
  "hostname": "api",
  "connections": [
    {"hostname": "db",     "status": "ACTIVE",   "direction": "outgoing"},
    {"hostname": "cache",  "status": "ACTIVE",   "direction": "outgoing"},
    {"hostname": "search", "status": "CREATING", "direction": "outgoing"},
    {"hostname": "frontend", "status": "ACTIVE", "direction": "incoming"}
  ]
}
```

Agent wanting just active hostnames: `[c for c in connections if c.status == "ACTIVE"]`. Agent wanting to know what's provisioning: `[c for c in connections if c.status == "CREATING"]`. Single shape covers both first-deploy scenarios and steady-state.

Plus top-level `envSummary.serviceTopology` reverse-index for cross-cutting agent queries:
```json
"serviceTopology": {
  "db":    {"connectedFrom": ["appdev","appstage","workerstage"], "type": "managed"},
  "redis": {"connectedFrom": ["appdev","appstage","workerstage"], "type": "managed"}
}
```

Plus top-level `envSummary.serviceTopology` reverse-index:
```json
"serviceTopology": {
  "db": {"connectedFrom": ["appdev","appstage","workerstage"], "type": "managed"},
  "redis": {"connectedFrom": ["appdev","appstage","workerstage"], "type": "managed"}
}
```

### C. Managed env catalog with fully-qualified refs

```json
{
  "hostname": "db",
  "managedEnvCatalog": {
    "normal": [
      {"key": "hostname",         "ref": "${db_hostname}",         "sensitive": false, "purpose": "TCP connect target"},
      {"key": "port",             "ref": "${db_port}",             "sensitive": false},
      {"key": "user",             "ref": "${db_user}",             "sensitive": false},
      {"key": "password",         "ref": "${db_password}",         "sensitive": true},
      {"key": "dbName",           "ref": "${db_dbName}",           "sensitive": false},
      {"key": "connectionString", "ref": "${db_connectionString}", "sensitive": true,
       "completenessFlags": {"includesDbName": false},
       "warning": "omits /${db_dbName}; for Prisma/Drizzle/sqlx/SQLAlchemy/Sequelize compose explicitly"}
    ],
    "elevated": [
      {"key": "superUser",         "ref": "${db_superUser}",         "sensitive": false, "purpose": "DDL/migrations only"},
      {"key": "superUserPassword", "ref": "${db_superUserPassword}", "sensitive": true,  "purpose": "DDL/migrations only"}
    ]
  }
}
```

LLM doesn't compose `${...}` strings from hostname + key; it copies `ref` verbatim. Closes class of bugs around hostname-canonicalization.

### D. EnvView.Actionability axis (Codex Section 5 insight)

For each env, computed in context:

```
read-only-info      — agent should not touch (platform-managed, e.g. hostname, serviceId, appVersionId)
safe-to-use-ref     — fine in zerops.yaml ${...} refs (e.g. db_hostname, redis_password)
needs-classification — must be bucketed before launch-production / export (USER project envs not yet classified)
invalid-authored-yaml — agent attempted to author this key in run.envVariables; will be rejected at deploy
control-plane-omit  — ZCP-internal, never include in .env / export / classify
pending-restart     — set recently, not yet effective in containers
stale-reference     — value contains ${...} pointing at a non-existent or deleted service
```

This is **the bridge** between platform truth (origin/sensitive/reserved) and agent action (do this / don't do that). LLM-friendly: verb-centric.

### E. Cross-project trust boundary explicit

Launch-production classify-prompt response (revised — NOT including raw value!):

```json
"classifications": [
  {
    "key": "APP_KEY",
    "currentBucket": "",
    "trustBoundary": "source-project",        // explicit
    "valuePreview": "gCl...19Y",              // first 3 + last 3, server-redacted
    "valueKind": "literal",                    // "literal" | "reference" | "preprocessor-directive" | "empty"
    "valueLength": 32,
    "sensitive": true,
    "suggestedClassification": {"bucket": "auto-secret", "confidence": "name-pattern", "rationale": "..."}
  }
]
```

**Key safety change vs v1:** No raw `value` field. `valuePreview` is opt-out partial only when `Sensitive=true`. `valueKind` lets LLM pattern-detect (`literal sk_live_...` vs `reference ${db_*}`) without seeing full secret.

Plus, for existing-project path: optional `targetExistingEnvs`:
```json
"targetExistingEnvs": [
  {"key": "APP_KEY", "present": true, "lastModified": "2025-12-01..."}
]
```
Lets agent flag source→target conflicts before commit.

### F. Setup-block awareness per service

```json
{
  "hostname": "workerstage",
  "lastDeployedSetup": "worker",          // NEW: which setup-block was used at last deploy
  "managedEnvCatalog": null,
  "envs": [...]
}
```

`lastDeployedSetup` derives from `ActiveAppVersion` metadata on the service stack (already in SDK; ZCP currently drops).

---

## 5. Safety properties — by design

Pinning the policies that v2 enforces:

| Property | v2 Mechanism |
|----------|--------------|
| **No raw value leak into classify-prompt** | classify-prompt response carries `valuePreview` (partial) + `valueKind` only. `value.full` NEVER populated. Pinned by existing `TestHandleExport_ClassifyPromptDoesNotLeakValues` and extended for launch-classify. |
| **Sensitive default redaction** | `EnvView.Value` is `*EnvValueView` (pointer; absent when keys-only). When present + `sensitive=true`, `Value.Full = ""` unless `includeSensitiveValues=true`. Partial preview + hash always present so agent can pattern-match. Pinned by `TestEnvViewRedaction_SensitiveBehavior_*`. |
| **Control-plane never leaks** | `topology.ControlPlaneEnvKeys` allowlist. `EnvOrigin == EnvOriginSystem OR IsControlPlane(key)` is the joint filter for .env / export / classify. Pinned by `TestControlPlaneEnvFiltering_*` + integration test against eval-zcp. |
| **Reserved-key annotation scope-aware** | `ReservedKeyContext.Scope ∈ {"authored-yaml", "discovered-managed-env", "discovered-runtime-env"}`. `Action ∈ {"forbidden", "advisory", "informational"}`. `hostname` in authored yaml → `forbidden`; same key as `discovered-managed-env` → `informational`. |
| **envSyncStatus doesn't overpromise** | Field renamed to `pendingPlatformSync: true|false|unknown` with companion `pendingPlatformSyncSince: <RFC3339>` (from `ServiceStack.LastUpdate`). `unknown` is default when `HasUnsyncedUserDataRecord` is absent from SDK response (eventual consistency or older SDK). `false` ≠ "containers restarted" — just "ES list reflects state". Per Safety audit S3 + S8: this is **NOT a polling trigger** — atom guidance in Phase 9 explicitly forbids re-discover loops waiting for it to flip. Use `since` for "is this taking unusually long" escalation. Doc-comment explicit. |
| **Self-shadow detection** | `annotations.selfShadow=true` only when `value` is EXACTLY `${key}` (whitespace-stripped, case-sensitive per Linux env semantics). Existing rule from `env_shadow.go::DetectSelfShadows`. Hostname canonicalization (dashes↔underscores) preserved. |
| **Cross-project boundary explicit** | Every classify-prompt entry carries `trustBoundary: "source-project" | "target-project"`. Agent cannot accidentally mix. |
| **Single-project session invariant** | ZCP MCP server bound to one `projectID` at start. v2 doesn't change this. Cross-project paths (`launchKey`, `existingProdToken`) flow through `ProjectAdminClient` with explicit credentialing. |

---

## 6. Updated phased migration

Each phase passes `make lint-local` + full test suite. v2 has 11 phases (was 10). Estimated effort 7-9 working days (was 5-7; the typed-struct refactor + actionability axis + setup-graph add load).

### Phase 0 — SDK field plumbing (NEW)

Pre-Phase-1: add fields to platform types that are dropped today.

**Files:**
- `internal/platform/types.go::ProjectEnvVar`: add `Created`, `LastUpdate`, `Editable` field uses existing.
- `internal/platform/types.go::ServiceEnvVar`: add `Created`, `LastUpdate`.
- `internal/platform/zerops_env.go:30-37, 110-118`: populate from SDK output.

**Tests:** `TestGetProjectEnv_ParsesLastUpdate_*`.

### Phase 1 — Typed EnvView + EnvViewOpts

**Files:**
- `internal/ops/env_view.go` (NEW): typed struct definitions per §3.C.
- `internal/ops/env_view_helpers.go` (NEW): `projectEnvViewsFrom`, `serviceEnvViewsFrom`.
- `internal/topology/env_classification.go` (NEW): EnvOrigin, EnvOriginFromString, ReservedKey* enums + maps, ControlPlaneEnvKeys, CredentialPattern, Actionability enum.
- `internal/ops/inventory/envs.go`: add ProjectEnvOrigin / ServiceEnvOrigin bridges.
- `internal/platform/types.go`: drop `EnvAccessor` interface.
- `internal/ops/helpers.go`: delete `envVarsToMaps`; callers migrate to typed helpers.
- `internal/ops/discover.go`: response uses `[]EnvView` instead of `[]map[string]any`.

**Tests:** golden regen for discover JSON; new tests for typed shape.

### Phase 2 — Annotations + envSummary (typed)

**Files:**
- `internal/ops/env_view.go`: EnvAnnotations + populate (selfShadow, reservedKey, completenessFlags, refTargets).
- `internal/ops/discover.go`: compute envSummary post-fetch (project + perService).
- `internal/topology/env_classification.go::ReservedKeyContext` (scope-aware).
- `internal/ops/env_classify.go` (NEW): per-env actionability computation.

**Tests:** scope-aware reservedKey assertion table; actionability fixture.

### Phase 3 — Service-scoped discover returns project envs too

(Unchanged from v1.) `discover.go:76-96` early-return removed; project envs always present when `includeEnvs=true`.

### Phase 4 — mapEsServiceStack preserves UserData / ConnectedStacks / HasUnsyncedUserDataRecord / ActiveAppVersion

**Files:**
- `internal/platform/zerops_mappers.go::mapEsServiceStack`: keep all five fields.
- `internal/platform/types.go::ServiceStack`: add `Envs []ServiceEnvVar`, `Connections ServiceConnections`, `PendingPlatformSync *bool` (pointer for tri-state unknown), `LastDeployedSetup string`.

**Tests:** mock additions; golden updates.

### Phase 5 — Discover surfaces connections (active+creating+consumedBy) + pendingPlatformSync + lastDeployedSetup

(Was v1 Phase 5; expanded scope.)

### Phase 5a — `resolveRefs=true` parameter on discover (NEW)

**Files:**
- `internal/ops/discover.go`: accept `resolveRefs bool` param; when true, for each EnvView with `annotations.isReference == true`, walk the `${...}` chain and populate `value.Resolved` + `value.ResolutionChain` + `value.ResolutionDepth` + `value.ResolutionStatus`.
- Implementation: reuse `internal/ops/env_generate.go::refExpander` (already cycle-safe + depth-capped at 16 + Layer-3-correct). Refactor `refExpander` to a public type usable from discover path. Don't duplicate the resolver.
- `internal/tools/discover.go`: add `resolveRefs` schema param. Default `false` (token-conserving).
- `internal/ops/env_view.go`: `EnvValueView` already extended (see §3.C above) with Resolved/Chain/Depth/Status fields.

**Safety chain (Phase 5a):**
- `resolveRefs=true` without `includeEnvValues=true` → ignored (`Resolved` not populated; agent must opt-in to values first)
- `resolveRefs=true` with `includeEnvValues=true`, NO `includeSensitiveValues=true` → `Resolved` populated but sensitive leaves redacted to `[REDACTED:&lt;sha8&gt;]` placeholder inline; ResolutionChain entries with `Sensitive=true` have `Leaf` redacted
- `resolveRefs=true` with `includeSensitiveValues=true` → full plaintext resolved value + plain leaves; auth-audit-logged per other sensitive-value endpoints
- `resolveRefs=true` on classify-prompt response → **REJECTED at handler** (server-side guard); classify-prompt minimal-disclosure invariant is absolute; pinned by `TestClassifyPrompt_RejectsResolveRefs`

**Failure modes:**
- Cycle detection → `ResolutionStatus: "partial-cycle"` + warning array with the cycle path
- Max-depth (16) → `ResolutionStatus: "partial-max-depth"` + warning
- Unknown ref (`${oldservice_user}` for a deleted service) → `ResolutionStatus: "partial-unknown-ref"` + the chain stops at that step; `annotations.staleRefTargets` (Phase 2) catches this independently as static-analysis hint

**Tests:**
- `TestResolveRefs_Single1Hop_*` — `DATABASE_URL: ${db_connectionString}` → 3-level chain (DATABASE_URL → db.connectionString → db.{user,password,hostname,port})
- `TestResolveRefs_MultiProvider_*` — value referencing both `${api_*}` and `${db_*}`
- `TestResolveRefs_CycleDetection` — A → B → A pattern
- `TestResolveRefs_MaxDepth` — synthetic chain length 20
- `TestResolveRefs_StaleRef_*` — ref to non-existent service
- `TestResolveRefs_SensitiveLeafRedacted_*` — without includeSensitiveValues, leaf with Sensitive=true is redacted in chain
- `TestClassifyPrompt_RejectsResolveRefs` — error response when classify-prompt context

**Token impact:** ~150-400 extra chars per resolved env. Default off; opt-in.

### Phase 6 — Managed env catalog (live-derived primary, knowledge fallback)

**Files:**
- `internal/knowledge/managed_envs.go` (NEW): hardcoded fallback catalog for offline use.
- `internal/ops/discover.go`: for managed services with `UserData` populated, derive catalog live; fall back to knowledge table otherwise. Mark `normal` vs `elevated` (per service type rules in topology).
- `internal/topology/env_classification.go`: `IsElevatedKey(serviceType, key) bool` helper.
- `internal/tools/knowledge.go`: new `zerops_knowledge action="managed-envs" type=<type>` for offline access.

**Tests:** `TestManagedEnvCatalog_LiveDerived_*`, `TestManagedEnvCatalog_FallbackKnowledge_*`, `TestManagedEnvCatalog_ContractVsLiveAPI` (`+build live`).

### Phase 7 — classify-prompt safety-compliant response

**Files:**
- `internal/tools/workflow_launch_production.go::handleLaunchClassifyPrompt`: response carries `valuePreview` + `valueKind` + `valueLength` + `suggestedClassification` + `trustBoundary`. NO raw `value`.
- `internal/tools/workflow_export.go::classifyPromptResponse`: identical changes; existing redaction invariant strengthened.
- `internal/tools/launch_envs.go`: enrich `launchClassifyRow` with new fields.

**Tests:** existing `TestHandleExport_ClassifyPromptDoesNotLeakValues` extended to also forbid `Value.Full` populated; new `TestLaunchClassifyPrompt_NoRawValueLeak`.

### Phase 8 — zeropsYamlEnvGraph (NEW, opt-in)

**Files:**
- `internal/ops/yaml_env_graph.go` (NEW): parse zerops.yaml setup blocks into typed graph.
- `internal/ops/discover.go`: when `includeYamlEnvGraph=true` and yaml accessible (mount or SSH), populate `zeropsYamlEnvGraph` field.
- `internal/tools/discover.go`: new param.

**Tests:** parse fixtures (Laravel showcase, single-setup, no-yaml).

### Phase 9 — Atom rewrites + workflow_export.go:471 code-bug fix

**Files (code):**
- `internal/tools/workflow_export.go:471`: fix `hostname=` → `service=`.
- All atoms in `internal/content/atoms/` referenced in v1 §6 Phase 8.
- New atom `internal/content/atoms/env-classification-buckets.md` (shared between launch + export).
- DELETE `internal/content/atoms/launch-classify-platform-envs.md` (folded into shared atom + actionability axis).

**Tests:** atom lint passes; goldens regen.

### Phase 10 — Consolidation + invariant pin

**Files:**
- `internal/ops/env_generate.go::platformInternalKeys` → use `topology.IsControlPlane(key)`. Denylist deleted but BEHAVIOR PRESERVED.
- `internal/ops/env_plan.go:367`: same migration.
- `internal/ops/deploy_validate.go::hardReservedEnvKeys` + `runScopeReservedEnvKeys`: move to `topology/env_classification.go` (data only; `CheckReservedEnvNames` stays in ops).
- `internal/envclass/classify.go::credentialPattern`: move to `topology.CredentialPattern`; envclass references.
- `internal/ops/helpers.go::platformInjectedKeys` map: DELETE (replaced by Origin check).
- `internal/topology/env_classification_test.go::TestNoLegacyEnvDenylists`: drift-gate test asserting no `platformInternalKeys` / `platformInjectedKeys` references remain.
- `CLAUDE.md`: add invariant *"Mapping functions (`map*` prefix) must preserve all platform-exposed env-related fields by default; drops require inline comment justification + AST-based test enforcement."*
- `internal/platform/zerops_mappers_test.go::TestMappers_AllFieldsJustified` (NEW): AST-based check parsing platform mappers + SDK shapes.

### Phase 11 — Verification + eval re-run

(Unchanged from v1 Phase 10.)

---

## 7. Refined open questions

### A. envclass package's post-refactor home

**Codex:** Don't merge envclass into topology. Keep it as Layer-3 policy.
**Architecture audit:** "evnclass becomes thin wrapper but document its semantic role."

**v2 decision (preferred):** Keep envclass package. Move `credentialPattern` regex to topology. envclass owns Decision logic (Drop / PromptUser / Bias). New rules (e.g., framework-specific overrides) belong in envclass. Topology stays vocabulary-only.

### B. Sensitive value redaction default

**v1:** Partial-redact `github_pat_11AR...XE7`.
**Codex:** Partial values are still secret-bearing for short tokens.

**v2 decision:** Default `Value` field is OMITTED for sensitive entries. When `includeValues=true`, populate `EnvValueView` with `redacted=true` + `preview` (first 3 + last 3 chars) + `length` + `hash` + `kind`. Full value only with `includeSensitiveValues=true` (new param, distinct from `includeValues`).

`generate-dotenv` doesn't go through discover view path; reads platform directly, writes to disk. No conflict.

### C. Actionability axis — server-computed or client-computed?

**Options:**
- Server-computed per-env, single response. Simpler agent code, but server must know workflow context.
- Client-computed in agent. Server returns raw metadata; agent decides actionability per their workflow.

**v2 decision:** **Server-computed when in a workflow context** (launch-production, export, develop-active). Discover OUTSIDE workflow context returns `actionability` only for unambiguous cases (`control-plane-omit`, `read-only-info`). Context-dependent actionabilities (`needs-classification`, `pending-restart`) require workflow state and are populated only by the corresponding workflow handler responses.

### D. zeropsYamlEnvGraph opt-in or default?

**v2 decision:** Opt-in (`includeYamlEnvGraph=true`). Reading yaml requires SSH/mount which can be slow. Most discover calls don't need it.

### E. `zerops_env action="get"` deprecation

**v1 §10.E:** Defer.
**Codex:** action=get delegates to discover w/ values; schema needs same redaction flag.

**v2 decision:** Defer deprecation. Add `includeSensitiveValues` to env action=get param so it matches discover's safety model. Plan removing action=get in v3.

---

## 8. Test invariant changes summary

| Test | Phase | Old | New |
|------|-------|-----|-----|
| `TestDiscover_ProjectEnvs_WithServiceFilter` | 3 | project envs nil w/ filter | project envs ALWAYS present when includeEnvs=true |
| `TestEnvVarsToMaps_*` | 1 | exists, returns map | DELETED; replaced by `TestEnvViewBuilder_*` |
| `TestHandleExport_ClassifyPromptDoesNotLeakValues` | 7 | existing | EXTENDED: also forbids `Value.Full`, requires `valuePreview` partial only |
| `TestLaunchClassifyPrompt_NoRawValueLeak` | 7 | doesn't exist | NEW: parallel to export test |
| `TestControlPlaneEnvFiltering_*` | 10 | exists via platformInternalKeys | MIGRATED: uses topology.IsControlPlane |
| `TestNoLegacyEnvDenylists` | 10 | doesn't exist | NEW: drift-gate against denylist regrowth |
| `TestMappers_AllFieldsJustified` | 10 | doesn't exist | NEW: AST-based mapper-field-preservation check |
| `TestManagedEnvCatalog_*` | 6 | doesn't exist | NEW: live-derived + fallback + contract |
| `TestArchitectureContract` | 1 | passes today | MUST STILL PASS — topology stdlib-only enforced |

---

## 9. Definition of done (revised)

After all phases, the following are true:

1. **Typed env response model** — `EnvView` struct replaces `[]map[string]any`. Adding a field is a Go edit, not a discovery surface change.
2. **Single source of truth for env taxonomy** — `topology/env_classification.go` holds enums + control-plane allowlist + reserved-key data tables. `envclass/` holds Decision policy. `ops/env_classify.go` holds context-aware actionability. No duplicated denylists.
3. **Safety invariants pinned by tests:**
   - classify-prompt never carries raw value
   - Sensitive default redacts to preview + hash + length
   - Control-plane keys (ZCP_API_KEY etc.) never leak to .env / classify / export — independent of Origin
   - Reserved-key annotation is scope-aware (authored vs discovered)
4. **Multi-service first-class:** Per-setup env graph available; managed env catalog with fully-qualified refs; connections include CREATING + reverse-index; lastDeployedSetup surfaced.
5. **Actionability axis:** verb-oriented `read-only-info / safe-to-use-ref / needs-classification / control-plane-omit / pending-restart / invalid-authored-yaml / stale-reference` populated per-env in workflow context.
6. **CLAUDE.md invariant** + AST-based test prevents future map* field-drop regression.
7. **Eval friction baseline:** Re-run 5 env-heavy scenarios.
   - launch-classify discover calls drop (`hostname=` bug fix + project envs in service-scoped query)
   - dev-loop refs use `${...}` from `managedEnvCatalog.normal[].ref` verbatim (no hostname pattern-match)
   - Worker setup-block scenarios surface `lastDeployedSetup` + zeropsYamlEnvGraph distinctions
8. **No regressions:** existing scenarios all pass; control-plane filtering preserved.

---

## 10. Risks v2

### R1. Typed struct migration touches many files

`envVarsToMaps` is called from `internal/ops/discover.go` + indirectly through env tool. Migrating to typed structs ripples through golden tests. Mitigation: Phase 1 lands as a single PR with all golden regens.

### R2. `actionability` axis introduces "actionability lies"

If actionability is wrong (e.g., flags safe-to-use-ref when service is being deleted), agent acts on misleading info. Mitigation: actionability fields are **advisory**; deploy-time enforcement still primary. Wrong actionability is documented as a known limitation; tested per axis-case.

### R3. zeropsYamlEnvGraph parse failures

zerops.yaml may be unparseable (during edit, on a feature branch). Mitigation: parse errors return `zeropsYamlEnvGraph: null` + warning. Discover doesn't fail.

### R4. Live-derived managedEnvCatalog drift

If a managed service in eval-zcp lags adding a new env, contract test fails post-deploy. Mitigation: contract test in `+build live` mode only; manual update workflow in PR template.

### R5. Phase 4 ServiceStack heavier

Every ListServices caller now carries embedded UserData. ~1-3KB per service × ~10 services = ~30KB extra payload on every workflow status check.

Mitigation: introduce optional `ServiceStackEnvFetch bool` on the `ListServices` API call (defaults false; only discover sets true). Avoids the bloat for status-only callers.

### R6. SuggestedClassification mis-bias

Single `bucket` field may bias agent toward wrong choice. Mitigation: always populate `alternatives` + `contextNote`. Agent prompt instructs "verify before accepting".

---

## 11. Migration path from v1

For Karel's review:

- v1 plan stays in repo as historical reference (`plans/env-model-fundamental-redesign-2026-05-20.md`).
- v2 supersedes (`plans/env-model-fundamental-redesign-2026-05-20-v2.md`).
- After Karel approves v2: v1 moves to `plans/archive/` per CLAUDE.md plan-archival convention.
- Backlog item promoted: any items in `plans/backlog/` covered by v2 should be `git rm`-ed in the implementing PR per CLAUDE.md atomic-closure rule.

---

## 12. Acknowledged limitations of v2

- **Actionability axis is incomplete.** First batch covers 7 axis values; more emerge as agents use the API. Schema is open (no closed-enum lint test).
- **zeropsYamlEnvGraph requires accessible yaml.** Fresh greenfield projects without yaml on disk have empty graph. Not a fallback; documented.
- **No env value diff / history.** v2 surfaces `lastModified` timestamp but not value-change history. Future feature.
- **Cross-project value classification (launch-production) carries valuePreview.** Even partial values in agent context are leakage. Trade-off: classification quality vs context-window minimal disclosure.

---

## 13. Sequencing

- Phases 0+1+3 ship together (foundation — typed structs + Created/LastUpdate + project-envs-in-service-query). ~2 days.
- Phases 2+4+5+6 ship together (annotations, ConnectedStacks, managedEnvCatalog, syncStatus). ~2-3 days.
- Phase 7 (classify-prompt) ships alone or paired with Phase 9 atom rewrites. ~1 day.
- Phase 8 (yaml env graph) is independent; ship anytime after Phase 1. ~1 day.
- Phase 10 (consolidation + invariant test) is the final cleanup. ~1 day.
- Phase 11 = post-merge verification (eval re-run).

Total ~7-9 working days.

---

## 14. Why this is structurally sound (v2 self-check)

Per Codex's challenge: "is the plan structurally sound?" v2 addresses every objection:

| Codex concern | v2 response |
|---------------|-------------|
| `platformInternalKeys` ≠ `Type=SYSTEM` | `topology.ControlPlaneEnvKeys` allowlist preserved as separate gate |
| `value` in classify-prompt violates safety | DROPPED; `valuePreview` + `valueKind` partial only |
| Topology stdlib violated | Conversion bridges in `inventory/`; topology stays pure |
| `[]map[string]any` data-loss abstraction | REPLACED by typed `EnvView` struct |
| `connections` lossy | EXPANDED with CREATING + reverse-index |
| `managedEnvCatalog` makes LLM join | REPLACED with fully-qualified refs |
| `suggestedBucket` unsafe single value | KEPT advisory with confidence + alternatives + contextNote |
| Cross-project under-modeled | Explicit `trustBoundary` + `targetExistingEnvs` |
| `envSyncStatus` overpromises | RENAMED to `pendingPlatformSync` with tri-state |
| Missing actionability axis | ADDED |

Per Architecture audit: every layer rule confirmed:
- topology stdlib-only — pinned by `architecture_test.go`
- platform imports no internal/ — pinned
- ops doesn't import workflow/tools/recipe — pinned
- `EnvAccessor` interface dropped instead of forced through abstraction
- Mapper field preservation enforced by AST test (new)

Per Multi-service stress test: every shape problem addressed:
- envSummary scales (perService + serviceTopology reverse-index)
- Managed env catalog fully-qualified
- Setup-block awareness via `lastDeployedSetup` + opt-in yaml graph
- Cross-project trust boundary explicit
- Worker/pair semantics surfaced (`lastDeployedSetup`)

Per Safety audit: every property has a pinning test or invariant doc.

This is the structurally sound version.

---

---

## 15. Secret-handling principles (unified policy table)

Per Safety audit cross-axis recommendation. v2 touches secrets in four surfaces; this is the canonical policy across all of them.

| Surface | Default behavior | Override | Pin |
|---------|------------------|----------|-----|
| `zerops_discover includeEnvs=true` (no `includeValues`) | keys + metadata only; no `EnvValueView` | n/a | `TestDiscover_KeysOnly_NoValueView` |
| `zerops_discover includeEnvValues=true`, sensitive=false | `EnvValueView.Full` populated | n/a | `TestDiscover_NonSensitive_FullValue` |
| `zerops_discover includeEnvValues=true`, sensitive=true | `EnvValueView{Redacted:true, Preview, Hash, Length, Kind, Shape}`; `Full` empty | `includeSensitiveValues=true` populates `Full` | `TestDiscover_Sensitive_RedactedByDefault` |
| `zerops_discover includeEnvValues=true resolveRefs=true` (Phase 5a) | `EnvValueView.Resolved` populated; sensitive leaves redacted to `[REDACTED:sha8]` placeholders inline | `includeSensitiveValues=true` → full plain `Resolved` + plain `Leaf` per step | `TestResolveRefs_SensitiveLeafRedacted_*` |
| classify-prompt rows (launch + export) | `valuePreview` + `valueShape` + `valueKind` + `valueLength`; NEVER `value` (full), NEVER `resolved` | none — minimal-disclosure is absolute | `TestHandleExport_ClassifyPromptDoesNotLeakValues` (extended) + `TestLaunchClassifyPrompt_NoRawValueLeak` (new) + `TestClassifyPrompt_RejectsResolveRefs` (new) |
| `zerops_env action="get"` | same as `discover includeEnvValues=true` (delegates) | `includeSensitiveValues=true` per discover semantics | `TestEnvGet_RedactionInherited` |
| `zerops_env action="generate-dotenv"` → `.env` file | full values written to disk | n/a — file path IS the disclosure boundary; user gitignores | `TestGenerateDotenv_WritesFullValues` (existing) |

### Redact-trigger predicate (S1 layered)

```go
// internal/topology/env_classification.go
//
// ShouldRedact returns true when the env value should be redacted by default
// in agent-facing responses. Layered policy: any of three triggers fires:
//   1. Server-marked sensitive (best signal but unreliable for ZCP_API_KEY etc.)
//   2. Key matches credentialPattern (suffix _KEY|_SECRET|_TOKEN|_PASS|APP_KEY)
//   3. Key is in ControlPlaneEnvKeys (literal bearer tokens like ZCP_API_KEY)
//
// Protects against future server-flag drift; tested per-trigger.
func ShouldRedact(key string, sensitive bool) bool {
    if sensitive {
        return true
    }
    if CredentialPattern.MatchString(key) {
        return true
    }
    if IsControlPlane(key) {
        return true
    }
    return false
}
```

### Minimal-disclosure principle for classify-prompt

The classify-prompt response is **the most secret-sensitive shape in the entire surface** (cross-project — source-project secrets land in the agent context window if exposed). v2's policy: full value NEVER appears in classify-prompt under any flag. Agent that needs full disclosure for debugging calls `zerops_discover service=X includeSensitiveValues=true` separately, which logs the access via auth audit trail.

This is a **strictly stronger** invariant than v1 (which had no constraint). Existing test `TestHandleExport_ClassifyPromptDoesNotLeakValues` is extended; symmetric test added for launch-classify.

### EnvValueView.Shape — value-pattern discriminator (S5 enhancement)

Adds qualitative signal without leaking the value. Detected via regex registry in topology:

| Shape value | Detection regex (illustrative) | Use |
|-------------|-------------------------------|-----|
| `stripe-live-key` | `^sk_live_` | external-secret bucket bias (production traffic) |
| `stripe-test-key` | `^sk_test_` | external-secret bucket bias (test traffic) |
| `github-pat` | `^github_pat_` | external-secret |
| `github-pat-classic` | `^ghp_` | external-secret |
| `openai-key` | `^sk-[A-Za-z0-9]{40,}` | external-secret |
| `jwt` | `^eyJ[A-Za-z0-9_-]+\.eyJ` | likely auto-secret if signing |
| `managed-ref` | `\$\{[a-z]+_[a-z]+\}` | infrastructure |
| `url-https` | `^https://` | plain-config or infrastructure (depends on host) |
| `uuid` | UUID v4 regex | plain-config |
| `hex32` | 32-char hex string | auto-secret (Laravel APP_KEY pattern) |
| `base64` | base64 alphabet + padding | likely auto-secret |
| `empty` | empty string | plain-config (or review needed) |
| `unknown` | no match | fall back to key-name pattern |

Allows agent to verify a suggested bucket: "agent classified APP_KEY as auto-secret; value shape is hex32 — matches Laravel/Django convention — confidence high".

### Unified test surface

Single table-driven test `TestRedactionPolicy_*` covering every (surface, sensitivity, override) combination. Adding a new redaction surface requires extending the table — fails closed if missed.

---

## 16. Final pre-RED checklist

Before Phase 0 starts:

- [ ] Karel approves §3 architecture corrections (topology stdlib-only confirmed, EnvAccessor drop, typed structs)
- [ ] Karel approves §4 multi-service additions (zeropsYamlEnvGraph opt-in, connections shape, managedEnvCatalog with refs)
- [ ] Karel approves §15 secret-handling unified policy
- [ ] Karel decides on Open Q A (envclass placement: keep as policy package — recommended)
- [ ] Karel decides on Open Q C (actionability server-computed in workflow context — recommended)
- [ ] Karel decides on Open Q E (zerops_env action=get deprecation — defer to v3)

Then Phase 0+1 ships as one PR (foundation). Subsequent phases ship as outlined in §13.

---

**End of plan v2.** Awaiting Karel review.
