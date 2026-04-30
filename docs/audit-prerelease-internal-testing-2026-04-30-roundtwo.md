# Pre-Internal-Testing Audit — Round 2 (post-fix re-pass)

**Date**: 2026-04-30
**Scope**: Re-verification of the 16 findings from `audit-prerelease-internal-testing-2026-04-29.md` (C1-C10 + H1-H3 + M1-M4 + L1-L3) against the current tree, plus a fresh Codex adversarial pass to find drift introduced BY the fixes themselves.
**Trigger**: 12 fix commits landed since the original audit, all six deferred items written up as named backlog plans. User asked: "what's actually still open after the sweep?"

---

## What changed since 2026-04-29

12 commits across atoms, handlers, engine, spec, and tests:

| Commit | Theme | Findings closed |
|---|---|---|
| `a70e6073` sweep(deploy-strategy-vocab) (40 files) | C1 — vocab sweep | C1, C6, C8 |
| `504474b0` fix(plan-spec-align) | H1 cross-deploy + spec D2b | H1, H2, C10 |
| `ffac8ad1` fix(git-push) | C2 — remove auto-stamp on async | C2, indirectly C4 |
| `85897d31` fix(asset-pipeline) + axis-hot-shell lint | C5 — Vite via dev-server | C5 |
| `9e019a70` fix(atoms) | M3 — local-stage axes | M3 |
| `f36ad726` fix(workflows) | build-integration handoff order | (new — not in original audit) |
| `ee8d4315` + `4fac8d7a` | F5 root — `WorkSessionState` lifecycle signal | (new) |
| `d68edec9` fix(api-error-enrichment) | setup-not-found APIMeta | (new) |
| `9fae1a2d` P5-Lever-A | 14 atoms → multiService=aggregate | H3 (partial) |
| `366f9acb` P5-Lever-B | per-service axis narrows to WorkSession scope | H3 (partial) |
| `6a246201` fix(subdomain) | unify auto-enable predicate | (new) |
| `53ebc002` + `d7ecf63c` close + archive | plan archived as `pre-internal-testing-fixes-2026-04-30.md` | C7 |

Deferred to backlog (named plans created):

- `plans/backlog/c3-failure-classification-async-events.md` (C3) — **superseded by Round-2 implementation; classifier wired into `ops.Events`**
- `plans/backlog/m1-glc-safety-net-identity-reset.md` (M1)
- `plans/backlog/m2-stage-timing-validation-gate.md` (M2)
- `plans/backlog/m4-aggregate-atom-placeholder-lint.md` (M4)
- `plans/backlog/deploy-intent-resolver.md` (NEW — H1 structural follow-up)
- `plans/backlog/auto-wire-github-actions-secret.md` (NEW — build-integration=actions UX)

Dismissed (NOT a backlog item):

- **~~C9 — recipes have no git-push scaffolding~~** — re-examined as a scope question, not a gap. Recipes are stack templates (services + initial code structure); close-mode + git-push capability are develop-flow concerns per spec invariants B7 ("bootstrap does NOT set deploy strategy"), E2 ("bootstrap leaves CloseDeployMode + GitPushState + BuildIntegration empty"), and S2 ("close-mode never auto-assigned"). The post-bootstrap path is already complete: `bootstrap-recipe-close.md:14` states "close-mode is not chosen at bootstrap; develop picks it on first use" and points at `action="close-mode"` + `action="git-push-setup"`; `develop-strategy-review.md` (priority 2, fires on `deployStates: [deployed]` + `closeDeployModes: [unset]`) presents all three modes including git-push and chains to `git-push-setup`; `setup-git-push-{container,local}.md` (strategy-setup phase) walks the configuration. Recipe authoring atoms (`internal/content/workflows/recipe/...`) correctly contain ZERO references to git-push because deploy strategy is out of recipe scope. R12's original framing assumed recipes should pre-stage git-push — that violates separation of concerns (recipes = stack templates, develop = deploy mechanism) and is now formally rejected. Backlog plan deleted.

Backlog discipline (`plans/backlog/README.md`) requires Surfaced + Why-deferred + Trigger-to-promote on every entry. Confirmed all entries comply.

---

## Round-3 sweep — knowledge-timing + Option B remnants (KT-1 through KT-10)

After C9 dismissal exposed a deeper question — *"is bootstrap delivering content the agent can't act on yet?"* — Codex + local audit found 8 atom-level + 4 spec-level instances where Option B (verification-server pattern, retired in v8.100+) still leaked into bootstrap-phase knowledge.

### Atoms swept

| ID | File | Change |
|---|---|---|
| **KT-1** | `develop-platform-rules-common.md` | Dropped `bootstrap-active` from `phases:` axis. All 6 platform-rules bullets are deploy-side; agent in bootstrap can't act on them. Matrix simulator confirms atom no longer fires in bootstrap scenarios (was firing in ALL 5 bootstrap scenarios pre-sweep). |
| **KT-2** | `bootstrap-recipe-close.md` | Trimmed close-mode menu (auto/git-push/manual options + git-push-setup pointer) → one-line forward reference to `develop-strategy-review` which fires when actionable. |
| **KT-3** | `bootstrap-classic-plan-dynamic.md` | Removed *"Classic bootstrap deploys a minimal verification server per runtime with a `/status` endpoint"* (Option B). Replaced with truthful *"creates runtime + managed services with `startWithoutCode: true`"*. Dropped *"and deploy strategy"* phrase. |
| **KT-4** | `bootstrap-classic-plan-static.md` | Removed *"no verification server needed"* (implies others have it) + deployFiles details + deploy strategy mention. |
| **KT-5** | `bootstrap-runtime-classes.md` | Shrunk runtime-class definitions to "what KIND of service"; moved `zerops_dev_server` lifecycle, `run.start`, `healthCheck`, `documentRoot` mechanics into forward references to `develop-dynamic-runtime-start-{container,local}`, `develop-implicit-webserver`, `develop-first-deploy-scaffold-yaml`. |
| **KT-6** | `bootstrap-close.md` | Trimmed develop handoff checklist; iteration-cap detail (5-attempt cap) is develop-flow concern, removed from bootstrap. |
| **KT-7a** | `bootstrap-adopt-discover.md` | Dropped legacy "strategy state" vocab. Adoption now correctly described as leaving close-mode + git-push capability empty (per E2 invariant). |
| **KT-7b** | `idle-adopt-entry.md` | Same legacy vocab cleanup. |
| **KT-8** | `bootstrap-env-var-discovery.md` + `develop-first-deploy-env-vars.md` | Moved per-service canonical env-var usage table (10+ rows) from bootstrap to develop, where the agent actually wires `run.envVariables`. Bootstrap atom keeps only the discover command + forward reference. |
| **KT-10** | `bootstrap-intro.md` | Removed *"deploy a minimal verification server per runtime"* (Option B). Reframed as *"infrastructure-only (Option A since v8.100+)"* with explicit boundary. |

### Spec swept (KT-9)

| Section | Change |
|---|---|
| `spec-workflows.md:36-40` (§1.1 narrative) | Rewrote Phase 1 / Phase 2 / boundary paragraphs to drop "verification server is deployed" + "Bootstrap writes zerops.yaml" claims. New text matches Option A: bootstrap creates services with `startWithoutCode: true`, develop owns code + first deploy + `FirstDeployedAt` stamp. |
| `spec-workflows.md:65-77` (§"Why Verification-First") | Renamed to "Why Infrastructure-First". Rewrote all 4 principles to drop verification-server references — same architectural argument (fault isolation, universal deploy flow, reduced blast radius, faster develop iteration) restated against Option A mechanics (RUNNING status check, env discovery). |
| `spec-workflows.md:496-559` (§2.5 Step 3: Generate) | **Deleted entirely.** Step doesn't exist in code (`bootstrap_steps.go` has only Discover / Provision / Close). |
| `spec-workflows.md:560-614` (§2.6 Step 4: Deploy) | **Deleted entirely.** Same reason — Option B step retired. |
| `spec-workflows.md:615` (§2.7 Step 5: Close) | Renumbered to §2.5 Step 3: Close. Body unchanged. |
| `spec-workflows.md:641` (§2.8 Fast Paths) | Renumbered to §2.6. |
| `spec-workflows.md:655` (§2.9 Mode Behavior Matrix) | Renumbered to §2.7. |
| Cross-references: 3× `§2.8` → `§2.6` updated (lines 402, 587, 963). |
| `spec-workflows.md:635, 641` (§4.1 develop entry) | Removed *"Service has only a verification server"* + *"verification server from bootstrap or existing application"* phrasings; rewrote to match empty-filesystem reality. |

### Tests + pins updated

- `corpus_coverage_test.go:104-108` — bootstrap_classic_discover_dynamic fixture: replaced `"verification server"` MustContain pin with `"startWithoutCode: true"` + `"workflow=develop"`.
- `corpus_coverage_test.go:143-147` — bootstrap_recipe_close fixture: replaced `closeMode={"<hostname>":"auto|git-push|manual"}` pin with `closeMode: unset` + `develop-strategy-review` + `zerops_workflow action="start" workflow="develop"`.

### Verification

- All 26 packages: ✅ PASS
- Matrix simulator: 2 anomalies (unchanged — both size warnings just over my 25KB heuristic, under real 28KB cap)
- Bootstrap classic/provision atom count: **6 → 5** (`develop-platform-rules-common` no longer fires)
- Bootstrap briefing size: shrunk (`bootstrap-runtime-classes` lost lifecycle table; `bootstrap-env-var-discovery` lost 10-row reference table)
- Grep `verification server` across atoms: **0 hits** (was 3 atoms + 4 spec sections pre-sweep)

### Knowledge timing principle (now codified)

After this sweep, the principle is: **deliver knowledge when actionable, not when topical.** Atoms in bootstrap-active phase describe ONLY what the agent does during bootstrap (plan, import, mount, discover, write meta). Anything about close-mode, git-push capability, build-integration, deploy mechanics, dev-server lifecycle, or `zerops.yaml` shape lives in develop-active atoms (`develop-strategy-review`, `develop-first-deploy-*`, `develop-dynamic-runtime-start-*`) and fires when the agent reaches the moment of need.

Matrix simulator: **23 anomalies → 2** (after refining detector to honor stateless phases). Both remaining are size warnings just over my arbitrary 25KB heuristic — under the real 28KB MCP soft cap. Multi-runtime scenario shrunk 32.8 KB → 29.2 KB (~11%) thanks to P5-Lever-A.

---

## Scorecard

| ID | Original severity | Status | Citation |
|---|---|---|---|
| **C1** | CRITICAL | ✅ FIXED | `a70e6073` swept 40 files; grep for `action="strategy"` returns zero outside the deploy-decomp archived plan |
| **C2** | HIGH | ✅ FIXED | `internal/tools/deploy_git_push.go:354-360` documents the old auto-stamp bug; new flow requires explicit `record-deploy` |
| **C3** | HIGH | 🅱️ DEFERRED | `plans/backlog/c3-failure-classification-async-events.md`. Trigger: live-agent feedback on async build failure diagnosis. |
| **C4** | MEDIUM | ✅ FIXED | `develop-record-external-deploy.md:1-7` now `deployStates: [never-deployed]` + `buildIntegrations: [webhook, actions]` |
| **C5** | MEDIUM | ✅ FIXED + new lint | Vite block uses `zerops_dev_server`; `axis-hot-shell` lint at `atoms_lint_axes.go:280` prevents regression |
| **C6** | MEDIUM | ✅ FIXED | Folded into the C1 sweep |
| **C7** | LOW | ✅ FIXED | `plans/develop-flow-enhancements.md` archived as `develop-flow-enhancements-2026-04-20.md` |
| **C8** | LOW | ✅ FIXED | Test comments swept |
| **C9** | LOW | ❌ DISMISSED | Out-of-scope concern: recipes = stack templates, close-mode + git-push = develop-flow. Lifecycle path complete via `bootstrap-recipe-close` → `develop-strategy-review` → `setup-git-push-{container,local}`. Backlog plan deleted (see "Dismissed" section below). |
| **C10** | HIGH | ✅ FIXED | Shared `gitPushMetaPreflight` at `deploy_git_push.go:48-66`, called for both container + local at `:217` |
| **H1** | HIGH | ✅ FIXED (symptom) + 🅱️ structural deferred | `build_plan.go::deployActionFor` consults `services` to find dev half + emit `setup="prod"` cross-deploy. Structural target: `plans/backlog/deploy-intent-resolver.md` |
| **H2** | HIGH | ✅ FIXED | `develop-first-deploy-promote-stage.md:20` emits `setup="prod"` |
| **H3** | HIGH | 🟡 PARTIAL | P5 levers shrunk multi-service briefing 32.8 → 29.2 KB. Still over my 25KB heuristic but under real 28KB cap. No status-side overflow envelope yet. Decide: ship as-is or extend dispatch-brief overflow to status. |
| **M1** | MEDIUM | 🅱️ DEFERRED | `plans/backlog/m1-glc-safety-net-identity-reset.md` |
| **M2** | MEDIUM | 🅱️ DEFERRED | `plans/backlog/m2-stage-timing-validation-gate.md` |
| **M3** | MEDIUM | ✅ FIXED | `9e019a70` added `local-stage` / `local-only` axes to the three excluding atoms |
| **M4** | MEDIUM | 🅱️ DEFERRED | `plans/backlog/m4-aggregate-atom-placeholder-lint.md`. Caught one slip in P5-Lever-A by hand; lint not yet automated. |
| **L1** | LOW | ✅ FIXED | `bootstrap-route-options.md:29` row for recipe explains slug + collisions inline |
| **L2** | LOW | OPEN (cosmetic) | Plan rationale doesn't mention "first deploy ignores close-mode" — atom `develop-first-deploy-intro.md:24` covers it |
| **L3** | LOW | 🅱️ DEFERRED via discovery surfacing | `route.go:90` carries `Collisions[]` on the discovery response; the agent already sees it pre-commit |

Headline: **9 FIXED, 1 PARTIAL, 6 DEFERRED with explicit triggers, 1 OPEN cosmetic, 0 still surprising.** All deferrals trace to a backlog plan with rationale.

---

## NEW drift surfaced by Codex on the post-fix tree

Round-2 Codex pass found 8 issues — 7 are real new drift introduced by the fixes themselves or pre-existing places the original audit missed. Numbered N1-N7 (N8 is my matrix detector noise, already fixed inline).

### ~~CRITICAL~~ → DOWNGRADED (recipe-flow scope, not develop-flow)

#### ~~N1~~ — `zerops_deploy_batch` doesn't append to Work Session — **DISMISSED**

**Original framing (Codex)**: batch tool violates W4 invariant *"Deploy and verify tools append to Work Session"*.

**Re-scope (post-user-correction)**: `zerops_deploy_batch` is **recipe-authoring tool, not develop-flow tool**:
- Docstring at `internal/tools/deploy_batch.go:18-22`: *"Use for every 3-deploy cluster in a recipe run: initial dev, snapshot-dev, stage cross-deploy, close redeploys. Single-target redeploys still use `zerops_deploy` directly."*
- Zero `develop-active` atoms reference it (`grep -rln 'zerops_deploy_batch' internal/content/atoms/` returns empty)
- All atom-level guidance lives under `internal/content/workflows/recipe.md` and `internal/content/workflows/recipe/phases/deploy/*.md`
- W4 invariant (`spec-workflows.md:1115`) explicitly scopes to develop-flow Work Session — the recipe v3 engine has its own state model in `internal/recipe/`

The MCP tool IS registered globally (`server.go:188`), so a misbehaving develop-flow agent could discover it from the MCP catalog and call it. But no atom directs them to, and the recipe-flow scope is the documented contract. Adding W4-style writes would be misleading — a develop-flow agent calling a recipe-flow tool is an out-of-band move, not a supported flow.

**Action**: keep as-is. If the symptom ever surfaces during internal testing (a develop-flow agent using batch and getting confused state), revisit; otherwise this is closed.

### HIGH

#### N2 — `record-deploy` stamps success without checking actual build status

**Location**: `internal/tools/workflow_record_deploy.go:77` writes `RecordExternalDeploy` (stamps `FirstDeployedAt`); line 107 appends a synthetic successful `DeployAttempt` with `SucceededAt = time.Now()`. The handler does NOT check `zerops_events` or live `appVersion` status.

The "wait until ACTIVE" instruction lives ONLY in atom prose: `develop-build-observe.md:31` and the post-push hint at `deploy_git_push.go:366`.

**Failure mode**: this is the C2 fix's flip side. C2 said "don't stamp on push" and pointed at `record-deploy` as the bridge. But `record-deploy` is itself unguarded — agent calls it too early (build still running) → service flips `Deployed=true` → verify runs against stale code → may pass against the OLD build → auto-close fires → agent thinks task done while build still running → next session opens against stale state.

**Fix size**: structural — `record-deploy` should fetch latest `zerops_events`, find the most recent appVersion event, refuse if `Status != ACTIVE`. Or accept a `buildId` arg the agent must read from events first.

#### N3 — `record-deploy` mutates Work Session but doesn't return `WorkSessionState`

**Location**: `recordDeployResult` struct (`workflow_record_deploy.go:23`) has `Hostname / Stamped / FirstDeployedAt / Note / SubdomainAccessEnabled / SubdomainURL / Warnings` — **no `WorkSessionState` field**. Yet line 107 appends a deploy attempt that can change auto-close eligibility.

Other deploy-touching responses do: `deploy_git_push.go:381`, `verify.go:48+58`. **Missed `record-deploy` in the F5 sweep.**

**Failure mode**: agent calls `record-deploy` for the second of two scope services → auto-close fires server-side → agent's response carries no `workSessionState` → agent doesn't know the session closed → next `action=status` returns `develop-closed-auto` and agent has to reconcile.

**Fix size**: small — add `WorkSessionState *WorkSessionState` to the struct + populate it before return. Mirrors what `verify` and `deploy_*` already do.

#### N4 — `develop-close-mode-git-push` atom fires on broken / unconfigured git-push state

**Location**: `internal/content/atoms/develop-close-mode-git-push.md:5` axes are `closeDeployModes: [git-push]` ONLY — no `gitPushStates: [configured]` filter.

Confirmed in matrix scenario 8.6 (`git-push / broken / webhook`): atom fires, instructs `zerops_deploy ... strategy="git-push"`, handler at `deploy_git_push.go:106` rejects the call with `PushNotConfigured`.

**Failure mode**: agent on a broken-capability path is guided into a predictable preflight error instead of the recovery path (`setup-git-push-container` / `setup-git-push-local`).

**Fix size**: small — add `gitPushStates: [configured]` to the atom's frontmatter; create or reuse a `develop-close-mode-git-push-needs-setup` atom for the broken/unconfigured case.

### MEDIUM

#### N5 — `setup-git-push-container.md` claims `strategy="git-push"` does `git init`; handler requires existing committed repo

**Location**: `setup-git-push-container.md:45` — *"`zerops_deploy strategy="git-push"` handles `git init`, `.netrc` configuration, and `git remote add` internally — these are not separate manual steps."*

But `deploy_git_push.go:241` checks for `.git` directory + `:250` rejects with "git-push requires committed code" if no commit exists.

**Failure mode**: agent reads atom, skips the explicit `ssh "git init && git add . && git commit"` step (because the atom said it's automatic), runs `zerops_deploy strategy="git-push"`, hits the rejection. Loops back, finds the contradicting atoms, gets confused.

**Fix size**: small — atom rewrite to acknowledge the working-tree-must-have-a-commit gate, OR handler enhancement to do `git init && first commit` automatically (riskier — semantic creep).

#### N6 — `develop-close-mode-auto-local` includes `local-only` mode but handler rejects `auto` for `local-only`

**Location**: `develop-close-mode-auto-local.md:6` declares `modes: [dev, stage, local-stage, local-only]` and instructs `zerops_deploy targetService="{hostname}"` at `:18`.

But `workflow_close_mode.go:98` explicitly rejects `closeMode=auto` for `PlanModeLocalOnly` (with rationale: "local-only services have no Zerops runtime target — closeMode=auto needs one to push to"). And `deploy_strategy_gate.go::checkLocalOnlyGate` rejects default deploy for `PlanModeLocalOnly`.

**Failure mode**: a service whose meta is somehow stamped `Mode=local-only` + `CloseDeployMode=auto` (migration leftover, manual edit, or stale state) renders this atom — the agent gets impossible guidance. In practice unlikely because `close_mode` setter blocks the combination, but stale meta could surface it.

**Fix size**: small — drop `local-only` from the atom's `modes` axis.

#### N7 — Spec self-contradicts on git-push `FirstDeployedAt` gate

**Location**:
- `docs/spec-workflows.md:797` (§4.3) — *"Pre-flight gate (`zerops_deploy strategy="git-push"`): refuses with `PREREQUISITE_MISSING` when `FirstDeployedAt` is empty."*
- `docs/spec-workflows.md:1100` (D2b) — *"`handleGitPush` refuses with `PREREQUISITE_MISSING` when there is no committed code at the working directory ... The earlier `meta.IsDeployed()` / `FirstDeployedAt` gate was replaced because it false-positived on adopted services."*

Two adjacent sections describe the SAME gate with INCOMPATIBLE semantics. The implementation matches D2b (working-tree commit), so §4.3 is the stale text.

**Failure mode**: spec readers (LLM authors writing new atoms or tests) take the §4.3 wording as authoritative and re-introduce the `FirstDeployedAt` check or mis-document the behavior.

**Fix size**: small — rewrite §4.3 bullet to match D2b. Add `TestSpecD2b_HandleGitPush_RefusesWithoutCommit` or similar pin.

### LOW (already fixed)

#### N8 — Lifecycle matrix simulator marks intentional stateless phases as ERROR

Codex caught this — addressed inline this round. `lifecycle_matrix_test.go:88-92` now whitelists `PhaseStrategySetup` + `PhaseExportActive`. Anomaly count dropped 5 → 2.

---

## Lifecycle smell summary (Codex's verbatim assessment)

> Async git-push is still a convention more than a closed contract: push → build observation → `record-deploy` → verify → auto-close are spread across handler strings and atoms, but the bridge call trusts the agent's timing and does not return the same session-state signal as deploy/verify. The lifecycle is coherent when the agent follows the happy-path text exactly, but brittle under compaction, stale atoms, or early/late acknowledgements.

**My read**: N1 + N2 + N3 are the same root cause — the asynchronous deploy lifecycle (`zerops_deploy strategy="git-push"` → external build → `record-deploy`) was mostly cleaned at the push side (C2 fix removed auto-stamp), but the BRIDGE (`record-deploy`) and the BATCH path are still trusting the agent + lacking the session-state signal that `deploy` and `verify` now carry post-F5. Three small fixes (N3 small, N1 medium, N2 structural) close the gap.

---

## Updated fix order — Round 2

### Round 2A — close the F5 lifecycle signal gap (small, ship together)

1. **N3** — add `WorkSessionState` field + populate to `recordDeployResult`. Mirrors `verify.go:70+78`.
2. **N4** — add `gitPushStates: [configured]` to `develop-close-mode-git-push.md`; create or extend a `*-needs-setup` variant for unconfigured/broken/unknown.
3. **N5** — rewrite `setup-git-push-container.md:45` to acknowledge the commit gate.
4. **N6** — drop `local-only` from `develop-close-mode-auto-local.md:6` modes axis.
5. **N7** — rewrite `spec-workflows.md:797` bullet to match D2b at §8; add a test pin.
6. **L2** — add "first deploy ignores close-mode" rationale string to the deploy `Plan.Primary` action when `deployStates=[never-deployed]`.

### Round 2B — async lifecycle hardening (medium-to-structural, after live-agent feedback)

7. **N1** — wire `RecordDeployAttempt` per host into `deploy_batch.go`. Add `TestDeployBatch_RecordsPerHostAttempts` pin.
8. **N2** — gate `record-deploy` on a live `zerops_events` lookup OR require an explicit `buildId` arg the agent must read from events first. STRUCTURAL.
9. **H3** — extend dispatch-brief overflow envelope to status responses. Trigger: matrix briefing > 28 KB, or live-agent context-window pressure.

### Round 3 — backlog promotions (when triggers fire)

10. **C3** — `failureClassification` on TimelineEvent. Trigger: live-agent feedback on async build failure diagnosis.
11. **C9** — recipe git-push scaffolding. Trigger: recipe-bootstrap → git-push UX friction.
12. **M1, M2, M4** — defenses-in-depth from the original audit's M-tier.
13. **`deploy-intent-resolver`** — H1 structural follow-up after a third symptom-fix surfaces.
14. **`auto-wire-github-actions-secret`** — build-integration=actions zero-touch UX after manual snippet path proves the demand.

---

## How to re-verify after Round 2A lands

```sh
ZCP_RUN_MATRIX=1 go test ./internal/workflow -run TestLifecycleMatrixDump -count=1
# expect: anomaly count 2 → 0 (or only size warnings if multi-runtime briefing didn't shrink further)

go test ./internal/tools -run 'TestRecord|TestDeployBatch' -count=1
# expect: new pin tests for N1+N3 land green
```

The matrix simulator is the cheapest signal — re-run after each atom edit. The lint axes (`axis-hot-shell` from C5, plus M4's planned aggregate-placeholder lint when it lands) catch regressions at PR time without needing the matrix.

---

## Recommended runtime × recipe coverage matrix (unchanged from Round 1)

The 9-scenario matrix from `audit-prerelease-internal-testing-2026-04-29.md` remains the recommended cross-product cover. Re-run those after Round 2A lands; if N2 isn't fixed, watch specifically for premature `record-deploy` calls against webhook-built services.
