# Production Lifecycle Plan v2 — One-shot Key Model

**Date:** 2026-05-11
**Version:** v2 (replaces v1 — see git history for v1; addresses Plan-agent + Codex critique + user feedback)
**Status:** Ready for Phase A spike

---

## 1. TL;DR

ZCP closes the dev/stage → prod lifecycle gap with a single workflow, under a strict trust boundary: **ZCP has zero standing access to the production project**. User generates a one-shot Zerops API key for the launch window, pastes it into ZCP, ZCP performs `create → import → deploy poll → verify external-secret presence` with that key, then explicitly tells the user to delete the key. After the launch window, ZCP has no further access to prod. Ongoing prod iteration is user-driven, via their own tooling and Zerops UI. ZCP can prepare anything for the user, but never holds keys to mutate prod outside the launch window.

Five phases over ~5 weeks. Two building blocks: a physically-separate `ProjectAdminClient` interface (injected only into the launch workflow handler), and the launch workflow itself with a 6-state machine + structured `blockers[]`/`checks[]` arrays + source-control mutation phase + idempotent resume + source-snapshot integrity guard.

No `ModeProduction` in topology, no `ExportVariantProduction` (internal `LaunchBundleBuilder` instead), no data migration tooling, no custom-domain L7 mutation (guidance + DNS verify only), no adopt-production handoff (user does prod's MCP setup themselves with a separate fresh key, if ever), no rollback tool (backlog).

---

## 2. The user pain

Direct quote: *"zcp je k nicemu kdyz rikame ze se ma pouzivat pro dev/stage a produkce ma byt jinde a neumime uzivateli pomoc s tim tu porodukci co nejlepe vytvorit"* — ZCP feels useless if dev/stage works but prod is on the user.

Concrete gaps inventoried in research:
- ZCP has no cross-project surface; Client binds one `projectID`.
- ZCP has no production-aware bundle generation; export emits dev/stage bundles only.
- Zerops UI itself has no clone/promote/duplicate flow — the gap is platform-product-level, not ZCP-only.
- `PostClientProjectImport` exists in the SDK (= `zcli project project-import`) — creates project + services in one call. SDK already supports everything we need; ZCP just doesn't expose it.

What the user wants: a single ZCP-driven flow that takes their working dev/stage to a healthy production deployment in a separate Zerops project, with the trust boundary they can verify.

---

## 3. Trust boundary — first-class design constraint

User's explicit principle, locked in this version:

> *"my to llm nechceme pustit do toho produkcniho prostredi … ten produkcni projekt musi uzivatel trochu vice odmakat sam, nedaval bych tam llm zadne pristupy"*
>
> Refinement: *"bychom mu mohli poskytnout klic — ze bychom rekli uzivateli ze ten klic je jednorazovy a ze ho pak proste musi smazat. to by stacilo a bylo by docela dobre."*

Translation into mechanics:

| What | Where access lives | Lifetime |
|---|---|---|
| `ZCP_API_KEY` (org-scoped, in `.mcp.json`) | Reads source dev/stage project; writes to source repo (source-control mutation) | Standing, user-controlled at org level |
| **Launch-window key** (user generates per launch) | Used ONLY by the launch workflow handler, ONLY during the launch window | Single launch; **user deletes immediately after** |
| Post-launch prod access | None for ZCP | n/a |

Concrete rules:

1. The launch-window key arrives via `WorkflowInput.launchKey` on the `launch-production` workflow only.
2. The key flows through a temporary `ProjectAdminClient` instance created **per workflow invocation** — never written to state, logs, transcripts, or any persistent storage. Pinned by `TestLaunchWorkflow_LaunchKeyNeverWrittenToState` (greps state file + log output post-test).
3. State persistence (`.zcp/state/launch-production/{launchID}.json`) carries `launchID`, `targetProjectID`, `importProcessID`, `firstDeployProcessID`, source snapshot hashes, decision log — but **never the key**.
4. On compaction or process-restart recovery, if the launch is mid-pipeline and needs another mutation call, the user re-supplies a fresh launch-window key. The handler refuses to proceed with a stale or absent key.
5. Once the workflow reaches `launched` status, ZCP's response carries a **mandatory next-step**: "DELETE the launch-window key now in Zerops dashboard → Access Tokens." This is an enforced atom (not just guidance) — pinned by `TestLaunchedResponse_AlwaysContainsKeyDeletionStep`.
6. Custom domain L7 mutation is **not** done by ZCP even within the launch window. User handles domain attachment in Zerops UI. ZCP synthesizes DNS records + runs verification probes only.
7. External secret values (Stripe, OpenAI, etc.) — user enters them in Zerops UI per service, not through MCP. ZCP verifies their presence via `GetServiceEnv` against prod (with one-shot key) but never reads or persists values. Per Q5 answer.

Future evolution (out of v1 scope): user may expand the trust boundary later — e.g. per-project scoped keys with longer TTL, or ZCP-managed prod sessions with separate `.mcp.json`. **Not in v1.** User explicit: *"alespon pro ted, v budoucnu to treba bude jinak."*

---

## 4. North star

A user with a working Zerops dev/stage (or simple, or local-stage, or local-only) project can run, in one Claude Code session:

> *"Launch this to production. Project name myapp-prod, region prg1, custom domain myapp.com."*

…and arrive at:
1. A separate Zerops production project created and populated.
2. Services imported with HA defaults (for managed deps) + prod-tier scaling (for runtimes).
3. First deploy successful, prod-readiness checks green or explicitly waived.
4. A precise checklist of what the user must complete in Zerops UI: delete launch key, set N external secrets, attach custom domain, verify DNS.

ZCP does the irreducible bootstrap (project creation + import + deploy poll + presence-verification of secrets). Everything else the user owns. Strong line.

---

## 5. Constraints from the Zerops platform (re-verified, with v1 corrections)

| Constraint | Source |
|---|---|
| No first-class environment concept; recommended pattern = one project per env | `zerops-docs/features/infrastructure.mdx:21-23` |
| `mode: HA \| NON_HA` immutable; set at create | `zerops-docs/features/scaling.mdx:303-331` |
| HA applies to managed services + shared-storage only | `zerops-docs/features/scaling.mdx` |
| Runtime "HA" implemented via `minContainers ≥ 2` + stateless design | `production-checklist.mdx:121-130` |
| `zerops.app` shared subdomain NOT for prod | `references/networking/public-access.mdx:51-55` |
| Custom domain → dedicated IPv4 ($3/30 days) recommended | `public-access.mdx:77-92` |
| Tag-trigger build recommended for prod | `references/github-integration.mdx:45` |
| Backups per-service, restore is download + manual | `features/backup.mdx:32-78, 187` |
| API key is **client-scoped (org-level)**, not project-scoped | `zerops-go@v1.0.17` SDK semantics + auth model |
| `PostClientProjectImport` creates project + services in ONE call (= `zcli project project-import`) | `sdk/PostClientProjectImport.go` |
| `PostProject.MaxCreditLimit` + `Location` + `SshIsolation` fields available at create | `dto/input/body/postProject.go` |
| Custom domain via `PostProjectPublicHttpRouting` exists, but L7 config is read-modify-write whole-config shape (clobber risk) | `sdk/PostProjectPublicHttpRouting.go` + `GetProjectL7httpbalancerConfig.go` |

**v1 errata corrected here:**

- v1 §5 claimed *"Client only has `GetProject(projectID)`"* — **wrong**. `Client.ListProjects(ctx, clientID)` exists at `internal/platform/client.go:12`. Used at auth time only (`internal/auth/auth.go:169`) but in the interface.
- v1 §6.3 split create + import into two methods (`CreateProject` + `ImportProjectFromYaml`). `PostClientProjectImport` does both in one call. The confused dual-path is removed.

---

## 6. Current state inventory (carried from v1, citations re-verified)

What ZCP has today that this plan builds on:

- **Export workflow** (`internal/tools/workflow_export.go:52`) — three-call narrowing with `ExportStatus` (`internal/topology/types.go:144-184`). Bundle composition (`internal/ops/export_bundle.go:38-72`). Used as substrate for the new `LaunchBundleBuilder` but **not extended** — keeps export's existing surface stable.
- **Schema validation** (`schema.ValidateImportYAML` / `ValidateZeropsYAML`) — reusable for prod bundles unchanged.
- **Source-control mutation pattern** — export workflow already writes/commits/pushes yaml files (atom-driven). Launch workflow reuses this for the `setup: prod` block in the source repo.
- **Topology vocabulary** (`Mode`, `RuntimeClass`, `CloseDeployMode`, `GitPushState`, `BuildIntegration`, `ExportVariant`) — used as-is. **No additions.**
- **Failure classification** (`topology.FailureClass` + `ops.DeployResult.FailureClassification`) — reused verbatim.
- **`ServiceMeta` pair-key model** — used to identify the source half for the launch.

What ZCP **lacks** (the gap this plan fills):

- `ProjectAdminClient` interface for cross-project mutation surface.
- `LaunchBundleBuilder` for prod-tier bundle composition.
- `launch-production` workflow handler + status machine.
- Source-snapshot integrity guard (record + verify source state hashes).
- Audit manifest writer.
- Launch-specific atoms.

---

## 7. Design — two building blocks

### 7.1 `ProjectAdminClient` — separate interface, physical segregation

Codex's critique was load-bearing: discipline-via-lint doesn't hold; physical interface segregation does.

```go
// internal/platform/project_admin.go
package platform

// ProjectAdminClient is the cross-project mutation surface. It is constructed
// per launch-production invocation from a user-supplied launch-window key,
// used during a single workflow run, and discarded. It is NOT a method set
// on the standard Client. Tool handlers MUST NOT import this package's
// constructor except via the launch-production workflow.
type ProjectAdminClient interface {
    // CreateAndImportProject calls PostClientProjectImport — single API call
    // that creates the project AND imports services from import YAML.
    // Returns the new project ID and the async-process ID for the initial
    // import.
    CreateAndImportProject(ctx context.Context, yaml string, opts CreateOpts) (projectID, processID string, _ error)

    // GetProjectImportStatus polls the import process state.
    GetProjectImportStatus(ctx context.Context, processID string) (ProcessState, error)

    // ListServices for the target prod project — needed to verify secret presence post-import.
    ListServices(ctx context.Context, projectID string) ([]ServiceStack, error)

    // GetServiceEnv on the target prod project — returns env keys + sensitive
    // flags only; values are NOT exposed to ZCP code (the response struct
    // omits Value field for sensitive entries).
    GetServiceEnv(ctx context.Context, serviceID string) ([]EnvKey, error)

    // GetProjectEnv on the target prod project — same shape.
    GetProjectEnv(ctx context.Context, projectID string) ([]EnvKey, error)

    // DeleteProject — used on launch-fail rollback only. Returns the
    // async-process ID (SDK pattern).
    DeleteProject(ctx context.Context, projectID string) (processID string, _ error)
}

type CreateOpts struct {
    ClientID       string // org ID, derived from launch-window key at construction
    Location       string // region
    MaxCreditLimit *Decimal // optional safety net per PostProject.MaxCreditLimit
    Tags           []string // includes "env:prod" + "source-project:<sourceID>"
}

// NewProjectAdminClient constructs a ProjectAdminClient from a launch-window key.
// The key is held in memory only; the returned client carries it internally
// but never exposes it. After workflow completion, the caller MUST nil-out
// the reference so GC reclaims it.
func NewProjectAdminClient(launchKey string) (ProjectAdminClient, error)
```

The launch workflow handler signature:

```go
// internal/tools/workflow_launch_production.go
func handleLaunchProduction(
    ctx context.Context,
    client Client,                  // bound source-project client
    sourceProjectID string,
    deps LaunchDeps,                // includes a *function* that constructs ProjectAdminClient
    input WorkflowInput,
) (*Response, error) {
    // ...
    if mutationPhase {
        if input.LaunchKey == "" {
            return promptForLaunchKey(...)
        }
        admin, err := deps.NewProjectAdminClient(input.LaunchKey)
        if err != nil { return ... }
        defer admin.Close() // explicit zeroing
        // use admin for create+import+poll
    }
    // ...
}
```

**Why this works architecturally:**

- `project_admin.go` is a separate file; `internal/tools/` files OTHER THAN `workflow_launch_production.go` cannot import `NewProjectAdminClient` without showing up in a grep lint.
- The lint test is structural ("grep all `import "..../platform"` then assert only one file references `NewProjectAdminClient` or `ProjectAdminClient`") — 10 lines, not a method-call analyzer.
- `Client` (existing interface) is unchanged. Existing tools see no new surface.
- The launch-window key flows through one entry point. Pinned by `TestProjectAdminClient_KeyNotReachableFromClientInterface`.

### 7.2 The `launch-production` workflow

A new top-level workflow alongside `bootstrap` / `develop` / `verify` / `export`:

```
zerops_workflow workflow="launch-production" \
  action="status|scope|classify|prepare|publish|finalize" \
  productionProjectName=<string> \
  region=<string> \
  customDomain=<string-optional> \
  keepNonHA=[<hostname>...] \
  envOverrides={<key>: <value>, ...} \  # plain-config overrides only; NO secret values
  launchKey=<one-shot-key>              # required only at publish action onward
```

Six top-level statuses (collapsed from v1's 11):

| Status | When | Mutation? |
|---|---|---|
| `scope-prompt` | First call, inputs incomplete | No |
| `classify-prompt` | Source envs present, classifications incomplete | No |
| `ready-to-launch` | Bundle composed, source-control mutation done (or queued for agent), schema clean, blockers cleared | No — preview only |
| `launching` | Mutation pipeline in flight: import → deploy poll → verify-secrets | **Yes** (uses launchKey) |
| `failed` | Any mutation step failed; structured `blockers[]` describes recovery | No — read-side only |
| `launched` | All checks green or explicitly waived; user has the post-launch checklist | No — final report |

Each status response carries two structured arrays alongside the freeform payload:

- `blockers: []Blocker` — what must change before next status transition. Each Blocker = `{id, severity (block|warn), category (schema|source-control|external-secret|cost|other), message, recovery: { tool, action, args }}`.
- `checks: []CheckResult` — production-readiness rubric results. Each = `{id, status (pass|fail|skip|waived), severity, message, recovery?}`. Reused from existing `verify` infrastructure.

Compared to v1's flat 11 statuses, this design separates **state machine progression** (6 statuses) from **per-status detail** (arrays). The agent reads the top-level status to decide its action, then iterates blockers/checks for specifics.

---

## 8. The launch workflow — anatomy

### 8.1 Phases

```
┌─────────────────────────────────────────────────────────────────┐
│  READ-SIDE NARROWING  (no mutation, no launch key needed)       │
│                                                                  │
│  scope-prompt → classify-prompt → ready-to-launch               │
│                                                                  │
│  In ready-to-launch:                                            │
│   • LaunchBundleBuilder has composed full import YAML            │
│   • Source repo zerops.yaml has setup: prod block committed     │
│     (atom-driven; agent runs writes/commits/pushes — same        │
│     pattern as today's export workflow)                          │
│   • Source snapshot hashes captured: git SHA, zerops.yaml hash,  │
│     env digest, service-list digest                              │
│   • Cost preview rendered                                        │
│   • External-secret list rendered (user-action checklist)        │
│   • Blockers all cleared                                         │
└────────────────────────────┬────────────────────────────────────┘
                             │ user runs publish action with launchKey
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│  MUTATION PIPELINE  (per-call ProjectAdminClient, launchKey)    │
│                                                                  │
│  launching ─┬─→ CreateAndImportProject                          │
│             │     ↓ poll                                         │
│             ├─→ wait for first deploy completion (poll)         │
│             │     ↓                                              │
│             ├─→ verify external-secret presence (Get*Env)       │
│             │     ↓                                              │
│             ├─→ run prod-readiness rubric (checks[])            │
│             │     ↓                                              │
│             └─→ launched (final report)                         │
│                                                                  │
│  Any step fail → failed (with blockers[] + recovery hints)       │
│  Source-snapshot drift detected → failed (source-immutability)   │
└─────────────────────────────────────────────────────────────────┘
```

### 8.2 Source-control mutation phase

This is the architectural step v1 missed (caught by Codex). For `buildFromGit` to work in the prod project, the source repo MUST carry the `setup: prod` block before import is called.

The launch workflow handles this via the same agent-driven write+commit+push pattern as today's `export` workflow:

1. During `classify-prompt` → `ready-to-launch` transition, the handler synthesizes the `setup: prod` block to be appended to the source `zerops.yaml`.
2. Returns it in the response with explicit "agent writes this block to /var/www/zerops.yaml (container mode) or <workingDir>/zerops.yaml (local mode), commits, pushes".
3. Reuses existing close-mode + git-push-setup infrastructure for the actual push.
4. Hash of the committed zerops.yaml is recorded in the source snapshot.
5. The handler verifies the post-push state by reading back via SSH (container) or local FS (local mode) before flipping to `ready-to-launch`.

If git-push isn't set up yet, the chain falls back to the existing `setup-git-push-{container,local}` atoms — same pattern as export's E3 invariant.

### 8.3 One-shot key handling

Key lifecycle:

1. Agent reaches `ready-to-launch` status. Response includes: "Generate a launch-window API key in Zerops dashboard at `https://app.zerops.io/settings/access-tokens` (link templated). Pass it as `launchKey=<key>`. After launch completes, DELETE THE KEY."
2. Agent calls `action="publish" launchKey=<key>`.
3. Handler constructs `ProjectAdminClient` via `NewProjectAdminClient(input.LaunchKey)`. Validates the key can authenticate (one cheap GET call).
4. Handler proceeds through the mutation pipeline. The key is held in `admin` instance memory.
5. On `launched` or `failed`, handler explicitly `admin.Close()` (zeros internal field).
6. Response carries the **mandatory key-deletion atom** (`launch-delete-key.md` priority 1).
7. State file at `.zcp/state/launch-production/{launchID}.json` is written WITHOUT the key. Test `TestLaunchState_NoKeyContents` greps the state file after a full run, asserts the key string is absent.

Key safety properties:

- Never serialized to JSON (struct field marked `json:"-"`).
- Never logged (handler uses key only as method receiver; logs use `[REDACTED]` placeholder if mention is unavoidable).
- Never written to env vars or system files.
- Never carried in workflow response (response carries `targetProjectID` once known, but never the key).
- Compaction recovery: state file alone is insufficient to resume mutation; user re-supplies key on next `action="publish"` (or `action="status"` followed by `action="publish"` if mid-pipeline).

### 8.4 Idempotency + resume

On `action="publish"` with a `launchID` matching an existing state file:

1. Handler reads state file → has `targetProjectID` (if create succeeded).
2. If `targetProjectID` is set, **don't re-create**. Skip to whichever step the state file says is pending.
3. If state shows `importProcessID` exists, poll it instead of re-calling `CreateAndImportProject`.
4. If state shows `firstDeployProcessID` exists, poll deploy.
5. Each step records its completion timestamp in state for forensic and idempotency purposes.

Resume across process restarts:

- `launchID` is deterministic from source-project + launch-target-name + initiating timestamp.
- State file path: `.zcp/state/launch-production/{launchID}.json`. NOT pid-scoped.
- New process invoking the same launch with the same `productionProjectName` reads existing state. If state shows partial mutation, the handler returns `status=launching` with current step + how to continue.

### 8.5 Source-immutability guard

Before any mutation:

1. Recompute source snapshot: current git SHA, current source `zerops.yaml` hash, current env digest, current service-list digest.
2. Compare to the snapshot recorded at `ready-to-launch`.
3. If any field differs, refuse to proceed → `failed` status with `blockers: [{id: source-drift, severity: block, message: <field changed>, recovery: {action: re-classify}}]`.

Rationale: the user kept iterating on dev mid-launch. Re-classifying is cheap; mutating with stale bundle is catastrophic.

### 8.6 Failure cleanup

If mutation fails after `CreateAndImportProject` returned a `targetProjectID`:

- `failed` status carries `blockers: [{id: orphan-project, severity: warn, message: "Target project ${targetProjectID} created but import failed; project is orphaned (billable empty shell). Delete via Zerops UI OR retry launch (will reuse projectID and complete import).", recovery: {tool: "zerops_workflow", action: "launch-production", launchID: <id>, then "delete-target"}}]`.
- A `delete-target` sub-action on the workflow calls `admin.DeleteProject(targetProjectID)` with the user's still-valid launch key.
- ZCP does **not** auto-delete on every fail — sometimes import is recoverable. User explicit ack required.

### 8.7 Audit manifest

After each successful mutation transition, append to `.zcp/state/launch-audit-log.json`:

```json
{
  "launchID": "...",
  "timestamp": "2026-05-11T14:30:00Z",
  "action": "create-and-import",
  "sourceProjectID": "...",
  "targetProjectID": "...",
  "sourceCommitSHA": "...",
  "sourceZeropsYAMLHash": "...",
  "classifiedEnvs": {"<key>": "<bucket>"},
  "overrides": {...},
  "haOptOut": [...],
  "userAcksRecorded": ["..."],
  "result": "success|failure"
}
```

No secret values, no raw env values. Log is append-only (open-with-O_APPEND), survives crashes.

---

## 9. What's cut from v1 (explicit, with rationale)

| v1 item | Status | Why |
|---|---|---|
| `ModeProduction` in topology | **CUT** | Codex + Plan-agent: production = launch target intent, not source service mode. No `ServiceMeta` for prod project exists in ZCP (user-on-the-line); no axis-filter consumer. Defer indefinitely. |
| `ExportVariantProduction` (public) | **CUT** | Bundle prod-tier transforms live in internal `LaunchBundleBuilder`, not as a third value of `ExportVariant`. Keeps export workflow's existing dev/stage surface stable. |
| Data migration tooling | **CUT** | User-confirmed; "data se mezi dev a stage neprenasi, ma to vlastni migrace". Atom-only; even Phase 4a from v1 dropped. |
| Custom-domain L7 mutation | **CUT** (v1: full automation) | Read-modify-write whole-config shape; clobber risk too high for v1. ZCP synthesizes DNS records + runs DNS-propagation + HTTP-readiness probes only. User attaches domain in UI. Backlog: v2 with proper RMW. |
| Adopt-production handoff / second MCP session | **CUT** | User-on-the-line principle. Prod is user-managed post-launch. If user later wants ZCP-driven prod iteration, they generate fresh keys themselves and set up a separate `.mcp.json` — that's user-driven, out of v1 scope. |
| Rollback tool (`zerops_pipeline action="activate"`) | **DEFERRED** (backlog) | Zerops UI handles rollback; no v1 need. Backlog item with trigger condition: "if eval scenario surfaces rollback as friction." |
| Recipe knowledge updates per-runtime | **MOSTLY CUT** | Only ship `production-launch-checklist.md` atom + the launch-specific atoms below. Per-runtime prod recipe expansion is a separate scope (Aleš's domain — recipes). |
| Cost ack mandatory gate | **DEFERRED** (per user "ted bych neresil") | v1 ships an informational cost preview in `ready-to-launch` response, but no `confirmCost` field requirement. Backlog: add gate if launch-fails-due-to-budget show up in evals. |

---

## 10. How each source mode maps (simplified from v1)

| Source mode | Transition shape |
|---|---|
| `ModeStandard` (dev half of pair) | Default. Bundle the dev-half runtime + all managed deps. Stage half untouched. |
| `ModeStage` | Reject. Redirect to dev-half — same as today's export. |
| `ModeSimple` | Bundle the single runtime + managed deps. |
| `ModeDev` (legacy dev-only) | Bundle as if simple. Warning emitted that no staging step happened. |
| `ModeLocalStage` | Bundle from local `zerops.yaml` + project envs. |
| `ModeLocalOnly` | Bundle from local zerops.yaml + git remote. No source Zerops project means scope-prompt collects everything fresh (no `Discover` call against source). |

---

## 11. Phased implementation — 5 phases, ~5 weeks

### Phase A — Platform spike (1 week)

**Goal:** verify every platform assumption before any code commits to a contract.

Tasks (each produces a written finding):
- A.1: Generate a one-shot Zerops API key, call `PostClientProjectImport` against eval-zcp playground, observe response shape + async-process semantics. Note process polling cadence + completion signal.
- A.2: Call `DeleteProject` on a throwaway, observe async-process behavior + how long until resources actually free.
- A.3: Read `GetProjectL7httpbalancerConfig` for an existing project. Confirm the whole-config shape vs incremental routes. Document why mutation deferred.
- A.4: Verify region/location field — what values are accepted, what defaults, does it affect available core packages.
- A.5: Verify `PostClientProjectImport` rejects non-yaml-valid input (does platform run schema validation server-side, or does it accept anything and fail at provision?).
- A.6: Verify `PostProject.MaxCreditLimit` behavior — does it actually halt at limit or just warn?
- A.7: Document `ProjectModeEnum` — `LIGHT` vs `SERIOUS` semantics, can we change after create?
- A.8: Verify one-shot key permissions — what scopes are available? Can a key be scoped to single project? Or is it always full-org? (This determines how "one-shot" actually constrains blast radius.)

Output: `docs/spec-launch-production-platform-spike.md`.

**Phase A is mandatory; the rest of the plan locks contracts that depend on A's findings.**

### Phase B — `ProjectAdminClient` interface (1 week)

- Implement `internal/platform/project_admin.go` (the interface + constructor).
- Implement against `zerops-go` SDK: `CreateAndImportProject` → `PostClientProjectImport`; `GetProjectImportStatus` → poll process; `ListServices` / `GetServiceEnv` / `GetProjectEnv` for verify-only; `DeleteProject` → `DeleteProject` SDK.
- Mock implementation at `internal/platform/project_admin_mock.go` for tests.
- Unit tests: each method, mock-only.
- E2E test (opt-in `ZCP_E2E_PROD_LAUNCH=1`): real platform call, create + delete throwaway project, asserts shape.
- **No tool yet exposes this interface.** Phase D wires it.

Verification: `go test ./internal/platform/... -race`.

### Phase C — `LaunchBundleBuilder` + source-control mutation atom (1 week)

- `internal/ops/launch_bundle.go` — composes prod bundle from source-project state. Reuses pieces of `ops/export_bundle.go`'s machinery without altering its public surface.
- Bundle transforms documented in spec:
  - Managed-service `mode: HA` (with `keepNonHA` opt-out per-hostname).
  - Runtime `mode: NON_HA` (platform constraint).
  - `minContainers ≥ 2` for runtime.
  - `cpuMode: DEDICATED` for runtime.
  - `verticalAutoscaling` envelope per service-type defaults (table in `docs/spec-launch-production.md`).
  - `enableSubdomainAccess` stripped from runtime entries.
  - Project envs filtered per classify-prompt bucket map.
  - Tags: `["env:prod", "source-project:<sourceID>", "managed-by:zcp-launch"]`.
- Source-snapshot computation: git SHA, zerops.yaml SHA-256, project-env digest, service-list digest.
- Atoms for the source-control mutation phase: `launch-write-prod-setup.md` (instructs agent to append `setup: prod` block to source zerops.yaml + commit + push).
- Unit + tool-layer tests for bundle composition.

### Phase D — Launch workflow handler + one-shot key handling (2 weeks)

Splits internally:

**D.1 (1 week)** — handler skeleton + read-side narrowing path:
- `internal/tools/workflow_launch_production.go` — handler with status dispatch (scope-prompt, classify-prompt, ready-to-launch).
- `internal/topology/types.go`: add `LaunchProductionStatus` enum (6 values) + `Blocker` struct + reuse existing `CheckResult`.
- `WorkflowInput` fields: `productionProjectName`, `region`, `customDomain`, `keepNonHA`, `envOverrides`, `launchKey` (json:"-" marshaling).
- State persistence at `.zcp/state/launch-production/{launchID}.json`.
- Atoms: `launch-scope-prompt`, `launch-classify-prompt`, `launch-ready-to-launch`, `launch-source-control-required`, `launch-delete-key`, `launch-post-checklist`.
- Tests per status branch.

**D.2 (1 week)** — mutation pipeline + idempotent resume + audit log:
- Wire `LaunchBundleBuilder` → `ProjectAdminClient.CreateAndImportProject` → poll loop → verify external-secret presence → checks rubric.
- Source-immutability guard before each mutation.
- Failure cleanup with explicit `delete-target` sub-action.
- Audit manifest writer.
- Compaction-recovery tests (`TestLaunchStatus_RecoversAfterRestart_PollsLiveImport`).
- Orphan-cleanup test (`TestLaunchProduction_ImportFailsAfterCreate_OffersCleanup`).
- Key-safety test (`TestLaunchWorkflow_LaunchKeyNeverWrittenToState`).
- Integration test through MCP server with mocks.

### Phase E — Readiness checks + eval scenarios (1 week)

- Production-readiness check group (split blocking vs advisory):
  - **Blocking** (must pass or explicit waive): HA on managed services, `minContainers ≥ 2` on runtime, `enableSubdomainAccess: false`, `run.healthCheck` present, schema-valid.
  - **Advisory** (warn-only): debug env vars absent, real SMTP wired, cpuMode DEDICATED, daily backups present (read-only check, no setup).
- Each check: existing `CheckResult` shape with optional `Recovery`.
- Behavioral-eval scenarios (under `eval/behavioral/scenarios/`):
  1. `launch-production-from-standard-pair.md`
  2. `launch-production-from-simple.md`
  3. `launch-production-from-local-only.md`
  4. `launch-production-source-drift-detected.md` (mid-launch dev mutation triggers source-immutability guard)
  5. `launch-production-import-fail-orphan-cleanup.md`
- One local-mode sibling: `eval/behavioral/scenarios-local/launch-production-from-local-stage.md`.

Eval scenarios run **starting in Phase D.1** (not waited until end) so workflow surface gets exercised continuously.

Total: ~5 weeks. Phase A is gating; B+C can parallelize (1 week wall-clock for both); D is the bulk (2 weeks); E is final week.

---

## 12. Test plan

| Layer | Files | Estimated tests |
|---|---|---|
| Unit (`internal/topology/`) | `launch_production_status_test.go` | ~10 (enum coverage, blocker shape, status transitions) |
| Unit (`internal/ops/`) | `launch_bundle_test.go` | ~30 (transforms per service-type, source-snapshot hashing, hash drift detection) |
| Unit (`internal/platform/`) | `project_admin_test.go` | ~15 (SDK wrapper success/failure paths, key never logged, key cleared on Close) |
| Tool (`internal/tools/`) | `workflow_launch_production_test.go` | ~25 (each status branch, error paths, compaction recovery, key-safety) |
| Integration | `launch_production_test.go` | ~6 (full flow through MCP server, mocks for ProjectAdminClient) |
| E2E (`e2e/`, opt-in `ZCP_E2E_PROD_LAUNCH=1`) | `launch_production_e2e_test.go` | ~3 (create-import-delete roundtrip, source-immutability guard live, orphan-cleanup live) |
| Behavioral eval | scenarios above | 6 |
| Scenarios fixture | `internal/workflow/scenarios_test.go` | add `LaunchProductionActive_*` 6 states |

Total: ~95 unit, ~6 integration, ~3 e2e, 6 evals. Smaller than v1's estimate, more focused.

### Critical invariants to pin

- **P-LP-1:** Launch-window key never persisted. `TestLaunchWorkflow_LaunchKeyNeverWrittenToState` greps state files + log output post-run.
- **P-LP-2:** `ProjectAdminClient` constructor is reachable only from `workflow_launch_production.go`. `TestProjectAdminClient_RestrictedImport` (structural grep).
- **P-LP-3:** Source-immutability guard fires before every mutation. `TestLaunchProduction_DriftBetweenReadyAndPublish_Rejects`.
- **P-LP-4:** `launched` response always carries the key-deletion atom. `TestLaunchedResponse_AlwaysContainsKeyDeletionStep`.
- **P-LP-5:** External secret values are never read by ZCP. `TestProjectAdminClient_GetServiceEnv_OmitsValues` (response struct lacks Value field for sensitive entries).
- **P-LP-6:** Audit log writes are append-only. `TestAuditLog_AppendOnlyMode`.

---

## 13. Documentation + atoms

### New spec

- `docs/spec-launch-production.md` — full design + invariants P-LP-1..6. Effectively this plan, promoted to spec after Phase E ships.
- `docs/spec-workflows.md` — add §10 "Launch Production Flow" mirroring §9's structure.

### New atoms (`internal/content/atoms/`)

(Scoped via `phases: [launch-production-active]` + per-status axis where applicable.)

- `launch-intro.md` — universal framing.
- `launch-scope-prompt.md` — initial input collection.
- `launch-classify-prompt.md` — env classification with prod overrides; explicit redaction.
- `launch-write-prod-setup.md` — source-control mutation (write+commit+push `setup: prod` block).
- `launch-source-control-required.md` — fall-through atom when git-push isn't set up yet.
- `launch-ready-to-launch.md` — preview render: cost estimate, bundle summary, external-secret checklist, key-acquisition steps.
- `launch-mutation-key-required.md` — prompt for one-shot launch key, includes link to Zerops dashboard.
- `launch-launching.md` — poll progress display.
- `launch-failed-recovery.md` — uses blockers[] structure to drive recovery.
- `launch-delete-key.md` (**priority 1, mandatory in launched response**) — explicit key-deletion instruction with link.
- `launch-post-checklist.md` — what user does next (set external secrets in UI, attach custom domain in UI, verify DNS, etc.).
- `launch-dns-verify.md` — DNS-propagation + HTTP-readiness probe pattern for custom domain.

Atom count: 12 (down from v1's 14, status axis carries some load).

### Guide content (out of scope; coordinate with docs team)

- `zerops-docs/guides/launch-to-production.mdx` — user-facing walkthrough.

---

## 14. Q1 follow-up — access, keys, CI/CD answered explicitly

User's question: *"jak tam chces vyresit pristup do jineho projektu, klice, nastaveni cicd atd?"*

| Concern | v2 answer |
|---|---|
| **Access to another project** | ZCP_API_KEY is org-scoped, used for dev/stage. For prod, user generates a **one-shot launch-window key** in Zerops dashboard, passes it on workflow invocation, deletes it after `launched`. ZCP never has standing prod access. |
| **External secrets (Stripe, OpenAI, etc.)** | User sets directly in Zerops UI per service. ZCP receives a checklist of which secrets to set; verifies their presence via `GetServiceEnv` (returns keys only, never values) using the launch-window key during the launch. After launch, ZCP cannot access them at all. |
| **CI/CD setup for prod** | After `launched`, user generates a fresh prod-scoped key (separate from the launch-window key, if they want ongoing automation) OR keeps prod fully manual. The existing `git-push-setup` + `build-integration` workflows continue to work for the source project's build-from-git into prod — the source repo's `setup: prod` block + tag trigger drives prod builds. For GitHub Actions / webhook setup on the prod project specifically, that's user-managed in Zerops UI (or backlogged `auto-wire-github-actions-secret.md` — separate plan). |
| **Tag-trigger setup** | Existing `build-integration` workflow extended with optional `tagRegex` parameter (default `^v\d+\.\d+\.\d+$`). Minor follow-up, +1 file. Atom: `build-integration-tag-trigger.md`. Lands in Phase D.1. |

---

## 15. Risks (carry from v1, updated)

| Risk | Likelihood | Mitigation |
|---|---|---|
| HA mode immutable → wrong-HA at create blocks user | Medium | `ready-to-launch` shows explicit HA decision per managed service; `keepNonHA` opt-out. |
| Launch key leaks into transcripts | High if not careful | json:"-" marshaling, log scrubbing, structural grep test, post-`launched` mandatory delete atom. |
| User keeps deploying to dev mid-launch | Medium | Source-immutability guard rejects mutation with `source-drift` blocker. |
| Import fails → orphan prod project (billable empty shell) | High (real failure mode) | Explicit `delete-target` sub-action; `failed` blocker recovery offers cleanup. |
| Compaction loses launch state | Medium | State at `.zcp/state/launch-production/{launchID}.json` (not pid-scoped); server-truth re-query on `action="status"`. |
| User pastes wrong key OR expired key | Medium | Cheap validation call before mutation; clear error message. |
| User forgets to delete key after launch | High | Mandatory `launch-delete-key` atom priority 1 in `launched` response. (Cannot enforce server-side, but persistent reminder. Future v2: ZCP could ping a key-status endpoint and warn if key still active 24h after launch — backlog.) |
| `PostClientProjectImport` async behavior misunderstood | Mitigated by Phase A spike | Phase A documents the exact response + poll semantics before Phase D commits. |
| Cost surprise on first invoice | Medium | `ready-to-launch` includes informational cost estimate. Per user, no gate in v1. |

---

## 16. Scope cuts — explicit non-goals

- Multi-cloud or non-Zerops deploys.
- Generic any-env-to-any-env promotion (production is special).
- Auto-rollback on prod failure.
- Continuous deployment orchestration (canary, blue-green, manual gates).
- PII-aware data migration (no data migration at all in v1).
- Backup configuration tooling (verify presence only, no setup).
- Monitoring / alerting setup.
- Cross-region replication (one region per invocation; multi-region is a second invocation).
- Custom-domain L7 mutation (guidance + DNS verify only; mutation = backlog).
- Pipeline rollback tool (backlog).
- ZCP-managed prod sessions / adopt-production handoff.
- Cost gate / `confirmCost` requirement (deferred per user).

---

## 17. Verification — definition of done

After Phase E, a user with a working dev/stage Zerops project can:

1. Run `launch-production` workflow, reach `ready-to-launch` showing: bundle preview, cost estimate, external-secret checklist, key-acquisition link.
2. Generate one-shot key, paste as `launchKey`, run `action="publish"`.
3. Watch `launching` poll through import → deploy → secret-presence verify → checks rubric.
4. Reach `launched` with: prod project URL, services list, checks results, explicit key-deletion instruction, what's left for them.
5. Delete the launch-window key in Zerops UI.
6. Independently set external secrets in Zerops UI per the checklist.
7. Independently attach custom domain in Zerops UI per ZCP's emitted DNS records.
8. ZCP has no ongoing access to prod project. User-on-the-line principle held.

Six eval scenarios pin the source-mode coverage + source-drift + orphan-cleanup. Three e2e tests pin real-platform behavior. The lifecycle is closed.

---

## Appendix A — Citations of key research findings (carried, corrected)

- Zerops platform docs (Agent A): `zerops-docs/guides/production-checklist.mdx`, `features/infrastructure.mdx:21-23`, `features/scaling.mdx:303-331`, `references/networking/public-access.mdx:51-92`, `references/github-integration.mdx:45`, `features/pipeline.mdx:447, 467-471`, `features/backup.mdx:32-78, 187`.

- ZCP source (Agent B + corrections): `internal/topology/types.go:22-29` (Mode enum — unchanged in v2), `internal/topology/predicates.go:90-98`, `internal/topology/types.go:116-184` (ExportVariant + ExportStatus unchanged), `internal/tools/workflow_export.go:52`, `internal/ops/export_bundle.go:38-72`, `internal/platform/client.go:12` (**ListProjects exists**, v1 missed this), `docs/spec-workflows.md §9 (1104-1160)`.

- Zerops frontend (Agent C): no clone/promote anywhere in `apps/zerops/src/` — gap is platform-product-level.

- Zerops SDK (`zerops-go@v1.0.17`): `sdk/PostClientProjectImport.go` (create+import in one call), `sdk/PostProject.go` (with `MaxCreditLimit`, `Location`, `SshIsolation` fields), `sdk/DeleteProject.go`, `sdk/GetProjectL7httpbalancerConfig.go`, `sdk/PostProjectPublicHttpRouting.go` (exists, mutation deferred), `dto/input/body/postProject.go`.

- Review feedback (Plan agent + Codex, both completed 2026-05-11):
  - Convergent on: ModeProduction wrong axis, ProjectAdminClient interface segregation, status machine collapse to 6, source-control mutation missing, secrets handling unsafe, cost gate, source-immutability guard, orphan-cleanup, audit log.
  - Plan-agent specific: `EnvStage` axis on ServiceMeta as alternative (deferred — prod ServiceMeta doesn't exist in v1 because user-on-the-line).
  - Codex specific: `TargetProfile`/`LaunchTarget` framing (adopted), false dichotomy in tradeoffs (Q3 collapsed to guidance-only), platform spike as Phase A.

- User feedback (this session):
  - Q1=A (cross-project surface yes) → with one-shot key model.
  - Q2=cut entirely (no data migration).
  - Q3=B (guidance + DNS verify, no L7 mutation) — locked despite SDK availability.
  - Q4=ted bych neresil (cost gate deferred).
  - Q5=user-sets-in-UI-ZCP-verifies (secrets entry).
  - Q6=user-on-the-line (no adopt handoff).
  - One-shot key refinement: ZCP uses key during launch window, user deletes after.

---

**End of plan v2. Ready for Phase A platform spike.**
