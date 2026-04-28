# Audit — Bootstrap & Develop Workflows — Team Plan

> **Status:** plan only. Seven agents will be dispatched in parallel; each produces a standalone findings brief. Final consolidation follows in `docs/audit-bootstrap-develop-findings.md`.

## Goal

Find every latent bug, contradiction, complexity smell, and spec/code drift across the bootstrap and develop workflows, across all four modes (standard / dev / simple / managed-only) and both environments (container / local), and across all three deploy strategies (push-dev / push-git / manual).

The target is problems invisible in happy-path testing — state-machine corner cases, routing mismatches, knowledge-injection gaps, lifecycle race conditions, compaction-survival failures, hostname-inversion edge cases, and adoption-path surprises. LLM-only development has no human peer review, so anything self-consistent in tests but inconsistent across specs must be surfaced.

## Mental Model (the 3-dimensional grid)

Every bootstrap/develop code path is an instantiation of three independent axes. The audit must verify coherence across ALL cells:

| Axis | Values | Impact |
|---|---|---|
| Environment | container, local | Hostname inversion, SSHFS vs direct FS, `.env` bridge, `zcli push` vs SSH deploy |
| Mode | standard, dev, simple, managed-only | Pair creation, guidance selection, role derivation, verify scope |
| Strategy | push-dev, push-git, manual (unset) | Briefing content, close instructions, CI/CD offering |

2 × 4 × 3 = **24 canonical cells**, each with a distinct guidance assembly. A bug can exist in one cell and not others.

## State-Layer Contract (the refresher)

Three layers — each is authoritative for its domain, each has its own lifecycle:

1. **Infrastructure state** (filesystem, survives process restart)
   - `ServiceMeta` per hostname at `.zcp/state/services/{hostname}.json` — `Mode`, `StageHostname`, `CloseDeployMode`, `CloseDeployModeConfirmed`, `GitPushState`, `RemoteURL`, `BuildIntegration`, `BootstrapSession`, `BootstrappedAt`, `FirstDeployedAt`.
   - Partial meta (`BootstrappedAt == ""`) signals in-progress bootstrap → hostname lock.
   - Registry at `.zcp/state/registry.json` (flock-protected) → single source of session ownership.
   - Bootstrap/Recipe session state at `.zcp/state/sessions/{sessionID}.json`.

2. **Work state** (per-PID, dies with process)
   - `WorkSession` at `.zcp/state/work/{pid}.json`.
   - Records deploys/verifies per hostname (capped at 10 each).
   - `CleanStaleWorkSessions` scans for dead-PID files at engine boot.

3. **Action state** (none — tools are stateless)
   - Every MCP tool call is a fresh operation. No state lives across calls beyond what was written to the filesystem.

## Invariants Under Test

These come from `spec-workflows.md`, `spec-work-session.md`, and `spec-knowledge-distribution.md`. Any violation is a bug regardless of whether tests pass.

- **E1–E6** — environment routing (hostname inversion, SSHFS-only-in-container, local mounting blocked)
- **B1–B10** — bootstrap state machine (step order, checker-is-gate, skip rules, partial-meta lock, adoption fast path, IsAllExisting idempotency)
- **D0–D10** — develop flow (work session creation, auto-adopt on empty metas, strategy resolution at briefing time, compaction survival)
- **W1–W6** — work session (per-PID, auto-close on dep+verify, cleanup on PID death, idle threshold)
- **S1–S4** — strategy (user-confirmed vs default, per-service granularity, transition surfaces strategy prompt)
- **O1–O6** — orphans (dead-PID cleanup, stale meta pruning, orphaned bootstrap without metas, hostname lock release)
- **KD-01 … KD-19** — knowledge distribution (INJECT vs POINT, information-asymmetry rules, mode/env guidance layering)

## Team Structure — Seven Parallel Agents

Each agent is self-contained: own entry file list, own spec anchors, own invariants, own evidence requirements. Each produces a findings brief under `docs/audit-reports/agent-{X}.md` with fields `finding`, `severity`, `env`, `mode`, `strategy`, `evidence`, `spec-anchor`, `recommended-action`.

### Agent A — Bootstrap State Machine Correctness

- **Scope:** 5-step lifecycle, checker-as-gate, skip rules, iteration cycle, step advancement, completion writes.
- **Entry files:** `internal/workflow/engine.go`, `bootstrap.go`, `bootstrap_steps.go`, `bootstrap_checks.go`, `bootstrap_outputs.go`, `validate.go`, `session.go`, `registry.go`, `internal/tools/workflow_bootstrap.go`, `workflow.go`, `workflow_checks*.go`.
- **Spec anchors:** `spec-workflows.md §2`, `spec-bootstrap-flow-current.md §6–9, §11`.
- **Invariants:** B1–B10, O1–O6.
- **Focus questions:**
  - Can the state machine advance past a failing checker under any path?
  - Does `ResetForIteration()` + `writeProvisionMetas()` double-create or orphan metas on rapid iterate?
  - Does `checkHostnameLocks` reject only when a DIFFERENT live session owns an incomplete meta — or are there gaps?
  - Does `SkipStep` honor `IsAllExisting()` correctly for mixed plans?
  - What happens when `BootstrapComplete("close")` runs but `writeBootstrapOutputs` fails partway (some metas written, some not)?
  - Are dead-PID sessions reliably classified on startup, and does `MigrateRemoveLegacyWorkState` + `CleanStaleWorkSessions` compose correctly?
  - Does `CompleteStep(name, attestation)` permit running in parallel with `SkipStep(name, reason)` on the same PID?
  - Does `BootstrapResponse.Current.DetailedGuide` stay consistent after reset?
  - Can checker skip-result (`nil`) be confused with pass?

### Agent B — Develop + Work Session Lifecycle

- **Scope:** briefing assembly, WorkSession create/resume/refuse-different-intent, deploy/verify recording, auto-close, `SuggestNext`, compaction-survival block, workflow_develop auto-adoption.
- **Entry files:** `internal/workflow/work_session.go`, `work_session_hint.go`, `briefing.go`, `internal/tools/workflow_develop.go`, `internal/tools/workflow.go` (close/status handlers), `internal/ops/deploy.go` (for record hooks), deploy tool handlers that call `RecordDeployAttempt` / `RecordVerifyAttempt`.
- **Spec anchors:** `spec-work-session.md` entire doc, `spec-workflows.md §4, §7`.
- **Invariants:** D0–D10, W1–W6.
- **Focus questions:**
  - Is the PID-scoped session truly single-writer, or can a concurrent tool-call race under `workSessionMu` + file rename?
  - Does `handleDevelopBriefing` correctly distinguish "resume same session" from "refuse different intent" from "open new after close"?
  - Does `EvaluateAutoClose` consider ONLY services in `ws.Services`, or can stage services accidentally block close in dev/simple modes?
  - Does `workSessionScope` handle stage-free modes correctly (dev / simple) without leaking stage names?
  - Does `RecordDeployAttempt` trigger auto-close when the last verify was already passed but a new deploy fails?
  - Does the "Lifecycle Status" block in the system prompt survive compaction cycles — i.e. does it carry enough state to re-orient the LLM without re-calling `zerops_workflow action="status"`?
  - Does the intent-mismatch refusal message suggest the right next action (close + restart with new intent)?
  - What happens if a work session's `Services` list no longer matches the project (services deleted between briefing and next deploy)?
  - Close is idempotent, but what happens if the work-session file was already deleted by `CleanStaleWorkSessions` while the PID is still alive (clock skew / test harness)?

### Agent C — Environment Routing (Container vs Local)

- **Scope:** `DetectEnvironment`, conditional tool registration (`RegisterDeploySSH` vs `RegisterDeployLocal`), hostname inversion in `writeBootstrapOutputs` / `writeProvisionMetas`, `ResolveProgressiveGuidance` append-vs-replace rules, generate-local self-contained behavior, deploy-local flow, `.env` generation via `zerops_env`, `LocalConfig` persistence, VPN guidance.
- **Entry files:** `internal/workflow/environment.go`, `bootstrap_guidance.go`, `bootstrap_outputs.go`, `local_config.go`, `briefing.go`, `internal/runtime/runtime.go`, `internal/tools/deploy_ssh.go`, `deploy_local.go`, `env.go`, `mount.go`.
- **Spec anchors:** `spec-local-dev.md` entire doc, `spec-workflows.md §5–§6`, `spec-knowledge-distribution.md KD-05, KD-11`.
- **Invariants:** E1–E6, KD-05.
- **Focus questions:**
  - Does the hostname-inversion logic (`metaHostname = stageHostname` when local+standard) produce a meta whose `StageHostname` is empty, and does `RoleFromMode` / `BuildBriefingTargets` handle this correctly later?
  - Can a meta written in container mode ever be loaded into a local-mode workflow (or vice versa), and what happens?
  - Does `ResolveProgressiveGuidance` silently return the base section when `generate-local` is missing, masking a broken local path?
  - Does the `local.json` persist information that could go stale (port, envFile) vs be regenerated?
  - Do `writeBriefingLocalDeploy` / `writeBriefingPushDevWorkflow` emit consistent commands for every mode?
  - Does the local-mode briefing ever reference SSH or SSHFS paths (should not)?
  - Is `zerops_mount` correctly UNAVAILABLE in local mode, or can it be registered?
  - Does the briefing's `Platform rules` section correctly omit "Code on SSHFS mount" for local env?
  - Does env-var generation via `generate-dotenv` correctly resolve `${host_var}` references when some deps are shared across multiple targets?

### Agent D — Mode Routing (Standard / Dev / Simple / Managed-only)

- **Scope:** `PlanMode()` aggregation, `BootstrapTarget.Runtime.EffectiveMode()`, `distinctModes()`, per-mode guidance sections, deploy role derivation, `EvaluateAutoClose` scoping, `writeBriefingTargetSummary` and per-mode deploy commands, `IsAllExisting()` + empty-plan managed-only flow.
- **Entry files:** `internal/workflow/validate.go`, `bootstrap.go` (PlanMode), `bootstrap_guidance.go`, `briefing.go` (writeBriefing*Deploy), `adopt.go` (pair inference), `work_session.go` (scope/EvaluateAutoClose).
- **Spec anchors:** `spec-workflows.md §2.3–§2.5`, `spec-knowledge-distribution.md KD-06, KD-09`.
- **Invariants:** B3 (mode aggregation), KD-06.
- **Focus questions:**
  - What does `PlanMode()` return for a *mixed* plan, and does `ResolveProgressiveGuidance` emit guidance for each mode present, or only one?
  - Does the managed-only path (empty plan / `len(Targets) == 0`) reach `BuildTransitionMessage` cleanly, or does it crash when `plan.Targets` is indexed?
  - `EvaluateAutoClose` requires every service to have succeeded-deploy + passed-verify. In dev mode (no stage) is `workSessionScope` correctly excluding stage? In standard mode is it correctly including stage?
  - Does `writeBriefingStandardDeploy` handle the case where `targets[i+1]` is not the stage for target `i` (e.g. two dev targets in a row, then two stages)?
  - Does `StageHostname()` auto-derivation handle hostnames like `mydevelopment` (ending in `dev` but not by convention)?
  - Do simple-mode services ever accidentally get a stage hostname written to their meta?
  - Is the dev mode's briefing emitting `setup: dev` correctly or does it reference `prod` somewhere?

### Agent E — Guidance / Knowledge Distribution

- **Scope:** `assembleGuidance` layering, `needsRuntimeKnowledge`, `getCoreSection`, `formatEnvVarsForGuide`, `BuildIterationDelta` escalation tiers, `BuildDevelopBriefing` knowledge pointers, knowledge-provider integration, `zerops_knowledge` tool handler behavior, recipe/guide delivery.
- **Entry files:** `internal/workflow/guidance.go`, `bootstrap_guidance.go`, `bootstrap_guide_assembly.go`, `briefing.go`, `internal/knowledge/*.go`, `internal/tools/knowledge.go`.
- **Spec anchors:** `spec-knowledge-distribution.md` entire doc (all KD-01..KD-19).
- **Invariants:** KD-01 through KD-19.
- **Focus questions:**
  - Is knowledge injected at the right step (INJECT) and pointed to elsewhere (POINT) — or does anything double-deliver?
  - Does `BuildIterationDelta` correctly REPLACE normal guidance at iteration > 0 for deploy step only, and leave other steps untouched? (See line: `if step != StepDeploy || iteration == 0 { return "" }`.)
  - If `params.KP == nil`, does guidance still function with static content only, or does something crash?
  - Does `formatEnvVarsForGuide` render correctly when `envVars` is empty, nil, or has a service with zero keys?
  - Does `zerops_knowledge recipe="{name}"` deliver the recipe at discover time (when needed), or only after the plan is submitted?
  - Is the Generate Rules lifecycle block correctly EXCLUDED from deploy step to avoid duplication with `bootstrap.md`'s inline rules?
  - Can a stale `knowledgeCache` on the `Engine` cause briefings to miss freshly-added recipes/runtimes?
  - Is the develop briefing's "Knowledge on demand" section correctly personalized per runtime type (only showing relevant runtimes)?
  - Does the **discover** step inject the platform model exactly once, not at every complete/status call?

### Agent F — Adoption Paths (Manual + Auto)

- **Scope:** `InferServicePairing` two-pass algorithm, `AdoptCandidate` classification, `IsAllExisting` fast path in `BootstrapComplete`, auto-adopt from `handleDevelopBriefing`, mixed-plan behavior (some `isExisting=true`, some not), stage-hostname conventions (ending in `dev`, `stage`, `prod`), `BootstrapSession="" for adopted` semantics, `managedByZCP` + `isInfrastructure` discover fields.
- **Entry files:** `internal/workflow/adopt.go`, `internal/workflow/engine.go` (BootstrapComplete fast path, IsAllExisting), `internal/tools/workflow_develop.go` (adoptUnmanagedServices), `internal/ops/discover.go` (managedByZCP + isInfrastructure).
- **Spec anchors:** `spec-workflows.md §3`, `spec-bootstrap-flow-current.md §5, §8.2, §11.2`.
- **Invariants:** B9 (adoption fast path idempotent), B10 (meta-for-unknown-hostname bug).
- **Focus questions:**
  - Does `IsAllExisting()` return the right value on an empty `Targets` slice? What about a slice where every runtime is `isExisting=true` but one dep has `resolution: CREATE`?
  - Does `adoptUnmanagedServices` reset cleanly if `BootstrapStart` succeeds but `BootstrapCompletePlan` fails partway?
  - Does `InferServicePairing` correctly skip zcp* hostnames, managed services, and unpaired `*stage` hostnames?
  - For adopted services, is `bootstrapSession` always empty (signal "adopted"), or can a fresh bootstrap accidentally inherit an adoption flag?
  - For standard-mode adoption with non-`dev` hostname suffix, is `stageHostname` explicit-only, or can auto-derive fire incorrectly?
  - Does `handleStrategy` create a minimal meta for a hostname that doesn't exist in infrastructure (bug 11.2)? If so, what are the downstream effects?
  - What happens when the auto-adopt flow is triggered but a human later deletes a service between discover + develop start?

### Agent G — Spec-vs-Code Drift

- **Scope:** cross-check claims in all 7 specs against current code. Flag obsolete sections, stale file references, abandoned invariants, renamed constants, contradictions between specs.
- **Entry files:** all `docs/spec-*.md`, walked line-by-line with grep into `internal/` for every named function, constant, or file path.
- **Specs:** `spec-workflows.md`, `spec-work-session.md`, `spec-knowledge-distribution.md`, `spec-bootstrap-flow-current.md`, `spec-local-dev.md`, `spec-progressive-guidance.md`, `spec-recipe-quality-process.md`.
- **Invariants:** none direct — purpose is to surface CLAUDE.md rule "specs AUTHORITATIVE for workflow design" breakage.
- **Focus questions:**
  - Does each named code reference in a spec still exist?
  - Do invariants still describe observable behavior?
  - Are any "TODO" / "planned" items in a spec actually shipped, and is the spec not updated?
  - Are there contradictions between specs (e.g. `spec-work-session.md` says X, `spec-workflows.md` says Y)?
  - Are the code-reference-map sections in `spec-knowledge-distribution.md` accurate?
  - Are the 4 known bugs in `spec-bootstrap-flow-current.md §11` still present, or have they been silently fixed without updating the spec?

## Dispatch Protocol

1. Each agent is dispatched with `run_in_background=false` so findings are available in the main conversation for consolidation.
2. Every agent is told:
   - its scope (above)
   - report format: markdown, 1 entry per finding, fields `finding`, `severity` (CRITICAL/HIGH/MEDIUM/LOW), `env`, `mode`, `strategy`, `evidence` (file:line), `spec-anchor`, `recommended-action`
   - keep report under 2500 words
   - DO NOT fix anything, only find
   - if a finding overlaps another agent's scope, note it but don't investigate outside your own files
3. Findings accumulate into the final `docs/audit-bootstrap-develop-findings.md` with:
   - deduplicated list
   - classified by severity
   - sorted by (severity desc, env matrix impact desc)
   - recommended remediation order

## Deliverables

- This file: `docs/audit-bootstrap-develop-team-plan.md` (plan).
- Per-agent reports: inline (returned by each Agent invocation).
- Consolidated report: `docs/audit-bootstrap-develop-findings.md` (Task #4).
- The consolidated report is the only artifact that should be checked in; per-agent reports remain in-conversation.
