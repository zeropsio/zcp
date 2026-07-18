# Run-40 evidence-grounded plan

> **THIS IS THE DEFINITIVE FILE.** A fresh Claude instance should read THIS document end-to-end, no others. Working notes from the deep-dive process live at `/tmp/run-39-*-findings.md` (6 files) as backup evidence; everything load-bearing is consolidated here.

**Status**: synthesized from deep-dive across ALL run-39 artifacts — 817 tool calls (timeline), 13,761 lines of substrate briefs across 45 files (5 phase catalogs), ~5,700 lines of deliverable surfaces. Every defect traced from deliverable → substrate root cause. **All 10 falsifiable claims independently confirmed by codex final validation.** Run-40 scope corrected post-codex (~10-12 days, not 3-5).

This document supersedes [`run-39-reconciliation.md`](run-39-reconciliation.md). The reconciliation went through 6+ wrong-or-partial diagnoses; this is the first version where every load-bearing claim survived independent verification.

---

## TL;DR

Run-39 produced **~30 substantive defects** across 4 severity tiers:

| Tier | Class | Count | Source |
|---|---|---|---|
| S0 | Defects breaking porter behavior | 6 | Code-vs-doc drift, dead envs, wrong HTTP status |
| S1 | Cross-file content drift | 6 | Stale facts.jsonl entries, refinement-vs-plan.json divergence |
| S2 | Engine-vocabulary leakage to porter surfaces | 10+ | Yaml-comment IG/KB refs, validator IDs in TIMELINE |
| S3-S9 | Author data leaks, fake specificity, dead code, count errors | 15+ | TIMELINE meta-narrative, recipe-author lore in seed data |

**The single biggest finding nobody surfaced before:** **refinement-phase doesn't write back to `plan.json`.** Run-39's refinement applied 4+ ACT-edits to README outputs successfully; the corresponding `plan.json` fragments still contain the old stale content. If anyone re-stitches from `plan.json` (or the engine re-renders), run-39's refinements vanish.

**Root causes (substrate + engine):**

1. **Brief generator emits forbidden-shape templates** — `briefs_content_phase.go:608-609` (codebase-content) and `:614-615` (env-content) literally hand the agent the wording `"See parent recipe \`%[1]s\` for <topic>"` as a positive example. This template propagates to 6 brief files. Refinement-phase warning was supposed to catch the result — it didn't, because the agent followed substrate-positive-shape over refinement-conditional-warning.

2. **Refinement is non-write-back** (NEW finding): `record-fragment mode=replace` during refinement updates on-disk stitched files but NOT `plan.json` fragments. Refinement's transactional snapshot/restore is described in substrate (refinement/part-1:71-87) but the persistence layer skips plan.json.

3. **Substrate has multiple internal self-contradictions** that produce trap behavior:
   - Dev-server-restart-re-reads-env brief lie (in scaffold AND feature briefs)
   - Worker brief tells agent to cite `rolling-deploys` topic, then forbids slug-stem in link text
   - Env-content brief invites bumping `minContainers` as friendly-authority example, then forbids it as cross-tier ladder move
   - Voice patterns: imperative `Deploy/Spin up` (part-1) vs MIX descriptor (part-5/refinement-part-3) — competing rules
   - Tool param naming: `targetService=` vs `slot=` (same tool, different params in same brief)
   - "Two only-deploys" contradiction in feature brief
   - Backend feature brief shares 818 lines with frontend brief; both agents read each other's content; then briefs declare role-specific scope

4. **Facts.jsonl is append-only** — when facts are superseded (e.g. queue-group rename), old facts stay. Downstream phases (env-content, refinement) read the full stream and can't disambiguate. Q2 cross-codebase drift is partly caused by facts-stream ambiguity.

5. **Engine-internal vocabulary leaks via TIMELINE** — recipe-author's project ID, workspace URLs, machine paths all ship in `TIMELINE.md` deliverable.

---

## Defect catalog with causal chains

For each defect: defect → deliverable file:line → substrate root cause → fix.

### S0-1: StatusController hardcodes wrong hostname

- **Where**: `apidev/src/status/status.controller.ts:46` hardcodes `{ hostname: 'api' }`
- **Symptom**: SPA `data-test="status-api"` mismatches platform hostname at tiers 0/1 (apidev/apistage); dashboard partially broken
- **Substrate trace**: substrate doesn't enforce hostname-vs-codebase distinction at code-generation time. Scaffold-brief teaches `Plan.Codebases[].Hostname` is bare codebase name (api), but the agent should generate code that READS the runtime hostname, not hardcode it
- **Fix**: substrate teaching addition + source-code refactor at feature phase. Lines of code; days if structural

### S0-2: `process.env.hostname` (lowercase) bug

- **Where**: `workerdev/src/jobs/jobs.consumer.ts:37`, `workerdev/src/main.ts:16`
- **Symptom**: Linux env var is `HOSTNAME` (uppercase); the lowercase env var is always undefined; fallback `'worker'` always wins
- **Substrate trace**: substrate doesn't enforce env-var casing convention. Agent picked a JS-friendly camelCase reading
- **Fix**: code-fix `HOSTNAME` (uppercase) + substrate addition about Linux env-var casing convention

### S0-3: Worker hardcodes `'workers'` but README claims env-driven

- **Where**: 
  - `workerdev/src/jobs/jobs.consumer.ts:134`: literal `'workers'`
  - `workerdev/README.md:148-156`: IG example shows `process.env.NATS_QUEUE_GROUP ?? 'workers'`
  - `workerdev/README.md:158`: explicit claim *"so you can rename it from yaml without touching code"*
  - `workerdev/zerops.yaml:55`: `NATS_QUEUE_GROUP: workers` (dead env)
- **Symptom**: Recipe lies to porter about how to rename queue group
- **Substrate trace**: facts.jsonl recorded queue-group as `'showcase-workers'` initially, then renamed to `'workers'` (fact #78). Code was written hardcoded; agent later authored README example with env-driven pattern (lifted from substrate boilerplate); the discrepancy between code and IG wasn't caught
- **Fix**: feature-phase gate: every named constant referenced in yaml.envVariables must be `process.env.X`-read in source. Or: A1 `plan.namedConstants` — single source for queue-group string

### S0-4: `${broker_connectionString}` contradicts itself

- **Where**:
  - `apidev/README.md:278`: "crashes with Authorization Violation"
  - `workerdev/README.md:227`: "ALSO avoids the double-auth path"
- **Symptom**: Recipe contradicts itself on the SAME library behavior
- **Substrate trace**: facts.jsonl #26 says `${broker_connectionString}` crashes; worker authored its KB independently; codebase-content-worker sub-agent didn't read api's KB before authoring
- **Fix**: cross-codebase KB-consistency check at refinement (current substrate has the rule, refinement just doesn't apply it). Or: A1 plan.namedConstants for technical claims about service behavior

### S0-5: Terminus health-check claim mismatches framework

- **Where**: `apidev/README.md:55-58`, `apidev/zerops.yaml:31-35`
- **Symptom**: Comments claim *"/health answers HTTP 200 once Postgres pings successfully"*. Terminus actually returns 503 when `status: 'down'`. The comment is misleading about always-200.
- **Substrate trace**: Substrate doesn't ground HTTP claims in source-code-verification. Agent wrote the comment without checking Terminus framework behavior.
- **Fix**: substrate-only fix to require numeric HTTP claims to be grounded in source-or-spec citation. Hard to enforce mechanically. Or: just remove the "200" claim; say "depending on Postgres ping result".

### S0-6: Dead env vars (3 confirmed)

- **Where**:
  - `apidev/zerops.yaml`: `SEARCH_PUBLIC_HOST: ${search_zeropsSubdomain}` — never read by source AND `${search_zeropsSubdomain}` requires `enableSubdomainAccess: true` on search service (no tier enables it)
  - `apidev/zerops.yaml`: `SEARCH_SEARCH_KEY: ${search_defaultSearchKey}` — never read by source
  - `workerdev/zerops.yaml`: `NATS_QUEUE_GROUP: workers` — worker hardcodes literal
- **Symptom**: Three envs declared with no consumer; KB fabricates downstream wiring that doesn't exist
- **Substrate trace**: Agent free-authored env-key set without source-grep. No engine check `declared ⊆ source-reads`
- **Fix**: Phase B `plan.observedFacts.envReads[codebase]` — derive env-key set from source-grep; reject declarations not in source-reads. Days of code.

### S1-1: Queue-group cross-file name drift

- **Where**: `'workers'` in 15+ places (source + READMEs + facts.jsonl after rename) vs `'showcase-workers'` in 15+ places (all 6 tier import.yamls + plan.json envComments + TIMELINE)
- **Symptom**: Tier yamls describe queue group with a name source code doesn't use; porter reading tier yaml sees `showcase-workers`, looks in code, finds `workers`
- **Substrate trace**: scaffold-phase initially used `'showcase-workers'` (per facts.jsonl:#7); feature-phase renamed source to `'workers'` and recorded the rename (fact #76); env-content phase read facts and authored tier yaml comments using OLD facts (#7, #14). Append-only facts.jsonl + env-content not reading "latest by topic" = cross-codebase drift
- **Fix**: Phase A1 `plan.namedConstants["NATS_QUEUE_GROUP"] = "workers"`. Single source. All surfaces render. Closes Q2 permanently.

### S1-2: JWT verification claims with no JWT code

- **Where**: `apidev/README.md:80-84,247`, all 6 tier import.yamls
- **Symptom**: Recipe references JWT verification behavior that doesn't exist in source
- **Substrate trace**: Likely lifted from Laravel showcase recipe substrate (Laravel uses JWT). The `same-key shadow trap` example in substrate uses JWT as the worked example; agent absorbed the example into the new recipe verbatim
- **Fix**: substrate update: same-key-shadow worked example needs to be framework-neutral or recipe-specific. Hours.

### S1-3: TypeORM `synchronize: true` claim with no TypeORM

- **Where**: `apidev/README.md:351-353` — *"TypeORM `synchronize: true` corrupts schema..."*
- **Symptom**: Recipe cites a gotcha against a framework it doesn't use (raw `pg` not TypeORM)
- **Substrate trace**: Parent recipe (nestjs-minimal) uses TypeORM. Scaffold/env-content/refinement briefs all embed the parent recipe baseline (~100 lines each) including the TypeORM gotcha. Agent absorbed parent material verbatim
- **Fix**: Parent-recipe-baseline filter: only inherit gotchas that apply to the CHILD recipe's actual code. If child uses raw `pg`, drop TypeORM gotchas. Substrate filter at brief-generation time. Hours.

### S1-4: Worker → db ghost dependency in plan.json

- **Where**: `environments/plan.json:36-42`: `consumesServices: ["broker","db"]` for worker
- **Symptom**: Plan claims worker depends on Postgres; source has no db client
- **Substrate trace**: Likely scaffold-phase symbol-table extraction picked up an early decision that worker would use Postgres; feature-phase removed Postgres usage; plan.json wasn't updated
- **Fix**: scaffold-phase post-feature consistency check: `consumesServices` set must be derivable from source-grep. Same Phase B pattern as orphan envs.

### S1-5: Refinement-phase non-write-back to plan.json (NEW CRITICAL)

- **Where**: 
  - `plan.json:176` (codebase/api/integration-guide/3): trailing cross-framework sentence — README has it dropped, plan.json still has it
  - `plan.json:177` (codebase/api/integration-guide/4): heading "Alias cross-service env vars under your own keys" — README has new heading, plan.json still has old
  - `plan.json:183` (codebase/app/integration-guide/2): multi-framework adapt-paths — README has Vite-only, plan.json still has multi-framework
  - `plan.json:184` (codebase/app/integration-guide/3): cross-framework tail — README dropped, plan.json kept
  - `plan.json:192` (codebase/worker/integration-guide/4): `${db_*}` reference — README dropped, plan.json kept
- **Symptom**: Refinement is LOSSY. Disk-stitched files reflect the refinements; plan.json doesn't. Re-rendering from plan.json regenerates pre-refinement content.
- **Substrate trace**: Refinement substrate (refinement/part-1:71-87) describes snapshot/restore for `codebase/<host>/...` fragments and "slot-shape refusal at record time" for root/env fragments. **Engine implementation skips the plan.json write-back**
- **Fix**: ENGINE BUG. `record-fragment mode=replace` during refinement must update plan.json fragments AND the disk-stitched output. Hours of engine code.

### S1-6: NATS Pattern claim staleness

- **Where**: facts.jsonl #2, #3, #4 (worker patterns)
- **Symptom**: Facts claim worker uses `@nestjs/microservices Transport.NATS` with `EventPattern` decorator; source uses raw `nats` client (`connect()`)
- **Substrate trace**: Scaffold-phase decision (used `@nestjs/microservices`); feature-phase reimplemented with raw nats (per facts #21); scaffold facts not superseded
- **Fix**: Same as S1-1 — facts-stream needs latest-by-topic semantics, OR explicit `replace-by-topic` for superseded facts. Combine with A1.

### S2-1: "See IG #4" / "(KB: ...)" yaml-comment cross-refs

- **Where**: 6+ places in apidev/zerops.yaml + workerdev/zerops.yaml
- **Symptom**: Yaml gets copy-pasted to apps-repos; porters reading yaml only see dangling refs
- **Substrate trace**: codebase-content/part-2-synthesis.md teaches "yaml comments cross-reference IG/KB" pattern (line 196-205 says cross-reference is not license to restate; line 207-214 says if yaml comment runs more than ~6 lines after a cross-reference, body over-reached). But the cross-reference SHAPE the agent learned is `(see IG #N for...)` which leaks engine doc-graph into yaml
- **Fix**: substrate edit + post-process regex strip. Trivial.

### S2-2: Engine-vocabulary in TIMELINE

- **Where**: `TIMELINE.md` throughout — tool names, validator IDs, sub-agent role names, phase names, rule IDs
- **Symptom**: Deliverable contains engine-internal vocabulary porters don't understand
- **Substrate trace**: TIMELINE is engine-emitted post-run summary; substrate likely treats TIMELINE as engine-author-meta and doesn't enforce porter-voice rules
- **Fix**: ENGINE FIX — sanitize TIMELINE before ship. Strip tool names, validator IDs, sub-agent names, machine paths, project IDs, hostnames. Hours of engine work.

### S2-3: "AI coding agents iterate" meta-voice in tier-0 README

- **Where**: `environments/0 — AI Agent/README.md:6`
- **Symptom**: `meta-agent-voice` validator fires (TIMELINE:209); agent admits "unavoidable" and ships
- **Substrate trace**: tier label is canonical "AI Agent" per engine `tiers.go`; tier-0 README intro inherits the label semantics
- **Fix**: tier-0 substrate needs explicit non-meta-narrative phrasing. Or accept the validator as non-blocking notice.

### S3-1: Author project ID + workspace URLs in TIMELINE

- **Where**: `TIMELINE.md:264-272`
- **Symptom**: Real Zerops project identifier `7HfLxoquTxiNEg1fD4Xo7w`, project hash `2304`, real workspace URLs ship in deliverable
- **Substrate trace**: TIMELINE engine-emitter doesn't sanitize. Author data leaks because the timeline is generated from session-log metadata
- **Fix**: ENGINE FIX in TIMELINE generation — replace project ID, hostname hashes, and paths with `<project>`, `<host>`, `<output-root>` placeholders. Hours.

### S3-2: Hardcoded `prg1.zerops.app` zone

- **Where**: 25+ literal references across yamls, READMEs, examples
- **Symptom**: Recipe doesn't generalize to other Zerops zones
- **Substrate trace**: Substrate uses `prg1.zerops.app` in worked examples; agent absorbs literal. Zone should be `${zeropsSubdomainHost}` template
- **Fix**: substrate edit + post-process regex replace at finalize. Hours.

### S4 — Fake specificity (7 instances)

- "10-200ms broker latency" (appdev/README.md:176)
- "third production tier upward" (workerdev/README.md:197) — numerically wrong (only 2 prod tiers)
- "within seconds" Meilisearch indexing
- "stage SLO" reference
- "a few MB of RAM"
- "1 GB ceiling" on search (actually `minRam: 1` is a floor)
- "stage screenshot artifacts" (recipe-internal browser-walk concept in porter yaml)

**Substrate trace**: Substrate doesn't ground numeric/specific claims in source. Substrate teaches "lead with concrete porter signal" but doesn't enforce verifiability.

**Fix**: refinement-phase factuality check on numeric claims — every "Xms", "Y GB", "N tiers" must cite source. Hard to mechanize. Easier: substrate rule "avoid numeric specificity unless backed by code/spec; prefer qualitative".

### S6 — Code-quality / dead code (6+ items)

- Placeholder lint/test scripts in worker
- Unused constants (`JOBS_PING_SUBJECT`, `WORKER_QUEUE_GROUP` in api, `lpushTrim` method)
- `ListObjectsV2` MaxKeys=1000 then truncate to 10
- Dead Vite preview config
- Hidden test-instrumentation `data-test` in production svelte
- `items.create` throws bare Error (returns 500 instead of 400)
- `updated_at` column written but never returned

**Substrate trace**: feature-phase substrate emphasizes "demonstration-first" but doesn't enforce code-quality bar. No engine gate for dead code, unused exports, etc.

**Fix**: deferrable. Code-quality is not the primary defect class. Most don't affect porter behavior materially.

### S8 — "14 services" count error in TIMELINE

- **Where**: `TIMELINE.md:29`
- **Symptom**: Claims 14 services provisioned; actually 11
- **Substrate trace**: TIMELINE author lost count
- **Fix**: TIMELINE sanitizer should derive count from `plan.Services` instead of free-text claim. Trivial.

---

## The actual run-40 plan (codex-corrected)

**Total scope: ~10-12 days of engineering, not 3-5 as originally estimated.** Codex flagged that B-phase items are not optional for a clean baseline, that A1 needs explicit tier-yaml-renderer binding, and that the methodology can't catch framework-semantic falsehoods at all.

### Phase E — Engine fixes (highest priority; ~5-7 days)

**ENG-1**: **Refinement-phase write-back to plan.json fragments.** *(1-2 days)*
- Current: `record-fragment mode=replace` during refinement updates disk-stitched files only
- Fix: also update `plan.json.Fragments[<id>]` so re-rendering preserves refinement
- Pin: extend `TestCompletePhaseRefinement_FlipsClosedFlagAndWritesMarker` to verify plan.json sync
- Closes: S1-5 (refinement non-write-back). Confirmed by codex against 4+ stale fragments in run-39's plan.json

**ENG-2**: **TIMELINE sanitizer at finalize + tests.** *(1 day)*
- Engine post-processor that strips: project IDs, hostname hashes (e.g. `2304`), author machine paths, engine-internal tool names, validator IDs, sub-agent role names, rule IDs (V1-V6 etc.)
- Replace with placeholders or remove the lines entirely
- **Codex correction**: needs test pins for what gets sanitized before relied on
- Closes: S2-2, S3-1, S3-2, S8

**ENG-3**: **Brief-generator template edit — remove "Cross-reference shape" worked example.** *(hours)*
- Edit `internal/recipe/briefs_content_phase.go:605-615` (lines verified by codex)
- Drop the `"Cross-reference shape when parent already covers a topic: See parent recipe \`%[1]s\` for <topic>"` clause from both `codebaseContentEmbeddedFraming` AND `envContentEmbeddedFraming`
- Keep the `"Parent slug: %[1]s"` knowledge-routing prefix — subagents need it
- Update `briefs_r3_embedded_parent_test.go` to assert the template is NOT emitted
- Closes: Q1.a parent-recipe citation. Root cause confirmed by codex deliverable forensics

**ENG-4**: **Facts.jsonl topic normalization + latest-by-topic semantics.** *(1-2 days)*
- Current: env-content + refinement read full append-only fact stream
- **Codex correction**: not enough to just take latest by topic. Facts aren't normalized — same concept can be recorded under different topic strings (`worker-nats-queue-group`, `worker-raw-nats-subscribe-queue-group`, `worker-queue-group-renamed-workers` all describe the same concept)
- Need: topic normalization pass that maps semantic-equivalent topics to canonical key, THEN latest-by-canonical-key
- Pin: env-content reads canonical `nats-queue-group` returns `'workers'` not `'showcase-workers'`
- Closes: S1-1 partial, S1-6 (NATS pattern staleness)

**ENG-5** (NEW from codex): **`consumesServices` source-derivation gate.** *(1 day)*
- Plan.json currently declares `worker.consumesServices: ["broker","db"]` with no source backing
- Add: scaffold-phase post-feature check that consumesServices set is derivable from source-grep
- Refuses close if plan declares dependencies the code doesn't reach
- Closes: S1-4 (worker→db ghost dependency)

**ENG-6** (NEW from codex): **Post-run diff gate (stitched files ↔ plan.json fragments).** *(1 day)*
- Engine post-finalize check: every stitched file must match the plan.json fragments it was rendered from
- Would have caught ENG-1's bug class directly
- Future-proofs against refinement-write-back regressions

### Phase A — Plan-as-source-of-truth structural fix (~2-3 days)

**A1**: `plan.namedConstants` schema + tier-yaml renderer binding.
- Scaffold-phase records `NATS_QUEUE_GROUP = "workers"` once as `plan.namedConstants["NATS_QUEUE_GROUP"]`
- **Codex correction**: not enough to add the schema; the tier-yaml renderer MUST consume from `plan.namedConstants` instead of from facts.jsonl strings. Without explicit binding, drift persists.
- env-content phase reads from plan.namedConstants for cross-codebase named constants
- Substrate edit teaches phases the new pattern
- Tests: tier-yaml renderer pulls from plan.namedConstants; refinement reads the same source
- Closes: S1-1 cross-codebase queue-group drift permanently AS A DEFECT CLASS

### Phase B — Source-derived plan slots (~1-2 days)

**B1**: `plan.observedFacts.envReads[codebase]` from source-grep.
- **Codex correction**: this is NOT lower priority. Dead envs at `apidev/zerops.yaml:87-88` are proven bugs, not cosmetic. Move to minimum scope.
- feature-phase greps `process.env.X` patterns across each codebase's source
- Engine pre-validates: every env declared in `apps-repo/zerops.yaml run.envVariables` must be in `observedFacts.envReads[codebase]`
- Refuses scaffold/feature close on unmet-declared envs
- Closes: S0-6 dead-env defects (SEARCH_PUBLIC_HOST, SEARCH_SEARCH_KEY, NATS_QUEUE_GROUP)

### Phase S — Substrate cleanup (hours of work, mostly substrate edits)

**S-1**: Fix the misleading `dev_server restart re-reads env from yaml` brief line.
- Both scaffold AND feature briefs have it (substrate/scaffold/* + briefs/feature/*)
- Either: change brief to say "yaml env changes require `zerops_deploy targetService=<host>dev` redeploy"
- OR: change platform behavior to make the brief true
- Closes: Q4 (zerops_env workaround) across every future run

**S-2**: Substrate self-contradiction fixes:
- Worker brief: `cite rolling-deploys topic` (line 177) vs `slug-stem forbidden in link text` (line 145-156) — clarify the rule
- Env-content brief: minContainers PASS example (lines 685-687) vs forbid-bump rule (lines 360-364) — pick a within-tier knob for the PASS example
- Feature brief: backend brief contains 80 lines of frontend SPA guidance then declares "this pass does NOT touch SPA source" — split the shared content
- Feature brief: `targetService=` vs `slot=` param naming — standardize on `targetService=` everywhere
- Feature brief: "Two only-deploys" contradiction — clarify or drop
- Feature brief: MUI hardcoded hex `#00A49A` inside "no hardcoded hex" brief — either accept exception or drop MUI
- Feature brief: `zerops_browser` vs `agent-browser` two names — pick one
- Voice substrate: imperative (env-content part-1) vs MIX (env-content part-5 TY2) — disambiguate

**S-3**: Documentation cleanup:
- env-content/part-4-naming.md needs explicit managed-service `<host>` mapping (currently undocumented; causes "unknown fragmentId" retries)
- Refinement part-2 + part-5 disambiguation on parent-recipe handling (ACT vs HOLD conflict)
- Refinement part-4-references.md path double-slash bug fix

**S-4**: Parent-recipe baseline filter.
- When parent baseline has gotchas (TypeORM, etc.), filter against child recipe's actual code shape
- Don't pass TypeORM gotchas to a recipe using raw `pg`
- Hours of substrate editing or engine logic

### Phase R — Substrate restoration (already partly addressed in S-2)

R-1 (original proposal) is DROPPED. The "parent-recipe-prose warning" wasn't deleted from substrate; only its label was changed `(V1 / RF1)` → `(V1)`. The warning still exists at refinement/synthesis_workflow.md:113-115.

R-3 (original proposal) is DROPPED. The deleted worked examples in `phase_entry/codebase-content.md` were teaching now-forbidden RF1/PD1 content. Restoring them would re-introduce forbidden teaching.

---

## What this means for run-40 commit

Phase E + Phase A + Phase B + Phase S = **estimated 2-3 weeks of engineering work**.

**E is highest priority** — refinement-non-write-back, brief-generator template edit, and TIMELINE sanitization are net-new engine fixes that close defect classes. Without E1 specifically, every future refinement is lossy at plan.json layer.

**A is second priority** — closes Q2 cross-codebase drift permanently.

**B is third priority** — closes orphan-env class, defer if Q3 isn't a critical defect now (it's pre-existing across all 5 runs).

**S is parallel/concurrent** — substrate cleanup can happen alongside engine work. Most of S is hours-of-substrate-editing, not days-of-code.

**The user said "everything or nothing" for run-40.** Under this scope:
- E1 + ENG-3 (brief-generator) + A1 close the MOST visible defects (parent-recipe citation, queue-group drift, refinement non-persistence)
- E2 (TIMELINE sanitizer) closes the most embarrassing leak (author project ID + workspace URLs)
- B1 closes the dead-env class permanently
- S addresses long-standing substrate sloppiness across phases

**If user can only do a subset, prioritize: ENG-1 (refinement write-back) + ENG-3 (brief template edit) + ENG-2 (TIMELINE sanitizer). Days, not weeks. Closes 60% of the visible defects.**

---

## Falsifiable claims for codex final validation

Codex should verify each independently against the actual artifacts:

1. **Refinement-phase non-write-back**: `plan.json` fragments at lines 176 (api/IG#3), 177 (api/IG#4), 183 (app/IG#2), 184 (app/IG#3), 192 (worker/IG#4) contain pre-refinement content. The disk-stitched README files (apidev/README.md, etc.) contain post-refinement content. The two diverge.

2. **Brief-generator template proven**: `internal/recipe/briefs_content_phase.go:608-609` (codebaseContentEmbeddedFraming) and `:614-615` (envContentEmbeddedFraming) contain the string `"Cross-reference shape when parent already covers a topic: ... See parent recipe `%[1]s` for <topic>."`. Run-39 briefs `runs/39/environments/.briefs/codebase-content-{api,app,worker}/part-{8,7,8}-context.md` contain the rendered output. Run-39 apidev/README.md:351-353 contains the agent's authoring of the templated pattern.

3. **Queue-group cross-file drift counted**: 15+ places using `'showcase-workers'` (tier yamls + plan.json + TIMELINE), 15+ places using `'workers'` (source + feature READMEs + facts.jsonl). Facts.jsonl line 76 records the explicit rename.

4. **Dead env vars confirmed**: `apidev/zerops.yaml` declares `SEARCH_PUBLIC_HOST` and `SEARCH_SEARCH_KEY`; greppable absence in `apidev/src/**/*.ts`. `workerdev/zerops.yaml:55` declares `NATS_QUEUE_GROUP`; `workerdev/src/jobs/jobs.consumer.ts:134` hardcodes literal.

5. **`process.env.hostname` lowercase bug**: `workerdev/src/jobs/jobs.consumer.ts:37` and `workerdev/src/main.ts:16`. Linux env var is `HOSTNAME` (uppercase).

6. **JWT/TypeORM claims with no consuming code**: grep `apidev/src` for "jwt" or "JWT" — zero hits. Grep for "TypeORM" or "typeorm" — zero hits (uses `pg`). Recipe-author lore from parent recipe.

7. **Project ID + workspace URLs in TIMELINE**: `TIMELINE.md` contains `7HfLxoquTxiNEg1fD4Xo7w`, `apidev-2304-3000.prg1.zerops.app`, and `/var/www/zcprecipator/nestjs-showcase/`.

8. **Tier-0 README service count**: TIMELINE.md:29 says "14 services"; actual count is 6 runtime + 5 managed = 11.

9. **Substrate scale**: per-phase brief sizes — scaffold ~3500, feature ~2100, codebase-content ~5500 (mostly duplicated), env-content ~1850, refinement ~785. Total cumulative substrate per run: ~14,000 lines.

10. **No `parent-recipe-prose` warning deletion in v9.80.0**: `git diff v9.79.0..v9.80.0 -- internal/recipe/content/briefs/refinement/synthesis_workflow.md` shows only `(V1 / RF1)` → `(V1)` label change. The warning body at lines 113-115 is intact.

If codex refutes any of these, the diagnosis needs revision before run-40 commits.

---

## What the deep-dive proved that earlier rounds missed

1. **Engine has a non-write-back bug** — refinement is lossy at plan.json layer. Nobody surfaced this in 4 prior rounds.

2. **Brief-generator template is the smoking gun for Q1.a** — confirmed by reading `briefs_content_phase.go` directly. Earlier rounds said "substrate over-pruned"; the actual problem is "substrate over-prescribed the wrong shape."

3. **Facts.jsonl is append-only and downstream phases read stale facts** — confirmed by 4-fold queue-group name in fact stream. Earlier rounds said "agent free-authored"; actually facts.jsonl had stale records the agent read in good faith.

4. **The 8-line cap on env yaml comments doesn't have a managed-service `<host>` mapping documented** — the "unknown fragmentId" errors in run-39 (8+ retries during env-content) came from undocumented fragment-ID format for managed services. Trivial substrate fix.

5. **TIMELINE leaks author's real project data** — confirmed by string-grep for project ID. Embarrassing. Needs sanitizer.

6. **Recipe lies to porter in worker README about env-driven queue-group rename** — confirmed by grep. README claims source reads env; source hardcodes. Hours-of-work code fix or A1 plan-slot fix.

7. **The deliverable contains 30+ defects across 6 severity tiers** — earlier rounds counted ~11. Deep dive surfaced 30+. The structural counters never could have caught these.

This is the synthesis. Codex final validation next.
