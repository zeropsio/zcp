# ZCP Workflow System — Complete Flow Architecture

Container environment only. Local flow planned (Wave 4-5), architecture prepared but not implemented.

---

## 1. System Overview

```mermaid
flowchart LR
    subgraph STATEFUL["Stateful workflows (session + steps)"]
        BOOT["bootstrap\n6 steps"]
        DEP["deploy\n3 steps"]
        CICD["cicd\n3 steps"]
    end

    subgraph STATELESS["Stateless workflows (guidance only)"]
        DBG["debug"]
        SCL["scale"]
        CFG["configure"]
    end

    USER([User]) --> ROUTER{Router}
    ROUTER --> BOOT
    ROUTER --> DEP
    ROUTER --> CICD
    ROUTER --> DBG
    ROUTER --> SCL
    ROUTER --> CFG

    BOOT -->|"writes service metas\nhostname, type, mode,\nstage, deps, strategy"| METAS[(metas)]
    METAS -->|"reads at start"| DEP
    METAS -->|"reads at start"| CICD
    METAS -->|"reads at start"| DBG
    METAS -->|"reads at start"| SCL
    METAS -->|"reads at start"| CFG

    KNOW[(Knowledge store\nembedded in binary)] -->|"injected into\nstep guides"| BOOT
    KNOW -->|"injected into\nstep guides"| DEP
    KNOW -->|"zerops_knowledge\ntool"| USER
```

**Bootstrap** creates infrastructure and writes service metas. All other workflows read metas for context. Knowledge is embedded and injected into guides automatically.

---

## 2. Project Lifecycle

```mermaid
flowchart TD
    FRESH["Fresh project\nno runtime services"] -->|"action=start\nworkflow=bootstrap"| BOOT

    BOOT["BOOTSTRAP\ndiscover - provision - generate\ndeploy - verify - strategy"] -->|"metas written\nstrategy chosen"| READY

    READY["Project ready\nservices running\nstrategy recorded"] -->|"user wants changes"| DEV_LOOP

    DEV_LOOP["DEPLOY WORKFLOW\nprepare - deploy - verify"] -->|"done"| READY

    READY -->|"set up CI/CD"| CICD_FLOW["CICD WORKFLOW\nchoose - configure - verify"]
    CICD_FLOW --> READY

    READY -->|"something broken"| DEBUG_OP["DEBUG\ndiagnose with service context"]
    DEBUG_OP -->|"needs code fix"| DEV_LOOP
    DEBUG_OP -->|"config fix"| CFG_OP

    READY -->|"performance issue"| SCALE_OP["SCALE\nwith service context"]
    READY -->|"change env/ports"| CFG_OP["CONFIGURE\nwith service context"]
```

Bootstrap runs once. Deploy is the primary loop. Debug/scale/configure are side operations.

---

## 3. Router

Determines which workflow to suggest based on project state, service metas, and user intent.

```mermaid
flowchart TD
    IN["RouterInput"] --> PS{Project state?}

    PS -->|FRESH| F["bootstrap p1"]
    PS -->|CONFORMANT| C{Dominant strategy?}
    PS -->|NON_CONFORMANT| NC["bootstrap p1\n+ deploy p2"]
    PS -->|UNKNOWN| U["all workflows p3"]

    C -->|push-dev| CP["deploy p1"]
    C -->|ci-cd| CC["cicd p1\ndeploy p2"]
    C -->|manual| CM["manual-deploy p1"]
    C -->|none set| CN["deploy p1"]

    F --> UTIL["Append utilities:\ndebug p5, scale p5, configure p5"]
    CP --> UTIL
    CC --> UTIL
    CM --> UTIL
    CN --> UTIL
    NC --> UTIL
    U --> UTIL

    UTIL --> IB{Intent keywords\nin user message?}
    IB -->|"broken, error, crash"| BD["boost debug -2"]
    IB -->|"deploy, push, ship"| BDP["boost deploy -2"]
    IB -->|"add, create, new service"| BB["boost bootstrap -2"]
    IB -->|"slow, cpu, memory, scale"| BS["boost scale -2"]
    IB -->|none| DONE["Sort by priority\nreturn offerings"]
    BD --> DONE
    BDP --> DONE
    BB --> DONE
    BS --> DONE
```

Priority: p1 = primary, p2 = secondary, p3 = unknown, p5 = utility. Intent boost reduces priority by 2.

---

## 4. Bootstrap Flow

Container-only. Mode-aware (standard/dev/simple). 6 steps with automated checkers.

### 4.1 Step sequence

```mermaid
flowchart TD
    START(["start bootstrap"]) --> D

    D["DISCOVER\nclassify project\nidentify services\nchoose mode\npresent plan to user"]
    D -->|"action=complete plan=..."| DV{Plan validation\nhostnames, types,\nmodes, resolutions}
    DV -->|fail| D
    DV -->|"pass: plan stored\nmetas PLANNED"| P

    P["PROVISION\nGuide + import.yml Schema knowledge\nwrite import.yml\nzerops_import\nzerops_mount\nzerops_discover includeEnvs"]
    P -->|"action=complete"| PV{Provision checker\nservices RUNNING?\nenv vars found?}
    PV -->|fail| P
    PV -->|"pass: env vars stored\nmetas PROVISIONED"| G

    G["GENERATE\nGuide: generate-common + generate-mode\nKnowledge: runtime + services +\nenv vars + yml schema + rules\nwrite zerops.yml + app code"]
    G -->|"action=complete"| GV{Generate checker\nzerops.yml valid?\nenv refs OK? ports? deployFiles?}
    GV -->|fail| G
    GV -->|pass| DP

    DP["DEPLOY\nGuide: deploy-overview + deploy-mode\nKnowledge: Schema Rules + env vars\ndeploy per mode"]
    DP -->|"action=complete"| DPV{Deploy checker\nall RUNNING?\nsubdomains enabled?}
    DPV -->|fail| DP
    DPV -->|pass| V

    V["VERIFY\nzerops_verify all targets"]
    V -->|"action=complete"| VV{Verify checker\nall healthy?}
    VV -->|"pass"| S["STRATEGY\nuser chooses:\npush-dev / ci-cd / manual"]
    VV -->|"fail"| VDEC{Agent decides}
    VDEC -->|"retry: fix and\ncomplete again"| V
    VDEC -->|"iterate: reset\nsteps 2-4"| ITER

    ITER["iteration++\nescalation tier advances\ngenerate+deploy+verify reset"] --> G

    S --> FIN(["Bootstrap complete\nmetas BOOTSTRAPPED\ntransition message"])
```

### 4.2 Checker mechanics

```
action="complete" step="X" attestation="..."
  │
  ├─ Checker runs BEFORE step advances
  │
  ├─ IF Passed == false:
  │    Step stays in_progress
  │    Response includes CheckResult.checks array
  │    Agent can fix + retry action="complete"
  │
  └─ IF Passed == true:
       CompleteStep() → step marked complete
       Next step becomes in_progress
```

| Step | Checker | Validates | Side effects |
|------|---------|-----------|-------------|
| discover | `ValidateBootstrapTargets` | Hostnames a-z0-9 max 25, types vs live catalog, modes, stage derivation, resolutions, HA defaults | None |
| provision | `checkProvision` | All services RUNNING, env vars discoverable for managed deps | Stores env var names on session |
| generate | `checkGenerate` | zerops.yml parses, setup entry per dev hostname, env refs valid, ports defined, deployFiles defined | None |
| deploy | `checkDeploy` | All runtimes RUNNING, subdomain enabled for services with ports | None |
| verify | `checkVerify` | VerifyAll HTTP health for plan targets | None |
| strategy | None | User choice | None |

### 4.3 Iteration escalation

When verify fails and agent calls `action="iterate"`:

| Iteration | Tier | Guidance |
|-----------|------|----------|
| 1-2 | Diagnose | Check zerops_logs for specific error, fix, redeploy |
| 3-4 | Systematic | 6-point checklist: env vars, 0.0.0.0, deployFiles, ports, start cmd, zerops.yml |
| 5+ | Escalate | STOP. Present history to user. Ask before continuing. |
| >10 | Max | Session must be reset |

### 4.4 Knowledge injection per step

```mermaid
flowchart LR
    subgraph DISC["discover"]
        DA["No injection\nplan does not exist yet"]
    end

    subgraph PROV["provision"]
        PA["import.yml Schema\nPreprocessor Functions"]
    end

    subgraph GEN["generate"]
        GA["Runtime guide\nService cards + wiring\nDiscovered env vars\nzerops.yml Schema\nRules and Pitfalls"]
    end

    subgraph DEP["deploy"]
        DPA["Schema Rules\nDiscovered env vars"]
    end

    subgraph VER["verify + strategy"]
        VA["No injection"]
    end

    CORE[(core.md)] --> PROV
    CORE --> GEN
    CORE --> DEP
    RUNTIMES[(runtimes/*.md)] --> GEN
    SERVICES[(services.md)] --> GEN
    SESSION[(session state\nenv vars)] --> GEN
    SESSION --> DEP
```

### 4.5 Mode differences

| | Standard | Dev | Simple |
|---|---|---|---|
| Services | dev + stage + managed | dev + managed | 1 runtime + managed |
| zerops.yml | dev entry only, stage later | dev entry only | single entry |
| start cmd | zsc noop --silent | zsc noop --silent | real command |
| healthCheck | none in dev | none | required |
| server start | SSH manual | SSH manual | auto after deploy |
| deploy | dev then cross-deploy stage | dev only | direct |
| iteration | edit on mount, SSH restart | same | edit on mount, redeploy |
| generate section | generate-standard | generate-dev | generate-simple |
| deploy section | deploy-standard | deploy-dev | deploy-simple |

---

## 5. Deploy Flow

Primary post-bootstrap workflow. 3 steps. Mode-aware. Reads service metas for context.

```mermaid
flowchart TD
    START(["start deploy"]) --> LOAD

    LOAD["Load service metas\nBuild targets: dev before stage\nBuild ServiceContext:\nruntimeType, dependencyTypes"] --> PREP

    PREP["PREPARE\nGuide: deploy-prepare\nKnowledge: runtime briefing +\nservice wiring + yml schema + rules\n\nAgent checks config,\nwrites/modifies code if needed"]
    PREP -->|"action=complete\nstep=prepare"| DEP

    DEP["DEPLOY\nGuide: deploy-execute + mode section\nKnowledge: Schema Rules + env vars"]

    DEP -->|"standard mode"| STD["1 zerops_deploy dev\n2 SSH start dev\n3 zerops_verify dev\n4 zerops_deploy stage from dev\n5 zerops_verify stage"]
    DEP -->|"dev mode"| DONLY["1 zerops_deploy dev\n2 SSH start dev\n3 zerops_verify dev"]
    DEP -->|"simple mode"| SIMP["1 zerops_deploy\n2 auto-start\n3 zerops_verify"]

    STD -->|"action=complete\nstep=deploy"| VER
    DONLY -->|"action=complete\nstep=deploy"| VER
    SIMP -->|"action=complete\nstep=deploy"| VER

    VER["VERIFY\nGuide: deploy-verify\nzerops_verify all targets"]
    VER -->|healthy| FIN(["Deploy complete"])
    VER -->|unhealthy| FIX["Diagnose from checks array\nFix code or config"]
    FIX -->|"action=complete retry"| VER
    FIX -->|"action=iterate\nreset deploy+verify"| DEP
```

**ServiceContext** populated at start from metas:
- `RuntimeType` — e.g. nodejs@22 (from first runtime meta)
- `DependencyTypes` — e.g. postgresql@16, valkey@7.2 (from dep metas)
- Enables runtime-specific and dependency-specific knowledge injection

---

## 6. CI/CD Setup Flow

3 steps. Provider-specific guidance.

```mermaid
flowchart TD
    START(["start cicd"]) --> CH

    CH["CHOOSE\nPresent providers:\nGitHub Actions, GitLab CI,\nZerops webhook, generic zcli\n\nUser picks one"]
    CH -->|"action=complete\nstep=choose"| CONF

    CONF{"Provider?"}
    CONF -->|github| GH["Create Zerops token\nAdd GitHub secret\nCreate workflow file\nCommit and push"]
    CONF -->|gitlab| GL["Create Zerops token\nAdd CI variable\nCreate pipeline file\nCommit and push"]
    CONF -->|webhook| WH["Connect repo in\nZerops dashboard\nSet trigger branch"]
    CONF -->|generic| GN["Install zcli in CI\nSet ZEROPS_TOKEN\nAdd push step"]

    GH -->|"action=complete\nstep=configure"| VER
    GL -->|"action=complete\nstep=configure"| VER
    WH -->|"action=complete\nstep=configure"| VER
    GN -->|"action=complete\nstep=configure"| VER

    VER["VERIFY\nPush test commit\nMonitor build\nzerops_verify"]
    VER --> FIN(["CI/CD setup complete"])
```

---

## 7. Stateless Workflows

Debug, scale, configure. No session. Service context from metas prepended to guidance.

```mermaid
flowchart TD
    START(["start debug/scale/configure"]) --> META

    META["Read service metas from disk"] --> CTX

    CTX["BuildServiceContextSummary\n\nRuntime services:\n  appdev nodejs@22 standard, stage: appstage\nManaged services:\n  db postgresql@16"]

    CTX --> GUIDE["Prepend context to\nworkflow guidance markdown"]

    GUIDE --> AGENT["Agent has full picture:\nwhat services exist,\ntheir types, modes,\nstrategies, dependencies\n+ operational guidance"]

    AGENT -->|"after debug"| TRANS["Transition hints:\ndeploy, scale, configure"]
    AGENT -->|"after scale"| TRANS
    AGENT -->|"after configure"| TRANS
```

---

## 8. Knowledge Injection Pipeline

Shared by bootstrap and deploy. Assembles fresh guide every time from embedded sources.

```mermaid
flowchart TD
    BR["BuildResponse called"] --> BG["buildGuide\nstep, iteration, kp"]

    BG --> IT{Iteration > 0\nand deploy step?}
    IT -->|yes| ESC["BuildIterationDelta\n3-tier escalation\nreplaces entire guide"]
    IT -->|no| RPG

    RPG["ResolveProgressiveGuidance\nstep, plan, failureCount"]
    RPG --> ST{Step type?}

    ST -->|"discover, provision,\nverify, strategy"| SINGLE["Single section\nfrom bootstrap.md"]
    ST -->|generate| GMODE["generate-common\n+ generate-standard/dev/simple"]
    ST -->|deploy| DMODE["deploy-overview\n+ deploy-standard/dev/simple\n+ iteration/agents/recovery"]

    SINGLE --> AK["assembleKnowledge\nstep, kp"]
    GMODE --> AK
    DMODE --> AK

    AK --> KP{Knowledge provider\navailable?}
    KP -->|nil or no plan| BASE(["Return base guide only"])
    KP -->|available| INJ["Inject step-specific\nknowledge from embedded store"]
    INJ --> FULL(["Return base guide\n+ separator\n+ knowledge sections"])
```

### Context recovery

All guide sources are always available:
- `bootstrap.md` / `deploy.md` — embedded in binary
- Knowledge store — embedded in binary
- Session state (plan, env vars, step progress) — on disk

`action="status"` rebuilds the identical guide. No tracking state to lose.

---

## 9. Data Persistence

```mermaid
flowchart LR
    subgraph DISK[".zcp/state/"]
        REG["registry.json\nactive session index\nflock-protected"]
        SESS["sessions/id.json\nWorkflowState:\n  Bootstrap or Deploy or CICD\n  iteration, intent, PID"]
        META["services/hostname.json\nServiceMeta:\n  type, mode, stage,\n  deps, strategy, status"]
    end

    BOOT["Bootstrap"] -->|"writes at plan/provision/complete"| META
    BOOT -->|"session lifecycle"| SESS
    BOOT -->|"register/unregister"| REG

    DEP["Deploy"] -->|"reads at start"| META
    DEP -->|"session lifecycle"| SESS
    CICD_W["CI/CD"] -->|"reads at start"| META
    CICD_W -->|"session lifecycle"| SESS

    DBG["Debug/Scale/Configure"] -->|"reads for context"| META
```

Metas survive session deletion. They carry decisions forward across workflows.

---

## 10. Mode x Environment Matrix

### Implemented (container only)

| | Standard | Dev | Simple |
|---|---|---|---|
| Services | dev + stage + managed | dev + managed | 1 runtime + managed |
| zerops.yml | dev noop, stage later | dev noop | single real start |
| healthCheck | none in dev | none | required |
| Deploy | SSH self + cross-deploy | SSH self | SSH self |
| Start | SSH manual | SSH manual | auto |
| Iteration | SSHFS edit, SSH restart | same | SSHFS edit, redeploy |
| File access | SSHFS /var/www/hostname/ | same | same |

### Not implemented (local, Wave 4-5)

| Aspect | Container now | Local future |
|--------|--------------|-------------|
| Files | SSHFS mount | Local filesystem |
| Deploy | SSH git+zcli push | zcli push from local |
| Dev start | zsc noop + SSH | Real start always |
| Prereqs | Container auto | zcli + VPN + auth |
| Iteration | Mount edit, SSH restart | Local edit, zcli push |

Architecture ready: Environment type + DetectEnvironment exist. Content and tooling missing.

---

## 11. File Map

| File | Role |
|------|------|
| **Bootstrap** | |
| `workflow/bootstrap.go` | BootstrapState, BuildResponse, step state machine |
| `workflow/bootstrap_guide_assembly.go` | buildGuide, assembleKnowledge, formatEnvVarsForGuide |
| `workflow/bootstrap_guidance.go` | ResolveProgressiveGuidance, BuildIterationDelta, extractSection |
| `workflow/bootstrap_steps.go` | Step definitions: name, category, tools, verification |
| `content/workflows/bootstrap.md` | Sections: discover, provision, generate-common/standard/dev/simple, deploy-overview/standard/dev/simple/iteration/agents |
| **Deploy** | |
| `workflow/deploy.go` | DeployState, DeployServiceContext, BuildDeployTargets, assembleDeployKnowledge |
| `workflow/deploy_guidance.go` | resolveDeployStepGuidance, ResolveDeployGuidance |
| `content/workflows/deploy.md` | Sections: deploy-prepare, deploy-execute-overview/standard/dev/simple, deploy-verify |
| **CI/CD** | |
| `workflow/cicd.go` | CICDState, provider constants, step logic |
| `workflow/cicd_guidance.go` | resolveCICDGuidance |
| `content/workflows/cicd.md` | Sections: cicd-choose, cicd-configure-github/gitlab/webhook/generic, cicd-verify |
| **Engine** | |
| `workflow/engine.go` | All Start/Complete/Status/Skip/Iterate methods |
| `workflow/state.go` | WorkflowState with Bootstrap + Deploy + CICD fields |
| `workflow/session.go` | Session management, iteration, max iterations |
| `workflow/environment.go` | Environment type, DetectEnvironment |
| `workflow/validate.go` | Plan validation, RuntimeBase, DependencyTypes |
| `workflow/service_meta.go` | ServiceMeta CRUD |
| `workflow/service_context.go` | BuildServiceContextSummary for stateless workflows |
| `workflow/router.go` | Route, intent detection, strategy offerings |
| **Tools** | |
| `tools/workflow.go` | Action dispatcher, handleStart, detectActiveWorkflow |
| `tools/workflow_bootstrap.go` | Bootstrap handlers, step checkers |
| `tools/workflow_checks.go` | checkProvision, checkDeploy, checkVerify |
| `tools/workflow_checks_generate.go` | checkGenerate, zerops.yml validation |
| `tools/workflow_deploy.go` | Deploy handlers |
| `tools/workflow_cicd.go` | CI/CD handlers |
| **Knowledge** | |
| `tools/knowledge.go` | zerops_knowledge MCP tool: scope, briefing, query, recipe |
| `knowledge/engine.go` | Provider interface, GetEmbeddedStore, GetBriefing, GetCore |
| `knowledge/sections.go` | H2/H3 parsing, runtime/service normalizers |
