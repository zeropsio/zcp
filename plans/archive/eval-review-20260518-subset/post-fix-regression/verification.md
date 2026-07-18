# Post-fix regression verification — 2026-05-18

## Headline

Of the 8 distinct fixes Karel landed across phases 1-3 (commits `9314f772`, `793e088d`, `db16345e`), **7 are CONFIRMED-FIXED** in the re-run and **1 is PARTIALLY-FIXED** (Phase 2.3 mount-claim: 2.3's "If empty → bootstrap-adopt" framing helps the not-yet-adopted case but mis-directs platform-side-only tasks where the mount is irrelevant by design — surfaces in cross-deploy + launch-production-dev-only). Top RC reproductions from the prior synthesis are all gone in their primary scenarios: `ToolSearch` bare-prefix dance, `http_root: pass` on 404, `SERVICE_NOT_FOUND: not bootstrapped`, and the `READY_TO_DEPLOY` 3-call destructive dance. No regressions detected on the launch-production cross-tests.

## Per-fix verdict matrix

| Phase | Scenario | Verdict | Evidence (quoted from self-review or transcript) |
|---|---|---|---|
| 1.1 ToolSearch prefix | develop-loop-after-bootstrap | CONFIRMED-FIXED | Transcript: 13 calls all `select:mcp__zerops__zerops_*`, 0 bare-prefix calls, 0 "No matching deferred tools found". Self-review never mentions the dance — it's gone. |
| 1.2 git_push_setup ErrAdoptRequired | delivery-git-push-actions-setup | CONFIRMED-FIXED | Transcript: handler now returns structured `{"code":"ADOPT_REQUIRED","recovery":{"tool":"zerops_workflow","action":"start","args":{"route":"adopt","workflow":"bootstrap"}}}`. Self-review never mentions `SERVICE_NOT_FOUND`. |
| 1.2 build_integration ErrAdoptRequired | git-push-setup-with-cicd-method-prompt | CONFIRMED-FIXED | Same `ADOPT_REQUIRED` + structured recovery shape. Agent followed recovery first try, completed integration. Self-review: "The error did give me the right recovery (`workflow=bootstrap route=adopt`)." |
| 1.2 cross-test | launch-production-pipeline-not-configured | CONFIRMED-FIXED (NO REGRESSION) | Returns `{"code":"ADOPT_REQUIRED","error":"Service \"appstage\" is not bootstrapped","recovery":{...}}`. Friction shifted to "build-integration can't see prod project" (different RC, pre-existing). No regression on the path 1.2 touched. |
| 1.3 cross-deploy atom rephrase | cross-deploy-stage-promote-from-dev | CONFIRMED-FIXED | Self-review never complains about "no second build" misleading. Build with `buildStatus: ACTIVE, buildDuration: 1m23s` ran; agent did not flag the atom as deceptive. The prior "I read that as 'no build runs at all'" frustration is absent. |
| 2.1 Status field populated | recover-failed-buildfromgit-missing-dep | CONFIRMED-FIXED | First `zerops_verify` returns `"detail":"service status: READY_TO_DEPLOY"` with structured `recovery: {tool:"zerops_import", action:"import", args:{override:"true", startWithoutCode:"true"}}` — Status surfaces live. |
| 2.2 NonRunningRecovery broadened | recover-failed-buildfromgit-missing-dep | CONFIRMED-FIXED | Above Recovery struct fires on `READY_TO_DEPLOY` (no `LatestFailedAppVersionContext`-gating). Transcript shows 0 `DIAGNOSIS_REQUIRED`, 0 `wouldDestroy`, 0 `confirmDestructive` — prior 3-call dance entirely absent. |
| 2.2 non-regression | launch-production-pipeline-not-configured | CONFIRMED (NO REGRESSION) | Adopt-required path still surfaces correctly; no destructive-ack misfires. |
| 2.2 non-regression | launch-production-dev-only | CONFIRMED (NO REGRESSION) | Flow reached `ready-to-launch` (then stalled on fake tokens — unrelated). No spurious DIAGNOSIS_REQUIRED, no destructive flow misfires. |
| 2.3 mount-claim conditional | resume-after-compaction | CONFIRMED-FIXED | `claude_container.md:3` now reads "After bootstrap or adopt provision closes, service code SSHFS-mounts..." Agent ran bootstrap+adopt as first move; transcript shows `autoMounts: [{hostname:"appdev", mountPath:"/var/www/appdev", status:"MOUNTED"}]` after close. Self-review never re-litigates the mount lie. |
| 2.3 mount-claim conditional | cross-deploy-stage-promote-from-dev | PARTIALLY-FIXED | Conditional text landed but new framing still mis-directs the agent: "CLAUDE.md says 'if `ls /var/www/{hostname}/` is empty... bootstrap+adopt first.' That's wrong (or at least an over-broad heuristic) for a 'promote existing build' task." Platform-side-only ops (cross-deploy, env, scale) don't need a local mount. |
| 3 http_root fails on 4xx | api-node-postgres-classic-dev | CONFIRMED-FIXED | Transcript: `"http_root","status":"fail","detail":"HTTP 404: ...Cannot GET /..."` (prior: was `pass` on 404). Agent noticed, added root route, got `"status":"pass","httpStatus":200`. Self-review: "Verify 'degraded' status from 404 on `/`... Detail says directly ('server reachable but root path not serving a 2xx/3xx — verify a real endpoint or accept as cosmetic')." New atom prose landed in lockstep. |

## Detail per scenario

### develop-loop-after-bootstrap (Phase 1.1)
**Previously broken:** ToolSearch `select:` with bare `zerops_*` prefix failed with "No matching deferred tools found"; agent had to retry with full `mcp__zerops__zerops_*` form.
**Now:** CONFIRMED-FIXED. Atom `idle-tool-preload.md` documents the fully-qualified form; agent's first ToolSearch call uses `select:mcp__zerops__zerops_workflow` directly. 13 select calls in the transcript, all prefixed.
**Other observations:** Self-review surfaces a new clean signal — agent now relies on `availableStacks` block and `failureClassification.likelyCause` for type/format discovery and recovery. Three NEW-but-already-known frictions still flagged (OS-prefix on plan-step + composite-type, subdomain not auto-enabling on first import despite `enableSubdomainAccess: true`, develop-active guidance wall). All are pre-existing surfaces, not regressions.

### cross-deploy-stage-promote-from-dev (Phases 1.3 + 2.3)
**Previously broken:** (1.3) Cross-deploy atom said "no second build" / "without a second build" but response showed `buildStatus: ACTIVE` and `buildDuration: 1m9s`; agent felt lied to. (2.3) Mount-claim absolute.
**Now:** 1.3 CONFIRMED-FIXED. Self-review describes the cross-deploy as it actually works ("the deploy tool operates over SSH directly to the containers and doesn't need the local mount populated"); zero "no second build" complaints. 2.3 PARTIALLY-FIXED — new conditional text is still being read as universal: "Empty mount ≠ unbootstrapped service; it just means 'nobody has adopted this into the local working tree.' When the task is platform-side only (cross-deploy, env, scale, logs), skip the local mount entirely."
**Other observations:** Mixed-OS dev/stage pair (ubuntu/nodejs@22 + alpine/nodejs@22) noted as informational only — agent picks `setup=prod` correctly via the deploy-tool `suggestedAction`.

### api-node-postgres-classic-dev (Phase 3)
**Previously broken:** `http_root: pass` on HTTP 404 with body "Cannot GET /". Agent didn't notice 404, claimed service healthy on bare-Express scaffold.
**Now:** CONFIRMED-FIXED. `http_root: fail` with detail explaining "server reachable but root path not serving a 2xx/3xx — verify a real endpoint or accept as cosmetic." Agent paused to read response twice, recovered by adding a `/` route, got `pass` on retry. Self-review explicitly cites the new atom prose: "Detail says directly... so it's not broken, just cosmetic."
**Other observations:** No regression on the rest of the verify pipeline. Other NEW signals are pre-existing: composite-type asymmetry, npm ci no-lockfile (handled via `failureClassification`), close-mode gating.

### recover-failed-buildfromgit-missing-dep (Phases 2.1 + 2.2)
**Previously broken:** `READY_TO_DEPLOY` recovery dance — agent had to read buried instructions, hesitate on destructive warning, three-call sequence (DIAGNOSIS_REQUIRED → first reject → confirmDestructive → accept).
**Now:** CONFIRMED-FIXED on both. First `zerops_verify` against the broken `api` service returns structured `{recovery: {tool: "zerops_import", action: "import", args: {override: "true", startWithoutCode: "true"}}}` — single hop. Status field surfaces as `"detail":"service status: READY_TO_DEPLOY"`. Transcript shows zero `DIAGNOSIS_REQUIRED`/`wouldDestroy`/`confirmDestructive` calls.
**Other observations:** Self-review's "READY_TO_DEPLOY services need a destructive re-import to become deployable" still notes the destructive-warning copy is loud but no longer load-bearing — agent now reads the structured Recovery and acts. New friction surfaces are Python-specific (vendored binaries not on PATH; `pip install --target=./vendor`), not platform mechanics.

### resume-after-compaction (Phase 2.3)
**Previously broken:** Unconditional `/var/www/{hostname}/` mount-claim led agent to expect mount on every fresh session.
**Now:** CONFIRMED-FIXED. Self-review never re-litigates the mount lie. Agent's first move was bootstrap+adopt; `autoMounts` was reported in the response; agent proceeded. The only mount-related text in the self-review is now about the discovery-floor rule when "session may exist": "I ran both [status + discover] in parallel and that was the right call" — an atom-content phrasing observation, not a mount-lie complaint.
**Other observations:** Mixed-OS warning (ubuntu+alpine pair) surfaces as a NEW-BROKEN candidate the agent self-flags for future work; warrants a bootstrap-adopt-time check.

### delivery-git-push-actions-setup (Phase 1.2)
**Previously broken:** `git-push-setup` returning `SERVICE_NOT_FOUND: not bootstrapped` for services the agent hadn't adopted yet; error read as "service missing."
**Now:** CONFIRMED-FIXED. Handler returns `{"code":"ADOPT_REQUIRED","error":"No bootstrapped services found","recovery":{"tool":"zerops_workflow","action":"start","args":{"route":"adopt","workflow":"bootstrap"}}}` matching `close_mode.go:90-102` pattern. Self-review: "The `build-integration` action has an undocumented prerequisite chain... `git-push-setup` (no URL, just to fetch the walkthrough) → `git-push-setup` (with `remoteUrl`, to stamp the meta) → `build-integration`." Agent navigates the chain via the structured Recovery on the ADOPT path.
**Other observations:** Self-review flags the `walkthrough` prose as overloaded (lists GIT_TOKEN setup + commit + push as if all required when only meta-stamping matters for actions integration) — pre-existing guidance-overload signal, not new.

### git-push-setup-with-cicd-method-prompt (Phase 1.2)
**Previously broken:** Same `SERVICE_NOT_FOUND: not bootstrapped` for `build-integration` against unadopted service.
**Now:** CONFIRMED-FIXED. Same structured ADOPT_REQUIRED shape: `{"code":"ADOPT_REQUIRED","error":"Service \"appstage\" is not bootstrapped","recovery":{...}}`. Self-review: "The `develop` workflow start blew up with `ADOPT_REQUIRED` because the existing services hadn't been adopted into ZCP yet. The error did give me the right recovery (`workflow=bootstrap route=adopt`)." Same path resolves cleanly.
**Other observations:** New friction surfaces are CI/CD-specific (workflow file hard-wires appdev's service ID when user intent is appstage; needs explicit substitution by agent). Not platform-mechanic, not regression.

### launch-production-pipeline-not-configured (Phase 1.2 cross-test + 2.2 non-regression)
**Previously broken:** `SERVICE_NOT_FOUND` on `build-integration appstage` reading as "service missing"; status envelope returning stale `launch-active`.
**Now:** Phase 1.2 path CONFIRMED-FIXED on this scenario (same ADOPT_REQUIRED + Recovery shape). Phase 2.2 NO REGRESSION (no spurious DIAGNOSIS_REQUIRED/wouldDestroy misfires). Status envelope returned `idle` cleanly — already-improved-or-clean-state.
**Other observations:** Underlying RC ("source-project ZCP cannot see production-project pipeline") survives. That's a cross-project-scope issue, distinct from 1.2's parallel-migration; tracked in Phase 4 backlog (item 4.4).

### launch-production-dev-only (Phase 2.2 non-regression)
**Previously broken (baseline reference):** RC1 status envelope stale state + RC2 mount-lie + productionProjectId top-level surfacing (untested).
**Now:** Phase 2.2 NO REGRESSION. No spurious DIAGNOSIS_REQUIRED/destructive flows. Flow stalled at `ready-to-launch` (agent burned 4 user-sim turns on fake tokens — agent-side anchoring + scenario design, not platform fault).
**Other observations:** Phase 2.3 still incomplete here too — self-review: "I also tried to grep the actual source code to classify envs the proper way and walked straight into a documented-but-easy-to-miss constraint: the service code is supposed to be SSHFS-mounted at `/var/www/{hostname}/`, but it wasn't mounted in this session... for a launch-production task you do not actually need the code mounted." Same root as cross-deploy's 2.3 partial-fix: agent needs guidance that **launch-production / platform-side-only tasks don't need the mount at all**, not just "if empty → bootstrap." Phase 2.3 fixed the not-yet-adopted case; the never-needs-mount case remains.

## Cross-cutting signals

- **All three "Top 3 RC" baseline reproductions are resolved.** ToolSearch bare-prefix, http_root false-pass on 404, and SERVICE_NOT_FOUND-as-not-bootstrapped are gone from their primary scenarios. Highest-frequency unfixed item from the prior synthesis is fixed.
- **Structured Recovery is fanning out correctly.** verify::http_root + verify::service_running + workflow_git_push_setup + workflow_build_integration all now emit `recovery:{tool,action,args}` for actionable failures. Agents read these and execute the next call directly in the post-fix runs.
- **Phase 2.3 has a residual gap that matches Phase 4.2 backlog (sourceMountAvailable boolean).** The conditional text fixes the "not-yet-adopted" framing but leaves the "platform-side ops don't need mount" case under-served — surfaces 2/9 scenarios (cross-deploy + launch-production-dev-only). Worth promoting the backlog item next round.
- **READY_TO_DEPLOY destructive-warning copy is still verbose** but no longer load-bearing — agents now read the structured Recovery and act, so the warning is decorative noise rather than reasoning friction. Atom phrasing simplification opportunity, not bug.
- **Composite-type plan-vs-yaml asymmetry continues to surface.** 4/9 self-reviews name it as biggest friction (`postgresql:single@18` for plan vs `postgresql@18 + mode: NON_HA` in yaml; runtime needs `ubuntu/nodejs@22` for plan, bare `nodejs@22` accepted in yaml). Pre-existing Phase 4 backlog item (4.1 + 4.3) — needs design pass, not point fix.
- **Guidance overload is back as a top-2 friction.** 3/9 self-reviews cite walls-of-prose in develop-active and bootstrap-status responses. Same pre-existing Phase 4 candidate; gating-by-detected-scope work hasn't landed.

## Items to backlog

- **`sourceMountAvailable: bool` in discover** (already filed as Phase 4.2) — promote to active plan. Two re-run scenarios reason around mount irrelevance for platform-side-only ops despite Phase 2.3's conditional text. The tier-2 structural fix (boolean flag + atoms branch on flag) is the next move.
- **Subdomain L7 not auto-enabling on first import despite `enableSubdomainAccess: true`** (NEW from develop-loop self-review) — separate from Phase 2.3. Verify gives a Recovery pointing at `zerops_subdomain action=enable` so self-healing; worth investigating if the import-yaml flag SHOULD do what its name suggests (post-deploy waiting bug? import semantics?).
- **Mixed-OS dev/stage adopt-plan check** (NEW from resume-after-compaction) — adopt should warn or surface a setup-mismatch signal when `ubuntu/nodejs@22` + `alpine/nodejs@22` get paired; native-module compatibility is a real foot-gun.
- **CI/CD workflow file hard-wires source service ID instead of intent target** (NEW from cicd-method-prompt) — build-integration generates a workflow keyed to whichever service the integration was called against, but user intent for auto-deploy on push almost always means stage/prod, not dev. Substitution burden falls on agent. Worth ergonomic pass.
- **Walkthrough prose for `git-push-setup` reads as universally-required when only meta-stamping matters for actions path** (delivery-git-push-actions-setup self-review) — guidance-overload candidate; tighten or branch the walkthrough on the integration the agent is heading to.

No regressions to watch. Cross-tests on launch-production (2.2 non-regression) both clean.
