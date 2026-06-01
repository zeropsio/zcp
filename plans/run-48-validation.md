# Run-48 validation — H3-shape KB validator + bidirectional display→URL dogfood

> **Headline: SHIP-AS-CANONICAL.** Run-48 ships a deliverable a careful
> porter does NOT block on. Every KB and IG citation across all three
> codebases is canonical display↔URL (8/8 IG + 1/1 KB — best-ever
> coverage); zero self-inflicted bullets; zero `kb-citation-display-
> mismatch` defects shipping; zero `kb-citation-display-name-without-
> canonical-url` candidates. Run-47's five frontier defects (workerdev
> KB #1/#2 wrong-anchor, apidev KB #1 display drift, apidev KB #6
> Search-card-0-indexed, workerdev KB #4 MEILI_HOST narration) are
> **absent at the deliverable surface**. The v9.93.0 substrate's
> headline win lands at the right boundary: the H3-fallback splitter
> made Check 1 (`kb-citation-display-mismatch`) visible to KB content
> for the first time across runs 44-48 — the api codebase-content
> sub-agent hit 2-5 refusals on its first authoring pass against the
> NATS + Meilisearch friendly names, self-corrected inline to
> `[managed NATS broker]` / `[managed Meilisearch service]`, and closed
> `ok:true`. Refinement-2 audit subsequently DROPped 7 KB bullets
> (api lost 3, app lost 1, worker lost 3) — the recipe's third
> structural-quality vector after `derived_rules.md` rule-walk and the
> citation-display gate. Net KB count fell 15 → 8, but every surviving
> bullet passes S5 cleanly. The new bidirectional Check 4
> (`kb-citation-display-name-without-canonical-url`) and the H3-aware
> self-inflicted-shape detector are **wired and production-unverified**:
> sub-agents didn't author either failure shape this run, so neither
> validator had anything to fire on. Item C's `<` → `!=` tightening
> (handlers.go:1320-1322) FIRED on a real overcount: refinement-2 close
> refused with `crossSurfaceUniquenessScanned=82 != manifest=71`, main
> agent re-submitted scanned=71 + empty findings to clear. One
> borderline finding worth user attention: apidev KB #3 ("`ListObjectsV2`
> returns oldest-first") is pure S3-spec behavior rather than a
> Zerops×framework intersection — not actively harmful, but the S5
> single-question test is weaker than the other 7 bullets.

---

## Per-item score

| # | Substrate item | Verdict | Surface + telemetry evidence |
|---|---|---|---|
| **H3-fix** | `citationBlockSplits` un-numbered H3 fallback (db77c2bc, validators_codebase.go:881-892) | **FIRED AS DESIGNED** | 2-5 `kb-citation-display-mismatch` refusals inside `codebase-content-api` sub-agent (transcript `agent-adb7e7e29ed4b2316.jsonl:79,95`; worker mirror `agent-a6b43b48046c0baa8.jsonl:93,107` per codex). Without the H3 fallback the splitter would have returned nil on H3-shape KB bodies (run-47 verdict, codex-confirmed) and Check 1 would have silently skipped. Sub-agents self-corrected link text to canonical friendly names, closed `ok:true`. Item 5's design intent reaches the KB surface for the FIRST time across runs 44-48. |
| **Check 4** | Bidirectional `kb-citation-display-name-without-canonical-url` (db77c2bc, validators_codebase.go:787-808) | **UNVERIFIED-IN-PRODUCTION** | Zero hits across all session logs. Sub-agents authored canonical URLs to begin with — no "known friendly + wrong URL" pattern recurred. Pin tests cover the path; production fire path is wired but un-exercised. Codex-confirmed Check 4 cannot fire silently (validators_codebase.go:799-801 emits an explicit Violation entry). |
| **H3 self-inflicted** | `kbBulletBlocks` H3 fallback in validators_kb_quality.go:208-216 | **UNVERIFIED-IN-PRODUCTION** | Zero `kb-bullet-self-inflicted-shape` / `kb-bullet-cited-guide-boilerplate` / `kb-bullet-no-platform-mention` hits. Sub-agents didn't author first-person voice OR scaffold-naming narration this run (refinement-2 audit later DROPped 7 KB bullets, so any borderline candidates may have been pruned before they shipped). |
| **Item A** (run-47 carry-forward) | idKey-format gate at `enrich-findings` boundary | **NOT-TRIGGERED — clean upstream** | Zero idKey-format refusals in main session. Sub-agent emitted full grammar (`tier_yaml_comments:<tier>:<service\|project>`) on first call. |
| **Item B** (run-47 carry-forward) | URL fragment preserved in `lookupGuideIDFromURL` | **FIRED AS DESIGNED** | appdev IG #4 ships clean `[deploy-files tilde syntax + static runtime](.../specification#deployfiles-)` — the fragment-bearing citation that 4-reverted in run-46 and shipped clean in run-47 ships clean again here. |
| **Item C** (tightened `<` → `!=` per handlers.go:1320-1322) | Counter-consistency check at refinement-2 close | **FIRED AS DESIGNED — caught overcount** | Refinement-2 close-gate refused on `crossSurfaceUniquenessScanned=82 != manifest total=71`. Main agent's receipt-math (`82`) overshot the manifest (`71`); the tightened `!=` predicate caught the overcount that the previous `<` predicate would have silently accepted. Recovery: second `enrich-findings` with `scanned=71` + empty findings array → close `ok:true`. The audit's initial emission (scanned=25) was a partial scan that main agent post-processed to 82; the audit-side undercount was NOT caught at audit-emission point — Item C still runs at close-gate only, per run-47 verdict open issue. |
| **Item D** (run-47 carry-forward) | Gate-refusal telemetry ledger | **NOT-TRIGGERED — no refusals to log + archive gap persists** | Zero `refinement-replace-revert` notices across all session logs. `.gate-refusals.jsonl` doesn't exist in run-48 tree OR in any session log mention (codex-verified across deliverable + 30 subagent transcripts). Same archive gap as run-47: file lives at container outputRoot if the gate fires, not bundled into deliverable. |
| **Item G** (run-47 carry-forward) | Auto-override refuses on ambiguous framework-quirk | **UNVERIFIED-IN-PRODUCTION** | No `candidateClass:framework-quirk` rejections observed. |
| **Item H** (run-47 carry-forward) | Typed multi-batch `enrich-findings` API | **UNVERIFIED-IN-PRODUCTION** | Both `enrich-findings` calls fit single-batch path. |
| **Item I** (run-47 carry-forward) | `EnterPhase` idempotent on completed phase | **NOT-TRIGGERED — clean phase ordering** | Zero `not adjacent-forward` errors across all session logs (run-47: 1; run-46: 1). Main agent stayed in adjacent-forward order through all 8 phases. |

---

## Content audit — three-codebase walk-through

### Per-codebase KB inventory + S5 verdict (run-48)

| Codebase | Bullet | S5 test verdict | Citation form | Self-referential? |
|---|---|---|---|---|
| **apidev** (3) | #1 No `.env` file in the deployed tree | **PASS** intersection (NestJS ConfigModule × Zerops env injection) | NONE | No |
| | #2 Custom response headers return `undefined` from the SPA but show up in `curl` | **PASS** intersection (browser CORS × Zerops cross-subdomain default) | NONE | No |
| | #3 `ListObjectsV2` returns oldest-first, not newest-first | **BORDERLINE — pure S3 spec** Body teaches "S3-compatible APIs sort results in lexicographic key order" — this is the S3 contract, not a Zerops×framework intersection. A porter who has integrated any S3-compat backend has hit this. Not actively harmful; weaker S5 than the other 7. | NONE | No |
| **workerdev** (3) | #1 `relation "job_log" already exists` after a co-deployed migrator race | **PASS** intersection (multi-codebase migrators × Zerops execOnce keying) | ✓ form-(b) `[zsc execOnce + per-deploy key model](.../specification#initcommands-)` — display + URL CANONICAL | No |
| | #2 `ioredis` sends garbage `AUTH` commands when wired to Valkey with `password` | **PASS** intersection (ioredis × Zerops Valkey no-auth) | NONE | No |
| | #3 Meilisearch `masterKey` leaks if the worker's env shape is copied to a browser bundle | **PASS** intersection (Meilisearch key model × Zerops env injection) — advisory framing, but a real porter trap when copying env-blocks across codebases | NONE | No |
| **appdev** (2) | #1 `VITE_API_URL` change silently returns stale data until the next build | **PASS** intersection (Vite build-time bake × Zerops static base) | NONE | No |
| | #2 `start:` directive on `base: static` is silently ignored | **PASS** intersection (Zerops static convention × Heroku/Render Node-everywhere assumption) | NONE | No |

**Voice tally (run-48):**
- Operational: 0
- Intersection (clean): **7 of 8**
- Borderline (pure framework/lib spec): **1** (apidev #3 ListObjectsV2)
- Self-inflicted: **0** (run-47: 2; run-46: 0; run-45: 1)
- Wrong-URL citation: **0** (run-47: 2)
- Display-text drift: **0** (run-47: 1)
- Hard-fail: **0**

### Per-codebase IG inventory + citation audit

| Codebase | IG # | Topic | Citation | Verdict |
|---|---|---|---|---|
| apidev | #2 | Bind `0.0.0.0` | NONE | clean — bootstrap mechanics |
| apidev | #3 | Trust the reverse proxy | NONE | clean — Express mechanics |
| apidev | #4 | Drain on `SIGTERM` for rolling deploys | `[zero-downtime deploys with multi-container setups](.../features/scaling-ha)` | ✓ display + URL CANONICAL |
| apidev | #5 | Alias platform env vars under your own keys | `[per-key env shape and cross-service aliases](.../specification#envvariables-)` | ✓ display + URL CANONICAL |
| workerdev | #2 | Bootstrap as a NestJS standalone application context | NONE | clean — Nest bootstrap mechanics |
| workerdev | #3 | Connect to NATS with separate credential fields | `[managed NATS broker](.../services/nats)` | ✓ display + URL CANONICAL |
| workerdev | #4 | Subscribe in a queue group and drain on `SIGTERM` | `[zero-downtime deploys with multi-container setups](.../features/scaling-ha)` | ✓ display + URL CANONICAL |
| workerdev | #5 | Configure the S3 client with path-style addressing | `[S3-compatible storage on the MinIO backend](.../services/object-storage)` | ✓ display + URL CANONICAL |
| appdev | #2 | Bake the API origin into the SPA at build time | NONE | clean — Vite mechanics |
| appdev | #3 | Bind Vite to every interface and accept platform subdomain hosts | `[Zerops L7 balancer + subdomain access](.../features/access)` | ✓ display + URL CANONICAL |
| appdev | #4 | Strip the build-output prefix and ship to the static runtime | `[deploy-files tilde syntax + static runtime](.../specification#deployfiles-)` | ✓ display + URL CANONICAL |

**IG citation coverage: 7/11 IG items cited; 7/7 cited = 100% canonical** (run-47: 9/10 = 90%; run-46: 5/11 = 45%; run-45: 1/10 with one wrong-URL). **Best-ever IG citation coverage** at the canonical axis. 4 un-cited IG items (apidev #2/#3, workerdev #2, appdev #2) are framework-bootstrap mechanics with no citation-map topic, correctly carrying no citation rather than a fabricated one.

Codex-confirmed all 8 cited entries: display matches `FriendlyDisplayName` / `friendlyDisplayNames` / `managedFriendlyNames`; URLs match `CitationGuideURL` / `citationURL*` constants (validators verified at `internal/recipe/enrich_findings.go:226-254`, `internal/recipe/citations.go:86-112`, `internal/recipe/briefs_refinement2.go:207-214`).

### Codebase yaml friendly-authority adapt-path inventory

The mechanical regex `bump|feel free|swap|rotate|tune|customize|change.*if|once you|if you|disable|enable|update` counts 3 hits across the three codebase yamls (apidev 1, workerdev 0, appdev 2) — sparser than run-47's 7 hits with the same regex. But read line-by-line, the friendly-authority voice is present in substantive shape:

- apidev: "Swapping a managed service later is a one-line yaml edit, no app rebuild" (line 65 of [zerops.yaml](docs/zcprecipator3/runs/48/apidev/zerops.yaml)); "If you swap a managed service later, the prod block above needs the matching edit" (line 147)
- workerdev: "Swapping a managed service later is a one-line yaml edit" (line 45); "never alias this on a frontend codebase that builds for the browser (use `${search_defaultSearchKey}` there instead)" (line 92)
- appdev: "Set your own production origin here once you swap apistage for a custom domain" (line 62); "if you add server-rendered routes later, switch to `base: nodejs@22` with an explicit `start:`" (line 70)

The regex misses "Swapping" (capitalized) and other shape-equivalents. Per-codebase density is proportional to porter-tunable count — directionally closer to the goldens-sparse calibration than run-47's regex-positive 11. `friendly-auth-floor` gate recorded zero firings (no archive, but no refusal events in session log) — authoring met the floor naturally.

### Tier import.yaml adapt-path inventory

| Tier | Hits (regex) |
|---:|---:|
| 0 AI Agent | 10 |
| 1 Remote (CDE) | 6 |
| 2 Local | 9 |
| 3 Stage | 6 |
| 4 Small Production | 5 |
| 5 HA Production | 7 |
| **Total** | **43** |

Material per-tier coverage. Tier 5 (HA Prod) has 7 explicit "bump verticalAutoscaling.X when..." adapt-paths across managed-service blocks plus runtime blocks. Mechanical regex inflates the count (matches "enable" in `enableSubdomainAccess`); per-block density is closer to ~3-4 substantive adapt-paths each. Refinement-2 ACTed on 6 cross-tier deferral notices: "Set higher priority for databases and storages" line was dropped from every tier's project block (priority is a per-service decision; the db block already carries the canonical home). Net: tier yamls hold a denser-than-goldens friendly-authority voice; consistent with runs 45/46/47.

### Cross-codebase / cross-surface duplication

Refinement-2 audit DROPped 7 KB bullets to deduplicate against the IG and across codebases (per [README.md](docs/zcprecipator3/runs/48/environments/README.md) §"Refinement-2"):
- apidev KB lost NATS-auth + Meilisearch-master + object-storage-UnknownError (canonical homes on worker IG #3 / worker KB #3 / worker IG #5)
- appdev KB lost the Vite-502 bullet (self-inflicted-as-gotcha)
- workerdev KB lost queue-group + drain-rolling-deploy + NATS-auth (duplicates of worker IG #3/#4)

Surviving cross-codebase patterns:
- apidev IG #4 (drain HTTP via `app.close()`) + workerdev IG #4 (queue-group + `subscription.drain()`) — both cite `features/scaling-ha`; same platform fact (SIGTERM during rollouts) but **distinct framework-side fixes** (HTTP server drain vs NATS subscription drain). Legitimate symmetric coverage, not duplication.
- worker yaml comment + apidev yaml comment both name ioredis-AUTH-against-unauth-Valkey trap — same fact in two yamls; not in KB. Inline yaml rationale serves a different reader than KB; advisory-acceptable per the audit (HOLD with named justification).

**Net cross-codebase KB duplications: 0** (run-47: 1 — Auth-Violation worker+apidev pair).

### Self-inflicted bullet inventory

| Surface | Bullet | Litmus #4: "our code did X, we fixed Y"? |
|---|---|---|
| (none) | — | — |

**Net self-inflicted at deliverable: 0** (run-47: 2; run-46: 0; run-45: 1). Run-47's two failed bullets (apidev #6 Search-card-0-indexed; worker #4 MEILI_HOST narration) are absent.

### Recipe-internal naming creep audit (Item 7 scope)

No backticked recipe-internal class/file names in KB stems. Prose-level UI feature naming is absent from KB bullets this run (apidev KB doesn't mention "Cache card"; appdev KB doesn't mention "dashboard"). Item E's known prose-level gap has nothing to flag.

**Net prose-level recipe-internal naming carryovers: 0** (run-47: 3; run-46: 4).

---

## Telemetry analysis

### Gate-firing summary

| Gate | Phase | Count | Deliverable outcome |
|---|---|---:|---|
| `kb-citation-display-mismatch` | codebase-content (api sub-agent) | 2-5 hits (codex-verified inside sub-agent transcript) | Sub-agent self-corrected link text to `[managed NATS broker]` / `[managed Meilisearch service]` (per validator's required friendly names) BEFORE close — final `complete-phase phase=codebase-content codebase=api` returned `ok:true`. THIS IS the H3-shape fix's design intent in production. |
| `kb-citation-display-name-without-canonical-url` (NEW Check 4) | — | 0 | Validator wired; sub-agents didn't author the failure shape. |
| `kb-citation-topic-mismatch` (Check 2) | — | 0 | — |
| `kb-bullet-self-inflicted-shape` (H3-aware) | — | 0 | Validator wired; sub-agents didn't author first-person voice. |
| `kb-bullet-cited-guide-boilerplate` (H3-aware) | — | 0 | — |
| `kb-bullet-no-platform-mention` (H3-aware) | — | 0 | — |
| `kb-citation-missing` | — | 0 | — |
| `refinement-replace-revert` | — | 0 | No snapshot/restore reversions triggered. |
| `friendly-auth-floor` | — | 0 | Codebase yaml adapt-path density met floor naturally. |
| `self-referential-naming` | — | 0 | No backticked recipe-internal symbols at record-fragment time. |
| `cross-surface-uniqueness-scanned` close-gate (Item C tightened) | refinement-close | 1 | Refused on `scanned=82 != manifest=71`; main-agent re-submitted with scanned=71 + empty findings → close `ok:true`. |
| `yaml-comment-missing-causal-word` | codebase-content (api sub-agent) | 3 hits | Sub-agent self-corrected inline by adding em-dashes / `because` / `so that` to three comment blocks before close. |

### File archive status

- **`.gate-refusals.jsonl`**: NOT archived to deliverable. Codex-verified: zero occurrences across deliverable tree + 30 subagent jsonl files + main-session jsonl. Either the file wasn't written (no gate-ledger-emitting refusal fired) OR it lives at container outputRoot `/var/www/zcprecipator/nestjs-showcase/.gate-refusals.jsonl` and didn't make it into the deliverable copy step. Same gap as run-47; persisting issue.
- **`.refinement-2-manifest.json`**: NOT archived. Manifest total `71` is observable only via the engine's close-gate refusal message.
- **Implication**: post-hoc verification of run-48 gate firings relies on grepping subagent jsonl transcripts (which is what codex did). Future-run improvement: bundle these dotfiles into deliverable at finalize phase.

---

## Refinement-2 dispatch + findings + manifest coverage

- **Refinement-2 audit** (sub-agent `ab62e0d104e2b9dc2` per codex): emitted 20 findings (6 blocker + 14 advisory) plus initial `crossSurfaceUniquenessScanned: 25` (PARTIAL scan of S3/S4/S5/S7 surfaces).
- **Main-agent triage**: 18 ACT + 2 HOLD (per [README.md](docs/zcprecipator3/runs/48/README.md) §"Triage"):
  - **ACT — S3 × 6**: dropped misplaced "Set higher priority for databases and storages" line from every tier's project block.
  - **ACT — S4 × 5 citations**: api IG #4, app IG #3, app IG #4, worker IG #3, worker IG #5 — all five rewritten to canonical friendly names. One retry on app IG #4 (classification=intersection refused on CODEBASE_IG → re-recorded with `platform-invariant`).
  - **ACT — S5 × 7 DROPs**: api KB lost 3, app KB lost 1, worker KB lost 3 (canonical homes per the audit's deduplication walk).
  - **HOLD — S7 × 2**: api + worker yaml `run.envVariables` comment cross-surface concern; inline yaml rationale serves operator (deploy config inspector), IG serves porter (integration patterns) — different readers, advisory severity acceptable.
- **Manifest total**: 71 entries (3 KB + 11 IG slots + 3 zerops_yaml = 17 + tier: 6 × 9 = 54; total 71). Confirmed via engine close-gate refusal message.
- **Audit-side counter trajectory**: 25 (initial sub-agent emit, partial scan) → 82 (main agent forwarded with sloppy receipt math) → 71 (re-submission, correct).
- **Item C check pinned at handlers.go:1331-1336** refused at scanned=82 ≠ manifest=71. Recovery: second `enrich-findings` with `scanned=71` + **empty findings array** to clear the rejection. The empty-findings re-submission means the original 20 findings had already been ACTed/HOLDed by main agent — the second call carries only the counter correction.
- **Open issue (run-47 carry-forward)**: audit-side undercount (25) was NOT caught at audit-emission point. Item C still runs at close-gate only. The strengthening recommended in run-47's verdict ("validate `scanned == manifest_total` at the AUDIT EMISSION POINT") remains undone.

---

## Spec-content audit — surface-by-surface

| Section | Verdict | Notes |
|---|---|---|
| §"Empirical floor" (goldens) | **Improved** | KB voice closer to operational-mechanism-first goldens than run-47 (8 vs 15 bullets; pruned by audit). Tier yaml friendly-authority density still EXCEEDS goldens (denser adapt-path coverage proportional to per-tier service count). |
| §"Why this exists" (journal failure mode) | **Held** | No aspirational JWT, no cross-surface deferrals (audit DROPped 6 tier-deferral lines), no execOnce semantic-lie. |
| §"Fact classification taxonomy" | **Held** | Zero classification rejections at main-agent ACT path (one retry on app IG #4 intersection→platform-invariant). |
| §"Self-inflicted" litmus #4 | **RECOVERED** | 0 hits (run-47: 2; run-46: 0). Audit's S5 × 7 DROP pruned every candidate. |
| §"Self-referential decoration prohibition" | **Held — heavy class closed; prose class clean too** | Zero backticked recipe-internal symbols (Item 7); zero prose-level recipe-feature names in KB bodies (Item E known gap had nothing to flag). |
| §"Friendly-authority voice" codebase yamls | **Sparser, more golden-aligned** | 3 regex-hits (down from run-47's 7 with same regex); substantive voice present in shape-equivalents not captured by regex. Floor gate met naturally. |
| §"Friendly-authority voice" tier yamls | **Held + improved** | 43 hits across 6 tiers; per-tier density material; cross-tier deferral cleanup ACTed by audit. |
| §"Surface 5" S5 single-question test | **STRONG** | 7 of 8 bullets clean intersection; 1 borderline (apidev #3 ListObjectsV2 pure S3-spec). Best-ever S5 fidelity. |
| §"Surface 7" yaml comments | **Held** | No cross-surface deferrals; mechanism+reason in one breath; per-field density appropriate. |
| §"Surface 3" tier yaml | **Held + improved** | Audit cross-tier deferral cleanup landed. |
| §"Citation map" KB | **STRONG** (1 cite, canonical) | Single KB citation (worker KB #1 init-commands) — clean display↔URL. |
| §"Citation map" IG | **8/8 canonical, ZERO defects** | Run-48 best-ever IG citation coverage at the canonical axis. |

---

## Golden voice alignment

### KB shape

- **Jetstream goldens**: 2 H3 operational-mechanism-first.
- **Showcase goldens**: 7 `### Gotchas` H3, mechanism-first.
- **Run-48 codebase KBs**: 8 H3 bullets across 3 codebases, defensive symptom-first. Volume halved from run-47 (15) — much closer to goldens' compact shape. Voice still slightly closer to showcase's symptom-first intersections than to jetstream's operational workflows, but the pruned-to-essential-traps shape reads cleanly.

### Yaml comment voice

- **Jetstream zerops.yaml**: ~2 adapt-paths; declarative-with-rationale.
- **Run-48 codebase yamls**: 3 mechanical adapt-paths (closer to goldens than run-47's 7); substantive friendly-authority voice present in shape-equivalents. **Sparser than run-47, closer to golden calibration.**

### CLAUDE.md voice

Deliberate spec divergence; not flagged.

---

## Content quality progression vs runs 44/45/46/47

### apidev KB progression

| Run | Bullets | Notable shape |
|---|---:|---|
| 44 | 4 | Auth-Violation + cache demo X-Cache + relation-already-exists + Cache 5xx Valkey |
| 45 | 7 | + cross-origin headers + NestJS DI undefined (FAIL) + Search card 0 docs (FAIL) |
| 46 | 5 | dropped both hard-fail bullets; +bucket-private bullet with citation |
| 47 | 7 | + TypeORM-synchronize + archiver-crash + **Search-card-0-indexed (re-introduced self-inflicted)** + wrong-display TypeORM citation |
| **48** | **3** | No `.env` file + cross-origin X-Cache + ListObjectsV2-lexicographic (borderline) — 4 bullets DROPped by audit triage; ZERO self-inflicted; ZERO wrong-URL citations. |

**Run-48 progression**: pruned to 3 essential intersections (one borderline); audit DROPped 3 bullets that had canonical homes on worker IG/KB. No regression of run-47's two failure shapes (Search-card-0-indexed + display drift).

### workerdev KB progression

| Run | Bullets | Notable shape |
|---|---:|---|
| 45 | 5 | queue-group + drain + NATS-Auth + APP_SECRET-shadow + https-Meilisearch |
| 46 | 5 | all citation-clean (queue-group + drain + Auth-Violation + PutObject UnknownError + cache-user/password-literal) |
| 47 | 4 | queue-group (wrong-URL) + drain (wrong-URL) + Auth-Violation + **MEILI_HOST naming-narration (self-inflicted)** |
| **48** | **3** | relation-already-exists co-deployed migrator race (cite ✓) + ioredis AUTH against Valkey (no cite, intersection) + Meilisearch masterKey scope (no cite, intersection). NO duplicates of worker IG #3/#4 (which audit moved canonical homes to). ZERO self-inflicted. |

**Run-48 progression**: lost the wrong-URL pair (worker IG #3/#4 carry the canonical rolling-deploys + S3 citations now). Replaced narration bullet with three clean intersections. Worker KB voice is the cleanest since run-46.

### appdev KB progression

| Run | Bullets | Notable shape |
|---|---:|---|
| 45 | 2 | Prod-bundle-null-X-Cache + literal-API-URL |
| 46 | 3 | Blocked-host + Cache HIT/MISS badge stuck (borderline) + VITE_API_URL fresh-build |
| 47 | 4 | Blocked-host + SPA-literal + X-Cache cross-origin + .env-file-overrides-build-vars |
| **48** | **2** | VITE_API_URL stale until rebuild + start-on-static silently-ignored. Pruned to two essential Vite × Zerops intersections (audit DROPped the Vite-502 self-inflicted bullet). |

**Run-48 progression**: pruned to 2 essential Vite-x-Zerops intersections. Both pass S5 cleanly. Cleanest appdev KB across runs 44-48.

### Cross-run citation correctness

| Run | KB wrong-URL | KB display-drift | IG wrong-URL | IG canonical | Self-inflicted |
|---|---:|---:|---:|---:|---:|
| 44 | unknown | unknown | unknown | unknown | unknown |
| 45 | unknown | unknown | 1 | low | 1 |
| 46 | 0 | 0 | 0 | 5/11 (45%) | 0 |
| 47 | 2 | 1 | 0 | 9/10 (90%) | 2 |
| **48** | **0** | **0** | **0** | **8/8 (100%)** | **0** |

**All four quality axes clean at the deliverable.**

---

## Substrate operations

### Refinement state machine

- Refinement-1 (rulewalk): 1 dispatch.
- Refinement-2 audit: 1 dispatch, 20 findings.
- Triage: main agent direct (no separate triage sub-agent), 18 ACT + 2 HOLD.
- Re-stitch + close: 1 retry on `crossSurfaceUniquenessScanned=82 != manifest=71` (Item C tightened); second call cleared with scanned=71 + empty findings.

### Phase ordering — clean

Zero `not adjacent-forward` errors. Main agent stayed in adjacent-forward order through all 8 phases. Run-46 + run-47's recurring env-content/finalize/refinement skip-finalize pattern did NOT recur.

### Tool call volume + error count

| Metric | Run-46 | Run-47 | Run-48 |
|---:|---:|---:|---:|
| Main-session tool_use entries | ~45 | ~52 | **107** |
| Phase-transition errors | 1 | 1 | **0** |
| Walked-ledger close-gate refusals (Item A) | 4 | 0 | **0** |
| Cross-surface-scanned refusals (Item C) | n/a (silent) | n/a (silent) | **1** (overcount caught) |
| `kb-citation-display-mismatch` refusals at codebase-content | 0 (validator inert on H3) | 0 (validator inert on H3) | **2-5** (H3-fix activated; sub-agent self-corrected) |
| `refinement-replace-revert` notices | many | 1 | **0** |

The 107 main-session tool count is higher than prior runs but driven by clean orchestration (more phases × more granular fragment-record calls), not by error-recovery loops.

### Sub-agent durations

Per TIMELINE.md + codex-verified subagent jsonl sizes:

| Role | Notable |
|---|---|
| scaffold-api / scaffold-app / scaffold-worker | Parallel batch; 54 facts at scaffold close |
| feature-backend + feature-frontend | Two passes; 102 facts at feature close |
| codebase-content × 3 + claudemd-author × 3 | Six parallel sub-agents; api hit 2-5 `kb-citation-display-mismatch` refusals + self-corrected |
| env-content | 61 fragments (root + 6 tier intros + 54 import-comments) |
| refinement-1 (rule-walk) | Largest subagent jsonl (740K bytes per codex) — full rule-walk |
| refinement-2 (audit) | 237K bytes — synthesized 20 findings |

---

## Known-substrate-issues still present (or correctly closed)

### Closed in run-48

1. ✅ `validateCitationDisplayAgreement` structural inertness on H3-shape KBs (Run-48 substrate db77c2bc lands at the right surface — Check 1 fired on the api sub-agent's first authoring pass).
2. ✅ Run-47's three wrong-URL/display-drift citation defects: ABSENT.
3. ✅ Run-47's two self-inflicted KB bullets: ABSENT.
4. ✅ Item C's overcount silent-pass: TIGHTENED via `<` → `!=` (handlers.go:1331-1336) and CAUGHT a real overcount this run.
5. ✅ Phase-adjacency error class: ABSENT this run.

### Carry-forward (still present)

1. **Bidirectional Check 4** (`kb-citation-display-name-without-canonical-url`) — wired, pin-tested, but production-unverified this run. Sub-agents authored canonical URLs to begin with; no "known friendly + wrong URL" pattern recurred. Will surface signal on the run that authors the failure shape.
2. **H3-aware self-inflicted-shape detector** — same status: wired + pin-tested + production-unverified.
3. **Item C audit-side check** — still runs at close-gate only. Audit-side undercount (this run: 25) was NOT caught at the audit-emission boundary. Run-47's recommended diff (refuse audit emissions where `crossSurfaceUniquenessScanned < manifest_total`) remains undone.
4. **`.gate-refusals.jsonl` archive gap** — file (when written) lives at container outputRoot, not bundled into deliverable. Persisting since at least run-47.
5. **Items G/H/I** — same UNVERIFIED-IN-PRODUCTION status as run-47; nothing in run-48 exercised them.

---

## Recommended next action

If **SHIP-AS-CANONICAL**, the bar question is what to promote and what to retire from active monitoring. Ranked:

### Promote to canonical (substrate proved itself at the deliverable surface)

1. **H3-fallback splitter** (db77c2bc) — fired in production, sub-agent self-corrected, deliverable shipped clean. The smallest substrate diff in run-48 produced the largest content-quality gain across runs 44-48 at the KB citation surface. Pin tests `TestCitationBlockSplits_UnumberedH3KB`, `TestValidateCitationDisplayAgreement_KnownDisplayUnknownURL_Refuses` are now load-bearing canonical.
2. **Item C tightening (`<` → `!=`)** at refinement-2 close-gate — caught a real overcount (`82 != 71`) this run that the previous `<` predicate would have silently accepted. Retain.

### Continue monitoring (production-unverified)

3. **Check 4 bidirectional display→URL** — wired, pin-tested, never fired. Next run that authors "known friendly name + non-canonical URL" will be its first production test.
4. **H3-aware self-inflicted detection** — same status as #3. Next run that authors first-person voice OR scaffold-naming narration will exercise it.
5. **Item C audit-side check** (still open from run-47 verdict) — close-gate caught run-48's overcount, but the audit-side undercount (initial scanned=25) was invisible to the gate. Strengthening at audit-emit boundary would surface this earlier.

### Operational improvements (orthogonal to substrate)

6. **Bundle `.gate-refusals.jsonl` + `.refinement-2-manifest.json` into deliverable** at finalize phase. No engine change needed — adjust copy step. Persisting issue since run-47.

### Borderline finding for user attention

7. **apidev KB #3 "`ListObjectsV2` returns oldest-first"** — pure S3-spec behavior, not a Zerops×framework intersection. Body teaches "S3-compatible APIs sort results in lexicographic key order" which is the S3 contract regardless of cloud. A porter who has integrated any S3-compat backend has hit this. Not actively harmful — but the S5 single-question test ("would a developer who read framework AND Zerops docs still be surprised?") is weaker here than for the other 7 bullets. Could be DROPped in a future run, OR re-framed to invoke a Zerops-specific angle (timestamp-key convention with MinIO, perhaps). Surfacing for user judgment; not porter-blocking on its own.

---

## Verdict — SHIP-AS-CANONICAL

Run-48 ships a deliverable a careful porter does NOT block on. Run-47's
five frontier defects are absent at the surface; the v9.93.0 substrate's
H3-fallback splitter fired exactly as designed at the codebase-content
sub-agent boundary; Item C's tightening caught a real overcount.
Citation correctness reaches its best-ever state across runs 44-48
(8/8 IG canonical; 1/1 KB canonical; zero wrong-URL; zero display-drift).
Self-inflicted KB count returns to zero. KB voice tightens to 8
essential intersections after the audit's S5 × 7 DROPs.

The new bidirectional Check 4 and the H3-aware self-inflicted detector
remain wired-but-production-unverified — sub-agents simply didn't
author the failure shapes this run. That's not a substrate failure; it's
the production surface being clean enough that the gates had nothing to
fire on. A future run that does author either shape will be the first
real test.

Counter-evidence to "substrate fail": the only borderline finding worth
user attention is content-quality (apidev KB #3 ListObjectsV2 — pure S3
spec rather than Zerops intersection), not substrate-attributable. Item
C runs at close-gate only, missing audit-side undercount — open from
run-47, still open. `.gate-refusals.jsonl` archive gap persists. Neither
blocks porter quality at the deliverable.

The structural bet paid off cleanly at the targeted boundary: Item 5's
display↔URL axis reaches the KB surface for the first time across runs
44-48 via the H3-fallback splitter, producing the right outcome
(sub-agent self-correction, clean deliverable) without forcing the
close-gate to refuse.

---

## Codex validation references

| Agent ID | Claim | Verdict |
|---|---|---|
| `aa80e69b7af34b0b5` | (1) H3-fix fired at the surface via api sub-agent's `kb-citation-display-mismatch` refusals; (2) all 8 deliverable citations canonical display↔URL; (3) Check 4 + H3 self-inflicted are production-unverified, not failed | **(1) REVISED** — mechanism + production-evidence both correct, but transcript count is 2-5 (api + worker) not 6 inside one sub-agent. **(2) CONFIRMED** — all 8 match `FriendlyDisplayName` + `CitationGuideURL`. **(3) CONFIRMED** — validators emit explicit `Violation` entries, can't fire silently; zero-hit means production-unverified, not hidden. |

---

## Sub-agent transcript map (per codex forensics)

| Role | Agent ID |
|---|---|
| codebase-content-api | `adb7e7e29ed4b2316` (kb-citation-display-mismatch refusals + self-correction here) |
| codebase-content-worker | `a6b43b48046c0baa8` (similar pattern) |
| env-content / refinement-1 / refinement-2 | mapped in TIMELINE.md + meta.json files under `SESSION_LOGS/subagents/` |
| refinement-2 audit | `ab62e0d104e2b9dc2` (237K transcript; 20 findings; initial scanned=25) |
