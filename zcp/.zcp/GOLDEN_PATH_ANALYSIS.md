# ZCP Workflow Golden Path Analysis

## Overview

The Zerops Workflow is a **gated state machine** that enforces a structured process for developing and deploying code. Each phase transition requires **evidence** — JSON files proving specific tasks were completed successfully.

## Phase Flow

```
INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE
  │         │          │         │         │
  └─Gate 0  └─Gate 1   └─Gate 2  └─Gate 3  └─Gate 4
```

## The Six Phases

| Phase | Purpose | Evidence Required to Exit |
|-------|---------|---------------------------|
| **INIT** | Session started, workflow mode selected | `/tmp/recipe_review.json` (Gate 0) |
| **DISCOVER** | Services identified and recorded | `/tmp/discovery.json` (Gate 1) |
| **DEVELOP** | Code written and tested on dev | `/tmp/dev_verify.json` (Gate 2) |
| **DEPLOY** | Code pushed to stage | `/tmp/deploy_evidence.json` (Gate 3) |
| **VERIFY** | Stage tested and confirmed working | `/tmp/stage_verify.json` (Gate 4) |
| **DONE** | Workflow complete | - |

## Gate Details

### Gate 0: INIT → DISCOVER (Recipe Review)
**File:** `/tmp/recipe_review.json`
**Created by:** `.zcp/recipe-search.sh quick {runtime} [managed]`

**What it checks:**
- File exists
- `verified: true`
- `patterns_extracted` contains valid data

**Why it exists:** Every documented mistake could have been prevented by reviewing recipes first. This gate ensures the agent has valid patterns before creating any services.

---

### Gate 1: DISCOVER → DEVELOP
**File:** `/tmp/discovery.json`
**Created by:** `.zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}`

**What it checks:**
- File exists
- `session_id` matches current session
- `dev.name` ≠ `stage.name` (unless `--single` mode)

**Why it exists:** Development requires knowing which services are dev and which are stage. This gate ensures the mapping is recorded.

---

### Gate 2: DEVELOP → DEPLOY
**File:** `/tmp/dev_verify.json`
**Created by:** `.zcp/verify.sh {dev} {port} / /status /api/...`

**What it checks:**
- File exists
- `session_id` matches current session
- `failed == 0` (all endpoints passed)

**Why it exists:** Dev is for debugging. Stage is for final validation. This gate prevents deploying broken code to stage.

---

### Gate 3: DEPLOY → VERIFY
**File:** `/tmp/deploy_evidence.json`
**Created by:** `.zcp/status.sh --wait {stage}` or `.zcp/workflow.sh record_deployment {stage}`

**What it checks:**
- File exists
- `session_id` matches current session

**Why it exists:** Ensures deployment actually completed before verification begins.

---

### Gate 4: VERIFY → DONE
**File:** `/tmp/stage_verify.json`
**Created by:** `.zcp/verify.sh {stage} {port} / /status /api/...`

**What it checks:**
- File exists
- `session_id` matches current session
- `failed == 0` (all endpoints passed)

**Why it exists:** Final confirmation that deployed code works correctly in stage environment.

---

## DISCOVER Phase: Two Sub-Flows

The DISCOVER phase **detects whether runtime services exist** and provides different guidance:

### Standard Flow (Services Exist)

When runtime services are already in the project:

```
1. zcli service list -P $projectId
2. .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}
3. .zcp/workflow.sh transition_to DEVELOP
```

### Bootstrap Flow (No Services)

When starting from scratch or adding new services:

```
STEP 1: Recipe Search (REQUIRED - Gate 0)
──────────────────────────────────────────
.zcp/recipe-search.sh quick {runtime} [managed-service]

Example: .zcp/recipe-search.sh quick go postgresql

This:
  ✓ Searches Zerops recipe API
  ✓ Prefers hello-world recipes (clean skeleton)
  ✓ Falls back to documentation if no hello-world
  ✓ Saves ready-to-use config to /tmp/fetched_recipe.md or /tmp/fetched_docs.md
  ✓ Creates /tmp/recipe_review.json (Gate 0 evidence)

STEP 2: Plan Services (RECOMMENDED)
───────────────────────────────────
.zcp/workflow.sh plan_services {runtime} [managed-service]

Creates /tmp/service_plan.json with:
  • Service hostnames (appdev, appstage, db)
  • Runtime versions
  • Configuration topology

STEP 3: Create import.yml (REQUIRED)
────────────────────────────────────
⚠️ KEY DECISION POINT: Use fetched recipe or construct manually?

If recipe_review.json shows:
  pattern_source: "recipe_hello_world"
  has_ready_import_yml: true
→ USE THE IMPORT.YML FROM /tmp/fetched_recipe.md DIRECTLY

If recipe_review.json shows:
  pattern_source: "documentation"
  has_ready_import_yml: false
→ Construct import.yml using patterns from /tmp/fetched_docs.md

STEP 4: Import Services (REQUIRED)
──────────────────────────────────
.zcp/workflow.sh extend import.yml

This:
  ✓ Validates YAML
  ✓ Runs zcli project service-import
  ✓ Waits for services to be ready
  ✓ Creates /tmp/services_imported.json
  ✓ Provides SSHFS mount commands

⚠️ CRITICAL: Restart ZCP to get new env vars!

STEP 5: Record Discovery (REQUIRED - Gate 1)
────────────────────────────────────────────
zcli service list -P $projectId
.zcp/workflow.sh create_discovery {dev_id} appdev {stage_id} appstage

Creates /tmp/discovery.json
```

---

## Recipe Search: Hello-World vs Framework vs Documentation

The recipe-search.sh has specific logic for finding patterns:

| Search | API Returns | Action |
|--------|-------------|--------|
| `go` | `go-hello-world` (hello-world category) | ✓ Use recipe directly |
| `python` | `django` (framework category) | ✗ Fall back to docs |
| `django` | `django` (framework category) | ✓ Use framework recipe |

**Why?**
- **Hello-world recipes** are clean skeletons ideal for bootstrap/extend
- **Framework recipes** have extra framework-specific config that may not be appropriate for basic runtime setup
- **Documentation** provides examples when no hello-world exists

The evidence file (`/tmp/recipe_review.json`) indicates the source:
```json
{
  "pattern_source": "recipe_hello_world",  // or "recipe_framework" or "documentation"
  "has_ready_import_yml": true,            // true for recipes, false for docs
  "configuration_guidance": "..."          // specific guidance based on source
}
```

---

## What Went Wrong: The Screenshot Analysis

In the screenshot, the agent:

1. ✅ Ran `workflow.sh init` - correct (Phase → INIT)
2. ✅ Ran `recipe-search.sh quick go postgresql` - correct (Gate 0 evidence created)
3. ❌ **SKIPPED `transition_to DISCOVER`** - never saw bootstrap guidance!
4. ❌ Created its own import.yml instead of using the fetched recipe
5. ❌ Wrote to wrong path (`/var/www/import.yml`)
6. ❌ Hit zcli auth issues

### The Key Mistake

**The agent skipped the phase transition.** After recipe-search, it should have run:
```bash
.zcp/workflow.sh transition_to DISCOVER
```

This transition:
1. Checks Gate 0 (recipe_review.json exists) ✓
2. Enters DISCOVER phase
3. Detects no runtime services exist → shows **BOOTSTRAP FLOW guidance**
4. STEP 3 of that guidance (now dynamic) tells it to USE the fetched import.yml

Without transitioning, the agent:
- Never entered DISCOVER phase
- Never saw the bootstrap guidance
- Improvised its own import.yml creation
- Missed the explicit instruction to use `/tmp/fetched_recipe.md`

### The Correct Path

```bash
# 1. Start session
.zcp/workflow.sh init                           # Phase: INIT

# 2. Recipe search (Gate 0)
.zcp/recipe-search.sh quick go postgresql       # Creates evidence + /tmp/fetched_recipe.md

# 3. CRITICAL: Transition to see bootstrap guidance
.zcp/workflow.sh transition_to DISCOVER         # Shows BOOTSTRAP FLOW with STEP 1-5

# 4. Follow STEP 3 guidance (now dynamic)
# If has_ready_import_yml=true: USE /tmp/fetched_recipe.md
# If documentation fallback: construct manually

# 5. Import services
.zcp/workflow.sh extend import.yml              # Creates services, waits for ready

# 6. Record discovery (Gate 1)
zcli service list -P $projectId
.zcp/workflow.sh create_discovery {dev_id} appdev {stage_id} appstage

# 7. Continue to development
.zcp/workflow.sh transition_to DEVELOP          # Checks Gate 1, enters DEVELOP
```

---

## Command Reference

```bash
# Start workflow
.zcp/workflow.sh init                    # Full mode
.zcp/workflow.sh init --dev-only        # Dev-only mode
.zcp/workflow.sh init --hotfix          # Skip dev verification
.zcp/workflow.sh --quick                # Quick mode (no enforcement)

# Check status
.zcp/workflow.sh show                   # Current state + next steps
.zcp/workflow.sh show --full            # Extended context
.zcp/workflow.sh recover                # Full context recovery

# Recipe discovery
.zcp/recipe-search.sh quick {rt} [db]   # Gate 0 evidence

# Service creation (Bootstrap)
.zcp/workflow.sh plan_services {rt} [db]
.zcp/workflow.sh extend import.yml

# Discovery recording
.zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}

# Phase transitions
.zcp/workflow.sh transition_to DISCOVER
.zcp/workflow.sh transition_to DEVELOP
.zcp/workflow.sh transition_to DEPLOY
.zcp/workflow.sh transition_to VERIFY
.zcp/workflow.sh transition_to DONE

# Verification
.zcp/verify.sh {service} {port} / /status /api/...
.zcp/status.sh --wait {service}

# Completion
.zcp/workflow.sh complete               # Final validation
```

---

## Evidence File Locations

| File | Created By | Gate |
|------|-----------|------|
| `/tmp/recipe_review.json` | recipe-search.sh quick | Gate 0 |
| `/tmp/service_plan.json` | workflow.sh plan_services | - |
| `/tmp/services_imported.json` | workflow.sh extend | - |
| `/tmp/discovery.json` | workflow.sh create_discovery | Gate 1 |
| `/tmp/dev_verify.json` | verify.sh {dev} | Gate 2 |
| `/tmp/deploy_evidence.json` | status.sh --wait {stage} | Gate 3 |
| `/tmp/stage_verify.json` | verify.sh {stage} | Gate 4 |
| `/tmp/fetched_recipe.md` | recipe-search.sh (API) | - |
| `/tmp/fetched_docs.md` | recipe-search.sh (docs) | - |

---

## Key Insights

1. **Evidence-based progression**: The workflow doesn't trust statements; it requires proof (JSON files with session IDs).

2. **Session isolation**: Evidence from one session cannot be used in another. This prevents stale data from causing issues.

3. **Recipe API is truth**: The recipe-search.sh queries the actual Zerops recipe API, not static patterns.

4. **Hello-world preference**: For basic runtime setup, hello-world recipes are preferred over framework recipes.

5. **Dev ≠ Stage**: The gate explicitly checks that dev and stage are different services to prevent source corruption.

6. **DISCOVER detects context**: The DISCOVER phase automatically detects whether services exist and provides appropriate guidance (standard vs bootstrap flow).
