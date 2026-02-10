# ZCP — Zerops Control Plane

Single Go binary merging ZAIA CLI + ZAIA-MCP. AI-driven Zerops PaaS management via MCP protocol.

---

## Source of Truth

```
1. Tests (table-driven, executable)    ← AUTHORITATIVE for behavior
2. Code (Go types, interfaces)         ← AUTHORITATIVE for implementation
3. Design docs (design/*.md)           ← AUTHORITATIVE for intent & invariants
4. Plans (plans/*.md)                  ← TRANSIENT (roadmap, expires)
5. CLAUDE.md                           ← OPERATIONAL (workflow, conventions)
```

---

## Architecture

```
cmd/zcp/main.go → internal/server → MCP tools → internal/ops → internal/platform → Zerops API
                                                                internal/auth
                                                                internal/knowledge (BM25)
```

| Package | Responsibility | Key file |
|---------|---------------|----------|
| `cmd/zcp` | Entrypoint, STDIO server | `main.go` |
| `internal/server` | MCP server setup, registration | `server.go` |
| `internal/tools` | MCP tool handlers (12 tools) | `discover.go`, `manage.go`, ... |
| `internal/ops` | Business logic, validation | `discover.go`, `manage.go`, ... |
| `internal/platform` | Zerops API client, types, errors | `client.go`, `errors.go` |
| `internal/auth` | Token resolution (env var / zcli), project discovery | `auth.go` |
| `internal/knowledge` | BM25 search engine, embedded docs | `engine.go` |

Error codes: see `internal/platform/errors.go` for all codes (AUTH_REQUIRED, SERVICE_NOT_FOUND, etc.)

---

## TDD — Mandatory

1. **RED**: Write failing test BEFORE implementation
2. **GREEN**: Minimal code to pass
3. **REFACTOR**: Clean up, tests stay green

### Seed test pattern

Write ONE seed test per new package — establishes naming, structure, helpers. Follow for all subsequent tests.

```go
func TestDiscover_WithService_Success(t *testing.T) {
    t.Parallel() // OMIT for packages with global state (e.g., output.SetWriter)
    tests := []struct { ... }{ ... }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel() // OMIT for packages with global state
            // ...
        })
    }
}
```

### Testing layers

| Layer | Scope | Command |
|-------|-------|---------|
| Unit | platform, auth, ops | `go test ./internal/platform/... ./internal/auth/... ./internal/ops/...` |
| Tool | MCP handlers | `go test ./internal/tools/...` |
| Integration | Multi-tool flows | `go test ./integration/` |
| E2E | Real Zerops API | `go test ./e2e/ -tags e2e` |

### Rules

- **Table-driven tests** — no exceptions
- **`testing.Short()`** — long tests must check and skip
- **`t.Parallel()` only where safe** — document global state preventing it (see seed patterns above)
- **Test naming**: `Test{Op}_{Scenario}_{Result}` (e.g. `TestDiscover_WithService_Success`)
- **Test headers**: `// Tests for: design/<feature>.md § <section>`

### Design-doc workflow

For features with `design/<feature>.md`:
1. Read design doc (MUST/WHEN-THEN contracts) → read plan (scope, test cases)
2. Write failing tests with header → implement minimal code to pass
3. Update plan status (RED → GREEN → DONE), capture divergent decisions

Design docs: **stable**, MUST/WHEN-THEN only, no code. Without design doc: standard TDD. >50 lines: create design doc first.

Automated: Tier 1 (edit) → Tier 2 (turn) → Tier 3 (commit) → Tier 4 (CI). See `.claude/settings.json`.

---

## Commands

```bash
go test ./internal/<pkg> -run TestName -v   # Single test
go test ./... -count=1 -short               # All tests (fast)
go test ./... -count=1                      # All tests (full, add -race for race detection)
go build -o bin/zcp ./cmd/zcp              # Build
make setup                                  # Bootstrap dev environment
make lint-fast                              # Fast lint (~3s)
make lint-local                             # Full lint (~15s)
```

---

## Conventions

- **JSON-only stdout** — debug to stderr (if `--debug`)
- **Service by hostname** — resolve to ID internally
- **Prefer simplest solution** — plain functions over abstractions, fewer lines over more
- **Stateless STDIO tools** — each MCP call = fresh operation, not HTTP
- **MockClient + MockExecutor for tests** — `platform.MockClient` for API, in-memory MCP for tools
- **Max 300 lines per .go file** — split when growing
- **English everywhere** — code, comments, docs, commits
- **`go.sum` committed, no `vendor/`** — reproducible builds via module cache

## Do NOT

- Use global mutable state (except `sync.Once` for init)
- Use `replace` directives in go.mod (temporary dev only, never committed)
- Use `interface{}`/`any` when concrete type is known, or `panic()` — use concrete types, return errors
- Skip error checks — `errcheck` enforces this
- Write tests and implementation in the same commit without RED phase first
- Add `t.Parallel()` to packages with global state without making state thread-safe first
- Use `fmt.Sprintf` for SQL/shell commands — use parameterized queries only
- Hold mutexes during I/O (network, disk) — copy data under lock, release, then I/O
- Return bare `err` without context — always `fmt.Errorf("op: %w", err)`
- Iteratively fix security issues — each fix must be independently validated

---

## Maintenance

| Change | Action |
|--------|--------|
| New package | Update Architecture table |
| New MCP tool | Update Architecture table + register in server.go |
| New convention | Add to Conventions (max 15 bullets) |
| Interface change | Verify key file reference still accurate |
| New error code | Add to `internal/platform/errors.go` |
| Global state added | Document in test seed as non-parallel + add comment |
| Design doc concept change | Update design doc, verify tests still match |
| Plan completed | Move to plans/archive/ |
| New feature area | Create design doc before implementation |
