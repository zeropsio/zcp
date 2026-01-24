# ZCP Deep System Audit Plan

## Project Overview

| Metric | Value |
|--------|-------|
| Total LOC | 11,345 |
| Shell Scripts | 26 files (10,523 LOC) |
| Documentation | 2,500 lines (help + md) |
| Quality Gates | 10+ gates |
| Command Modules | 10 files |
| Core Libraries | 8 files |
| Git Commits | 46 (30+ iterations) |

---

## Audit Strategy

### Phase 1: Command Surface Audit
Test every single command and flag combination, document outputs, find inconsistencies.

### Phase 2: Code Quality Audit
Review each file for dead code, redundant logic, outdated patterns.

### Phase 3: Documentation Audit
Find excessive/outdated text, duplicated content, inconsistencies.

### Phase 4: Architecture Audit
Identify structural improvements, consolidation opportunities.

---

## Phase 1: Command Surface Audit

### 1.1 Main Workflow Commands

```
.zcp/workflow.sh init
.zcp/workflow.sh init --dev-only
.zcp/workflow.sh init --hotfix
.zcp/workflow.sh --quick
.zcp/workflow.sh show
.zcp/workflow.sh show --full
.zcp/workflow.sh recover
.zcp/workflow.sh state
.zcp/workflow.sh reset
.zcp/workflow.sh reset --keep-discovery
.zcp/workflow.sh transition_to {phase}
.zcp/workflow.sh transition_to --back {phase}
.zcp/workflow.sh create_discovery
.zcp/workflow.sh refresh_discovery
.zcp/workflow.sh plan_services
.zcp/workflow.sh compose --runtime {rt}
.zcp/workflow.sh compose --runtime {rt} --services {s}
.zcp/workflow.sh verify_synthesis
.zcp/workflow.sh extend {import.yml}
.zcp/workflow.sh upgrade-to-full
.zcp/workflow.sh iterate
.zcp/workflow.sh iterate --to {PHASE}
.zcp/workflow.sh retarget {dev|stage} {id} {name}
.zcp/workflow.sh intent [text]
.zcp/workflow.sh note [text]
.zcp/workflow.sh validate_config {yml}
.zcp/workflow.sh validate_code {yml}
.zcp/workflow.sh record_deployment {svc}
.zcp/workflow.sh --help
.zcp/workflow.sh --help {topic}
```

**Help Topics:**
- discover, develop, deploy, verify, done
- vars, services, trouble, example, gates
- extend, bootstrap, synthesis

### 1.2 Standalone Tools

```
.zcp/verify.sh {service} {port} {endpoints...}
.zcp/verify.sh --help
.zcp/status.sh {service}
.zcp/status.sh --wait {service}
.zcp/status.sh --help
.zcp/validate-config.sh {yml}
.zcp/validate-config.sh --help
.zcp/validate-import.sh {yml}
.zcp/validate-import.sh --help
.zcp/recipe-search.sh quick {runtime}
.zcp/recipe-search.sh search {query}
.zcp/recipe-search.sh docs {topic}
.zcp/recipe-search.sh --help
.zcp/mount.sh {service}
.zcp/mount.sh --help
```

### 1.3 Test Matrix

| Command | Tested | Output OK | Errors Found | Notes |
|---------|--------|-----------|--------------|-------|
| workflow.sh init | [ ] | [ ] | | |
| workflow.sh --quick | [ ] | [ ] | | |
| workflow.sh show | [ ] | [ ] | | |
| ... | | | | |

---

## Phase 2: Code Quality Audit

### 2.1 Files by Size (Audit Priority)

| Priority | File | Lines | Audit Status |
|----------|------|-------|--------------|
| P1 | recipe-search.sh | 1,557 | [ ] |
| P1 | help/topics.sh | 1,348 | [ ] |
| P2 | lib/commands/transition.sh | 786 | [ ] |
| P2 | lib/gates.sh | 743 | [ ] |
| P2 | lib/commands/compose.sh | 635 | [ ] |
| P2 | lib/commands/status.sh | 576 | [ ] |
| P3 | lib/utils.sh | 558 | [ ] |
| P3 | lib/state.sh | 523 | [ ] |
| P3 | validate-import.sh | 490 | [ ] |
| P3 | help/full.sh | 366 | [ ] |
| P4 | verify.sh | 367 | [ ] |
| P4 | lib/commands/extend.sh | 300 | [ ] |
| P4 | status.sh | 280 | [ ] |
| P4 | validate-config.sh | 274 | [ ] |
| P5 | lib/commands/init.sh | 245 | [ ] |
| P5 | lib/commands/planning.sh | 242 | [ ] |
| P5 | lib/commands/iterate.sh | 219 | [ ] |
| P5 | workflow.sh | 200 | [ ] |
| P6 | lib/commands/context.sh | 196 | [ ] |
| P6 | lib/commands/discovery.sh | 152 | [ ] |
| P6 | lib/commands/retarget.sh | 118 | [ ] |
| P7 | lib/env.sh | 65 | [ ] |
| P7 | lib/security-hook.sh | 61 | [ ] |
| P7 | mount.sh | ~80 | [ ] |
| P7 | lib/commands.sh | 20 | [ ] |
| P7 | lib/help.sh | 9 | [ ] |

### 2.2 Code Smell Checklist

- [ ] Dead code (unreachable, unused functions)
- [ ] Duplicate code (copy-paste across files)
- [ ] Inconsistent patterns (different ways of doing same thing)
- [ ] Hardcoded values that should be configurable
- [ ] Outdated comments that don't match code
- [ ] Overly complex conditionals
- [ ] Missing error handling
- [ ] Excessive echo/printf statements
- [ ] Inconsistent naming conventions
- [ ] Unused variables
- [ ] Redundant condition checks
- [ ] Functions doing too much
- [ ] Magic numbers/strings

---

## Phase 3: Documentation Audit

### 3.1 Documentation Files

| File | Lines | Audit Status | Issues Found |
|------|-------|--------------|--------------|
| README.md | 621 | [ ] | |
| CLAUDE.md | 165 | [ ] | |
| help/topics.sh | 1,348 | [ ] | |
| help/full.sh | 366 | [ ] | |

### 3.2 Documentation Issues to Find

- [ ] Outdated information (doesn't match current code)
- [ ] Duplicate content (same thing explained multiple times)
- [ ] Excessive verbosity (can be condensed)
- [ ] Missing information (referenced but not explained)
- [ ] Inconsistent terminology
- [ ] Broken examples
- [ ] Dead links

---

## Phase 4: Architecture Audit

### 4.1 Structural Questions

- [ ] Can any command modules be consolidated?
- [ ] Are there circular dependencies?
- [ ] Is the help system over-engineered?
- [ ] Can gate logic be simplified?
- [ ] Is state management too complex?
- [ ] Are there too many evidence file types?

### 4.2 Potential Consolidations

| Candidate | Files | Potential Merge |
|-----------|-------|-----------------|
| Help system | topics.sh + full.sh | Single help file? |
| Validation | validate-config.sh + validate-import.sh | Unified validator? |
| Status tools | status.sh (wrapper) + commands/status.sh | One file? |
| ... | | |

---

## Execution Plan

### Parallel Subagent Strategy

**Wave 1: Command Testing (5 agents)**
1. Agent 1: Test all `workflow.sh` commands (init variants, show, state, reset)
2. Agent 2: Test all transition and phase commands
3. Agent 3: Test all discovery/planning commands
4. Agent 4: Test all synthesis commands (compose, extend, verify_synthesis)
5. Agent 5: Test all standalone tools (verify, status, validate-*, recipe-search)

**Wave 2: Code Quality (4 agents)**
1. Agent 1: Audit P1 files (recipe-search.sh, help/topics.sh)
2. Agent 2: Audit P2 files (transition.sh, gates.sh, compose.sh, commands/status.sh)
3. Agent 3: Audit P3-P4 files
4. Agent 4: Audit P5-P7 files

**Wave 3: Documentation (2 agents)**
1. Agent 1: Audit README.md and CLAUDE.md
2. Agent 2: Audit help system (topics.sh, full.sh)

**Wave 4: Cross-cutting Analysis (1 agent)**
1. Agent 1: Architecture review, consolidation opportunities

---

## Findings Log

### Command Issues
| Command | Issue | Severity | Fix |
|---------|-------|----------|-----|
| | | | |

### Code Issues
| File:Line | Issue | Severity | Fix |
|-----------|-------|----------|-----|
| | | | |

### Documentation Issues
| File | Issue | Severity | Fix |
|------|-------|----------|-----|
| | | | |

### Architecture Issues
| Component | Issue | Severity | Recommendation |
|-----------|-------|----------|----------------|
| | | | |

---

## Progress Tracker

- [ ] Phase 1: Command Surface Audit
  - [ ] Wave 1: Command Testing (5 parallel agents)
- [ ] Phase 2: Code Quality Audit
  - [ ] Wave 2: Code Quality (4 parallel agents)
- [ ] Phase 3: Documentation Audit
  - [ ] Wave 3: Documentation (2 parallel agents)
- [ ] Phase 4: Architecture Audit
  - [ ] Wave 4: Cross-cutting Analysis (1 agent)

---

## Session Info

**Started:** 2026-01-24
**Status:** Planning Complete
**Next Action:** Execute Wave 1 - Command Testing
