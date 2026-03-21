# Team Execute

`$ARGUMENTS` = `<filepath>` — path to the plan file (ideally `.final.md` or `.vN.md`). If no arguments provided, ask the user for the plan filepath.

## Setup

1. Read the plan file at `<filepath>`.
2. Extract `<basename>` and `<dir>` from filepath.
3. Look for supporting context in this priority order:
   a. `<dir>/<basename>.context.md` — analysis/review context with decisions, rejected alternatives, resolved concerns, and confidence map.
   b. `<dir>/<basename>.analysis-*.md` — analysis reports (from `/team-analyze`).
   c. `<dir>/<basename>.review-*.md` — review reports (legacy).
   Read all that exist. Later files take precedence on conflicting decisions.
4. Read `/Users/macbook/Documents/Zerops-MCP/zcp/CLAUDE.md`.

## Team DNA — Include verbatim in EVERY agent prompt

```
## ZCP Team DNA — Mandatory Principles

You are working on the ZCP project (Zerops Control Plane — Go binary, MCP protocol).

### 1. Fix Upstream, Not Downstream
- Trace concerns to ROOT CAUSE. Don't flag symptoms.
- "Where should this be prevented so it never propagates?"
- If fixed at source, downstream consequences disappear — don't flag them separately.
- Prefer structural enforcement (types, validation, gates) over text instructions.

### 2. Verify, Don't Assume
- READ actual code/docs before claims. Never reason from what "should" exist.
- If you reference a file/function/API — verify it exists and works as described.
- Unverified claims: say "UNVERIFIED: [claim]" explicitly.
- Zerops is NOT Kubernetes. Don't map K8s concepts onto it.

### 3. Rational Pragmatism
- Flag REAL problems with realistic failure scenarios. No theoretical concerns.
- No over-engineering: no abstractions without 3+ uses. Simple > clever.
- No over-simplification: if complexity is genuinely needed, say so.
- Measure: does this reliably deliver a working result end-to-end?

### 4. Logical Consistency End-to-End
- If X is true, what else MUST be true? Follow the chain.
- Internal consistency: no contradictions between sections.
- External consistency: matches codebase, CLAUDE.md, test patterns.

### 5. Deep Reading, Not Scanning
- Read ENTIRE document/code. Understand logic, not just structure.
- Cite specific sections/lines. Vague findings are worthless.

### 6. Confidence & Honesty
- HIGH = verified. MEDIUM = logically sound, not fully verified. LOW = speculative.
- Never present speculation as fact.

### 7. ZCP-Specific
- Go, TDD mandatory, table-driven tests, max 350 lines/file
- Architecture: cmd → server → tools → ops → platform (strict)
- MCP = "dumb" — data quality + guardrails, not cleverness
- Extend existing mechanisms before creating new ones
- Simplest solution: plain functions > abstractions
```

---

## Phase 1: Analysis & Decomposition

Analyze the plan and decompose it into independent work units. For each work unit, determine:

- **Agent name**: `impl-{area}`, `fix-{id}`, or `test-{area}` (descriptive, kebab-case)
- **Scope**: which files/packages this agent owns
- **Dependencies**: which other work units must complete first (if any)
- **Test scope**: which test layers this agent must verify
- **Estimated complexity**: LOW / MEDIUM / HIGH

### ZCP Composition Heuristics

Use these to guide decomposition:
- State/lifecycle changes → `internal/workflow/engine.go`, `session.go`
- Content/guidance changes → `internal/content/bootstrap.md`, `deploy.md`
- Checker changes → `internal/workflow/workflow_checks_*.go`, `validate.go`
- Flow-specific changes → per-mode files
- Cross-cutting integration → `internal/ops/` ↔ `internal/tools/` ↔ `internal/platform/`

### Dependency Rules

- No circular dependencies between work units
- Test agents can depend on implementation agents
- Integration work depends on its component work units
- Prefer more independent units over fewer coupled ones

---

## Phase 2: Team Assembly — MUST GET USER CONFIRMATION

Present the proposed team to the user in this format:

```
## Proposed Execution Team

| # | Agent | Scope | Dependencies | Test Layers | Complexity |
|---|-------|-------|-------------|-------------|------------|
| 1 | impl-{x} | internal/{pkg}/... | none | unit, tool | MEDIUM |
| 2 | impl-{y} | internal/{pkg2}/... | none | unit | LOW |
| 3 | test-integration | integration/ | 1, 2 | integration | LOW |
| ... |

### Execution Waves
```
Wave 1 (parallel): {agents with no dependencies}
Wave 2 (depends on wave 1): {agents depending on wave 1}
Wave 3 (depends on wave 2): {if applicable}
```
**Critical path**: {longest dependency chain}

**Total agents**: N
**Parallel waves**: M
**Context available**: Yes/No ({basename}.context.md)

Proceed with this team? [Yes / Modify / Cancel]
```

**STOP AND WAIT for user confirmation.** Do not spawn agents until the user approves.

---

## Phase 3: Execution

After user approval:

1. Use `TeamCreate` with name `"execute-team"`.
2. Create tasks with `TaskCreate` for each work unit, setting dependencies.
3. Spawn agents in **isolated worktrees** (`isolation: "worktree"`), respecting dependency order.

### Agent Prompt Template

Each agent receives:

```
{Team DNA}

## Your Assignment: {agent name}

### Plan Section
{the specific section of the plan relevant to this agent's scope}

### Full Plan Reference
{complete plan — agents may need broader context}

### Review Context (Supporting Material)
{contents of .context.md if it exists}

This context contains:
- **Decision Log**: WHY decisions were made — follow these, don't re-debate them.
- **Rejected Alternatives**: approaches that were tried and discarded — do NOT re-propose these.
- **Resolved Concerns**: issues that were already fixed in the plan — do NOT re-flag these.
- **Open Questions**: unresolved items — flag if you encounter them during implementation.
- **Confidence Map**: areas marked LOW confidence need extra care and testing.

### CLAUDE.md Conventions
{relevant conventions from CLAUDE.md}

### Your Scope
- **Files**: {list of files this agent owns}
- **Packages**: {list of packages}
- **Test layers**: {unit/tool/integration/e2e}

### TDD Protocol — MANDATORY

1. **RED**: Write failing test(s) FIRST. Run them. They MUST fail.
   ```
   go test ./internal/{pkg}/... -run TestName -v -count=1
   ```
2. **GREEN**: Write minimal implementation to pass. Run tests. They MUST pass.
   ```
   go test ./internal/{pkg}/... -count=1 -v
   ```
3. **VERIFY**: Run full package tests.
   ```
   go test ./internal/{pkg}/... -count=1
   ```
4. **LINT**: Run linter.
   ```
   make lint-fast
   ```
5. **REPORT**: Send status via SendMessage to "lead":
   - Tests written: N
   - Tests passing: N
   - Files modified: list
   - Issues encountered: list (if any)
   - Status: COMPLETE / BLOCKED / NEEDS_REVIEW

### Rules
- Stay within your assigned scope. Do NOT modify files outside your scope.
- If you discover you need to change a file owned by another agent, send a message to "lead" describing the needed change.
- Follow TDD strictly — no implementation without a failing test first.
- Every file must be under 350 lines.
- Run `make lint-fast` before reporting completion.
```

### Lead Agent

One agent is designated as `lead` (typically the most complex or central work unit, or a dedicated coordination agent if needed). The lead:
- Receives status messages from all other agents
- Tracks completion
- Identifies conflicts between agents' changes
- Reports overall status to the orchestrator

---

## Phase 4: Collection & Merge — MUST GET USER CONFIRMATION

After all agents complete:

1. Collect all results and diffs from each agent.
2. Check for conflicts between agents' changes.
3. Present results to user:

```
## Execution Results

| Agent | Status | Tests | Files Changed | Issues |
|-------|--------|-------|---------------|--------|
| impl-{x} | COMPLETE | 5/5 pass | 3 files | none |
| impl-{y} | COMPLETE | 3/3 pass | 2 files | none |
| ... |

### Conflicts
{list any conflicting changes between agents, or "None detected"}

### Merge Order
1. {agent} — {reason for order}
2. {agent} — ...

### Verification Plan
- [ ] `go test ./... -count=1 -short` — all tests pass
- [ ] `make lint-fast` — no lint errors
- [ ] Manual review of {specific areas}

Proceed with merge? [Yes / Review first / Cancel]
```

**STOP AND WAIT for user confirmation before merging.**

After merge approval:
- Merge changes in the specified order
- Run `go test ./... -count=1 -short` to verify
- Run `make lint-fast` to verify
- Report final status

## Execution Report

Write `<dir>/<basename>.execution-report.md`:

```markdown
# Execution Report: {basename}
**Date**: {today}
**Plan**: {filepath}
**Context**: {basename}.context.md (Yes/No)

## Team

| Agent | Scope | Status | Tests |
|-------|-------|--------|-------|
| ... |

## Changes

### {Agent 1}
- {file}: {description of change}
- ...

### {Agent 2}
- ...

## Verification

- All tests: PASS / FAIL ({count})
- Lint: PASS / FAIL
- Conflicts resolved: {count}

## Notes
{any issues, deviations from plan, or items needing follow-up}
```

## Cleanup

Use `TeamDelete` to clean up `"execute-team"`.

Report final status to user with link to execution report.
