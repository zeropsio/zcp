# ZCP Version Selection System - Deep Analysis

## Executive Summary

The current version selection in ZCP is **fundamentally broken** because it relies on hardcoded defaults that never get updated from actual API data. This analysis identifies **three authoritative data sources** and recommends an optimal architecture.

---

## Data Sources Discovered

### 1. **docs.zerops.io/data.json** (AUTHORITATIVE - RECOMMENDED)

**URL:** `https://docs.zerops.io/data.json`

This is the **single source of truth** for all service types and versions. Structure:

```json
{
  "go": {
    "default": "1.22",
    "base": [["go@1.22", "go@1", "golang@1", "go@latest"]],
    "import": [["go@1.22", "go@1", "golang@1"]],
    "readable": ["1.22"]
  },
  "postgresql": {
    "import": [["postgresql@17"], ["postgresql@16"], ["postgresql@14"]],
    "readable": ["17 (17.5)", "16 (16.9)", "14 (14.18)"]
  },
  "bun": {
    "default": "1.1",
    "base": [["bun@1.2.2", "bun@1.2", "bun@latest"], ["bun@nightly"], ["bun@canary"]],
    "import": [["bun@1.2.2", "bun@1.2", "bun@latest"], ...],
    "readable": ["1.2", "1.1.34 (Ubuntu only)", "nightly", "canary"]
  }
}
```

**Available keys:** alpine, bun, clickhouse, deno, docker, dotnet, elasticsearch, elixir, gleam, go, java, kafka, keydb, mariadb, meilisearch, nats, nginx, nodejs, objectstorage, php, postgresql, python, qdrant, rabbitmq, rust, sharedstorage, static, typesense, ubuntu, valkey

**Pros:**
- Always up-to-date (docs team maintains it)
- Clean structure with `default`, `base`, `import`, `readable` fields
- First item in `import[0][0]` is always the recommended version
- Includes aliases (go@1, golang@1)

**Cons:**
- None significant

---

### 2. **Zerops API JSON Schema** (AUTHORITATIVE - COMPLETE)

**URL:** `https://api.app-prg1.zerops.io/api/rest/public/settings/import-project-yaml-json-schema.json`

This is the **validation schema** used by zcli for import YAML. Contains 98 valid type strings.

```json
{
  "definitions": {
    "ServiceStackType": {
      "enum": [
        "go@1", "go@1.22", "golang@1",
        "bun@1.1", "bun@1.2", "bun@latest",
        "postgresql@14", "postgresql@16", "postgresql@17",
        ...
      ]
    }
  }
}
```

**Pros:**
- Complete list of ALL valid type strings
- Used for actual validation
- Includes edge cases (bun@nightly, rust@nightly)

**Cons:**
- Flat list, no hierarchy or "recommended" indicator
- No readable names or descriptions

---

### 3. **Recipe API** (stage-vega.zerops.dev) (SUPPLEMENTARY)

**URL:** `https://stage-vega.zerops.dev/recipes/{recipe}.md?environment=ai-agent`

Provides **ready-to-use import.yml** for known recipes:
- `go-hello-world` - go@1 + postgresql@17
- `bun-hello-world` - bun@1
- `nestjs-hello-world` - nodejs@22

**Example output (go-hello-world):**
```yaml
services:
  - hostname: appstage
    type: go@1
    zeropsSetup: prod
    buildFromGit: https://github.com/zerops-recipe-apps/go-hello-world-app
    enableSubdomainAccess: true

  - hostname: appdev
    type: go@1
    zeropsSetup: dev
    buildFromGit: https://github.com/zerops-recipe-apps/go-hello-world-app
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 0.5

  - hostname: db
    type: postgresql@17
    mode: NON_HA
```

**Pros:**
- Provides complete, tested import.yml configurations
- Includes dev/stage patterns with correct settings
- Shows working combinations (go + postgresql)

**Cons:**
- Only 3 hello-world recipes exist (go, bun, nestjs)
- Not useful for managed services directly

---

## Current Implementation Problems

### Problem 1: Broken JSON Path in get_service_version()

```bash
# Current code looks for:
jq -r '.patterns[$t].version'

# But recipe_review.json has:
{
  "patterns_extracted": {
    "runtime_patterns": {
      "go": { "dev_runtime_base": "go@1" }
    }
  }
}
```

**Result:** Always falls back to hardcoded defaults.

### Problem 2: Hardcoded Defaults Are Stale

```bash
case "$service_type" in
    go) echo "go@1" ;;           # OK (aliases to go@1.22)
    bun) echo "bun@1" ;;         # WRONG! bun@1 doesn't exist, should be bun@1.2
    nodejs) echo "nodejs@22" ;;  # OK
    valkey) echo "valkey@8" ;;   # WRONG! Should be valkey@7.2
    ...
esac
```

### Problem 3: Recipe Search Only Handles First Runtime

```bash
# recipe-search.sh line 20
runtimes=$(echo "$plan" | jq -r '.runtimes // [.runtime] | .[]' | head -1)
```

For multi-runtime (go + bun), only searches for go patterns.

---

## Recommended Architecture

### Tier 1: Fetch from docs.zerops.io/data.json (ALWAYS)

```bash
fetch_type_data() {
    local cache_file="${ZCP_TMP_DIR:-/tmp}/zerops_types.json"
    local cache_age=3600  # 1 hour

    # Use cache if fresh
    if [ -f "$cache_file" ]; then
        local age=$(($(date +%s) - $(stat -f %m "$cache_file" 2>/dev/null || stat -c %Y "$cache_file")))
        if [ $age -lt $cache_age ]; then
            cat "$cache_file"
            return 0
        fi
    fi

    # Fetch and cache
    curl -sf "https://docs.zerops.io/data.json" > "$cache_file" 2>/dev/null
    cat "$cache_file"
}

get_recommended_version() {
    local service_type="$1"
    local data
    data=$(fetch_type_data)

    # Get first import type (recommended)
    echo "$data" | jq -r --arg t "$service_type" '.[$t].import[0][0] // empty'
}
```

### Tier 2: Check Recipe API for Hello-World (OPTIONAL)

For runtimes with hello-world recipes (go, bun, nodejs), fetch the recipe to get:
- Complete import.yml template
- Tested dev/stage configuration
- Working service combinations

```bash
has_hello_world_recipe() {
    local runtime="$1"
    case "$runtime" in
        go|bun|nodejs) return 0 ;;
        *) return 1 ;;
    esac
}

fetch_hello_world_recipe() {
    local runtime="$1"
    local recipe_name
    case "$runtime" in
        go) recipe_name="go-hello-world" ;;
        bun) recipe_name="bun-hello-world" ;;
        nodejs) recipe_name="nestjs-hello-world" ;;
    esac

    curl -sf "https://stage-vega.zerops.dev/recipes/${recipe_name}.md?environment=ai-agent"
}
```

### Tier 3: Validate Against JSON Schema (VERIFICATION)

Before generating import.yml, validate all types exist:

```bash
validate_type() {
    local type_string="$1"
    local schema
    schema=$(curl -sf "https://api.app-prg1.zerops.io/api/rest/public/settings/import-project-yaml-json-schema.json")

    echo "$schema" | jq -e --arg t "$type_string" '.definitions.ServiceStackType.enum | index($t)' >/dev/null
}
```

---

## Optimal Flow for "go + bun + postgres + valkey + nats"

```
1. FETCH TYPE DATA
   curl https://docs.zerops.io/data.json → cache to /tmp/zerops_types.json

2. RESOLVE VERSIONS
   go        → data.go.import[0][0]         → "go@1.22"
   bun       → data.bun.import[0][0]        → "bun@1.2.2"
   postgresql→ data.postgresql.import[0][0] → "postgresql@17"
   valkey    → data.valkey.import[0][0]     → "valkey@7.2"
   nats      → data.nats.import[0][0]       → "nats@2.10"

3. FETCH HELLO-WORLD RECIPES (optional, for structure)
   go  → stage-vega.zerops.dev/recipes/go-hello-world.md
   bun → stage-vega.zerops.dev/recipes/bun-hello-world.md

4. GENERATE import.yml
   services:
     - hostname: db
       type: postgresql@17
       mode: NON_HA
       priority: 10
     - hostname: cache
       type: valkey@7.2
       mode: NON_HA
       priority: 10
     - hostname: queue
       type: nats@2.10
       mode: NON_HA
       priority: 10
     - hostname: appdev
       type: go@1.22
       startWithoutCode: true
       verticalAutoscaling:
         minRam: 0.5
     - hostname: appstage
       type: go@1.22
       startWithoutCode: true
     - hostname: bundev
       type: bun@1.2.2
       startWithoutCode: true
       verticalAutoscaling:
         minRam: 0.5
     - hostname: bunstage
       type: bun@1.2.2
       startWithoutCode: true

5. VALIDATE (optional)
   Each type string exists in JSON schema enum
```

---

## Implementation Recommendation

### Option A: Minimal Fix (Quick)

Replace hardcoded defaults with data.json lookup:

```bash
# import-gen.sh
get_service_version() {
    local service_type="$1"
    local types_file="${ZCP_TMP_DIR:-/tmp}/zerops_types.json"

    # Fetch if not cached
    if [ ! -f "$types_file" ]; then
        curl -sf "https://docs.zerops.io/data.json" > "$types_file" 2>/dev/null
    fi

    # Get recommended version
    local version
    version=$(jq -r --arg t "$service_type" '.[$t].import[0][0] // empty' "$types_file" 2>/dev/null)

    if [ -n "$version" ]; then
        echo "$version"
        return
    fi

    # Fallback for unknown types
    echo "${service_type}@latest"
}
```

**Effort:** ~20 lines changed
**Risk:** Low

### Option B: Full Redesign (Recommended)

Create dedicated `type-resolver.sh` that:
1. Fetches and caches data.json
2. Supports version pinning via CLI flags
3. Validates against JSON schema
4. Logs version decisions for debugging

**Effort:** ~100 lines new code
**Risk:** Medium (new component)

### Option C: Add zcli Command (Best Long-term)

Request Zerops team to add `zcli types` command that exposes `/api/rest/public/service-stack-type/search` endpoint directly.

---

## Current Bugs to Fix Immediately

| Bug | File | Line | Fix |
|-----|------|------|-----|
| bun@1 doesn't exist | import-gen.sh | 40 | Change to `bun@1.2` |
| valkey@8 doesn't exist | import-gen.sh | 46 | Change to `valkey@7.2` |
| nats@2 should be nats@2.10 | import-gen.sh | 49 | Change to `nats@2.10` |
| recipe-search only handles first runtime | recipe-search.sh | 20 | Loop over all runtimes |

---

## Appendix: Complete Version Map (from data.json)

| Service | Recommended | All Valid |
|---------|-------------|-----------|
| go | go@1.22 | go@1, golang@1 |
| bun | bun@1.2.2 | bun@1.2, bun@latest, bun@nightly, bun@canary, bun@1.1.34 |
| nodejs | nodejs@22 | nodejs@20, nodejs@18 |
| python | python@3.12 | python@3.11 |
| rust | rust@1.86 | rust@1.80, rust@1.78, rust@nightly, rust@stable |
| dotnet | dotnet@9 | dotnet@8, dotnet@7, dotnet@6 |
| java | java@21 | java@17, java@latest |
| php-nginx | php-nginx@8.4+1.22 | php-nginx@8.3+1.22, php-nginx@8.1+1.22 |
| postgresql | postgresql@17 | postgresql@16, postgresql@14 |
| valkey | valkey@7.2 | - |
| nats | nats@2.12 | nats@2.10 |
| elasticsearch | elasticsearch@9.2 | elasticsearch@8.16 |
| keydb | keydb@6 | - |
| rabbitmq | rabbitmq@3.9 | - |
| mariadb | mariadb@10.6 | - |
