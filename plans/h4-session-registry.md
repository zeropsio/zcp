# H4 — Session Registry: Full Implementation Plan

Self-contained reference for implementing multi-process session management in ZCP.

---

## 1. Problem Statement

### 1.1 Current Architecture

Single `zcp_state.json` file at `.zcp/state/zcp_state.json`. No PID tracking, no locking, no stale detection.

**Key files (current state):**

#### `internal/workflow/state.go` (84 lines)
```go
type WorkflowState struct {
    Version   string            `json:"version"`
    SessionID string            `json:"sessionId"`
    ProjectID string            `json:"projectId"`
    Workflow  string            `json:"workflow"`
    Phase     Phase             `json:"phase"`
    Iteration int               `json:"iteration"`
    Intent    string            `json:"intent"`
    CreatedAt string            `json:"createdAt"`
    UpdatedAt string            `json:"updatedAt"`
    History   []PhaseTransition `json:"history"`
    Bootstrap *BootstrapState   `json:"bootstrap,omitempty"`
}

// Phase constants: PhaseInit, PhaseDiscover, PhaseDevelop, PhaseDeploy, PhaseVerify, PhaseDone
// Immediate (stateless) workflows: debug, scale, configure
```

#### `internal/workflow/session.go` (146 lines)
```go
const (
    stateFileName = "zcp_state.json"
    stateVersion  = "1"
)

func InitSession(stateDir, projectID, workflowName, intent string) (*WorkflowState, error)
    // Checks if LoadSession succeeds → error "active session exists"
    // Generates random 8-byte sessionID
    // Creates WorkflowState with Phase=PhaseInit
    // Calls saveState()

func LoadSession(stateDir string) (*WorkflowState, error)
    // Reads stateDir/zcp_state.json → unmarshal

func ResetSession(stateDir string) error
    // os.Remove(stateDir/zcp_state.json)

func IterateSession(stateDir, evidenceDir string) (*WorkflowState, error)
    // LoadSession, archive evidence, reset to PhaseDevelop, increment iteration

func saveState(stateDir string, state *WorkflowState) error
    // Atomic: MkdirAll, MarshalIndent, CreateTemp, Write, Close, Rename

func generateSessionID() (string, error)
    // crypto/rand 8 bytes → hex
```

#### `internal/workflow/engine.go` (295 lines)
```go
type Engine struct {
    stateDir    string
    evidenceDir string
}

func NewEngine(baseDir string) *Engine
    // stateDir = baseDir, evidenceDir = baseDir/evidence

func (e *Engine) Start(projectID, workflowName, intent string) (*WorkflowState, error)
    // LoadSession → if DONE, auto-reset; if active, error "reset first"
    // Returns InitSession(...)

func (e *Engine) Transition(phase Phase) (*WorkflowState, error)
    // LoadSession, validate transition, CheckGate, save

func (e *Engine) RecordEvidence(ev *Evidence) error
    // LoadSession, set ev.SessionID, SaveEvidence

func (e *Engine) Reset() error
    // ResetSession(e.stateDir)

func (e *Engine) Iterate() (*WorkflowState, error)
    // IterateSession(e.stateDir, e.evidenceDir)

func (e *Engine) HasActiveSession() bool
    // _, err := LoadSession(e.stateDir); return err == nil

func (e *Engine) GetState() (*WorkflowState, error)
    // LoadSession(e.stateDir)

func (e *Engine) BootstrapStart(projectID, intent string) (*BootstrapResponse, error)
    // e.Start("bootstrap"), create NewBootstrapState, save

func (e *Engine) BootstrapComplete(ctx context.Context, stepName string, attestation string, checker StepChecker) (*BootstrapResponse, error)
    // LoadSession, run checker (hard check), CompleteStep, mark next in_progress
    // If all done: autoCompleteBootstrap (evidence + WriteServiceMeta + AppendReflogEntry)

func (e *Engine) BootstrapCompletePlan(targets []BootstrapTarget, liveTypes, liveServices) (*BootstrapResponse, error)
    // LoadSession, ValidateBootstrapTargets, CompleteStep("discover"), store plan

func (e *Engine) BootstrapSkip(stepName, reason string) (*BootstrapResponse, error)
    // LoadSession, SkipStep, mark next in_progress

func (e *Engine) StoreDiscoveredEnvVars(hostname string, vars []string) error
    // LoadSession, set Bootstrap.DiscoveredEnvVars[hostname], save

func (e *Engine) BootstrapStatus() (*BootstrapResponse, error)
    // LoadSession, return BuildResponse (read-only)
```

#### Callers (who uses Engine/session functions)

| Caller | What it calls | File |
|--------|--------------|------|
| `server/instructions.go:63` | `workflow.LoadSession(stateDir)` directly | System prompt hint |
| `tools/guard.go:19` | `engine.HasActiveSession()` | Workflow guard |
| `tools/workflow.go` | All Engine methods via MCP handlers | Tool layer |
| `tools/workflow_bootstrap.go` | `handleBootstrapComplete`, `handleBootstrapSkip` | Bootstrap handlers |
| `tools/workflow_checks.go:101` | `engine.StoreDiscoveredEnvVars()` | checkProvision |
| `tools/workflow_checks_test.go` | `eng.GetState()` | Test assertions |
| `tools/workflow_test.go:278` | `engine.HasActiveSession()` | Test assertions |

### 1.2 Problems

1. **Stale sessions:** Crashed ZCP process leaves active session → new bootstrap blocked until manual `action="reset"`
2. **Concurrent corruption:** Multiple Claude instances (typically 5) share `.zcp/state/`, each running `zcp serve`. Concurrent writes to `zcp_state.json` corrupt state.
3. **No awareness:** No instance knows what other instances are doing
4. **Single session limit:** Only one workflow at a time, even though independent workflows (deploy phpdev + deploy apidev) should coexist

### 1.3 Requirements

1. Central registry tracks all active sessions
2. Each MCP instance registers on start, unregisters on completion
3. Each instance manages its own workflow independently
4. PID-based stale detection for crashed processes
5. Bootstrap exclusivity: one active bootstrap per project
6. Multiple deploys can coexist (different services)
7. Must work on Darwin (macOS) — flock, PID checking

---

## 2. Design

### 2.1 Disk Layout

```
.zcp/state/
  .registry.lock           # flock target (empty file, never written to)
  registry.json            # Index of active sessions (read/write under flock)
  sessions/
    {sessionID}.json       # Per-session WorkflowState (one per zcp serve process)
  evidence/                # Unchanged — already scoped by sessionID
    {sessionID}/*.json
  services/                # Unchanged — shared across sessions
    {hostname}.json
```

### 2.2 Registry Types

```go
// registry.go
type Registry struct {
    Version  string         `json:"version"`
    Sessions []SessionEntry `json:"sessions"`
}

type SessionEntry struct {
    SessionID string `json:"sessionId"`
    PID       int    `json:"pid"`
    Workflow  string `json:"workflow"`   // "bootstrap", "deploy"
    ProjectID string `json:"projectId"`
    Phase     Phase  `json:"phase"`
    Intent    string `json:"intent"`
    CreatedAt string `json:"createdAt"`
    UpdatedAt string `json:"updatedAt"`
}
```

### 2.3 Locking Strategy

- **Lock target:** `.registry.lock` file (never written to, only flock'd)
- **Lock type:** `syscall.Flock(fd, syscall.LOCK_EX)` — exclusive, blocking
- **Lock duration:** Microseconds — read registry JSON, modify in-memory, write back, release
- **No I/O under lock** beyond reading/writing `registry.json` itself
- **Lock release:** FD close (automatic on process exit if crashed)

```go
func withRegistryLock(stateDir string, fn func(*Registry) (*Registry, error)) error {
    lockPath := filepath.Join(stateDir, ".registry.lock")
    // Open lock file (create if needed)
    // syscall.Flock(fd, LOCK_EX)
    // defer fd.Close() (releases lock)
    // Read registry.json (or empty Registry if missing)
    // Call fn(registry)
    // If fn returns non-nil registry, write it back (atomic temp+rename)
}
```

### 2.4 Registry Functions

```go
func RegisterSession(stateDir string, entry SessionEntry) error
    // withRegistryLock: append entry to Sessions

func UnregisterSession(stateDir, sessionID string) error
    // withRegistryLock: remove entry by sessionID (no-op if not found)

func UpdateRegistryEntry(stateDir, sessionID string, phase Phase) error
    // withRegistryLock: find entry, update Phase + UpdatedAt

func ListSessions(stateDir string) ([]SessionEntry, error)
    // withRegistryLock: RefreshRegistry (prune dead), return copy of Sessions

func RefreshRegistry(stateDir string) error
    // withRegistryLock: for each entry, if !isProcessAlive(PID), remove + clean session file

func isProcessAlive(pid int) bool
    // if pid <= 0 → false
    // os.FindProcess(pid) then p.Signal(syscall.Signal(0))
    // nil error = alive, ESRCH = dead
    // Works on Darwin: same-user processes always checkable
```

### 2.5 State Changes

**`state.go`** — Add PID field:
```go
type WorkflowState struct {
    // ... all existing fields unchanged ...
    PID       int               `json:"pid"`  // NEW: owning process PID
}
```

JSON backward compatible: old files deserialize with `PID: 0`.

### 2.6 Session Changes

**`session.go`** — Per-session file paths:

```go
const (
    stateVersion    = "2"        // bumped from "1"
    sessionsDirName = "sessions"
    oldStateFile    = "zcp_state.json"  // v1 singleton path (migration)
)

func sessionFilePath(stateDir, sessionID string) string {
    return filepath.Join(stateDir, sessionsDirName, sessionID+".json")
}

func InitSession(stateDir, projectID, workflowName, intent string) (*WorkflowState, error)
    // NO LONGER checks for existing singleton
    // Generates sessionID, creates WorkflowState with PID: os.Getpid()
    // Saves to sessions/{id}.json
    // Calls RegisterSession()

func LoadSessionByID(stateDir, sessionID string) (*WorkflowState, error)
    // Reads sessions/{sessionID}.json → unmarshal

func ResetSessionByID(stateDir, sessionID string) error
    // Remove sessions/{sessionID}.json
    // Call UnregisterSession()

func IterateSession(stateDir, evidenceDir, sessionID string) (*WorkflowState, error)
    // LoadSessionByID, archive, reset to DEVELOP, save

func saveSessionState(stateDir, sessionID string, state *WorkflowState) error
    // Atomic write to sessions/{sessionID}.json

// Migration
func LoadSession(stateDir string) (*WorkflowState, error)
    // KEPT for migration only — reads old zcp_state.json

func migrateV1(stateDir string) (*WorkflowState, error)
    // If old zcp_state.json exists AND sessions/ doesn't:
    //   Load old state
    //   If DONE: delete old file, return nil (no migration needed)
    //   If active: copy to sessions/{id}.json, set PID, register, delete old file
    //   Return migrated state
```

### 2.7 Engine Changes

**Engine gains `sessionID` field:**
```go
type Engine struct {
    stateDir    string
    evidenceDir string
    sessionID   string  // set after Start/BootstrapStart, empty before
}
```

**No mutex needed:** STDIO = sequential request-response per process. No concurrent method calls within one Engine instance.

**`SessionID() string`** — new getter, returns `e.sessionID`.

**`Start()` flow:**
```
1. Check for v1 migration (migrateV1)
2. If migrated state returned: set e.sessionID, return it
3. RefreshRegistry (prune dead PIDs)
4. Scan registry: session with THIS PID exists? → reconnection (set e.sessionID, return)
5. For bootstrap: scan for active bootstrap with same projectID → error if found
6. InitSession (creates per-session file + registers)
7. Set e.sessionID
```

**`HasActiveSession() bool`** — returns `e.sessionID != ""`. Process-local check, no file I/O.

**`GetState()`** — `LoadSessionByID(e.stateDir, e.sessionID)`

**`Reset()`** — `ResetSessionByID(e.stateDir, e.sessionID)`, clear `e.sessionID`

**`Iterate()`** — `IterateSession(e.stateDir, e.evidenceDir, e.sessionID)`

**`Transition(phase)`** — `LoadSessionByID`, validate, `CheckGate`, save, `UpdateRegistryEntry`

**All Bootstrap methods** — route through `e.sessionID` for load/save.

**New method:**
```go
func (e *Engine) ListActiveSessions() ([]SessionEntry, error)
    // Delegates to ListSessions(e.stateDir)
```

### 2.8 Caller Changes

#### `server/instructions.go:buildWorkflowHint()`

Currently calls `workflow.LoadSession(stateDir)` directly (singleton). Change to read registry:

```go
func buildWorkflowHint(stateDir string) string {
    sessions, err := workflow.ListSessions(stateDir)
    if err != nil || len(sessions) == 0 {
        return ""
    }
    // Format all active sessions as hints
    // Each Claude instance sees its own session + awareness of others
}
```

#### `tools/guard.go:requireWorkflow()`

Calls `engine.HasActiveSession()` — now returns `e.sessionID != ""`. **No change needed** to guard.go itself, only the Engine method semantics change.

#### `tools/workflow.go`

Add `action="list"` handler that returns `engine.ListActiveSessions()`.

No changes to `WorkflowInput` — session ID is NOT passed by agent. Engine stores it internally after `Start()`.

---

## 3. Bootstrap Exclusivity

During `BootstrapStart()`:
1. Lock registry
2. Refresh (prune dead PIDs)
3. Scan living entries for `workflow=="bootstrap"` with matching projectID and phase != DONE
4. If found → error: `"bootstrap already active (session %s, PID %d)"`
5. Otherwise create and register

Multiple deploy workflows can coexist (they target different services).

---

## 4. Process Lifecycle

### Normal flow
```
zcp serve starts → Engine created (sessionID="")
  → Agent calls action="start" workflow="bootstrap"
    → InitSession → sessionID set, registered in registry
  → Agent works through steps (complete, skip, etc.)
  → Final step completes → autoCompleteBootstrap → DONE
  → Agent calls action="start" workflow="deploy" (or new bootstrap)
    → DONE session unregistered, new session created
```

### Process crash
```
zcp serve crashes → registry entry has dead PID
  → Next zcp serve starts → Engine created (sessionID="")
  → Agent calls action="start"
    → RefreshRegistry prunes dead PID entry
    → Fresh session created
  → Old session file in sessions/ remains as forensic artifact
```

### Reconnection (Claude Code restarts connection to running zcp serve)
```
zcp serve still running → Engine has sessionID in memory
  → Claude Code reconnects → system prompt calls buildWorkflowHint
    → Shows active session from registry
  → Agent resumes with action="status" or action="complete"
    → Engine uses stored sessionID
```

### Process restart with existing session
```
zcp serve restarts → Engine created (sessionID="")
  → Agent calls action="start" with same workflow
    → RefreshRegistry: old PID dead → pruned
    → New session created (old session file orphaned)
```

---

## 5. Edge Cases

| Case | Handling |
|------|----------|
| **PID reuse (Darwin ~100K cycle)** | RefreshRegistry also checks `UpdatedAt` — entries >24h old with "alive" PID are pruned. Manual `reset` always available. |
| **Crash mid-flock write** | temp+rename for registry.json ensures atomicity. Lock file FD closes on exit → lock released. |
| **Two processes start bootstrap simultaneously** | flock serializes — second sees first's entry and fails. |
| **Multiple working directories** | Each has its own `.zcp/state/`. No cross-directory conflicts. |
| **Old v1 state file** | `migrateV1()` handles it once — first v2 binary start migrates or deletes. |
| **PID=0 in old state** | Treated as dead during RefreshRegistry. Migrated or pruned. |

---

## 6. Test Plan (RED first — TDD)

### `registry_test.go` (new file, ~220 lines)

| Test | Validates |
|------|-----------|
| `TestRegisterSession_Success` | Register + list → entry present |
| `TestRegisterSession_Multiple` | Register 3 → all present |
| `TestUnregisterSession_Success` | Register + unregister → absent |
| `TestUnregisterSession_NotFound` | Unregister nonexistent → no error |
| `TestRefreshRegistry_PrunesDeadPIDs` | Dead PID=999999 → pruned |
| `TestRefreshRegistry_KeepsLivePIDs` | PID=os.Getpid() → kept |
| `TestUpdateRegistryEntry_Success` | Phase update reflected |
| `TestListSessions_EmptyRegistry` | Empty dir → empty slice |
| `TestListSessions_AutoRefreshes` | Dead PID registered → list returns empty |
| `TestWithRegistryLock_ConcurrentAccess` | Goroutines contend → no corruption |
| `TestIsProcessAlive_CurrentProcess` | os.Getpid() → true |
| `TestIsProcessAlive_DeadProcess` | 999999 → false |
| `TestIsProcessAlive_ZeroPID` | 0 → false |

### `session_test.go` (updated, ~280 lines)

| Test | Validates |
|------|-----------|
| `TestInitSession_PerSessionFile` | File at `sessions/{id}.json`, not `zcp_state.json` |
| `TestInitSession_SetsPID` | `state.PID == os.Getpid()` |
| `TestInitSession_RegistersInRegistry` | Entry present in registry |
| `TestLoadSessionByID_Success` | Init then load by ID |
| `TestLoadSessionByID_NotFound` | Missing ID → error |
| `TestResetSessionByID_DeletesFile` | Session file removed |
| `TestResetSessionByID_Unregisters` | Registry entry removed |
| `TestIterateSession_WithSessionID` | Takes sessionID param, works correctly |
| `TestMigrateV1_ActiveSession` | Old singleton → per-session + registry |
| `TestMigrateV1_DoneSession` | Old DONE → deleted, returns nil |
| `TestMigrateV1_NoOldFile` | No migration needed, returns nil |

### `engine_test.go` (updated, ~1100 lines)

| Test | Validates |
|------|-----------|
| `TestEngine_Start_StoresSessionID` | `SessionID() != ""` after Start |
| `TestEngine_Start_RegistersInRegistry` | Entry in ListActiveSessions |
| `TestEngine_Reset_ClearsSessionID` | `SessionID() == ""` after Reset |
| `TestEngine_Reset_Unregisters` | Entry removed from registry |
| `TestEngine_MultipleEngines_Coexist` | Two engines in same stateDir, both active |
| `TestEngine_BootstrapExclusivity` | Second bootstrap fails while first active |
| `TestEngine_BootstrapExclusivity_DeadPID` | First PID dead → second succeeds |
| `TestEngine_HasActiveSession_NoSession` | Before Start → false |
| `TestEngine_HasActiveSession_AfterStart` | After Start → true |
| `TestEngine_Transition_UpdatesRegistry` | Phase change reflected in registry |
| `TestEngine_Reconnection_SamePID` | Start finds existing session with same PID → reuses |

---

## 7. Implementation Sequence

### Phase 1: Registry Primitives
**Files:** `registry.go` (new ~180 lines), `registry_test.go` (new ~220 lines)

Create:
- Types: `Registry`, `SessionEntry`
- `withRegistryLock()` with flock
- `isProcessAlive()` with signal(0)
- `RegisterSession`, `UnregisterSession`, `ListSessions`, `RefreshRegistry`, `UpdateRegistryEntry`
- All tests (RED first)

**Verify:**
```bash
go test ./internal/workflow/... -run "TestRegister|TestUnregister|TestRefresh|TestList|TestIsProcess|TestWithRegistry" -v
```

### Phase 2: Per-Session State Files
**Files:** `state.go` (+2 lines), `session.go` (~200 lines rewrite), `session_test.go` (~280 lines)

Changes:
- Add `PID int` to `WorkflowState`
- `sessionFilePath()`, `LoadSessionByID()`, `saveSessionState()`, `ResetSessionByID()`
- Update `InitSession()` to create per-session file + register
- `IterateSession()` gains sessionID param
- `migrateV1()` for old format
- Keep `LoadSession()` for migration only
- All tests (RED first)

**Verify:**
```bash
go test ./internal/workflow/... -run "TestInitSession|TestLoadSession|TestResetSession|TestMigrate|TestIterate" -v
```

### Phase 3: Engine Integration
**Files:** `engine.go` (~330 lines rewrite), `engine_test.go` (~1100 lines)

Changes:
- Add `sessionID` field to Engine
- All methods route through `LoadSessionByID(e.stateDir, e.sessionID)`
- `Start()` with migration + reconnection + exclusivity logic
- `Reset()` clears sessionID + unregisters
- `HasActiveSession()` = process-local check
- `SessionID()` getter
- `ListActiveSessions()` method
- All tests (RED first)

**Verify:**
```bash
go test ./internal/workflow/... -run "TestEngine_" -v
```

### Phase 4: Tools & Server
**Files:** `server/instructions.go` (~5 lines changed), `tools/workflow.go` (~15 lines added)

Changes:
- `buildWorkflowHint()` reads registry via `ListSessions`
- `action="list"` handler
- Update guard.go if needed (likely no change)

**Verify:**
```bash
go test ./internal/server/... -v
go test ./internal/tools/... -v
go test ./integration/... -v
go test ./... -count=1 -short
make lint-fast
```

---

## 8. File Change Summary

| File | Lines (current) | Action | Est. lines (after) |
|------|----------------|--------|---------------------|
| `internal/workflow/registry.go` | NEW | Create | ~180 |
| `internal/workflow/registry_test.go` | NEW | Create | ~220 |
| `internal/workflow/state.go` | 84 | Add PID field | ~90 |
| `internal/workflow/session.go` | 146 | Per-session paths, migration, new functions | ~200 |
| `internal/workflow/session_test.go` | 234 | Update + migration tests | ~280 |
| `internal/workflow/engine.go` | 295 | Add sessionID, route all methods | ~330 |
| `internal/workflow/engine_test.go` | 1033 | Add multi-engine, exclusivity tests | ~1100 |
| `internal/server/instructions.go` | 127 | Read registry instead of singleton | ~130 |
| `internal/tools/workflow.go` | 321 | Add action="list" | ~340 |

**Total new/changed: ~2870 lines across 9 files (2 new, 7 modified).**
