# Fix Plan — eval review 20260518 subset

Based on three deep-research reports in `deep-research/`. Eight root-cause findings originally; **#4 (runtime-class OS-prefix) split out to `plans/backlog/zerops-yaml-os-prefix-shape-drift.md`** because Karel's clarification revealed it's a Zerops-upstream-yaml-shape drift that needs its own audit pass, not a 5-line strip. **#3 reworded** after Karel's clarification that cross-deploy doesn't touch dev source. Seven items remain in this plan.

## Headline

Most remaining findings are **parallel-path drift** — same fact encoded two ways at different surfaces (atom text vs. handler, snapshot vs. live, error code at site A vs. site B). The discipline is the same throughout: pick one canonical form per concept, normalize at every boundary, remove the alternate. Phases 1–2 land structural-but-low-risk fixes; Phase 3 needs Karel to pick between two semantics for `http_root`; Phase 4 backlog covers items that need a design pass.

Verified prefix (correcting earlier synthesizer error): MCP server name is `zerops` per `internal/content/templates/mcp-config.json` → tools register as `mcp__zerops__zerops_*`, NOT `mcp__zcp__zerops_*`.

---

## Phase 1 — Mechanical / parallel migration (~3-5 files each, low risk)

| # | Item | File:line | Fix | Test |
|---|---|---|---|---|
| 1.1 | **ToolSearch `select:` bare prefix** | `internal/content/atoms/{idle,bootstrap,develop}-tool-preload.md:16-17` | Replace `select:zerops_workflow,zerops_…` → `select:mcp__zerops__zerops_workflow,mcp__zerops__zerops_…` (all 3 atoms). | New atom-lint axis in `internal/content/atoms_lint.go`: any `select:` token must start with `mcp__zerops__`. |
| 1.2 | **`SERVICE_NOT_FOUND: not bootstrapped` parallel migration miss** | `internal/tools/workflow_git_push_setup.go:79-83` + `internal/tools/workflow_build_integration.go:75-80` | Copy the `close_mode.go:90-102` pattern: replace `platform.ErrServiceNotFound` + `WithRecoveryStatus()` → `platform.ErrAdoptRequired` + `WithRecovery(&RecoveryHint{Tool:"zerops_workflow", Action:"start", Args:{workflow:"bootstrap", route:"adopt"}})`. Update error text to mention adopt route. | Extend `TestErrAdoptRequiredCarriesAdoptRecovery` table with 2 new rows. |
| 1.3 | **Cross-deploy atom language is ambiguous — agent misreads** | 3 atoms: `develop-first-deploy-promote-stage.md:24-25`, `develop-close-mode-auto-standard.md:29`, `develop-standard-unset-promote-stage.md:25` | Atom isn't factually wrong — Karel confirms: cross-deploy (dev→stage) doesn't touch dev source; dev source is packaged and **stage builds it** (stage runtime is overwritten, dev stays as-is). But "no second build" / "without a second build" reads to the agent as "no build happens at all," so when response shows `buildStatus: ACTIVE` the agent thinks the atom lied. Rephrase to make WHO-doesn't-change vs WHO-builds explicit: "Cross-deploy packages the dev source and builds it on stage (stage runtime overwritten; dev source and dev runtime unchanged)." Avoid the phrase "no second build." | Rely on re-run regression — atom-content lint can't easily catch ambiguous prose. |

**Verifies:** `develop-loop-after-bootstrap` (1.1), `delivery-git-push-actions-setup` + `git-push-setup-with-cicd-method-prompt` (1.2), `cross-deploy-stage-promote-from-dev` (1.3).

---

## Phase 2 — Recovery wiring + mount-claim (medium risk, single-site root cause + downstream self-correct)

(Item formerly numbered 2.1 — classifier OS-prefix strip — is now `plans/backlog/zerops-yaml-os-prefix-shape-drift.md` per Karel's clarification.)

| # | Item | File:line | Fix | Test |
|---|---|---|---|---|
| 2.1 | **`develop-ready-to-deploy.md` atom never fires** — D1 | `internal/workflow/bootstrap_guide_assembly.go:162-183::planTargetSnapshots` | Populate `Status` field from live discover state when building `ServiceSnapshot`. Mirror what `handleLifecycleStatus` already does. | New `TestPlanTargetSnapshots_PopulatesStatusFromLive` pinning Status field surfaces correctly for adopted services in `READY_TO_DEPLOY`. |
| 2.2 | **`WAITING_TO_BUILD` not recognized as failure phase** — D2 | `internal/ops/deploy_failure.go:40-50::FailurePhaseFromStatus` | Either (a) add `WAITING_TO_BUILD → "init"` mapping, or (b) change discriminator at `non_running_recovery.go:36-47` from `LatestFailedAppVersionContext != nil` to "service is in any non-running state." (a) is smaller but masks intent; (b) is cleaner. **Recommend (b).** | New `TestNonRunningRecovery_WaitingToBuildPointsAtImport` + extension of `TestFailurePhaseFromStatus` table. |
| 2.3 | **`/var/www/{hostname}/` mount-claim — tier-1 conditional fix** | `internal/content/templates/claude_container.md:3` | Replace unconditional claim with conditional: "After bootstrap or adopt provision closes, service code mounts at `/var/www/{hostname}/`. If `ls /var/www/{hostname}/` returns empty, the service hasn't been bootstrapped — run `workflow=bootstrap route=adopt`." | New `TestClaudeContainerShim_MountClaimConditional` asserting word "after" precedes the path. |

**Verifies:** `recover-failed-buildfromgit-missing-dep` (2.1 + 2.2); `cross-deploy-stage-promote-from-dev` + `resume-after-compaction` (2.3).

---

## Phase 3 — Verify semantics (needs Karel's design call)

Verify reports `http_root: pass` on HTTP 404 because `verify_checks.go:134` deliberately gates pass on `<500`. The pinned test `TestCheckHTTPRoot_NonFailingStatuses:606-662` was added to fix a real production-showcase issue ("every showcase apidev flagged degraded because `/api/health` works but `/` returns 404"). Two options:

### Option (a) — Gate `pass` on non-4xx/5xx

| File:line | Fix |
|---|---|
| `internal/ops/verify_checks.go:100-134` | Change pass criterion: `4xx → fail with detail "HTTP {code} at /: server reachable but root path not served"`; `5xx → fail`; `2xx/3xx → pass`. |
| `internal/ops/verify_test.go:606-662 TestCheckHTTPRoot_NonFailingStatuses` | Invert: 404/401/405 now expected as fail. Update docstring to explain the trade-off Karel chose. |
| `internal/content/atoms/develop-first-deploy-verify.md:16` | Update prose: "verify reports fail on 404 — that's the framework saying no route matches `/`; not a server fault. Either verify a real endpoint or accept the fail as cosmetic." |

**Trade-off:** Re-opens the "showcase apidev /api/health-works-but-/-404" surface that motivated 64e44be7. Atom prose must land in lockstep, OR the next user re-trips the same fix decision.

### Option (b) — Split into `http_reachable` + `http_responsive`

| File:line | Fix |
|---|---|
| `internal/ops/verify_checks.go` | Two checks. `http_reachable`: TCP/HTTP responds at all (any HTTP code). `http_responsive`: non-4xx/5xx response code. Both surfaces returned in `zerops_verify` summary. |
| `internal/ops/verify.go` | Orchestrator emits both. |
| Atoms | Rewrite verify-related atoms to reference the new vocabulary. |

**Trade-off:** Cleaner long-term, but ~10x more files touched. Both checks need pinning tests, recovery hints, atom alignment.

**Recommendation in plan:** Land Option (a) now (1-2 hour patch + atom lockstep). Backlog Option (b) as a separate design pass.

**Verifies:** `api-node-postgres-classic-dev`.

---

## Phase 4 — Backlog candidates (separate plans / design pass needed)

| # | Item | Status |
|---|---|---|
| 4.1 | **Zerops yaml OS-prefix shape drift** (was Phase 2.1 — classifier strip) | **Moved to backlog**: `plans/backlog/zerops-yaml-os-prefix-shape-drift.md`. Reason: classifier strip is the visible leaf; the actual drift surface spans `runtime_class.go`, plan validator, recipe yamls, atom inline-yaml, render goldens. Needs an audit pass + canonical-form decision, not a 5-line strip. |
| 4.2 | **`sourceMountAvailable: bool` in discover response** (tier-2 for finding B) | Backlog. Phase 2.3 conditional text covers the immediate symptom. Tier-2 structural change (add boolean to `zerops_discover` response, atoms branch on flag) is the right long-term shape but ~8 files touched; defer if 2.3 holds in re-run. |
| 4.3 | **Composite-type plan-vs-yaml asymmetry** (finding G) | Backlog. Related to 4.1 (same canonical-form question, different axis: `postgresql:single@18` vs `postgresql@18 + mode: NON_HA`). Plan validator `validate.go:387-397::typeExists` does literal match against composite-only forms; import-yaml API accepts both. Needs design pass — not a point fix. Bundle with 4.1's audit if both promoted together. |
| 4.4 | **Cross-project SERVICE_NOT_FOUND scope confusion** | Backlog. Separate root cause from 1.2: `FindServiceMeta` has no way to distinguish "wrong project entirely" from "right project, no ZCP meta." Needs platform-side disambiguation. Out of scope for Phase 1.2 migration. |
| 4.5 | **eval-infra: `cadence-multiservice-build` 15-min user-sim cap** | Backlog. `cadence-multiservice-build` deploy poll was in-flight at 15-min cutoff (NOT a 49s stall — synthesizer misread `scenarioWallTime` of first-turn deliberation). Either widen user-sim wall budget for multi-service scenarios, or split scenario. |
| 4.6 | **eval-infra: `classic-rust-postgres-standard` 30-min context-deadline** | Backlog. Same scenario succeeded in 23m41s on 20260517, timed out at 30m on 20260518. Needs partial-transcript triage before next subset re-run to identify the wedge. |

---

## Risk summary

- **Phase 2.2 (recovery discriminator)** changes which Recovery surfaces for which non-running state. Cross-test BOTH develop-adopt (`recover-failed-buildfromgit-missing-dep`) AND launch-production (any of the 4 launch-production-* scenarios) for non-regression — recovery shouldn't get worse on the path `33fb9358` already fixed.
- **Phase 3 (http_root)** Karel's call. Option (a) is irreversible without re-doing the showcase fix; Option (b) is broader work but doesn't burn the existing pinned test.
- **Phase 1.2 (parallel migration)** completely mechanical, no design risk. Pattern is in `close_mode.go:90`; tests already exist.
- **Phase 2.3 (mount-claim conditional)** text edit; agent re-evaluation only.
- **Phase 1.3 (cross-deploy atom prose)** text rephrase only; semantic content is correct — just removes the ambiguous "no second build" obstacle.

---

## Re-run plan (verifies fix landed)

### After Phase 1 (subset ~5 scenarios, ~50 min wall)

`develop-loop-after-bootstrap`, `delivery-git-push-actions-setup`, `git-push-setup-with-cicd-method-prompt`, `cross-deploy-stage-promote-from-dev`, `launch-production-pipeline-not-configured`.

### After Phase 2 (subset ~4 scenarios, ~50 min wall)

`recover-failed-buildfromgit-missing-dep`, `cross-deploy-stage-promote-from-dev`, `resume-after-compaction`, and one launch-production scenario (cross-test for 2.2 non-regression).

### After Phase 3 (subset ~3 scenarios)

`api-node-postgres-classic-dev`, `develop-loop-after-bootstrap`, one greenfield to check JIT-compile timeout still surfaces or got better.

---

## Order-of-attack recommendation

1. **Phase 1.1** (ToolSearch prefix) — 5 min, dirt simple, highest-frequency unfixed.
2. **Phase 1.2** (parallel migration) — 20 min, mechanical, copy `close_mode.go` pattern.
3. **Phase 2.3** (mount text) — 10 min, text edit.
4. **Phase 1.3** (cross-deploy atom rephrase) — 10 min, text edit per Karel's clarification.
5. **Phase 2.1 + 2.2** (recovery wiring) — 1-2 hr, careful + cross-tested.
6. **Phase 3** (verify semantics) — Karel picks option, then 1-2 hr including atom lockstep.
7. Re-run regression subset to verify.

Phase 4 items already in `plans/backlog/` (item 4.1) or to be filed there per CLAUDE.local.md backlog workflow.
