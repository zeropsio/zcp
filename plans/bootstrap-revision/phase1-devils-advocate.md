# Phase 1: Devil's Advocate — Bootstrap Flow Revision

This document challenges every major design decision in `analysis-bootstrap-revision.md` against the actual codebase on the `v2` branch. The goal is to surface hidden risks, unstated assumptions, and places where the analysis may be optimistic or incomplete.

---

## 1. Runtime-Centric Model: Is This the Right Abstraction?

### Challenge

The analysis proposes replacing `[]PlannedService` with `[]BootstrapTarget`, each containing a `RuntimeTarget` plus `[]Dependency`. The claim is that "the runtime is the primary unit of work."

**Counter-argument: This breaks the orthogonality that flat plans provide.** A flat plan is simple to validate, simple to serialize, and maps directly to what the Zerops API sees (individual services). The runtime-centric model introduces a tree structure where:

1. The same managed service (`db`) can appear as a dependency in multiple targets. The `SHARED` resolution adds cross-target coupling that makes validation order-dependent.
2. `IsExisting` runtimes create a mixed-mode plan where some targets need full bootstrap and others need partial updates. The generate and deploy steps must now branch per-target on `IsExisting`, adding conditional complexity to every step.
3. `Simple` mode (no stage pair) is a flag on `RuntimeTarget` that changes behavior across every step. This is a boolean that should be a type (two distinct workflow paths), not a toggle on a shared structure.

**Risk**: The current `ValidateServicePlan()` in `validate.go` (113 lines) is straightforward. The proposed `ValidateBootstrapTargets()` must handle: hostname validation for dev AND stage, type existence, resolution consistency (CREATE vs EXISTS vs SHARED), cross-target SHARED deduplication, hostname length overflow for derived stage names, Simple mode exemptions, and IsExisting mode exemptions. This is at least 3x the validation complexity, with more edge cases than the analysis acknowledges.

**What could go wrong**: Multi-target plans with mixed CREATE/EXISTS/SHARED resolutions are a combinatorial explosion. The analysis hand-waves "ValidateBootstrapTargets() replaces ValidateServicePlan()" but the actual validation logic is significantly harder. For example: Target 1 creates `db` (CREATE), Target 2 references `db` (SHARED). But what if Target 1 fails at import? Target 2's SHARED dependency is now unresolvable, but the plan was already validated as valid.

### Verdict

The abstraction is directionally correct but the analysis underestimates validation complexity. The SHARED resolution in particular introduces temporal coupling (validation at plan time vs reality at provision time) that needs explicit error handling for partial import failures.

---

## 2. "API Is Source of Truth" — Does This Actually Work?

### Challenge

Section 4.1 states: "No document should describe project state. The API is the source of truth." This led to dropping the persistent registry in favor of session-only state + per-service decision metadata + CLAUDE.md reflog.

**Counter-argument: The API provides data, not knowledge.** The API tells you "service `appdev` of type `bun@1.2` exists with status RUNNING." It does NOT tell you:
- That `appdev` and `appstage` are a dev/stage pair
- That `appdev` depends on `db` and `cache`
- That this was bootstrapped using the `hono` framework
- That SSH self-deploy was chosen
- What the intended architecture is

The analysis proposes `.zcp/services/{hostname}.json` decision metadata files to bridge this gap. But these files are:
1. **Local to the workstation** — team members on different machines won't have them.
2. **Written once at bootstrap completion** — any changes made after bootstrap (adding a dependency, changing deploy flow) are invisible.
3. **Optional** — if the file doesn't exist, the LLM starts near-blind.

**Risk**: The analysis correctly identifies that a persistent registry creates reconciliation problems. But the alternative (API + local decision files + reflog) creates a different problem: the LLM must reconstruct relationships from fragmentary hints every session. The CLAUDE.md reflog says "March 3rd: bootstrapped bun+postgres." But if the user added valkey via dashboard between sessions, the reflog is misleading — it omits a dependency. The LLM must call `zerops_discover` and reconcile the reflog against reality, which is the same reconciliation problem the analysis claims to avoid.

### Verdict

The "no persistent state" principle is sound, but the analysis overstates how clean this is. In practice, every new session still requires reconciliation (reflog hints vs API reality). The decision metadata files add a third source of potentially-stale information. The analysis should acknowledge that the "API is source of truth" principle means accepting slower session starts (full discovery every time) and occasional confusion (reflog mentions dependencies that no longer exist).

---

## 3. Step Consolidation (11 to 5): Are We Losing Safety?

### Challenge

The analysis consolidates 11 steps to 5, claiming hard checks replace the safety that granularity provided. The old `load-knowledge` step ensured knowledge was loaded before `generate-import`. The old `discover-envs` step ensured env vars were known before `generate-code`.

**Counter-argument: Granularity was a feature, not overhead.** Each old step was a checkpoint where the LLM reported status and the human could review. With 5 steps:

1. The `provision` step now does: load knowledge + generate import YAML + execute import + mount dev + discover env vars. If any sub-action fails, the error is buried in a mega-step. The LLM must debug a complex multi-action sequence instead of a single focused action.
2. The `discover` step now does: gather context + load knowledge + clarify with user + submit plan. This combines investigation (read-only, safe) with commitment (plan submission, consequential). The old separate `detect` step was safe to re-run. The new `discover` step's plan submission is a one-shot commitment.
3. The hard checks at step boundaries are proposed as replacements, but they run AFTER the work is done. The old granularity prevented bad work from starting. For example, old `load-knowledge` was a gate — you couldn't proceed to `generate-import` without it. New `provision` loads knowledge as an internal sub-step with no gate; if the LLM skips it, the hard check catches the omission only after import has already happened.

**Risk from code**: Looking at `bootstrap_steps.go` (275 lines), each of the 11 step definitions includes `Guidance`, `Tools`, and `Verification` fields. These provide targeted context per step. Consolidating to 5 steps means guidance becomes longer and more complex per step. The `extractSection()` function in `bootstrap_guidance.go` extracts section-tagged content from `bootstrap.md`. With 5 mega-sections instead of 11 focused sections, each section becomes a wall of text that reduces LLM comprehension.

**What could go wrong**: An LLM performing the `provision` step skips the "discover env vars" sub-action because the guidance is 3 pages long and the env var discovery paragraph gets lost in context. The hard check catches this, but only after the LLM has already proceeded to generate code, failed, iterated, and wasted 2-3 minutes. The old dedicated `discover-envs` step would have prevented this entirely.

### Verdict

The 11-to-5 consolidation optimizes for fewer round-trips at the cost of reduced checkpointing. The claim that "hard checks replace granularity" is partially true — hard checks catch errors but don't prevent them. The analysis should weigh whether saving 6 MCP round-trips (~6 seconds of API calls) is worth the increased risk of LLM confusion in mega-steps.

---

## 4. Hard Checks: Are They Actually Deterministic?

### Challenge

Section 7.1 claims hard checks are "strictly better" than attestations because they are "deterministic, cannot be faked." Let's examine each proposed check.

**Provision hard checks:**
- "All planned services exist (ListServices)" — deterministic, good.
- "Dev runtimes: status = RUNNING" — **non-deterministic**. A service can be RUNNING when checked but crash 2 seconds later. The check confirms a point-in-time state, not a stable state.
- "Dev services: SSHFS mount active" — **depends on local filesystem state**. SSHFS can disconnect between check and use. Also, per CLAUDE.local.md, mounting is forbidden ("NEVER mount any service filesystem"). This is a contradiction that the analysis doesn't address.
- "Each MANAGED_WITH_ENVS service has non-empty env vars" — deterministic but **insufficient**. Non-empty doesn't mean correct. A database service could have env vars from a previous failed initialization.

**Generate hard checks:**
- "zerops.yml exists on mount path" — depends on mount being active (see above).
- "Env ref full validation: ${hostname_var}" — **requires DiscoveredEnvVars to be populated**. If the provision step's env var discovery was incomplete (API timeout on one service), the generate check will validate against incomplete data, potentially passing invalid refs or rejecting valid ones.

**Deploy hard checks:**
- "Subdomain gate: subdomain MUST be enabled before verify" — **temporal dependency**. The LLM must call `zerops_subdomain` before the hard check runs. If it doesn't, the check fails with a confusing error about subdomains when the actual problem is missing a step.

**Risk**: The analysis presents hard checks as binary pass/fail, but many checks depend on preconditions that are themselves non-deterministic. The chain of dependencies (mount active -> file exists -> YAML valid -> env refs match discovered vars) means a failure at any point produces an error that may not clearly indicate the root cause.

### Verdict

Hard checks are better than attestations, but calling them "deterministic" overstates the case. They are deterministic given stable preconditions, but the preconditions are often unstable (network state, API availability, mount status). The analysis should include guidance for when hard checks produce misleading failures.

---

## 5. SSHFS Mount Dependency: A Fundamental Contradiction

### Challenge

The analysis assumes SSHFS mounts are available for dev filesystem access (provision step mounts, generate step reads/writes files on mount). However:

1. `CLAUDE.local.md` explicitly states: "NEVER mount any service filesystem. No SSHFS, no `zerops_mount`, no mount operations of any kind. Zero exceptions."
2. The analysis's provision step (section 6.3) includes: "Mount dev filesystems: `zerops_mount` for each non-IsExisting target."
3. The generate hard check validates "zerops.yml exists on mount path."
4. The bootstrap flow's fundamental assumption is that the LLM writes code to a mounted filesystem, then deploys from it.

**This is a direct contradiction.** The local development instructions forbid mounting, but the bootstrap flow requires it. Either:
- The bootstrap flow needs an alternative to SSHFS (e.g., SSH + file transfer, or deploying from local files), or
- The CLAUDE.local.md rule needs an exception for bootstrap, or
- The analysis is designed for a different deployment context than the one described in CLAUDE.local.md.

**Risk**: If the mount prohibition is real, the entire generate step (write zerops.yml + app code to dev filesystem) and the deploy step (self-deploy via SSH from mounted files) need redesign. This isn't a minor adjustment — it's a foundational assumption of the bootstrap flow.

### Verdict

This contradiction must be resolved before implementation. The analysis should explicitly state whether bootstrap is exempt from the mount prohibition, or redesign the file delivery mechanism.

---

## 6. Session Registry (H4): Over-Engineering for Early Development?

### Challenge

Section 4.7 proposes a registry pattern with `registry.json` + per-session files + flock locking + PID-based stale detection. This replaces the current singleton `zcp_state.json`.

**Counter-argument: ZCP is in early development with a single-user workflow.** The analysis itself acknowledges "No migration needed" (section 10.3) because there are no users to migrate. The registry pattern solves concurrent session corruption, but:

1. **MCP is STDIO-based** — one client, one server, one process. There's no concurrent access in the normal workflow.
2. **The PID-based stale detection** (`syscall.Kill(pid, 0)`) is Unix-specific and doesn't work on all platforms.
3. **flock on `registry.json`** adds complexity for a scenario (multiple concurrent bootstraps on the same project) that doesn't exist in practice — the MCP client is Claude Code, which runs one agent at a time.
4. **~120 lines of new code** for a problem that could be solved by a simple "if state file exists and is older than 1 hour, delete it" heuristic.

**Risk**: The registry pattern is well-designed but premature. It introduces locking, PID tracking, and multi-file state management for a single-user tool in early development. This contradicts the project's own principle: "No overengineering — solve the problem at hand."

### Verdict

Replace the registry proposal with a simpler solution: staleness timeout on the existing singleton file. If the state file is older than 24 hours or the owning PID is dead, reset it. Defer the full registry pattern to when concurrent sessions are actually needed.

---

## 7. Reflog vs Snapshot: Is Append-Only Actually Simpler?

### Challenge

The analysis chooses reflog (append-only history) over snapshot (current state), arguing "no regeneration, no reconciliation." But:

1. **Append-only accumulates cruft.** After 5 bootstraps, CLAUDE.md has 5 reflog entries, potentially with overlapping or contradictory information. The LLM must read all of them and determine which are still relevant. This IS reconciliation — just done by the LLM instead of code.
2. **Reflog entries have no expiry.** If a service is deleted via dashboard, its reflog entry persists forever. The LLM sees "March 3rd: bootstrapped bun+postgres" and must verify via API whether these still exist. For a project with many bootstrap iterations, this becomes a significant context burden.
3. **The reflog is in CLAUDE.md, which has a total token budget.** Each reflog entry is ~5-7 lines of markdown. After 10 bootstraps, that's 50-70 lines of potentially-stale history consuming context window.

**Counter-argument to the counter-argument**: The analysis correctly notes that snapshots create reconciliation complexity. But the analysis doesn't compare against a third option: **write nothing.** If the API is truly the source of truth, and every session starts with `zerops_discover`, why write anything to CLAUDE.md? The decision metadata in `.zcp/services/` already records decisions. The reflog adds information that is either (a) stale and misleading, or (b) redundant with what the API provides.

### Verdict

The reflog is a reasonable compromise, but the analysis should address the accumulation problem (cap at N entries? prune entries for deleted services?) and justify why the reflog provides value beyond what `.zcp/services/` metadata + API discovery already provide.

---

## 8. Env Var Reference Validation (C2): Scope and Timing Problems

### Challenge

The analysis proposes full `${hostname_varName}` validation: both hostname existence AND variable name existence, using `DiscoveredEnvVars` populated during provision.

**Problems:**

1. **Timing gap.** Env vars are discovered during provision. Code is generated during generate (possibly minutes later). If a managed service is reconfigured between provision and generate (unlikely but possible), the discovered vars are stale. The analysis stores var NAMES not values, so value changes are fine, but if a service is recreated, the var names could change.

2. **Scope limitation.** `DiscoveredEnvVars` only contains vars for services in the current plan's dependencies. But zerops.yml can reference env vars from ANY service in the project (e.g., an existing service not in the plan). The validation would reject valid references to services outside the plan.

3. **Case sensitivity complexity.** The analysis specifies "case-sensitive match." But Zerops env var names are not always consistent in casing across service types. For example, PostgreSQL uses `connectionString` (camelCase) while some other services might use different conventions. The LLM must match exactly, including casing, against discovered names — a strict requirement that increases failure rate for correct-intent-wrong-case references.

4. **Platform-injected vars.** Zerops injects certain variables (e.g., `hostname`) that may not appear in `GetServiceEnv()` results. If the validation only checks against API-returned env vars, it would reject references to platform-injected vars that actually work at runtime.

### Verdict

Env var validation is valuable (Zerops's silent literal-string behavior is dangerous), but the implementation needs to handle: (a) vars from services outside the plan, (b) platform-injected vars, and (c) potential staleness. The analysis should specify how these edge cases are handled, or explicitly scope the validation as "best-effort with known limitations."

---

## 9. Auto-Completion of Mechanical Steps: Hidden LLM Confusion Risk

### Challenge

The analysis proposes that mechanical steps (provision, verify) auto-complete when hard checks pass, without the LLM calling `action="complete"`. This saves round-trips.

**Problem: The LLM loses its mental model of progress.** When a step auto-completes, the LLM receives the next step's guidance in the response. But the LLM may not realize a step completed. Consider:

1. LLM calls `zerops_import` (part of provision).
2. Import succeeds. The LLM then calls `zerops_discover` to verify.
3. The discover call triggers provision's hard checks (internally). All pass. Provision auto-completes.
4. The response includes generate step guidance.
5. The LLM is confused — it was in the middle of provision and now sees generate guidance. Did provision complete? When?

**Risk**: Auto-completion is invisible to the LLM. The current 11-step model has explicit `action="complete"` calls that the LLM chooses to make, creating a clear cognitive checkpoint. Auto-completion removes this checkpoint, and the LLM must infer state transitions from response content.

**Mitigation needed**: The response must include an extremely clear signal: "Step provision AUTO-COMPLETED (all 6 checks passed). Now on step: generate." The analysis mentions `resp.Message` but doesn't specify the format clearly enough to prevent LLM confusion.

### Verdict

Auto-completion is efficient but needs an explicit, prominent completion signal in the response. The analysis should specify the exact response format for auto-completed steps, including which checks passed and that the transition was automatic.

---

## 10. Verification Model: 2-Endpoint Is Simpler but Weaker

### Challenge

The analysis eliminates `/health` and adopts a 2-endpoint model: `GET /` (any 200) and `GET /status` (JSON with connection checks).

**Problem with GET / as health signal:** `GET /` returning 200 only proves the HTTP server is responding. It does NOT prove:
- Database connections work
- Cache connections work
- The application is actually initialized (not just returning a default page)
- Static assets are served correctly

For static/nginx runtimes, the analysis proposes checking that the response body "contains hostname." This is fragile — it depends on the generated `index.html` including the hostname, which is a code generation requirement leaking into verification logic.

**Problem with skipping startup_detected for implicit webserver runtimes:** PHP-FPM + nginx starts quickly, but database migrations (common in Laravel/Symfony) can take 10+ seconds. Skipping startup detection for PHP runtimes means verify might run before migrations complete, hitting a 500 error on `/status` that's actually a timing issue, not a code issue.

### Verdict

The 2-endpoint model is a reasonable simplification, but the analysis should acknowledge its limitations. GET / is a necessary but not sufficient health signal. The `/status` endpoint is where real validation happens, and skipping it for static runtimes is correct, but the "body contains hostname" check for static runtimes is brittle and should be replaced with a simpler "200 + non-empty body" check.

---

## 11. Implementation Order: Dependencies Are Underspecified

### Challenge

Section 12 lists 15 implementation items in dependency order. But:

1. **Item 1 (BootstrapTarget types) changes the core data model.** Every subsequent item depends on it. If the type design needs revision mid-implementation (e.g., SHARED resolution proves more complex than expected), items 2-15 must be reworked.

2. **Item 6 (Hard checks) depends on items 3-5** (verify speedup, validation, batch verify) but also depends on the step consolidation in item 7. Hard checks must know the 5-step model to know which checks to run per step. The analysis says items 3-4 run in parallel, but item 6 requires both to be complete.

3. **Item 8 (Import error surfacing)** is listed as a bug fix, but it touches `ops/import.go` which is also affected by the provision step's hard checks in item 6. These should be ordered explicitly.

4. **"I1 test blast radius: delete PlannedService outright"** — this is listed as a note, not a step. Deleting `PlannedService` is the highest-risk change (it breaks all existing tests), and the analysis doesn't give it a dedicated implementation slot.

### Verdict

The implementation order is reasonable at a high level but underspecifies dependencies between items. A more detailed dependency graph would reduce the risk of mid-implementation design changes propagating through already-completed items.

---

## 12. Performance Claims: Are the Numbers Realistic?

### Challenge

Section 13 claims:
- Verify time: 75-100s to 7-10s (parallel + batch)
- Typical bootstrap: 4-6 min to 2-3 min
- Build poll: 3s initial to 1s initial

**Verify time analysis**: The claim of 7-10s assumes perfect parallelization. But `VerifyAll()` runs with "errgroup, max 5 concurrency." Each service's verification still requires API calls (log fetch) and HTTP calls (GET /, GET /status). With 5 services hitting the same project, the Zerops API may rate-limit or slow down. The realistic improvement is probably 75s to 20-30s, not 7-10s.

**Bootstrap time**: The 2-3 minute claim assumes all steps execute without iteration. But the deploy step has a max-3 iteration loop, and real-world bootstraps often need 1-2 iterations (code fixes after first deploy failure). A realistic "happy path" might be 3-4 minutes, with "typical with one iteration" being 4-5 minutes.

**Build polling**: Changing from 3s to 1s initial poll increases API calls by 3x during the first 30 seconds. For a build that takes 60+ seconds, this adds ~30 extra API calls. The analysis doesn't discuss API rate limiting or whether Zerops has documented poll rate limits.

### Verdict

The performance improvements are directionally correct but the specific numbers are optimistic. The analysis should present conservative estimates alongside the best-case numbers.

---

## Summary: Critical Issues to Resolve Before Implementation

| # | Issue | Severity | Section |
|---|-------|----------|---------|
| 1 | SSHFS mount contradiction with CLAUDE.local.md | **Blocking** | 5 |
| 2 | SHARED resolution + partial import failure handling | High | 1 |
| 3 | DiscoveredEnvVars scope (only plan dependencies, not all project services) | High | 8 |
| 4 | Auto-completion response format underspecified | Medium | 9 |
| 5 | Session registry over-engineering for single-user tool | Medium | 6 |
| 6 | Reflog accumulation / pruning not addressed | Medium | 7 |
| 7 | Hard check precondition instability | Medium | 4 |
| 8 | PlannedService deletion not given implementation slot | Medium | 11 |
| 9 | Performance estimates optimistic | Low | 12 |
| 10 | Static runtime "body contains hostname" check is brittle | Low | 10 |

---

## Recommendations

1. **Resolve the mount contradiction first.** This is a foundational assumption that affects every step.
2. **Add explicit error handling for partial import + SHARED dependencies.** The happy path is designed; the failure path is not.
3. **Scope env var validation clearly.** Document what it catches and what it misses (vars outside plan, platform-injected vars).
4. **Simplify the session registry.** Use staleness timeout on singleton file. Defer full registry to when needed.
5. **Specify auto-completion response format.** Make the step transition unmistakable in the response.
6. **Add reflog pruning strategy.** Cap entries or note that pruning is deferred.
7. **Present conservative performance estimates.** Optimistic numbers set wrong expectations.
