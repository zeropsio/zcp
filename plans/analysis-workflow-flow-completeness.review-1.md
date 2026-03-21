# Deep Review Report: Workflow Flow Completeness — Review 1

**Date**: 2026-03-21
**Reviewed version**: `docs/spec-bootstrap-deploy.md` + full workflow codebase
**Knowledge team**: kb-research (zerops-knowledge), kb-verifier (platform-verifier)
**Analysis team**: correctness, architecture, security, adversarial
**Focus**: Complete flow analysis for all workflows — steps, gates, evaluation. Identify gaps, unused code, broken implementations. Container mode only.
**Resolution method**: Evidence-based (no voting)

---

## Stage 1: Knowledge Base

### Factual Brief (kb-research)

**Bootstrap Flow (6 steps):**
- Discover: NO checker. Special plan routing via `BootstrapCompletePlan()`. `ValidateBootstrapTargets()` enforces hostnames, types, resolution, modes. Stack catalog injected into response.
- Provision: `checkProvision()` verifies service status (RUNNING/ACTIVE), type cross-check, env vars for managed deps. Stores env var NAMES in session. Writes partial ServiceMeta (no BootstrappedAt).
- Generate: `checkGenerate()` validates zerops.yml existence, setup entries, env ref validity (via `ops.ValidateEnvReferences()`), ports, deployFiles, healthCheck (simple), run.start, build cmd detection, dev deployFiles `[.]`.
- Deploy: `checkDeploy()` verifies RUNNING status + SubdomainAccess. Env vars NOT injected at deploy (D5 rule). Iteration delta replaces all guidance when iteration > 0.
- Verify: `checkVerify()` calls `ops.VerifyAll()`, filters to plan hostnames only.
- Strategy: `checkStrategy()` validates every target has valid strategy. Auto-assigns push-dev for dev/simple in `writeBootstrapOutputs()`.

**Deploy Flow (3 steps):**
- prepare→deploy→verify. ZERO checkers — attestation-only progression.
- Deploy's `buildGuide()` passes empty RuntimeType/DependencyTypes/DiscoveredEnvVars to `assembleKnowledge()`.
- `handleDeployStart()` reads ServiceMeta, rejects incomplete metas, filters to runtime only.

**CI/CD Flow (3 steps):**
- choose→configure→verify. ZERO checkers. No iteration support. No skip support.
- Skip action falls through to bootstrap skip handler — would error.
- No knowledge injection.

**Key Spec-Code Discrepancies:**
- D1: ServiceMeta fields in spec (Type, Status, Dependencies, Decisions) removed from code
- D3: Spec says 3 ServiceMeta lifecycle points, code has 2 (provision + completion)
- D7: Deploy workflow has NO step checkers despite spec implying verification
- D9: CI/CD skip would crash — no handler
- D10: Deploy knowledge injection gap — RuntimeType empty
- D11: Env var validation gap listed in spec is actually fixed in code
- D12: `isManagedNonStorage` duplicates managed prefix list

### Platform Verification Results (kb-verifier)

- **20 CONFIRMED, 3 PARTIAL, 0 REFUTED**
- Key PARTIAL findings:
  - Deploy buildGuide never passes DiscoveredEnvVars — env vars NOT injected in deploy guidance
  - BuildDeployTargets orders dev before stage within pair, but no cross-service ordering
- Key CONFIRMED:
  - Bootstrap exclusivity enforced via registry lock
  - All 5 bootstrap step checkers work as described
  - Plan validation enforces `len(targets) > 0` (managed-only gap confirmed)
  - Strategy auto-assign for dev/simple confirmed in `writeBootstrapOutputs()`
  - Deploy workflow has ZERO step checkers — `DeployComplete()` has no checker parameter

---

## Stage 2: Analysis Reports

### Correctness Analysis
**Assessment**: SOUND
- All 3 workflows logically consistent. No data flow contradictions end-to-end.
- Bootstrap 5-checker model is correct and complete. Discover has no checker by design (validation in `BootstrapCompletePlan()`).
- Deploy attestation-only is correct for operational workflows.
- CI/CD attestation-only is correct for configuration workflows.
- Env var name-vs-value 2-tier design is intentional and defensible.

### Architecture Analysis
**Assessment**: CONCERNS
- F1: engine.go 484L exceeds 350L convention (MINOR)
- F2: Duplicate `ExtractSection()` / `extractDeploySection()` — identical implementations (MAJOR)
- F3: Managed service prefix duplication between `workflow/managed_types.go` and `tools/workflow_checks.go` (MAJOR)
- F4: Deploy has 0 checkers vs bootstrap's 5 — architectural asymmetry (MAJOR)
- F5: Deploy `buildGuide()` doesn't pass RuntimeType → no runtime knowledge injection (CRITICAL)
- F6: Three different guidance assembly patterns across bootstrap/deploy/cicd (MAJOR)
- F7: CI/CD structurally incomplete — no iteration, no skip, no knowledge injection (MAJOR)
- F8: Step checkers in tools/ package instead of workflow/ — package boundary tension (MINOR)

### Security Analysis
**Assessment**: SOUND with concerns
- F1: Discover free-text attestation bypass — allows advancing without plan (details below)
- F2: TOCTOU race in Resume PID check (MAJOR — low probability)
- F3: Session file concurrent read/write — no per-session lock (MAJOR — low probability)
- F4: Discover tool exposes unmasked secrets — intentional by design (MAJOR — accepted risk)
- F5: Provision skip claim — **REFUTED by orchestrator** (provision has Skippable: false)
- F6: Session cleanup errors silent (MINOR)

### Adversarial Analysis
**Assessment**: UNSOUND (aggressive)
- C1: CI/CD skip crashes — falls through to bootstrap handler (CRITICAL)
- C2: Deploy guidance knowledge gap — RuntimeType not passed (CRITICAL)
- C3: CI/CD iteration undefined — no reset, no error (CRITICAL)
- M1: Deploy start doesn't validate metas match live API (MAJOR)
- M2: Router returns all metas when liveServices is empty (MAJOR)

---

## Stage 3: Evidence-Based Resolution

### Pre-Resolution Verification
- `git status`: Clean — no uncommitted changes
- `git log --since="30 minutes ago"`: No unexpected commits
- **READ-ONLY compliance**: All 4 analysis agents complied. No violations.

### Findings by Evidence Strength

#### VERIFIED (confirmed by KB or independent orchestrator check)

| # | Finding | Severity | Evidence | Source |
|---|---------|----------|----------|--------|
| V1 | CI/CD skip action falls through to bootstrap handler — returns confusing error | MAJOR | `workflow.go:111-115` — no CI/CD case in skip dispatch; `handleCICDSkip` does not exist (grep confirmed) | Adversarial C1, KB D9 |
| V2 | Deploy `buildGuide()` passes empty RuntimeType/DependencyTypes/DiscoveredEnvVars to `assembleKnowledge()` — no runtime knowledge injected | CRITICAL | `deploy.go:332-344` — only Step, Mode, KP passed; `guidance.go:74-80` requires RuntimeType for briefing | Architecture F5, Adversarial C2, KB D10 |
| V3 | CI/CD has no iteration support — `IterateSession()` silently skips CI/CD state, no reset, no error | MAJOR | `session.go:106-111` — only resets Bootstrap and Deploy; `cicd.go` has no `ResetForIteration()` | Adversarial C3 |
| V4 | Duplicate section extraction: `ExtractSection()` (workflow/) and `extractDeploySection()` (tools/) are byte-identical | MINOR | `bootstrap_guidance.go:129-142` vs `workflow_strategy.go:105-118` | Architecture F2 |
| V5 | Managed service prefix lists duplicated across packages | MINOR | `workflow/managed_types.go:14-18` vs `tools/workflow_checks.go:322-338` | Architecture F3, KB D12 |
| V6 | Spec ServiceMeta fields don't match code — Type, Status, Dependencies, Decisions removed | MINOR | `service_meta.go:22-29` vs spec lines 734-744 | KB D1, D3, D5 |
| V7 | Spec lists env var validation as gap — actually fixed in code | MINOR | `workflow_checks_generate.go:92-109` calls `ops.ValidateEnvReferences()` | KB D11 |
| V8 | Discover step accepts free-text attestation without plan — advances step with nil plan | MAJOR | `workflow_bootstrap.go:41,55-61` — if `len(input.Plan) == 0`, falls to free-text path; `buildStepChecker("discover")` returns nil | Security F1 |
| V9 | Deploy start doesn't validate metas against live API | MAJOR | `workflow.go:186-235` — reads metas, checks IsComplete(), but never verifies services still exist in API | Adversarial M1 |
| V10 | Three different guidance assembly patterns across workflows | MAJOR | Bootstrap: `assembleGuidance()` wrapper; Deploy: inline `resolveDeployStepGuidance()` + `assembleKnowledge()`; CI/CD: pure `resolveCICDGuidance()` with no knowledge | Architecture F6 |

#### LOGICAL (follows from verified facts)

| # | Finding | Severity | Reasoning Chain | Source |
|---|---------|----------|----------------|--------|
| L1 | If discover advances with nil plan, ALL downstream checkers return nil (skip checking) because each checks `if plan == nil { return nil, nil }` | MAJOR | V8 → `workflow_checks.go:41-42,149-150,214-215` — all checkers short-circuit on nil plan | Security F1 cascade |
| L2 | Router returning all metas when liveServices is empty could mislead routing after API failure | MINOR | `router.go:110-111` — empty liveServices = no filtering. Defensive fallback, not a bug — better to over-suggest than under-suggest | Adversarial M2 |
| L3 | CI/CD skip + iterate gaps mean CI/CD workflow is "forward-only" — must complete or reset | MINOR | V1 + V3 — no skip, no iterate means once started, only complete or reset | Combined |

#### UNVERIFIED (flagged but not confirmed)

| # | Finding | Severity | Why Unverified | Source |
|---|---------|----------|---------------|--------|
| U1 | TOCTOU race in Resume — concurrent resume calls could both succeed | LOW | Requires precise timing + PID reuse; file-based locking on registry prevents most cases but session-level locking is absent | Security F2 |
| U2 | Session file concurrent read/write could produce logical inconsistency | LOW | Atomic rename prevents corruption, but read could see pre-write state in theory | Security F3 |

### Disputed Findings

| # | Finding | Position A | Evidence A | Position B | Evidence B | Resolution |
|---|---------|-----------|-----------|-----------|-----------|------------|
| 1 | Provision can be skipped | Security F5: "agent can skip provision" | No code citation | Orchestrator check: `bootstrap_steps.go:31` — `Skippable: false` | Code shows `lookupDetail("provision").Skippable == false`, SkipStep would error | **REFUTED** — provision cannot be skipped. Security analyst wrong. |
| 2 | Deploy no-checker is a vulnerability | Architecture F4: "Missing validation gates" | Deploy has 0 checkers, bootstrap has 5 | Correctness F3: "correct by design — operational workflow" | Deploy is attestation-only because it's operational, not structural | **Design choice, not vulnerability** — deploy operates on already-validated infrastructure. BUT V2 (no knowledge injection) compounds this: agents lack both validation AND guidance. |
| 3 | Discover free-text bypass is CRITICAL | Security F1: "attestation-only path bypasses all checks" | `workflow_bootstrap.go:41,55-61` — free-text path exists | Correctness F1: "discover has no checker by design — validation in BootstrapCompletePlan()" | Plan validation is correct, but free-text path circumvents it | **MAJOR, not CRITICAL** — practical harm is "empty bootstrap" (no metas written, no services created). Not destructive. But should be blocked. |
| 4 | Strategy auto-assign exists vs validation-only | Correctness F10: "no auto-assign in checkStrategy" | `workflow_checks_strategy.go` validates only | KB-research: "auto-assign in writeBootstrapOutputs" | `bootstrap_outputs.go:28-29` auto-assigns push-dev for dev/simple at OUTPUT time, not at VALIDATION time | **Both correct** — auto-assign happens in outputs, not checker. Checker validates explicit assignment. If agent doesn't submit strategy and skips step, auto-assign still happens at completion. |

### Key Insights from Knowledge Base

1. **CI/CD is structurally immature** compared to bootstrap/deploy — no iteration, no skip, no knowledge injection, no checkers. It's a "v0.5" workflow that needs either maturation or explicit scoping documentation.

2. **Deploy guidance pipeline is broken** — the most impactful finding. `buildGuide()` doesn't pass RuntimeType or DependencyTypes, so `assembleKnowledge()` can't inject runtime briefings. Deploy agents fly blind on framework-specific guidance. This affects every deploy workflow invocation.

3. **Spec is stale in 7+ places** — ServiceMeta fields, lifecycle points, function names, env var validation gap status. The spec should be treated as approximate, not authoritative.

4. **The discover free-text path** is a design oversight, not a security vulnerability. The practical consequence is an empty bootstrap that writes nothing. But it should be blocked for correctness.

---

## Action Items

### Must Address (VERIFIED Critical + Major)

| # | Item | Evidence | Priority |
|---|------|----------|----------|
| A1 | **Fix deploy `buildGuide()` to pass RuntimeType + DependencyTypes** — deploy agents get no runtime knowledge | V2: `deploy.go:332-344` — only Step, Mode, KP passed | P0 |
| A2 | **Add CI/CD skip handler** — currently falls through to bootstrap handler, returns confusing error | V1: `workflow.go:111-115` — no cicd case | P1 |
| A3 | **Add CI/CD iterate guard** — either implement CI/CD iteration or return explicit error | V3: `session.go:106-111` — CI/CD silently ignored on iterate | P1 |
| A4 | **Block discover free-text path** — require structured plan for discover step completion | V8: `workflow_bootstrap.go:41,55-61` — free-text path bypasses plan validation | P1 |
| A5 | **Validate deploy targets against live API at deploy start** — metas could be stale if services deleted | V9: `workflow.go:186-235` — no live API check | P2 |

### Should Address (LOGICAL + VERIFIED Minor)

| # | Item | Evidence | Priority |
|---|------|----------|----------|
| B1 | Unify guidance assembly — deploy/cicd should use `assembleGuidance()` wrapper | V10: three different patterns | P2 |
| B2 | Remove duplicate `extractDeploySection()` — use `workflow.ExtractSection()` | V4: identical implementations | P3 |
| B3 | Consolidate managed service prefix lists | V5: `managed_types.go` vs `workflow_checks.go` | P3 |
| B4 | Update spec to match code reality | V6, V7: stale fields, fixed gaps | P3 |

### Investigate (UNVERIFIED but plausible)

| # | Item | Why Investigate | Priority |
|---|------|----------------|----------|
| I1 | Session-level locking for Resume TOCTOU | U1: concurrent resume theoretically possible | P4 |
| I2 | Per-session file locking for concurrent access | U2: atomic rename prevents corruption but not logical inconsistency | P4 |

---

## Change Log

| # | Section | Change | Evidence |
|---|---------|--------|----------|
| 1 | Deploy buildGuide | Must pass RuntimeType + DependencyTypes from DeployState or ServiceMeta | V2: `deploy.go:335-339` missing params; `guidance.go:74-80` requires them |
| 2 | CI/CD skip dispatch | Add `workflowCICD` case in skip handler or return explicit "not supported" | V1: `workflow.go:111-115` missing case |
| 3 | CI/CD iterate | Add `ResetForIteration()` to CICDState OR return error from iterate action | V3: `session.go:106-111` silently ignores |
| 4 | Discover completion | Require `len(input.Plan) > 0` for discover step — remove free-text path | V8: free-text advances with nil plan |
| 5 | Deploy start validation | Add live API check for target hostnames before creating session | V9: metas not validated against API |
