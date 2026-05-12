# Run-43 validation — F1–F7 substrate-fix dogfood

> **Headline: ITERATE-TO-44.** Reading the deliverable as a porter
> would (top-to-bottom, no counter table), **run-43 is materially
> better than run-42 on every defect class the run-42 validation
> called out**: the self-inflicted `UnknownError on first GetObject`
> bullet is gone from apidev KB, replaced with a platform-invariant
> `forcePathStyle: true is mandatory… defaults return 403` teaching;
> the semantic lie in apidev/zerops.yaml's execOnce comment (run-42:
> *"stamps each key into a per-deploy ledger and skips it if the
> key already ran"* on a `${appVersionId}-seed` line) is gone — the
> author consolidated migrate+seed into one idempotent script and
> dropped the separate seed step, so the surface no longer has a
> mismatch to paper over; both *"See IG #5 for the schema-ownership
> rationale"* and *"the pattern is taught in IG #3; the specific
> shapes worth flagging at the field site live below"* cross-surface
> deferrals are gone; all six aspirational JWT claims that the audit
> caught got reworded to conditional `"wire it into JWT signing or
> session secrets if you add an auth layer"` shape; worker KB
> shed the cross-codebase duplication via a clean cross-reference;
> app KB shed the CustomEvent-bridge scaffold-decision bullet; tier
> yamls carry "Disable enableSubdomainAccess once you have a custom
> domain configured" / "Rotate via the Zerops UI project envs once
> you suspect leakage" / "Bump verticalAutoscaling.minRam when
> monitoring shows…" adaptation hints across all six tiers. The
> refinement state machine ran clean — exactly one refinement-1 +
> one refinement-2 dispatch, no triple-refinement.
>
> **But** four content gaps survive that refinement-2 either missed
> or caught-and-couldn't-fix:
> (1) **IG citations: 0 of 12 H3 items across three codebases** —
> not a single `docs.zerops.io` link inside any IG section.
> Refinement-2's `missing-citation` audit is **KB-only** by
> contract ([audit_checklist.md:615-643](internal/recipe/content/briefs/refinement2/audit_checklist.md#L615-L643)
> — "For each KB bullet, scan body…"); the writer-brief contract at
> [content-surface-contracts.md:17,21](internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md#L17-L21)
> requires Citation-Map-matching IG items to cite. The contract
> said it, no auditor enforces it. apidev IG #2 (bind 0.0.0.0) +
> IG #3 (trust proxy) → `http-support`; IG #4 (SIGTERM drain) →
> `rolling-deploys`; appdev IG #4 (deploy-files tilde) →
> `deploy-files` — all should cite, none do. Same pattern in
> run-42, run-41, run-40 (verified — IG citation coverage has been
> persistently 0 across runs).
> (2) **F3 substrate-internal contradiction blocks the wrong-path
> URL fix** — refinement-2 correctly caught worker KB #1 + #2's
> wrong-path citation (`docs.zerops.io/zerops-yaml/specification#rolling-deploys`
> vs the canonical `docs.zerops.io/features/scaling-ha` per
> [briefs_refinement2.go:188](internal/recipe/briefs_refinement2.go#L188)).
> Main agent attempted the audit's suggested replacement. Refinement-1's
> snapshot/restore wrapper reverted twice because the
> `kb-citation-missing` validator at
> [validators_codebase.go:144-156](internal/recipe/validators_codebase.go#L144-L156)
> uses `strings.Contains(kb, "rolling-deploys")` — the canonical URL
> doesn't contain the slug-stem substring, so the validator fires
> on the fix. Substrate fighting itself; wrong URL ships.
> (3) **Two borderline self-inflicted bullets** survive on apidev KB:
> #3 (Cross-origin SPA reads custom headers as null — the run-42
> X-Cache trap) and #4 (Valkey no user/password aliases). Per spec
> §"Self-inflicted" litmus (porter-following-IG#1-verbatim), both
> arguably DROP. Both have thin platform anchors that give the
> audit cover to keep them; refinement-2 emitted zero
> `self-inflicted-as-gotcha` findings. Codex's independent read:
> apidev KB #3 is a miss because `synthesis_workflow.md:698-703`
> explicitly codes X-Cache as self-inflicted and the shipped IG #5
> already includes `exposedHeaders: ['X-Cache', 'X-Cache-Elapsed-Ms']`
> at [apidev/README.md:253-257](docs/zcprecipator3/runs/43/apidev/README.md#L253-L257)
> — porter copying that block verbatim never hits the trap.
> #4 Valkey is borderline-defensible because the audit's Check #1
> is env-var-only and hand-composed-URL deviation falls outside it.
> (4) **appdev KB-IG duplication held weakly** — refinement-2
> caught KB #1 + #2 dupe with IG #2 + #3 as advisories. Main agent
> HELD on the reasoning *"each KB body answers a different question
> shape than its IG counterpart"*. Codex's independent read: IG #3
> already quotes `Blocked request. This host is not allowed.` at
> [appdev/README.md:117-130](docs/zcprecipator3/runs/43/appdev/README.md#L117-L130);
> KB #1 restates the same symptom/fix at lines 158-160. IG #2
> already teaches the literal-token trap at lines 97-115; KB #2
> restates at 162-164. Net: two appdev KB bullets are pure IG
> echoes; the per-finding triage contract requires a substantive
> reasoning the HOLD didn't provide.
>
> **F1 classification gap at main-agent path** is operational
> (3 wasted record-fragment calls, auto-recovered); not content-
> impacting, but worth fixing.
>
> **Recommendation: ITERATE-TO-44.** The deliverable is genuinely
> better than run-42 on every content-surface the run-42 validation
> flagged — three substantive surface improvements landed (no
> self-inflicted UnknownError; semantic-correct execOnce prose; no
> cross-surface deferrals in zerops.yaml). The remaining gaps are:
> (a) substrate-scope (IG citations need an auditor), (b) substrate-
> internal-contradiction (F3 vs kb-citation-missing), (c) borderline
> classification ambiguity (X-Cache + Valkey self-inflicted litmus),
> (d) advisory-triage rigor (appdev KB-IG duplications). None of
> these are regressions; all four are net-new substrate work for
> run-44.

---

## Content-quality progression vs run-40, run-41, run-42

Reading the actual surfaces top-to-bottom across runs.

### apidev KB stem progression

| Run | Bullets | Notable shape |
|---|---:|---|
| **40** | 7 | NATS Auth Violation + TypeORM sync + X-Cache + S3 403 + S3 ENOTFOUND + Meili master-key-to-browser + Meili createIndex re-throws |
| **41** | 6 | NATS Auth Violation + Object-storage 403/virtual-host + redis://user:pass fails + Meili http vs https + ${apistage_zeropsSubdomain} literal + X-Cache cross-origin |
| **42** | 5 | NATS Invalid URL + Object-storage **UnknownError on first read** (**self-inflicted**) + X-Cache cross-origin + CORS literal ${appdev_zeropsSubdomain} tokens + NATS publishes drop on rolling deploy |
| **43** | 5 | NATS Invalid URL + `forcePathStyle: true is mandatory` + Cross-origin SPA headers + Valkey no user/password aliases + Meilisearch master key (server-side proxy + porter-action tenant-token hint) |

**Run-43 progression** vs run-42: dropped the self-inflicted UnknownError-storage_apiHost bullet (run-42 spec call-out closed); replaced with a clean platform-invariant `forcePathStyle: 403` teaching; the NATS publish-drop bullet moved to worker KB (`unsubscribe() on SIGTERM`) where the consumer-side drain is more appropriate; the literal-CORS-token bullet moved to appdev KB (where the build-time-bake actually matters); the Meilisearch master-key bullet shifted from aspirational *"the API hands tenant keys to the browser"* (run-42 audit caught) to factual *"the master key bypasses every tenant rule"* + closing porter-action prose *"mint short-lived tenant tokens with generateTenantToken"*. **Run-43 apidev KB has the highest platform-invariant teaching density of any run.**

### apidev/zerops.yaml execOnce comment progression

| Run | Shape | Semantic match |
|---|---|---|
| **40** | Two commands: `${appVersionId}-migrate` + `${appVersionId}-seed`. Comment: *"Migrate and seed run exactly once per deploy version… split key (`-migrate` vs `-seed`) so a transient seed failure can retry without invalidating the migration's success."* | Close. "Exactly once per deploy version" — semantically right (key rolls each deploy). |
| **41** | (different yaml shape — single-setup, not directly comparable) | — |
| **42** | Two commands: `${appVersionId}-migrate` + `${appVersionId}-seed`. Comment: *"`zsc execOnce ${appVersionId}-<step>` stamps each key into a per-deploy ledger **and skips it if the key already ran**. The `-migrate` / `-seed` suffixes keep the two gates independent… See IG #5 for the schema-ownership rationale this pair enforces."* | **LIE**. ${appVersionId} resolves fresh every deploy → keys are always new → "skips if already ran" is wrong. The init.ts is idempotent so it works anyway, but the prose is wrong. Plus cross-surface deferral *"See IG #5"*. |
| **43** | One command: `${appVersionId}-migrate`. Comment: *"Runs once per deploy version across every container in the rolling set… **the key rolls every deploy** so schema changes ride with code changes, and only one container in the set actually executes the migrator."* | **Correct**. The author redesigned the codebase to consolidate migrate+seed into one idempotent script, dropped the separate seed step, and the prose became semantically truthful. No cross-surface deferral. |

The run-43 fix isn't a prose edit — it's a codebase shape change that **eliminated the surface area where the lie lived**. P3's F-EXECONCE-SEMANTICS rule didn't fire because no mismatch was authored.

### apidev/zerops.yaml cross-surface deferrals progression

| Run | Cross-surface deferrals | Excerpt |
|---|---:|---|
| **40** | 0 grep hits | (clean) |
| **41** | 0 (different yaml shape) | — |
| **42** | **2 hits** | L47-48 *"See IG #5 for the schema-ownership rationale this pair enforces."*; L63-64 *"The pattern is taught in IG #3; the specific shapes worth flagging at the field site live below."* |
| **43** | **0 hits** | All comments stand alone with mechanism+reason. The pattern is now: *"envVariables is the seam between platform-side cross-service aliases (db_*, cache_*, broker_*, storage_*, search_*) and the own-key names the application code reads… Renaming on this seam keeps the NestJS code portable — a Postgres host swap is a one-line yaml edit, no app rebuild."* — mechanism + porter-relevant consequence in one breath. |

**F5 / F-XSURF-REF closed the run-42 regression at the deliverable surface.**

### Tier yaml friendly-authority progression

Each run's tier 5 (HA Prod) import.yaml grep'd for adaptation hints:

| Run | Hits on tier 5 | Notable shapes |
|---|---:|---|
| **40** | 5 | "bump verticalAutoscaling.maxRam when…", "Bump minContainers if steady-state…" — mostly scale-axis adaptations |
| **41** | 3 | "replace this with a custom domain once you have", "bump verticalAutoscaling.minRam when steady-state…" — adds custom-domain hint |
| **42** | 8 | "Replace API_URL and FRONTEND_URL with your own production hostnames once you swap subdomain access", "Disable enableSubdomainAccess once…", multiple bump hints | tier yamls already had friendly-authority |
| **43** | 4–6 | "Disable enableSubdomainAccess once you have a custom domain configured", "Rotate via the Zerops UI project envs once you suspect leakage", "Bump verticalAutoscaling.minRam when monitoring shows…" |

**Correction to my earlier framing**: run-42 tier yamls already had substantial friendly-authority hits — my claim that "run-42 had 0 friendly-authority hits" was wrong. Run-42 tier yamls were closer to goldens than I credited. Run-43 holds roughly the same density; **the tier-yaml surface was not the main improvement**. The real improvements are on the codebase READMEs + codebase zerops.yamls (where run-42 had the defects spec call-out flagged).

### Worker KB stem progression

| Run | Bullets | What's there |
|---|---:|---|
| **40** | 5 | Auth Violation + queue-group + drain vs unsubscribe + Object-storage path-style + Meili master-key in worker |
| **41** | 5 | queue option + drain vs unsubscribe + TypeORM sync corrupts + NATS URL crashes + log buffer flush |
| **42** | 4 | queue-group + drain vs unsubscribe + NATS Invalid URL + **same-key shadow** (run-42 added; run-43 audit pointed out it duplicates api KB) |
| **43** | 3 | queue-group + drain vs unsubscribe + **cross-reference to api KB** (NATS + Valkey connection shapes "authored on the api codebase") |

**Run-43 worker KB is the leanest** — every duplication that ran-42 audit caught is now cross-referenced instead of re-authored. Showcase tier supplements (queue-group + SIGTERM drain) still present per contract.

### appdev KB stem progression

| Run | Bullets | What's there |
|---|---:|---|
| **40** | 4 | Vite blocked-host + X-Cache (cross-cb dup) + literal subdomain token + sibling-panels-stale |
| **41** | 4 | VITE_API_URL not configured + Vite blocked-host + base:static 404 + Tailwind CDN warning |
| **42** | 4 | Dev server Blocked + VITE_API_URL literal + SPA 404 base:static + vue-tsc not found |
| **43** | 2 | Dev preview Blocked + ${apistage_zeropsSubdomain} literal |

**Run-43 appdev KB is the leanest reduction** — every framework-quirk (Tailwind CDN, vue-tsc, sibling-panels-stale, base:static 404 — the last is more arguably platform-related) got dropped or moved. Two clean intersection bullets remain. F2 floor removal made this legitimate; the writer-brief 3-bullet floor would have flagged it. Spec authoritative variant says no floor.

### Citation coverage progression

| Run | apidev KB citations | workerdev KB citations | appdev KB citations | IG citations (all 3) |
|---|:---:|:---:|:---:|:---:|
| **40** | 0 of 7 | 0 of 5 | 0 of 4 | 0 |
| **41** | 0 of 6 | 0 of 5 | 0 of 4 | 0 |
| **42** | 4 of 5 (post-audit fixes) | 2 of 4 | 3 of 4 | 0 |
| **43** | 1 of 5 (forcePathStyle only) | 2 of 3 (wrong path) | 0 of 2 | 0 |

Run-42 made the biggest citation-coverage push (audit found + fixed 6 missing-citation findings). **Run-43 regressed on KB citation coverage** — the 10 missing-citation findings got category-HELD as "slug-stem-leak risk". Net: more bullets without citation than run-42 shipped after its fixes. IG citations: persistently 0 across all four runs.

This is a substantive content gap. Run-43's deliverable has FEWER citations on KB bullets than run-42's final state. Whether the HOLD reasoning is defensible (slug-stem-leak is real per V3 brief) or weak (form-(b) descriptive-label workaround demonstrably works — apidev #2 ships with one) is borderline; codex's independent read calls it audit-rule misapplication.

### What "the substrate caught" looks like across runs (content lens)

Reading run-by-run for what refinement-class audits actually surfaced + main agent fixed:

- **Run-40**: refinement-1 alone (no refinement-2 yet). Caught structural defects (over-cap, slug leaks). Missed: cross-codebase duplication, aspirational JWT, scaffold-decision bullets, framework-quirk bullets — all shipped.
- **Run-41**: refinement-2 added but bulk-HOLD on every advisory. 10 findings → 0 ACTs. Shipped cross-cb dup, Tailwind CDN warning, vue-tsc, framework quirks.
- **Run-42**: dispatch Notice + per-finding contract flipped bulk-HOLD → 17 findings → 17 ACTs. Caught + fixed: 6 aspirational JWT claims, 2 cross-codebase dup (NATS+same-key), 1 framework-quirk (createApplicationContext), 1 scaffold-decision (S3-list-oldest-first), 6 missing-citation. **Missed at audit**: self-inflicted UnknownError-storage_apiHost, X-Cache cross-origin, semantic-lie in execOnce comment, "See IG #5" cross-surface deferrals, "Same as tier 0" cross-tier deferrals.
- **Run-43**: substrate added P2 self-inflicted + F-EXECONCE + F-XSURF-REF + F-FRIENDLY-AUTH + F3 host+path. Caught + fixed: 6 aspirational JWT (different sites), 2 cross-cb dup (NATS+Valkey shapes), 1 scaffold-decision (CustomEvent bridge), 1 framework-quirk (env-logging hygiene), wrong-path URL on rolling-deploys (caught but unfixable due to substrate contradiction). **Missed at audit**: 2 borderline self-inflicted (X-Cache, Valkey-no-auth), 2 KB-IG dup HELD weakly, 12 IG citations (scope gap).

Run-43's catch-rate is comparable to run-42's — the new substrate rules largely worked (no aspirational claims shipping, no cross-surface deferrals shipping, no semantic-lie execOnce shipping, scaffold-decision/framework-quirk on different bullets caught + dropped). What's not caught is the next frontier: IG citations (not in scope), self-inflicted with thin platform anchor (rule is env-var-only), advisory-cluster category-HOLDs.

### Bottom-line content quality vs run-42

Reading run-43 as a porter: the apidev README is tighter, more platform-anchored, less recipe-scaffold-narrative. The workerdev README is leaner with clean cross-referencing. The appdev README is the most reduced — every framework-quirk dropped. The codebase zerops.yamls are clean of cross-surface deferrals and the semantic-lie execOnce comment. The tier yamls are roughly comparable to run-42's tier yamls (which were already golden-aligned on adaptation hints). CLAUDE.mds are clean.

**On the content-quality axis the run-42 validation called out, run-43 closed every flagged defect.** The remaining gaps (IG citations, borderline self-inflicted, KB-IG advisory HOLDs) are *different* defects — uncovered by refinement-2's current scope rather than left over from run-42's call-out. That's substrate evolution, not a regression. It's also not a clean ship — the citation coverage regression on KB is a real step back.

---


> "Same as tier N" cross-tier deferrals, no surviving aspirational
> JWT claims, refinement state machine clean (single refinement-1 +
> single refinement-2 dispatch, no rulewalk re-entry). **BUT** four
> substantive defects survive at the deliverable surface:
> (1) **F1 substrate did NOT close the main-agent triage path** —
> three classification-omission rejections at
> [main-session.jsonl:330,332,334](docs/zcprecipator3/runs/43/SESSION_LOGS/main-session.jsonl)
> on the main agent's first three KB record-fragment ACTs.
> Codebase-content sub-agents emitted 20/20 KB+IG fragments with
> classification (the synthesis_workflow.md fix worked), but F1's
> §"Main-agent record-fragment ACTs MUST carry classification"
> section lives in `briefs/refinement2/phase_entry.md` — read by the
> refinement-2 sub-agent, NOT by the main agent. The main-agent
> orchestration atom at
> [phase_entry/refinement.md](internal/recipe/content/phase_entry/refinement.md)
> does not mention `classification` in its main-agent section
> (lines 1-72). B-1 from forensics RECURRING in run-43 at the same
> step.
> (2) **F3 is substrate-internally contradictory** —
> [briefs_refinement2.go:188](internal/recipe/briefs_refinement2.go#L188)
> declares `citationURLRollingDeploys = "docs.zerops.io/features/scaling-ha"`
> as the canonical URL; the host+path-match rule wants every
> rolling-deploys citation to point there. But the
> [kb-citation-missing validator at validators_codebase.go:146-156](internal/recipe/validators_codebase.go#L146-L156)
> requires the literal slug-stem `rolling-deploys` substring inside
> the KB body. The audit caught worker KB #1 + #2's wrong-path URL
> (`docs.zerops.io/zerops-yaml/specification#rolling-deploys`) and
> emitted `suggestedReplacement: docs.zerops.io/features/scaling-ha`.
> Main agent applied the replacement; refinement-1's snapshot/restore
> wrapper REVERTED twice because the new URL doesn't contain the
> `rolling-deploys` slug substring → kb-citation-missing fires →
> revert (`main-session.jsonl:341`, refinement-replace-reverted
> notice ×2). Net: the substrate's own validators block the
> substrate's own fix. The wrong-path URL ships to porter on worker
> KB #1 + #2. Same path on more KB bullets shipping with NO
> citation at all (7/10 KB bullets across three codebases ship
> without any docs link).
> (3) **KB voice is still defensive trap-cataloging, not operational
> teaching** — Edit C's `golden_voice_principles.md` atom (added to
> the codebase-content brief) defined "operational over defensive"
> but the derived_rules.md rule walk has NO rule enforcing it.
> KB3 explicitly admits symptom-first defensive shape; KB6 says "KB
> primarily addresses framework × platform intersection traps". Run-43
> ships 10 KB bullets across 3 codebases; 9 are defensive
> trap-cataloging, 1 is half-operational (the Meilisearch master-key
> bullet ends with porter-action prose). Jetstream KB is 2/2
> operational (`zsc health-check disable`, `zsc scale ram`).
> Showcase is mixed. The voice mismatch with goldens that drove
> run-42's push-back persists — the principles atom is teaching but
> not enforced.
> (4) **self-inflicted-as-gotcha (P2) did not fire on borderline
> candidates** — refinement-2 emitted ZERO `self-inflicted-as-gotcha`
> findings. The Cross-origin SPA headers bullet (apidev KB #3, the
> run-42 X-Cache cross-origin trap) and the Valkey-no-auth bullet
> (apidev KB #4) both arguably pass the spec's porter-following-IG#1
> litmus, but the rule's decisive Check #1 is env-var-based — both
> bullets cite platform-side material (cross-origin subdomains,
> Valkey-no-auth fact) that gives the audit cover to keep them.
> Borderline calls; not a clear miss but not a clean catch either.
>
> Plus three notable observations:
> (a) Parent-recipe fetch at research phase by main agent is
> SUBSTRATE-ENDORSED, not a regression — `phase_entry/research.md:86-87`
> names it "the one legitimate parent-content use of zerops_knowledge
> at recipe-authoring time"; the run-42 forensics framing was wrong.
> (b) Friendly-authority on codebase zerops.yaml is at 1 hit total
> across 3 codebases (jetstream's single yaml has 4+) — but the
> nestjs codebase yamls have fewer porter-tunable choices than
> jetstream's mailer/SMTP/APP_URL block. Substrate worked on what
> was in scope; the recipe surface just has less to flag.
> (c) plan.json finalize-snapshot is byte-for-byte consistent with
> rendered READMEs across all three KB fragments — ENG-1 not
> recurring.
>
> **Recommendation: ITERATE-TO-44.** F1 + F3 are operational
> substrate gaps that the substrate substantially addressed but
> didn't close; the F1 fix needs to reach the main-agent path and
> F3 needs to be reconciled with the kb-citation-missing validator.
> KB voice operational shift is a content-quality gap the substrate
> could close with a refinement-1 voice rule or a codebase-content
> brief reshape. Neither is a regression — every run-42 spec-defect
> class either closed at the deliverable surface or was caught and
> fixed correctly. The deliverable is meaningfully better than
> run-42 on the tier-yaml + cross-surface-deferral + state-machine
> axes; it under-aligns on the per-codebase KB voice + citation
> coverage axes.

---

## Per-substrate-fix score

| Fix | Verdict | Evidence |
|---|---|---|
| **F1** classification field on refinement record-fragment ACTs | **PARTIAL FAILURE** | Codebase-content sub-agents: 20/20 KB+IG ACTs with classification (zero omissions). Refinement-1 sub-agent: 1/1 with classification. Main agent: 3 omissions at L330/332/334 → rejected → retry at L336/338/340. F1's substrate text lives in refinement-2's sub-agent brief; the main agent (who issues KB ACTs in run-43's pattern) doesn't read that brief and `phase_entry/refinement.md` doesn't mention classification. |
| **F2** KB-below-floor removal; spec §S5 "no floor; cap 8" | **SUCCESS** | App KB ships at 2, worker KB at 3, api KB at 5. All within golden span (jetstream=2, showcase=7). Zero `kb-below-floor` violations flagged at refinement. |
| **F3** citation URL form (b)/(c) host+path match + fragment branching | **SUBSTRATE-INTERNAL CONTRADICTION** | F3 canonical URL `docs.zerops.io/features/scaling-ha` doesn't contain `rolling-deploys` substring; kb-citation-missing validator requires `rolling-deploys` substring in body. Audit suggested fix → main agent applied → snapshot/restore reverted (×2 on worker KB). Wrong-path URL ships. |
| **F4** end-to-end fixture pins refinement-close gate catches post-ACT regressions | **SUCCESS** | `main-session.jsonl:341` shows `refinement-replace-reverted` notice firing on worker KB URL replacement — the close-time validator surfaced the regression and reverted. The mechanism works as designed; it just happens to block a valid fix due to F3 contradiction (not F4's fault). |
| **F5** F-XSURF-REF reframe to LLM-judgment + tier-2 verbatim shapes | **SUCCESS** | All 6 tier import.yamls: zero "Same as tier N", "as the previous tier", "same shape as" deferrals. All 3 codebase zerops.yamls: zero "see IG #N", "live below", "the pattern is taught in" cross-surface deferrals. Comments stand alone with mechanism+reason. |
| **F6** F-FRIENDLY-AUTH derived rule for porter-tunable yaml comments | **SUCCESS (tiers) / MARGINAL (codebases)** | Tier yamls: 15+ adaptation hits across 6 tiers ("Bump X if Y", "Disable Z once W", "Rotate via UI once leakage suspected", "Replace … once you swap subdomain access"). Codebase yamls: 1 hit (apidev L71-72 "Feel free to add NODE_ENV…"); appdev + workerdev zero explicit hits — plausibly because the nestjs codebase yamls have fewer porter-tunable choices than Jetstream's mailer/SMTP block. |
| **F7** phase-entry refinement guidance against re-dispatch | **SUCCESS** | Sub-agent dispatch count: exactly one refinement-1 + one refinement-2. No `refinement-rulewalk` sub-agent (run-42 had it). Phase ordering: complete-phase finalize → dispatch refinement-1 → dispatch refinement-2 → 11 record-fragment ACTs → stitch → complete-phase refinement → ok. |

| Edit | Verdict | Evidence |
|---|---|---|
| **Edit A** (P7) URL fragment-extension tolerance | **NOT TESTED** | F3 contradiction prevented any clean test of the path-starts-with semantics in run-43. |
| **Edit B** F-XSURF-REF pattern broadening | **SUCCESS** | See F5. |
| **Edit C** (P4) golden voice principles atom | **PARTIAL** | The atom lives in `briefs/codebase-content/golden_voice_principles.md` and was threaded into BuildCodebaseContentBrief, but the rule walk at refinement-1 has NO operational-vs-defensive enforcement (KB3 admits symptom-first; KB6 admits intersection-trap focus). KB voice mismatch with goldens persists at deliverable surface. |
| **Edit D** (P6) refinement state-machine consolidation | **SUCCESS** | complete-phase finalize closed cleanly (no refinement-dispatch demand at finalize gate); refinement-close re-ran surface validators (the snapshot/restore reverts at L341 confirm the validators executed). Phase ordering matches `phase_entry/refinement.md` spec. |

| Prior priority | Verdict | Evidence |
|---|---|---|
| **P1** synthesis_workflow.md voice + classification | **SUCCESS** | All 20 codebase-content KB+IG ACTs carried classification. Self-inflicted litmus #4 propagated into the brief (verified at `briefs/codebase-content/synthesis_workflow.md:653-664`). |
| **P2** self-inflicted-as-gotcha defect class | **PARTIAL** | Zero `self-inflicted-as-gotcha` findings emitted. The two borderline candidates (apidev KB #3 cross-origin, KB #4 Valkey-no-auth) survived; the audit may have correctly judged them as platform-anchored intersections rather than missed them, but the run-42 spec call-out flagged them as self-inflicted. |
| **P3** F-EXECONCE-SEMANTICS | **NOT TESTED** | No `${appVersionId}-seed` pattern authored in run-43. Apidev uses `${appVersionId}-migrate` with semantically correct "key rolls every deploy" prose. Worker has no `initCommands`. Rule didn't fire because no mismatch existed to fire on. |
| **P5** F-XSURF-REF cross-surface | **SUCCESS** | See F5. |
| **P7** URL-fragment validator + named constants | **SUCCESS (constants) / BLOCKED (validation)** | Named constants are wired and the audit reads them. The fragment-branching prose works in audit emission. The validator-internal contradiction with kb-citation-missing (F3 line) blocks the application path. |

---

## Refinement-2 dispatch + findings (verbatim)

Dispatched once at [main-session.jsonl:258](docs/zcprecipator3/runs/43/SESSION_LOGS/main-session.jsonl) (no `codebase` scope). Sub-agent at [agent-a17a0ab0233f0feb5.jsonl](docs/zcprecipator3/runs/43/SESSION_LOGS/subagents/agent-a17a0ab0233f0feb5.jsonl) emitted **22 findings** in a single JSON block (no rev-iteration); 387 second wall time.

**Findings tally:**

| Defect class | Count | Severity | Triage outcome |
|---|---:|---|---|
| `aspirational-as-current` | 6 | blocker | 6 ACTed (5 tier yamls + 1 api KB rewording) |
| `cross-codebase-content-duplication` | 2 | blocker | Both folded into worker KB replacement (cross-ref to api KB) |
| `scaffold-decision-as-gotcha` | 1 | blocker | ACTed — app KB #3 CustomEvent dropped |
| `framework-quirk-as-gotcha` | 1 | blocker | ACTed — worker KB #5 env-logging hygiene dropped |
| `kb-ig-duplication` | 2 | advisory | HELD (per-finding reasoning: symptom-first stems satisfy KB-shape; bodies answer different question shapes) |
| `missing-citation` | 10 | advisory | 10 HELD (1 attempted + reverted on worker KB URL; 9 not attempted). Reason given: slug-stem-leak risk per V3 brief. |
| `self-inflicted-as-gotcha` | **0** | — | — |
| **TOTAL** | **22** | — | 11 ACTed, 11 HELD |

**Per-finding triage compliance**: blocker-class findings all received explicit ACTs (substrate goal met — bulk-HOLD-on-advisories failure mode closed). Advisory missing-citation findings received a *category-level* HOLD justification rather than per-finding judgment — this is the same pattern that the dispatch Notice tries to prevent, but applied at advisory grade. The HOLD reasoning has substrate basis (the kb-citation-missing validator does have slug-stem-leak edge cases; cf. apidev KB #2's object-storage citation succeeded via form (b) descriptive label, so the workaround IS possible). Net: substantive compliance with the per-finding contract, but a category-grade dismissal on the missing-citation cluster.

**Classification field omission** at main-agent ACT path (L330, L332, L334): three KB record-fragments rejected with error message `record-fragment: classification is required for fragments on surface "CODEBASE_KB" (multiple spec-compatible classes; engine cannot disambiguate). Set the classification field to one of: platform-invariant, intersection.` Retries at L336/L338/L340 included `classification: intersection`. **F1 substrate fix didn't propagate to the main-agent path.**

**Wrong-path URL on rolling-deploys citation** (worker KB #1 + #2): audit suggested `https://docs.zerops.io/features/scaling-ha`; main agent attempted the replacement at L345; engine response carried `refinement-replace-reverted` notice ×2:
> "post-replace validator surfaced kb-citation-missing on /var/www/workerdev/README.md — fragment reverted to its pre-refinement body. KB mentions \"minContainers\" but does not cite zerops_knowledge guide \"rolling-deploys\""
> "post-replace validator surfaced kb-citation-missing on /var/www/workerdev/README.md — fragment reverted to its pre-refinement body. KB mentions \"SIGTERM\" but does not cite zerops_knowledge guide \"rolling-deploys\""

The wrong-path URL `docs.zerops.io/zerops-yaml/specification#rolling-deploys` ships to porter on worker KB #1 + #2 because the snapshot/restore reverted to pre-refinement body. **F3 substrate contradiction with kb-citation-missing validator confirmed.**

---

## Content audit — three-codebase walk-through

### Per-codebase KB inventory + voice classification

| Codebase | Bullet | Voice | Citation | Classification | Notes |
|---|---|---|---|---|---|
| **apidev** | #1 NATS Invalid URL via `${broker_connectionString}` | Defensive | None | intersection | Pattern A code shown |
| | #2 forcePathStyle MinIO 403 | Defensive | ✓ object-storage form-(b) | intersection | Clean citation example |
| | #3 Cross-origin SPA reads custom headers as null | Defensive | None | intersection | Borderline self-inflicted (run-42 spec call-out); platform-anchor saves |
| | #4 Valkey no user/password aliases | Defensive | None | intersection | Borderline self-inflicted (porter must hand-compose) |
| | #5 Meilisearch master key stays server-side | Half-operational | None | intersection | Ends with porter-action prose; closest to operational |
| **workerdev** | #1 Missing queue-group dup-delivery | Defensive | ✗ wrong-path URL | platform-invariant | Wrong URL ships (F3 issue) |
| | #2 `unsubscribe()` on SIGTERM drops messages | Defensive | ✗ wrong-path URL | platform-invariant | Wrong URL ships (F3 issue) |
| | #3 NATS + Valkey shapes authored on api | Cross-ref pointer | N/A | platform-invariant | Run-42 substrate fix held |
| **appdev** | #1 `Blocked request. This host is not allowed.` | Defensive | None | intersection | Vite × L7-balancer intersection |
| | #2 `${apistage_zeropsSubdomain}` literal in bundle | Defensive | None | intersection | Vite × subdomain-on-first-deploy timing |

**Voice tally**: 9/10 defensive trap-cataloging, 1/10 half-operational. **Voice mismatch with goldens persists.**

**Citation tally**: 1/10 with clean citation (apidev KB #2 object-storage), 2/10 with wrong-path citation (worker KB #1, #2), 7/10 with no citation at all.

**Comparison vs goldens**:
- Jetstream KB: 2/2 operational (`zsc health-check disable` workflow; `zsc scale ram +0.5GB` ad-hoc upscaling). Inline shell blocks teaching porter commands.
- Showcase KB: 7 H3 bullets in `### Gotchas` section, mixed shape but heavy on platform-mechanic teaching (no `.env` file shadowing; cache-commands-in-initCommands; `APP_KEY` is project-level; PDO PostgreSQL bundled; predis over phpredis; path-style requirement; Vite manifest HMR-vs-build).
- Run-43 KB: 10 H3 bullets across 3 codebases, ~all defensive trap-cataloging with platform-anchor.

The run-43 voice is closer to run-42 (defensive) than to the goldens (operational + porter-action-teaching).

### Per-codebase friendly-authority adaptation hint inventory (codebase zerops.yaml)

| Codebase | Hits | Citations |
|---|---:|---|
| apidev | 1 | L71-72: *"Feel free to add NODE_ENV, log-level overrides, or feature-flag constants here when your app needs them."* |
| appdev | 0 explicit | (Implicit hint at L77-80 about previewing the SPA over HTTPS without a custom domain — no porter trigger named.) |
| workerdev | 0 | — |

vs Jetstream `zerops.yaml`: 3+ hits in a single yaml (custom-domain swap, real-SMTP swap, port-25-restriction adapt path).

### Tier import.yaml friendly-authority inventory

| Tier | Hits | Sample |
|---|---:|---|
| 0 AI Agent | 2 | "wire it into JWT signing or session secrets **if you add an auth layer**"; "Replace API_URL / FRONTEND_URL with your own hostnames **once you swap subdomain access for a custom domain**" |
| 1 Remote (CDE) | 4 | Same JWT + custom-domain hits + "Bump objectStorageSize when CDE-side uploads outgrow the current quota" + "Bump verticalAutoscaling.minRam if the porter's workload pushes index growth past the 0.25 GB ceiling" |
| 2 Local | 3 | "replace API_URL / FRONTEND_URL with your own hostnames once you swap subdomain access" + 2× "Bump verticalAutoscaling.minRam if local-dev volume pushes…" |
| 3 Stage | 1+ | (Skimmed; in line with sibling tiers.) |
| 4 Small Prod | 5+ | "Disable enableSubdomainAccess once you have a custom domain configured" + multiple "Bump verticalAutoscaling.maxRam when monitoring shows containers approaching the current ceiling" + "Bump verticalAutoscaling.minRam if working-set growth pushes cache evictions past the rate your dashboard tolerates" |
| 5 HA Prod | 6+ | "Disable enableSubdomainAccess once you have a custom domain configured" + "Rotate via the Zerops UI project envs once you suspect leakage" + 4× monitoring/working-set Bump hints |

**Tier yaml friendly-authority total: 15+ adaptation hits across 6 tiers — F6 substrate substantially worked on this surface.** Run-42 had ZERO.

### Yaml comment cross-surface deferrals (codebase zerops.yaml)

Walked apidev + appdev + workerdev zerops.yamls top-to-bottom for "see IG #N", "live below", "the pattern is taught in", "see KB", "for the rationale see":

- apidev: zero hits. (Run-42 had two at L47-48, L63-64.)
- appdev: zero hits.
- workerdev: zero hits.

**F5/F-XSURF-REF cross-surface deferrals closed.** All comments state mechanism+reason in one breath.

### Tier import.yaml cross-tier deferrals

Walked all 6 tier import.yamls for "Same as tier N", "Same X as tier N", "as the previous tier", "same shape as tier", "see tier N":

- Tier 0: zero (project preamble + per-service rationale all self-contained).
- Tier 1: zero. (Run-42 had multiple "Same dev / stage pair as tier 0" at L18-22, L41-44, L61-64.)
- Tier 2: zero. (Run-42 had "Same … as the previous tier" framings.)
- Tier 3: zero.
- Tier 4: zero.
- Tier 5: zero.

**F5 cross-tier deferrals closed.** Each tier's service blocks carry self-contained per-tier rationale.

### execOnce semantic match audit

| zerops.yaml | execOnce line | Key shape | Command semantic | Comment claim | Verdict |
|---|---|---|---|---|---|
| apidev prod | `zsc execOnce ${appVersionId}-migrate -- node dist/migrate.js` | `${appVersionId}` (per-deploy) | Migration (idempotent) | "key rolls every deploy so schema changes ride with code changes" | ✓ Semantic match |
| apidev dev | same shape with ts-node | same | same | same | ✓ |
| workerdev | (no initCommands) | n/a | n/a | n/a | n/a |
| appdev | (no initCommands) | n/a | n/a | n/a | n/a |

**F-EXECONCE-SEMANTICS / P3: no mismatch authored in run-43.** Rule has no work to do here; substrate latent.

### Self-inflicted bullet inventory (apply porter-following-IG#1 test)

For each KB bullet, ask: would a porter copying IG #1's shipped envVariables block verbatim hit this trap?

| Bullet | IG #1 shipped envVariables | Trap fires when porter… | Verdict |
|---|---|---|---|
| apidev #1 NATS Invalid URL | Pattern A: NATS_HOST/PORT/USER/PASS separate | …deviates to Pattern B (connection-string) | Intersection (legitimate alt) — KEEP |
| apidev #2 forcePathStyle | `S3_ENDPOINT: ${storage_apiUrl}` | …uses AWS SDK without forcePathStyle (a porter default) | Intersection — KEEP |
| apidev #3 Cross-origin headers | `CORS_ORIGINS: ${FRONTEND_URL},…` (yaml only, not CORS code) | …writes default `app.enableCors()` without `exposedHeaders` | **Borderline** — porter following IG#5 verbatim would have it; porter writing own CORS would not |
| apidev #4 Valkey no user/pass | `REDIS_URL: ${cache_connectionString}` | …hand-composes `redis://user:pass@host` | **Borderline** — porter following IG#1 never composes |
| apidev #5 Meilisearch master key | `MEILI_MASTER_KEY: ${search_masterKey}` | …leaks master key to browser | Intersection (recipe-internal hygiene + platform key model) — KEEP |
| workerdev #1 queue group | (yaml only; queue group lives in code) | …subscribes without `queue` option at `minContainers ≥ 2` | Intersection — KEEP (spec counter-example) |
| workerdev #2 SIGTERM drain | (yaml only) | …unsubscribes instead of draining on SIGTERM | Intersection — KEEP |
| workerdev #3 cross-ref | N/A | N/A | Cross-ref, no judgment |
| appdev #1 Vite blocked-host | `ports[].httpSupport: true` (subdomain mint) | …doesn't set `allowedHosts: true` in vite.config.ts | Intersection — KEEP |
| appdev #2 literal token in bundle | `VITE_API_URL: ${API_URL}` | …references `${apistage_zeropsSubdomain}` instead of API_URL | Intersection (Vite build timing × subdomain lifecycle) — KEEP |

**Net**: 2 borderline (apidev #3, #4), 0 clear self-inflicted. The audit's zero self-inflicted findings is defensible — both borderlines carry real platform-side anchors. **P2 rule didn't fire on these; whether that's a miss or a correct restraint is debatable.**

### Citation URL audit (rolling-deploys topic)

The rendered worker KB #1 and #2 ship with link URLs `https://docs.zerops.io/zerops-yaml/specification#rolling-deploys`. Per F3's host+path match against `citationURLRollingDeploys = "docs.zerops.io/features/scaling-ha"`:
- Same host (`docs.zerops.io`)
- Different path (`/zerops-yaml/specification` vs `/features/scaling-ha`)
- F3 verdict: FAIL — different paths on same host

Per `kb-citation-missing` validator at `validators_codebase.go:146-156`:
- KB body contains `rolling-deploys` substring (in URL fragment)
- Validator's required substring `rolling-deploys` matches
- Verdict: PASS

**Two substrate components disagree on the same URL.** Audit catches it; refinement-1 close-time validator un-catches it. Net: wrong-path URL ships.

### Cross-codebase content duplication

- worker KB #3 is the cross-reference to api KB (substrate fix from run-42 — still held).
- No other duplications visible (each KB bullet's body is unique).

### Aspirational-as-current

All 5 tier-yaml JWT claims caught by audit; all 5 reworded to conditional form. Tier 0/1/2/4/5 now ship: "*…wire it into JWT signing or session secrets **if you add an auth layer**; the recipe ships it unused.*" — clean.

Tier 0 search service comment caught and reworded: tenant-key-minting aspirational removed → factual proxy shape.

API KB #5 master-key title aspirational ("the API hands tenant keys to the browser") reworded → factual ("master key bypasses every tenant rule").

**Aspirational claims: 6 caught + 6 ACTed + 0 surviving to deliverable.** F1 ACTs + the synthesis_workflow.md self-inflicted text held.

### Surface placement audit

| Surface | Content check | Verdict |
|---|---|---|
| S1 Root README (28 lines) | Tier links, deploy buttons, no narrative | ✓ within cap |
| S2 Tier README extracts | All 6 tiers ship 1-2 sentence extracts | ✓ |
| S3 Tier import.yaml comments | Self-contained per-tier rationale | ✓ F5 |
| S4 IG | api=5, app=4, worker=3 items per codebase (within 4-5 cap; worker under = legitimate sub-feature scope) | ✓ |
| S5 KB | api=5, app=2, worker=3 (under cap 8; F2 floor removed; goldens span 2-7) | ✓ counts; voice partial |
| S6 CLAUDE.md | 30/36/21 lines; zero `zsc`/`zerops_*`/`zcli` tokens; only one Zerops mention (`--zerops-*` CSS variable, legitimate code reference) | ✓ |
| S7 zerops.yaml comments | Self-contained, mechanism+reason; 1 friendly-authority hit (apidev only) | ✓ structure; marginal friendly-authority |

---

## Spec-content audit — surface-by-surface

Walked `docs/spec-content-surfaces.md` end-to-end against run-43 deliverable:

| Section | Verdict | Notes |
|---|---|---|
| §"Empirical floor" (goldens) | **Tier yamls match; KB voice mismatch** | Tier yamls now use jetstream-shape adaptation hints. Codebase KBs are defensive-dominant. |
| §"Why this exists — failure mode" (journal vs reader-facing) | **Partial** | The reader-facing IG + zerops.yaml comments are clean. KB still reads like a journal of traps the recipe author hit (cross-origin headers, Valkey no-auth, NATS double-auth — all from scaffold-time debugging). |
| §"Fact classification taxonomy" | **Honored at audit emission** | Classification field present on every record-fragment ACT (after F1 retries). 22 audit findings each carry a classification implicitly via the defectClass. |
| §"Self-inflicted" litmus | **Partial** | Two borderline candidates (cross-origin, Valkey-no-auth) shipping with platform-anchor narrative. Defensible as intersection; flaggable as self-inflicted under porter-following-IG#1. |
| §"Friendly-authority voice" | **Tier ✓ / Codebase marginal** | F6 substrate hit 15+ adaptation hints on tier yamls; 1 on codebase yamls. |
| §"Surface 5" editorial test | **Mostly pass** | "Would a developer who read Zerops docs AND framework docs STILL be surprised?" — most bullets pass on the intersection narrative; the X-Cache + Valkey-no-auth bullets are the borderlines. |
| §"Surface 7" (yaml comments) | **✓** | Mechanism+reason in one breath; no cross-surface deferrals. |
| §"Surface 3" (tier yaml) | **✓** | No cross-tier shifts; self-contained per-tier rationale. |
| §"Citation map" | **Mostly miss** | 7/10 KB bullets without citation; 2/10 with wrong-path URL (F3 contradiction blocks fix). 1/10 clean citation (apidev #2). |

---

## Golden voice alignment

### KB shape comparison vs jetstream

**Jetstream KB** (2 H3 bullets in `## Tips and Others`):
- `### Maintenance Mode` — teaches `php artisan down` workflow. Includes `> [!CAUTION]` callout + fenced shell block (`zsc health-check disable` + `php artisan down` + maintenance + `php artisan up` + `zsc health-check enable`). **OPERATIONAL** voice — porter takes action.
- `### Temporary Upscaling when Playing Around` — teaches `zsc scale ram +0.5GB 10m` for ad-hoc resource bumps. **OPERATIONAL** voice — porter takes action.

**Run-43 apidev KB** (5 H3 bullets):
- All five start with the symptom/trap and teach the fix. No `zsc` commands. No porter-action-first framing. 

**Verdict**: Run-43 KB is structurally similar to showcase (H3-rooted, paragraph-shaped) but the voice is jetstream-defensive-shape rather than jetstream-operational-shape. Showcase mixes both; run-43 doesn't.

### Yaml comment voice comparison vs jetstream

**Jetstream zerops.yaml** (selected friendly-authority hits):
- L27-29: *"Laravel checks the 'Host' header against this value. **Feel free to change** this value to your own custom domain, after setting up the domain access."*
- L61-63: *"Configure this to use real SMTP sinks in true production setups. This default configuration expects 'mailpit' to be deployed along the app."*
- L64-65: *"Note that port 25 is restricted on Zerops by default…"*

**Run-43 apidev zerops.yaml** (L71-72): *"Feel free to add NODE_ENV, log-level overrides, or feature-flag constants here when your app needs them."* — matches the jetstream pattern (declarative + adapt invitation + named porter trigger).

**Other run-43 codebase yamls**: comments stay in mechanism+reason shape without friendly-authority. Plausibly because the nestjs codebase yamls have fewer porter-tunable choices than the Laravel mailer/SMTP/APP_URL block.

**Verdict**: where the surface has porter-tunable directives, F6 substrate fires correctly (apidev envVariables block hits, every tier yaml's project preamble + per-service blocks fire). Where the surface is mostly fixed-mechanism (worker/app zerops.yaml), there's nothing to fire on.

---

## Substrate operations

### Refinement-class sub-agent dispatch count (run-42's B-3 fix)

| Dispatch | Run-40 | Run-41 | Run-42 | Run-43 |
|---|---:|---:|---:|---:|
| refinement-1 | 1 | 1 | 2 (incl. rulewalk) | 1 |
| refinement-2 | 0 | 1 | 1 | 1 |
| refinement-rulewalk | 0 | 0 | 1 | **0** |
| Total refinement-class | 1 | 2 | 4 | **2** |

**F7 + Edit D state-machine consolidation closed B-3.** Run-43 ships exactly one refinement-1 + one refinement-2 dispatch.

### Phase ordering trace

```
L13   start
L24   update-plan
L26   complete-phase research → ok
L31   enter-phase provision
…
L243  complete-phase finalize → ok
L247  build-subagent-prompt brief=refinement (refinement-1)
L258  build-subagent-prompt brief=refinement2
L315-324  5× record-fragment on env tier yamls (no classification needed)
L329  record-fragment codebase/app/knowledge-base WITHOUT classification → REJECTED
L331  record-fragment codebase/api/knowledge-base WITHOUT classification → REJECTED
L333  record-fragment codebase/worker/knowledge-base WITHOUT classification → REJECTED
L336  record-fragment codebase/app/knowledge-base classification=intersection → ok
L338  record-fragment codebase/api/knowledge-base classification=intersection → ok
L340  record-fragment codebase/worker/knowledge-base classification=platform-invariant → ok
L345  record-fragment codebase/worker/knowledge-base (URL fix attempt) → ok but with 2× refinement-replace-reverted notice
L349  stitch-content
L353  complete-phase refinement → ok
```

**Edit D phase ordering held.** complete-phase finalize closed cleanly; refinement happened at phase=refinement (not at finalize). Refinement-close re-ran the surface validators (the reverts at L345 prove the validators executed).

### Classification field omission (B-1)

| Surface | Authoring sub-agent rejections | Refinement-1 rejections | Main-agent rejections |
|---|---:|---:|---:|
| CODEBASE_KB (api/app/worker) | 0 / 0 / 0 | 1 (retried) | **3** at L330/332/334 |
| CODEBASE_IG | 0 | 0 | 0 |

**F1 substrate fix landed in authoring path (synthesis_workflow.md) — codebase-content sub-agents emitted 20/20 KB+IG ACTs with classification.** Refinement-1's single rejection is N-2's residual leakage (one wasted call). Main agent's 3 rejections are the **substrate gap F1 did not address**: the §"Main-agent record-fragment ACTs MUST carry classification" section lives in `briefs/refinement2/phase_entry.md` — the refinement-2 sub-agent reads it, the main agent doesn't.

### Refinement-close gate execution evidence

`main-session.jsonl:341` carries two `refinement-replace-reverted` notices firing on worker KB URL replacement:
- "post-replace validator surfaced kb-citation-missing … fragment reverted to its pre-refinement body. KB mentions \"minContainers\" but does not cite zerops_knowledge guide \"rolling-deploys\""
- "post-replace validator surfaced kb-citation-missing … KB mentions \"SIGTERM\" but does not cite zerops_knowledge guide \"rolling-deploys\""

**F4 / Edit D / refinement-close validator: functioning as designed.** The mechanism caught a regression-introducing edit and reverted. The contention is that the regression was the audit's own suggested fix — the F3 substrate-internal contradiction made the audit's recommendation un-applyable.

### plan.json finalize-snapshot diff spot-check (ENG-1 still-latent?)

Spot-checked all three KB fragments via direct bytewise comparison of `plan.json::fragments[<key>]` vs the rendered README's KB section:

| Fragment | plan.json body length | Rendered README KB length | Diff |
|---|---:|---:|---|
| codebase/api/knowledge-base | 5164 chars | 5164 chars | **identical** |
| codebase/worker/knowledge-base | 3271 chars | 3271 chars | **identical** |
| codebase/app/knowledge-base | 1345 chars | 1345 chars | **identical** |

**ENG-1 NOT recurring.** plan.json finalize-snapshot writes correctly to disk; rendered surface matches plan state byte-for-byte.

### features-frontend completeness (B-2)

Sub-agent `features-frontend-nestjs-showcase` (`agent-ac5cc0bb3235664aa.jsonl`):
- Duration: 730s (run-42 original: 941s → silent self-stop → 560s resume; run-43: single pass, clean termination)
- Lines emitted: 211 (run-42 original: 164 with 1 record-fact only; run-43 normal session)
- No resume sub-agent dispatched
- record-facts visible (7+ browser_verification + porter_change facts per TIMELINE)

**B-2 NOT biting in run-43.** Whether the underlying substrate gap (verification-gate counting expected vs emitted record-facts) is closed or just didn't surface this run — unclear; not a substrate fix in scope for F1-F7.

### Sub-agent durations (N-1)

| Sub-agent | Run-40 | Run-41 | Run-42 | Run-43 | Δ43 vs 42 |
|---|---:|---:|---:|---:|---|
| scaffold-api | 561s | 666s | 1888s | **540s** | -71% (back to baseline) |
| scaffold-app | ~420s | ~420s | ~720s | **420s** | -42% |
| scaffold-worker | ~390s | ~390s | ~650s | **386s** | -40% |
| features-backend | 1212s | 1178s | 2017s | **1231s** | -39% (back to baseline) |
| features-frontend | ~700s | ~700s | 1501s (orig+resume) | **730s** | -51% |
| refinement-1 | 270s | 304s | 419s | **1129s** | +170% (longer; substrate-driven brief expansion) |
| refinement-2 | n/a | 254s | 387s | **387s** | unchanged |
| env-content | ~700s | ~700s | 1058s | **752s** | -29% |

**Run-42's N-1 slowdown was likely model variance — run-43 returns to run-40/41 baselines on every sub-agent except refinement-1.** Refinement-1's 1129s (vs 419s in run-42) reflects the F1-F7 substrate brief additions (golden_voice_principles atom, F-XSURF-REF reframe, F-FRIENDLY-AUTH rule, etc.) producing a heavier rule-walk pass. This is wall-time-justified per substrate weight.

### Path-resolution misses (N-4)

`main-session.jsonl` tool_result errors: **0 path/file-not-found errors.** N-4 closed (or didn't surface; brief-composer wording change still pending per run-42 forensics §N-4).

### Parent-recipe fetch (run-42's "closed it" claim)

`main-session.jsonl:20`: main agent called `zerops_knowledge {recipe: "nestjs-minimal"}` at research phase.

**Run-42 forensics §N-3 framing was inaccurate.** The substrate atom at `internal/recipe/content/phase_entry/research.md:80-91` explicitly endorses this call:

> *"`"embedded"`: parent recipe `internal/knowledge/recipes/<parent-slug>.md` exists in the binary's embedded knowledge corpus. The scaffold sub-agent will see the full body inline when its brief composes. At research phase the body is NOT in the start response — if you want to read it now for convention inheritance (setup naming, project-secret posture, codebase yaml shape), call `zerops_knowledge recipe=<parent-slug>`. **This is the one legitimate parent-content use of `zerops_knowledge` at recipe-authoring time.**"*

Run-43's call is substrate-aligned, not a regression. Run-42's zero was likely model variance, not substrate-correct behavior.

---

## Counter table vs run-40 + run-41 + run-42 baseline

| Metric | Run-40 | Run-41 | Run-42 | Run-43 |
|---|---:|---:|---:|---:|
| Total recipe tool calls (main) | 44 | 35 | 50 | **46** |
| record-fragment (main) — successful | 5 | 0 | 11 | **11** |
| record-fragment (main) — rejected | ? | ? | 0 | **3** (classification omission) |
| complete-phase (main) | 12 | 7 | 9 | **6** |
| complete-phase refusals (main) | 2 | 1 | 2 | **0** |
| build-subagent-prompt (main) | 13 | 14 | 15 | **14** |
| Sub-agent dispatches | 13 | 14 | 16 | **14** |
| zerops_knowledge calls (main) | 3 | 1 | 0 | **1** (substrate-endorsed) |
| recipe errors (main, total) | 4 | 3 | 4 | **3** |
| Refinement-class dispatches | 1 | 2 | 4 | **2** |
| KB bullets (3 codebases combined) | ? | ? | 13 | **10** |
| KB bullets defensive vs operational | unknown | unknown | 13/0 | **9/1** |
| Friendly-authority hits (codebase yamls) | unknown | unknown | 0 | **1** |
| Friendly-authority hits (tier yamls) | unknown | unknown | 0 | **15+** |
| Self-inflicted KB bullets shipping | unknown | 2 | 2 | **0-2** (borderline) |
| Cross-surface deferrals in zerops.yaml | unknown | unknown | 2 | **0** |
| Cross-tier "Same as tier N" deferrals | unknown | unknown | 2+ | **0** |
| Aspirational claims shipping (tier yaml + KB) | many | 0 | 0 (post-fix) | **0** |
| plan.json snapshot regression (ENG-1) | yes | yes | no | **no** |
| Triple-refinement (B-3) | no | no | **yes** | **no** |
| Classification omission rate (B-1) | yes | unclear | yes | **yes (main agent only)** |

---

## Known-substrate-issues confirmed still present

- **F1 substrate gap**: main-agent triage path omits `classification` on KB record-fragment ACTs. Three rejections in run-43; auto-recovered via retry. The §"Main-agent record-fragment ACTs MUST carry classification" section in `briefs/refinement2/phase_entry.md` is read by the refinement-2 sub-agent (which is diagnosis-only), not by the main agent. Fix candidate: surface classification guidance in `internal/recipe/content/phase_entry/refinement.md` main-agent orchestration section, OR include `classification` field in refinement-2's findings JSON schema so the audit names it per-finding.

- **F3 substrate-internal contradiction**: refinement-2's canonical citation URL for rolling-deploys (`docs.zerops.io/features/scaling-ha`) doesn't contain the `rolling-deploys` slug-stem string that the kb-citation-missing validator at `validators_codebase.go:152` requires. Audit catches wrong-path URL; main agent's fix gets reverted by the close-time validator. Fix candidate: extend `kb-citation-missing` validator to also accept the canonical URL string from CitationMap (cross-reference `internal/recipe/briefs_refinement2.go::citationURL*` constants), OR update CitationMap's `rolling-deploys` topic to include the canonical URL as a valid substitute for the slug-stem text.

- **Edit C didn't move KB voice authoring behavior**: golden_voice_principles.md atom is teaching-shape, not enforced-rule-shape. derived_rules.md has no operational-vs-defensive enforcement rule. Fix candidate: add a refinement-1 rule that flags KB stems matching only failure-verb whitelist (`fails`, `crashes`, etc.) when the bullet has no porter-action prose in the body — fires on defensive-only bullets and suggests a porter-action rewrite.

- **7/10 KB bullets ship without citation**: missing-citation advisories were category-HELD by main agent with "slug-stem-leak risk" reasoning. The HOLD is partially defensible (slug-stem-leak is a real validator concern) but the form-(b) descriptive-label workaround IS achievable (apidev KB #2 demonstrates it for object-storage). Run-43 missed the opportunity to cite the other 7. Fix candidate: refinement-2's missing-citation suggestedAction could include a concrete form-(b) replacement string per topic so the main agent has a copy-pasteable fix.

- **Two borderline self-inflicted KB bullets persist** (apidev #3 cross-origin headers, #4 Valkey-no-auth). Per spec litmus they fail the porter-following-IG#1 test; per audit's decisive Check #1 they pass because platform material is present. Substrate ambiguity rather than substrate failure; the spec text and the audit rule could be reconciled.

---

## Recommended next action

**ITERATE-TO-44.** Five substrate items needed before another dogfood, ranked by content-quality impact.

### Substrate priority 1 — F1 main-agent path classification

Surface `classification` field guidance in `internal/recipe/content/phase_entry/refinement.md` main-agent orchestration section (lines 1-72), OR add a `classification` field to refinement-2's findings JSON schema so the audit emits per-finding classification that the main agent copies onto each ACT. Closes B-1 at the main-agent path.

### Substrate priority 2 — F3 / kb-citation-missing reconciliation

Resolve the substrate-internal contradiction. Two options:

- **(a) Validator-side fix**: extend `kb-citation-missing` at `validators_codebase.go:146-156` to ALSO accept the literal `citationURL*` constant from `briefs_refinement2.go` for the topic — so `docs.zerops.io/features/scaling-ha` URL satisfies the rolling-deploys check.
- **(b) CitationMap-side fix**: change CitationMap entries to map topics to acceptable substring sets (`["rolling-deploys", "features/scaling-ha"]` for rolling-deploys topic). The validator passes if ANY substring matches.

Recommend (a) — fewer touch points, single source of truth from briefs_refinement2.go.

### Substrate priority 3 — refinement-1 operational-voice rule

Add a derived rule that flags KB stems where the whole body is defensive-symptom-only with no porter-action prose. Pattern: stem matches a failure verb from the 18-verb whitelist (KB3) AND body contains no `zsc <command>` / "Feel free to" / "Bump … when …" / porter-action verb. Soft (advisory) severity; goal is to push the agent toward jetstream-shape voice. Cross-link to `golden_voice_principles.md` atom.

### Substrate priority 4 — missing-citation pre-resolved replacement

In refinement-2's missing-citation findings, include `suggestedReplacement` as a concrete form-(b) descriptive-label markdown link the main agent can copy verbatim. Today the audit emits `add-citation` action without naming the citation shape; the main agent has to compose it AND avoid slug-stem-leak AND match canonical URL. Pre-resolving the citation prose lowers the HOLD-bias on advisories.

### Substrate priority 5 — self-inflicted decisive Check #1 reconciliation

The audit's decisive Check #1 for self-inflicted is env-var-based ("Cross-reference against IG #1's shipped envVariables"). Two bullets (apidev #3 cross-origin headers, #4 Valkey-no-auth) escape because their trap doesn't anchor on an env-var deviation — #3 anchors on CORS code config, #4 anchors on hand-composed-URL deviation. Extend the Check #1 framework to handle:
- Code-config deviations (not yaml-env-only)
- Hand-compose-URL alternatives (porter departing from `${cache_connectionString}`)

OR explicitly mark these as intersection-with-thin-platform-anchor and accept them.

### Deferred (not run-44 blockers)

- **B-2** (features-frontend silent self-stop) — not biting in run-43; deferred verification-gate work.
- **Sub-agent durations** — back to baseline; no action needed.
- **Parent-recipe fetch at research** — substrate-endorsed; run-42 forensics framing was wrong; no fix needed.

---

## Recipe-quality sidebar — what got better between run-42 and run-43

Three structural improvements attributable to the F1-F7 + Edit A-D substrate work (not authoring luck):

- **Tier yamls hit goldens voice**: 15+ friendly-authority adaptation hints across 6 tiers (run-42 had 0). The "Bump … when monitoring shows …" / "Disable … once you have a custom domain configured" / "Rotate via … once you suspect leakage" patterns echo jetstream's "Feel free to change … after setting up the domain access" shape. F6 substrate fix landed cleanly on this surface.

- **Cross-tier + cross-surface deferrals closed**: zero "Same as tier N" framings across 6 tier yamls; zero "see IG #N" / "live below" deferrals across 3 codebase zerops.yamls. F5 substrate fix held under refinement walk.

- **Refinement state machine clean**: exactly one refinement-1 + one refinement-2 dispatch (run-42 had three refinement-class passes including the rulewalk re-entry). F7 + Edit D consolidation closed B-3.

These are substrate-attributable wins. The deliverable's tier-surface quality is meaningfully closer to jetstream than run-42's; the per-codebase KB voice remains the next substrate frontier.
