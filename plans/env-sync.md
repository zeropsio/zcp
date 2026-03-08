# Plan: `zcp env-sync` Command + Local Flow Foundation

## Context

### Local Flow Vision

Zerops currently uses a **remote dev** model: `appdev` + `appstage` services both run in Zerops cloud. Dev-to-dev deploys are used for iteration. The **local flow** replaces this:

- **Dev = user's local machine** (Win/Mac/Linux) with ZCP + zcli installed
- User connects via **zcli VPN** to the Zerops project
- Local machine gets direct network access to all managed services (db, cache, etc.)
- Deployments go **local → stage** (via `zcli push` or CI/CD git push)
- No `appdev` service in Zerops — only `appstage` + managed services

**Key problem**: On Zerops, env vars are auto-managed (injected into containers, `${hostname_varName}` refs resolved at runtime). Locally, this doesn't work. The app needs a `.env` file with **resolved values**.

### Team Discussion Summary

- zerops.yml `envVariables` should be the **single source of truth** for what envs an app needs
- A `zcp env-sync` command should produce a `.env` with resolved values from live Zerops data
- Secret envs (currently auto-injected) should eventually require explicit reference in zerops.yml
- Future: `envVariablesFile: .env.zerops` to avoid cluttering zerops.yml with 80% env definitions
- Most frameworks already support dotenv loading — `.env` is the universal local format

### Zerops Platform Behavior (verified on live API)

- `${hostname_varName}` references resolved at container level, NOT by API
- Invalid references silently kept as literal strings — no error from API
- `${db_connectionString}` → resolved to actual value (e.g., `postgresql://...`)
- `${db_totallyFakeVar}` → literal string `${db_totallyFakeVar}` in container
- `${nonexistent_something}` → literal string in container
- Env vars from `GetServiceEnv()` contain **unresolved** `${...}` patterns
- Inside running container, env vars contain **resolved** actual values
- **Implication for env-sync**: Cannot use `GetServiceEnv()` on the target service to get resolved values of `${...}` refs — must resolve them ourselves by fetching env vars from the REFERENCED services

### Team Discussion — Full Context (Nov 2025 – Mar 2026)

**Core insight (Aleš)**: Secret envs are used both as secrets AND as "convenient to change at runtime" vars. The `envVariables` section in zerops.yml makes up 80% of its content and makes it hard to read.

**Proposed model**:
1. Secret envs ("environment vault") should NOT be auto-injected into containers
2. Apps must explicitly reference them in zerops.yml: `API_KEY: ${API_KEY}`
3. This creates a **definitive list** of what envs an app needs
4. Enable `envVariablesFile: .env.zerops` to avoid cluttering zerops.yml
5. `zcli env sync [hostname]` command produces `.env` with resolved values

**Aleš's example**:
```yaml
# Before (current): secret env API_KEY=foobar auto-injected, zerops.yml only has refs
envVariables:
  DB_URL: ${db_connectionString}

# After (proposed): everything explicit
envVariables:
  DB_URL: ${db_connectionString}
  API_KEY: ${API_KEY}
```

Then `zcli env sync` outputs:
```
DB_URL=postgres://db@...
API_KEY=foobar
```

**Karlos's concern**: On local, apps need to load `.env` but on Zerops they read from OS env. Some frameworks (like Nette) need a special driver for `.env` loading.

**Aleš's response**: 90% of frameworks have dotenv support. The `zcli env sync` just outputs to stdout — format and target file are up to the user.

**Karlos's insight on local flow**: You wouldn't want the hostname (appdev) to exist in Zerops at all. No dev service — just push from local to stage.

**Aleš confirmed**: Local dev needs exactly the same envs that the dev service would have had. The zerops.yml `envVariables` defines what those are.

**Petra's work** (in progress): Adding `sensitive` flag to envs — will separate true secrets from non-secrets.

**Jan Saidl's concern**: Linking .env files into zerops.yml is non-trivial — needs to work with GitHub/GitLab, locally, and in GUI redeploy.

---

## Current System — Deep Analysis

**No .env handling exists in ZCP today.** All env vars are managed through the Zerops API.

### File Map

| Component | File | Role |
|-----------|------|------|
| API methods | `internal/platform/zerops_env.go` | `GetServiceEnv()`, `GetProjectEnv()`, `SetServiceEnvFile()`, `CreateProjectEnv()`, `DeleteProjectEnv()`, `DeleteUserData()` |
| Data types | `internal/platform/types.go:115-119` | `EnvVar{ID, Key, Content}` — no `isSecret` or `scope` field |
| Client interface | `internal/platform/client.go` | `Client` interface with all env methods |
| Mock | `internal/platform/mock.go` + `mock_methods.go` | `MockClient` with `WithServices`, `WithServiceEnv`, `WithProjectEnv` |
| MCP tool handler | `internal/tools/env.go` | `RegisterEnv()`, `EnvInput{Action, ServiceHostname, Project, Variables}` — actions: "set", "delete" |
| Ops business logic | `internal/ops/env.go` | `EnvSet()`, `EnvDelete()` — service-level batched, project-level per-var |
| Ref parsing | `internal/ops/deploy_validate.go:244-278` | `parseEnvRefs()` / `envRef{raw, hostname, varName}` — **unexported** |
| Ref validation | `internal/ops/deploy_validate.go:210-242` | `ValidateEnvReferences()` checks refs against discovered vars |
| zerops.yml parsing | `internal/ops/deploy_validate.go:76-123` | `ZeropsYmlDoc`, `ZeropsYmlEntry`, `ParseZeropsYml()`, `FindEntry()` |
| Helpers | `internal/ops/helpers.go` | `parseEnvPairs()`, `envVarsToMaps()`, `crossRefPattern`, `platformInjectedKeys` |
| Discovery | `internal/ops/discover.go` | `Discover()` with `includeEnvs=true` → `attachEnvs()`, `attachProjectEnvs()`, `addEnvRefNotes()` |
| Workflow state | `internal/workflow/bootstrap.go:48` | `BootstrapState.DiscoveredEnvVars map[string][]string` |
| Workflow checks | `internal/tools/workflow_checks_generate.go:79-96` | Validates env refs during generate step |
| CLI pattern | `cmd/zcp/eval.go` | Reference for subcommand structure, auth bootstrap via `initPlatformClient()` |
| CLI entrypoint | `cmd/zcp/main.go` | Subcommand dispatch switch |

### Platform API Details

**Service env vars** (`GetServiceEnv`):
```go
func (z *ZeropsClient) GetServiceEnv(ctx context.Context, serviceID string) ([]EnvVar, error)
// SDK: z.handler.GetServiceStackEnv(ctx, path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)})
// Returns all env vars for a service (user-defined + platform-injected like zeropsSubdomain)
```

**Setting service envs** (`SetServiceEnvFile`):
```go
func (z *ZeropsClient) SetServiceEnvFile(ctx context.Context, serviceID string, content string) (*Process, error)
// SDK: z.handler.PutServiceStackUserDataEnvFile(ctx, path, envBody)
// Content format: "KEY=value\nKEY2=value2\n" (all vars in single file)
// Single process for all vars (atomic)
```

**Project env vars** (`GetProjectEnv`):
```go
func (z *ZeropsClient) GetProjectEnv(ctx context.Context, projectID string) ([]EnvVar, error)
// Uses PostProjectSearch with clientId + projectId filters
// Env vars embedded in project object (project.EnvList)
```

**Creating project envs** (`CreateProjectEnv`):
```go
func (z *ZeropsClient) CreateProjectEnv(ctx context.Context, projectID, key, content string, sensitive bool) (*Process, error)
// One API call per variable (not batched)
// sensitive bool = secret marking (only at project level!)
```

### Key Data Structure

```go
// internal/platform/types.go
type EnvVar struct {
    ID      string `json:"id"`      // UUID
    Key     string `json:"key"`     // Variable name
    Content string `json:"content"` // Value (may contain ${...} references as literals)
}
```

**Important**: No `isSecret`, `scope`, or `sensitive` field in the returned struct. The API doesn't distinguish secret vs non-secret in GET responses — only `CreateProjectEnv` accepts a `sensitive` bool on creation.

### Reference System Details

**Pattern**: `${hostname_varName}` — exactly one underscore separating hostname from var name.

```go
// internal/ops/deploy_validate.go (currently unexported)
type envRef struct {
    raw      string // e.g. "${db_connectionString}"
    hostname string // e.g. "db"
    varName  string // e.g. "connectionString"
}
```

**Resolution**: References are resolved **at container level** by the Zerops runtime, NOT by the API. When you call `GetServiceEnv()`, you get the literal `${db_connectionString}` string. Inside a running container, it's resolved to the actual PostgreSQL connection string.

**Invalid references**: Silently kept as literal strings by Zerops. No API error, no deploy failure. `${db_totallyFakeVar}` → literal string `${db_totallyFakeVar}` in the container. ZCP's `ValidateEnvReferences()` catches these before deploy.

**Platform-injected keys** (auto-generated, not user-defined):
```go
var platformInjectedKeys = map[string]bool{
    "zeropsSubdomain": true,
}
```

### Env Annotation in Discovery

When `includeEnvs=true`, `Discover()` returns env vars as annotated JSON maps:
```go
[]map[string]any{
    {"key": "DATABASE_URL", "value": "${db_connectionString}", "isReference": true, "isPlatformInjected": false},
    {"key": "zeropsSubdomain", "value": "app-xyz", "isReference": false, "isPlatformInjected": true},
}
```

### Workflow Integration

Bootstrap workflow stores discovered env var **names only** (not values):
```go
type BootstrapState struct {
    DiscoveredEnvVars map[string][]string // hostname -> [var names]
    // e.g. {"db": ["connectionString", "port", "user", "password", "host"]}
}
```

Used during generate step to validate `zerops.yml` references before deployment.

### Common Managed Service Env Vars (from bootstrap.md)

| Service Type | Common Env Vars |
|-------------|-----------------|
| PostgreSQL | `connectionString`, `host`, `port`, `user`, `password` |
| Valkey/Redis | `connectionString`, `host`, `port`, `password` |
| MariaDB | `connectionString`, `host`, `port`, `user`, `password` |
| MongoDB | `connectionString`, `host`, `port`, `user`, `password` |
| RabbitMQ | `connectionString`, `host`, `port`, `user`, `password` |

### zerops.yml Structure (relevant parts)

```go
type ZeropsYmlDoc struct {
    Zerops []ZeropsYmlEntry `yaml:"zerops"`
}

type ZeropsYmlEntry struct {
    Setup        string            `yaml:"setup"`        // hostname
    Build        zeropsYmlBuild    `yaml:"build"`
    Deploy       zeropsYmlDeploy   `yaml:"deploy"`
    Run          zeropsYmlRun      `yaml:"run"`
    EnvVariables map[string]string `yaml:"envVariables"` // KEY -> VALUE or ${ref}
}
```

### Test Infrastructure

- `internal/tools/env_test.go` — Tool handler tests (set/delete polling)
- `internal/ops/env_test.go` — Ops layer tests (partial failure, project-level)
- `internal/ops/helpers_test.go` — `envVarsToMaps`, reference annotation
- `internal/ops/deploy_validate_test.go` — 620+ lines, extensive ref validation tests
- `internal/ops/discover_test.go` — Discovery with env attachment

---

## Implementation Plan

### Step 1: Export `parseEnvRefs` from ops package

**File**: `internal/ops/deploy_validate.go`

- Rename `envRef` → `EnvRef` (export fields: `Raw`, `Hostname`, `VarName`)
- Rename `parseEnvRefs` → `ParseEnvRefs`
- Update the single internal caller (`ValidateEnvReferences`)
- Update tests in `internal/ops/deploy_validate_test.go` if they reference the unexported names

### Step 2: Add `ParseZeropsYmlFile` helper

**File**: `internal/ops/deploy_validate.go`

```go
func ParseZeropsYmlFile(path string) (*ZeropsYmlDoc, error)
```

Reads from exact file path (vs `ParseZeropsYml` which appends `/zerops.yml` to a dir). Needed for `--config` flag support.

### Step 3: Create `internal/envsync/` package (TDD — tests first)

Follows existing pattern of `internal/init/` and `internal/eval/` as standalone CLI subcommand packages.

**File**: `internal/envsync/sync.go` (~150 lines)

```go
type Config struct {
    Hostname   string // target service hostname in zerops.yml
    ConfigPath string // zerops.yml path (default: "./zerops.yml")
    OutputFile string // output path (default: ".env")
    Stdout     bool   // write to stdout instead of file
    Diff       bool   // show diff against existing .env without writing
}

type Result struct {
    EnvCount   int
    Warnings   []string
    OutputPath string // empty if stdout
}

func Sync(ctx context.Context, client platform.Client, projectID string, cfg Config) (*Result, error)
```

**Core logic**:
1. Parse zerops.yml via `ops.ParseZeropsYml` or `ops.ParseZeropsYmlFile`
2. Find entry for `cfg.Hostname` via `doc.FindEntry()`
3. Fetch **all env sources** and build lookup:
   a. All project services → `client.ListServices()` + `client.GetServiceEnv()` for each referenced hostname
   b. Service's own env vars (secret/user envs set on the target service)
   c. Project-level env vars via `client.GetProjectEnv()`
4. Build lookup map: `hostname → varName → resolvedValue`
5. Start with zerops.yml `envVariables` — resolve all `${hostname_varName}` refs
6. **Merge in** service's own env vars (secret envs) that aren't already in zerops.yml
7. **Merge in** project env vars (resolved values) that aren't already present
8. Collect warnings for unresolvable refs
9. Format as `.env` and write to file or stdout

**Merge priority** (highest wins): zerops.yml envVariables > service env vars > project env vars

### Runtime Env Changes

Secret/project envs can be added or changed at any time via GUI, API, or `zerops_env` MCP tool. The sync command handles this naturally:

- **Scenario A — Initial setup**: `zcp env-sync appstage` → generates `.env` with all current values.
- **Scenario B — Env added at runtime**: Someone adds `NEW_SECRET=xyz` to the service via GUI. Re-run → `.env` includes `NEW_SECRET=xyz`.
- **Scenario C — Env value changed**: `API_KEY` changed from `old` to `new`. Re-run → `.env` updated.
- **Scenario D — Project env added**: New project-level env `GLOBAL_FLAG=true`. Re-run → merged in.

Every run is a **full snapshot** — fetches all live data, resolves, writes. No incremental logic. The `.env` is fully regenerated each time (`# Generated by zcp env-sync` header signals it's managed).

**User-added values in .env**: Overwritten on re-sync. This is by design — zerops.yml + Zerops API is the source of truth. For local-only overrides, users should use `.env.local` (standard dotenv convention).

### Adoption Scenarios

**Scenario 1 — Greenfield (new project)**: Straightforward — no conflicts, no existing state.

**Scenario 2 — Brownfield (existing project with .env)**:
1. **Diff mode**: `zcp env-sync appstage --diff` — shows what WOULD change without writing:
   - `+ NEW_VAR=value` (would be added)
   - `- OLD_VAR=value` (exists in .env but not in Zerops — would be lost)
   - `~ CHANGED_VAR: "old" → "new"` (value differs)
2. User reviews, moves local-only vars to `.env.local`
3. Run `zcp env-sync appstage` → clean `.env` from Zerops

**File**: `internal/envsync/resolve.go` (~80 lines)

```go
func resolveValue(value string, lookup map[string]map[string]string) (string, []string)
func buildEnvLookup(ctx context.Context, client platform.Client, projectID string, hostnames []string) (map[string]map[string]string, error)
```

**File**: `internal/envsync/format.go` (~100 lines)

```go
func FormatDotEnv(vars map[string]string) string
func ParseDotEnv(content string) map[string]string  // parse existing .env for diff
func DiffEnvs(existing, generated map[string]string) []DiffEntry
```

```go
type DiffEntry struct {
    Key      string
    Type     string // "added", "removed", "changed"
    OldValue string // for "changed" and "removed"
    NewValue string // for "changed" and "added"
}
```

- Sort keys alphabetically for deterministic output
- Quote values with spaces, `#`, `"`, `$`, newlines
- Header comment: `# Generated by zcp env-sync — do not edit manually`
- `ParseDotEnv`: simple KEY=VALUE parser (skip comments, handle quoted values)
- `DiffEnvs`: compare existing vs generated, produce human-readable diff

### Step 4: Wire CLI subcommand

**File**: `cmd/zcp/envsync.go` (~80 lines)

```
Usage: zcp env-sync <hostname> [flags]

Flags:
  --stdout              Output to stdout instead of file
  -o, --output <path>   Output file path (default: .env)
  --config <path>       Path to zerops.yml (default: ./zerops.yml)
  --diff                Show what would change without writing (compares against existing .env)
```

Project envs and service secret envs are **always included** (no flag needed).

Auth bootstrap: reuse `initPlatformClient()` pattern from `cmd/zcp/eval.go`.

**File**: `cmd/zcp/main.go` — add case:

```go
case "env-sync":
    runEnvSync(os.Args[2:])
    return
```

### Step 5: Tests

| File | Tests |
|------|-------|
| `internal/envsync/sync_test.go` | `TestSync_BasicResolve`, `TestSync_PlainValues`, `TestSync_UnresolvableRef_Warning`, `TestSync_NoEnvVariables`, `TestSync_MissingEntry_Error`, `TestSync_MultipleRefsInValue`, `TestSync_MergesSecretEnvs`, `TestSync_MergesProjectEnvs`, `TestSync_MergePriority` |
| `internal/envsync/resolve_test.go` | `TestResolveValue_AllResolved`, `TestResolveValue_Mixed`, `TestBuildEnvLookup` |
| `internal/envsync/format_test.go` | `TestFormatDotEnv_Simple`, `TestFormatDotEnv_SpecialChars`, `TestFormatDotEnv_Sorted`, `TestFormatDotEnv_ConnectionString`, `TestParseDotEnv_Basic`, `TestParseDotEnv_QuotedValues`, `TestDiffEnvs_Added`, `TestDiffEnvs_Removed`, `TestDiffEnvs_Changed`, `TestDiffEnvs_NoChanges` |

All table-driven. Use `platform.MockClient` for API mocking.

### Step 6: Add knowledge guide for LLM

**File**: `internal/knowledge/guides/env-sync.md` (new)

Short guide so the LLM agent knows `zcp env-sync` exists and can instruct users:
- What it does, usage examples, when to use it (local flow)
- Not an MCP tool — CLI command the user runs directly
- Re-run after env changes (new secrets, project envs, managed service changes)
- Use `.env.local` for local-only overrides (not overwritten by sync)

### Step 7: Update CLAUDE.md

Add `internal/envsync` to Architecture table.

---

## Key Reuse

- `ops.ParseZeropsYml()` / `ops.FindEntry()` — zerops.yml parsing
- `ops.ParseEnvRefs()` (after export) — ref extraction
- `platform.Client.ListServices()` / `GetServiceEnv()` / `GetProjectEnv()` — API calls
- `auth.ResolveCredentials()` + `auth.Resolve()` — auth flow
- `platform.MockClient` — test mocking

---

## Local Flow Vision

### Architecture

```
LOCAL MACHINE (dev)                     ZEROPS PROJECT
├── App source code                     ├── db (postgresql@16) ← VPN access
├── zerops.yml (source of truth)        ├── cache (valkey@7) ← VPN access
├── .env (generated by zcp env-sync)    ├── storage (object-storage) ← VPN access
├── zcp (MCP server)                    └── appstage (python@3.12) ← deploy target
└── zcli (VPN + push)
```

### Workflow

1. `zcli vpn up` — connect to Zerops project network
2. `zcp env-sync appstage` — generate `.env` with resolved values from managed services
3. Develop locally — app reads `.env`, connects to real db/cache via VPN
4. `zcli push appstage` or `git push` (CI/CD) — deploy to stage

### Key Differences from Remote Flow

| Aspect | Remote (current) | Local (new) |
|--------|-----------------|-------------|
| Dev environment | `appdev` in Zerops | Local machine |
| Env vars | Auto-injected by Zerops | `.env` file via `zcp env-sync` |
| Managed service access | Internal Zerops network | VPN tunnel |
| Deploy iteration | dev→dev SSH deploy | Local run + hot reload |
| Stage deploy | dev→stage or separate push | local→stage via zcli/CI |
| Cost | Pays for dev service | No dev service cost |

### Env Sync Model

Three env sources are merged (highest priority wins):

1. **zerops.yml `envVariables`** — resolved `${hostname_varName}` refs (highest priority)
2. **Service env vars** — secret/user envs set directly on the service
3. **Project env vars** — project-level envs (lowest priority)

```yaml
# zerops.yml
zerops:
  - setup: appstage
    envVariables:
      DATABASE_URL: ${db_connectionString}
      CACHE_HOST: ${cache_host}
```

Plus service has secret env `API_KEY=foobar`, project has `SENTRY_DSN=https://...`

`zcp env-sync appstage` produces:

```env
# Generated by zcp env-sync — do not edit manually
API_KEY=foobar
CACHE_HOST=cache
DATABASE_URL=postgresql://user:pass@db:5432/db
SENTRY_DSN=https://...
```

### Future Enhancements (not in scope now)

- `envVariablesFile: .env.zerops` in zerops.yml to reduce env clutter
- Watch mode: `zcp env-sync --watch` re-syncs when Zerops envs change
- Multi-service sync: `zcp env-sync --all` generates .env for all services
- `.env.zerops` template file with `${...}` refs (Zerops platform feature)

---

## Verification

1. **Unit tests**: `go test ./internal/envsync/... -v`
2. **Manual test**: In a project with `zerops.yml` referencing `${db_connectionString}`:
   ```bash
   zcp env-sync appdev --stdout
   # Should output: DATABASE_URL=postgresql://...
   ```
3. **E2E**: Against live Zerops project with db service, verify resolved connection string is valid

## File Summary

| File | Action | Lines (est.) |
|------|--------|-------------|
| `internal/ops/deploy_validate.go` | Edit (export envRef/parseEnvRefs, add ParseZeropsYmlFile) | ~20 changed |
| `internal/envsync/sync.go` | New | ~150 |
| `internal/envsync/resolve.go` | New | ~80 |
| `internal/envsync/format.go` | New | ~100 |
| `internal/envsync/sync_test.go` | New | ~200 |
| `internal/envsync/resolve_test.go` | New | ~100 |
| `internal/envsync/format_test.go` | New | ~180 |
| `cmd/zcp/envsync.go` | New | ~80 |
| `cmd/zcp/main.go` | Edit (3 lines) | ~3 changed |
| `internal/knowledge/guides/env-sync.md` | New | ~40 |
| `CLAUDE.md` | Edit (1 row in Architecture) | ~1 changed |
