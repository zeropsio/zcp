# ZCP Knowledge Distribution Specification

> **Status**: Authoritative — all knowledge delivery, guidance assembly, and init instructions MUST conform to this document.
> **Scope**: All workflows, both container and local environments, all modes and strategies.
> **Date**: 2026-04-04

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

### 1.3 Information Asymmetry

At any point in a workflow, the agent's knowledge state has three categories:

| Category | Description | Example |
|----------|-------------|---------|
| **CANNOT KNOW** | Zerops-specific or state-specific. Agent cannot derive from training or context. | zerops.yaml schema, env var wiring syntax, deploy pipeline behavior |
| **CAN DERIVE** | Agent can obtain by reading code, calling tools, or reasoning from prior steps. | Current zerops.yaml content, service status via zerops_discover |
| **KNOWS** | From training data or prior workflow steps. | How to write a Node.js server, what env vars were discovered |

### 1.4 The Delivery Rule

Every knowledge delivery decision follows this logic:

```
Agent CANNOT KNOW + NEEDS NOW       → INJECT into response
Agent CANNOT KNOW + MIGHT NEED      → POINT to where it is  
Agent KNOWS or CAN DERIVE           → NOTHING (save context window)
```

INJECT costs context window space (~50-200 lines). Justified only when agent can't proceed without the information. POINT costs a tool call. NOTHING saves context but risks agent missing critical info.

### 1.5 How Asymmetry Shifts

Each completed step adds to what the agent knows.

- **Bootstrap**: HIGH at start (first contact with Zerops) → LOW by deploy step (agent wrote the config, knows the platform).
- **Deploy**: LOW throughout — agent already bootstrapped, has full context from prior session.
- **Recipe**: Varies per step — research is LOW (agent's domain knowledge), generate is HIGH (Zerops config), deploy is LOW again.

---

## 2. System Variables

Seven variables determine what knowledge the system delivers at any point.

### Workflow (D1)

| Value | Steps | Nature |
|-------|-------|--------|
| `bootstrap` | discover → provision → generate → deploy → close | Stateful, 5-step creative workflow |
| `deploy` | start (knowledge) → work → pre-deploy → deploy → verify | Stateful development lifecycle |
| `recipe` | research → provision → generate → deploy → finalize → close | Stateful, 6-step hybrid workflow |
| `cicd` | (immediate) | Stateless — returns guidance, no session |
| `export` | (immediate) | Stateless — returns guidance, no session |

### Mode (D3)

| Value | Topology | zerops.yaml shape |
|-------|----------|-----------------|
| `standard` | `{name}dev` + `{name}stage` + managed | Dev entry: `zsc noop`, no healthCheck. Stage entry: real start, healthCheck. |
| `dev` | `{name}dev` + managed | Dev entry: `zsc noop`, no healthCheck. No stage. |
| `simple` | `{name}` + managed | Single entry: real start, healthCheck. Auto-starts. |
| `managed-only` | managed only (no runtime) | No zerops.yaml needed. Steps generate/deploy/close skipped. |

Set at bootstrap discover step via plan submission. PlanMode() logic: if ANY target uses `standard` → return `standard`. All same mode → return that mode. Multiple non-standard modes → return `mixed`. No targets → return `""`.

### Environment (D4)

| Value | Signal | Code access | Deploy method |
|-------|--------|-------------|---------------|
| `container` | `serviceId` env var present | SSHFS mounts at `/var/www/{hostname}/` | SSH into service, `zcli push` from inside |
| `local` | `serviceId` env var absent | Working directory | `zcli push` from local machine |

Detected at startup by `runtime.Detect()`. Immutable for process lifetime.

### Strategy (D5)

| Value | Interaction model | Description |
|-------|-------------------|-------------|
| `push-dev` | Agent triggers each deploy | SSH self-deploy (container) or zcli push (local) |
| `push-git` | Push to git remote | Commit + push, optional CI/CD (GitHub Actions / webhook) |
| `manual` | User manages directly | Agent informs, user executes |
| (empty) | Not yet chosen | Deploy flow informs agent, resolves before deploying |

Set via `zerops_workflow action="strategy"`. Never auto-assigned. Always read from ServiceMeta at deploy time — never cached. See `spec-workflows.md` §4 for full deploy flow behavior.

### Iteration (D7)

| Value | Guidance behavior |
|-------|-------------------|
| `0` | Normal procedure guidance |
| `1-2` | DIAGNOSE: "Check zerops_logs for the error." Replaces normal guidance. |
| `3-4` | SYSTEMATIC: "Check ALL config systematically." Replaces normal guidance. |
| `5+` | STOP: "Present summary to user, ask for input." Replaces normal guidance. |

Max 10 iterations (configurable via `ZCP_MAX_ITERATIONS`).

### Runtime Class (D6)

| Value | Examples | SSH start needed | healthCheck on dev |
|-------|----------|:---:|:---:|
| `dynamic` | nodejs, go, bun, python, java, rust, dotnet | yes | no |
| `implicit-webserver` | php-nginx, php-apache | no | no |
| `static` | nginx, static | no | no |
| `managed` | postgresql, valkey, mariadb, elasticsearch | N/A | N/A |

### Invalid Combinations

- Strategy `manual` never creates deploy sessions.
- `managed-only` mode skips generate/deploy/close steps.
- Strategy (empty) blocks deploy session creation.
- Mount tool unavailable in local environment.
- Mixed strategies in single deploy session are rejected.

---

## 3. System Initialization

When ZCP starts, before any workflow, three things happen:

### 3.1 Tool Registration

The environment determines which deploy tool is available:
- **Container**: `RegisterDeploySSH` — SSH-based deploy (auto-infers source, runs git init + zcli push inside container). `zerops_mount` available for SSHFS.
- **Local**: `RegisterDeployLocal` — zcli push-based deploy from local machine. `zerops_mount` returns error.

This is a platform fact: the deploy mechanism is fundamentally different between environments because container mode has SSH access to services while local mode does not.

### 3.2 Init Instructions Assembly

`BuildInstructions()` assembles the agent's foundational mental model. This runs once at startup.

**Base instructions** (always): Routing instructions explaining when to use which workflow or tool. Overridable via `ZCP_INSTRUCTION_BASE` env var.

**Environment section** (one of two):
- **Container** (`rt.InContainer == true`): "Control plane container — manages OTHER services, does not serve traffic. Files at `/var/www/{hostname}/` are SSHFS mounts to live services. Commands via `ssh {hostname} '...'`. Edits survive restarts but NOT deploys." If `rt.ServiceName` is set, appends which service ZCP is running on. Overridable via `ZCP_INSTRUCTION_CONTAINER`.
- **Local** (`rt.InContainer == false`): "Local machine — code in working directory. Deploy via `zcli push`. zerops.yaml at repo root. Each deploy = full rebuild + new container." Overridable via `ZCP_INSTRUCTION_LOCAL`.

This is the agent's first contact with platform facts — it cannot know that deploy creates new containers or that SSHFS mounts exist. Without this, the agent would treat the environment like a normal development machine.

**Workflow hint** (conditional): If `.zcp/state/` has active sessions, shows resumable workflow with session ID, type, current step, and intent. This lets the agent continue previous work without rediscovering state.

**Project summary** (conditional): If client + projectID available, classifies all services:
- **Bootstrapped**: ServiceMeta exists with BootstrappedAt set. Shows mount path (container) or status label.
- **Unmanaged**: Runtime without complete meta, not a stage of bootstrapped service. Labeled "needs ZCP adoption."
- **Managed**: Infrastructure services (databases, caches). Labeled by type.
- Includes post-bootstrap orientation for bootstrapped services and workflow offerings from `Route()`.

The classification is state the agent cannot derive without reading ServiceMeta files — the init instructions do this work upfront.

---

## 4. Bootstrap Flow

Bootstrap is a **creative workflow** — the agent builds from scratch on an unfamiliar platform. Information asymmetry starts HIGH and decreases with each step. Knowledge is INJECTED because the agent needs it to create correct configuration and doesn't know what to ask for.

### 4.1 Lifecycle

Five steps in strict order: discover → provision → generate → deploy → close.

Each step has status (pending → in_progress → complete | skipped), requires attestation (min 10 chars) on completion, and may have a checker that validates before allowing completion. Steps progress strictly — no skipping ahead. If checker fails, step stays active, agent receives failure details to fix.

Session exclusivity is per-service, not global: multiple bootstraps can run for different services. Same-hostname lock enforced at discover step — if incomplete ServiceMeta exists from alive session, bootstrap is blocked for that hostname.

### 4.2 Discover Step

**What the agent needs**: Understanding of Zerops platform model — what modes exist, what service types are available, how to structure a plan.

**What the system delivers**:
- INJECT: `GetModel()` — full platform model from `themes/model.md`. This is the agent's first introduction to Zerops concepts (modes, service types, topology). Agent CANNOT KNOW any of this.
- Static guidance: `discover` section from `bootstrap.md` describing the classification and plan submission procedure.

**Why inject the platform model**: The agent has never seen Zerops. It doesn't know what "standard mode" means, what dev/stage pairing is, or what service types exist. Without this, it would guess — and guesses lead to wrong mode choices and missing infrastructure.

**Environment variation**: If local, appends `discover-local` addendum after the base section. This adds local-specific notes but doesn't replace the base procedure — discovery works the same way regardless of environment.

### 4.3 Provision Step

**What the agent needs**: The import.yaml schema to create services on Zerops.

**What the system delivers**:
- INJECT: H2 section "import.yaml Schema" extracted from `zerops://themes/core`. The import format is entirely Zerops-specific — agent cannot guess field names (`startWithoutCode`, `enableSubdomainAccess`, etc.).
- Static guidance: `provision` section from `bootstrap.md`.

**Why inject schema here and not earlier**: At discover, the agent didn't need it yet (was planning, not creating). At provision, it needs to write import.yaml immediately. Injecting at the point of need saves context window during discover.

**Environment variation**: If local, appends `provision-local` addendum. Mentions .env credential bridge, VPN setup for service access. These are addenda, not replacements — the import procedure is the same.

### 4.4 Generate Step — the most complex knowledge delivery point

This is where the agent writes zerops.yaml and an infrastructure verification server for the first time on Zerops. Asymmetry is at its peak for configuration knowledge.

**What the agent needs**: Everything about how zerops.yaml works on Zerops, runtime-specific patterns, env var wiring, and mode-specific rules.

**What the system delivers** (all injected):
1. **Runtime briefing** via `GetBriefing(runtimeBase, dependencyTypes, mode, liveTypes)` — a 7-layer composition:
   - Live service stacks (what's available)
   - Runtime guide (nodejs/go/php/etc. specifics for Zerops)
   - Matching recipe hints (available framework patterns)
   - Service cards (PostgreSQL/Valkey/etc. configuration)
   - Wiring syntax (`${hostname_varName}` patterns)
   - Decision hints (choose-database, choose-cache summaries)
   - Version check (✓/⚠ for active versions)

2. **zerops.yaml schema** — H2 section "zerops.yaml Schema" from core reference.

3. **Discovered env var names** — formatted as `${hostname_varName}` reference list from session state. Names only, never values.

**Why inject all three**: Agent writes zerops.yaml FROM SCRATCH. It cannot know: that `deployFiles` is the only thing surviving deploy, that `${hostname_varName}` typos become silent literal strings, that 0.0.0.0 binding is required, that build container ≠ run container. It also cannot know the curated path: `zsc noop` for dev start, no healthCheck on dev, stage entry deferred until after dev verification. Without this injection, the agent would write generic config that works on other platforms but fails silently on Zerops.

**Why env vars are injected HERE but NOT at deploy step**: At generate, the agent is writing env var references into zerops.yaml for the first time. It needs the list. At deploy, it already wrote them — it KNOWS them from the generate step it just completed.

#### 4.4.1 Environment routing at generate (REPLACEMENT, not addendum)

This is a critical design decision: **local environment completely replaces container guidance for generate and deploy steps**.

**Container** (`env == container`):
- Base `generate` section + mode-specific addenda (see §4.4.2)
- Describes: write files to SSHFS mount `/var/www/{hostname}/`, SSH-based dependency installation, remote file paths

**Local** (`env == local`):
- `generate-local` section **replaces everything** — base section and all mode addenda are NOT used.
- Describes: write files locally in working directory, .env credential bridge for local development, VPN setup for service access, real start command (not zsc noop), healthCheck present, deploy via zcli push.

**Why full replacement instead of addenda**: The local workflow is fundamentally different — no SSH, no SSHFS, code in working directory, different start command semantics. Mixing container instructions ("SSH into service") with local context would create confusing contradictions. A self-contained local section is clearer than a patched version of container guidance.

**Priority**: Environment routing is checked FIRST. If local, mode addenda (§4.4.2) do not apply — the generate-local section covers all modes in its own way.

#### 4.4.2 Mode routing at generate (container only, ADDITIVE)

When environment is container, mode-specific sections are **appended** to the base `generate` section:

**Standard mode** — append `generate-standard`:
- Dev entry uses `zsc noop --silent` as start command. WHY: agent needs manual control over server lifecycle for debugging. With real start, every deploy auto-starts the server and the agent can't intercept startup errors or iterate on code without redeploying.
- No `healthCheck` on dev entry. WHY: `zsc noop` exits immediately. If healthCheck were present, Zerops would detect the "failed" process and restart the container in a loop.
- `deployFiles: [.]` always. WHY: dev iterates on source code, not build output. Everything must be available in the container.
- Stage entry NOT written yet. WHY: stage entry comes after dev is verified. Writing both upfront risks deploying untested config to stage.

**Dev mode** — append `generate-dev`:
- Same rules as standard for the dev entry. No stage service exists.

**Simple mode** — append `generate-simple`:
- Real start command (not `zsc noop`). WHY: simple mode has no manual iteration cycle — service auto-starts.
- `healthCheck` required. WHY: Zerops monitors the process and restarts on failure.
- `deployFiles: [.]`. WHY: same reasoning as dev — single service iterates on source.

**Mixed plans**: If the plan has targets with different modes, multiple mode sections are appended simultaneously. Each target follows its own mode's rules.

### 4.5 Deploy Step

**What the agent needs**: Just the deployment procedure. It already wrote the code and config in the previous step.

**What the system delivers**: Static guidance ONLY. No `GetBriefing()`, no schema injection, no env var injection.

**Why NO knowledge injection**: The agent just completed the generate step — it wrote zerops.yaml, it wrote the verification server, it used the env var references. It KNOWS all of this. Re-injecting would waste ~100 lines of context on information the agent created moments ago. The deploy procedure (SSH push, subdomain enable, verify) is entirely in the static guidance.

**Contrast with generate step**: Generate injects because the agent hasn't seen Zerops config before. Deploy doesn't inject because the agent just wrote it.

#### 4.5.1 Environment routing at deploy

Same replacement pattern as generate:
- **Container**: base `deploy` section from `bootstrap.md`.
- **Local**: `deploy-local` section replaces everything. Describes zcli push workflow, no SSH.

#### 4.5.2 Conditional sections (container only)

Two conditions can append extra sections:
- **3+ services** (`len(plan.Targets) >= 3`): appends `deploy-agents` section describing multi-service orchestration — parent agent spawns sub-agents per service pair. WHY: sequential deployment of 3+ services is slow and error-prone. Parallel sub-agents with a parent coordinator is the curated path.
- **Failure recovery** (`failureCount > 0`): appends `deploy-recovery` section. Only on retry, not first attempt — avoids noise when things work.

### 4.6 Close Step

When the close step completes, `Active` becomes `false`, triggering:

1. **ServiceMeta writes**: Full meta for each runtime target with `BootstrappedAt` timestamp (signals completion). Strategy stays empty — never auto-assigned.
   - Container: meta hostname = devHostname, StageHostname = stageHostname.
   - Local + standard: meta hostname = stageHostname, StageHostname = "" (inverted).

2. **Transition message** via `BuildTransitionMessage()`:
   - Lists all bootstrapped services with modes and stage hostnames.
   - Presents strategy selection: push-dev, push-git, manual — all three equally, with explanations.
   - Provides command: `zerops_workflow action="strategy" strategies={"hostname":"push-dev"}`.
   - Next-step hints per strategy choice.

3. **Reflog** appended to CLAUDE.md (history record).

**Why strategy is never auto-assigned**: Strategy is a project/team decision (how does this team deploy? CI/CD? Manual?). The wrong default would establish an unwanted deployment pattern. Agent/user must decide explicitly.

### 4.7 Iteration Behavior

When agent calls `action="iterate"` during bootstrap:

**What resets**: Steps `generate` and `deploy` go back to `pending`. CurrentStep moves to generate.
**What's preserved**: `discover`, `provision`, `close` stay as-is. Plan, DiscoveredEnvVars, ServiceMeta all preserved.

**Why only generate+deploy**: Infrastructure exists (provision done), services discovered (discover done). The problem is in code or config, so only the code-writing and deployment steps need retry.

**Guidance replacement**: When iteration > 0 and step is `deploy`, `BuildIterationDelta()` fires FIRST and replaces ALL normal guidance:
- **Iteration 1-2** (DIAGNOSE): "Check zerops_logs severity=error. Build failed? → build log. Container didn't start? → start command, runtime version, env vars, port binding."
- **Iteration 3-4** (SYSTEMATIC): "PREVIOUS FIXES FAILED. Systematic check: zerops.yaml (ports, start, deployFiles), env var references, runtime version, 0.0.0.0 binding."
- **Iteration 5+** (STOP): "STOP. Multiple fixes failed. Present diagnostic summary to user: exact error, current config, env var values, what you've tried. User decides."

Format: `"ITERATION {N} (session remaining: {max-N})\n\nPREVIOUS: {lastAttestation}\n\n{tier guidance}"`

**Why replace instead of append**: Normal guidance was already read — agent KNOWS it. Re-injecting "deploy to dev, then stage" after three failed attempts is noise. Replacement signals "the problem isn't the procedure, it's something specific — diagnose."

**Why escalating tiers**: Each tier represents increasing scope of investigation. Tier 1 is targeted ("find THE error"). Tier 3 is systematic ("question ALL assumptions"). Tier 5 recognizes that after 4+ attempts, the problem is likely outside agent capability — user intervention is the efficient path.

### 4.8 Managed-Only Fast Path

When the plan has zero runtime targets (only databases, caches, storage):

- `discover`: normal (plan submitted with empty targets array)
- `provision`: normal (import managed services, discover env vars)
- `generate`: SKIPPED — nothing to generate (no zerops.yaml needed)
- `deploy`: SKIPPED — nothing to deploy
- `close`: SKIPPED — no ServiceMeta written (managed services are API-authoritative)

Skip validation (`validateSkip()`): `discover` and `provision` are ALWAYS mandatory — even managed-only needs infrastructure created and env vars discovered. `generate`, `deploy`, `close` are skippable ONLY when `plan == nil || len(plan.Targets) == 0`.

### 4.9 Adoption of Existing Services

When the project has pre-existing runtime services not managed by ZCP (`managedByZCP=false`), the agent can adopt them into ZCP management.

Adoption is **per-target**, not global. Each target has `isExisting: true` (adopt) or `false` (create new). Mixed plans are fully supported.

**Per-target behavior differences for `isExisting=true`**:
- **Provision**: Skip import for this target (service already exists). Still mount filesystem and discover env vars.
- **Generate**: Skip zerops.yaml and code generation for this target. Checker skips validation for adopted targets.
- **Deploy**: Verify-only (`zerops_verify`, no `zerops_deploy`). Service is already running — just confirm health.
- **Close**: ServiceMeta written identically for adopted and new targets. Both get `BootstrappedAt`.

The `isExisting` flag is immutable after plan submission.

**Why adoption doesn't redeploy**: Existing services are running with user's code. Redeploying would destroy their current state. Adoption means "register in ZCP" (write ServiceMeta), not "rebuild from scratch."

---

## 5. Deploy Flow

> **Note**: Implementation uses internal step names "prepare", "execute", "verify". These map to the conceptual phases described in spec-workflows.md §4: Start → Work → Pre-deploy → Deploy → Verify.

Deploy is an **operational workflow** — services already exist, config probably exists, the agent has context from a prior bootstrap or from reading the codebase. Information asymmetry is LOW. Knowledge is POINTED TO (not injected) because the agent knows what it needs and can request on demand.

### 5.1 Strategy Gate

Before a deploy session can start, `handleDeployStart()` runs a sequential gate:

1. **Read ServiceMetas**: Load all from `.zcp/state/services/`. Filter to complete (`BootstrappedAt` set) and runtime (has Mode or StageHostname). If none found → error.

2. **Strategy check**: If ANY meta has empty `DeployStrategy` → return strategy selection guidance. This is conversational (not an error) — presents push-dev, push-git, manual with explanations. Agent must call `action="strategy"` first. WHY: strategy determines the entire interaction model. Without it, the system doesn't know whether to guide SSH workflow or git push setup.

3. **All-manual check**: If ALL metas have strategy `manual` → return manual deploy response with per-service `zerops_deploy` commands. No session created. WHY: manual strategy means user manages deployments — adding a session with prepare/execute/verify adds overhead for someone who explicitly said "I'll handle it."

4. **Mixed strategy check**: If targets have different strategies → error: "Mixed strategies not supported." WHY: push-dev guidance says "SSH into dev" while push-git says "push to git remote." Can't interleave in one session.

5. **Session creation**: Build targets via `BuildDeployTargets(metas)`, create session.

### 5.2 Target Construction

`BuildDeployTargets()` creates ordered targets from ServiceMetas:

- Each meta produces a DeployTarget with hostname and role.
- Role assignment: `simple` mode → role `simple`. `dev` mode → role `dev`. `standard` mode → role `dev`, PLUS a stage target with role `stage` appended after dev.
- Dev targets always ordered before their stage targets.
- Mode and strategy detected from first meta.

**Why dev before stage**: Standard mode deploys dev first for validation. Stage receives code from dev. Dev must be verified healthy before promoting to stage. This ordering enforces the curated path.

### 5.3 Prepare Step

**What the agent needs**: Compact reminder of setup + pointers to knowledge it might need.

**What the system delivers** (injected, but compact — max ~55 lines):
- **Setup summary**: Target hostnames, mode, strategy — personalized to THIS agent's services.
- **Mode-specific checklist**:
  - Standard or dev: "Dev entry: `zsc noop --silent`, NO healthCheck"
  - Simple: "Entry must have real `start:` command and `healthCheck`"
  - Standard only: "Stage entry: real `start:` + healthCheck required"
- **Platform facts reminder** (compact, ~5 lines): deploy = new container, deployFiles only survivor, env var typo = silent.
- **Environment-specific**: If container, shows SSHFS mount paths for non-stage targets.
- **Strategy note**: Current strategy + brief mention of alternatives + change command.
- **Knowledge MAP** (pointers, not content):
  - Per runtime: `zerops_knowledge query="{runtimeBase}"` (deduplicated, skips stage targets)
  - General: `zerops_knowledge query="zerops.yaml"`, `zerops_discover`

**Why pointers instead of injection**: Asymmetry is LOW. Agent bootstrapped these services — it has seen the briefing, written the zerops.yaml, deployed the code. Injecting ~100 lines of runtime briefing would waste context on information the agent already has. Pointers let it fetch ONLY what it needs (maybe nothing, maybe one specific thing).

**Contrast with bootstrap generate**: Bootstrap injects full briefing because it's the agent's first time — it doesn't know what to ask for. Deploy points because the agent knows what it needs.

### 5.4 Execute Step

#### Normal execution (iteration 0)

**Mode-specific workflow guidance** — the core of deploy guidance:

**Local environment** (any mode): `writeLocalWorkflow()` — per-target `zcli push` commands + verification. No SSH, no SSHFS references.

**Container environment**:
- **Standard mode** (`writeStandardWorkflow()`):
  1. Deploy to dev: `zerops_deploy targetService={devHostname}`
  2. Start server manually via SSH (dev uses `zsc noop`)
  3. Verify dev: `zerops_verify serviceHostname={devHostname}`
  4. Deploy to stage: `zerops_deploy sourceService={dev} targetService={stage}`
  5. Stage auto-starts (real start + healthCheck)
  6. Verify stage: `zerops_verify serviceHostname={stage}`

- **Dev mode** (`writeDevWorkflow()`):
  1. Deploy to dev
  2. Start server manually
  3. Verify dev

- **Simple mode** (`writeSimpleWorkflow()`):
  1. Deploy: `zerops_deploy targetService={hostname}` (auto-starts)
  2. Verify

**Key facts** (environment-specific):
- Container with dev role: "Dev uses `zsc noop` — start server manually via SSH after every deploy. Exception: implicit-webserver runtimes (php-nginx, php-apache, nginx, static) auto-start."
- Container with stage role: "Stage auto-starts with real `start:` + healthCheck. Zerops monitors and restarts on failure."
- Local: "Deploy via `zcli push`, each deploy = full rebuild + new container."

**Code-only changes guidance**:
- Container: "Edit on SSHFS mount, restart via SSH. Redeploy ONLY if zerops.yaml changed."
- Local: "Edit locally, hot reload if supported. Redeploy to persist on Zerops."

#### Iteration execution (iteration ≥ 1)

When iteration > 0, `writeIterationEscalation()` fires FIRST and **replaces** all workflow guidance above. The agent does NOT receive mode-specific workflows — it receives diagnostic guidance instead.

Tier logic is the same as bootstrap (§4.7): DIAGNOSE at 1-2, SYSTEMATIC at 3-4, STOP at 5+.

**Why same escalation as bootstrap**: The diagnostic process is identical regardless of whether the deploy happens within bootstrap or as a standalone workflow. The problem is always: something in the code, config, or platform is wrong.

### 5.5 Verify Step

**What the system delivers**: Extract `deploy-verify` section from `deploy.md`. Fallback: "Run zerops_verify for each target. Check health status."

No mode/environment branching. No knowledge injection. Verification is the same process regardless of dimensions. The checker is nil — this is an informational step.

### 5.6 Iteration Behavior

When agent calls `action="iterate"` during deploy:

**What resets**: `execute` and `verify` go back to `pending`. All targets reset to `pending` status.
**What's preserved**: `prepare` stays complete.

**Why only execute+verify**: Preparation (zerops.yaml validation) passed. The problem is in deployment or code, not in config validation.

---

## 6. Recipe Flow

Recipe is a **hybrid workflow** — some steps are creative (like bootstrap), some are operational (like deploy). Knowledge injection follows the asymmetry: HIGH where agent writes Zerops config, LOW where agent does framework research or deployment.

### 6.1 Research Step

**What the system delivers**: Static guidance ONLY. No knowledge provider calls.
- If tier is `showcase`: `research-showcase` section. If `minimal`: `research-minimal` section.

**Why no knowledge injection**: Agent researches the framework from its own training data. Injecting Zerops knowledge would bias the research toward existing recipe patterns rather than independently discovering what the framework needs.

### 6.2 Provision Step

**What the system delivers**: Inject H2 section "import.yaml Schema" from core reference. Same as bootstrap provision (§4.3) — agent needs the Zerops-specific format to create test infrastructure.

### 6.3 Generate Step

**What the system delivers**: Same pattern as bootstrap generate (§4.4) but with one key difference:
- If `plan.RuntimeType != ""`: inject `GetBriefing(runtimeBase, nil, "", nil)` — note: **mode is NOT passed**.
- If discovered env vars exist: inject formatted references.
- Inject zerops.yaml schema.
- Static guidance: `generate` + `generate-fragments` sections from `recipe.md`.

**Why mode is NOT passed to GetBriefing**: Recipes must cover ALL modes — the agent writes base, prod, AND dev setup blocks. Passing a specific mode would filter the briefing to show only one mode's patterns, but the recipe needs to contain patterns for all modes.

**Contrast with bootstrap generate**: Bootstrap passes mode to GetBriefing because the agent writes for ONE specific mode. Recipe doesn't pass mode because the agent writes for ALL modes.

### 6.4 Deploy Step

**What the system delivers**: Static guidance only (same reasoning as bootstrap deploy §4.5). If iteration ≥ 1, replaces with escalation tiers via `buildRecipeIterationDelta()`.

### 6.5 Finalize Step

**What the system delivers**: Inject H2 section "import.yaml Schema" from core reference. Static guidance: `finalize` section from `recipe.md`.

**Why import schema again**: Agent generates 6 environment-specific import.yaml files (minimal, balanced, ha, ha-benchmark, dev, showcase). Needs the schema reference to validate correct format across all environments.

### 6.6 Close Step

Static guidance only. Only skippable step in recipe workflow.

### 6.7 Iteration Behavior

**What resets**: `generate`, `deploy`, `finalize` go back to `pending`. Preserved: `research`, `provision`, `close`. Same reasoning as bootstrap — research and provisioning are done, the problem is in code/config.

---

## 7. Knowledge Engine

The knowledge engine is the subsystem that assembles and delivers platform knowledge on demand. It serves both workflow guidance injection (§4-6) and direct `zerops_knowledge` tool calls.

### 7.1 Briefing Assembly — 7-Layer Composition

`GetBriefing(runtime, services, mode, liveTypes)` composes knowledge from bottom up. Each layer is optional — missing layers are silently skipped.

**Layer order** (sequential, each appended if non-empty):

1. **Live service stacks**: `FormatServiceStacks(liveTypes)` — shows available service types from live API. Marks build-capable runtimes with [B].

2. **Runtime guide**: If `runtime != ""` → normalize to base slug (e.g., `nodejs@22` → `nodejs`) → fetch from `recipes/{slug}-hello-world` or `bases/{slug}`. Contains: Zerops-specific knowledge valid for ANY app of this runtime — binding rules, deploy patterns, common mistakes.

3. **Matching recipes hint**: If runtime base found → list available framework recipes for this runtime (e.g., nodejs → nestjs, nextjs, express). Agent can fetch specific recipe via `zerops_knowledge recipe="{name}"`.

4. **Service cards**: If services provided → for each: normalize name (e.g., `postgresql@16` → `PostgreSQL`) → extract H2 section from `themes/services.md`. Contains: service-specific configuration, default env vars, connection patterns.

5. **Wiring syntax**: If services provided → extract "Wiring Syntax" section from `services.md`. Shows `${hostname_varName}` cross-service reference syntax.

6. **Decision hints**: Map services to decision categories (postgresql → "Choose Database", valkey → "Choose Cache", etc.) → extract first-paragraph summaries from `decisions/*.md`.

7. **Version check**: `FormatVersionCheck(runtime, services, liveTypes)` — checkmarks (✓) for active versions, warnings (⚠) for deprecated/unknown with suggestions.

**Auto-promotion**: If `runtime` is empty but services list contains a known runtime name (e.g., `python@3.12`), the first match is promoted to runtime parameter and removed from services. This prevents duplicate processing.

**Best-effort principle**: No layer failure stops the composition. Missing runtime guide → skip layer 2. No managed services → skip layers 4-6. Empty liveTypes → skip layers 1 and 7. The result is always a valid (possibly sparse) briefing.

### 7.2 Mode Adaptation for Recipes

When `GetRecipe(name, mode)` is called with non-empty mode:

- `mode = "dev"` or `"standard"`: prepend `"> **Mode: dev** — Use the \`dev\` setup block from the zerops.yaml below.\n\n"`
- `mode = "simple"`: prepend `"> **Mode: simple** — Use the \`prod\` setup block below, but override \`deployFiles: [.]\`.\n\n"`

Universals (platform constraints) are always prepended before recipe content.

**Why a one-line directive**: Recipes contain multiple setup blocks (base, prod, dev). The agent needs to know which block to use. A short directive saves agent time vs reading the full recipe to figure out which block applies.

### 7.3 Session-Aware Knowledge Filtering

When agent calls `zerops_knowledge` during any workflow, `resolveKnowledgeMode()` auto-detects the current mode:

1. Explicit `inputMode` parameter → used immediately (highest priority). Agent can always override.
2. Active bootstrap session → `PlanMode()` (returns standard/dev/simple/mixed/"").
3. Active deploy session → `Deploy.Mode` field.
4. No active session → `""` (unfiltered — agent gets complete content).

**Why auto-filter**: Agent in a standard mode bootstrap doesn't need simple mode patterns — they'd be confusing noise. Filtering by session mode provides relevant knowledge without agent having to specify mode on every call.

**Why overridable**: Edge cases exist — agent might want to peek at stage patterns during dev workflow. Explicit `mode` parameter always wins.

---

## 8. ServiceMeta Lifecycle

ServiceMeta files (`.zcp/state/services/{hostname}.json`) are the persistent bridge between bootstrap and deploy workflows. They record bootstrap decisions for use in future sessions.

### 8.1 Partial Meta After Provision

After provision step completes, `writeProvisionMetas()` writes a partial meta for each runtime target:
- Fields set: Hostname, Mode, StageHostname, Environment, BootstrapSession.
- Fields NOT set: BootstrappedAt (empty), DeployStrategy (empty).
- `IsComplete()` returns `false` — signals bootstrap in-progress.

**Purpose**: Hostname lock. Other sessions check for incomplete metas to prevent concurrent bootstrap of the same service. If the owning session's PID is alive, bootstrap is blocked. If PID is dead (orphaned meta), the lock auto-releases.

### 8.2 Full Meta After Bootstrap Completion

When bootstrap completes (Active→false), `writeBootstrapOutputs()` overwrites with full meta:
- `BootstrappedAt` = today's date → `IsComplete()` returns `true`.
- `DeployStrategy` stays empty — set separately by `action="strategy"`.

**Environment-specific hostname**:
- Container: meta hostname = devHostname, StageHostname = stageHostname.
- Local + standard mode: meta hostname = stageHostname, StageHostname = "" (inverted). WHY: in local mode, stageHostname is the primary deployment target.

### 8.3 Strategy Update

`action="strategy"` updates `ServiceMeta.DeployStrategy` for specified hostnames. Returns next-step hint:
- All `manual` → "When code is ready: `zerops_deploy` directly."
- All `push-git` → "Set up CI/CD: `zerops_workflow action=\"start\" workflow=\"cicd\"`"
- Otherwise → "When code is ready: `zerops_workflow action=\"start\" workflow=\"deploy\"`"

---

## 9. Workflow Routing

`zerops_workflow action="route"` evaluates the project's current state and returns prioritized offerings. The agent uses this to understand what actions are available.

`Route()` considers: existing ServiceMetas, active sessions (from registry), live services (from API), and unmanaged runtimes (services without complete meta).

**Priority ordering**:
1. (P1) Incomplete bootstrap → resume hint or start hint
2. (P1) Unmanaged runtimes → bootstrap adoption offering
3. (P1-P2) Bootstrapped services with strategy set → deploy or cicd offering based on strategy
4. (P3) Add new services → bootstrap start hint
5. (P4-P5) Utilities → recipe, scale

Manual strategy produces no deploy/cicd offering — user manages directly.

Route returns **facts, not recommendations**. It says "these workflows are available" — the agent decides based on user intent.

---

## 10. Invariants

These properties must always hold. Each can be verified by reading the code at the referenced location.

| ID | Invariant | Code reference |
|----|-----------|---------------|
| KD-01 | Bootstrap discover/provision/generate inject knowledge; deploy does NOT | `guidance.go:assembleKnowledge()` — no case for StepDeploy |
| KD-02 | Deploy workflow delivers knowledge as pointers, never full injection | `deploy_guidance.go:buildPrepareGuide()` contains `zerops_knowledge query=` strings |
| KD-03 | Iteration ≥ 1 replaces normal guidance, never appends | `guidance.go:assembleGuidance()` — iteration check is FIRST, returns immediately if non-empty |
| KD-04 | Strategy is never auto-assigned | `bootstrap_outputs.go` — DeployStrategy stays empty; only `handleStrategy()` writes it |
| KD-05 | Local environment replaces (not extends) container guidance for generate/deploy | `bootstrap_guidance.go` — env check fires BEFORE mode addenda |
| KD-06 | Mode addenda are additive in container mode (multiple sections for mixed plans) | `bootstrap_guidance.go` — each mode flag checked independently |
| KD-07 | Environment routing fires before mode routing | `bootstrap_guidance.go` — local branch returns before mode section loop |
| KD-08 | discover/provision are never skippable | `bootstrap.go:validateSkip()` — returns error for these step names |
| KD-09 | generate/deploy/close skippable only for managed-only (no targets) | `bootstrap.go:validateSkip()` — checks `len(plan.Targets) == 0` |
| KD-10 | Session-aware mode filtering applies automatically | `knowledge.go:resolveKnowledgeMode()` — checks bootstrap then deploy state |
| KD-11 | Recipe GetBriefing does NOT pass mode; bootstrap does | `recipe_guidance.go:assembleRecipeKnowledge()` passes `""` vs `guidance.go` passes `params.Mode` |
| KD-12 | Adoption modifies per-target behavior, not step progression | `spec-bootstrap-deploy.md §3.2` — isExisting affects within-step logic |
| KD-13 | Iteration resets preserve earlier steps | `bootstrap.go`, `deploy.go`, `recipe.go` — ResetForIteration only resets specific step indices |
| KD-14 | Briefing layers are best-effort (missing = skip, no error) | `briefing.go:GetBriefing()` — each layer wrapped in non-empty check |
| KD-15 | Env vars injected at generate step only, not deploy | `guidance.go` — `if step != StepDeploy` gates env var inclusion |
| KD-16 | Deploy prepare guidance max ~55 lines | `deploy_guidance_test.go` — line count assertion |
| KD-17 | Mixed strategies in single deploy session produce error | `workflow_deploy.go:handleDeployStart()` — compares all target strategies |
| KD-18 | Partial ServiceMeta (no BootstrappedAt) signals incomplete bootstrap | `service_meta.go:IsComplete()` — checks BootstrappedAt non-empty |
| KD-19 | All init instructions delivered once at startup, not per tool call | `server.go` — BuildInstructions called during server setup |

---

## Appendix: Code Reference Map

| Section | Primary code location | Function |
|---------|----------------------|----------|
| §3 Init instructions | `server/instructions.go:43-85` | `BuildInstructions()` |
| §3 Workflow hint | `server/instructions.go:87-130` | `buildWorkflowHint()` |
| §3 Project summary | `server/instructions.go:142-310` | `buildProjectSummary()` |
| §3 Tool registration | `server/server.go:102-106` | Conditional tool registration |
| §4.2 Discover knowledge | `workflow/guidance.go:85-87` | `assembleKnowledge()` StepDiscover |
| §4.3 Provision knowledge | `workflow/guidance.go:89-91` | `assembleKnowledge()` StepProvision |
| §4.4 Generate knowledge | `workflow/guidance.go:93-128` | `assembleKnowledge()` StepGenerate |
| §4.4.1 Env routing | `workflow/bootstrap_guidance.go:49-73` | `ResolveProgressiveGuidance()` |
| §4.4.2 Mode routing | `workflow/bootstrap_guidance.go:52-64` | Mode section appending |
| §4.5 Deploy no-injection | `workflow/guidance.go:~128` | No case for StepDeploy |
| §4.5.2 Conditional sections | `workflow/bootstrap_guidance.go:67-82` | Deploy agents + recovery |
| §4.6 Close / transition | `workflow/bootstrap_guide_assembly.go:63-142` | `BuildTransitionMessage()` |
| §4.7 Iteration escalation | `workflow/bootstrap_guidance.go:113-143` | `BuildIterationDelta()` |
| §4.8 Skip validation | `workflow/bootstrap.go:312-327` | `validateSkip()` |
| §4.9 Iteration reset | `workflow/bootstrap.go:98-119` | `ResetForIteration()` |
| §5.1 Strategy gate | `tools/workflow_deploy.go:13-97` | `handleDeployStart()` |
| §5.2 Target construction | `workflow/deploy.go:120-176` | `BuildDeployTargets()` |
| §5.3 Prepare guidance | `workflow/deploy_guidance.go:25-72` | `buildPrepareGuide()` |
| §5.4 Execute guidance | `workflow/deploy_guidance.go:76-141` | `buildDeployGuide()` |
| §5.4 Iteration escalation | `workflow/deploy_guidance.go:296-310` | `writeIterationEscalation()` |
| §5.5 Verify guidance | `workflow/deploy_guidance.go:145-155` | `buildVerifyGuide()` |
| §5.6 Deploy iteration reset | `workflow/deploy.go:240-261` | `ResetForIteration()` |
| §6.1 Recipe research | `workflow/recipe_guidance.go:56-62` | `resolveRecipeGuidance()` |
| §6.3 Recipe generate | `workflow/recipe_guidance.go:112-127` | `assembleRecipeKnowledge()` |
| §6.5 Recipe finalize | `workflow/recipe_guidance.go:134-136` | `assembleRecipeKnowledge()` |
| §6.7 Recipe iteration reset | `workflow/recipe.go:212-231` | `ResetForIteration()` |
| §7.1 Briefing assembly | `knowledge/briefing.go:18-99` | `GetBriefing()` |
| §7.2 Mode adaptation | `knowledge/briefing.go:105-139, 231-240` | `GetRecipe()`, `prependModeAdaptation()` |
| §7.3 Session-aware filter | `tools/knowledge.go:26-47` | `resolveKnowledgeMode()` |
| §8.1 Partial meta | `workflow/bootstrap_outputs.go:65-89` | `writeProvisionMetas()` |
| §8.2 Full meta | `workflow/bootstrap_outputs.go:13-58` | `writeBootstrapOutputs()` |
| §8.3 Strategy update | `tools/workflow_strategy.go:22-85` | `handleStrategy()` |
| §9 Routing | `workflow/router.go:28-96` | `Route()` |
