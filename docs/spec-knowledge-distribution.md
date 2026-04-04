# ZCP Knowledge Distribution Specification

> **Status**: Authoritative — all knowledge delivery, guidance assembly, and init instructions MUST conform to this document.
> **Scope**: All workflows, both container and local environments, all modes and strategies.
> **Date**: 2026-04-04
> **Companion docs**: `spec-bootstrap-deploy.md` (workflow step procedures), `spec-guidance-philosophy.md` (delivery principles)

---

## 1. The Model

### 1.1 Problem Statement

ZCP guides an AI agent through Zerops PaaS operations. The agent knows how to program but doesn't know Zerops. It could figure things out eventually — but would hit dead ends, silent failures, and suboptimal configurations that manifest only later.

The knowledge distribution system exists to bridge this gap efficiently: deliver the right information, at the right time, in the right way.

### 1.2 Two Knowledge Types

Every piece of information the system delivers falls into one of two categories:

**PLATFORM FACT** — objectively true about Zerops, no alternative exists.
- Deploy creates a new container. All local files are lost. Only `deployFiles` content survives.
- `${hostname_varName}` typos become silent literal strings. No error. No deploy failure.
- Build container ≠ run container. Packages installed during build are NOT available at runtime.
- Tone: "this IS how it works."

**CURATED PATH** — best practice from the Zerops team's experience. Alternatives exist but lead to problems the agent cannot predict.
- "Deploy dev first, then stage" — could deploy stage first, but untested config hits production path.
- "Use `zsc noop --silent` as dev start" — could use real start, but agent loses manual iteration control.
- "Write stage entry AFTER dev verification" — could write both upfront, but deploys untested config.
- "Default to standard mode" — could use simple, but loses dev/stage iteration cycle.
- Tone: "we RECOMMEND this because X."

### 1.3 Information Asymmetry Model

At any point in a workflow, the agent's knowledge falls into three categories:

| Category | Description | Example |
|----------|-------------|---------|
| **CANNOT KNOW** | Zerops-specific or state-specific. Agent has no way to derive this from training or context. | zerops.yml schema, env var wiring syntax, deploy pipeline behavior |
| **CAN DERIVE** | Agent can obtain by reading code, calling tools, or reasoning from prior steps. | Current zerops.yml content, service status via zerops_discover |
| **KNOWS** | From training data or prior workflow steps already completed. | How to write a Node.js server, what env vars were discovered in prior step |

### 1.4 The Delivery Rule

```
Agent CANNOT KNOW + NEEDS NOW       → INJECT into response
Agent CANNOT KNOW + MIGHT NEED      → POINT to where it is
Agent KNOWS or CAN DERIVE           → NOTHING (save context window)
```

Every rule in this specification is an application of this principle to a specific state.

### 1.5 How Asymmetry Shifts Across Workflows

| Workflow | Start | Middle | End |
|----------|-------|--------|-----|
| **Bootstrap** | HIGH — first contact with Zerops, agent knows nothing platform-specific | MEDIUM — knows plan, services, env vars from prior steps | LOW — has written config, deployed, verified |
| **Deploy** | LOW — agent bootstrapped previously, has full context | LOW — operational, knows what to ask for | LOW |
| **Recipe** | VARIES — research=LOW (agent's domain), generate=HIGH (Zerops config), deploy=LOW | | |

Each completed checkpoint ADDS to what the agent KNOWS, reducing what it CANNOT KNOW.

### 1.6 Context Window Cost Model

| Delivery | Cost | Justified when |
|----------|------|----------------|
| INJECT | Occupies agent context window (~50-200 lines) | CANNOT KNOW + NEEDS NOW — agent can't proceed without it |
| POINT | Extra tool call, latency, agent decides whether to fetch | CANNOT KNOW + MIGHT NEED — optional, agent knows what to ask |
| NOTHING | Risk of agent missing critical info | KNOWS or CAN DERIVE — information is redundant |

---

## 2. Dimensions

Seven dimensions determine the system's knowledge delivery behavior at any point. Each rule in §3 references these by ID.

### D1: Workflow

| Value | Description |
|-------|-------------|
| `bootstrap` | 5-step creative workflow: discover → provision → generate → deploy → close |
| `deploy` | 3-step operational workflow: prepare → execute → verify |
| `recipe` | 6-step hybrid workflow: research → provision → generate → deploy → finalize → close |
| `cicd` | Immediate (stateless) — returns CI/CD setup guidance, no session |
| `export` | Immediate (stateless) — returns export guidance, no session |

Set by: `zerops_workflow action="start" workflow="..."`. Immutable for session lifetime.

### D2: Step

Steps are workflow-specific. Each step has allowed tools and verification criteria.

**Bootstrap steps** (strict order):

| Index | Step | Mandatory | Tools | Verification |
|-------|------|:---------:|-------|-------------|
| 0 | `discover` | always | zerops_discover, zerops_knowledge, zerops_workflow | Plan submitted with valid targets |
| 1 | `provision` | always | zerops_import, zerops_process, zerops_discover, zerops_mount | All services exist, types match, env vars discovered |
| 2 | `generate` | if runtime targets | zerops_knowledge | zerops.yml valid, env refs match, ports defined |
| 3 | `deploy` | if runtime targets | zerops_deploy, zerops_discover, zerops_subdomain, zerops_logs, zerops_mount, zerops_verify, zerops_manage | All runtimes deployed, accessible, healthy |
| 4 | `close` | if runtime targets | zerops_workflow | Metas written, transition presented |

**Deploy steps** (strict order):

| Index | Step | Tools | Checker |
|-------|------|-------|---------|
| 0 | `prepare` | zerops_discover, zerops_knowledge | checkDeployPrepare — validates zerops.yml, setup entries, env refs |
| 1 | `execute` | zerops_deploy, zerops_subdomain, zerops_logs, zerops_verify, zerops_manage | checkDeployResult — verifies services deployed (informational, non-blocking) |
| 2 | `verify` | zerops_verify, zerops_discover | None (informational step) |

**Recipe steps** (strict order):

| Index | Step | Mandatory | Tools | Checker |
|-------|------|:---------:|-------|---------|
| 0 | `research` | always | zerops_knowledge, zerops_discover, zerops_workflow | None (plan submitted separately) |
| 1 | `provision` | always | zerops_import, zerops_process, zerops_discover, zerops_mount | None |
| 2 | `generate` | always | zerops_knowledge | checkRecipeGenerate — zerops.yml + fragment quality + comment ratio ≥ 0.3 |
| 3 | `deploy` | always | zerops_deploy, zerops_discover, zerops_subdomain, zerops_logs, zerops_mount, zerops_verify, zerops_manage | None |
| 4 | `finalize` | always | zerops_workflow | checkRecipeFinalize — 6 import.yaml + 7 READMEs valid |
| 5 | `close` | skippable | zerops_workflow | None |

### D3: Mode

| Value | Description | Topology |
|-------|-------------|----------|
| `standard` | Dev + stage pairing. Dev for iteration, stage for validation. | `{name}dev` + `{name}stage` + managed |
| `dev` | Dev only. Prototyping, no stage. | `{name}dev` + managed |
| `simple` | Single service, real start, auto-starts. | `{name}` + managed |
| `managed-only` | No runtime services. Only databases/caches/storage. | managed only |
| `mixed` | Multiple non-standard modes in same plan. | varies |

Set by: plan submission at bootstrap discover step. Immutable for session.

**PlanMode() resolution logic**: if ANY target uses `standard` → return `standard`. If all targets use single mode → return that mode. Multiple non-standard modes → return `mixed`. No targets → return `""`.

### D4: Environment

| Value | Description | Signal |
|-------|-------------|--------|
| `container` | ZCP running inside Zerops. Code via SSHFS mounts. SSH to services. | `serviceId` env var present |
| `local` | ZCP running on developer machine. Code in working directory. Deploy via `zcli push`. | `serviceId` env var absent |

Set by: `runtime.Detect()` at startup. Immutable for process lifetime.

### D5: Strategy

| Value | Description | Interaction model |
|-------|-------------|-------------------|
| `push-dev` | SSH self-deploy from dev container. Agent triggers each deploy. | Deploy workflow (3-step session) |
| `push-git` | Push to git remote, optional CI/CD for auto-deploy. | Deploy + optional cicd/export workflows |
| `manual` | User controls deployment directly. Agent calls zerops_deploy. | Direct tool calls (no session) |
| `unset` | Strategy not yet chosen. | Strategy selection prompt (blocks deploy workflow) |

Set by: explicit `zerops_workflow action="strategy"` after bootstrap. NEVER auto-assigned.

### D6: Runtime Class

| Value | Description | SSH start needed | healthCheck in dev |
|-------|-------------|:---:|:---:|
| `dynamic` | nodejs, go, bun, python, java, rust, dotnet, elixir | yes | no |
| `implicit-webserver` | php-nginx, php-apache | no (auto-starts) | no |
| `static` | nginx, static | no (auto-starts) | no |
| `worker` | no ports, background processing | yes | no |
| `managed` | postgresql, valkey, mariadb, elasticsearch, etc. | N/A | N/A |

Derived from: service type string (e.g., `nodejs@22` → dynamic, `php-nginx@8.4` → implicit-webserver).

### D7: Iteration

| Value | Tier | Guidance behavior |
|-------|------|-------------------|
| `0` | Normal | Full procedure guidance (curated path) |
| `1-2` | DIAGNOSE | Replaces normal guidance. "Check zerops_logs for the error." |
| `3-4` | SYSTEMATIC | Replaces normal guidance. "Check ALL config systematically." |
| `5+` | STOP | Replaces normal guidance. "Present summary to user, ask for input." |

Set by: `zerops_workflow action="iterate"`. Increments counter, max 10 (configurable via `ZCP_MAX_ITERATIONS`).

### Dimension Constraints (Invalid Combinations)

| ID | Constraint | Reason |
|----|-----------|--------|
| C1 | D5=`manual` + D1=`deploy` session | Manual strategy does not create deploy sessions |
| C2 | D3=`managed-only` + D2=`generate`/`deploy`/`close` active | Managed-only skips these steps |
| C3 | D5=`unset` + D1=`deploy` session started | Strategy gate blocks session creation |
| C4 | D4=`local` + `zerops_mount` operational | Mount tool not available in local mode |
| C5 | Mixed D5 values in single deploy session | Error: "Mixed strategies not supported" |

---

## 3. Rules

Rules are ordered by execution flow. Each rule specifies WHEN it activates, WHAT it does, and WHY.

**Notation**: `D1=bootstrap` means dimension D1 has value `bootstrap`. `|` means OR within a dimension. Omitted dimensions mean "any value."

**Conflict resolution**: When multiple rules match, all fire (additive) unless explicitly noted as REPLACE. Higher priority number wins on conflict.

---

### 3.1 System Startup Rules

#### R-001: Init instructions — environment routing

**WHEN**: System startup (before any workflow)

**THEN**:
- If D4=`container`: use `containerEnvironment` section. Describes: control plane role, SSHFS mounts at `/var/www/{hostname}/`, SSH access via `ssh {hostname}`, file edits survive restarts but not deploys.
  - If `rt.ServiceName != ""`: append "You are running on the '{serviceName}' service."
- If D4=`local`: use `localEnvironment` section. Describes: code in working directory, deploy via `zcli push`, zerops.yml at repo root.
- All sections overridable via env vars: `ZCP_INSTRUCTION_BASE`, `ZCP_INSTRUCTION_CONTAINER`, `ZCP_INSTRUCTION_LOCAL`.

**BECAUSE**: Agent needs foundational mental model of WHERE code lives and HOW deployment works. This is CANNOT KNOW (platform fact) — agent can't infer that deploy creates new containers or that SSHFS mounts exist. Delivered once at startup, never re-injected.

#### R-002: Init instructions — workflow hint

**WHEN**: System startup AND `.zcp/state/` has active sessions

**THEN**: Include resumable workflow hint with session ID, workflow type, current step, intent.
- D1=`bootstrap`: show step progress + intent
- D1=`deploy`: show step progress + intent
- D1=`recipe`: show step progress + intent + framework/tier (if plan exists)

**BECAUSE**: Agent may be resuming a previous session. Hint enables continuation without agent having to discover state. This is session-specific context the agent CANNOT KNOW without this hint.

#### R-003: Init instructions — project summary

**WHEN**: System startup AND client + projectID available

**THEN**: Classify all services into categories:
- **Bootstrapped**: has complete ServiceMeta (BootstrappedAt set). Show with mount path (container) or label.
- **Unmanaged**: runtime without complete meta, not stage of bootstrapped. Label: "needs ZCP adoption."
- **Managed**: IsManagedService=true. Label by type.
- Include post-bootstrap orientation if bootstrapped services exist.
- Include workflow offerings from `Route()` if available.

**BECAUSE**: Agent needs to know current project state to decide what to do. The classification (bootstrapped vs unmanaged) is CANNOT KNOW — it depends on ServiceMeta files the agent hasn't read.

#### R-004: Tool registration — environment routing

**WHEN**: System startup, tool registration

**THEN**:
- If D4=`container`: register `RegisterDeploySSH` (SSH-based deploy), register `zerops_mount` (SSHFS mount tool).
- If D4=`local`: register `RegisterDeployLocal` (zcli push-based deploy). Mount tool returns error if called.

**BECAUSE**: Deploy mechanism is fundamentally different between environments. Container uses SSH into service to run zcli push; local uses zcli directly. CANNOT KNOW which is available — determined by runtime detection.

---

### 3.2 Bootstrap Workflow — Knowledge Rules

#### R-010: Bootstrap discover — inject platform model

**WHEN**: D1=`bootstrap`, D2=`discover`

**THEN**:
- Inject: `GetModel()` — full platform model from `themes/model.md`
- Static guidance: `discover` section from `bootstrap.md`
- If D4=`local`: append `discover-local` section

**BECAUSE**: Asymmetry HIGH — first contact with Zerops. Agent needs to understand platform topology, service types, modes. Platform model is CANNOT KNOW. Agent can't choose mode or plan services without understanding what modes mean on Zerops.

#### R-011: Bootstrap provision — inject import schema

**WHEN**: D1=`bootstrap`, D2=`provision`

**THEN**:
- Inject: H2 section "import.yaml Schema" from `zerops://themes/core`
- Static guidance: `provision` section from `bootstrap.md`
- If D4=`local`: append `provision-local` section

**BECAUSE**: Agent needs to write import.yml to create services. The import.yaml format is Zerops-specific — CANNOT KNOW. Without the schema, agent would guess at field names and likely produce invalid YAML.

#### R-012: Bootstrap generate — knowledge injection (PRIMARY RULE)

**WHEN**: D1=`bootstrap`, D2=`generate`, D3≠`managed-only`

**THEN** (additive, all fire):
1. Inject: `GetBriefing(runtimeBase, dependencyTypes, mode, liveTypes)` — 7-layer composition (see R-050)
2. Inject: H2 section "zerops.yaml Schema" from `zerops://themes/core`
3. Inject: `formatEnvVarsForGuide(session.DiscoveredEnvVars)` — `${hostname_varName}` reference list
4. Static guidance resolved by R-013 (environment) and R-014 (mode)

**BECAUSE**: Asymmetry HIGH — agent writes zerops.yml for first time on Zerops. CANNOT KNOW: deployFiles semantics (only thing surviving deploy), env var wiring syntax (`${hostname_varName}` with silent failure on typo), 0.0.0.0 binding requirement (Zerops routing), build≠run container. CANNOT DERIVE: no existing config to read (new project). Curated path needed: mode-specific start commands, healthCheck rules, deployFiles patterns. Full injection justified because agent doesn't know what to ask for — it doesn't know what it doesn't know.

**CONTRAST with R-030 (deploy prepare)**: Deploy only POINTS to knowledge because agent already has context from bootstrap — asymmetry LOW. Re-injecting ~100 lines of briefing would waste context on derivable information.

#### R-013: Bootstrap generate/deploy — environment routing

**WHEN**: D1=`bootstrap`, D2=`generate`|`deploy`

**THEN**:
- If D4=`local` AND D2=`generate`: use `generate-local` section. REPLACES base + mode-specific sections entirely.
  - Includes: files written locally, .env credential bridge, VPN setup, zcli push workflow, REAL start command.
- If D4=`local` AND D2=`deploy`: use `deploy-local` section. REPLACES base deploy section entirely.
  - Includes: zcli push from local, no SSH, no SSHFS references.
- If D4=`container`: use base section + mode addenda (R-014).

**BECAUSE**: Local workflow is fundamentally different from container workflow. No SSH, no SSHFS, code in working directory, deploy via zcli push. Mixing container + local instructions would create confusing interleave. Self-contained replacement section is clearer than conditional addenda. This is a curated path decision: local gets its own coherent guidance rather than a patched version of container guidance.

**PRIORITY**: Environment routing fires BEFORE mode routing. If D4=`local`, mode addenda (R-014) do NOT apply (replaced by self-contained section).

#### R-014: Bootstrap generate — mode routing (container only)

**WHEN**: D1=`bootstrap`, D2=`generate`, D4=`container`

**THEN**: Append mode-specific sections to base `generate` section:
- If D3 includes `standard`: append `generate-standard`. Contains: `zsc noop --silent` start, no healthCheck, `deployFiles: [.]`, stage entry NOT yet.
- If D3 includes `dev`: append `generate-dev`. Contains: dev-only mode, no stage service.
- If D3 includes `simple`: append `generate-simple`. Contains: real start command, healthCheck required, auto-starts.
- Multiple mode sections can be appended simultaneously (mixed plan with multiple targets).

**BECAUSE**: Each mode has different zerops.yml rules — these are curated path:
- Standard uses `zsc noop` so agent controls server lifecycle manually for debugging. Alternative (real start) works but agent loses ability to iterate without redeploy.
- Standard has no healthCheck on dev because noop exits immediately — HC would restart container.
- Simple has healthCheck because Zerops monitors and restarts on failure.
- Stage entry deferred to AFTER dev verification to avoid deploying untested config.

#### R-015: Bootstrap deploy — conditional sections (container only)

**WHEN**: D1=`bootstrap`, D2=`deploy`, D4=`container`

**THEN**:
- Base: `deploy` section from `bootstrap.md`
- If `len(plan.Targets) >= 3`: append `deploy-agents` section (multi-service orchestration guidance)
- If `failureCount > 0`: append `deploy-recovery` section

**BECAUSE**: 3+ services require parallel orchestration — spawning sub-agents per service pair. This is curated path: sequential deployment of 3+ services would work but is slow and error-prone. Recovery section only on failure to avoid noise on first attempt.

#### R-016: Bootstrap deploy — NO knowledge injection

**WHEN**: D1=`bootstrap`, D2=`deploy`

**THEN**: No `GetBriefing()`, no schema injection, no env var injection. Static guidance only.

**BECAUSE**: Agent just completed generate step — it wrote the zerops.yml and code. It KNOWS the config, the env vars, the runtime specifics. Re-injecting would waste context on information the agent created moments ago. Deploy procedure (SSH push, subdomain enable, verify) is in the static guidance.

**CONTRAST with R-012**: Generate injects because agent hasn't seen config yet. Deploy doesn't inject because agent just wrote it.

#### R-017: Bootstrap close — transition message

**WHEN**: D1=`bootstrap`, D2=`close`, bootstrap completes (Active→false)

**THEN**:
- `BuildTransitionMessage()` returns:
  - List of bootstrapped services with modes and stage hostnames
  - Strategy selection options (push-dev, push-git, manual) with explanations
  - Command hint: `zerops_workflow action="strategy" strategies={"hostname":"push-dev"}`
  - Next step hints per strategy
- Write ServiceMeta files with `BootstrappedAt` timestamp
- Append reflog to CLAUDE.md

**BECAUSE**: Agent needs to choose strategy — this is a project/team decision, not a technical one. CANNOT KNOW which strategy fits the user's workflow. Presenting all three equally lets agent/user decide. Strategy is NEVER auto-assigned because the wrong default would establish an unwanted deployment pattern.

#### R-018: Bootstrap non-progressive steps — local addendum

**WHEN**: D1=`bootstrap`, D2=`discover`|`provision`|`close`, D4=`local`

**THEN**: Append `{step}-local` section from `bootstrap.md` after base section (addendum, not replacement).

**BECAUSE**: These steps have minor local-specific additions (e.g., provision-local mentions .env and VPN) rather than entirely different workflows. Addendum is sufficient — the base procedure is the same.

**CONTRAST with R-013**: Generate/deploy have fundamentally different local workflows requiring full replacement. Discover/provision/close only need supplementary notes.

---

### 3.3 Deploy Workflow — Knowledge Rules

#### R-020: Deploy start — strategy gate

**WHEN**: D1=`deploy`, action=`start`

**THEN** (sequential gates):
1. Read ServiceMeta files. Filter to complete (`BootstrappedAt` set) + runtime (Mode or StageHostname present).
2. If no complete runtime metas → error (suggest checking incomplete bootstraps).
3. If ANY meta has D5=`unset` → return strategy selection guidance (conversational, not error). Block session creation.
4. If ALL metas have D5=`manual` → return manual deploy response with per-service `zerops_deploy` commands. No session created.
5. If metas have MIXED strategies → error: "Mixed strategies not supported in single deploy session."
6. Otherwise → create deploy session with D5 from first meta.

**BECAUSE**: Strategy determines the entire interaction model. Push-dev = guided SSH workflow. CI/CD = webhook setup. Manual = no workflow. Without knowing strategy, system can't assemble correct guidance. Manual gets no session because the user explicitly said "I'll handle it." Mixed strategies can't coexist because push-dev guidance says "SSH into dev" while push-git says "set up webhook" — interleaving makes no sense.

#### R-021: Deploy start — target construction

**WHEN**: D1=`deploy`, session created (passed strategy gate)

**THEN**: `BuildDeployTargets(metas)` constructs ordered targets:
- For each ServiceMeta: create DeployTarget with hostname, role, strategy.
- Role assignment via `deployRoleFromMode()`:
  - D3=`simple` → role=`simple`
  - D3=`dev` → role=`dev`
  - D3=`standard` with StageHostname → role=`dev`, then append stage target with role=`stage`
  - D3=`standard` without StageHostname → role=`simple` (fallback)
- Dev targets always ordered before their stage counterparts.
- Mode detected from first meta's Mode (default to `standard` if empty).

**BECAUSE**: Target ordering matters — dev MUST deploy before stage (curated path). Stage receives code from dev, so dev must be verified first. Role determines what guidance the agent receives per-target.

#### R-030: Deploy prepare — compact guidance + pointers

**WHEN**: D1=`deploy`, D2=`prepare`

**THEN**:
- INJECT (compact, ~25-40 lines):
  - Setup summary: target hostnames, mode, strategy (personalized)
  - Checklist (mode-specific):
    - D3=`standard`|`dev`: "Dev entry: `zsc noop --silent`, NO healthCheck"
    - D3=`simple`: "Entry must have real `start:` command and `healthCheck`"
    - D3=`standard`: "Stage entry: real `start:` + healthCheck required"
  - Platform facts: deploy = new container, deployFiles only thing surviving, env var typo = silent
  - If D4=`container`: SSHFS mount paths for non-stage targets
  - Strategy note: current strategy + alternatives + change command
- POINT (knowledge map):
  - Per runtime: `zerops_knowledge query="{runtimeBase}"` (deduplicated, skips stage role)
  - General: `zerops_knowledge query="zerops.yml"`, `zerops_discover`

**BECAUSE**: Asymmetry LOW — agent bootstrapped previously, knows the config structure. Platform facts reminder is compact (5 lines) because agent has seen them before but may have forgotten across sessions. Knowledge MAP (pointers) instead of injection because agent knows what it needs and can request on demand. Saves ~100 lines of context vs bootstrap generate.

**CONTRAST with R-012**: Bootstrap generate injects full briefing (agent's first time). Deploy prepare only points (agent's second+ time).

#### R-031: Deploy execute — mode-specific workflow

**WHEN**: D1=`deploy`, D2=`execute`, D7=`0` (no iteration)

**THEN**:
- If D4=`local`: call `writeLocalWorkflow()` — per-target `zcli push` + verify
- If D4=`container`:
  - If D3=`standard`: call `writeStandardWorkflow()` — dev deploy → manual start → verify → stage deploy → auto-start → verify
  - If D3=`dev`: call `writeDevWorkflow()` — dev deploy → manual start → verify
  - If D3=`simple`: call `writeSimpleWorkflow()` — deploy → auto-start → verify
  - Default: `writeStandardWorkflow()`
- Key facts (D4-dependent):
  - D4=`container` + has dev role: "Dev uses `zsc noop` — start server manually via SSH after deploy"
  - D4=`container` + has stage role: "Stage auto-starts with real `start:` + healthCheck"
  - D4=`local`: "Deploy via `zcli push`, each deploy = full rebuild + new container"
- Code-only changes:
  - D4=`container`: "Edit on SSHFS mount, restart via SSH. Redeploy only if zerops.yml changed."
  - D4=`local`: "Edit locally, hot reload if supported. Redeploy to persist."

**BECAUSE**: Each (environment, mode) combination produces a fundamentally different deploy procedure. Standard mode has dev→stage sequencing (curated path: catch errors cheaply on dev). Local mode has no SSH/SSHFS concepts. These are curated path — the agent could deploy in a different order, but our sequencing minimizes wasted iteration.

#### R-032: Deploy execute — iteration escalation

**WHEN**: D1=`deploy`, D2=`execute`, D7≥`1`

**THEN**: REPLACE normal workflow guidance (R-031) with escalation tier:
- D7=`1-2`: `"DIAGNOSE: Check zerops_logs severity=error. Build failed? → build log. Container didn't start? → start command, runtime version, env vars, port binding."`
- D7=`3-4`: `"PREVIOUS FIXES FAILED. Systematic check: zerops.yml (ports, start, deployFiles), env var references (${hostname_varName}), runtime version, 0.0.0.0 binding."`
- D7=`5+`: `"STOP. Multiple fixes failed. Present diagnostic summary to user: exact error, current config, env var values, what was tried. User decides."`
- Format: `"ITERATION {N} (session remaining: {max-N})\n\nPREVIOUS: {lastAttestation}\n\n{guidance}"`

**BECAUSE**: Normal guidance was already read (agent KNOWS it). Re-injecting procedure is noise + "try same thing again" signal. Problem shifted from "how to do it" to "why it failed" — CANNOT KNOW for agent, requires DIAGNOSTIC knowledge. Escalation tiers represent increasing desperation: specific diagnosis → systematic review → human intervention. Tier 5+ recognizes that if 4+ iterations failed, the problem is likely outside agent capability.

**PRIORITY**: Iteration > 0 ALWAYS fires instead of R-031. Check: `if iteration > 0` runs before mode-specific workflow selection.

#### R-033: Deploy verify — static guidance

**WHEN**: D1=`deploy`, D2=`verify`

**THEN**: Extract `deploy-verify` section from `deploy.md`. Fallback: "Run zerops_verify for each target. Check health status."

**BECAUSE**: Verification is straightforward — run zerops_verify per service. No mode/environment branching needed. The guidance is the same regardless of dimensions.

---

### 3.4 Recipe Workflow — Knowledge Rules

#### R-040: Recipe research — no knowledge injection

**WHEN**: D1=`recipe`, D2=`research`

**THEN**:
- Static guidance: if `tier == RecipeTierShowcase` → `research-showcase` section; else `research-minimal` section
- No knowledge provider calls

**BECAUSE**: Agent researches the framework from its own training data. Zerops knowledge would bias the research toward existing patterns rather than discovering what the framework needs.

#### R-041: Recipe provision — inject import schema

**WHEN**: D1=`recipe`, D2=`provision`

**THEN**: Inject H2 section "import.yaml Schema" from `zerops://themes/core`. Static guidance: `provision` section from `recipe.md`.

**BECAUSE**: Same reasoning as R-011 — agent needs import.yaml format to create test infrastructure.

#### R-042: Recipe generate — inject briefing + schema

**WHEN**: D1=`recipe`, D2=`generate`

**THEN**:
- If `plan.RuntimeType != ""`: inject `GetBriefing(runtimeBase, nil, "", nil)` — runtime-specific Zerops knowledge
- If `len(discoveredEnvVars) > 0`: inject formatted env var references
- Inject H2 section "zerops.yaml Schema"
- Static guidance: `generate` + `generate-fragments` sections from `recipe.md`

**BECAUSE**: Same reasoning as R-012 — agent writing zerops.yml needs Zerops-specific config patterns. Note: mode is NOT passed to GetBriefing for recipes (recipe must cover all modes — agent writes base + prod + dev setup blocks).

**CONTRAST with R-012**: Bootstrap passes mode to GetBriefing (agent writes for ONE mode). Recipe doesn't pass mode (agent writes for ALL modes).

#### R-043: Recipe deploy — iteration escalation only

**WHEN**: D1=`recipe`, D2=`deploy`

**THEN**:
- If D7≥`1`: replace guidance with `buildRecipeIterationDelta()` (same escalation tiers as R-032)
- If D7=`0`: static guidance only, no knowledge injection

**BECAUSE**: Same as R-016 — agent just wrote the code, KNOWS the config. Iteration escalation follows same logic as R-032.

#### R-044: Recipe finalize — inject import schema

**WHEN**: D1=`recipe`, D2=`finalize`

**THEN**: Inject H2 section "import.yaml Schema" from `zerops://themes/core`. Static guidance: `finalize` section from `recipe.md`.

**BECAUSE**: Agent generates 6 environment-specific import.yaml files. Needs schema to validate correct format across environments (minimal, balanced, ha, ha-benchmark, dev, showcase).

---

### 3.5 Cross-Cutting Rules

#### R-050: Briefing assembly — 7-layer composition

**WHEN**: `GetBriefing(runtime, services, mode, liveTypes)` called (from R-012, R-042, or on-demand via zerops_knowledge)

**THEN** (sequential composition, each layer optional):
1. **Live stacks**: if `FormatServiceStacks(liveTypes)` non-empty → prepend formatted stack list
2. **Runtime guide**: if `runtime != ""` → normalize to base slug → inject `getRuntimeGuide(slug)` (from `recipes/{slug}-hello-world` or `bases/{slug}`)
3. **Matching recipes**: if `runtimeBase != ""` → inject hints for available framework recipes
4. **Service cards**: if `len(services) > 0` → for each service: normalize name → inject H2 section from `themes/services.md`
5. **Wiring syntax**: if services present → inject "Wiring Syntax" section from `services.md`
6. **Decision hints**: if relevant decisions found (via `decisionSectionMap`) → inject first-paragraph summaries from `decisions/*.md`
7. **Version check**: if `FormatVersionCheck(runtime, services, liveTypes)` non-empty → inject version validation (✓/⚠)

**Auto-promotion**: if `runtime == ""` and services contain a known runtime name → promote first match from services to runtime parameter, remove from services list.

**BECAUSE**: Each layer adds specificity. Layer composition is best-effort — missing layers are silently skipped. This enables graceful degradation: a stack with no managed services skips layers 4-6 without error.

#### R-051: Mode adaptation — recipe prepend

**WHEN**: `GetRecipe(name, mode)` called AND `mode != ""`

**THEN**:
- D3=`dev`|`standard`: prepend `"> **Mode: dev** — Use the \`dev\` setup block from the zerops.yml below.\n\n"`
- D3=`simple`: prepend `"> **Mode: simple** — Use the \`prod\` setup block below, but override \`deployFiles: [.]\`.\n\n"`
- Always prepend universals (platform constraints) before recipe content

**BECAUSE**: Recipes contain multiple setup blocks (base, prod, dev). Agent needs to know which block to use for current mode. Standard and dev both use the `dev` block. Simple uses `prod` but with deployFiles override. This is curated path: agent could read the full recipe and decide, but a one-line directive saves time and prevents errors.

#### R-052: Session-aware knowledge filtering

**WHEN**: `zerops_knowledge` called (any mode: briefing, recipe, query)

**THEN**: `resolveKnowledgeMode()` determines mode filter:
1. Explicit `inputMode` parameter → return immediately (highest priority)
2. Active bootstrap session → return `PlanMode()` (standard/dev/simple/mixed/"")
3. Active deploy session → return `Deploy.Mode`
4. No active session → return `""` (unfiltered)

**BECAUSE**: Agent in a bootstrap/deploy session needs mode-filtered knowledge. If mode is `standard`, agent shouldn't see `simple` mode patterns (and vice versa). Session-aware filtering eliminates noise. Agent can always override with explicit `mode` parameter for edge cases (e.g., `mode="stage"` to see prod patterns during dev workflow).

#### R-053: Iteration — guidance replacement pattern

**WHEN**: D7≥`1`, D2 is a re-entered step after iteration

**THEN**: Check `BuildIterationDelta()` FIRST:
- If returns non-empty: return it immediately. Normal guidance (static + knowledge) is NOT assembled.
- If returns empty (step is not `deploy`, or iteration=0): proceed with normal assembly.

**BECAUSE**: Iteration delta replaces normal guidance — it does not append to it. Normal guidance was already read. Appending would mix "how to do it" (already tried) with "why it failed" (new information). Clean replacement signals "try something DIFFERENT."

**Implementation**: In `assembleGuidance()`, iteration check is the FIRST operation. If delta is non-empty, function returns immediately without calling `resolveStaticGuidance()` or `assembleKnowledge()`.

#### R-054: Bootstrap iteration — step reset scope

**WHEN**: D1=`bootstrap`, action=`iterate`

**THEN**: Reset steps 2-3 (`generate` + `deploy`) to `pending`. Preserve: `discover`, `provision`, `close`. Set CurrentStep to first reset step. Set Active=true.

**BECAUSE**: Discovery and provisioning are done — infrastructure exists, env vars discovered. The problem is in code/config, so only generate and deploy need retry. Close is deferred until deploy succeeds.

#### R-055: Deploy iteration — step reset scope

**WHEN**: D1=`deploy`, action=`iterate`

**THEN**: Reset steps 1-2 (`execute` + `verify`) to `pending`. Preserve: `prepare`. Reset all targets to `pending` status.

**BECAUSE**: Preparation (zerops.yml validation) is done. The problem is in deployment execution, so only execute and verify need retry.

#### R-056: Recipe iteration — step reset scope

**WHEN**: D1=`recipe`, action=`iterate`

**THEN**: Reset steps 2-4 (`generate` + `deploy` + `finalize`) to `pending`. Preserve: `research`, `provision`, `close`.

**BECAUSE**: Research and provisioning are done. Code/config needs retry.

#### R-057: Skip validation

**WHEN**: `zerops_workflow action="skip"` for any step

**THEN**:
- D2=`discover`|`provision`: ALWAYS error — "mandatory and cannot be skipped"
- D2=`generate`|`deploy`|`close`: allowed ONLY if `plan == nil || len(plan.Targets) == 0` (managed-only)
  - If runtime targets exist: error — "cannot skip — runtime services in plan require it"
- Recipe: only `close` is skippable

**BECAUSE**: Discover and provision are always needed — even managed-only projects need infrastructure created and env vars discovered. Generate/deploy/close are only skippable when there's nothing to generate/deploy (no runtime services).

#### R-058: Adoption — per-target behavior modification

**WHEN**: D1=`bootstrap`, target has `isExisting=true`

**THEN** (per-target, not global):
- Provision: skip import for this target (already exists). Still mount + discover env vars.
- Generate: skip zerops.yml + code generation for this target. Checker skips validation.
- Deploy: verify-only for this target (no `zerops_deploy`, just `zerops_verify`).
- Close: write ServiceMeta identically (adopted and new get same meta).

**BECAUSE**: Existing services are already running — recreating would destroy them. Agent adopts by registering them in ZCP (writing ServiceMeta) without modifying the service itself. Mixed plans (some new, some existing) are fully supported — each target follows its own path.

---

### 3.6 ServiceMeta Lifecycle Rules

#### R-060: Partial meta on provision

**WHEN**: D1=`bootstrap`, D2=`provision` completes

**THEN**: Write ServiceMeta for each runtime target with: Hostname, Mode, StageHostname, Environment, BootstrapSession. NO `BootstrappedAt`, NO `DeployStrategy`.

**BECAUSE**: Signals bootstrap in-progress. `IsComplete()` returns false (no BootstrappedAt). Hostname lock check (`checkHostnameLocks()`) uses this to prevent concurrent bootstrap of same service by another session.

#### R-061: Full meta on bootstrap completion

**WHEN**: D1=`bootstrap`, Active→false (all steps done)

**THEN**: Overwrite ServiceMeta with full data: add `BootstrappedAt = today's date`. Strategy stays empty (set separately via R-020 strategy gate).

**BECAUSE**: `BootstrappedAt` timestamp signals completion. `IsComplete()` returns true. Future workflows (deploy, route) use this to identify available services.

**Environment-specific hostname**:
- D4=`container`: meta hostname = devHostname, StageHostname = stageHostname
- D4=`local` + standard mode: meta hostname = stageHostname, StageHostname = "" (inverted)

#### R-062: Strategy update

**WHEN**: `zerops_workflow action="strategy" strategies={...}`

**THEN**: For each hostname→strategy pair:
- Validate strategy is one of: `push-dev`, `push-git`, `manual`
- Update `ServiceMeta.DeployStrategy`
- Return next-step hint:
  - All `manual` → "When code is ready: `zerops_deploy` directly"
  - All `push-git` → "Set up CI/CD: `zerops_workflow action=\"start\" workflow=\"cicd\"`"
  - Otherwise → "When code is ready: `zerops_workflow action=\"start\" workflow=\"deploy\"`"

**BECAUSE**: Strategy determines interaction model. Hint saves agent from having to figure out what to do next. Different strategies lead to different workflows.

---

### 3.7 Workflow Routing Rules

#### R-070: Route — flow offerings

**WHEN**: `zerops_workflow action="route"`

**THEN**: `Route()` evaluates project state and returns prioritized offerings:
1. Incomplete bootstrap (priority 1): resume or start hint
2. Unmanaged runtimes (priority 1): bootstrap adoption offering
3. Bootstrapped with push-dev strategy (priority 1): deploy workflow
4. Bootstrapped with push-git strategy (priority 1): cicd workflow + deploy (priority 2)
5. Bootstrapped with manual strategy: no deploy offering (direct tool use)
6. Add new services (priority 3): bootstrap start hint
7. Nothing bootstrapped (priority 1): bootstrap creation hint
8. Utilities (priority 4-5): recipe, scale

**BECAUSE**: Route returns FACTS, not recommendations. Agent decides based on user intent + discovered state. Prioritization helps agent present most relevant options first.

---

## 4. Scenarios

Worked end-to-end traces showing rules composing. Each shows accumulated state and asymmetry shift.

### 4.1 Scenario A: Bootstrap standard, container, nodejs + postgresql

**Initial state**: `{D1=bootstrap, D3=standard, D4=container, D5=unset, D7=0}`

**Step: discover**
- Rules fired: R-010 (inject platform model)
- Agent receives: GetModel() + discover section
- Agent KNOWS after: Zerops modes, service types, plan structure
- Asymmetry: HIGH → HIGH-MEDIUM

**Step: provision**
- Rules fired: R-011 (inject import schema), R-018 does NOT fire (D4=container, no local addendum for provision)
- Agent receives: import.yaml schema + provision section
- Agent KNOWS after: service states, env var names (db→[connectionString, host, port, user, password])
- Asymmetry: HIGH-MEDIUM → MEDIUM

**Step: generate**
- Rules fired: R-012 (inject briefing+schema+envvars), R-013 (D4=container → base sections), R-014 (D3=standard → generate-standard)
- NOT fired: R-013 local replacement (D4≠local)
- Agent receives: 7-layer briefing (nodejs guide + postgresql card + wiring + version check) + zerops.yml schema + env var list + generate + generate-standard sections
- Agent KNOWS after: zerops.yml structure, runtime specifics, env var wiring syntax
- Asymmetry: MEDIUM → LOW

**Step: deploy**
- Rules fired: R-016 (no knowledge injection), R-015 (D4=container, base deploy section), R-015 conditional: if 2 services (appdev+db) < 3 → no deploy-agents
- Agent receives: static deploy guidance only
- Asymmetry: LOW → LOW (no new information needed)

**Step: close**
- Rules fired: R-017 (transition message + strategy selection)
- Agent receives: service list, strategy options, next-step hints
- Output: ServiceMeta written with BootstrappedAt, reflog appended

**After bootstrap → strategy → deploy**:
- Agent calls `action="strategy" strategies={"appdev":"push-dev"}` → R-062 fires
- Agent calls `action="start" workflow="deploy"` → R-020 fires:
  - Gate 1: strategy set ✓
  - Gate 2: not all manual ✓
  - Gate 3: not mixed ✓
  - → session created
- R-030 fires (prepare: compact guidance + pointers)
- R-031 fires (execute: standard workflow for container)

### 4.2 Scenario B: Bootstrap simple, local, bun + valkey

**Initial state**: `{D1=bootstrap, D3=simple, D4=local, D5=unset, D7=0}`

**Step: discover**
- Rules fired: R-010, R-018 (D4=local → append discover-local)
- Agent receives: platform model + discover section + discover-local addendum

**Step: provision**
- Rules fired: R-011, R-018 (D4=local → append provision-local)
- Agent receives: import schema + provision section + local addendum (mentions .env, VPN)

**Step: generate**
- Rules fired: R-012 (inject briefing for bun+valkey), R-013 (D4=local → REPLACE with generate-local)
- NOT fired: R-014 (blocked by R-013 — local replaces everything)
- Agent receives: briefing (bun guide + valkey card + wiring) + zerops.yml schema + env vars + generate-local section
- Note: generate-local includes: files written locally, .env bridge, VPN setup, REAL start command (not zsc noop), healthCheck present

**Step: deploy**
- Rules fired: R-013 (D4=local → deploy-local), R-016 (no knowledge injection)
- Agent receives: deploy-local section (zcli push, no SSH)

### 4.3 Scenario C: Deploy with iteration failure

**Initial state**: `{D1=deploy, D3=standard, D4=container, D5=push-dev, D7=0}`

**Step: prepare (iteration 0)**
- Rules fired: R-030 (compact guidance + pointers)
- Agent receives: setup summary, mode checklist, platform facts, knowledge map
- Checker: `checkDeployPrepare()` validates zerops.yml

**Step: execute (iteration 0)**
- Rules fired: R-031 (standard workflow for container)
- Agent deploys dev → fails verification

**iterate (D7 becomes 1)**
- Rules fired: R-055 (reset execute+verify, preserve prepare)
- Agent re-enters execute step

**Step: execute (iteration 1)**
- Rules fired: R-032 (D7=1 → DIAGNOSE tier). R-031 NOT fired (replaced by R-032).
- Agent receives: "ITERATION 1 (session remaining: 9)\n\nDIAGNOSE: Check zerops_logs..."
- Agent diagnoses → fixes → still fails

**iterate (D7 becomes 2)**
- Same reset. Agent re-enters execute.

**Step: execute (iteration 2)**
- Rules fired: R-032 (D7=2, still ≤2 → DIAGNOSE tier)
- Agent gets similar guidance but with last attestation context

**iterate (D7 becomes 3)**

**Step: execute (iteration 3)**
- Rules fired: R-032 (D7=3, ≤4 → SYSTEMATIC tier)
- Agent receives: "SYSTEMATIC: Check ALL config: zerops.yml, env vars, runtime version, 0.0.0.0 binding..."

### 4.4 Scenario D: Recipe workflow generate step

**Initial state**: `{D1=recipe, D2=generate, D3="", D4=container, D7=0}`

**Step: generate**
- Rules fired: R-042 (inject briefing + schema)
- Note: mode NOT passed to GetBriefing (recipe covers all modes)
- Agent receives: runtime briefing (all deploy patterns, not mode-filtered) + env vars + zerops.yml schema + generate + generate-fragments sections
- Agent writes zerops.yml with base, prod, and dev setup blocks

---

## 5. Invariants

Verifiable properties derived from the model. Each references the rules that enforce it.

| ID | Invariant | Enforcing rules | Testable assertion |
|----|-----------|----------------|-------------------|
| KD-001 | HIGH asymmetry + NEEDS NOW → MUST inject | R-010, R-011, R-012, R-041, R-042, R-044 | Bootstrap discover/provision/generate and recipe provision/generate/finalize inject knowledge |
| KD-002 | LOW asymmetry → MUST NOT inject full knowledge | R-016, R-030, R-043 | Bootstrap deploy and deploy prepare never call GetBriefing() directly into response |
| KD-003 | Deploy workflow delivers knowledge as POINTERS, not injection | R-030 | Deploy prepare guidance contains `zerops_knowledge query=` strings, not knowledge content |
| KD-004 | Iteration ≥ 1 REPLACES normal guidance, never appends | R-032, R-043, R-053 | When iteration > 0, assembleGuidance returns iteration delta without calling resolveStaticGuidance |
| KD-005 | Strategy is NEVER auto-assigned | R-017, R-020, R-061 | ServiceMeta.DeployStrategy is empty after bootstrap; only set by explicit action="strategy" |
| KD-006 | D4=local REPLACES (not extends) container guidance for generate/deploy | R-013 | When env=local, generate-local section is used; generate-standard/dev/simple NOT appended |
| KD-007 | Mode addenda are ADDITIVE in container mode | R-014 | Multiple mode sections can be appended simultaneously for mixed plans |
| KD-008 | Environment routing fires BEFORE mode routing | R-013, R-014 | R-013 (env check) determines whether R-014 (mode addenda) applies |
| KD-009 | discover/provision are NEVER skippable | R-057 | validateSkip returns error for "discover" and "provision" regardless of plan |
| KD-010 | generate/deploy/close skippable ONLY for managed-only | R-057 | validateSkip returns nil only when plan.Targets is empty |
| KD-011 | Session-aware mode filtering applies automatically | R-052 | Active bootstrap → PlanMode() used; active deploy → Deploy.Mode used; no session → unfiltered |
| KD-012 | Recipe GetBriefing does NOT pass mode | R-042, R-012 | Recipe passes `mode=""` to GetBriefing; bootstrap passes actual mode |
| KD-013 | Per-target adoption does NOT affect global step flow | R-058 | isExisting modifies behavior within a step, not step progression |
| KD-014 | Iteration resets preserve earlier steps | R-054, R-055, R-056 | discover/provision never reset; prepare never reset; research/provision never reset |
| KD-015 | 7-layer briefing is best-effort | R-050 | Missing layers silently skipped; no error on absent runtime guide or service card |
| KD-016 | Env vars injected at generate, NOT at deploy | R-012, R-016 | assembleKnowledge passes envVars only when step != StepDeploy |
| KD-017 | Init instructions delivered ONCE at startup | R-001, R-002, R-003 | BuildInstructions called during server setup, not per tool call |
| KD-018 | Deploy prepare guidance ≤ 55 lines | R-030 | Max line count enforced by test |
| KD-019 | Mixed strategies in single deploy session → error | R-020 | targets[i].Strategy != targets[0].Strategy triggers error |

---

## 6. Verification Protocol

When verifying a proposed change against this specification:

### Step 1: Identify affected dimensions

Determine which of D1-D7 the change touches. A change to deploy guidance affects D1=deploy, possibly D2, D3, D4.

### Step 2: Find all rules mentioning those dimensions

Scan §3 for rules whose WHEN clause includes the affected dimensions. Use this index:

| Dimension | Rules |
|-----------|-------|
| D1=bootstrap | R-010 through R-018, R-054, R-057, R-058, R-060, R-061 |
| D1=deploy | R-020, R-021, R-030 through R-033, R-055, R-062 |
| D1=recipe | R-040 through R-044, R-056 |
| D2 (any step) | All step-specific rules in §3.2-3.4 |
| D3=standard | R-014, R-021, R-030, R-031 |
| D3=dev | R-014, R-021, R-030, R-031 |
| D3=simple | R-014, R-021, R-030, R-031 |
| D3=managed-only | R-057 |
| D4=container | R-001, R-004, R-013, R-014, R-015, R-030, R-031 |
| D4=local | R-001, R-004, R-013, R-018, R-031 |
| D5 (strategy) | R-017, R-020, R-062, R-070 |
| D6 (runtime class) | R-050 (briefing layer 2), R-031 (SSH start) |
| D7 (iteration) | R-032, R-043, R-053, R-054, R-055, R-056 |

### Step 3: For each matched rule, evaluate

a. **Does the change modify WHEN?** If so, does the rule now fire in states where it shouldn't, or fail to fire where it should?

b. **Does the change modify THEN?** If so, is the new behavior consistent with the BECAUSE rationale? If the rationale no longer applies, the change may be wrong.

c. **Does the change create new conflicts?** Check PRIORITY and REPLACE annotations.

### Step 4: Check invariants

Scan §5 for invariants that reference the affected rules. Verify each still holds after the change.

### Step 5: Trace through scenarios

Pick the scenario from §4 closest to the affected state. Trace through with the proposed change. Does the output still make sense?

### Step 6: Run tests

```bash
go test ./internal/workflow/... ./internal/knowledge/... ./internal/tools/... -count=1 -v
```

---

## 7. Implementation Target

This specification is designed to be implemented as a Go-defined rule set:

```
internal/workflow/spec/
  dimensions.go         ← Dimension type definitions + valid values
  rules_bootstrap.go    ← Rules R-010 through R-018
  rules_deploy.go       ← Rules R-020 through R-033
  rules_recipe.go       ← Rules R-040 through R-044
  rules_cross.go        ← Rules R-050 through R-062
  rules_routing.go      ← Rules R-070+
  composition.go        ← Resolution algorithm (priority + specificity)
  spec_test.go          ← Completeness: every valid dimension combo has matching rule
                           Conformance: every rule matches actual engine behavior
```

This document serves as the design specification. The Go implementation is the enforcement mechanism. Generated markdown from Go structs replaces this document as the living reference once implemented.

---

## Appendix A: Code Reference Map

| Rule | Primary code location | Function |
|------|----------------------|----------|
| R-001 | `internal/server/instructions.go:43-85` | `BuildInstructions()` |
| R-002 | `internal/server/instructions.go:87-130` | `buildWorkflowHint()` |
| R-003 | `internal/server/instructions.go:142-310` | `buildProjectSummary()`, `classifyServices()` |
| R-004 | `internal/server/server.go:102-106` | Tool registration conditional |
| R-010 | `internal/workflow/guidance.go:85-87` | `assembleKnowledge()` case StepDiscover |
| R-011 | `internal/workflow/guidance.go:89-91` | `assembleKnowledge()` case StepProvision |
| R-012 | `internal/workflow/guidance.go:93-128` | `assembleKnowledge()` case StepGenerate |
| R-013 | `internal/workflow/bootstrap_guidance.go:49-73` | `ResolveProgressiveGuidance()` env checks |
| R-014 | `internal/workflow/bootstrap_guidance.go:52-64` | `ResolveProgressiveGuidance()` mode sections |
| R-015 | `internal/workflow/bootstrap_guidance.go:67-82` | `ResolveProgressiveGuidance()` deploy conditionals |
| R-016 | `internal/workflow/guidance.go:~128` | `assembleKnowledge()` — no case for StepDeploy |
| R-017 | `internal/workflow/bootstrap_guide_assembly.go:63-142` | `BuildTransitionMessage()` |
| R-018 | `internal/workflow/bootstrap_guidance.go:33-41` | Local addendum for non-generate/deploy steps |
| R-020 | `internal/tools/workflow_deploy.go:13-97` | `handleDeployStart()` |
| R-021 | `internal/workflow/deploy.go:120-176` | `BuildDeployTargets()` |
| R-030 | `internal/workflow/deploy_guidance.go:25-72` | `buildPrepareGuide()` |
| R-031 | `internal/workflow/deploy_guidance.go:76-141` | `buildDeployGuide()` |
| R-032 | `internal/workflow/deploy_guidance.go:296-310` + `bootstrap_guidance.go:113-143` | `writeIterationEscalation()`, `BuildIterationDelta()` |
| R-033 | `internal/workflow/deploy_guidance.go:145-155` | `buildVerifyGuide()` |
| R-040 | `internal/workflow/recipe_guidance.go:56-62` | `resolveRecipeGuidance()` research |
| R-041 | `internal/workflow/recipe_guidance.go:106-110` | `assembleRecipeKnowledge()` provision |
| R-042 | `internal/workflow/recipe_guidance.go:112-127` | `assembleRecipeKnowledge()` generate |
| R-043 | `internal/workflow/recipe_guidance.go:16-19` | `buildGuide()` iteration check |
| R-044 | `internal/workflow/recipe_guidance.go:134-136` | `assembleRecipeKnowledge()` finalize |
| R-050 | `internal/knowledge/briefing.go:18-99` | `GetBriefing()` |
| R-051 | `internal/knowledge/briefing.go:105-139, 231-240` | `GetRecipe()`, `prependModeAdaptation()` |
| R-052 | `internal/tools/knowledge.go:26-47` | `resolveKnowledgeMode()` |
| R-053 | `internal/workflow/guidance.go:30-35` | `assembleGuidance()` iteration-first check |
| R-054 | `internal/workflow/bootstrap.go:98-119` | `ResetForIteration()` |
| R-055 | `internal/workflow/deploy.go:240-261` | `ResetForIteration()` |
| R-056 | `internal/workflow/recipe.go:212-231` | `ResetForIteration()` |
| R-057 | `internal/workflow/bootstrap.go:312-327` | `validateSkip()` |
| R-058 | `docs/spec-bootstrap-deploy.md §3.2 Adoption` | Per-target isExisting behavior |
| R-060 | `internal/workflow/bootstrap_outputs.go:65-89` | `writeProvisionMetas()` |
| R-061 | `internal/workflow/bootstrap_outputs.go:13-58` | `writeBootstrapOutputs()` |
| R-062 | `internal/tools/workflow_strategy.go:22-85` | `handleStrategy()` |
| R-070 | `internal/workflow/router.go:28-96` | `Route()` |
