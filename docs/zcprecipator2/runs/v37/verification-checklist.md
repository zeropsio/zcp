# runs/v37 verification checklist

**Machine report SHA**: `10003228c87797f577cc7f2364d9bbb1996ed7d48e1fdf60bb8c6273de49919d`
**Generated at**: 2026-04-21T21:56:12Z
**Tier**: showcase
**Slug**: nestjs-showcase
**Deliverable**: `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37`
**Analyst**: Claude Opus 4.7 (1M context) on behalf of Ales Rechtorik
**Analyst session start (UTC)**: 2026-04-21T21:53Z

## Phase reached

- [ ] `close` complete (auto=false per harness; contradiction: TIMELINE.md line 259 states "Close step complete. All 6 workflow steps complete." and the engine response for tool_use_id `toolu_01UgzsfQzogH4WrhfbQxGg8x` at 2026-04-21T21:48:39Z returned `progress.steps[].status = "complete"` for all six steps including close — treat as B-25: harness close-detection bar is too narrow, not a run regression)
- [x] editorial-review dispatched (auto; 3 attempts — editorial-1 at 21:13Z, editorial-2 at 21:16Z, editorial-re-dispatch at 21:39Z)
- [x] code-review dispatched (auto; NestJS framework expert at ~21:29Z)
- [ ] close-browser-walk attempted (auto=false; TIMELINE line 249-252 says "Attempted dev + stage walks", agent-browser daemon wedged, SKIPPED AT USER REQUEST — harness lacks signal; classify as `unmeasurable-valid` environmental-soft-pass)

If `close` is not complete, downstream cells must be `unmeasurable-valid` with explicit justification. **Close IS complete** per engine response (harness false negative); downstream cells are measurable.

## Structural integrity bars (auto)

- [x] B-15 ghost_env_dirs: threshold 0, observed 0, **status pass** — **BAR BLIND SPOT**: `environments/` is clean, but `environments-generated/` sibling tree exists with 6 slug-named subdirs (`ai-agent`, `local-dev`, `prod`, `prod-ha`, `remote-dev`, `stage`), each containing a writer-authored README.md. Writer Agent `agent-a42d46a31a4f05a2c.jsonl` wrote 6 files at `/var/www/environments/{slug}/README.md` (see session-log `Write` targets, confirmed by `agent-a42d46a31a4f05a2c.jsonl` paths list). F-9 **NOT CLOSED**; defect mutated from "ghosts inside `environments/`" to "parallel `environments-generated/` tree". File new defect: **B-15b ghost_env_tree_outside_canonical** or sharpen B-15 scope.
- [x] B-16 tarball_per_codebase_md: threshold 6, observed 6, **status pass** — every codebase has README.md + CLAUDE.md (Cx-CLOSE-STEP-STAGING landed correctly at filesystem level).
- [x] B-17 marker_exact_form: threshold 0, observed 0, **status pass** — every fragment marker carries trailing `#`; verified per [apidev/README.md:7](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/apidev/README.md) `<!-- #ZEROPS_EXTRACT_START:intro# -->` and similar across 9 fragment pairs. F-12 **CLOSED**.
- [x] B-18 standalone_duplicate_files: threshold 0, observed 6, **status fail** — apidev/GOTCHAS.md, apidev/INTEGRATION-GUIDE.md, appdev/GOTCHAS.md, appdev/INTEGRATION-GUIDE.md, workerdev/GOTCHAS.md, workerdev/INTEGRATION-GUIDE.md present on disk and staged into deliverable. Writer dispatch prompt at [flow-showcase-v37-dispatches/author-recipe-readmes-claude-md-manifest.md:52-53](flow-showcase-v37-dispatches/author-recipe-readmes-claude-md-manifest.md) explicitly instructs writer to create both paths per codebase. F-13 **NOT CLOSED**.
- [x] B-22 atom_template_vars_bound: threshold 0, observed 0, **status pass** — build-time lint clean at HEAD.

## Session-metric bars (auto)

- [x] B-20 deploy_readmes_retry_rounds: threshold 2, observed 5, **status fail** — 5 distinct `complete step=deploy substep=readmes` cycles with `checkResult.passed=false` from 20:47:46Z to 20:59:42Z. TIMELINE line 177-189 confirms "5 rounds of README fixes to clear content gates". F-14/F-15/F-16 class failures persist from v36 (was 4 rounds on v36; **regressed to 5 rounds on v37**).
- [x] B-21 sessionless_export_attempts: threshold 0, observed 2, **status fail** — **BAR FALSE POSITIVE**: the 2 exports occurred at ~21:49Z and ~21:52Z AFTER close completed at 21:48:39Z, by user-explicit post-run artifact prompt ("Export the recipe session artifacts:..."). Export stderr shows `note: no session context (--session unset, $ZCP_SESSION_ID unset); skipping close-step gate` ([export.go:109](../../../internal/sync/export.go#L109)) — advisory-skip path, not hard-blocked. Cx-CLOSE-STEP-GATE-HARD correctly refuses only when LIVE session matches `OutputDir`; closed session yields no match → advisory skip fires. Session metrics bar B-21 is therefore too coarse and needs to exclude post-close legitimate exports. F-8 gate behavior is **CORRECT for closed-session case** but was not exercised for live-session case in v37 (no attempt).
- [x] B-23 writer_first_pass_failures: threshold 3, observed 0, **status skip** — bar fired `reason: "no writer Agent dispatch observed"`, **but writer WAS dispatched** (agent-a42d46a31a4f05a2c, 2026-04-21T20:36:55Z). **B-23 harness bar is broken**: it didn't recognize the "Author recipe READMEs + CLAUDE.md + manifest" dispatch as writer. Not a v37 regression; file as harness-lint issue.
- [x] B-24 dispatch_integrity: threshold 0, observed 0, **status unverified** — no harness-side diff implemented; see analyst-side diff below.
- sub_agent_count: 13 (vs v36's ~8; indicates 3 writer-fix rounds + 3 editorial-review rounds)

## Dispatch integrity (analyst-filled per Rule 5)

Byte-diff each captured Agent dispatch prompt against atom source. Divergence listed where found.

- **Author recipe READMEs + CLAUDE.md + manifest** (writer-1):
  - [x] dispatch_vs_source_diff: **Status: divergent** — 3 root-cause divergences:
    1. [author-recipe-readmes-claude-md-manifest.md:52-53](flow-showcase-v37-dispatches/author-recipe-readmes-claude-md-manifest.md) lists `/var/www/{hostname}/INTEGRATION-GUIDE.md` + `/var/www/{hostname}/GOTCHAS.md` as canonical paths. Atom source [canonical-output-tree.md:9-12](../../../internal/content/workflows/recipe/briefs/writer/canonical-output-tree.md#L9) at HEAD explicitly removed these (Cx-STANDALONE-FILES-REMOVED, commit `3fca235`).
    2. [author-recipe-readmes-claude-md-manifest.md:57-64](flow-showcase-v37-dispatches/author-recipe-readmes-claude-md-manifest.md) lists env folders as `ai-agent`, `remote-dev`, `local-dev`, `stage`, `prod`, `prod-ha` — slug names. Atom source uses `{{range .EnvFolders}}` which renders to canonical numbered names (`0 — AI Agent` etc.) via `CanonicalEnvFolders()` at [recipe_templates.go:47](../../../internal/workflow/recipe_templates.go#L47).
    3. Surface contracts naming at [author-recipe-readmes-claude-md-manifest.md:85-87](flow-showcase-v37-dispatches/author-recipe-readmes-claude-md-manifest.md) uses "integration-guide + INTEGRATION-GUIDE.md" form — same defect F-13 pattern.
  - Root cause: main agent paraphrased its own Task prompt from memory/prior run rather than stitching atom bytes returned by `dispatch-brief-atom` (which ALSO returned pre-Cx body with literal `{{.ProjectRoot}}` + `{{index .EnvFolders i}}` — see [tool-results/brepzqpg1.txt] response for atomId `briefs.writer.canonical-output-tree`). **This is a NEW defect class F-17: envelope content loss between engine → main agent → sub-agent**.
  - [x] Read-receipt: 2026-04-22T00:05Z
- **Feature sub-agent: implement 5 showcase features**:
  - [x] dispatch_vs_source_diff: **Status: unmeasurable-valid** — feature-sub-agent Go source at `internal/workflow/recipe_builder_feature.go` builds from plan; dispatch is high-volume + SymbolContract-specific; spot-checked: `flow-showcase-v37-dispatches/feature-sub-agent-implement-5-showcase-f.md` is 12591 chars and references valid service hostnames (apidev, appdev, workerdev) + valid features per plan.
  - [x] Read-receipt: 2026-04-22T00:08Z
- **Scaffold apidev NestJS API**, **Scaffold appdev Svelte SPA**, **Scaffold workerdev NestJS worker**:
  - [x] dispatch_vs_source_diff: **Status: clean-ish** (3 dispatches) — sizes 10747, 7886, 6999 chars respectively. Scaffold dispatches are plan-specific, no envfolder hardcoding observed. Deploy succeeded → structural correctness confirmed by downstream evidence.
  - [x] Read-receipt: 2026-04-22T00:10Z
- **Fix README fragment format + YAML comments** (writer-fix-1, agent-a9de…):
  - [x] dispatch_vs_source_diff: **Status: divergent** — main-agent composed fix brief with specific check-name references, no paraphrase of atom content; size 6084 chars. This is a LEGITIMATE custom Task — not stitched from atoms (no writer-fix atoms exist). But attempts to fix issues that atoms should have prevented.
  - [x] Read-receipt: 2026-04-22T00:12Z
- **Final README fix round** (writer-fix-3, agent-a7fc…):
  - [x] dispatch_vs_source_diff: **Status: divergent** — same pattern as above; custom fix brief 5741 chars; targets cross-README uniqueness issues.
  - [x] Read-receipt: 2026-04-22T00:13Z
- **Apply editorial CRIT+WRONG fixes** (agent-a7f5…):
  - [x] dispatch_vs_source_diff: **Status: divergent** — custom fix brief; 5784 chars; applies specific corrections per editorial reviewer JSON payload.
  - [x] Read-receipt: 2026-04-22T00:13Z
- **Editorial review of recipe content** (editorial-1, agent-aeaa…):
  - [x] dispatch_vs_source_diff: **Status: clean-ish** — dispatch-brief-atom sequence fetched 9 editorial-review atoms; main agent attempted `briefs.editorial-review.per-surface-checklist` which **DOES NOT EXIST** in atom corpus (engine error at 2026-04-21T21:15:14Z: `dispatch-brief atom "briefs.editorial-review.per-surface-checklist" unknown or unreadable`). Another F-17 instance: main agent hallucinated atom ID. See [flow-showcase-v37-main.md:224](flow-showcase-v37-main.md).
  - [x] Read-receipt: 2026-04-22T00:14Z
- **Editorial review: return JSON payload** (editorial-2, agent-a9e2…):
  - [x] dispatch_vs_source_diff: **Status: clean-ish** — re-dispatched after editorial-1 returned prose-not-JSON; custom 6922-char brief for JSON-payload form.
  - [x] Read-receipt: 2026-04-22T00:14Z
- **Re-dispatch editorial review** (agent-a80…):
  - [x] dispatch_vs_source_diff: **Status: clean-ish** — third editorial pass; 3995 chars. TIMELINE line 236 says this one returned clean.
  - [x] Read-receipt: 2026-04-22T00:15Z
- **Code-review: NestJS framework expert** (agent-a5e1…):
  - [x] dispatch_vs_source_diff: **Status: clean-ish** — 4526-char brief; framework-specific inspection dispatch; returned 0 CRIT / 0 WRONG / 4 STYLE with one inline Meilisearch read-after-write CRIT.
  - [x] Read-receipt: 2026-04-22T00:15Z
- **Close-step static review** (agent-a66…):
  - [x] dispatch_vs_source_diff: **Status: clean-ish** — 2637-char custom brief; lightweight pre-close audit.
  - [x] Read-receipt: 2026-04-22T00:15Z

## Writer content quality (analyst-fill, required)

### apidev/README.md

- **intro fragment** — auto markers_present=true exact_form=true line_count=1: **status pass**
  - [x] Analyst qualitative grade: **pass** — [apidev/README.md:9](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/apidev/README.md) one substantive line naming all 5 managed services.
- **integration-guide fragment** — auto markers_present=true exact_form=true h3=6 every_h3_has_fenced_code_block=true: **status pass**
  - [x] Analyst qualitative grade: **pass** — [apidev/README.md:40-327](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/apidev/README.md) 6 H3 items: (1) Adding zerops.yaml with full commented YAML, (2) Bind 0.0.0.0, (3) Trust one proxy hop, (4) Managed-service credentials as structured options, (5) Init commands own schema + seed, (6) Dev setup ships compiled dist/. Every H3 has fenced code, principle-level, not self-referential.
- **knowledge-base fragment** — auto markers_present=true exact_form=true gotcha_bullet_count=6 gotchas_h3_present=true: **status pass**
  - [x] Analyst qualitative grade: **pass** — [apidev/README.md:340-382](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/apidev/README.md) 6 gotchas: 502 bind, ts-node .js resolution, NATS creds, presigned URL, execOnce duplicate, readiness gate. Each bullet has concrete symptom + platform-topic citation (http-support, init-commands, object-storage, readiness-health-checks).
- [x] Read-receipt: 2026-04-22T00:00Z (via Read tool during analysis)

### appdev/README.md

- **intro fragment** — auto pass
  - [x] Analyst qualitative grade: **pass** — [appdev/README.md:8](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/appdev/README.md) one line; names Svelte + Vite + static base + api dependency.
- **integration-guide fragment** — auto pass (h3=4)
  - [x] Analyst qualitative grade: **pass** — [appdev/README.md:48-210](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/appdev/README.md) 4 H3: (1) Adding zerops.yaml, (2) deployFiles: dist/~, (3) allowedHosts in Vite dev server, (4) Centralise fetch. Every H3 has fenced code, principle-level.
- **knowledge-base fragment** — auto pass (gotcha_bullet_count=5)
  - [x] Analyst qualitative grade: **pass-with-caveat** — 5 gotchas: dist listing, stale VITE_API_URL, multipart boundary, CORS with subdomain rotation, SPA deep-link 404. CORS gotcha cites `env-var-model` which doesn't directly cover CORS; gotcha framing "SPA returns 404 on deep routes" at [appdev/README.md:241](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/appdev/README.md) describes a TRUE static-base limitation with no Zerops-specific fix beyond deploy-files config.
- [x] Read-receipt: 2026-04-22T00:02Z

### workerdev/README.md

- **intro fragment** — auto pass
  - [x] Analyst qualitative grade: **pass** — [workerdev/README.md:7](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/workerdev/README.md) one line.
- **integration-guide fragment** — auto pass (h3=4)
  - [x] Analyst qualitative grade: **pass** — [workerdev/README.md:42-265](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/workerdev/README.md) 4 H3: (1) Adding zerops.yaml (no HTTP port), (2) NATS creds + queue group, (3) Drain on SIGTERM then exit, (4) Match API column mapping. Every H3 has fenced code.
- **knowledge-base fragment** — auto pass (gotcha_bullet_count=6)
  - [x] Analyst qualitative grade: **pass** — 6 gotchas including required showcase supplements: queue-group semantics (line 279), SIGTERM drain (line 294), NATS auth silent fail (line 322), plus "Worker container stays ACTIVE even when Nest crashed" (line 330) — excellent concrete-symptom framing citing `rolling-deploys`, `readiness-health-checks`.
- [x] Read-receipt: 2026-04-22T00:03Z

### apidev/CLAUDE.md

- [x] size ≥ 1200 bytes: true (observed 4953 bytes)
- [x] base sections present: [Dev Loop Migrations Container Traps Testing]
- [x] custom sections ≥ 2: true (observed 2)
- auto **status pass**
- [x] Analyst narrative sign-off: Operator-focused content. Dev Loop describes SSH + watch mode; Migrations describe dist/migrate.js; Container Traps names specific Node crashes; Testing covers API surface checks. No deploy instructions. Useful for the specific repo.
- [x] Read-receipt: 2026-04-22T00:04Z

### appdev/CLAUDE.md

- [x] size ≥ 1200 bytes: true (observed 3094 bytes)
- [x] base sections present: [Dev Loop Migrations Container Traps Testing]
- [x] custom sections ≥ 2: true (observed 2)
- auto **status pass**
- [x] Analyst narrative sign-off: Good operator context for Svelte 5 + Vite 8; notes allowedHosts quirk + VITE_API_URL bake-in behavior. Shorter than apidev (3.1KB vs 4.9KB) because Svelte operationally simpler.
- [x] Read-receipt: 2026-04-22T00:04Z

### workerdev/CLAUDE.md

- [x] size ≥ 1200 bytes: true (observed 4318 bytes)
- [x] base sections present: [Dev Loop Migrations Container Traps Testing]
- [x] custom sections ≥ 2: true (observed 2)
- auto **status pass**
- [x] Analyst narrative sign-off: Includes the wire contract (NATS subject, queue group, payload shape) that apidev/workerdev both must honor. Migrations section correctly notes "schema owned by apidev". High quality.
- [x] Read-receipt: 2026-04-22T00:05Z

## Env README quality (spot check)

- [x] `0 — AI Agent/README.md` (42 lines): pass — Who-this-is-for, First-tier context, Promotion, Operational sections all present; [env 0/README.md:41](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/environments/0%20%E2%80%94%20AI%20Agent/README.md) correctly states "initCommands do NOT fire automatically at this tier" which is the key AI-Agent tier distinction.
- [x] `5 — Highly-available Production/README.md` (46 lines): pass — [env 5/README.md:17-26](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/environments/5%20%E2%80%94%20Highly-available%20Production/README.md) "What changes vs adjacent tier" correctly distinguishes `mode: HA` + queue-group from tier-4. Terminal-tier framing correct.
- [x] `environments-generated/ai-agent/README.md` (51 lines): **ORPHAN** — writer-authored slug-path duplicate of finalize-emitted canonical; not in canonical tree so not published. Evidence of F-9 mutation (see B-15 above). [environments-generated/ai-agent/README.md:1-51](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/environments-generated/ai-agent/README.md).
- [x] Read-receipt: 2026-04-22T00:06Z

## Env import.yaml comment quality (spot check)

- [x] `4 — Small Production/import.yaml` pass — [env 4/import.yaml:1-80](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/environments/4%20%E2%80%94%20Small%20Production/import.yaml) `#zeropsPreprocessor=on` first line, APP_SECRET project-comment explains balancer-shared-session constraint, each service block 4-10 comment lines explaining decisions (why NON_HA at tier 4, SIGTERM context on worker, autoscale headroom on runtime). Factual claims match YAML below (`minContainers: 2` observable).
- [x] Read-receipt: 2026-04-22T00:07Z

## Manifest integrity

- [x] ZCP_CONTENT_MANIFEST.json: **UNREACHABLE IN DELIVERABLE** — writer authored at `/var/www/ZCP_CONTENT_MANIFEST.json` (confirmed Write in agent-a42d46a31a4f05a2c paths list), but file does not appear under `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/` (`find` returns 0 matches). F-10-class regression at root-level: per-codebase staging works (B-16 pass) but root-level ZCP_CONTENT_MANIFEST.json staging does NOT work. New defect class F-23: "root-level writer artifacts not staged into deliverable by close-step" (partial closure of F-10).
- [x] Read-receipt: 2026-04-22T00:15Z

## Retry-cycle attribution (analyst-fill)

| Cycle | Timestamp | Substep | Failing checks | Attribution |
|---|---|---|---|---|
| 1 | 2026-04-21T20:47:46.487Z | deploy/readmes | fragment_intro/IG/KB markers × 3 codebases (9) + worker_drain_code_block + writer_manifest_honesty_any_as_intro | F-14-writer-compliance writer-1 first pass missed fragment_intro trailing-# form + one-line rule |
| 2 | 2026-04-21T20:49:36.176Z | deploy/readmes | comment_ratio × 3, heading_level × 6, blank_after_marker × 9, knowledge_base_gotchas × 3, intro_length × 3, intro_no_titles × 3, app_comment_specificity × 1 | F-14/F-15/F-16 cascade — writer-fix-1 corrected marker form but introduced H2-inside-fragment + blank-line violations |
| 3 | 2026-04-21T20:55:47.424Z | deploy/readmes | app/api/worker_knowledge_base_authenticity × 3, worker_worker_queue_group_gotcha, writer_manifest_honesty_any_as_intro | writer-fix-1 re-emitted synthetic/non-Zerops gotcha stems; authenticity check caught them |
| 4 | 2026-04-21T20:59:05.039Z | deploy/readmes | app_gotcha_distinct_from_guide, cross_readme_gotcha_uniqueness | writer-fix cross-code uniqueness — appdev gotcha restated IG item (same claim in two surfaces) |
| 5 | 2026-04-21T20:59:42.755Z | deploy/readmes | worker_gotcha_distinct_from_guide | single remaining worker uniqueness issue; one more fix round |
| 6 | 2026-04-21T21:01:39.326Z | finalize/ | envComments contradicted platform YAML across 6 tiers + no_version_anchors_in_published_content | **F-21-finalize-comment-factuality**: writer envComments invented specific GB/minContainers numbers mismatching auto-generated YAML; `bootstrap-seed-v1` key flagged as version anchor |
| 7 | 2026-04-21T21:05:01.355Z | finalize/ | 3 — Stage/4 — Small Production/5 — HA Production comment_ratio | F-21 residual — top 3 envs still below 30% comment ratio after first fix |

## Final verification

- [x] All cells are non-`pending`
- [x] Every Read-receipt timestamp is after analyst session start (2026-04-21T21:53Z)
- [ ] No `unmeasurable-invalid` cells — ZCP_CONTENT_MANIFEST.json cell marked `unmeasurable-valid` because file is genuinely not in deliverable (separate defect F-23); close-browser-walk marked `unmeasurable-valid` environmental. B-23 marked as harness-broken, not invalid.
- [x] Machine-report SHA matches file content (`10003228c87797f577cc7f2364d9bbb1996ed7d48e1fdf60bb8c6273de49919d`)
- [ ] Checklist SHA will match on-file content after write

**Analyst sign-off**: Claude Opus 4.7 (1M context), 2026-04-22T00:18Z — v37 evidence points to multiple uncontested defects (F-9 mutated, F-13 reopened, F-17 new, F-21 new, F-23 new) mixed with confirmed closures (F-12, F-10-partial-codebase-level) plus two harness bar-sharpness issues (B-21 false-positive, B-23 broken writer-detection, close-step harness false-negative).
