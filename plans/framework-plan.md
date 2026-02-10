# ZCP — Development Framework Setup

## Goal

Set up a complete, professional development framework for AI-driven Go development with Claude Code. This covers: hooks, permissions, settings, TDD workflow, CLAUDE.md, CI/CD, linting, and conventions.

Implementation details (Go packages, business logic) are **out of scope**.

### What is ZCP

Single Go binary merging two repos:
- **zaia** (`../zaia/`) — Go CLI (platform, auth, knowledge, business logic)
- **zaia-mcp** (`../zaia-mcp/`) — MCP server (tool handlers, executor)

Source repos remain as reference. Code will be ported into `zcp/internal/`.

---

## 1. Prerequisites

### Local tooling required

| Tool | Version | Install |
|------|---------|---------|
| Go | 1.24.x | `go install golang.org/dl/go1.24.4@latest && go1.24.4 download` |
| golangci-lint | v2.8.0 | `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0` |
| make | any | — |
| jq | any | `brew install jq` |

### Go version decision

Both source repos pin `go 1.24.0` in go.mod. Go 1.25 has a regression in cross-compilation for darwin Mach-O signatures.

**Decision**: Use `go 1.24.0` in go.mod. Local Go 1.25.x can compile 1.24 modules, but CI must use `go-version-file: "go.mod"` to match. Cross-compilation happens in CI (Ubuntu) where this is tested.

### `tools/install.sh` — CI tooling setup

Installs golangci-lint into project-local `bin/`:

```bash
#!/bin/bash
cd "$(dirname "$0")"
set -e
cd ..
export GOBIN=$PWD/bin
export PATH="${GOBIN}:${PATH}"
[[ ! -d "${GOBIN}" ]] && mkdir -p "${GOBIN}"
echo "GOBIN=${GOBIN}"
rm -rf tmp
GOBIN="$GOBIN" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0
```

---

## 2. Claude Code Settings (`.claude/settings.json`)

### Key facts (from official docs)

- **Exit 0** = success (JSON output parsed from stdout)
- **Exit 2** = blocking error (stderr fed to Claude, stdout ignored)
- **Exit 1 / other** = non-blocking warning (stderr shown in verbose mode only, **NOT fed to Claude**)
- **Hook matcher** = regex (e.g., `Edit|Write` matches either tool)
- **Permission syntax**: `Bash(go test *)` — **space separator, NOT colon** (`:*` is deprecated)
- **Permission argument** = glob pattern matched against tool arguments
- **`*` matches within a path segment, `**` matches across path separators** — use `**/.env*` not `.env*`
- **Settings merge**: hooks from all sources (user, project, local) run in parallel; deny > allow for permissions
- **Hooks snapshotted at startup** — mid-session changes require `/hooks` review
- **`async: true`** — hook runs in background, results delivered on next turn; cannot block
- **`type: "prompt"`** — LLM-judged hook (Haiku by default); returns `{ok: true/false, reason: ...}`
- **`type: "agent"`** — spawns subagent with tool access; returns `{ok: true/false, reason: ...}`
- **Stop hooks** — must check `stop_hook_active` to prevent infinite loops
- **JSON vs exit 2** — never mix; exit 2 ignores stdout JSON
- **Async PostToolUse** — `decision: "block"` is advisory feedback only (edit already applied)

### Exact `settings.json`

```json
{
  "permissions": {
    "allow": [
      "Bash(go test *)",
      "Bash(go build *)",
      "Bash(go vet *)",
      "Bash(go mod *)",
      "Bash(go run *)",
      "Bash(golangci-lint *)",
      "Bash(make *)",
      "Bash(git status)",
      "Bash(git status *)",
      "Bash(git diff *)",
      "Bash(git log *)",
      "Bash(git branch *)",
      "Bash(git show *)",
      "Bash(ls *)"
    ],
    "deny": [
      "Read(**/.env*)",
      "Edit(**/.env*)",
      "Write(**/.env*)",
      "Read(**/secrets/**)",
      "Read(**/*credential*)",
      "Read(**/*token*)"
    ]
  },
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/session-start.sh",
            "timeout": 10
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/pre-bash.sh",
            "timeout": 120
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          {
            "type": "command",
            "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/post-edit.sh",
            "timeout": 120,
            "async": true
          },
          {
            "type": "command",
            "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/check-claude-md.sh",
            "timeout": 5,
            "async": true
          }
        ]
      }
    ],
    "PostToolUseFailure": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/on-failure.sh",
            "timeout": 5
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/stop.sh",
            "timeout": 90
          }
        ]
      }
    ],
    "TaskCompleted": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/task-completed.sh",
            "timeout": 120
          }
        ]
      }
    ],
    "PreCompact": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/pre-compact.sh",
            "timeout": 10
          }
        ]
      }
    ],
    "SubagentStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo '{\"additionalContext\": \"Go 1.24.0 project. Module: github.com/zeropsio/zcp. Use -short flag for go test. golangci-lint v2.8.0 with --fast-only for quick lint.\"}'",
            "timeout": 5
          }
        ]
      }
    ]
  }
}
```

### Settings scope

| File | Purpose | Git |
|------|---------|-----|
| `.claude/settings.json` | Shared team config (hooks + permissions) | Checked in |
| `.claude/settings.local.json` | Personal overrides | In .gitignore |

---

## 3. Hook Scripts

### Design principles

1. **Use `jq` for JSON parsing** — grep/sed is fragile; official docs use `jq`
2. **Use `$CLAUDE_PROJECT_DIR`** — portable, no `dirname` hacks
3. **Exit 0** for informational feedback (PostToolUse hooks)
4. **Exit 2 + stderr** for blocking (PreToolUse security/lint gates)
5. **JSON output on stdout** for structured feedback to Claude
6. **Async PostToolUse hooks** — never block edits, provide feedback on next turn
7. **Async feedback is advisory** — `"decision": "block"` tells Claude to prioritize fixing, but cannot undo the edit
8. **Stop hooks check `stop_hook_active` first** — prevents infinite loops (must be the FIRST check)
9. **Cache test results** — `post-edit.sh` and `stop.sh` write results to `.claude/last-test-result` for `session-start.sh` to read
10. **Hook timeout > inner command timeout** — always leave headroom (e.g., 90s hook for 60s test)

### Tiered verification pyramid

Core architecture for quality gates — each tier runs at a different point:

```
┌─────────────────────────────────────────────────┐
│  Tier 4: CI                                      │
│  go test -race ./... + cross-platform build      │
│  + multi-GOOS lint                               │
│  Trigger: push/PR                                │
├─────────────────────────────────────────────────┤
│  Tier 3: Pre-commit + pre-push gate             │
│  golangci-lint run ./... (blocking, exit 2)      │
│  go test -short ./... before push (blocking)     │
│  Trigger: git commit / git push                  │
├─────────────────────────────────────────────────┤
│  Tier 2: Stop hook + TaskCompleted               │
│  go test -short ./... + go vet ./...             │
│  Trigger: Claude finishes responding / task done │
├─────────────────────────────────────────────────┤
│  Tier 1: PostToolUse (async)                     │
│  go test -short ./<pkg> + vet + fast lint        │
│  Incremental: targeted -run for test files       │
│  Trigger: every .go file edit                    │
└─────────────────────────────────────────────────┘
```

Tier 1 is async (Claude keeps working). Tier 2 blocks Claude from stopping or completing tasks. Tier 3 blocks commits and pushes. Tier 4 runs in CI. Each tier is progressively broader and slower.

### 3.1 `session-start.sh` — Project context injection

**Trigger**: SessionStart (every session start/resume)
**Purpose**: Inject project state so Claude starts with awareness. Reads cached test results instead of running tests (<1s vs 30s+).

```bash
#!/bin/bash
# SessionStart hook: inject project context
# Always exit 0 — provides additionalContext via JSON
# Does NOT run tests — reads cached results from last post-edit/stop hook

cd "$CLAUDE_PROJECT_DIR" 2>/dev/null || exit 0

# Skip if no Go files yet (empty project)
[ -f "go.mod" ] || exit 0

CONTEXT=""

# Current branch
BRANCH=$(git branch --show-current 2>/dev/null)
[ -n "$BRANCH" ] && CONTEXT+="Branch: ${BRANCH}. "

# Recent commits (one-line)
RECENT=$(git log --oneline -3 2>/dev/null | tr '\n' ' ')
[ -n "$RECENT" ] && CONTEXT+="Recent: ${RECENT}. "

# Cached test results (written by post-edit.sh and stop.sh)
CACHED="$CLAUDE_PROJECT_DIR/.claude/last-test-result"
if [ -f "$CACHED" ]; then
    AGE=$(( $(date +%s) - $(stat -f%m "$CACHED" 2>/dev/null || stat -c%Y "$CACHED" 2>/dev/null || echo 0) ))
    if [ "$AGE" -lt 3600 ]; then
        FAIL_LINES=$(grep -E 'FAIL' "$CACHED" 2>/dev/null | head -3 | tr '\n' ' ')
        if [ -n "$FAIL_LINES" ]; then
            CONTEXT+="FAILING (cached ${AGE}s ago): ${FAIL_LINES}. "
        else
            CONTEXT+="Tests passing (cached ${AGE}s ago). "
        fi
    else
        CONTEXT+="Test cache stale (${AGE}s). Run 'go test -short ./...' to refresh. "
    fi
fi

# Uncommitted changes summary
DIRTY=$(git diff --stat HEAD 2>/dev/null | tail -1)
[ -n "$DIRTY" ] && CONTEXT+="Uncommitted: ${DIRTY}. "

if [ -n "$CONTEXT" ]; then
    jq -n --arg ctx "$CONTEXT" '{ additionalContext: $ctx }'
fi

exit 0
```

### 3.2 `pre-bash.sh` — Security gate + pre-commit lint + pre-push tests

**Trigger**: PreToolUse on every Bash command
**Purpose**: Block destructive operations, run lint before commits, run tests before pushes

```bash
#!/bin/bash
# PreToolUse hook: Security gate + pre-commit lint gate + pre-push test gate
# Exit 0 = allow, Exit 2 = block (stderr fed to Claude)

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')
[ -z "$COMMAND" ] && exit 0

# ============================================================
# SECURITY GATE — block destructive operations
# ============================================================

# Block rm -rf / rm -r with broad or relative dangerous targets
# Catches: rm -rf /, rm -rf ., rm -rf .., rm -rf ~, rm -rf *, rm -r /foo && ..., etc.
if echo "$COMMAND" | grep -qE 'rm\s+(-[a-zA-Z]*r[a-zA-Z]*\s+)+(\.\.|\/|\.\s|~|\$HOME|\$\{HOME\}|\*)'; then
    echo "BLOCKED: Destructive rm command. Specify exact paths instead." >&2
    exit 2
fi

# Block dangerous git operations
# force push, hard reset, clean -f, checkout ., restore ., stash drop
if echo "$COMMAND" | grep -qE 'git\s+(push\s+.*--force|push\s+-f\b|reset\s+--hard|clean\s+-[a-zA-Z]*f|checkout\s+(--\s+)?\.(\s|$)|restore\s+\.(\s|$)|stash\s+drop)'; then
    echo "BLOCKED: Dangerous git operation. Use safer alternatives." >&2
    exit 2
fi

# Block chmod 777 and other overly permissive operations
if echo "$COMMAND" | grep -qE 'chmod\s+777'; then
    echo "BLOCKED: chmod 777 is too permissive. Use specific permissions." >&2
    exit 2
fi

# Block pipe-to-shell patterns (curl|bash, wget|sh, etc.)
if echo "$COMMAND" | grep -qE '\|\s*(ba)?sh\b|\|\s*sudo'; then
    echo "BLOCKED: Piping to shell is dangerous. Download first, inspect, then execute." >&2
    exit 2
fi

# Block dd to disk devices
if echo "$COMMAND" | grep -qE 'dd\s+.*of=/dev/'; then
    echo "BLOCKED: Direct disk write with dd." >&2
    exit 2
fi

# ============================================================
# PRE-PUSH TEST GATE — block push if tests fail
# ============================================================

if echo "$COMMAND" | grep -qE '^git\s+push'; then
    MODULE_ROOT="$CLAUDE_PROJECT_DIR"
    [ -z "$MODULE_ROOT" ] && MODULE_ROOT=$(cd "$(dirname "$0")/../.." && pwd)
    cd "$MODULE_ROOT" || exit 0
    [ -f "go.mod" ] || exit 0

    TEST_OUTPUT=$(go test ./... -count=1 -short -timeout=60s 2>&1)
    if echo "$TEST_OUTPUT" | grep -qE 'FAIL'; then
        echo "BLOCKED: Tests failing. Fix before pushing." >&2
        echo "$TEST_OUTPUT" | grep -E 'FAIL|---' | tail -5 >&2
        exit 2
    fi

    exit 0
fi

# ============================================================
# PRE-COMMIT LINT GATE — block commit if lint fails
# ============================================================

echo "$COMMAND" | grep -qE '^git\s+commit' || exit 0

MODULE_ROOT="$CLAUDE_PROJECT_DIR"
[ -z "$MODULE_ROOT" ] && MODULE_ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$MODULE_ROOT" || exit 0

STAGED=$(git diff --cached --name-only 2>/dev/null)
[ -z "$STAGED" ] && exit 0

# Check if CLAUDE.md should be staged
KEY_PATTERNS='internal/.*/[a-z]+\.go$|go\.mod$|cmd/'
KEY_STAGED=$(echo "$STAGED" | grep -E "$KEY_PATTERNS" | head -5)
CLAUDE_MD_STAGED=$(echo "$STAGED" | grep -E 'CLAUDE\.md$')

if [ -n "$KEY_STAGED" ] && [ -z "$CLAUDE_MD_STAGED" ]; then
    echo "CLAUDE.md CHECK: Key files staged but CLAUDE.md not included. Consider: git add CLAUDE.md"
fi

# Lint gate — exit 2 to actually block the commit
GO_STAGED=$(echo "$STAGED" | grep -E '\.go$' | head -1)
if [ -n "$GO_STAGED" ]; then
    if command -v golangci-lint &>/dev/null; then
        echo "-- pre-commit: golangci-lint run ./... --"
        LINT_OUTPUT=$(golangci-lint run ./... --timeout=60s 2>&1)
        LINT_EXIT=$?
        if [ $LINT_EXIT -ne 0 ]; then
            echo "$LINT_OUTPUT" | tail -30 >&2
            echo "LINT FAILED: fix issues before committing" >&2
            exit 2
        else
            echo "-- LINT PASSED --"
        fi
    fi
fi

exit 0
```

### 3.3 `post-edit.sh` — Async test + vet + lint after Go edits (incremental)

**Trigger**: PostToolUse on Edit/Write (runs **async** — does not block Claude)
**Purpose**: Fast feedback loop — test the changed package, report via structured JSON. When editing `_test.go` files, uses targeted `-run` for faster feedback.

```bash
#!/bin/bash
# PostToolUse hook (ASYNC): Auto-run tests + vet + fast lint after Go file edits
# Runs in background — results delivered to Claude on next turn
# NOTE: "decision": "block" is ADVISORY — the edit is already applied.
# It tells Claude to prioritize fixing the issue, but cannot undo the edit.

INPUT=$(cat)
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty')
[ -z "$FILE_PATH" ] && exit 0

# Only for Go files
echo "$FILE_PATH" | grep -qE '\.go$' || exit 0

# Skip generated, test data, embedded files
echo "$FILE_PATH" | grep -qE '(testdata/|embed/|\.pb\.go$|_generated\.go$)' && exit 0

MODULE_ROOT="$CLAUDE_PROJECT_DIR"
[ -z "$MODULE_ROOT" ] && MODULE_ROOT=$(cd "$(dirname "$0")/../.." && pwd)

# Only run for files in this project
echo "$FILE_PATH" | grep -q "$MODULE_ROOT" || exit 0

cd "$MODULE_ROOT" || exit 0

# Determine package from file path
PKG_DIR=$(echo "$FILE_PATH" | sed "s|^${MODULE_ROOT}/||" | xargs dirname)

PROBLEMS=""
CACHE_FILE="$CLAUDE_PROJECT_DIR/.claude/last-test-result"

if [ -d "$PKG_DIR" ]; then
    # Incremental test: if editing a test file, target specific test function
    RUN_FLAG=""
    if echo "$FILE_PATH" | grep -qE '_test\.go$'; then
        # Extract test function names from the edited file, use the last one
        LAST_FUNC=$(grep -oE 'func (Test[A-Za-z0-9_]+)' "$FILE_PATH" 2>/dev/null | tail -1 | awk '{print $2}')
        if [ -n "$LAST_FUNC" ]; then
            RUN_FLAG="-run ${LAST_FUNC}"
        fi
    fi

    # Run tests (short mode for speed, with optional -run targeting)
    if [ -n "$RUN_FLAG" ]; then
        TEST_OUTPUT=$(go test "./${PKG_DIR}" $RUN_FLAG -count=1 -short -timeout=15s 2>&1)
    else
        TEST_OUTPUT=$(go test "./${PKG_DIR}" -count=1 -short -timeout=30s 2>&1)
    fi

    # Cache test results for session-start.sh
    echo "$TEST_OUTPUT" > "$CACHE_FILE" 2>/dev/null

    if echo "$TEST_OUTPUT" | grep -qE 'FAIL'; then
        FAIL_LINES=$(echo "$TEST_OUTPUT" | grep -E 'FAIL|---' | tail -10)
        PROBLEMS+="Tests FAILED in ./${PKG_DIR}:\n${FAIL_LINES}\n"
    fi

    # Run vet
    VET_OUTPUT=$(go vet "./${PKG_DIR}" 2>&1)
    if [ -n "$VET_OUTPUT" ]; then
        PROBLEMS+="Vet issues in ./${PKG_DIR}:\n${VET_OUTPUT}\n"
    fi

    # Fast lint (non-blocking)
    if command -v golangci-lint &>/dev/null; then
        LINT_OUTPUT=$(golangci-lint run "./${PKG_DIR}" --fast-only --timeout=30s 2>&1)
        LINT_EXIT=$?
        if [ $LINT_EXIT -ne 0 ] && [ -n "$LINT_OUTPUT" ]; then
            LINT_LINES=$(echo "$LINT_OUTPUT" | tail -10)
            PROBLEMS+="Lint warnings in ./${PKG_DIR}:\n${LINT_LINES}\n"
        fi
    fi
fi

# Structured JSON feedback
if [ -n "$PROBLEMS" ]; then
    # Escape for JSON
    ESCAPED=$(echo -e "$PROBLEMS" | jq -Rs '.')
    # NOTE: "decision": "block" is advisory — tells Claude to prioritize fixing,
    # but cannot undo the edit (it's already applied in async context)
    echo "{\"decision\": \"block\", \"reason\": ${ESCAPED}}"
else
    echo "{\"additionalContext\": \"Tests, vet, and lint passed for ./${PKG_DIR}\"}"
fi

exit 0
```

### 3.4 `check-claude-md.sh` — CLAUDE.md sync reminder (async)

**Trigger**: PostToolUse on Edit/Write (runs **async**)
**Purpose**: Remind to update CLAUDE.md when architectural files change

```bash
#!/bin/bash
# PostToolUse hook (ASYNC): Remind to check CLAUDE.md when key files change
# Always exit 0 — informational only

INPUT=$(cat)
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty')
[ -z "$FILE_PATH" ] && exit 0

# Unified key file patterns from both source repos (zaia + zaia-mcp)
if echo "$FILE_PATH" | grep -qE '(go\.mod$|cmd/.+\.go$|internal/platform/client\.go|internal/server/server\.go|internal/tools/.+\.go$|internal/auth/manager\.go|internal/knowledge/engine\.go)'; then
    jq -n --arg file "$(basename "$FILE_PATH")" \
      '{ additionalContext: ("Key file changed: " + $file + ". Check if CLAUDE.md Architecture table needs updating.") }'
fi

exit 0
```

### 3.5 `task-completed.sh` — Quality gate before task completion

**Trigger**: TaskCompleted (when TaskUpdate sets status to "completed")
**Purpose**: Block task completion if tests are failing

```bash
#!/bin/bash
# TaskCompleted hook: verify tests + vet pass before task completion
# Exit 0 = allow completion, Exit 2 = block (stderr fed to Claude)

cd "$CLAUDE_PROJECT_DIR" 2>/dev/null || exit 0

# Skip if no Go files
[ -f "go.mod" ] || exit 0

CACHE_FILE="$CLAUDE_PROJECT_DIR/.claude/last-test-result"

# Run tests
TEST_OUTPUT=$(go test ./... -count=1 -short -timeout=60s 2>&1)

# Cache results
echo "$TEST_OUTPUT" > "$CACHE_FILE" 2>/dev/null

if echo "$TEST_OUTPUT" | grep -qE 'FAIL'; then
    echo "Cannot complete task: tests are failing" >&2
    echo "$TEST_OUTPUT" | grep -E 'FAIL|---' | tail -10 >&2
    exit 2
fi

# Run vet
VET_OUTPUT=$(go vet ./... 2>&1)
if [ -n "$VET_OUTPUT" ]; then
    echo "Cannot complete task: go vet reports issues" >&2
    echo "$VET_OUTPUT" | tail -5 >&2
    exit 2
fi

exit 0
```

### 3.6 `pre-compact.sh` — Save state before context compaction

**Trigger**: PreCompact (before auto or manual context compaction)
**Purpose**: Preserve project state that would otherwise be lost during compaction. Does NOT run tests (hook timeout too short for that); reads cached results instead.

```bash
#!/bin/bash
# PreCompact hook: save project state before context compaction
# Cannot block compaction — only saves state for recovery
# Does NOT run tests (10s timeout too short); reads cached results instead

cd "$CLAUDE_PROJECT_DIR" 2>/dev/null || exit 0
[ -f "go.mod" ] || exit 0

STATE_FILE="$CLAUDE_PROJECT_DIR/.claude/compact-state.md"
CACHE_FILE="$CLAUDE_PROJECT_DIR/.claude/last-test-result"

{
    echo "# Pre-compaction state ($(date -u +%Y-%m-%dT%H:%M:%SZ))"
    echo ""
    echo "## Branch"
    git branch --show-current 2>/dev/null
    echo ""
    echo "## Uncommitted changes"
    git diff --stat HEAD 2>/dev/null | tail -10
    echo ""
    echo "## Test status (cached)"
    if [ -f "$CACHE_FILE" ]; then
        FAIL=$(grep -E 'FAIL|---' "$CACHE_FILE" 2>/dev/null | head -10)
        if [ -n "$FAIL" ]; then
            echo "$FAIL"
        else
            echo "All tests passing (from cache)."
        fi
    else
        echo "No cached test results. Run 'go test -short ./...' after compaction."
    fi
} > "$STATE_FILE" 2>/dev/null

jq -n '{ additionalContext: "State saved to .claude/compact-state.md. Read it after compaction to restore context about current work." }'

exit 0
```

### 3.7 `on-failure.sh` — Common failure pattern detection + memory recording

**Trigger**: PostToolUseFailure on Bash commands
**Purpose**: Detect common Go failure patterns, suggest fixes, auto-record recurring errors to memory for cross-session persistence

```bash
#!/bin/bash
# PostToolUseFailure hook: detect common failure patterns and suggest fixes
# Auto-records to memory/errors.md when a pattern recurs
# Always exit 0 — provides additionalContext

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')
STDERR=$(echo "$INPUT" | jq -r '.tool_response.stderr // empty')

[ -z "$STDERR" ] && exit 0

SUGGESTION=""
PATTERN=""

# Package not found
if echo "$STDERR" | grep -qE 'no required module provides package|cannot find package'; then
    SUGGESTION="Run 'go mod tidy' to resolve missing packages."
    PATTERN="missing-package"
fi

# Build constraint / tag issue
if echo "$STDERR" | grep -qE 'build constraints exclude|no Go files'; then
    SUGGESTION="Check build tags. E2E tests need '-tags e2e'. Some files may have //go:build constraints."
    PATTERN="build-tags"
fi

# Permission denied
if echo "$STDERR" | grep -qE 'permission denied'; then
    SUGGESTION="Check file permissions. Hook scripts need 'chmod +x'."
    PATTERN="permission-denied"
fi

# golangci-lint not found
if echo "$STDERR" | grep -qE 'golangci-lint.*not found|command not found.*golangci'; then
    SUGGESTION="Install golangci-lint: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0"
    PATTERN="lint-not-found"
fi

# Import cycle
if echo "$STDERR" | grep -qE 'import cycle not allowed'; then
    SUGGESTION="Import cycle detected. Move shared types to a separate package or use interfaces."
    PATTERN="import-cycle"
fi

# Undefined / undeclared
if echo "$STDERR" | grep -qE 'undefined:|undeclared name'; then
    SUGGESTION="Undefined symbol. Check spelling, imports, and whether the type/function is exported."
    PATTERN="undefined"
fi

if [ -n "$SUGGESTION" ]; then
    jq -n --arg s "$SUGGESTION" '{ additionalContext: $s }'

    # Auto-record recurring patterns to memory (if memory dir exists)
    MEMORY_DIR="$HOME/.claude/projects/-Users-macbook-Documents-Zerops-MCP-zcp/memory"
    if [ -d "$MEMORY_DIR" ] && [ -n "$PATTERN" ]; then
        ERRORS_FILE="$MEMORY_DIR/errors.md"
        # Only append if this pattern isn't already recorded
        if ! grep -q "$PATTERN" "$ERRORS_FILE" 2>/dev/null; then
            echo "" >> "$ERRORS_FILE"
            echo "## $PATTERN ($(date -u +%Y-%m-%d))" >> "$ERRORS_FILE"
            echo "- Command: \`$(echo "$COMMAND" | head -c 100)\`" >> "$ERRORS_FILE"
            echo "- Fix: $SUGGESTION" >> "$ERRORS_FILE"
        fi
    fi
fi

exit 0
```

### 3.8 `stop.sh` — Test gate before Claude finishes (command-based)

**Trigger**: Stop (when Claude finishes responding)
**Purpose**: Verify tests pass before Claude can stop. Uses `command` type (not `agent`) — sufficient for running tests and 10x cheaper.

**Critical**: Must check `stop_hook_active` FIRST to prevent infinite loops (Stop -> test -> fail -> Claude responds -> Stop -> ...).

**Performance**: Only runs tests if `.go` files were modified since last cached test run. If no `.go` files changed (e.g., Claude answered a question), skips tests entirely (~0s instead of 60s).

```bash
#!/bin/bash
# Stop hook: verify tests + vet pass before Claude finishes responding
# Exit 0 with JSON {ok: true/false} — Stop hooks use ok/reason protocol
# CRITICAL: Must check stop_hook_active FIRST to prevent infinite loops
# PERF: Skips tests if no .go files changed since last test run

INPUT=$(cat)

# ============================================================
# INFINITE LOOP GUARD — must be the FIRST check
# When stop_hook_active is true, another stop hook is already running.
# We MUST return ok:true immediately to break the loop.
# ============================================================
ACTIVE=$(echo "$INPUT" | jq -r '.stop_hook_active // false')
if [ "$ACTIVE" = "true" ]; then
    echo '{"ok": true}'
    exit 0
fi

cd "$CLAUDE_PROJECT_DIR" 2>/dev/null || { echo '{"ok": true}'; exit 0; }

# Skip if no Go files
[ -f "go.mod" ] || { echo '{"ok": true}'; exit 0; }

CACHE_FILE="$CLAUDE_PROJECT_DIR/.claude/last-test-result"

# ============================================================
# SKIP IF NO .go FILES CHANGED SINCE LAST TEST RUN
# Avoids 60s test suite on every non-code response
# ============================================================
if [ -f "$CACHE_FILE" ]; then
    CACHE_MTIME=$(stat -f%m "$CACHE_FILE" 2>/dev/null || stat -c%Y "$CACHE_FILE" 2>/dev/null || echo 0)
    # Find newest .go file modified after the cache
    NEWER_GO=$(find . -name '*.go' -not -path './vendor/*' -newer "$CACHE_FILE" -print -quit 2>/dev/null)
    if [ -z "$NEWER_GO" ]; then
        # No .go files changed — trust cached results
        if grep -qE 'FAIL' "$CACHE_FILE" 2>/dev/null; then
            FAIL_LINES=$(grep -E 'FAIL|---' "$CACHE_FILE" | tail -5 | tr '\n' ' ')
            jq -n --arg reason "Tests failing (cached): ${FAIL_LINES}" '{"ok": false, "reason": $reason}'
        else
            echo '{"ok": true}'
        fi
        exit 0
    fi
fi

PROBLEMS=""

# Run tests (timeout must be LESS than hook timeout: 60s test < 90s hook)
TEST_OUTPUT=$(go test ./... -count=1 -short -timeout=60s 2>&1)

# Cache results for session-start.sh
echo "$TEST_OUTPUT" > "$CACHE_FILE" 2>/dev/null

if echo "$TEST_OUTPUT" | grep -qE 'FAIL'; then
    FAIL_LINES=$(echo "$TEST_OUTPUT" | grep -E 'FAIL|---' | tail -5 | tr '\n' ' ')
    PROBLEMS+="Tests failing: ${FAIL_LINES} "
fi

# Run vet
VET_OUTPUT=$(go vet ./... 2>&1)
if [ -n "$VET_OUTPUT" ]; then
    VET_LINES=$(echo "$VET_OUTPUT" | tail -3 | tr '\n' ' ')
    PROBLEMS+="Vet issues: ${VET_LINES} "
fi

if [ -n "$PROBLEMS" ]; then
    jq -n --arg reason "$PROBLEMS" '{"ok": false, "reason": $reason}'
else
    echo '{"ok": true}'
fi

exit 0
```

---

## 4. CLAUDE.md — Project Constitution

### Principles

- **Max ~150 lines** — every line competes for LLM attention (research shows LLMs follow ~150-200 instructions with consistency)
- **Only actionable rules** — no generic "write clean code"
- **Code is authoritative** — CLAUDE.md references, doesn't copy
- **No style rules** — enforced by golangci-lint + gofmt
- **English only** — per CLAUDE.local.md
- **Specific > generic** — concrete examples, not abstract guidance
- **Progressive disclosure** — detailed knowledge in `.claude/skills/` (plain markdown files read with Read tool when needed), not here

### What does NOT belong in CLAUDE.md

- Hook descriptions (live in settings.json)
- Implementation status (code is source of truth)
- Generic best practices (enforced by linter/hooks)
- Verbose architecture trees (use compact tables)
- Style rules (golangci-lint handles this)
- Detailed API reference (goes in `.claude/skills/zerops-api.md`)

### Required sections (in order)

1. One-liner description
2. Source of truth hierarchy
3. Architecture (pipeline + packages table)
4. TDD rules + seed test patterns
5. Testing layers
6. Commands
7. Conventions
8. Do NOT (anti-patterns)
9. Maintenance table

### Full CLAUDE.md content

```markdown
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

Design docs: `design/<feature>.md` — read before writing tests for any feature.
Plans: `plans/<feature>-plan.md` — read to find current implementation unit.

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
| `internal/tools` | MCP tool handlers (11 tools) | `discover.go`, `manage.go`, ... |
| `internal/ops` | Business logic, validation | `discover.go`, `manage.go`, ... |
| `internal/platform` | Zerops API client, types, errors | `client.go`, `errors.go` |
| `internal/auth` | Login, token, project discovery | `manager.go`, `storage.go` |
| `internal/knowledge` | BM25 search engine, embedded docs | `engine.go` |

Error codes: see `internal/platform/errors.go` for all codes (AUTH_REQUIRED, SERVICE_NOT_FOUND, etc.)

---

## TDD — Mandatory

1. **RED**: Write failing test BEFORE implementation
2. **GREEN**: Minimal code to pass
3. **REFACTOR**: Clean up, tests stay green

### Seed test pattern

When starting a new package, write ONE complete seed test first. This establishes naming, assertion style, table-driven structure, and helper patterns. Then follow the seed for all subsequent tests.

**Parallel-safe packages** (no global state):
```go
func TestDiscover_WithService_Success(t *testing.T) {
    t.Parallel()
    tests := []struct { ... }{ ... }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            // ...
        })
    }
}
```

**Packages with global state** (e.g., `output.SetWriter` — inherits from zaia):
```go
// NOTE: output.SetWriter is global — DO NOT use t.Parallel()
func TestFormat_JSONOutput_Success(t *testing.T) {
    tests := []struct { ... }{ ... }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // NO t.Parallel() — SetWriter is not thread-safe
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

---

## Commands

```bash
go test ./internal/<pkg> -run TestName -v   # Single test
go test ./... -count=1 -short               # All tests (fast)
go test ./... -count=1                      # All tests (full)
go test -race ./... -count=1                # With race detection
go build -o bin/zcp ./cmd/zcp              # Build
go vet ./...                                # Vet
make setup                                  # Bootstrap dev environment
make lint-fast                              # Fast lint (~3s)
make lint-local                             # Full lint (~15s)
make lint                                   # CI lint (2 platforms)
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
- Use `interface{}` / `any` when a concrete type is known
- Use `panic()` in library code — return errors
- Skip error checks — `errcheck` linter enforces this
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
```

---

## 4b. Document Architecture — Design Docs & Plans

### Truth Hierarchy

```
1. Tests (table-driven, executable)     ← AUTHORITATIVE for behavior
2. Code (Go types, interfaces)          ← AUTHORITATIVE for implementation
3. Design docs (design/*.md)            ← AUTHORITATIVE for intent & invariants
4. Plans (plans/*.md)                   ← TRANSIENT (implementation roadmap)
5. CLAUDE.md                            ← OPERATIONAL (workflow, conventions)
```

**Conflict resolution**: higher layer wins — but conflict signals a needed update to the lower layer. If tests pass but design doc says different behavior, the tests are authoritative and the design doc needs updating (or a bug needs filing).

### Design Documents — `design/<feature>.md`

Single source of truth for **concepts, behavioral contracts, and invariants**. Never for implementation details.

**Principles:**
- Written in **MUST/MUST NOT/WHEN-THEN** language — directly translatable to test assertions
- **No code, no types, no package names** — those belong in code
- **Stable**: changes only when the concept changes, not when implementation details change
- **Short**: under 100 lines — agent loads this into context as specification
- **Open Questions** section = explicit blocker — agent MUST NOT proceed past unresolved questions

**Template:**

```markdown
# Feature: <name>

## Purpose
One paragraph: what this does and why it exists.

## Behavioral Contract
- MUST: <invariant — becomes a test assertion>
- MUST: <invariant>
- MUST NOT: <anti-behavior — becomes a negative test>
- WHEN <condition> THEN <expected behavior — becomes a test case>
- WHEN <error condition> THEN <error behavior — becomes an error test>

## Interfaces (conceptual)
- Input: what the feature receives (not Go types — concepts)
- Output: what the feature produces
- Errors: what can go wrong and how it's signaled

## Constraints
- Performance: <if relevant>
- Security: <if relevant>
- Compatibility: <if relevant>

## Open Questions
- <unresolved question — blocks implementation>
```

**What does NOT go here:**
- Go types, struct definitions, function signatures (that's code)
- Package organization decisions (that's plans or code)
- Response JSON format details (that's code)

### Plans — `plans/<feature>-plan.md`

Transient documents that decompose a large feature into TDD-sized units. They have an explicit lifecycle and expire after completion.

**Lifecycle:**

```
DRAFT → (human reviews) → APPROVED → IN_PROGRESS → COMPLETED → ARCHIVED
                                          │
                                     units go through:
                                     PENDING → RED → GREEN → DONE
```

**Template:**

```markdown
# Plan: <feature name>

Status: DRAFT | APPROVED | IN_PROGRESS | COMPLETED | ARCHIVED
Design ref: design/<feature>.md
Created: <date>

## Scope
What this plan covers and what it doesn't.

## Units (implementation order)

### Unit 1: <name>
- Design ref: design/<feature>.md § Behavioral Contract, items 1-3
- Test cases:
  - <scenario> → <expected>
  - <scenario> → <expected>
- Dependencies: none
- Status: PENDING | RED | GREEN | DONE

### Unit 2: <name>
- Design ref: design/<feature>.md § Behavioral Contract, items 4-5
- Test cases:
  - <scenario> → <expected>
- Dependencies: Unit 1
- Status: PENDING

## Decisions Made During Implementation
- <decision>: <rationale> (<date>)
```

**Principles:**
- **Units are TDD-sized** — each unit = one RED-GREEN-REFACTOR cycle (typically 1 session)
- **Test cases sketched in natural language** — agent translates to Go table-driven tests
- **References design docs, never restates them** — plan links to design doc sections, doesn't copy
- **Decisions section captures drift** — if implementation diverges from original plan, record why
- **COMPLETED plans → `plans/archive/`** — never leave stale plans as "current"

### Traceability

```
design/discover.md                    plans/discover-plan.md
  § Behavioral Contract          →      § Unit 1: test cases
  - MUST return all services     →      - TestDiscover_AllServices_Success
  - MUST filter by hostname      →      - TestDiscover_WithFilter_Success
  - MUST return AUTH_REQUIRED    →      - TestDiscover_NoAuth_ReturnsError
        │                                       │
        │                               internal/ops/discover_test.go
        │                                 // Tests for: design/discover.md § Behavioral Contract
        │                                       │
        │                               internal/ops/discover.go
        │                                       │
        └───────────── verify ──────────────────┘
           "does the code still satisfy the design contract?"
```

Test file headers MUST reference their design doc:
```go
// Tests for: design/discover.md § Behavioral Contract
// - MUST return all services in project
// - MUST filter by hostname when specified
```

### Anti-patterns

| Anti-pattern | Problem | Fix |
|---|---|---|
| Design doc contains code | Duplicates truth, goes stale | Use MUST/WHEN-THEN language only |
| Plan restates design contracts | Duplication, inconsistency | Plan references design doc sections |
| No plan for large features | Agent lacks scope, implements everything at once | Break into TDD-sized units |
| Plan never archived | Stale plans confuse agent about current state | COMPLETED → archive/ immediately |
| Tests don't reference design doc | No traceability from spec to test | Add header comment |
| Design doc updated for every impl change | Becomes implementation log | Only update for concept changes |

---

## 5. TDD Workflow

### Mandatory process

1. **RED**: Write failing test BEFORE implementation
2. **GREEN**: Minimal implementation to pass
3. **REFACTOR**: Clean up, tests stay green

Enforced by: Stop hook (command-based test gate) + TaskCompleted hook (blocks completion) + CLAUDE.md rules.

### Document-aware workflow

For any feature with a design doc, the agent follows this per-unit workflow:

1. **Read design doc** → understand behavioral contract (WHAT)
2. **Read plan** (current unit) → understand scope and test cases (HOW MUCH)
3. **Write failing tests** → RED — translate MUST/WHEN-THEN to table-driven assertions
4. **Human reviews tests** → "is this what I meant?" (only human checkpoint)
5. **Implement** → GREEN — minimal code to pass
6. **Automated gates validate** → hooks verify quality
7. **Update plan unit status** → RED → GREEN → DONE
8. **Capture decisions** → if implementation diverged, record in plan's Decisions section

For features WITHOUT a design doc (small fixes, refactors): standard RED-GREEN-REFACTOR applies.
For features >50 lines: a design doc is strongly recommended (prevents scope creep and context pollution).

### Seed test pattern

When starting a new package, write ONE complete "seed" test that establishes all conventions. Two variants depending on whether the package has global mutable state:

**Parallel-safe seed** (most packages):

```go
func TestDiscover_WithService_Success(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name     string
        service  string
        mock     func(*platform.MockClient)
        wantErr  bool
        wantCode string
    }{
        {
            name:    "returns service details",
            service: "api",
            mock: func(m *platform.MockClient) {
                m.On("GetService", "api").Return(testService, nil)
            },
        },
        // ... more cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            mock := platform.NewMockClient()
            if tt.mock != nil {
                tt.mock(mock)
            }
            // ... test body
        })
    }
}
```

**Non-parallel seed** (packages with global state like `output.SetWriter`):

```go
// NOTE: output.SetWriter is global and not thread-safe.
// All tests in this package MUST be sequential (no t.Parallel()).
// See: https://github.com/zeropsio/zaia/blob/main/CLAUDE.md (output.SetWriter note)

func TestFormat_JSONOutput_Success(t *testing.T) {
    // NO t.Parallel() — see package-level comment

    tests := []struct {
        name   string
        input  any
        want   string
    }{
        {
            name:  "formats sync response",
            input: output.SyncData{Status: "ok"},
            want:  `{"type":"sync","status":"ok"}`,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // NO t.Parallel()
            var buf bytes.Buffer
            output.SetWriter(&buf)
            defer output.ResetWriter()
            // ... test body
        })
    }
}
```

The non-parallel seed documents WHY parallelism is disabled. This prevents Claude (or any developer) from "improving" the test by adding `t.Parallel()` and introducing race conditions.

### Testing layers

| Layer | Scope | Command | Mock level |
|-------|-------|---------|------------|
| Foundation | Client, auth, errors | `go test ./internal/platform/ ./internal/auth/` | Unit mocks |
| Unit | Business logic (ops/) | `go test ./internal/ops/...` | platform.Mock |
| Tool | MCP handlers (tools/) | `go test ./internal/tools/...` | in-memory MCP + Mock |
| Integration | Multi-tool flows | `go test ./integration/` | in-memory MCP + Mock |
| E2E | Real API lifecycle | `go test ./e2e/ -tags e2e` | Real Zerops API |

### Tiered verification (automated)

| Tier | When | What | Blocking? |
|------|------|------|-----------|
| 1 | Every `.go` edit | `go test ./<pkg> -short` + `go vet` + fast lint (incremental `-run` for test files) | No (async) |
| 2 | Claude finishes turn | `go test ./... -short` + `go vet` (command-based Stop hook) | Yes (`{ok: false}`) |
| 2b | Task marked complete | `go test ./... -short` + `go vet` (TaskCompleted hook) | Yes (exit 2) |
| 3 | `git commit` | `golangci-lint run ./...` | Yes (exit 2) |
| 3b | `git push` | `go test ./... -short` | Yes (exit 2) |
| 4 | Push/PR (CI) | `go test -race ./...` + full lint + multi-GOOS lint + cross-build | Yes (CI) |

### Test naming convention

```
Test{Operation}_{Scenario}_{ExpectedResult}
```

Examples: `TestDiscover_WithService_Success`, `TestManage_Scale_InvalidCPU_ValidationError`

### Rules

- **All tests use Go table-driven pattern** — no exceptions
- **`testing.Short()`** — long-running tests check `testing.Short()` and skip (the post-edit hook uses `-short`)
- **`t.Parallel()` only where safe** — document global state that prevents parallelism (e.g., `output.SetWriter`). Use the non-parallel seed pattern and add a comment explaining why.
- **Test files next to implementation** — `foo.go` and `foo_test.go` in same package

### Context isolation for complex features

For features >50 lines of implementation, consider separate sessions:
1. Session A writes failing tests (RED) and commits
2. Session B implements against committed tests (GREEN)
3. Either session refactors under green tests

Prevents context pollution where the agent designs tests around planned implementation.
Source: Alex Op found 20% → 84% TDD activation with context isolation.

### Coverage guidance

Quality signal, not enforced in hooks (adds 10-15s latency). Per-package targets:
- `internal/platform/`, `internal/auth/`: >85% (critical/security code)
- `internal/ops/`: >80% (business logic)
- `internal/tools/`: >70% (MCP handlers)

Check: `go test -coverprofile=c.out ./internal/<pkg>/... && go tool cover -func=c.out`

---

## 6. Directory Structure

```
zcp/
├── .claude/
│   ├── settings.json              # Permissions + hooks (checked in)
│   ├── settings.local.json        # Personal overrides (in .gitignore)
│   ├── compact-state.md           # Auto-generated by PreCompact hook (in .gitignore)
│   ├── last-test-result           # Cached test output (in .gitignore)
│   ├── hooks/
│   │   ├── session-start.sh       # SessionStart: inject project context (reads cache)
│   │   ├── pre-bash.sh            # PreToolUse: security + pre-commit lint + pre-push tests
│   │   ├── post-edit.sh           # PostToolUse (async): incremental test + vet + fast lint
│   │   ├── check-claude-md.sh     # PostToolUse (async): CLAUDE.md sync reminder
│   │   ├── stop.sh               # Stop: verify tests before Claude finishes (command)
│   │   ├── task-completed.sh      # TaskCompleted: verify tests before completion
│   │   ├── pre-compact.sh         # PreCompact: save git state (no test run)
│   │   └── on-failure.sh          # PostToolUseFailure: suggest fixes + auto-record
│   └── skills/
│       ├── zerops-services.md     # Zerops service types, versions, defaults
│       └── error-codes.md         # Error code catalog with resolution steps
├── .github/
│   └── workflows/
│       ├── ci.yml                 # Push/PR: test + multi-GOOS lint
│       ├── release.yml            # Tag v*: test + cross-compile + GitHub Release
│       └── e2e.yml                # Manual/daily: real Zerops API tests
├── cmd/zcp/
│   └── main.go                    # Entrypoint (minimal stub)
├── internal/                      # All packages
├── integration/                   # Integration tests
├── e2e/                           # E2E tests (build tag isolated)
├── design/                          # Behavioral contracts (stable, conceptual)
│   └── <feature>.md                 # MUST/WHEN-THEN specifications
├── plans/                           # Implementation plans (transient)
│   ├── <feature>-plan.md            # Active plans
│   └── archive/                     # Completed plans (history)
├── tools/
│   └── install.sh                 # CI: install golangci-lint
├── CLAUDE.md                      # Project constitution
├── CLAUDE.local.md                # Personal instructions (in .gitignore)
├── Makefile                       # Build targets (incl. setup)
├── .golangci.yaml                 # Linter config (61 linters + 2 formatters)
├── .gitignore
├── go.mod                         # go 1.24.0
└── go.sum                         # Committed — reproducible builds
```

### `.claude/skills/` — Progressive disclosure

Skills are **plain markdown files** that Claude reads with the `Read` tool when it needs domain-specific knowledge. They are NOT automatically loaded — Claude reads them on demand when working on tasks that require the information. CLAUDE.md should reference these files so Claude knows they exist.

**`zerops-services.md`** — Zerops service catalog:
```markdown
# Zerops Services Reference

## Runtime services
nodejs@22, php@8.3, python@3.12, go@1.22, rust@1.78, java@21, dotnet@8, elixir@1.17, gleam@1, bun@1, deno@1

## Database services
postgresql@16 (default), mariadb@11, clickhouse@24

## Cache services
valkey@7.2 (default, redis-compatible), keydb@6 (deprecated)

## Search services
meilisearch@1.10 (default), elasticsearch@8, typesense@27, qdrant@1 (internal-only)

## Queue services
nats@2.10 (default), kafka@3

## Storage services
object-storage (S3/MinIO), shared-storage (POSIX)

## Web services
nginx, static (SPA-ready)

## Defaults (use unless user specifies otherwise)
- postgresql@16, valkey@7.2, meilisearch@1.10, nats@2.10
- alpine base, NON_HA, SHARED CPU

## Networking rules
- Internal: ALWAYS http://, NEVER https:// (SSL terminates at L7 balancer)
- Ports: 10-65435 only (0-9 and 65436+ reserved)
- Cross-service env refs: ${service_hostname} (underscore, not dash)
- Cloudflare: MUST use "Full (strict)" SSL mode
- No localhost — services communicate via hostname
```

**`error-codes.md`** — error code catalog:
```markdown
# ZCP Error Codes

| Code | Exit | Description | Resolution |
|------|------|-------------|------------|
| AUTH_REQUIRED | 2 | Not authenticated | Run `zaia login --token <value>` |
| AUTH_INVALID_TOKEN | 2 | Invalid token | Check token format |
| AUTH_TOKEN_EXPIRED | 2 | Expired token | Re-authenticate |
| TOKEN_NO_PROJECT | 2 | Token has no project access | Token needs project scope |
| TOKEN_MULTI_PROJECT | 2 | Token has 2+ projects | Use project-scoped token |
| INVALID_ZEROPS_YML | 3 | Invalid zerops.yml | Run `zcp validate --file zerops.yml` |
| INVALID_IMPORT_YML | 3 | Invalid import.yml | Check YAML syntax + required fields |
| IMPORT_HAS_PROJECT | 3 | import.yml contains project: section | Remove `project:` — only `services:` allowed |
| INVALID_SCALING | 3 | Invalid scaling parameters | Check min/max CPU/RAM ranges |
| INVALID_PARAMETER | 3 | Invalid parameter | Check parameter types and ranges |
| INVALID_ENV_FORMAT | 3 | Bad KEY=VALUE format | Use `KEY=value` syntax |
| FILE_NOT_FOUND | 3 | File doesn't exist | Verify file path |
| SERVICE_NOT_FOUND | 4 | Service doesn't exist | Run `zcp discover` to list services |
| PROCESS_NOT_FOUND | 4 | Process doesn't exist | Check process ID |
| PROCESS_ALREADY_TERMINAL | 4 | Process already finished | No action needed |
| PERMISSION_DENIED | 5 | Insufficient permissions | Check token permissions |
| NETWORK_ERROR | 6 | Network error | Check connectivity |
| API_ERROR | 1 | Generic API error | Check API response details |
| API_TIMEOUT | 6 | Timeout | Retry or increase timeout |
| API_RATE_LIMITED | 6 | Rate limit | Wait and retry |
```

---

## 7. Makefile

Self-documenting `grep/awk` pattern with `##` comments (adopted from zaia-mcp).

```makefile
.PHONY: help setup test test-short test-race lint lint-fast lint-local vet build all clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILT   ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
MODULE  := github.com/zeropsio/zcp
LDFLAGS  = -s -w \
  -X $(MODULE)/internal/server.Version=$(VERSION) \
  -X $(MODULE)/internal/server.Commit=$(COMMIT) \
  -X $(MODULE)/internal/server.Built=$(BUILT)

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

setup: ## Bootstrap development environment (install all tools)
	@echo "==> Checking prerequisites..."
	@command -v go >/dev/null 2>&1 || { echo "ERROR: Go not installed"; exit 1; }
	@command -v jq >/dev/null 2>&1 || { echo "ERROR: jq not installed (brew install jq)"; exit 1; }
	@echo "==> Installing golangci-lint..."
	@./tools/install.sh
	@echo "==> Making hooks executable..."
	@chmod +x .claude/hooks/*.sh 2>/dev/null || true
	@echo "==> Verifying..."
	@go version
	@./bin/golangci-lint version 2>/dev/null || golangci-lint version
	@jq --version
	@echo "==> Setup complete."

test: ## Run all tests
	go test ./... -count=1

test-short: ## Run tests (short mode, ~3s)
	go test ./... -count=1 -short

test-race: ## Run tests with race detection
	go test -race ./... -count=1

lint: ## Run linter for all target platforms
	GOOS=darwin GOARCH=arm64 golangci-lint run ./...
	GOOS=linux GOARCH=amd64 golangci-lint run ./...

lint-fast: ## Fast lint (native platform, fast linters only, ~3s)
	golangci-lint run ./... --fast-only

lint-local: ## Full lint (native platform only)
	golangci-lint run ./...

vet: ## Run go vet
	go vet ./...

build: ## Build binary
	go build -ldflags "$(LDFLAGS)" -o bin/zcp ./cmd/zcp

clean: ## Remove build artifacts
	rm -rf bin/ builds/

#########
# BUILD #
#########
all: linux-amd linux-386 darwin-amd darwin-arm windows-amd ## Cross-build all platforms

linux-amd: ## Build for Linux amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o builds/zcp-linux-amd64 ./cmd/zcp

linux-386: ## Build for Linux 386
	CGO_ENABLED=0 GOOS=linux GOARCH=386 go build -ldflags "$(LDFLAGS)" -o builds/zcp-linux-386 ./cmd/zcp

darwin-amd: ## Build for macOS amd64
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o builds/zcp-darwin-amd64 ./cmd/zcp

darwin-arm: ## Build for macOS arm64
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o builds/zcp-darwin-arm64 ./cmd/zcp

windows-amd: ## Build for Windows amd64
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o builds/zcp-win-x64.exe ./cmd/zcp
```

---

## 8. `.golangci.yaml`

Based on source repos' identical config (60 linters) plus `modernize` (catches outdated Go patterns in AI-generated code). **61 linters + 2 formatters (gofmt, goimports) = 63 analysis tools.**

```yaml
version: "2"
run:
  concurrency: 16
  issues-exit-code: 1
  tests: true
output:
  formats:
    text:
      path: stdout
linters:
  default: none
  enable:
    - asasalint
    - asciicheck
    - bidichk
    - bodyclose
    - containedctx
    - contextcheck
    - decorder
    - dogsled
    - dupword
    - durationcheck
    - errcheck
    - errchkjson
    - errname
    - errorlint
    - exhaustive
    - forcetypeassert
    - gocheckcompilerdirectives
    - gochecksumtype
    - goconst
    - gocritic
    - godox
    - goheader
    - gomodguard
    - goprintffuncname
    - gosec
    - gosmopolitan
    - govet
    - grouper
    - importas
    - ineffassign
    - loggercheck
    - maintidx
    - makezero
    - mirror
    - misspell
    - modernize
    - musttag
    - nakedret
    - nilerr
    - nilnil
    - noctx
    - nolintlint
    - nosprintfhostport
    - prealloc
    - predeclared
    - reassign
    - revive
    - staticcheck
    - tagalign
    - tagliatelle
    - testableexamples
    - testifylint
    - thelper
    - tparallel
    - unconvert
    - unparam
    - unused
    - usestdlibvars
    - usetesting
    - wastedassign
    - whitespace
  settings:
    goconst:
      min-len: 3
      min-occurrences: 3
    godox:
      keywords:
        - FIXME
    revive:
      rules:
        - name: blank-imports
        - name: context-as-argument
        - name: dot-imports
        - name: error-return
        - name: error-strings
        - name: error-naming
        - name: exported
        - name: increment-decrement
        - name: var-naming
        - name: range
        - name: receiver-naming
        - name: time-naming
        - name: unexported-return
        - name: indent-error-flow
        - name: errorf
        - name: empty-block
        - name: superfluous-else
        - name: unreachable-code
  exclusions:
    generated: lax
    presets:
      - comments
      - common-false-positives
      - legacy
      - std-error-handling
    rules:
      - linters:
          - errorlint
          - forcetypeassert
        path: _test\.go
      - linters:
          - gosec
        path: _test\.go
        text: 'G306:'
      - linters:
          - gosec
        path: testutil/
        text: 'G306:'
      - linters:
          - goconst
        path: _test\.go
      - linters:
          - gosec
        text: 'G101:'
      - linters:
          - gosec
        text: 'G115:'
      - linters:
          - revive
        path: internal/platform/client\.go
        text: 'var-naming: struct field'
      - linters:
          - revive
        text: 'exported: type name will be used as'
    paths:
      - third_party$
      - builtin$
      - examples$
issues:
  max-same-issues: 0
formatters:
  enable:
    - gofmt
    - goimports
  exclusions:
    generated: lax
    paths:
      - third_party$
      - builtin$
      - examples$
```

---

## 9. `.gitignore`

```gitignore
# Build artifacts
bin/
builds/

# Secrets
.env
.env.*
*.key
*.pem

# Claude Code personal settings + generated files
CLAUDE.local.md
.claude/settings.local.json
.claude/compact-state.md
.claude/last-test-result

# OS
.DS_Store
Thumbs.db

# Go
tmp/
vendor/

# IDE
.idea/
.vscode/
*.swp
*~
```

---

## 10. GitHub Actions

### 10.1 `ci.yml` — Push/PR: test + multi-GOOS lint

Lint runs for both `linux/amd64` and `darwin/arm64` (using GOOS env var — doesn't need native runner) to catch platform-specific lint issues.

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: ['*']

jobs:
  test:
    name: test & lint
    runs-on: ubuntu-22.04
    env:
      CGO_ENABLED: '0'
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: "go.mod"

      - name: Install tools
        run: |
          export GOPATH=$HOME/go
          ./tools/install.sh
          echo "$PWD/bin" >> $GITHUB_PATH

      - name: Verify tooling
        run: |
          go version
          golangci-lint version

      - name: Build (all platforms)
        run: |
          GOOS=linux GOARCH=amd64 go build -v ./...
          GOOS=darwin GOARCH=arm64 go build -v ./...
          GOOS=windows GOARCH=amd64 go build -v ./...

      - name: Test
        run: go test -race ./... -count=1

      - name: Lint (linux/amd64)
        run: GOOS=linux GOARCH=amd64 golangci-lint run ./...

      - name: Lint (darwin/arm64)
        run: GOOS=darwin GOARCH=arm64 golangci-lint run ./...
```

### 10.2 `release.yml` — Tag v*: test + cross-compile + GitHub Release

```yaml
name: Release

on:
  push:
    tags: ['v*']

permissions:
  contents: write

jobs:
  test:
    name: verify
    runs-on: ubuntu-22.04
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: "go.mod"

      - name: Install tools
        run: |
          export GOPATH=$HOME/go
          ./tools/install.sh
          echo "$PWD/bin" >> $GITHUB_PATH

      - name: Test
        run: go test -race ./... -count=1

      - name: Lint
        run: golangci-lint run ./...

  build:
    name: build ${{ matrix.name }}
    needs: test
    runs-on: ubuntu-22.04
    env:
      CGO_ENABLED: '0'
    strategy:
      matrix:
        include:
          - name: linux amd64
            env: { GOOS: linux, GOARCH: amd64 }
            file: zcp-linux-amd64
            compress: true
            strip: true
          - name: linux 386
            env: { GOOS: linux, GOARCH: 386 }
            file: zcp-linux-386
            compress: true
            strip: true
          - name: darwin amd64
            env: { GOOS: darwin, GOARCH: amd64 }
            file: zcp-darwin-amd64
            compress: false
            strip: false
          - name: darwin arm64
            env: { GOOS: darwin, GOARCH: arm64 }
            file: zcp-darwin-arm64
            compress: false
            strip: false
          - name: windows amd64
            env: { GOOS: windows, GOARCH: amd64 }
            file: zcp-win-x64.exe
            compress: false
            strip: false

    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: "go.mod"

      # NOTE: Module path must match MODULE in Makefile (github.com/zeropsio/zcp)
      - name: Build
        env: ${{ matrix.env }}
        run: >-
          go build
          -o builds/${{ matrix.file }}
          -ldflags "-s -w
          -X github.com/zeropsio/zcp/internal/server.Version=${{ github.ref_name }}
          -X github.com/zeropsio/zcp/internal/server.Commit=${{ github.sha }}
          -X github.com/zeropsio/zcp/internal/server.Built=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
          ./cmd/zcp

      - name: Compress binary
        if: ${{ matrix.compress }}
        uses: svenstaro/upx-action@v2
        with:
          file: ./builds/${{ matrix.file }}
          strip: ${{ matrix.strip }}

      - uses: actions/upload-artifact@v4
        with:
          name: ${{ matrix.file }}
          path: ./builds/${{ matrix.file }}

  release:
    name: Create release
    needs: build
    runs-on: ubuntu-22.04
    steps:
      - uses: actions/download-artifact@v4
        with:
          path: ./builds
          merge-multiple: true

      - name: Create GitHub release
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: >-
          gh release create "${{ github.ref_name }}"
          ./builds/*
          --repo "${{ github.repository }}"
          --title "${{ github.ref_name }}"
          --generate-notes
```

### 10.3 `e2e.yml` — Real API tests

```yaml
name: E2E Tests

on:
  schedule:
    - cron: '0 6 * * *'
  workflow_dispatch:

jobs:
  e2e:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: Build ZCP
        run: go build -o bin/zcp ./cmd/zcp

      - name: Run E2E tests
        env:
          ZEROPS_TOKEN: ${{ secrets.ZEROPS_E2E_TOKEN }}
        run: go test ./e2e/ -tags e2e -v -count=1 -timeout 5m
```

---

## 11. `cmd/zcp/main.go` — Minimal stub

Just enough for `go build` to succeed. Will be replaced when server package is implemented.

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "zcp: not yet implemented")
	os.Exit(1)
}
```

---

## 12. `go.mod` — Initial module

```
module github.com/zeropsio/zcp

go 1.24.0
```

After `go mod init`, run `go mod tidy` to generate `go.sum`. Both files must be committed — `go.sum` ensures reproducible builds across environments.

**Dependency note:** When porting code from zaia and zaia-mcp, dependencies will overlap (both use `github.com/mark3labs/mcp-go`, `github.com/zeropsio/zerops-go`, etc.). Run `go mod tidy` after each porting step to keep dependencies clean. Do NOT use `replace` directives for production code.

---

## 13. Conventions

### File organization
- Max 300 lines per `.go` file — split when growing
- Clear separation: ops/ = business logic, tools/ = MCP wrappers, platform/ = SDK abstraction

### Code quality
- `golangci-lint` with 61 linters + 2 formatters (source repos' 60 linters + `modernize`)
- `gofmt` + `goimports` as formatters (in .golangci.yaml)
- `go vet` on every edit (via async post-edit hook)

### Git workflow
- Pre-commit: lint must pass (**exit 2**, actually blocks)
- Pre-push: tests must pass (**exit 2**, actually blocks)
- CLAUDE.md checked when key files change (async reminder)
- No force-push (blocked by pre-bash hook)
- Destructive operations blocked (rm -rf, rm -r, git reset --hard, git checkout ., curl|bash, dd)
- `go.sum` always committed

### Language
- All code, comments, documentation, commit messages in **English**

### Test result caching
- `post-edit.sh`, `stop.sh`, and `task-completed.sh` write test output to `.claude/last-test-result`
- `session-start.sh` reads cached results (no redundant test run on session start)
- Cache file is gitignored (ephemeral, machine-local)

---

## 14. Memory System

Claude Code auto-memory at `~/.claude/projects/*/memory/MEMORY.md`.

### Structure

| File | Purpose | Max size |
|------|---------|----------|
| `MEMORY.md` | Top-level index (always loaded into system prompt) | 200 lines |
| `errors.md` | Recurring errors and their fixes (auto-populated by on-failure.sh) | Unlimited |
| `patterns.md` | Project-specific patterns discovered during development | Unlimited |
| `decisions.md` | Architectural decisions with rationale | Unlimited |

### What to record
- Errors encountered and how they were resolved
- Pattern decisions made for this project
- Environment constraints (Go version quirks, SDK issues)
- Things that didn't work (to avoid repeating)
- Linter issues Claude keeps making

### What NOT to record
- Obvious Go patterns (already in LLM training)
- Things already in CLAUDE.md (no duplication)
- Temporary debugging notes

### Auto-recording (via hooks)
The `on-failure.sh` hook (PostToolUseFailure) detects common failure patterns and **auto-appends new patterns to `memory/errors.md`**. This creates cross-session persistence: errors encountered once are recorded and available in future sessions.

---

## 15. Hook System Reference

### All Claude Code hook events

| Event | When | Can block? | Used in ZCP? | Hook type |
|-------|------|------------|--------------|-----------|
| **SessionStart** | Session begins/resumes | No (context injection) | **session-start.sh** (reads cache) | command |
| UserPromptSubmit | User submits prompt | Yes | Not yet | — |
| **PreToolUse** | Before tool executes | **Yes** | **pre-bash.sh** | command |
| PermissionRequest | Permission dialog shown | Yes | Not yet | — |
| **PostToolUse** | After tool succeeds | No (advisory feedback) | **post-edit.sh** (async), **check-claude-md.sh** (async) | command (async) |
| **PostToolUseFailure** | After tool fails | No (context injection) | **on-failure.sh** (auto-records) | command |
| Notification | Notification sent | No | Not yet | — |
| **SubagentStart** | Subagent spawned | No (context injection) | **inline** (settings.json) | command |
| SubagentStop | Subagent finished | Yes | Not yet | — |
| **Stop** | Claude finishes responding | **Yes** (`{ok: false}`) | **stop.sh** (with `stop_hook_active` guard) | command |
| TeammateIdle | Teammate going idle | Yes (exit 2) | Not yet | — |
| **TaskCompleted** | Task marked complete | **Yes (exit 2)** | **task-completed.sh** | command |
| **PreCompact** | Before context compaction | No (state save) | **pre-compact.sh** (git state only) | command |
| SessionEnd | Session terminates | No | Not yet | — |

### Hook types available

| Type | Description | Timeout default | Use case |
|------|-------------|-----------------|----------|
| `command` | Runs shell script | 600s | Deterministic checks (lint, test, security) |
| `prompt` | Sends prompt to LLM (Haiku) | 30s | Judgment calls (TDD quality, code review) |
| `agent` | Spawns subagent with tool access | 60s | Complex verification (needs tool access) |

ZCP uses `command` for all hooks including Stop. The `agent` type is reserved for cases that genuinely need tool access (e.g., reading multiple files, making decisions). For "run tests and report pass/fail", a command is sufficient and 10x cheaper.

### JSON input/output reference

**PreToolUse Bash input:**
```json
{
  "session_id": "...",
  "cwd": "/path/to/project",
  "hook_event_name": "PreToolUse",
  "tool_name": "Bash",
  "tool_input": {
    "command": "go test ./..."
  }
}
```

**PreToolUse blocking via JSON (alternative to exit 2):**
```json
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "deny",
    "permissionDecisionReason": "Destructive command blocked"
  }
}
```

**PostToolUse Edit/Write input:**
```json
{
  "hook_event_name": "PostToolUse",
  "tool_name": "Edit",
  "tool_input": {
    "file_path": "/path/to/file.go",
    "old_string": "...",
    "new_string": "..."
  },
  "tool_response": {
    "filePath": "/path/to/file.go",
    "success": true
  }
}
```

Note: `tool_input` uses `file_path` (snake_case), `tool_response` uses `filePath` (camelCase).

**PostToolUse structured feedback (async hook stdout):**

Async PostToolUse hooks provide **advisory** feedback that Claude receives on the next turn. `"decision": "block"` tells Claude to prioritize fixing the issue, but **cannot undo the edit** (it's already applied):

```json
{
  "decision": "block",
  "reason": "Tests failed in ./internal/ops: TestDiscover_Success FAIL"
}
```

Success feedback:
```json
{
  "additionalContext": "Tests, vet, and lint passed for ./internal/ops"
}
```

**Stop hook input (command-based):**

The Stop hook receives `stop_hook_active` to prevent infinite loops:
```json
{
  "stop_hook_active": false
}
```

When `stop_hook_active` is `true`, the hook **MUST** immediately return `{"ok": true}` without doing anything. This is the FIRST check in `stop.sh`. Failure to check this field causes infinite loops.

**SessionStart context injection:**
```json
{
  "additionalContext": "Branch: main. Recent: abc123 fix auth. Tests passing (cached 120s ago). Uncommitted: 2 files changed."
}
```

### Timeout discipline

Every hook timeout must be **greater than** the longest command inside:

| Hook | Hook timeout | Longest inner command | Headroom |
|------|-------------|----------------------|----------|
| session-start.sh | 10s | git commands (~1s) | 9s |
| pre-bash.sh | 120s | golangci-lint (~60s) | 60s |
| post-edit.sh | 120s | test+vet+lint (~90s) | 30s |
| check-claude-md.sh | 5s | grep (~0.1s) | ~5s |
| stop.sh | 90s | go test -timeout=60s | 30s |
| task-completed.sh | 120s | go test -timeout=60s + vet | 30s+ |
| pre-compact.sh | 10s | git commands (~1s) | 9s |
| on-failure.sh | 5s | grep + file write (~0.1s) | ~5s |

### Future hook opportunities

Not in scope for Phase 1 but worth noting:

- **UserPromptSubmit hook** — inject context based on what's being asked
- **SubagentStop hook** — verify subagent results before accepting
- **`type: "prompt"` PreToolUse** — LLM-judged TDD enforcement (check if edit follows red-green-refactor)

---

## 16. Implementation Checklist

### Phase 0: Prerequisites
- [ ] Install golangci-lint v2.8.0 locally
- [ ] Install jq (for hook scripts)
- [ ] Verify: `golangci-lint version`, `jq --version`

### Phase 1: Framework Setup
- [ ] Initialize `go.mod` (`github.com/zeropsio/zcp`, go 1.24.0)
- [ ] Create directory structure (cmd/zcp/, internal/, integration/, e2e/, design/, plans/, plans/archive/, tools/, .claude/hooks/, .claude/skills/, .github/workflows/)
- [ ] Write `.claude/settings.json` (Section 2)
- [ ] Write `.claude/hooks/session-start.sh` (Section 3.1)
- [ ] Write `.claude/hooks/pre-bash.sh` (Section 3.2)
- [ ] Write `.claude/hooks/post-edit.sh` (Section 3.3)
- [ ] Write `.claude/hooks/check-claude-md.sh` (Section 3.4)
- [ ] Write `.claude/hooks/stop.sh` (Section 3.8)
- [ ] Write `.claude/hooks/task-completed.sh` (Section 3.5)
- [ ] Write `.claude/hooks/pre-compact.sh` (Section 3.6)
- [ ] Write `.claude/hooks/on-failure.sh` (Section 3.7)
- [ ] Make all hooks executable: `chmod +x .claude/hooks/*.sh`
- [ ] Write `.claude/skills/zerops-services.md` (Section 6)
- [ ] Write `.claude/skills/error-codes.md` (Section 6)
- [ ] Write `CLAUDE.md` (Section 4)
- [ ] Write `Makefile` (Section 7)
- [ ] Write `.golangci.yaml` (Section 8)
- [ ] Write `.gitignore` (Section 9)
- [ ] Write `tools/install.sh` (Section 1)
- [ ] Write `.github/workflows/ci.yml` (Section 10.1)
- [ ] Write `.github/workflows/release.yml` (Section 10.2)
- [ ] Write `.github/workflows/e2e.yml` (Section 10.3)
- [ ] Write `cmd/zcp/main.go` (Section 11)
- [ ] Run `go mod tidy` to generate `go.sum`
- [ ] Run `make setup` to verify bootstrap
- [ ] Verify framework (Section 17)

### Phase 2: Code Implementation (document-driven)

Each feature area follows: Design doc → Plan → Unit-by-unit TDD

- [ ] **Design docs** — create `design/<feature>.md` for each major area:
  - [ ] `design/platform.md` — API client, types, errors
  - [ ] `design/auth.md` — login, token, project discovery
  - [ ] `design/discover.md` — service/project discovery
  - [ ] `design/manage.md` — start/stop/restart/scale
  - [ ] `design/env.md` — environment variables
  - [ ] `design/logs.md` — log fetching
  - [ ] `design/deploy.md` — code deployment
  - [ ] `design/import.md` — infrastructure import
  - [ ] `design/validate.md` — YAML validation
  - [ ] `design/knowledge.md` — BM25 search
  - [ ] `design/server.md` — MCP server, tool registration
- [ ] **Foundation plan** — `plans/foundation-plan.md`:
  - [ ] Unit: platform client + MockClient
  - [ ] Unit: error types + codes
  - [ ] Unit: auth manager + storage
- [ ] **Ops plan** — `plans/ops-plan.md`:
  - [ ] Unit per operation (discover, manage, env, logs, deploy, import, validate)
- [ ] **Tools plan** — `plans/tools-plan.md`:
  - [ ] Unit per MCP tool handler
- [ ] **Server plan** — `plans/server-plan.md`:
  - [ ] Unit: server setup + tool registration
  - [ ] Unit: cmd/zcp main.go (full)
- [ ] **Integration + E2E**
  - [ ] Integration test plan
  - [ ] E2E test plan (real Zerops API)
- [ ] Add `.mcp.json` with gopls MCP server config (stdio transport)
- [ ] Add CLAUDE.md note: "Use gopls MCP tools for symbol search and diagnostics"

---

## 17. Verification Criteria

After Phase 1, verify each component:

| # | Test | Expected result |
|---|------|----------------|
| 1 | `make setup` | All tools installed, hooks executable |
| 2 | Edit a `.go` file | post-edit hook runs async: test + vet + lint feedback on next turn |
| 3 | Edit a `_test.go` file | post-edit runs with targeted `-run` (incremental) |
| 4 | Run `rm -rf .` via Bash | Blocked by pre-bash.sh (exit 2) |
| 5 | Run `rm -r .` via Bash | Blocked by pre-bash.sh (exit 2) |
| 6 | Run `git push --force` via Bash | Blocked by pre-bash.sh (exit 2) |
| 7 | Run `git push` with failing tests | Blocked by pre-bash.sh pre-push gate (exit 2) |
| 8 | Run `git checkout .` via Bash | Blocked by pre-bash.sh (exit 2) |
| 9 | Run `git checkout -- .` via Bash | Blocked by pre-bash.sh (exit 2) |
| 9b | Run `git restore .` via Bash | Blocked by pre-bash.sh (exit 2) |
| 9c | Run `git stash drop` via Bash | Blocked by pre-bash.sh (exit 2) |
| 10 | Run `curl ... \| bash` via Bash | Blocked by pre-bash.sh (exit 2) |
| 11 | Run `git commit` with lint errors | Blocked by pre-bash.sh lint gate (exit 2) |
| 12 | Run `go test ./...` | Runs without confirmation prompt |
| 13 | Run `go run ./cmd/zcp` | Runs without confirmation prompt |
| 14 | Attempt to read `.env` | Blocked by deny permission |
| 15 | Edit key architectural file | CLAUDE.md reminder shown (async) |
| 16 | `make test-short` | Passes |
| 17 | `make lint-fast` | Passes |
| 18 | `make build` | Produces `bin/zcp` |
| 19 | CLAUDE.md line count | Under 150 lines |
| 20 | Claude tries to stop with failing tests | Stop hook returns `{ok: false}`, Claude fixes |
| 20b | Claude stops after non-code response | Stop hook skips tests (no .go changes), returns `{ok: true}` instantly |
| 21 | Stop hook with `stop_hook_active: true` | Immediately returns `{ok: true}` (no tests run) |
| 22 | Mark task completed with failing tests | TaskCompleted hook blocks (exit 2) |
| 23 | Start new session | SessionStart injects branch, commits, cached test status |
| 24 | Context auto-compacts | PreCompact saves git state (no test run, no delay) |
| 25 | Bash command fails with "package not found" | on-failure.sh suggests `go mod tidy`, records to errors.md |
| 26 | Subagent spawned | Gets Go version and module info injected |
| 27 | Cached test results | `.claude/last-test-result` updated after post-edit, stop, task-completed |
