# Launch-production — finalization audit (2026-06-16)

**Question Karel asked:** dotáhnout launch-production do finále — full audit (kód, atomy, spec, testy) + live eval na zcp, vše podložené reálným fungováním a kódem.

**Method (3 independent static passes + 2 live runs, all evidence-grounded):**
- My own deep read of the newest/highest-risk surfaces (single-token staging, confirm-production close, state-init sites, atoms).
- **Codex** independent source audit (12 findings + solid section).
- **9-dimension Workflow** (state-machine / single-token / pipeline-first / source-control gate / existing-project / readiness-consent / atoms / spec+dead-code / tests+evals) — each finding adversarially verified against code by an independent skeptic. 32/33 confirmed.
- **Live run #1** `cmd/zcp-launch-live` — composer + `CreateAndImportProject` against the real platform (Muad org), project created + cleaned up.
- **Live run #2** flow-eval `launch-production-from-standard-pair` — real agent, container, real MCP tools.

---

## Verdict

**launch-production is FUNCTIONALLY SOLID and ships correctly. There is NO shipped runtime blocker.** The happy path is live-verified; the single-token lifecycle, pipeline-first import, existing-project merge/conflict path, subdomain-strip, HA-floor, and the source-control read-vs-state split are all correct and pinned by green tests. **Every confirmed defect is hygiene / coverage-trust / doc-drift — not broken behavior.** The finale is a focused cleanup pass, not a redesign.

The dominant theme: the two big recent migrations (**pipeline-first** + **single-token lifecycle**, both 2026-06-11) landed correctly in the *handler*, but left a **drift trail** in the satellite surfaces — rotted live tests, stale eval scenarios, atoms/retry-payloads still describing the pre-migration handshake, doc-comments asserting gates that no longer exist, and dead helpers. None breaks the running product; all erode trust + agent guidance.

---

## LIVE-VERIFIED SOLID (the healthy core — do not touch)

**Live run #1 (`cmd/zcp-launch-live`, real platform):** composer emits exactly the pipeline-first shape and the platform accepts it:
```yaml
services:
  - hostname: appdev
    minContainers: 2
    startWithoutCode: true        # pipeline-first ✓
    type: php-nginx@8.4           # NO buildFromGit ✓
  - hostname: db
    type: postgresql:ha@18        # HA in type-variant, not mode: ✓
project:
    corePackage: SERIOUS          # P-LP-13 ✓
    location: eu-central          # P-LP-13 ✓
```
`CreateAndImportProject` → 4 services created → `DeleteProject` cleanup. envclass dropped 6 SYSTEM envs, `JWT_SECRET → <@generateRandomString>`.

**Static-verified solid (all three passes agree):**
- **Single-token lifecycle** — token staged strictly BEFORE the irreversible create on both paths (new `:695` before create `:740`; existing `:305` before mutation `:322`); confirm-production deletes the staged secret BEFORE stamping `WindowClosedAt` (`launch_confirm.go:157→169`); the value is absent from state / audit / response structs by construction.
- **Pipeline-first import** — sole `buildFromGit` emit site is `export.go`, never launch; `executeLaunchPipelineCheck` is read-only (no PUT), not-configured = warn severity (P-LP-7/8/9).
- **Existing-project P-LP-12** — one blocker per colliding hostname before any mutation; skip=additive; NO path advances past a replace-flagged conflict without `confirmDestructive`.
- **State-machine recovery is deadlock-free** — failed+TargetProjectID and launching+empty both route to `action=reset` with a real orphan-delete escape; resume/idempotent re-check matches spec.
- **`runReadinessRubric` IS wired** (`:470`) — the prior "ZERO callers" concern is resolved (the remaining issue is enforcement/doc, see B4).
- **Atom corpus broadly accurate + lint-clean**; `launch-write-prod-setup` now correctly tells "use the proposed derived block" (prior empty-placeholder drift fixed); `launch-delete-key` tells the confirm-production story correctly.
- **P-LP-1..14 pins are substantive + green** (one hollow exception, M4): token-leak serialization scans, admin-surface restriction, source-immutability frozen-snapshot.

**Live run #2 (flow-eval, real agent):** the read-side source-control gate + prerequisite recovery chain (bootstrap-adopt → git-push-setup → build-integration) works end-to-end with clean recovery messages. *Caveat — this run never reached `launched`: the fixture `nodejs-standard-deployed.yaml` seeds no ServiceMeta + unwired git, so the agent funnels through prerequisites. See "Eval-coverage gap" below.*

---

## FINDINGS — finale gate (cleanup clusters)

### HIGH — coverage-trust rot (invisible to every gate)

**B1 · Live tests rotted + no CI builds the `live`/`api` tags.** The three `//go:build live` launch files (`live_launch_production_test.go`, `live_build_integration_test.go`, `live_git_push_setup_test.go`) **do not compile** — `live_launch_production_test.go:127` uses removed `LaunchBundleInputs` fields (`TargetHostname/ServiceType/SetupName/RepoURL/ZeropsYAMLBody` → moved into `Runtimes []LaunchRuntimeInput`); the other two omit the `ops.SSHDeployer` param the current handler signatures require. `go test ./...` never builds tagged files and no Makefile/CI step runs `-tags live`, so the rot is **permanently invisible**. The live suite the spec cites as the live half of P-LP-9/P-LP-13 is dead weight; `cmd/zcp-launch-live` is the working duplicate that drifts independently.
→ **Fix:** rewrite the 3 live files to the current shape (mirror `cmd/zcp-launch-live`) OR delete them in favor of `cmd/zcp-launch-live` as the canonical live check; THEN add a compile-only gate (`go vet -tags live,api ./internal/...` or golangci `build-tags`). Both halves in one change, or they re-rot.

**B2 · Three eval scenarios assert a never-shipped design.** `launch-production-new-project-push-mode.md`, `launch-production-existing-project-token.md`, and `launch-production-existing-with-webhook.md` pin the agent against status `awaiting-project-mode-choice`, inputs `LaunchProjectMode`/`CICDMethod`, and atoms `launch-generate-prod-token`/`launch-cicd-actions-handoff` — **none exist in code or the atom corpus** (abandoned Phase-6b handshake; `plans/workflow-family-architecture-2026-05-14.md:1238`). The two named files also cite P-LP numbers that contradict the current spec + a phantom `ZEROPS_TOKEN_STAGE`. Running any grades the agent against an interaction the handler cannot produce — false friction, 14-17 min burned each. *(`git-push-setup-with-cicd-method-prompt.md` is NOT stale — that cicd prompt IS shipped in git-push-setup; only the launch-terminal claim is phantom.)*
→ **Fix:** rewrite all three to the shipped pipeline-first flow + reconcile P-LP citations, OR retire them. Optionally add a grep-test for known-removed identifiers in eval `.md`.

### MEDIUM — real-but-narrow correctness + tell≠check drift

**B3 · Source-control gate: empty live read masquerades as `remote-mismatch`.** No empty-string guard before the mismatch compare (`launch_source_control_gate.go:221-241`): when the live reader returns `('', nil)` — which the real readers DO on non-transport git failures (dubious-ownership, broken perms, origin removed) and on a local-mode CWD that is not a git repo — the gate computes `CanonicalRepoURL('') != CanonicalRepoURL(meta)` and fires `remote-mismatch live=''`, handing the agent "rewrite your remote / re-run git-push-setup" for a READ/environment problem. Local mode compounds it (no equivalent of container check 3a presence-probe). The earlier read-vs-state split only closed the ssh-exit-nonzero half; the swallowed-empty half is open + untested against the real reader.
→ **Fix:** empty-live → `source-read-failed`/`git-state-missing` classification (not `remote-mismatch`); bring local mode to parity via an `is-inside-work-tree` presence probe.

**B4 · Readiness rubric is display-only but its doc-comment claims it gates.** `hasBlockingFailures` has ZERO non-test callers; the rubric is fed into the response's `ReadinessChecks[]` for display and never gates the mutation — yet its doc-comment (`launch_readiness.go:327-329`) asserts twice the handler uses it "to decide whether ready-to-launch can advance to launching." A P-LP-13-forbidden gating claim a maintainer could "wire up" and accidentally introduce a hard block the spec explicitly bans. (`run.healthCheck` is likewise atom-claimed "required" but the rubric defers it to "Future iteration".)
→ **Fix:** DELETE `hasBlockingFailures` + its test, rewrite the two doc-comments to "advisory-consent-only" so tell matches check (P-LP-13: recommendation, never a block).

**B5 · Single-token migration didn't fully sweep the "re-supply launchKey" tells.** `launch-pipeline-configure-dashboard.md:24`, `launch-pipeline-skipped.md:16`, `launch-pipeline-configuring.md:16` still say re-call "with the same `launchKey`", and `launch_prod_ops.go:348-360` emits a `retryCall` with `"launchKey":"<re-supply the launch key>"` — training the agent to re-ask for the token while the staged secret is supposed to be the working copy. Related: window-op token resolution is `launchKey`-first / stage-fallback (`:342-348`, prod-ops/reset/confirm same), the REVERSE of the spec's "staged secret first" (spec `:1204`), and stage-read errors are swallowed (look like "absent").
→ **Fix:** atoms + retry payloads omit `launchKey` (mention only as fallback when the stage is gone); make window-op resolution stage-first + surface stage-read errors distinctly.

**B6 · Missing prod-`setup:` block reaches `ready-to-launch` before the token ask.** By design the read-side is a soft-read (`workflow_launch_production.go:363-368` — emits `ready-to-launch` even when `setup:prod` is missing); the prerequisite is enforced only at the hard mutation read (`:610/:1031`), which fires BEFORE staging so nothing irreversible happens — BUT the agent has already crossed the one-time token in a call that returns a blocker, then must cross it again on retry (violates "token crosses exactly once"). Mitigated by the `launch-write-prod-setup` atom firing at ready-to-launch.
→ **Fix:** surface missing-prod-setup as a read-side blocker (like `source-control-required`) so `ready-to-launch` isn't shown until it clears — OR accept soft-read + rely on the atom (document the choice).

**Other MEDIUM:** dead `hasSetupProd` wrapper (M2); atom `launch-classify-platform-envs` hard-codes a closed key-table while the real check is two mechanisms — SYSTEM-type drop + a 5-key infra allowlist (M3, single-owner violation); hollow P-LP-4 pin — asserts only substring "delete", not the `launch-delete-key` atom presence (M4).

### LOW / nits

- **M1** dead stub `ensureNoExistingProdTokenInState` (`launch_existing.go:634-639`) — only a doc-comment + `var _ = errors.New` propping up an otherwise-unused `errors` import. Delete.
- **M5** state-count drift: spec titles "Eight-state machine" + diagram omits the real 9th `existing-project-conflict-prompt`; `topology/types.go:236` comment says "six-state"; handler doc-comment + `launch-intro.md` list six. Three competing counts.
- **M6** existing-path `TargetServiceHostname` persisted from raw `input.TargetService` (`launch_existing.go:328`) vs new-path's canonical `primaryRuntime.PushHostname` (`:724`). **Equivalent today** (handler normalizes `input.TargetService` stage→dev at `:183` + fills from `Promotables[0]` at `:175`); only a narrow empty-first-promotable edge diverges. Robustness/parity smell, not a live bug — set existing-path from `firstResolvedRuntime(resolved).PushHostname` for local correctness.
- **M7** `configuring-pipeline` is documented as pollable but set-then-immediately-overwritten to `launched` (`:958-960`) — agent never observes it.
- **M8** P-LP-13 spec says GetProject read-back is "the only proof" corePackage/location honored, but the handler does no read-back. Soften wording or add best-effort GetProject warn.
- **M9** `launch-post-checklist.md` step 3 shows bare `action="release"` with no `service=`, but `handleRelease` hard-requires it.
- **M10/M11** stage-half docs/schema stale (code accepts+normalizes); spec method-name typo (`GetServiceStackExternalRepositoryIntegrationStatus` vs ZCP-level `GetServiceStackIntegrationStatus`); completed launch plans not archived + spec cites live `plans/...` paths as source.

---

## Eval-coverage gap (the one structural hole)

**No automated test exercises the handler's staging → launched → confirm path live.** The go `live` test is broken (B1); `cmd/zcp-launch-live` structurally CAN'T test staging (it has no source service to stage the secret onto — it only creates a prod project); and no flow-eval scenario seeds a launch-READY source (bootstrapped + git-configured + deployed), so they funnel through prerequisites. The newest/highest-risk piece (single-token staging inside the handler) is **unit-tested only**. → A launch-ready fixture + one green flow-eval-to-`launched` would close this; folds into B1/B2.

---

## Recommended finale sequence

1. **B2** (stale scenarios) — cheapest high-value; unblocks honest eval verification.
2. **B1** (live tests + CI gate) — fix-or-delete + compile-only gate so tagged files can't re-rot. Add a launch-ready fixture to close the eval-coverage gap.
3. **B4** (readiness doc-lie) — delete `hasBlockingFailures` + rewrite doc-comments advisory-only.
4. **B3** (gate empty-guard) + **B5** (single-token tell sweep) + **B6** (decide read-side prod-setup gate).
5. **Dead-code + doc-drift pass** (one commit each, clean-code discipline): M1, M2, M3, M5, M6, M9, M10.
6. **Re-verify:** rewritten flow-eval reaches `launched`; compile-only CI gate green locally.

No `make release` / `make install` — finale lands on main; release is a separate Karel decision.

---

## SHIPPED (2026-06-16) — per-item status

Gates: `go build ./...` ✓ · `go test ./... -short` ✓ · `go test -race` (tools/topology/content/ops) ✓ · `make lint-local` → **0 issues** · `make vet-tags` ✓. Every behavioral change ships RED→GREEN pins. No release / install.

| Item | Status | What shipped |
|---|---|---|
| **B1** | done¹ | Deleted 3 rotted legacy live tests (redundant with the maintained `e2e/` layer + `cmd/zcp-launch-live`); added `make vet-tags` + a `ci.yml` step that `go vet`s the `live`/`api`/`e2e`/`probe` tags so build-tagged tests can't silently rot again. |
| **B2** | done | Rewrote 3 stale scenarios to the shipped pipeline-first flow; added `TestNoNeverShippedLaunchVocabInEvalScenarios` (scoped grep-guard, RED→GREEN). |
| **B3** | done | Empty-live-read → `source-read-failed` (not `remote-mismatch live=""`); local push-proof returns the ls-remote error (parity with container) instead of swallowing it into `head-not-pushed`; 2 new tests. |
| **B4** | done | Moved dead `hasBlockingFailures` to the test file (regression-detection helper); rewrote the readiness-severity doc to "display-only, never gates" (P-LP-13); fixed the atom's `run.healthCheck` "gates" overclaim. |
| **B5** | done | Dropped "re-supply launchKey" from 3 atoms + the prod-ops `retryCall`; new `resolveLaunchWindowToken` (stage-first, explicit-as-fallback, surfaces stage-read errors) wired into prod-ops / reset / confirm / resume; `TestProdOps_StageFirst_PrefersStageOverExplicit`. |
| **B6** | done | `readAndValidateSourceState` now reports `transientReadFailure`; the ready-to-launch soft read surfaces a DETERMINISTIC missing-prod-setup blocker (with the derived proposal) before the one-shot token is asked, while still tolerating transient SSH outages; 2 new tests. |
| **M1** | done | Deleted the `ensureNoExistingProdTokenInState` stub + parasitic `errors` import (+ the same-class `var _ = workflow.ServiceMeta{}` keeper in `launch_source_read.go`). |
| **M2** | done | Deleted dead `hasSetupProd` wrapper; repointed its test to `hasSetupNamed(body,"prod")`; fixed 3 drifted comments. |
| **M3** | done | Rewrote `launch-classify-platform-envs` atom to the TWO real mechanisms (Type=SYSTEM drop — open set; the exact-key infra allowlist — closed) instead of a drifted hand-copied key table. |
| **M4** | done | Strengthened the P-LP-4 pin to assert the `launch-delete-key` atom's markers (`confirm-production` / close-window / `ZCP_LAUNCH_TOKEN`), not a stray "delete". |
| **M5** | done | Reconciled the state-count drift: spec §10.1 retitled + notes the 9th status; `topology/types.go`, the handler doc-comment, and `launch-intro` now list the real statuses (incl. `source-control-required`). |
| **M6** | done | Existing-project path records `TargetServiceHostname`/`SourceRepoURL` from the resolved push hostname + gate-validated remote (parity with new-project; robust by construction). |
| **M9** | done | `launch-post-checklist` release example now carries `service="<hostname>"` (handleRelease requires it). |
| **M10** | done² | Spec method-name corrected; `idle-launch-entry` + the `targetService` jsonschema now say stage-half is accepted+normalized (not rejected); rubric doc-comment count fixed. |
| **M11** | done | Stripped the 3 live `plans/*.md` source citations from spec §10/P-LP; `git mv`'d 17 completed launch/prod plans to `plans/archive/` and repointed every provenance reference (code / e2e / cross-plan) to the archive path (0 stale refs). |

**Not shipped (flagged, not silently cut):**
- ¹ **B1 launch-ready fixture** — NOT built; determined unnecessary. The staging→launched→confirm handler path IS covered live by `e2e/launch_single_token_test.go` (now PASSED, see below). The flow-eval `from-standard-pair` funnel-through-prerequisites is a separate fixture-seeding observation, not a coverage hole.
- ² **M10 tag-regex hardcode** — left as-is. The `^v\d+\.\d+\.\d+$` default is stable + appears across several atoms; single-owner-izing it is a larger refactor with low payoff.

## LIVE VERIFICATION — full lifecycle PASSED (2026-06-16, on-container "eval flow")

`TestE2E_LaunchSingleTokenLifecycle` cross-compiled (linux/amd64, my changes) → shipped to the zcp-eval container → run there with the container's project-scoped `ZCP_API_KEY` + in-network SSH (the local box's account-wide token + VPN couldn't satisfy the auth/SSH path). **PASS (76.87s), all 20 steps**, self-cleaned (prod project + provisioned source service deleted). The steps that directly exercise this finale's changes:
- **step 10 ready-to-launch** → B6 read-side missing-prod-setup change does NOT block a valid launch.
- **step 11 publish** → prod project created; HA floor (minContainers 1→2), cpuMode DEDICATED warnings; GrantSelfRole expected-fail (integration token) covered by creator access.
- **step 14 prod-ops status WITHOUT launchKey** → B5 `resolveLaunchWindowToken` stage-first read.
- **steps 16-18 confirm-production close → staged secret physically gone (API+ssh) → post-close prod-ops refuses** with the lifecycle message.
- **step 19 reset (explicit launchKey, staged copy gone)** → B5 explicit-fallback; **step 20 reset deletes the prod project + clears state**.

`cmd/zcp-launch-live` separately ran the pipeline-first composer + `CreateAndImportProject` live earlier this session. The full single-token lifecycle + my B3/B5/B6 changes are now **live-verified end-to-end against the real platform.**
