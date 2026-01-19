# ZCP Agent Workflow System: Improvement Analysis

> **Analysis Date:** 2026-01-19
> **Scope:** Context flow, state recovery, and agent resilience
> **Perspective:** Unbiased evaluation from AI agent operational standpoint

---

## Executive Summary

The Zerops Control Plane (ZCP) workflow system is a well-designed phase-gated orchestration framework for AI agents. Its core design principle of **"knowledge lives in tools via `--help`"** is sound for surviving context compaction. However, critical analysis reveals several gaps that can cause agent failures when entering workflows mid-flow or after context loss.

**Key Finding:** The system optimizes for *sequential flow* but underserves *context recovery scenarios* which are common in real agent operation.

---

## Part 1: Verified Findings (Triple-Confirmed)

### 1.1 CLAUDE.md Content Analysis

**File:** `zcp/CLAUDE.md` (89 lines)

| Content Category | Present | Depth |
|-----------------|---------|-------|
| Decision table (which command to run) | Yes | Complete |
| Platform orientation (ZCP, SSHFS, SSH) | Yes | Basic |
| Service types (Runtime vs Managed) | Yes | Basic |
| Variable patterns | Yes | Minimal (4 lines) |
| Common gotchas table | Yes | 10 entries |
| Workflow phases | No | - |
| Gate requirements | No | - |
| Triple-kill pattern | No | - |
| deployFiles checklist | No | - |
| Functional testing rules | No | - |
| run_in_background reminder | No | - |

**Confirmed:** CLAUDE.md is intentionally minimal. It assumes the agent will call tools for detailed guidance.

### 1.2 Phase Transition Output Analysis

Each `transition_to {PHASE}` outputs phase-specific guidance:

| Phase | Lines | Key Content | Missing from CLAUDE.md |
|-------|-------|-------------|------------------------|
| DISCOVER | 20 | zcli login, service list, create_discovery | zcli scope warning |
| DEVELOP | 80 | Full dev loop, functional testing, triple-kill, run_in_background | Everything |
| DEPLOY | 53 | deployFiles checklist, zerops.yaml structure, zcli push | Everything |
| VERIFY | 34 | verify.sh, browser testing, HTTP 200 warning | Everything |
| DONE | 8 | Just `complete` command | - |

**Confirmed:** Critical operational knowledge is only available in phase guidance outputs, not in always-loaded context.

### 1.3 `show` Command Output Analysis

The `show` command provides:
1. Current session/mode/phase
2. Evidence status (exists/stale/missing for each file)
3. Discovered service names (from discovery.json)
4. Phase-specific next steps with **actual service names substituted**

**Confirmed:** `show` is the primary context recovery mechanism, but:
- Does NOT include phase-specific operational details
- Does NOT repeat critical rules (triple-kill, deployFiles, etc.)
- Assumes agent knows what each phase requires

### 1.4 Context Loss Scenarios

| Scenario | What Agent Loses | Recovery Path |
|----------|------------------|---------------|
| Context compaction | All guidance except CLAUDE.md | Must run `show`, then phase transition |
| Subagent spawn | Everything | Must run `show` from scratch |
| Long-running task | Previous phase outputs | Must re-run transition or `show` |
| IDE session restart | ZCP state preserved, agent context lost | Must run `show` |

**Confirmed:** All scenarios require manual recovery steps. No automatic context restoration.

---

## Part 2: Gap Analysis

### Gap 1: No Automatic Context Recovery Directive

**Problem:** CLAUDE.md tells agent to "Run one. READ its output completely. FOLLOW the rules it shows." but doesn't tell agent what to do when entering mid-flow.

**Evidence:** Line 14 of CLAUDE.md: "Run one. READ its output completely."
- This assumes fresh start, not recovery.

**Impact:** Agent entering mid-flow may:
- Assume service names (instead of reading discovery)
- Skip critical pre-phase steps
- Miss gate requirements

### Gap 2: Phase Guidance Not Repeatable Without Transition

**Problem:** To get DEVELOP phase guidance, agent must call `transition_to DEVELOP`. But if already in DEVELOP phase, this succeeds silently without re-outputting guidance.

**Evidence:** `transition.sh` line 167-168:
```bash
set_phase "$target_phase"
output_phase_guidance "$target_phase"
```
When already in phase, the transition may succeed without re-outputting (depending on mode).

**Impact:** Agent that loses context mid-DEVELOP cannot easily retrieve the detailed guidance.

### Gap 3: Critical Rules Not in Always-Loaded Context

**Problem:** The most frequently needed rules are only in phase-specific outputs:

| Rule | Where It Lives | Should Be In |
|------|---------------|--------------|
| Triple-kill pattern | DEVELOP, DEPLOY guidance | CLAUDE.md or quick reference |
| run_in_background=true | DEVELOP guidance | CLAUDE.md Gotchas |
| HTTP 200 != working | DEVELOP, VERIFY guidance | CLAUDE.md |
| deployFiles checklist | DEPLOY guidance | CLAUDE.md |
| zcli push from dev container | DEPLOY guidance | CLAUDE.md |

**Impact:** Agent must remember these rules across context compaction, or re-run transitions.

### Gap 4: `show` Doesn't Include Phase Operational Details

**Problem:** `show` provides excellent state recovery but minimal operational guidance.

**Evidence:** DEVELOP phase in `show` (lines 127-141):
```
⚠️  See DISCOVERED SERVICES above for SSH vs client tool rules

1. Build and test on dev ($dev_name):
   ssh $dev_name "{build_cmd} && ./{binary}"
2. Verify endpoints:
   .zcp/verify.sh $dev_name {port} / /api/...
3. Then: .zcp/workflow.sh transition_to DEPLOY
```

Compare to DEVELOP phase guidance (80 lines) which includes:
- Complete develop loop
- Triple-kill pattern
- run_in_background reminder
- Functional testing examples (backend, frontend, logs, database)
- DO NOT / Deploy ONLY when rules

**Impact:** Agent running `show` gets minimal guidance, may miss critical steps.

### Gap 5: No Quick Reference for Common Operations

**Problem:** Agent frequently needs:
- Triple-kill command
- zcli login command
- agent-browser sequence
- verify.sh syntax

These require remembering or re-running phase transitions.

**Impact:** Increased token usage, potential for errors when reconstructing commands.

### Gap 6: Evidence File Paths Not in CLAUDE.md

**Problem:** Evidence files are critical for understanding state, but their paths aren't documented in CLAUDE.md.

**Evidence:** Paths are defined in `utils.sh`:
```bash
SESSION_FILE="/tmp/claude_session"
MODE_FILE="/tmp/claude_mode"
PHASE_FILE="/tmp/claude_phase"
DISCOVERY_FILE="/tmp/discovery.json"
DEV_VERIFY_FILE="/tmp/dev_verify.json"
STAGE_VERIFY_FILE="/tmp/stage_verify.json"
DEPLOY_EVIDENCE_FILE="/tmp/deploy_evidence.json"
```

**Impact:** Agent cannot directly inspect state without running `show`.

### Gap 7: No "Where Am I?" Quick Check

**Problem:** Agent needs multiple commands to understand current state:
1. Check if session exists
2. Check current phase
3. Check evidence status
4. Get service names

`show` does all this, but there's no single-line quick check.

**Impact:** Minor - `show` is sufficient, but could be more ergonomic.

---

## Part 3: Improvement Recommendations

### Tier 1: High Impact, Low Effort

#### 1.1 Add Context Recovery Section to CLAUDE.md

**Location:** After "Start Here" section

```markdown
## Lost Context? Run This

If you're resuming work or unsure of current state:
```bash
.zcp/workflow.sh show    # Current state, evidence, next steps
```

Then follow the NEXT STEPS section in the output.
```

**Rationale:** Explicit recovery directive for mid-flow entry.

#### 1.2 Add Critical Rules Summary to CLAUDE.md

**Location:** New section before Reference

```markdown
## Critical Rules (memorize these)

| Rule | Pattern |
|------|---------|
| Kill orphan processes | `ssh {svc} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true'` |
| Long-running commands | Set `run_in_background=true` in Bash tool |
| HTTP 200 | Does NOT mean feature works. Check response content, logs, console |
| Deploy from | Dev container (`ssh {dev} "zcli push..."`), NOT from ZCP |
| deployFiles | Must include ALL artifacts. Check before every deploy |
```

**Rationale:** Most common failure points should be in always-loaded context.

#### 1.3 Add `--guidance` Flag to `show`

**Implementation:** Add to `cmd_show` in `status.sh`:

```bash
if [ "$1" = "--guidance" ]; then
    cmd_show
    echo ""
    output_phase_guidance "$(get_phase)"
fi
```

**Usage:** `.zcp/workflow.sh show --guidance`

**Rationale:** Single command for full context recovery.

### Tier 2: Medium Impact, Medium Effort

#### 2.1 Add Evidence File Paths to CLAUDE.md

**Location:** In Reference section

```markdown
## Evidence Files

| File | Purpose |
|------|---------|
| `/tmp/claude_session` | Session ID |
| `/tmp/claude_phase` | Current phase |
| `/tmp/discovery.json` | Dev/stage service mapping |
| `/tmp/dev_verify.json` | Dev verification results |
| `/tmp/stage_verify.json` | Stage verification results |
| `/tmp/deploy_evidence.json` | Deployment completion proof |
```

**Rationale:** Enables direct state inspection without tool calls.

#### 2.2 Create `recover` Command

**New command:** `.zcp/workflow.sh recover`

**Behavior:**
1. Run `show`
2. Output current phase guidance
3. Output critical rules reminder

**Rationale:** Single command for complete context recovery.

#### 2.3 Enhance `show` Next Steps Section

**Current:** Minimal 3-4 line guidance per phase
**Proposed:** Include critical reminders for current phase

Example for DEVELOP:
```
💡 NEXT STEPS
━━━━━━━━━━━━━━━━━━━━━━━━

⚠️  REMINDERS FOR DEVELOP PHASE:
   • Kill existing: ssh {dev} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true'
   • Long commands: run_in_background=true
   • HTTP 200 ≠ working. Check content, logs, console

1. Build and test on dev ($dev_name):
   ...
```

**Rationale:** Critical reminders visible after every `show`.

### Tier 3: Low Priority / Nice to Have

#### 3.1 Add `cheatsheet` Topic Help

**New topic:** `.zcp/workflow.sh --help cheatsheet`

**Content:** One-page quick reference with all common commands and patterns.

#### 3.2 Add Phase Guidance Re-Output

**New behavior:** When already in a phase, `transition_to` re-outputs guidance with notice:

```
⚠️  Already in DEVELOP phase. Showing guidance:

✅ Phase: DEVELOP
...
```

#### 3.3 Single-Line State Check

**New command:** `.zcp/workflow.sh state`

**Output:** `DEVELOP | full | dev=appdev stage=appstage | dev_verify=0_failures`

**Rationale:** Quick check without full `show` output.

---

## Part 4: Implementation Priority Matrix

| ID | Improvement | Impact | Effort | Priority |
|----|-------------|--------|--------|----------|
| 1.1 | Context recovery section | High | Low | P0 |
| 1.2 | Critical rules in CLAUDE.md | High | Low | P0 |
| 1.3 | `show --guidance` flag | High | Low | P0 |
| 2.2 | `recover` command | Medium | Medium | P1 |
| 2.3 | Enhanced `show` reminders | Medium | Medium | P1 |
| 2.1 | Evidence paths in CLAUDE.md | Low | Low | P2 |
| 3.1 | Cheatsheet topic | Low | Medium | P3 |
| 3.2 | Transition re-output | Low | Low | P3 |
| 3.3 | Single-line state | Low | Low | P3 |

---

## Part 5: Risk Assessment

### Current System Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Agent deploys without testing dev | Medium | High | Gate blocks, but agent may work around |
| Agent forgets deployFiles | High | Medium | Manual checklist in DEPLOY phase |
| Agent uses wrong service names | Low | High | Discovery system prevents |
| Context loss causes wrong phase ops | Medium | Medium | `show` available but not automatic |
| Agent reuses stale evidence | Low | Low | Session scoping prevents |

### Post-Improvement Risks

With P0 improvements implemented:
- Context loss risk: **Reduced** (explicit recovery directive)
- deployFiles risk: **Reduced** (in always-loaded context)
- Wrong operations risk: **Reduced** (critical rules memorized)

---

## Part 6: Conclusion

The ZCP workflow system is architecturally sound. The "knowledge in tools" approach is correct for LLM agent operation. However, the current implementation assumes agents follow a happy path from init to done without context loss.

**Primary recommendation:** Implement P0 improvements (1.1, 1.2, 1.3) to:
1. Make context recovery explicit and discoverable
2. Put critical failure-causing rules in always-loaded context
3. Enable single-command full context recovery

**Estimated effort:** 1-2 hours for all P0 improvements.

**Expected outcome:** Significant reduction in agent failures from context loss scenarios, without increasing CLAUDE.md size substantially (adding ~30 lines).

---

## Appendix: Content Size Comparison

| File | Current Lines | With P0 | Change |
|------|---------------|---------|--------|
| CLAUDE.md | 89 | ~120 | +35% |
| Full help | 363 | 363 | 0% |
| Phase guidance (all) | ~200 | ~200 | 0% |

The increase to CLAUDE.md is justified because:
1. These are the rules agents fail on most often
2. They need to survive context compaction
3. They're actionable (commands, not concepts)
