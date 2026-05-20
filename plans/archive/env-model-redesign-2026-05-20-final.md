# Plan FINAL — Env-var lifecycle redesign

**Date:** 2026-05-20
**Status:** Final after 4 independent review rounds. Supersedes `env-model-fundamental-redesign-2026-05-20.md` (v1) and `env-model-fundamental-redesign-2026-05-20-v2.md` (v2). Both retained as historical reference.

**Review trail:**
- v1: initial proposal based on 39-eval analysis
- v2: refined after Karel's "rozšíř na multi-service, by-design safe" review (codex + 3 agents)
- v3 (this doc): refined after Karel's "test live + token-efficient + LLM-consumer perspective" review (codex resolveRefs critique + LLM-consumer agent on 70 transcripts + empirical confirmation from `env_generate_test.go` fixtures + live discover calls against eval-zcp)

---

## 1. What v2 got over-engineered

Two independent reviewers (codex on resolveRefs design + LLM-consumer agent walking 70 transcripts across 6 workflows) converged on the same verdict:

> **v2's response surface is ~40% larger than eval evidence supports.** Many fields are either dead (zero references in transcripts), workflow-specific (load-bearing in 1 of 6 flows), or category-theoretically clean but bloated for LLM cognition.

| What v2 proposed | What evidence supports | Action |
|------------------|------------------------|--------|
| `EnvValueView.Hash` (sha256-of-secret) | Zero diff workflows in 70 transcripts; for low-entropy secrets the hash IS metadata leakage | **DROP** |
| `EnvValueView.Raw` (template form) | Reconstructable from key + Kind | **DROP** |
| `EnvValueView.Length` | No agent decision turns on it | **DROP** |
| `annotations.isReference` | Same fact as `Kind == "reference"/"reference-composed"` | **COLLAPSE into Kind** |
| `EnvRef.Raw` | Reconstructable from provider+keys | **DROP** |
| `connections[].direction` | Zero filter uses; agents only ask "what's connected" | **DROP** |
| `connections[].status` as enum | One transcript referenced CREATING; rare | **MERGE into hostname suffix** `"db", "search (CREATING)"` |
| `envSummary.serviceTopology` reverse-index | Zero queries observed; trivially derivable | **DROP** (+ v2 had copy-paste bug at lines 325-339) |
| `pendingPlatformSync` + `since` | Zero transcripts referenced sync-state; observable truth is container-side; field whose doc-comment says "don't react to this" is itself the bug | **DROP** entirely |
| `suggestedClassification.alternatives[]` | Zero overrides observed; when primary wrong, agent asks user, not array | **DROP** |
| `suggestedClassification.contextNote` | Same surface as rationale | **MERGE into rationale** |
| `actionability` 7-value enum | Three verb-actions cover all transcript patterns | **REDUCE to 3** values: `omit / reference-safe / needs-action` |
| `ReservedKeyContext{Regime, Scope, Action}` | Only authored-yaml scope ever triggers agent action | **REDUCE to `{reserved:bool, hint:string}`** |
| `managedEnvCatalog.normal + .elevated` split | 2-bucket layout = cognitive load; flat with `purpose` annotation carries identical signal | **MERGE flat with purpose field** |
| Bulk `resolveRefs=true` on discover | Bulk resolved provenance = high complexity, moderate token cost, weakly bounded value; invites agents to reason over provenance instead of using runtime truth (SSH + container `env`) | **REPLACE with narrow `zerops_env action="resolve"` per-env tool action** |
| `LastModified` field per env | One transcript ever (ambiguous choice arbitration) | **OMIT-BY-DEFAULT** (recompute on demand) |
| `staleRefTargets` annotation | Debug-only signal | **OMIT-BY-DEFAULT** |
| Shape/preview/suggestedClassification on every discover entry | Only useful in classify-prompt | **WORKFLOW-SCOPE** to classify-prompt response only |
| Audit-log claim for sensitive reads | No existing audit infrastructure in MCP server | **DROP claim**; defer audit to future work |

**Net effect:** Response payload reduction ~35-45% on typical discover responses with zero eval-observed signal loss.

---

## 2. Empirical grounding for design (Karel's verification ask)

### Test-fixture confirmation of managed-service env shape

`internal/ops/env_generate_test.go:177-197` is the load-bearing fixture pinning real platform behavior:

```go
serviceEnvs: map[string][]platform.ServiceEnvVar{
    "db": {
        {ID: "e1", Key: "user", Content: "myuser"},
        {ID: "e2", Key: "password", Content: "s3cret"},
        {ID: "e3", Key: "hostname", Content: "db"},
        {ID: "e4", Key: "port", Content: "5432"},
        {ID: "e5", Key: "connectionString",
         Content: "postgresql://${user}:${password}@${hostname}:${port}/main"},
    },
},
```

**Key observations:**
- `db.connectionString` is a **literal template string** — not a resolved value
- Template uses **lone refs** (`${user}` not `${db_user}`) because they're sibling lookups within db's own env namespace
- Resolution model is hierarchical: cross-service ref (`${db_connectionString}`) → fetch host's envs → template with lone refs → resolve as siblings within source service

### Recursive resolver behavior confirmed

`internal/ops/env_generate.go::refExpander` (cycle-safe + depth-capped + service-cached):
- Cycle detection is **path-local** (visited map copied per descent — shared subtrees `A=${b}_${c}; b=${d}; c=${d}` do NOT false-positive)
- Max-depth 16 per chain (not total tree)
- Service env list cached after first fetch (no duplicate `GetServiceEnv`)
- **API call count for 5-hop chain across 3 services:** 1 ListServices + 3 GetServiceEnv

### Live discover behavior on eval-zcp (verified)

Live `mcp__zcp__zerops_discover includeEnvs=true includeEnvValues=true`:
- Service-scoped query silently omits project envs ✓ (confirmed bug, matches v1+v2 finding)
- `ZCP_API_KEY` returns plaintext + `Sensitive=false` ✓ (matches `envclass/classify.go:18` documentation)
- Platform envs `staticCdnUrl`, `zeropsSubdomainHost`, etc. return as `Type=SYSTEM` per SDK (confirmed via mock test paths)

### LLM-consumer empirical audit (70 transcripts)

Cut-list of 19 reductions derived from field-by-field frequency analysis. Methodology: each EnvView field × 6 lifecycle workflows × frequency-of-reference in transcript thinking blocks + tool calls + retries. Validated via mentions vs. absences.

---

## 3. Final response shape

### Discover (default + variants)

**Default `zerops_discover includeEnvs=true`** — single canonical call returns full picture:

```json
{
  "project": {
    "id": "...", "name": "eval-zcp",
    "envs": [
      {"key": "APP_KEY", "scope": "project", "origin": "user", "sensitive": true},
      {"key": "GIT_TOKEN", "scope": "project", "origin": "user", "sensitive": true,
       "annotations": {"controlPlane": true, "hint": "platform-injected for git-push"}},
      {"key": "zeropsSubdomainHost", "scope": "project", "origin": "system"}
    ]
  },
  "services": [
    {
      "hostname": "api", "type": "ubuntu/nodejs@22", "status": "ACTIVE",
      "connections": ["db", "cache"],
      "lastDeployedSetup": "dev",
      "envs": [
        {"key": "DATABASE_URL", "scope": "service:api", "origin": "user",
         "kind": "reference-composed",
         "annotations": {"refTargets": [{"provider": "db", "keys": ["user","password","hostname","port","dbName"]}]},
         "actionability": ["reference-safe"]}
      ]
    },
    {
      "hostname": "db", "type": "postgresql@18", "status": "ACTIVE",
      "managedEnvCatalog": [
        {"key": "hostname", "ref": "${db_hostname}", "sensitive": false},
        {"key": "user", "ref": "${db_user}", "sensitive": false},
        {"key": "password", "ref": "${db_password}", "sensitive": true},
        {"key": "dbName", "ref": "${db_dbName}", "sensitive": false},
        {"key": "connectionString", "ref": "${db_connectionString}", "sensitive": true,
         "annotations": {"completenessFlags": {"includesDbName": false},
                         "warning": "omits /${db_dbName}; compose explicitly for Prisma/Drizzle/sqlx"}},
        {"key": "superUser", "ref": "${db_superUser}", "purpose": "DDL/migrations only"},
        {"key": "superUserPassword", "ref": "${db_superUserPassword}", "purpose": "DDL/migrations only", "sensitive": true}
      ]
    }
  ],
  "envSummary": {
    "project": {"totalUser": 2, "totalSystem": 1, "totalSensitive": 2},
    "perService": {"api": {"withReferences": 1}, "db": {"managedCatalogEntries": 7}}
  }
}
```

**`includeEnvValues=true`** — adds typed `value` block per entry:

```json
{"key": "APP_KEY", ..., "value": {"redacted": true, "preview": "gCl...19Y", "kind": "literal"}}
{"key": "NODE_ENV", ..., "value": {"redacted": false, "full": "development", "kind": "literal"}}
```

For sensitive entries: `preview` (first-6 + last-3) + `kind` only. `full` omitted unless explicit override.

**`includeSensitiveValues=true`** — populates `value.full` for sensitive entries too. Server logs the access in a future audit pipeline (deferred; see §11 Open Q).

### Classify-prompt (workflow-scoped — different shape)

```json
{
  "classifications": [
    {
      "key": "APP_KEY",
      "currentBucket": "",
      "suggestedBucket": "auto-secret",
      "trustBoundary": "source-project",
      "valuePreview": "gCl...19Y",
      "valueShape": "hex32",
      "valueKind": "literal",
      "sensitive": true,
      "rationale": "credentialPattern: _KEY suffix on USER project env"
    },
    {
      "key": "STRIPE_SECRET",
      "suggestedBucket": "external-secret",
      "valuePreview": "sk_l...XE7",
      "valueShape": "stripe-live-key",
      "sensitive": true,
      "rationale": "value shape stripe-live-key → external SDK credential"
    }
  ]
}
```

**Hard invariant:** classify-prompt rows NEVER carry `value.full`. Even with `includeSensitiveValues=true`. Pinned by `TestHandleExport_ClassifyPromptDoesNotLeakValues` (existing) + new symmetric test for launch-classify.

### Per-env resolve (new tool action, replaces bulk `resolveRefs`)

```
zerops_env action="resolve" serviceHostname="api" key="DATABASE_URL"
```

Default flat response:
```json
{
  "key": "DATABASE_URL",
  "raw": "${db_connectionString}",
  "resolved": "postgresql://myuser:[REDACTED:s1]@db:5432/main",
  "status": "complete",
  "depth": 3,
  "warnings": []
}
```

`trace=true` adds chain provenance:
```json
{
  "key": "DATABASE_URL", "raw": "...", "resolved": "...", "status": "complete", "depth": 3, "warnings": [],
  "chain": [
    {"step": "DATABASE_URL", "via": "${db_connectionString}", "service": "db"},
    {"step": "db.connectionString", "expands": "postgresql://${user}:${password}@${hostname}:${port}/main"},
    {"step": "db.user", "leaf": "myuser"},
    {"step": "db.password", "leaf": "[REDACTED:s1]", "sensitive": true},
    {"step": "db.hostname", "leaf": "db"},
    {"step": "db.port", "leaf": "5432"}
  ]
}
```

`includeSensitiveValues=true` replaces `[REDACTED:s1]` with plaintext. Server-side guards:

- `maxRefsWalked: 32` — hard cap on ref-traversal count (catches pathological branching)
- `maxExpandedBytes: 8192` — hard cap on resolved-string length (catches expanding-string attacks)
- `maxChainEntries: 24` — hard cap on chain length for trace mode
- Cycle / max-depth / unknown-ref → `status` becomes `"partial-cycle" | "partial-max-depth" | "partial-unknown-ref"` + warnings array populated
- Reuse `refExpander` from `env_generate.go` with **mode flag** so `.env` generation stays strict-error mode (cycle/unknown-ref fails) while resolve diagnostic mode returns partial-status

**Redaction labels** — per-response `[REDACTED:s1]` / `[REDACTED:s2]` (counter per call), NOT stable sha hashes. Reason: low-entropy secrets + stable hashes = metadata leakage in transcripts. Per-response labels enable disambiguation within one response without enabling cross-response correlation.

### Reduced actionability (3 values)

```go
type Actionability string
const (
    ActionOmit          Actionability = "omit"          // never put in .env / export / classify (ZCP_API_KEY, *_TOKEN ZCP-internal)
    ActionReferenceSafe Actionability = "reference-safe"// fine in ${...} refs (managed-service exposed envs)
    ActionNeedsAction   Actionability = "needs-action"  // requires agent decision (classify-prompt unbucketed, reserved-key violation)
)
```

Single-value field per env (not array). Maps directly to agent's 3 decision verbs.

### Simplified reservedKey annotation

```json
"annotations": {
  "reserved": true,
  "reservedHint": "API rejects in run.envVariables — pick a different key name"
}
```

Only populated when scope is *authored-yaml* (i.e., this annotation fires on synthesized yaml validation, not on observed managed-service envs that happen to share a name with a reserved key).

### Merged managedEnvCatalog (flat with purpose annotation)

```json
"managedEnvCatalog": [
  {"key": "hostname", "ref": "${db_hostname}", "sensitive": false},
  {"key": "user", "ref": "${db_user}", "sensitive": false},
  {"key": "password", "ref": "${db_password}", "sensitive": true},
  {"key": "superUser", "ref": "${db_superUser}", "purpose": "DDL/migrations only"},
  {"key": "superUserPassword", "ref": "${db_superUserPassword}", "purpose": "DDL/migrations only", "sensitive": true}
]
```

Agents filter by `purpose` field if they care; default reading order is "scan list, find what you need".

### Connections — flat string array with optional suffix

```json
"connections": ["db", "cache", "search (CREATING)"]
```

Agents split on `" ("` only when they need provisioning state. 95% of reads ignore the suffix.

---

## 4. Architecture (carried from v2 with corrections)

### Layer placement (corrected per Codex Q3 architecture audit)

```
topology/env_classification.go         — stdlib only
  • EnvOrigin enum (user/system) + EnvOriginFromString(s string) helper
  • ReservedKeyRegime + HardReservedKeys + RunScopeReservedKeys maps
  • ControlPlaneEnvKeys explicit allowlist + IsControlPlane(key) helper
  • CredentialPattern regex
  • SecretClassification enum (existing) + Actionability enum (3-value)
  • Shape detector registry (regex map for stripe-live-key, jwt, hex32, ...) — pure functions, stdlib

inventory/envs.go                       — Layer 2 platform bridge
  • ProjectEnvVar / ServiceEnvVar type aliases (existing)
  • ProjectEnvOrigin(platform.ProjectEnvType) topology.EnvOrigin
  • ServiceEnvOrigin(platform.ServiceEnvType) topology.EnvOrigin
  • FetchProjectEnvs / FetchServiceEnvs (existing)

ops/env_view.go                         — typed response model
  • EnvView, EnvValueView, EnvAnnotations, EnvRef structs
  • projectEnvViewsFrom(envs []platform.ProjectEnvVar, opts ViewOpts) []EnvView
  • serviceEnvViewsFrom(envs []platform.ServiceEnvVar, scope string, opts ViewOpts) []EnvView

ops/env_resolve.go                      — new file for per-env resolve tool
  • Resolve(ctx, client, projectID, hostname, key, opts) (*ResolveResult, error)
  • Wraps refExpander with diagnostic-mode flag + resource guards

envclass/classify.go                    — Layer 3 policy (keeps semantic)
  • ClassifyServiceEnv / ClassifyProjectEnv (Decision + Bias)
  • Uses topology.CredentialPattern, topology.IsControlPlane
  • New rules (framework-specific overrides) belong here

knowledge/managed_envs.go               — offline fallback catalog
  • Static table per service-stack-type for recipe-authoring (no live project)
  • Imported only by zerops_knowledge tool

internal/ops/env_generate.go::refExpander
  • Refactored to public type with mode flag (StrictMode for .env-gen, DiagnosticMode for resolve)
  • Resource guards: maxRefsWalked, maxExpandedBytes, maxChainEntries
  • Reused by both env_generate (existing) and env_resolve (new)
```

### Typed `EnvView` struct (final, post-cuts)

```go
package ops

type EnvView struct {
    Key           string                 `json:"key"`
    Scope         EnvScope               `json:"scope"`                  // "project" | "service:<host>"
    Origin        topology.EnvOrigin     `json:"origin"`                 // "user" | "system"
    Sensitive     bool                   `json:"sensitive,omitempty"`
    Editable      *bool                  `json:"editable,omitempty"`     // nil for service scope
    Kind          string                 `json:"kind,omitempty"`         // "literal" | "reference" | "reference-composed" | "preprocessor-directive" | "empty"
    Value         *EnvValueView          `json:"value,omitempty"`        // populated when includeEnvValues=true
    Annotations   EnvAnnotations         `json:"annotations,omitzero"`
    Actionability topology.Actionability `json:"actionability,omitempty"` // single value (not array)
}

type EnvValueView struct {
    Redacted bool   `json:"redacted"`
    Preview  string `json:"preview,omitempty"`  // first-6 + last-3 (sensitive only)
    Full     string `json:"full,omitempty"`     // populated when not sensitive OR includeSensitiveValues=true
    // No Hash, no Length, no Raw, no Resolved* (moved to resolve action)
}

type EnvAnnotations struct {
    RefTargets         []EnvRef        `json:"refTargets,omitempty"`        // empty when Kind != reference*
    SelfShadow         bool            `json:"selfShadow,omitempty"`
    Reserved           bool            `json:"reserved,omitempty"`          // only when authored-yaml context
    ReservedHint       string          `json:"reservedHint,omitempty"`
    ControlPlane       bool            `json:"controlPlane,omitempty"`      // matches topology.ControlPlaneEnvKeys
    CompletenessFlags  map[string]bool `json:"completenessFlags,omitempty"` // db connectionString etc.
    Warning            string          `json:"warning,omitempty"`
}

type EnvRef struct {
    Provider string   `json:"provider"`        // owning hostname
    Keys     []string `json:"keys"`            // ["user","password","hostname","port","dbName"]
    // No Raw — agent reconstructs ${provider_key}
}
```

### Reuse `refExpander` with mode flag (Codex insight)

```go
// internal/ops/env_generate.go (existing refExpander, refactored)

type ResolveMode int
const (
    ResolveStrict     ResolveMode = iota // errors on cycle/unknown-ref (used by .env generation)
    ResolveDiagnostic                    // partial-status on cycle/unknown-ref (used by resolve action)
)

type RefResolver struct {
    Mode             ResolveMode
    MaxRefsWalked    int  // default 32
    MaxExpandedBytes int  // default 8192
    MaxChainEntries  int  // default 24
    // ...existing fields...
}

func (r *RefResolver) Resolve(value string) (ResolveOutput, error) {
    // strict mode → existing behavior (error)
    // diagnostic mode → return partial + status + warnings
}
```

Keeps `.env` generation strict (no behavior change) while enabling diagnostic surface.

### CLAUDE.md invariant + AST-based test

```
"Mapping functions (map* prefix) must preserve all platform-exposed env-related
 fields by default; field drops require inline comment justification + AST-based
 test enforcement (TestMappers_AllFieldsJustified in platform/zerops_mappers_test.go)."
```

AST test parses `func map*` in `platform/zerops_mappers.go`, extracts input SDK type field set, asserts every input field either appears in output type OR has an inline comment `// SDK.<Field> dropped: <reason>` on the output struct.

---

## 5. Phased migration (revised)

11 phases, ~6-8 working days (down from 7-9 in v2 due to scope reductions).

### Phase 0 — SDK field plumbing
- `platform/types.go::ProjectEnvVar/ServiceEnvVar`: add `Created`, `LastUpdate` (parsed from SDK, not yet surfaced to LLM)
- `platform/zerops_env.go`: populate fields
- Tests: `TestGetProjectEnv_ParsesLastUpdate_*`

### Phase 1 — Typed EnvView + topology foundation
- `internal/topology/env_classification.go` (new): EnvOrigin, ReservedKey* maps, ControlPlaneEnvKeys, CredentialPattern, 3-value Actionability, Shape regex registry
- `internal/topology/env_classification_test.go` (new): closed-enum + lookup tables + shape regex
- `internal/ops/inventory/envs.go`: add ProjectEnvOrigin / ServiceEnvOrigin bridges
- `internal/platform/types.go`: drop EnvAccessor interface
- `internal/ops/env_view.go` (new): typed EnvView struct + projectEnvViewsFrom + serviceEnvViewsFrom
- `internal/ops/helpers.go`: delete `envVarsToMaps`
- `internal/ops/discover.go`: use `[]EnvView` in response

### Phase 2 — Annotations + envSummary
- `EnvAnnotations` populated (refTargets, selfShadow, reserved, controlPlane, completenessFlags, warning)
- `envSummary` computed (project + perService — no serviceTopology)
- Actionability assigned (3-value, single-field-per-env)

### Phase 3 — Service-scoped discover returns project envs too
- `internal/ops/discover.go:76-96`: remove early-return; `attachProjectEnvs()` always called when includeEnvs=true
- Invert test invariant `TestDiscover_ProjectEnvs_AlwaysIncludedWhenIncludeEnvs`

### Phase 4 — mapEsServiceStack preserves UserData + ConnectedStacks + ActiveAppVersion
- `internal/platform/zerops_mappers.go::mapEsServiceStack`: keep UserData + ConnectedStacks + ActiveAppVersion
- `internal/platform/types.go::ServiceStack`: add `Envs []ServiceEnvVar`, `Connections []string` (flat with suffix), `LastDeployedSetup string`
- ⚠ NO `pendingPlatformSync` / `HasUnsyncedUserDataRecord` surfacing (dropped per LLM-consumer audit)

### Phase 5 — Discover surfaces connections + lastDeployedSetup + managedEnvCatalog
- `connections []string` flat with optional suffix `(CREATING)` / `(DELETING)` for non-ACTIVE
- `lastDeployedSetup` from ActiveAppVersion metadata
- `managedEnvCatalog` flat with `purpose` annotation for elevated keys
- Eliminate per-service `GetServiceStackEnv` fetch when ListServices already returned embedded UserData (project-wide unscoped query)

### Phase 6 — Per-env resolve tool (NEW)
- `internal/ops/env_resolve.go` (new): `Resolve(ctx, client, projectID, hostname, key, opts) (*ResolveResult, error)`
- `internal/tools/env.go`: add action `"resolve"` accepting `serviceHostname`, `key`, `trace`, `includeSensitiveValues`
- Reuse `refExpander` with mode flag + resource guards
- `[REDACTED:s1]` per-response labels (not sha hashes)
- Tests: `TestResolve_Single1Hop_*`, `TestResolve_Recursive_*`, `TestResolve_Cycle`, `TestResolve_MaxDepth`, `TestResolve_MaxRefsWalked`, `TestResolve_MaxExpandedBytes`, `TestResolve_SensitiveLeafRedacted`, `TestResolve_TraceMode`

### Phase 7 — classify-prompt safety-compliant response (workflow-scoped fields)
- `internal/tools/workflow_launch_production.go::handleLaunchClassifyPrompt`: response carries valuePreview + valueShape + valueKind + suggestedClassification + trustBoundary; NO value.full ever
- `internal/tools/workflow_export.go::classifyPromptResponse`: same shape (strengthens existing invariant)
- `internal/tools/workflow_export.go:471`: **fix `hostname=` → `service=`** code bug
- `launchClassifyRow` struct: new fields (suggestedBucket, valueShape, etc.)
- Tests: `TestHandleExport_ClassifyPromptDoesNotLeakValues` extended; `TestLaunchClassifyPrompt_NoRawValueLeak` (new)

### Phase 8 — zeropsYamlEnvGraph opt-in
- `internal/ops/yaml_env_graph.go` (new): parse zerops.yaml setup blocks
- `internal/ops/discover.go`: when `includeYamlEnvGraph=true` and yaml accessible, populate `zeropsYamlEnvGraph` field
- Multi-setup yaml visibility for Laravel-showcase-style projects

### Phase 9 — Atom rewrites
- `launch-classify-platform-envs.md`: deleted (folded into new `env-classification-buckets.md`)
- `launch-classify-prompt.md`, `export-classify-envs.md`: simplified (accept suggestedBucket bias by default, override-rationale must be stated)
- `develop-first-deploy-env-vars.md`: replace inline cheatsheet with managedEnvCatalog reference
- `develop-env-var-model.md`: brief envSummary mention; "to inspect resolved chain: zerops_env action=resolve"
- Atom hostname=→service= already addressed by workflow_export.go:471 code fix

### Phase 10 — Consolidation + invariant pin
- `internal/ops/env_generate.go::platformInternalKeys`: replace consumers with `topology.IsControlPlane(key)` allowlist
- `internal/ops/env_plan.go:367`: same migration
- `internal/ops/deploy_validate.go::hardReservedEnvKeys / runScopeReservedEnvKeys`: data moves to topology; CheckReservedEnvNames stays in ops
- `internal/envclass/classify.go::credentialPattern`: moves to topology (keeps existing test pinning)
- `internal/ops/helpers.go::platformInjectedKeys` + `envKeyZeropsSubdomain`: DELETE
- `TestNoLegacyEnvDenylists` (new): drift-gate
- `TestMappers_AllFieldsJustified` (new): AST-based mapper-field-preservation check
- CLAUDE.md invariant added

### Phase 11 — Verification + eval re-run
- `make lint-local` + `go test ./... -race` green
- Atom goldens regenerated
- Eval re-run for 5 env-heavy scenarios: develop-loop, launch-production-from-standard-pair, launch-production-pipeline-not-configured, recipe-laravel-showcase-fullstack, greenfield-node-postgres-dev-stage
- Compare transcript discover/env call counts pre/post — expect classify-prompt to be 1 call (was 2-3), develop-loop to read managedEnvCatalog refs directly (no hostname pattern-match)

---

## 6. Safety policy (unified)

| Surface | Default | Override | Pin |
|---------|---------|----------|-----|
| `discover includeEnvs=true` (no values) | keys + annotations + actionability | n/a | `TestDiscover_KeysOnly_NoValueView` |
| `discover includeEnvValues=true`, sensitive=false | `value.full` populated | n/a | `TestDiscover_NonSensitive_FullValue` |
| `discover includeEnvValues=true`, sensitive=true | `value.preview` (6+3) + `value.kind`; `full` empty | `includeSensitiveValues=true` → populate `full` | `TestDiscover_Sensitive_RedactedByDefault` |
| `env action="resolve"` default | `resolved` with `[REDACTED:s1]` placeholders for sensitive leaves | `includeSensitiveValues=true` → plain leaves | `TestResolve_SensitiveLeafRedacted` |
| `env action="resolve" trace=true` | adds `chain[]` with same redaction rule | same | `TestResolve_TraceMode_SensitiveRedacted` |
| classify-prompt rows (launch + export) | `valuePreview` + `valueShape` + `valueKind` + `suggestedClassification` + `trustBoundary`; **NEVER `value.full`, NEVER `resolved`** | none — minimal-disclosure absolute | `TestHandleExport_ClassifyPromptDoesNotLeakValues` + `TestLaunchClassifyPrompt_NoRawValueLeak` |
| `env action="generate-dotenv"` → `.env` file | full plain values to disk | n/a — file path IS disclosure boundary | `TestGenerateDotenv_WritesFullValues` |

### Redact-trigger predicate

```go
// internal/topology/env_classification.go
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

Layered: server-marked Sensitive OR credential-pattern OR control-plane allowlist. Protects against server-flag drift (ZCP_API_KEY case).

### Per-response REDACTED labels (Codex insight)

`[REDACTED:s1]`, `[REDACTED:s2]`, ... counter per call. NOT stable sha hashes — those leak metadata for low-entropy secrets. Labels enable within-response disambiguation without enabling cross-response correlation. If stable diff truly needed in future, use HMAC keyed outside transcript.

### What's NOT in scope

- **Audit logging for sensitive reads** — claim removed. No existing MCP audit infrastructure. Defer to future work (when/if audit pipeline exists).
- **Cross-project value classification redaction during classify-prompt** — already absolute (no value field ever).
- **Per-env access-rate-limiting** — not in scope; rely on user discipline + resource guards.

---

## 7. Multi-service correctness (verified against scenarios)

| Project shape | Default discover response | Token cost (est.) | Issues addressed |
|---------------|---------------------------|-------------------|------------------|
| Single-runtime + single-managed (api + db) | 5 envs project + 2 svc × 8 entries each | ~1 KB | baseline |
| Multi-runtime + single-managed (frontend + api + db) | 3 svc, ~30 envs total | ~2 KB | connections shows wiring |
| Multi-managed (api + db + cache + search + storage) | 5 svc, managedEnvCatalogs sum to 20+ entries | ~3 KB | flat catalog avoids LLM joins |
| Recipe Laravel showcase (3 runtimes + 4 managed) | 7 svc, 50+ envs | ~4 KB | lastDeployedSetup distinguishes worker; opt-in yamlEnvGraph |
| Standard-mode pair + multi-managed | 4 svc | ~2.5 KB | pair handled via meta indexing (existing) |
| Launch-production cross-project classify | 5 envs | ~600 bytes | trustBoundary explicit, no value leak |

### Multi-setup yaml visibility

`zerops_discover includeYamlEnvGraph=true` (opt-in) parses zerops.yaml setup blocks. Returns:

```json
"zeropsYamlEnvGraph": {
  "setups": {
    "dev":    {"runEnvVariables": {"APP_ENV": "development"}, "buildEnvVariables": {}},
    "prod":   {"runEnvVariables": {"APP_ENV": "production"}, "buildEnvVariables": {}},
    "worker": {"runEnvVariables": {"APP_ENV": "production", "QUEUE_DRIVER": "redis"}}
  },
  "warnings": []
}
```

For Laravel-showcase-style multi-setup projects this is the structural surface that v1+v2 missed entirely.

---

## 8. Definition of done

1. **Typed `EnvView` struct** replaces `[]map[string]any`. No `any`-map data-loss path.
2. **Single source of truth for env taxonomy** in `topology/env_classification.go`. envclass remains Layer-3 policy. ops/env_resolve.go provides diagnostic per-env tool.
3. **Safety invariants pinned by tests:**
   - classify-prompt never carries raw value or resolved value
   - Sensitive default redacts to `value.preview` + `value.kind`
   - Control-plane keys filtered independently of Type=SYSTEM
   - Reserved-key annotation scope-aware
4. **Multi-service first-class:** managedEnvCatalog with full `${refs}`, connections w/ status suffix, lastDeployedSetup, opt-in zeropsYamlEnvGraph
5. **Per-env resolve diagnostic:** narrow tool action with flat default response + opt-in trace + resource guards
6. **Actionability axis (3-value):** verb-oriented bridge between platform truth and agent action
7. **CLAUDE.md invariant + AST test** prevents future mapping-function field-drop regression
8. **Eval friction baseline:** re-run 5 env-heavy scenarios; expected:
   - launch-classify single-call (was 2-3)
   - develop-loop reads managedEnvCatalog refs verbatim (no hostname pattern-match)
   - worker-setup scenarios surface lastDeployedSetup distinctly
9. **No regressions:** control-plane filtering preserved (now via topology, not deleted denylist); .env generation behavior unchanged (refExpander strict mode preserved)

---

## 9. Risks

### R1. Typed-struct migration ripples through golden tests
Phase 1 lands as single PR with all golden regens. Mitigation: explicit reviewer checklist; atom lint catches axis drift.

### R2. Per-response REDACTED labels lose stable comparison
For workflows that DO need diff (none observed in 70 transcripts, but future possibility), the per-response label scheme breaks. Mitigation: documented as design choice; revisit if eval evidence emerges.

### R3. ListServices payload heavier (UserData embedded)
~1-3 KB per service × ~10 services = ~30 KB extra payload on every workflow status check. Mitigation: introduce `ListServicesOpts{IncludeEnvs bool}` (default false). discover sets true; other callers default false.

### R4. zeropsYamlEnvGraph parse failures
zerops.yaml may be unparseable mid-edit. Mitigation: `zeropsYamlEnvGraph: null` + warning; discover doesn't fail.

### R5. refExpander mode flag adds branch to existing code path
`.env` generation must stay strict. Mitigation: explicit `ResolveStrict` constant + tests for both modes; mode flag is non-optional on the resolver constructor.

### R6. Live-derived managedEnvCatalog drift
Live SDK is source-of-truth via embedded UserData (Phase 4); knowledge/managed_envs.go is offline-only fallback for recipe authoring. Drift only matters in offline path. Mitigation: contract test in `+build live` mode against eval-zcp.

---

## 10. Open questions for Karel

### A. Audit log for sensitive reads — build or defer?
- **Defer (recommended):** Drop the audit claim from v2 §15. Adding `includeSensitiveValues=true` without audit IS an acceptable risk for pre-production. Build audit pipeline when ZCP gets multi-tenancy or persistent storage.
- **Build:** Adds ~1 day for a basic file-based audit log (per-projectID JSONL of "tool action key timestamp"). No PII in the log, just access records.

### B. workflow_export.go:471 code-bug fix — Phase 7 or Phase 9?
- Code emits `zerops_discover hostname=...` instruction (wrong param name). v2 puts fix in Phase 9 atom rewrite. v3 puts in Phase 7 (classify-prompt overhaul) since it's a code change in the same handler. **Recommend Phase 7** (code-level fix alongside response shape change).

### C. envclass package post-refactor — keep or merge?
- **Keep** (recommended per Codex Q4): envclass remains Layer-3 decision-tree policy. Topology owns vocabulary + regex; envclass owns Decision orchestration. New rules (framework-specific overrides) land in envclass.
- **Merge into topology:** simpler package count but conflates vocabulary with policy.

### D. `zerops_env action="get"` deprecation
- **Defer to v4** (recommended): 0 calls across 70 transcripts but harmless to keep. Mark "discover preferred" in atom guidance; remove later when test data confirms zero usage.

### E. `lastDeployedSetup` source — runtime metadata or yaml parse?
- **Runtime metadata** (recommended): `EsServiceStack.ActiveAppVersion` carries setup name. Cheap.
- **yaml parse:** secondary fallback when no deploy yet.

---

## 11. Sequencing + timeline

| Bundle | Phases | Days | Notes |
|--------|--------|------|-------|
| Foundation | 0+1+3 | 2 | SDK plumbing, typed struct, project-envs-in-service-query |
| Multi-service data | 2+4+5 | 2-3 | annotations, ConnectedStacks preserve, connections/lastDeployedSetup/managedEnvCatalog |
| Resolve tool | 6 | 1 | new action with guards + redaction labels |
| Classify-prompt + code-bug fix | 7 | 1 | safety-compliant response + workflow_export.go:471 |
| YAML graph | 8 | 1 | opt-in zeropsYamlEnvGraph |
| Atoms + consolidation | 9+10 | 1 | atom rewrites + cleanup + AST test |
| Verify | 11 | 0.5 | eval re-run validation |

**Total: 6-8 working days** (reduced from v2's 7-9 due to scope reductions).

---

## 12. What v3 explicitly does NOT change (carried from v2/v1)

- 3-channel env merge model (project < yaml < .env.local) for .env generation — internal mechanic, unchanged
- 4-bucket SecretClassification enum — keep
- `generate-dotenv` tool behavior — unchanged
- Bootstrap/develop/launch-production workflow flow — unchanged
- Deploy preflight env validation (env_refs + env_self_shadow checks) — unchanged
- Self-shadow detection rule (exact-match `key: ${key}`) — unchanged
- Reserved-key enforcement at deploy preflight — unchanged

---

## 13. Why v3 is structurally final

**Independent reviews converged:**
- Codex critiqued resolveRefs as bulk-discover param → narrow per-env tool. ✅ Adopted (Phase 6).
- LLM-consumer agent walked 70 transcripts × 6 workflows × every field → 19 specific reductions. ✅ All adopted in §1.
- My live-testing confirmed test-fixture-grade understanding of resolution model (`connectionString` is literal template with lone refs). ✅ Phase 6 design matches platform reality.
- Architecture audit confirmed topology stdlib-only boundary via Layer-2 bridges (inventory). ✅ Preserved.
- Safety audit identified ZCP_API_KEY-as-control-plane edge case + per-response label scheme + missing audit. ✅ All addressed.

**The plan ships what evidence supports, no more.** Empty fields with no eval evidence ARE bugs in API design. v3 corrects v2's surface bloat (~40% reduction) while keeping all signal that 70 transcripts showed agents actually use.

**End of plan v3.** Awaiting Karel approval.
