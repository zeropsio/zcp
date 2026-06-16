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

### 1. Bootstrap target validation

When the LLM completes a bootstrap/recipe plan, the submitted target service **types** are validated against the schema-derived catalog (`internal/workflow/validate.go`: `ValidateBootstrapTargets` → `catalogTypeErrors`):

| Field | Validated against | What it catches |
|-------|------------------|-----------------|
| `RuntimeTarget.Type` | import.yaml service type enum (`HasServiceType`) | Invalid runtime like `foobar@1.0` |
| `RuntimeTarget.StageType` | import.yaml service type enum (`HasServiceType`) | Invalid stage runtime type |
| managed `dep.Type` | import.yaml service type enum (`HasServiceType`) | Invalid managed dependency type |

Schema-only (the `schemas==nil` sim/offline path skips existence checks; the platform re-validates at import regardless). Membership is equivalence-aware, so a bare authored type (`php@8.4`) matches a composite-only live enum (`alpine/php@8.4`).

`build.base` / `run.base` enum validation is **separate** — it does not happen here. It runs against the zerops.yaml schema in `internal/schema/validate_bases.go` (`CheckZeropsBasesLive`, using `HasBuildBase`/`HasRunBase`) and is invoked only from the authoring recipe gate (`internal/authoring/recipe/validators_zerops_yaml_schema.go`).

**Why this matters:** `build.base` and `run.base` enums are different from service types — only the zerops.yaml JSON schema carries which values are valid for `build.base`. The (now-deleted) stack-types API never carried build bases at all (its BUILD category was only `*/build_runtime`), which is precisely why the schema is the irreplaceable single source.

### 2. Import (`zerops_import`) — server-side only

`zerops_import` takes **no client-side validator** (`internal/tools/import.go`: "takes no client-side type catalog"). The Zerops API is the sole validator for everything the import YAML declares — fields, types, modes, hostnames — and returns structured `apiMeta` on the error response when anything is wrong. There is no client-side type/mode/policy enum check on this path.

(`mode:`/HA is in any case retired as an import field — HA is now a type variant `postgresql:ha@16`, not a `services[].mode` value — so nothing client-side reads it.)

### 3. Export / launch structure validation

The export and launch bundle builders validate the YAML they emit against a **structure-only** schema (`internal/schema/validate_structure.go`: `ValidateImportYAMLStructure` / `ValidateZeropsYAMLStructure`), called from `internal/ops/bundle/export.go` + `launch.go` (and the recipe push path in `internal/sync/push_recipes.go`). The volatile membership enums (`services[].type`, `zerops[].build.base`, `zerops[].run.base`) are stripped, but the **stable** enums are preserved — so a bad `objectStoragePolicy` / `corePackage` / `location` / `cpuMode` value, a typo'd field (`additionalProperties:false`), or a missing required field still rejects client-side here. See `validate_structure_test.go` ("bad objectStoragePolicy still rejected"). This is NOT the `zerops_import` path.

## What we explicitly do NOT use schema for

### LLM knowledge injection

The LLM gets its knowledge from two existing mechanisms:

1. **`core.md`** — curated field descriptions, preprocessor function docs, dryRun warnings, field constraints, rules & pitfalls (~60 rules), deploy semantics, multi-service examples. Static but complete with context the JSON schema doesn't have.

2. **`AvailableStacks`** — the schema-derived service type list (`FormatStackList` / `FormatServiceStacks` over `*schema.Schemas`), injected into the bootstrap discover response and the knowledge briefing. Shows valid types with versions grouped compactly by canonical bare base (e.g., `nodejs@{18,20,22}`), `[B]` markers for runtimes whose base is also a `build.base`, and a `Build-only:` line for build bases with no runtime (e.g. `php`).

These two cover everything the LLM needs. Adding the formatted JSON schema on top would duplicate both without adding new information.

## Code locations

| Package | File | What it does |
|---------|------|-------------|
| `internal/schema` | `schema.go` | Parse JSON schemas, extract enums, build O(1) lookup sets |
| `internal/schema` | `cache.go` | 15-min TTL cache, embedded-seeded (never nil) + poison guard, concurrent-fetch coalescing, 5MB response limit |
| `internal/schema` | `validate_structure.go` | Structure-only validators for export/launch (`ValidateImportYAMLStructure`/`ValidateZeropsYAMLStructure`; volatile type/base enums stripped, stable enums kept) |
| `internal/schema` | `validate_bases.go` | `CheckZeropsBasesLive` — validate `zerops[].build.base`/`run.base` against the live base enums (`HasBuildBase`/`HasRunBase`) |
| `internal/schema` | `sync.go` | `schema sync`/`check`: refresh embedded schemas + derive catalog from one fetch; drift detection |
| `internal/workflow` | `validate.go` | `ValidateBootstrapTargets`/`catalogTypeErrors` — validate bootstrap target service types via `HasServiceType` |
| `internal/authoring/recipe` | `validators_zerops_yaml_schema.go` | `gateZeropsYamlSchema` — authoring recipe gate; runs the structure + base-enum validators over the session's zerops.yaml |
