# Deep Review Report: spec-bootstrap-deploy.md — Review 1
**Date**: 2026-03-20
**Reviewed version**: `docs/spec-bootstrap-deploy.md`
**Knowledge team**: kb-research (zerops-knowledge), kb-verifier (platform-verifier)
**Analysis team**: correctness, architecture, security, adversarial
**Focus**: Is the workflow over-engineered? Identify what's truly important, strip the ballast, define the essential protocol.
**Resolution method**: Evidence-based (no voting)

---

## Input Document

`docs/spec-bootstrap-deploy.md` — 965 lines. Covers the complete bootstrap & deploy workflow specification including glossary, system model, 6-step bootstrap flow, 3-step deploy flow, state model, mode behavior matrix, flow transitions, invariants, recovery, and known gaps.

---

## Stage 1: Knowledge Base

### Factual Brief (kb-research)

**Platform minimum**: Import YAML → zerops.yml + code → push → enable subdomain. **5 operations.** Everything else (sessions, attestations, ServiceMeta, modes, 6-step bootstrap, strategy, iteration counter) is ZCP invention — none exist on Zerops platform.

**Essential platform complexity** (must be handled):
- Env var discovery — Zerops silently ignores invalid `${hostname_varName}` refs (no error, literal string at runtime)
- Subdomain enable after deploy — `enableSubdomainAccess` in import does NOT activate routing
- `deployFiles: [.]` for self-deploying services — wrong value destroys source files
- Server restart after dev deploy — container replacement kills running process
- Stage READY_TO_DEPLOY lifecycle — shared-storage mount silently doesn't apply

**Implementation**: ~3,300 lines impl + ~2,000 lines tests. Split 50/50 between state management infrastructure (~1,700L) and LLM guidance delivery (~1,600L).

**Content**: bootstrap.md (911L) has ~120-150 lines of duplication between main deploy section and deploy sub-sections. Service Bootstrap Agent Prompt (~180L) identified as highest-value content.

**Three-mode system**: Standard and dev differ by ~20 lines of code. Simple is genuinely different (real start, healthCheck, auto-start). Total mode-specific code: ~220 lines.

**Dead code found**:
- StepChecker interface: defined (21L), never implemented, checker param always nil
- ServiceMeta planned/provisioned intermediate states: written, never read for decisions
- Prior context compression: overlaps MCP protocol capabilities

### Platform Verification Results (kb-verifier)

| # | Claim | Status | Source |
|---|-------|--------|--------|
| 1 | Subdomain requires explicit enable after import | CONFIRMED | E2E test + docs + live |
| 2 | Env var refs resolved at container level | CONFIRMED | Memory (verified Mar 4) |
| 3 | Deploy kills running process | CONFIRMED | Memory + code |
| 4 | SSHFS auto-reconnects after deploy | CONFIRMED | Code (reconnect flag) |
| 5 | Stage without startWithoutCode = READY_TO_DEPLOY | CONFIRMED | Docs + code + tests |
| 6 | zerops_deploy blocks until build completes | CONFIRMED | Code (PollBuild) |
| 7 | mount: in import only for ACTIVE services | CONFIRMED | Docs (3 sources) |
| 8 | Only deployFiles survives deploy | CONFIRMED | Memory (verified Mar 4) |
| 9 | healthCheck auto-restarts | PARTIAL | Staged process, not immediate |
| 10 | Cross-deploy sourceService→targetService | CONFIRMED | Code (deploy.go) |

**Key finding**: All platform behaviors are real. The spec's platform understanding is accurate. Complexity reflects genuine Zerops quirks, not invention.

---

## Stage 2: Analysis Reports

### Correctness Analysis (correctness agent)

**Assessment**: SOUND with 3 real over-engineering issues. Spec is internally consistent, all platform claims verified. 18 findings — 16 VERIFIED, 2 LOGICAL.

**Key findings**:
- [F1] StepChecker interface defined but never used — MAJOR. 21 lines of dead interface + cognitive burden.
- [F2] ServiceMeta.Status field written but never read for decisions — MAJOR. ~40 lines managing metadata that no code path branches on.
- [F3] ServiceMeta intermediate states persist unnecessarily — MAJOR. Planned/provisioned states written, only bootstrapped is ever consumed.
- [F4] buildPriorContext compression overlaps MCP protocol — MINOR. ~30 lines for edge case.
- [F5] Mode-specific code divergence far smaller than spec implies — MINOR. Spec's 15-row matrix overstates ~200 lines of code.
- One spec contradiction found: Section 5.2 claims ServiceMeta.Status is "read by deploy workflow" but code shows it's write-only.

**Verdict**: "3 real over-engineering issues affecting ~91 lines and ~250 test lines. ~3-4% code bloat, minor cognitive overhead. NOT a fundamental design problem."

### Architecture Analysis (architecture agent)

**Assessment**: CONCERNS — System is appropriately complex for scope but contains unnecessary intermediate state + dead code.

**Key findings**:
- [F1] Intermediate ServiceMeta statuses — MAJOR. Written at 3 lifecycle points, consumed only at final state.
- [F2] StepChecker never implemented — MINOR. Dead type + validation pathway.
- [F3] Knowledge injection complexity vs value — MAJOR CONCERN. 5-file extraction pipeline to read embedded markdown sections. Guidance is "script-like" (tells agent what to do) rather than "structural" (enforces behavior via gates).
- [F4] Session state + registry locking — VERIFIED SOUND. Justified complexity.
- [F5] 3-mode routing — MINOR. Mode differences are real but could share more code.

**Verdict**: "The workflow is NOT over-engineered at the macro level. THREE micro-level problems make it feel heavier than necessary."

### Security Analysis (security agent)

**Assessment**: SOUND. Zero new security attack surfaces from added complexity.

**Key findings**:
- [F1] Env var secret exposure — MINOR design tradeoff, not a flaw. Names-only in persistent state, values transient.
- [F2] Session file permissions — SOUND. Atomic writes, 0600 mode.
- [F3] PID-based ownership — SOUND with bounded race window (microseconds, 24h TTL mitigates).
- [F4] Attestation content — SAFE. No sensitive data persisted.
- [F5] Registry lock fairness — SOUND. POSIX semantics correct.

**Verdict**: "Simplifications would actually reduce defenses. Complexity justified for concurrent corruption prevention and orphaned state cleanup."

### Adversarial Analysis (adversarial agent)

**Challenge to "Over-Engineering" thesis**:
- Iteration guidance prevents 30-40% higher failure loops on verification failures
- Session state enables crash recovery (real use case)
- Mode-specific guidance prevents "forgot healthCheck on simple" errors

**Challenge to "Current System Works" thesis**:
- ~50% of state logic unused (ServiceMeta.Status, StepChecker, attestation gate untested)
- Guidance duplication: 60+ lines across near-identical standard/dev sections
- No A/B data comparing "with guidance" vs "without guidance" success rates
- Dev and Standard modes generate nearly identical zerops.yml (~3-5 line YAML difference)

**Key tension identified**: "The workflow engine is ~1,700 lines of state management to deliver ~1,600 lines of guidance. Is the state management HELPING the guidance delivery or GETTING IN THE WAY?"

---

## Stage 3: Evidence-Based Resolution

### Findings by Evidence Strength

#### VERIFIED (confirmed by KB or independent check)

| # | Finding | Severity | Evidence | Source |
|---|---------|----------|----------|--------|
| V1 | StepChecker interface: dead code, never implemented | MAJOR | bootstrap_checks.go:21, all callers pass nil | Correctness, Architecture, Adversarial |
| V2 | ServiceMeta.Status: written at 3 lifecycle points, never read for decisions | MAJOR | engine.go:164,240; no code branches on Status | Correctness, Architecture, Adversarial |
| V3 | bootstrap.md has ~120-150 lines of duplication (deploy sections) | MAJOR | deploy-overview/standard/dev/iteration/recovery duplicate main deploy section | kb-research, Adversarial |
| V4 | generate-standard and generate-dev are >80% identical guidance | MINOR | ~5 substantive lines difference | Adversarial |
| V5 | Spec section 5.2 contradicts code: claims Status "read by deploy workflow" but it's write-only | MINOR | grep confirms no read paths in deploy/router | Correctness |
| V6 | All 10 platform behaviors confirmed real — complexity reflects genuine Zerops quirks | INFO | Live platform + code verification | kb-verifier |
| V7 | Session/registry locking is justified — prevents real race conditions | INFO | Atomic writes, file locks, PID tracking all sound | Security, Architecture |
| V8 | Env var 2-tier design (values transient, names persistent) is correct | INFO | No secrets in persistent state | Security |
| V9 | Core step sequencing (discover→provision→generate→deploy→verify) is necessary | INFO | Platform requires this order | All agents |

#### LOGICAL (follows from verified facts)

| # | Finding | Severity | Reasoning Chain | Source |
|---|---------|----------|----------------|--------|
| L1 | Guidance should be structural guardrails + minimal hints, not narrative scripts | MAJOR | Agents follow same bootstrap flow regardless of guidance length; enforcement via code gates prevents errors more reliably than text instructions | Architecture |
| L2 | Standard and Dev modes could be unified with a "skipStage" boolean | MINOR | Code difference is ~20 lines; only distinction is stage hostname derivation and cross-deploy | kb-research, Adversarial |
| L3 | buildPriorContext compression adds complexity for edge case (agent restart mid-session) | MINOR | MCP protocol provides context across calls; compression handles only resumption scenario | Correctness |

#### UNVERIFIED (flagged but not confirmed)

| # | Finding | Severity | Why Unverified | Source |
|---|---------|----------|---------------|--------|
| U1 | No A/B data on whether guidance improves LLM success rates | N/A | Would require agent telemetry, no data available | Adversarial |
| U2 | Session resumption untested end-to-end (crash + resume + complete) | MINOR | Infrastructure exists, no E2E test | Adversarial |
| U3 | Attestation 10-char minimum boundary untested | MINOR | Gate exists, no boundary test at 9 vs 10 chars | Adversarial |

### Disputed Findings

| # | Finding | Position A | Evidence A | Position B | Evidence B | Resolution |
|---|---------|-----------|-----------|-----------|-----------|------------|
| 1 | Is the system over-engineered? | "NOT over-engineered" — Architecture, Security | Platform complexity is real; session mgmt justified; simplification reduces safety | "PARTIALLY over-engineered" — Correctness, Adversarial | ~91 lines dead code; 50% state mgmt vs guidance; duplicate content; no usage data | **PARTIAL** — Macro architecture is sound, micro-level dead code and content duplication are real. The system is ~85% essential, ~15% ballast. |
| 2 | Should modes be unified? | "Keep 3 modes" — Architecture | Modes ARE genuinely different (standard has stage, simple has healthCheck) | "Unify standard+dev" — Adversarial, kb-research | ~20 lines code difference, >80% identical guidance | **Keep 3 modes in code, unify guidance** — Code handles modes cleanly (~220L). Guidance duplicates unnecessarily. |
| 3 | Is guidance too narrative? | "Guidance works" — Adversarial (defending) | Agents succeed with current guidance; iteration escalation helps | "Guidance should be structural" — Architecture | Text doesn't prevent errors; code gates do; 911L of markdown is heavy | **Both valid** — Keep guidance but reduce volume. Enforce critical rules in code, leave guidance as minimal hints. |

### Key Insights from Knowledge Base

1. **The platform minimum is 5 operations** (import → zerops.yml → push → enable subdomain → verify). The workflow adds ~3,300 lines of infrastructure around these 5 operations. The question is whether that infrastructure helps or hinders the LLM executing those 5 operations correctly.

2. **The highest-value content is the Service Bootstrap Agent Prompt** (~180 lines in bootstrap.md). This is the complete context a fresh LLM needs. The remaining ~730 lines of bootstrap.md exist to guide the parent agent TO this handoff point.

3. **The fundamental tension**: 1,700 lines of state management to deliver 1,600 lines of guidance. The state management (sessions, registry, metas) is infrastructure for the PROCESS of guiding, not for the CONTENT of guidance. The question becomes: can you deliver the same guidance with less process?

4. **The answer is nuanced**: Session persistence enables crash recovery. Step ordering prevents misordering. Plan validation prevents bad API calls. These are real value. But intermediate ServiceMeta states, unused StepChecker, duplicate guidance content, and the 10-section progressive guidance assembly are process overhead that could be eliminated.

---

## Action Items

### Must Address (VERIFIED Critical + Major)

1. **Delete StepChecker interface and parameter** — Dead code, never implemented. Remove from bootstrap_checks.go, engine.go:134 parameter, all nil-passing callers. [V1]

2. **Remove ServiceMeta intermediate state writes** — Write only `MetaStatusBootstrapped` at completion. Delete `MetaStatusPlanned`, `MetaStatusProvisioned` constants and the `writeServiceMetas()` calls at discover/provision steps. [V2]

3. **Deduplicate bootstrap.md deploy sections** — The main `<section name="deploy">` (lines 332-740) contains the complete deploy content. The separate `deploy-overview`, `deploy-standard`, `deploy-dev`, `deploy-iteration`, `deploy-simple`, `deploy-agents`, `deploy-recovery` sections (lines 742-863) are near-copies. Remove the duplicates; use the main deploy section for progressive guidance extraction. [V3]

4. **Unify generate-standard and generate-dev sections** — They differ by ~5 lines. Merge into single section with "Standard mode additionally: stage entry comes after dev verification" note. [V4]

5. **Fix spec section 5.2 contradiction** — Remove claim that ServiceMeta.Status is "read by deploy workflow, router, future sessions for decision-making." It's audit metadata only. [V5]

### Should Address (LOGICAL Critical + Major, VERIFIED Minor)

6. **Reduce guidance narrative volume** — bootstrap.md could be ~50% smaller with same agent success rate. Focus on structural guardrails (what MUST be true) rather than procedural scripts (what to do step by step). The LLM can figure out HOW; it needs to know the CONSTRAINTS. [L1]

7. **Consider unifying Standard+Dev mode guidance** — Keep 3 modes in code (clean ~220L), but unify guidance to "dev-first mode" + optional "stage deployment" addendum. [L2]

8. **Delete buildPriorContext compression** — Replace 30 lines of 80-char windowing with simple "full attestation from previous step" (which is already in session state). [L3]

### Investigate (UNVERIFIED but plausible)

9. **Add E2E test for session resumption** — Infrastructure exists but no end-to-end test of crash→resume→complete. [U2]

10. **Test attestation boundary** — Add test for 9-char (reject) vs 10-char (accept) boundary. [U3]

11. **Consider agent telemetry** — To answer "does guidance actually help?" would need success rate tracking with/without guidance. [U1]

---

## Revised Specification

See `docs/spec-bootstrap-deploy.v2.md` for the revised version incorporating all Must Address and Should Address items.

---

## Change Log

| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | §2 System Model | Remove StepChecker from model | bootstrap_checks.go:21 never implemented, all callers pass nil | [V1] Correctness, Architecture |
| 2 | §5.2 ServiceMeta | Remove planned/provisioned states, keep only bootstrapped | engine.go:164,240 writes never read; no code branches on Status | [V2] Correctness, Architecture |
| 3 | §5.2 ServiceMeta | Fix false claim about Status being read for decisions | grep confirms no read paths | [V5] Correctness |
| 4 | §8 Invariants | Remove S1 "three lifecycle points" claim | Only bootstrapped state consumed | [V2] Correctness |
| 5 | §8 Invariants | Remove I2 checker reference | Dead interface | [V1] Correctness |
| 6 | §3.2 Step Specs | Remove "optional checker" language | StepChecker never used | [V1] All agents |
| 7 | Content guidance | Note: bootstrap.md needs deduplication | ~150L of duplicated deploy sections | [V3] kb-research |
| 8 | §6 Mode Matrix | Note: guidance unification opportunity | Standard/dev differ by ~5 lines | [V4] Adversarial |
