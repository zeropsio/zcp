# Phase 4: Outputs — Metadata, Reflog, Guidance

**Agent**: DELTA (outputs + content)
**Dependencies**: Phase 3 complete
**Risk**: LOW — all additive, new files only

---

## Feature 4A: Per-Service Decision Metadata

### Purpose
Record decisions made during bootstrap (deploy flow, mode, dependencies) as historical records at `.zcp/services/{hostname}.json`. These are NOT state — they're decisions that inform future sessions.

### Implementation

```go
// internal/workflow/service_meta.go (NEW ~50 lines)
type ServiceMeta struct {
    Hostname         string            `json:"hostname"`
    Type             string            `json:"type"`
    Mode             string            `json:"mode"`             // "standard" or "simple"
    StageHostname    string            `json:"stageHostname,omitempty"`
    DeployFlow       string            `json:"deployFlow"`       // "ssh"
    Dependencies     []string          `json:"dependencies,omitempty"`
    BootstrapSession string            `json:"bootstrapSession"`
    BootstrappedAt   string            `json:"bootstrappedAt"`
    Decisions        map[string]string `json:"decisions,omitempty"`
}

func WriteServiceMeta(baseDir string, meta *ServiceMeta) error {
    dir := filepath.Join(baseDir, "services")
    os.MkdirAll(dir, 0o755)
    path := filepath.Join(dir, meta.Hostname+".json")
    // atomic write: temp + rename
}

func ReadServiceMeta(baseDir, hostname string) (*ServiceMeta, error) {
    path := filepath.Join(baseDir, "services", hostname+".json")
    // read + unmarshal, return nil if not found
}
```

### Tests

```
internal/workflow/service_meta_test.go (NEW ~80 lines):
  TestWriteServiceMeta_Success
  TestWriteServiceMeta_CreatesDirectory
  TestReadServiceMeta_Success
  TestReadServiceMeta_NotFound_ReturnsNil
  TestServiceMeta_JSONRoundTrip
```

### Integration
Called from verify step completion in engine (after all hard checks pass):
```go
// In verify step auto-complete path:
for _, target := range plan.Targets {
    meta := &ServiceMeta{
        Hostname: target.Runtime.DevHostname,
        Type: target.Runtime.Type,
        // ... populate from plan + session context
    }
    WriteServiceMeta(e.stateDir, meta)
}
```

---

## Feature 4B: CLAUDE.md Reflog

### Purpose
Append-only historical record in CLAUDE.md. Each bootstrap adds one entry. Never updated, never regenerated.

### Implementation

```go
// internal/workflow/reflog.go (NEW ~50 lines)
func AppendReflogEntry(claudeMDPath string, intent string, targets []BootstrapTarget, sessionID string, timestamp string) error {
    // Generate markdown entry
    // Append to file (create if not exists)
    // Each entry wrapped in <!-- ZEROPS:REFLOG --> markers
}
```

### Entry Format

```markdown
<!-- ZEROPS:REFLOG -->
### 2026-03-04 — Bootstrap: {intent summary}

- **Runtime:** {devHostname}/{stageHostname} ({type})
- **Dependencies:** {dep1} ({type1}), {dep2} ({type2})
- **Evidence:** .zcp/evidence/{sessionID}/
- **Mode:** {standard|simple}

> This is a historical record. Verify current state via `zerops_discover`.
<!-- /ZEROPS:REFLOG -->
```

### Tests

```
internal/workflow/reflog_test.go (NEW ~80 lines):
  TestAppendReflogEntry_NewFile
  TestAppendReflogEntry_ExistingFile_Appends
  TestAppendReflogEntry_MultipleEntries
  TestAppendReflogEntry_CorrectFormat
  TestAppendReflogEntry_MultiTarget
```

---

## Feature 4C: Clarification + Mode-Aware Guidance

### Clarification in Discover Step
Update `bootstrap_steps.go` discover guidance to include:
1. Gather context (zerops_discover)
2. Load knowledge (zerops_knowledge)
3. Clarify with user (if ambiguous) — ask RUNTIME, MANAGED SERVICES, MODE
4. Submit structured plan

### Mode-Aware Generate
Update `BuildResponse()` to filter guidance by plan mode:
- Standard mode → dev+stage template only
- Simple mode → single-service template only
- Prevents LLM from mixing templates

### Tests
```
internal/workflow/bootstrap_test.go:
  TestBuildResponse_StandardMode_FilteredGuidance
  TestBuildResponse_SimpleMode_FilteredGuidance

internal/workflow/bootstrap_guidance_test.go:
  TestExtractSection_DiscoverContainsClarification
```

---

## Feature 4D: Content Deduplication

### Problem
bootstrap.md has duplicated content:
- /status endpoint spec (3x)
- Hostname rules (4x)
- Dev vs stage config matrix
- PHP runtime exceptions

### Solution
Add reference appendix at end of bootstrap.md. Replace inline duplicates with "see Appendix: {topic}".

### Tests
```
internal/workflow/bootstrap_guidance_test.go:
  TestExtractSection_AppendixExtractable
```

---

## Deploy & Test

```bash
go test ./internal/workflow/... -count=1 -v
go test ./... -count=1 -short
make lint-fast
./eval/scripts/build-deploy.sh
```

### Live Tests
1. Complete full bootstrap → check `.zcp/services/{hostname}.json` files exist on zcpx
2. Complete full bootstrap → check CLAUDE.md has reflog entry
3. Start bootstrap with simple mode plan → verify guidance is mode-filtered
