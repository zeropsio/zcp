# Live Schema Integration

ZCP fetches the official Zerops JSON schemas at runtime and uses them **for validation only**. The LLM never sees the raw schema — it gets curated knowledge from `core.md` and live service types from `AvailableStacks`.

## What schemas

Two public JSON schemas, no auth required:

| Schema | URL | What we extract |
|--------|-----|-----------------|
| **zerops.yml** | `api.app-prg1.zerops.io/.../zerops-yml-json-schema.json` | `build.base` enum (79 values), `run.base` enum (97 values) |
| **import.yaml** | `api.app-prg1.zerops.io/.../import-project-yml-json-schema.json` | Service type enum (119 values), mode enum, corePackage enum, objectStoragePolicy enum |

Fetched once, cached 24 hours. Concurrent requests are coalesced (only one HTTP fetch at a time). Response bodies are capped at 5MB. All enums precomputed into O(1) lookup sets at parse time.

## Where we use it — validation only

### 1. Recipe plan validation

When the LLM submits a `RecipePlan` after the research step, we validate against schema enums:

| Field | Validated against | What it catches |
|-------|------------------|-----------------|
| `runtimeType` | import.yaml service type enum | Invalid runtime like `foobar@1.0` |
| `buildBases[]` | zerops.yml `build.base` enum | Invalid build base like `php-nginx@8.4` (that's a run base, not a build base) |
| `targets[].type` | import.yaml service type enum | Invalid service type in targets |

Falls back to live API types (`liveTypes`) if schemas are unavailable.

**Why this matters:** `build.base` and `run.base` enums are different from service types. The live API's `ServiceStackType` list doesn't tell you which values are valid for `build.base` in zerops.yml. Only the zerops.yml JSON schema has this. This is the one thing the schema provides that nothing else does.

### 2. Import pre-flight validation

When the LLM imports services via `zerops_import`, we validate enum fields:

| Field | What we check | When |
|-------|--------------|------|
| `services[].mode` | Must be `HA` or `NON_HA` | Only validated when present (required for managed services only) |
| `services[].objectStoragePolicy` | Must be one of 5 valid policies | Only validated when present (only relevant for `object-storage`) |

Service type validation uses `liveTypes` from the API (which already existed before schema work).

## What we explicitly do NOT use schema for

### LLM knowledge injection

The LLM gets its knowledge from two existing mechanisms:

1. **`core.md`** — curated field descriptions, preprocessor function docs, dryRun warnings, field constraints, rules & pitfalls (~60 rules), deploy semantics, multi-service examples. Static but complete with context the JSON schema doesn't have.

2. **`AvailableStacks`** — live service type list from the API (`FormatStackList` / `FormatServiceStacks`), injected at discover/generate/research steps. Shows all valid types with versions grouped compactly (e.g., `nodejs@{18,20,22}`). The recipe variant adds `[B]` markers for build-capable runtimes.

These two cover everything the LLM needs. Adding the formatted JSON schema on top would duplicate both without adding new information.

## Code locations

| Package | File | What it does |
|---------|------|-------------|
| `internal/schema` | `schema.go` | Parse JSON schemas, extract enums, build O(1) lookup sets |
| `internal/schema` | `cache.go` | TTL cache with concurrent-fetch coalescing, 5MB response limit |
| `internal/schema` | `format.go` | LLM formatting functions (kept for tests, not used in production responses) |
| `internal/workflow` | `recipe_validate.go` | Validate recipe plans against build/run base enums |
| `internal/knowledge` | `versions.go` | Validate import YAML mode and policy enums |
