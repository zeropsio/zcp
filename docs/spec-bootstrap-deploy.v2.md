# ZCP Bootstrap & Deploy Workflow — Essential Protocol

> **Status**: Revised specification (v2) — incorporates Review 1 findings.
> **Scope**: Container mode only.
> **Date**: 2026-03-20
> **Changes from v1**: Dead code removed (StepChecker, intermediate ServiceMeta states), guidance deduplication, narrative reduced to structural guardrails. See `spec-bootstrap-deploy.review-1.md` for evidence.

---

## 1. Core Concept

ZCP helps LLMs create and develop applications on Zerops by providing:
1. **A clear process** — 6-step linear flow matching the platform's actual requirements
2. **Real verification** — each step has testable success criteria
3. **Platform knowledge** — runtime briefings, YAML schemas, env var discovery to prevent silent failures
4. **Iteration support** — when deploys fail, escalating guidance helps diagnose instead of loop

**Platform minimum** (what Zerops actually requires): import YAML → zerops.yml + code → push → enable subdomain. **5 operations.**

**What the workflow adds**: Step sequencing, plan validation, env var discovery injection, mode-specific guidance, iteration recovery, crash resumption. These prevent the top LLM failure modes: wrong hostnames, guessed env vars, missing subdomain enable, wrong deployFiles, port binding errors.

---

## 2. Glossary

| Term | Definition |
|------|-----------|
| **Session** | Ephemeral workflow state at `.zcp/state/sessions/{id}.json`. Created on start, deleted on completion. |
| **ServiceMeta** | Persistent per-service file at `.zcp/state/services/{hostname}.json`. Written once at bootstrap completion. Records decisions for future workflows. |
| **Mode** | `standard` (dev+stage pair), `dev` (dev only), `simple` (single service, real start). Chosen during discover. |
| **Step** | Atomic workflow unit: pending → in_progress → complete/skipped. Requires attestation (min 10 chars). |
| **Iteration** | Reset of generate/deploy/verify steps with incremented counter. Max 10. Preserves discovery context. |

---

## 3. System Model

```
Agent → zerops_workflow action=... → Tool Handler → Workflow Engine → Session State
                                                                    → ServiceMeta (on completion only)
                                                                    → Registry (exclusivity lock)
       → zerops_discover/import/mount/deploy/verify/subdomain (operational tools)
```

The engine:
- Enforces step ordering (linear, no skipping mandatory steps)
- Validates plans against live API catalog (hostnames, types, resolutions)
- Injects platform knowledge and discovered env vars per step
- Persists ServiceMeta at completion for future workflows (deploy, CI/CD, routing)

---

## 4. Bootstrap Flow — 6 Steps

**Entry**: `zerops_workflow action="start" workflow="bootstrap"` → exclusive session, first step active.

**Exit**: All steps complete/skipped → session deleted, ServiceMeta written.

### Step 1: DISCOVER (mandatory)

**Purpose**: Detect project state, identify services, choose mode, submit validated plan.

**What to do**:
1. `zerops_discover` → classify: FRESH / CONFORMANT (route to deploy) / NON_CONFORMANT (ask user)
2. Identify runtime + managed services from user intent. Validate against `availableStacks`.
3. Choose mode: **standard** (default, dev+stage), **dev** (dev only), **simple** (single, real start)
4. Present plan to user, get confirmation
5. Submit: `zerops_workflow action="complete" step="discover" plan=[...]`

**Plan validation** (engine-enforced):
- Hostnames: `[a-z0-9]`, max 25 chars
- Types: matched against live catalog
- Standard mode: devHostname must end in `"dev"`, stage derived as `{prefix}stage`
- Dependencies: resolution CREATE (must not exist) / EXISTS (must exist) / SHARED (another target creates it)
- Managed services: mode defaults to NON_HA

**Key constraint**: User must confirm before submission. NON_CONFORMANT = STOP, ask user.

### Step 2: PROVISION (mandatory)

**Purpose**: Create infrastructure, mount dev filesystems, discover env vars.

**What to do**:
1. Generate import.yml (dev: `startWithoutCode: true`, `maxContainers: 1`, `enableSubdomainAccess: true`; stage: omit startWithoutCode)
2. `zerops_import` → blocks until processes complete
3. `zerops_discover` → verify services exist
4. `zerops_mount` dev runtime filesystems (NOT stage, NOT managed)
5. `zerops_discover includeEnvs=true` → store env var **NAMES ONLY** in session

**Critical platform rules**:
- Stage stays READY_TO_DEPLOY (no wasted resources)
- `mount:` in import only applies to ACTIVE services (stage mount silently ignored)
- Two kinds of "mount": SSHFS dev tool vs shared-storage platform mount

### Step 3: GENERATE (skippable — skip if managed-only)

**Purpose**: Write zerops.yml and application code to mounted filesystem.

**Structural constraints** (non-negotiable):
- Write to `/var/www/{hostname}/` (SSHFS mount path), NOT `/var/www/`
- `deployFiles: [.]` for ALL self-deploying services — wrong value destroys source files
- `envVariables`: ONLY use discovered var names (`${hostname_varName}` references)
- App must bind `0.0.0.0:{port}`, NOT localhost
- Required endpoints: `GET /`, `GET /health`, `GET /status` (with real connectivity checks)
- No `.env` files — Zerops injects env vars as OS vars

**Mode-specific zerops.yml**:

| Property | Standard/Dev | Simple |
|----------|-------------|--------|
| `start` | `zsc noop --silent` (PHP: omit) | Real command (`node index.js`, etc.) |
| `healthCheck` | None | Required (`httpGet` on app port) |
| `buildCommands` | Deps install only | Deps + compile if needed |

Standard mode: write dev entry ONLY. Stage entry comes after dev verification.

### Step 4: DEPLOY (skippable — skip if managed-only)

**Purpose**: Deploy, start servers, enable subdomains, verify health.

**Core principle**: Deploy first — env vars activate at deploy time.

**Universal deploy sequence** (per runtime service):
1. `zerops_deploy targetService="{hostname}"` — blocks until build completes
2. Start server via SSH (Bash `run_in_background=true`) — deploy kills the running process. **Skip for**: PHP runtimes (auto-start), simple mode (real start + healthCheck auto-starts)
3. `zerops_subdomain action="enable"` — MUST be called after EVERY deploy (even if set in import)
4. `zerops_verify` — check health

**Standard mode additions** (after dev verified):
5. Generate stage entry in zerops.yml (real start command, healthCheck, build output deployFiles)
6. `zerops_deploy sourceService="{dev}" targetService="{stage}"` — cross-deploy
7. Connect shared-storage if applicable: `zerops_manage action="connect-storage"`
8. Enable subdomain + verify stage

**Iteration on failure**: diagnose → fix → redeploy → re-verify. Use `zerops_workflow action="iterate"` for session-level reset.

**Multi-service (2+ runtime)**: Parent imports all, mounts all, discovers all env vars. Spawns Service Bootstrap Agent per runtime pair in parallel.

### Step 5: VERIFY (mandatory)

**Purpose**: Independent batch verification and final report.

1. `zerops_verify` (batch — all services)
2. Aggregate: healthy / degraded / unhealthy
3. Present: hostnames, types, status, subdomain URLs, next steps

### Step 6: STRATEGY (skippable — skip if managed-only)

**Purpose**: Record deployment strategy per runtime service.

Options: `push-dev` (SSH push), `ci-cd` (Git pipeline), `manual` (no automation).
Auto-assigns `push-dev` for dev/simple modes if no explicit choice.

---

## 5. Deploy Flow — 3 Steps

Deploy operates on services that already exist (created by bootstrap). Uses ServiceMeta for context.

**Entry**: `zerops_workflow action="start" workflow="deploy"` → reads ServiceMeta files, builds ordered targets.

| Step | Purpose |
|------|---------|
| **prepare** | Discover targets, check zerops.yml, load knowledge |
| **deploy** | Execute per-mode deploy sequence (same as bootstrap step 4) |
| **verify** | Batch verification (same as bootstrap step 5) |

---

## 6. State Model

### Session State (ephemeral)

```
WorkflowState {
  SessionID, PID, ProjectID, Workflow, Iteration, Intent,
  CreatedAt, UpdatedAt,
  Bootstrap: *BootstrapState | Deploy: *DeployState | CICD: *CICDState
}
```

- Exclusive: one bootstrap session per project (file-lock enforced)
- PID-based ownership: dead PID = session can be resumed
- Auto-pruned: sessions >24h automatically cleaned

### ServiceMeta (persistent)

Written **once at bootstrap completion** (not at intermediate steps).

```
ServiceMeta {
  Hostname, Type, Mode, Status: "bootstrapped",
  StageHostname, Dependencies, BootstrapSession, BootstrappedAt,
  Decisions: {deployStrategy: "push-dev"|"ci-cd"|"manual"}
}
```

**Consumers**: Deploy workflow (targets + mode), Router (workflow suggestions), CI/CD (hostnames), Immediate workflows (service context).

**Rules**: EXISTS/SHARED dependencies never overwrite existing metas.

### Discovered Environment Variables

- `zerops_discover includeEnvs=true` returns actual VALUES (transient, for validation)
- Session stores NAMES ONLY: `map[hostname][]varNames`
- Guidance injects `${hostname_varName}` references (safe, no secrets)
- Zerops resolves references at container level — invalid refs become literal strings silently

---

## 7. Mode Behavior Summary

| Aspect | Standard | Dev | Simple |
|--------|----------|-----|--------|
| Services | `{name}dev` + `{name}stage` + managed | `{name}dev` + managed | `{name}` + managed |
| zerops.yml start | `zsc noop --silent` (dev) / real (stage) | `zsc noop --silent` | Real command |
| healthCheck | None (dev) / required (stage) | None | Required |
| Server start | SSH manual (dev) / auto (stage) | SSH manual | Auto |
| Deploy flow | dev→verify→stage→verify | dev→verify | deploy→verify |
| Strategy auto-assign | None (user chooses) | `push-dev` | `push-dev` |

Standard and dev are identical for the dev service. Standard adds stage deployment after dev verification.

---

## 8. Iteration & Recovery

**Bootstrap iteration** (`action="iterate"`):
- Increments counter, resets steps 3-5 (generate/deploy/verify) to pending
- Preserves: discover + provision context, env vars, plan

**Escalating guidance** (deploy step, based on iteration count):
- **1-2**: "DIAGNOSE: check errors, fix, redeploy"
- **3-4**: Systematic checklist (env vars, zerops.yml, binding, ports, deployFiles)
- **5+**: "STOP. Present full diagnosis to user. Ask before continuing."

**Common fix patterns**:

| Symptom | Cause | Fix |
|---------|-------|-----|
| Build FAILED: "command not found" | Wrong buildCommands | Check runtime knowledge |
| App crashes: "EADDRINUSE" | Port conflict | Check run.ports matches app |
| App crashes: "connection refused" | Wrong env var name | Check vs discovered vars |
| HTTP 502 | Subdomain not activated | `zerops_subdomain action="enable"` |
| Empty response | Not on 0.0.0.0 | Fix binding |

---

## 9. Invariants

### Session
| Invariant | Enforced by |
|-----------|-------------|
| One bootstrap session at a time | `InitSessionAtomic()` with registry lock |
| Attestation ≥ 10 chars per step | `CompleteStep()` validation |
| Steps progress strictly in order | `CompleteStep()` name-matching |
| discover, provision, verify cannot be skipped | `Skippable` field |
| generate/deploy cannot be skipped with runtime targets | `validateConditionalSkip()` |

### State
| Invariant | Enforced by |
|-----------|-------------|
| ServiceMeta written once at bootstrap completion | `writeBootstrapOutputs()` |
| EXISTS/SHARED deps never overwrite existing meta | Skip check in outputs |
| Env var names (not values) in session | `DiscoveredEnvVars` type = `map[string][]string` |

### Operational
| Invariant | Enforced by |
|-----------|-------------|
| `zerops_deploy` blocks until build completes | `PollBuild()` |
| `zerops_import` blocks until processes complete | Process polling |
| Subdomain must be enabled after every deploy | Guidance + deploy step |
| Server restart after dev deploy | Guidance (container restart kills server) |

---

## 10. Knowledge Injection

Each step receives relevant platform knowledge automatically:

| Step | Injected |
|------|----------|
| Provision | import.yml schema |
| Generate | Runtime briefing + dependency wiring + discovered env vars + zerops.yml schema + rules |
| Deploy | Schema rules + discovered env vars |

This prevents the top LLM failure modes: wrong buildCommands, guessed env var names, incorrect YAML structure.

---

## 11. Known Gaps

| Gap | Impact | Status |
|-----|--------|--------|
| Managed-only plan validation (`len(targets) > 0` required) | Cannot bootstrap managed-only projects | Code fix needed in validate.go |
| Env var reference validation at generate step | Invalid `${hostname_varName}` refs silently kept | No validation against discovered vars |
| Least-privilege discover mode | Discover returns actual secret values | Future: names-only mode option |

---

## 12. Implementation Cleanup (from Review 1)

These items should be addressed in the codebase:

| Item | File | Action |
|------|------|--------|
| Delete StepChecker interface | bootstrap_checks.go, engine.go | Remove dead type + nil parameter |
| Remove intermediate ServiceMeta writes | engine.go:164,240 | Delete writeServiceMetas calls at discover/provision |
| Remove MetaStatusPlanned/Provisioned | bootstrap_outputs.go | Delete unused constants |
| Deduplicate bootstrap.md deploy sections | content/workflows/bootstrap.md | Remove deploy-overview/standard/dev/iteration/simple/agents/recovery duplicates |
| Merge generate-standard + generate-dev | content/workflows/bootstrap.md | Unify with "Standard additionally: stage after dev" note |
| Simplify buildPriorContext | bootstrap.go | Replace 80-char compression with full previous attestation |
