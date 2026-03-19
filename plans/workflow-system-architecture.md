# ZCP Workflow System — Complete Flow Architecture

How the entire workflow system operates from first contact to ongoing development. Container environment only (local flow planned for Wave 4-5).

---

## 1. Project Lifecycle

```mermaid
flowchart TD
    USER([User arrives with intent]) --> ROUTE{Router:<br/>project state + metas<br/>+ intent keywords}

    ROUTE -- "FRESH / no services" --> BOOT
    ROUTE -- "CONFORMANT / has metas" --> DEPLOY
    ROUTE -- "'it's broken'" --> DEBUG
    ROUTE -- "'too slow'" --> SCALE
    ROUTE -- "'change config'" --> CONFIG
    ROUTE -- "'set up CI/CD'" --> CICD

    subgraph BOOT["BOOTSTRAP (stateful, 6 steps)"]
        B1[discover] --> B2[provision] --> B3[generate]
        B3 --> B4[deploy] --> B5[verify] --> B6[strategy]
    end

    B6 --> METAS[(Service metas written:<br/>hostname, type, mode,<br/>stageHostname, dependencies,<br/>strategy)]

    METAS --> LOOP

    subgraph LOOP["DEVELOPMENT LOOP"]
        direction TB
        L1["User: 'add dashboard'<br/>'fix the login'<br/>'update the API'"]
        L1 --> DEPLOY
    end

    subgraph DEPLOY["DEPLOY (stateful, 3 steps)"]
        DP1["prepare: load context + knowledge,<br/>write/modify code"]
        DP1 --> DP2["deploy: mode-aware push<br/>standard: dev→stage<br/>dev: dev only<br/>simple: direct"]
        DP2 --> DP3["verify: health check + iterate"]
    end

    DEPLOY --> |done| LOOP

    subgraph DEBUG["DEBUG (stateless)"]
        DB1["Service context injected<br/>+ diagnostic guidance"]
        DB1 --> DB2["Agent diagnoses: logs, events, verify"]
        DB2 --> DB3{"Fix requires code?"}
        DB3 -- Yes --> DEPLOY
        DB3 -- No --> DB4["Config/scale fix"]
    end

    subgraph SCALE["SCALE (stateless)"]
        SC1["Service context injected<br/>+ scaling guidance"]
    end

    subgraph CONFIG["CONFIGURE (stateless)"]
        CF1["Service context injected<br/>+ config guidance"]
    end

    subgraph CICD["CI/CD SETUP (stateful, 3 steps)"]
        CI1[choose provider] --> CI2[configure] --> CI3[verify]
    end
```

**Key principle:** Bootstrap runs once. After that, deploy is the primary loop. Debug/scale/configure are side operations that may feed back into deploy.

---

## 2. Router — How Workflows Are Selected

```mermaid
flowchart TD
    INPUT["RouterInput:<br/>ProjectState + ServiceMetas<br/>+ ActiveSessions + Intent"] --> STATE{Project State}

    STATE -- FRESH --> FRESH["bootstrap (p1)"]
    STATE -- CONFORMANT --> CONF
    STATE -- NON_CONFORMANT --> NONCONF
    STATE -- UNKNOWN --> ALL["all workflows (p3)"]

    CONF --> STRAT{Strategy from metas}
    STRAT -- "push-dev" --> PDEP["deploy (p1)"]
    STRAT -- "ci-cd" --> PCI["cicd (p1) + deploy (p2)"]
    STRAT -- "manual" --> PMAN["manual-deploy (p1)"]
    STRAT -- "none" --> PDEP2["deploy (p1)"]

    NONCONF --> NONC2["bootstrap (p1) + strategy offerings (p2)"]

    FRESH --> UTIL
    CONF --> UTIL
    NONCONF --> UTIL
    ALL --> UTIL

    UTIL["+ utilities: debug, scale, configure (p5)"]

    UTIL --> INTENT{Intent keywords?}
    INTENT -- "'broken' 'error'" --> BOOST_DBG["debug priority boosted"]
    INTENT -- "'deploy' 'push'" --> BOOST_DEP["deploy priority boosted"]
    INTENT -- "'add service'" --> BOOST_BOOT["bootstrap priority boosted"]
    INTENT -- "'scale' 'slow'" --> BOOST_SCALE["scale priority boosted"]
    INTENT -- "none" --> SORT["Sort by priority, return"]
    BOOST_DBG --> SORT
    BOOST_DEP --> SORT
    BOOST_BOOT --> SORT
    BOOST_SCALE --> SORT
```

---

## 3. Bootstrap Flow — Creating Infrastructure

**Container-only.** All guidance assumes SSHFS mounts, SSH deploy, SSH server start. Local flow (Wave 4-5) is not implemented — the `Environment` parameter exists in code but is unused.

**Mode-aware.** Generate and deploy steps deliver different guidance per mode (standard/dev/simple) via `ResolveProgressiveGuidance`. Provision uses inline callouts for mode differences.

6 steps. Plan is the pivot — before plan exists (discover), knowledge is manual. After plan (provision+), knowledge is injected automatically. Each step has an **automated checker** that validates against the live API before allowing advancement.

```mermaid
flowchart TD
    START([action=start workflow=bootstrap]) --> DISC

    subgraph DISC["DISCOVER — plan submission + validation"]
        D1["zerops_discover — classify project"]
        D1 --> D2["Identify services, choose mode"]
        D2 --> D3["Present plan to user, confirm"]
        D3 --> D4["action=complete step=discover"]
        D4 --> D5{ValidateBootstrapTargets}
        D5 -- fail --> D4
    end

    D5 -- "pass — plan stored" --> PROV

    subgraph PROV["PROVISION — create infra + discover env vars"]
        P1["Guide + import.yml Schema"]
        P1 --> P2["Write import.yml, zerops_import"]
        P2 --> P3["zerops_mount + zerops_discover"]
        P3 --> P4["action=complete step=provision"]
        P4 --> P5{checkProvision}
        P5 -- fail --> P3
    end

    P5 -- "pass — env vars stored" --> GEN

    subgraph GEN["GENERATE — write zerops.yml + app code"]
        G1["Guide: generate-common + generate-mode"]
        G1 --> G2["Write zerops.yml + app code"]
        G2 --> G3["action=complete step=generate"]
        G3 --> G4{checkGenerate}
        G4 -- fail --> G2
    end

    G4 -- pass --> DEP

    subgraph DEP["DEPLOY — mode-aware push"]
        DE1["Guide: deploy-overview + deploy-mode"]
        DE1 --> DE2["Deploy per mode"]
        DE2 --> DE3["action=complete step=deploy"]
        DE3 --> DE4{checkDeploy}
        DE4 -- fail --> DE2
    end

    DE4 -- pass --> VER

    subgraph VER["VERIFY — independent health check"]
        V1["zerops_verify all plan targets"]
        V1 --> V2["action=complete step=verify"]
        V2 --> V3{checkVerify}
        V3 -- "fail — step stays in_progress" --> V4{Agent decides}
        V4 -- "fix + retry" --> V2
        V4 -- "action=iterate" --> ITER["iteration++, reset steps 2-4"]
        V4 -- "action=reset" --> RESET(["Session deleted"])
    end

    ITER --> GEN

    V3 -- pass --> STR["STRATEGY — user chooses strategy"]
    STR --> DONE(["Bootstrap complete"])
```

### Step checkers — what gets validated

| Step | Checker | What it validates | On failure |
|------|---------|-------------------|------------|
| **discover** | `ValidateBootstrapTargets()` | Hostnames (a-z0-9, ≤25), types vs live catalog, modes, stage derivation, resolution consistency, HA mode defaults | Validation error returned, agent fixes plan |
| **provision** | `checkProvision()` | All plan services RUNNING/ACTIVE, env vars discoverable for managed deps (side effect: stores env vars on session) | `CheckResult.Passed=false`, step stays `in_progress` |
| **generate** | `checkGenerate()` | zerops.yml exists and parses, setup entry for each dev hostname, env var references valid against discovered vars, ports defined, deployFiles defined | Same — agent sees failed checks, fixes |
| **deploy** | `checkDeploy()` | All runtime services RUNNING, subdomain access enabled for services with ports | Same |
| **verify** | `checkVerify()` | `VerifyAll()` — HTTP health endpoints for all plan target hostnames | Same — agent can iterate or escalate |
| **strategy** | None | User choice, no automated validation | N/A |

### How checkers interact with step advancement

```
Agent: action="complete" step="X" attestation="..."
  │
  ├─ Checker runs BEFORE CompleteStep()
  │
  ├─ IF checker.Passed == false:
  │    → Response includes CheckResult with failed checks
  │    → Step stays in_progress (CompleteStep NOT called)
  │    → Agent sees what failed, can fix and retry action="complete" again
  │
  └─ IF checker.Passed == true:
       → CompleteStep() marks step as complete
       → CurrentStep advances to next step
       → Next step becomes in_progress
```

**This means:**
- An agent can retry the same step's `complete` action multiple times until the checker passes
- The checker runs fresh each time (re-queries API)
- No step advancement happens until the checker passes
- `action="iterate"` is a DIFFERENT escape hatch — it resets steps 2-4 entirely and goes back to generate

**When to use iterate vs. retry:**
- **Retry** (`action="complete"` again): fix was small (e.g., enable subdomain, restart service)
- **Iterate** (`action="iterate"`): need to rewrite code or zerops.yml from scratch

**Provision checker side effect:** Besides validation, it calls `GetServiceEnv()` for each managed dependency and stores discovered env var names on the session via `StoreDiscoveredEnvVars()`. This data powers the generate step's env var knowledge injection.

### What each step gets (knowledge injection)

| Step | Base guidance | Injected knowledge |
|------|--------------|-------------------|
| **discover** | Project classification, mode selection, plan submission | None (plan doesn't exist yet) |
| **provision** | import.yml patterns, hostname rules, env var discovery | import.yml Schema (+ Preprocessor Functions) |
| **generate** | generate-common + generate-{mode} (zerops.yml rules per mode) | Runtime guide + service cards + wiring + env vars + zerops.yml Schema + Rules & Pitfalls |
| **deploy** | deploy-overview + deploy-{mode} + conditionals | Schema Rules + env vars |
| **verify** | Verification protocol | None |
| **strategy** | Strategy options | None |

---

## 4. Deploy Flow — The Development Cycle

3 steps. This is the primary post-bootstrap workflow. Agent loads context, writes code, deploys, verifies.

```mermaid
flowchart TD
    START([action=start workflow=deploy]) --> LOAD

    LOAD["Load service metas from disk<br/>→ Build targets (dev→stage ordering)<br/>→ Build ServiceContext (runtime type, deps)"]
    LOAD --> PREP

    subgraph PREP["PREPARE"]
        direction TB
        PR_GUIDE["Guide: deploy-prepare (config check, prerequisites)"]
        PR_KNOW["Knowledge: runtime briefing + service wiring<br/>+ zerops.yml Schema + Rules & Pitfalls"]
        PR_GUIDE --- PR_KNOW
        PR_KNOW --> PR1["Agent understands the setup"]
        PR1 --> PR2["Write/modify code if needed"]
        PR2 --> PR3["Verify zerops.yml is correct"]
    end

    PR3 --> DEP

    subgraph DEP["DEPLOY (mode-aware)"]
        direction TB
        DEP_GUIDE["Guide: deploy-execute-{mode}"]
        DEP_KNOW["Knowledge: Schema Rules + env vars"]
        DEP_GUIDE --- DEP_KNOW
    end

    DEP --> |standard| STD["1. zerops_deploy dev<br/>2. SSH start<br/>3. Verify dev<br/>4. zerops_deploy stage<br/>5. Verify stage"]
    DEP --> |dev| DDEV["1. zerops_deploy dev<br/>2. SSH start<br/>3. Verify"]
    DEP --> |simple| DSIM["1. zerops_deploy<br/>2. Auto-start<br/>3. Verify"]

    STD --> VER
    DDEV --> VER
    DSIM --> VER

    subgraph VER["VERIFY"]
        V1["zerops_verify → healthy?"]
        V1 -- Yes --> DONE([Deploy complete])
        V1 -- No --> DIAG["Diagnose from checks array"]
        DIAG --> FIX["Fix code/config"]
        FIX --> |"action=iterate"| DEP
    end
```

### Deploy ServiceContext

At `DeployStart`, service metas are read and converted to `DeployServiceContext`:

```
ServiceMeta files → BuildDeployTargets() → DeployServiceContext {
    RuntimeType:     "nodejs@22"       // from first runtime meta
    DependencyTypes: ["postgresql@16"] // from dep metas
    DiscoveredEnvVars: (if available)
}
```

This enables `assembleDeployKnowledge` to inject **runtime-specific** and **dependency-specific** knowledge — same quality as bootstrap generate step.

---

## 5. Stateless Workflows — Operations with Context

Debug, scale, configure are stateless (no session, no steps). But they now receive **service context** — a summary of the project's services from metas.

```mermaid
flowchart TD
    START([action=start workflow=debug/scale/configure]) --> LOAD

    LOAD["Load service metas from disk"] --> BUILD["BuildServiceContextSummary()"]
    BUILD --> INJECT["Prepend to workflow guidance"]

    INJECT --> GUIDE["Agent receives:<br/><br/>## Your Project Services<br/>Runtime: appdev (nodejs@22) [standard] → stage: appstage<br/>Managed: db (postgresql@16)<br/>---<br/>(original workflow guidance)"]
```

The agent knows WHAT exists before starting to diagnose, scale, or configure. No blind `zerops_discover` needed to understand the setup.

### Cross-workflow transitions

Each stateless workflow ends with transition hints:

- **After debug:** → deploy (if code fix needed), scale, configure
- **After scale:** → debug (if still slow), deploy, configure
- **After configure:** → deploy (to apply changes), debug, scale

---

## 6. CI/CD Setup Flow

3 steps. Stateful, provider-specific guidance.

```mermaid
flowchart TD
    START([action=start workflow=cicd]) --> CHOOSE

    subgraph CHOOSE["CHOOSE"]
        C1["Present providers: GitHub Actions, GitLab CI,<br/>Zerops webhook, generic zcli"]
        C1 --> C2["User picks provider"]
        C2 --> C3["action=complete step=choose attestation='Provider: github'"]
    end

    C3 --> CONF

    subgraph CONF["CONFIGURE"]
        CF1{"Provider?"}
        CF1 -- github --> GH["Guide: create token, add secret,<br/>create .github/workflows/deploy.yml"]
        CF1 -- gitlab --> GL["Guide: create token, add CI var,<br/>create .gitlab-ci.yml"]
        CF1 -- webhook --> WH["Guide: connect repo in Zerops dashboard"]
        CF1 -- generic --> GN["Guide: install zcli, add to CI pipeline"]
    end

    CONF --> VER["VERIFY → test push, monitor build, zerops_verify"]
    VER --> DONE([CI/CD setup complete])
```

---

## 7. Mode × Environment Matrix

### What's implemented now

**All flows are container-only.** The table shows what actually works:

| | Standard (container) | Dev (container) | Simple (container) |
|---|---|---|---|
| **Services** | dev + stage + managed | dev + managed | 1 runtime + managed |
| **zerops.yml** | Dev entry (noop), stage later | Dev entry (noop) | Single entry (real start) |
| **`healthCheck`** | None in dev | None | Required |
| **Deploy mechanism** | SSH self-deploy + cross-deploy | SSH self-deploy | SSH self-deploy |
| **Server startup** | Agent via SSH | Agent via SSH | Auto (real start cmd) |
| **Deploy order** | dev → verify → gen stage → cross-deploy → verify stage | dev → verify | deploy → verify |
| **Iteration** | Edit on SSHFS → SSH restart → test | Same | Edit on SSHFS → redeploy → test |
| **File access** | SSHFS mount at `/var/www/{hostname}/` | Same | Same |

### What local flow would need (Wave 4-5, NOT implemented)

| Aspect | Container (now) | Local (future) |
|--------|----------------|----------------|
| **File access** | SSHFS mount `/var/www/{hostname}/` | Local filesystem |
| **Deploy** | SSH into container, `git init` + `zcli push` | `zcli push` from local working dir |
| **Server start** | SSH `run_in_background` | Local process or auto-start after deploy |
| **Dev zerops.yml** | `start: zsc noop --silent` (SSH iteration) | `start: <real command>` (no SSH available) |
| **Prerequisites** | Container exists (auto) | zcli installed + logged in + VPN active |
| **Iteration** | Edit on mount → SSH kill+start | Edit locally → `zcli push` → verify |
| **Env vars** | OS env vars after deploy | `.env.local` generation or VPN access |

**What needs to change for local:**
1. `assembleKnowledge` / `buildGuide` must check `env` and select local-specific sections
2. bootstrap.md needs `generate-standard-local`, `deploy-standard-local` etc. sections (or environment prefixed variants)
3. `zerops_deploy` tool needs local mode (`zcli push` instead of SSH)
4. Preflight checks at workflow start (zcli installed? VPN active?)
5. `bootstrap_steps.go` deploy guidance must not mention SSH/SSHFS for local

**Architecture is ready:** `Environment` type, `DetectEnvironment()`, and parameter plumbing exist. The guidance content and tool implementation are missing.

---

## 8. Knowledge Injection — How It Works

### Guide assembly pipeline (shared by bootstrap + deploy)

```
BuildResponse()
  └─ buildGuide(step, iteration, env, kp)
       ├─ iteration > 0 on deploy? → BuildIterationDelta (3-tier escalation)
       ├─ ResolveProgressiveGuidance(step, plan, iteration)
       │    ├─ generate → generate-common + generate-{mode}
       │    ├─ deploy → deploy-overview + deploy-{mode} + conditionals
       │    └─ other → single <section> from markdown
       └─ assembleKnowledge(step, kp)
            └─ step-specific content from embedded knowledge store
```

### Bootstrap iteration escalation

| Iterations | Tier | Action |
|------------|------|--------|
| 1–2 | Diagnose | `zerops_logs severity="error"`, fix specific error |
| 3–4 | Systematic | 6-point checklist (env vars, 0.0.0.0, deployFiles, ports...) |
| 5+ | Escalate | STOP, show user what was tried, ask before continuing |
| >10 | Max | Session must be reset |

### Context recovery

```
Context lost (compaction / crash / new session)
  → action="status"
  → Engine loads session from disk
  → buildGuide assembles fresh guide from:
      bootstrap.md / deploy.md (embedded)
      + knowledge store (embedded)
      + session state (disk: plan, env vars, step progress)
  → Agent receives identical guide — no state to lose
```

---

## 9. Data Flow — What Gets Persisted Where

```
.zcp/state/
  registry.json              ← active session index
  sessions/{sessionID}.json  ← per-session state:
    WorkflowState {
      Bootstrap *BootstrapState  (plan, env vars, steps, strategies)
      Deploy    *DeployState     (targets, service context, steps)
      CICD      *CICDState       (provider, hostnames, steps)
    }
  services/{hostname}.json   ← service metas (survive session deletion):
    ServiceMeta {
      hostname, type, mode, stageHostname,
      dependencies, strategy, status
    }
```

**Service metas are the bridge between workflows.** Bootstrap writes them, deploy/debug/scale/configure read them. They carry the decisions forward: what runtime, what mode, what dependencies, what strategy.

---

## 10. File Map

| File | Role |
|------|------|
| **Bootstrap** | |
| `workflow/bootstrap.go` | BootstrapState, BuildResponse, step state machine |
| `workflow/bootstrap_guide_assembly.go` | `buildGuide`, `assembleKnowledge`, `formatEnvVarsForGuide` |
| `workflow/bootstrap_guidance.go` | `ResolveProgressiveGuidance`, `BuildIterationDelta`, `extractSection` |
| `workflow/bootstrap_steps.go` | Step definitions (name, category, tools, verification) |
| `content/workflows/bootstrap.md` | Section content: discover, provision, generate-{common,standard,dev,simple}, deploy-{overview,standard,dev,simple,iteration,agents} |
| **Deploy** | |
| `workflow/deploy.go` | DeployState, DeployServiceContext, `BuildDeployTargets`, `assembleDeployKnowledge` |
| `workflow/deploy_guidance.go` | `resolveDeployStepGuidance`, `ResolveDeployGuidance` (strategy-based) |
| `content/workflows/deploy.md` | Section content: deploy-prepare, deploy-execute-{overview,standard,dev,simple}, deploy-verify |
| **CI/CD** | |
| `workflow/cicd.go` | CICDState, provider constants, step logic |
| `workflow/cicd_guidance.go` | `resolveCICDGuidance` (provider-specific sections) |
| `content/workflows/cicd.md` | Section content: cicd-choose, cicd-configure-{github,gitlab,webhook,generic}, cicd-verify |
| **Shared** | |
| `workflow/engine.go` | Engine with env + knowledge, all Start/Complete/Status/Skip methods |
| `workflow/state.go` | WorkflowState (Bootstrap + Deploy + CICD), `IsImmediateWorkflow` |
| `workflow/environment.go` | Environment type (container/local) |
| `workflow/service_meta.go` | ServiceMeta CRUD, ListServiceMetas |
| `workflow/service_context.go` | `BuildServiceContextSummary` for stateless workflows |
| `workflow/router.go` | Route(), intent detection, strategy offerings |
| `workflow/session.go` | Session management, iteration, max iterations |
| **Tools** | |
| `tools/workflow.go` | Action dispatcher, `handleStart`, `detectActiveWorkflow` |
| `tools/workflow_bootstrap.go` | Bootstrap-specific handlers |
| `tools/workflow_deploy.go` | Deploy-specific handlers |
| `tools/workflow_cicd.go` | CI/CD-specific handlers |
| **Knowledge** | |
| `tools/knowledge.go` | `zerops_knowledge` MCP tool (scope, briefing, query, recipe) |
| `knowledge/engine.go` | Provider interface, GetEmbeddedStore, GetBriefing, GetCore |
| `knowledge/sections.go` | H2/H3 section parsing, runtime/service normalizers |
