# Launch-production residuals: stage gating, HA consent, tag→prod CD (2026-06-10)

> **SUPERSEDED as the work plan by `plans/git-delivery-foundations-2026-06-10.md`** (Karel's
> verdict: this doc's phases were symptom-level; the foundations doc carries the ground-up model —
> filesystem/git/credential laws, matrices, scenario catalog — and the re-derived fix program
> FP-1..6, which absorbs P0–P5 below). Kept as the gap-analysis evidence record (the 10-agent map
> + prod.txt second-pass findings remain valid and are referenced from the foundations doc).

**Origin:** Karel's review of a live production-transition session (transcript `.zcp/manual/prod.txt`
— a 2026-06-10 container session: Bun weather dashboard, simple mode, single service, bootstrap →
develop → launch-production into `weather-prod`; second-pass findings folded in below) + a 10-agent
verified code analysis (7 mappers + 3 adversarial verifiers, all load-bearing claims confirmed with
file:line). Builds directly on `plans/archive/production-transition-2026-06-10.md` (F0–F9, merged to main
2026-06-10 as 20b5737b). The session ran on `zcp v9.112.1-25-g20b5737b-dirty` (built 15:06, the
post-F0–F9 merge) — verified live via `ssh zcp 'zcp version'` — so every observed friction is
CURRENT-code behavior, F5/F6 included.

**Karel's three issue areas (from the session review):**
1. A project with only a dev service entering launch-production should be ASKED whether to create a
   stage service first (verify everything there; stage becomes the promotion basis). If declined,
   ZCP must say explicitly how the production zerops.yaml setup + infra will be derived.
2. Production services run HA (2 containers per runtime). Before that happens there must be a check
   whether the app is HA-ready, the user must be asked whether they WANT HA at all (both answers
   first-class), and optionally asked about container counts / expected load.
3. Push topology must be clear and collected early: git + secrets configured on the SERVICE; the
   recommended ideal is "give repo name + token with workflows scope → ZCP wires everything so
   `git push` builds stage and `git push --tags` runs the production pipeline". All historical
   variants (dashboard webhook etc.) stay supported.

---

## Verified current state (the gaps, with evidence)

### Gap 1 — dev-only sources: no recommendation, one deadlock, one silent promotion

- **No mode gating anywhere in launch.** `validateLaunchSourceControl` never reads `meta.Mode`
  (launch_source_control_gate.go:147-200); the readiness rubric's 6 checks are bundle-shape pins
  only (launch_readiness.go:16-23). A dev-only **ModeSimple** singleton is the promotion headline
  (launch_source_context.go:150-168) and promotes silently end-to-end. No launch atom or handler
  string recommends creating a stage first (grep verified — only presentational stage mentions).
- **ModeDev singleton deadlocks.** Gate fires `git-push-unconfigured-<host>` whose Recovery points
  at git-push-setup, which REFUSES ModeDev (`PushSourceCheckFor` → PushSourceModeUnsupported,
  workflow_git_push_setup.go:144-158; `topology.IsPushSource` predicates.go:111-119). The only
  escape (mode-expansion) is buried in that error string. This is the backlogged LP-5
  (export-residual-ergonomics.md item 5) — absorbed into this plan.
- **Silent iteration-setup promotion.** Setup-name cascade: per-promotable override → workflow
  override → `meta.ProdSetupName` → `StageSetupName` → `PrimarySetupName` → literal `"prod"`
  (launch_promotables.go:153-172). A dev-only pair with `PrimarySetupName="dev"` imports production
  with `zeropsSetup: dev` — the dev iteration block (hot-reload watchers) runs in production with
  zero mention. `deriveProdSetupBlock` (launch_prod_setup_derive.go) fires ONLY when no name
  resolves at all. Doc drift: launch_promotables.go:27-32 still claims no "prod" default exists.
- **F4 verified-setup evidence is write-only.** `RecordVerifiedSetup` is called from verify
  (verify.go:205-218); `workflow.ReadVerifiedSetups` has ZERO production callers. Launch neither
  warns nor blocks on never-verified setups. (Also: the sidecar lacks the commit SHA/appVersion
  field the F4 plan promised.)

### Gap 2 — HA is a silent unchecked policy, consent is implicit-by-silence

- Runtimes: `mode: NON_HA` hardcoded + `minContainers` floored to 2 + `cpuMode: DEDICATED`
  (ops/bundle/launch.go:14-22, 213-269). The floor's own comment says "HA-via-replication for
  **stateless apps**" — an assumption nothing checks. Managed deps: `mode: HA` by default,
  `keepNonHA` opt-out (rules.go:66-95) — documented as a parameter, never instructed as a question
  (launch-scope-prompt.md:30).
- **No HA-readiness check exists** — no statelessness / local-disk / session-storage / mount
  analysis anywhere in the launch path. The knowledge EXISTS unwired:
  `guides/production-checklist.md` ("File-based sessions break with multiple containers", "Sessions
  in Valkey, uploads in Object Storage — no local state") is pull-only; the worker queue-group
  double-processing check lives in recipe authoring only (ops/checks/worker_gotcha.go).
- **Consent screen hides scaling.** `launchPreviewService` carries Hostname/Type/Role/Mode/Setup —
  no container counts (workflow_launch_production.go:1352-1422). Scaling surfaces only via
  transform warnings; a source whose scaling read failed (nil) is floored to 2 with NO warning
  (warn branch requires a present value <2).
- **Rubric is informational.** `hasBlockingFailures` has zero production callers (stale doc-comment
  claims otherwise, launch_readiness.go:236-246); publish independently gates schema errors via a
  parallel `len(bundle.Errors)>0` check (workflow_launch_production.go:650-661). The
  `prod-runtime-min-containers` check frames <2 as "composer regression" — i.e. the floor is
  non-negotiable by pin, blocking any consented 1-container production.
- **prod-ops has no `scale`.** The F7 plan row listed it (production-transition:111-112); the
  shipped op set is status/logs/env-keys/restart/stop/start/delete-service (launch_prod_ops.go:42-50).
  Undocumented drop — post-launch container adjustment has no ZCP path inside the window.
- Schema note: the live import schema marks `services[].mode` **deprecated** ("use Type version
  only" — colon forms `postgresql:ha@16`); its type enum has NO plain forms. ZCP's plain-type+mode
  emission passes only because structure-validation strips the type enum. Also
  bootstrap-provision-rules.md:53-60 says "do NOT set mode on runtime services" while launch/export
  emit `mode: NON_HA` on runtimes — tell≠check contradiction.

### Gap 3 — tag→prod exists only as a dashboard instruction; the Actions prod path was silently cut

- Stage half (ZCP-configured): Actions template triggers `on: push: branches: [main]`, deploys
  `zcli push --service-id <STAGE> --setup <setup>` (workflow_build_integration.go:520-541). Webhook
  variant: dashboard OAuth on the stage service. Both stage-only.
- Prod half (instruction-only): launch recommends `eventType=TAG`, regex `^v\d+\.\d+\.\d+$`
  (launch_pipeline.go:19-25) and the user clicks it together in the prod dashboard (Path B,
  P-LP-7 read-only; Path A = platform-PUT rejected empirically — per-clientUser OAuth,
  backlog/launch-pipeline-close-loop-oauth.md).
- **F7 "Actions prod-secrets post-create" shipped NOWHERE** — not in commit 8cd096a8, not in the
  ship log, not in any backlog entry (verified; the existing auto-wire-github-actions-secret.md is
  the unrelated 2026-04-29 stage-side item). A GitHub-Actions-driven production deploy does not
  exist. This is a silent scope cut by our own plan-fidelity standard — surfaced here, resolved by
  Phase P3 below.
- Steady-state today: prod rebuilds ONLY via the user-configured dashboard TAG integration or
  manual `zcli push`; after launchKey revocation ZCP has no prod deploy path (by design).

## Second pass — what the real session showed (prod.txt, 2026-06-10 15:16–17:35)

The session (Bun weather dashboard, `simple` mode, single runtime `weather` in eval-zcp →
launch-production into `weather-prod`) CONFIRMED Karel's three areas in the wild and surfaced three
NEW root-cause findings:

- **T1 — Unrelated managed dep bundled into production (NEW → P2.0).** The composer auto-included
  the PostgreSQL `db` left over from an earlier Laravel task — weather has zero `${db_*}` references
  — and defaulted it to HA. The user's explicit "chci tam jenom ten weather" was UNSERVABLE: no
  input excludes a managed service from the bundle. The agent improvised `keepNonHA=[db]` + post-
  launch `prod-ops delete-service db` (provision-then-destroy — billable churn + needless risk).
  Same root leaks project envs: the stale Laravel `APP_KEY` went through classify into prod. Launch
  scopes the bundle to the whole SOURCE PROJECT, not to the promoted runtime's actual dependencies.
- **T2 — Simple-mode git state is artifact-ephemeral; the gate breaks AFTER the first git-push
  build (NEW → P0.2 + P1.5).** Sequence observed: git-push-setup OK → commit + `zerops_deploy
  strategy=git-push` PUSHED → the async platform build completed and REPLACED the container →
  `/var/www/.git` vanished (artifact tree carries no .git) → the publish-side gate re-run failed
  `remote-mismatch-weather` with live="" (real state on container: `fatal: not a git repository`)
  — at the worst possible moment, launchKey already in hand. The agent manually reconstructed git
  over SSH (init + remote add + fetch + reset to remote HEAD) and the launch then passed. On ANY
  self-deploying simple/dev-only service this reproduces after EVERY successful build — the live
  git checks are structurally at odds with artifact-replacement. (A dev/stage pair is immune: the
  dev half's volume is persistent — one more argument for the P1.2 stage recommendation.)
- **T3 — GIT_TOKEN rotation path missing (NEW → P0.1).** The first PAT lacked Secrets+Workflows;
  the user issued a NEW token. Re-running git-push-setup with the same remote + the new token
  short-circuited `already-configured` ("no probe, env write, or container restart performed") —
  the O3 check ignores a freshly supplied gitToken. The agent bypassed via raw `zerops_env set
  GIT_TOKEN=…`, which (a) echoed the token value back in the response payload and (b) skipped the
  probe-first discipline. Rotation must be a first-class same-remote path.
- Confirmations: no stage question for the simple singleton (area 1 — the launch went scope →
  source-control → classify → ready with zero topology prompt); HA/scaling was ANNOUNCED, never
  consented — the agent relayed "minContainers 1→2, DEDICATED, SERIOUS" as fait accompli and the
  only question it had to improvise was the db one (area 2); prod CD ended in manual dashboard
  clicking with the TAG walkthrough, session ends mid-checklist (area 3). Positives worth keeping:
  bundle preview + prod-policy warnings reached the user verbatim; AskUserQuestion was used at every
  decision the workflow DID surface; the launchKey walkthrough worked first try.

### Cross-cutting drifts found on the way

- launch-intro.md:17 still lists "optional custom domain" + "emitted DNS records" (F2 hard-deleted
  customDomain; ZCP never emits DNS records).
- idle-launch-entry.md:19 claims stage-half targetService "fires a corrective scope-prompt blocker"
  — the handler silently normalizes either half (`normalizeTargetServiceForLaunch`).
- In-flight uncommitted WIP in the tree (apiHost threading into prod-ops, export Variant removal)
  — coordinate before starting; P2's prod-ops scale op touches the same handler.

---

## Design constraints honored (from the decision ledger)

- **No new hard gates** outside the goal-contracts §3.4 list — every new ask is a precomputed
  recommendation + choices[] with ready-to-execute filled-args calls, warn+ack dismissible
  (delivery-model-final + goal-contracts, 2026-06-09). The shape to clone is
  `build-integration-recommended` + `skipBuildIntegration` ack.
- **A required create-stage-before-prod gate was considered and rejected** (source-of-truth plan,
  Karel picked LastDeployedCommitSHA tracking instead). What Karel asks for NOW is an interactive
  recommendation at entry — a consent question, not a gate. P1 implements exactly that.
- **Path A (ZCP PUTs platform integration) stays rejected** — P3's Actions prod job is repo-side,
  agent-executed via gh CLI, the same trust boundary as today's stage half. one-collect
  (ZCP-as-GitHub-actor) stays backlogged.
- **No auto-push inside launch; dirty tree stays a hard block; no buildFromGit ref pinning.**

---

## Phases

### P0 — Transcript fix-now bundle [S]

1. **git-push-setup token rotation (T3):** same canonical remote + a gitToken PRESENT in the call →
   do NOT short-circuit; run the full probe → service-secret re-write → restart → stamp sequence
   (the O3 short-circuit keeps firing only for token-LESS re-calls). Kills the raw-env bypass and
   its token echo. Pin: TestGitPushSetupContainer_SameRemoteNewToken_Rotates,
   TestGitPushSetupContainer_SameRemoteNoToken_ShortCircuits.
2. **Honest `git-state-missing` blocker (T2, detection half):** the gate's live-remote read
   distinguishes `fatal: not a git repository` (and live="" generally) from a genuine URL mismatch —
   new blocker `git-state-missing-<host>`: "the container filesystem was replaced by a deploy and
   carries no .git — this is expected on self-deploying services, not a remote drift" + Recovery →
   git-push-setup re-run (which after P1.5 reconstructs). Until P1.5 lands, the blocker carries the
   prefilled reconstruction command block (the sequence the agent improvised, verbatim and safe:
   init + remote add from meta.RemoteURL + fetch + reset to remote HEAD when porcelain-clean).
   Pin: TestValidateLaunchSourceControl_GitStateMissing_DistinctFromMismatch.

### P1 — Dev-only entry decision (stage recommendation + honest setup provenance) [M]

1. **LP-5 absorbed (un-backlog):** `meta.PushSourceCheckFor` at the top of
   `validateLaunchSourceControl`. ModeDev / standalone-ModeStage → blocker
   `mode-unsupported-<host>` with structured Recovery = prefilled bootstrap mode-expansion call
   (route=adopt, isExisting, bootstrapMode=standard, suggested stageHostname). Kills the
   git-push-unconfigured ↔ ModeDev-refusal loop. No "proceed anyway" here — push is impossible.
2. **Stage recommendation for no-stage push-capable sources** (ModeSimple, ModeLocalOnly): the
   scope-prompt response gains a `stageRecommendation` block — precomputed recommendation ("create
   a stage half first; verify there; the verified stage setup becomes the promotion source") +
   choices[]: (a) expand to pair — prefilled bootstrap mode-expansion call (recommended), (b)
   proceed with direct promotion — re-call with ack input (`skipStageRecommendation=true`,
   accumulator-carried). Warn severity, fires once, ack self-extinguishes. New atom
   `launch-stage-recommendation` (axis launch-production-active) carrying the why (stage = verified
   last-known-good; prod buildFromGit builds the same pushed HEAD) and both calls.
3. **Kill silent iteration-setup promotion:** resolved setup name carries provenance
   (override | prodSetupName | stageSetupName | primarySetupName). When provenance is
   `primarySetupName` on a no-stage source — the dev iteration setup — the response does NOT
   silently proceed: it attaches the `deriveProdSetupBlock` proposal (already implemented, today
   reachable only via the missing-setup blocker) as choices[]: commit the derived `setup: prod`
   block (recommended) / `prodSetupNameOverride="<existing>"`. This is the Karel item "if I decline
   stage, say how the yaml is derived". The §P5 `"prod"` literal cascade tail dies (structured
   blocker on empty — finishing the deferred P5 cut; safe now, the choices flow seeds the name).
   Fix the launch_promotables.go:27-32 doc drift as part of this.
4. **Wire the F4 evidence read (completes the half-shipped F4):** new readiness check
   `prod-setup-verified` (warn): reads `ReadVerifiedSetups`; pass → "setup <name> verified <when>
   against <host>"; warn → "never verified — run zerops_verify on <host> before launch
   (recommended)". Add the missing commit-SHA/appVersion field to the sidecar at write time
   (verify-side, cheap) so the check can say "verified at commit <sha>" and the stage
   recommendation rationale has teeth.

5. **Simple-mode git lifecycle reconstruction (T2, fix half):** git-push-setup re-run on a target
   whose `/var/www/.git` is absent but whose meta records `GitPushState=configured` + RemoteURL
   performs the reconstruction HANDLER-SIDE (single owner of container git wiring; probe-first):
   init + remote add (clean URL, token stays in $GIT_TOKEN/.netrc, never embedded) + fetch + reset
   to remote HEAD — REFUSED when the working tree differs from the remote tree beyond
   artifact-derivable paths (don't destroy un-pushed edits; surface a diff summary instead).
   Spec gains the platform fact: on self-deploying services every successful build replaces
   /var/www including .git — live git state is artifact-ephemeral, the recorded meta + remote are
   the durable truth. Pin: TestGitPushSetup_ReconstructsMissingGit_*,
   TestGitPushSetup_ReconstructRefusesDivergentTree.

   Pins: TestValidateLaunchSourceControl_ModeUnsupported_*, TestScopePrompt_StageRecommendation_*,
   TestResolveLaunchSetupName_Provenance_*, TestReadinessRubric_SetupVerified_*. Spec §10: new
   rows for mode-unsupported + stage-recommendation + setup-provenance; P-LP table untouched.

### P2 — Production scope + HA consent, readiness assessment, honest preview, prod-ops scale [L]

0. **Bundle scoping — promoted runtime's dependencies, not the whole project (T1):** the composer's
   managed-dep collection becomes REFERENCE-DRIVEN: deps the promoted runtime(s) actually wire
   (`${<dep>_*}` refs in the source setup/envs — the same resolution export claims for its bundle;
   verify export's actual collection rule and unify on ONE owner) are included by default;
   UNREFERENCED managed services in the source project become explicit per-dep include/exclude
   choices inside the P2.1 profile ask (new accumulator input, e.g.
   `managedDeps={"db":"exclude","redis":"include"}`). Same scoping for project envs feeding
   classify: envs referenced by the promoted runtime classify as today; orphan envs (the stale
   APP_KEY case) get a precomputed `exclude` recommendation instead of a forced bucket. The
   provision-then-destroy workaround (launch with unwanted dep → prod-ops delete-service) dies.
   Pins: TestBuildLaunch_UnreferencedDepExcludedByChoice, TestLaunchScope_OrphanEnvRecommendsExclude.

1. **Production-profile ask at scope-prompt:** response gains `productionProfile` block with
   precomputed recommendation per service: managed deps → HA (recommended) with per-dep keepNonHA
   opt-out; runtimes → containers 2 (recommended, "no-downtime deploys + failover") with consented
   alternatives (1 = cheaper, no failover; >2 for load). New input
   `runtimeScaling={"<host>": {"minContainers": N, "maxContainers": M}}` (accumulator-carried).
   The scope atom instructs an explicit AskUserQuestion covering: HA for managed deps yes/no,
   runtime container count (with the expected-load framing), defaulting to the recommendation.
2. **Composer + rubric follow consent:** floor stays the DEFAULT (no consent → 2, as today);
   consented `runtimeScaling` overrides it, including minContainers=1.
   `prod-runtime-min-containers` becomes "matches consented profile": consented 1 → pass with warn
   row "single-container production by request — no failover, brief downtime per deploy";
   unconsented <2 → fail/block (still catches composer regressions).
3. **HA-readiness assessment (the "is the app ready" check):** new atom `launch-ha-assessment`
   (launch-production-active) carrying the distilled checklist derived from
   guides/production-checklist.md: sessions external (Valkey/DB, file sessions break across
   containers), uploads/state in Object Storage (no local-disk writes), worker queue-group
   semantics (every message once per replica without it), migrations via `zsc execOnce`. Instructed
   flow: BEFORE the user confirms containers ≥2, the agent assesses the source code against the
   checklist (it has repo access) and reports findings inside the profile ask. ZCP-side cheap
   programmatic signals attached to the profile block where derivable from Discover: shared-storage
   deps present (flag mounts), worker-shaped runtimes (flag queue-group item). Honest contract: ZCP
   provides the checklist + signals; the agent performs the code-level assessment; the user
   consents. No heuristic auto-verdict.
4. **bundlePreview scaling honesty:** preview service rows gain `containers` ("2–3") and `cpuMode`;
   emit the floor warning ALSO when source scaling read failed (today nil-scaling floors silently).
5. **prod-ops `scale`:** add the dropped F7 op (admin-client delegation + jsonschema enum +
   per-call launchKey as the rest). Non-destructive — no confirmDestructive. Post-launch container
   adjustment becomes possible inside the bring-up window; document the alternative (fresh
   prod-scoped MCP session) stays.
6. **Runtime `mode:` emission decision (Karel decision #4 below):** recommended — STOP emitting
   `mode: NON_HA` on runtime entries in launch + export composers (schema marks it deprecated;
   bootstrap-provision-rules atom already forbids it; runtime HA semantics live in minContainers).
   Aligns tell==check; export-validate.md adjusted in the same sweep.

   Pins: TestScopePrompt_ProductionProfile_*, TestBuildLaunch_ConsentedScaling_*,
   TestReadinessRubric_ConsentedSingleContainer_*, TestLaunchPreview_CarriesContainers,
   TestHandleLaunchProdOps_Scale_*, TestComposeLaunchImportYAML_NoRuntimeMode (if #4 approved).
   Live verification on eval-zcp: one launch with consented scaling, read-back of containers.

### P3 — Tag→prod Actions path (resurrects the cut F7 item) + topology declared early [L]

1. **Topology told once, early:** build-integration=actions confirm response + atom gain the
   two-track story explicitly: "push to main → stage build (wired now); at launch, a tag-triggered
   prod workflow is added — `git push --tags` deploys production". The integration CHOICE made at
   git-push-setup remains the single early collection point (Karel's "ask within the workflow");
   no new meta field — launch derives the prod-CD shape from the recorded BuildIntegration.
2. **Launched response wires the Actions prod half** (BuildIntegration=actions): carries (a) a
   complete second workflow file `.github/workflows/zerops-prod.yml` with
   `on: push: tags: ['v*.*.*']` running `zcli push --service-id <PROD-service-id> --setup <prod
   setup>` per promoted runtime — service IDs are concrete (known post-import from
   ImportedServices), YAML fully prefilled; (b) gh commands to set `ZEROPS_TOKEN_PROD` (the durable
   prod-project-scoped token the post-checklist ALREADY tells the user to create for ongoing prod
   iteration — one token, two uses) — agent-executed via gh CLI, same trust boundary as the stage
   half; (c) commit+push instruction. launch-post-checklist step 5 branches by integration.
3. **Earn/verify:** launch resume (and prod-ops status done-boundary) earns the Actions prod path
   via the F1 machinery — workflow file present at pushed HEAD → pipelineSummary state
   `configured-actions`; the platform-webhook read-back path stays for webhook users. The done
   boundary accepts either earned shape (or explicit skip).
4. **Webhook path unchanged as the supported alternative** (dashboard TAG integration, Path B
   read-only). launch-pipeline-* atoms present both options, recommending the one matching the
   declared integration.

   Pins: TestActionsProdWorkflowYAML_TagTrigger, TestLaunchLaunched_ActionsProdCD_*,
   TestDoneBoundary_ActionsEarned, TestPipelineSummary_ConfiguredActions. Spec §10: prod-CD
   two-option row; P-LP-7 untouched (still no platform PUT). flow-eval: the actions-tag journey
   (launch → secrets+file → resume earn); GitHub-side execution itself is out of eval reach —
   file-earn is the verified signal.

### P4 — Consistency + drift sweep [S]

- launch-intro.md:17: drop customDomain + DNS-records claims (F2 leftover).
- idle-launch-entry.md:19: fix the stale "stage-half fires a corrective blocker" claim →
  "either half accepted, handler normalizes".
- `hasBlockingFailures`: wire it as the publish-side gate replacing the parallel ad-hoc
  `len(bundle.Errors)>0` check (single owner for "can this bundle publish"), or — if wiring is
  rejected — delete it and fix the stale doc-comment. Recommended: wire it (rubric runs on publish
  branch too; block-severity = refuse with the rubric rows as blockers).
- bootstrap-provision-rules.md runtime-mode bullet: becomes true once P2.6 ships (verify, adjust
  wording if needed).

### P5 — flow-eval + e2e round-trip [M]

Scenarios: (1) dev-only simple → prod with stage recommendation DECLINED (asserts: profile ask,
derived prod-setup proposal, no silent dev-setup promotion); (2) stage recommendation ACCEPTED
(mode-expansion → verify → launch; asserts verified-evidence check passes); (3) HA consent with
consented 1-container (asserts rubric warn row, composed yaml); (4) actions-tag prod journey
(launch → prod workflow + secrets instructions → resume earn → done boundary); (5) webhook-path
regression (existing scenario re-run); (6) the prod.txt replay: simple service + unrelated managed
dep in source project — asserts the dep surfaces as an exclude choice (no provision-then-destroy)
and a post-build launch re-call hits `git-state-missing` with working reconstruction, not
remote-mismatch; (7) token rotation mid-flow (new PAT, same remote) — asserts re-probe + secret
re-write, no raw-env bypass. e2e on eval-zcp: consented-scaling launch read-back (piggybacks on the
still-pending F3 ZCP_LAUNCH_KEY matrix run).

---

## Open decisions for Karel

1. **Stage recommendation strength** (P1.2): dismissible warn+ack (recommended — matches the
   no-new-gates ledger) vs hard requirement for no-stage sources. Note the hard variant was
   considered and rejected in the 2026-05-20 plan; re-proposing it needs this explicit flag.
2. **Prod-CD recommended default for actions users** (P3): Actions tag workflow (recommended —
   end-to-end instructable, no dashboard step; durable prod token lives in GitHub repo secrets) vs
   dashboard TAG webhook (platform-native, no token in GitHub, needs GUI OAuth). Both supported
   either way; this decides which one the atoms recommend first.
3. **Consented 1-container production** (P2.2): allow with warn ack (recommended) vs keep the hard
   floor of 2.
4. **Drop runtime `mode: NON_HA` emission** (P2.6): recommended yes (schema-deprecated, atom
   contradiction); needs one live ImportServices verification on eval-zcp that a runtime entry
   without `mode` imports clean (expected — bootstrap imports never set it).
5. **Unreferenced managed deps default** (P2.0): exclude-by-default with per-dep opt-in choice
   (recommended — matches the observed user intent "jen weather" and the no-surprise-billing
   principle) vs include-by-default with opt-out (today's behavior, additive-safe). Either way the
   choice is explicit in the profile ask; this decides only the precomputed recommendation.

## Backlog interactions

- `plans/backlog/export-residual-ergonomics.md`: LP-5 extracted into P1 (remove item 5 on plan
  approval); other items untouched.
- NEW backlog candidate: canonical colon-form type emission (`postgresql:ha@16` instead of
  plain-type + deprecated `mode:`) across launch/export/bootstrap composers — schema-canonical
  modernization, separate design pass (touches discover type passthrough + recipe corpus).
- `plans/backlog/launch-one-collect-credential-flow.md`: stays deferred (gh-CLI trust boundary
  holds; P3 works within it).
- F7's silently-cut "Actions prod-secrets post-create" is resolved by P3 (no backlog entry needed).

## Effort

| Phase | Size | Est. |
|---|---|---|
| P0 transcript fix-now (rotation + git-state-missing) | S | ~0.5–1 d |
| P1 dev-only entry decision + git reconstruction | M | ~2.5 d |
| P2 production scope + HA consent + readiness + scale | L | ~4 d |
| P3 Actions tag→prod | L | ~2.5–3 d |
| P4 drift sweep | S | ~0.5 d |
| P5 flow-eval + e2e | M | ~2 d |
| **Total** | | **~12 d** |

Each phase lands green (unit+tool+integration+e2e -short) + the matching flow-eval scenario before
the next starts. Coordinate with the in-flight uncommitted WIP (apiHost → prod-ops, export Variant
removal) before P2/P3 touch the same files.
