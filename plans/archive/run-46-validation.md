# Run-46 validation — seven-item structural substrate dogfood

> **Headline: ITERATE-TO-47.** Reading run-46 as a porter against the
> goldens: **the seven-item structural substrate moved real authoring
> behavior at the codebase surfaces where run-45 most needed it**, but
> the gating machinery at refinement-close exposed a new failure-mode
> class (idKey format-mismatch loop) that turned a clean Pillar-C
> deliverable into a four-refusal retry cycle. Substrate-attributable
> wins at the porter-facing surface: **both run-45 hard-fail apidev KB
> bullets are gone** (the NestJS DI circular-import framework-quirk and
> the Search-card-0-docs self-inflicted entry); the wrong-URL form-(b)
> citation on apidev IG #4 is corrected (display "zero-downtime deploys
> with multi-container setups" + URL `docs.zerops.io/features/scaling-ha`
> — Item 5's display↔URL axis ships clean); **IG citation coverage
> moved 1/10 → 4/10 with zero wrong-URL** (apidev IG #4 scaling-ha,
> worker IG #2 scaling-ha, worker IG #3 services/nats, appdev IG #3
> features/env-variables); **codebase yaml friendly-authority adapt-
> path hits recovered 2 → 9 across three yamls** (apidev 5, appdev 2,
> workerdev 2 — matching run-44's distribution depth); **20 audit
> findings vs run-45's 18, spanning all four in-scope surfaces** (S3=4
> / S4=7 / S5=8 / S7=1) vs run-45's 18-all-on-S3 — Item 1's stated
> intent (force the walk on codebase surfaces) was satisfied at the
> audit-emit path.
>
> Substrate-frontier weaknesses surfaced in this run:
>
> 1. **Item 1's walked-ledger gate is bypassable by iterative empty-
>    findings retries** — the refinement-close gate refused 4 times
>    (main-session.jsonl L266/277/284/293) for "walked-ledger covers X
>    of 71 manifest entries" then accepted on the 5th retry. Codex-
>    verified `internal/recipe/handlers.go:735` shows the ledger is
>    latest-wins (`sess.Refinement2Ledger = &Refinement2Ledger{Walked:
>    append([]string(nil), in.Walked...)}`), not monotonic — so each
>    retry's walked array overwrites the prior. The main agent never
>    re-dispatched the sub-agent; it iterated `enrich-findings
>    findings:[]` + `complete-phase` until un-walked dropped to 0. The
>    sub-agent emitted free-form ledger idKeys (`S5:codebase/api/
>    knowledge-base/<name>`); manifest idKeys are
>    `codebase_kb:<host>:<empty>` / `codebase_ig:<host>:<slot>` /
>    `codebase_zerops_yaml:<host>` / `tier_yaml_comments:<N>:<key>`.
>    Format mismatch caused 71 manifest entries to read as un-walked
>    even though the audit had actually walked them. Gate fired as
>    designed; gate's "structural fix" intent (sub-agent walks the
>    manifest) was not realized — sub-agent ran `head -50` + `grep -c`
>    on the manifest file, never `Read` it, never learned its idKey
>    format.
>
> 2. **`crossSurfaceUniquenessReport` shape regression** — audit
>    emitted a flat `crossSurfaceUniquenessScanned: 95` (an int), not
>    the spec'd `{scanned, duplicates_found, duplicates_resolved}`
>    object. Item 6's close-gate accepted the flat int; the engine
>    never reported the uniqueness pass as the gating reason during
>    the 4-refusal loop. Audit's own counters disagree internally:
>    `walked=92 entries` vs `scanned=95`. Walked decomposition (60 S3
>    + 14 S4 + 15 S5 + 3 S7) sums to 92; the closing prose narrated
>    "95 items" with no reconciliation.
>
> 3. **Real engine validator bug surfaced by Item 5** — `internal/
>    recipe/validators_codebase.go::lookupGuideIDFromURL` strips URL
>    fragments before path-matching (codex-verified at L615–623),
>    collapsing `#initcommands-`, `#deployfiles-`, `#envvariables-`
>    into whichever guide's canonical-path entry matches the bare
>    `docs.zerops.io/zerops-yaml/specification` path first. Triage
>    finding #15 (appdev IG #4 deploy-files cite) hit this 4 times
>    via successive `refinement-replace-reverted` notices then HELD
>    with cited rationale. End-state deliverable: no `#deployfiles-`
>    cite ships on appdev IG #4; substrate worked around the bug.
>    This is a real Item-5-attributable validator defect, not a
>    tradeoff.
>
> 4. **Items 3 / 4 / 7 are unverified-in-production at the run-46
>    surface** — Item 3 (snapshot/restore for env/import-comments
>    fragments): zero `refinement-replace-reverted` notices on any
>    `env/<N>/import-comments/*` fragment. Triage S3 ACTs all landed
>    clean upstream. Item 4 (friendly-authority gate, proportional
>    floor): zero complete-phase codebase-content refusals citing
>    `friendly-auth-floor-below-tunable-count`. Either authoring was
>    clean enough to pass or the codebase yamls' porter-tunable counts
>    yielded floors ≤ adapt-path counts. Item 7 (self-referential-
>    naming linter at record-fragment): zero `record-fragment`
>    refusals citing the linter at the main-agent surface. Codex-
>    verified Item 7's regex would catch `cache.module.ts` (suffix
>    match) and `SearchIndexer` (export match) but NOT
>    `ItemsService.onModuleInit` (dotted-class requires filename
>    prefix correlation `items.service.ts` doesn't supply) and NOT
>    prose tokens like "cache demo" / "Cache panel" / "Cache card"
>    (linter scans backticked-only). Run-46 deliverable still names
>    `"The Cache card's load-bearing demonstration"` (appdev KB #2
>    line 158) and `"cache panel renders MISS forever"` (apidev KB
>    #2 line 289) — Item 7's regex doesn't reach prose-level recipe-
>    internal feature naming. Codex framing on absence of evidence:
>    structurally expected (Item 4 fires at codebase-content close
>    returning errors to the invoking agent, not main-session; Item
>    7 fires at record-fragment returning `r.Error` in-band to the
>    sub-agent which then internally fixes and reports clean).
>    Unverified ≠ broken, but the run didn't exercise these gates
>    visibly.
>
> 5. **`zerops_recipe` tool call volume + failure cluster at
>    refinement-close** is the run's UX-cost signal. Main-session
>    carries 45 total `zerops_recipe` calls; 10 (22%) sit in the
>    refinement-close retry loop (4 × `enrich-findings findings:[]` +
>    4 × refused `complete-phase` + 1 final close). Six tool errors
>    visible: 1 phase-adjacency mis-step (`enter-phase
>    phase=refinement` when env-content had already self-advanced to
>    finalize-expected), 4 walked-ledger refusals, 1 `Read` of an
>    oversized tool-results JSON. The refinement-2-audit sub-agent
>    output (58.7 KB, 20 findings + 92-entry walked-ledger + 95-int
>    scan + 5-duplicate report) overflowed the inline cap, requiring
>    Python re-extraction from the persisted tool-results file. The
>    refinement-2-triage sub-agent ran 131 tool calls in 17m42s.
>
> 6. **Concerning behavior**: refinement-1-rulewalk sub-agent ran
>    `Bash: find / -path "*/jetstream*" -o -path
>    "*/laravel-showcase*"` searching for the golden recipes on the
>    container filesystem (agent transcript `a16657c1e4c662ebd.jsonl
>    :101`). The substrate brief embeds golden anchors via
>    `part-3-rules-from-goldens.md`; the sub-agent shouldn't need
>    disk-search for them. Not a deliverable defect; a sub-agent-
>    behavior signal worth attention. (User-reported attribution was
>    refinement-2; transcript-verified it was refinement-1.)
>
> 7. **CLAUDE.md voice — deliberate divergence from goldens (spec
>    wins)**. Run-46 codebase CLAUDE.md files are Zerops-free `/init`-
>    shape (api + worker + app all carry framework intro + Build & run
>    + per-file Architecture only). Goldens (`/Users/fxck/www/laravel-
>    jetstream-app/CLAUDE.md` + `/laravel-showcase-app/CLAUDE.md`) are
>    Zerops-AWARE: service-facts blocks, dev-mode contracts, `"use zcp
>    MCP not zcli"` directives. **Goldens aren't perfect on every
>    dimension** — the CLAUDE.md case is one where the spec
>    deliberately diverges (repo-local, not published with the recipe;
>    refinement effort wasted on a file porters won't see). Substrate
>    Item 1 explicitly marks S6 out-of-scope. The divergence is
>    intentional, not a defect. Tracking here only because the
>    divergence is worth being explicit about, not because it needs
>    fixing.
>
> **The structural bet partly paid off.** Run-45's specific
> deliverable defects are absent or addressed: framework-quirk DROPped,
> self-inflicted DROPped, wrong-URL citation corrected, codebase yaml
> adapt-paths recovered, audit walked all four surfaces. **But** the
> run shipped through a 4-refusal retry loop on a substrate-introduced
> gate, and three of seven items are unverified-in-production. Porter
> reading the deliverable would not block on any single finding —
> meets the reframed run-46 bar at the surface — but the engine
> trajectory shows a substrate-frontier shift, not a closed loop.
>
> **Recommendation: ITERATE-TO-47.** Two smallest substrate diffs that
> would close the remaining gaps:
> (a) Wire walked-ledger idKey-format validation INTO `enrich-findings`
>     (refuse the call when `walked[i]` doesn't match the manifest's
>     idKey grammar) so format-mismatch fails fast at the sub-agent
>     boundary, not at close 4 retries later. This closes Item 1's
>     "integration-level format-mismatch" failure class.
> (b) Fix the `lookupGuideIDFromURL` fragment-strip in
>     `validators_codebase.go:615–623` — preserve `#initcommands-` /
>     `#deployfiles-` / `#envvariables-` anchors in the canonical-URL
>     match. This closes the legitimate-deploy-files-cite refusal
>     class Item 5 surfaced (4 reverts before triage HELD).

---

## Per-item score

| # | Item | Verdict | Surface evidence |
|---|---|---|---|
| **1** | Surface manifest + walked-ledger receipt | **PARTIAL — concept fired, integration failed** | Engine wrote `.refinement-2-manifest.json` at session OutputRoot before dispatch (`handlers.go:506-516`); brief instructed "Read this FIRST" (`briefs_refinement2.go:99-103`). Audit emitted 20 findings spanning all 4 in-scope surfaces (S3=4 / S4=7 / S5=8 / S7=1) vs run-45's 18-all-on-S3. BUT sub-agent only ran `head -50` + `grep -c "topicCandidates"` on the manifest — never `Read` the full file (refinement-2-audit transcript `agent-a21d883ae5d76e13a.jsonl`). Walked-ledger emitted free-form idKeys (`S5:codebase/api/knowledge-base/<name>`); manifest idKey format is `codebase_kb:<host>:<empty>` / `codebase_ig:<host>:<slot>` / `codebase_zerops_yaml:<host>` / `tier_yaml_comments:<N>:<key>`. Close-gate refused 4 times (`main-session.jsonl:266/277/284/293`); main agent iterated `enrich-findings findings:[]` to bypass. Walked over-count of 92 vs manifest 71 + 71 un-walked entries on first refusal proves format incompatibility coexists with un-walked manifest IDs. Codex (`ae1bf60ef79e77e2f`) verified ledger is latest-wins (`handlers.go:735`), not monotonic — each retry overwrites the prior; bypass came from main agent re-emitting a different walked array on each retry, not from empty-findings retries accumulating. |
| **3** | Snapshot/restore wrapper for env/import-comments fragments | **UNVERIFIED IN PRODUCTION** | Zero `refinement-replace-reverted` notices on any `env/<N>/import-comments/*` fragment across main-session + sub-agent transcripts. Triage finding #16 (priority-rationale duplicate strip across env/0..5), #17 (env/3-5 api block lead reordering), #18 (env/0..1 api `zsc noop --silent` literal cleanup), #19 (env/4..5 broker subject + queue-group leak), #20 (S7 api+worker zerops-yaml `zsc noop` dev comment) all ACTed without revert notices. Wrapper might be wired correctly but had nothing to revert in this run. Code path itself codex-spot-checkable in `snapshot.go:48`. |
| **4** | F-FRIENDLY-AUTH gate at codebase-content close, proportional floor | **UNVERIFIED IN PRODUCTION (clean upstream authoring)** | Zero complete-phase codebase-content refusals in main-session.jsonl citing `friendly-auth-floor-below-tunable-count`. Deliverable yaml adapt-path counts: apidev 5 / appdev 2 / workerdev 2 = total 9 across 3 yamls (vs run-45's 6 / run-44's 9 / run-43's 4). Codebase-content sub-agents may have produced authoring above the gate threshold naturally. Codex framing: gate fires returning errors only to invoking codebase-content agent at `gates.go:175`; refusals could have fired in run-46 without main-session traces. The recovered yaml adapt-path density is consistent with EITHER "gate fired and forced corrections during codebase-content authoring" OR "authoring was clean enough" — surface evidence doesn't distinguish. |
| **5** | G1 citation validator: display↔URL↔topic agreement | **FIRED HARD AT TRIAGE-REPLACE PATH** | 10+ `refinement-replace-reverted` notices in refinement-2-triage transcript (`agent-a5008393f3fd78136.jsonl`). Sample at line 3280: `"post-replace validator surfaced kb-citation-display-mismatch on /var/www/apidev/README.md — fragment reverted to its pre-refinement body. citation display text \"deployment lifecycle\" does not match the friendly name \"zero-downtime deploys with multi-container setups\" for the URL's matched guide \"rolling-deploys\""`. Surfaced REAL engine validator bug — codex (`ae1bf60ef79e77e2f`) verified at `validators_codebase.go:615-623`: `lookupGuideIDFromURL` strips fragment before path-match, collapsing `#initcommands-` / `#deployfiles-` / `#envvariables-` into one canonical-path entry. Triage HOLD'd finding #15 (appdev IG #4 deploy-files cite). End-state: clean cites ship, 4-revert loop on the legitimate deploy-files cite. **Item 5 is REAL but its proximate effect on run-46 was citation correctness + one real validator bug surfaced.** |
| **6** | Cross-codebase uniqueness pass receipt at close | **PARTIAL — shape regression** | Audit emitted `crossSurfaceUniquenessScanned: 95` (a flat int) + a `duplicates:[]` array with 5 entries (nats-pattern-a, s3-force-path-style+MinIO, vite-build-time-bake, cors-exposed-headers-x-cache, same-key-shadow-trap). Spec wants `crossSurfaceUniquenessReport: {scanned, duplicates_found, duplicates_resolved_in_findings}`. The flat-int + sibling-array shape passed the close gate. `scanned=95 vs walked=92` mismatch — audit's own counters disagree. 2 of 5 duplicates landed DROPs (same-key shadow + storage-recent-alphabetical, finding pairs). 3 of 5 landed HOLD. **Item 6 fired** (audit emitted the report shape; engine accepted it) **but with weaker semantics than designed** (flat int vs object; no `duplicates_resolved` ledger). |
| **7** | Self-referential-naming linter at record-fragment for codebase KB | **UNVERIFIED IN PRODUCTION + INCOMPLETE REGEX COVERAGE** | Zero `record-fragment` refusals citing the linter in main-session or any sampled sub-agent transcript. Run-45's specific recipe-internal-naming KB bullets (`cache.module.ts`, `CacheService`, `CacheController`, `REDIS_CLIENT`, `SearchIndexer`) are absent from run-46 deliverable. Codex verified at `self_referential_linter.go:42-55,75,437,452`: nest-module-suffix list covers `.module.ts` / `.service.ts` / `.controller.ts` / `.tokens.ts` / etc.; `backtickedTokenRE` scans backticked-only; export-symbol detection covers `SearchIndexer`. Run-46 deliverable STILL has prose-level recipe-internal feature naming: apidev KB #2 line 289 `"cache panel renders MISS forever"`, appdev KB #2 line 158 `"The Cache card's load-bearing demonstration"` — outside Item 7's regex (not backticked, not class names). Run-45's `"ItemsService.onModuleInit"` would also escape (dotted-class requires filename-prefix correlation that `items.service.ts` doesn't supply). Item 7 closes the heavy run-45 class (backticked file/class names) but leaves prose-level recipe-internal feature-naming creep open. |

S6 (CLAUDE.md) contract update: pin-test (`briefs_rendered_substrate_pin_test.go`) extended per substrate plan; CLAUDE.md is formally out-of-scope for refinement-2 audit. Deliverable CLAUDE.mds reflect `/init`-shape (Zerops-free) per spec. **Spec-vs-goldens divergence flagged**: goldens are Zerops-aware. Decision needed.

---

## What the user reported — substantiated

### "Extreme amount of `zerops_recipe` tool calls"

Main-session.jsonl carries **45 total `zerops_recipe` tool calls**. Breakdown:

| Action | Count |
|---|---:|
| `complete-phase` | 13 (incl. 4 refusals in refinement-close retry loop) |
| `build-subagent-prompt` | 17 |
| `enter-phase` | 7 |
| `enrich-findings` | 5 |
| `update-plan` | 2 |
| `stitch-content` | 2 |
| `emit-yaml` | 1 |
| `status` | 1 |
| `start` | 1 |

Refinement-2-triage made an additional **40 `zerops_recipe` calls** (30 × `record-fragment mode=replace`, 8 × `complete-phase`, 2 × `stitch-content`, 1 × `status`) in its 17m42s span. Combined main + triage: ≈85 `zerops_recipe` calls. The refinement-close retry loop alone is 22% of main-session's volume.

### "A lot of recipe tool call fails"

Six tool errors in main-session:
- 1 × `enter-phase phase=refinement` after env-content self-advanced to finalize-position (adjacent-forward refusal).
- 4 × `complete-phase phase=refinement` walked-ledger refusals (`main-session.jsonl:266/277/284/293`).
- 1 × `Read` of oversized tool-results JSON (26140 tokens > 25000 limit).

Plus refinement-2-triage's 4× `kb-citation-display-mismatch` reverts on `codebase/app/integration-guide/4` (engine validator bug surfaced — see Item 5).

### "Tried looking for jetstream recipe in one of the refinements"

User attributed to refinement-2; transcript shows it was **refinement-1-rulewalk**
(`agent-a16657c1e4c662ebd.jsonl:101`): `Bash: find / -path "*/jetstream*" -o -path "*/laravel-showcase*" 2>/dev/null | grep -v proc | head -20` description: `"Find golden recipe directories"`. Followed by `ToolSearch select:mcp__zerops__zerops_knowledge`. The sub-agent attempted to source the golden corpus externally instead of trusting the embedded `part-3-rules-from-goldens.md` substrate. The refinement-1 verdict text acknowledges the constraint: `"I attempted complete-phase as instructed; the engine refused with a clear gate explanation. My refinement-1 work is done."` Behavior signal, not a deliverable defect.

---

## Content audit — three-codebase walk-through

### Per-codebase KB inventory + spec-test classification (run-46)

| Codebase | Bullet | S5 test verdict | Citation form | Self-referential? |
|---|---|---|---|---|
| **apidev** (5 bullets) | #1 NATS `Authorization Violation` on first CONNECT | **PASS** intersection | NONE | No |
| | #2 Cross-origin fetch returns `undefined` from `response.headers.get('X-Cache')` | **PASS** intersection | NONE | **Partial** body still names "Valkey cache demo emits" + "cache panel renders MISS forever" |
| | #3 Object-storage requests return 403 / `UnknownError` | **PASS** intersection | ✓ form-(b) `[S3-compatible storage on Zerops MinIO backend](https://docs.zerops.io/services/object-storage)` | No |
| | #4 Buckets private by default — direct URLs 403 | **PASS** intersection (NEW bullet) | ✓ form-(b) `[S3-compatible storage on Zerops MinIO backend](https://docs.zerops.io/services/object-storage)` | No |
| | #5 Search ranks UUIDs ahead of titles (searchableAttributes default) | **PASS** intersection | NONE | **Borderline** body names "the welcome row whose title is 'Valkey cache'" (demo-data reference) |
| **workerdev** (5 bullets) | #1 Missing queue-group crashes exactly-once | **PASS** intersection | ✓ form-(b) `[zero-downtime deploys with multi-container setups](https://docs.zerops.io/features/scaling-ha)` | No |
| | #2 In-flight NATS messages dropped on SIGTERM unless `subscription.drain()` runs | **PASS** intersection | ✓ form-(b) `[zero-downtime deploys with multi-container setups](https://docs.zerops.io/features/scaling-ha)` | No |
| | #3 `Authorization Violation` crash on first NATS CONNECT | **PASS** intersection | ✓ form-(b) `[managed NATS broker](https://docs.zerops.io/services/nats)` | No |
| | #4 First S3 `PutObject` returns `UnknownError` | **PASS** intersection | ✓ form-(b) `[S3-compatible storage on Zerops MinIO backend](https://docs.zerops.io/services/object-storage)` | No |
| | #5 `cache_user` and `cache_password` resolve to literal `${...}` strings | **PASS** intersection | NONE | No |
| **appdev** (3 bullets) | #1 `Blocked request. This host is not allowed.` on dev subdomain | **PASS** intersection (borderline Vite-framework × Zerops-dynamic-host edge) | NONE | No |
| | #2 Cache HIT/MISS badge stuck on `—` | **PASS** intersection | NONE | **Partial** body names "Cache card's load-bearing demonstration" |
| | #3 Bumping `VITE_API_URL` requires a fresh build | **PASS** intersection | NONE | No |

**Voice tally** (run-46):
- **Operational**: 0
- **Intersection (clean)**: 11 of 13
- **Borderline (intersection with prose-level recipe-internal feature naming)**: 2 (apidev #2, appdev #2 — both name "Cache card"/"cache panel" in body; outside Item 7's backticked-only regex)
- **Framework-quirk fail**: 0 (vs run-45's 1)
- **Self-inflicted fail**: 0 (vs run-45's 1)

### Per-codebase IG inventory

| Codebase | IG count | Citations | Form correctness |
|---|---:|---:|---|
| apidev | 4 (#2-#5) | 1 / 4 | ✓ #4 `[zero-downtime deploys with multi-container setups](https://docs.zerops.io/features/scaling-ha)` — display matches friendly name, URL canonical for rolling-deploys |
| workerdev | 4 (#2-#5) | 3 / 4 | ✓ #2 scaling-ha; ✓ #3 services/nats; ✓ #5 cleared via refinement-1 reframe |
| appdev | 3 (#2-#4) | 1 / 3 | ✓ #3 `[Zerops environment variables](https://docs.zerops.io/features/env-variables)` |

**IG citation coverage: 5 of 11 = 45%** (run-45: 1/10 with one wrong-URL form-(b); run-44: 0/13; run-43: 0/13). **All 5 citations carry correct display↔URL↔topic agreement.** Item 5's axis ships clean.

### Codebase yaml friendly-authority adapt-path inventory

| Codebase | Run-43 | Run-44 | Run-45 | **Run-46** |
|---|---:|---:|---:|---:|
| apidev | 3 | 3 | 1 | **5** |
| appdev | 0 | 4 | 1 | **2** |
| workerdev | 1 | 0 | 0 | **2** |
| **Total** | 4 | 7 | 2 | **9** |

Material recovery on the run-44 substrate win that run-45 lost. Item 4's gate either fired silently during codebase-content authoring OR the codebase-content sub-agents authored clean enough to pass naturally. Surface result matches Item 4's design intent.

### Tier import.yaml adapt-path inventory (grep `bump|feel free|swap|rotate|tune|customize|change.*if|once you|if you`)

| Tier | Hits |
|---:|---:|
| 0 AI Agent | 1 |
| 1 Remote (CDE) | 1 |
| 2 Local | 3 |
| 3 Stage | 6 |
| 4 Small Production | 5 |
| 5 HA Production | 6 |
| **Total** | **22** |

vs run-45's 24. Same shape. Tier 5 carries 6 adapt-paths — material across all managed-service blocks, no regression.

### Cross-codebase / cross-surface duplication

Audit's `duplicates` array enumerated 5 cross-codebase duplicate cases:
1. **nats-pattern-a-separate-credentials**: S4 worker IG #3 + S5 worker KB Auth Violation + S5 api KB NATS-connect-rejects + S7 api+worker yaml NATS_HOST blocks → audit recommended DROP apidev KB NATS Auth bullet (canonical at worker KB). Triage ACTed.
2. **s3-force-path-style + MinIO endpoint**: HOLD recommended — both KBs needed (worker `PutObject` UnknownError, api `403 / UnknownError`). Triage held.
3. **vite-build-time-bake**: HOLD — IG owns mechanism, KB owns post-deploy symptom.
4. **cors-exposed-headers-x-cache**: HOLD — symmetric KB pair across api/app acceptable.
5. **same-key-shadow-trap**: DROP S4 api IG #5 + S4 worker IG #5 recommended. Triage ACTed for api IG #5; worker IG #5 HELD because the trap is taught at the canonical site (worker zerops.yaml:38-45) per refinement-1's reframe.

**Cross-codebase KB duplications surviving**: at first read I couldn't find ones the audit missed. Run-45's two carry-forwards (APP_SECRET shadow apidev IG#5+worker KB#4; https-Meilisearch apidev KB#3+worker KB#5) are addressed: apidev IG #5 redux is the same-key shadow with the canonical site at worker zerops.yaml; apidev no longer has a Meilisearch-TLS KB bullet (it's gone in run-46 apidev's 5-bullet set). Cross-surface uniqueness held.

### Self-inflicted bullet inventory (porter-following-IG#1 test)

Walked every KB bullet in run-46 with the litmus *"Could this observation be summarized as 'our code did X, we fixed it to do Y'?"*:
- apidev #1-#5: all platform-trap or Zerops-managed-service edges. None self-inflicted.
- workerdev #1-#5: all platform-anchor edges. None self-inflicted.
- appdev #1-#3: Vite-Zerops intersections + build-time bake mechanism. None self-inflicted.

**Self-inflicted bullets at deliverable: zero**. Run-45's apidev KB #7 (Search-card-0-docs, recipe-specific seed.js + SearchIndexer interaction) is DROPped.

### Recipe-internal naming creep audit

| Surface | Item | Recipe-internal symbol | Item 7 reach? |
|---|---|---|---|
| apidev KB #2 body | "cache panel renders MISS forever" | prose "cache panel" | NO — prose, not backticked |
| apidev KB #2 body | "the Valkey cache demo emits" | prose "cache demo" | NO — prose, not backticked |
| apidev KB #5 body | "the welcome row whose title is 'Valkey cache'" | demo-data reference | NO — prose narrative |
| appdev KB #2 body | "The Cache card's load-bearing demonstration" | prose "Cache card" | NO — prose, not backticked |

**Net**: 4 prose-level recipe-internal-naming carryovers, all on UI-element references that fall outside Item 7's regex (backticked-tokens + class-symbol detection). Run-45's heavy violations (`cache.module.ts`, `CacheService`, `SearchIndexer`, `ItemsService.onModuleInit`) — all backticked or AST-discoverable — DROPped. Item 7 closed the heavy class; left the prose class open.

---

## Refinement-2 dispatch + findings + manifest coverage

### The fenced JSON findings block (block 2, final emission — 20 findings)

Surface distribution: S3=4 / S4=7 / S5=8 / S7=1. Verdict distribution at audit-emit: DROP=4, REWRITE=16, MOVE-TO-Sn=0, ACCEPT=0.

### The walked ledger

92 entries — decomposition `60 S3 + 14 S4 + 15 S5 + 3 S7`. idKey format `S5:codebase/api/knowledge-base/<bullet-name>`; manifest idKey format `codebase_kb:api:<empty>` (Item 1 generates per-host-not-per-bullet ids; the walked ledger keyed per-bullet). Format mismatch → 4-refusal close-gate loop.

### crossSurfaceUniquenessReport

Emitted as flat `crossSurfaceUniquenessScanned: 95` + `duplicates: [5 entries]`. Spec'd shape was `{scanned, duplicates_found, duplicates_resolved_in_findings}`. Engine accepted the flat-int shape. Internal counter mismatch: walked=92 vs scanned=95.

### Per-finding triage outcomes (verbatim from `agent-a5008393f3fd78136.jsonl` final table)

ACT=18, HOLD=2 (worker IG #5 + appdev IG #4 deploy-files cite), ACCEPT-partial=1 (finding #18 env/1 sub-target).

### Engine override: worker IG #2 (REWRITE → DROP based on framework-quirk class)

Main agent HELD the override per substrate Item 1's S4 surface test (createApplicationContext + enableShutdownHooks is Zerops-forced for porters bringing own worker code). Applied as REWRITE with rolling-deploys cite (`features/scaling-ha`). Substrate-attributable: the engine's framework-quirk → DROP heuristic mis-routed a platform-invariant fact; main-agent surface test caught it. Worth tracking as a substrate frontier — engine classification of framework-quirk via fact's `candidateClass` is brittle when the candidate-class assignment happened at scaffold-time before platform-invariant nature became visible.

---

## Spec-content audit — surface-by-surface

| Section | Verdict | Notes |
|---|---|---|
| §"Empirical floor" (goldens) | **Held; CLAUDE.md is deliberate divergence** | KB-shape closer to showcase defensive-intersections (run-46 has 13 clean + 2 borderline; run-45 had 10 clean + 2 borderline + 2 hard-fail). Yaml friendly-authority recovered to run-44 level. CLAUDE.md is `/init`-shape (Zerops-free) vs goldens' Zerops-aware shape — deliberate spec divergence (repo-local, not published with recipe; goldens aren't authoritative on every dimension). |
| §"Why this exists" (journal failure mode) | **Held** | No aspirational JWT, no cross-surface deferrals, no execOnce semantic-lie. |
| §"Fact classification taxonomy" | **Held with one engine-override caveat** | Zero classification rejections at main-agent ACT path. Engine auto-overrode worker IG #2 framework-quirk → DROP; main-agent HELD per surface test. |
| §"Self-inflicted" litmus #4 | **Held** | Zero self-inflicted bullets at deliverable. Item 1 walk caught the equivalents at audit-emit. |
| §"Self-referential decoration prohibition" | **Partial — heavy class closed; prose class open** | All backticked recipe-internal class/file names gone (Item 7 + Item 1 walk). 4 prose-level "Cache card"/"cache panel"/"cache demo" carryovers survive. |
| §"Friendly-authority voice" codebase yamls | **RECOVERED** (2→9 hits) | Item 4 + clean upstream authoring. |
| §"Friendly-authority voice" tier yamls | **Held** (22 hits, near run-45's 24) | No regression on the run-45 substrate's tier-yaml win. |
| §"Surface 5" S5 single-question test | **Exercised on codebase surfaces** | Audit emitted 8 S5 findings (vs run-45's 0). |
| §"Surface 7" yaml comments | **Held** | No cross-surface deferrals; mechanism+reason in one breath; per-field comment density across codebase yamls. |
| §"Surface 3" tier yaml | **Held + improved cross-surface uniqueness** | Triage finding #16-#20 cleaned residual cross-tier and S7 issues. |
| §"Citation map" KB | **5/13 — 38%, all canonical** | Up from run-45's 4/14 with one bullet on a wrong-surface. |
| §"Citation map" IG | **5/11 — 45%, all canonical, zero wrong-URL** | Up from run-45's 1/10 with one wrong-URL. Item 5 axis ships clean at the deliverable. |

---

## Golden voice alignment

### KB shape

- **Jetstream** (2 H3, operational mechanism-first): `Maintenance Mode` + `Temporary Upscaling`. Both narrate the porter's WORKFLOW (use `php artisan down` + `zsc health-check disable`; `zsc scale ram +0.5GB 10m` for dev composer/npm).
- **Showcase** (1 `### Gotchas` H3 with 7 bullets, mechanism-first): `No .env file`, `Cache commands in initCommands not buildCommands`, `APP_KEY is project-level`, `PDO PostgreSQL extension`, `Predis over phpredis`, `Object storage requires path-style`, `Vite manifest missing on dev after fresh deploy`. Operational voice — leads with what to do, not what breaks.
- **Run-46 codebase KBs** (13 H3 bullets across 3 codebases, defensive symptom-first): every bullet's stem is the platform-trap symptom (`Authorization Violation`, `UnknownError`, `Blocked request`). Voice shape closer to showcase's defensive-intersection-bullet shape than to jetstream's operational-workflow shape, but neither matches exactly — run-46 KBs lead with the ERROR token, goldens lead with the TASK or MECHANISM.

Verdict: run-46 KB voice is **defensive symptom-first intersections** — neither matches operational-mechanism-first (jetstream) nor matches mechanism-first-with-bullets (showcase Gotchas). Voice is closer to the goldens' tone than run-45 was, but the canonical bar may be operational-not-defensive.

### Yaml comment voice

- **Jetstream zerops.yaml**: ~2 adapt-paths (`Feel free to change this value to your own custom domain` + `Configure this to use real SMTP sinks`).
- **Showcase zerops.yaml**: ~0 explicit adapt-paths; declarative-with-rationale (`MUST`, `mandatory`, `no manual install needed`).
- **Run-46 codebase yamls**: 9 adapt-path hits total. Apidev's yaml (5 hits) is the densest — "us-east-1 is the conventional placeholder; change it if your SDK or audit policy expects a specific value", "Bump expiresIn for longer-lived links; rotate the bucket's `${storage_secretAccessKey}` via the Zerops UI if a signed URL leaks", "Feel free to add your own custom-domain origins once you've configured access through your own domain(s)".

Verdict: run-46 yaml adapt-path density EXCEEDS goldens. The substrate's F-FRIENDLY-AUTH push has overshot the empirical bar. **Substrate-text-vs-goldens-bar mismatch worth tracking** — the goldens are sparse-adapt-path, the substrate gates dense-adapt-path proportional to porter-tunable count. The goldens may be the empirical bar; the substrate may be overshooting.

### CLAUDE.md voice — deliberate divergence

Run-46 codebase CLAUDE.mds are framework-intro + npm-script summary + per-file architecture bullets. Zero Zerops content. Matches the substrate spec's S6 OUT-OF-SCOPE contract + `/init`-shape authoring contract.

Goldens (`/Users/fxck/www/laravel-jetstream-app/CLAUDE.md` + `/laravel-showcase-app/CLAUDE.md`) carry Zerops-aware content: service-facts blocks, dev-mode contracts, `use zcp MCP not zcli` directives.

The divergence is **deliberate, not a defect**. CLAUDE.md is repo-local — porters running their own codebase don't see the recipe author's CLAUDE.md, so authoring effort on Zerops-aware CLAUDE.md content is wasted. Goldens aren't authoritative on every dimension; this is one where the spec wins. S6 stays out-of-scope.

---

## Content quality progression vs runs 43/44/45

### apidev KB progression

| Run | Bullets | Notable shape |
|---|---:|---|
| 42 | 5 | NATS + obj-storage (self-inflicted) + X-Cache + CORS + NATS-publish-drop |
| 43 | 5 | NATS + forcePathStyle (cite ✓) + cross-origin headers + Valkey no creds + Meilisearch master key |
| 44 | 4 | Auth-Violation + Cache demo X-Cache (recipe-internal stem) + relation-already-exists + Cache 5xx Valkey (recipe-internal tryGetClient) |
| 45 | **7** | Above 4 + cross-origin headers (borderline) + **NestJS DI undefined (FAIL)** + **Search card 0 docs (FAIL)** |
| **46** | **5** | Auth-Violation + X-Cache cross-origin + obj-storage 403 (cite ✓) + bucket private (cite ✓ NEW) + searchableAttributes |

**Run-46 progression**: dropped both run-45 hard-fail bullets; added bucket-private-presigned bullet with citation. Net voice classification: -2 hard-fail, +1 clean intersection, +1 net citation, -1 framework-quirk creep.

### workerdev KB progression

| Run | Bullets | Notable shape |
|---|---:|---|
| 43 | 3 | queue-group (cite ✓) + drain (cite ✓) + api-codebase cross-ref (borderline) |
| 44 | 3 | queue-group (cite ✓) + drain (cite ✓) + Auth-Violation (borderline) |
| 45 | 5 | queue-group (cite ✓) + drain (cite ✓) + NATS-Auth + APP_SECRET-shadow + https-Meilisearch |
| **46** | **5** | queue-group (cite ✓) + drain (cite ✓) + Auth-Violation (cite ✓ NEW) + PutObject UnknownError (cite ✓ NEW) + cache-user/password-literal-strings |

**Run-46 progression**: maintained 5-bullet shape; added 2 new citations (NATS broker + S3 storage); dropped APP_SECRET-shadow (canonical now at IG #5 / worker zerops.yaml) and https-Meilisearch (collapsed to internal-traffic comment in yaml).

### appdev KB progression

| Run | Bullets | Notable shape |
|---|---:|---|
| 43 | 2 | Blocked-host + literal-API-URL |
| 44 | 2 | Blocked-host + literal-fetch |
| 45 | 2 | Prod-bundle-null-X-Cache + literal-API-URL |
| **46** | **3** | Blocked-host + Cache HIT/MISS badge stuck + VITE_API_URL fresh-build |

**Run-46 progression**: +1 bullet (VITE_API_URL fresh-build operational); KB #2 reframed but still names "Cache card" in body.

---

## Substrate operations

### Refinement state machine

- Refinement-1-rulewalk: 1 dispatch, 56 tool calls, 6m12s, 3 ACTs, 4 HOLDs.
- Refinement-2-audit: 1 dispatch, 34 tool calls, 5m48s, 20 findings emitted in 2 fenced JSON blocks (revised counter-position).
- Refinement-2-triage: 1 dispatch, 131 tool calls, 17m42s, 18 ACT + 2 HOLD + 1 partial ACCEPT, 10+ `refinement-replace-reverted` notices (all from kb-citation-display-mismatch + kb-citation-topic-mismatch).
- Main agent ran 1 `enrich-findings` call after refinement-2-audit emission then 4 more empty-findings retries during the close-gate loop = 5 total.

### Phase ordering anomaly

LN 192 — main agent called `enter-phase phase=refinement` after env-content sub-agent had already self-advanced (env-content closed from within its own session). Engine refused with "phase transition env-content → refinement not adjacent-forward". Main agent pivoted to `enter-phase phase=finalize` at LN 195. Functional but exposes a stateless-flow assumption: env-content sub-agents can close their own phase, leaving the main agent's phase-advance call out of sync.

### Manifest coverage + walked ledger gate exercise

Manifest written at session OutputRoot (engine code path verified). Sub-agent never `Read` it — only `head -50` + `grep -c`. Walked ledger emitted in free-form idKey format; gate refused 4 times. Main agent iterated empty-findings to bypass. **Gate fired as designed; structural-fix intent (sub-agent walks the manifest) not realized**.

### Env-fragment snapshot/restore exercise (Item 3)

ZERO reverts on `env/<N>/import-comments/*`. Wrapper unverified in production. Either the wrapper is wired and had nothing to revert OR it's wired but env triage authored clean.

### Classification rejection count

ZERO classification rejections at main-agent ACT path. Holds the run-45 substrate Pillar-C win.

### Snapshot/restore revert count by surface

- `codebase/api/integration-guide/4`: 1 revert
- `codebase/app/integration-guide/3`: 1 revert
- `codebase/app/integration-guide/4`: 4 reverts (engine bug — `lookupGuideIDFromURL` fragment strip)

All on S4 codebase IG fragments. All citation-validator (Item 5) reverts.

### Sub-agent durations (seconds)

| Role | Duration |
|---|---:|
| scaffold-api | 335 |
| scaffold-app | 344 |
| scaffold-worker | 537 |
| features-backend | 1421 |
| features-frontend | 1484 |
| codebase-content-api | 315 |
| codebase-content-app | 322 |
| codebase-content-worker | 263 |
| claudemd-author-api | 49 |
| claudemd-author-app | 39 |
| claudemd-author-worker | 44 |
| env-content | 631 |
| refinement-1-rulewalk | 372 |
| refinement-2-audit | 348 |
| refinement-2-triage | 1062 |

Total agent time ≈ 7570s (≈ 2h6m). Refinement-2-triage is the single largest sub-agent at 17m42s (citation-revert loop included).

---

## Known substrate issues still present

1. **Walked-ledger idKey format mismatch class** (Item 1 integration). Codex-verified: sub-agent emits free-form keys; manifest writes a specific format; gate refuses without telling the sub-agent the expected grammar. Sub-agent must Read the manifest to learn the format; sub-agent didn't Read the manifest in run-46.
2. **`lookupGuideIDFromURL` fragment-strip bug** (Item 5 surfaced). Codex-verified at `validators_codebase.go:615-623`. Strips `#initcommands-`/`#deployfiles-`/`#envvariables-` before path-match → collapses three distinct guides into one. Legitimate `#deployfiles-` cites refused 4 times in this run before triage HELD.
3. **`crossSurfaceUniquenessReport` shape regression** (Item 6). Audit emits flat int + sibling array; spec wants nested object with three named keys. Engine accepts flat shape — gate's structural-validation is weaker than designed.
4. **Item 7 prose-level recipe-internal naming uncaught**. Linter's backticked-token + class-symbol scope leaves "Cache card" / "cache panel" / "cache demo" prose creep open. Run-46 deliverable retains 4 such occurrences.
5. **CLAUDE.md voice — deliberate divergence from goldens (spec wins)**. CLAUDE.md is repo-local, not published with the recipe; refinement effort wasted on a file porters won't see. Goldens carry Zerops content; spec says `/init`-shape Zerops-free. Not a defect — note explicitly so the divergence is intentional.
6. **Sub-agent disk-search for goldens** (refinement-1 behavior). Substrate brief embeds golden anchors; sub-agent searched `/var/www/` filesystem for `*/jetstream*` paths. Behavior-signal, not deliverable defect.

## Known substrate issues correctly closed

1. **apidev KB framework-quirk surfaces** — Item 1 walked, audit found, DROPped.
2. **apidev KB self-inflicted bullets** — Item 1 walked, audit found, DROPped.
3. **Wrong-URL form-(b) citations** — Item 5 enforced display↔URL↔topic agreement; run-45's apidev IG #4 wrong-URL is fixed.
4. **Codebase yaml friendly-authority adapt-path regression** — recovered 2→9 hits.
5. **Audit walks codebase surfaces** — 16 of 20 audit findings on S4+S5+S7 (run-45: 0 on codebase surfaces).
6. **Zero classification rejections** — substrate Pillar-C held from run-45.

---

## Recommended next action (ranked by content-quality impact)

1. **(highest impact, lowest cost) Wire walked-ledger idKey-format validation into `enrich-findings`**. When `walked[i]` doesn't match the manifest's idKey grammar (`codebase_ig:<host>:<slot>` / `codebase_kb:<host>:<empty>` / `codebase_zerops_yaml:<host>` / `tier_yaml_comments:<N>:<key>`), refuse the `enrich-findings` call with the expected format echoed in the error message. Sub-agent learns the grammar at the audit boundary, not via reverse-engineering the close-gate's refusal messages. Closes the run-46 substrate-frontier failure class. Pin: `TestEnrichFindings_WalkedLedgerIDKeyFormatRefusal`.
2. **Fix `lookupGuideIDFromURL` fragment-strip bug** in `validators_codebase.go:615-623`. Preserve URL fragments in the canonical-URL match. Closes the legitimate `#deployfiles-` cite refusal class. Pin: `TestLookupGuideIDFromURL_PreservesFragmentAnchor`.
3. **CLAUDE.md — no action**. Deliberate divergence from goldens (S6 stays out-of-scope; repo-local file, not published with recipe; goldens aren't authoritative on every dimension). Listed only for completeness — nothing to fix.
4. **Extend Item 7 self-referential-naming linter to prose-level recipe-internal feature naming**. Scope-creep risk acknowledged: prose-level detection requires per-recipe feature-name vocabularies (`Cache card`/`cache panel`/`cache demo`/`Search card`) that don't generalize across recipes. Alternative: at codebase-content close, require KB bodies to not name recipe-specific UI features (regex on `(Cache|Search|Items|Storage|Queue) (card|panel|demo|tab)` etc.). Marginal cost, marginal value — the 4 carryovers don't break a porter's read.
5. **Audit the `framework-quirk → auto-DROP` engine heuristic**. Worker IG #2 was auto-DROPed by engine override based on `candidateClass=framework-quirk`; main agent HELD with surface-test rationale. The auto-DROP heuristic is brittle when the candidate-class assignment happened at scaffold time before platform-invariant nature became visible (NestJS standalone application context is Zerops-forced for porters bringing own worker code via the rolling-deploys SIGTERM-drain mechanism). Run-46 caught this; future runs may not.
6. **Token-budget the refinement-2 audit output**. 58.7 KB output overflowed inline cap, forcing Python re-extraction. Either compress the audit's emission shape (drop redundant rationale prose) or instruct the sub-agent to chunk findings into multiple smaller emissions.

If only one diff lands: **#1** (walked-ledger idKey-format validation). Without it, the Item-1 close-gate will continue to refuse-then-bypass on every future run.

---

## Verdict — ITERATE-TO-47

Run-46 ships a deliverable a porter would not block on: zero framework-quirk KBs, zero self-inflicted KBs, zero wrong-URL citations, recovered codebase-yaml adapt-path density, audit walked all four in-scope surfaces. The seven-item structural substrate moved real authoring behavior at the surfaces where run-45 most needed it. Run-45's specific defects are absent or addressed.

But the engine trajectory tells a substrate-frontier-shift story, not a closed-loop story: a 4-refusal close-gate loop on idKey-format mismatch, a real engine validator bug surfaced by Item 5, 3 of 7 items unverified-in-production at the run surface, plus a CLAUDE.md spec-vs-goldens contradiction that the spec ignores.

Smallest substrate diffs to close the remaining gaps name themselves: wire idKey-format validation at `enrich-findings`; fix the URL-fragment-strip bug. With those two diffs, run-47 should ship without the close-gate retry loop and without the validator-bug-driven citation reverts.

The structural bet partly paid off. Counter-evidence to "Pillar B failed at audit-emit": the substrate moved the audit from 18-S3-only-findings (run-45) to 20-findings-across-all-4-surfaces (run-46), and the audit found 4 DROP-class bullets that would have shipped under run-45's failure mode. The bet on structural fixes over prose-redesign was directionally right; it just exposed a new failure class at the structural boundary.

---

## Codex validation references

All load-bearing analytical claims in this report were submitted to `Agent subagent_type=codex:codex-rescue` (agentId `ae1bf60ef79e77e2f`) for independent verification before publication. Codex REVISED Claims 1 and 2 (sharpening the framing on walked-ledger latest-wins semantics + the `lookupGuideIDFromURL` fragment-strip path); HELD Claim 3 (Items 3/4/7 unverified-in-production framing).

## Sub-agent transcript map

| Role | Agent ID |
|---|---|
| scaffold-api | `a5e0d0ad7e87a2162` |
| scaffold-app | `a7f569b0d5480c2ed` |
| scaffold-worker | `ac0c984c1ee956734` |
| features-backend | `acff4d6f94297ca1b` |
| features-frontend | `a3a8d680fd3b1d3a8` |
| codebase-content-api | `a208048c5b915e876` |
| codebase-content-app | `ad75065966def0836` |
| codebase-content-worker | `afddea9d52020d696` |
| claudemd-author-api | `a31a1620001e690f3` |
| claudemd-author-app | `a636019bfc7fc5f86` |
| claudemd-author-worker | `aaf29b6b90bb00922` |
| env-content | `adcd96f74b4103c22` |
| refinement-1-rulewalk | `a16657c1e4c662ebd` |
| refinement-2-audit | `a21d883ae5d76e13a` |
| refinement-2-triage | `a5008393f3fd78136` |
