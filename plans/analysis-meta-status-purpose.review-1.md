# Deep Review Report: analysis-meta-status-purpose — Review 1

**Date**: 2026-03-20
**Reviewed version**: `plans/analysis-meta-status-purpose.md`
**Knowledge team**: kb-research (zerops-knowledge), kb-verifier (platform-verifier)
**Analysis team**: correctness, architecture, security, adversarial
**Focus**: How meta Status is used in workflows, what it could do, and what meta should store to avoid being a second source of truth
**Resolution method**: Evidence-based (no voting)

---

## Stage 1: Knowledge Base

### Factual Brief (kb-research)

**Status WRITES** (3 points):
| When | Value | Code Location |
|------|-------|---------------|
| Plan step completed | `MetaStatusPlanned` ("planned") | `engine.go:240` |
| Provision step completed | `MetaStatusProvisioned` ("provisioned") | `engine.go:164` |
| Bootstrap completed | `MetaStatusBootstrapped` ("bootstrapped") | `bootstrap_outputs.go:35,57` |

`MetaStatusDeployed` ("deployed"): defined at `service_meta.go:24`, NEVER written anywhere.

**Status READS** (0 decision points):
- `service_meta.go:44-46` — backward-compat normalization only
- `service_context.go:28-29` — cosmetic text rendering in agent guidance
- Test files — validation only

**Fields that DO drive decisions**: Hostname, Type, Mode, StageHostname, Dependencies, Decisions["deployStrategy"]

**Fields that are write-only**: Status, BootstrappedAt, BootstrapSession

**Meta stores what API doesn't know**: Mode, StageHostname, Dependencies, Decisions, BootstrapSession

**API stores what meta doesn't know**: serviceId, operational status (ACTIVE), containers, resources, envs, subdomain, appVersionId

### Platform Verification Results (kb-verifier)

| Claim | Result | Evidence |
|-------|--------|----------|
| API service lifecycle states | CONFIRMED | API returns single `status: "ACTIVE"`, no multi-phase lifecycle |
| planned/provisioned/deployed/bootstrapped from API? | CONFIRMED local-only | Zero counterpart in any API response |
| Meta file persistence | CONFIRMED local-only | Never sent to API, no sync mechanism |
| What API knows that meta doesn't | CONFIRMED | serviceId, containers, resources, envs, subdomain, appVersionId |
| What meta knows that API doesn't | CONFIRMED | mode, stageHostname, dependencies, decisions, bootstrapSession |

**Key insight**: API and meta operate in **completely separate domains**. API = operational state (is it running?). Meta = bootstrap decisions (how was it set up?). They are orthogonal, not duplicative.

---

## Stage 2: Analysis Reports

### Correctness Analysis
**Assessment**: CONCERNS (8/9 verified)

Key findings:
- **[F1]** Status has 0 decision-point reads — CRITICAL
- **[F2]** Intermediate Status values always overwritten by bootstrapped — MAJOR
- **[F3]** Status orthogonal to actual bootstrap progress (progress tracked by session steps, not meta) — MAJOR
- **[F4]** MetaStatusDeployed never written — MAJOR (misleading API surface)
- **[F5]** Decisions field correctly designed and actively used — VERIFIED positive
- **[F6]** Meta correctly records ZCP knowledge the API can't provide — VERIFIED positive
- **[F7]** Overwrite pattern violates "historical records" comment — MAJOR

### Architecture Analysis
**Assessment**: CONCERNS (3 critical, 2 major)

Key findings:
- **[C1]** Layering violation: session state persisted as service identity — CRITICAL
- **[C2]** Type field redundant with API — CRITICAL (DISPUTED by adversarial)
- **[C3]** Status contradicts API contract — CRITICAL
- **[M1]** Package boundary: tools/ can write metas directly — MAJOR
- Proposed minimal schema: Hostname, Mode, StageHostname, Dependencies, Decisions only

### Security Analysis
**Assessment**: SOUND (20+ locations verified)

Key findings:
- **[F1]** Path traversal NOT vulnerable (hostname regex validation) — VERIFIED safe
- **[F2]** Meta file input validation gaps (no Status enum, no Decisions schema) — MINOR
- **[F3]** File tampering: contained impact (0o600 perms, strategy lookup bounded) — MAJOR but contained
- **[F4]** No secrets stored in meta — VERIFIED safe
- **[F5]** No checksums needed for local-only files — VERIFIED acceptable
- **[F6]** Graceful degradation on missing/corrupted metas — VERIFIED good design

### Adversarial Analysis
**Assessment**: SOUND with critical corrections

Key challenges:
- **[C1]** Status "dead code" — CONFIRMED accurate, removal approved
- **[C2]** Type field NOT redundant — **CRITICAL CORRECTION**: `deploy.go:163` reads `m.Type` for `svcCtx.RuntimeType`, used for knowledge injection in deploy guidance. Architecture's removal proposal would break deploy path.
- **[C3]** BootstrapSession/BootstrappedAt: unused but intentional audit design
- **[C4]** "Layering violation" claim FALSE — session and meta are orthogonal, not coupled
- **[S1]** service_context.go:28-29 rendering is agent guidance input, not "just cosmetic" — removing Status changes what agents see

---

## Stage 3: Evidence-Based Resolution

### Findings by Evidence Strength

#### VERIFIED (confirmed by KB or independent check)

| # | Finding | Severity | Evidence | Source |
|---|---------|----------|----------|--------|
| V1 | Status field has 0 decision-point reads | CRITICAL | Grep all *.go: only cosmetic reads at service_context.go:28-29 and normalization at service_meta.go:44-46 | Correctness F1, KB-FACT |
| V2 | MetaStatusDeployed defined but never written | MAJOR | service_meta.go:24 definition, 0 write sites | Correctness F4, KB-FACT |
| V3 | Intermediate Status values (planned, provisioned) always overwritten by bootstrapped | MAJOR | engine.go:240→164→171 call chain; writeBootstrapOutputs unconditionally writes bootstrapped | Correctness F2 |
| V4 | Type field IS actively read in deploy path | CRITICAL | deploy.go:163 reads m.Type for svcCtx.RuntimeType; deploy.go:184 reads dep meta Type for DependencyTypes | Adversarial C2, orchestrator-verified |
| V5 | Decisions["deployStrategy"] is the primary decision driver | VERIFIED | router.go:197 (routing), deploy_guidance.go:26 (guidance), service_context.go:31 (rendering) | Correctness F5 |
| V6 | API and meta are orthogonal domains | VERIFIED | API returns operational status (ACTIVE); meta stores bootstrap decisions (mode, strategy). Zero overlap. | KB-PLATFORM |
| V7 | Meta is secure: no path traversal, no secrets, graceful degradation | VERIFIED | hostname regex validation, 0o600 perms, nil-nil return on missing, atomic write | Security F1-F6 |
| V8 | BootstrapSession and BootstrappedAt are never read | MAJOR | 0 read sites in production code | Adversarial C3, grep verification |

#### LOGICAL (follows from verified facts)

| # | Finding | Severity | Reasoning Chain | Source |
|---|---------|----------|----------------|--------|
| L1 | Intermediate meta writes are vestigial | MAJOR | Written at plan/provision steps but overwritten at bootstrap completion; no code reads intermediate values for crash recovery; session state already tracks step progress | Correctness F2+F3, Adversarial S2 |
| L2 | "Historical records, NOT state" comment contradicts overwrite behavior | MINOR | If historical, should accumulate; if transient, shouldn't persist to disk; current design is neither | Correctness F7 |
| L3 | Removing Status from BuildServiceContextSummary changes agent-visible text | MINOR | Agents currently see "— bootstrapped" in service summaries; removing changes guidance output but no logic depends on it | Adversarial S1 |
| L4 | Architecture's "layering violation" claim is overstated | MINOR | Session and meta are orthogonal stores; meta is written at step boundaries as a checkpoint, not as live session state | Adversarial S4 |

#### UNVERIFIED (flagged but not confirmed)

| # | Finding | Severity | Why Unverified | Source |
|---|---------|----------|---------------|--------|
| U1 | External systems may monitor intermediate meta files during bootstrap | LOW | No evidence of CI/CD reading .zcp/services/*.json, but cannot rule out | Correctness, Adversarial |
| U2 | Agents may rely on Status text in guidance for decision-making | LOW | Status appears in deploy guidance but no evidence agents parse it | Adversarial S1 |

### Disputed Findings

| # | Finding | Position A | Evidence A | Position B | Evidence B | Resolution |
|---|---------|-----------|-----------|-----------|-----------|------------|
| D1 | Type field redundancy | Architecture: "redundant with API, remove" | API has Type info | Adversarial: "actively read, removal breaks deploy" | deploy.go:163 reads m.Type | **Adversarial wins** — orchestrator verified deploy.go:163. Type is load-bearing. |
| D2 | Layering violation | Architecture: "session state in persistent store" | Intermediate Status writes during session | Adversarial: "orthogonal stores, correct separation" | Meta written at step boundaries, not bidirectional | **Adversarial wins** — intermediate writes are checkpoints, not session leakage. The overwrite pattern is poor design but not a layering violation. |
| D3 | BootstrapSession/BootstrappedAt removal | Architecture: "audit, move to reflog" | 0 production reads | Adversarial: "intentional audit design, defer" | Written explicitly at bootstrap completion | **Defer** — low cost to keep, provides audit trail. Remove only if schema minimization becomes urgent. |

### Key Insights from Knowledge Base

1. **Meta and API are orthogonal, not duplicative.** Meta stores ZCP's bootstrap decisions (mode, stage pairing, dependencies, deploy strategy). API stores operational state (running, resources, envs). The user's concern about "creating another source of truth" is unfounded for most meta fields — they record information the API doesn't have.

2. **Status is the ONE field that creates confusion.** It's the only meta field that looks like it should track state but doesn't. The API has its own `status: "ACTIVE"`, and meta has `status: "bootstrapped"` — two unrelated meanings using the same word. This is the root of the "second source of truth" concern.

3. **The Decisions map is the proven pattern.** It's typed (constant keys), persisted, and read for routing + guidance. Any new ZCP-specific metadata should follow this pattern rather than adding new top-level fields.

4. **Type is a bridge between bootstrap and deploy.** The deploy workflow is stateless per-call and gets ALL service context from metas. Type enables knowledge injection (recipe suggestions, framework-specific guidance). This is a legitimate use of meta as a persistent store.

---

## Action Items

### Must Address (VERIFIED Critical + Major)

1. **Remove Status field and all 4 constants** — Status has 0 decision reads, MetaStatusDeployed is never written, intermediate values are always overwritten. Remove: constants (service_meta.go:20-26), field (service_meta.go:34), normalizeServiceMeta function (service_meta.go:43-47), rendering (service_context.go:28-30), all writes in engine.go and bootstrap_outputs.go, test assertions. [V1, V2, V3]

2. **Remove intermediate meta writes** — `writeServiceMetas()` calls at engine.go:164 (provision) and engine.go:240 (plan) write metas that are unconditionally overwritten by `writeBootstrapOutputs()`. Remove both calls. Only write metas once at bootstrap completion. [V3, L1]

3. **Remove `writeServiceMetas()` function entirely** — With intermediate writes removed and Status field gone, this function has no callers. Delete `bootstrap_outputs.go:80-117`. [V3, L1]

### Should Address (LOGICAL + VERIFIED Minor)

4. **Update ServiceMeta comment** — Change "historical records, NOT state" to explicitly document what meta IS: "ZCP's persistent record of bootstrap decisions. Not operational state — the API is the source of truth for service status. Meta stores mode, pairing, dependencies, and deploy strategy that the API doesn't track." [L2]

5. **Keep Type field** — Despite architecture's removal proposal, Type is actively read in `deploy.go:163` for knowledge injection. Removing would break deploy workflow. Document: "Type is cached from bootstrap for deploy-time knowledge injection (no API call needed)." [V4, D1]

6. **Keep BootstrapSession and BootstrappedAt** — Low cost, provides audit trail. But document their purpose explicitly since they have 0 current reads. [V8, D3]

### Investigate (UNVERIFIED but plausible)

7. **Check if external systems read intermediate meta files** — Grep CI/CD configs and monitoring for `.zcp/services/*.json` reads during active bootstrap. If found, intermediate writes may need to be preserved. [U1]

8. **Consider adding Status-like signal to Decisions map** — If agents benefit from knowing "bootstrap completed" vs "in progress," add `Decisions["bootstrapComplete"] = "true"` instead of a separate Status field. This extends the proven Decisions pattern. [U2, L3]

---

## Revised Analysis Document

Based on findings, here is the revised understanding of what meta should be:

### ServiceMeta: ZCP's Bootstrap Decision Store

**Purpose**: Persistent record of decisions made during bootstrap that the Zerops API doesn't track. NOT operational state — the API is the source of truth for "is this service running?"

**Schema (after cleanup)**:

```go
// ServiceMeta records bootstrap decisions for a service.
// These are ZCP's persistent knowledge — the API doesn't track mode,
// stage pairing, dependencies, or deploy strategy.
// The API is the source of truth for operational state (running, resources, envs).
type ServiceMeta struct {
    Hostname         string            `json:"hostname"`          // Identity key, immutable
    Type             string            `json:"type"`              // Cached for deploy-time knowledge injection
    Mode             string            `json:"mode,omitempty"`    // dev/standard/simple (ZCP-only concept)
    StageHostname    string            `json:"stageHostname,omitempty"` // Dev→stage pairing (ZCP-only)
    Dependencies     []string          `json:"dependencies,omitempty"`  // Co-created services
    BootstrapSession string            `json:"bootstrapSession"`  // Audit: which session created this
    BootstrappedAt   string            `json:"bootstrappedAt"`    // Audit: when bootstrap completed
    Decisions        map[string]string `json:"decisions,omitempty"` // Deploy strategy + future decisions
}
```

**Removed**: `Status` field and all 4 `MetaStatus*` constants. Status was written 3 times but never read for decisions. The overwrite pattern (planned → provisioned → bootstrapped) made intermediate values ephemeral despite being written to persistent files. The Decisions map is the correct mechanism for recording ZCP-specific metadata that drives behavior.

**Write policy**: Meta is written ONCE at bootstrap completion (`writeBootstrapOutputs`). No intermediate writes during active session — session state tracks step progress. Post-bootstrap updates allowed only for Decisions (e.g., strategy changes via `handleStrategy`).

**Read consumers**:
- **Router** (`router.go`): Reads Decisions for workflow offerings
- **Deploy** (`deploy.go`): Reads Mode, Type, StageHostname, Dependencies for target construction + knowledge injection
- **Guidance** (`deploy_guidance.go`): Reads Decisions for strategy-specific guidance
- **Context** (`service_context.go`): Renders all fields for agent guidance summaries
- **System prompt** (`instructions.go`): Passes metas to router for startup offerings
- **Strategy** (`workflow_strategy.go`): Reads/writes Decisions for post-bootstrap strategy changes
- **Delete** (`delete.go`): Cleanup on service deletion

### What Meta is NOT

- **NOT operational state** — don't add fields like "isRunning", "lastDeployTime", "containerCount". API provides these.
- **NOT session state** — don't write intermediate progress. Session files track step completion.
- **NOT a cache of API data** — except Type, which is explicitly cached for offline deploy-time use.

### Extending Meta in the Future

New ZCP-specific decisions should be added to the `Decisions` map with new constant keys, following the `DecisionDeployStrategy` pattern. This avoids schema bloat and keeps the struct stable. Only add new top-level fields if the information is structurally different from a key-value decision (e.g., a list like Dependencies).

---

## Change Log

| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | Status field | REMOVE — 0 decision reads, intermediate writes overwritten | Grep: 0 control-flow reads; engine.go:171 overwrites all | V1, V2, V3 from Correctness |
| 2 | Intermediate writes | REMOVE — writeServiceMetas calls at plan/provision steps | engine.go:164,240 write values that bootstrap_outputs.go:35,57 overwrites | V3, L1 from Correctness |
| 3 | writeServiceMetas function | REMOVE — no remaining callers after intermediate write removal | bootstrap_outputs.go:80-117 | L1 |
| 4 | Type field | KEEP — actively read at deploy.go:163 for knowledge injection | deploy.go:163-164 reads m.Type for svcCtx.RuntimeType | V4, D1 from Adversarial |
| 5 | BootstrapSession/At | KEEP — low-cost audit trail, 0 reads but intentional design | Written at bootstrap_outputs.go:36,61 | V8, D3 |
| 6 | ServiceMeta comment | UPDATE — clarify purpose as "bootstrap decision store" | Current comment says "historical records, NOT state" which is vague | L2 |
| 7 | Meta write policy | DOCUMENT — write once at completion, no intermediate writes | Derived from removing intermediate writes | L1 |
