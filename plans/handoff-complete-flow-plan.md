# ZCP Flow Architecture — Complete Handoff Document

**Release target: v0.6**

This document contains EVERYTHING needed to implement the comprehensive flow system. It is self-contained — the implementing team does not need prior context.

---

## Table of Contents

1. [What ZCP Is](#1-what-zcp-is)
2. [Current State — What Works Today](#2-current-state)
3. [Problems to Solve](#3-problems-to-solve)
4. [Target Architecture](#4-target-architecture)
5. [Design Decisions (Confirmed)](#5-design-decisions)
6. [Codebase Snapshot — Exact Current State](#6-codebase-snapshot)
7. [Wave 1: Container Bootstrap Completion](#7-wave-1)
8. [Wave 2: Stateful Deploy Workflow](#8-wave-2)
9. [Wave 3: Post-Bootstrap Transitions](#9-wave-3)
10. [Wave 4–5: Local Flow](#10-waves-4-5)
11. [Wave 6: Polish](#11-wave-6)
12. [Knowledge Content Matrix](#12-knowledge-matrix)
13. [Test Strategy](#13-test-strategy)
14. [Files Reference](#14-files-reference)

---

## 1. What ZCP Is

ZCP (Zerops Control Plane) is an MCP server that orchestrates LLM agents through Zerops PaaS infrastructure management. It does NOT execute operations — it tells the LLM what to do, validates results, and prevents drift. The LLM is the actor; ZCP is the conductor.

**Single Go binary.** Runs in two environments:
- **Container:** Inside a Zerops service (`zcpx`), with SSH/SSHFS access to other services
- **Local:** On user's machine, connected to Zerops project via VPN

**15 MCP tools** for discovery, deployment, verification, knowledge delivery, and workflow orchestration.

**Workflow system:** Stateful bootstrap (6-step conductor) + stateless utility workflows (deploy, debug, scale, configure, monitor).

---

## 2. Current State — What Works Today

### Working
- **Three-mode system:** `standard` (dev+stage), `dev` (dev only, expandable), `simple` (single service). Implemented via `RuntimeTarget.Mode` + `EffectiveMode()`.
- **6-step bootstrap:** discover → provision → generate → deploy → verify → strategy. Session state persists to `.zcp/state/sessions/{id}.json`.
- **Progressive deploy guidance:** Mode-specific sections from `bootstrap.md` assembled by `ResolveProgressiveGuidance()`.
- **Service metadata:** Per-service JSON persists mode, strategy, dependencies across sessions.
- **Workflow router:** Pure function routes users to appropriate workflow based on project state + metadata.
- **Knowledge engine:** 75+ embedded documents, BM25-like search, 7-layer briefing assembly, 29 recipes, 17 runtime guides.
- **Container flow:** SSH deploy, SSHFS mount, SSH start/stop iteration cycle — all functional.

### Not Working / Missing
1. **Knowledge delivery is disconnected from workflow.** Guide says "call zerops_knowledge" → LLM calls it → 35KB blob delivered → compacted away → LLM loses it. Guide and knowledge are separate channels.
2. **ContextDelivery dedup blocks recovery.** `BriefingFor`, `ScopeLoaded`, `GuideSentFor` track what was delivered and block re-delivery. After context compaction or process crash, knowledge is permanently lost for that session.
3. **Deploy workflow is stateless.** Returns 243 lines of markdown, no tracking, no resume, no mode-awareness at runtime.
4. **No post-bootstrap transition.** Strategy step ends with "push-dev/ci-cd/manual" choice but no actual handoff or CI/CD setup assistance.
5. **Local flow not implemented.** `zerops_deploy` and `zerops_mount` return `ErrNotImplemented` outside containers. No local bootstrap flow exists.
6. **Mode is silently decided by LLM.** No explicit user confirmation of the plan before submission.
7. **No escalating recovery.** Iterations 1-10 all get the same recovery table. No user involvement after repeated failures.

---

## 3. Problems to Solve

### Root Cause
Knowledge is delivered SEPARATELY from workflow guidance, EARLY in the session, and the system ACTIVELY BLOCKS re-delivery. This single design flaw causes all downstream problems (context loss, no recovery, stale knowledge).

### Solution Principle: Solve at the Beginning
If each step guide includes the knowledge relevant to that step, assembled fresh every time from always-available sources (knowledge store = embedded in binary, session state = on disk), then:
- Context compaction → `action="status"` returns fresh guide with knowledge → **non-issue**
- Process crash → resume → fresh guide with knowledge → **non-issue**
- Follow-up in same conversation → knowledge tools work without dedup → **non-issue**

### The Five Changes
1. **Knowledge injection into guides** — guide builder assembles from bootstrap.md + knowledge store + session state
2. **Remove ContextDelivery entirely** — no tracking, no dedup, no gating. Every guide is always fresh.
3. **Stateful deploy workflow** — 3 steps (prepare → deploy → verify), mode-aware, knowledge-injected
4. **Post-bootstrap decision point + CI/CD setup flow** — new workflow for git-based deployment setup
5. **Local flow architecture** — environment-aware everything, preflight checks, zcli push deploy

---

## 4. Target Architecture

### Environment × Mode Matrix

**Container modes:**

| Mode | Services on Zerops | Dev lifecycle | Deploy mechanism |
|---|---|---|---|
| **standard** | dev + stage + managed | SSHFS edit → SSH start/stop → SSH deploy dev → SSH cross-deploy stage | SSH (git init + zcli push inside container) |
| **dev** | dev + managed | Same as standard dev, no stage | SSH push |
| **simple** | 1 runtime + managed | SSHFS edit → SSH deploy → auto-start (real start cmd) | SSH push |

**Local modes:**

| Mode | Services on Zerops | Dev lifecycle | Deploy mechanism |
|---|---|---|---|
| **standard** | dev + stage + managed | Local edit → local server → zcli push dev → zcli push stage | zcli push from local |
| **dev** | managed only (NO runtime) | Local edit → local server (connects to managed via VPN) | No deploy target |
| **simple** | stage + managed | Local edit → zcli push stage | zcli push from local |

**Key differences local vs container:**
- In container: dev service = Zerops container (SSHFS + SSH for editing and process control)
- In local: dev environment = user's machine. Dev service on Zerops (if exists) is a "test deployment target" with REAL start command (not `zsc noop`), because user won't SSH in to manually start it
- In local: ALL pushes go from local machine directly to target. No cross-deploy between services.
- VPN gives local machine full network access: `hostname:port` (same as container), SSH to all services

### Network Access (Both Environments)

| From | To | Access pattern |
|---|---|---|
| Container | Any service | `hostname:port` direct |
| Local (VPN active) | Any service | `hostname:port` direct (same) |
| Local (VPN active) | SSH | `ssh hostname` direct (same) |

### Post-Bootstrap Flow

```
Bootstrap complete
        ↓
"Infrastructure is ready. How do you want to deploy going forward?"
        ↓
    ┌───────────────────────────────────────┐
    │                                       │
    ▼                                       ▼
Option A: "Same way"               Option B: "Git-based CI/CD"
    ↓                                       ↓
Deploy workflow                     CI/CD Setup workflow (NEW)
(stateful, 3 steps)                (stateful, assists with
                                    repo connection, config
                                    generation, test push,
                                    verification)
    ↓                                       ↓
    └───────────────────────────────────────┘
                        ↓
            Record decision to service meta
            .zcp/state/services/{hostname}.json
            Decisions["deployStrategy"] = "push-dev" | "ci-cd" | "manual"
```

**Persistence:** The chosen flow MUST be recorded in service meta files so that:
1. Next session knows which workflow to suggest (router reads `Decisions["deployStrategy"]`)
2. Deploy workflow knows HOW to deploy (push-dev vs ci-cd behavior differs)
3. CI/CD setup workflow can check if already configured

**Existing mechanism:** `ServiceMeta.Decisions` map in `service_meta.go` with constants:
- `DecisionDeployStrategy = "deployStrategy"`
- `StrategyPushDev = "push-dev"` — SSH push (container) or zcli push (local)
- `StrategyCICD = "ci-cd"` — git push triggers deploy
- `StrategyManual = "manual"` — user manages deploys manually

**Where it's written:** During strategy step completion in `handleStrategy()` (`tools/workflow_strategy.go`). Currently only writes during bootstrap. Post-bootstrap flow changes (e.g., switching from push-dev to ci-cd) must also update the meta.

**Where it's read:** `router.go` → `strategyOfferings()` reads dominant strategy across all service metas to suggest the appropriate workflow. Deploy workflow will also read it to determine deploy behavior.

### Knowledge Flow

```
Every guide request:
  1. Extract step section from bootstrap.md (mode-specific)
  2. Append relevant knowledge from knowledge store:
     - provision: import.yml schema + rules for planned services
     - generate: runtime guide + wiring + discovered env vars + zerops.yml schema
     - deploy: deploy rules + env var reminder
  3. Return assembled guide

Sources are ALWAYS available:
  - bootstrap.md = embedded in binary
  - knowledge store = embedded in binary
  - session state = on disk (.zcp/state/)
  → Guide can always be reassembled fresh
  → No caching, no dedup, no gating needed
```

---

## 5. Design Decisions (Confirmed with User)

### D1: Backward Compatibility
**NONE NEEDED.** Early dev phase, no users. Can change state.go, session format, API, anything.

### D2: Overengineering Definition
NOT about avoiding large changes. Large but SIMPLE changes are fine. Avoid COMPLEX solutions that create new problems. Solve at the root, not downstream symptoms.

### D3: Local Env Vars
Plan for it in design, implement later. Local dev will need env vars from managed services (likely `.env.local` generation), but not in scope for immediate implementation.

### D4: Local Deploy
Always from local machine via `zcli push [hostname]`. Never cross-deploy between services in local mode.

### D5: Local Dev Service Behavior
In local standard mode, dev service on Zerops gets REAL start command (not `zsc noop`) because user won't SSH in to manually start it. Dev is a "test deployment target", not an interactive environment.

### D6: Mode Selection
LLM MUST present plan to user and get confirmation before submitting. Smart defaults based on environment (container → standard, local → simple). Guidance instructs LLM to ask.

### D7: Post-Bootstrap Decision Point
After bootstrap, explicit decision: (A) continue same way → deploy workflow, or (B) want CI/CD → NEW ci/cd setup workflow with assisted configuration, test push, and verification.

### D8: CI/CD Setup
Separate stateful workflow (not just guidance text). Assists with: repo connection (webhook or GitHub Actions), config generation, test push verification.

### D9: Deploy Workflow
3 simple steps: prepare → deploy → verify. Stateful with session tracking. Mode-aware. Knowledge-injected.

### D10: Stateless Workflows
Scale, configure, debug, monitor — keep stateless. Improve knowledge delivery but don't add state tracking. They're simple enough.

### D11: Preflight Checks
Check VPN/zcli at workflow start (not ZCP startup). Don't block operations that don't need VPN.

---

## 6. Codebase Snapshot — Exact Current State

### Core Files

#### `internal/workflow/state.go` (46 lines)
```go
type WorkflowState struct {
    Version   string          `json:"version"`
    SessionID string          `json:"sessionId"`
    PID       int             `json:"pid"`
    ProjectID string          `json:"projectId"`
    Workflow  string          `json:"workflow"`
    Iteration int             `json:"iteration"`
    Intent    string          `json:"intent"`
    CreatedAt string          `json:"createdAt"`
    UpdatedAt string          `json:"updatedAt"`
    Bootstrap *BootstrapState `json:"bootstrap,omitempty"`
}

// TO BE DELETED:
type ContextDelivery struct {
    GuideSentFor map[string]int `json:"guideSentFor,omitempty"`
    StacksSentAt string         `json:"stacksSentAt,omitempty"`
    ScopeLoaded  bool           `json:"scopeLoaded,omitempty"`
    BriefingFor  string         `json:"briefingFor,omitempty"`
}

// deploy is currently immediate (stateless):
var immediateWorkflows = map[string]bool{
    "debug": true, "scale": true, "configure": true, "deploy": true,
}
```

#### `internal/workflow/engine.go` (331 lines)
```go
type Engine struct {
    stateDir  string
    sessionID string
    // MISSING: environment Environment
    // MISSING: knowledge knowledge.Provider
}

func NewEngine(baseDir string) *Engine // NEEDS: env + knowledge params
```

Key methods: `BootstrapStart`, `BootstrapComplete`, `BootstrapCompletePlan`, `BootstrapSkip`, `BootstrapStatus`, `Resume`, `StoreDiscoveredEnvVars`, `Iterate`

`BootstrapStatus()` (line 280): Currently overrides guide with `resolveGuideFresh()` — this hack goes away when all guides are always fresh.

`Resume()` (line 297): Currently clears `GuideSentFor` for current step — this cleanup goes away with ContextDelivery removal.

`UpdateContextDelivery()` exists in `bootstrap_context.go` (25 lines) — entire file to be deleted.

#### `internal/workflow/bootstrap.go` (396 lines)
```go
type BootstrapState struct {
    Active            bool                `json:"active"`
    CurrentStep       int                 `json:"currentStep"`
    Steps             []BootstrapStep     `json:"steps"`
    Plan              *ServicePlan        `json:"plan,omitempty"`
    DiscoveredEnvVars map[string][]string `json:"discoveredEnvVars,omitempty"`
    Context           *ContextDelivery    `json:"context,omitempty"`  // TO BE DELETED
    Strategies        map[string]string   `json:"strategies,omitempty"`
}
```

Key methods to change:
- `BuildResponse()` (line 222): Add `env Environment, kp knowledge.Provider` params. Replace `resolveGuideWithGating` with `buildGuide`.
- `resolveGuideWithGating()` (line 264): DELETE — replace with `buildGuide()`
- `resolveGuideFresh()` (line 298): DELETE — `buildGuide()` is always fresh
- `NewBootstrapState()` (line 98): Remove Context initialization
- `ResetForIteration()` (line 114): Remove GuideSentFor clearing

#### `internal/workflow/bootstrap_guidance.go` (130 lines)
```go
// KEEP:
func ResolveGuidance(step string) string             // extracts <section> from bootstrap.md
func ResolveProgressiveGuidance(step, plan, failure)  // mode-filtered deploy sections
func BuildIterationDelta(step, iteration, plan, att)  // REWRITE: escalating recovery
func extractSection(md, name string) string           // finds <section> tags
func distinctModes(plan) map[string]bool              // collects modes from plan

// ADD:
func (b *BootstrapState) buildGuide(step, iteration, env, kp) string      // NEW: main guide assembly
func (b *BootstrapState) assembleKnowledge(step, env, kp) string          // NEW: knowledge injection
func formatEnvVarsForGuide(envVars map[string][]string) string            // NEW: env var formatting
```

#### `internal/workflow/validate.go` (249 lines)
```go
type RuntimeTarget struct {
    DevHostname   string `json:"devHostname"`
    Type          string `json:"type"`
    IsExisting    bool   `json:"isExisting,omitempty"`
    BootstrapMode string `json:"bootstrapMode,omitempty"` // Note: field name is BootstrapMode
}

func (r RuntimeTarget) EffectiveMode() string  // defaults to "standard"
func (r RuntimeTarget) StageHostname() string  // derives stage from dev (standard only)

type ServicePlan struct {
    Targets   []BootstrapTarget `json:"targets"`
    CreatedAt string            `json:"createdAt"`
    // MISSING: RuntimeBase(), DependencyTypes() helpers
}
```

#### `internal/workflow/bootstrap_steps.go` (112 lines)
6 steps defined as `stepDetails` array. Step names: `discover`, `provision`, `generate`, `deploy`, `verify`, `strategy`.

Discover guidance (line 24) includes: `"5. Load knowledge: zerops_knowledge runtime=... AND scope=infrastructure"` — this line needs to change.

#### `internal/tools/knowledge.go` (192 lines)
Dedup code to DELETE:
- `isScopeLoaded(engine)` (line 160): checks `state.Bootstrap.Context.ScopeLoaded`
- `getBriefingFor(engine)` (line 172): returns `state.Bootstrap.Context.BriefingFor`
- `buildBriefingKey(runtime, services)` (line 184): constructs dedup key
- Briefing dedup check (lines 114-117): returns stub if same briefing
- Scope dedup check (lines 88-92): skips universals if already loaded
- Scope recording (lines 96-100): sets `ScopeLoaded = true`
- Briefing recording (lines 133-137): sets `BriefingFor = key`

After cleanup: `zerops_knowledge` always delivers full content. No tracking, no stubs.

#### `internal/knowledge/sections.go` (262 lines)
`parseH2Sections(content string) map[string]string` — **NOT EXPORTED.** Need to either:
- Export as `ParseH2Sections` for use by workflow package
- OR use `Document.H2Sections()` method via `Provider.Get(uri)` which returns `*Document` with cached sections

`Document.H2Sections()` (in documents.go line 28): Already exported, caches parsed sections. **Use this path — no export needed.**

#### `internal/knowledge/engine.go` (305 lines)
```go
type Provider interface {
    List() []Resource
    Get(uri string) (*Document, error)          // Returns Document with H2Sections()
    Search(query string, limit int) []SearchResult
    GetCore() (string, error)                    // Returns themes/core.md content
    GetUniversals() (string, error)              // Returns universals content
    GetBriefing(runtime string, services []string, liveTypes []platform.ServiceStackType) (string, error)
    GetRecipe(name string) (string, error)
}
```

For guide assembly, use:
- `kp.Get("zerops://themes/core")` → `doc.H2Sections()` → specific sections
- `kp.GetBriefing(runtime, nil, nil)` → runtime guide + recipes (liveTypes=nil skips version check, which is fine — already validated at discover)
- `kp.GetBriefing("", services, nil)` → service cards + wiring

#### `internal/runtime/runtime.go` (30 lines)
```go
type Info struct {
    InContainer bool
    ServiceName string
    ServiceID   string
    ProjectID   string
}
func Detect() Info  // reads env vars, InContainer = serviceId present
```

#### `internal/server/server.go` (128 lines)
Engine creation at line ~85:
```go
var wfEngine *workflow.Engine
if cwd, err := os.Getwd(); err == nil {
    stateDir := filepath.Join(cwd, ".zcp", "state")
    wfEngine = workflow.NewEngine(stateDir)  // NEEDS: env + knowledge params
}
```

Knowledge store available as `s.store` (knowledge.Provider). Runtime info as `s.rtInfo`.

#### `cmd/zcp/main.go` (148 lines)
Runtime detection at line ~108:
```go
rtInfo := runtime.Detect()
// ... later:
srv := server.New(ctx, client, authInfo, store, logFetcher, sshDeployer, mounter, idleWaiter, rtInfo)
```

#### `internal/workflow/service_meta.go` (130 lines)
```go
type ServiceMeta struct {
    Hostname         string            `json:"hostname"`
    Type             string            `json:"type"`
    Mode             string            `json:"mode"`
    Status           string            `json:"status"`
    StageHostname    string            `json:"stageHostname,omitempty"`
    Dependencies     []string          `json:"dependencies,omitempty"`
    BootstrapSession string            `json:"bootstrapSession,omitempty"`
    BootstrappedAt   string            `json:"bootstrappedAt,omitempty"`
    Decisions        map[string]string `json:"decisions,omitempty"`
}
```

Strategy constants: `StrategyPushDev = "push-dev"`, `StrategyCICD = "ci-cd"`, `StrategyManual = "manual"`

#### `internal/workflow/router.go` (250 lines)
Pure function `Route(RouterInput) []FlowOffering` — routes based on project state + service metas. Already reads strategies from metas. Will need update for CI/CD workflow offering.

### Knowledge Content Files

| File | Size | Purpose |
|---|---|---|
| `knowledge/themes/universals.md` | 4KB | Non-negotiable platform constraints |
| `knowledge/themes/core.md` | 31KB | Full YAML schemas + rules + examples |
| `knowledge/themes/services.md` | 16KB | 14 managed service cards |
| `knowledge/themes/operations.md` | 14KB | Networking, CI/CD, scaling, decisions |
| `knowledge/runtimes/*.md` | 17 files, ~27KB | Per-runtime guides |
| `knowledge/recipes/*.md` | 29 files, ~96KB | Framework configs |
| `knowledge/guides/*.md` | 19 files, ~80KB | Operational guides |
| `knowledge/decisions/*.md` | 4 files, ~8KB | Service selection |
| `content/workflows/bootstrap.md` | 52KB | Bootstrap step guidance |
| `content/workflows/deploy.md` | 13KB | Deploy guidance |
| `content/workflows/debug.md` | 6KB | Debug guidance |
| `content/workflows/scale.md` | 4KB | Scale guidance |
| `content/workflows/configure.md` | 4KB | Configure guidance |
| `content/workflows/monitor.md` | 4KB | Monitor guidance |

### Key H2 Sections in core.md (for knowledge injection)

From `themes/core.md` — these are the sections extractable via `Document.H2Sections()`:
- `"import.yml Schema"` — full import schema with field descriptions
- `"zerops.yml Schema"` — full zerops.yml schema
- `"Rules & Pitfalls"` — 30+ NEVER/ALWAYS rules with reasons
- `"Schema Rules"` — deployFiles tilde syntax, cache architecture, public access
- `"Preprocessor Functions"` — `<@generateRandomString()>` etc.
- `"Container Universe"` — platform model intro
- `"The Two YAML Files"` — import vs zerops purpose
- `"Build/Deploy Lifecycle"` — phase ordering diagram
- `"Networking"` — L7 LB, ports, SSL
- `"Storage"` — disk, shared, object
- `"Scaling"` — vertical, horizontal, HA
- `"Immutable Decisions"` — hostname, mode, bucket
- `"Base Image Contract"` — Alpine vs Ubuntu
- `"Causal Chains"` — action → effect → root cause table
- `"Multi-Service Examples"` — import.yml examples

---

## 7. Wave 1: Container Bootstrap Completion

### 7A. Remove ContextDelivery

**Delete entirely:**
- `ContextDelivery` struct from `state.go`
- `Context *ContextDelivery` field from `BootstrapState`
- `bootstrap_context.go` file (25 lines — `UpdateContextDelivery` method)
- `resolveGuideWithGating()` from `bootstrap.go`
- `resolveGuideFresh()` from `bootstrap.go`
- Context init in `NewBootstrapState()`
- GuideSentFor clearing in `ResetForIteration()`
- GuideSentFor clearing in `Resume()`
- BootstrapStatus override hack in `engine.go`
- `isScopeLoaded()`, `getBriefingFor()`, `buildBriefingKey()` from `tools/knowledge.go`
- All dedup conditionals in `tools/knowledge.go` (scope check lines 88-100, briefing check lines 114-117, briefing recording lines 133-137)

**Result:** `zerops_knowledge` always returns full content. Every guide is fresh.

### 7B. Add Environment to Engine

**New type** in `workflow/` package:
```go
type Environment string
const (
    EnvContainer Environment = "container"
    EnvLocal     Environment = "local"
)

func DetectEnvironment(rt runtime.Info) Environment {
    if rt.InContainer {
        return EnvContainer
    }
    return EnvLocal
}
```

**Modify Engine:**
```go
type Engine struct {
    stateDir    string
    sessionID   string
    environment Environment
    knowledge   knowledge.Provider
}

func NewEngine(baseDir string, env Environment, kp knowledge.Provider) *Engine {
    return &Engine{stateDir: baseDir, environment: env, knowledge: kp}
}
```

**Update server.go:**
```go
env := workflow.DetectEnvironment(s.rtInfo)
wfEngine = workflow.NewEngine(stateDir, env, s.store)
```

### 7C. Knowledge-Aware Guide Assembly

**New method on BootstrapState:**
```go
func (b *BootstrapState) buildGuide(step string, iteration int, env Environment, kp knowledge.Provider) string {
    // 1. Iteration delta (escalating) for deploy retries
    if iteration > 0 {
        if delta := BuildIterationDelta(step, iteration, b.Plan, b.lastAttestation()); delta != "" {
            return delta
        }
    }

    // 2. Base guidance from bootstrap.md (mode-aware for deploy)
    guide := ResolveProgressiveGuidance(step, b.Plan, iteration)

    // 3. Append step-specific knowledge
    if extra := b.assembleKnowledge(step, env, kp); extra != "" {
        guide += "\n\n---\n\n" + extra
    }

    return guide
}
```

**Knowledge assembly:**
```go
func (b *BootstrapState) assembleKnowledge(step string, env Environment, kp knowledge.Provider) string {
    if b.Plan == nil || kp == nil {
        return ""
    }
    var parts []string

    switch step {
    case StepProvision:
        if doc, err := kp.Get("zerops://themes/core"); err == nil {
            sections := doc.H2Sections()
            for _, name := range []string{"import.yml Schema", "Preprocessor Functions"} {
                if s, ok := sections[name]; ok && s != "" {
                    parts = append(parts, s)
                }
            }
        }

    case StepGenerate:
        // Runtime guide
        if rt := b.Plan.RuntimeBase(); rt != "" {
            if briefing, err := kp.GetBriefing(rt, nil, nil); err == nil && briefing != "" {
                parts = append(parts, briefing)
            }
        }
        // Service wiring
        if deps := b.Plan.DependencyTypes(); len(deps) > 0 {
            if briefing, err := kp.GetBriefing("", deps, nil); err == nil && briefing != "" {
                parts = append(parts, briefing)
            }
        }
        // Discovered env vars
        if len(b.DiscoveredEnvVars) > 0 {
            parts = append(parts, formatEnvVarsForGuide(b.DiscoveredEnvVars))
        }
        // zerops.yml schema + rules
        if doc, err := kp.Get("zerops://themes/core"); err == nil {
            sections := doc.H2Sections()
            for _, name := range []string{"zerops.yml Schema", "Rules & Pitfalls"} {
                if s, ok := sections[name]; ok && s != "" {
                    parts = append(parts, s)
                }
            }
        }

    case StepDeploy:
        if doc, err := kp.Get("zerops://themes/core"); err == nil {
            sections := doc.H2Sections()
            if s, ok := sections["Schema Rules"]; ok && s != "" {
                parts = append(parts, "## Deploy Rules\n\n"+s)
            }
        }
        if len(b.DiscoveredEnvVars) > 0 {
            parts = append(parts, formatEnvVarsForGuide(b.DiscoveredEnvVars))
        }
    }

    if len(parts) == 0 {
        return ""
    }
    return strings.Join(parts, "\n\n---\n\n")
}
```

**ServicePlan helpers (add to validate.go):**
```go
func (p *ServicePlan) RuntimeBase() string {
    if p == nil || len(p.Targets) == 0 {
        return ""
    }
    base, _, _ := strings.Cut(p.Targets[0].Runtime.Type, "@")
    return base
}

func (p *ServicePlan) DependencyTypes() []string {
    if p == nil {
        return nil
    }
    seen := make(map[string]bool)
    var types []string
    for _, t := range p.Targets {
        for _, d := range t.Dependencies {
            if !seen[d.Type] {
                seen[d.Type] = true
                types = append(types, d.Type)
            }
        }
    }
    return types
}
```

**Env var formatter:**
```go
func formatEnvVarsForGuide(envVars map[string][]string) string {
    var sb strings.Builder
    sb.WriteString("## Discovered Environment Variables\n\n")
    sb.WriteString("**ONLY use these in zerops.yml envVariables. Anything else = empty at runtime.**\n\n")
    for hostname, vars := range envVars {
        sb.WriteString("**" + hostname + "**: ")
        refs := make([]string, len(vars))
        for i, v := range vars {
            refs[i] = "`${" + hostname + "_" + v + "}`"
        }
        sb.WriteString(strings.Join(refs, ", "))
        sb.WriteString("\n\n")
    }
    return sb.String()
}
```

### 7D. Wire BuildResponse

All callers of `BuildResponse()` gain two params:
```go
func (b *BootstrapState) BuildResponse(sessionID, intent string, iteration int, env Environment, kp knowledge.Provider) *BootstrapResponse
```

Inside, replace:
```go
// OLD:
resp.Current.DetailedGuide = b.resolveGuideWithGating(detail.Name, iteration)

// NEW:
resp.Current.DetailedGuide = b.buildGuide(detail.Name, iteration, env, kp)
```

Callers to update in `engine.go`:
- `BootstrapStart()` line 113
- `BootstrapComplete()` lines 133, 167
- `BootstrapCompletePlan()` line 233
- `BootstrapSkip()` line 258
- `BootstrapStatus()` line 288

Each passes `e.environment, e.knowledge`.

### 7E. Escalating Recovery

Rewrite `BuildIterationDelta()` in `bootstrap_guidance.go`:

```go
func BuildIterationDelta(step string, iteration int, plan *ServicePlan, lastAttestation string) string {
    if step != StepDeploy || iteration == 0 {
        return ""
    }
    remaining := max(maxIterations()-iteration, 0)

    var guidance string
    switch {
    case iteration <= 2:
        guidance = `DIAGNOSE: zerops_logs severity="error" since="5m"
FIX the specific error, then redeploy + verify.`

    case iteration <= 4:
        guidance = `PREVIOUS FIXES FAILED. Systematic check:
1. zerops_discover includeEnvs=true — are all env vars present and correct?
2. Does zerops.yml envVariables ONLY use discovered variable names?
3. Does the app bind 0.0.0.0 (not localhost/127.0.0.1)?
4. Is deployFiles correct? (dev MUST be [.], stage = build output)
5. Is run.ports.port matching what the app actually listens on?
6. Is run.start the RUN command (not a build command)?
Fix what's wrong, redeploy, verify.`

    default:
        guidance = `STOP. Multiple fixes failed. Present to user:
1. What you tried in each iteration
2. Current error (from zerops_logs + zerops_verify)
3. Ask: "Should I continue, or would you like to debug manually?"
Do NOT attempt another fix without user input.`
    }

    return fmt.Sprintf("ITERATION %d (remaining: %d)\n\nPREVIOUS: %s\n\n%s",
        iteration, remaining, lastAttestation, guidance)
}
```

### 7F. Transition Message

```go
func buildTransitionMessage(state *WorkflowState) string {
    if state.Bootstrap == nil || state.Bootstrap.Plan == nil {
        return "Bootstrap complete."
    }
    var sb strings.Builder
    sb.WriteString("Bootstrap complete.\n\n## Services\n\n")

    for _, t := range state.Bootstrap.Plan.Targets {
        mode := t.Runtime.EffectiveMode()
        sb.WriteString(fmt.Sprintf("- **%s** (%s, %s mode)\n", t.Runtime.DevHostname, t.Runtime.Type, mode))
        if mode == PlanModeStandard {
            sb.WriteString(fmt.Sprintf("  Stage: **%s**\n", t.Runtime.StageHostname()))
        }
        for _, d := range t.Dependencies {
            sb.WriteString(fmt.Sprintf("  - %s (%s)\n", d.Hostname, d.Type))
        }
    }

    sb.WriteString("\n## What's Next?\n\n")
    sb.WriteString("Infrastructure is ready and verified. Choose how to continue:\n\n")
    sb.WriteString("**A) Continue deploying the same way** — edit code, push, verify.\n")
    sb.WriteString("   → `zerops_workflow action=\"start\" workflow=\"deploy\"`\n\n")
    sb.WriteString("**B) Set up CI/CD** — connect git repo for automatic deployments.\n")
    sb.WriteString("   → `zerops_workflow action=\"start\" workflow=\"cicd\"`\n\n")
    sb.WriteString("**Other operations:**\n")
    sb.WriteString("- Scale: `zerops_workflow action=\"start\" workflow=\"scale\"`\n")
    sb.WriteString("- Debug: `zerops_workflow action=\"start\" workflow=\"debug\"`\n")
    sb.WriteString("- Configure: `zerops_workflow action=\"start\" workflow=\"configure\"`\n")

    return sb.String()
}
```

Wire into `handleBootstrapComplete()` — when bootstrap becomes inactive (all steps done), append transition message to response.

### 7G. Guidance Content Updates

**bootstrap_steps.go — discover step:**
Change line 24 from:
```
5. Load knowledge: zerops_knowledge runtime="{type}" services=[...] AND scope="infrastructure"
6. Submit plan via zerops_workflow action="complete" step="discover" plan=[...]
```
To:
```
5. PRESENT the plan to user for confirmation before submitting:
   "I'll set up: [services]. Mode: [standard/dev/simple]. OK?"
6. Submit confirmed plan via zerops_workflow action="complete" step="discover" plan=[...]
NOTE: Platform knowledge is delivered with each step guide automatically.
For specific frameworks: zerops_knowledge recipe="{name}"
```

**bootstrap_steps.go — generate step:**
Add to guidance: `"Platform rules, runtime knowledge, and discovered env vars are included below."`

### 7H. Summary of Wave 1 Changes

| File | Action | Net lines |
|---|---|---|
| `state.go` | Remove ContextDelivery | -6 |
| `bootstrap.go` | Remove gating+fresh, add buildGuide+assembleKnowledge+transition, update BuildResponse | +80, -60 |
| `bootstrap_guidance.go` | Rewrite BuildIterationDelta, add formatEnvVarsForGuide | +50, -20 |
| `bootstrap_context.go` | DELETE ENTIRE FILE | -25 |
| `engine.go` | Add env+knowledge fields, simplify Status/Resume, update all callers | +15, -25 |
| `validate.go` | Add RuntimeBase, DependencyTypes | +20 |
| `bootstrap_steps.go` | Update guidance text | ~10 |
| `tools/knowledge.go` | Remove all dedup code | -40 |
| `server/server.go` | Pass env+knowledge to NewEngine | +5 |
| Tests | Remove dedup tests, add knowledge injection tests | +120, -80 |
| **Total** | | **+300, -256 = net +44** |

---

## 8. Wave 2: Stateful Deploy Workflow

### Design

**3 steps:**
1. **prepare** — discover targets, validate zerops.yml, load knowledge
2. **deploy** — per-service deploy (mode-aware ordering: dev first, then stage)
3. **verify** — per-service health verification, iteration loop

**State:**
```go
type DeployState struct {
    Active    bool           `json:"active"`
    Mode      string         `json:"mode"`
    Step      string         `json:"step"`      // prepare, deploy, verify
    Targets   []DeployTarget `json:"targets"`
    Iteration int            `json:"iteration"`
}

type DeployTarget struct {
    Hostname string `json:"hostname"`
    Role     string `json:"role"`     // dev, stage, simple
    Status   string `json:"status"`   // pending, deployed, verified, failed
}
```

**Session:**
- `WorkflowState.Deploy *DeployState` — new field alongside Bootstrap
- Remove `"deploy"` from `immediateWorkflows` map

**Guide assembly:** Same pattern as bootstrap — `buildGuide()` with knowledge injection from deploy.md sections + runtime/service knowledge from store.

**deploy.md restructure:** Add `<section>` tags for step extraction:
- `<section name="deploy-prepare">`
- `<section name="deploy-execute">` (with mode subsections)
- `<section name="deploy-verify">`

**Engine methods:**
- `DeployStart(projectID, intent string) (*DeployResponse, error)`
- `DeployComplete(step, attestation string) (*DeployResponse, error)`
- `DeployStatus() (*DeployResponse, error)`

**Mode detection:** Read from service metas (written during bootstrap) or infer from project state.

**Environment-aware deploy:**
- Container: SSH-based deploy (existing mechanism)
- Local: zcli push (new mechanism — `zerops_deploy` tool gets local mode)

### Implementation Files

| File | Action |
|---|---|
| `workflow/deploy.go` | NEW: DeployState, step logic (~150 lines) |
| `workflow/deploy_guidance.go` | NEW: guide assembly for deploy (~80 lines) |
| `workflow/engine.go` | Add Deploy methods (~100 lines) |
| `workflow/state.go` | Add Deploy field, remove deploy from immediateWorkflows |
| `tools/workflow.go` | Handle deploy actions in dispatcher |
| `tools/workflow_deploy.go` | NEW: deploy-specific handlers (~100 lines) |
| `content/workflows/deploy.md` | Restructure with section tags |

**Estimate: ~500 lines new, ~100 modified**

---

## 9. Wave 3: Post-Bootstrap Transitions

### CI/CD Setup Workflow (NEW)

**Stateful workflow** with 3 steps:
1. **choose** — user picks: GitHub Actions, GitLab CI, Zerops webhook, or generic zcli
2. **configure** — generate config files, guide user through setup
3. **verify** — test push or webhook trigger, verify deploy completes

**State:**
```go
type CICDState struct {
    Active   bool   `json:"active"`
    Step     string `json:"step"`
    Provider string `json:"provider"` // github, gitlab, webhook, generic
    // ... provider-specific config
}
```

**Content:** New `content/workflows/cicd.md` with setup guides per provider.

**Knowledge:** CI/CD guide from `knowledge/guides/ci-cd.md` (93 lines today — needs expansion).

### Router Update

Add CI/CD workflow to offerings when strategy is "ci-cd" or when user requests it.

### Implementation

| File | Action |
|---|---|
| `workflow/cicd.go` | NEW: CICDState, step logic (~100 lines) |
| `workflow/engine.go` | Add CICD methods (~60 lines) |
| `workflow/state.go` | Add CICD field, add "cicd" workflow |
| `workflow/router.go` | Add cicd offering |
| `tools/workflow.go` | Handle cicd actions |
| `content/workflows/cicd.md` | NEW: CI/CD setup guidance (~200 lines) |

**Estimate: ~400 lines new**

---

## 10. Waves 4–5: Local Flow

### Wave 4: Foundation

**Preflight tool or checks:**
```go
type PreflightResult struct {
    ZCLI    bool   `json:"zcli"`    // zcli binary found
    Auth    bool   `json:"auth"`    // zcli logged in (cli.data exists + valid)
    VPN     bool   `json:"vpn"`     // VPN active (can reach services)
    Project string `json:"project"` // scoped project ID
}
```

Check at workflow start (not ZCP startup). If check fails, guide tells user how to fix.

**Local deploy via zcli push:**
Extend `zerops_deploy` tool to handle local mode:
- Detect environment
- Container: existing SSH deploy path
- Local: run `zcli push [hostname]` from working directory, poll build

**Local bootstrap sections:**
New sections in bootstrap.md for environment-specific guidance where needed.

### Wave 5: Completion

All 3 local modes fully functional:
- Local standard: dev+stage on Zerops, push from local
- Local dev-only: managed only, local development
- Local simple: stage on Zerops, push from local

### Key Architectural Points for Local

1. **Local dev service gets REAL start command** (not `zsc noop`). User won't SSH in to start it.
2. **All pushes from local.** No cross-deploy between services.
3. **VPN gives full access:** `hostname:port`, SSH — same as container.
4. **Env vars for local dev:** Design for `.env.local` generation but implement later.
5. **zerops.yml stays the same** for both environments. Stage entry is production config; dev entry differs (container: zsc noop, local: real start).

---

## 11. Wave 6: Polish

- Improve stateless workflows (scale/configure/debug/monitor) with env-specific notes
- Service metadata injection into workflow responses
- Cross-workflow transitions ("debug didn't help? try scale")
- End-to-end integration tests across all modes and environments

---

## 12. Knowledge Content Matrix

### What Gets Injected Into Each Step Guide

| Step | Knowledge injected | Source |
|---|---|---|
| **discover** | Available stacks (live) | API via StackTypeCache |
| **provision** | import.yml Schema, Preprocessor Functions | `themes/core.md` H2 sections |
| **generate** | Runtime guide, service cards + wiring, discovered env vars, zerops.yml schema + rules | `runtimes/*.md`, `themes/services.md`, session state, `themes/core.md` |
| **deploy** | Schema Rules (deployFiles/tilde), discovered env vars | `themes/core.md`, session state |
| **verify** | (none — self-sufficient) | — |
| **strategy** | (none — self-sufficient) | — |

### What Differs by Environment

| Aspect | Container | Local |
|---|---|---|
| Import template | dev + stage + managed | Depends on mode: standard=dev+stage+managed, dev=managed only, simple=stage+managed |
| Dev zerops.yml | `start: zsc noop --silent`, no healthCheck | `start: <real command>`, healthCheck present |
| Deploy guidance | SSH-based, SSHFS paths, SSH start/stop | zcli push, local paths, local process |
| Iteration | SSH kill → SSH start → curl via SSH | Local kill → local start → curl localhost |
| Prerequisites | Container exists (auto) | VPN + zcli login required |

### What Differs by Mode

| Aspect | Standard | Dev | Simple |
|---|---|---|---|
| Services created | dev + stage + managed | Container: dev + managed. Local: managed only | Container: 1 runtime + managed. Local: stage + managed |
| Deploy order | dev → verify → stage → verify | dev → verify | service → verify |
| Stage zerops.yml | Production config (buildCommands, optimized deployFiles, real start, healthCheck) | None (no stage) | N/A (single service acts as stage) |
| Post-bootstrap | Deploy or CI/CD | Deploy or expand to standard | Deploy or CI/CD |

---

## 13. Test Strategy

### Wave 1 Tests

**Delete:**
- `bootstrap_context_test.go` (entire file)
- Dedup test cases in `knowledge_test.go` (BriefingFor, ScopeLoaded assertions)
- GuideSentFor assertions in `bootstrap_test.go`
- ContextDelivery serialization tests in `state_test.go`

**Add:**
```go
// bootstrap_guidance_test.go
TestBuildGuide_NilKnowledge_ReturnBaseGuide
TestBuildGuide_Provision_ContainsImportSchema
TestBuildGuide_Generate_ContainsRuntimeGuide
TestBuildGuide_Generate_ContainsEnvVars
TestBuildGuide_Deploy_ContainsSchemaRules
TestBuildIterationDelta_Escalation_Iter1
TestBuildIterationDelta_Escalation_Iter3
TestBuildIterationDelta_Escalation_Iter5

// validate_test.go
TestRuntimeBase
TestDependencyTypes

// engine_test.go (or integration)
TestBootstrapStatus_ReturnsFreshGuideWithKnowledge
TestResume_ReturnsFreshGuideWithKnowledge
```

### Wave 2 Tests
```go
TestDeployStart_CreatesSession
TestDeployComplete_AdvancesStep
TestDeployStatus_ReturnsFreshGuide
TestDeploy_ModeAwareOrdering_StandardDevFirst
TestDeploy_Iteration_Escalating
```

### Wave 3 Tests
```go
TestCICDStart_CreatesSession
TestCICDComplete_AdvancesStep
TestRouter_IncludesCICDOffering
```

### Integration Tests
```go
TestFullBootstrap_Standard_Container
TestFullBootstrap_Dev_Container
TestFullBootstrap_Simple_Container
TestBootstrap_Then_Deploy_Handoff
TestBootstrap_StatusRecovery_WithKnowledge
```

---

## 14. Files Reference — Complete List

### Files to MODIFY

| File | Wave | Changes |
|---|---|---|
| `internal/workflow/state.go` | 1,2,3 | Remove ContextDelivery, add Deploy/CICD state, update immediateWorkflows |
| `internal/workflow/engine.go` | 1,2,3 | Add env+knowledge, deploy/cicd methods, simplify status/resume |
| `internal/workflow/bootstrap.go` | 1 | Remove gating, add buildGuide+assembleKnowledge, update BuildResponse |
| `internal/workflow/bootstrap_guidance.go` | 1 | Rewrite BuildIterationDelta, add formatEnvVarsForGuide |
| `internal/workflow/bootstrap_steps.go` | 1 | Update guidance text |
| `internal/workflow/validate.go` | 1 | Add RuntimeBase, DependencyTypes |
| `internal/workflow/router.go` | 3 | Add cicd offering |
| `internal/tools/knowledge.go` | 1 | Remove all dedup code |
| `internal/tools/workflow.go` | 2,3 | Handle deploy/cicd actions |
| `internal/server/server.go` | 1 | Pass env+knowledge to NewEngine |
| `internal/content/workflows/bootstrap.md` | 1,4 | Guidance updates, local sections |
| `internal/content/workflows/deploy.md` | 2 | Restructure with section tags |

### Files to CREATE

| File | Wave | Purpose |
|---|---|---|
| `internal/workflow/environment.go` | 1 | Environment type + detection (~15 lines) |
| `internal/workflow/deploy.go` | 2 | DeployState + step logic (~150 lines) |
| `internal/workflow/deploy_guidance.go` | 2 | Deploy guide assembly (~80 lines) |
| `internal/tools/workflow_deploy.go` | 2 | Deploy action handlers (~100 lines) |
| `internal/workflow/cicd.go` | 3 | CICDState + step logic (~100 lines) |
| `internal/content/workflows/cicd.md` | 3 | CI/CD setup guidance (~200 lines) |

### Files to DELETE

| File | Wave | Reason |
|---|---|---|
| `internal/workflow/bootstrap_context.go` | 1 | UpdateContextDelivery — no longer needed |

### zcli Reference (for Wave 4-5)

```
zcli login <token>                    # Auth with personal access token
zcli vpn up [project-id]              # WireGuard VPN to project
zcli push [hostname-or-service-id]    # Build + deploy from local
  --working-dir string                # Custom working dir (default: ./)
  --zerops-yaml-path string           # Custom zerops.yml path
  --setup string                      # Override setup name
  -g, --deploy-git-folder             # Include .git in upload
  --no-git                            # Upload without git
zcli scope project [project-id]       # Set default project
```

Token storage: `~/Library/Application Support/zerops/cli.data` (macOS), `~/.config/zerops/cli.data` (Linux)

Auth resolution in ZCP: `internal/auth/auth.go` — primary: `ZCP_API_KEY` env var, fallback: zcli `cli.data` file.

---

## Priority & Dependencies

```
Wave 1 (container bootstrap) ← START HERE, no dependencies
  ↓
Wave 2 (stateful deploy) ← reuses Wave 1 patterns (knowledge injection, env on engine)
  ↓
Wave 3 (transitions + CI/CD) ← needs Wave 2 (deploy workflow exists)
  ↓
Wave 4 (local foundation) ← needs Wave 1 architecture (Environment type)
  ↓
Wave 5 (local completion) ← needs Wave 4
  ↓
Wave 6 (polish) ← needs everything
```

**Critical path:** Wave 1 → Wave 2 → Wave 3
**Parallel path:** Wave 4 can start design during Wave 2, implementation after Wave 1

---

## Constraints & Rules

From `CLAUDE.md`:
- TDD mandatory: RED → GREEN → REFACTOR
- Table-driven tests, `testing.Short()` for long tests
- Max 350 lines per .go file
- English everywhere
- `go.sum` committed, no vendor/
- No `interface{}`/`any`, no `panic()`, always wrap errors
- Test naming: `Test{Op}_{Scenario}_{Result}`

From `CLAUDE.local.md`:
- Never add "Co-Authored-By" to commits
- Never use zcli (for ZCP development — NOT for the product)
- Never mount service filesystem (for ZCP development)
- SSH access via `ssh [service_name]` for debugging
