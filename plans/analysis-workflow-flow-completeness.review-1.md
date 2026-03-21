# Deep Review Report: Workflow Flow Completeness — Review 1
**Date**: 2026-03-21
**Reviewed version**: `docs/spec-bootstrap-deploy.md`
**Knowledge team**: kb-research (zerops-knowledge), kb-verifier (platform-verifier)
**Analysis team**: correctness, architecture, security, adversarial
**Focus**: Complete flow analysis for all workflows (bootstrap, deploy, CI/CD, immediate). Gate/checker evaluation. Gaps, unused code, non-functional parts.
**Resolution method**: Evidence-based (no voting)

---

## Stage 1: Knowledge Base

### Factual Brief (kb-research)

**Documentation:**
- bootstrap.md: 13 `<section>` tags (discover, provision, generate, generate-standard, generate-dev, generate-simple, deploy, deploy-agents, deploy-recovery, verify, close, plus inline content)
- deploy.md: 10 sections (deploy-prepare, deploy-execute-overview/standard/dev/simple, deploy-iteration, deploy-verify, deploy-push-dev, deploy-ci-cd, deploy-manual)
- cicd.md: 7 sections (cicd-choose, cicd-configure-github/gitlab/webhook/generic, cicd-verify)
- debug.md, scale.md, configure.md: exist as flat markdown (no sections)
- monitor.md: exists (145L) but NOT in immediateWorkflows map — inaccessible
- core.md (zerops://themes/core): 471L, contains import.yml schema, zerops.yml schema, Rules & Pitfalls, Schema Rules

**Codebase:**
- Step checkers: discover=nil, provision=real, generate=real, deploy=real, close=nil
- Bootstrap iteration: resets indices 2-3 (generate, deploy), preserves 0-1 and 4
- Deploy iteration: resets indices 1+ (deploy, verify), preserves prepare(0)
- Env vars: bootstrap generate=yes, bootstrap deploy=no; deploy workflow deploy=yes
- handleStrategy ignores engine param — operates on ServiceMeta files directly
- BootstrapCompletePlan passes nil liveServices — CREATE/EXISTS validation skipped
- checkGenerate: looks at projectRoot/{hostname}/ then projectRoot/

### Platform Verification Results (kb-verifier)

| Claim | Result | Evidence |
|-------|--------|----------|
| Service status values | PARTIAL | Only "ACTIVE" observed; RUNNING/READY_TO_DEPLOY/NEW untestable without imports |
| Subdomain two-step activation | PARTIAL | MCP tool docs confirm pattern; live test needs import |
| Current project state | CONFIRMED | zcpx service, ACTIVE, zcp@1 type |
| Workflow tests | CONFIRMED | All pass (0.618s) |
| Tools tests | CONFIRMED | All pass (0.435s) |
| Registry lock path | CONFIRMED | `.registry.lock` in stateDir |
| Session file path | CONFIRMED | `sessions/{id}.json` |
| ServiceMeta file path | CONFIRMED | `services/{hostname}.json` |

---

## Stage 2: Analysis Reports

### Correctness Analysis
**Assessment**: SOUND — 4 MINOR findings
- All core flows verified end-to-end against code
- Step progression strictly linear, enforced by CompleteStep name-matching
- Checker enforcement verified: checker runs BEFORE step advances; failure keeps step in_progress
- Managed-only fast path works: empty plan.Targets allows skipping generate/deploy/close
- Deploy strategy gate: conversational (not error) when strategy missing
- CI/CD strategy gate: hard error when no ci-cd strategy services
- Iteration logic: bootstrap resets 2-3, deploy resets 1+, both verified
- Session cleanup: outputs written BEFORE session deleted (correct ordering)
- Env var injection timing: generate-only for bootstrap, deploy-step for deploy workflow

### Architecture Analysis
**Assessment**: SOUND — 3 MINOR findings
- F1: engine.go 489L exceeds 350L convention (justified — cohesive orchestration methods)
- F2: monitor.md exists but inaccessible via workflow actions (dead content)
- F3: WorkflowState allows simultaneous workflow fields (permissive design, no practical risk)
- Dependency direction correct throughout: tools→workflow→platform
- Checkers correctly placed in tools/ (need I/O access), types in workflow/
- Duplication between bootstrap/deploy handlers below extraction threshold
- API surface (WorkflowInput) well-designed with optional fields

### Security Analysis
**Assessment**: SOUND — 2 MINOR findings
- F1: Session/ServiceMeta files world-readable (no secrets stored — names only)
- F2: Strategy handler creates ServiceMeta for non-existent hostnames (rejected downstream at deploy)
- Attestation injection: SAFE (JSON-escaped, never evaluated)
- Reflog markdown injection: cosmetic only, no code execution
- Registry locking: SOUND (POSIX flock, all mutations locked)
- PID ownership: acceptable risk with 24h TTL
- Path traversal: prevented by hostname validation ([a-z0-9] only)

### Adversarial Analysis
**Assessment**: CONCERNS — 8 findings raised, 3 verified valid after cross-verification

**Findings raised by adversarial agent:**

| # | Finding | Adversarial Severity | Cross-verification Result |
|---|---------|---------------------|--------------------------|
| F1 | monitor.md orphaned (not in immediateWorkflows) | MAJOR | CONFIRMED — dead content |
| F2 | checkGenerate SSHFS path simulation | MAJOR | CONFIRMED but non-issue — graceful fallback design |
| F3 | handleStrategy bypasses Engine.BootstrapStoreStrategies | MINOR | CONFIRMED — intentional design choice |
| F4 | Iteration on managed-only bootstrap | unverified | REFUTED — validateConditionalSkip prevents re-running skipped steps |
| F5 | nil liveServices/liveTypes in BootstrapCompletePlan | CRITICAL | PARTIALLY REFUTED — liveTypes IS provided; only liveServices is nil (by design) |
| F6 | "Strict linearity" claim is wrong because of skip | MAJOR | REFUTED — skipping IS linear (forward-only progression) |
| F7 | Deploy iteration behavior undocumented in spec | MINOR | CONFIRMED — deploy.go:253-272 resets indices 1+ but spec doesn't document this |
| F8 | `<section name="verify">` in bootstrap.md without verify step | unverified | DISPUTED — architecture agent claimed section doesn't exist, but **orchestrator confirmed it DOES exist at line 778** |

**Dispute Resolution on F8:**
- Architecture agent's rebuttal claimed no `<section name="verify">` exists (incomplete grep)
- Orchestrator directly verified: bootstrap.md line 778 has `<section name="verify">` containing Verification Protocol table (3 checks: deploy result, subdomain activation, full verification)
- No code in the bootstrap flow calls `ExtractSection(md, "verify")`
- Close step uses hardcoded `closeGuidance` constant, not section extraction
- **VERDICT: F8 CONFIRMED — the verify section IS orphaned dead content**

---

## Stage 3: Evidence-Based Resolution

### Findings by Evidence Strength

#### VERIFIED (confirmed by KB or independent check)

| # | Finding | Severity | Evidence | Source |
|---|---------|----------|----------|--------|
| 1 | bootstrap.md `<section name="verify">` is orphaned — never extracted by any code | MAJOR | bootstrap.md:778 has section; grep: no `ExtractSection(md, "verify")` in bootstrap flow; close step uses hardcoded constant | Orchestrator + Adversarial F8 |
| 2 | monitor.md exists but not in immediateWorkflows map — dead content | MINOR | state.go:32-34 defines only debug/scale/configure | Architecture F2, Adversarial F1 |
| 3 | BootstrapCompletePlan always passes nil for liveServices — CREATE/EXISTS validation skipped | MINOR | workflow_bootstrap.go:42 hardcodes nil; liveTypes IS passed | Adversarial F5 (partially refuted) |
| 4 | BootstrapStoreStrategies unused by any tool handler — only in tests | MINOR | grep: only in engine.go definition and bootstrap_outputs_test.go | Adversarial F3 |
| 5 | engine.go 489L exceeds 350L convention | MINOR | wc -l output (489 lines) | Architecture F1 |
| 6 | handleStrategy creates ServiceMeta for non-existent hostnames | MINOR | workflow_strategy.go:50-53 creates empty meta if nil | Security F2 |
| 7 | Deploy iteration behavior not documented in spec | MINOR | deploy.go:253-272 resets indices 1+ (deploy+verify), preserves prepare; spec §7.3 only documents bootstrap iteration | Adversarial F7 |

#### LOGICAL (follows from verified facts)

| # | Finding | Severity | Reasoning Chain | Source |
|---|---------|----------|----------------|--------|
| 8 | Verification protocol content (3-check table, subdomain rules) in bootstrap.md is dead — agents don't receive it during bootstrap | MAJOR | verify section exists at line 778 → no step named "verify" → deploy step extracts "deploy" section only → close uses hardcoded constant → verify section unreachable | Orchestrator |
| 9 | Deploy step guidance in bootstrap includes inline verification mentions but misses the structured protocol table | MINOR | deploy section has inline verify mentions → but structured 3-check protocol in separate verify section → only deploy section extracted | KB-research A1 |

#### UNVERIFIED (flagged but not confirmed)

| # | Finding | Severity | Why Unverified | Source |
|---|---------|----------|---------------|--------|
| 10 | checkGenerate path: projectRoot/{hostname}/ may not match SSHFS mount path in container | UNKNOWN | Depends on where .zcp/ lives relative to SSHFS mounts on zcpx | KB-research B10, Adversarial F2 |
| 11 | WorkflowState allows simultaneous Bootstrap+Deploy+CICD fields — no structural enforcement | LOW | Engine lifecycle likely prevents this; no test demonstrates failure | Architecture F3 |
| 12 | No test covers managed-only iterate scenario | LOW | Logic is sound (skipped steps stay skipped), but no explicit test | Adversarial F4 |

### Disputed Findings

| # | Finding | Position A | Evidence A | Position B | Evidence B | Resolution |
|---|---------|-----------|-----------|-----------|-----------|------------|
| 1 | bootstrap.md verify section exists? | Adversarial: exists, orphaned | bootstrap.md:778 `<section name="verify">` | Architecture rebuttal: "no verify section found" | Incomplete grep output | **Adversarial WINS** — orchestrator confirmed section at line 778 |
| 2 | Managed-only iteration is broken? | Adversarial: generates/deploy restart with no state | Speculative reasoning | Correctness + Architecture: validateConditionalSkip prevents this | bootstrap.go:315-327 | **Rebuttal WINS** — managed-only steps re-skip correctly |
| 3 | Strict linearity broken by skip? | Adversarial: skipping = not linear | Semantic argument | All agents: skip = forward progression = still linear | CompleteStep enforces order | **Rebuttal WINS** — skipping is compatible with linearity |

### Key Insights from Knowledge Base

1. **The verify section in bootstrap.md is a significant content gap**: 20+ lines of structured verification protocol (3 checks: deploy result, subdomain activation, full verify; managed service skip rules) that agents NEVER see. The deploy section has inline verification mentions, but the structured table is in the orphaned verify section.

2. **Two strategy storage paths exist with no production caller for one**: BootstrapStoreStrategies stores in session state (used by writeBootstrapOutputs), while handleStrategy writes directly to ServiceMeta files. Since strategy is always set AFTER bootstrap, only the direct path matters.

3. **Plan validation is intentionally lenient at submission**: nil liveServices means CREATE/EXISTS checks are skipped. liveTypes IS provided for type validation. This is documented but creates delayed feedback for CREATE/EXISTS errors.

4. **Deploy iteration is undocumented in spec**: Bootstrap iteration is thoroughly documented (§7.3), but deploy iteration (resets steps 1-2, preserves prepare, resets target statuses) has no spec coverage.

---

## Action Items

### Must Address (VERIFIED Major)

1. **Fix orphaned verify section in bootstrap.md** — Either:
   - (a) Merge verify section content into the deploy section (so agents receive it during deploy step guidance), OR
   - (b) Have `ResolveProgressiveGuidance` also extract the "verify" section when assembling deploy step guidance
   - The 3-check verification protocol table and managed service skip rules are valuable agent guidance currently lost.

2. **Remove or activate monitor.md** — Either:
   - (a) Add `"monitor": true` to immediateWorkflows map in state.go, OR
   - (b) Delete monitor.md and remove from test expectations

### Should Address (VERIFIED Minor)

3. **Pass liveServices to BootstrapCompletePlan** — Load services list from API (`client.ListServices`) and pass to ValidateBootstrapTargets for CREATE/EXISTS validation at plan submission time, giving agents immediate feedback

4. **Remove BootstrapStoreStrategies from engine** — Unused by any tool handler; only tests call it. Strategy is set post-bootstrap via handleStrategy → ServiceMeta files directly. Remove method + update tests to use the production path.

5. **Validate hostname existence in handleStrategy** — Before creating ServiceMeta for a hostname, verify the hostname exists in existing metas or live API. Prevents phantom ServiceMeta files.

6. **Document deploy iteration in spec** — Add deploy iteration behavior to spec §7.3: resets steps 1-2 (deploy+verify), preserves prepare(0), resets all target statuses to pending.

### Investigate (UNVERIFIED but plausible)

7. **checkGenerate path in container mode** — Verify that `projectRoot/{hostname}/` matches the actual SSHFS mount base when running on zcpx.

8. **Add test for managed-only iterate** — Logic is sound but no test covers managed-only session calling action=iterate.

---

## Change Log

| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | bootstrap.md verify section | Orphaned content — needs integration into deploy guidance | bootstrap.md:778 has section; no code extracts it | Orchestrator + Adversarial |
| 2 | immediateWorkflows map | monitor.md dead — needs activation or removal | state.go:32-34 missing "monitor" | Architecture + Adversarial |
| 3 | BootstrapCompletePlan | nil liveServices = skipped CREATE/EXISTS validation | workflow_bootstrap.go:42 | Adversarial (partially) |
| 4 | BootstrapStoreStrategies | Unused in production code paths | grep: only in tests | Adversarial |
| 5 | handleStrategy hostname check | Missing existence validation | workflow_strategy.go:50-53 | Security |
| 6 | spec §7.3 deploy iteration | Undocumented behavior | deploy.go:253-272 | Adversarial |
