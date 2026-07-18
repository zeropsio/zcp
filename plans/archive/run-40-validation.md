# Run-40 validation — evidence-grounded audit

> **Headline: PARTIAL.** Six of eight named engine fixes closed cleanly. ENG-2 (TIMELINE sanitizer) is the only material gap — implementation regex coverage is narrower than the run-40 plan §ENG-2 spec, so the project-ID parenthetical, session UUID, no-port stage hostnames, and engine-vocab tool/agent/validator names still survive the export path. Substrate-only defect classes that weren't on Phase E (S1-2 JWT, S2-1 `See IG #N` yaml cross-refs, S3-2 prg1.zerops.app zone) persist unchanged. Three NEW soft defects surfaced: `${search_password}` vs `${search_masterKey}` yaml-comment drift, `/api/...` endpoint paths in tier-0 yaml + facts.jsonl contract entries, and an unimplemented "SPA gets MEILI_SEARCH_KEY" prescription in two KBs.

The deliverable is shippable as a quality leap over run-39 (queue-group drift gone, dead envs gone, lowercase-hostname bug gone, ghost-dep gone, refinement write-back closed, TypeORM gotcha now factual because run-40 actually uses TypeORM). Recommend one more iteration (run-41) to widen ENG-2's regex coverage and address the three new soft defects before promoting to canonical recipe.

---

## Per-engine-fix closure verdict

### ENG-1 — Refinement write-back to plan.json fragments — **CLOSED**

Spot-checked plan.json fragments against disk-stitched README outputs at the exact run-39 divergence sites:

- [environments/plan.json:178](docs/zcprecipator3/runs/40/environments/plan.json#L178) `codebase/api/integration-guide/3` matches [apidev/README.md:230-239](docs/zcprecipator3/runs/40/apidev/README.md#L230-L239) ("Trust the reverse proxy") byte-for-byte.
- [environments/plan.json:179](docs/zcprecipator3/runs/40/environments/plan.json#L179) `codebase/api/integration-guide/4` matches [apidev/README.md:241-251](docs/zcprecipator3/runs/40/apidev/README.md#L241-L251) ("Drain in-flight requests on SIGTERM").
- [environments/plan.json:195](docs/zcprecipator3/runs/40/environments/plan.json#L195) `codebase/worker/integration-guide/4` matches [workerdev/README.md:184-196](docs/zcprecipator3/runs/40/workerdev/README.md#L184-L196) (queue-group section).

The five run-39 stale fragments (plan.json lines 176/177/183/184/192) all have synced content in run-40. No divergence detectable.

### ENG-2 — TIMELINE sanitizer — **PARTIAL**

Sanitizer exists at `internal/sync/timeline_sanitizer.go` and fires at the export-tarball boundary ([internal/sync/export.go:278](internal/sync/export.go#L278)). The on-disk TIMELINE.md at `docs/zcprecipator3/runs/40/TIMELINE.md` is pre-sanitizer by design.

Simulated the sanitizer against the on-disk TIMELINE — the **exported** file would still ship the following leaks because the regex coverage is narrower than the run-40 plan §ENG-2 spec:

| Leak | Reason sanitizer misses it |
|---|---|
| `f1NS28GZRByGbQz3WaihAw` at [TIMELINE.md:3](docs/zcprecipator3/runs/40/TIMELINE.md#L3) | Emitted as `` (`f1NS28GZRByGbQz3WaihAw`) `` (bare backticks inside parens). Regex requires `` (id `...`) `` keyword-id form. |
| Session UUID `ca8266e6-3e42-4620-854a-cd02c6ac2b40` at [TIMELINE.md:3](docs/zcprecipator3/runs/40/TIMELINE.md#L3) | Not on the redactor list at all. |
| `appstage-2311.prg1.zerops.app` at [TIMELINE.md:34,58](docs/zcprecipator3/runs/40/TIMELINE.md#L34) | Hostname-hash regex requires the `-<port>` segment; `base: static` stage subdomains have no port suffix. |
| Engine-vocab tokens: `complete-phase`, `enter-phase`, `record-fragment`, `stitch-content`, `featurePass=`, `phase=research/provision/finalize/refinement`, validator ID `kb-citation-missing`, sub-agent role names `codebase-content-{api,app,worker}` / `claudemd-author-*`, MCP tool names `zerops_knowledge` / `zerops_import` / `zerops_discover` / `zerops_verify` | Plan §ENG-2 listed these in the strip-list ("engine-internal tool names, validator IDs, sub-agent role names, rule IDs"). Implementation [internal/sync/timeline_sanitizer.go:64-77](internal/sync/timeline_sanitizer.go#L64-L77) only redacts: project-ID-paren-form, hostname-with-port, /var/www/zcprecipator paths, /Users/ paths, service-count. None of the engine-vocab tokens are touched. |

Hostname-with-port pattern *does* match the apistage URLs in facts.jsonl (e.g. `apistage-2311-3000.prg1.zerops.app`) — those would be redacted on export.

Plan §ENG-2 also called out a `provisioned N services` substitution; sanitizer wires it as `SanitizeTimelineOpts.ServiceCount`. TIMELINE.md says "Workspace YAML emitted, 11 service blocks" — `11` already correct, no substitution needed. S8 (run-39 "14 services" count error) is naturally closed because the agent counted correctly this run, not because the sanitizer fired.

**Verdict: implementation lands the smaller half of the spec. Project-ID + session-UUID + no-port-stage hostname + engine-vocab still ship to porter at export.**

### ENG-3 — Brief-generator template edit (parent-recipe citation) — **CLOSED**

Zero hits for `See parent recipe`, `Cross-reference shape`, or `nestjs-minimal` across porter-facing surfaces:
- All three apps-repo READMEs (apidev/appdev/workerdev)
- All six tier `import.yaml` + `README.md` files
- Root `README.md` + `environments/README.md`
- All three apps-repo `zerops.yaml` + `CLAUDE.md`

`nestjs-minimal` appears only once in the entire deliverable — at [TIMELINE.md:12](docs/zcprecipator3/runs/40/TIMELINE.md#L12) in the research log (engine-meta narrative, expected). Brief artifacts under `.briefs/` also contain zero "Cross-reference shape" template strings.

### ENG-4 — Facts.jsonl canonical-topic + latest-by-canonical — **CLOSED (outcome)**

Queue-group named-constant consistent everywhere in deliverable:
- Source: [workerdev/src/nats/nats-consumer.service.ts](docs/zcprecipator3/runs/40/workerdev/src/nats/nats-consumer.service.ts) uses `'worker-indexer'`.
- Plan: [plan.json:87,92,118,157](docs/zcprecipator3/runs/40/environments/plan.json) — all `worker-indexer` in envComments.
- Tier yamls: 0/1/2/3/4/5 — all `worker-indexer`.
- Apps-repo READMEs: api + worker — all `worker-indexer`.
- Apps-repo CLAUDE.md: worker — `worker-indexer`.
- facts.jsonl: lines 42, 45, 46, 47, 49, 64, 84 — all `worker-indexer`.

Zero hits for `'showcase-workers'` or `'workers'` (alone) in deliverable. The run-39 30+-place drift is gone.

Caveat — topic *canonicalization* isn't strictly visible at outcome layer. facts.jsonl line 8 ("worker_dev_server_started" scope `appdev/runtime`), line 18 (same topic scope `app`), line 23 (`api_dev_server_started` scope `apidev/runtime`), line 30 (`worker_dev_server_started` scope `apidev/runtime`), line 37 (`worker_dev_server_started_api` scope `api`), line 38, 46, 47 all show duplicate-topic-with-scope-noise. The named-constant pipeline produced correct output regardless. Cannot tell from artifacts whether ENG-4 canonicalization fired or the agent simply never recorded a `'showcase-workers'` fact this run.

### ENG-5 — consumesServices source-derivation gate — **CLOSED**

- `worker.consumesServices: ["broker","cache","search","storage"]` at [plan.json:37-42](docs/zcprecipator3/runs/40/environments/plan.json#L37-L42). Grep confirms worker source reads `NATS_*`, `REDIS_URL`, `MEILI_*`, `S3_*` and zero `DB_*`. The run-39 ghost-`db` dependency is gone.
- `api.consumesServices: ["broker","cache","db","search","storage"]` at [plan.json:15-21](docs/zcprecipator3/runs/40/environments/plan.json#L15-L21) — api reads all five via process.env.

TIMELINE.md:52 confirms the active drop: "Plan update: dropped `db` from `worker.consumesServices` (ghost-dep cleanup — worker never reads Postgres)" — gate fired during feature phase.

### ENG-6 — Stitched-matches-plan diff gate — **CLOSED**

Indirect verification: ENG-1 spot-checks (above) show byte-for-byte sync between plan.json fragments and stitched outputs. If the diff gate hadn't fired (or hadn't been wired), one of those would have diverged. TIMELINE.md:92-105 narrates `complete-phase phase=finalize` → refused → refinement → re-stitch → `complete-phase phase=refinement: ok:true` → `complete-phase phase=finalize: ok:true`, consistent with a working gate.

### A1 — plan.namedConstants + tier-yaml renderer binding — **NOT-VERIFIABLE**

plan.json has no explicit `namedConstants` slot visible in the deliverable. The canonical run-39 example (queue-group string) is consistent everywhere (see ENG-4 above), but whether that's because A1 wired a named-constants table or because facts-stream-canonicalization (ENG-4) was sufficient is indistinguishable from artifacts alone. From an outcome standpoint the goal is met; the structural claim isn't separately verifiable.

### B1 — observedFacts.envReads + dead-env gate — **CLOSED**

[plan.json:208-249](docs/zcprecipator3/runs/40/environments/plan.json#L208-L249) carries `observedFacts.envReads` for api/app/worker. Cross-checked against zerops.yaml `run.envVariables` declarations:

| Codebase | Declared in yaml | Read in source | Drift |
|---|---|---|---|
| api | PORT, DB_HOST, DB_PORT, DB_NAME, DB_USER, DB_PASSWORD, REDIS_URL, NATS_HOST, NATS_PORT, NATS_MGMT_PORT, NATS_USER, NATS_PASS, S3_ENDPOINT, S3_REGION, S3_BUCKET, S3_ACCESS_KEY_ID, S3_SECRET_ACCESS_KEY, MEILI_HOST, MEILI_MASTER_KEY, CORS_ORIGINS (20) | All 20 read in [apidev/src/](docs/zcprecipator3/runs/40/apidev/src/) | 0 |
| worker | REDIS_URL, NATS_HOST, NATS_PORT, NATS_USER, NATS_PASS, S3_ENDPOINT, S3_REGION, S3_BUCKET, S3_ACCESS_KEY_ID, S3_SECRET_ACCESS_KEY, MEILI_HOST, MEILI_MASTER_KEY (12) | All 12 read in [workerdev/src/nats/nats-consumer.service.ts](docs/zcprecipator3/runs/40/workerdev/src/nats/nats-consumer.service.ts) | 0 |
| app | VITE_API_URL (build-time) | `import.meta.env.VITE_API_URL` consumed | 0 |

All three run-39 dead envs (`SEARCH_PUBLIC_HOST`, `SEARCH_SEARCH_KEY`, `NATS_QUEUE_GROUP`) are absent from run-40 zerops.yamls.

### Phase S — Substrate cleanup — **PARTIAL (expected)**

- S-1 (`dev_server restart re-reads env`) — STILL PRESENT in [scaffold-api-…md:646](docs/zcprecipator3/runs/40/environments/.briefs/scaffold-api-1778523378373453028.md#L646), [scaffold-app-…md:646](docs/zcprecipator3/runs/40/environments/.briefs/scaffold-app-1778523379154556329.md#L646), [feature-phase-…md:366](docs/zcprecipator3/runs/40/environments/.briefs/feature-phase-1778524165388497412.md#L366), and the substrate source [internal/recipe/content/principles/mount-vs-container.md:62-66](internal/recipe/content/principles/mount-vs-container.md#L62-L66). Known issue (per user-stated deferral to live empirical test).
- S-4 (parent-recipe baseline filter) — NOT IMPLEMENTED. TypeORM gotcha still appears in worker briefs ([codebase-content-worker/part-2-synthesis.md:373,687](docs/zcprecipator3/runs/40/environments/.briefs/codebase-content-worker/part-2-synthesis.md), part-8-context.md:54) — but in run-40 the **child** recipe also uses TypeORM (apidev/package.json declares `typeorm@0.3.20` + `@nestjs/typeorm`), so the gotcha is appropriate where authored (apidev/README.md:296-300). For worker briefs the noise is harmless this run because the agent didn't promote it to porter prose.
- B1#10 false-positive on `const env = process.env; const config = ...` JS shape — not exercised in run-40 (no such pattern in apidev/appdev/workerdev source). Pathological case stays as documented non-coverage.

Substrate-self-contradictions (S-2 list) — verifying these per-line would require reading all 13K lines of briefs. Spot-check: worker brief still cites `rolling-deploys` topic ([codebase-content-worker/part-1-phase-entry.md:173-176](docs/zcprecipator3/runs/40/environments/.briefs/codebase-content-worker/part-1-phase-entry.md#L173-L176)) AND uses the substituted porter-recognizable phrasing in the deliverable ([workerdev/README.md:229](docs/zcprecipator3/runs/40/workerdev/README.md#L229): "Zerops's [zero-downtime deploys with multi-container setups]"). The brief contradiction is still present in substrate, but the agent navigated it correctly.

---

## NEW defects surfaced in run-40

### N-1 (S0 class) — `${search_password}` in tier-0 import.yaml does not exist

[environments/0 — AI Agent/import.yaml:135-140](docs/zcprecipator3/runs/40/environments/0%20%E2%80%94%20AI%20Agent/import.yaml#L135-L140):

> ```yaml
> # Deploy single-node Meilisearch — used by the worker to index
> # items reactively after each `items.created` event and by the
> # api to serve `/api/search` queries. The master key is
> # generated once at import and shared with both services via
> # ${search_password}, so the api and worker authenticate
> # against the same value without manual wiring.
> ```

The actual env wiring at [apidev/zerops.yaml:112](docs/zcprecipator3/runs/40/apidev/zerops.yaml#L112) + [workerdev/zerops.yaml:65](docs/zcprecipator3/runs/40/workerdev/zerops.yaml#L65) reads `MEILI_MASTER_KEY: ${search_masterKey}`. `${search_password}` is not a Zerops Meilisearch alias — recipe lies to porter about which alias name to use. Tier-0 only; other tiers' `search` import-comment doesn't mention the alias key. Causal chain: env-content phase free-authored the alias name in the yaml comment without cross-checking source/yaml. No engine gate catches drift between yaml-comment named-constants and yaml-content named-constants.

### N-2 (S1 class — repeats run-39 S1-2) — JWT verification claim with no JWT code

Three sites:
- [environments/0 — AI Agent/import.yaml:4](docs/zcprecipator3/runs/40/environments/0%20%E2%80%94%20AI%20Agent/import.yaml#L4): "APP_SECRET is generated once at import and shared across api + worker so JWT verification holds across containers."
- [environments/4 — Small Production/import.yaml:4](docs/zcprecipator3/runs/40/environments/4%20%E2%80%94%20Small%20Production/import.yaml#L4): same JWT framing.
- [apidev/README.md:255](docs/zcprecipator3/runs/40/apidev/README.md#L255): "JWTs sign with the literal `${APP_SECRET}` text" (same-key shadow trap worked example, inherited substrate).

`apidev/package.json` has NO `@nestjs/jwt`, `jsonwebtoken`, or `passport-jwt` dependency. Source grep for `jwt`/`JWT`/`jsonwebtoken` returns zero hits. The recipe demonstrates Items CRUD, not auth.

S1-2 was tracked in run-40 plan but NOT on Phase E priorities ("Hours. substrate update: same-key-shadow worked example needs to be framework-neutral or recipe-specific."). Persists as expected — but worth flagging because it's the most visible "recipe lies" surface to a porter who reads the tier-0 README before opening source.

### N-3 (S4/factuality) — `/api/...` endpoint paths in tier-0 yaml + facts.jsonl contract entries

[environments/0 — AI Agent/import.yaml:137](docs/zcprecipator3/runs/40/environments/0%20%E2%80%94%20AI%20Agent/import.yaml#L137) yaml comment says `/api/search`. facts.jsonl `contract` entries claim `/api/items` ([facts.jsonl:51](docs/zcprecipator3/runs/40/environments/facts.jsonl#L51)), `/api/items/state`, `/api/items/:id`, `/api/cache/state`, `/api/queue/state`, `/api/storage/upload`, `/api/storage/state` (lines 51-60).

[apidev/src/main.ts](docs/zcprecipator3/runs/40/apidev/src/main.ts) has NO `setGlobalPrefix('api')`. Controllers are at root paths: [search.controller.ts:4](docs/zcprecipator3/runs/40/apidev/src/search/search.controller.ts#L4) `@Controller('search')`, items.controller.ts uses `@Controller('items')`, etc.

facts.jsonl `curl_verification` entries are CORRECT and use the actual endpoint paths: [facts.jsonl:65](docs/zcprecipator3/runs/40/environments/facts.jsonl#L65) `200 OK against .../items`, line 73 `.../storage/upload`, line 75 `.../search?q=`, line 77 `.../status`.

So the facts.jsonl `contract` entries and the `curl_verification` entries record divergent paths for the same endpoints. The README content (apidev/README.md "GET /items", "GET /search") is correct.

This is one of:
- the agent free-authored `/api/...` in contract entries before writing the curl (then never reconciled), or
- the agent inherited a pattern from substrate that assumes `setGlobalPrefix('api')`.

Either way it's an in-deliverable contradiction. Magnitude low because the README is correct; magnitude non-zero because porters and downstream tooling may use facts.jsonl as a source of truth.

### N-4 (S2 soft / aspirational-as-current) — SPA MEILI_SEARCH_KEY prescription unimplemented

Two KB sections describe a key-split policy:
- [apidev/README.md:318](docs/zcprecipator3/runs/40/apidev/README.md#L318): "the SPA build receives only `${search_defaultSearchKey}` (read-only) for query traffic. Keep `MEILI_MASTER_KEY` server-side; expose `MEILI_SEARCH_KEY` to the frontend through a config endpoint or via the SPA's own `build.envVariables` block"
- [workerdev/README.md:273-275](docs/zcprecipator3/runs/40/workerdev/README.md#L273-L275): "The frontend codebase consumes `${search_defaultSearchKey}` instead, which scopes to read-only search queries."

Actual SPA wiring at [appdev/zerops.yaml](docs/zcprecipator3/runs/40/appdev/zerops.yaml): `build.envVariables` carries only `VITE_API_URL`. No `MEILI_SEARCH_KEY` exposed. SPA doesn't import `meilisearch` either — it calls the api's `/search` endpoint instead.

The prose is written in present-tense ("receives only…", "consumes…") but reads more like aspirational guidance (= "if you were going to expose Meili to the SPA, here's how to do it without leaking master key"). The recipe doesn't expose Meili to the SPA at all. Soft factuality drift; not a porter-breaking lie but an example of "tells the porter we do X" when we don't.

### N-5 (S2-1 persistent) — `See IG #N` yaml-comment cross-refs

Run-39 S2-1 persists at the same magnitude:
- [appdev/zerops.yaml:58](docs/zcprecipator3/runs/40/appdev/zerops.yaml#L58): "See IG #5"
- [appdev/zerops.yaml:68](docs/zcprecipator3/runs/40/appdev/zerops.yaml#L68): "See IG #4"
- [workerdev/zerops.yaml:42](docs/zcprecipator3/runs/40/workerdev/zerops.yaml#L42): "see IG #3"
- [apidev/zerops.yaml:67](docs/zcprecipator3/runs/40/apidev/zerops.yaml#L67): "See IG #5"

When zerops.yaml ships to an apps-repo, the IG anchors are not co-located — porters reading the yaml see dangling cross-refs. Was substrate fix, not on Phase E priority list. Expected.

### N-6 — `S3_FORCE_PATH_STYLE` env-var fact mismatch

[facts.jsonl:16](docs/zcprecipator3/runs/40/environments/facts.jsonl#L16) records api fact-content: `S3_FORCE_PATH_STYLE: "true"` declared in zerops.yaml envVariables. [facts.jsonl:27](docs/zcprecipator3/runs/40/environments/facts.jsonl#L27) repeats. Actual [apidev/zerops.yaml](docs/zcprecipator3/runs/40/apidev/zerops.yaml) has NO `S3_FORCE_PATH_STYLE` declaration — the env-var is gone (storage_service.ts hardcodes `forcePathStyle: true` in the SDK constructor instead). Stale scaffold-phase fact never superseded. Doesn't reach deliverable surfaces; classed alongside N-3 as "facts.jsonl ground-truth drift".

### N-7 (minor) — engine-vocab in fact `fixApplied` field

[facts.jsonl:39,40](docs/zcprecipator3/runs/40/environments/facts.jsonl#L39): `fixApplied` field contains "complete-phase gate accepts the missing port because the worker role-contract sets ServesHTTP=false". Engine-vocab leak in a fact record. V6 forbidden-tokens didn't refuse on `complete-phase`. Doesn't reach deliverable prose; cosmetic.

---

## Known-issues confirmed still present (expected)

- **S-1** dev-server-restart-re-reads-env brief lie at [mount-vs-container.md:62-66](internal/recipe/content/principles/mount-vs-container.md#L62-L66) and four briefs in run-40 — deferred to live Zerops empirical test before brief edit. ✓ As expected.
- **S-4** parent-recipe baseline filter not implemented — TypeORM gotcha still appears in worker briefs. Harmless this run because (a) the child recipe also uses TypeORM and (b) the agent didn't propagate the gotcha to worker porter prose. ✓ As expected.
- **B1 grep #10** pathological JS shape — not exercised in run-40 source. ✓ As expected.

---

## Counter table vs prior runs (defect-class deltas)

Run-38 not deeply audited (out of scope per prompt); run-39 numbers per `plans/run-40-evidence-grounded-plan.md` defect catalog.

| Defect class | Run-39 | Run-40 | Δ |
|---|---:|---:|---|
| S0-1 hardcoded hostname (StatusController) | 1 | 0 | -1 ✓ (field renamed `hostname`→`service`, refactored) |
| S0-2 `process.env.hostname` lowercase | 2 | 0 | -2 ✓ |
| S0-3 worker hardcodes queue + lying README | 1 | 0 | -1 ✓ |
| S0-4 `${broker_connectionString}` contradicts itself | 1 | 0 | -1 ✓ (consistent: both api + worker KB call it safe) |
| S0-5 Terminus HTTP 200 claim | 1 | 0 | -1 ✓ (no `200 once Postgres pings` claim in run-40) |
| S0-6 dead envs | 3 | 0 | -3 ✓ |
| S1-1 queue-group cross-file drift | 30+ | 0 | -30+ ✓ |
| S1-2 JWT claim with no JWT code | many | 3 | -? (substrate fix not done — expected) |
| S1-3 TypeORM gotcha with no TypeORM | yes | n/a | flipped (recipe now uses TypeORM, gotcha appropriate) |
| S1-4 worker→db ghost dep | 1 | 0 | -1 ✓ |
| S1-5 refinement non-write-back | 5 fragments | 0 | -5 ✓ |
| S1-6 NATS pattern staleness | yes | 0 | -1 ✓ |
| S2-1 yaml-comment IG/KB cross-refs | 6+ | 4 | partial |
| S2-2 engine-vocab in TIMELINE | many | many (sanitizer narrower than plan) | unchanged on disk; partial close at export |
| S2-3 tier-0 meta-agent-voice | 1 | 1 (TIMELINE-narrated as by-design) | unchanged |
| S3-1 project ID + workspace URLs in TIMELINE | yes | yes on disk; partial-redaction at export | partial |
| S3-2 `prg1.zerops.app` zone literal | 25+ | ~10 (similar pattern, narrower count in v40 corpus) | partial |
| S4 fake specificity | 7 | 1 (the `/api/…` paths) + soft N-4 | better |
| S6 dead code | 6+ | not deeply audited | — |
| S8 "14 services" miscount | 1 | 0 (TIMELINE says 11) | -1 ✓ |
| **NEW** N-1 `${search_password}` | 0 | 1 | new |
| **NEW** N-3 `/api/...` endpoint paths in facts/yaml | 0 | 8 (1 yaml + 7 facts) | new |
| **NEW** N-4 MEILI_SEARCH_KEY prescription unimplemented | 0 | 2 (api+worker KB) | new |
| **NEW** N-6 `S3_FORCE_PATH_STYLE` stale fact | 0 | 2 facts | new (deliverable surfaces unaffected) |

Net: ~13 run-39 defect classes closed, ~5 remain unchanged at expected magnitudes (S-1/S-4/S2-1/S3-2/S1-2 — all substrate-only items not on Phase E), 4 new defect classes surfaced (3 of which are agent-authoring noise that doesn't reach load-bearing porter surfaces, 1 of which (N-1) directly contradicts the yaml content right next to it).

---

## Substrate-internalization signal — gate fire counts

Brief artifacts present in `.briefs/`:
- scaffold: 2 disk files (api + app — worker brief was inline in TIMELINE, not on disk)
- feature: 2 disk files (backend + frontend pass)
- codebase-content: 3 sub-agent directories (api, app, worker), each 8 part-files
- env-content: 1 directory, 6 part-files
- refinement: 1 directory, 7 part-files

TIMELINE.md narrates the gate-fire surface:
- Scaffold: ok:true on all three sub-agents first try.
- Feature: ok:true on both passes; mid-phase plan-update (dropped `db` from worker.consumesServices).
- Codebase-content: 6 sub-agents, all ok:true. Iteration: first pass surfaced `kb-citation-missing` on api + worker KBs; sub-agents fixed by adding cite-by-name shape (1 fix-iteration).
- Env-content: 1 sub-agent, ok:true with 3 advisory notices (tier-0 meta-voice + two tier-5 missing-causal-word — addressed in autonomous-loop polish).
- Finalize: refused first call → required refinement → refinement sub-agent ACT-3 attempts reverted by snapshot/restore (transactional wrapper firing as designed) → main-agent took over with dual-form citation → re-stitch + complete-phase: ok:true.
- Final state: "all completed, 0 violations, 0 blocking notices on final close".

No retry storms. Engine version stamped `v9.83.0` ([plan.json:3](docs/zcprecipator3/runs/40/environments/plan.json#L3)). One genuinely interesting failure surface: refinement-phase snapshot/restore reverted 3 ACT attempts because they introduced new violations on the slug-stem-as-link-text rule. The per-fragment edit cap of 1 prevented an infinite loop. Main agent picked up the work cleanly.

vs run-39 floor: forensics report claimed 817 tool calls in run-39 (per plan). Run-40 main-session.jsonl is the canonical log; not exhaustively audited here but the issues table in TIMELINE.md:113-122 lists only 5 blockers + 2 recoverables + 1 advisory across the run. Vastly better signal-to-noise.

---

## Side-by-side vs run-39 on engine-fix-touched surfaces

| Surface | Run-39 | Run-40 |
|---|---|---|
| plan.json fragments vs disk-stitched (ENG-1) | 5 stale fragments | byte-for-byte sync at every spot-check |
| TIMELINE.md author-data leaks (ENG-2) | project ID + hostname-hashes + machine paths + "14 services" | sanitizer at export covers /var/www/zcprecipator + hostname-with-port + service count; on-disk still has project ID + session UUID + no-port stage hostname + engine-vocab |
| Parent-recipe citations in porter prose (ENG-3) | "See parent recipe `nestjs-minimal` for ..." in multiple READMEs | 0 hits |
| Queue-group named constant (ENG-4 / A1) | 4-fold split: `'workers'` / `'showcase-workers'` across 30+ places | unified `worker-indexer` in 30+ places |
| Worker consumesServices (ENG-5) | `["broker","db"]` ghost-dep | `["broker","cache","search","storage"]` source-derived |
| Stitched↔plan diff (ENG-6) | divergent (proved by ENG-1 findings) | converged at finalize |
| Dead envs in zerops.yamls (B1) | 3 (SEARCH_PUBLIC_HOST, SEARCH_SEARCH_KEY, NATS_QUEUE_GROUP) | 0 |

---

## Recommended next action

**Iterate to run-41 with a narrow scope**, then ship as canonical recipe. The remaining gaps are addressable in hours-to-day, not days-to-weeks.

## Run-41 closure summary

Engine work landed for the following items from the original recommendation list:

1. **ENG-2 sanitizer widening — LANDED.** [internal/sync/timeline_sanitizer.go](internal/sync/timeline_sanitizer.go) now redacts the four leak shapes the run-40 deliverable surfaced through the original regex:
   - **Bare-paren project ID** — new `projectIDProjectKeywordRedactor` matches `Zerops project `<slug>` (`<id>`)` shape the TIMELINE prompt actually emits. Original `(id `xxx`)` redactor kept for back-compat.
   - **Session UUID** — new `sessionUUIDRedactor` matches `Session: `<uuid>`` lines.
   - **No-port stage hostname** — new `hostnameHashNoPortRedactor` matches `<host>stage-<digits>.<zone>.zerops.app` (base: static runtime URLs with no port suffix).
   - **Engine-vocab tokens** — new `engineMCPToolRedactor` (`zerops_*`) + `enginePhaseCommandRedactor` (`complete-phase` / `enter-phase` / `record-fragment` / `stitch-content` / `build-subagent-prompt` / etc.). Both replace with `<engine-detail>`. Whitelist preserves porter-facing `zerops.app`/`zerops.io`/`zerops.yaml`/`zsc`.
   - **Test pins:** `TestSanitizeTimeline_ProjectIDBareParenForm`, `TestSanitizeTimeline_SessionUUID`, `TestSanitizeTimeline_HostnameHashNoPort`, `TestSanitizeTimeline_EngineMCPToolNames`, `TestSanitizeTimeline_EnginePhaseCommands`, `TestSanitizeTimeline_Run40FixtureFullCoverage`. End-to-end test runs the sanitizer against the actual run-40 TIMELINE-shape fixture and asserts every leak class is scrubbed.
   - **Verified:** simulator run over the actual on-disk `docs/zcprecipator3/runs/40/TIMELINE.md` produces output where `f1NS28GZRByGbQz3WaihAw`, `ca8266e6-...`, `appstage-2311.prg1.zerops.app`, `zerops_import`, `zerops_discover`, `zerops_verify`, `complete-phase`, `stitch-content` are all replaced.

2–5. **Cross-surface audit sub-agent (refinement-2) — LANDED.** [internal/recipe/briefs_refinement2.go](internal/recipe/briefs_refinement2.go) + [internal/recipe/content/briefs/refinement2/](internal/recipe/content/briefs/refinement2/). Diagnosis-only sub-agent dispatched between refinement-1 and finalize-close. The audit_checklist atom encodes ten defect classes the run-40 deliverable surfaced — including the run-41 priorities (2)–(5):
   - `yaml-comment-content-drift` (closes **N-1** class) — flags any `${<host>_<key>}` in a yaml comment that doesn't exist in the same yaml's envVariables block AND isn't a documented Zerops alias.
   - `aspirational-as-current` (closes **N-2 JWT** and **N-4 MEILI_SEARCH_KEY** classes) — flags present-tense prose claims that name an env var or framework feature whose backing dependency / yaml declaration is absent.
   - `kb-ig-duplication` — flags KB bullets that restate the IG-item fix without adding the symptom dimension (closes the run-40 workerdev KB↔IG #1/#2/#3 + appdev KB↔IG #1/#3 duplication classes).
   - `surface-misplacement` / `scaffold-code-in-kb` — flags KB bullets citing `src/<path>` (closes the run-40 appdev KB #4 `bus.js` class).
   - `cross-codebase-named-constant-drift` — defense in depth against the run-39 queue-group split.
   - `ig-cites-recipe-internal-file` + `missing-citation` + `kb-below-floor`/`kb-over-cap` — additional cross-surface defect classes the audit walks.
   - **Wiring:** new `BriefRefinement2` kind + `Refinement2Dispatched` session flag + `complete-phase phase=refinement` gate refusing closure until both refinement-1 and refinement-2 dispatch. Schema field updated. Multi-file disk-fallback NOT used (brief is small enough for single-file path).
   - **Test pins:** `TestBuildRefinement2Brief_AssemblesCoreAtoms`, `_CarriesAuditDefectClasses`, `_StitchedPointerBlockListsAllSurfaces`, `_NoFilesystemReferenceLeak`, `_EmptyRunDirSkipsStitchedPointerBlock`, `_CitationMapMatchesSpec`. Gate tests: `TestBuildSubagentPromptRefinement2_FlipsDispatchFlag`, `TestCompletePhaseRefinement_RefusesWithoutRefinement2`, `TestCompletePhaseRefinement_RefusesWithoutRefinement1`.
   - **N-3 `/api/...` path drift** is NOT explicitly named in the audit checklist (different shape — facts.jsonl `contract` entries vs source endpoints, not a cross-surface check). Tracked separately for run-42 if it recurs.

6. **S-1 dev-server-restart-re-reads-env** — deferred (stretch; needs live Zerops empirical test before the substrate edit lands). Unchanged.

What the refinement-2 audit does NOT replace:

- The audit is **diagnosis-only**. It emits a fenced JSON findings list; the main agent reads the findings and decides per-finding whether to ACT, HOLD, or accept-as-known. No auto-fix at the audit boundary — that design choice avoids the cross-rule conflict pattern that reverted refinement-1's ACT attempts in run-40 (transactional snapshot/restore wrapper reverted 3 edits on slug-stem rule conflict).
- It does not catch per-fragment voice issues (V1/V2/V3 violations) — those stay refinement-1's scope.
- It does not catch defects between the deliverable and the source-of-truth Zerops platform (e.g., "is `${search_password}` actually a valid alias?") — the audit relies on the engine's documented cross-service alias list, not a live API call.

After run-41 ships and the next dogfood proves the audit closes the named classes, the recipe is on solid ground for canonical publication.

## Run-41 dual-review fixes (post-implementation)

Independent codex + fresh-eyes review of the run-41 implementation surfaced 5 structural defects that landed before initial commit:

1. **Fragment-ID surface routing in audit_checklist.md** — `codebase/<host>/zerops-yaml` maps to S7 (`SurfaceCodebaseZeropsComments`) per [surfaces.go:313-317](internal/recipe/surfaces.go#L313-L317); my v1 checklist classified it under S4 (IG). Findings citing fragmentId would have mis-routed. Fixed: the fragment ID table in [audit_checklist.md](internal/recipe/content/briefs/refinement2/audit_checklist.md) now explicitly notes IG #1 is engine-emitted from S7, and tier surfaces (S2/S3) are routed to the typed `plan.EnvComments` store, not `plan.fragments`.

2. **`aspirational-as-current` missed tier-yaml prose (S3)** — v1 rule only walked KB/IG prose; the run-40 N-2 JWT claim at `environments/0 — AI Agent/import.yaml:4` ("APP_SECRET... so JWT verification holds across containers") sits on S3 and would have escaped the audit. Fixed: rule extended to cover all six prose surfaces (S2 + S3 + S4 + S5 + S6 + S7) with a tier-yaml-specific check sub-section + an explicit "Framework-feature manifest scan" sub-section enumerating `package.json` / `composer.json` / `pyproject.toml` / `requirements.txt` as cross-check targets.

3. **Pointer block missing per-codebase dependency manifests** — `aspirational-as-current` asks the agent to read `package.json` / `composer.json` for framework-feature claim verification, but [briefs_refinement2.go](internal/recipe/briefs_refinement2.go) v1 only listed README + zerops.yaml + CLAUDE.md per codebase. Fixed: composer now emits a "Per-codebase dependency manifests" pointer block enumerating candidate manifest paths (Node / PHP / Python).

4. **codebase= edge-case in build-subagent-prompt** — `requiresCodebase(BriefRefinement2)=false` but the gate `if in.Codebase == ""` only fires on the no-codebase path. A confused caller passing `codebase=api briefKind=refinement2` would have flipped `Refinement2Dispatched=true` after a brief that only covered one codebase, skipping the cross-codebase relationship checks the audit exists to run. Fixed: [handlers.go](internal/recipe/handlers.go) now refuses `build-subagent-prompt briefKind=refinement2` when `codebase` is non-empty, BEFORE any side effect. Pinned by `TestBuildSubagentPromptRefinement2_RefusesCodebaseScope`.

5. **`scaffold-code-in-kb` too narrow** — v1 rule only matched `[src/<path>]` link form, missing bare-backtick and bare-prose forms (`"the recipe wires a small refresh bus in src/lib/bus.js"`). Fixed: rule body now enumerates all three shapes explicitly and lists "what this codebase does" prose patterns (poll intervals, refresh-bus shape) the rule should flag.

Both reviews converged on three concerns not fixed (out of scope or by design):

- **Diagnosis-only contract is honor-system** — by design, documented at [phase_entry.md](internal/recipe/content/briefs/refinement2/phase_entry.md) §"What you do NOT do" and at the dispatch close footer. No engine-side `record-fragment` refusal; if the audit sub-agent disobeys, the main agent detects deviation by reading the transcript. Trade-off accepted to avoid the cross-rule conflict pattern that reverted refinement-1's ACT attempts in run-40.

- **Path semantics in pointer block** (`<runDir>/environments/...` vs `<runDir>/...`) — codex flagged this as a defect; verified that the inherited convention from [briefs_refinement.go:128-129](internal/recipe/briefs_refinement.go#L128-L129) produces the same path shape, and refinement-1 has shipped successfully across multiple runs. Not a refinement-2-specific regression; fix lives at the orchestrator/`outputRoot` layer if it lives anywhere. Tracked for run-42 if a dogfood failure surfaces.

- **`engineMCPToolRedactor` over-eager on hypothetical `zerops_*` porter tokens** — no actual false positive in run-40 TIMELINE. Tighten only when a future run produces one.

Test count after fixes: 6 brief-composer tests + 4 gate/dispatch tests + 5 sanitizer regression tests. Full repo `go test ./... -short` + `make lint-local` green.

## Run-41 dual-review round 2 (post-fix validation)

A second round of codex + opus-subagent review against the post-round-1 implementation, this time tasked with simulating the audit end-to-end against the actual run-40 deliverable. Round 2 surfaced 5 more issues — 4 P0/P1 fixes landed, 1 deferred:

1. **yaml-comment-content-drift documented-alias list was service-type-agnostic (P0)** — round-1 listed `${<host>_password}` as a generic Zerops alias, which would have let `${search_password}` pass the rule's check despite the worked example explicitly flagging it. A literal-reading sub-agent would NOT have detected N-1. Fixed: replaced the generic list with a per-service-type allowlist table (postgresql/valkey publish `password`; meilisearch publishes `masterKey`/`defaultSearchKey` and NOT `password`; nats publishes its own subset; object-storage publishes the S3 cluster). The audit must now identify the host's service type from `plan.services[].type` and validate against the type-specific allowlist. Pinned by `TestBuildRefinement2Brief_PerServiceTypeAliasAllowlist`, which includes a negative check that the meilisearch row does NOT contain `password`.

2. **surface-misplacement was in the phase-entry enum but had no rule body (P1)** — round-1 merged it with `scaffold-code-in-kb` under one rule header, leaving the broader surface-misplacement class (e.g., framework-setup in KB, recipe-internal IG, Zerops content in CLAUDE.md) without a check definition. The phase-entry enum at [phase_entry.md:23](internal/recipe/content/briefs/refinement2/phase_entry.md#L23) allowed the value; the checklist had no rule to fire. Non-deterministic output. Fixed: split into two distinct defect-class headers in [audit_checklist.md](internal/recipe/content/briefs/refinement2/audit_checklist.md). `surface-misplacement` now has its own check definition (anchored on the per-surface one-question editorial tests) + clear action mapping. `scaffold-code-in-kb` becomes a named sub-case with its concrete signature.

3. **kb-below-floor `suggestedAction` enum mismatch (P1)** — round-1 rule said "no auto-suggest. The main agent decides", but the JSON output schema in `phase_entry.md` requires `suggestedAction` to be one of a fixed enum. A finding emitted without the field would have failed schema validation. Fixed: rule explicitly emits `suggestedAction: "drop"` as the conservative fallback, with rationale explaining the main agent decides (add bullets from facts log for below-floor; rank-and-cut for over-cap).

4. **S4 IG floor not actually checked (P1)** — caps/floors table at the top of audit_checklist listed an S4 IG floor (4 items) but the actual `kb-below-floor/kb-over-cap` rule body only counted S5 KB H3 headings. IG floors would have silently slipped past the audit. Fixed: rule body now has explicit Check S5 KB + Check S4 IG sections; caps table updated to show defect-class mapping for both surfaces.

5. **Citation map drift between brief-composer's inlined map and audit_checklist's duplicate list (P1)** — round-1 had `missing-citation` rule listing its own hardcoded topic→guide pairs (5 entries) while the composer's inlined citation map block had 7 entries (added managed NATS + managed Meilisearch). A sub-agent reading the audit_checklist would miss the two extra topics. Fixed: missing-citation rule now points at the engine-rendered `## Citation map` block in the same brief instead of duplicating; the citation map block is authoritative. One source of truth.

Deferred (acknowledged design tradeoff or speculative):

- **Honor-system contract has no engine-side guardrail for record-fragment** — opus flagged P1; same as round-1 codex Q4. Both rounds documented as design choice — the alternative (transient `inRefinement2Audit` flag on session) complicates the protocol because the main agent legitimately needs `record-fragment` AFTER reading findings. Trade-off accepted: contract enforced by prompt + close-footer; main agent detects deviation via transcript inspection.
- **`forcePathStyle` citation over-fire risk** — speculative; would not fire on the run-40 deliverable. Tighten if a future run produces a false positive.
- **`cross-codebase-named-constant-drift` silent skip when canonical facts empty** — design gap, not a current defect; refinement2 fires post-finalize when facts are populated.
- **Defensive `plan.Codebases` empty check** — refinement2 fires post-finalize; codebases must be populated by then.

Per-defect catch-rate verdict from round-2 simulated audit run: **6/6 ground-truth defects from run-40 caught** (N-1, N-2 ×3 sites, N-4 ×2 sites, KB↔IG ×5 pairs, kb-below-floor, scaffold-code-in-kb). N-3 explicitly out of scope as designed. No false positives on legitimate apidev KB content (7 bullets scanned individually).

Test count after round-2: 7 brief-composer tests + 4 gate/dispatch tests + 5 sanitizer regression tests = 16 total refinement-2 + sanitizer test pins. Full repo `go test ./... -short` + `make lint-local` green.
