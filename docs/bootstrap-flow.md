# Bootstrap Flow: Complete Reference

How a client LLM bootstraps a Zerops project, from MCP connection to fully deployed services.

---

## Two-System Architecture

Bootstrap uses two complementary systems:

1. **Init Instructions** — static text injected into the LLM's system prompt when the MCP connection opens. Provides project state, service inventory, and routing rules. The LLM sees this *before* any tool call.

2. **Bootstrap Conductor** — a stateful 10-step sequencer accessed via `zerops_workflow`. Guides the LLM through each step with tools, verification criteria, and attestation requirements. Tracks progress in persistent state.

The init instructions *route* the LLM to the conductor. The conductor *drives* the bootstrap.

---

## Phase 0: Init Instructions

**Source:** `internal/server/instructions.go` — `BuildInstructions()`, injected at `internal/server/server.go:44`.

When the MCP server starts, `BuildInstructions()` builds a system prompt message with three sections:

### Section A: Runtime Context

If ZCP is running inside a Zerops container, identifies the service name:

```
You are running inside the Zerops service 'zcpx'. You manage services in the same project.
```

Omitted when running locally.

### Section B: Project Summary

Calls `buildProjectSummary()` which:
1. Lists all non-system services with hostname, type+version, and status
2. Detects project state via `workflow.DetectProjectState()`
3. Appends a routing directive based on state

**Project state detection logic** (from `internal/workflow/engine.go`):
- Filters services into runtime vs managed using `managedServicePrefixes` (postgresql, mariadb, valkey, etc.)
- No runtime services → **FRESH**
- Runtime services with `{name}dev` + `{name}stage` pairs → **CONFORMANT**
- Runtime services without dev/stage pattern → **NON_CONFORMANT**

**Routing directives by state:**

| State | Directive | Strength |
|-------|-----------|----------|
| FRESH | `zerops_workflow action="start" workflow="bootstrap" mode="full"` | REQUIRED |
| NON_CONFORMANT | `zerops_workflow action="start" workflow="bootstrap" mode="full"` | REQUIRED |
| CONFORMANT | `zerops_workflow action="start" workflow="deploy" mode="full"` | Recommended |
| Empty project | `zerops_workflow action="start" workflow="bootstrap" mode="full"` | REQUIRED |

### Section C: Base + Routing Table

Static instructions listing all workflow entry points:

```
ZCP manages Zerops PaaS infrastructure.
Tool routing:
- Bootstrap/create services: zerops_workflow action="start" workflow="bootstrap" mode="full"
- Deploy code: zerops_workflow action="start" workflow="deploy" mode="full"
- Debug issues: zerops_workflow action="start" workflow="debug" mode="quick"
- Scale: zerops_workflow action="start" workflow="scale" mode="quick"
- Configure: zerops_workflow action="start" workflow="configure" mode="quick"
- Monitor: zerops_discover
- Search docs: zerops_knowledge query="..."

NEVER call zerops_import directly. ALWAYS start with zerops_workflow.
```

---

## Phase 1: Conductor Start

**Trigger:** LLM calls `zerops_workflow action="start" workflow="bootstrap" mode="full"`.

**Source:** `internal/tools/workflow.go` — `handleStart()` → `engine.BootstrapStart()`.

### What happens

1. `handleStart()` checks: workflow is `"bootstrap"` and mode is not `"quick"` → routes to `engine.BootstrapStart()`.
2. `BootstrapStart()` creates a new `WorkflowState` via `InitSession()` (UUID session ID, phase=INIT, persisted to `zcp_state.json`).
3. Creates `BootstrapState` with 10 steps, all `"pending"`.
4. Sets step 0 (`detect`) to `"in_progress"`.
5. Saves state and returns a `BootstrapResponse`.

### Response structure

```json
{
  "sessionId": "uuid",
  "mode": "full",
  "intent": "user's intent string",
  "progress": {
    "total": 10,
    "completed": 0,
    "steps": [
      {"name": "detect", "status": "in_progress"},
      {"name": "plan", "status": "pending"},
      ...
    ]
  },
  "current": {
    "name": "detect",
    "index": 0,
    "category": "fixed",
    "guidance": "...",
    "tools": ["zerops_discover"],
    "verification": "..."
  },
  "message": "Step 1/10: detect"
}
```

The `current` block tells the LLM exactly what to do, which tools to use, and how to verify success.

---

## Steps 1–10: The Bootstrap Sequence

**Source:** `internal/workflow/bootstrap_steps.go` — `stepDetails` array.

Each step has: name, category, guidance text, required tools, verification criteria, and a skippable flag.

### Step categories

| Category | Meaning |
|----------|---------|
| `fixed` | Deterministic — follow instructions exactly |
| `creative` | LLM exercises judgment (stack selection, YAML generation) |
| `branching` | LLM chooses between inline execution or subagent delegation |

### Step 1: detect (fixed, mandatory)

**Tools:** `zerops_discover`

**What the LLM does:** Calls `zerops_discover` to inspect the project. Classifies state as FRESH, PARTIAL, CONFORMANT, or EXISTING.

**Routing logic:**
- FRESH → proceed through all steps
- PARTIAL → resume from failed step (check `zerops_events`)
- CONFORMANT → skip to deploy step
- EXISTING → warn user about non-standard naming

**Verification:** "Project state classified as FRESH/PARTIAL/CONFORMANT/EXISTING with evidence"

### Step 2: plan (creative, mandatory)

**Tools:** `zerops_knowledge`

**What the LLM does:** Identifies the application stack from user intent — runtime services (language + framework), managed services (databases, caches), and environment mode (standard dev+stage pairs or simple).

**Key constraints:**
- Hostnames: only `[a-z0-9]`, no hyphens/underscores, max 25 chars, immutable
- Dev pattern: `{app}dev`, Stage pattern: `{app}stage`

**Verification:** "Service list with hostnames, types, versions documented"

### Step 3: load-knowledge (fixed, mandatory)

**Tools:** `zerops_knowledge`

**What the LLM does:** Two mandatory knowledge calls:
1. **Runtime briefing:** `zerops_knowledge runtime="{type}" services=["{managed1}", ...]` — binding rules, ports, env vars, wiring patterns
2. **Infrastructure rules:** `zerops_knowledge scope="infrastructure"` — import.yml schema, zerops.yml schema, env var system

Optional: `zerops_knowledge recipe="{framework}"` for framework-specific configs.

**Verification:** "Runtime briefing loaded for each runtime type AND infrastructure scope loaded"

### Step 4: generate-import (creative, mandatory)

**Tools:** `zerops_knowledge`

**What the LLM does:** Generates `import.yml` following infrastructure rules. Key properties:
- Dev services: `enableSubdomainAccess: true`, `startWithoutCode: true`, `maxContainers: 1`
- Stage services: `enableSubdomainAccess: true`, no `startWithoutCode`
- Managed services: shared, `mode: NON_HA`

**Validation checklist:** hostnames follow `[a-z0-9]`, types match stacks, no duplicates, object-storage has `objectStorageSize`, preprocessor syntax correct.

**Verification:** "import.yml generated with valid hostnames, types, and all required fields"

### Step 5: import-services (fixed, mandatory)

**Tools:** `zerops_import`, `zerops_process`, `zerops_discover`

**What the LLM does:**
1. `zerops_import content="<yaml>"` — submits the import
2. `zerops_process processId="<id>"` — polls until FINISHED
3. `zerops_discover` — verifies services exist in expected states (dev RUNNING, stage NEW/READY_TO_DEPLOY, managed RUNNING)

**Verification:** "All services imported and verified in expected states"

### Step 6: mount-dev (fixed, skippable)

**Tools:** `zerops_mount`

**What the LLM does:** Mounts ONLY dev runtime service filesystems via SSHFS. Does NOT mount stage, managed, or shared-storage services.

**Skip condition:** No runtime services exist (managed-only project).

**Verification:** "All dev runtime service filesystems mounted successfully"

### Step 7: discover-envs (fixed, skippable)

**Tools:** `zerops_discover`

**What the LLM does:** For each managed service, calls `zerops_discover service="{hostname}" includeEnvs=true`. Records exact env var names (connectionString, host, port, user, password, dbName). These real values must be used in `zerops.yml` — never hardcoded guesses.

**Skip condition:** No managed services exist.

**Verification:** "All managed service env vars discovered and documented"

### Step 8: deploy (branching, skippable)

**Tools:** `zerops_deploy`, `zerops_discover`, `zerops_subdomain`, `zerops_logs`, `zerops_mount`

This is the most complex step. See [Deploy Step Deep-Dive](#deploy-step-deep-dive) below.

**Skip condition:** No runtime services exist (managed-only project).

**Verification:** "All runtime services deployed, /status endpoints returning 200 with connectivity proof"

### Step 9: verify (fixed, mandatory)

**Tools:** `zerops_discover`

**What the LLM does:** Independent verification of ALL services — does NOT trust self-reports from the deploy step.

- Runtime services: confirm RUNNING + HTTP check on subdomain + `/status` endpoint returns 200 with connectivity proof
- Managed services: confirm RUNNING

Partial success is acceptable — the attestation captures the actual state (e.g., "3/5 services healthy, apidev failing").

**Verification:** "All services independently verified with status documented"

### Step 10: report (fixed, mandatory)

**Tools:** `zerops_discover`

**What the LLM does:** Presents final results to the user:
- Each service with hostname, type, status, URL
- Grouped by: runtime dev, runtime stage, managed
- Summary of total services, skipped steps, partial failures
- Actionable next steps

**Verification:** "Final report presented to user with all service URLs and statuses"

---

## Deploy Step Deep-Dive

### Branching decision

| Service count | Approach |
|---------------|----------|
| 1 service pair (or inline) | Deploy directly in current conversation |
| 2+ service pairs | Spawn one subagent per service pair |

### Per-service deploy sequence

For each runtime service pair (dev + stage):

1. Write `zerops.yml` with correct build/run commands and discovered env vars
2. Write application code with required endpoints: `GET /`, `GET /health`, `GET /status`
3. Deploy dev: `zerops_deploy targetService="{devHostname}"`
4. Verify dev: `zerops_discover` + HTTP check on subdomain URL
5. Enable subdomain: `zerops_subdomain action="enable"`
6. Deploy stage: `zerops_deploy targetService="{stageHostname}"`
7. Verify stage: same checks as dev
8. Enable subdomain: `zerops_subdomain action="enable"`

### Dev vs prod deploy differentiation

| Aspect | Dev (source deploy) | Stage (build deploy) |
|--------|---------------------|----------------------|
| zerops.yml deployFiles | `[.]` (entire source) | Build output only |
| Build step | Optional/minimal | Full build pipeline |
| Env vars | Dev-friendly | Production-ready |

### 8-Point Verification Protocol

Defined in `internal/content/workflows/bootstrap.md`. Every deployment must pass all 8 checks:

| # | Check | Tool/Method | Pass criteria |
|---|-------|-------------|---------------|
| 1 | Build/deploy completed | `zerops_events` (poll 10s, max 300s) | Build event not FAILED |
| 2 | Service status | `zerops_discover` | RUNNING |
| 3 | No error logs | `zerops_logs severity="error" since="5m"` | Empty |
| 4 | Startup confirmed | `zerops_logs search="listening\|started\|ready"` | At least one match |
| 5 | No post-startup errors | `zerops_logs severity="error" since="2m"` | Empty |
| 6 | Activate subdomain | `zerops_subdomain action="enable"` + `zerops_discover` for URL | Success |
| 7 | HTTP health check | `curl "{subdomain}/health"` | HTTP 200 |
| 8 | Managed svc connectivity | `curl "{subdomain}/status"` | 200, all connections "ok" |

### Iteration loop

When verification fails, the LLM iterates up to 3 times:

1. **Diagnose** — identify which check failed and read logs/errors
2. **Fix** — edit files on mount path (zerops.yml, app code)
3. **Redeploy** — `zerops_deploy` to the same service
4. **Re-verify** — run the full 8-point protocol again

After 3 failed iterations, the LLM reports to the user with what was tried and the current error state.

---

## Auto-Completion

**Source:** `internal/workflow/engine.go` — `autoCompleteBootstrap()`.

When the last step (report) is completed, `BootstrapState.Active` becomes `false`. The engine then:

### 1. Collects attestations

Gathers all non-empty attestation strings from completed steps, keyed by step name.

### 2. Maps steps to evidence types

| Evidence type | Source steps |
|---------------|-------------|
| `recipe_review` | detect, plan, load-knowledge |
| `discovery` | discover-envs |
| `dev_verify` | deploy, verify |
| `deploy_evidence` | deploy |
| `stage_verify` | verify, report |

For each evidence type, concatenates the attestations from its source steps. If no source steps had attestations, falls back to `"auto-recorded from bootstrap steps"`.

### 3. Saves evidence files

Each evidence type is saved as a JSON file at `{evidenceDir}/{sessionID}/{type}.json` with `verificationType: "attestation"` and `passed: 1`.

### 4. Transitions through all phases

Walks the phase sequence for the current mode (e.g., for `full`: INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE) and records all transitions in the history. Sets the final phase to the last in the sequence.

This auto-completion is transparent to the LLM — it completes step 10 normally and gets back the final `BootstrapResponse` with `message: "Bootstrap complete. All steps finished."`.

---

## Step Lifecycle

**Source:** `internal/workflow/bootstrap.go`.

### Completing a step

```
zerops_workflow action="complete" step="{name}" attestation="{description}"
```

Engine calls `BootstrapState.CompleteStep()`:
1. Validates bootstrap is active
2. Validates step name matches the current step (sequential enforcement)
3. Validates attestation is at least 10 characters
4. Sets status to `"complete"`, records attestation and timestamp
5. Advances `CurrentStep`
6. Marks next step as `"in_progress"`
7. If last step completed → triggers auto-completion

### Skipping a step

```
zerops_workflow action="skip" step="{name}" reason="{why}"
```

Engine calls `BootstrapState.SkipStep()`:
1. Validates step name matches current step
2. Validates step is skippable (checks `StepDetail.Skippable`)
3. Sets status to `"skipped"`, records reason and timestamp
4. Advances `CurrentStep`

**Skippable steps:** mount-dev, discover-envs, deploy. All others are mandatory.

### Checking status

```
zerops_workflow action="status"
```

Returns the current `BootstrapResponse` (read-only).

---

## Validation Model

### Formal vs real validation

| What | Formal (engine enforces) | Real (LLM self-attests) |
|------|--------------------------|-------------------------|
| Step ordering | Sequential only, name must match current | LLM follows guidance |
| Attestation length | Minimum 10 characters | Content accuracy is LLM's responsibility |
| Skippability | Engine rejects skip on mandatory steps | LLM decides *when* to skip |
| Tool usage | Not enforced — guidance lists tools | LLM calls the right tools |
| Verification criteria | Not checked by engine | LLM evaluates and reports |
| Evidence types | Validated on save (type must be known) | Content is attestation text |
| Gate requirements | Checked on phase transition | Auto-completion bypasses gates |

The conductor trusts the LLM's attestations. It enforces structure (ordering, skippability, minimum attestation length) but not correctness.

---

## Key Invariants

1. **Single active step** — only one step can be `"in_progress"` at a time. The engine enforces sequential progression.

2. **Sequential only** — `CompleteStep` and `SkipStep` validate that the requested step name matches `CurrentStepName()`. Skipping ahead or going back is not possible.

3. **Attestation minimum** — 10 characters minimum (`minAttestationLen`). Enforced by `CompleteStep`.

4. **Static instructions** — `BuildInstructions()` runs once at server start. The project summary is a snapshot; it does not update if services are added/removed during the session.

5. **Bootstrap activates on start** — `BootstrapStart()` creates the state and immediately sets step 0 to `"in_progress"`. There is no separate "activate" action.

6. **Auto-completion is atomic** — when the last step completes, all evidence is recorded and all phase transitions happen in a single save. There is no intermediate state where bootstrap is done but phases haven't transitioned.

7. **Quick mode bypasses conductor** — `mode="quick"` uses generic `Start()` instead of `BootstrapStart()`, returning static workflow guidance without the step sequencer.

8. **Phase gates are bypassed** — auto-completion writes evidence and transitions directly, without calling `CheckGate()`. The bootstrap conductor is a higher-level abstraction that supersedes the phase/gate system.
