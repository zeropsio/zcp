# Live Schema Integration

ZCP fetches the official Zerops JSON schemas at runtime and uses them to validate configuration and teach the LLM what values are actually valid on the platform right now.

## What schemas

Two public JSON schemas, no auth required:

| Schema | URL | What it defines |
|--------|-----|-----------------|
| **zerops.yml** | `api.app-prg1.zerops.io/.../zerops-yml-json-schema.json` | Build/run configuration — `build.base`, `run.base`, `deployFiles`, ports, health checks, cron, etc. |
| **import.yaml** | `api.app-prg1.zerops.io/.../import-project-yml-json-schema.json` | Service creation — service types, modes (HA/NON_HA), corePackage, autoscaling, storage policies, etc. |

Fetched once, cached 24 hours. Concurrent requests are coalesced (only one HTTP fetch at a time). Response bodies are capped at 5MB.

## What we extract

From **zerops.yml schema**:
- 79 valid `build.base` values (e.g., `php@8.4`, `nodejs@22`, `go@1`, `rust@stable`)
- 97 valid `run.base` values (e.g., `php-nginx@8.4`, `static`, `nodejs@22`, `docker@26.1`)
- Full field structure with types and descriptions

From **import.yaml schema**:
- 119 valid service types (e.g., `php-nginx@8.4`, `postgresql@16`, `object-storage`)
- Mode enum: `HA`, `NON_HA`
- Core package enum: `LIGHT`, `SERIOUS`
- Storage policy enum: `private`, `public-read`, `public-objects-read`, `public-write`, `public-read-write`
- Full field structure with types, required fields, and descriptions

All enums are precomputed into O(1) lookup sets at parse time.

## Where we use it

### 1. Bootstrap workflow — teaching the LLM valid values

When an LLM bootstraps a project ("I need a Node.js app with PostgreSQL"), it needs to know what service types and config values actually exist.

| Bootstrap step | Schema injected | Why |
|---------------|----------------|-----|
| **provision** | import.yaml schema | LLM is writing `import.yaml` to create services — needs to know valid types, modes, fields |
| **generate** | zerops.yml schema | LLM is writing `zerops.yml` build/run config — needs to know valid build bases, run bases, field structure |

The schema lands in the `schemaKnowledge` field of the workflow response. The LLM sees it alongside the step guidance.

Previously, the LLM only got static schema docs from an embedded markdown file. Those could go stale when Zerops added new service types. Now it gets the live truth.

### 2. Recipe workflow — teaching + validating

Recipes are reference implementations (laravel-hello-world, nestjs-showcase, etc.) with 6 environment tiers. The schema is used for both knowledge injection and hard validation.

**Knowledge injection** (same pattern as bootstrap):

| Recipe step | Schema injected | Why |
|------------|----------------|-----|
| **research** | Both schemas | LLM is planning the recipe — needs full picture of what's available |
| **provision** | import.yaml | Creating workspace services |
| **generate** | zerops.yml | Writing app config |
| **finalize** | Both schemas | Generating 6 environment-specific import.yaml files |
| **deploy** | zerops.yml | Troubleshooting config issues |

**Validation** — when the LLM submits a recipe plan after research, we validate:

| Field | Validated against | Error if invalid |
|-------|------------------|------------------|
| `runtimeType` | import.yaml service type enum | "runtimeType `foobar@1.0` not found in available service types (schema)" |
| `buildBases[]` | zerops.yml build.base enum (base name part) | "buildBase `foobar@1.0`: base name `foobar` not found in zerops.yml schema" |
| `targets[].type` | import.yaml service type enum | "target[0]: type `foobar@1.0` not found in import.yaml schema" |

Falls back to live API types if schemas are unavailable.

### 3. Import tool — validating user-provided YAML

When the LLM (or user) imports services via `zerops_import`, we validate the YAML against schema enums:

| Field | What we check | When |
|-------|--------------|------|
| `services[].type` | Must exist in schema service types (or live API types as fallback) | Always |
| `services[].mode` | Required for managed services (databases, caches, storage). Value must be `HA` or `NON_HA`. Runtimes don't need it. | Only if the type is a managed service category (STANDARD, SHARED_STORAGE, OBJECT_STORAGE) |
| `services[].objectStoragePolicy` | If present, must be one of the 5 valid policies | Only validated when the field is set (only relevant for `object-storage` type) |
| `project.corePackage` | If present, must be `LIGHT` or `SERIOUS` | Only validated when the field is set |

Without schema validation, an LLM could write `mode: MEGA` or `corePackage: HARDCORE` and the error would only surface at the API call — too late and with a cryptic error message.

### 4. Recipe eval — priming headless creation

When running headless recipe creation (`zcp eval create --framework laravel`), the prompt sent to Claude now includes the full formatted schema. This means the LLM knows all valid service types and build/run bases before it even starts the workflow — no guessing.

## What we removed (deduplication)

The knowledge engine previously injected static schema sections from an embedded `core.md` file at certain workflow steps. These static copies described the same fields as the live schema but could become stale. We removed:

| Workflow | Step | Removed static section | Replaced by |
|----------|------|----------------------|-------------|
| Bootstrap | provision | "import.yaml Schema" from core.md | Live `FormatImportYmlForLLM` in `schemaKnowledge` |
| Bootstrap | generate | "zerops.yaml Schema" from core.md | Live `FormatZeropsYmlForLLM` in `schemaKnowledge` |
| Recipe | research | "import.yaml Schema" from core.md | Live `FormatBothForLLM` in `schemaKnowledge` |
| Recipe | provision | "import.yaml Schema" from core.md | Live `FormatImportYmlForLLM` in `schemaKnowledge` |
| Recipe | generate | "zerops.yaml Schema" from core.md | Live `FormatZeropsYmlForLLM` in `schemaKnowledge` |

**Kept**: "Rules & Pitfalls" and "Schema Rules" sections from core.md — these contain deployment semantics (tilde syntax, deploy modes, cache architecture, public access rules) that are not part of the JSON schema.

## Code locations

| Package | File | What it does |
|---------|------|-------------|
| `internal/schema` | `schema.go` | Parse JSON schemas, extract enums, build lookup sets |
| `internal/schema` | `cache.go` | TTL cache with concurrent-fetch coalescing, 5MB response limit |
| `internal/schema` | `format.go` | Format schemas for LLM consumption (sorted fields, grouped enums) |
| `internal/tools` | `workflow_bootstrap.go` | Inject schema into bootstrap responses per step |
| `internal/tools` | `workflow_recipe.go` | Inject schema into recipe responses per step |
| `internal/workflow` | `recipe_validate.go` | Validate recipe plans against schema enums |
| `internal/knowledge` | `versions.go` | Validate import YAML fields against schema enums |
| `internal/eval` | `recipe_create.go` | Prepend schema context to headless recipe prompts |
