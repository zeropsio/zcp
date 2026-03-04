# Phase 0: Independent Quick Wins

**Agent**: DELTA
**Dependencies**: None
**Risk**: None — all additive/fix changes

---

## Feature 0A: Import Error Surfacing (C5)

### Problem
`internal/ops/import.go` silently drops per-service API errors from `ImportedServiceStack.Error`. Services that fail import with API error (not process failure) are invisible to the LLM.

### Implementation

**RED** (tests first):
```
internal/ops/import_test.go:
  TestImport_ServiceError_Surfaced — import with one service having ss.Error set
  TestImport_MixedSuccessAndError — 3 services: 2 succeed, 1 has error
  TestImport_AllErrors — all services fail with API errors
```

**GREEN**:
```go
// internal/ops/import.go
type ServiceImportError struct {
    Service string `json:"service"`
    Error   string `json:"error"`
}

type ImportResult struct {
    // existing fields...
    ServiceErrors []ServiceImportError `json:"serviceErrors,omitempty"`
}

// In the ServiceStacks loop, add:
if ss.Error != "" {
    result.ServiceErrors = append(result.ServiceErrors, ServiceImportError{
        Service: ss.Name,
        Error:   ss.Error,
    })
}
```

Also map `p.FailReason` to `ImportProcessOutput.FailReason` (currently dead code).

### Verification
```bash
go test ./internal/ops/... -run TestImport -v
```

---

## Feature 0B: BuildInstructions Routing Fix (H2)

### Problem
When project is CONFORMANT (has dev+stage pairs), system prompt routes to deploy workflow. But if user wants to ADD a new runtime type that doesn't exist, bootstrap is the correct workflow.

### Implementation

**RED**:
```
internal/server/instructions_test.go (new or extend):
  TestBuildProjectSummary_Conformant_SuggestsBootstrapForNewRuntime
  TestBuildProjectSummary_Conformant_SuggestsDeployForExisting
```

**GREEN**:
Update `buildProjectSummary()` CONFORMANT case:
```go
case workflow.StateConformant:
    b.WriteString("\nDev+stage service pairs detected.")
    b.WriteString("\nIf the request matches existing services, use: zerops_workflow action=\"start\" workflow=\"deploy\"")
    b.WriteString("\nTo ADD new services (different runtime type), use: zerops_workflow action=\"start\" workflow=\"bootstrap\"")
    b.WriteString("\nIf the user wants a DIFFERENT stack, ASK how to proceed before making any changes.")
    b.WriteString("\nDo NOT delete existing services without explicit user approval.")
```

### Verification
```bash
go test ./internal/server/... -v
```

---

## Feature 0C: KnowledgeTracker Per-Type (H10)

### Problem
`KnowledgeTracker.IsLoaded()` returns true if ANY briefing was loaded. Multi-runtime bootstrap needs per-type tracking: "has php-nginx briefing been loaded?" separately from "has nodejs briefing been loaded?"

### Implementation

**RED**:
```
internal/ops/knowledge_tracker_test.go:
  TestKnowledgeTracker_IsLoadedForType_SingleRuntime
  TestKnowledgeTracker_IsLoadedForType_MultiRuntime — loaded PHP, not Node
  TestKnowledgeTracker_IsLoadedForType_EmptyRuntime — edge case
```

**GREEN**:
```go
// Extract runtime type from briefingCalls entries (format: "runtime+service1,service2")
func (kt *KnowledgeTracker) IsLoadedForType(runtimeType string) bool {
    kt.mu.Lock()
    defer kt.mu.Unlock()
    for _, entry := range kt.briefingCalls {
        rt, _, _ := strings.Cut(entry, "+")
        if rt == runtimeType {
            return true
        }
    }
    return false
}
```

Keep existing `IsLoaded()` as-is for backward compat.

### Verification
```bash
go test ./internal/ops/... -run TestKnowledgeTracker -v
```

---

## Feature 0D: Build Polling Speedup

### Implementation

**RED**:
```
internal/ops/progress_test.go:
  Update expected intervals in existing tests
```

**GREEN**:
```go
// internal/ops/progress.go
var defaultBuildPollConfig = pollConfig{
    initialInterval: 1 * time.Second,    // was 3s
    stepUpInterval:  5 * time.Second,    // was 10s
    stepUpAfter:     30 * time.Second,   // was 60s
    timeout:         15 * time.Minute,   // unchanged
}
```

### Verification
```bash
go test ./internal/ops/... -run TestPoll -v
```

---

## Deploy & Test

```bash
go test ./internal/ops/... ./internal/server/... -count=1 -v
go test ./... -count=1 -short
make lint-fast
./eval/scripts/build-deploy.sh
```

### Live Tests on zcpx
1. `zerops_import` with invalid service type → verify `serviceErrors` in response
2. `zerops_knowledge runtime="php-nginx@8.4"` then check tracker state
3. Build a service → verify faster polling (subjective)
