# Dependency Analysis

> Source: PRD `design/zcp-prd.md` sections 2, 10, 12, 14
> Source go.mod files: `../zaia/go.mod`, `../zaia-mcp/go.mod`, `/Users/macbook/Sites/mcp60/go.mod`

---

## go.mod Content

Module path: `github.com/zeropsio/zcp`
Go version: `1.24.0`

### Required Dependencies (exact versions from source)

| Dependency | Version | Source | Purpose |
|-----------|---------|--------|---------|
| `github.com/modelcontextprotocol/go-sdk` | `v1.2.0` | zaia-mcp/go.mod | MCP server, tools, STDIO transport |
| `github.com/zeropsio/zerops-go` | `v1.0.16` | zaia/go.mod | Zerops API SDK |
| `github.com/blevesearch/bleve/v2` | `v2.5.7` | zaia/go.mod | BM25 full-text search (knowledge) |
| `gopkg.in/yaml.v3` | `v3.0.1` | zaia/go.mod, zaia-mcp/go.sum | YAML parsing (validate, import) |

### Test Dependencies

| Dependency | Version | Source |
|-----------|---------|--------|
| `github.com/stretchr/testify` | `v1.10.0` | zaia/go.mod (direct in go.sum) |

### NOT Needed (PRD section 10)

- `github.com/spf13/cobra` — no CLI framework, simple `os.Args[1]` dispatch
- Subprocess executor infrastructure — direct API calls, no shelling to zaia

### Proposed go.mod

```go
module github.com/zeropsio/zcp

go 1.24.0

require (
	github.com/blevesearch/bleve/v2 v2.5.7
	github.com/modelcontextprotocol/go-sdk v1.2.0
	github.com/zeropsio/zerops-go v1.0.16
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/stretchr/testify v1.10.0
)
```

After `go mod tidy`, indirect dependencies will be populated automatically. See "Transitive Dependency Analysis" below for expected indirects.

---

## Dependency Graph

### Internal Package Dependencies

```
cmd/zcp/main.go
  ├── internal/init          (when os.Args[1] == "init")
  │     └──> internal/content    (shared embedded templates)
  │
  └── MCP server mode (no args):
      │
      internal/server
        ├──> internal/tools      (MCP tool handlers)
        │      └──> internal/ops     (business logic)
        │             ├──> internal/platform  (Zerops API client)
        │             ├──> internal/auth      (token + project discovery)
        │             ├──> internal/knowledge (BM25 search)
        │             └──> internal/content   (workflow content)
        │
        │    internal/tools also imports internal/platform (types only: PlatformError)
        │
        └──> internal/knowledge  (MCP resource registration)
```

### Compact Import Map

```
cmd/zcp       → server, init
server        → tools, knowledge
tools         → ops, platform (types only)
ops           → platform, auth, knowledge, content
platform      → zerops-go SDK, stdlib
auth          → platform
knowledge     → bleve, stdlib
content       → stdlib (go:embed only)
init          → content, stdlib
```

### External Dependency Map

```
internal/platform  → github.com/zeropsio/zerops-go
                     github.com/shopspring/decimal (transitive via zerops-go)
internal/knowledge → github.com/blevesearch/bleve/v2
                     github.com/json-iterator/go (transitive via bleve → geo)
internal/server    → github.com/modelcontextprotocol/go-sdk
                     github.com/google/jsonschema-go (transitive via go-sdk)
                     golang.org/x/oauth2 (transitive via go-sdk)
ops/validate       → gopkg.in/yaml.v3
ops/import         → gopkg.in/yaml.v3 (dryRun validation)
```

### Dependency Rules (PRD section 2.2)

| Package | May Import | MUST NOT Import |
|---------|-----------|-----------------|
| `tools/` | `ops/`, `platform/` (types only) | `platform.Client` methods directly |
| `ops/` | `platform/`, `auth/`, `knowledge/`, `content/` | `tools/`, `server/` |
| `platform/` | `zerops-go`, stdlib | anything internal |
| `auth/` | `platform/` | `ops/`, `tools/` |
| `knowledge/` | `bleve`, stdlib | anything internal |
| `content/` | stdlib (`go:embed`) | anything external or internal |
| `init/` | `content/`, stdlib | `ops/`, `tools/`, `platform/` |

---

## File Creation Order (Compilation-Respecting)

### Batch 1 — No Internal Dependencies (parallel-safe)

These packages depend only on stdlib and/or external modules. They can all be created in parallel because they have zero internal imports.

| File | Package | External Deps | Internal Deps |
|------|---------|--------------|---------------|
| `internal/platform/types.go` | platform | stdlib only | None |
| `internal/platform/errors.go` | platform | stdlib only | None (uses types from same package) |
| `internal/platform/client.go` | platform | stdlib only | None (uses types from same package) |
| `internal/platform/mock.go` | platform | stdlib only | None (implements client.go interface) |
| `internal/knowledge/documents.go` | knowledge | stdlib (`go:embed`) | None |
| `internal/knowledge/query.go` | knowledge | stdlib | None |
| `internal/knowledge/engine.go` | knowledge | `bleve/v2` | None |
| `internal/content/content.go` | content | stdlib (`go:embed`) | None |

**Within platform/**: Files must be created in order `types.go` → `errors.go` → `client.go` → `mock.go` because they reference types from the same package. Go compiles packages as a unit, so they compile together, but for development they should be created in this logical order.

**Within knowledge/**: `documents.go` defines the `Document` type used by `engine.go` and `query.go`. Create `documents.go` first, then the others.

**Compilation verification**:
```bash
go build ./internal/platform/...
go build ./internal/knowledge/...
go build ./internal/content/...
```

### Batch 2 — Depends on Batch 1 Packages

| File | Package | Internal Deps |
|------|---------|---------------|
| `internal/platform/zerops.go` | platform | Same-package (types, errors, client interface) |
| `internal/platform/logfetcher.go` | platform | Same-package (types, client) |
| `internal/platform/apitest/harness.go` | platform/apitest | `platform` (for ZeropsClient, types) |
| `internal/platform/apitest/cleanup.go` | platform/apitest | `platform` (for Client interface) |
| `internal/auth/auth.go` | auth | `platform` (for Client, types) |
| `internal/init/init.go` | init | `content` |
| `internal/init/templates.go` | init | `content` |

**Compilation verification**:
```bash
go build ./internal/platform/...
go build ./internal/platform/apitest/...
go build ./internal/auth/...
go build ./internal/init/...
```

### Batch 3 — Depends on Batch 2 Packages

| File | Package | Internal Deps |
|------|---------|---------------|
| `internal/ops/helpers.go` | ops | `platform` (types for service resolution) |
| `internal/ops/progress.go` | ops | `platform` (Client for polling) |
| `internal/ops/discover.go` | ops | `platform` |
| `internal/ops/manage.go` | ops | `platform` |
| `internal/ops/env.go` | ops | `platform` |
| `internal/ops/logs.go` | ops | `platform` (Client + LogFetcher) |
| `internal/ops/import.go` | ops | `platform` + `yaml.v3` |
| `internal/ops/validate.go` | ops | `yaml.v3` (no platform dep) |
| `internal/ops/delete.go` | ops | `platform` |
| `internal/ops/subdomain.go` | ops | `platform` |
| `internal/ops/events.go` | ops | `platform` |
| `internal/ops/process.go` | ops | `platform` |
| `internal/ops/context.go` | ops | None (static string) |
| `internal/ops/workflow.go` | ops | `content` |
| `internal/ops/deploy.go` | ops | `platform`, `auth` |

All ops files are in the same package and can be created in parallel. They depend on Batch 1/2 packages but not on each other (no cross-file deps within ops except through shared types).

**Compilation verification**:
```bash
go build ./internal/ops/...
```

### Batch 4 — Depends on Batch 3 Packages

| File | Package | Internal Deps |
|------|---------|---------------|
| `internal/tools/convert.go` | tools | `platform` (PlatformError type) |
| `internal/tools/discover.go` | tools | `ops`, `platform` (types) |
| `internal/tools/manage.go` | tools | `ops`, `platform` (types) |
| `internal/tools/env.go` | tools | `ops`, `platform` (types) |
| `internal/tools/logs.go` | tools | `ops`, `platform` (types) |
| `internal/tools/deploy.go` | tools | `ops`, `platform` (types) |
| `internal/tools/import.go` | tools | `ops`, `platform` (types) |
| `internal/tools/validate.go` | tools | `ops` |
| `internal/tools/knowledge.go` | tools | `knowledge` |
| `internal/tools/process.go` | tools | `ops`, `platform` (types) |
| `internal/tools/delete.go` | tools | `ops`, `platform` (types) |
| `internal/tools/subdomain.go` | tools | `ops`, `platform` (types) |
| `internal/tools/events.go` | tools | `ops`, `platform` (types) |
| `internal/tools/context.go` | tools | `ops` |
| `internal/tools/workflow.go` | tools | `ops` |

All tools files can be created in parallel (same package, no cross-deps).

**Compilation verification**:
```bash
go build ./internal/tools/...
```

### Batch 5 — Depends on Batch 4 (final assembly)

| File | Package | Internal Deps |
|------|---------|---------------|
| `internal/server/instructions.go` | server | None (static string) |
| `internal/server/server.go` | server | `tools`, `knowledge` |
| `cmd/zcp/main.go` | main | `server`, `init`, `auth` |

**Compilation verification**:
```bash
go build ./internal/server/...
go build ./cmd/zcp/...
```

---

## Build Verification Commands per Phase

### Phase 1: Foundation (platform + auth + knowledge)

```bash
# After platform types/errors/client/mock:
go build ./internal/platform/...
go vet ./internal/platform/...
go test ./internal/platform/... -count=1 -short

# After apitest harness:
go build ./internal/platform/apitest/...

# After zerops.go + logfetcher.go:
go build ./internal/platform/...
go test ./internal/platform/... -count=1 -short

# After auth:
go build ./internal/auth/...
go test ./internal/auth/... -count=1 -short

# After knowledge:
go build ./internal/knowledge/...
go test ./internal/knowledge/... -count=1 -short

# Phase 1 gate (mock tests):
go test ./internal/platform/... ./internal/auth/... ./internal/knowledge/... -count=1 -short -v

# Phase 1 gate (API contract — requires ZCP_API_KEY):
go test ./internal/platform/... ./internal/auth/... -tags api -v
```

### Phase 2: Business Logic (ops + content)

```bash
# After content:
go build ./internal/content/...

# After each ops file:
go build ./internal/ops/...
go test ./internal/ops/... -count=1 -short

# Phase 2 gate (mock tests):
go test ./internal/ops/... -count=1 -short -v

# Phase 2 gate (API contract):
go test ./internal/ops/... -tags api -v
```

### Phase 3: MCP Layer (tools + server + entrypoint)

```bash
# After tools:
go build ./internal/tools/...
go test ./internal/tools/... -count=1 -short

# After server:
go build ./internal/server/...
go test ./internal/server/... -count=1 -short

# After main.go update:
go build -o bin/zcp ./cmd/zcp

# Phase 3 gate:
go test ./internal/tools/... ./internal/server/... -count=1 -short -v
go test ./internal/tools/... -tags api -v
```

### Phase 4: Integration + E2E

```bash
# Integration tests (mock-based multi-tool flows):
go test ./integration/ -count=1 -v

# Full lifecycle E2E:
go test ./e2e/ -tags e2e -v

# Full suite:
go test ./... -count=1
go test ./... -count=1 -tags api
go test ./... -count=1 -tags e2e
```

### Phase 5: Init Subcommand

```bash
# After init package:
go build ./internal/init/...
go test ./internal/init/... -count=1 -short

# Full binary:
go build -ldflags "-s -w" -o bin/zcp ./cmd/zcp
```

---

## Binary Size Estimate

### Source Binary Sizes (measured, `-ldflags "-s -w"`, `CGO_ENABLED=0`)

| Binary | Platform | Size | Main Contributors |
|--------|----------|------|-------------------|
| zaia (CLI) | darwin/arm64 | 18 MB | bleve (BM25), zerops-go, cobra |
| zaia (CLI) | linux/amd64 | 19 MB | Same |
| zaia-mcp (MCP server) | linux/amd64 | 5.4 MB | go-sdk, (no bleve, no zerops-go direct) |
| zcp (current placeholder) | darwin/arm64 | 20 MB | (incomplete — just a stub) |

### Size Contribution Estimate

| Component | Estimated Size | Notes |
|-----------|---------------|-------|
| Go runtime + stdlib | ~3 MB | Base overhead |
| bleve (BM25) | ~10-12 MB | Largest contributor. 20+ transitive deps (roaring, zapx, vellum, etc.) |
| zerops-go SDK | ~2-3 MB | API client + types + json-iterator |
| go-sdk (MCP) | ~2-3 MB | MCP protocol + jsonschema-go + oauth2 |
| yaml.v3 | ~0.5 MB | Small |
| ZCP application code | ~0.5-1 MB | ops, tools, server, auth, knowledge engine |

### Estimate

**ZCP stripped binary (linux/amd64): ~20-22 MB**

Rationale: ZCP = zaia (18-19 MB) - cobra (~1 MB) + go-sdk (~2-3 MB). The bleve dependency dominates — it accounts for roughly 55-60% of the binary. Without bleve, ZCP would be ~8-10 MB.

**PRD target: <30 MB** — Well within budget. Current estimate is ~20-22 MB, leaving ~8-10 MB headroom.

### Size Optimization (if needed in future)

1. UPX compression: typically 30-40% reduction → ~13-15 MB (at startup cost)
2. Replace bleve with lighter BM25: custom implementation ~1-2 MB vs bleve's ~12 MB
3. Neither is needed for v1 — the PRD target is easily met.

---

## External Dependency Analysis

### github.com/modelcontextprotocol/go-sdk v1.2.0

- **Source**: zaia-mcp/go.mod (direct dependency)
- **Purpose**: MCP protocol implementation — server, tool registration, STDIO transport, progress notifications
- **Transitive deps**: 3 (`google/jsonschema-go`, `yosida95/uritemplate`, `golang.org/x/oauth2`)
- **Notes**: zaia-mcp uses v1.2.0; mcp60 (reference) uses v0.6.0 (outdated). Use v1.2.0.
- **Key API**: `mcp.NewServer()`, `mcp.NewClient()`, `InMemoryTransports()`, `ProgressNotificationParams`
- **Known issues**: None identified. v1.2.0 is stable.

### github.com/zeropsio/zerops-go v1.0.16

- **Source**: zaia/go.mod (direct dependency)
- **Purpose**: Zerops API SDK — HTTP client, request/response types, authentication
- **Transitive deps**: 2 (`shopspring/decimal`, `json-iterator/go`)
- **Notes**: zaia uses v1.0.16, mcp60 uses v1.0.14 (outdated). Use v1.0.16 (latest from zaia).
- **Key API**: `sdk.New()`, `sdk.Handler`, all API operations
- **Known issues**: `shopspring/decimal` version differs between zaia (v1.3.1) and mcp60 (v1.4.0). Go's MVS will resolve to v1.4.0 when both are present, but ZCP only uses zerops-go (not mcp60's direct dep), so v1.3.1 is expected.

### github.com/blevesearch/bleve/v2 v2.5.7

- **Source**: zaia/go.mod (direct dependency)
- **Purpose**: BM25 full-text search engine for knowledge base (65+ embedded docs)
- **Transitive deps**: 20+ (roaring, zapx/v11-v16, vellum, geo, segment, snowballstem, etc.)
- **Notes**: Largest dependency by far. All transitive deps are bleve ecosystem packages.
- **CGO_ENABLED=0**: Works fine — bleve uses pure Go implementations. go-faiss is optional and unused with CGO disabled.
- **Key API**: `bleve.NewMemOnly()`, `index.Search()`, field boosting, custom mappings
- **Known issues**: `json-iterator/go` at unusual version `v0.0.0-20171115153421-f7279a603ede` (2017 commit, not a tagged release). This is pulled by bleve's geo dependency. Works but looks odd in go.mod.

### gopkg.in/yaml.v3 v3.0.1

- **Source**: zaia/go.mod (direct), zaia-mcp/go.sum (indirect)
- **Purpose**: YAML parsing for zerops.yml and import.yml validation
- **Transitive deps**: 0
- **Notes**: Stable, universally used. No issues.

### github.com/stretchr/testify v1.10.0

- **Source**: zaia/go.sum (test dependency)
- **Purpose**: Test assertions (`assert`, `require` packages)
- **Transitive deps**: 2 (`davecgh/go-spew`, `pmezard/go-difflib`)
- **Notes**: Test-only, does not affect binary size.

---

## Transitive Dependency Analysis

### Expected Indirect Dependencies in ZCP go.mod

From **zerops-go** (2 indirects):
- `github.com/shopspring/decimal v1.3.1`
- `github.com/json-iterator/go v0.0.0-20171115153421-f7279a603ede`

From **bleve** (20+ indirects):
- `github.com/RoaringBitmap/roaring/v2 v2.4.5`
- `github.com/bits-and-blooms/bitset v1.22.0`
- `github.com/blevesearch/bleve_index_api v1.2.11`
- `github.com/blevesearch/geo v0.2.4`
- `github.com/blevesearch/go-faiss v1.0.26`
- `github.com/blevesearch/go-porterstemmer v1.0.3`
- `github.com/blevesearch/gtreap v0.1.1`
- `github.com/blevesearch/mmap-go v1.0.4`
- `github.com/blevesearch/scorch_segment_api/v2 v2.3.13`
- `github.com/blevesearch/segment v0.9.1`
- `github.com/blevesearch/snowballstem v0.9.0`
- `github.com/blevesearch/upsidedown_store_api v1.0.2`
- `github.com/blevesearch/vellum v1.1.0`
- `github.com/blevesearch/zapx/v11 v11.4.2`
- `github.com/blevesearch/zapx/v12 v12.4.2`
- `github.com/blevesearch/zapx/v13 v13.4.2`
- `github.com/blevesearch/zapx/v14 v14.4.2`
- `github.com/blevesearch/zapx/v15 v15.4.2`
- `github.com/blevesearch/zapx/v16 v16.2.8`
- `github.com/golang/snappy v0.0.4`
- `github.com/mschoch/smat v0.2.0`
- `go.etcd.io/bbolt v1.4.0`
- `golang.org/x/text v0.33.0`
- `google.golang.org/protobuf v1.36.6`

From **go-sdk** (3 indirects):
- `github.com/google/jsonschema-go v0.3.0`
- `github.com/yosida95/uritemplate/v3 v3.0.2`
- `golang.org/x/oauth2 v0.30.0`

From **testify** (2 indirects, test only):
- `github.com/davecgh/go-spew v1.1.1`
- `github.com/pmezard/go-difflib v1.0.0`

Shared:
- `golang.org/x/sys v0.29.0` (bleve + zerops-go)

**Total expected transitive dependencies**: ~30 indirect modules.

---

## Internal Package Creation Order

Specific order to create packages to avoid compilation errors. Within each step, files compile as a unit (same package). Steps must be sequential.

### Step 1: `internal/platform` (core types and interfaces)

```
1. internal/platform/types.go       — All domain types (UserInfo, Project, ServiceStack, etc.)
                                      No deps. Defines the vocabulary everything uses.
2. internal/platform/errors.go      — Error codes, PlatformError type, mapAPIError
                                      Uses types from same package.
3. internal/platform/client.go      — Client interface + LogFetcher interface
                                      Uses types from same package.
4. internal/platform/mock.go        — MockClient + MockLogFetcher (builder pattern)
                                      Implements interfaces from client.go, uses types.go.
```

After step 1: `go build ./internal/platform/...` must succeed.

### Step 2: `internal/platform` (implementations)

```
5. internal/platform/zerops.go      — ZeropsClient (zerops-go SDK implementation)
                                      Implements Client interface. External dep: zerops-go.
6. internal/platform/logfetcher.go  — ZeropsLogFetcher (HTTP-based log backend)
                                      Implements LogFetcher interface.
```

After step 2: `go build ./internal/platform/...` and `go test ./internal/platform/... -short` must succeed.

### Step 3: `internal/platform/apitest` (test harness)

```
7. internal/platform/apitest/harness.go — API test harness (real client, skip logic)
                                           Imports platform for ZeropsClient + types.
8. internal/platform/apitest/cleanup.go — Resource cleanup helpers
                                           Imports platform for Client interface.
```

After step 3: `go build ./internal/platform/apitest/...` must succeed.

### Step 4: `internal/auth`

```
9. internal/auth/auth.go             — Token resolution, project discovery
                                       Imports platform for Client + types.
```

After step 4: `go build ./internal/auth/...` and `go test ./internal/auth/... -short` must succeed.

### Step 5: `internal/knowledge` (parallel with step 4)

```
10. internal/knowledge/documents.go  — Document type, go:embed, frontmatter parsing
11. internal/knowledge/query.go      — Query expansion, suggestions, snippets
12. internal/knowledge/engine.go     — Store, Search(), List(), Get()
                                       External dep: bleve.
```

After step 5: `go build ./internal/knowledge/...` and `go test ./internal/knowledge/... -short` must succeed.

### Step 6: `internal/content` (parallel with steps 4-5)

```
13. internal/content/content.go      — go:embed for workflow markdown, CLAUDE.md template
    internal/content/workflows/      — Embedded workflow markdown files
    internal/content/templates/      — CLAUDE.md template, MCP config template, SSH config
```

After step 6: `go build ./internal/content/...` must succeed.

### Step 7: `internal/ops`

```
14. internal/ops/helpers.go          — resolveServiceID, hostname helpers, time parsing
15. internal/ops/progress.go         — PollProcess helper with callback
16. internal/ops/discover.go         — Discover logic
17. internal/ops/manage.go           — Start/stop/restart/scale
18. internal/ops/env.go              — Env get/set/delete
19. internal/ops/logs.go             — 2-step log fetch
20. internal/ops/import.go           — Import with dry-run
21. internal/ops/validate.go         — YAML validation (offline)
22. internal/ops/delete.go           — Service deletion
23. internal/ops/subdomain.go        — Subdomain enable/disable
24. internal/ops/events.go           — Activity timeline merge
25. internal/ops/process.go          — Process status/cancel
26. internal/ops/context.go          — Static platform knowledge content
27. internal/ops/workflow.go         — Workflow catalog + per-workflow content
28. internal/ops/deploy.go           — Deploy logic (SSH + local)
```

After step 7: `go build ./internal/ops/...` and `go test ./internal/ops/... -short` must succeed.

### Step 8: `internal/tools`

```
29. internal/tools/convert.go        — PlatformError → MCP result conversion
30. internal/tools/discover.go       — zerops_discover handler
31. internal/tools/manage.go         — zerops_manage handler
32. internal/tools/env.go            — zerops_env handler
33. internal/tools/logs.go           — zerops_logs handler
34. internal/tools/deploy.go         — zerops_deploy handler
35. internal/tools/import.go         — zerops_import handler
36. internal/tools/validate.go       — zerops_validate handler
37. internal/tools/knowledge.go      — zerops_knowledge handler
38. internal/tools/process.go        — zerops_process handler
39. internal/tools/delete.go         — zerops_delete handler
40. internal/tools/subdomain.go      — zerops_subdomain handler
41. internal/tools/events.go         — zerops_events handler
42. internal/tools/context.go        — zerops_context handler
43. internal/tools/workflow.go       — zerops_workflow handler
```

After step 8: `go build ./internal/tools/...` and `go test ./internal/tools/... -short` must succeed.

### Step 9: `internal/server`

```
44. internal/server/instructions.go  — Ultra-minimal MCP init message
45. internal/server/server.go        — MCP server setup, tool + resource registration
```

After step 9: `go build ./internal/server/...` must succeed.

### Step 10: `internal/init`

```
46. internal/init/init.go            — Init orchestrator
47. internal/init/templates.go       — Template generation using content/
```

After step 10: `go build ./internal/init/...` must succeed.

### Step 11: `cmd/zcp/main.go` (update)

```
48. cmd/zcp/main.go                  — Full entrypoint (MCP server + init dispatch)
```

After step 11: `go build -o bin/zcp ./cmd/zcp` must produce working binary.

---

## Gotchas from Source Analysis

### Version Conflicts Between Source Repos

| Dependency | zaia | zaia-mcp | mcp60 | ZCP Should Use |
|-----------|------|----------|-------|----------------|
| `zerops-go` | v1.0.16 | (none) | v1.0.14 | **v1.0.16** (latest, from zaia) |
| `go-sdk` | (none) | v1.2.0 | v0.6.0 | **v1.2.0** (latest, from zaia-mcp) |
| `shopspring/decimal` | v1.3.1 (indirect) | (none) | v1.4.0 (indirect) | v1.3.1 (via zerops-go v1.0.16) |
| `google/jsonschema-go` | (none) | v0.3.0 (indirect) | v0.2.3 (indirect) | v0.3.0 (via go-sdk v1.2.0) |
| `golang.org/x/tools` | (go.sum only) | (go.sum only) | (go.sum only) | Handled by go mod tidy |

**No blocking conflicts.** Go's MVS (Minimum Version Selection) resolves these cleanly. The main risk is `shopspring/decimal` if a future zerops-go version changes its API — but v1.3.1 and v1.4.0 are both backward-compatible.

### json-iterator/go Unusual Version

```
github.com/json-iterator/go v0.0.0-20171115153421-f7279a603ede
```

This is a 2017 pseudo-version (pre-release commit), pulled transitively by bleve via `blevesearch/geo`. It works but looks unusual in go.mod. Not a problem — it is a well-known bleve transitive dep and has been stable for years.

### CGO_ENABLED=0 and Bleve

Bleve works fine with `CGO_ENABLED=0`. The `go-faiss` dependency is a no-op without CGO (FAISS requires C libraries). The Makefile already uses `CGO_ENABLED=0` for cross-compilation. No issues.

### Platform-Specific Issues

- **darwin arm64 cross-compile**: Go 1.25 has a Mach-O regression (see MEMORY.md). ZCP pins Go 1.24.0 — safe.
- **linux amd64**: Primary target platform (Zerops containers). All cross-builds use `CGO_ENABLED=0`.
- **No platform-specific code**: ZCP uses only stdlib and Go-native dependencies. No `//go:build` platform tags needed except for zcli path resolution in auth (darwin vs linux config paths).

### Build Tag Implications

| Tag | Used By | Effect |
|-----|---------|--------|
| `api` | Platform/auth/ops/tools contract tests | Enables real API tests (skip if no ZCP_API_KEY) |
| `e2e` | E2E lifecycle tests | Enables full lifecycle (creates/deletes real resources) |
| None | All mock/unit tests | Default — runs everywhere, no external deps |

Build tags are additive: `-tags api` runs everything including api-tagged tests. `-tags e2e` runs everything including both api and e2e tagged tests.

### zcli cli.data Path Differences

Auth fallback reads zcli config from platform-specific paths:
- **Linux**: `~/.config/zerops/cli.data`
- **macOS**: `~/Library/Application Support/zerops/cli.data`

This requires `runtime.GOOS` check or `os.UserConfigDir()` (which handles both). PRD section 3.1 documents both paths.

### go:embed Path Constraints

- `go:embed` patterns are relative to the source file's directory
- `internal/knowledge/embed/` must contain the 65+ markdown files at build time
- `internal/content/workflows/` and `internal/content/templates/` must contain embedded content
- These directories must exist with content before compilation — empty `go:embed` patterns cause build errors

### sync.Once Requirements (PRD mandated)

Two locations where source code has race conditions that ZCP must fix:
1. `ZeropsClient.clientID` — zaia uses bare string check (racy). ZCP must use `sync.Once`.
2. `knowledge.GetEmbeddedStore()` — zaia uses bare nil check (TOCTOU). ZCP must use `sync.Once`.

---

## Summary

| Metric | Value |
|--------|-------|
| Module | `github.com/zeropsio/zcp` |
| Go version | 1.24.0 |
| Direct dependencies | 4 (go-sdk, zerops-go, bleve, yaml.v3) + 1 test (testify) |
| Total transitive deps | ~30 modules |
| Internal packages | 9 (`platform`, `platform/apitest`, `auth`, `knowledge`, `content`, `ops`, `tools`, `server`, `init`) |
| Compilation batches | 5 (parallel within batch, sequential between) |
| Estimated binary size | 20-22 MB stripped (target: <30 MB) |
| Phase gates | 4 (Phase 1: platform+auth API, Phase 2: ops API, Phase 3: tools API, Phase 4: E2E) |
