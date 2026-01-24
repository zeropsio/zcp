# ZCP Comprehensive Audit Findings
## Complete Output from 12 Parallel Subagents

**Date:** 2026-01-24
**Project:** /Users/fxck/www/zcp/zcp/.zcp
**Total Shell Scripts Analyzed:** 26+
**Total Lines Audited:** ~10,500+

---

# EXECUTIVE SUMMARY

## Critical Bugs (3)
1. **`head -n -1` macOS incompatibility** - [topics.sh:35, 38](zcp/.zcp/lib/help/topics.sh#L35) - `--help trouble` and `--help example` crash on macOS
2. **Missing `synthesis` help topic** - [CLAUDE.md:164](zcp/CLAUDE.md#L164) references non-existent topic
3. **`mount.sh --help` broken** - [mount.sh](zcp/.zcp/mount.sh) - Tries to mount "--help" as service name

## Code Duplication (~800 lines)
- Gate checking boilerplate: 9× in gates.sh (~180 lines)
- Phase guidance messages: Duplicated in transition.sh + status.sh (~400 lines)
- `get_session()` duplicate: verify.sh:90-96 duplicates utils.sh (7 lines)
- Evidence creation patterns: 5× across command files (~100 lines)

## Documentation Bloat
- `--help` output: 359 lines (should be ~60)
- README.md: ~50 lines duplicate content with CLAUDE.md
- Help system total: 1,723 lines → reducible to ~700 lines (60% reduction)

## Architecture Opportunities
- 10 command files → 4-6 consolidated modules (~35% reduction)
- Unified validator (validate-config.sh + validate-import.sh) → ~200 lines saved
- Overall codebase reducible by ~38% (~2,982 lines)

---

# WAVE 1: COMMAND SURFACE AUDIT

---

## Agent 1: Init/Show/State/Reset Commands (Task ID: abd25fc)

### Commands Tested
- `.zcp/workflow.sh --help`
- `.zcp/workflow.sh init --help`
- `.zcp/workflow.sh show`
- `.zcp/workflow.sh show --full`
- `.zcp/workflow.sh state`
- `.zcp/workflow.sh recover`

### Findings

#### 1. `--help` Output
**Output Length:** 359 lines

**Issues Found:**
1. **EXCESSIVE CONTENT** - The `--help` output is a complete platform reference manual (359 lines), not a help screen. It includes:
   - Full orientation section
   - Complete variable documentation
   - All phases with examples
   - Full troubleshooting table
   - Complete example walkthrough
   - Gates documentation

2. **TOPIC LIST INCONSISTENCY** - The `--help` output lists topics as:
   ```
   Topics: discover, develop, deploy, verify, done, vars, services,
           trouble, example, gates, extend, bootstrap
   ```

   But the actual available topics (from error output) are:
   ```
   Available topics:
     discover, develop, deploy, verify, done
     vars, services, trouble, example, gates
     extend, bootstrap, cheatsheet, import-validation
   ```

   **Missing from --help:** `cheatsheet`, `import-validation`

3. **CLAUDE.md REFERENCES SYNTHESIS TOPIC THAT DOESN'T EXIST**
   ```
   - `synthesis` — Bootstrap flow: COMPOSE → EXTEND → SYNTHESIZE
   ```
   But `--help synthesis` returns:
   ```
   Unknown help topic: synthesis
   ```

#### 2. `init --help` Behavior
**Behavior:** Does NOT show init-specific help. Instead shows full workflow status.

**Issue:** `init --help` should show init command options, not the current workflow status. The `--help` flag is being consumed as an unknown flag rather than triggering help.

#### 3. `show` Command
**Output Length:** 24 lines (concise, good)
**Assessment:** Well-designed and appropriately sized.

#### 4. `show --full` Command
**Output Length:** 30 lines

**Issues:**
1. **MINIMAL DIFFERENCE FROM `show`** - Only adds 6 lines
2. **THE NAME "EXTENDED CONTEXT" IS MISLEADING** - Users might expect much more

#### 5. `state` Command
**Output:** One-line summary (exactly as documented)
```
COMPOSE | full | dev=? stage=? | dev_verify=missing stage_verify=missing
```
**Assessment:** Excellent. Concise, machine-parseable, fits purpose perfectly.

#### 6. `recover` Command
**Output Length:** 137 lines

**Issues:**
1. **REDUNDANT WITH `show --guidance`** - Nearly identical output
2. **EXCESSIVE FOR "RECOVERY"** - Compare:
   - `show`: 24 lines
   - `show --full`: 30 lines
   - `show --guidance`: 123 lines
   - `recover`: 137 lines

### Cross-Topic Inconsistencies

| Command | In --help | In Unknown Cmd Error | In CLAUDE.md |
|---------|-----------|---------------------|--------------|
| `iterate` | NO | YES | NO |
| `retarget` | NO | YES | NO |
| `intent` | NO | YES | NO |
| `note` | NO | YES | NO |
| `compose` | NO | YES | NO |
| `verify_synthesis` | NO | YES | NO |
| `plan_services` | NO | YES | NO |
| `snapshot_dev` | NO | YES | NO |

### Progress Bar Bug
In `show --guidance` output, the progress JSON shows:
```json
"percent": -16
```
This appears to be a calculation bug when in early phases.

### Recommendations

**High Priority:**
1. Create concise `--help` - The current `--help` should become `--help reference` or `--help full`. Default `--help` should be 20-30 lines.
2. Fix topic list - Update `full.sh` to list all 14 available topics
3. Remove non-existent synthesis topic from CLAUDE.md
4. Fix `init --help` - Should show init options, not workflow status
5. Document missing commands in --help

**Medium Priority:**
6. Consider merging `recover` and `show --guidance`
7. Fix negative progress percentage bug
8. Rename or enhance `show --full`

---

## Agent 2: Help and Phase-Related Commands (Task ID: a0b0180)

### Commands Tested
All 14 help topics plus main help

### Help Topic Analysis

| Topic | Lines | Status | Notes |
|-------|-------|--------|-------|
| Main help | 359 | Working | EXCESSIVE - This is a manual, not help |
| `discover` | 37 | Working | Concise, good |
| `develop` | 124 | Working | Slightly verbose but comprehensive |
| `deploy` | 74 | Working | Good length |
| `verify` | 66 | Working | Good length |
| `done` | 41 | Working | Good length |
| `vars` | 144 | Working | Very comprehensive, possibly too detailed |
| `services` | 113 | Working | Good, explains hostname vs type well |
| `trouble` | N/A | **BROKEN** | `head -n -1` not supported on macOS |
| `example` | N/A | **BROKEN** | Same issue as `trouble` |
| `gates` | 24 | Working | Concise and useful |
| `extend` | 208 | Working | VERBOSE - full tutorial |
| `bootstrap` | 227 | Working | VERBOSE - full tutorial |
| `cheatsheet` | 98 | Working | Good quick reference |
| `import-validation` | 149 | Working | Comprehensive but reasonable |

### BROKEN COMMANDS

#### `trouble` Topic - **BROKEN**
```bash
.zcp/workflow.sh --help trouble
# Error: head: illegal line count -- -1
```

**Root Cause:** Line 35 in `/Users/fxck/www/zcp/zcp/.zcp/lib/help/topics.sh`:
```bash
show_full_help | sed -n '/🔧 TROUBLESHOOTING/,/📖 COMPLETE EXAMPLE/p' | head -n -1
```
The `head -n -1` syntax (remove last line) is GNU-specific and not supported on macOS BSD `head`.

**Fix:** Replace `head -n -1` with macOS-compatible alternative:
```bash
# Option 1: Use sed to delete last line
show_full_help | sed -n '/🔧 TROUBLESHOOTING/,/📖 COMPLETE EXAMPLE/p' | sed '$d'

# Option 2: Use awk
show_full_help | sed -n '/🔧 TROUBLESHOOTING/,/📖 COMPLETE EXAMPLE/p' | awk 'NR>1{print prev} {prev=$0}'
```

#### `example` Topic - **BROKEN**
**Same root cause** at line 38

### Missing `synthesis` Topic
The CLAUDE.md file lists `synthesis` as a help topic but running `.zcp/workflow.sh --help synthesis` returns:
```
❌ Unknown help topic: synthesis
```

### Duplication Analysis

| Content | Locations | Lines Duplicated |
|---------|-----------|------------------|
| Triple-kill pattern | Main help, develop, deploy, cheatsheet | 4× |
| zcli login command | Main help, discover, deploy, example | 4× |
| zeropsSubdomain warning | Main help, vars, verify, cheatsheet | 4× |
| deployFiles reminder | Main help, deploy, cheatsheet | 3× |
| verify.sh usage | Main help, develop, verify, cheatsheet | 4× |
| agent-browser commands | Main help, develop, verify, cheatsheet | 4× |
| DB tools from ZCP warning | Main help, develop, verify, extend, cheatsheet | 5× |
| "HTTP 200 != working" | Main help, develop, verify, cheatsheet | 4× |

**Impact:** At least 200+ lines of pure duplication across topics.

### Topic List Mismatch
- **Main help lists:** 12 topics
- **Error message lists:** 14 topics (adds `cheatsheet`, `import-validation`)
- **CLAUDE.md lists:** includes `synthesis` (non-existent)

---

## Agent 3: Discovery and Planning Commands (Task ID: a9d513d)

### Commands Analyzed
- `create_discovery`
- `refresh_discovery`
- `plan_services`

### `create_discovery` Analysis

**File:** `/Users/fxck/www/zcp/zcp/.zcp/lib/commands/discovery.sh` (lines 4-99)

**Arguments:**
| Position | Name | Required | Description |
|----------|------|----------|-------------|
| 1 | `--single` | Optional | Flag to enable single-service mode |
| 1/2 | `dev_id` | Yes | Service ID for development |
| 2/3 | `dev_name` | Yes | Service hostname for development |
| 3 | `stage_id` | Yes (unless `--single`) | Service ID for staging |
| 4 | `stage_name` | Yes (unless `--single`) | Service hostname for staging |

**Issues Found:**

1. **Argument parsing quirk in single mode (lines 12-23)**
   - Variable naming is confusing since `stage_id` and `stage_name` are set to dev values
   - **Recommendation:** Add inline comments explaining this intentional behavior

2. **No validation of service ID format**
   - Service IDs are accepted without any format validation
   - Bad IDs will silently be stored and cause issues later

3. **Dependency check redundancy**
   - Line 53-56 checks for `jq`, but `workflow.sh` already checks

4. **Help text mentions only in error case**
   - No `--help` flag support

### `refresh_discovery` Analysis

**File:** Lines 101-152

**Issues:**
1. **Hardcoded projectId location (line 119)**
   ```bash
   pid=$(cat /tmp/projectId 2>/dev/null || echo "$projectId")
   ```
   Uses hardcoded `/tmp/projectId` rather than `$ZCP_TMP_DIR`

2. **Magic number in output (line 126)**
   ```bash
   zcli service list -P "$pid" 2>/dev/null | grep -v "^Using config" | head -15
   ```
   Hardcoded limit of 15 services displayed

3. **Weak service existence check (lines 135-140)**
   Uses simple grep substring match - could false-positive

4. **No session validation**
   Unlike `create_discovery`, doesn't check if discovery file belongs to current session

5. **Read-only operation - misleading naming**
   Command name suggests it "refreshes" (updates) discovery, but it only validates

### `plan_services` Analysis

**File:** Lines 4-182

**Issues:**

1. **Hardcoded version strings (lines 32-75)**
   ```bash
   case "$runtime" in
       go|golang)
           prod_base="alpine@3.21"
           dev_base="go@1"
           ...
       python)
           prod_base="python@3.12"
           dev_base="python@3.12"
   ```
   These will become stale as new versions are released

2. **Hardcoded database version (line 99)**
   ```bash
   local db_version="17"
   ```
   PostgreSQL version hardcoded to 17

3. **Hardcoded service hostnames (lines 95-97, 115-129)**
   Service names hardcoded to "appdev", "appstage", "db"

4. **Misleading variable name (lines 115-129)**
   Variable `--arg db "$dev_base"` is confusing - named `db` but contains runtime base

5. **No validation of runtime type**
   Unknown runtime types fall through to a generic handler

---

## Agent 4: Synthesis/Bootstrap Commands (Task ID: a9cd561)

### Commands Analyzed
- `compose`
- `verify_synthesis`
- `extend`
- `upgrade-to-full`
- `validate-import.sh`

### `compose` Command

**File:** `/Users/fxck/www/zcp/zcp/.zcp/lib/commands/compose.sh` (636 lines)

**Arguments:**
| Argument | Short | Required | Description |
|----------|-------|----------|-------------|
| `--runtime` | `-r` | Yes | Runtime type: go, nodejs, python, php, rust, bun, java, dotnet |
| `--services` | `-s` | No | Comma-separated managed services |
| `--hostname` | `-h` | No | Hostname prefix (default: `api`) |
| `--dev-only` | - | No | Skip stage service creation |
| `--help` | - | No | Show help |

**Output:**
- Creates `/tmp/synthesis_plan.json`
- Creates `/tmp/synthesized_import.yml`

**Help Text Quality:** EXCELLENT

**Issues:**
1. **Duplicated lookup** (line 140 and 156): `get_service_type_info()` called twice for same runtime
2. **Complex string building** (lines 191-211): Building JSON by string concatenation is fragile
3. **Hardcoded versions** (lines 18-36): Service versions hardcoded

### `verify_synthesis` Command

**Issues:**
1. **Runtime-specific file checks duplicated** (lines 489-522)
2. **Inconsistent array handling** (lines 526-548)
3. **Hash calculation** (line 549): Uses `md5sum` with fallback - platform-specific

### `extend` Command

**File:** Lines 40-232 (193 lines)

**Arguments:**
| Argument | Position | Required | Description |
|----------|----------|----------|-------------|
| `import_file` | 1 | Yes | Path to import.yml file |
| `--skip-validation` | 2 | No | Bypass Gate 0.5 validation |

**Issues:**
1. **Hardcoded timeout** (line 140): `timeout_seconds=600` - should be configurable
2. **Duplicate service list fetches** (lines 143, 170, 203): Three separate `zcli service list` calls

### `validate-import.sh` (490 lines)

**Issues:**
1. **Undocumented function:** `check_post_import_status()` (lines 367-436) - callable via `--check-status` flag but not documented in help
2. **Hardcoded type lists** (lines 31-34)
3. **Color definitions duplicated** with recipe-search.sh

### Summary

| Command | Lines | Complexity | Help Quality | Error Handling |
|---------|-------|------------|--------------|----------------|
| compose | 636 | Moderate | Excellent | Good |
| verify_synthesis | ~150 | Low | Missing | Good |
| extend | 301 | Moderate | Excellent | Excellent |
| upgrade-to-full | 35 | Low | Minimal | Good |
| validate-import.sh | 491 | Moderate | Excellent | Excellent |

---

## Agent 5: Standalone Tools (Task ID: a868072)

### Files Analyzed
- `verify.sh` (367 lines)
- `status.sh` (280 lines)
- `validate-config.sh` (274 lines)
- `validate-import.sh` (490 lines)
- `recipe-search.sh` (1,557 lines)
- `mount.sh` (28 lines)

### Help Output Analysis

| Tool | Help Format | Colors | Error Format | Exit Codes |
|------|-------------|--------|--------------|------------|
| verify.sh | show_help() | No | ❌ symbol | 0/1/2 |
| status.sh | show_help() | No | ❌ symbol | 0/1/2 |
| validate-config.sh | inline cat | No | ✓/✗ symbols | 0/1 |
| validate-import.sh | show_help() + colors | Yes | ANSI colors | 0/1 |
| recipe-search.sh | show_help() + colors | Yes | ANSI colors | 0/1 |
| mount.sh | **None** | No | ⚠/✓ symbols | 0/1 |

### mount.sh --help - **BROKEN**

**Output Quality:** MISSING

**Critical Issue:** No `--help` flag handling. The script tried to create a mount for "--help" service.

**Current behavior:**
```
Creating mount for --help...
mkdir: /var/www: Permission denied
```

### status.sh (280 lines)

**Issues:**
1. **Unused function:** `parse_table_field()` (lines 51-58) - never called
2. **Hardcoded limits:** `head -20` throughout
3. **Redundant project ID function:** `get_project_id()` just echoes `$projectId`

**Dead Code:**
```bash
parse_table_field() {  # NEVER CALLED
    local line="$1"
    local field="$2"
    echo "$line" | sed 's/│/|/g' | cut -d'|' -f"$field" | xargs
}
```

### verify.sh (367 lines)

**Issues:**
1. **Unused variables:** `first_failure_body` captured but `body_snippet` used inconsistently
2. **Complex frontend detection** (lines 153-163) - SSH check that may time out
3. **Redundant frontend check:** `check_frontend` called twice (lines 341, 351)

### recipe-search.sh (1,557 lines) - **MAJOR AUDIT TARGET**

**Major Issues:**

1. **Massive hardcoded pattern blocks** (lines 530-846):
   - 316 lines of `extract_patterns()` with case statements
   - Each runtime has ~50 lines of hardcoded text

2. **Version lists hardcoded** (lines 849-903):
   - `get_versions()` has static version lists that will become stale

3. **Dead/Redundant Code:**
   - `has_local_patterns()` (line 1026-1029) - Always returns false

4. **Duplicated color definitions** (lines 8-15)

5. **Complex evidence file generation** (lines 1224-1462):
   - 238 lines for `create_evidence_file()`

6. **Temp file sprawl** - creates many temp files

### Priority Recommendations

**P0 (Critical):**
1. **Fix mount.sh --help** - Currently broken

**P1 (High):**
2. **Refactor recipe-search.sh** - Extract hardcoded patterns to data files
3. **Remove dead code in status.sh** - `parse_table_field()` function

**P2 (Medium):**
4. **Document or remove validate-import.sh --check-status** flag
5. **Extract color definitions to utils.sh**
6. **Consolidate temp file usage** in recipe-search.sh

---

# WAVE 2: CODE QUALITY AUDIT

---

## Agent 1: P1 Files (Task ID: a9a67ad)

### Files Audited
- `recipe-search.sh` (1,557 lines)
- `topics.sh` (1,348 lines)

### recipe-search.sh Analysis

#### 1. DUPLICATE CODE

**Issue 1.1: Duplicate Version Extraction Logic**
Lines 106-117 and 263-274 contain nearly identical code:
```bash
# Both functions have:
local versions
versions=$(echo "$response" | grep -oE "(go|golang|nodejs|...)@[0-9a-z.]+" | sort -u | head -10)
local versions_json="[]"
if [ -n "$versions" ]; then
    # identical JSON building logic
fi
```
**Recommendation:** Extract to a helper function

**Issue 1.2: Duplicate JSON Pattern Generation**
Lines 186-200 and 309-323 create nearly identical JSON structures for `/tmp/fetched_patterns.json`

#### 2. OVERLY COMPLEX FUNCTIONS

**`create_evidence_file()` - 183 lines (1224-1406)**
This function does too many things:
- Builds recipes JSON
- Checks fetched patterns
- Determines recipe type
- Builds runtime patterns
- Sets configuration guidance
- Creates evidence file
- Handles cleanup
- Provides next steps guidance

**`extract_patterns()` - 317 lines (530-846)**
One giant switch-case with embedded heredocs for each runtime

#### 3. DEAD CODE

**`has_local_patterns()` - Always returns false**
```bash
has_local_patterns() {
  # Always return false - recipe API and docs are the only sources of truth
  return 1
}
```
This function is never called.

#### 4. MAGIC STRINGS/NUMBERS

- Hardcoded API endpoints (lines 18-21)
- Pagination size `200` hardcoded
- Multiple hardcoded temp file paths

#### 5. INCONSISTENT PATTERNS

- Mixed use of `[[ ]]` and `[ ]`
- Inconsistent error message formatting

### topics.sh Analysis

#### 1. EXCESSIVE VERBOSITY / REPEATED INFORMATION

**Database Connection Pattern Repeated 5+ Times:**
- Line 185-186 (show_help_develop)
- Line 347-356 (show_help_verify)
- Line 789-795 (show_help_extend)
- And more...

**Triple-Kill Pattern Repeated 3 Times:**
- Line 147-148 (show_help_develop)
- Line 271-272 (show_help_deploy)
- Line 903 (show_help_cheatsheet)

**Browser Testing Instructions Repeated:**
- Lines 175-179, 326-336, 924-929

#### 2. OVERLY LONG HELP TOPICS

- `show_help_extend` - 207 lines (666-872)
- `show_help_bootstrap` - 226 lines (972-1197)

#### 3. INCONSISTENT FORMATTING

- Mixed use of emojis vs ASCII
- Inconsistent placeholder syntax (`{placeholder}` vs `<placeholder>` vs `NAME`)

### Summary

| Category | Issues Found | Priority |
|----------|-------------|----------|
| Duplicate Code | 2 major patterns | High |
| Complex Functions | 2 (235+ lines each) | High |
| Dead Code | 1 function | Low |
| Magic Strings | Multiple temp paths | Medium |

---

## Agent 2: P2 Files (Task ID: aeb94d1)

### Files Audited
- `transition.sh` (786 lines)
- `gates.sh` (743 lines)
- `compose.sh` (635 lines)
- `status.sh` (576 lines)

### CRITICAL ISSUES

#### Issue 1: Massive Code Duplication - Gate Checking Pattern
**Type:** Duplicate Code
**Files:** `gates.sh` (9 occurrences)
**Lines:** 14-16, 129-131, 264-266, 338-340, 404-406, 449-451, 521-523, 629-631, 688-690

**Code Snippet (repeated 9 times):**
```bash
local checks_passed=0
local checks_total=0
local all_passed=true
```

**Recommendation:** Extract gate checking logic into a reusable helper function

#### Issue 2: Duplicated Failure Count Extraction
**Type:** Duplicate Code
**Total Occurrences:** 10 (2 in gates.sh, 6 in status.sh, 1 in planning.sh, 1 in state.sh)

```bash
failures=$(jq -r '.failed // 0' "$DEV_VERIFY_FILE" 2>/dev/null)
```

#### Issue 3: Duplicated Phase Case Statement
**Type:** Duplicate Code / Architectural Issue
**Files:** `transition.sh` (lines 588-786), `status.sh` (lines 110-342)

**Lines in transition.sh:** ~200 lines of guidance output
**Lines in status.sh:** ~230 lines of guidance output

**Recommendation:** Consolidate all phase guidance into `transition.sh`'s `output_phase_guidance()` function

#### Issue 4: Redundant Session Check Pattern
**Type:** Duplicate Code
**Occurrences in gates.sh:** Lines 284, 358, 425, 469, 565, 661, 709
**Occurrences in status.sh:** Lines 41, 50, 61, 77, 143, 233, 244, 264, 287, 460, 466, 488, 495, 509

### MODERATE ISSUES

#### Issue 5: Overly Complex `cmd_transition_to()` Function
**Type:** Function Doing Too Much
**File:** `transition.sh`
**Lines:** 4-226 (222 lines)

#### Issue 6: Hardcoded Service Type List Duplication
**Type:** Hardcoded Values / Duplicate Code
**Files:** `compose.sh` lines 17-38, `utils.sh` lines 500-513

#### Issue 7: Inconsistent Error Handling in Gate Functions
Some gates output JSON to stderr for WIGGUM, others don't.

### Summary

| Issue | Type | Severity | Files Affected | Lines Impacted |
|-------|------|----------|----------------|----------------|
| Gate checking pattern duplication | Duplicate Code | HIGH | gates.sh | ~180 lines |
| Failure count extraction duplication | Duplicate Code | HIGH | gates.sh, status.sh, planning.sh, state.sh | ~20 lines (10 occurrences) |
| Phase guidance duplication | Duplicate Code | HIGH | transition.sh, status.sh | ~400 lines |
| Session check pattern duplication | Duplicate Code | MEDIUM | gates.sh, status.sh | ~60 lines |
| cmd_transition_to complexity | Function Too Large | MEDIUM | transition.sh | 222 lines |

**Total estimated duplicate/redundant code:** ~750-800 lines (~27% of total)

---

## Agent 3: P3-P4 Files (Task ID: a4d6d8a)

### Files Audited
- `utils.sh` (558 lines) - P3
- `state.sh` (523 lines) - P3
- `validate-import.sh` (490 lines) - P3
- `full.sh` (366 lines) - P3
- `verify.sh` (367 lines) - P4
- `extend.sh` (300 lines) - P4
- `status.sh` (280 lines) - P4
- `validate-config.sh` (274 lines) - P4

### HIGH PRIORITY: Duplicate `get_session()` in verify.sh

**Lines:** 90-96 in verify.sh

```bash
# DUPLICATE!
get_session() {
    if [ -f "/tmp/claude_session" ]; then
        cat "/tmp/claude_session"
    else
        echo "$(date +%Y%m%d%H%M%S)-$RANDOM-$RANDOM"
    fi
}
```

This duplicates the function from `utils.sh` (lines 235-239). The implementations differ:
- **verify.sh**: Falls back to random ID if session file missing
- **utils.sh**: Returns empty string if session file missing

**This inconsistency could cause session mismatches in evidence files.**

### utils.sh vs state.sh Overlap Analysis

| Function | In utils.sh | In state.sh | Issue |
|----------|-------------|-------------|-------|
| `get_session()` | Line 235 | Uses from utils.sh | OK |
| `get_mode()` | Line 241 | Uses from utils.sh | OK |
| `get_phase()` | Line 247 | Uses from utils.sh | OK |
| `set_phase()` | Line 255 | Uses from utils.sh | OK |
| `validate_phase()` | Line 259 | - | Should move to state.sh |
| Phase sequences | `PHASES` array (line 47) | `PHASES_*` arrays | **CONFLICT** |

**Recommendation:**
1. Keep basic file I/O in `utils.sh`
2. Move `validate_phase()` and `PHASES` to `state.sh`

### validate-import.sh vs validate-config.sh

**These should remain separate:**
- `validate-import.sh` runs at Gate 0.5 (before service creation)
- `validate-config.sh` runs at Gate 3 (before deployment)
- They validate different file formats with different rules

**However**, consider extracting shared utilities:
- Color definitions (duplicated in both)
- Service type detection (RUNTIME_TYPES, MANAGED_TYPES)
- Evidence file creation pattern

### status.sh wrapper vs commands/status.sh

**These are NOT redundant - they are complementary:**

| Aspect | `status.sh` | `lib/commands/status.sh` |
|--------|------------|--------------------------|
| Invocation | Direct script | Via workflow.sh |
| Focus | zcli infrastructure | Workflow state |
| User | Human checking deploy | Agent following workflow |

### Summary by Severity

**High Priority (1 issue):**
- Duplicate `get_session()` in verify.sh - Potential session ID mismatch

**Medium Priority (5 issues):**
- utils.sh/state.sh overlap
- state.sh manual JSON construction
- validate-import.sh long function
- state.sh redundant phase functions
- verify.sh late sourcing of utils.sh

---

## Agent 4: P5-P7 Files (Task ID: ada5349)

### Files Audited (12 files)

**P5 (200-250 lines):**
- `init.sh` (245 lines)
- `planning.sh` (242 lines)
- `iterate.sh` (219 lines)
- `workflow.sh` (200 lines)

**P6 (100-200 lines):**
- `context.sh` (196 lines)
- `discovery.sh` (152 lines)
- `retarget.sh` (118 lines)

**P7 (<100 lines):**
- `env.sh` (65 lines)
- `security-hook.sh` (61 lines)
- `mount.sh` (29 lines)
- `commands.sh` (20 lines)
- `help.sh` (9 lines)

### Key Findings

#### Duplicate Functions in iterate.sh
**Lines:** 138-149
`get_iteration()` and `set_iteration()` also exist in `utils.sh` (lines 342-369)

**Dead Code:** These are redundant with `utils.sh` implementations

#### Session ID Generation Duplication
Same pattern repeated 4 times in init.sh:
```bash
$(date +%Y%m%d%H%M%S)-$RANDOM-$RANDOM
```
Lines: 53-54, 97-98, 141-142, 227-228

#### Hardcoded Version Strings in planning.sh
Lines 34-74 contain hardcoded versions that will become stale

#### Missing Error Handling in env.sh
- Line 19: SSH failures silently return empty string
- Missing validation for variable name format

### Consolidation Recommendations

**Merge Loaders:**
`commands.sh` (20 lines) and `help.sh` (9 lines) could be merged

**Remove Duplicate Functions:**
`iterate.sh` contains `get_iteration()` and `set_iteration()` that duplicate `utils.sh`

**Merge Small Commands:**
- `retarget.sh` (118 lines) into `discovery.sh` (152 lines)

### Metrics Summary

| Category | Count | Lines | Notes |
|----------|-------|-------|-------|
| P5 Files | 4 | 906 | Core command logic |
| P6 Files | 3 | 466 | Support commands |
| P7 Files | 5 | 184 | Utilities and loaders |
| **Total** | **12** | **1,556** | Audit scope |

**Dead Code Identified:** 12 lines (duplicate functions in `iterate.sh`)

---

# WAVE 3: DOCUMENTATION AUDIT

---

## Agent 1: Main Documentation (Task ID: a77f72a)

### Files Audited
- `README.md` (621 lines)
- `CLAUDE.md` (165 lines)

### README.md Analysis

#### Accuracy Issues Found
1. **Missing Gate 2 details**: Import validation gate (services_imported) exists but could use more documentation
2. **Evidence file naming**: `services_imported.json` documented at line 394, consistent with Gate 2
3. **Missing command**: `state` command not explicitly listed (though `show` command documented)
4. **Hardcoded path**: Line 8 has absolute path `/Users/fxck/www/zcp/`
5. **Intent file location**: Documented in `.zcp/state/` AND as `claude_intent.txt` - both locations mentioned

#### Condensation Recommendations

| Section | Current Lines | Recommended | Savings |
|---------|--------------|-------------|---------|
| Purpose/Core Principle | 14 | 6 | 57% |
| Architecture | 19 | 8 | 58% |
| Bootstrap vs Standard | 46 | 20 | 57% |
| Typical Workflow | 54 | 25 | 54% |
| Gates (dedupe) | 80 | 35 | 56% |
| Environment Context | 42 | 0 (move to CLAUDE.md only) | 100% |
| Gotchas/Deployment | 25 | 0 (already in CLAUDE.md) | 100% |

**Estimated total reduction: ~150 lines (24%)**

### CLAUDE.md Analysis

#### Accuracy Issues
1. **Non-existent help topic (Line 164)**: `synthesis` help topic is listed but doesn't exist
2. **Line 19**: COMPOSE transition instruction may confuse agents

### Cross-File Duplication

**Same information in both files:**
1. Core principle "dev for iterating, stage for validation" - similar concept, different wording
2. Environment Context / Service Types - ~30 lines overlap
3. Gotchas Table - 6 shared entries (~15 lines overlap)
4. Variable Patterns - different contexts, minimal true duplication
5. Deployment Pattern - similar guidance

**Current overlap: ~40-50 lines of actual duplicate content**

### Help Topics Mismatch (Not Previously Documented)
- README.md (line 370-372): Lists 12 help topics
- CLAUDE.md (line 152-164): Lists 13 topics (includes `cheatsheet` and non-existent `synthesis`)
- This inconsistency should be resolved

### Recommendations

**README.md (Aggressive Cuts):**
1. Remove all duplicate content that's in CLAUDE.md
2. Consolidate Gates section - keep only the table
3. Add missing Gate 0.5 documentation
4. Add missing `state` and `--guidance` command documentation
5. **Target: 400 lines (35% reduction)**

**CLAUDE.md (Minor Fixes):**
1. Remove non-existent `synthesis` help topic from list
2. Clarify COMPOSE transition instruction
3. Add `validate-import.sh` reference for import issues
4. **Target: 155 lines (6% reduction)**

---

## Agent 2: Help System Documentation (Task ID: a86aef4)

### Files Audited
- `help.sh` (9 lines)
- `full.sh` (366 lines)
- `topics.sh` (1,348 lines)

**Total: 1,723 lines for help alone (16% of codebase!)**

### Duplication with CLAUDE.md

| Help Location | CLAUDE.md Section | Overlap % |
|---------------|-------------------|-----------|
| full.sh Orientation | "Your Position" | 80% |
| full.sh Variables | "Variables" | 90% |
| full.sh Troubleshooting | "Gotchas" | 70% |
| cheatsheet Critical Rules | "Critical Rules" | 95% |
| cheatsheet Workflow Commands | "Start Here" table | 90% |
| vars topic | "Variables" | 60% |

### Topic Analysis

| Topic | Lines | Redundancy Level |
|-------|-------|------------------|
| `discover` | 37 | HIGH - overlaps full.sh Phase: Discover |
| `develop` | 124 | HIGH - 50% overlaps full.sh |
| `deploy` | 74 | HIGH - nearly identical to full.sh Deploy section |
| `verify` | 66 | MEDIUM |
| `done` | 41 | LOW - unique content |
| `vars` | 144 | MEDIUM - overlaps full.sh Variables + CLAUDE.md |
| `services` | 113 | LOW - mostly unique |
| `extend` | 208 | MEDIUM - some overlap with bootstrap |
| `cheatsheet` | 98 | HIGH - duplicates CLAUDE.md and full.sh |
| `bootstrap` | 227 | MEDIUM - overlaps extend topic |
| `import-validation` | 149 | LOW - specialized content |
| `trouble` | N/A | 100% duplicate (sed extraction) |
| `example` | N/A | 100% duplicate (sed extraction) |
| `gates` | N/A | 100% duplicate (sed extraction) |

### Cross-Topic Duplications

1. **Triple-Kill Pattern (appears 4+ times)**
2. **zerops.yaml Structure (appears 3 times)**
3. **zcli Login Command (appears 4+ times)**
4. **Browser Testing Block (appears 3 times)**
5. **Variable Patterns Table (appears in 3 locations)**

### Condensation Recommendations

| Strategy | Savings |
|----------|---------|
| Eliminate cheatsheet topic entirely | ~100 lines |
| Merge trouble, example, gates topics | ~150 lines |
| Condense deploy topic | ~40 lines |
| Merge extend and bootstrap | ~100 lines |
| Reduce verbosity in develop topic | ~60 lines |
| Remove duplicate examples | ~80 lines |

### Summary

| Component | Current Lines | After Condensation | Savings |
|-----------|---------------|-------------------|---------|
| help.sh | 9 | 9 | 0 |
| full.sh | 366 | 200 | 166 |
| topics.sh | 1,348 | 500 | 848 |
| **TOTAL** | **1,723** | **709** | **1,014 (59%)** |

---

# WAVE 4: ARCHITECTURE AUDIT

---

## Agent 1: Architecture Consolidation Analysis (Task ID: ade6476)

### Current Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           WORKFLOW.SH (201 LOC)                                  │
│                              Main Router                                         │
│  Commands → case statement → delegates to lib/commands/*.sh                      │
└─────────────────────────────────────────────────────────────────────────────────┘
                                      │
            ┌─────────────────────────┼─────────────────────────┐
            ▼                         ▼                         ▼
┌───────────────────────┐  ┌───────────────────────┐  ┌───────────────────────┐
│   LIB/COMMANDS.SH     │  │     LIB/UTILS.SH      │  │    LIB/GATES.SH       │
│      (21 LOC)         │  │       (559 LOC)       │  │      (743 LOC)        │
│  Pure loader          │  │  State paths, session │  │  Gate check functions │
│  Sources all commands │  │  Evidence handling    │  │  10+ gate checks      │
└───────────────────────┘  └───────────────────────┘  └───────────────────────┘
```

### Structural Analysis

#### Command Module Structure (10 command files)

| File | LOC | Necessity | Merge Candidate |
|------|-----|-----------|-----------------|
| transition.sh | 786 | KEEP | Core routing logic |
| compose.sh | 635 | KEEP | Bootstrap-specific |
| status.sh | 576 | MERGE | Overlaps with standalone |
| extend.sh | 300 | KEEP | Service import |
| init.sh | 245 | MERGE | Could merge with transition |
| planning.sh | 242 | KEEP | Service planning |
| iterate.sh | 219 | MERGE | Small continuity command |
| context.sh | 196 | MERGE | Very thin layer |
| discovery.sh | 152 | MERGE | Could be part of init flow |
| retarget.sh | 118 | MERGE | Trivial command |

**Recommendation:** Consolidate to 5-6 files

### Validation Tools Unification

**Current State:**
- `validate-config.sh` (274 LOC) - validates zerops.yml
- `validate-import.sh` (490 LOC) - validates import.yml

**Recommendation:** Create unified **validation.sh** (estimated 500 LOC combined)
**Estimated Savings:** 250-300 LOC

### Help System Over-Engineering

**Current: 1,723 LOC for help alone (16% of codebase!)**

**Recommendation:**
1. Keep essential help in full.sh (~200 LOC)
2. Phase guidance stays in transition.sh
3. Remove duplicated topic content, link to CLAUDE.md
4. Target: 400 LOC for help system

**Estimated Savings:** 1,300 LOC

### Gate System Complexity

**Current: 10+ gates in gates.sh (743 LOC)**

**Recommendation:** Data-driven gates instead of 10 separate functions
```bash
GATE_DEFS=(
    "INIT:DISCOVER:recipe_review|check_gate_init"
    "DISCOVER:DEVELOP:discovery|check_gate_discover"
    # ...
)
check_gate() {
    local from=$1 to=$2
    # Generic implementation
}
```

**Estimated Savings:** 400 LOC

### Evidence File Proliferation

**Current: 15+ evidence file types**

**Recommendation:** Consolidate to core set:
- `session.json` - All session metadata
- `evidence.json` - All gate evidence
- `discovery.json` - Keep (critical service info)
- `workflow_state.json` - Keep (WIGGUM orchestration)

### Estimated LOC Savings

| Category | Current LOC | Projected LOC | Savings |
|----------|-------------|---------------|---------|
| Commands (10 files) | 3,469 | 2,600 | 869 |
| Help System | 1,723 | 400 | 1,323 |
| Gates | 743 | 350 | 393 |
| Validation Tools | 764 | 500 | 264 |
| Utils/State | 1,083 | 950 | 133 |
| **TOTAL** | **7,782** | **4,800** | **~2,982** |

**Net reduction: ~38% of codebase**

### Recommended Consolidated Structure

```
.zcp/
├── workflow.sh              # Main router (200 LOC)
├── lib/
│   ├── core.sh              # Merge: utils.sh + commands.sh (500 LOC)
│   ├── state.sh             # WIGGUM state management (500 LOC)
│   ├── gates.sh             # Data-driven gates (350 LOC)
│   ├── help.sh              # Streamlined help (400 LOC)
│   └── commands/
│       ├── flow.sh          # Merge: init + transition + iterate (700 LOC)
│       ├── status.sh        # show, state, complete, reset (400 LOC)
│       ├── services.sh      # Merge: discovery + extend + planning (500 LOC)
│       └── synthesis.sh     # compose + verify_synthesis (500 LOC)
├── tools/
│   ├── validate.sh          # Unified validation (500 LOC)
│   ├── verify.sh            # Endpoint verification (350 LOC)
│   ├── deploy-status.sh     # Deployment wait/polling (250 LOC)
│   └── recipe-search.sh     # Recipe search (1,200 LOC after cleanup)
└── mount.sh                 # Keep as-is (28 LOC) + add --help handler

TOTAL: ~5,200 LOC (from ~10,500)
```

### Bold Recommendations

1. **Eliminate `lib/commands.sh` entirely** - Source commands directly in workflow.sh
2. **Kill separate evidence files** - One unified `evidence.json`
3. **Help should reference CLAUDE.md** - Stop duplicating content
4. **Data-driven gates** - Replace 10 functions with one generic function + config
5. **Rename `.zcp/status.sh` to `.zcp/deploy-status.sh`** - Eliminates confusion

---

# IMPLEMENTATION PRIORITY

## Quick Fixes (< 10 minutes each)

1. **Fix macOS `head -n -1`** → Replace with `sed '$d'` in topics.sh lines 35, 38
2. **Add --help handler to mount.sh** → 3 lines at top
3. **Remove duplicate `get_session()`** → Delete from verify.sh, source utils.sh
4. **Fix or remove synthesis reference** → Update CLAUDE.md:164

## Medium Effort Refactoring

1. **Consolidate phase guidance** → Move status.sh guidance to transition.sh
2. **Extract gate checking helper** → Create generic function for 9× repeated pattern
3. **Merge small command files** → Combine iterate, context, retarget, discovery
4. **Unify validation tools** → Combine validate-config.sh and validate-import.sh

## Large Refactoring Projects

1. **Condense help system** → From 1,723 to ~700 lines
2. **Data-driven gates** → Replace 10 functions with config + generic checker
3. **Consolidate evidence files** → From 15+ to 4 core files
4. **Refactor recipe-search.sh** → Extract hardcoded patterns to data files

---

# APPENDIX: FILE LOCATIONS

## Core Files
- `/Users/fxck/www/zcp/zcp/.zcp/workflow.sh` (200 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/lib/utils.sh` (558 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/lib/state.sh` (523 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/lib/gates.sh` (743 lines)

## Command Files
- `/Users/fxck/www/zcp/zcp/.zcp/lib/commands/transition.sh` (786 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/lib/commands/compose.sh` (635 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/lib/commands/status.sh` (576 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/lib/commands/extend.sh` (300 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/lib/commands/init.sh` (245 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/lib/commands/planning.sh` (242 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/lib/commands/iterate.sh` (219 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/lib/commands/context.sh` (196 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/lib/commands/discovery.sh` (152 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/lib/commands/retarget.sh` (118 lines)

## Standalone Tools
- `/Users/fxck/www/zcp/zcp/.zcp/recipe-search.sh` (1,557 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/validate-import.sh` (490 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/verify.sh` (367 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/status.sh` (280 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/validate-config.sh` (274 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/mount.sh` (28 lines)

## Help System
- `/Users/fxck/www/zcp/zcp/.zcp/lib/help/topics.sh` (1,348 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/lib/help/full.sh` (366 lines)
- `/Users/fxck/www/zcp/zcp/.zcp/lib/help.sh` (9 lines)

## Documentation
- `/Users/fxck/www/zcp/README.md` (621 lines)
- `/Users/fxck/www/zcp/zcp/CLAUDE.md` (165 lines)

---

*Generated by 12 parallel audit agents on 2026-01-24*
*Verified and corrected by 12 parallel verification agents on 2026-01-24*
*Triple-verified with 7 corrections applied on 2026-01-24*
