# Bootstrap Revision — Mental Model Analysis Findings

Multi-agent stress-test of `plans/analysis-bootstrap-revision.md`. Four specialized agents (Architect, Combinatorics, Devil's Advocate, Pragmatist) independently analyzed the plan against the codebase. Findings cross-validated and deduplicated below.

---

## Critical — Must fix before implementation

### C1. Verification endpoint model needs runtime-class awareness and simplification

**Impact**: (a) Any bootstrap with `static` or `nginx` runtime fails verification unconditionally — these runtimes cannot implement `/status` or `/health`. (b) The current 3-endpoint contract (`/`, `/health`, `/status`) is more complex than necessary — `/health` is provably redundant. (c) Services without subdomain enabled pass verification with all HTTP checks skipped, creating a false-healthy gap.

**Detail — verified on live Zerops services (2026-03-04):**

The plan requires every generated app to expose 3 endpoints. Live testing reveals:

1. **`/health` is redundant with Zerops platform health probing.** The platform runs its own `Zerops-http-probe/1.0` at a configurable path (`healthCheck.httpGet.path` in zerops.yml) — e.g., `/up` on laravelstage, `/health` on nodejsstage. Our verify check #5 (`http_health`) hits `/health` — a DIFFERENT path from the platform probe. It provides the same signal as `GET /` (HTTP server responds). When `/status` fails, the detail message already specifies which connection broke — `/health` passing adds no diagnostic value.

2. **Static/nginx cannot implement any dynamic endpoint.** They serve files from disk. No `/health`, no `/status`. Any bootstrap with these runtimes fails verification.

3. **Subdomain-off = false healthy.** `persisttest` returns plain text `ok` at `/status` (NOT valid JSON) but reports `status: "healthy"` because subdomain isn't enabled and both `http_health` and `http_status` are skipped. Any service without subdomain bypasses all HTTP verification.

4. **Dev services MUST NOT have healthCheck configured.** Dev uses `start: zsc noop --silent` (no server running). If healthCheck were configured, the platform probe would fail and kill the container. Verified: nodejsdev has no healthCheck.

5. **`hostname` env var is always available.** Zerops injects `hostname=<service_hostname>` in every container. Apps should use this for self-identification instead of hardcoding. Verified bug: nodejsstage code hardcodes `'nodejsdev'` in root response — wrong hostname on stage.

**Evidence**:
- `ops/verify_checks.go:checkHTTPStatus()` — requires `/status` JSON, impossible for static/nginx
- `ops/verify.go:135-152` — subdomain URL empty → skip http_health + http_status → healthy with no HTTP checks
- `laravelstage` logs: `Zerops-http-probe/1.0` probes `/up`, not `/health`
- `persisttest`: returns `ok` (plain text) at `/status`, passes verify as healthy
- `nodejsdev` zerops.yml: no healthCheck (correct for dev)
- `nodejsstage` zerops.yml: `healthCheck.httpGet.path: /health`
- All containers: `hostname` env var = service hostname (e.g., `hostname=nodejsstage`)

**Fix — simplify to 2-tier, 2-endpoint model:**

Runtime classification for verification (new function in `ops/verify.go`, separate from `hasImplicitWebServer()` which answers a different question about `run.start`/`run.ports`):

| Runtime class | Examples | Can execute code? | Verify endpoints |
|---------------|----------|-------------------|------------------|
| **Explicit server** | nodejs, go, bun, python, rust, java, deno, dotnet | Yes | `GET /` + `GET /status` |
| **Implicit webserver + app logic** | php-apache, php-nginx | Yes (PHP) | `GET /` + `GET /status` |
| **Static-only** | static, nginx | No | `GET /` only |

LLM generation contract (simplified from 3 to 2 endpoints):

| Class | What LLM generates | Verify check |
|-------|--------------------|----|
| Dynamic (explicit + implicit) | `GET /` → any 200 response; `GET /status` → connectivity JSON | `http_root` (200) + `http_status` (JSON) |
| Static-only | `index.html` containing hostname | `http_root` (200 + body contains hostname) |

Verify check changes:
```
Current:  service_running → logs → startup_detected → logs → http_health(/health) → http_status(/status)
Revised:  service_running → logs → startup_detected → logs → http_root(GET /) → http_status(/status)
```

- `http_health` (GET /health) → **REPLACED** by `http_root` (GET /) — same signal (HTTP 200) plus deployment proof for static
- `http_status` (GET /status) → **SKIP for static/nginx** (no server-side logic)
- `startup_detected` → **SKIP for implicit webserver + static** (detects Apache/nginx startup, not PHP readiness — see M1)
- `http_root` body check → hostname-in-body ONLY for static/nginx (dynamic runtimes may return full HTML apps where hostname isn't present)

zerops.yml changes:
- Dev: `start: zsc noop --silent`, NO healthCheck (would kill container)
- Stage: `start: {real command}`, NO healthCheck for bootstrap scaffolds (process monitoring sufficient; user configures own healthCheck when replacing scaffold with real app)
- `/health` endpoint eliminated entirely — not generated, not checked, not in healthCheck config

Subdomain gate: bootstrap verify step must enforce subdomain enabling as a hard prerequisite. Without it, all HTTP checks are skipped and the service falsely reports healthy.

Why `/status` stays at `/status` (not merged into `/`):
1. **HealthCheck isolation** — if someone configures `healthCheck.httpGet.path: /`, DB queries at root would cascade DB outages into container restarts
2. **Diagnostic clarity** — `http_root` fail = app crashed; `http_root` pass + `http_status` fail = app works but DB/cache wiring broken
3. **Landing page stays human-friendly** — root returns app content, not JSON

---

### C2. Env var NAME validation missing from generate hard check

**Impact**: LLM can write `DATABASE_URL: ${db_totallyFakeVar}` — Zerops accepts it without error, deploy succeeds, but the container receives the literal string `${db_totallyFakeVar}` instead of a resolved value. Application gets a nonsense connection string and fails with a cryptic error (not "missing env var" but e.g. "invalid URL").

**Detail**: The plan's generate hard check validates "envVariables only reference discovered env var names (hostname prefix match)." This only checks hostname prefix, not the actual variable name suffix. The discovered env var might be `connectionString` but the LLM could reference `db_CONNECTION_STRING` or `db_databaseUrl`.

**Platform behavior (verified 2026-03-04 on live Zerops — nodejsdev service)**:
- `${db_connectionString}` → resolved correctly to `postgresql://db:...@db:5432`
- `${db_totallyFakeVar}` → literal string `${db_totallyFakeVar}` in container env (valid hostname, invalid var name)
- `${nonexistent_something}` → literal string `${nonexistent_something}` in container env (invalid hostname)
- Zerops API accepts all three without error. No deploy failure. No warning. Silent data corruption.

**Evidence**: `ops/deploy_validate.go:ValidateZeropsYml()` has no env var name validation currently. The plan proposes hostname-prefix matching but doesn't validate the full reference.

**Fix**: Store discovered env var names per service in session state (from discover-envs step). In the generate hard check, parse `envVariables` values for `${hostname_varName}` patterns and validate both:
1. `hostname` exists as a service in the project
2. `varName` exists in the discovered env var set for that hostname (case-sensitive match)

---

### C3. Cross-target dependency ordering with EXISTS resolution

**Impact**: When Target A depends on a managed service that Target B creates (both in same bootstrap), the `EXISTS` resolution is set at plan time but the service doesn't exist yet.

**Detail**: The plan proposes `Resolution: "CREATE" | "EXISTS"` set during discover. In Scenario D (multi-runtime), if runtime A needs PostgreSQL and runtime B also needs it, the plan marks it `CREATE` for A and `EXISTS` for B. But if provision runs A first and fails, B's `EXISTS` dependency fails too.

**Evidence**: `validate.go:ValidateServicePlan()` checks for duplicate hostnames but has no dependency resolution logic.

**Fix**: Two-phase dependency resolution:
1. At plan time: mark all dependencies as `CREATE` or `EXISTS_EXTERNAL` (pre-existing in project)
2. At provision time: resolve `CREATE` dependencies first, then validate `EXISTS_EXTERNAL` against API
3. Intra-session `EXISTS` (same bootstrap creates it) should be `SHARED` — provision deduplicates automatically

---

### C4. `zeropsYmlBuild.Base` string type breaks multi-base runtimes

**Impact**: PHP+Node builds (e.g., Laravel with Vite) use `base: [php@8.4, nodejs@22]` which is a YAML array, but `zeropsYmlBuild.Base` is typed as `string` in the validation code. Array values are silently dropped during YAML unmarshal.

**Detail**: Zerops docs confirm `build.base` accepts both string and array. Array installs multiple runtimes in the build container — used by Laravel Jetstream, Twill CMS, and any PHP framework with JS tooling. Real project services (`laraveldev`/`laravelstage`) currently use single `base: php@8.4` but would break if Vite were added.

Three copies of `zeropsYmlBuild` exist in the codebase with inconsistent typing:
- `ops/deploy_validate.go:98` — `Base string` (**wrong**, silently drops arrays)
- `eval/prompt.go:115` — `Base any` (correct, defensive type assertion)
- `knowledge/recipe_lint_test.go:229` — `Base any` (correct)

`hasImplicitWebServer(runBase, buildBase string)` in `deploy_validate.go:137` also only accepts string — cannot detect `php-nginx` inside an array base.

Note: `run.base` is always a single runtime string — only `build.base` supports arrays.

**Fix**: In `deploy_validate.go`:
1. Change `Base` to `any` (matching `eval/prompt.go` and `recipe_lint_test.go`)
2. Add `baseStrings()` normalizer (following existing `deployFilesList()` pattern)
3. Change `hasImplicitWebServer()` to accept `[]string` for buildBases
4. Update call site: `hasImplicitWebServer(entry.Run.Base, entry.Build.baseStrings())`
5. Add test cases for array base, including implicit webserver detection within arrays

---

### C5. Partial import has no recovery path

**Impact**: If `zerops_import` creates 3 of 5 services and then fails, the bootstrap is stuck — re-running import fails on duplicate hostnames, but skipping import leaves services incomplete.

**Detail**: The plan consolidates generate-import + import-services into a single provision step but doesn't address partial failure recovery.

**Evidence**: `zerops_import` is atomic at the API level — but service creation within it is sequential. A failure mid-import leaves some services created and others not.

**Fix**: Add idempotent import handling:
1. Before import, query existing services via `zerops_discover`
2. Filter the import YAML to only include services that don't already exist
3. If all services exist, skip import entirely
4. Document this in the provision step hard check

---

## High — Should fix before or during implementation

### H1. `BootstrapComplete` needs `context.Context` parameter

**Impact**: Hard checks that call the Zerops API (verify service state, check env vars) need context for timeouts and cancellation.

**Detail**: Current signature: `BootstrapComplete(stepName, attestation string)`. Hard checks need API access for deterministic validation.

**Evidence**: `engine.go:BootstrapComplete()` — no context parameter. All `platform.Client` methods require context.

**Fix**: Change to `BootstrapComplete(ctx context.Context, stepName string, opts ...CompleteOption)`. The attestation becomes optional (hard checks generate their own evidence).

---

### H2. BuildInstructions routing gap for Scenario B

**Impact**: When a CONFORMANT project needs a NEW runtime added (Scenario B), `BuildInstructions()` routes to deploy workflow instead of bootstrap.

**Detail**: `instructions.go:BuildInstructions()` detects CONFORMANT state and says "use deploy workflow." But adding a new runtime to an existing project requires bootstrap (new services, new import, new code generation).

**Evidence**: `internal/server/instructions.go` — CONFORMANT routing logic.

**Fix**: Add stack-match detection: if user intent includes a runtime type not present in existing services, route to bootstrap even when CONFORMANT. The detect step's guidance already handles this ("if stack matches request, skip to deploy. If user wants a different stack, ASK"), but `BuildInstructions` short-circuits before reaching detect.

---

### H3. Stage deploy failure doesn't block bootstrap completion

**Impact**: Bootstrap can report "success" with only dev deployed and stage failing silently.

**Detail**: The plan's verify step says "Do NOT block — the conductor accepts partial success." This is appropriate for dev services but stage failure should at minimum be prominently flagged.

**Evidence**: `bootstrap_steps.go` verify step guidance: "Record the failure in attestation... Do NOT block."

**Fix**: Distinguish between "advisory partial success" (dev working, stage build timing out) and "real failure" (dev not working). The hard check for verify should require at minimum: all dev services passing `http_root` + `http_status` + all managed services RUNNING. Stage failures are warnings, not blockers. **Prerequisite**: subdomain must be enabled before verify — without it, HTTP checks are skipped and services falsely report healthy (see C1 subdomain gap finding).

---

### H4. Session state file has no locking — multi-session registry design

**Impact**: Two LLM sessions can corrupt `zcp_state.json` by writing simultaneously. Crashed processes leave stale sessions that block new ones ("active session exists, reset first"). Only one workflow can run per project at a time.

**Detail**: `session.go` uses atomic temp+rename writes, which prevents partial writes but not lost updates. Two sessions reading the same state, modifying independently, and writing back will lose one session's changes. The singleton model (`stateFileName = "zcp_state.json"`) forces serial workflow execution — a bootstrap blocks any concurrent deploy.

**Evidence**: `session.go:saveState()` — no file locking, no compare-and-swap. `engine.go:Start()` — rejects if any session exists ("active session in phase %s, reset first"). No PID tracking, no stale detection.

**Fix**: Replace singleton state file with a **registry + per-session files** pattern.

#### Architecture

```
.zcp/state/
  registry.json          # Index of sessions (flock-protected)
  sessions/
    {id1}.json           # Full WorkflowState (single-writer: owner PID)
    {id2}.json
  evidence/              # Unchanged — per-session evidence
    {id1}/*.json
```

#### Registry (`registry.json`)

```go
type Registry struct {
    Version  string         `json:"version"`
    Sessions []SessionEntry `json:"sessions"`
}

type SessionEntry struct {
    SessionID string `json:"sessionId"`
    Workflow  string `json:"workflow"`   // "bootstrap", "deploy"
    Phase     Phase  `json:"phase"`      // summary for quick reads
    PID       int    `json:"pid"`        // owning process
    Stale     bool   `json:"stale"`      // true if PID dead + Phase != DONE
    CreatedAt string `json:"createdAt"`
    UpdatedAt string `json:"updatedAt"`
}
```

#### Ownership model: leader-only

Each session is owned by exactly one process (PID). Worker agents (in team/swarm scenarios) never touch session state — they use Zerops API directly and report results to the leader via SendMessage. The leader updates session state based on worker results. This eliminates multi-writer complexity entirely.

#### Locking: flock on registry only

| Operation | Lock | Rationale |
|-----------|------|-----------|
| Register/unregister session | `LOCK_EX` on `registry.json` | Multi-process safety for index |
| List sessions | `LOCK_SH` on `registry.json` | Consistent reads |
| Read/write session file | **None** | Single-writer (owner PID), temp+rename atomicity suffices |

No session-level flock needed — only one process ever writes to a given session file. Lock held only for brief registry JSON read-modify-write (milliseconds).

#### Stale detection: PID-based, no auto-delete

```go
func processAlive(pid int) bool {
    return syscall.Kill(pid, 0) == nil
}
```

On every registry read: dead PID + `Phase != DONE` → `Stale: true`. Dead PID + `Phase == DONE` → normal (completed, historical record). Stale sessions are reported in workflow hint but **never auto-deleted** — completed sessions serve as historical reference (attestations, evidence, plan). The LLM's conversation references session IDs. User explicitly cleans via `action="reset" sessionId="..."`.

#### Constraints

- **One active bootstrap per project** (non-stale, non-DONE). Bootstraps modify infrastructure — concurrent bootstraps would conflict.
- **Multiple deploys OK** — different services, no conflict.
- **Immediate workflows** (debug, scale, configure) — stateless, no session, no constraint.

#### Engine changes

```go
type Engine struct {
    stateDir    string
    evidenceDir string
    sessionID   string  // NEW: set after Start(), empty before
    pid         int     // NEW: os.Getpid()
}
```

- `HasActiveSession()` → `return e.sessionID != ""` (no file I/O, process-local check)
- `Start()` registers in registry, creates `sessions/{id}.json`, sets `sessionID`
- `Reset()` unregisters from registry, deletes session file, clears `sessionID`; accepts optional `sessionId` for targeted cleanup of stale sessions
- New: `SessionID()`, `ListSessions()` methods

#### Consumer changes

- `buildWorkflowHint()` reads registry, shows all active sessions + stale warnings
- `requireWorkflow()` unchanged API — `HasActiveSession()` still returns bool
- `action="status"` includes session list; `action="reset"` accepts optional `sessionId`

#### Migration

Not needed. ZCP is in early development — no backward compatibility requirement for state files. Old `zcp_state.json` can be ignored or deleted. New sessions start fresh with the registry model.

#### Implementation: new file `internal/workflow/registry.go` (~120 lines)

Types, `withRegistryLock()` flock wrapper, `registerSession()`, `unregisterSession()`, `listSessions()`, `refreshStale()`, `processAlive()`. Session file functions in `session.go` gain a `sessionID` parameter for path resolution.

---

### H5. SSHFS mount — deploy overwrites filesystem

**Impact**: Files written via SSHFS mount are lost when deploy runs, because deploy replaces the container filesystem with the build output (deployFiles).

**Detail**: Zerops containers are NOT volatile on restart — they survive container restarts, scaling events, and platform updates. The only events that replace the filesystem are: (1) a new deploy (build output replaces prior content), and (2) hardware failure (rare). This means mounted files are safe between writes and restarts, but the act of deploying itself overwrites them. For dev services using `deployFiles: [.]`, this is fine — the deploy includes all files. But if the mount path and deploy source diverge (e.g., files written to mount but deploy triggered from a different source), work is lost.

**Evidence**: `bootstrap_steps.go` generate-code step says "Consider committing generated code before proceeding to deploy" — but the real risk isn't container volatility, it's deploy overwrite semantics.

**Fix**: Downgrade from original "volatility" concern. The actual guard needed: ensure dev services use `deployFiles: [.]` so that deploy preserves all files written via mount. This is already enforced by the generate hard check ("Dev setup uses deployFiles: [.] — NO EXCEPTIONS"). No additional pre-deploy file existence check needed.

---

### H6. `validateConditionalSkip` step constants won't match new step names

**Impact**: If steps are renamed (11→5), the skip guard constants (`stepDiscoverEnvs`, `stepMountDev`, etc.) won't match, silently disabling conditional skip protection.

**Detail**: The plan consolidates steps but doesn't mention updating skip guard constants.

**Evidence**: `bootstrap.go` lines 24-29 — hardcoded step name constants.

**Fix**: When implementing step consolidation, update all constants and add a test that verifies every skip guard constant exists in `stepDetails[].Name`. Breaking backward compatibility here is fine — ZCP is in early development with no external consumers of the step name API.

---

### H7. Hostname length overflow for stage services

**Impact**: A dev hostname like `myapplicationdev` (16 chars) produces `myapplicationstage` (18 chars) — fine. But `myverylongservicenamedev` (23 chars) produces `myverylongservicenamestage` (25+ chars) — may exceed the 25-char limit.

**Detail**: The plan derives stage hostname by replacing `dev` suffix with `stage` (+2 chars). No validation catches this overflow at plan time.

**Evidence**: `platform.ValidateHostname()` checks max 25 chars but is called per-hostname, not on the derived pair.

**Fix**: In `ValidateServicePlan`, when processing dev/stage pairs, validate both the dev AND derived stage hostname lengths. Reject if either exceeds 25 chars.

---

### H8. Rust/Go/Java build timeouts

**Impact**: Compiled language first builds can take 5-10 minutes. Default polling timeout may expire.

**Detail**: The plan proposes faster polling (1s/5s/30s) but doesn't address total timeout. First Rust build downloads and compiles all dependencies.

**Evidence**: `ops/progress.go` — adaptive polling intervals exist but total timeout is caller-controlled.

**Fix**: Add runtime-class-aware timeout defaults: interpreted (5min), compiled (15min), Rust (20min). Document in the deploy step guidance.

---

### H9. shared-storage fails provision env var hard check

**Impact**: shared-storage has no env vars (it's a mount-based service), but the provision hard check may expect env vars for all managed services.

**Detail**: The plan's provision hard check: "All managed services have env vars populated." shared-storage and object-storage don't follow the same env var pattern as databases/caches.

**Evidence**: `knowledge.ManagedBaseNames()` includes shared-storage in managed service list.

**Fix**: Exclude storage services from the env var hard check. Only database and cache services have connection env vars. Classify managed services as: `MANAGED_WITH_ENVS` (postgresql, mariadb, valkey, etc.) and `MANAGED_STORAGE` (shared-storage, object-storage).

---

### H10. KnowledgeTracker per-type tracking

**Impact**: The plan requires tracking which knowledge calls have been made per runtime type. Current `KnowledgeTracker` is boolean only.

**Detail**: For multi-runtime bootstrap, the system needs to know "has php-nginx briefing been loaded?" separately from "has nodejs briefing been loaded?"

**Evidence**: No `KnowledgeTracker` type exists in current codebase — it's a new addition in the plan.

**Fix**: Implement as `map[string]bool` keyed by runtime type. Track: runtime briefing per type, infrastructure scope (global), recipe per framework. Hard check for generate requires all planned runtime types have briefings loaded.

---

## Important — Address during implementation

### I1. Test blast radius understated

**Impact**: The `PlannedService` → `BootstrapTarget` change touches 30+ test cases across 4+ test files.

**Detail**: The plan lists implementation order but doesn't quantify test migration effort. Every test that creates a `ServicePlan` or calls `ValidateServicePlan` needs rewriting.

**Evidence**: `validate_test.go`, `bootstrap_test.go`, `engine_test.go`, `workflow_bootstrap_test.go` all use `PlannedService`.

**Mitigation**: Plan test migration as a separate task. No backward compatibility needed — ZCP is in early development. Delete `PlannedService` outright and rewrite all tests against `BootstrapTarget` in one pass.

---

### I2. `.zcp/services/{hostname}.json` decision metadata staleness

**Impact**: If a service is deleted externally (via Zerops GUI), its `.zcp/services/` file persists as stale metadata.

**Detail**: The plan proposes per-service decision files but has no cleanup mechanism.

**Mitigation**: On each `detect` step, reconcile `.zcp/services/` against live API state. Remove files for services that no longer exist.

---

### I3. Reflog in CLAUDE.md — not a real concern

**Impact**: Theoretical — unlikely to materialize.

**Detail**: Bootstrap runs are infrequent — they only happen when adding or removing runtime services, not for deploys, config changes, or scaling. A typical project might run bootstrap 2-5 times total. The reflog will stay small naturally.

**Mitigation**: No action needed. If it ever becomes a problem (unlikely), a simple cap at N entries is trivial to add later.

---

### I4. `autoCompleteBootstrap` evidence always reports Failed=0

**Impact**: None — this code is being replaced entirely.

**Detail**: Hard checks replace LLM attestation + synthetic evidence. No transition period needed (no backward compatibility). The `autoCompleteBootstrap` function and the evidence system it feeds will be deleted when hard checks land.

**Mitigation**: No action. Delete `autoCompleteBootstrap` and `bootstrapEvidenceMap` as part of the hard checks implementation.

---

### I5. generate guidance for `IsExisting` targets unclear

**Impact**: When a runtime already exists (Scenario C: adding managed services), the generate step needs to know whether to regenerate zerops.yml or just add env vars.

**Detail**: The plan mentions `IsExisting: true` on `RuntimeTarget` but the generate step guidance doesn't explain what to do differently for existing runtimes.

**Mitigation**: Add explicit guidance: "For IsExisting targets, ONLY update envVariables in zerops.yml. Do NOT regenerate application code or change build/run configuration."

---

## Minor — Nice to have

### M1. PHP `startup_detected` false positives (VERIFIED)

PHP with implicit webservers (php-apache, php-nginx) starts Apache/nginx which binds to the port, but the PHP app itself might not be ready. The `startup_detected` check passes on webserver startup, not app readiness. Verified on laraveldev: `startup_detected: pass` is from Apache startup logs, not PHP.

**Mitigation**: Skip `startup_detected` for implicit webserver runtimes (php-apache, php-nginx, nginx, static). Rely on `http_root` (GET /) for deployment proof. This is addressed as part of C1's runtime-class-aware verification.

---

### M2. Verify filters should exclude pre-existing unhealthy services

In Scenario B/C, pre-existing services that were already unhealthy before bootstrap shouldn't cause the verify step to report failure.

**Mitigation**: Filter verify results by services in the current `BootstrapTarget` list, not all project services.

---

### M3. `Simple` mode (single service, no dev/stage pair) underspecified

The plan mentions `Simple: true` on `RuntimeTarget` but doesn't detail how it affects provision (no stage hostname derivation), deploy (no stage deploy), or verify (single service check).

**Mitigation**: Add Simple mode handling to each step's hard check logic.

---

### M4. Performance improvements are independent and low-risk

The batch verify and polling speedup changes are purely additive and don't depend on the BootstrapTarget refactor. They can be implemented first as quick wins.

---

## Implementation Order Recommendation

Based on risk analysis, recommended order:

1. **Performance wins first** (M4): Batch verify + polling speedup — independent, low risk, immediate value
2. **Hard checks infrastructure** (H1): Add `context.Context` to `BootstrapComplete`, build hard check framework
3. **BootstrapTarget type** (I1): New type + test migration (biggest blast radius, do early)
4. **Step consolidation** (H6): 11→5 steps with updated constants + skip guards
5. **Runtime classification** (C1, H8, H9): Static/compiled/interpreted awareness for verify + timeouts + env checks
6. **Env var validation** (C2): Full name validation in generate hard check
7. **Dependency resolution** (C3): Two-phase EXISTS handling
8. **Multi-base support** (C4): Fix `zeropsYmlBuild.Base` type
9. **Import recovery** (C5): Idempotent import handling
10. **Session safety** (H4): File locking or session-scoped state
11. **Instructions routing** (H2): Fix CONFORMANT→bootstrap routing for Scenario B
12. **Metadata lifecycle** (I2, I3): Staleness cleanup + reflog rotation

---

## Verification Checklist

The plan is **architecturally sound** with these caveats:
- 5 critical gaps must be addressed before implementation
- 10 high-priority items should be fixed during implementation
- The BootstrapTarget refactor has significant test blast radius (plan accordingly)
- Performance improvements can ship independently as quick wins
- The "no persistent registry" decision is valid — API as source of truth is correct
- Hard checks replacing LLM attestation is the single most valuable change
- The dev/stage naming convention is pragmatic and working (don't change it)
