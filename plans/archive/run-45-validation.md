# Run-45 validation — three-pillar substrate dogfood

> **Headline: ITERATE-TO-46.** Reading run-45 as a porter against the
> goldens: **the three-pillar redesign did not move authoring behavior at
> the codebase surface where it most needed to**. Pillar A
> (brief-render verification) held at build time — substrate-pin tests
> green on v9.90.0 and the rendered refinement-2 brief in the run-45
> audit transcript carries the per-surface single-question tests
> verbatim. Pillar C (engine-side determinism) held on what got emitted
> — five `enrich-findings` calls, **zero classification rejections** at
> the main-agent ACT path (run-43: 3, run-44: 2), **zero
> refinement-replace-reverted** notices (run-44: 4) — but only on the
> 18 findings that DID get emitted. **Pillar B (per-surface
> single-question walk) failed at the audit-emit path on every codebase
> surface**: the refinement-2-audit sub-agent ran `ls -la
> /var/www/zcprecipator/nestjs-showcase/`, saw 6 tier dirs + `.briefs/`
> + `facts.jsonl` + `plan.json` + root `README.md` but NO `apidev/` /
> `appdev/` / `workerdev/` trees, concluded that *"S4/S5/S6/S7
> (codebase IG/KB/CLAUDE.md/zerops.yaml) surfaces have NO content
> materialized on disk yet"*, and emitted **18 findings ALL on S3 (tier
> import.yaml comments), zero on any codebase surface across api / app
> / worker**.
>
> The same outputRoot scope was given to refinement-1-rulewalk — but
> refinement-1 additionally ran `ls /var/www/` + `ls /var/www/apidev/
> /var/www/appdev/ /var/www/workerdev/`, found the codebase trees one
> directory up, and walked them. Refinement-2-audit did not do that
> extra exploration. Codex-validated (`a65c3fdba8889322b`): the
> dispatch-scope claim is partially correct — both dispatches use the
> same scope; the difference is that refinement-1's brief
> (derived_rules.md Y1-Y15) drives the sub-agent to seek the codebase
> trees while refinement-2's audit_checklist.md describes S4-S7 fragment
> IDs in the abstract without telling the sub-agent where on the live
> filesystem those fragments are stitched.
>
> Consequence at the deliverable: **two clear surface defects shipped
> that the Pillar B per-surface walk would have caught had it run**:
>
> 1. **apidev KB #6** — *"Cache-injected token resolves to `undefined`
>    on `@Inject`"*: NestJS-specific decorator-timing /
>    circular-import edge case. *"Putting a DI string token in the same
>    module file that imports the service consuming it
>    (`cache.module.ts` declaring `REDIS_CLIENT` while importing
>    `CacheService` and `CacheController`, both of which
>    `@Inject(REDIS_CLIENT)`) hits a circular-import edge case…"* —
>    cited verbatim. Per S5 single-question test (*"Would a developer
>    who read the framework docs STILL be surprised?"*): framework
>    docs cover NestJS DI cycles — DROP. Codex-validated CLAIM-HOLDS.
> 2. **apidev KB #7** — *"Search card shows zero documents after a
>    fresh deploy"*: *"A standalone seed script invoked from the
>    platform's per-deploy init hook runs outside the NestJS
>    application context… the api side handles the integration gap by
>    re-pushing every existing row into the search index inside
>    `ItemsService.onModuleInit`."* — names `SearchIndexer`,
>    `ItemsService.onModuleInit`, "Search card", "Items card". Per S5
>    self-referential prohibition + spec litmus #4 (*"Could this be
>    summarized as 'our code did X, we fixed it to do Y'?"*): the
>    recipe ships both the seed.js script AND the in-app
>    SearchIndexer; a porter who replaced both with their own design
>    would not have this trap. DROP / MOVE-TO-S6. Codex-validated
>    CLAIM-HOLDS.
>
> Plus one borderline survivor — **apidev KB #5** (cross-origin headers
> return undefined) — the stem rephrased to symptom-first (*"Cross-origin
> custom response headers return `undefined`"*) vs run-44's recipe-internal
> *"Cache demo silently returns null for X-Cache from the SPA"*, but the
> body still names *"The cache demo's `X-Cache`, `X-Cache-Elapsed-Ms`,
> and `X-Cache-Key` headers"*. Stem improved; body did not.
>
> Plus one **substrate-attributable regression on the run-44 surface
> win**: codebase yaml friendly-authority adapt-path hits dropped from
> ~7 (run-44, distributed across all three codebases) to **~2** (run-45:
> appdev L57-58 `"Feel free to point this at your own custom domain
> once you've set one up for the api"`; apidev L21 `"npm callable at
> runtime if you ever shell in"`; workerdev zero). F6 / F-FRIENDLY-AUTH
> pattern that ran-44 author broadened did not carry forward to run-45
> author. Pillar A pins brief-rendered text but not authoring
> patterns downstream.
>
> Three substrate-attributable wins (real, non-trivial):
>
> (i) **Zero classification rejections + zero snapshot/restore reverts at
> the main-agent ACT path** — Pillar C engine-side enrichment
> propagated cleanly on the 18 S3 findings. Run-44 had 2 classification
> rejections + 4 reverts; run-43 had 3 + lower; run-45 has 0 + 0.
> Pillar C closes the run-40 → run-44 "main agent retries with the
> missing field" cycle on emitted findings.
>
> (ii) **Refinement-2-audit found genuine cross-surface duplication on
> tier yamls + the Valkey HA platform-routing fabrication**. The
> Valkey HA *"the platform forwards the standard 6379 port on the
> replica nodes to the master, so the api + worker client code does
> not need to learn HA-specific hostnames"* claim at tier 5 has no
> backing fact and is the exact "fabrications past runs hit" class the
> env-content brief part-3 warns about. Audit caught it, triage
> rewrote. Substrate per-surface walk works WHEN the sub-agent can
> see the surface.
>
> (iii) **No aspirational JWT claims, no execOnce semantic mismatches**.
> Run-44 author re-introduced 3 aspirational JWT claims and audit
> caught all 3; run-45 author held those closed at authoring time.
> Within-stochastic-floor improvement, not Pillar-attributable.
>
> **The headline test moved barely**: IG citation coverage 0/10 (runs
> 40-44) → **1/10** in run-45, and that one is a **wrong-URL
> form-(b)** citation: apidev IG #4 SIGTERM-drain section cites
> `[zero-downtime deploys with multi-container setups](https://docs.zerops.io/zerops-yaml/specification#minrunningcontainers-)`
> — the display text is form-(b) friendly for `rolling-deploys` but
> the canonical URL for that topic is
> `docs.zerops.io/features/scaling-ha`, not
> `docs.zerops.io/zerops-yaml/specification`. G1's anchored host+path
> matcher accepts it as init-commands' canonical path; the deliverable
> ships a citation that mislabels the doc. Substrate progress on the
> IG citation axis exists in principle but not in this run's surface.
>
> **Recommendation: ITERATE-TO-46.** Pillar B needs a fix to the
> dispatch path discovery — either the dispatch prompt explicitly
> tells the sub-agent the codebase trees live at
> `/var/www/<host>{dev,stage}/` (the SSHFS-mount path the engine wrote
> codebase content to), or the audit checklist's "for each surface,
> for each item" walk gates on a directory listing the sub-agent
> proves it walked. Pre-flight check: refuse `complete-phase
> phase=refinement` if `Refinement2Dispatched=true` AND the audit
> sub-agent emitted zero findings for S4/S5/S6/S7 (a sub-agent that
> emits "I couldn't find the codebase trees" is a substrate-detectable
> failure mode, not an audit conclusion).

---

## Per-pillar score

| Pillar | Verdict | Surface evidence |
|---|---|---|
| **A** brief-render verification | **HELD AT BUILD TIME, PROPAGATED TO BRIEF** | Substrate pin tests pass on v9.90.0 (`TestBriefsRendered_SubstrateOK`). Rendered refinement-2 dispatch prompt (line 1 of `agent-aaf3644b4a4c7083b.jsonl`) carries the five per-surface single-question tests verbatim, the failure-mode triple `DROP / MOVE-TO-Sn / REWRITE`, the citation-map families, the cross-surface uniqueness pass description. G5/G7 silent-drop shape impossible — confirmed at build + at dispatch surface. Pillar A is no longer a substrate frontier. |
| **B** per-surface single-question walk | **FAILED AT AUDIT-EMIT PATH ON CODEBASE SURFACES** | 18 findings emitted; **ALL on S3 tier import.yaml comments**. ZERO findings on S4 IG / S5 KB / S6 CLAUDE.md / S7 zerops.yaml across api, app, worker. Sub-agent explicitly said *"S4/S5/S6/S7 (codebase IG/KB/CLAUDE.md/zerops.yaml) surfaces have NO content materialized on disk yet — only S3 (tier import.yaml comments) and tier READMEs are present"* and ran ONLY `ls -la /var/www/zcprecipator/nestjs-showcase/` (which excludes the codebase trees at `/var/www/{apidev,appdev,workerdev}/`). Refinement-1-rulewalk did the extra exploration and walked codebases. Refinement-2-audit did not. Per-surface walk that closes Pillar B's intent did not occur on any codebase surface. |
| **C** engine-side determinism | **HELD ON EMITTED FINDINGS** | 5 `enrich-findings` MCP calls visible in main-session.jsonl (5 batches × ~4 findings/batch ≈ 18 + 1 second-pass). Zero classification rejections at main-agent ACT path (run-44 had 2 at L273/275; run-43 had 3). Zero `refinement-replace-reverted` notices (run-44 had 4 wrong-URL reverts on apidev KB). 19 fragment replacements applied by the triage sub-agent without a single classification compatibility failure. **The DISCARD-override sub-mechanism never fired**: facts.jsonl carries 9 intersection / 23 platform-invariant / 28 scaffold-decision facts — ZERO framework-quirk, self-inflicted, or library-metadata candidateClass values. Upstream codebase-content sub-agents never classified any fact as DISCARD-class, so the engine had no DISCARD-override case to test. Pillar C's classification + suggestedReplacement pre-fill path is verified-in-production; the DISCARD-override path is unverified-in-production. |

---

## Refinement-2 audit + triage — verbatim findings inventory

The refinement-2-audit sub-agent (`agent-aaf3644b4a4c7083b.jsonl`,
328s) emitted ONE fenced JSON block with 18 findings, every one
carrying `surface: "S3"`. Triage (separate sub-agent dispatch,
`agent-a21f2204968191b32.jsonl`, 316s, 56 tool uses) consumed the
enriched findings and applied 19 fragment replacements.

### Findings distribution (audit emission)

| Surface | Count | Notes |
|---|---:|---|
| S3 (tier import.yaml comments) | **18** | All 18 findings here |
| S4 (codebase IG) | **0** | Sub-agent did not see trees |
| S5 (codebase KB) | **0** | Sub-agent did not see trees |
| S6 (codebase CLAUDE.md) | **0** | Sub-agent did not see trees |
| S7 (codebase zerops.yaml) | **0** | Sub-agent did not see trees |
| **TOTAL** | **18** | — |

### Failure-mode distribution (audit emission)

| Mode | Count |
|---|---:|
| REWRITE | 12 |
| MOVE-TO-S5 (target = codebase KB the sub-agent never walked) | 5 |
| MOVE-TO-S7 | 1 |
| DROP | 0 |

Note the **5 MOVE-TO-S5 findings**: each says "this teaching belongs on
the codebase KB". But the audit never walked codebase KBs to check
whether the teaching ALREADY lives there. Triage ACTed on these as
"strip from tier yaml" without verifying the canonical home actually
carries the cross-referenced teaching. End state: drain() rolling-deploy
teaching DOES live on worker KB #2; VITE_API_URL build-time bake DOES
live on appdev KB #2; 0.0.0.0 binding DOES live on apidev IG #2. The
MOVE-TO-S5 ACTs lucked into correct outcomes because the codebase
authoring sub-agents had already authored the canonical home — not
because Pillar B verified it.

### Triage decisions (verbatim from main-session.jsonl)

| Finding | Severity | Triage |
|---|---|---|
| 1 — priority rationale duplicate (top-of-file + before db) | advisory | ACT (broke TY5 validator at first close — re-record needed) |
| 2 — 0.0.0.0 binding narration in tier 0/1/2 api blocks | advisory | ACT (strip; canonical home apidev KB exists) |
| 3 — VITE_API_URL build-time bake across 5 tier app blocks | **blocker** | ACT (strip; canonical appdev KB #2 exists) |
| 4 — "No HTTP port" deliberate-absence on tier-0 worker | advisory | ACT (MOVE-TO-S7) |
| 5 — Worker drain() teaching across tiers 4 + 5 | **blocker** | ACT (strip; worker KB #2 exists) |
| 6 — At-most-once managed-NATS claim on tiers 4 + 5 | advisory | ACT |
| 7 — echo-workers queue group repeated across 5 tiers | advisory | ACT (keep tier-4 anchor only) |
| 8 — Presigned-URL flow narration on tiers 0 + 4 | advisory | ACT |
| 9 — Valkey HA platform-routing fabrication on tier 5 | **blocker** | ACT (rewrite to porter-observable shape) |
| 10 — Tier 5 db block leads with HA topology not role | advisory | ACT |
| 11 — search empty-fallback behavior in tiers 4 + 5 | advisory | ACT |
| 12 — Tier 2 broker "exactly-once-per-group" conflicts w/ at-most-once at 4/5 | advisory | ACT |
| 13 — Tier 0 storage opens "Provision" + field-restatement | advisory | ACT |
| 14 — Tier 5 storage "not a recipe-tunable knob" awkward | advisory | ACT |
| 15 — Tier 0 service blocks use "deployed twice" meta-narrative | advisory | **ACCEPT** (frames role-leading per tier-0 dev+stage shape) |
| 16 — `corePackage: SERIOUS` field-site comment missing | **blocker** | ACT |
| 17 — Project envVariables URL constants lack rationale | advisory | **HOLD** (would breach 3-5 line project-block cap) |
| 18 — Tier 1 worker "processed once" collides with at-most-once | advisory | ACT |

**Net at deliverable**: 16 ACT + 1 ACCEPT + 1 HOLD. No bulk-HOLD
failure pattern. Per-finding triage discipline holds.

**One genuine substantive catch**: finding #9 — Valkey HA
platform-routing claim was unbacked-fabrication territory and audit
caught it without a backing fact. Same shape as run-44's aspirational
JWT regression catch, but on a different fact-family.

**One substrate-induced re-record cycle at close**: triage stripped the
TY5 canonical *"Set higher priority for databases and storages…"*
block too aggressively (finding #1 ACT). First `complete-phase
phase=refinement` returned 6 `missing-priority-justification-block`
violations; main agent re-recorded `env/N/import-comments/db` for
N=0..5 prepending the canonical 2-line block; re-stitched; second
close ok. **Triage scope was wider than the TY5 validator expected** —
same shape as run-44's wrong-path-URL retries, different failure
class. Pillar C's engine-determinism doesn't cover surface-validator
compatibility checks on triage replacements. **Worth flagging as a
substrate frontier**: triage ACTs could be wrapped in the same
snapshot/restore primitive as refinement-1's record-fragment calls.

---

## Content audit — three-codebase walk-through

### Per-codebase KB inventory + spec-test classification

| Codebase | Bullet | S5 test verdict | Citation form | Self-referential? |
|---|---|---|---|---|
| **apidev** (7 bullets) | #1 NATS `Authorization Violation` | **PASS** (intersection: nats@2 URL parsing × managed broker auth) | NONE (managed-services-nats topic match) | No |
| | #2 Object-storage `PutObject` DNS errors | **PASS** (intersection: AWS SDK default × MinIO path-style) | ✓ form-(b) `[S3-compatible storage running on the MinIO backend](https://docs.zerops.io/services/object-storage)` | No |
| | #3 Meilisearch TLS handshake | **PASS** (intersection: managed service internal port × L7 termination edge) | NONE (managed-services-meilisearch topic match) | No |
| | #4 Valkey `NOAUTH` on every reply | **PASS** (intersection: managed Valkey unauthenticated × client AUTH default) | NONE (managed-services-valkey topic match) | No |
| | #5 Cross-origin custom headers `undefined` | **BORDERLINE** — stem rephrased to symptom-first (improvement vs run-44 "Cache demo silently…"), body still names "The cache demo's X-Cache, X-Cache-Elapsed-Ms, X-Cache-Key headers" — recipe-internal feature reference in the body. Per spec §S5 self-referential prohibition: rewrite body to drop "the cache demo's" framing, keep the CORS+exposedHeaders teaching. | NONE | **Partial** — stem clean, body names "cache demo" |
| | #6 Cache-injected token `@Inject` undefined | **FAIL — framework-quirk** | NONE | **YES** — names `cache.module.ts`, `CacheService`, `CacheController`, `REDIS_CLIENT`, `cache.tokens.ts`. Framework-quirk per S5 ("framework docs cover NestJS DI cycles → DROP"). Codex-validated. |
| | #7 Search card zero documents | **FAIL — self-inflicted + self-referential** | ✓ form-(c) bare URL `[per-deploy init-step gate](https://docs.zerops.io/zerops-yaml/specification#initcommands-)` (init-commands canonical) | **YES** — names `SearchIndexer`, `ItemsService.onModuleInit`, "Search card", "Items card". Per spec litmus #4: porter without seed.js + in-app SearchIndexer wouldn't hit this. Codex-validated. |
| **workerdev** (5 bullets) | #1 Missing `queue:` option crashes exactly-once | **PASS** (intersection: NATS core pub/sub × multi-replica) | ✓ form-(b) `[zero-downtime deploys with multi-container setups](https://docs.zerops.io/features/scaling-ha#high-availability)` | No |
| | #2 `subscription.unsubscribe()` on SIGTERM drops | **PASS** (intersection: NATS client unsubscribe semantics × rolling deploy SIGTERM) | ✓ form-(b) `[zero-downtime deploys with multi-container setups](https://docs.zerops.io/features/scaling-ha#high-availability)` | No |
| | #3 `Authorization Violation` URL-embedded creds | **PASS** (intersection: nats@2 URL parsing × managed broker) | NONE (managed-services-nats topic match) | No |
| | #4 Re-declaring `APP_SECRET: ${APP_SECRET}` corrupts | **PASS** (intersection: per-service write timing × project-level inject) | NONE (env-var-model topic match) | No |
| | #5 `https://` Meilisearch breaks in-cluster handshake | **PASS** (intersection: L7 TLS termination × managed service internal port) | NONE (managed-services-meilisearch topic match) | No |
| **appdev** (2 bullets) | #1 Prod bundle returns null where X-Cache should be | **PASS** (intersection: CORS expose-headers × cross-origin SPA-API subdomain shape) | NONE | **Partial** — names "Cache panel" |
| | #2 Prod bundle ships with literal `${apistage_zeropsSubdomain}` | **PASS** (intersection: Vite build-time inline × peer subdomain mint timing) | NONE (env-var-model topic match) | No |

**Voice tally** (run-45):
- **Operational**: 0 bullets
- **Intersection (clean)**: 9 bullets
- **Borderline (intersection with recipe-internal naming creep)**: 2
- **Framework-quirk that should be DROPPED**: 1 (apidev KB #6)
- **Self-inflicted that should be DROPPED**: 1 (apidev KB #7)

**Comparison vs goldens**:
- Jetstream KB (2 bullets, all operational): does NOT match.
- Showcase KB (7 bullets, all symptom-first defensive intersections, zero
  self-referential): partially matches — run-45 apidev KB has 5/7
  clean intersections matching the showcase shape, plus 2 that
  violate spec §S5.

**Comparison vs run-44 voice**:
- Run-44 apidev KB had 4 bullets, 2 borderline (Cache demo, tryGetClient).
- Run-45 apidev KB has 7 bullets, 2 hard-fail (KB #6, KB #7), 1 borderline.
- Stem-quality on apidev #5 IMPROVED (symptom-first stem replacing
  run-44's recipe-internal "Cache demo silently returns null…").
- New bullets KB #6 + KB #7 are NEW recipe-internal naming + new
  framework-quirk shipping — REGRESSIONS introduced by run-45 author.

### Per-codebase friendly-authority adapt-path inventory (codebase yamls)

| Codebase | Run-44 hits | Run-45 hits | Run-45 evidence |
|---|---:|---:|---|
| apidev | 2 | **1** | L21: "npm callable at runtime if you ever shell in" (degraded — adapt-path framing is weak; the comment is informational, not invitational) |
| appdev | 4 | **1** | L57-58: "Feel free to point this at your own custom domain once you've set one up for the api" (clean — matches jetstream's `APP_URL` adapt-path shape) |
| workerdev | 1 | **0** | — |

**Total: ~2 friendly-authority hits across 3 codebase yamls in run-45**
(run-44 had ~7). **Material regression on the run-44 substrate's
biggest surface win** — F6 / F-FRIENDLY-AUTH pattern that run-44 author
distributed across api + worker + app codebase yamls did not carry to
run-45 author. Pillar A pins the *substrate text* but not authoring
patterns the substrate aimed to broaden.

### Tier import.yaml friendly-authority inventory

Triage applied 19 fragment replacements that included friendly-authority
adapt-paths in the rewritten tier yaml comments (verified via the
post-replace deliverable). Did NOT pull verbatim numbers per-tier as
the triage was substantive and the tier yamls have been rewritten
multiple times against the run-44 baseline.

**Spot-check via verbatim grep**:

```
grep -ic "bump.*if\|bump.*when\|disable.*once\|rotate.*for" \
  environments/{0..5}/import.yaml
```

| Tier | Adapt-path hits |
|---|---:|
| 0 AI Agent | 3 |
| 1 Remote (CDE) | 2 |
| 2 Local | 4 |
| 3 Stage | 4 |
| 4 Small Production | 5 |
| 5 HA Production | 6 |

**Tier yaml friendly-authority total: 24 adapt-path hits across 6
tiers** (run-44: 18; run-43: 15+). **Material improvement** —
triage broadened the per-tier porter-tunable rationale, particularly
on tier 5 where the Valkey HA fabrication-rewrite added "bump
verticalAutoscaling.minRam if working set grows" porter-observable
phrasing.

### Yaml comment cross-surface deferral check (codebase yamls)

```
grep -nE 'see IG|see KB|see CLAUDE|see env|the pattern is taught' \
  {apidev,appdev,workerdev}/zerops.yaml
```

Zero matches. **F-XSURF-REF held — codebase yamls carry mechanism + reason in one breath, no "see IG #N" references**.

### Tier import.yaml cross-tier deferral check

```
grep -ncE 'see tier|same as tier|same shape as|like tier|promote to' \
  environments/{0..5}/import.yaml
```

Zero matches. **F5 cross-tier deferrals stay closed**. Tier 3 README
keeps the comparative *"Stage environment uses the same shape as
production"* preamble — positively comparative, not cross-tier
deferral (matches jetstream pattern).

### execOnce semantic-match audit

| Yaml | execOnce line | Key shape | Comment claim | Verdict |
|---|---|---|---|---|
| apidev prod | `zsc execOnce ${appVersionId}-migrate --retryUntilSuccessful -- node dist/migrate.js` + `…-seed` | `${appVersionId}` (per-deploy) | "per-deploy because `${appVersionId}` resolves to a fresh string on every deploy — each migrate/seed step re-runs once per deploy regardless of replica count, with the first container to claim the key running the script while the rest block" + "Two distinct keys (not one combined `-migrate-seed`) so a seed failure doesn't burn the migrate gate" + "`--retryUntilSuccessful` papers over the brief window where Postgres isn't yet accepting connections" | ✓ Semantic match + decomposition rationale + retry rationale |
| apidev dev | Same shape with ts-node | Same | (inherited from prod via setup) | ✓ Semantic match |
| workerdev | (no initCommands) | n/a | n/a | n/a |
| appdev | (no initCommands) | n/a | n/a | n/a |

**F-EXECONCE-SEMANTICS / P3: clean.** Apidev prod yaml gained a
substantive expansion explaining WHY two distinct keys (failure mode:
seed burning migrate gate). Pillar A holds.

### Citation URL audit per KB + IG bullet (the headline test)

**KB citations (run-45 final state)**:

| Surface | Bullet | Topic | Citation form | URL canonical-match |
|---|---|---|---|---|
| apidev KB #1 | NATS Auth Violation | managed-services-nats | NONE | — |
| apidev KB #2 | Object-storage DNS | object-storage | form-(b) | ✓ host+path matches `docs.zerops.io/services/object-storage` |
| apidev KB #3 | Meilisearch TLS | managed-services-meilisearch | NONE | — |
| apidev KB #4 | Valkey NOAUTH | managed-services-valkey | NONE | — |
| apidev KB #5 | Cross-origin headers | env-var-model | NONE | — |
| apidev KB #6 | NestJS DI undefined | (framework — no citation topic) | NONE | — |
| apidev KB #7 | Search card 0 docs | init-commands | form-(c) bare | ✓ host+path matches `docs.zerops.io/zerops-yaml/specification` (canonical for init-commands), but the BULLET BELONGS ON SURFACE-FAIL DROP |
| worker KB #1 | Queue group | rolling-deploys | form-(b) | ✓ host+path matches `docs.zerops.io/features/scaling-ha` |
| worker KB #2 | drain() on SIGTERM | rolling-deploys | form-(b) | ✓ host+path matches `docs.zerops.io/features/scaling-ha` |
| worker KB #3 | NATS URL creds | managed-services-nats | NONE | — |
| worker KB #4 | APP_SECRET shadow | env-var-model | NONE | — |
| worker KB #5 | Meilisearch https://-breaks | managed-services-meilisearch | NONE | — |
| appdev KB #1 | Cross-origin X-Cache | env-var-model | NONE | — |
| appdev KB #2 | VITE_API_URL literal | env-var-model | NONE | — |

**KB citation coverage: 4 of 14 bullets carry citations**. Two of the
four (worker KB #1 + #2) match canonical exactly. One (apidev KB #2)
matches canonical exactly. One (apidev KB #7) is on a bullet that
fails the surface test entirely — citation correctness doesn't redeem
wrong-surface placement.

**Vs run-44: 5/9 → 4/14 — coverage RATIO regressed; total citations
absolute count went DOWN from 5 to 4 despite KB total going UP from 9
to 14**. Pillar B's substrate intent (audit walks every KB bullet for
missing citation) didn't fire on codebase surfaces; the citations
that shipped came from authoring-time decisions, not audit nudges.

**IG citations (run-45 final state)**:

| Surface | Item | Topic | Citation |
|---|---|---|---|
| apidev IG #2 | Bind 0.0.0.0 / read PORT | http-support | NONE |
| apidev IG #3 | Trust proxy | http-support | NONE |
| apidev IG #4 | Drain SIGTERM | rolling-deploys | form-(b) **WRONG URL** — `[zero-downtime deploys with multi-container setups](https://docs.zerops.io/zerops-yaml/specification#minrunningcontainers-)`. Display text is form-(b) friendly name for rolling-deploys; URL host+path is `docs.zerops.io/zerops-yaml/specification` which is INIT-COMMANDS' canonical, not rolling-deploys' (`docs.zerops.io/features/scaling-ha`). G1's anchored matcher accepts this URL as init-commands. **The deliverable ships a citation that mislabels the doc** — porter clicking it lands on the specification anchor for `minrunningcontainers-`, not the scaling-ha guide. Marginal correctness, real defect. |
| apidev IG #5 | APP_SECRET own-key | env-var-model | NONE |
| worker IG #2 | NATS Pattern A | managed-services-nats | NONE |
| worker IG #3 | Queue group | env-var-model (or managed-services-nats) | NONE |
| worker IG #4 | SIGTERM drain | rolling-deploys | NONE |
| appdev IG #2 | Bind 0.0.0.0 + allowedHosts | http-support | NONE |
| appdev IG #3 | VITE_API_URL build-time bake | env-var-model | NONE |
| appdev IG #4 | dist/~ strip-prefix | deploy-files | NONE |

**IG citation coverage: 1 of 10 IG items carry a citation, and that
one is wrong-URL form-(b)** — runs 40-44 shipped 0/10-13; run-45
nominally moves to 1/10 but the deliverable correctness is degraded
by the mislabeled citation. **Substrate progress on the IG citation
axis: marginal, with new wrong-URL failure mode**.

### Self-inflicted bullet inventory (porter-following-IG#1 test)

For every KB bullet, the spec litmus is: "Could this observation be
summarized as 'our code did X, we fixed it to do Y'? If yes, DROP."

| Bullet | IG #1 ships | Trap fires when porter… | Verdict |
|---|---|---|---|
| apidev #1 NATS Auth | NATS Pattern A separate vars | …reaches for `${broker_connectionString}` URL form | **Intersection (KEEP)** |
| apidev #2 Object-storage | `forcePathStyle: true` in IG #2 (S3 client) | …uses default virtual-host AWS SDK style | **Intersection (KEEP)** |
| apidev #3 Meilisearch TLS | `http://` MEILI_HOST in yaml | …points at `https://` for in-cluster traffic | **Intersection (KEEP)** |
| apidev #4 Valkey NOAUTH | `REDIS_URL: redis://${cache_hostname}:${cache_port}` (no user/pass segment) | …adds `${cache_user}/${cache_password}` to URL | **Intersection (KEEP)** |
| apidev #5 Cross-origin headers | `CORS_ORIGINS` in yaml + `exposedHeaders` in main.ts | …ships custom response headers without `exposedHeaders` enumeration | **Intersection (KEEP) — but body recipe-internal-names "cache demo"** |
| apidev #6 NestJS DI undefined | (yaml doesn't address NestJS internal wiring) | …structures NestJS DI tokens in the same module as their consumer | **DROP — framework-quirk per S5 spec litmus** (Codex-validated) |
| apidev #7 Search card 0 docs | execOnce-gated `dist/seed.js` in yaml + in-app SearchIndexer in scaffold | …ships a seed script outside the NestJS context AND an in-app SearchIndexer | **DROP — self-inflicted + self-referential per spec litmus #4 + §"Self-referential decoration prohibition"** (Codex-validated). The "fix" is the recipe re-pushing rows in `ItemsService.onModuleInit` — pure scaffold-specific reconciliation. |
| worker #1-#5 | Various | … | **All Intersection (KEEP) — clean platform-anchor teaching** |
| appdev #1 X-Cache from SPA | (yaml doesn't address SPA-side fetch credentials) | …reads custom response headers cross-origin without the api exposing them | **Intersection (KEEP) — partial recipe-internal naming "Cache panel"** |
| appdev #2 VITE_API_URL literal | `VITE_API_URL: ${API_URL}` build-time in yaml | …points VITE_API_URL at peer subdomain alias before peer deploys | **Intersection (KEEP)** |

**Net**: 2 hard-fail bullets shipping (apidev KB #6 framework-quirk;
apidev KB #7 self-inflicted), 2 borderline (apidev KB #5, appdev KB #1
— recipe-internal naming in body), 10 clean intersections.

**Run-44 had 2 borderline (Cache demo, tryGetClient); run-45 has 2
hard-fail + 2 borderline**. The borderline-creep continued AND
introduced two new clear-fail bullets. Pillar B's per-surface walk
would have caught both (S5 single-question test framework-quirk DROP
litmus; §"Self-referential decoration prohibition" recipe-internal
helper-class DROP).

### Aspirational-as-current check

Walked tier yamls + KB + IG for aspirational claims (claims about
behavior the code doesn't implement).

- No aspirational JWT claims. apidev IG #5 names `API_SIGNING_KEY:
  ${APP_SECRET}` and worker KB #4 names `WORKER_SIGNING_KEY:
  ${APP_SECRET}` — both are clean own-key alias teachings (intersection
  + porter-portable). The teaching doesn't aspirationally claim
  cross-service JWT verification works.
- No aspirational HA-failover claims at deliverable. The Valkey HA
  fabrication ("platform forwards 6379 on replica to master") was
  AUTHORED at env-content phase, CAUGHT by refinement-2-audit (finding
  #9, blocker), and REWRITTEN by triage to porter-observable shape.
  **Within-run regression closure by Pillar B on the surface that
  Pillar B did walk**.
- No execOnce semantic-lie comments. Apidev prod yaml's "two distinct
  keys so a seed failure doesn't burn the migrate gate" is
  factually-grounded decomposition rationale.

**Aspirational claims surviving to deliverable: zero** — clean
authoring this run + audit caught the one Valkey HA fabrication.
Run-43 / run-44 stochastic regression closure shape holds.

### Cross-codebase / cross-surface duplication

The audit's cross-surface uniqueness pass on S3 caught 5 cases (audit
findings #3, #5, #7, #8, #11) and triage acted on all 5. Net at
deliverable: tier yamls no longer carry codebase-mechanism teaching
verbatim.

**But two cross-codebase KB duplications survived on codebase surfaces**:

- **apidev IG #5 + worker KB #4** both teach the `APP_SECRET`
  same-key shadow trap — apidev as IG step (with `API_SIGNING_KEY:
  ${APP_SECRET}` example), worker as KB bullet (with
  `WORKER_SIGNING_KEY: ${APP_SECRET}` example). Each codebase teaches
  its own slot's alias and references the underlying trap; neither
  cross-references the other. Per spec §"Cross-surface discipline"
  ("each fact lives on exactly one surface"), one of these should
  cross-reference. Per spec §"Self-referential decoration
  prohibition", neither is recipe-internal. Borderline — defensible
  as parallel codebase-specific instances, but the substrate's
  one-fact-one-surface clause says one is canonical, the other
  references.
- **apidev KB #3 + worker KB #5** both teach the `https://` Meilisearch
  in-cluster TLS handshake failure. Worker KB #5 is the later-read.
  Per cross-surface uniqueness pass DROP-the-later-read rule, worker
  KB #5 should cross-reference apidev KB #3. Substrate per-surface
  walk on codebase surfaces would catch this.

The audit didn't walk codebase surfaces, so neither cross-codebase
duplication was emitted.

### Recipe-internal naming creep audit

The substrate spec §"Self-referential decoration prohibition" forbids
items whose meaning depends on the reader knowing the recipe's helper
file/class/symbol names. Walked every KB body + IG body + yaml
comment for recipe-internal symbols.

| Surface | Item | Recipe-internal symbol | Survives Pillar B walk? |
|---|---|---|---|
| apidev KB #5 body | "The cache demo's X-Cache, X-Cache-Elapsed-Ms, X-Cache-Key headers are visible to curl but undefined when the SPA reads them" | "cache demo" (recipe endpoint name) | **Stem rephrased symptom-first; body retains "cache demo" framing** |
| apidev KB #6 body | Names `cache.module.ts`, `CacheService`, `CacheController`, `REDIS_CLIENT`, `cache.tokens.ts` | Full module structure | **Survives — Pillar B never walked** |
| apidev KB #7 body | Names `SearchIndexer`, `ItemsService.onModuleInit`, "Search card", "Items card" | Module + endpoint + UI feature names | **Survives — Pillar B never walked** |
| appdev KB #1 body | Names "Cache panel" | UI feature name (dashboard tab) | **Survives — borderline** |
| apidev CLAUDE.md | Names architecture components (legitimate, ✓ S6 test pass) | — | n/a |
| Tier yamls | "deployed twice" meta-narrative (audit finding #15, ACCEPTed) | — | n/a |

**Run-44 vs run-45 recipe-internal naming creep**:
- Run-44 apidev KB #2 stem: "Cache demo silently returns null for X-Cache from the SPA" — recipe-internal stem
- Run-45 apidev KB #5 stem: "Cross-origin custom response headers return `undefined`" — symptom-first stem (IMPROVEMENT), body still names "cache demo"
- Run-44 apidev KB #4 body: "tryGetClient()" helper name — recipe-internal helper
- Run-45 apidev KB #6 body: `cache.module.ts`/`CacheService`/`CacheController`/`REDIS_CLIENT`/`cache.tokens.ts` — recipe-internal MODULE STRUCTURE (regression in scope)
- Run-45 apidev KB #7 body: `SearchIndexer`/`ItemsService.onModuleInit`/Search card/Items card — recipe-internal MODULE STRUCTURE + UI FEATURE NAMES (new regression)

**Net**: stem-quality on KB #5 improved; new bullets KB #6 and KB #7
introduced wider recipe-internal naming surface area than run-44's
two borderlines. **Pillar B per-surface walk would have caught the
new bullets at refinement-2 — but didn't run on codebase surfaces**.

---

## Spec-content audit — surface-by-surface

| Section | Verdict | Notes |
|---|---|---|
| §"Empirical floor" (goldens) | **Partial** | Codebase yaml friendly-authority hits regressed 7→2 across 3 yamls; tier yaml hits improved 18→24 across 6 tiers; KB voice shape matches showcase defensive form except for 2 hard-fail bullets shipping. |
| §"Why this exists — journal failure mode" | **Partial** | Recipe-internal naming surface expanded (module structure + UI feature names); cross-surface deferrals stay closed; aspirational claims stay closed. |
| §"Fact classification taxonomy" | **Held on emitted findings; DISCARD-override path unverified** | Pillar C enrichment fires cleanly on S3 findings. Zero classification rejections (down from 2-3 in prior runs). DISCARD-override path never tested because zero DISCARD-class facts got recorded upstream. |
| §"Self-inflicted" litmus #4 | **REGRESSED** | Run-44 had 2 borderline; run-45 has 2 hard-fail + 2 borderline. Audit didn't walk codebase surfaces. |
| §"Self-referential decoration prohibition" | **REGRESSED** | New recipe-internal module-structure naming in apidev KB #6 + #7. |
| §"Friendly-authority voice" codebase yamls | **REGRESSED** (7→2 hits) | F6 substrate text held in brief but authoring pattern not reproduced by run-45 author. |
| §"Friendly-authority voice" tier yamls | **IMPROVED** (18→24 hits) | Triage rewrites added adapt-paths per tier. |
| §"Surface 5" S5 single-question test | **NOT EXERCISED on codebase surfaces** | Refinement-2-audit emitted zero S5 findings. |
| §"Surface 7" (yaml comments) | **HELD** | No cross-surface deferrals; mechanism + reason in one breath; substrate Y15 density rules held. |
| §"Surface 3" (tier yaml) | **IMPROVED** | Cross-tier deferrals zero; 18 findings + 16 ACTs cleaned a real cross-surface duplication baseline. |
| §"Citation map" — KB | 4/14 (down from 5/9 absolute) | Audit didn't walk codebase KB for missing citation. |
| §"Citation map" — IG | **1/10 with wrong-URL form-(b)** | Marginal axis movement; new wrong-URL failure shape. |

---

## Golden voice alignment

### KB shape comparison

**Jetstream KB** (2 H3 bullets, operational): teaches `php artisan
down` workflow and `zsc scale ram +0.5GB 10m` — porter takes action.

**Showcase KB** (7 H3 bullets, defensive symptom-first): all
intersections with platform-mechanism anchors. NestJS context: `No
.env file`, `Cache commands in initCommands`, `APP_KEY is
project-level`, `PDO PostgreSQL extension`, `Predis over phpredis`,
`Object storage requires path-style`, `Vite manifest missing on dev
after fresh deploy`. **Zero self-inflicted; zero recipe-internal
naming**.

**Run-45 KB** (14 distinct H3 bullets across 3 codebases): 10/14 clean
intersections matching showcase shape; 2/14 borderline; 2/14
hard-fail. **Voice shape closer to showcase than run-44 on the clean
bullets (more bullets per codebase, better symptom-first stems), but
the 2 hard-fail bullets break the showcase invariant of "every bullet
is platform-anchored intersection"**.

**Verdict**: closer to showcase quantitatively, breaks the
golden's qualitative invariant.

### Yaml comment voice comparison vs jetstream

**Jetstream zerops.yaml** has 3+ friendly-authority hits in a single
yaml — `Feel free to change this value to your own custom domain`,
`Configure this to use real SMTP sinks in true production setups`,
`Note that port 25 is restricted on Zerops by default` (the SMTP
adapt-path one is the cleanest run-45 example match too).

**Run-45 codebase zerops.yamls** have ~2 hits across 3 yamls — material
regression vs run-44's 7. The one clean run-45 hit (appdev L57-58
"Feel free to point this at your own custom domain") matches the
jetstream `APP_URL` adapt-path shape exactly.

**Verdict**: jetstream-style adapt-path KNOW-HOW present (matches
shape) but NOT broadcast across the surface (one-off vs distributed).
Run-44's distribution was substrate-attributable; run-45 lost it. F6
brief text held but author didn't reproduce.

---

## Content quality progression vs runs 42/43/44

### apidev KB progression

| Run | Bullets | Notable shape |
|---|---:|---|
| **42** | 5 | NATS Invalid URL + Object-storage UnknownError (self-inflicted) + X-Cache + CORS literal + NATS publish-drop |
| **43** | 5 | NATS Invalid URL + forcePathStyle: 403 + Cross-origin SPA headers + Valkey no user/password aliases + Meilisearch master key |
| **44** | 4 | Authorization Violation + Cache demo X-Cache (recipe-internal stem) + relation already exists (clean platform-invariant) + Cache 5xx Valkey blip (recipe-internal tryGetClient) |
| **45** | **7** | Authorization Violation + Object-storage DNS + Meilisearch TLS + Valkey NOAUTH + Cross-origin headers (borderline) + **NestJS DI undefined (framework-quirk SHIPS)** + **Search card 0 docs (self-inflicted SHIPS)** |

**Run-45 progression**: kept 4 run-44/run-43 platform-invariant
teachings cleanly; rephrased apidev #5 stem to symptom-first
(improvement); added 2 new wrong-surface bullets (KB #6 + KB #7).
Stem-quality net: +1 (KB #5 improvement); voice-classification net: -2
(KB #6 framework-quirk + KB #7 self-inflicted both fail S5 test).

### workerdev KB progression

| Run | Bullets | Notable shape |
|---|---:|---|
| **42** | 4 | queue-group + drain + NATS Invalid URL + same-key shadow |
| **43** | 3 | queue-group + drain + cross-ref to api KB |
| **44** | 3 | queue group + drain + NatsError (cross-ref + symptom dim) |
| **45** | **5** | queue group + drain + NATS URL creds + APP_SECRET self-shadow + Meilisearch https://-breaks |

**Run-45 worker KB**: 5 bullets, all clean intersections, all
symptom-first stems. **Best workerdev KB shape across all four runs**
— teaching surface broadened while quality held. **Substrate-adjacent
improvement (Pillar A held the surface-test invariant in the brief);
substrate-actionable improvement on showcase parity (queue-group + drain
both carry rolling-deploys citation form-(b))**.

### appdev KB progression

| Run | Bullets | Notable shape |
|---|---:|---|
| **42** | 4 | Dev Blocked + VITE_API_URL literal + SPA 404 + vue-tsc not found |
| **43** | 2 | Dev preview Blocked + `${apistage_zeropsSubdomain}` literal |
| **44** | 2 | Dev container Blocked + `${API_URL}` literal (both IG-echoes) |
| **45** | **2** | Cross-origin X-Cache from SPA (cross-codebase teaching at appdev) + VITE_API_URL literal token |

**Run-45 appdev KB**: same 2-bullet count, both intersection-class.
Bullet #1 (X-Cache cross-origin) is a new appdev-side teaching of the
trap apidev KB #5 teaches from the API side — borderline cross-
codebase duplication but legitimately framed (SPA-side fix is
credentials mode, API-side fix is exposedHeaders). Bullet #2
(`${apistage_zeropsSubdomain}` literal) is the run-43 / run-44 carry-
forward with the same teaching depth.

### Citation coverage progression

| Run | apidev KB | worker KB | appdev KB | IG citations (all 3) |
|---|:---:|:---:|:---:|:---:|
| **40** | 0/7 | 0/5 | 0/4 | 0 |
| **41** | 0/6 | 0/5 | 0/4 | 0 |
| **42** | 4/5 | 2/4 | 3/4 | 0 |
| **43** | 1/5 (wrong-path) | 2/3 (wrong-path) | 0/2 | 0 |
| **44** | 2/4 (cite-by-name) | 2/3 (cite-by-name) | 0/2 | 0 |
| **45** | **2/7** (form-(b) + form-(c)) | **2/5** (both form-(b)) | **0/2** | **1/10** (form-(b) wrong URL) |

**Run-45 KB citation progression**: 4/14 citations (absolute count down
from run-44's 5/9; ratio significantly down). Both worker KB citations
form-(b) with canonical-correct URLs. One apidev KB citation form-(b)
canonical-correct (object-storage), one form-(c) bare-URL-style at a
bullet that fails the surface test (KB #7 init-commands).

**IG citation: 1/10 across three codebases — nominal axis movement,
wrong-URL form-(b) failure shape (apidev IG #4)**. The friendly-display
text correctly maps to the rolling-deploys topic but the URL points at
init-commands' canonical path with a `#minrunningcontainers-` fragment
(presumably the author confused the rolling-deploys topic with the
specification's minrunningcontainers field reference). G1's anchored
matcher accepts the URL as init-commands canonical so it didn't
reject. Substrate gap: the validator doesn't cross-check display-text
topic-family against URL guide-id.

### What "the substrate caught" looks like across runs

- **Run-42**: 17 findings → 17 ACTs. Defect classes enumerated. Missed
  several class boundaries.
- **Run-43**: 22 findings → 11 ACTed + 11 HELD. Missed IG citations,
  self-inflicted borderlines.
- **Run-44**: 10 findings → 7 ACTed + 2 MOOT + 1 collapsed. Caught
  aspirational JWT regression. Missed IG citations (G2 emit failure)
  + self-inflicted borderlines (G3 strict letter).
- **Run-45**: **18 findings → 16 ACTed + 1 ACCEPT + 1 HOLD. ALL 18 ON
  S3.** Caught Valkey HA fabrication + 5 cross-surface tier-yaml
  duplications + corePackage SERIOUS field-site comment gap. Missed
  every codebase-surface defect because the audit didn't walk
  codebase surfaces.

**Run-45 catch rate on S3 is best across all four runs**. Run-45
catch rate on S4/S5/S6/S7 is zero.

### Bottom-line content quality vs run-44

Reading run-45 as a porter: apidev README has expanded teaching surface
(7 KB bullets vs 4) but two new wrong-surface bullets ship; appdev KB
holds; worker KB has best shape across all four runs (5 clean
intersections with 2 rolling-deploys citations). Codebase yaml
friendly-authority adapt-paths regressed materially (7→2). Tier yaml
friendly-authority adapt-paths improved materially (18→24).

**On the content-quality axes the run-44 validation flagged**:
- IG citations (run-44 gap): **1/10 with wrong-URL form-(b) — marginal
  axis movement, new failure mode**. Pillar B substrate intent
  unrealized on codebase surfaces.
- F3 substrate-internal contradiction (G1 target): **CLOSED — zero
  wrong-path-URL retries, zero snapshot/restore reverts**. Pillar C
  closed the cycle on the path it covers.
- Borderline self-inflicted (G3 target): **REGRESSED — 2 hard-fail bullets
  ship**. Pillar B substrate intent unrealized on codebase surfaces.
- KB-IG advisory HOLDs (G4 target): **NOT EXERCISED — audit didn't walk
  codebase surfaces; both cross-codebase KB duplications + the
  cross-codebase KB+IG echo on APP_SECRET self-shadow ship without
  cross-references**.
- KB voice operational shift (G7 target): **HELD — no fully-defensive
  KB; rolling-deploys cite-by-name and form-(b) URLs on workerdev**.

**Run-45 represents substrate-attributable improvement on tier yaml
audit catch rate + Pillar C engine-determinism for emitted findings.
Material regression on codebase yaml friendly-authority hits. New
deliverable defect class: codebase-surface defects ship without audit
catch because Pillar B's audit-emit path on codebase surfaces is
broken at the dispatch-discovery layer.**

---

## Substrate operations

### Refinement-class sub-agent dispatch count

| Dispatch | Run-42 | Run-43 | Run-44 | Run-45 |
|---|---:|---:|---:|---:|
| refinement-1 | 2 | 1 | 1 | **1** |
| refinement-2-audit | 1 | 1 | 1 | **1** |
| refinement-2-triage | 0 | 0 | 0 | **1** (NEW — main agent delegated triage to sub-agent) |
| refinement-rulewalk | 1 | 0 | 0 | 0 |
| **Total refinement-class** | 4 | 2 | 2 | **3** |

**Run-45 dispatches refinement-2-triage as a separate sub-agent**.
Substrate brief says main agent triages — this run delegated. Per
session flags (`Refinement2Dispatched`) only the audit dispatch
counts toward the gate; triage is a downstream worker. Not a
regression in the dispatch-flag sense but a deviation from the
substrate's main-agent-triages pattern. Worth flagging for substrate
review: should the substrate codify "triage may be delegated"?

### Phase ordering trace

```
L25-66     complete-phase research → ok
L67-83     scaffold (3 parallel) → complete-phase scaffold → ok
L83-110    feature backend + frontend → complete-phase feature → ok
L110-141   codebase-content (6 parallel: 3 codebase + 3 claudemd) → complete-phase codebase-content → ok
L141-175   env-content (1 sequential) → complete-phase env-content → ok
L175-185   complete-phase finalize → ok
L185-192   refinement-1-rulewalk dispatch + return
L192-202   refinement-2-audit dispatch + return (18 findings JSON)
L202-280   refinement-2-triage dispatch + return (19 fragment replacements)
~L285      complete-phase phase=refinement → FAIL (6 missing-priority-justification-block)
~L290      record-fragment ×6 (re-prepend TY5 canonical) + stitch
~L295      complete-phase phase=refinement → ok
```

**Edit D phase ordering held**. complete-phase finalize closed
cleanly; refinement happened at phase=refinement. Refinement-close
re-ran validators (6 missing-priority-justification-block surfaced
on first close prove the validators executed). The TY5 canonical-block
strip in triage's #1 ACT broke surface validation; re-record cycle
closed the gap. **Same shape as run-44's snapshot/restore reverts but
on a different failure class — surface-validator-blocking ACT instead
of citation-URL-mismatch ACT**.

### enrich-findings pipeline behavior

```
grep -c enrich-findings main-session.jsonl  # → 5 calls
```

Five `action=enrich-findings` invocations visible. Sub-agent emitted
slim 7-field findings JSON (verified at audit transcript line 92);
main agent posted to `enrich-findings` MCP; engine returned enriched
shape with `fragmentId` / `classification` / `suggestedReplacement`
populated per the implementation at
[enrich_findings.go:358](internal/recipe/enrich_findings.go#L358).
Main agent then issued record-fragment ACTs with the enriched fields
copied verbatim. **Zero classification rejections at ACT path**
(verified by absence of `classification is required for fragments on
surface` errors in main-session.jsonl). Run-44 had 2 such rejections;
run-45 has 0. **Pillar C copy-verbatim pipeline works**.

### Classification rejection rate

| Run | Rejections at main-agent ACT | Notes |
|---|---:|---|
| 43 | 3 | Sub-agent didn't emit classification; main agent retried |
| 44 | 2 | Same — G5 substrate failed at brief-render |
| **45** | **0** | **Pillar C closes the cycle** |

### Snapshot/restore revert count

| Run | refinement-replace-reverted notices | Notes |
|---|---:|---|
| 43 | 0 | URL-form path wasn't exercised |
| 44 | 4 | Wrong-path URLs (init-commands + rolling-deploys) rejected by G1 anchored matcher |
| **45** | **0** | **Pillar C suggestedReplacement pre-fill (where audit emitted topic + REWRITE) gave main agent canonical URL to copy** |

### Sub-agent durations

| Sub-agent | Run-44 | Run-45 | Delta |
|---|---|---|---|
| refinement-1-rulewalk | ~1130s | ~404s | -65% (substantial decrease — narrower violation surface? Y15 density HOLDs?) |
| refinement-2-audit | ~387s | ~328s | -15% (within stochastic variance) |
| refinement-2-triage | n/a | ~316s | NEW |
| codebase-content api/app/worker | 28/34/36 min | 380s/279s/299s ≈ 6/5/5 min | **Significant speedup** — codebase-content sub-agents 4-5× faster |
| feature-backend | 22 min | 1618s ≈ 27 min | +20% |
| feature-frontend | 10 min | 624s ≈ 10 min | flat |
| env-content | n/a | 994s ≈ 17 min | — |

**Refinement-1 speedup is notable** — could be either substrate
narrowing the violation surface (fewer rules fire) OR sub-agent
shortcutting. Spot-checked refinement-1 transcript: walked
`derived_rules.md` Y1-Y15 + IG1-IG6 + KB1/KB3-KB6, applied 2 ACTs
(IG6 self-shadow framing + V6 "scaffold" → porter framing).
Substantive walk, just narrower violation surface. **Not a substrate
concern**.

**Codebase-content speedup is notable** — could indicate the
codebase-content brief is tighter and the codebases are smaller than
run-44, OR the sub-agents short-circuited. Not investigated further;
content quality on workerdev/appdev READMEs supports legitimate
substrate maturity rather than short-circuit.

### Brief-render path integrity (Pillar A pin)

Spot-checked the rendered refinement-2 brief in line 1 of
`agent-aaf3644b4a4c7083b.jsonl`:

- ✓ All five per-surface single-question tests verbatim (S3 / S4 / S5
  / S6 / S7) from content-surface-contracts.md
- ✓ Closed-set failure modes (DROP / MOVE-TO-Sn / REWRITE)
- ✓ Cross-surface uniqueness pass description
- ✓ Citation map family list
- ✓ slim 7-field findings JSON schema
- ✓ factRef shape `<topic>@<recordedAt>`
- ✓ DISCARD-class override notice ("when factRef class can't legally
  land on this surface, engine overrides to DROP")

**Pillar A pin holds**: every load-bearing local-substrate paragraph
reaches the rendered brief. Pin test fires at build time, brief
inspection at audit time confirms.

### Parent-recipe fetch at research

Per TIMELINE step 1: "Parent recipe `nestjs-minimal` detected as
embedded — convention inheritance pulled from that guide before plan
submission." Substrate-endorsed; not flagged.

### Build-time substrate health

```
go test ./internal/recipe/ -run "TestBriefsRendered|TestEnrichFindings|TestAtomAuthoringLint" -count=1 -short
ok  	github.com/zeropsio/zcp/internal/recipe	1.008s
```

**Pin tests + Pillar C tests + atom lint all pass on main at v9.90.0.**
Run started after substrate landed; substrate-state correct.

---

## Known-substrate-issues — what's still present + what closed

### Closed (no longer surface gaps)

- **G5 substrate gap (classification field on findings)** — Pillar A
  pin tests prevent silent-drop at build; Pillar C engine pre-fills
  classification so the sub-agent never has to emit it. Zero
  classification rejections at main-agent ACT path. **CLOSED**.
- **G6 substrate gap (suggestedReplacement pre-resolved markdown
  link)** — Pillar C `suggestedReplacementForTopic()` renders form-(b)
  link from citation map. Zero snapshot/restore reverts. **CLOSED on
  the emit path** (orthogonal to the codebase-surface-not-walked
  finding).
- **G7 substrate gap (KB-DEFENSIVE-FLOOR rule)** — Pillar A pins
  derived_rules.md to the rendered refinement-1 brief; the rule fires
  in refinement-1's rule walk. **CLOSED at brief-render** (run-44's
  smoking-gun render-pipeline gap).
- **G1 anchored URL acceptance** — held. Apidev IG #4's wrong-URL is a
  separate failure shape (display-text-topic-family vs URL-guide-id
  mismatch), not an anchored-URL-mismatch.

### Open (substrate frontiers)

- **Pillar B audit-emit on codebase surfaces — NEW substrate gap**.
  Refinement-2-audit sub-agent dispatched with outputRoot-scoped path
  scope; doesn't run `ls /var/www/{apidev,appdev,workerdev}/` to find
  the codebase trees that live one directory up at the SSHFS-mount
  point. Concludes "S4-S7 not materialized" and emits ZERO findings
  on codebase surfaces.  Fix candidate: dispatch prompt explicitly
  names `/var/www/<host>{dev,stage}/` codebase paths; OR audit brief
  gates on a directory-listing the sub-agent proves it ran; OR
  refinement-close refuses if `Refinement2Dispatched=true` AND zero
  S4/S5/S6/S7 findings were emitted (the audit emitting zero
  codebase findings is a detectable substrate failure mode).
- **Cross-codebase KB duplication** — apidev IG #5 + worker KB #4
  teach `APP_SECRET` same-key shadow; apidev KB #3 + worker KB #5
  teach https://-Meilisearch in-cluster TLS. Per spec §"Cross-surface
  discipline" each fact lives on exactly one surface with cross-
  references on the others. Audit didn't walk codebase surfaces so
  neither was flagged. Fix candidate: Pillar B per-surface walk on
  codebase surfaces (i.e. fix the dispatch-discovery first).
- **Wrong-URL form-(b) citation acceptance** — apidev IG #4 ships
  `[rolling-deploys friendly display](init-commands canonical URL)`
  — display text matches one topic, URL host+path matches another.
  G1's anchored matcher accepts because URL host+path matches
  init-commands. **Substrate gap**: validator doesn't cross-check
  display-text topic-family against URL guide-id. Fix candidate:
  extend G1 to require {topic of the bullet's primary teaching, URL
  guide-id} agreement, not just URL host+path canonical match.
- **Triage ACT surface-validator compatibility** — triage's TY5
  canonical-block strip broke surface validation at first close (6
  missing-priority-justification-block); re-record needed. Same
  shape as run-44 wrong-path-URL but at a different validator.
  Fix candidate: wrap triage ACTs in snapshot/restore primitive
  (currently only refinement-1's record-fragment is wrapped); OR
  triage brief explicitly warns "do not strip surface-validator-
  required canonical blocks".
- **Codebase yaml friendly-authority adapt-path REGRESSION**: 7→2 hits
  across 3 yamls. Substrate F6 text held but author didn't reproduce
  the pattern. Fix candidate: codebase-content brief carries explicit
  worked examples of adapt-path comments per managed-service-family,
  OR a structural validator that gates codebase-content close on N≥1
  adapt-path-shape comment per codebase yaml.
- **DISCARD-class facts never recorded upstream** — codebase-content
  + feature sub-agents recorded zero facts with `candidateClass:
  framework-quirk / self-inflicted / library-metadata`. Pillar C's
  DISCARD-override path is unverified-in-production. Fix candidate:
  codebase-content brief carries worked examples of when to emit
  each DISCARD class; OR upstream sub-agents are gated on the spec
  litmus and the brief teaches the four-question test.

---

## Recommended next action (ranked by content-quality impact)

1. **Fix Pillar B dispatch discovery so refinement-2-audit walks
   codebase surfaces.** This is the highest-impact substrate fix:
   without it, framework-quirk + self-inflicted bullets continue to
   ship at run-46+. Smallest possible diff: extend the refinement-2
   dispatch prompt (`internal/recipe/briefs_refinement2.go::
   buildRefinement2Brief`) with an explicit "Stitched output to audit"
   section that names `/var/www/<host>{dev,stage}/` for each codebase
   in the plan, AND require the sub-agent to emit a "directory-walk
   manifest" (paths it actually listed) so refinement-close can
   refuse on empty manifests. Pin: extend
   `internal/recipe/briefs_rendered_substrate_pin_test.go` to assert
   the codebase-path enumeration is present in the rendered brief
   when `plan.codebases` has entries.

2. **Add Pillar C DISCARD-class authoring teaching to codebase-content
   brief.** The four-question spec litmus
   (could-be-summarized-as-our-code-did-X / framework-docs-cover /
   library-metadata / scaffold-decision-not-platform-trap) lives in
   `spec-content-surfaces.md` §"Self-inflicted" but isn't surfaced in
   the codebase-content brief's authoring teaching. Author emits 60
   facts at codebase-content phase, ZERO with DISCARD-class
   candidateClass. Pillar C's DISCARD-override is wired but never
   tested. Fix candidate: codebase-content brief gains a "DISCARD-class
   detection checklist" worked-examples section keyed to the spec litmus.

3. **Wrap triage ACTs in snapshot/restore.** The triage sub-agent
   stripped a TY5-required canonical block; the surface validator
   caught it at refinement-close but only after a re-record cycle.
   Same primitive wrapper that refinement-1 enjoys would catch this
   class of ACT at ACT time, not at close time.

4. **Add F-FRIENDLY-AUTH structural gate to codebase-content close.**
   Run-44 author broadcast adapt-paths to all three codebase yamls
   (~7 hits); run-45 author broadcast ~2. The pattern's broadcast is
   author-stochastic and the substrate doesn't enforce. Soft floor:
   N≥1 adapt-path-shape comment (regex match on "if you", "bump",
   "once you", "feel free to", "swap to", "rotate") per codebase yaml
   that has a porter-tunable directive (init-commands, build env
   vars, custom domain, etc.).

5. **Extend G1's URL canonical-match to display-text topic-family
   cross-check.** Apidev IG #4 ships `[rolling-deploys friendly](init-
   commands URL)` and passes. Fix: in the citation-validation gate
   (`citations.go::validateCitationURL` or similar), require
   `friendlyDisplayName(topic_of_bullet) == display_text` AND
   `CitationGuideURL[topic_of_bullet] == URL_host_path`. Pin: extend
   `TestCitationGuideURL_*` with a wrong-URL-form-(b) case.

If only one substrate fix lands before run-46, **make it #1**. Pillar B
not walking codebase surfaces is the dominant deliverable-quality
gap; #2-5 are smaller axes.

---

## Codex independent validation

Codex (gpt-5.4) independently validated this plan against the raw
artifacts on 2026-05-13:

- (a) Pillar B dispatch-discovery framing — **CLAIM-PARTIALLY-WRONG**.
  Both refinement-1 and refinement-2-audit dispatches use the same
  scope (`/var/www/zcprecipator/nestjs-showcase/`). The asymmetry isn't
  in dispatch scope; it's in the sub-agents' filesystem exploration.
  Refinement-1's rule substrate (derived_rules.md Y1-Y15) drives the
  sub-agent to seek codebase trees; refinement-2's audit_checklist.md
  describes S4-S7 fragment IDs abstractly without instructing where on
  the live filesystem those fragments live. Final report incorporates
  this correction.
- (b) apidev KB #6 framework-quirk classification — **CLAIM-HOLDS**.
  *"Pure NestJS DI decorator-timing / circular-import. Valkey/Redis is
  only the incidental provider payload; the trap is entirely in NestJS
  module wiring."*
- (c) apidev KB #7 self-inflicted classification — **CLAIM-HOLDS**.
  *"Names SearchIndexer and ItemsService.onModuleInit — both
  scaffold-specific symbols. The S5 self-referential litmus fires.
  A porter who replaced the scaffold with their own indexer would not
  hit this exact trap."*

The codex correction (a) is the same shape as run-44's G5
misattribution correction: collapsing two distinct mechanisms into one
framing obscures the substrate frontier. The corrected framing in this
plan: refinement-2-audit's brief tells the sub-agent WHAT to walk
(per-surface single-question tests on fragments S4-S7) but doesn't
tell the sub-agent WHERE the fragments are stitched on the live
filesystem. Refinement-1's brief incidentally drives exploration via
the per-rule walk over yaml rules that name codebase paths.

Codex thread agentId: `a65c3fdba8889322b`.
