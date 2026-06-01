# Launch-Family Validation Verdict — launch-production · export · cicd · git-push

**Updated 2026-06-01** (re-verified against the current checkout). Original analysis 2026-05-29.

**Question:** is the "ship to production" family (launch-production + export + cicd/build-integration
+ git-push) completely + correctly implemented, reliable, and functional?

**Method (3 independent passes):** (1) read the 3 prior audits + spec §9/§10 + 57 family commits;
(2) a 17-agent default-refute workflow re-derived every family finding against the code — 11/11
high+critical verified real, 0 refuted; (3) empirical: compile + unit-green, deterministic
repros (LAUNCH-1, XCUT-1), live composer→real-import against eval-zcp.

---

## Branch context — read first (this is what changed since the 2026-05-29 verdict)

The original verdict was derived from `env-lifecycle-fundamental-fixes` @ `e91aba8e`. That work is
now **already on `origin/main`** (`1e99f5f5`) — verified via `git cherry origin/main
env-lifecycle-fundamental-fixes` (all 31 commits show `-` = patch-equivalent on main; env(E7),
RC1–RC3, E3, "launch: surface bundle warnings", "P0 push-blockers" all present on `origin/main`).
The local `env-lifecycle-fundamental-fixes` branch is therefore **redundant**.

- **`origin/main` (`1e99f5f5`) already contains** the full launch family (P1–P4, git-push Phase 3)
  AND every env-lifecycle fix (incl. env-F1). So my original verdict was effectively against current
  main.
- **The only unpushed branch with NEW work is `session-lifecycle-rebuild` (`1de7cdc3`) = main + 9
  commits (P0–P7, "session & lifecycle rebuild").** It branches from the current main tip
  (`merge-base(main,HEAD) == origin/main == 1e99f5f5`), so it applies **cleanly — no merge conflict
  risk** (earlier "two diverging branches" framing was wrong; env-lifecycle is already on main).
- HEAD's 9 commits change `service_meta.go`, `adopt_local.go`, `compute_envelope.go`, the dimension
  handlers, `workflow.go` — NOT `workflow_launch_production.go`/`launch_pipeline.go` (which is why
  LAUNCH-1/2/3 are untouched + still open).

**Independent Codex review (2026-06-01) confirmed all 12 spot-checked claims** against the code with
its own file:line evidence — XCUT-1 + WF-5b genuinely fixed; all 10 open findings genuinely open; no
product-code verdict wrong; no missed family-impacting change. Its only correction was the
branch/merge framing above (now fixed).

### What the session-lifecycle-rebuild commits FIXED (re-verified this pass, with proof)

| Finding | Was | Now | Proof |
|---|---|---|---|
| **XCUT-1** (ServiceMeta RMW race) | #1 critical OPEN | **FIXED** | New `UpdateServiceMeta`/`UpsertServiceMeta` wrap RMW in a `withFileLock` flock; **all ~15 RMW sites routed through it (0 raw `WriteServiceMeta` callers remain)**; new `TestServiceMetaWriteIsLocked` lint forbids the raw pattern. Empirical: raw path 200/200 damaged → **locked path 0/200 damaged**. |
| **WF-5b** (launch status recovery shadowed by bootstrap) | OPEN | **FIXED** | `workflow.go:459-476` — launch demoted from the status preempt to a `FocusIdle` project-overlay injected post-`ComputeEnvelope` (plan I10); "no longer preempts and hides develop." |
| cross-mutex inconsistency (`stampFirstDeployedAt` vs dim handlers; `degradeGitPushStateToBroken` 6th unlocked site) | OPEN (missed-defects) | **FIXED** | all now route through the single `.services.lock`. |
| Adjacent spine (out of family scope, noted): SPINE-1, SPINE-4, derived auto-complete, (pid,startTime) liveness | OPEN | **FIXED** (P3–P6) | `TestEngine_HostnameLock_BlocksStageHalf` etc. green. |

WF-5 *core* (launch runs a state model parallel to the spine) is now a **deliberate design choice**
(launch = project overlay per I10), not a bug — reframed from "owner-decision" to "by-design."

**Everything else below remains OPEN** — re-verified this pass against the current branch; the
session-lifecycle commits did not touch those files (the touched family files —
`deploy_git_push`, `export_probe`, `close_mode`, `build_integration` — changed *only* to route
through the XCUT-1 lock; the substantive findings there live in untouched files).

---

## Verdict in one line

**Core (bundle/composer + platform-mutation) is sound and live-verified; the one data-corruption
race (XCUT-1) is now fixed. But the HANDLER ORCHESTRATION layer is still unreliable — multiple
verified silent success-on-failure bugs remain.** The simple single-runtime new-project happy path
works; every still-open defect lives where two things meet (source≠prod hostname, single vs
multi-runtime, stage-half, container vs local). **Not production-trustworthy until LAUNCH-1/2/3 +
WF-1 land.**

`go test ./...` green is still misleading: unit fixtures use bug-masking shapes (same-name
source/prod, single-runtime), and **the `//go:build live` suite — Karel's "no release until each of
the 4 flows passes a live test" gate — does not compile** (`handleGitPushSetup` +3 params;
`LaunchBundleInputs` 6 fields restructured for multi-runtime). The family still has **no compiling
live coverage** of the handler layer.

---

## Empirical proof (current branch, this pass)

| Check | Result |
|---|---|
| Family unit/tool tests (`tools ops bundle cicd checks workflow topology`, `-short`) | **green** |
| `//go:build live` suite compiles | **NO** — bit-rotted; handler refactors never propagated |
| **XCUT-1** repro — raw `WriteServiceMeta` path | 200/200 damaged (why the lint exists) |
| **XCUT-1** repro — locked `UpdateServiceMeta` path (what handlers now use) | **0/200 damaged — FIX HOLDS** |
| **LAUNCH-1** repro (source `appdev` vs prod `app`) | **STILL REPRODUCES** — 0 platform probes, `atom="launch-pipeline-configured"`, 0 blockers; control (source==prod) probes once + nags |
| Launch composer → live `CreateAndImportProject` (eval-zcp) | **ACCEPTED** with Discover-canonical type `alpine/php-nginx@8.4` (project created + deleted) |

---

## What WORKS (verified — don't touch)

XCUT-1 (now fixed) · WF-5b (launch demoted) · P-LP-1/3/4 · **P-LP-10/11 (buildFromGit
source-of-truth gate — recipe-template + never-pushed loopholes closed)** · P-LP-12 · env-F1
(warnings surfaced both mutation paths) · export 7-state narrowing happy path · bundle E1/E2/E4 ·
build-integration handler gate + role-echo + gh-auth precondition · git-push-setup **probe-first**
(failed probe writes zero state) · **launch composer → import is platform-valid (live-confirmed).**

---

## What's STILL BROKEN (all OPEN, re-verified this pass against `1de7cdc3`)

| ID | Sev | Defect (current file:line) | User-visible failure |
|---|---|---|---|
| **LAUNCH-1** | **crit** | `launch_pipeline.go:159` keys check on source hostname; bundle imports prod hostname (`appdev`→`app`) — file untouched | every dev/stage promotion reports "CD configured" with **zero** integration probes; resume loop also dead (passes dev-half too) |
| **LAUNCH-2** | high | `launch_existing.go:347-382` reports `launched`/audit `success` on per-service `ImportError`; never polls processes | user deletes one-shot key, believes prod live; prod partial/broken, no signal |
| **LAUNCH-3** | high | read-side gate validates only `TargetService`; publish-side loops all runtimes | 2-runtime launch w/ unconfigured B passes read-side → **wastes the one-shot launchKey** at publish |
| **LAUNCH-4** | high | `Promotables[]` multi-runtime path has **zero** end-to-end test coverage | green suite proves nothing about the multi-runtime surface |
| **WF-1** | high | `deploy_intent.go:149/158/164` hardcodes `RecipeSetupProd/Dev`, never reads `SetupName` — file untouched | committed `.github/workflows/zerops.yml` bakes `--setup prod`; renamed setup honored at deploy but not CI |
| **EXPORT-1** | high | `workflow_export.go:133 sourceMode := meta.Mode` (not `ModeFor`) — file untouched | stage-half export resolves wrong variant + wrong setup candidates + corrupts `pickSetupName` |
| **GAP0-1** | high | bundle carries only project envs; drops per-service userData layer — `bundle/inputs.go` untouched | `zerops_env set serviceHostname=X` value silently missing on re-import; no warning, no review row |
| **XCUT-2** | high | `workflow_git_push_setup.go:435 _,_ = pollManageProcess(...)` then unconditional `configured` stamp (`:441`) | restart timeout → state says configured + "GIT_TOKEN live in shell"; next git-push deploy fails cryptically |
| **GAP4-1** | high | `BuildGitOriginSyncCommand` has no `test -d .git \|\| git init` guard the deploy path has | swallowed bootstrap git-init failure → git-push-setup crashes on missing `.git` |
| **TOPO-1/WF-2/DELIV-2** | high | `adopt_local.go` case-1 (local-stage) + default still omit `GitPushState`/`BuildIntegration`; `compute_envelope.go:234-235` still raw-copies both (only `CloseDeployMode` normalized) | git-push-setup + build-integration atoms never fire for empty-dim local-stage metas (the mode they target) |
| cicd-DEAD | med | `internal/ops/cicd` fully dead; live path uses bare `ZEROPS_TOKEN` | their own comment: "Phase 6b will wire that composer" — never shipped |
| DEPLOY-3 | med | env-ref preflight on container git-push, not local git-push | bad `${peer_var}` → actionable in container, silent remote build failure locally |
| WF-5 (core) | by-design | launch overlay state model (now formalized as I10) | acceptable; recovery now works (WF-5b fixed) |
| WF-1-export-variant | cleanup | export `variant` dimension inert (`_ = variant`) | extra MCP round-trip + gate that changes nothing |

---

## Suggested fix order (state-integrity done → false-success → seams → cleanup)

1. ~~XCUT-1~~ **DONE** (session-lifecycle-rebuild P2).
2. **LAUNCH-1 + LAUNCH-2** — key pipeline check on prod hostname + iterate all runtimes (fix resume too); existing-project path must poll + flip to failed on `ImportError`. *Both report success on failure.*
3. **WF-1** — `Resolve` reads `SetupName`/`StageSetupName`; pin a renamed-setup committed-CI test.
4. **LAUNCH-3 (+LAUNCH-4)** — run read-side gate over `resolveLaunchRuntimes`; add a real 2-runtime test.
5. **XCUT-2, GAP4-1, EXPORT-1, GAP0-1, TOPO-1, DEPLOY-3** — parallel-path parity + swallowed-flag + empty-dim-normalization sweep.
6. **Repair the `live` suite** (re-establish the 4-flow gate) + decide cicd-dead / export-variant.

---

## Validation harnesses created (build-tagged, invisible to default build/CI)

`internal/tools/zz_familyval_test.go` — LAUNCH-1 repro + XCUT-1 raw-vs-locked comparison;
`internal/tools/zz_familyval_live_test.go` — live launch-import baseline. These are the missing
RED/regression pins the audits said "is how the bug slipped" — keep as the basis for the fixes, or
delete. **No product code changed by this validation work.**
