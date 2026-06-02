# Live Schema Integration

ZCP fetches the official Zerops JSON schemas at runtime and uses them **for validation only**. The LLM never sees the raw schema — it gets curated knowledge from `core.md` and live service types from `AvailableStacks`.

## What schemas

Two public JSON schemas, no auth required. The host is **derived from `ZCP_API_HOST`** at runtime (`schema.URLs`), so validation hits the instance the user deploys to; dev tooling (`schema sync`/`check`) + the empty-host default pin `schema.CanonicalAPIHost` (`api.app-prg1.zerops.io`):

| Schema | Path | What we extract |
|--------|-----|-----------------|
| **zerops.yaml** | `…/settings/zerops-yml-json-schema.json` | `build.base` + `run.base` enums |
| **import.yaml** | `…/settings/import-project-yml-json-schema.json` | service type enum, corePackage enum, objectStoragePolicy enum |

Embedded-seeded so `Get` is never nil, then refreshed live on a 15-min TTL. Fetches are coalesced (one HTTP fetch at a time), poison-guarded (an `HTTP 200 {error:502}` empty-enum body is rejected, last-good kept), bodies capped at 5MB. All enums precomputed into O(1) lookup sets at parse time.

The schema is the **single client-side source of truth** for type/base existence + latest version + the briefing stack list (the `*schema.Schemas` catalog: `HasServiceType`/`HasRunBase`/`HasBuildBase`/`ManagedBaseNames`). It replaced the deleted `StackTypeCache` + stack-types API. Managed/runtime/utility **classification** lives in `internal/topology`; all matching is composite-aware (`topology.CanonicalBareForm`) so a bare authored type matches a composite-only live schema.

## Where we use it — validation only

### 1. Recipe plan validation

When the LLM submits a `RecipePlan` after the research step, we validate against schema enums:

| Field | Validated against | What it catches |
|-------|------------------|-----------------|
| `runtimeType` | import.yaml service type enum | Invalid runtime like `foobar@1.0` |
| `buildBases[]` | zerops.yaml `build.base` enum | Invalid build base like `php-nginx@8.4` (that's a run base, not a build base) |
| `targets[].type` | import.yaml service type enum | Invalid service type in targets |

Schema-only (the `schemas==nil` sim/offline path skips existence checks; the platform re-validates at import regardless). Membership is equivalence-aware, so a bare authored base (`php@8.4`) matches a composite-only live enum (`alpine/php@8.4`).

**Why this matters:** `build.base` and `run.base` enums are different from service types — only the zerops.yaml JSON schema carries which values are valid for `build.base`. The (now-deleted) stack-types API never carried build bases at all (its BUILD category was only `*/build_runtime`), which is precisely why the schema is the irreplaceable single source.

### 2. Import pre-flight validation

When the LLM imports services via `zerops_import`, we validate enum fields:

| Field | What we check | When |
|-------|--------------|------|
| `services[].mode` | Must be `HA` or `NON_HA` | Only validated when present (required for managed services only) |
| `services[].objectStoragePolicy` | Must be one of 5 valid policies | Only validated when present (only relevant for `object-storage`) |

Service type validation uses the schema-derived catalog (`schemas.HasServiceType`, equivalence-aware) — the stack-types API it formerly fell back to is deleted.

## What we explicitly do NOT use schema for

### LLM knowledge injection

The LLM gets its knowledge from two existing mechanisms:

1. **`core.md`** — curated field descriptions, preprocessor function docs, dryRun warnings, field constraints, rules & pitfalls (~60 rules), deploy semantics, multi-service examples. Static but complete with context the JSON schema doesn't have.

2. **`AvailableStacks`** — the schema-derived service type list (`FormatStackList` / `FormatServiceStacks` over `*schema.Schemas`), injected at discover/generate/research steps. Shows valid types with versions grouped compactly by canonical bare base (e.g., `nodejs@{18,20,22}`), `[B]` markers for runtimes whose base is also a `build.base`, and a `Build-only:` line for build bases with no runtime (e.g. `php`).

These two cover everything the LLM needs. Adding the formatted JSON schema on top would duplicate both without adding new information.

## Code locations

| Package | File | What it does |
|---------|------|-------------|
| `internal/schema` | `schema.go` | Parse JSON schemas, extract enums, build O(1) lookup sets |
| `internal/schema` | `cache.go` | 15-min TTL cache, embedded-seeded (never nil) + poison guard, concurrent-fetch coalescing, 5MB response limit |
| `internal/schema` | `validate_structure.go` | Structure-only validators for export/launch (volatile type/base enums stripped) |
| `internal/schema` | `sync.go` | `schema sync`/`check`: refresh embedded schemas + derive catalog from one fetch; drift detection |
| `internal/workflow` | `recipe_validate.go` | Validate recipe plans against build/run base enums |
| `internal/knowledge` | `versions.go` | Validate import YAML mode and policy enums |
