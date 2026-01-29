# ZCP Bootstrap Flow - Deep Analysis

## What You're Verifying

The bootstrap flow must ensure that when a user requests:
```
"add go + bun + postgres + valkey + nats"
```

The system:
1. **Discovers** relevant patterns from recipes/docs
2. **Understands the topology** - what services exist, how they connect
3. **Passes complete context** to subagents so they can:
   - Write code that USES the managed services
   - Verify connectivity to each service
   - Follow best practices without copy-pasting

---

## Current Flow Analysis

### What's Working ✅

```
plan.sh
  ├── Parses: runtimes=["go","bun"], managed_services=["postgresql","valkey","nats"]
  ├── Creates: dev_hostnames=["appdev","bundev"], stage_hostnames=["appstage","bunstage"]
  └── Outputs: Complete plan JSON

recipe-search.sh (for first runtime only)
  ├── Fetches: stage-vega.zerops.dev/recipes/go-hello-world.md
  ├── Saves: /tmp/fetched_recipe.md, /tmp/recipe_review.json
  └── Extracts: runtime version, some patterns

finalize.sh
  ├── Builds handoffs for EACH dev/stage pair
  ├── Maps managed services to env prefixes (postgresql→DB, valkey→REDIS, nats→AMQP)
  └── Includes: hostname, type, env_prefix for each managed service

spawn-subagents.sh
  ├── Generates: Comprehensive subagent prompts
  ├── Includes: Env var mappings (${db_hostname}, ${db_port}, etc.)
  └── Provides: zerops.yml template with env vars
```

### What's Missing ❌

#### 1. **Recipe search only handles FIRST runtime**
```bash
# recipe-search.sh line 20
runtimes=$(echo "$plan" | jq -r '.runtimes // [.runtime] | .[]' | head -1)
```

For `go + bun`, only fetches go recipe. Bun subagent gets no patterns.

#### 2. **No managed service reference docs**

The current flow finds `go-hello-world` recipe, but that only shows runtime patterns.

It does NOT provide reference material about the managed services themselves:
- What env vars does PostgreSQL expose?
- What's the connection string format for Valkey?
- What ports does NATS use?

**What's needed:** Fetch managed service overview docs as reference:
- `https://docs.zerops.io/postgresql/overview.md` → env vars, connection info
- `https://docs.zerops.io/valkey/overview.md` → env vars, connection info
- `https://docs.zerops.io/nats/overview.md` → env vars, connection info

The agent knows how to connect Go to PostgreSQL - it just needs to know what Zerops provides.

#### 3. **No connectivity verification context**

Subagents are told to "verify connectivity" but don't know:
- That `psql` and `redis-cli` are available on ZCP (not in containers)
- That NATS verification requires application-level connection test
- Which tools exist where

This context should come from the managed service reference docs, not be hardcoded.

---

## Ideal Flow

### Phase 1: Type Resolution (docs.zerops.io/data.json)

```bash
# Fetch authoritative versions
curl -s https://docs.zerops.io/data.json > /tmp/zerops_types.json

# Resolve each service
go         → go@1.22
bun        → bun@1.2.2
postgresql → postgresql@17
valkey     → valkey@7.2
nats       → nats@2.12
```

### Phase 2: Recipe Discovery (Per Runtime)

```bash
for runtime in go bun; do
  # Find hello-world recipe
  recipe=$(find_recipe_for_runtime "$runtime")  # go-hello-world, bun-hello-world

  if [ -n "$recipe" ]; then
    # Fetch as INSPIRATION (not copy-paste template)
    curl -sf "https://stage-vega.zerops.dev/recipes/${recipe}.md?environment=ai-agent" \
      > "/tmp/recipe_${runtime}.md"
  fi
done
```

**Output:** `/tmp/recipe_go.md`, `/tmp/recipe_bun.md`

These are reference examples, not templates. Agent uses them to understand patterns.

### Phase 3: Managed Service Reference Docs

```bash
for svc in postgresql valkey nats; do
  # Fetch managed service overview as reference
  curl -sf "https://docs.zerops.io/${svc}/overview.md" \
    > "/tmp/service_${svc}.md" 2>/dev/null || true
done
```

**Output:**
- `/tmp/service_postgresql.md` - PostgreSQL env vars, connection patterns
- `/tmp/service_valkey.md` - Valkey env vars, connection patterns
- `/tmp/service_nats.md` - NATS env vars, connection patterns

The agent reads these to understand what Zerops provides, then implements connections its own way.

### Phase 4: Build Topology Context

```json
{
  "topology": {
    "managed_services": [
      {
        "name": "db",
        "type": "postgresql@17",
        "env_vars": ["db_hostname", "db_port", "db_user", "db_password", "db_dbName", "db_connectionString"],
        "reference_doc": "/tmp/service_postgresql.md"
      },
      {
        "name": "cache",
        "type": "valkey@7.2",
        "env_vars": ["cache_hostname", "cache_port", "cache_password", "cache_connectionString"],
        "reference_doc": "/tmp/service_valkey.md"
      },
      {
        "name": "queue",
        "type": "nats@2.12",
        "env_vars": ["queue_hostname", "queue_port", "queue_user", "queue_password"],
        "reference_doc": "/tmp/service_nats.md"
      }
    ],
    "runtimes": [
      {
        "name": "go",
        "dev_hostname": "appdev",
        "stage_hostname": "appstage",
        "version": "go@1.22",
        "recipe_file": "/tmp/recipe_go.md"
      },
      {
        "name": "bun",
        "dev_hostname": "bundev",
        "stage_hostname": "bunstage",
        "version": "bun@1.2.2",
        "recipe_file": "/tmp/recipe_bun.md"
      }
    ]
  }
}
```

The agent receives:
- **What exists** (topology)
- **What's available** (env vars)
- **Where to look** (reference docs)

The agent decides:
- Which libraries to use
- How to structure the code
- How to verify connectivity

### Phase 5: Subagent Prompt Enhancement

Current prompt has:
```
## Managed Services
${env_var_mappings}  ← Just variable names
```

Enhanced prompt should have:
```
## Managed Services Topology

You have access to these managed services. Reference docs are provided for Zerops-specific patterns.

### db (postgresql@17)
- Hostname: db
- Available env vars: db_hostname, db_port, db_user, db_password, db_dbName, db_connectionString
- Reference: /tmp/service_postgresql.md

### cache (valkey@7.2)
- Hostname: cache
- Available env vars: cache_hostname, cache_port, cache_password, cache_connectionString
- Reference: /tmp/service_valkey.md

### queue (nats@2.12)
- Hostname: queue
- Available env vars: queue_hostname, queue_port, queue_user, queue_password
- Reference: /tmp/service_nats.md

Your app should:
1. Connect to each service using appropriate libraries (your choice)
2. Expose /health endpoint that verifies connectivity to all services
3. Handle connection failures gracefully
```

The agent knows how to write Go/Bun code that connects to PostgreSQL. We just tell it what's there.

---

## Data Sources Summary

| Source | What It Provides | When to Use |
|--------|------------------|-------------|
| `docs.zerops.io/data.json` | Authoritative types + versions | **Always** - version resolution |
| `stage-vega.zerops.dev/recipes/{name}.md` | Example import.yml + zerops.yml (inspiration) | When hello-world recipe exists |
| `docs.zerops.io/{runtime}/how-to/build-pipeline.md` | Build patterns for runtime (reference) | Fallback when no recipe |
| `docs.zerops.io/{service}/overview.md` | Managed service env vars + patterns (reference) | **Always** for managed services |

**No SDK recommendations needed** - the agent is smart enough to figure out which libraries to use for Go+PostgreSQL, Bun+Valkey, etc.

---

## Implementation Priority

### P0: Fix Multi-Runtime Recipe Search
```bash
# Current (broken)
runtimes=$(... | head -1)

# Fixed
for runtime in $(echo "$plan" | jq -r '.runtimes[]'); do
  fetch_recipe_for_runtime "$runtime"
  # Save to /tmp/recipe_${runtime}.md
done
```

### P1: Add Managed Service Reference Docs
```bash
# For each managed service, fetch overview doc as reference
for svc in $(echo "$plan" | jq -r '.managed_services[]'); do
  curl -sf "https://docs.zerops.io/${svc}/overview.md" > "/tmp/service_${svc}.md"
done
```

### P2: Enhanced Handoff with Topology
Include in handoff:
```json
{
  "managed_services": [
    {
      "name": "db",
      "type": "postgresql@17",
      "env_vars": ["db_hostname", "db_port", "db_user", "db_password", "db_dbName", "db_connectionString"],
      "reference_doc": "/tmp/service_postgresql.md"
    }
  ]
}
```

### P3: Clear Mandate in Subagent Prompt
Tell the agent WHAT to achieve, not HOW:
- "Your app must connect to db (postgresql) and expose /health that verifies connectivity"
- Provide env var list and reference doc path
- Agent decides libraries, patterns, implementation

---

## Key Principle

> **Recipes and docs are INSPIRATION, not templates.**
>
> The bootstrap should:
> 1. Understand what the user is building (go API + bun worker + postgres + valkey + nats)
> 2. Fetch relevant patterns as REFERENCE
> 3. Build a CUSTOM topology context
> 4. Generate CUSTOM code that fits the actual use case
>
> Never blindly copy. Always construct.
