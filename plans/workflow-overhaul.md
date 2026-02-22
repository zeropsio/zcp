# Workflow Overhaul Plan

## Problem Statement

The v2 workflow docs are **too abstract**. They describe WHAT to do but not HOW. The `main` branch (shell-based) worked because it gave EXACT commands, SPECIFIC requirements, and SELF-CONTAINED context to subagents. The v2 bootstrap workflow creates Go binaries instead of source code, Bun code that doesn't demonstrate real service connections, wrong env var wiring, and no iteration loop for fixing broken deploys. Any interruption is fatal because there's no recovery path.

### Feedback Summary
> "v dev gočka je jen binárka, v dev bunu je kód, ale místo toho aby to opravdu demonstrovalo napojení na všechny ty servisy, což pak vyžaduje správně nastavený envy atp., to pak vyžaduje iterace, správný puštění dev serveru, správně aktitovaný envy v zerops.yml, přesnější pořadí kroků.. a pak se to začne ztrácet, jakýkoliv přerušení je smrtelný"

---

## Root Cause Analysis

Comparing `main` branch (working) vs `v2` branch (broken), 7 critical gaps identified:

### Gap 1: No App Specification
**main**: Explicit app spec — `/`, `/health`, `/status` with JSON connectivity proof for each managed service. `/status` must actually ping DB, Redis, etc. and report success/failure.
**v2**: "Recommend adding /health and /status endpoints" — no specification of what they must do. LLM generates hello-world code.

### Gap 2: No Development Loop
**main**: 4-step dev loop — start server in background → curl endpoints → check logs → fix and restart. Explicit commands per runtime.
**v2**: Deploy and hope. If 8-point verification fails, "max 2 retries before asking user." No structured debug → fix → redeploy cycle.

### Gap 3: Subagent Context Starvation
**main**: Self-contained 300-line prompt with EVERYTHING: mount paths, SSH commands, discovered env vars with mapping examples, runtime recipes, 17-step task list, recovery procedures, progress tracking.
**v2**: Configure-Service Agent gets 30 lines — deploy + verify steps only. Doesn't create code, doesn't know the environment, doesn't have env var mappings, doesn't have recovery procedures.

### Gap 4: Env Var Wiring is Guesswork
**main**: `build_env_var_section()` reads actual discovered env vars from `service_discovery.json`, generates specific mapping instructions per service type (connectionString vs individual vars, password presence).
**v2**: "Pass the env vars from import.yml's envVariables/envSecrets" — no discovery, no mapping examples, LLM must guess.

### Gap 5: Code Generation vs Deployment Split
**main**: Single subagent does EVERYTHING for a service pair — creates code, installs deps, starts dev server, verifies, deploys dev, verifies, deploys stage, verifies. 17 steps, one context.
**v2**: Bootstrap "create-files" step has 2-sentence guidance. Then a separate Configure-Service Agent handles deploy+verify but NOT code creation. The gap between "create files" and "deploy" is where everything breaks.

### Gap 6: Go Produces Binary, Not Source
**main**: Explicit `deployFiles: [.]` for dev (entire directory), `deployFiles: [<binary>]` for prod. Dev code is source, stage deploys binary.
**v2**: No explicit instruction that dev must deploy SOURCE CODE. Go's default recipe produces a binary, and the LLM follows that for dev too.

### Gap 7: No Interruption Recovery
**main**: Heartbeat files, state tracking per step, `bootstrap.sh resume` command, mark-complete evidence files. Parent can detect where a subagent stopped.
**v2**: If context is lost, there's no way to know which step was last completed. No heartbeat, no progress files, no recovery command.

---

## Solution Architecture

### Principle: One Agent = One Service = Full Lifecycle

Instead of splitting "create files" and "configure+deploy" into separate steps with separate agents, make a single **Service Bootstrap Agent** responsible for the FULL lifecycle of each service pair (like `main` does). This eliminates the context gap.

### Implementation Plan (5 work units)

---

### Unit 1: App Specification Embedding

**What**: Add explicit app specification to bootstrap.md that defines what generated code MUST do.

**Changes to `internal/content/workflows/bootstrap.md`**:

Add new section after Phase 1 Step 5 (before Step 6 Validate):

```markdown
### Application Code Requirements

Every generated application MUST expose these endpoints:

| Endpoint | Response | Purpose |
|----------|----------|---------|
| `GET /` | `"Service: {hostname}"` | Landing page / smoke test |
| `GET /health` | `{"status":"ok"}` (200) | Liveness probe |
| `GET /status` | JSON connectivity report (200) | Proves managed service connectivity |

#### /status Endpoint Specification

The /status endpoint MUST **actually connect** to each managed service and report results:

```json
{
  "service": "{hostname}",
  "connections": {
    "db": {"status": "ok", "latency_ms": 5},
    "cache": {"status": "ok", "latency_ms": 1}
  }
}
```

**Required behavior per service type:**
- **PostgreSQL/MariaDB/MySQL**: Execute `SELECT 1` query
- **Valkey/KeyDB**: Execute `PING` command
- **MongoDB**: Run `db.runCommand({ping: 1})`
- **Object Storage**: List buckets or check endpoint reachability
- **Shared Storage**: Check mount path exists and is writable

If no managed services: return `{"service": "{hostname}", "status": "ok"}`.

**The /status endpoint is the PRIMARY verification gate.** HTTP 200 with all connections "ok" = app works. Anything else = iterate.
```

**Why**: The LLM needs an unambiguous spec. "Recommend adding /health and /status" is not actionable. This makes it a requirement with exact expected behavior per service type.

---

### Unit 2: Service Bootstrap Agent Prompt

**What**: Replace the separate "create-files step guidance" + "Configure-Service Agent Prompt" with a single comprehensive **Service Bootstrap Agent Prompt** that handles the full lifecycle.

**Changes to `internal/content/workflows/bootstrap.md`**:

Replace the current "Configure-Service Agent Prompt" section (lines ~247-288) with a new prompt modeled on `main` branch's `spawn-subagents.sh` output. Key sections:

1. **Environment context** — mount path, SSH equivalent (SSHFS mount = direct file access), working directory
2. **Service pair info** — dev hostname, stage hostname, zerops.yml setup names
3. **Discovered env vars** — ACTUAL discovered variables with mapping instructions (not generic). The parent agent must discover these BEFORE spawning subagents and embed them.
4. **Runtime guidance** — recipe patterns, build commands, deploy files (from `zerops_knowledge`)
5. **zerops.yml template** — dev setup (deployFiles: [.], source code) vs prod setup (deployFiles: [binary/dist])
6. **App specification** — from Unit 1 (endpoints, /status connectivity proof)
7. **Task list** — full lifecycle steps:
   - Write zerops.yml (dev + prod setups, NOT hostname names)
   - Write app code (HTTP server on configured port, all 3 endpoints)
   - Write .gitignore
   - Deploy to dev (`zerops_deploy` with includeGit=true)
   - Poll build completion
   - Verify dev (8-point protocol)
   - **Development loop if verification fails** — check logs, fix code, redeploy (max 3 iterations)
   - Deploy to stage
   - Verify stage
8. **Development loop** — explicit instructions for when verification fails:
   - Check build logs (`zerops_logs`, fallback to `zcli service log --showBuildLogs`)
   - Check runtime logs for errors
   - Fix code on mount path
   - Redeploy
   - Re-verify
9. **Recovery procedures** — common error → fix table
10. **Platform rules** — what NOT to generate (lock files, node_modules), deploy from mount path

**The parent agent's responsibility before spawning**:
1. Generate import.yml and import services (create-files equivalent)
2. Wait for services
3. Mount dev services
4. Discover env vars for ALL services: `zerops_discover includeEnvs=true`
5. Load knowledge: `zerops_knowledge runtime=X services=[Y,Z]`
6. Embed ALL of the above into the subagent prompt
7. Spawn one agent per service pair with the complete prompt

**Changes to `internal/workflow/bootstrap.go`**:

Update `stepGuidance` for steps 7-11 to reflect the new single-agent-per-service model:

```go
"create-files":      "MERGED INTO spawn-subagents. Skip this step — subagents handle code creation.",
"discover-services": "Discover all services and their env vars. For each runtime service: zerops_discover service=\"{hostname}\" includeEnvs=true. Collect the env var lists — these MUST be embedded into subagent prompts.",
"finalize":          "Load runtime knowledge for each service type. Prepare the complete context for subagent prompts: env vars, runtime recipes, zerops.yml templates.",
"spawn-subagents":   "Spawn one general-purpose agent per runtime service pair. Each agent gets the FULL Service Bootstrap Agent Prompt with embedded context (env vars, recipes, app spec). Agents handle the complete lifecycle: code generation, deploy, verify, iterate.",
"aggregate-results": "Collect results from all subagents. Run zerops_discover to independently verify all services are RUNNING. Check subdomain URLs. Present final results to user.",
```

---

### Unit 3: Dev vs Prod Deploy Differentiation

**What**: Make explicit that dev deploys SOURCE CODE and prod deploys BUILD OUTPUT.

**Changes to `internal/content/workflows/bootstrap.md`**:

In the zerops.yml guidance (Step 5), add:

```markdown
**CRITICAL: Dev vs Prod deployFiles**

| Environment | deployFiles | Why |
|-------------|-------------|-----|
| Dev (appdev) | `[.]` | Deploys entire source directory — enables iteration |
| Prod (appstage) | Runtime-specific build output | Only deploys what's needed to run |

**Per-runtime prod deployFiles:**
| Runtime | Dev deployFiles | Prod deployFiles | Prod start |
|---------|----------------|-------------------|------------|
| Go | `[.]` | `[app]` or `[./bin/app]` | `./app` |
| Bun | `[.]` | `[.]` | `bun run index.ts` |
| Node.js | `[.]` | `[., node_modules]` or `[dist, node_modules]` | `node index.js` |
| Python | `[.]` | `[.]` | `python app.py` |
| PHP | `[.]` | `[.]` | (apache/nginx serves it) |

**Go specifically**: Dev setup uses `go run .` as start command (compiles + runs source). Prod setup builds a binary in buildCommands and deploys it. This is why the zerops.yml must have TWO separate setup entries — dev and prod have different build/deploy pipelines.
```

---

### Unit 4: Env Var Discovery and Mapping Protocol

**What**: Add a mandatory env var discovery protocol to bootstrap.md that produces concrete mapping instructions for subagents.

**Changes to `internal/content/workflows/bootstrap.md`**:

Add new section between Step 5 and Step 6 (or update Phase 2):

```markdown
### Env Var Discovery Protocol (before spawning subagents)

After importing services and waiting for them to start, discover the ACTUAL env vars:

1. For each **managed service**, run:
   ```
   zerops_discover service="{hostname}" includeEnvs=true
   ```

2. Record which env vars exist for each service. Common patterns:
   - PostgreSQL: `{hostname}_connectionString`, `{hostname}_host`, `{hostname}_port`, `{hostname}_user`, `{hostname}_password`, `{hostname}_dbName`
   - Valkey/KeyDB: `{hostname}_connectionString`, `{hostname}_host`, `{hostname}_port` (NO password in private network)
   - MongoDB: `{hostname}_connectionString`, `{hostname}_host`, `{hostname}_port`, `{hostname}_user`, `{hostname}_password`

3. **Embed these into subagent prompts.** Don't just say "set env vars" — tell the subagent exactly which variables exist and how to map them:
   ```
   Available env vars for db (postgresql@16):
   - ${db_connectionString} — full connection string (RECOMMENDED)
   - ${db_host}, ${db_port}, ${db_user}, ${db_password}, ${db_dbName}

   Available env vars for cache (valkey@7.2):
   - ${cache_host}, ${cache_port}
   - NO password variable — private network, no auth
   ```

4. **In zerops.yml envVariables**, map discovered vars to app-expected names:
   ```yaml
   envVariables:
     DATABASE_URL: ${db_connectionString}
     REDIS_HOST: ${cache_host}
     REDIS_PORT: ${cache_port}
   ```
```

---

### Unit 5: Verification Iteration Loop

**What**: Replace the current "max 2 retries before asking user" with a structured iteration loop.

**Changes to `internal/content/workflows/bootstrap.md`**:

Replace the current build failure handling section with:

```markdown
### Verification Failure → Iteration Loop

When verification fails at any check point, enter the iteration loop:

**Iteration 1-3 (auto-fix):**
1. **Diagnose**: What check failed?
   - Build failed → `zerops_logs severity="error" since="10m"`, then `zcli service log {hostname} --showBuildLogs --limit 50`
   - No startup logs → App crashed. Check error logs
   - HTTP check failed → App started but endpoint broken. Check response body
   - /status failed → Service connectivity issue. Check env var resolution

2. **Fix**: Based on diagnosis:
   - Build error → Fix zerops.yml (buildCommands, deployFiles, start)
   - Runtime error → Fix app code on mount path
   - Env var issue → Fix zerops.yml envVariables, redeploy
   - Connection error → Verify managed service is RUNNING, check hostname/port

3. **Redeploy**: `zerops_deploy` to the SAME service (dev first, always)

4. **Re-verify**: Run the 8-point verification protocol again

**After 3 failed iterations**: Stop and report to user with:
- What was tried
- What the error is
- Suggested next steps

**NEVER deploy to stage until dev passes ALL verification checks.**
```

---

## Implementation Order

| Phase | Unit | Effort | Dependencies |
|-------|------|--------|-------------|
| 1 | Unit 1: App Specification | S | None |
| 2 | Unit 3: Dev vs Prod Deploy | S | None |
| 2 | Unit 4: Env Var Discovery Protocol | M | None |
| 3 | Unit 2: Service Bootstrap Agent Prompt | L | Units 1, 3, 4 |
| 4 | Unit 5: Verification Iteration Loop | M | Unit 2 |

Phases 1-2 can be done in parallel. Unit 2 is the largest and most critical — it's the core rewrite of how subagents are prompted.

---

## Files to Modify

| File | Changes |
|------|---------|
| `internal/content/workflows/bootstrap.md` | Units 1-5: Major rewrite of Phase 2, new app spec, new subagent prompt, env var protocol, iteration loop |
| `internal/content/workflows/deploy.md` | Minor: Add dev vs prod deploy differentiation, iteration loop |
| `internal/workflow/bootstrap.go` | Unit 2: Update stepGuidance for steps 7-11 |

## Files NOT Modified (no code changes needed)

| File | Reason |
|------|--------|
| `internal/workflow/engine.go` | Phase gate system works correctly as-is |
| `internal/workflow/gates.go` | Gate logic is sound |
| `internal/tools/workflow.go` | Tool handler correctly serves workflow docs |
| `internal/ops/deploy.go` | Deploy logic is fine — the problem is in instructions, not execution |

## Testing Strategy

This is primarily a **documentation/prompt engineering** change, not a code change. The bootstrap.go `stepGuidance` update needs unit test verification, but the main validation is:

1. **Manual E2E test**: Run a full bootstrap workflow with the updated docs and verify:
   - Generated code has `/`, `/health`, `/status` endpoints
   - `/status` actually connects to managed services
   - Dev deploys source code (not just binary for Go)
   - Env vars are correctly wired in zerops.yml
   - Verification catches real connectivity issues
   - Iteration loop works when something breaks

2. **Automated**: `go test ./internal/workflow/... -v` — verify stepGuidance map keys match bootstrapStepNames

---

## Success Criteria

1. Go service: deploys SOURCE CODE to dev, BINARY to stage. Both have working `/status` showing DB/cache connectivity.
2. Bun service: deploys source to both. `/status` shows actual managed service connections.
3. Subagent can complete full lifecycle without parent intervention.
4. If a deploy fails, the subagent iterates (fix → redeploy → verify) up to 3 times.
5. After interruption, `zerops_workflow action="start"` + `zerops_discover` allows recovery by detecting which services are already configured.
