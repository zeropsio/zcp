# Review Report: plan-local-dev-flow.md — Review 2
**Date**: 2026-03-26
**Reviewed version**: `plans/plan-local-dev-flow.md` (Draft v2)
**Agents**: kb (zerops-knowledge), filemap (Explore), primary (Explore), adversarial (Explore)
**Complexity**: Deep (ultrathink, 4 agents)
**Focus**: Deep audit — verify every claim against codebase, check consistency with container mode, find fundamental design flaws

## Evidence Summary
| Agent | Findings | Verified | Unverified | Downgrades |
|-------|----------|----------|------------|------------|
| Primary | 14 | 14 | 0 | 0 |
| Adversarial | 4 | 4 | 0 | 0 |
| Orchestrator | 3 | 3 | 0 | 0 |

**Overall**: CONCERNS — plan is architecturally sound but has 2 CRITICAL design flaws that would cause bugs if implemented as written.

---

## Knowledge Base

### VPN (VERIFIED from docs + live)
- VPN provides network access, NOT env vars
- Both `hostname` and `hostname.zerops` work over VPN (live-verified macOS 2026-03-26)
- DNS search domain handles plain hostname resolution

### Integration Tokens (VERIFIED + LOGICAL)
- Three scope levels: full, read, custom per-project (docs)
- `ListProjects` returning exactly 1 for single-project token: LOGICAL

### zcli push (VERIFIED from docs)
- Accepts positional hostname argument
- Flags: `--working-dir`, `-g` confirmed
- Can push from local machine

### Container Lifecycle (VERIFIED)
- Deploy = new container, files lost
- Restart/reload = same container, files survive
- `${hostname_varName}` resolved at container level, not API

### Object Storage (PARTIALLY VERIFIED)
- Docs: "accessible remotely over internet"
- Exact apiUrl format auto-generated, not specified
- TLS test to generic endpoint failed — needs E2E

### wg-quick/sudo (UNVERIFIED)
- Zerops docs: "administrator privileges may be required" for daemon install
- wg-quick calling sudo internally: UNVERIFIED (WireGuard implementation detail)
- Sudoers approach: UNVERIFIED

---

## Agent Reports

### Primary Analyst

**Assessment**: SOUND PROPOSAL WITH IMPLEMENTATION GAPS

All code references verified accurate. Line numbers match. Function signatures confirmed. The primary classified 12 findings as "UNIMPLEMENTED" — expected for a plan document. Key verified facts:

- `Environment` type exists in `environment.go:6-10` with `EnvContainer`/`EnvLocal`
- `ServiceMeta` struct at `service_meta.go:22-29` has 6 fields (no Environment yet — correct)
- `deployRoleFromMode` at `deploy.go:164-176` correctly returns `DeployRoleSimple` for mode=standard + stageHostname="" (plan claim VERIFIED)
- `BuildDeployTargets` at `deploy.go:122-162` creates single target for local meta (VERIFIED)
- `filterStaleMetas` at `router.go:66-81` checks `live[m.Hostname]` — appstage survives (VERIFIED)
- `buildPrepareGuide` at `deploy_guidance.go:25` HAS env parameter with container branch (VERIFIED)
- `buildDeployGuide` at `deploy_guidance.go:76` does NOT have env parameter (VERIFIED)

### Adversarial Analyst

Key challenge: bootstrap `buildGuide` at `bootstrap_guide_assembly.go:13` receives `_ Environment` — parameter exists but is **intentionally ignored**. Plan says "add env parameter" but it already exists as dead code. The plan should say "activate the ignored env parameter" and propagate to GuidanceParams.

Confirmed: `deployRoleFromMode` logic is correct for the local scenario.

---

## Evidence-Based Resolution

### CRITICAL Concerns

#### [C1] Strategy Lookup Key Mismatch in writeBootstrapOutputs — CRITICAL

**Evidence**: `bootstrap_outputs.go:23-27` + plan lines 166-186

Current code:
```go
hostname := target.Runtime.DevHostname       // "appdev"
strategy := state.Bootstrap.Strategies[hostname]  // Strategies["appdev"] → "push-dev" ✓
```

Plan's proposed change adds an environment branch between lines 23 and 27:
```go
hostname := target.Runtime.DevHostname       // "appdev"
stageHostname := target.Runtime.StageHostname()

if e.environment == EnvLocal && stageHostname != "" {
    hostname = stageHostname    // hostname becomes "appstage"
    stageHostname = ""
}
// ... (implicitly includes strategy lookup)
strategy := state.Bootstrap.Strategies[hostname]  // Strategies["appstage"] → "" ← BUG!
```

The Strategies map is keyed by `DevHostname` (agent sends `strategies={"appdev": "push-dev"}`). After the hostname override, the lookup key is "appstage" which doesn't exist in the map → **strategy is lost, ServiceMeta.DeployStrategy = ""**.

**Root cause**: The plan reuses `hostname` variable for two distinct purposes: (1) strategy lookup key (must be DevHostname), (2) ServiceMeta hostname (must be StageHostname in local mode). The override happens before the lookup.

**By-design fix**: Separate the two concerns:
```go
devHostname := target.Runtime.DevHostname
strategy := state.Bootstrap.Strategies[devHostname]  // lookup by DevHostname ALWAYS

metaHostname := devHostname
stageHostname := target.Runtime.StageHostname()
if e.environment == EnvLocal && stageHostname != "" {
    metaHostname = stageHostname
    stageHostname = ""
}

meta := &ServiceMeta{
    Hostname:       metaHostname,
    StageHostname:  stageHostname,
    DeployStrategy: strategy,
    // ...
}
```

**Impact**: Without fix, deploy workflow in local mode cannot determine strategy → blocks all post-bootstrap deploy flows.

---

#### [C2] BuildTransitionMessage References Non-Existent Service in Local Mode — CRITICAL

**Evidence**: `bootstrap_guide_assembly.go:79-103`

```go
// Line 81:
sb.WriteString(fmt.Sprintf("- **%s** (%s, %s mode)\n", t.Runtime.DevHostname, ...))
// Line 98:
hostname := t.Runtime.DevHostname
current := strategies[hostname]
```

In local mode, `DevHostname` = "appdev" which was never created on Zerops. The transition message:
- Shows "appdev" as a service name (confusing — service doesn't exist)
- Strategy display works (Strategies map correctly keyed by DevHostname)
- Deploy workflow hints reference "appdev" instead of "appstage"

**Root cause**: `BuildTransitionMessage` reads directly from `plan.Targets` which use DevHostname. Plan says "plan format unchanged" (D11) but doesn't account for the transition message needing env-aware display names.

**By-design fix**: `BuildTransitionMessage` needs environment parameter. In local mode, display StageHostname instead of DevHostname:
```go
func BuildTransitionMessage(state *WorkflowState, env Environment) string {
    for _, t := range targets {
        displayName := t.Runtime.DevHostname
        if env == EnvLocal {
            displayName = t.Runtime.StageHostname()
        }
        // use displayName for display, DevHostname for strategy lookup
    }
}
```

**Impact**: User sees non-existent service name in post-bootstrap summary. Agent may attempt operations on "appdev" which will fail.

---

### MAJOR Concerns

#### [M1] Bootstrap buildGuide Ignores Environment — Already Has Dead Parameter

**Evidence**: `bootstrap_guide_assembly.go:13`

```go
func (b *BootstrapState) buildGuide(step string, iteration int, _ Environment, kp knowledge.Provider) string {
```

The `_ Environment` parameter is already present but intentionally ignored. The plan (section 12.2, lines 648-672) says "Add env Environment parameter to ResolveProgressiveGuidance" — but the env already flows to `buildGuide`, it just dies there. GuidanceParams (guidance.go:10-22) has NO `Env` field.

**Root cause**: Plan misidentifies what exists. The plumbing from Engine to buildGuide is done; the gap is:
1. Rename `_ Environment` to `env Environment`
2. Add `Env Environment` to GuidanceParams struct
3. Pass env through assembleGuidance → resolveStaticGuidance → ResolveProgressiveGuidance

**By-design fix**: Plan should reference the existing dead parameter and describe activating it, not adding a new one. Implementation is simpler than described — no caller changes needed above buildGuide.

---

#### [M2] buildDeployGuide Missing env Parameter — Asymmetric Design

**Evidence**: `deploy_guidance.go:76` vs `deploy_guidance.go:25`

- `buildPrepareGuide(state *DeployState, env Environment)` — HAS env, uses it at line 56
- `buildDeployGuide(state *DeployState, iteration int)` — NO env parameter

Plan correctly identifies this (section 12.3, line 714). The deploy guidance system is half-env-aware: prepare step is, deploy step isn't. The plan's proposed fix is correct.

**Root cause**: Partial implementation during prior work. buildPrepareGuide got env when SSHFS-specific content was added; buildDeployGuide was not updated.

---

#### [M3] writeProvisionMetas Has Same Strategy Pattern Risk

**Evidence**: `bootstrap_outputs.go:56-72`

```go
for _, target := range state.Bootstrap.Plan.Targets {
    meta := &ServiceMeta{
        Hostname: target.Runtime.DevHostname,  // line 63
        // ...
    }
}
```

Plan says "Same change in writeProvisionMetas()" (line 188). writeProvisionMetas doesn't do strategy lookup (no DeployStrategy at provision time), so the C1 bug doesn't apply here. However, the meta filename will be "appstage.json" in local mode. If bootstrap fails and retries, the meta from a prior attempt with "appdev" hostname could coexist with a new "appstage" meta. filterStaleMetas would clean "appdev" (doesn't exist on Zerops), but this deserves a test case.

---

### MINOR Concerns

#### [N1] Plan Line References Slightly Off

- Plan says "server.go:108-110" for deploy registration. Actual: line 109 (`RegisterDeploy`). Close enough.
- Plan says "deploy.go:170-174" for deployRoleFromMode. Actual: lines 164-176. The logic description is correct.
- Plan says "router.go:76" for filterStaleMetas. Actual: lines 66-81. Function starts at 66.

None of these affect correctness — the descriptions match the actual code.

#### [N2] Plan Section 9.3: CanAutoVPN Detection

```go
canAutoVPN := exec.Command("sudo", "-n", "wg-quick", "--help").Run() == nil
```

This tests if sudo + wg-quick works passwordlessly. But `wg-quick --help` may not be a valid invocation (wg-quick expects subcommands like `up`/`down`). A safer probe would be `sudo -n true` to test passwordless sudo, then check wg-quick separately. [UNVERIFIED — needs actual wg-quick testing]

#### [N3] .env File Security

Plan generates `.env` with actual passwords (section 6 step 3, lines 304-325). Plan correctly notes `.gitignore` mitigation (line 301) and header warning (line 306-308). No additional concern — standard practice.

---

## Recommendations (max 7, evidence-backed)

| # | Recommendation | Priority | Evidence | Effort |
|---|----------------|----------|----------|--------|
| R1 | Fix strategy lookup: separate devHostname (strategy key) from metaHostname (ServiceMeta) in writeBootstrapOutputs | P0 | bootstrap_outputs.go:23-27 strategy lookup depends on hostname variable | ~5L |
| R2 | Add env parameter to BuildTransitionMessage; display StageHostname in local mode | P0 | bootstrap_guide_assembly.go:79-103 references DevHostname | ~15L |
| R3 | Activate dead `_ Environment` in buildGuide; add Env to GuidanceParams | P1 | bootstrap_guide_assembly.go:13 already receives env | ~10L |
| R4 | Add env parameter to buildDeployGuide to match buildPrepareGuide | P1 | deploy_guidance.go:76 vs :25 asymmetry | ~20L |
| R5 | Add test case for provision meta cleanup on retry (appdev → appstage transition) | P2 | bootstrap_outputs.go:56-72 meta filename changes per env | ~20L |
| R6 | Clarify plan text: buildGuide already receives env (dead param), not "add env parameter" | P2 | bootstrap_guide_assembly.go:13 `_ Environment` | 0L (doc fix) |
| R7 | E2E verify CanAutoVPN detection with actual wg-quick (--help may not be valid) | P3 | Plan section 9.3 | ~2h |

---

## Revised Version

See `plans/plan-local-dev-flow.v3.md` — incorporates VERIFIED + LOGICAL concerns only.

Key changes from v2:
1. **Section 3.6**: Separated `devHostname` (strategy key) from `metaHostname` (ServiceMeta hostname) — fixes C1
2. **Section 3.6**: Added note about BuildTransitionMessage env-awareness — fixes C2
3. **Section 12.2**: Corrected description — buildGuide already receives `_ Environment`, needs activation not addition — fixes M1
4. **Section 12.3**: Confirmed buildDeployGuide needs env parameter — M2 unchanged
5. **Section 12.7**: Updated files-to-modify list to include bootstrap_guide_assembly.go

---

## Change Log
| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | 3.6 writeBootstrapOutputs | Separate strategy lookup key from meta hostname | bootstrap_outputs.go:23-27 — strategy keyed by DevHostname | Orchestrator |
| 2 | 3.6 (new) | Add BuildTransitionMessage env-awareness requirement | bootstrap_guide_assembly.go:79-103 — uses DevHostname | Adversarial + Orchestrator |
| 3 | 12.2 | Correct: buildGuide already receives `_ Environment`, activate it | bootstrap_guide_assembly.go:13 | Adversarial |
| 4 | 12.7 | Add bootstrap_guide_assembly.go to files-to-modify | bootstrap_guide_assembly.go:13 | Adversarial |
