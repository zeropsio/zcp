# v8.86 implementation plan — self-verifying writers + structured facts log

**Status**: focused implementation guide for v8.86. Supersedes the §7.1–§7.7 list in [implementation-v23-postmortem.md](implementation-v23-postmortem.md) — that list addressed symptoms (truncation, brief framing, embedded-yaml docs) without addressing the root cause (external-gate-plus-dispatch is anti-convergent by construction).

**Target**: v24 hits **A− overall (matches v20 peak)** in ~80-95 min wall, **with the v8.78–v8.85 quality bar preserved**.

**Calibration**: this is the lower-risk of two viable shapes. The higher-risk shape (per-substep distributed fragment authoring) is reserved as **stage 2 (v8.87+)** if v24 evidence shows the lower-risk version doesn't fully close the convergence gap. We commit to one architectural change at a time.

---

## Table of contents

- [1. The diagnosis in one paragraph](#1-the-diagnosis)
- [2. Design principle — invert verification direction](#2-design-principle)
- [3. The six v8.86 fixes](#3-the-six-fixes)
  - [3.1 Structured facts log accumulated during deploy](#31-facts-log)
  - [3.2 Writer subagent brief: rules-as-runnable-validation + iterate-until-clean](#32-writer-brief)
  - [3.3 Demote v8.81 dispatch gate to confirmation-only](#33-demote-dispatch-gate)
  - [3.4 Restore v20's close-step critical-fix subagent](#34-close-critical-fix)
  - [3.5 Scaffold contract spec](#35-contract-spec)
  - [3.6 Folk-doctrine prevention (execOnce semantics + MCP channel docs)](#36-folk-doctrine)
- [4. Stage 2 (v8.87+) — per-substep distributed fragment authoring](#4-stage-2)
- [5. Validation plan](#5-validation)
- [6. Risk analysis](#6-risk)
- [7. Non-goals](#7-non-goals)
- [8. File inventory](#8-file-inventory)

---

## 1. The diagnosis

v8.81 added a content-fix dispatch gate that fires when the writer subagent ships content failing checks. The gate fires correctly. The brief it constructs is anti-convergent because the writer never sees the check rules upfront — only learns about them via dispatch round-trips. v23 ran 5 rounds (writer + 3 fix subagents + main inline) where 1 round should suffice, costing 23 minutes of the 119-minute total wall clock. The math: 17 active checks × ~95% per-check pass-rate = 42% probability of clean-on-first-write, so 58% of runs are mathematically forced into a fix loop. Each loop iteration costs a multi-minute subagent dispatch.

The fix is not a better dispatch gate. The fix is to invert the verification direction: writers learn the check rules as part of their brief and self-verify before returning.

## 2. Design principle

Today (v23):

```
writer ships → external gate verifies → if fail, dispatch fix subagent → loop
```

Multi-round, multi-minute per round, briefs reconstructed each round, truncated findings hide leftover work.

Target (v8.86):

```
writer prepares input → writer self-verifies against rules → writer ships clean
                                                          → external gate confirms
```

Single round in the success case. The external gate becomes a confirmation step that runs but rarely fires. If it does fire, that's a writer-brief bug, not a workflow recovery step.

Two preconditions make this work:

- **Pre-collected facts** — writers operating late in the workflow (e.g., the README writer at deploy.readmes) need pre-organized input rather than 90 minutes of session log to do archaeology against. v8.86 ships a structured facts log accumulated during deploy substeps.
- **Rules-as-runnable-validation** — every check rule the gate will run gets translated into a concrete pre-return validation command (grep, awk, ratio computation) the writer must execute against its own draft before returning.

## 3. The six v8.86 fixes

### 3.1 Structured facts log accumulated during deploy

**Problem**: writer at deploy.readmes consolidates 90 minutes of deploy-discovered facts (errors hit, fixes applied, platform behaviors observed, contract bindings established) by re-reading the session-log. Archaeology, not consolidation. Slow and lossy.

**Fix**: introduce a `zerops_record_fact` MCP tool the agent calls during deploy substeps when relevant facts emerge. Tool appends a structured record to `/tmp/zcp-facts-{sessionId}.jsonl`:

```json
{
  "ts": "2026-04-17T12:35:50.123Z",
  "substep": "deploy.deploy-dev",
  "codebase": "workerdev",
  "type": "gotcha_candidate",
  "title": "module: nodenext + raw ts-node = Cannot find module",
  "mechanism": "ts-node -r tsconfig-paths/register against tsconfig 'module: nodenext'",
  "failure_mode": "Cannot find module './app.module.js' — ts-node refuses to resolve relative imports without explicit .js suffix",
  "fix_applied": "Switch tsconfig to 'module: commonjs' + 'moduleResolution: node' + strip .js suffixes from relative imports. Alternative: use 'nest start --watch' (which @nestjs/cli handles transparently).",
  "evidence": "12:35:26 ts-node failed with module-resolve error; 12:35:50 tsconfig flipped; 12:35:59 dev server came up clean"
}
```

Record types: `gotcha_candidate`, `ig_item_candidate`, `verified_behavior`, `platform_observation`, `fix_applied`, `cross_codebase_contract`.

The agent's recipe.md instruction at each deploy substep is updated to include: *"When you encounter and fix a non-trivial issue, or verify a non-obvious platform behavior, call `zerops_record_fact` with the appropriate record type. The README writer at the end of deploy will consume these records — write them at the moment of freshest knowledge, not in retrospect."*

**Files to touch**:
- New: `internal/tools/record_fact.go` — MCP tool implementation
- New: `internal/ops/facts_log.go` — append/read primitives
- `internal/server/server.go` — register tool
- `internal/content/workflows/recipe.md` — add fact-recording instructions to deploy substeps (deploy-dev, init-commands, subagent, browser-walk, cross-deploy)

**Effort**: ~0.5 day.

### 3.2 Writer subagent brief: rules-as-runnable-validation + iterate-until-clean

**Problem**: writer brief at deploy.readmes substep tells the writer WHAT to write but not HOW the downstream gate verifies. Writer ships content that fails 23 checks. Dispatch loop ensues.

**Fix**: rewrite the writer brief template at `internal/workflow/recipe_writer_brief.go` (or wherever the brief is constructed) to include:

1. **Input section**: facts log content + scaffold output paths + plan + zerops.yaml + contract spec
2. **Output section**: the 6 files to write
3. **Validation section** (NEW) — for each content check that will run at the gate, an explicit pre-return validation:

```
BEFORE RETURNING, you MUST run each of these validations against your output and iterate until ALL pass:

✅ comment_ratio (zerops.yaml IG-step-1 embedded blocks)
   For each ```yaml block in apidev/README.md, appdev/README.md, workerdev/README.md inside "### 1. Adding `zerops.yaml`":
     awk '/^[[:space:]]*#/{c++} END{print c/NR}' that block must return ≥ 0.30
   If not: rewrite comments to name Zerops platform terms (L7 balancer, execOnce, ${db_hostname}, httpSupport, readinessCheck, etc.) and explain the failure mode without each comment.

✅ content_reality (no phantom file paths)
   grep -rE 'dist/[^[:space:]"`]+\.js|process\.env\.[a-zA-Z_]+|res\.json\(|response\.json\b|import\.meta\.env\.[a-zA-Z_]+' apidev/README.md apidev/CLAUDE.md appdev/README.md appdev/CLAUDE.md workerdev/README.md workerdev/CLAUDE.md
   Each match must be inside a fenced code block AND clearly framed as "output of `npm run build`" or "compiled JavaScript" or similar — NOT presented as an authoritative file path.

✅ gotcha_causal_anchor (per gotcha bullet)
   For each gotcha bullet: must contain at least one platform mechanism token (read from facts log) AND one strong-symptom verb from this list: rejects, deadlocks, drops, crashes, times out, throws, fails, returns 4xx, returns 5xx, returns wrong content-type, hangs, never reads, silent no-op.

[... and one entry per active check, with concrete validation commands ...]

If ANY validation fails: fix the relevant content, re-run all validations, iterate until clean.
ONLY return when every validation passes.
```

The brief becomes long (~5KB) but bounded. The writer subagent runs the validations as bash calls inside its own context — no main-agent round-trip, no fix-subagent dispatch.

**Files to touch**:
- `internal/workflow/recipe_writer_brief.go` (new file or extension of existing brief generator)
- `internal/content/workflows/recipe.md` — update writer subagent brief topic to reference the validation section
- All `internal/tools/workflow_checks_*.go` files — each check exposes its rule in two forms: (a) the gate-side check function (existing), (b) a `BriefValidationCommand()` method that returns the runnable validation string

**Effort**: ~1 day. The biggest piece is wiring every existing check to expose its validation command.

### 3.3 Demote v8.81 dispatch gate to confirmation-only

**Problem**: v8.81's `content_fix_dispatch_required` gate dispatches fix subagents on writer-failure. This makes failure the expected mode and entrenches the multi-round pattern.

**Fix**: change the gate's response when checks fail. Currently it instructs main to dispatch a fix subagent. New behavior: it returns a hard error:

```
WRITER BRIEF BUG: writer subagent shipped content failing N content checks.
This should not happen — the writer brief is supposed to include all check rules
as pre-return validation. Either:
  (a) The writer subagent did not run its self-validation (file an MCP-side bug)
  (b) The writer's validation passed but the gate-side check disagrees (file a
      check-vs-validation parity bug; the validation command for check X is
      ${validation_cmd} and gate-side check is ${check_function_path})
  (c) A new check was added without updating the writer brief (file a
      brief-completeness bug)

DO NOT dispatch a fix subagent. Fix the writer brief or the check parity.
The deploy step will not advance until the writer ships clean content.
```

This is intentionally loud. It removes the comfortable "fall back to fix subagent" escape hatch and forces every writer-failure to surface as a bug to fix at the brief layer.

**Files to touch**:
- `internal/workflow/recipe_content_fix_gate.go` — change failure mode
- `internal/workflow/recipe_content_fix_gate_test.go` — replay v23 SA dispatch scenarios, assert the new failure mode

**Effort**: ~0.25 day. Net reduction in code (the dispatch logic gets removed).

### 3.4 Restore v20's close-step critical-fix subagent

**Problem**: v20 had a clean Close-step pattern: code-review FINDS, dedicated critical-fix subagent FIXES + REDEPLOYS + REVERIFIES. v22 collapsed this; v23 inherits the collapsed shape, so close-step bugs cause main agent to absorb 8 minutes of redeploy + cross-deploy orchestration.

**Fix**: split the Close substep into two phases:

- `close.code-review` (existing) — code-review subagent's brief is constrained to FINDING bugs and emitting a structured findings list. No fix authority. Returns a JSON list:
  ```json
  {
    "critical": [{"file": "...", "line": ..., "issue": "...", "fix_hint": "..."}, ...],
    "wrong": [...],
    "style": [...]
  }
  ```
- `close.critical-fix` (NEW substep) — fires only if code-review returned ≥1 critical or wrong. Dispatches a critical-fix subagent that:
  1. Reads the findings JSON
  2. Applies fixes per finding (Edit/Write tool calls)
  3. Commits per codebase
  4. Calls `zerops_deploy` for each affected service (dev)
  5. Calls `zerops_dev_server start` and runs E2E verification curls
  6. Cross-deploys to stage
  7. Returns a structured verification report

Main agent stays at the orchestration level. The redeploy + cross-deploy choreography lives inside the critical-fix subagent.

**Files to touch**:
- `internal/content/workflows/recipe.md` — close section: split into two substeps with clear briefs
- `internal/workflow/recipe_substeps.go` — add `SubStepCloseCriticalFix` constant
- `internal/workflow/recipe_topic_registry.go` — register `close-critical-fix-brief` topic
- Tests: substep ordering, critical-fix subagent gate (only fires if findings warrant)

**Effort**: ~0.5 day.

### 3.5 Scaffold contract spec

**Problem**: scaffold subagents author independently, contract drift surfaces only at code-review (StatusPanel-vs-/api/status response shape, workerdev Item entity vs apidev migration, missing NATS queue group). All three were CRITs in v23.

**Fix**: introduce a `generate.contract-spec` substep BEFORE scaffold dispatch. Main agent (no subagent — small enough to inline) writes a structured spec to `/tmp/zcp-contract-{sessionId}.yaml`:

```yaml
contract_spec:
  http_endpoints:
    /api/status:
      response_shape: '{"db":"ok","redis":"ok","nats":"ok","storage":"ok","search":"ok"}'
      response_shape_kind: flat-object  # NOT nested-array
      consumed_by:
        - appdev/src/lib/StatusPanel.svelte
  database_tables:
    items:
      columns:
        - id uuid PRIMARY KEY
        - title varchar(200)  # NOT 255
        - body text
        - created_at timestamptz DEFAULT now()  # snake_case, NOT camelCase
        - updated_at timestamptz DEFAULT now()
      consumed_by:
        - apidev/src/migrations/CreateItems*.ts
        - apidev/src/entities/Item.entity.ts
        - workerdev/src/entities/Item.entity.ts  # MUST mirror apidev
  nats_subjects:
    jobs.process:
      queue_group: 'jobs-workers'  # required when minContainers > 1
      published_by: apidev/src/services/jobs.service.ts
      subscribed_by: workerdev/src/worker.controller.ts (with @EventPattern + queue group)
  graceful_shutdown:
    workerdev_main_ts:
      pattern: enableShutdownHooks() + process.on('SIGTERM', () => app.close())
```

Each scaffold subagent's brief gets the contract spec as required input + an explicit pre-return verification:

```
BEFORE RETURNING, verify your scaffold matches the contract spec for your codebase:
  - For each consumed_by entry that points to YOUR codebase, verify the named file exists with the specified shape
  - For each "required_in" entry, verify the pattern is present in the named file
  - If any contract violation: fix and re-verify
```

The shared contract spec also feeds into the README writer's facts log → IG items reference real contracts, gotchas reference the contract names not paraphrases.

**Files to touch**:
- New: `internal/workflow/recipe_contract_spec.go` — generate spec from research plan
- `internal/content/workflows/recipe.md` — add `generate.contract-spec` substep + scaffold subagent brief gets contract spec section
- Tests: contract spec generation from plan, scaffold brief includes contract section

**Effort**: ~0.75 day.

### 3.6 Folk-doctrine prevention

Two narrow but high-leverage fixes for the v23 platform-mental-model defects.

**3.6a — `execOnce-semantics` eager topic + `claude_md_no_burn_trap_folk` content check**

Same as the original §7.4 from the v23 postmortem doc — that part holds. New eager topic at `EagerAt: SubStepInitCommands`:

```
`zsc execOnce ${appVersionId}` keys on the deploy version — each new deploy gets a
fresh appVersionId, so the lock is NEVER pre-burned by a prior deploy. If your
first-deploy initCommand silently no-ops (✅ in <100ms with no body output), the
cause is your script — NOT a "burned key":
  - Check for early process.exit(0) or unhandled-rejection swallow
  - Check the runtime can resolve your script (ts-node + module resolution)
  - Check stdout buffering — pipe through `node --enable-source-maps` if ts-node

Do NOT use the term "burn" or "burn trap" in any CLAUDE.md or README — that
terminology does not exist in the platform; using it ships fictional folk-doctrine
to downstream users.
```

Plus a content check `claude_md_no_burn_trap_folk` that fails if any CLAUDE.md or README contains `burn trap` near `execOnce`. The validation runs as part of the writer's pre-return check loop.

**3.6b — `zerops_deploy` blocking-channel docs**

Same as original §7.5. Amend `zerops_deploy` tool description in `internal/tools/deploy.go`:

> **Channel-blocking**: this call holds the MCP STDIO channel for the duration of the build (typically 60–120s). DO NOT issue other zerops_* calls in the same response — they will return `Not connected` (an MCP transport error, not a platform rejection). Serialize all deploys.

**Files to touch**:
- `internal/content/workflows/recipe.md` — execOnce-semantics topic block
- `internal/workflow/recipe_topic_registry.go` — register topic at SubStepInitCommands
- `internal/tools/workflow_checks_claude_md_folk.go` — new check (extends or composes with claude_readme_consistency)
- `internal/tools/deploy.go` — tool description amendment

**Effort**: ~0.25 day.

---

## 4. Stage 2 (v8.87+) — per-substep distributed fragment authoring

**Trigger**: only proceed to stage 2 if v24 evidence shows the v8.86 self-verifying writer still has ≥2 internal iteration rounds in its self-validation loop, OR wall clock for the deploy.readmes substep exceeds 8 minutes.

**Shape**: decompose deploy.readmes from a single end-of-deploy authoring step into per-substep fragment authoring:

| Substep | Fragment authored at this substep |
|---|---|
| `generate.scaffolds` | per-codebase: code-shape facts, contract references |
| `generate.zerops-yaml` | per-codebase: zerops.yaml content → IG step 1 yaml block |
| `deploy.deploy-dev` | per-codebase: gotchas for issues encountered |
| `deploy.init-commands` | apidev: execOnce gotcha (with correct semantics) |
| `deploy.subagent` (feature subagent) | per-codebase: IG items for endpoints + cross-codebase contracts |
| `deploy.browser-walk` | appdev: UX-discovered gotchas |
| `deploy.cross-deploy` | per-codebase: stage-specific gotchas |
| `deploy.readmes` | **stitch fragments → README + CLAUDE.md per codebase + root README + architecture section. No new authoring.** |

Each substep authors and self-verifies its own fragment at the moment of freshest knowledge. The fragment is small (1-3 gotchas, 1 IG item, a CLAUDE.md section). Self-verification on a small fragment with 2-3 applicable check rules iterates in seconds.

**Why this is stage 2 not stage 1**: this is a workflow-mental-model change of the same magnitude as the v8.78 reform that triggered the v21 disaster. It introduces new failure classes (fragment-stitching contradictions, ordering disputes, fragment-format drift across substeps) that we have zero data on. Validating it requires a working baseline — which is what v8.86 establishes. **One architectural change per release. Validate before escalating.**

If v24 shows v8.86 fully closes the convergence gap (≤1 internal iteration round in writer, deploy.readmes substep ≤5 min), stage 2 is unnecessary. If v24 still shows multi-minute iteration costs inside the writer's self-validation loop, stage 2 becomes the v8.87 work and we have data to justify the bigger change.

---

## 5. Validation plan

### Unit tests (RED before implementation)

- `record_fact` MCP tool: append-only semantics, structured-record schema validation, session-id scoping
- Writer brief generator: includes facts log content + each active check's `BriefValidationCommand()`
- Each existing check's `BriefValidationCommand()` returns a runnable shell command
- v8.81 dispatch gate: failure now returns brief-bug error, does not include "dispatch fix subagent" instruction
- Close substep ordering: `close.code-review` then conditional `close.critical-fix`
- Contract spec generation: from research plan input, produces canonical YAML with all 4 sections (http_endpoints, database_tables, nats_subjects, graceful_shutdown)
- `claude_md_no_burn_trap_folk` check: shadow-test against v23 apidev/CLAUDE.md → fail; against v22 → pass

### Replay tests

- Feed v23 main-session.jsonl + facts that WOULD have been recorded into the new writer brief generator. Assert: brief includes runnable validation for every check that fired in v23. Assert: a hypothetical writer subagent following the brief would have caught all 23 v23 fails internally before returning.
- Feed v23 scaffold subagent inputs into the contract spec generator. Assert: spec captures the StatusPanel response-shape constraint, the Item entity column type/casing constraint, the NATS queue group requirement, the SIGTERM drain pattern. Verify a scaffold-subagent brief built with the spec would have caught the v23 CRIT class at scaffold time.

### Live validation (v24 run)

Pass criteria:
- Wall ≤95 min (was 119)
- ≤1 fix subagent dispatched for content (was 3)
- Writer subagent's internal iteration loop converges in ≤2 rounds
- 0 mentions of "burn trap" in shipped CLAUDE.md
- 0 mentions of "parallel deploys rejected" in TIMELINE
- Close-step CRITs reach 0 OR are caught at scaffold/feature time via contract spec
- Close-step critical-fix subagent fires (if any close CRITs found) and main agent does NO redeploy/cross-deploy calls itself

Fail criteria → trigger stage 2:
- Writer's internal iteration ≥3 rounds
- deploy.readmes substep takes ≥8 min
- Writer fails to converge in any single dispatch (escalation to brief-bug)

---

## 6. Risk analysis

| Fix | Risk | Mitigation |
|---|---|---|
| §3.1 facts log MCP tool | Agent forgets to call it during deploy substeps; facts log is empty; writer falls back to archaeology | recipe.md instruction at every relevant substep + post-substep gate that prompts the agent if no fact recorded for high-signal events (e.g., post-init-commands gate fails if seed verification doesn't show a recorded fact) |
| §3.2 writer brief size | Brief grows to ~5-8KB with all validation commands inlined | Within current substep response budget (deploy.readmes substep budgeted at 25KB per v8.84). Add size-budget guard test. |
| §3.2 validation-command parity | Brief validation says "pass" but gate-side check disagrees | Each check exposes both the gate-side function AND the brief validation command from the same source; parity test asserts they agree on a corpus of fixtures |
| §3.3 demoting dispatch gate | If writer ever genuinely needs help, no escape hatch | Loud failure surfaces the real issue (writer brief gap) instead of papering it over. The escape hatch was anti-convergent — removing it forces fixes at the right layer. |
| §3.4 close-critical-fix subagent | Adds another subagent to manage; may dispatch when not needed | Only fires if code-review returned ≥1 critical or wrong; gate test asserts no spurious dispatch on clean-review runs |
| §3.5 contract spec | Spec might be incomplete; new contract types emerge that aren't captured | Spec is a living artifact — gaps surface as code-review CRITs in subsequent runs and feed back into spec template. Initial spec covers the v23 CRIT classes (response shape, entity schema, queue group, SIGTERM). |
| §3.6 folk-doctrine check | False positives on legitimate uses of "burn" word | Check fires only on `burn` within 50 chars of `execOnce`. Shadow test against v18-v22 corpus to confirm zero false positives. |

---

## 7. Non-goals

1. **Don't add new content-quality checks.** The check inventory v23 has is the right inventory. v8.86 makes existing checks faster to satisfy by moving verification upstream into the writer.
2. **Don't decompose deploy.readmes (yet).** Stage 1 keeps the late-authoring shape (preserves deep content). Stage 2 (v8.87+) does the decomposition only if v24 proves it necessary.
3. **Don't add per-framework hardcoding.** §3.5 contract spec is structured around platform-level concepts (HTTP response shapes, DB schema, NATS subject + queue group, graceful shutdown). The spec instances are recipe-specific; the spec FORM is framework-agnostic.
4. **Don't try to fix the underlying MCP "Not connected" transport limitation.** §3.6b documents it as a known channel-blocking property. Fixing the MCP server's request multiplexing is separate scope.
5. **Don't consolidate the env-comments writer subagent into the README writer.** v23's degenerate 1-event env-comments dispatch is a recipe.md issue, not a v8.86 issue. Leave it for a future cleanup pass.
6. **Don't try to eliminate snapshot-dev redeploys.** §6 of the v23 postmortem confirmed all 17 deploys are justified; the perceived rhythm is a UX artifact, not a workflow defect.

---

## 8. File inventory

```
NEW FILES:
internal/tools/record_fact.go                         # MCP tool: zerops_record_fact
internal/tools/record_fact_test.go                    # RED tests for tool
internal/ops/facts_log.go                              # append/read primitives
internal/ops/facts_log_test.go
internal/workflow/recipe_writer_brief.go              # writer brief generator with validation injection
internal/workflow/recipe_writer_brief_test.go         # parity + completeness tests
internal/workflow/recipe_contract_spec.go             # contract spec generator from plan
internal/workflow/recipe_contract_spec_test.go
internal/tools/workflow_checks_claude_md_folk.go      # burn-trap detection check

MODIFIED FILES:
internal/server/server.go                              # register zerops_record_fact tool
internal/tools/workflow_checks_*.go (each)             # add BriefValidationCommand() method
internal/workflow/recipe_content_fix_gate.go          # demote to confirmation-only
internal/workflow/recipe_content_fix_gate_test.go     # update test expectations
internal/workflow/recipe_substeps.go                  # add SubStepCloseCriticalFix constant
internal/workflow/recipe_topic_registry.go            # register execOnce-semantics + close-critical-fix-brief topics
internal/tools/deploy.go                               # description: blocks MCP channel
internal/content/workflows/recipe.md                   # multiple sections:
                                                        #   - generate: add contract-spec substep
                                                        #   - scaffold-subagent-brief: add contract spec section
                                                        #   - deploy.<each substep>: fact-recording instruction
                                                        #   - deploy.readmes: writer brief restructured
                                                        #   - close: split into code-review + critical-fix substeps
                                                        #   - new topic: execOnce-semantics
```

**Estimated effort**: ~3-4 days implementation + 1 day test buildout + 0.5 day live-run validation. Total: ~5 working days.

---

## Order of operations

1. Write all RED tests first (1 day)
2. Implement §3.1 facts log MCP tool (0.5 day)
3. Implement §3.5 contract spec generator (0.75 day) — independent of writer brief work
4. Implement §3.2 writer brief generator + per-check BriefValidationCommand() (1 day)
5. Implement §3.3 dispatch gate demotion (0.25 day)
6. Implement §3.4 close-critical-fix substep split (0.5 day)
7. Implement §3.6 folk-doctrine prevention (0.25 day)
8. Run full test suite under `-race` (0.25 day)
9. Live validation: kick off v24 run (~1.5 hour wall, plus analysis time)

Each step is independently testable. Steps 2-7 can be parallelized across 2 implementers if needed.
