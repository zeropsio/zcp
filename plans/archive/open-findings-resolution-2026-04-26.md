# Plan: Open-Findings Resolution — 2026-04-26

> **Reader contract.** This plan is self-contained. A Claude Code instance
> opens the repo, reads this file, and can execute end-to-end per finding.
> No prior-conversation context is required. Cite this file by path when
> starting. The plan covers five open audit findings (G14, G10, G4, G5,
> G8) carried over from `docs/audit-instruction-delivery-synthesis-2026-04-26.md`.
> A sixth finding (G13 — corpus byte trim) is **out of scope** here and
> tracked separately in `plans/atom-corpus-context-trim-2026-04-26.md`.
>
> Recipe-related findings (G1, G2, G12) remain **out of scope per user
> direction** and are not addressed.

---

## 1. Scope at a glance

| ID  | Title                                              | Class               | Risk   | Touch           |
|-----|----------------------------------------------------|---------------------|--------|-----------------|
| G14 | Three error wire shapes coexist                    | structural debt     | HIGH   | wide refactor   |
| G10 | Errors carry no lifecycle pointer                  | structural debt     | HIGH   | wide refactor   |
| G4  | Orphan meta + externally-deleted service           | functional bug      | MEDIUM | medium refactor |
| G5  | Headless from non-init'd dir gets no doctrine      | doc / CLI warning   | LOW    | cmd + docs      |
| G8  | Per-service CLAUDE.md referenced but ungoverned    | template wording    | LOW    | template line   |

**G14 + G10 are bundled** — both touch the error response surface and
share the unified wire DTO produced in this plan. Splitting them produces
half-fixes that have to be re-walked.

**G4 is independent** of the error work — `compute_envelope.go` change.

**G5 + G8 are independent** of everything else — single-file edits + docs.

---

## 2. Codex critique that shaped the design

The plan went through two adversarial review passes. Pass-1 critiqued the
original "attach envelope+plan to errors" idea (six findings); the
redesign here addresses every one. Pass-2 critiqued the redesign itself
against the actual codebase shape (six more findings); §3, §4, and §5
were revised again before this version. Recording both here so the
implementer understands *why* the design below differs from the obvious
"just attach envelope" answer AND from a draft that would have hit
signature drift in Phase 2.

### 2.1 Pass-1 findings (against the envelope-attached design)

| Severity | What Codex caught                                            | How this plan addresses it                                    |
|----------|--------------------------------------------------------------|---------------------------------------------------------------|
| HIGH     | Envelope on errors competes with `status` (P4 contract)      | Errors carry a small **recovery hint**, not envelope + plan.  |
| HIGH     | `PlatformError` (layer 1) cannot know workflow types         | New tool-layer wire DTO `internal/tools/errwire.go` composes. |
| HIGH     | `ComputeEnvelope` on error path can mask the original failure| Recovery hint requires no I/O — it's a static pointer.        |
| HIGH     | `StateEnvelope` is unbounded; risks 32 KB MCP cap            | Recovery hint is < 200 B; envelope never travels with errors. |
| MEDIUM   | Plan must derive from envelope; passing both invites drift   | Plan never travels with errors; only the recovery hint does.  |
| MEDIUM   | `StepCheck` is bootstrap-shaped, not a generic check schema  | New wire DTO `CheckWire` with `kind` discriminator.           |

### 2.2 Pass-2 findings (against the redesign + plan execution shape)

| Severity | What Codex caught                                                      | How this plan addresses it                                                              |
|----------|------------------------------------------------------------------------|------------------------------------------------------------------------------------------|
| HIGH     | Preflight only fixed in `deploy_ssh.go`; `deploy_local.go` + `deploy_batch.go` have their own preflight wire shapes | §3.4 migration table now lists all 3 preflight sites; `deploy_batch.go:86` shape `{preFlightFailedFor, result}` is the 4th wire shape and dies in Phase 4. |
| HIGH     | Phase 5 sweep falsely classified `deploy_local*` and `deploy_batch` as no-engine handlers | Verified via `rg`: both take `engine *workflow.Engine` (deploy_local.go:63, deploy_batch.go:43). §3.6 handler list now includes them. |
| HIGH     | G4 design called methods that don't exist (`Engine.ComputeEnvelope`, `SessionRegistry.IsAlive`) and used wrong types (`[]platform.Service` vs actual `[]platform.ServiceStack`) | §4.3 rewritten: `ComputeEnvelope` is the package-level function it actually is; new helper takes `[]platform.ServiceStack` + `[]*ServiceMeta`; uses real `ListSessions` + `ClassifySessions` API from `internal/workflow/registry.go:84-104`. |
| MEDIUM   | `IdleOrphan` derivation didn't pin the no-live-services invariant; could fire when orphan coexists with live runtime | §4.3 specifies invariant precisely; §4.6 Phase 3 adds explicit mixed-state test cases. |
| MEDIUM   | `CheckWire` dropped the runnable-check contract from `StepCheck` (`PreAttestCmd`, `ExpectedExit`) | §3.3 adds both fields with `omitempty`; `WithChecks` preserves them when present. |
| MEDIUM   | G5 stderr warning was unpinned (manual smoke-test only)                | §5.3.1 extracts a testable `warnMissingClaudeMD(cwd, w io.Writer)` helper; §5.4 Phase 1 adds unit tests. |

### 2.3 Pass-3 confirmation findings (against the revised plan)

| Severity | What Codex caught                                              | How this plan addresses it                                                          |
|----------|----------------------------------------------------------------|--------------------------------------------------------------------------------------|
| HIGH     | G4 design referenced a `SessionID` named type that doesn't exist (`SessionEntry.SessionID` is plain `string` in `registry.go:29`) — would not compile | §4.3 sketch now uses `map[string]int` and looks up by `m.BootstrapSession` directly. |
| MEDIUM   | `deploy_batch` preflight TBD-by-implementer left contract unpinned | §3.4 now specifies first-failure-abort + hostname-in-message, with explicit test pin `TestDeployBatch_PreflightFailure_NamesTargetInError`. |
| LOW      | G5 helper placement allowed `internal/init/` OR `internal/serve/` but verify command hardcoded `internal/init/` | §5.3.1 picks `internal/init/` canonically (already owns startup helpers); §5.4 verify command stays as-is. |

**Net effect of all three passes**: the plan unifies the four error
shapes (plain text + PlatformError + 2 preflight variants) into one
(G14), does NOT attach envelope or plan to errors (P4 stays intact),
preserves the runnable-check contract, uses real codebase APIs +
existing types in the orphan-meta design, specifies a single concrete
contract for every refactored handler, and pins every behavioral fix
with a test.

---

## 3. G14 + G10 — Unified Error Wire DTO

### 3.1 Problem

Three distinct error wire shapes reach the LLM today (audit §3 G14,
verified live in P3/P5/E3):

1. **Plain text** (`internal/tools/convert.go:46-49`) — non-`PlatformError`
   Go errors flow through as raw strings.
2. **PlatformError JSON** (`convert.go:51-62`) —
   `{code, error, suggestion?, apiCode?, diagnostic?, apiMeta?}`.
3. **Preflight report** (`internal/tools/deploy_ssh.go:148` via
   `jsonResult(pfResult)`) — `{passed, checks[], summary}` from
   `workflow.StepCheckResult`.

The agent has to pattern-match on whichever shape it gets. There is no
`code` in shape (3); no `checks` in shape (2); shape (1) carries
nothing structured at all. Field grammar is invisible to the agent
ahead of time.

Independently, errors carry no lifecycle pointer (G10 / P4 residual).
`P4` in `docs/spec-workflows.md:1194` formally says errors stay terse
and the agent must call `status` to recover lifecycle context — but
neither the CLAUDE.md template nor any error response says this. Live
P5 + E3 sessions showed agents repeating failed deploys instead of
pivoting because they had no signal to call `status`.

### 3.2 Goal

- One canonical error wire shape for every tool error response.
- Plain text and preflight wire shapes eliminated.
- Every error response from a workflow-aware handler carries a
  small typed recovery hint pointing at the canonical recovery surface.
- P4 contract preserved: errors remain terse leaf payloads, but
  *typed* leaf payloads with a uniform schema and an explicit pointer
  to `status`.

### 3.3 Design — wire DTO (lives in `internal/tools/errwire.go`, new)

```go
// Package tools — internal/tools/errwire.go
//
// ErrorWire is the canonical JSON shape for every tool error response.
// Composes a platform-layer PlatformError with optional tool-layer
// extensions (multi-check failures, recovery hint). PlatformError stays
// pure platform-layer; this DTO is the layer-4 (entry point) wire form.
type ErrorWire struct {
    // From PlatformError (always present)
    Code       string             `json:"code"`
    Error      string             `json:"error"`
    Suggestion string             `json:"suggestion,omitempty"`
    APICode    string             `json:"apiCode,omitempty"`
    Diagnostic string             `json:"diagnostic,omitempty"`
    APIMeta    []platform.APIMeta `json:"apiMeta,omitempty"`

    // Multi-check failures (preflight, verify, mount). Always carries
    // its kind so the agent can interpret semantics by tool family.
    Checks []CheckWire `json:"checks,omitempty"`

    // Recovery pointer. Small, static, no I/O to compute.
    // Always points at the canonical lifecycle recovery surface.
    Recovery *RecoveryHint `json:"recovery,omitempty"`
}

// CheckWire is the wire form of a single check failure. Generic enough
// to carry preflight, verify, and mount checks. The kind discriminator
// tells the agent which tool family produced it. Runnable contract
// fields (PreAttestCmd, ExpectedExit) are preserved when present so the
// agent can re-run the check itself — these come from
// workflow.StepCheck and pre-existed the unification.
type CheckWire struct {
    Kind         string `json:"kind"`                   // "preflight" | "verify" | "mount"
    Name         string `json:"name"`
    Status       string `json:"status"`                 // "pass" | "fail" | "skip"
    Detail       string `json:"detail,omitempty"`
    PreAttestCmd string `json:"preAttestCmd,omitempty"` // shell cmd agent can re-run
    ExpectedExit int    `json:"expectedExit,omitempty"` // exit code that means pass
}

// RecoveryHint points the agent at the canonical lifecycle recovery
// surface. P4 contract: status is the single entry point for envelope
// + plan + guidance. The hint never duplicates that data — it only
// names the call.
type RecoveryHint struct {
    Tool   string            `json:"tool"`             // "zerops_workflow"
    Action string            `json:"action"`           // "status"
    Args   map[string]string `json:"args,omitempty"`   // future-proof
}
```

### 3.4 Conversion API — `internal/tools/convert.go` (extended)

The existing `convertError` becomes the orchestration entry point. Two
options-pattern variants cover the new fields:

```go
// ConvertError emits the canonical ErrorWire JSON for any error.
// Generic errors are wrapped as platform.PlatformError(ErrUnknown, ...)
// at this boundary — plain-text wire shape disappears.
func ConvertError(err error, opts ...ErrorOption) *mcp.CallToolResult

// ErrorOption configures the wire DTO. Composable.
type ErrorOption func(*ErrorWire)

// WithChecks attaches multi-check failures with a kind discriminator.
// Workflow-layer StepCheck values are converted to the wire form here
// (preserving the layer boundary — workflow types never serialize
// directly).
func WithChecks(kind string, checks []workflow.StepCheck) ErrorOption

// WithRecoveryStatus attaches the canonical recovery hint pointing
// at zerops_workflow action="status". Use in every workflow-aware
// handler (the 16 with engine in scope at the error point).
func WithRecoveryStatus() ErrorOption

// WithRecovery attaches a custom recovery hint. Reserved for future
// non-status recoveries (e.g. recipe action=status when v3 store
// becomes visible to the registry).
func WithRecovery(hint *RecoveryHint) ErrorOption
```

**Migration of the four current wire shapes** (Codex pass-2 caught two
preflight variants beyond `deploy_ssh`):

| Old shape       | Where produced                                | New emission                                                     |
|-----------------|-----------------------------------------------|------------------------------------------------------------------|
| Plain text      | `convertError` line 46-49 (generic err path)  | Wrap as `platform.NewPlatformError(ErrUnknown, err.Error(), "")` at the boundary; emit ErrorWire. |
| PlatformError JSON | `convertError` line 51-62                  | Same shape, now the canonical ErrorWire with optional fields zeroed. |
| Preflight (ssh) | `deploy_ssh.go:148` `jsonResult(pfResult)`    | `ConvertError(NewPlatformError(ErrPreflightFailed, summary, ""), WithChecks("preflight", pfResult.Checks))`. |
| Preflight (local) | `deploy_local.go:114` `jsonResult(pfResult)` | Same conversion as ssh — same helper, same shape.                |
| Preflight (batch) | `deploy_batch.go:86` returns ad-hoc map `{"preFlightFailedFor": <host>, "result": pfResult}` after first-failure abort | `ConvertError(NewPlatformError(ErrPreflightFailed, fmt.Sprintf("Preflight failed for %s: %s", host, summary), ""), WithChecks("preflight", pfResult.Checks))`. **Preserves current first-failure-abort semantics** (`deploy_batch.go:84-89` returns immediately on first failed target, never aggregates). Hostname rides in the message; check `name`/`detail` carry the per-check context as today. Test pin: `TestDeployBatch_PreflightFailure_NamesTargetInError` asserts the failed hostname appears in `error` field — guarantees per-target attribution survives even though only the first failure is surfaced. |

A new error code joins `internal/platform/errors.go`:

```go
ErrPreflightFailed = "PREFLIGHT_FAILED"
```

`ErrUnknown` already exists (verified — used as a fallback elsewhere).

### 3.5 Test impact

| Test                                                          | Update                                                                                                    |
|---------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------|
| `internal/tools/convert_test.go` (8 functions)                | Update assertions to ErrorWire field set; assert plain-text wrap path emits typed code.                  |
| `internal/tools/convert_contract_test.go`                     | Add `checks` + `recovery` field round-trip cases.                                                         |
| `internal/tools/deploy_preflight_test.go` (3 functions)       | Tests still validate `StepCheckResult` (workflow-layer); add new tests for `ErrorWire` JSON at handler.  |
| `internal/tools/deploy_ssh_test.go`                           | Update `TestDeploySSH_PreflightFailure_*` to expect ErrorWire shape with `code:PREFLIGHT_FAILED` + `checks[]`. |
| `integration/multi_tool_test.go::TestIntegration_ErrorPropagation` | Update field assertions; add `recovery` presence check for workflow-aware handlers. |
| `integration/multi_tool_test.go::TestIntegration_WorkflowNoParams_Error` | Plain-text expectation flips to ErrorWire shape.                                |

New tests required:

- `TestConvertError_GenericErrWrappedAsUnknown` — plain Go error → ErrorWire with `code: UNKNOWN`.
- `TestConvertError_WithChecks_PreflightShape` — `WithChecks("preflight", ...)` round-trip.
- `TestConvertError_WithRecoveryStatus_AttachesHint` — recovery hint round-trip.
- `TestConvertError_PreservesPlatformError` — typed input survives untouched in code/error/suggestion.
- `TestConvertError_NoEnvelopeNoPlan_LeafContract` — assert ErrorWire JSON has no `envelope` and no `plan` keys (P4 contract pin).

### 3.6 Handler-by-handler recovery hint plumbing (G10 part)

Workflow-aware handlers (engine and/or stateDir at the error point) get
`WithRecoveryStatus()` added at every `ConvertError(...)` call site.
The list below is verified against the codebase via `rg 'engine \*workflow\.Engine'`
and `rg 'stateDir string'` over `internal/tools/`:

- `internal/tools/workflow.go` — every `convertError` site (60+); the
  helper already knows it's in workflow context.
- `internal/tools/workflow_recipe.go` — 30+ sites.
- `internal/tools/workflow_bootstrap.go` — 10+ sites.
- `internal/tools/workflow_develop.go` — 8+ sites.
- `internal/tools/workflow_strategy.go` — 10 sites.
- `internal/tools/workflow_adopt_local.go` — 10 sites.
- `internal/tools/workflow_classify.go` — 1 site.
- `internal/tools/workflow_record_deploy.go` — 2 sites.
- `internal/tools/deploy_ssh.go` — 2 sites (one is the preflight conversion from §3.4).
- `internal/tools/deploy_local.go` — 4 sites + 1 preflight conversion. `RegisterDeployLocal` takes `engine *workflow.Engine` (`deploy_local.go:63`).
- `internal/tools/deploy_local_git.go` — 8 sites. Called from `deploy_local.go`; the wrapping handler holds engine + stateDir.
- `internal/tools/deploy_batch.go` — 1 site + 1 preflight conversion. `RegisterDeployBatch` takes `engine *workflow.Engine` (`deploy_batch.go:43`).
- `internal/tools/import.go` — 2 sites.
- `internal/tools/guidance.go` — 1 site.
- `internal/tools/knowledge.go` — 7 sites.
- `internal/tools/mount.go` — 7 sites.
- `internal/tools/verify.go` — 2 sites.
- `internal/tools/delete.go` — 2 sites.
- `internal/tools/guard.go` — 2 sites. `requireWorkflowContext` is a workflow-aware helper used by other handlers; either inherit the caller's recovery hint policy or attach unconditionally (decide at Phase 5).

Handlers without engine in scope keep bare `ConvertError(err)` — no
recovery hint. These are: `env`, `discover`, `logs`, `manage`, `scale`,
`browser`, `subdomain`, `dev_server`, `export`, `events`, `preprocess`,
`deploy_git_push` (called from contexts that hold engine but the helper
itself doesn't), and any future single-shot read-only tools. Their
CLAUDE.md guidance is the standard fallback documented in §3.7 (any
error without a `recovery` field → call `zerops_workflow action=status`
manually).

### 3.7 CLAUDE.md template addition

In `internal/content/templates/claude_shared.md`, append after the
"three entry points" block (around line 50):

```markdown
## Tool errors

Every tool error returns a uniform JSON shape:

\`\`\`
{ "code": "<TYPED_CODE>", "error": "<message>",
  "suggestion": "<optional recovery hint>",
  "apiCode": "<optional Zerops error code>",
  "diagnostic": "<optional context>",
  "apiMeta": [ ... optional structured metadata ... ],
  "checks": [ { "kind": "<preflight|verify|mount>",
                "name": "<check>", "status": "<fail>",
                "detail": "<...>" } ],
  "recovery": { "tool": "zerops_workflow", "action": "status" } }
\`\`\`

When a `recovery` field is present, the call it names is the
canonical lifecycle recovery surface — call it before retrying or
asking the user, not after several blind retries. When `recovery`
is absent, fall back to `zerops_workflow action="status"` yourself.
`status` returns the live envelope, plan, and guidance for the
current phase.
```

This addition is governed by `description_drift_test.go` like every
other template change.

### 3.8 Phased execution for G14 + G10

| Phase | Files                                                               | Verify                                                  |
|-------|---------------------------------------------------------------------|---------------------------------------------------------|
| 1     | `internal/platform/errors.go` — add `ErrPreflightFailed` constant.  | `go test ./internal/platform/... -count=1`              |
| 2     | New `internal/tools/errwire.go` (ErrorWire, CheckWire, RecoveryHint, ErrorOption, WithChecks, WithRecoveryStatus, WithRecovery). | `go test ./internal/tools/ -run TestErrorWire -count=1` (write tests for the new file alongside). |
| 3     | Refactor `internal/tools/convert.go::ConvertError` to emit ErrorWire shape; wrap generic errors as `ErrUnknown`; drop the plain-text branch. Update `internal/tools/convert_test.go` + `convert_contract_test.go` together. | `go test ./internal/tools/ -count=1` |
| 4     | Convert ALL THREE preflight call sites: `deploy_ssh.go:148`, `deploy_local.go:114`, `deploy_batch.go:86` no longer return their respective wire shapes; all three convert to `ConvertError(NewPlatformError(ErrPreflightFailed, summary, ""), WithChecks("preflight", pfResult.Checks))`. Update `deploy_preflight_test.go`, `deploy_ssh_test.go`, `deploy_local_test.go`, `deploy_batch_test.go` together. Add a contract test forbidding `jsonResult(pfResult)` and `"preFlightFailedFor"` literals from any tool handler going forward. | `go test ./internal/tools/ ./integration/ -count=1` + grep gate. |
| 5     | Sweep the workflow-aware handlers from §3.6, adding `WithRecoveryStatus()` to every `ConvertError(...)` call site. Order by file (workflow.go first since it's the largest blast radius, then descending by site count): workflow.go → workflow_recipe.go → workflow_bootstrap.go → workflow_develop.go → workflow_strategy.go → workflow_adopt_local.go → workflow_classify.go → workflow_record_deploy.go → deploy_ssh.go → deploy_local.go → deploy_local_git.go → deploy_batch.go → import.go → guidance.go → knowledge.go → mount.go → verify.go → delete.go → guard.go. **One file per commit** — easy to revert if a handler-specific test breaks. | After each file: `go test ./internal/tools/ -count=1` |
| 6     | CLAUDE.md template addition to `claude_shared.md` (see §3.7); `internal/tools/description_drift_test.go` MAY pick up the addition — verify it stays green. | `make lint-local` |
| 7     | Add a test pinning the recovery contract: `internal/tools/errwire_contract_test.go::TestErrorWire_NeverCarriesEnvelopeOrPlan` — scan a marshaled ErrorWire for forbidden keys `envelope`, `plan`, `nextAtoms`. P4-pinning. | `make lint-local` |

**Estimated diff size**: ~30 files touched across 7 commits.
**Estimated effort**: 1 long focused day (the sweep in Phase 5 is the
slowest part because each handler has tests that may assert specific
error JSON shapes).

---

## 4. G4 — Orphan Meta Visibility

### 4.1 Problem

`ServiceMeta` lives on disk independently of live `ServiceSnapshot`s.
`ComputeEnvelope` builds snapshots ONLY from the live service list
(`internal/workflow/compute_envelope.go:161-224`); disk metas are
joined into snapshots when there's a live match. **If the service is
deleted externally** (Zerops dashboard, `zcli`, manual API call), the
on-disk meta becomes invisible:

- Incomplete meta with `BootstrapSession != ""` was being routed to
  the resume option only when the live snapshot existed and reported
  `Resumable=true`. With no live snapshot, no resumable signal fires.
- Complete meta with no live match was pruned by
  `PruneServiceMetas(...)` at develop briefing
  (`workflow_develop.go:55`), but ONLY at develop briefing. At idle or
  bootstrap phase the orphan is invisible.

The `IdleIncomplete` constant exists (`internal/workflow/envelope.go:37-46`)
but no atom or fixture surfaces it; no `OrphanMeta` envelope field
exists; no plan branch handles cleanup.

Audit § G4 + spec scenario §6.5 (`docs/spec-scenarios.md`) name the
problem; this plan implements the by-design fix.

### 4.2 Goal

Disk metas without a live match are surfaced as first-class envelope
data at every phase. Idle phase routes orphan-only states to a
cleanup-or-recreate plan. Develop phase keeps existing
auto-prune behavior unchanged (see §4.5). Resumable orphans (incomplete
meta + dead session) are distinguished from cleanup orphans (complete
meta or incomplete with live session but missing live service).

Acceptance:

- `StateEnvelope` carries `OrphanMetas []OrphanMeta`.
- `IdleScenario` set extends with `IdleOrphan` for the
  "orphan metas only, no live services" case.
- New atom `idle-orphan-cleanup.md` fires for `IdleOrphan`.
- `corpus_coverage_test.go` carries `idle_orphan_only` fixture pinning
  the new atom firing.
- Existing develop-briefing prune behavior preserved (no auto-prune at
  idle — visibility is the contract; explicit `reset` clears).
- `BuildPlan` surfaces `Primary: zerops_workflow action=reset workflow=bootstrap` (or appropriate cleanup primitive) when orphans dominate.

### 4.3 Design — `OrphanMeta` envelope field + scenario routing

**Codebase shape** (verified against current code):

- `ComputeEnvelope` is a **package-level function**, not a method
  (`internal/workflow/compute_envelope.go:32-39`). Signature takes
  `services []platform.ServiceStack` and `metas []*ServiceMeta`.
- Session liveness is exposed via `ListSessions(stateDir) ([]SessionEntry, error)`
  and `ClassifySessions(sessions) (alive, dead []SessionEntry)`
  in `internal/workflow/registry.go:84-104`. There is no
  `SessionRegistry.IsAlive` interface — the function pair is the public API.
- `SessionEntry` carries `SessionID` (string) and `PID` (int) — both
  available in `registry.go:28-...`.

```go
// internal/workflow/envelope.go (new types added)

type OrphanReason string

const (
    // OrphanReasonLiveDeleted: meta on disk for a hostname/stagehostname
    // pair where neither half exists in the live API. Most common
    // cause: user deleted via Zerops dashboard or zcli.
    OrphanReasonLiveDeleted    OrphanReason = "live-deleted"
    // OrphanReasonIncompleteLost: incomplete meta (BootstrappedAt empty)
    // with a non-empty BootstrapSession that no longer corresponds to
    // a live PID in the registry. Bootstrap died before completion AND
    // before reset.
    OrphanReasonIncompleteLost OrphanReason = "incomplete-lost"
)

type OrphanMeta struct {
    Hostname         string       `json:"hostname"`
    StageHostname    string       `json:"stageHostname,omitempty"`
    BootstrapSession string       `json:"bootstrapSession,omitempty"`
    BootstrappedAt   string       `json:"bootstrappedAt,omitempty"` // empty = incomplete
    FirstDeployedAt  string       `json:"firstDeployedAt,omitempty"`
    Reason           OrphanReason `json:"reason"`
}

// Add to StateEnvelope:
type StateEnvelope struct {
    // ... existing fields
    OrphanMetas []OrphanMeta `json:"orphanMetas,omitempty"`
}

// Add scenario:
const (
    // ... existing scenarios
    IdleOrphan IdleScenario = "orphan"
)
```

In `internal/workflow/compute_envelope.go`:

```go
// computeOrphanMetas diffs disk metas against live services to find
// metas whose corresponding live service no longer exists. Pure: takes
// already-loaded inputs (services, metas) and a session-liveness
// snapshot (alivePIDs) — no I/O, no engine receiver, matches the
// existing ComputeEnvelope shape.
//
// alivePIDs is the set of PIDs still alive per ClassifySessions. A nil
// alivePIDs is permitted (treated as "session liveness unknown" — every
// incomplete meta with a BootstrapSession is then classified as
// LiveDeleted, never IncompleteLost). sessionByID maps the session-ID
// string (SessionEntry.SessionID is plain string in
// internal/workflow/registry.go:29) to PID for liveness lookup.
func computeOrphanMetas(
    services []platform.ServiceStack,
    metas []*ServiceMeta,
    alivePIDs map[int]struct{},
    sessionByID map[string]int,
) []OrphanMeta {
    liveByName := make(map[string]struct{}, len(services))
    for _, s := range services {
        liveByName[s.Name] = struct{}{}
    }
    var out []OrphanMeta
    for _, m := range metas {
        if m == nil { continue }
        _, devLive := liveByName[m.Hostname]
        stageLive := false
        if m.StageHostname != "" {
            _, stageLive = liveByName[m.StageHostname]
        }
        if devLive || stageLive {
            continue // pair-keyed: either half live → not orphan
        }
        reason := OrphanReasonLiveDeleted
        if !m.IsComplete() && m.BootstrapSession != "" {
            if alivePIDs != nil {
                pid, ok := sessionByID[m.BootstrapSession]
                if !ok {
                    reason = OrphanReasonIncompleteLost // session record gone
                } else if _, alive := alivePIDs[pid]; !alive {
                    reason = OrphanReasonIncompleteLost // session PID dead
                }
            }
        }
        out = append(out, OrphanMeta{
            Hostname:         m.Hostname,
            StageHostname:    m.StageHostname,
            BootstrapSession: m.BootstrapSession,
            BootstrappedAt:   m.BootstrappedAt,
            FirstDeployedAt:  m.FirstDeployedAt,
            Reason:           reason,
        })
    }
    return out
}

// Caller side, in ComputeEnvelope (added near the existing parallel
// reads of services + metas + work session):
sessions, _ := ListSessions(stateDir) // best-effort; nil on error → unknown
alivePIDs := make(map[int]struct{})
sessionByID := make(map[string]int)
if sessions != nil {
    alive, _ := ClassifySessions(sessions)
    for _, s := range alive {
        alivePIDs[s.PID] = struct{}{}
    }
    for _, s := range sessions {
        sessionByID[s.SessionID] = s.PID
    }
}
env.OrphanMetas = computeOrphanMetas(services, metas, alivePIDs, sessionByID)
```

**`IdleOrphan` invariant** (made precise to address Codex pass-2 finding):

> `IdleOrphan` fires only when (a) phase is idle, (b) at least one
> orphan meta exists, AND (c) **no non-self runtime ServiceSnapshot is
> live**. "Non-self" excludes the ZCP control-plane container itself
> (the existing `isManagedSelf` filter); managed dependencies (e.g.
> `postgresqldev`) DO count as live and suppress IdleOrphan routing
> because the user clearly has live infrastructure to work with even
> if some runtime metas are stale.

```go
// Update deriveIdleScenario in compute_envelope.go:
func deriveIdleScenario(env *StateEnvelope) IdleScenario {
    if env.Phase != PhaseIdle {
        return ""
    }
    bootstrapped, adoptable, resumable, liveOther := 0, 0, 0, 0
    for _, svc := range env.Services {
        if isManagedSelf(svc) { continue }
        liveOther++ // any non-self live service counts here
        if svc.Resumable { resumable++ }
        if svc.Bootstrapped { bootstrapped++ } else if !svc.Resumable { adoptable++ }
    }
    // Orphan-only takes precedence over empty: orphan metas exist AND
    // no non-self live services exist (managed deps, runtimes, anything).
    if len(env.OrphanMetas) > 0 && liveOther == 0 {
        return IdleOrphan
    }
    if resumable > 0 { return IdleIncomplete }
    if bootstrapped > 0 { return IdleBootstrapped }
    if adoptable > 0 { return IdleAdopt }
    return IdleEmpty
}
```

This guarantees that a project with an orphan meta plus a live
managed service routes to whichever live-bearing scenario applies, NOT
to IdleOrphan. The orphan still appears in `env.OrphanMetas` for
visibility — it just doesn't drive the primary plan.

### 4.4 Atom + corpus_coverage fixture

New atom `internal/content/atoms/idle-orphan-cleanup.md`:

```markdown
---
name: idle-orphan-cleanup
phases: [idle]
idleScenarios: [orphan]
priority: 30
references-fields:
  - workflow.StateEnvelope.OrphanMetas
  - workflow.OrphanMeta.Hostname
  - workflow.OrphanMeta.Reason
---

The project state has metas on disk for services that no longer exist
on Zerops. Either the services were deleted externally (dashboard,
zcli) or a bootstrap session died before completing.

Resolve before starting work:

- **Clean up only**: `zerops_workflow action="reset"` clears stale
  metas. Bootstrap-session state and per-service files are removed.
  Future bootstrap or develop calls operate on a clean slate.

- **Recreate the service**: start a fresh bootstrap with the same
  intent. The new live service replaces the orphan meta naturally.

Listing the orphans for context:

\`\`\`
{{range .Envelope.OrphanMetas}}
- {{.Hostname}}{{if .StageHostname}} (paired with {{.StageHostname}}){{end}} — reason: {{.Reason}}
{{end}}
\`\`\`

Don't `zerops_workflow action="start" workflow="develop"` against an
orphan hostname — develop will fail at scope validation.
```

(Atom body is illustrative; final wording follows the existing atom-
authoring contract per `internal/content/atoms_lint.go`.)

New fixture in `internal/workflow/corpus_coverage_test.go`:

```go
{
    name:        "idle_orphan_only",
    envelope: workflow.StateEnvelope{
        Phase:       workflow.PhaseIdle,
        Environment: workflow.EnvContainer,
        Services:    nil,
        OrphanMetas: []workflow.OrphanMeta{
            {Hostname: "appdev", StageHostname: "appstage",
             BootstrapSession: "sess-deadbeef",
             Reason: workflow.OrphanReasonIncompleteLost},
        },
    },
    mustContain: []string{
        "metas on disk for services that no longer exist",
        `zerops_workflow action="reset"`,
        "appdev",
    },
},
```

### 4.5 Develop-briefing prune behavior — preserved

`PruneServiceMetas(stateDir, liveHostnames)` continues to run at the
top of `handleDevelopBriefing` (`workflow_develop.go:55`). Rationale:

- The user's intent at develop entry is "I want to work on these
  services". Surfacing orphans here would be visual noise — the
  develop scope already filters to declared services.
- The existing prune logic is conservative (only deletes when both
  `Hostname` and `StageHostname` have no live match) — already
  tested + working.

Idle-phase visibility is the new contract; develop-phase auto-prune
is the existing contract. Both stand.

### 4.6 Phased execution for G4

| Phase | Files                                                                                                              | Verify                                                |
|-------|--------------------------------------------------------------------------------------------------------------------|-------------------------------------------------------|
| 1     | Add `OrphanMeta`, `OrphanReason`, `IdleOrphan` types/constants to `internal/workflow/envelope.go`.                 | `go vet ./...`                                        |
| 2     | Add `computeOrphanMetas` helper in `internal/workflow/compute_envelope.go` per the §4.3 design (uses real `[]platform.ServiceStack` + `[]*ServiceMeta` types, takes `alivePIDs` snapshot via `ListSessions`+`ClassifySessions`); wire into `ComputeEnvelope` next to existing parallel reads. Add unit test `TestComputeOrphanMetas_*` covering: live-deleted, incomplete-lost, dead-session-record, missing-session-record, both-pair-halves-live (not orphan), one-pair-half-live (not orphan), mixed orphan + live cases. | `go test ./internal/workflow/ -run TestComputeOrphanMetas -count=1` |
| 3     | Update `deriveIdleScenario` to handle `IdleOrphan` per the precise invariant in §4.3 ("orphan metas + zero non-self live services"). Add cases: `TestDeriveIdleScenario_OrphanOnly`, `TestDeriveIdleScenario_OrphanPlusLiveRuntime_NotIdleOrphan`, `TestDeriveIdleScenario_OrphanPlusLiveManaged_NotIdleOrphan`, `TestDeriveIdleScenario_OrphanPlusBootstrappedRuntime_RoutesToBootstrapped`. Update existing `TestDeriveIdleScenario_*` cases as needed. | `go test ./internal/workflow/ -run TestDeriveIdleScenario -count=1` |
| 4     | Update `BuildPlan` (`internal/workflow/plan.go` or wherever the idle-phase plan branches live) to surface reset-or-recreate Primary action when scenario is `IdleOrphan`. Add `TestBuildPlan_IdleOrphan_*`. | `go test ./internal/workflow/ -run TestBuildPlan -count=1` |
| 5     | Add atom `internal/content/atoms/idle-orphan-cleanup.md`. Run `make lint-local` to check atom-authoring contract (no spec-IDs leak, references-fields AST integrity, etc.). | `make lint-local` |
| 6     | Add `idle_orphan_only` fixture in `internal/workflow/corpus_coverage_test.go`. Run all corpus-coverage tests including `KnownOverflows_StillOverflow` companion (this fixture should NOT add to overflow allowlist — it's a small idle-phase render). | `go test ./internal/workflow/ -count=1` |
| 7     | End-to-end integration test: `integration/orphan_meta_test.go` simulates the scenario (write meta to disk; mock client returns no live match; `zerops_workflow action=status` returns IdleOrphan envelope with the reset hint). | `go test ./integration/ -count=1` |

**Estimated diff size**: ~10 files across 7 commits.
**Estimated effort**: half-day to one day.

---

## 5. G5 — Headless Doctrine Warning + Operator Doc

### 5.1 Problem

In container env (`zcp serve` running on the Zerops `zcp` container),
when no `zcp init` has been run in the working directory:

- `<cwd>/CLAUDE.md` doesn't exist → CLAUDE.md auto-discovery delivers
  nothing.
- MCP `Instructions` field is built from `BuildInstructions(rc)` which
  only carries `AdoptionNote` (local-only) and `StateHint` (per-PID
  active sessions). With no sessions active, the field is empty.
- `TestBuildInstructions_NoStaticRulesLeak` (`internal/server/instructions_test.go:53-72`)
  forbids any doctrine prose from being injected into Instructions —
  pinned forbidden patterns include `"Three entry points"`,
  `"workflow=\"develop\""`, `"workflow=\"bootstrap\""`, `"/var/www/"`,
  `"SSHFS"`, `"Don't guess"`. This test is by-design — doctrine lives
  in CLAUDE.md, the strong-adherence surface.

A headless `claude --print` invocation in a non-init'd directory thus
gets ZERO workflow doctrine. Live P0 confirmed: agent enumerated
services correctly via `zerops_discover` but never offered any next
step, never named entry points, never proposed status as recovery.

### 5.2 Goal

The fix is **not** weakening the test or injecting doctrine into MCP
init. The fix is operator hygiene:

1. A stderr warning at every `zcp serve` start when CLAUDE.md is
   missing in cwd. Tells the operator (or the agent's launcher
   script) to run `zcp init` first.
2. An operator doc explicitly marking `zcp init` as mandatory before
   headless use.

### 5.3 Design

#### 5.3.1 Stderr warning — testable helper

The warning is extracted into a small helper at
`internal/init/headless_warn.go` (canonical placement — `internal/init/`
already owns startup-adjacent helpers like `generateCLAUDEMD`; no
reason to spawn a new `internal/serve/` package). Pure function: takes
a CWD and a writer, returns a bool, only emits to the writer when
CLAUDE.md is missing. No global state, no direct `os.Stderr` reference
inside the helper.

```go
// internal/init/headless_warn.go
package init

import (
    "fmt"
    "io"
    "os"
    "path/filepath"
)

// WarnMissingClaudeMD writes a warning to w when no CLAUDE.md exists
// in cwd. Returns true when the warning was emitted (test affordance).
// Stderr is the channel for the production call site.
func WarnMissingClaudeMD(cwd string, w io.Writer) bool {
    _, err := os.Stat(filepath.Join(cwd, "CLAUDE.md"))
    if !os.IsNotExist(err) {
        return false
    }
    fmt.Fprintln(w,
        "WARNING: no CLAUDE.md in working directory; "+
        "MCP-only mode delivers no workflow doctrine. "+
        "Run `zcp init` here first for full agent guidance.")
    return true
}
```

Call site in `cmd/zcp/main.go::run()`, after `runtime.Detect()` and
before `server.New(...)`:

```go
if cwd, err := os.Getwd(); err == nil {
    headlesswarn.WarnMissingClaudeMD(cwd, os.Stderr)
}
```

This warning fires in BOTH container and local env — both modes need
CLAUDE.md for full doctrine. Stderr is the right channel
(`TestNoStdoutOutsideJSONPath` only governs `internal/...`; `cmd/`
is exempt).

Unit tests in `internal/init/headless_warn_test.go`:

- `TestWarnMissingClaudeMD_FileMissing_EmitsWarning` — pass a temp dir
  with no CLAUDE.md; assert returned `true` and writer captured the
  WARNING text.
- `TestWarnMissingClaudeMD_FilePresent_NoOp` — pass a temp dir with a
  zero-byte CLAUDE.md; assert returned `false` and writer captured nothing.
- `TestWarnMissingClaudeMD_StatErrors_NoEmit` — pass a non-existent
  cwd; assert no panic, no emission (treats permission/IO errors as
  "don't warn" rather than emit a misleading warning).

#### 5.3.2 Operator doc

New file `docs/spec-headless.md` (or extension to existing
`docs/spec-local-dev.md` if it ends up too thin to deserve its own
spec). Content:

```markdown
# ZCP — Headless Operation

Running `zcp serve` for an automated agent (Claude Code `--print`,
unattended scripts, CI flows) requires CLAUDE.md to be present in
the working directory the agent will operate from. Without it, the
agent has only tool descriptions — workflow doctrine, the canonical
status recovery primitive, and SSHFS mount semantics are all
delivered through CLAUDE.md, not through MCP init.

## Required setup

\`\`\`
cd /path/to/working/dir
zcp init
\`\`\`

`zcp init` is idempotent — re-running re-stamps the managed section
of CLAUDE.md without overwriting user additions outside the
`<!-- ZCP:BEGIN -->` / `<!-- ZCP:END -->` markers.

Container env additionally writes SSH config, git identity, and
a global Claude Code MCP entry. Local env writes a project-scoped
`.mcp.json` carrying the per-project `ZCP_API_KEY`.

## Verifying

`zcp serve` prints a stderr warning at startup if CLAUDE.md is
missing in cwd. If the warning fires, run `zcp init` and restart
the serve process.

## Why not auto-inject doctrine via MCP init

`internal/server/instructions_test.go::TestBuildInstructions_NoStaticRulesLeak`
forbids static doctrine in the MCP `Instructions` field. The reason
is duplication: the same prose lived in two places (template + MCP
init) and drifted. The single-source-of-truth is CLAUDE.md;
`zcp init` is the deployment mechanism.
```

### 5.4 Phased execution for G5

| Phase | Files                                                                  | Verify                                                |
|-------|------------------------------------------------------------------------|-------------------------------------------------------|
| 1     | Extract `WarnMissingClaudeMD(cwd, w)` helper into `internal/init/headless_warn.go` (or wherever the implementer chooses to put serve-startup utilities). Add unit tests per §5.3.1. Wire single call site in `cmd/zcp/main.go::run()`. | `go test ./internal/init/ -run TestWarnMissingClaudeMD -count=1`; manual smoke-test `cd /tmp && /path/to/zcp 2>&1 | grep WARNING`. |
| 2     | New `docs/spec-headless.md`. Cross-link from README + `docs/spec-local-dev.md` if applicable. | `make lint-local` |

**Estimated diff size**: 2-3 files.
**Estimated effort**: 30 minutes.

---

## 6. G8 — Per-Service CLAUDE.md Reframe

### 6.1 Problem

`internal/content/templates/claude_container.md:9-10` says:

```
Per-service rules (reload behaviour, start commands, asset pipeline)
live at `/var/www/{hostname}/CLAUDE.md` — read before editing.
```

Wording is **prescriptive** ("live at", "read before editing") — agent
treats the file as load-bearing. But ZCP doesn't generate or govern
this file. It's an optional artifact that recipes (per user
clarification) MAY include in the published deliverable. If absent,
the agent reads → not found → has to reason about it.

### 6.2 Goal

Reframe the wording from prescriptive to descriptive: the file MAY
exist if a recipe was imported that included one. Agent should look
for it but treat absence as normal.

### 6.3 Design — single-line template edit

In `internal/content/templates/claude_container.md`, replace lines
9-10 with:

```
Per-service rules (reload behaviour, start commands, asset pipeline)
MAY exist at `/var/www/{hostname}/CLAUDE.md` — recipes typically
include them. Read it if present before editing; absence is normal.
```

That is the entire fix. No code change. The atom corpus doesn't
reference this template, so no atom edits propagate.

### 6.4 Phased execution for G8

| Phase | Files                                                       | Verify           |
|-------|-------------------------------------------------------------|------------------|
| 1     | Edit `internal/content/templates/claude_container.md` lines 9-10. | `make lint-local`; `go test ./internal/tools/ -run TestDescriptionDrift -count=1` |

**Estimated diff size**: 1 file, ~3 lines changed.
**Estimated effort**: 5 minutes.

---

## 7. Sequenced execution across all findings

The five findings have low coupling. Recommended order, optimized
for shipping value early and saving the wide refactor for last:

1. **G8** — single-line template edit. 5 min. Ship first; zero blast radius.
2. **G5** — stderr warning + operator doc. 30 min. Independent of all other work.
3. **G4** — orphan meta visibility. ~1 day. Compute-envelope-scoped.
4. **G14 + G10** — error wire DTO + recovery hint. ~1 long day. Wide refactor; do last so the other test surfaces aren't churning while you sweep error tests.

Each phase is its own commit (or small commit set) per CLAUDE.md
"Phased refactors — verify each phase before continuing; no
half-finished states".

---

## 8. Test guardrails (what catches regressions)

| Test                                                                          | What it pins                                                                          |
|-------------------------------------------------------------------------------|---------------------------------------------------------------------------------------|
| `internal/tools/errwire_contract_test.go::TestErrorWire_NeverCarriesEnvelopeOrPlan` | P4 contract — errors carry no envelope or plan, only optional recovery hint.   |
| `internal/tools/errwire_contract_test.go::TestErrorWire_AlwaysHasCodeAndError` | Schema invariant — every emitted ErrorWire has typed code + human message.            |
| `internal/tools/errwire_contract_test.go::TestNoLegacyPreflightShapes` | Grep gate forbidding `jsonResult(pfResult)` and `"preFlightFailedFor"` literals from `internal/tools/*.go`. |
| `internal/tools/errwire_test.go::TestCheckWire_PreservesRunnableContract` | `WithChecks(...)` round-trips `PreAttestCmd` and `ExpectedExit` from `workflow.StepCheck`. |
| `internal/tools/convert_test.go::TestConvertError_GenericErrWrappedAsUnknown`  | Plain-text wire shape stays gone — generic errors always wrapped to typed code.       |
| `internal/tools/deploy_preflight_test.go::TestDeployPreflight_EmitsErrorWireShape` | Preflight wire shape stays gone — failures emit ErrorWire with `checks: [...]`.   |
| `internal/tools/deploy_local_test.go::TestDeployLocal_PreflightFailure_EmitsErrorWire` | Same for local-mode preflight (Codex pass-2 finding #1).                          |
| `internal/tools/deploy_batch_test.go::TestDeployBatch_PreflightFailure_EmitsErrorWire` | Same for batch-mode preflight; covers the formerly-ad-hoc `preFlightFailedFor` shape. |
| `internal/workflow/compute_envelope_test.go::TestComputeOrphanMetas_*`         | Disk-meta-vs-live diff produces correct `OrphanMeta` set and reason. Covers: live-deleted, incomplete-lost, dead-session-record, missing-session-record, both-pair-halves-live, one-pair-half-live, mixed orphan + live. |
| `internal/workflow/compute_envelope_test.go::TestDeriveIdleScenario_*`         | Mixed-state coverage: `OrphanOnly`, `OrphanPlusLiveRuntime_NotIdleOrphan`, `OrphanPlusLiveManaged_NotIdleOrphan`, `OrphanPlusBootstrappedRuntime_RoutesToBootstrapped`. |
| `internal/workflow/corpus_coverage_test.go::idle_orphan_only` fixture          | Atom delivery + plan render correct for IdleOrphan envelope.                          |
| `internal/init/headless_warn_test.go::TestWarnMissingClaudeMD_*`               | G5 stderr warning fires when CLAUDE.md missing, no-op when present, no panic on stat error. |
| `internal/server/instructions_test.go::TestBuildInstructions_NoStaticRulesLeak` | (Existing) — confirms G5 fix doesn't accidentally inject doctrine into MCP init.     |
| `internal/tools/description_drift_test.go`                                     | (Existing) — picks up any new forbidden-pattern matches in template / tool descriptions. |
| `internal/content/atoms/atom-lint`                                             | (Existing) — new orphan atom respects atom authoring contract.                        |

---

## 9. Anti-patterns (do not do these)

- **Do not attach `envelope` or `plan` to errors.** P4 is the
  contract; the recovery hint exists precisely so we don't have to
  duplicate envelope state across two surfaces. If you find yourself
  reaching for "let me just include the envelope on this one error,
  it's cheap" — stop. The recovery hint is the answer.
- **Do not extend `platform.PlatformError` with workflow types.**
  Layer 1 stays layer 1. The wire DTO `ErrorWire` lives in
  `internal/tools` (layer 4 entry point) where workflow imports are
  legal. Adding workflow imports to `internal/platform/` will fail
  `architecture_test.go` and `.golangci.yaml::depguard` and is the
  wrong abstraction either way.
- **Do not call `ComputeEnvelope` on the error path** for the sake
  of attaching it. Beyond P4, this risks masking the original failure
  with a secondary I/O error. The recovery hint requires no I/O.
- **Do not auto-prune orphan metas at idle.** Visibility is the
  contract; explicit `reset` or recreate is the cleanup. Auto-pruning
  loses the user's signal that a service was deleted externally.
- **Do not weaken `TestBuildInstructions_NoStaticRulesLeak`** to fix
  G5. The test exists precisely to prevent doctrine drift between MCP
  init and CLAUDE.md. The fix is `zcp init` hygiene + operator doc.
- **Do not add per-service CLAUDE.md generation** for G8. The user
  clarified these come from recipes when imported; we govern none of
  it. Reframing the template wording is the entire fix.
- **Do not drop `PreAttestCmd` or `ExpectedExit` from CheckWire.** The
  fields are the runnable contract: they tell the agent how to re-run
  the check itself. Stripping them turns "this check failed and here's
  how to verify the fix" into "this check failed; figure it out", which
  is a regression from the legacy preflight wire shape. Codex pass-2
  caught this; the test `TestCheckWire_PreservesRunnableContract`
  pins the contract.
- **Do not auto-detect "engine availability" by handler signature
  alone.** The Phase 5 sweep names every handler explicitly per §3.6
  with verified file:line citations. If a future handler gains engine,
  the sweep doesn't pick it up automatically — that's a feature, not a
  bug. The author of the new handler is the right person to decide
  whether `WithRecoveryStatus()` is appropriate.

---

## 10. Out of scope

- **G13 — corpus byte trim.** Tracked separately in
  `plans/atom-corpus-context-trim-2026-04-26.md`. The `IdleOrphan`
  fixture added by §4.6 should NOT add to that plan's
  `knownOverflowFixtures` — it's a small idle-phase render and
  belongs under the size cap.
- **G1 / G2 / G12 (recipe-related findings)** per explicit user
  direction. Touch nothing in `internal/recipe/` or the v3 recipe
  tool surfaces.
- **GhostServices** — spec promise from `docs/spec-scenarios.md §6.5`
  was already removed in the prior audit pass. The orphan-meta work
  here covers the same problem space from a different angle (disk
  metas without live match) but does NOT reintroduce a `GhostServices`
  envelope field — `OrphanMetas` is the correct primary key (meta is
  on disk regardless of whether work-session declared it).
- **`internal/recipe/` per-codebase CLAUDE.md generation** — recipe
  authoring continues to write per-codebase CLAUDE.md to
  `<codebase.SourceRoot>/CLAUDE.md` as part of the published
  deliverable. That pipeline is unaffected; G8 only changes the
  container template's prose about the file's existence.
- **Auth flows, performance, local-env-specific behavior** — same as
  the prior audit's deliberate exclusions.

---

## 11. First moves for the fresh instance

1. Read this plan end to end.
2. Read the audit synthesis: `docs/audit-instruction-delivery-synthesis-2026-04-26.md`
   sections §3 (G4, G5, G8, G10, G14) for original problem statements.
3. Read CLAUDE.md project-instructions (especially the architecture
   layers section + dependency rule table) so the layer boundaries
   for the new wire DTO are second nature before you start editing.
4. Read `docs/spec-workflows.md` lines 1191-1195 (P-series invariants
   including P4) — this is the contract the G14+G10 design respects.
5. Pick **G8** to ship first as a warm-up. Single template edit, full
   test verification, commit. You'll have a green build with one
   finding closed inside 10 minutes.
6. Pick **G5** next. Add the stderr warning, write the operator doc,
   commit. Manual smoke-test the warning by running `zcp serve` in
   `/tmp` after deleting any cached CLAUDE.md.
7. Pick **G4**. Follow the Phase 1-7 sequence in §4.6.
   Codex sanity-check the `computeOrphanMetas` design once written —
   the disk-vs-live diff is the kind of off-by-one trap Codex
   reliably catches. Suggested prompt:

   ```
   node "$HOME/.claude/plugins/cache/openai-codex/codex/1.0.4/scripts/codex-companion.mjs" \
     adversarial-review --wait --scope working-tree \
     "Review the OrphanMeta computation in internal/workflow/compute_envelope.go.
      Specifically: pair-key correctness (dev/stage halves treated as
      one meta), the IsAlive session check (handle nil registry, dead
      PID, stale registry file), and the IdleOrphan derivation (must
      not fire when ANY live service exists). Cite file:line for any
      issue. Severity-grade findings."
   ```

8. Pick **G14 + G10** last. Follow Phase 1-7 sequence in §3.8. Take
   the Phase 5 sweep one handler-file per commit — the test fixtures
   for each handler often bake in the old shape, and a per-file
   commit makes a single regression easy to diagnose. Codex
   adversarial review at the end of Phase 5 (post-sweep, pre-CLAUDE.md
   addition) catches missed sites. Suggested prompt:

   ```
   node "$HOME/.claude/plugins/cache/openai-codex/codex/1.0.4/scripts/codex-companion.mjs" \
     adversarial-review --wait --scope working-tree \
     "Review the ErrorWire migration in internal/tools/. Verify:
      (1) zero remaining call sites of ANY eliminated wire shape:
          - plain-text fallback in convert.go (the pre-refactor `case nil
            err: return mcp.NewToolResultText(err.Error())` branch);
          - `jsonResult(pfResult)` in deploy_ssh.go AND deploy_local.go;
          - the ad-hoc `{preFlightFailedFor, result}` map in deploy_batch.go;
          - any direct mcp.NewToolResultText for errors anywhere in tools/;
      (2) every workflow-aware handler attaches WithRecoveryStatus()
          per the §3.6 list, INCLUDING deploy_local.go, deploy_local_git.go,
          deploy_batch.go, and guard.go (these were missed in the original
          plan and added in pass-2);
      (3) CheckWire round-trips PreAttestCmd and ExpectedExit when the
          source StepCheck has them set;
      (4) the new tests exist and pass:
          - errwire_contract_test.go::TestErrorWire_NeverCarriesEnvelopeOrPlan
          - errwire_contract_test.go::TestNoLegacyPreflightShapes
          - errwire_test.go::TestCheckWire_PreservesRunnableContract.
      Cite file:line for missed sites or contract gaps."
   ```

---

## 12. Provenance

This plan was prepared after:

1. The 2026-04-26 audit pass concluded with eight findings shipped
   (G3, G6, G7, G9, G11, G13-gate, G15, G16) and six findings
   carried over (G4, G5, G8, G10, G13-trim, G14). G13-trim has its
   own plan (`plans/atom-corpus-context-trim-2026-04-26.md`).
2. Three parallel research agents mapped the wire-shape topology,
   ServiceMeta lifecycle, and doctrine-delivery surfaces. Findings
   are summarized inline above with file:line citations rather than
   linked, so this plan stays self-contained when the audit synthesis
   doc rotates.
3. A first-pass design (envelope+plan attached to errors) was sent
   to Codex adversarial review. Six high/medium findings landed; the
   redesign in this plan responds to all six. Recovery-hint pattern
   replaces envelope attachment; tool-layer wire DTO replaces
   PlatformError extension; CheckWire `kind` discriminator replaces
   StepCheck reuse.
4. The redesigned plan went through a second Codex adversarial pass.
   Six more findings landed (3 HIGH, 3 MEDIUM); §3.4, §3.6, §3.8,
   §4.3, §4.6, §5.3.1, §5.4, §8, §9, and §11 were all revised in
   response. Notable: preflight wire shape exists in three deploy
   handlers (not one), `deploy_local*` and `deploy_batch` ARE
   workflow-aware (handler signatures verified against the codebase),
   `ComputeEnvelope` is a package function not an `Engine` method,
   `SessionRegistry.IsAlive` doesn't exist (use `ListSessions` +
   `ClassifySessions`), `CheckWire` must preserve `PreAttestCmd` +
   `ExpectedExit`, G5 stderr warning needs a unit-tested helper.
5. A third Codex pass confirmed the redesign and caught three more
   defects (1 HIGH, 1 MEDIUM, 1 LOW) — all addressed in §4.3 (`SessionID`
   was a non-existent type; switched to `string`), §3.4 (deploy_batch
   contract pinned to first-failure-abort with hostname-in-message),
   and §5.3.1 (G5 helper placement nailed to `internal/init/`). All
   pass-3 findings have corresponding plan edits cited from §2.3.

The audit synthesis doc remains the historical record of what was
observed live and what slipped through; this plan is the executable
follow-through. Both are 2026-04-26 artifacts; the earlier audit
(`docs/audit-workflow-llm-information-flow.md`, 2026-04-25) and the
F1-F4 / T1-T3 prior findings are all transitively cited from the
synthesis doc if needed.
