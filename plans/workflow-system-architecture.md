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

6 steps. Plan is the pivot — before plan exists (discover), knowledge is manual. After plan (provision+), knowledge is injected automatically.

```mermaid
flowchart TD
    START([action=start workflow=bootstrap]) --> DISC

    subgraph DISC["DISCOVER"]
        D1["zerops_discover → classify: FRESH / CONFORMANT / NON_CONFORMANT"]
        D1 --> D2["Identify runtime + services from user intent"]
        D2 --> D3["Optional: zerops_knowledge recipe=... for framework specifics"]
        D3 --> D4["Present plan to user, confirm mode (standard/dev/simple)"]
        D4 --> D5["action=complete step=discover plan=[targets]"]
    end

    D5 --> |"Plan validated + stored.<br/>Service metas: PLANNED"| PROV

    subgraph PROV["PROVISION"]
        direction TB
        P_GUIDE["Guide: provision section"]
        P_KNOW["Knowledge: import.yml Schema + Preprocessor Functions"]
        P_GUIDE --- P_KNOW
        P_KNOW --> P1["Write import.yml → zerops_import"]
        P1 --> P2["zerops_discover includeEnvs=true → env vars stored"]
        P2 --> P3["zerops_mount dev services"]
    end

    P3 --> |"Metas: PROVISIONED"| GEN

    subgraph GEN["GENERATE (mode-filtered)"]
        direction TB
        G_GUIDE["Guide: generate-common + generate-{mode}"]
        G_KNOW["Knowledge: runtime guide + service cards<br/>+ env vars + zerops.yml Schema + Rules"]
        G_GUIDE --- G_KNOW
        G_KNOW --> G1["Write zerops.yml + app code"]
    end

    G1 --> DEP

    subgraph DEP["DEPLOY (mode-filtered)"]
        direction TB
        DEP_GUIDE["Guide: deploy-overview + deploy-{mode}"]
        DEP_KNOW["Knowledge: Schema Rules + env vars"]
        DEP_GUIDE --- DEP_KNOW
        DEP_KNOW --> DEP1["Deploy per mode"]
    end

    DEP1 --> VER["VERIFY → zerops_verify all targets"]
    VER --> |"healthy"| STR["STRATEGY → user chooses: push-dev / ci-cd / manual"]
    VER --> |"unhealthy"| ITER["ITERATE → reset steps 2-4, escalate"]
    ITER --> GEN
    STR --> DONE(["Bootstrap complete<br/>Metas: BOOTSTRAPPED<br/>Transition message: deploy / cicd / scale / debug"])
```

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

## 7. Mode Differences — Complete Matrix

### Modes affect generate, deploy, and iteration

| | Standard | Dev | Simple |
|---|---|---|---|
| **Services created** | dev + stage + managed | dev + managed | 1 runtime + managed |
| **zerops.yml entries** | Dev only (stage later) | Dev only | Single entry |
| **`start:`** | `zsc noop --silent` | `zsc noop --silent` | Real command |
| **`healthCheck:`** | None in dev | None | Required |
| **Server startup** | Agent via SSH | Agent via SSH | Auto after deploy |
| **Deploy order** | dev → verify → stage → verify | dev → verify | deploy → verify |
| **Stage entry** | Generated after dev verified | N/A | N/A |
| **Iteration** | Edit → SSH restart → test | Edit → SSH restart → test | Edit → redeploy → test |

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
