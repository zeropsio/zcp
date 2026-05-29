# ZCP Workflow-Family Deep Audit — Logic / Overcomplexity / Reliability / Fundamental-Design (2026-05-29)

**Scope:** the end-to-end workflow family — bootstrap → adopt → develop → deploy → verify →
export → launch-production — plus the shared spine (engine/session/plan/envelope/atoms),
env handling, the three-dimension deploy model, topology + ServiceMeta state, and the
platform/zerops-go SDK boundary.

**Lens (deliberately different from the two prior passes):** logic-correctness, overcomplexity,
unreliability, and fundamental-design — *not* the `_ =`-bug-hunt of
`plans/zcp-wholecodebase-audit-2026-05-29.md` nor the canonical-vs-fragmented drift-hunt of
`plans/workflow-family-audit-2026-05-28.md`. Where a prior finding was still open it was
independently re-verified with fresh evidence; the value here is the fundamental/logic issues
those passes did not center.

**Provenance:** dynamic workflow — 12 subsystem auditors + 5 cross-cutting hunters build an
intended-behavior model from the specs, compare against the real code, then adversarial
*default-refute* verifiers (correctness / reachability / root-cause lenses) judge each
high/critical finding, then a completeness-critic gap-fill round, then lead synthesis. **106
agents, ~8.1M tokens, ~33 min.** 57 raw → **38 confirmed + 7 partial, 5 refuted, 7 gap-confirmed.**
The 5 refutals (incl. two prior-audit-derived claims) are recorded below — unlike the prior
0/30 pass, the verify gate actually killed findings. The top-3 criticals (XCUT-1, DELIV-1,
LAUNCH-1) and the two prior keystones were hand-verified against the code before this report.

> **No edits without approval.** This is findings + proposed direction; Karel picks what to action.
> Recipe-generation engine internals (Aleš) were out of scope — none of the confirmed findings land there.

---

## 1. Executive verdict

**The workflow family is structurally sound at the spine but is being eaten by one systemic
disease: every time a foundation lands (typed env-availability, the three-dimension deploy model,
pair-keyed metas, the canonical setup-name store, multi-runtime promotion, the typed envelope),
the *triggering* consumer gets wired and the old/parallel/peer consumers do not — so one concept
ends up answered by two-to-four divergent code paths that silently disagree.** This is **not
architectural rot** — the layer boundaries, atomic-rename storage, the flock'd registry,
check-before-mutate idempotency, and the launch trust boundaries are genuinely well-built. It is a
**discipline gap with architectural consequences**: the repo's own "check parallel paths" / "map
blast radius" rules were not applied at the moment each foundation shipped, and the partial rewires
now generate *new* contradictions faster than the originals close (TOPO-1/WF-2: a partial fix made
three local-adopt branches mutually inconsistent where before they were uniformly wrong).

**Two findings rise above "discipline gap" to genuine design holes that need an owner decision:**
- **XCUT-1** — the three "orthogonal dimensions" are concurrent **unsynchronized read-modify-write
  on one JSON file** while the MCP SDK dispatches parallel tool_use on goroutines. A *lost-write
  data-corruption hole*, not drift. (Hand-verified: SDK `server.go:1441` `jsonrpc2.Async`; ZCP
  `WriteServiceMeta` unlocked; dimension handlers read-whole→mutate-one→write-whole.)
- **GAP0 / GAP2** — the export/launch bundle env model is 2-layer where the platform is 3-layer
  (silent data loss on export/launch), and the local-stage meta cannot represent a second runtime
  (a platform-supported topology is structurally unreachable). Missing-capability holes.

The common-case **single-lifecycle, single-runtime, container-mode happy path works.** Every
confirmed defect clusters where two lifecycles meet (infra vs work session), two modes meet
(container vs local), two runtimes meet (single vs multi-promotion), or two paths answer one
question (preflight vs render, deploy vs git-push-setup, single-deploy vs batch). The display
layer (`render.go`) repeatedly *masks* the matcher/validate disagreement — which is why these
survived: the response *looks* right.

**Counts:** 26 high + 19 medium confirmed/partial · categories: 16 logic-error, 16 unreliability,
11 fundamental-design, 2 overcomplexity.

---

## 2. Themes (severity-ranked)

| # | Theme | Root pattern | Members |
|---|---|---|---|
| **A** | Foundation lands, parallel/peer consumer never rewired (**dominant**) | capability threaded into the *triggering* consumer; sibling consumers keep pre-foundation logic / a hardcoded constant → display/validate/render/match disagree on one field | ENV-1, ENV-3, DEPLOY-1, DEPLOY-3, VERIFY-1, EXPORT-1, WF-1, LAUNCH-1, LAUNCH-3, DELIV-1, DELIV-2 |
| **B** | Silent error/flag discard at I/O & poll boundaries | `_ =` on a load-bearing write-error or poll-timeout flag → failure becomes silent success or partial persisted state | DEV-1, XCUT-2, XCUT-3, SPINE-3 |
| **C** | Two uncoordinated session/state lifecycles; "what is active" answered twice | infra session and develop work-session are separate lifecycles with no single owner; precedence computed by two functions with **opposite** ordering | SPINE-1, SPINE-2, DEV-2, WF-5 |
| **D** | Container `Status==ACTIVE` ≠ code-deploy-success — ignored only at the mutating doors | codebase knows ACTIVE is a container-lifecycle state (startWithoutCode trap, `LatestFailedAppVersionContext` gate) but adoption/override stamp/derive "deployed" off raw ACTIVE | ADOPT-1, ADOPT-2, GAP3-1 |
| **E** | Pair-keyed invariant under-enforced; local topology mis-modeled | hostname→meta resolution bypassing the pair-aware path; a contract test pinning only one syntactic shape; a local-mode bootstrap path never wired to the local-stage topology | SPINE-4/TOPO-2, TOPO-3, BOOT-1, BOOT-2, BOOT-3, TOPO-1/WF-2/DELIV-2 |
| **F** | Missing-capability holes (genuine design gaps, not unrewired consumers) | the model is structurally short of platform reality — concurrency, env layers, runtime cardinality, container git-init invariant | **XCUT-1**, GAP0-1/GAP0-2, GAP2-1/GAP2-2, GAP4-1/GAP4-3 |
| (cleanup) | Overcomplexity / deletable | abstraction more complex than the problem / inert dimension / stale annotation | WF-1(export-variant), WF-2(verify-annotations), LAUNCH-5 |

### Cross-workflow coherence
**The bootstrap→develop→deploy→verify core is one coherent spine; export, launch, and the
local-mode variants are islands bolted onto its shell.** The typed envelope
(`ComputeEnvelope`→`BuildPlan`/`Synthesize`→`RenderStatus`) is a real single source of state for
the core. But **launch and export each run their own state + recovery + audit model** (WF-5), so
any spine hardening must be hand-ported. **Local mode** is a half-island: it shares the envelope but
its *creation* path (BOOT-1/BOOT-2) was never wired to the local-stage topology the rest of the
system models. **The work session is a peer-island to the infra session** (Theme C) with no shared
lifecycle owner. The seams between the islands are where every confirmed defect lives.

---

## 3. Top act-now list (ranked)

1. **XCUT-1** — `internal/workflow/service_meta.go:366`. Add `UpdateServiceMeta(stateDir, hostname, mutate func(*ServiceMeta))` that locks→read-fresh→apply→write (pkg `sync.Mutex` mirroring `workSessionMu`, optionally flock per registry); route all ≥10 RMW sites through it. *Data-corruption hole — highest severity, fixable now.*
2. **WF-1 / setup-name** — `internal/workflow/deploy_intent.go:149,158,164`. Read `target.SetupName`/`StageSetupName`; OMIT `--setup` when empty instead of defaulting to `prod`/`dev`; populate the fields in *both* snapshot builders; delete `RecipeSetupProd/Dev` from the deploy path. *Bakes a permanently-wrong `--setup prod` into committed CI.*
3. **TOPO-1 / WF-2 / DELIV-2** — `internal/workflow/compute_envelope.go:231-235`. Add `NewServiceMeta(hostname, mode)` stamping all three sentinels; normalize `GitPushState`/`BuildIntegration` once at `parseMeta`-on-read (self-heals on-disk metas) alongside `CloseDeployMode`; delete the ad-hoc `==""` checks. *Silently kills the git-push-setup→build-integration atom chain for local-stage, the mode it targets.*
4. **LAUNCH-1** — `internal/tools/launch_pipeline.go:159`. Key the pipeline check on prod hostname (`resolved.ProdHostname`), iterate all resolved runtimes; gate the empty-guidance branch on `!PipelineCheckedAt.IsZero()` correctly. *Reports CD "configured" when never checked.*
5. **LAUNCH-2** — `internal/tools/launch_existing.go:347-382`. Extract the new-project tail (per-service-error→failed, `pollImportedServices`, `executeLaunchPipelineCheck`, status transition) into a shared helper both mutation paths call; build the pipeline client from `ExistingProdToken`. *Reports `launched`/`success` on a failed existing-project import.*
6. **SPINE-1** — `internal/tools/workflow.go:441`. Make `status` dispatch read the work-session file in the bootstrap/recipe branch and merge a backgrounded block (**bootstrap-primary per §5.3 — do NOT flip to work-first**; see partial spot-check). *Canonical recovery primitive omits in-flight develop work.*
7. **DEV-1** — `internal/tools/deploy_local.go:160` (+ ssh/git/batch/verify siblings). Stop `_ =`-discarding `RecordDeployAttempt`/`RecordVerifyAttempt`; ignore only `ErrHostnameOutOfScope` (surface a note), warn on everything else — parity with `workflow_record_deploy.go:172`.
8. **DEPLOY-1** — `internal/tools/deploy_batch.go:144` post-poll loop. Build + `RecordDeployAttempt` a per-entry `DeployAttempt` with `SucceededAt`, mirroring `deploy_local.go:178`; fix/delete the false comment at `ops/deploy_batch.go:66`. *The cluster the tool tells agents to use can't advance auto-close.*
9. **SPINE-4 / TOPO-2** — `internal/workflow/engine.go:726`. Swap `ReadServiceMeta`→`FindServiceMeta`; add a stage-half lock test; broaden `TestNoInlineManagedRuntimeIndex` (TOPO-3) to flag read-by-stage-hostname.
10. **VERIFY-1** — `internal/tools/deploy_subdomain.go:71/76`. Re-fetch the service after a successful cold enable before `ResolveSubdomainURL` (mirror `tools/subdomain.go:98`); add a multi-port http+tcp fixture. *Wrong subdomainUrl + spurious not-HTTP-ready on multi-port first deploy.*
11. **GAP3-1** — `internal/tools/import.go:99`. On successful override-import, clear `FirstDeployedAt` for every `overrideReplacements` hostname. *Stamp outlives the destroyed deploy.*
12. **GAP4-1** — `internal/ops/git_auth_probe.go:56`. Give `BuildGitOriginSyncCommand`/`RunGitOriginSync(Local)` the `test -d .git || git init -q -b main` guard `buildSSHCommand` already has; ideally one shared "ensure .git" primitive.
13. **XCUT-2 / XCUT-3** — `workflow_git_push_setup.go:427`, `env.go:316/224/236`. One sweep: capture `pollManageProcess`'s failed flag, don't stamp `configured` / don't claim "env values are live" on timeout.
14. **BOOT-1 / BOOT-2** — `internal/tools/workflow_checks.go:76`, `internal/workflow/bootstrap_outputs.go:43`. Gate `skipDevCheck` on `EnvLocal` regardless of route; derive the local-stage re-tag from `EnvLocal && mode==standard`, not from an empty `DevHostname`. *Local+standard classic bootstrap is broken end-to-end.*

**Lower but cheap:** SPINE-2 (reset clears work session), SPINE-3 (claimSession propagate registry error), BOOT-3 (global hostname uniqueness), DEV-2 (status fires `MaybeFireAutoClose`), GAP0-1 (service-env layer in bundle), DEPLOY-3 (local git-push env-ref preflight parity), ENV-2/ENV-3 (generate-dotenv literal-write + E3 parity).

---

## 4. Confirmed findings (38) + partials (7) — grouped by theme, with file:line

### Theme A — Foundation lands, consumer not rewired

- **[ENV-1] high · fundamental-design** — Typed per-layer availability (RC2/RC3) wired into 2 of 4 effective-env assembly sites. `discover.go:345` `attachEnvs` (powering `zerops_env get` + `zerops_discover includeEnvs`) re-implements slim+yaml assembly inline; on a yaml-baked transient failure it appends a free-text warning and returns slim-only `Envs` that *look* complete — the exact "transient masquerades as live" masquerade RC2/RC3 set out to kill, reproduced inside the read path an agent queries first. Only `deploy_preflight.go:235`/`tools/env.go:368` use the typed `EffectiveServiceEnv`. **Fix:** route `attachEnvs` (+ generate-dotenv) through `EffectiveServiceEnv`; surface `YamlBakedState.Unavailable()` as a typed flag, not a buried string; add a lint forbidding fresh `GetServiceEnv`+`AppVersionEnvVars` assembly outside `env_effective.go`.
- **[ENV-3] medium · unreliability** — generate-dotenv self/sibling refs resolve against STALE platform app-version, not the local candidate yaml. `deploy_preflight.go:254` got the E3 overlay (commit `ad6571a6`); `env_plan.go:440` `expandRefs` did not — so `BAR=${app_FOO}` for a new local key resolves against the last-deployed yaml (stale value, or hard-fail). Preflight and renderer disagree for identical input. **Fix:** thread the candidate's own `run.envVariables` as a local overlay into `expandRefs`.
- **[DEPLOY-3] medium · logic-error** — Env-ref preflight runs on container git-push (`deploy_git_push.go:333`, blocks on FAIL) but not on local git-push (`deploy_local_git.go:84` runs only `RunPreDeployValidation`). A bad `${peer_var}` gets actionable feedback in container, a delayed remote build failure in local. **Fix:** extract the parse+`preflightEnvRefs` block, call from both.
- **[VERIFY-1] medium · logic-error** — `maybeAutoEnableSubdomain` resolves the post-enable URL from a PRE-enable snapshot. `svc` is fetched (`deploy_subdomain.go:71`) before enable (`:76`); on first deploy `svc.SubdomainAccess==false`, so `ResolveSubdomainURL`'s first guard returns `""` on every cold enable, falling back to `SubdomainUrls[0]` (declaration-order, no scheme filter). A multi-port service (mailpit: SMTP 1025 before HTTP 8025) gets the SMTP URL as `subdomainUrl` and `WaitHTTPReady` probes the SMTP port → spurious not-ready. The scheme-aware path is dead on the cold path; single-port fixtures hide it. **Fix:** re-fetch after cold enable (mirror `tools/subdomain.go:98`); add a multi-port fixture.
- **[EXPORT-1] high · logic-error** — Export reads `meta.Mode` not `meta.ModeFor(hostname)` for stage-half resolution (`workflow_export.go:133`), so a stage hostname resolves as standard → wrong variant branch + wrong setup candidates. **Fix:** project mode through `ModeFor`.
- **[WF-1] high · logic-error** — `DeployIntent.Resolve` (`deploy_intent.go:149,158,164`) contains zero references to the `SetupName`/`StageSetupName` fields it was built to consume; always emits literal `prod`/`dev`. `anticipatedBuildTarget` bakes that into committed `.github/workflows/zerops.yml` (`--setup prod`). Meanwhile `deployPreFlight:82` DOES read `meta.SetupNameFor` — so the deploy that runs honors a renamed setup but the next-action args + committed CI do not. Triple mismatch (docstring names Resolve as consumer / code never reads field / store has the value). **Fix:** read the fields; OMIT `--setup` when empty; populate both snapshot builders; delete `RecipeSetupProd/Dev`; pin with a renamed-setup CI-YAML test.
- **[LAUNCH-1] high · logic-error** — Pipeline-config check keys on source hostname while imported services carry prod hostnames. `RuntimeHostname = input.TargetService` (source dev/stage) is matched against `ImportedServices[].Name` (prod, e.g. `appdev`→`app`); the `if svc.Name != RuntimeHostname { continue }` skips every service → no `GetStatus`, `PipelineConfigurations` empty, but `PipelineCheckedAt` set → `pickPipelineAtomID` returns `launch-pipeline-configured`. Reports CD wired when never checked; the only unit test masks it with identical names. **Fix:** key on prod hostname, iterate all runtimes, add a source≠prod test.
- **[LAUNCH-3] high · logic-error** — Read-side source-control gate validates only `TargetService` (`workflow_launch_production.go:152`), publish-side loops all `resolveLaunchRuntimes` (`launch_source_control_gate.go:628`). A 2-runtime promotion where runtime B is unconfigured passes the read-side gate, advances to ready-to-launch, mints the one-shot launchKey, then fails at publish — wasting the irreversible key the gate exists to protect. No handler test passes `Promotables`. **Fix:** run the read-side gate over `resolveLaunchRuntimes`, aggregate per-runtime blockers.
- **[DELIV-1] high · logic-error** — `BuildPlan`'s idle partition contradicts the envelope's `IdleScenario`. `deriveIdleScenario` (`compute_envelope.go:126`) is 3-way (resumable→`IdleIncomplete`); `countIdleServices` (`build_plan.go:217`) is 2-way (no resumable concept) → a resumable service is counted adoptable → `planIdle` returns Primary `route=adopt` while the rendered response shows the priority-1 `bootstrap-resume` atom ("do NOT adopt over these — resume"). The structured Plan (spec-declared canonical next-action) does exactly what the top atom forbids, colliding a fresh adopt session with the orphaned records. Spec §1.4 dispatch list itself omits the resume case. **Fix:** one shared idle-partition; add a BuildPlan resume branch (route menu, not route=adopt); update §1.4; extend the lifecycle-matrix test to assert Primary agrees with IdleScenario.
- **[DELIV-2] high · unreliability** — `buildOneSnapshot` normalizes empty→canonical for only `CloseDeployMode` (`compute_envelope.go:231-233`); `GitPushState`/`BuildIntegration` copied raw (`234-235`); synthetic local-only tail (`205-211`) normalizes none. `serviceSatisfiesAxes` (`synthesize.go:378`) does `slices.Contains([unconfigured], "")` = false → git-push/build-integration atoms never fire for empty-dim metas, while `render.go:200` displays them as `unconfigured/none` (masking). **Fix:** normalize all three at the snapshot boundary (see TOPO-1).
- **[DEPLOY-1] high · logic-error** — Batch deploy records no `DeployAttempt` and never stamps `FirstDeployedAt`. `DeployBatchSSH` + handler have zero `RecordDeployAttempt`/`stampFirstDeployedAt` calls (vs the single-deploy paths which do all of it). An agent following the batch tool's own guidance ("every 3-deploy cluster… close redeploy") deploys successfully but the session never auto-closes and `FirstDeployedAt` never stamps. The comment at `ops/deploy_batch.go:66` ("protected by workSessionMu in the workflow layer") is a comment-vs-code lie — no such call exists. **Fix:** record per-entry attempts in the post-poll loop; fix the comment.

### Theme B — Silent error/flag discard at I/O & poll boundaries

- **[DEV-1] high · unreliability** — All in-tree deploy/verify paths `_ =`-discard `RecordDeployAttempt`/`RecordVerifyAttempt` (`deploy_local.go:160`, `deploy_ssh.go:228`, `deploy_git_push.go:215`, `deploy_local_git.go:65`, `verify.go:127`); only `record-deploy` (`workflow_record_deploy.go:172`) inspects them. The exact spec §9.3 mid-task scenario (deploy a newly-bootstrapped out-of-scope service) succeeds on the platform but is silently untracked; a corrupt session file or save-failure during a normal deploy is equally invisible — violating the §10.5/§1.3 "every state transition is observable" promise. **Fix:** parity with `record-deploy` (ignore only `ErrHostnameOutOfScope` + surface a note; warn on everything else).
- **[XCUT-2] high · unreliability** *(open-from-wholecodebase; verifier split — see note)* — git-push-setup stamps `GitPushState=configured` even when the push-source restart poll times out. `_, _ = pollManageProcess(...)` (`workflow_git_push_setup.go:427`) discards the timeout flag → falls straight to the unconditional `configured` stamp (`430`) and the response asserts "GIT_TOKEN live in container shell." On timeout the shell lacks the token; the next git-push deploy fails confusingly against state claiming fully-configured. Timeout-treated-as-success. **Fix:** capture the failed flag, return `ErrAPITimeout` + recovery before stamping. *(Note: this finding was confirmed by the x-reliability hunter but a different hunter's near-duplicate was refuted; the headline + fix are solid — confirm the exact line once.)*
- **[XCUT-3] medium · unreliability** — Env set/delete + auto-restart claim "env values are live" when the apply/restart poll times out. `env.go:316` discards the failed flag, then unconditionally appends to `RestartedServices` and the response lands on "env values are live" (`334`); same at `:224/:236`. The container may still hold its old boot env. **Fix:** thread the failed flag, add a restart-pending warning. (Same root as XCUT-2 — one "never discard the poll-failed flag" sweep across manage/env/git-push closes the class.)
- **[SPINE-3] medium · unreliability** — `claimSession` saves the state file with the new PID (error checked), then `_ = updateRegistryPID(...)` (`engine.go:148`) discards the registry error. On a flock timeout the state file holds the live PID but the registry holds the dead PID → `detectActiveWorkflow` (reads state file) says active, `infraPhaseForPID` (reads registry) says idle: a silent self-inflicted SPINE-1 divergence triggered by an I/O error. **Fix:** propagate the error; ideally make claim a single registry-locked transaction.

### Theme C — Two uncoordinated session/state lifecycles

- **[SPINE-1] high · fundamental-design** — Phase precedence modeled twice with opposite ordering. Dispatcher `status` routes infra-first via `detectActiveWorkflow` (`workflow.go:441`) → `handleBootstrapStatus` (never reads the work-session file); the envelope pipeline `derivePhase` (`compute_envelope.go:398`) is work-first. In a concurrent bootstrap+develop session (reachable: start develop, then start bootstrap; neither closes the other), `action=status` — the canonical post-compaction recovery primitive — returns a `BootstrapResponse` with no work-session block and no deploy/verify Plan, hiding in-flight develop work exactly when the agent re-orients. Violates the §5 handler contract. No test pins it. **Fix:** one authoritative resolver; per §5.3 the canonical answer is **bootstrap-primary with a backgrounded work-session block merged in** (see partial spot-check below).
- **[SPINE-2] medium · logic-error** — `action=reset` clears only the infra session (`engine.go:186`); never `DeleteWorkSession`/`UnregisterSession(work-{pid})`. Spec §7.4 says "Deletes active session(s)" (both). After a "clean slate" reset the next `ComputeEnvelope` still finds the open work session. `resetReport.WorkSessions` is declared but never populated. **Fix:** also delete the work session + populate the report.
- **[DEV-2] medium · fundamental-design** *(partial)* — `action=status` never fires `MaybeFireAutoClose` (`handleLifecycleStatus` loads a plain `CurrentWorkSession`). Every mutation path fires the gate; the read-only recovery primitive — the one surface a compacted agent is guaranteed to consult — can only *report* a stale-but-unfired gate, never heal it. *(Partial: practical trigger is narrow — mainly a swallowed `SaveWorkSession` leaving `ClosedAt` unpersisted. Architectural gap is real; frequency low.)* **Fix:** call `MaybeFireAutoClose` before `ComputeEnvelope` in `handleLifecycleStatus`.
- **[WF-5] medium · fundamental-design** *(open-from-workflow-family)* — Launch-production runs a fully parallel state + status-recovery + audit model (`launch_state.go`, in `internal/tools/` not `internal/workflow/`), bypassing the StateEnvelope/Plan/session spine; export ships raw `nextSteps[]` — a third bespoke "status after compaction" mechanism. Spine hardening must be hand-ported. **Fix (owner decision):** fold launch into `WorkflowState.Launch`+spine (versioned migration) OR formally accept export+launch as stateless-narrowing and extract ONE shared recovery helper both consume — then delete the second mechanism.

### Theme D — ACTIVE ≠ deploy-success

- **[ADOPT-1] high · fundamental-design** *(partial)* — Adoption equates container `Status==ACTIVE` with "successfully code-deployed". Both paths gate purely on `rt.Status==StatusActive` and never query appVersion status or run the spec §3.2 step-2 "verify running and healthy" (unimplemented). A startWithoutCode-idle service (ACTIVE, zero real deploys) and a last-deploy-failed-but-container-ACTIVE service both get adopted as "deployed," suppressing first-deploy guidance. **Partial:** the startWithoutCode/first-deploy-suppression half is confirmed; the "bypasses diagnose-before-destruct" half is **refuted** (adoption is non-destructive). **Fix:** stamp/classify "deployed" only when the latest non-NONE appVersion is *successful* (reuse `failed_context.go`); implement or drop §3.2 step 2.
- **[ADOPT-2] medium · unreliability** — Local adopt eager-stamps `FirstDeployedAt` (frozen forever via `IsDeployed()` short-circuit) while container adopt leaves it empty and re-derives "deployed" live (self-heals if status drops). Same concern, two divergent mechanisms; the frozen local stamp makes the ADOPT-1 misclassification permanent. **Fix:** drop the eager stamp; let `DeriveDeployed` signal-3 own it uniformly, gated on real deploy-success.
- **[GAP3-1] high · unreliability** — Successful override-import never clears the destroyed service's `FirstDeployedAt` (`import.go:99`). The whole tree only ever SETS or copies-forward the stamp; no path clears it on destroy. After a destructive override the service is fresh/never-deployed but `DeriveDeployed` still returns true → develop first-deploy branch suppressed + `never-deployed` atoms won't fire — the exact startWithoutCode trap the design exists to avoid, reintroduced through the destructive door. **Fix:** clear `FirstDeployedAt` for every `overrideReplacements` hostname on success (mirror `zerops_delete`).

### Theme E — Pair-keyed under-enforced; local topology mis-modeled

- **[SPINE-4 / TOPO-2] high · unreliability/logic-error** *(open-from-wholecodebase)* — `checkHostnameLocks` resolves each hostname via non-pair-aware `ReadServiceMeta` (`engine.go:726`), which returns `(nil,nil)` for a stage-half hostname (no own file). A planned target colliding with an existing pair's `StageHostname` is not detected as locked → two concurrent bootstraps proceed against the same stage hostname, both writing incomplete metas. **Fix:** swap to `FindServiceMeta` (already pair-aware); add a stage-half lock test.
- **[TOPO-3] medium · unreliability** — `TestNoInlineManagedRuntimeIndex` (the pin for the E8 pair-keying invariant) only matches the AST shape `<map>[x.StageHostname]=v`; it cannot catch a direct `ReadServiceMeta(stateDir, stageHostname)` lookup — which is exactly how TOPO-2 slipped. An invariant pinned by a test that can't detect its most likely violation form gives false confidence. **Fix:** broaden the scan (or add a grep-gate) to flag read-by-stage-hostname; pair with the TOPO-2 fix so the new test goes red→green.
- **[BOOT-1] high · logic-error** — Local+standard *classic* bootstrap is broken end-to-end. `checkProvision` sets `skipDevCheck=true` only for `EnvLocal && Route==Recipe` (`workflow_checks.go:76`); for the classic route it still runs `checkServiceRunning(devHostname)` against Zerops — but local+standard creates NO dev service. Empirically reproduced: provision FAILS (`appdev_exists=fail`), bootstrap never iterates, agent hard-stops. The skip predicate keys on route when it should key on environment/topology. **Fix:** gate `skipDevCheck` on `EnvLocal` regardless of route.
- **[BOOT-2] high · fundamental-design** — The local-stage meta inversion (`bootstrap_outputs.go:43`) fires only when `DevHostname==""`, but `ValidateBootstrapTargets` REJECTS empty `DevHostname` and no path ever sets it empty → the inversion is dead, and a validated local+standard plan writes `Mode=standard` keyed by a phantom dev hostname with no Zerops service. Two divergent meta shapes for one topology (vs `LocalAutoAdopt`'s project-keyed local-stage). The `bootstrap_setup_name_test` PlanModeLocalStage assertion passes only by constructing `Plan{DevHostname:""}` directly, bypassing validation. **Fix:** derive the re-tag from `EnvLocal && mode==standard`, not from empty `DevHostname`; or converge on `LocalAutoAdopt`'s shape.
- **[BOOT-3] medium · unreliability** — No cross-target hostname-uniqueness validation. `depSeen` is per-target (`validate.go:342`); nothing rejects two targets sharing a dev/stage hostname or a CREATE-dep colliding with a runtime hostname created in the same plan. The collision slips to `zerops_import` as an opaque platform error (and for recipe, ZCP generates the colliding YAML). **Fix:** one global hostname set across all targets, accumulated errors.
- **[TOPO-1 / WF-2] high · fundamental-design/logic-error** *(open-from-workflow-family keystone)* — No `NewServiceMeta` constructor + no single normalization seam. `adopt_local.go` case-0 stamps `GitPushUnconfigured/BuildIntegrationNone` but case-1 (local-stage, the common path) and default (multi-runtime) omit both → empty on disk; `parseMeta` doesn't normalize; `compute_envelope.go:234-235` copies raw. So for a local-stage adoption that later sets `closeMode=git-push`, the recurring `develop-close-mode-git-push-needs-setup` atom never fires (S13 fixture masks it by hardcoding `Unconfigured`). The partial fix (case-0 only) made the three branches mutually inconsistent where they were uniformly wrong before. **Fix:** `NewServiceMeta` for all writers + normalize all three dims at `parseMeta`-on-read; add a local-stage atom-firing golden built from a real adopt_local meta.

### Theme F — Missing-capability holes (genuine design gaps)

- **[XCUT-1] high · fundamental-design** *(#1 act-now; hand-verified)* — The three "orthogonal dimensions" are concurrent unsynchronized read-modify-write on one shared file. `WriteServiceMeta` (`service_meta.go:366`) is atomic-rename only (torn-write safe, RMW-unsafe); `service_meta.go` has no mutex anywhere; close-mode / git-push-setup / build-integration handlers each read-whole→mutate-one-field→write-whole. SDK `server.go:1441` runs `jsonrpc2.Async(ctx)` for every non-initialize call, so an agent emitting the three as parallel tool_use lands them on concurrent goroutines → last writer silently discards the others' orthogonal, action-confirmed mutations (e.g. `GitPushState=configured` reverted to `unconfigured`). The work-session path got `workSessionMu` for exactly this reason; ServiceMeta — carrying MORE multi-owner state — was left unguarded. **Fix:** `UpdateServiceMeta(stateDir, hostname, mutate func(*ServiceMeta))` that locks→read-fresh→apply→write; route all ≥10 RMW sites through it.
- **[GAP0-1] high · logic-error** — Export/launch bundle drops the per-service userData env layer. The platform models env as 3 layers (Project / Service slim-userData / YamlBaked); the composers carry only Project (+ buildFromGit rebuilds YamlBaked). `BundleInputs`/`LaunchBundleInputs` have only `ProjectEnvs`; the runtime `services[]` entry emits no per-service env. A service env set via the documented `zerops_env action=set serviceHostname=X` re-imports as a service missing that key, with no error — destination can fail at boot with no signal. **Fix:** add `ServiceEnvs` to bundle inputs; read the slim layer at the handler boundary; emit as `envSecrets` (schema-correct, not `envVariables`); route through the same 4-bucket classification.
- **[GAP0-2] medium · fundamental-design** — classify-prompt enumerates only project envs (`workflow_export.go:445`, `classify_envs.go`), so the dropped service-env keys never appear on the agent's review surface → GAP0-1 is *silent*, not merely incomplete. **Fix:** extend the classify table to service-scoped rows once GAP0-1 carries them; minimum stop-gap: warn naming observed-but-dropped service keys.
- **[GAP2-1] high · fundamental-design** — local-stage meta frozen at first-adopt topology; a second runtime is unrepresentable and dead-ends in a self-contradicting loop. `LocalAutoAdopt` no-ops once any meta exists; when a single-runtime local-stage project gains a second runtime, the deploy/develop gates emit `recovery=action=adopt-local`, but `handleAdoptLocal` HARD-REFUSES (Mode already `PlanModeLocalStage`) and tells the agent to hand-edit `.zcp/state/`. Gate says "use adopt-local," adopt-local says "you can't" — closed loop, no in-product path. The local-only→local-stage *upgrade* IS recoverable via the same action, proving this is a hole not an end-state. **Fix (decide model first):** either local is genuinely single-stage (then fix the gate hints to stop promising adopt-local) or it may target N runtimes (then lift the hard-refuse into a relink/add sub-mode + N-target meta).
- **[GAP2-2] medium · unreliability** *(partial)* — `FormatLocalStateNote` switches shape on the frozen `meta.Mode` but enumerates from live services — a half-live mirror that renders "Adopted as local-stage (linked to A)" and never mentions a now-present runtime B, so the agent never learns B exists. **Fix:** make the note a true function of (frozen meta, live services) and surface divergence with the correct next action.
- **[GAP4-1] high · fundamental-design** — Bootstrap git-init failure is swallowed (`workflow_bootstrap.go:205`, stderr only, mount not marked failed), and git-push-setup — the FIRST step of the source-control chain, with no prior-deploy gate — runs `BuildGitOriginSyncCommand` (`git remote add/set-url`) with NO `.git` init guard, while the *deploy* path (`buildSSHCommand`) DOES have `test -d .git || git init`. One consumer heals the missing `.git`, the peer consumer crashes on it; the handler's own recovery string even says "Confirm the container has /var/www/.git initialized (bootstrap runs InitServiceGit)" — assuming the precondition the same subsystem silently drops. **Fix:** make git-init a recorded precondition (surface failure into `AutoMountInfo`) and/or give the origin-sync the same init guard — one shared "ensure /var/www/.git" primitive both consumers call.
- **[GAP4-3] medium · unreliability** *(partial)* — `cleanupImportYAML` deletes import.yaml from project root keyed only on mount `Status==MOUNTED`, ignoring git-init outcome and racing SSHFS reconnect; a partial multi-mount copy set can still trip `copied>0` and delete the root copy while some services never received it. **Partial:** the broad "service can't use import.yaml" severity is contradicted by deploy-time re-init; the real residue is the multi-mount partial-copy delete race. **Fix:** gate the root delete on a stronger predicate (≥1 verified-ready mount), re-stat copies before counting them durable.

### Cleanup — overcomplexity / deletable

- **[WF-1 export-variant] high · overcomplexity** — The export `variant` dimension is inert end-to-end: `resolveExportVariant` forces an extra MCP round-trip + a dev/stage mismatch gate to compute a `topology.ExportVariant` that `composeImportYAML` discards (`export.go:119` `_ = variant`); `runtimeImportMode` is mode-blind (always NON_HA), `pickSetupName` keys off `sourceMode`, and the yaml/git are read from the fixed `TargetService`. The variant only echoes back as a display string. The golden tests "look" like they pin variant behavior but differ only because `TargetHostname/SourceMode/SetupName` are manually changed. **Fix:** delete the variant dimension (drop `variantPromptResponse` + the mismatch branch + the arg), collapse export to a 2-call narrowing; rename `bundle.Variant`→`LaunchVariant` once export's half is gone. (Do NOT touch the live launch-side `bundle.Variant`.)
- **[WF-2 verify-annotations] medium · logic-error** — `zerops_verify` declares `ReadOnlyHint:true`+`IdempotentHint:true` (`verify.go:34-38`) but persistently mutates session state and can auto-close the session (one-way transition). A host trusting `ReadOnlyHint` (to skip confirmation / cache / run speculatively) silently mutates and can terminate a develop session — the annotation is load-bearing in MCP. **Fix:** flip both to false. (Subdomain's `IdempotentHint` is actually correct via check-before-mutate — leave it.)
- **[LAUNCH-5] medium · overcomplexity** *(partial)* — Three sites still hard-code the §P5-deleted `prod` setup-name fallback (`bundle/launch.go:77`, `launch_promotables.go:167`, `workflow_launch_production.go:916`); the composer comment says "ONLY here" — false (it's in three). **Partial:** two downstream guards surface the blocker so it masks nothing behaviorally; residue is the stale comment + cosmetic redundancy → downgrade to cleanup. **Fix:** seed `PrimarySetupName/StageSetupName` in fixtures, collapse to one resolver (empty→structured blocker), delete `legacyDefaultSetupName`.

### Platform/SDK (both partial — verify the SDK capability before acting)

- **[PLAT-1] high · unreliability** *(partial; open-from-workflow-family)* — Search APIs apply server `LIMIT` account-wide then post-filter by project in Go (`zerops_search.go:198`): the EsFilter contains only `{clientId eq <accountId>}`, sort DESC, limit N — so the newest N across the whole account are returned and ZCP discards the wrong-project ones *after* the limit. `pollBuild` (limit=10) can then never see its service-stack's build event → 15-min false-timeout on a build that succeeded. **Partial:** the account-wide multi-project trigger is gated out by the develop auth-gate (`ErrTokenMultiProject`); the *residual* in-project trigger is concurrent multi-service activity (recipe import, batch, parallel deploys) exhausting limit=10. Treat as defense-in-depth. **Before fixing, verify the search endpoint accepts a `projectId` predicate**, then add `{projectId eq <projectID>}` to both filters.
- **[PLAT-2] medium · unreliability** *(partial)* — Env upsert is non-atomic delete-then-create (`env.go:127`→`132`): on a key collision it deletes first, then creates; a transient create failure destroys the existing variable (possibly a secret) with no restore. **Partial:** the "delete-then-create is forced" premise is wrong — **the SDK exposes atomic per-id `PutUserData`/`PutProjectEnv`**; prefer wiring the PUT over a compensator. **Spot-check the SDK capability**, then either use PUT or add a compensating re-create from the captured `existing` value.

---

## 5. Refuted (verifiers killed these — recorded so we don't re-discover)

- **DM-2 self-deploy source-destruction guard "fails open when SSHFS mount absent"** (deploy) — refuted ×2.
- **Two snapshot builders disagree on SetupName population** (x-fragmentation) — refuted (subsumed/incorrect framing; the real SetupName issue is WF-1, which IS confirmed).
- **git-push-setup stamps configured on restart timeout** (x-fragmentation duplicate) — refuted as a duplicate framing; *however* the x-reliability instance (**XCUT-2**) was confirmed. Net: the bug is real (XCUT-2); one redundant framing was correctly killed.
- **git-push deploy records under wrong host key** (x-coherence) — refuted ×2.

---

## 6. Partial-verdict spot-check flags (treat location as real, headline severity as suspect)

- **ADOPT-1** — fix only the startWithoutCode/first-deploy-suppression half; the diagnose-before-destruct pillar was refuted.
- **DEV-2** — real but low-frequency (narrow trigger).
- **PLAT-1** — latent correctness real; the "root cause of build-poll 15-min timeout" headline is unverified; verify the `projectId` search predicate first.
- **PLAT-2** — prefer the SDK PUT over a compensator; spot-check the SDK capability.
- **LAUNCH-5** — downgrade from correctness-bug to cleanup.
- **GAP4-3** — real residue is the multi-mount partial-copy delete race, not git-init escalation.
- **SPINE-1 direction conflict** — root confirmed; the finding's *headline* fix (make work-first authoritative) **violates spec §5.3/§9.3** which makes **bootstrap-primary** canonical. Use the *secondary* fix (bootstrap status branch merges a backgrounded work-session block). Confirm the spec reading before landing.

---

## 7. Relationship to the two prior audits (2026-05-28 / 2026-05-29)

This pass **independently re-verified** the prior audits' still-open items with a default-refute gate
and confirmed they remain open with fresh evidence — while finding the fundamental/logic issues the
bug-hunt and drift-hunt did not center:

| Prior item | This pass |
|---|---|
| workflow-family F2/F3/T2 — local-adopt meta omits dims (keystone) | **Confirmed STILL OPEN** as TOPO-1/WF-2/DELIV-2; refined: partial fix (case-0 only) made the three branches *mutually* inconsistent; `parseMeta` still doesn't normalize. |
| workflow-family F1/F4/T1 — setup-name resolver wired at 2 | **Confirmed** as WF-1: proved the synthetic-snapshot omission bakes `--setup prod` into committed CI; triple docstring/code/store mismatch. |
| workflow-family F6/T3 — launch multi-runtime gating gaps | **Confirmed + extended** as LAUNCH-3 + the root LAUNCH-4 (zero test coverage of the whole multi-runtime path) + the new LAUNCH-1/LAUNCH-2 logic bugs that survive *because* no 2-runtime test exists. |
| workflow-family F13/T4 — launch parallel state model | **Confirmed** as WF-5 (owner decision). |
| wholecodebase #1 — stage-hostname lock bypass | **Confirmed** as SPINE-4/TOPO-2; added TOPO-3 (the pin can't catch its own invariant). |
| wholecodebase #2 — git-push stamps configured on poll timeout | **Confirmed** as XCUT-2 (one redundant framing refuted). |
| wholecodebase #4 — env precedence inversion | adjacent; this pass found the typed-availability read-path gap (ENV-1) + generate-dotenv issues (ENV-2/ENV-3) instead. |
| **NEW fundamental issues neither prior pass found** | **XCUT-1** (data-corruption RMW race), **SPINE-1** (phase precedence modeled twice), **DELIV-1** (Plan-vs-atom contradiction), **LAUNCH-1/LAUNCH-2** (launch reports success on failure), **GAP0** (bundle env-layer loss), **GAP2** (local multi-runtime hole), **GAP3-1** (override stamp persists), **GAP4-1** (git-init invariant). |

---

## 8. Suggested order (state-integrity first, then parallel-path parity, then holes, then cleanup)

1. **Data integrity:** XCUT-1 (the lock) — single highest-leverage, fixable now, no public-surface change.
2. **Wrong-artifact / false-success bugs:** WF-1 (committed `--setup prod`), LAUNCH-1 + LAUNCH-2 (launch reports success on failure), DELIV-1 (Plan contradicts atom), SPINE-1 (status hides develop work).
3. **Normalization seam + writers:** TOPO-1/WF-2/DELIV-2 (`NewServiceMeta` + one `parseMeta`-on-read normalization) — closes the atom-chain misfire for every writer at once.
4. **Parallel-path parity + swallowed flags:** DEV-1, DEPLOY-1, DEPLOY-3, XCUT-2/XCUT-3, SPINE-3, SPINE-4/TOPO-2 (+TOPO-3 pin), VERIFY-1.
5. **ACTIVE≠deployed:** ADOPT-1 (the startWithoutCode half) + ADOPT-2 + GAP3-1 — gate "deployed" on a successful appVersion at every door.
6. **Local mode:** BOOT-1/BOOT-2 (broken classic-local bootstrap), GAP2 (multi-runtime hole — decide model), GAP4-1 (git-init invariant).
7. **Missing capability:** GAP0 (bundle service-env layer — decide scope).
8. **Owner-gated structural:** WF-5 (launch state model), PLAT-1/PLAT-2 (verify SDK capability first).
9. **Cleanup:** export-variant deletion, verify annotations, LAUNCH-5, multi-runtime test coverage (LAUNCH-4).

Full machine-readable findings (intended/actual/problem/root/fix per finding) live in the audit run
output; this doc is the human map. Per repo policy, every fix that lands a new invariant must be
pinned with a test — several findings (XCUT-1, SPINE-1, LAUNCH-1/3/4, TOPO-2/3, DELIV-1/2, BOOT-1/2)
explicitly note the *missing* test is how the bug slipped.
