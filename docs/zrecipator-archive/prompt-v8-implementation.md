# Implementation Prompt — v8 Findings

Paste this into a fresh Claude Code session at `/Users/fxck/www/zcp`.

---

Read `docs/improvement-guide-v8-findings.md` first — it contains the full analysis of 9 findings from the v8 test run (nestjs-showcase on v8.54.1). Then read `CLAUDE.md` for project conventions and TDD requirements.

You are implementing structural fixes to the recipe workflow system. The recipe workflow is a 6-step conductor (research → provision → generate → deploy → finalize → close) that guides an AI agent through creating a deployable application on Zerops PaaS. The workflow content lives in `internal/content/workflows/recipe.md` (a structured document with `<section>` and `<block>` tags). The workflow engine, predicates, guidance assembly, and templates live in `internal/workflow/`.

## Critical constraint: FRAMEWORK AND RUNTIME AGNOSTIC

Every change you make must work for ANY framework on ANY runtime. The recipe system handles Node.js, PHP, Python, Go, Rust, Java, .NET, and more. You must NEVER:
- Hardcode framework names (no "npm", "composer", "pip" in guide text — use "the plan's package manager" or "the research data's install command")
- Hardcode port numbers (no "5173", "3000" — use "the port from the plan's research data")  
- Hardcode build tools (no "tsc", "vite" — use "the framework's compile/check command")
- Hardcode OS names in defaults (no "alpine", "ubuntu" as assumed defaults)
- Write examples with specific frameworks — use structural descriptions

The plan's `research` field contains: `packageManager`, `httpPort`, `buildCommands`, `startCommand`, and `deployFiles`. All smoke test commands derive from these fields. The guide must teach the PATTERN, not the specific command.

## What to implement

### Phase 1: On-container smoke test block (Finding 1)

**File**: `internal/content/workflows/recipe.md`

Add a new `<block name="on-container-smoke-test">` in the `<section name="generate">` section, between the existing `pre-deploy-checklist` block (ends at line 797) and the `completion` block (starts at line 799).

The block teaches:

1. **Principle**: The dev containers are live development environments. The agent must validate code ON the container before deploying. `zerops_deploy` triggers a full build cycle (30s–3min) — catching dependency errors, type errors, and startup crashes on the container takes seconds.

2. **Three validation steps** (framework-agnostic, derived from the plan's research data):
   - **Install dependencies**: Run the plan's package manager install command on the container via SSH. This catches hallucinated packages, version conflicts, and peer dependency mismatches.
   - **Compile/check** (if the framework has a compilation step): Run the relevant compile or type-check command. This catches type errors, syntax errors, and missing imports.
   - **Start the dev server**: Start the dev process and verify it binds to the expected port. Connection errors to managed services are EXPECTED (env vars not active yet). The goal is "process starts and binds", not "app serves requests". If the process crashes immediately, this catches native binding mismatches, missing modules, and config errors.

3. **What's available vs what's not**: These commands use only the base image's tools (runtime + package manager). `run.envVariables` are NOT available yet — that's fine, the smoke test doesn't need them. The existing constraint "do not run commands that bootstrap the framework" means "don't connect to databases", NOT "don't validate your code compiles."

4. **Failure handling**: If the smoke test catches an error, fix it on the mount and re-run. Only proceed to `zerops_deploy` when all three steps pass. Do NOT commit and deploy hoping the build container will produce a different result.

5. **Multi-codebase**: For plans with multiple dev mounts, run the smoke test on each container.

The block should have NO predicate (always emit) since it applies to every recipe type.

Also update the `completion` block's attestation template to include "On-container smoke test passed on all dev mounts" or similar.

### Phase 2: Dev setup zerops.yaml rules (Findings 2, 3)

**File**: `internal/content/workflows/recipe.md`

Modify the `setup-dev-rules` block (line 584) and/or the `serve-only-dev-override` block (line 600):

1. **No `run.os` override by default**: Add a clear rule that `setup: dev` does NOT set `run.os`. The agent operates from zcp (which has full Ubuntu tooling). The dev container needs only the runtime and its package manager — both are in the base image already. Omitting `run.os` means build and run use the same default OS, eliminating native binding mismatches entirely.

   Exception: if the on-container smoke test reveals a hard glibc-only dependency (the dev server crashes on the default OS), THEN add `run.os` with an appropriate OS AND add the package manager's rebuild command to `initCommands`. This is a reactive exception discovered during validation, not a proactive default.

2. **Dev ports for dev-server targets**: Add a rule that if `setup: dev` runs a dev server process (any framework with a bundler, HMR server, or dev-mode HTTP server on a non-standard port), `ports` MUST be declared with the dev server's port and `httpSupport: true`. Without it, subdomain access cannot be enabled. This applies specifically when the dev setup's runtime differs from the prod setup's (e.g., prod is serve-only, dev runs a dev server on an explicit port).

### Phase 3: Finalize serve-only type override (Finding 5)

**Code files**: Look in `internal/workflow/recipe_templates.go` and `internal/workflow/recipe_overlay.go`.

The finalize step generates import.yaml files for 6 environments. For environments 0 and 1 (which include dev services), when a target's prod type is serve-only (`static`, `nginx` — check the `serveOnlyBases` map in `recipe_plan_predicates.go`), the import.yaml must use the dev-appropriate service type instead.

The dev service type comes from the zerops.yaml's `setup: dev` → `run.base` field. For a target with prod `type: static` and dev `run.base: nodejs@22`, the env0/env1 import.yaml should emit `type: nodejs@22` for the dev service, not `type: static`.

This is a code change, not a guide change. Write tests first (RED), then implement (GREEN). Use the existing shape fixtures to test all combinations.

### Phase 4: Deploy verification completeness (Finding 6)

**File**: `internal/content/workflows/recipe.md`

In the `dev-deploy-flow-core` block (line 878) and `stage-deployment-flow` block (line 1202):

1. Add explicit verification requirements for ALL runtime targets. Currently the guide mentions `zerops_verify` but doesn't enumerate which targets need it. Make it structural: after deploying all dev targets, verify EACH one. After cross-deploying to stage, verify EACH stage target.

2. Distinguish HTTP targets (use `zerops_verify` + `zerops_subdomain`) from non-HTTP targets like workers (use `zerops_logs` to confirm the process started and connected to its message broker).

### Phase 4b: Feature subagent port hygiene (Finding 7)

**File**: `internal/content/workflows/recipe.md`

In the `dev-deploy-subagent-brief` block (line 1019):

1. Add instruction that the main agent MUST kill all dev server processes before dispatching the feature subagent. The subagent starts fresh — no leftover processes holding ports.

2. Add instruction in the subagent's brief to always kill the port holder (`fuser -k {port}/tcp` or equivalent) before starting a dev server, as a defensive measure.

### Phase 5: Knowledge delivery for managed service patterns (Finding 8)

This is a knowledge-base concern. Check how `internal/knowledge/` delivers managed service patterns. The connection patterns for each managed service type (auth format, connection string construction, known client-library pitfalls) should be available for injection into generate subagent prompts when the plan includes those services.

Assess the current state, identify the gap, and either fix the delivery path or document what needs to change. This may be a larger change — scope it and implement what's tractable.

### Phase 5b: Post-deploy initCommands verification (Finding 9)

**File**: `internal/content/workflows/recipe.md`

In the deploy section, after the initCommands log-check guidance (around the "Step 3a" area), add a note: if `initCommands` include data-seeding or index-population commands gated by `execOnce`, verify the expected data actually exists after deploy. If prior failed deploys burned the `execOnce` key, the successful deploy may skip those commands. The agent should check and re-run manually via SSH if needed.

## Verification

After implementing all phases:

1. Run `go test ./internal/workflow/... -v -count=1` — all tests must pass
2. Run `go test ./... -count=1 -short` — full test suite must pass  
3. Run `make lint-local` — full lint must pass
4. Check monotonicity: the shape caps in `showcaseStepCaps` must still hold across all 4 fixtures (ShapeHelloWorld ≤ ShapeBackendMinimal ≤ ShapeFullStackShowcase ≤ ShapeDualRuntimeShowcase at every step)

### Framework-agnosticism audit

After implementation, grep the ENTIRE `recipe.md` for any framework-specific terms you introduced. The following should appear ONLY in examples that are explicitly labeled as examples, never in rules or instructions:
- Specific package managers by name (npm, composer, pip, cargo, etc.) in rule text
- Specific port numbers in rule text  
- Specific compile commands in rule text
- `os: ubuntu` as a default or recommendation

Every rule must reference the plan's research data fields, not hardcoded values.
