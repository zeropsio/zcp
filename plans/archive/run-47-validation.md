# Run-47 validation — seven-item substrate dogfood

> **Headline: ITERATE-TO-48.** Run-47's seven-item substrate closed the
> two run-46 frontier defects exactly as designed — Item A's
> `enrich-findings` idKey-format gate refused the audit's compact
> `tier_yaml_comments:0` keys at the boundary (main-session.jsonl:327)
> and the main agent re-emitted with full `tier:service` grammar; Item
> B's fragment-preserving `lookupGuideIDFromURL` shipped a clean
> `#deployfiles-` cite at appdev/README.md:171 without the 4-revert
> loop run-46 hit. **Zero close-gate retry loops, zero
> `refinement-replace-reverted` notices on legitimate `#deployfiles-`
> citations.** But run-47 surfaced a NEW substrate-frontier defect
> class at the codebase KB surface: `validateCitationDisplayAgreement`
> is structurally inert against H3-shape KB bodies because
> `citationBlockSplits` requires `- **stem**` bullet syntax that the
> authored deliverable doesn't use — every KB section in the
> deliverable splits on `###` H3 headers instead. Three wrong-URL or
> display-drift citations shipped through this gap (worker KB #1 + #2
> point display "zero-downtime deploys with multi-container setups" at
> `zerops-yaml/specification#minrunningcontainers-` instead of
> `features/scaling-ha`; apidev KB #1 display text drifts to "across"
> from the canonical friendly name "with"). Plus three additional
> deliverable defects worth blocking on at the porter-reads-this bar:
> apidev KB #7 "Search-card badge reads `0 indexed`" re-introduces the
> same self-inflicted fact class run-46 correctly DROPped; workerdev
> KB #4 "MEILI_HOST is read first, SEARCH_URL is the fallback"
> narrates an internal naming-drift between codebases without porter
> value. Items D, G, H, I are unverified-in-production for this run
> (Item D fired once on the worker KB revert path, status JSON
> confirms; Items G + H + I had nothing to address). The structural
> bet largely paid off at the audit-boundary; the next frontier is the
> KB surface's citation validator wiring.

---

## Per-item score

| # | Item | Verdict | Surface + telemetry evidence |
|---|---|---|---|
| **A** | idKey-format gate at `enrich-findings` boundary | **FIRED AS DESIGNED** | Main session L327 refused 6 invalid keys with full grammar echo: `enrich-findings: 6 walked-ledger key(s) do not match the manifest idKey grammar. Valid idKey shapes: codebase_ig:<host>:<slot>, codebase_kb:<host>:<index\|empty>, codebase_zerops_yaml:<host>, tier_yaml_comments:<tier>:<service\|project>. Invalid keys received (first 5): [tier_yaml_comments:0 tier_yaml_comments:1 ...]`. Third call (L333) re-emitted walked-length 70 with full grammar — accepted. Zero refinement-close walked-ledger retry loops (run-46 had 4). |
| **B** | URL fragment preserved in `lookupGuideIDFromURL` | **FIRED AS DESIGNED** | Go-reproduced the function: `lookupGuideIDFromURL("...#deployfiles-")` returns `"deploy-files"`. appdev/README.md:171 ships clean `[deploy-files tilde syntax + static runtime](https://docs.zerops.io/zerops-yaml/specification#deployfiles-)` — the cite that 4-reverted in run-46. Zero `kb-citation-display-mismatch` reverts on a fragment-bearing URL that IS in the citation map. |
| **C** | Counter-consistency check at close-gate | **FIRED — but validates self-consistency, not audit truth** | Audit emitted `crossSurfaceUniquenessScanned:11` in transcript; main agent rewrote to `scanned:70` (matching `len(walked)=70`) when forwarding to `enrich-findings` at L333. Item C check `len(walked)==scanned` passed. The gate enforces internal counter equality but doesn't verify the audit actually scanned the full manifest corpus — the 11→70 rewrite collapsed audit-truth-vs-reported-truth into the same number with no signal that the rewrite happened. Codex-confirmed. |
| **D** | Gate-refusal telemetry ledger | **FIRED ONCE** | `.gate-refusals.jsonl` (visible at main-session.jsonl:293 via Bash `ls` + L309-310 via Bash `cat`) carries one entry: `{"ts":"2026-05-14T20:07:59Z","gate":"refinement-replace-revert","phase":"refinement","codebase":"worker","fragment":"codebase/worker/knowledge-base","reason":"post-replace validator surfaced kb-citation-missing on /var/www/workerdev/README.md — fragment reverted to its pre-refinement body. KB mentions \"SIGTERM\" but does not cite zerops_knowledge guide \"rolling-deploys\""}`. Status JSON at refinement complete-phase carries `gateRefusals:{refinement-replace-revert:{refinement:1}}`. **`friendly-auth-floor` and `self-referential-naming` recorded 0 firings**. Codex caveat: the ledger file itself was not archived into the deliverable tree (lives at `/var/www/zcprecipator/nestjs-showcase/`, the container outputRoot — not under `docs/zcprecipator3/runs/47/environments/`), so direct file-read verification was only possible via the in-session `cat` snapshot. |
| **G** | Auto-override refuses on ambiguous framework-quirk | **UNVERIFIED-IN-PRODUCTION (clean upstream)** | `facts.jsonl` carries 1 `candidateClass:framework-quirk` fact (`api-typescript-incremental-disabled`). `plan.json` has 0 `ClassificationAmbiguous` / `AutoOverrideRefused` fields. The fact had no cross-surface evidence triggering ambiguity check — engine never invoked the auto-DROP path. Item G's machinery exists; this run didn't exercise it. |
| **H** | Typed multi-batch `enrich-findings` API | **UNVERIFIED-IN-PRODUCTION (single-batch path)** | All 3 `enrich-findings` calls pass `totalBatches:1`. Audit emission fit the inline cap (4380 bytes — well under) so no need for batch chunking. Item H's accumulator + promotion machinery is wired but not exercised. |
| **I** | `EnterPhase` idempotent on completed phase | **NOT-TRIGGERED — different failure shape than run-46** | Main session L244 STILL shows `phase transition "env-content" → "refinement" not adjacent-forward`. Item I's logic (`workflow.go:194-196`) accepts no-op only when `s.Completed[requestedPhase]` is true — refinement wasn't completed yet. The run-47 case is a different shape than the run-46 motivating case: in run-46 env-content sub-agent self-advanced past finalize, then main agent's refinement call landed where Current was already past env-content. In run-47 main agent simply skipped finalize while still at env-content. Same error class re-emerged in a different shape; Item I had nothing to address. Recovery: main agent pivoted to `enter-phase=finalize` then `enter-phase=refinement`. |

---

## Critical finding — validator structural inertness on H3-shape KBs

Codex-confirmed. `validateCitationDisplayAgreement`
(`internal/recipe/validators_codebase.go:703-770`) calls
`citationBlockSplits` (L785-793) which requires one of two delimiters:

- `boldBulletRE = (?m)^\s*-\s+\*\*[^*]+\*\*` — `- **stem**` bullet shape
- `igHeadingItemRE = (?m)^### \d+\.\s+\S` — `### N. <title>` (numbered)

When the body has neither — which is the case for **every codebase KB in
runs 44/45/46/47 (and presumably earlier)** — `citationBlockSplits`
returns nil and `validateCitationDisplayAgreement` returns early on
`len(blocks) == 0`. The display↔URL and topic agreement checks NEVER
RUN against published KB content.

The deliverable KB shape:

```markdown
<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### TypeORM `synchronize: true` corrupts schema on rolling deploys

`TypeOrmModule.forRoot(...)`...

### NATS boot crashes with `Authorization Violation`

Composing `nats://...`...
```

Neither regex matches `###` (un-numbered H3). The `kb-citation-missing`
check (L154-179) DOES run against the whole KB body (it doesn't need
block-splits) — that's what fired at refinement-replace-revert for the
worker KB SIGTERM-vs-rolling-deploys edge. But display↔URL ↔ topic
agreement — Item 5's main axis — has been silently inert against
published KB content for at least four runs.

Surface evidence the gap shipped:

1. **workerdev/README.md:191** —
   `The Zerops [zero-downtime deploys with multi-container setups](https://docs.zerops.io/zerops-yaml/specification#minrunningcontainers-) reference covers how the rolling-deploys flow keeps that invariant during rollouts`
   - Display friendly name matches `rolling-deploys`
   - URL points at `#minrunningcontainers-` (not in citation map; `lookupGuideIDFromURL` returns `""`)
   - Validator skips on `matchedGuide == ""` (L725-729): "URL doesn't match any known guide — skip; this isn't a citation-map URL"

2. **workerdev/README.md:204** — same pattern, second occurrence.

3. **apidev/README.md:289** (NEW TypeORM bullet) —
   `With [zero-downtime deploys across multi-container setups](https://docs.zerops.io/features/scaling-ha)`
   - URL matches `rolling-deploys` canonical correctly
   - Display says "across multi-container setups" — canonical friendly is "with multi-container setups"
   - Even if `citationBlockSplits` produced blocks here, `validateCitationDisplayAgreement` Check 1 would fire `kb-citation-display-mismatch` on this 1-word drift — but the block-split fails before Check 1 runs.

Codex-verified (a) `lookupGuideIDFromURL` returns "" on the worker URL,
(b) `validateCitationDisplayAgreement` skips on empty matchedGuide,
(c) `kb-citation-missing` doesn't catch the bidirectional case
(known display → wrong URL). The gap is real and distinct from
Item B's fragment-preservation fix.

**This is the smallest substrate diff that would close the largest
class of run-47 defects**: extend `citationBlockSplits` to also match
un-numbered `### <stem>` H3 (KB bullet shape in the actual deliverable),
AND add a bidirectional display→guide→URL check inside
`validateCitationDisplayAgreement` (when display matches a known
`FriendlyDisplayName` but URL doesn't match that guide's canonical,
refuse). One regex addition + one symmetric check.

---

## Content audit — three-codebase walk-through

### Per-codebase KB inventory + spec-test classification (run-47)

| Codebase | Bullet | S5 test verdict | Citation form | Self-referential? |
|---|---|---|---|---|
| **apidev** (7 bullets) | #1 TypeORM `synchronize: true` corrupts schema on rolling deploys | **PASS** intersection (new in run-47) | ✓ form-(b) but display "across" vs friendly "with" — 1-word drift | No |
| | #2 NATS boot crashes with `Authorization Violation` | **PASS** intersection | NONE | No |
| | #3 Object-storage `UnknownError` or 301 on first GetObject | **PASS** intersection | ✓ form-(b) `[S3-compatible storage on the MinIO backend](https://docs.zerops.io/services/object-storage)` | No |
| | #4 `X-Cache` headers are `undefined` from the SPA | **PASS** intersection | NONE | **Partial** body says "Cache card reads X-Cache: HIT\|MISS to label hits vs misses" (prose-level recipe naming) |
| | #5 `target ./dist is missing` archiver crash on deploy | **PASS** intersection (new in run-47) | NONE | No (TS+ephemeral-build-container edge) |
| | #6 Search-card badge reads `0 indexed` after first deploy | **FAIL — self-inflicted** (re-introduced) | ✓ form-(b) `[zsc execOnce per-deploy gate](#initcommands-)` | **YES** Body: "On a fresh deploy, the seed inserts items into Postgres and the index stays empty until the porter manually triggers a re-index. The shipped fix folds the Meilisearch sync into `src/seed.ts`..." — literal "our code did X, we fixed it to do Y". Same fact class as run-45's apidev KB #7 that run-46 correctly DROPped. |
| | #7 `${cache_user}` / `${cache_password}` resolve to literal `${...}` tokens | **PASS** intersection | NONE | No |
| **workerdev** (4 bullets) | #1 Missing `queue` option crashes exactly-once | **PASS** intersection | ✓ form-(b) BUT display says "zero-downtime deploys with multi-container setups" + URL points at `zerops-yaml/specification#minrunningcontainers-` — **wrong URL for the topic**; validator skipped because URL isn't in map | No |
| | #2 Subscription drained mid-handler — use `drain()` | **PASS** intersection | ✓ form-(b) — **same wrong-URL pattern as #1** | No |
| | #3 Worker crashes with `Authorization Violation` after switching to a single NATS URL | **PASS** intersection | NONE | No |
| | #4 `MEILI_HOST` is read first, `SEARCH_URL` is the fallback | **FAIL — scaffold-internal-naming-narration** | NONE | **YES** Body: "The worker reads `process.env.MEILI_HOST ?? process.env.SEARCH_URL`... Feel free to rename to `MEILI_HOST` to match the api codebase's convention; both keys point to the same internal endpoint, so the alignment is cosmetic." — narrates internal naming-drift between codebases. Codex-confirmed not publishable. |
| **appdev** (4 bullets) | #1 `Blocked request. This host is not allowed.` on dev subdomain | **PASS** intersection | NONE | No |
| | #2 SPA ships with literal `${apistage_zeropsSubdomain}` strings in fetch URLs | **PASS** intersection | NONE | No |
| | #3 `fetch` from the SPA cannot read the `X-Cache` response header | **PASS** intersection | NONE | **Partial** body names "dashboard's cache card reads `res.headers.get('X-Cache')`" — prose-level recipe naming |
| | #4 Adding a `.env` file in the repo overrides Zerops-injected build vars | **PASS** intersection (new in run-47) | NONE | No |

**Voice tally** (run-47):
- **Operational**: 0
- **Intersection (clean)**: 12 of 15
- **Borderline (prose-level recipe-internal feature naming — known Item E carry-forward)**: 2 (apidev #4, appdev #3)
- **Wrong-URL citation (Item 5 gap — substrate-attributable, see Critical Finding)**: 2 (worker #1, worker #2)
- **Display-text drift on canonical friendly name (Item 5 gap)**: 1 (apidev #1 — "across" vs "with")
- **Self-inflicted fail**: 2 (apidev #6 Search-card-0-indexed; worker #4 MEILI_HOST naming-narration)

**Net vs run-46 (13 clean + 2 borderline + 0 hard-fail)**: 12 clean + 2
borderline + 3 citation-defective + 2 hard-fail. **3 net-new defects**
(2 wrong-URL citations + 2 self-inflicted bullets, offset by improved
breadth on the clean axis). Hard-fail count regressed from 0 → 2 at the
deliverable.

### Per-codebase IG inventory

| Codebase | IG count | Citations | Form correctness |
|---|---:|---:|---|
| apidev | 4 (#2-#5) | 4 / 4 | ✓ #2 `[Zerops L7 balancer + subdomain access](.../features/access)`; ✓ #3 same; ✓ #4 `[zero-downtime deploys with multi-container setups](.../features/scaling-ha)` — display↔URL clean; ✓ #5 `[per-key env shape and cross-service aliases](.../specification#envvariables-)` |
| workerdev | 3 (#2-#4) | 2 / 3 | ✓ #2 (no cite needed — bootstrap mechanics); ✓ #3 (no cite); ✓ #4 `[zero-downtime deploys with multi-container setups](.../features/scaling-ha)` — display↔URL clean (this is the fix run-47 made to the run-46 wrong-anchor cite) |
| appdev | 3 (#2-#4) | 3 / 3 | ✓ #2 `[Zerops L7 balancer + subdomain access](.../features/access)`; ✓ #3 `[per-key env shape and cross-service aliases](.../specification#envvariables-)`; ✓ #4 `[deploy-files tilde syntax + static runtime](.../specification#deployfiles-)` — Item B's fragment-preserving fix shipped this clean |

**IG citation coverage: 9 of 10 = 90%, ALL CANONICAL** (run-46: 5/11
= 45%; run-45: 1/10 with one wrong-URL). **Item B + run-46 substrate's
display-text axis ship clean at the IG surface.** The gap is the KB
surface — see Critical Finding.

### Codebase yaml friendly-authority adapt-path inventory

Grepping `bump|feel free|swap|rotate|tune|customize|change.*if|once you|if you|disable|enable|update` across each codebase yaml:

| Codebase | Run-43 | Run-44 | Run-45 | Run-46 | **Run-47** |
|---|---:|---:|---:|---:|---:|
| apidev | 3 | 3 | 1 | 5 | **6** |
| appdev | 0 | 4 | 1 | 2 | **3** |
| workerdev | 1 | 0 | 0 | 2 | **2** |
| **Total** | 4 | 7 | 2 | 9 | **11** |

**Material improvement over run-46's recovery.** Item 4
(friendly-auth-floor gate) recorded 0 firings in `.gate-refusals.jsonl`,
so either authoring was clean enough to pass naturally OR the gate's
ratio threshold yielded floors ≤ adapt-path counts. Either way the
surface result is consistent with Item 4's design intent.

### Tier import.yaml adapt-path inventory

| Tier | Hits |
|---:|---:|
| 0 AI Agent | 1 |
| 1 Remote (CDE) | 1 |
| 2 Local | 3 |
| 3 Stage | 5 |
| 4 Small Production | 7 |
| 5 HA Production | 7 |
| **Total** | **24** |

Matches run-46's 22 / run-45's 24 envelope. No regression. Tier-5 has
7 adapt-paths across all managed-service blocks plus the runtime
blocks — material.

### Cross-codebase / cross-surface duplication

`crossSurfaceUniquenessScanned:11` was emitted by the audit. With ~70
manifest entries, an 11-item scan is a 16% sample, not an exhaustive
walk. The audit's `duplicates` array on first inspection covered the
canonical same-key-shadow + NATS-pattern-A + Vite-build-time-bake
trifecta — but cross-surface uniqueness coverage on this run is
**unverifiable** because the audit emitted a tiny scanned count that
the main agent rewrote at the gate boundary. **Item C strengthening
needed**: validate `scanned == manifest_total` AT THE AUDIT
EMISSION POINT (refuse audit emissions where `scanned < manifest_total`),
not just at the close-gate forwarding point.

Cross-codebase KB duplications surviving (read by hand):
- worker KB #1 + apidev KB #1 both teach rolling-deploys mechanics
  (queue-group / TypeORM-on-rolling-deploys) — different angles,
  legitimate symmetric coverage.
- worker KB #3 (`Authorization Violation` after single NATS URL) +
  apidev KB #2 (NATS boot crashes with `Authorization Violation`) —
  same fact, two codebases. **Run-46's audit DROPped this duplication
  pair (canonical at worker KB)**; run-47 ships both again. Audit
  scope at scanned=11 likely missed it.

### Self-inflicted bullet inventory

| Surface | Bullet | Litmus #4: "our code did X, we fixed Y"? |
|---|---|---|
| apidev KB #6 | Search-card badge reads `0 indexed` after the first deploy | **YES — codex-confirmed.** Body: "the seed inserts items into Postgres and the index stays empty until the porter manually triggers a re-index. The shipped fix folds the Meilisearch sync into `src/seed.ts`". Same fact class as run-45's apidev KB #7 (Search-card-0-docs after fresh deploy) that run-46 correctly DROPped. Re-introduced with different framing (`SearchService.onModuleInit` + `ensureIndex` not back-filling vs. run-45's "standalone seed script outside NestJS context"). |
| workerdev KB #4 | `MEILI_HOST` is read first, `SEARCH_URL` is the fallback | **YES — codex-confirmed as scaffold-internal-naming-narration.** Body: "The worker reads `process.env.MEILI_HOST ?? process.env.SEARCH_URL`... Feel free to rename to `MEILI_HOST` to match the api codebase's convention; both keys point to the same internal endpoint, so the alignment is cosmetic." Porter doesn't need to know this. Narration of cross-codebase naming-drift. |

**Net self-inflicted at deliverable: 2** (run-46: 0; run-45: 1). REGRESSION.

### Recipe-internal naming creep audit (Item 7 scope)

| Surface | Token | Item 7 reach? |
|---|---|---|
| apidev KB #4 body | "Cache card reads X-Cache: HIT\|MISS" | NO — prose, not backticked |
| appdev KB #3 body | "dashboard's cache card reads `res.headers.get('X-Cache')`" | NO — prose narrative, backticked tokens are framework symbols not recipe names |
| apidev KB #6 stem + body | "Search-card badge reads `0 indexed`" + `SearchService.onModuleInit()` | NO at stem (prose-level UI feature naming, Item E DROPped); YES at backticked class.method but doesn't fire per Item 7's filename-prefix-correlation rule |
| apidev KB #4 stem | `X-Cache` headers — backticked but it's a framework-canonical HTTP header name, not a recipe-internal symbol | NO — Item 7's correct call |

**Net prose-level recipe-internal naming carryovers: 3** (run-46: 4).
Same Item-E-DROPped class; not porter-blocking per spec, listed for
completeness.

---

## Telemetry analysis (new in run-47)

`.gate-refusals.jsonl` (recovered via Bash `cat` in main-session L309-310):

```jsonl
{"ts":"2026-05-14T20:07:59Z","gate":"refinement-replace-revert","phase":"refinement","codebase":"worker","fragment":"codebase/worker/knowledge-base","reason":"post-replace validator surfaced kb-citation-missing on /var/www/workerdev/README.md — fragment reverted to its pre-refinement body. KB mentions \"SIGTERM\" but does not cite `zerops_knowledge` guide \"rolling-deploys\""}
```

| Gate | Phase | Count | Deliverable outcome |
|---|---|---:|---|
| `refinement-replace-revert` | `refinement` | 1 | worker KB locked at pre-refinement body. The pre-refinement body carries the wrong-URL `#minrunningcontainers-` citations (worker KB #1, #2) — the refinement-1 attempt to refactor the KB body broke `kb-citation-missing` because removing the slug-stem alongside the URL anchor changed the kbBodyCitesGuideURL match. Revert was correct per gate design; the underlying authoring defect (wrong URL on the original bullet) wasn't surfaced because the display↔URL validator is structurally inert against H3-shape KBs. |
| `friendly-auth-floor` | — | 0 | Gate had nothing to address; authoring met the floor naturally (11 hits across 3 yamls, up from run-46's 9). |
| `self-referential-naming` | — | 0 | Gate had nothing to address; sub-agents didn't emit backticked recipe-internal symbols at record-fragment time. Item 7's prose-level gap remains uncaught (per Item E DROP). |

Status JSON at `complete-phase phase=refinement` confirmed the count via
`gateRefusals:{refinement-replace-revert:{refinement:1}}` — Item D's
status-side surfacing fired as designed.

**Item D works**. The deliverable surface defect (worker KB #1, #2
wrong URL) is upstream of the revert path; the revert was the symptom
of a deeper validator wiring gap.

**File not archived to deliverable.** `.gate-refusals.jsonl` lives at
`/var/www/zcprecipator/nestjs-showcase/.gate-refusals.jsonl` (container
outputRoot) and was NOT copied into
`docs/zcprecipator3/runs/47/environments/`. Future runs should
ensure the telemetry ledger is bundled with the deliverable archive so
post-hoc validation can read it directly rather than reconstruct from
session-log `cat` output. Same applies to
`.refinement-2-manifest.json` (54KB, also container-only).

---

## Refinement-2 dispatch + findings + manifest coverage

- **Refinement-2 audit** (sub-agent `ac91d6eb94474f11b`): 244s, ~20
  tool calls, ~28 findings emitted (per TIMELINE.md). Audit emitted
  `crossSurfaceUniquenessScanned: 11` + `duplicates: [...]` shape.
- **No separate refinement-2-triage sub-agent** in run-47 — main agent
  did triage directly. Different orchestration vs run-46 (which had a
  17m42s triage sub-agent doing 30 `record-fragment mode=replace`
  calls). Main-agent triage in run-47 totaled 14 `record-fragment`
  calls (mostly ACT-class on the IG citation findings).
- **Manifest total**: ~70 entries (codebase: 3 KB + 11 IG slots + 3
  zerops_yaml = 17 + tier: 6 tiers × 9 (project+8 services) = 54;
  total 71, walked array L333 has 70).
- **Walked ledger after Item A's correction**: 70 entries with full
  grammar (`tier_yaml_comments:0:project` etc.).
- **scanned=70=walked**: Item C check passed after main-agent rewrite.
- **Audit's actual scanned=11 vs manifest_total=70**: Item C doesn't
  catch this — gate runs after the enrich-findings forwarding point.

---

## Spec-content audit — surface-by-surface

| Section | Verdict | Notes |
|---|---|---|
| §"Empirical floor" (goldens) | **Mixed** | KB voice closer to defensive-symptom-first intersections than to goldens' operational-mechanism-first shape (jetstream Maintenance Mode + Temporary Upscaling). Tier yaml friendly-authority density EXCEEDS goldens — substrate gates dense adapt-paths proportional to porter-tunable count; goldens are sparse. |
| §"Why this exists" (journal failure mode) | **Held** | No aspirational JWT, no cross-surface deferrals, no execOnce semantic-lie. |
| §"Fact classification taxonomy" | **Held** | Zero classification rejections at main-agent ACT path. Engine had 1 framework-quirk fact, no auto-DROP fired (Item G's ambiguity refusal path not exercised). |
| §"Self-inflicted" litmus #4 | **REGRESSED** | apidev KB #6 + worker KB #4 fail litmus. Run-46 shipped 0; run-47 ships 2. |
| §"Self-referential decoration prohibition" | **Held — heavy class closed; prose class still open (Item E)** | Item 7's regex closes backticked class/file names. Prose "Cache card"/"cache panel" carryovers (3 in run-47, 4 in run-46) per Item E's known scope gap. |
| §"Friendly-authority voice" codebase yamls | **Improved** (9→11 hits) | Item 4 firing or clean upstream; surface result matches design. |
| §"Friendly-authority voice" tier yamls | **Held** (24 hits, run-46's 22) | No regression. |
| §"Surface 5" S5 single-question test | **Mixed** | 12 of 15 bullets pass S5 cleanly; 2 fail (self-inflicted); 2 wrong-URL citation pairs slip through validator gap; 1 display-text drift. |
| §"Surface 7" yaml comments | **Held** | No cross-surface deferrals; mechanism+reason in one breath; per-field density across codebase yamls maintained. |
| §"Surface 3" tier yaml | **Held + improved** | Audit triage ACTed on JWT-fabrication framings; tier yaml lead lines now project-secret-shared not aspirational. |
| §"Citation map" KB | **PARTIAL (gap)** | 3 of 15 bullets carry citations; 2 wrong-URL pairs ship undetected (worker KB #1, #2) + 1 display-text drift (apidev KB #1). |
| §"Citation map" IG | **9/10 — 90%, all canonical, zero wrong-URL** | Run-47 best-ever IG citation coverage. Item 5 axis ships clean at IG surface. |

---

## Golden voice alignment

### KB shape

- **Jetstream**: 2 H3 operational-mechanism-first (Maintenance Mode +
  Temporary Upscaling). Workflow-focused.
- **Showcase**: 1 `### Gotchas` H3 with 7 bullets, mechanism-first.
- **Run-47 codebase KBs**: 15 H3 bullets across 3 codebases,
  defensive symptom-first. Same shape as run-46. Voice still closer
  to showcase's defensive intersections than to jetstream's
  operational workflows.

### Yaml comment voice

- **Jetstream zerops.yaml** ~2 adapt-paths; declarative-with-rationale.
- **Showcase zerops.yaml** ~0 explicit adapt-paths.
- **Run-47 codebase yamls** 11 adapt-path hits. **Still exceeds
  goldens.** Substrate-text-vs-goldens-bar mismatch carries forward.

### CLAUDE.md voice

Deliberate spec divergence (settled in run-46). Not flagged.

---

## Content quality progression vs runs 44/45/46

### apidev KB progression

| Run | Bullets | Notable shape |
|---|---:|---|
| 44 | 4 | Auth-Violation + cache demo X-Cache + relation-already-exists + Cache 5xx Valkey |
| 45 | 7 | + cross-origin headers + NestJS DI undefined (FAIL) + **Search card 0 docs (FAIL)** |
| 46 | 5 | dropped both hard-fail bullets; +bucket-private bullet with citation |
| **47** | **7** | + TypeORM-synchronize-on-rolling-deploys (intersection, new) + archiver-crash-incremental-tsbuildinfo (intersection, new) + **Search-card-0-indexed (re-introduced self-inflicted)** |

**Run-47 progression**: +2 intersection bullets (good); re-introduced
the run-45-pattern self-inflicted bullet (regression). Wrong display
"across" on the new TypeORM bullet.

### workerdev KB progression

| Run | Bullets | Notable shape |
|---|---:|---|
| 45 | 5 | queue-group + drain + NATS-Auth + APP_SECRET-shadow + https-Meilisearch |
| 46 | 5 | queue-group (cite ✓) + drain (cite ✓) + Auth-Violation (cite ✓) + PutObject UnknownError (cite ✓) + cache-user/password-literal |
| **47** | **4** | queue-group (wrong-URL cite) + drain (wrong-URL cite) + Auth-Violation + **MEILI_HOST/SEARCH_URL naming-narration (self-inflicted)** |

**Run-47 progression**: lost 2 citation-clean bullets vs run-46
(PutObject UnknownError, cache-user-literal), introduced 1
self-inflicted bullet, and 2 surviving citations carry wrong URLs.
Worst worker KB since run-43.

### appdev KB progression

| Run | Bullets | Notable shape |
|---|---:|---|
| 45 | 2 | Prod-bundle-null-X-Cache + literal-API-URL |
| 46 | 3 | Blocked-host + Cache HIT/MISS badge stuck (borderline) + VITE_API_URL fresh-build |
| **47** | **4** | Blocked-host + SPA-literal-${apistage_zeropsSubdomain} + X-Cache cross-origin + **.env-file-overrides-build-vars (intersection, new)** |

**Run-47 progression**: +1 clean intersection bullet (Vite-honors-.env
trap is Zerops-x-Vite); maintained clean voice. Best appdev KB so far.

---

## Substrate operations

### Refinement state machine

- Refinement-1 (rule-walk): 1 dispatch.
- Refinement-2 audit: 1 dispatch, 244s, ~28 findings emitted.
- **No separate refinement-2-triage sub-agent** — main agent triaged
  directly. Different orchestration vs run-46 (saved ~17m of sub-agent
  wall-clock).

### Phase ordering anomaly

Main session L244 — main agent called `enter-phase phase=refinement`
when current was env-content (skipping finalize). Engine refused with
"phase transition env-content → refinement not adjacent-forward". Main
agent then pivoted to enter-phase=finalize, then refinement. Item I's
completed-phase-noop machinery exists but didn't trigger because
refinement wasn't in Completed. **Same error class as run-46 in a
different shape**.

### Tool call volume + error count

| Metric | Run-46 | Run-47 |
|---:|---:|---:|
| `zerops_recipe` calls (main session) | 45 | 52 |
| Tool errors (main session) | 6 | **14** |
| `enrich-findings` calls | 5 | 3 |
| Walked-ledger close-gate refusals | **4** | **0** |
| `kb-citation-display-mismatch` reverts at refinement-2 | 10+ | 0 |
| `refinement-replace-revert` notices | many | 1 |

Tool errors doubled mostly from non-recipe errors: 6 × worktree-creation
failures during sub-agent dispatch (sub-agent type defaults to worktree
isolation against non-git `/var/www`, recovered by switching to
`general-purpose`), 3 × `record-fragment` line-count refusals on env
import-comments, 1 × `enter-phase` adjacent-forward refusal, 1 ×
`enrich-findings` idKey-format refusal (Item A — good!), 1 × oversized
ls error during diagnostic, 1 × jq error, 1 × `validating arguments`
shape mismatch on enrich-findings findings field.

**Item A's design intent realized: 4 walked-ledger refusals → 0**.

### Sub-agent durations (seconds, estimated from session logs)

| Role | Duration |
|---|---:|
| scaffold-api | 720 (per TIMELINE) |
| scaffold-app | 548 (per TIMELINE) |
| scaffold-worker | 462 (per TIMELINE) |
| features-backend | ~2640 (per TIMELINE 44 min) |
| codebase-content (3 parallel + 3 claudemd-author) | ~420 (per TIMELINE ~7 min) |
| env-content | ~600 (per TIMELINE 10 min) |
| refinement-1 | 426 |
| refinement-2-audit | 244 |

Total sub-agent time roughly comparable to run-46 (~2h on the dispatch
critical path).

---

## Known-substrate-issues still present (or correctly closed)

### Closed in run-47
1. ✅ Walked-ledger idKey-format mismatch class (Item A).
2. ✅ `#deployfiles-` fragment-strip bug (Item B).
3. ✅ Telemetry surface for gate firings (Item D + status JSON
   surfacing).
4. ✅ Walked-ledger close-gate retry loop class (Item A pre-empted).
5. ✅ Worker IG #4 wrong canonical anchor (refinement-2 triage fixed
   it during this run).

### Newly surfaced or persisting
1. **NEW**: `validateCitationDisplayAgreement` structurally inert on
   H3-shape KBs because `citationBlockSplits` requires `- **stem**` or
   `### N. <title>` shapes that the deliverable KBs don't use. Pin
   test: `TestCitationBlockSplits_UnumberedH3KB`.
2. **NEW**: Bidirectional display→guide→URL check missing — when
   display matches a known `FriendlyDisplayName` but URL is unknown to
   the citation map, the validator skips instead of flagging the
   display-URL mismatch. Pin test:
   `TestValidateCitationDisplayAgreement_KnownDisplayUnknownURL_Refuses`.
3. **PERSISTING**: Item C's counter-consistency check enforces
   self-consistency (walked == scanned) but doesn't verify that the
   audit actually scanned the full corpus — the main agent can rewrite
   `scanned` at the gate boundary to satisfy the equality. Pin test:
   `TestEnrichFindings_RefusesScannedBelowManifestTotal`.
4. **PERSISTING**: Item I's completed-phase-noop addresses the run-46
   pattern (env-content sub-agent self-advances) but not the run-47
   pattern (main agent skips finalize). Both are "main agent
   misordered phase advance around env-content/finalize/refinement".
   Could subsume via a broader phase-recovery primitive that accepts
   `enter-phase=refinement` after env-content with an auto-stitch
   through finalize.
5. **PERSISTING**: `.gate-refusals.jsonl` and
   `.refinement-2-manifest.json` not archived into the deliverable
   tree. Future runs should bundle for post-hoc validation.

---

## Recommended next action (ranked by content-quality impact)

1. **(highest impact, lowest cost) Fix `citationBlockSplits` to also
   match un-numbered `### <stem>` H3 — the KB shape every codebase
   actually authors.** Add a `kbHeadingItemRE = (?m)^### \S` fallback
   in `citationBlockSplits` after `igHeadingItemRE`. AND add a
   bidirectional display→guide→URL check inside
   `validateCitationDisplayAgreement`: when display matches a known
   `FriendlyDisplayName` (via reverse lookup over
   `CitationGuideURL`/`FriendlyDisplayName`) but the URL doesn't match
   that guide's canonical, refuse with
   `kb-citation-display-name-without-canonical-url`. This closes the
   3 wrong-citation defects in run-47 (worker KB #1, #2; apidev KB #1)
   in one diff. Pin tests:
   `TestCitationBlockSplits_UnumberedH3KB`,
   `TestValidateCitationDisplayAgreement_KnownDisplayUnknownURL_Refuses`.

2. **Surface `record-fragment` self-inflicted detection at
   codebase-content close**. Both apidev KB #6 (Search-card-0-indexed)
   and worker KB #4 (MEILI_HOST/SEARCH_URL fallback narration) are
   litmus-#4 hits ("our code did X, we fixed it to do Y"). Same
   semantic class as run-45's self-inflicted bullets that run-46
   closed via audit walk. Closing requires either (a) a record-fragment
   linter that scans KB bodies for "the seed inserts...the shipped fix
   folds..." or "the worker reads X ?? Y...feel free to rename" patterns,
   OR (b) require KB bullets to cite a guide OR carry a fact that's
   not classed as `scaffold-internal` (Item G surfaces facts;
   record-fragment should refuse on facts with class `internal`/`naming`).
   Marginal cost; high content-quality impact.

3. **Strengthen Item C to refuse audit emissions where
   `crossSurfaceUniquenessScanned < manifest_total`**. Currently the
   check runs only at close-gate, after main-agent forwarding. Wire it
   at the audit-emit boundary too (record-fragment style) so the audit
   can't silently emit `scanned=11` when manifest has 70 entries.

4. **Bundle `.gate-refusals.jsonl` + `.refinement-2-manifest.json`
   into the deliverable archive** so post-hoc validation can read the
   telemetry directly. No engine change needed — adjust the deliverable
   copy step at finalize phase to include the dotfiles.

5. **Item I refactor — broader phase-recovery primitive**. Accept
   `enter-phase=refinement` after env-content with auto-stitch through
   finalize (or refuse with a "did you forget finalize?" message
   instead of the generic adjacent-forward refusal). Same error class
   re-emerges across run-46 and run-47 in different shapes.

If only one diff lands: **#1** (citationBlockSplits H3 fallback +
bidirectional display→URL check). It closes the headline frontier
defect class on run-47 and brings Item 5's design intent to bear on
KB content for the first time across runs 44-47.

---

## Verdict — ITERATE-TO-48

Run-47 ships a deliverable that a careful porter WOULD block on:
2 wrong-URL citations on the worker KB pointing at an unknown anchor
when they should point at `features/scaling-ha`; 1 display-text drift
on the apidev TypeORM bullet; 2 self-inflicted KB bullets (apidev
Search-card-0-indexed re-introduction; worker MEILI_HOST naming-drift
narration). The substrate's seven items closed run-46's frontier as
designed — A + B + D fired at the right boundary and produced the
right outcomes; C + G + H + I had nothing to address in this run's
production path. **But Item 5's display↔URL axis, the run-46
substrate's main win at the IG surface, is structurally inert at the
KB surface because the regex split shape doesn't match the authored
deliverable shape.** Four runs of validated authoring on a no-op
gate, surfaced only when the KB happened to ship a wrong URL.

The structural bet partly paid off: zero walked-ledger close-gate
retry loops, zero `#deployfiles-` cite reverts, telemetry captured the
one revert that fired. **But run-47 made the IG citation surface
canonical (90% coverage, all clean) while regressing the KB surface
(3 citation defects + 2 self-inflicted bullets).** Net porter
read-quality at the KB surface declined.

Smallest substrate diff to close the headline gap: extend
`citationBlockSplits` to match un-numbered H3 AND add a bidirectional
display→URL check. One regex addition + one symmetric check. With
that diff, run-48 should ship clean KB citations.

Counter-evidence to "substrate fail": the substrate **did** structurally
close the run-46 frontier (A + B + D + the underlying close-gate retry
loop). The next frontier moved one surface over (IG → KB) and one
direction over (URL→guide → display→guide). Iteration on the substrate
is working; run-47 surfaced the next frontier explicitly via direct
deliverable inspection rather than retry-loop signal.

---

## Codex validation references

Three independent codex validations submitted before publication:

| Agent ID | Claim | Verdict |
|---|---|---|
| `ac9c6de6703e39e26` | Worker KB #1/#2 citation defect — display↔URL gap on `#minrunningcontainers-` URL distinct from Item B | **CONFIRMED** — (a) `lookupGuideIDFromURL` returns `""`; (b) `validateCitationDisplayAgreement` skips on empty matchedGuide; (c) no other validator catches the reverse direction. Gap is real and distinct from Item B. |
| `a152ae282626cfc97` | Worker KB #4 (MEILI_HOST/SEARCH_URL fallback) + apidev KB #6 (Search-card-0-indexed) self-inflicted classification | **CONFIRMED for #6, REVISED for #4** — #6 is a clean litmus-#4 hit (same as run-46-DROPped class). #4 is more precisely a scaffold-naming-convention narration than pure "our code did X, we fixed Y" — but the conclusion holds: doesn't belong in published KB. |
| `a5a57d67e2a6b5924` | Per-item verdicts on A-I, including Item I "different shape than run-46" framing | **CONFIRMED A/B/C/G/H/I; REVISED D** — D's status-JSON evidence confirmed; the ledger-file claim is indirect (file wasn't archived to deliverable; only in-session `cat` snapshot). |

---

## Sub-agent transcript map

| Role | Agent ID |
|---|---|
| scaffold-api | acce7a35e6282f7fb |
| scaffold-api (test) | a56ae732b688adbb5 |
| scaffold-app | afb3334c38b4c5e1c |
| scaffold-worker | a024b1c52a8db01e3 |
| features-backend | afbf424e0456e3d77 |
| codebase-content-api | acf2473e5e81a7035 |
| codebase-content-app | a42da8b96d72f5eee |
| codebase-content-worker | abbbd1ddec0befdc5 |
| claudemd-author-api | a02551035fc940125 |
| claudemd-author-app | af5743e6788d94482 |
| claudemd-author-worker | aaba49b0d8d06b902 |
| env-content | a775e20730cc1ec8f |
| refinement-1 | a75627a2a059c2c03 |
| refinement-2-audit | ac91d6eb94474f11b |
| refinement-2-triage | (none — main agent triaged directly) |
