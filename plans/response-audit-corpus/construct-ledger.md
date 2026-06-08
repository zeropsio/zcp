# Construct help-vs-hurt ledger (evidence pass over 293 errors + 88 failed deploys)

**Global ledger:** {"protective": 9.052, "induced": 145.187, "platform": 58.677, "knowledgeGap": 10.084}

| class | sampled | protective | induced | platform | knowledge-gap | dominant | median turns | induced-verdict-holds |
|---|---|---|---|---|---|---|---|---|
| step-complete-rejections (workflow:?:complete::error) — boot | 14 | 1 | 63 | 0 | 0 | CONSTRUCT_INDUCED | 1.5 | HOLDS |
| git-push-and-build-integration (workflow:?:git-push-setup::e | 15 | 1 | 19 | 46 | 6 | PLATFORM_REALITY | 1 | REFUTED/NARROWED |
| workflow:develop:start::error — rejections of `zerops_workfl | 32 | 0 | 32 | 0 | 0 | CONSTRUCT_INDUCED | 6 | HOLDS |
| deploy-preflight-vs-failures — 67 ZCP-authored deploy errors | 16 | 0.052 | 0.187 | 0.677 | 0.084 | PLATFORM_REALITY | 1 | HOLDS |
| import-errors-and-gates | 19 | 3 | 12 | 3 | 1 | CONSTRUCT_INDUCED | 1 | HOLDS |
| launch-export-classify | 17 | 3 | 10 | 4 | 0 | CONSTRUCT_INDUCED | 3 | HOLDS |
| bootstrap-start-and-misc | 18 | 1 | 9 | 5 | 3 | CONSTRUCT_INDUCED | 1 | REFUTED/NARROWED |

## Per-class exemplars + verifier assessments

### step-complete-rejections (workflow:?:complete::error) — bootstrap/launch step-gate rejecti
- exemplar: Agent's rejected call carried attestation "Adopting as a standard dev/stage pair." ZCP's response: "ambiguous dev/stage pairing: \"appdev\" (alpine/php-nginx@8.4) and \"appstage\" (alpine/php-nginx@8.4) share runtime base php-nginx@8.4 — likely a dev/stage pair, which ZCP will not guess." The gate then hands back a copy-pasteable plan encoding the exact decision the agent had already declared — the round-trip elicits format, not information. (launch-production-laravel-showcase 20260604-233841 t5; same shape fires in 32/64 instances, ~1 per session on the standard-pair fixture.)
- verifier (holds): The CONSTRUCT_INDUCED verdicts survive adversarial scrutiny for 12 of the 13 cited instances; one should be reclassified. I re-read five cited transcripts verbatim (20260604-224457/adopt-existing-standard-pair, 20260602-185022/recipe-laravel-showcase-fullstack, 20260513-133625/launch-production-from-standard-pair, 20260518-135551/api-node-postgres-classic-dev) plus the handler code (internal/workflow/adopt.go, engine.go:465-470, validate.go, recipe_shape.go). The decisive evidence is that ZCP itself subsequently classified most of these bounces as its own defects by shipping fixes: CanonicalBa
- corrected: {"induced": 62, "knowledgeGap": 0, "platform": 0, "protective": 2} — move classic-static-nginx-simple (20260503-144814 t4, omitted-bootstrapMode default-to-standard bounce) from CONSTRUCT_INDUCED to PROTECTIVE: the mode is genuinely required input and ZCP's shipped fix (validate.go: bootstrapMode now required, error names the actual missing field) retained the bounce and fixed only the diagnostic,

### git-push-and-build-integration (workflow:?:git-push-setup::error n=61 + workflow:?:build-i
- exemplar: "git-push-setup target \"appdev\" is in mode \"dev\" which does not support push-git (only Standard/Simple/LocalStage/LocalOnly do)" / suggestion: "Mode expansion (ModeDev → ModeStandard adds a stage half) is a bootstrap-with-isExisting flow, not a workflow action. Re-run bootstrap with route=adopt and a plan target that carries isExisting=true + bootstrapMode=\"standard\" + an explicit stageHostname." — issued (20260603-201645/launch-production-from-standard-pair, turn 7) against meta ZCP's OWN adopt auto-derive had just committed, two calls after the agent's attestation explicitly said "Adop
- verifier (refuted/narrowed): The CONSTRUCT_INDUCED verdict set does NOT survive intact: 4 of the 9 cited exemplars reclassify under adversarial scrutiny, including the three instances (1, 3, 7) that anchor the headline 6-8-turn detours — so the 19/26 (73%) induced rate is materially inflated. However, a genuine construct-induced core (5/9) survives decisively and should be kept.

WHAT SURVIVES (genuine induced):
(a) Instance 5 — smoking gun, transcript-verified (eval/behavioral/runs/20260605-032130/git-push-setup-with-cicd-method-prompt, lines 226→248): build-integration's needsGitPushSetup nextStep literally instructs `g
- corrected: From the original {"protective":1,"induced":19,"platform":46,"knowledgeGap":6}: reclassify the not-bootstrapped→adopt-ceremony instances (cited: instances 1, 3, 7, plus any uncited siblings of the same ADOPT_REQUIRED-over-two-same-base-runtimes shape) from induced → protective (the bounce carries a non-derivable pairing decision pinned by ErrAdoptPairingChoice; the residual finding is recovery cos

### workflow:develop:start::error — rejections of `zerops_workflow action=start workflow=devel
- exemplar: Agent: "Found `appdev` — it's a Node.js 22 service [ACTIVE]. Now I'll start a develop workflow scoped only to `appdev`." → ZCP: {"code":"ADOPT_REQUIRED","error":"No bootstrapped services found","suggestion":"Run bootstrap first: action=\"start\" workflow=\"bootstrap\" (route=\"adopt\" if services already live)"} → six calls of adopt ceremony later, the byte-identical develop start succeeds. (20260507-162820/existing-standard-appdev-only-reminders, turn 2)
- verifier (holds): I tried to refute and failed. Verified against three transcripts and the handler/spec/git history; the strongest protective rationales all collapse under ZCP's own code comments and ship history.

Steelmen examined and their fates:

1) "ServiceMeta is a genuine prerequisite — develop cannot compute scope/envelope/auto-close without it." True structurally, but that IS the construct: the refusal at internal/tools/workflow_develop.go:105-123 guards ZCP's evidence files, not platform state (every instance had ACTIVE services). Decisively, develop-start auto-adopt EXISTED and was removed by cb63bf3
- corrected: No reclassification: {"induced":32,"protective":0,"platform":0,"knowledgeGap":0} stands. Two corrections to the class's internal structure: (1) instance 9 (outOfScope INVALID_PARAMETER) should be split into its own class — it is induced by the RC-B subset-of-scope semantics plus a schema-description/check drift (workflow.go:88 vs developRoles), not by the adopt gate; lumping it with the 31 ADOPT_R

### deploy-preflight-vs-failures — 67 ZCP-authored deploy errors (preflights/gates/transport) 
- exemplar: Agent (had already diagnosed the failed build and fixed the missing public/ dir, then was refused, fetched events as the gate directed, and was refused AGAIN): "The deploy gate requires the events to be fetched first. I already did that, but it seems the gate is still blocking. Let me re-import appstage to reset its state." — followed by import-override + confirmDestructive wiping the service, 5 turns of pure ceremony to re-run a corrective redeploy that was correct on the first attempt. (recipe-nextjs-ssr-frontend-standard, 20260603-232540, turns 17-21)
- verifier (holds): I attempted to refute all three INDUCED families and failed on each; the CONSTRUCT_INDUCED verdicts survive adversarial scrutiny.

FAMILY 1 — DIAGNOSIS_REQUIRED redeploy gate (18/29, instances 2+3). Strongest possible confirmation: the repo itself removed this gate as a category error (CLAUDE.md "Hard gates" bullet, plans/deploy-gate-category-error-2026-06-04.md, regression guard TestDeployLocal_*Proceeds). Transcript verification: greenfield-fullstack (20260505-151844, line 185) shows the gate's OWN recovery field directing `zerops_import override=true startWithoutCode=true` — ZCP actively st
- corrected: No reclassification — the published distribution stands ({induced:0.187, knowledgeGap:0.084, platform:0.677, protective:0.052} over all 155; 29/67 INDUCED among ZCP-authored interventions). One refinement flag short of reclassification: the 3 dev_prod_env_divergence instances are "induced by severity miscalibration of a real quality nudge" (introducing commit targeted a genuine observed failure; c

### import-errors-and-gates
- exemplar: "The deploy gate requires the events to be fetched first. I already did that, but it seems the gate is still blocking. Let me re-import `appstage` to reset its state." — agent in recipe-nextjs-ssr-frontend-standard (20260603-232540), after complying with the deploy gate's own events-fetch prescription, still refused, and pushed into the destructive override whose ack ceremony is this error class.
- verifier (holds): The CONSTRUCT_INDUCED verdicts survive adversarial scrutiny. I re-read three cited transcripts end-to-end (20260505-151844 greenfield turns 61-64, 20260603-232540 nextjs turns 35-43, 20260521-084854 cadence turns 83-100) plus the kept import gate (internal/tools/import.go::gateOverrideOnFailedHistory), the verdict doc (plans/deploy-gate-category-error-2026-06-04.md), and the fix commits (17efbf9a deploy-gate deletion, 09d4dd0f R6 corrective). Steelman attempted: the DIAGNOSIS_REQUIRED bounce is fired by the import-override gate — the one gate ZCP deliberately kept, sanctioned by the published 
- corrected: Unchanged — all 12 CONSTRUCT_INDUCED instances stand; distribution remains {"induced":12,"protective":3,"platform":3,"knowledgeGap":1}. One nuance worth recording, not a reclassification: cadence-multiservice 20260521-084854 (turn 29) is the closest call — the import gate's wouldDestroy did protective surfacing (NEXTAUTH_URL) — but the surfacing was nullified by the construct chain (no non-destruc

### launch-export-classify
- exemplar: Agent at launch-production classify-prompt (guidance: "Classify each project env into one of four buckets ... before re-calling with envClassifications populated") submits correct envClassifications via action="classify" workflow="launch-production" and receives: {"error":"factType is required for action=classify","suggestion":"Pass factType=<one of gotcha_candidate, ig_item_candidate, verified_behavior, platform_observation, fix_applied, cross_codebase_contract>. The type comes from the fact record the writer sub-agent is classifying."} — the fact-record subsystem's handler answering a launch
- verifier (holds): I re-read three cited transcripts (20260515-145539/launch-production-dev-only, 20260604-071303/export-buildfromgit-self-snapshot, 20260513-133625/launch-production-from-standard-pair) and the handler/router code. I attempted four steelmen; all fail, and two collapse on code-level evidence the classifier didn't even cite.

LAUNCH VERB INSTANCES (7 of 10). Steelman 1 — "the stateless accumulator (re-call action=start with full inputs) is a deliberate design with compaction-recovery/idempotency rationale, so the classify/complete bounces are the agent ignoring the protocol." Refuted on timing and
- corrected: Unchanged: {"induced":10,"platform":4,"protective":3,"knowledgeGap":0}. No INDUCED instance reclassifies. Two precision notes, not reclassifications: (1) the three export instances are induced with partial mitigation — of the ~6 recovery turns, ~1 (the dev/stage pairing question) carries genuine decision content the construct rightly refuses to guess; the induced cost is the ~4-5 turns of bootstra

### bootstrap-start-and-misc
- exemplar: Agent: route=classic + full valid plan in one call. ZCP: "plan is not accepted in action=start; submit it via action=\"complete\" step=\"discover\" plan=[...]" / "Start commits the route only. The discover step is the reasoning space where the plan is produced from route-specific materials; commit it there." — two calls later the agent resubmits the BYTE-IDENTICAL plan (20260605-024523/greenfield-fullstack-multi-runtime). 8/8 runs across a month hit this same bounce; the model's natural move is route+plan together, and the rejection never reads the plan.
- verifier (refuted/narrowed): The 9-instance INDUCED set does not survive intact: 4 of 9 reclassify, including one resting on a factually wrong premise. The core construct critique DOES survive in narrowed form (5 instances), anchored by evidence no steelman explains away.

REFUTED instances (round-trip NOT manufactured by the construct):
(1) greenfield-node-postgres 20260503-114249 and (2) 20260503-125036 — I diffed the transcripts: both bounced plans carried empty bootstrapMode and no stageHostname. Empty mode defaults to standard (spec-workflows.md §2.3), and internal/workflow/validate.go:377-388 hard-fails standard mod
- corrected: {"induced":5,"platform":5,"knowledgeGap":7,"protective":1} — reclassify to knowledgeGap: greenfield-node-postgres-dev-stage 20260503-114249 (missing stageHostname → content-invalid under any design), greenfield-node-postgres-dev-stage 20260503-125036 (same defect), develop-add-managed-dep-to-existing 20260604-001223 (valkey@8 not in catalog → inevitable validation bounce), classic-go-simple 202606

## 63-friction tagging by root layer

**Totals:** {"construct": 17, "delivery": 13, "knowledge": 25, "platform": 1, "fixed": 6}

- F1 [KNOWLEDGE] — Adopt-discover atom's worked example promises 'plan derived for you' for the exact same-type pair the (by-design) handler refuses ~18x; fix 
- F2 [FIXED] — closeMode discoverability — report: ALREADY_FIXED via priority-1 DECISION heading + verify/deploy note; residual burial is F7's wall.
- F3 [DELIVERY] — zerops_knowledge/guidance verbosity drowns the dev-mode setup block in production content — R1 below-cap relevance, pure envelope size.
- F4 [KNOWLEDGE] — '502 on dev-mode = start dev_server, not a bug' fact lives only in post-mount CLAUDE.md; missing sentence on the verify/develop guidance sur
- F5 [CONSTRUCT] — outOfScope-vs-scope semantics (demote a pair-half, don't list it) are a counterintuitive model rule explained only after the call locks topo
- F6 [CONSTRUCT] — Fixing one yaml field after export validation-failed demands a full develop start->edit->close session ceremony (open: is the tree writable 
- F7 [DELIVERY] — develop-active response is a universal 300+-line wall — every runtime class/mode/caveat dumped, Next: buried at bottom; THE envelope finding
- F8 [CONSTRUCT] — Plan JSON for complete step=discover must be hand-authored and its simple/static shape guessed from examples — plan-schema authoring frictio
- F9 [KNOWLEDGE] — build.base != service type for implicit-webserver runtimes (php-apache needs php@X, static needs nodejs) — Zerops fact missing from develop-
- F10 [DELIVERY] — Deploy guidance omits the setup param though the workflow knows the zerops.yaml has multiple setups — server withholds info it already has.
- F11 [CONSTRUCT] — Recipe buildFromGit target is live-ACTIVE yet the workflow's stored deployed=false bool routes agents into first-deploy scaffolding — state 
- F12 [KNOWLEDGE] — Reconfirms F4: dev-mode idles on zsc noop so 502 reads as failure; the clarifying fact needs to land earlier than the mounted file.
- F13 [DELIVERY] — recipeNarrow is type:string with no enum so agents guess the token — the tool-contract surface under-communicates a value the server owns (d
- F14 [KNOWLEDGE] — Rust: stale target/ inherited from release buildFromGit SIGSEGVs the dev cargo run — recipe gotcha for the rust recipe, not an atom.
- F15 [FIXED] — Duplicate of F2 (closeMode discoverability) — ALREADY_FIXED per report; recurrence-despite-fix feeds F7.
- F16 [KNOWLEDGE] — Recipe zerops.yaml ships cross-deploy cherry-picked deployFiles that DM-2 rejects on self-deploy — recipe needs a dev/prod (self vs cross) b
- F17 [FIXED] — Two-phase bootstrap start (route-menu then commit) surprises everyone — report: ALREADY_FIXED, signal loud on 4 surfaces (kind field, Messag
- F18 [KNOWLEDGE] — Deploy guidance shows literal setup="prod" that won't match hostname-named setups — wrong example in guidance content.
- F19 [KNOWLEDGE] — run.start uses exec semantics, not shell (KEY=VAL prefix fails; needs env) — platform fact missing from develop-reserved-env-names atom; cos
- F20 [KNOWLEDGE] — Next.js dev OOMs and minRam guidance gives no number — recipe gotcha (dev needs >=2GB), nextjs recipe content.
- F21 [FIXED] — verify 'degraded' on root non-2xx is cosmetic but alarming — battery: detail text already says accept as cosmetic; guidance present.
- F22 [KNOWLEDGE] — Post-close MESSAGE says 'run zerops_verify' while the close-step guide says don't — two rendered-content surfaces contradict; fix carves the
- F23 [KNOWLEDGE] — Recipe simple-mode vs user's dev+stage intent never reconciled — discover guidance should state 'this recipe's simple mode IS the dev+stage 
- F24 [DELIVERY] — Same service displays OS-prefixed (alpine/php-nginx@8.4) in one surface and bare in another — cosmetic presentation inconsistency; validatio
- F25 [FIXED] — 'Next: deploy lies for code-only edits' — REFUTED by report: it's the auto-close GATE hint (no successful deploy recorded), agent misread; b
- F26 [CONSTRUCT] — Reconfirms F5: outOfScope must demote an already-in-scope pair-half, not exclude an unrelated service — model semantics mislead for dev-only
- F27 [KNOWLEDGE] — zerops_env guidance shows obsolete CLI pseudo-code (action=set key=X) vs the real project=true variables=[...] schema — hand-authored exampl
- F28 [DELIVERY] — attestation param's valid content is unspecified on the call surface — agent guesses what the contract accepts.
- F29 [CONSTRUCT] — SHARED-dep plan shape unclear — another face of the hand-authored plan-schema friction (F8/F32 cluster).
- F30 [CONSTRUCT] — Adopt plan must be authored blind: SSHFS mount is gated on provision-complete so the agent can't read zerops.yaml before committing the plan
- F31 [DELIVERY] — Cross-runtime HTTP internal-DNS literal exists in guidance but is buried — reachability, not absence.
- F32 [CONSTRUCT] — 'Add one managed dep to an existing service' has no plan path — the greenfield-shaped plan schema forces inferring isExisting/resolution/boo
- F33 [KNOWLEDGE] — build-integration-requires-git-push ordering (by-design dependency) is undiscoverable until the error — missing one upfront guidance line.
- F34 [CONSTRUCT] — build-integration defaults buildTarget to STAGE over the user's explicit 'build on appdev' with no discrepancy surfaced — convention-over-in
- F35 [CONSTRUCT] — Planless discover-complete advances, then provision deadlocks with misleading 'Start bootstrap first'; only reset escapes — step-gate bug, f
- F36 [DELIVERY] — git-push-setup probe swallows SSHExecError.Output into a catch-all (exit 128/GIT_TOKEN_INVALID) — the diagnostic exists but never reaches th
- F37 [CONSTRUCT] — No dashboard-only git-push path: the raw token MUST pass through git-push-setup (no probe+stamp-only mode) — design gap; user unwilling to p
- F38 [DELIVERY] — git-push-setup inputsRequired groups build-integration params as if they belong to the same call — response shape misleads on call boundarie
- F39 [KNOWLEDGE] — httpSupport/L7 routing is established only by a real deploy running the build pipeline — platform mechanic nothing on the verify-subdomain-r
- F41 [KNOWLEDGE] — F9 extended to static: run.base=nginx@1.22 != type 'static', build.base=nodejs@22 — same missing build/run-base fact, static flavor.
- F42 [DELIVERY] — Launch inputs accumulator registers existingProjectId/ProdToken on call 1 but echoes inputs:{} until scope completes — response misrepresent
- F43 [DELIVERY] — After 'launched', status/list can't say which pipeline was configured — recovery envelope lacks a pipelineSummary field (report fix: envelop
- F44 [KNOWLEDGE] — bootstrap-adopt is a hard precondition for launch-production but AGENTS.md routing never says so — missing routing note.
- F45 [KNOWLEDGE] — The 3-5-call source-control gate preamble is absent from the documented launch phase list — by-design gate, undocumented shape.
- F46 [KNOWLEDGE] — Source PAT (GIT_TOKEN) vs production CI/CD auth are two tokens never distinguished anywhere — missing explanation content.
- F47 [KNOWLEDGE] — build-integration targets the SOURCE side, not production — unstated fact that misreads as 'production pipeline broken' (sharpened by F61).
- F48 [DELIVERY] — source-control-required dumps the full blocker table while actual blockers sit in a small bottom array — launch-side wall-of-text (F7 siblin
- F49 [DELIVERY] — sourceContext promotionHeadline/targetServiceCanonical naming inversion — confusing response field naming.
- F50 [CONSTRUCT] — build-integration still warns 'ask user configure/skip' after the user already stated the preference — convention-over-stated-intent (META c
- F51 [FIXED] — Mounted .git can't push (it's git init, not a shallow clone) — report: ALREADY_FIXED, setup-git-push-container atom already warns; always de
- F52 [PLATFORM] — LaunchKey can create but not delete a project (token permission scoping) — Zerops platform behavior; ZCP can only warn teardown is dashboard
- F53 [KNOWLEDGE] — classify-prompt shows envClassifications as CLI syntax, not the MCP tool-call shape — same rendered-example drift as F27 (report bundles the
- F54 [KNOWLEDGE] — Export's compose-ready terminal (shipped in R5) was never added to the documented status table/axis allow-list/atoms — handler shipped a sta
- F55 [CONSTRUCT] — The OS-qualified types the adopt plan requires come only from discover output (catalog shows bare forms), so even a willing agent can't auth
- F56 [CONSTRUCT] — runtimeClass derives from the deployed zerops.yaml, so pre-deploy services misclassify as worker and mis-route dev-server triage — state-der
- F57 [KNOWLEDGE] — verify false-greens client-side-broken apps: nothing says NEXT_PUBLIC_*/browser-reachable URLs need the public subdomain, not internal DNS —
- F58 [CONSTRUCT] — Recipe route cannot expand bootstrapMode simple->standard; dev+stage on a simple recipe forces abandoning and redoing via classic — route ta
- F59 [CONSTRUCT] — 'dev' is overloaded across bootstrapMode pairing and dev-mode runtime supervision; the 'not durable' note persists post-deploy — model vocab
- F60 [CONSTRUCT] — Auto-close fires off a PRE-deploy verify while the deploy just killed the dev server (502) — the auto-close derivation counts a verify the s
- F61 [KNOWLEDGE] — build-integration status checks SOURCE git-push, reading as 'production webhook broken' — the source-vs-production distinction needs stating
- F62 [KNOWLEDGE] — zerops_knowledge has no php-nginx hello-world and falls back to php-apache — missing knowledge content.
- F63 [KNOWLEDGE] — Cross-deploy deployFiles cherry-pick has no template for non-build apps (plain PHP) — agent constructs the list blind and fails only at stag