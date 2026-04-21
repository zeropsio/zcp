# calibration-bars.md — v35 measurement surface (snapshot at post-run analysis)

**Snapshot status** (updated 2026-04-21 post-v35 analysis):

- **Frozen for v35**: this file is the v35-commission bar set as-drafted + the 6 new bars added at post-run analysis (§9 B-9..B-14) + the tightened measurement definition for C-1 / C-2. It is the source-of-truth for v35 measurements. v36 will start from a copy of this file under `runs/v36/calibration-bars.md` and evolve.
- **v35 run status**: stalled at deploy-check, never reached close. **Sections §3–§8, §9 B-9..B-14 evaluators, §11a Editorial-review are entirely UNMEASURABLE on v35** (they depend on close-step / finalize output that the run never produced). Only §1 substrate, §2 convergence, §9 B-1..B-8 dispatch-integrity, §10 tier bars, and a partial subset of §11 hybrid bars are measurable.
- **Verdict driven by this sheet**: [`verdict.md`](verdict.md) — PAUSE + engine-level defects identified. Six of those defects added to the defect-class-registry as rows 16.1–16.6 and surface here as new bars B-9..B-14 with evaluators pending fix-stack commits ([`../../HANDOFF-to-I6.md`](../../HANDOFF-to-I6.md)).

**Purpose** (unchanged from draft): the numeric / grep-verifiable thresholds v35 (first run under zcprecipator2) must hit. Every bar is measurable against the v35 session logs + exported deliverable tree + on-mount draft state — no qualitative "looks good" / "reasonable" / "acceptable" thresholds. Aggregated from [`../../05-regression/defect-class-registry.md`](../../05-regression/defect-class-registry.md) `calibration_bar` column + [README.md §10](../../README.md) success criteria + [principles.md §12](../../03-architecture/principles.md) open questions.

**Tier coverage**: bars are labeled `[S]` (showcase), `[M]` (minimal), or `[S+M]` (both). A bar without tier prefix applies to both. Minimal-specific bars account for single-inline-scaffold / main-writes-features-inline / smaller deliverable surface.

**Measurement source keys**:
- `log` = main-session.jsonl + sub-agent session logs under `SESSIONS_LOGS/`
- `mount` = `/var/www/{host}/` SSHFS mount state during run
- `export` = `/var/www/zcprecipator/{recipe}/` published deliverable after close
- `manifest` = `/var/www/ZCP_CONTENT_MANIFEST.json` writer output
- `tool` = `zerops_workflow action=status` / `zerops_record_fact` response payloads
- `shim` = `zcp check <name>` CLI shim output (per [check-rewrite.md §18](../03-architecture/check-rewrite.md))

**Bar-severity keys**:
- `[gate]` = regression triggers v35 rollback evaluation per step-6 rollback-criteria
- `[signal]` = measured but not gate; regression is a v36 plan input, not a rollback trigger
- `[advisory]` = editorial only; no action triggered

Total bars: **108** (102 original + 6 runtime-integrity bars B-9..B-14 added at v35 post-run analysis).

---

## 1. Operational substrate — bars inherited from pristine v34 performance

The operational substrate is declared pristine at [README.md §1](../README.md); v34 validated. These bars carry forward as "do not regress" floors.

| # | Bar | Threshold | Source | Severity |
|---|---|---|---|---|
| O-1 | Wall clock (recipe work, START → canonical close) `[S]` | ≤ 90 min (v34: 73; v31: 86) | log timestamps | `[gate]` |
| O-1M | Wall clock `[M]` | ≤ 60 min (minimal runs typically 25–35 min) | log timestamps | `[gate]` |
| O-2 | Main bash total time | ≤ 10 min (v34: 1.2; v31: 1.3; v33: 0.5) | `analyze_bash_latency.py` | `[gate]` |
| O-3 | Very-long main bash calls (≥120s) | `== 0` (v34: 0) | analyzer output | `[gate]` |
| O-4 | Errored main bash calls | `≤ 5` (v34: 0; v31: 2) | analyzer output | `[signal]` |
| O-5 | MCP schema-validation errors across main + all sub-agents | `== 0` (v34: 0; v31: 5) | `is_error:true` events w/ schema validator | `[gate]` |
| O-6 | `SUBAGENT_MISUSE` rejections | `== 0` (v34: 0; v25: 2) | tool error payloads | `[gate]` |
| O-7 | `.git/index.lock` / `.git/config.lock` contention events | `== 0` (v34: 0; v31: ~90s) | bash error scan | `[gate]` |
| O-8 | "File has not been read yet" errors across all sub-agents | `== 0` (v34: 0; v31: 7) | tool error payloads | `[gate]` |
| O-9 | Zcp-side exec patterns `cd /var/www/{host} && <exec>` | `== 0` across main + all sub-agents (v34: 0) | bash pattern scan | `[gate]` |
| O-10 | Phantom output directories `find /var/www -maxdepth 2 -type d -name 'recipe-*'` | `== 0` (v34: 0; v33: 1) | filesystem check at close | `[gate]` |
| O-11 | Autonomous `zcp sync recipe export` / `publish` after close | `== 0` (v34: 0; v33: 3) | post-close bash scan | `[gate]` |
| O-12 | Unicode Box-Drawing chars in `*/zerops.yaml` | `grep -rP '[\x{2500}-\x{257F}]' */zerops.yaml | wc -l == 0` (v34: 0; v33: ≥3) | filesystem grep | `[gate]` |
| O-13 | Seed uses static `execOnce` key | `grep -E 'execOnce \$\{appVersionId\}.*seed' */zerops.yaml | wc -l == 0` AND `grep -c 'execOnce bootstrap-seed-v1' */zerops.yaml ≥ 1` (v34: held) | filesystem grep | `[gate]` |
| O-14 | Post-scaffold `fatal: not a git repository` runtime | `== 0` (v34: 0; v33: 1) | bash error scan | `[gate]` |
| O-15 | `dev_server` stop exit-255 classified as failure | `== 0` (v34: 0; v21: 6) | tool result classification | `[gate]` |
| O-16 | `dev_server start` spawn hangs (≥ 300s) | `== 0` (v34: 0; v17: 1 × 300s) | analyzer output | `[gate]` |
| O-17 | "address already in use" errors within 10s of `dev_server stop` | `== 0` (v34: 0; v31: port-kill dance) | bash error scan | `[gate]` |
| O-18 | `zerops_knowledge` schema-churn rejections | `== 0` (v34: 0; v31: 5) | tool error payloads | `[gate]` |
| O-19 | `recipePlan` stringification retries at `complete step=research` | `== 0` (v34: 0; v26: 2) | tool result retries | `[gate]` |
| O-20 | Assistant events (main) `[S]` | `≤ 400` (v34: 267; v31: 280; v33: 321); Opus-4.7 cost-band | log event count | `[signal]` |
| O-21 | Tool calls (main) `[S]` | `≤ 250` (v34: 169; v31: 720 is subagent-counted; v33: 693 is mis-reported) | log tool_use count | `[signal]` |

---

## 2. Convergence — the most-important v35 measurements

Per [principles.md P1](../03-architecture/principles.md) + [README.md §10](../README.md): **convergence bars are the single most important v35 measurement.** Metadata-on-failure (v8.96 Theme A + v8.104 Fix E) demonstrably does NOT collapse rounds (v34 deploy 3→4, finalize 2→3). P1 author-runnable pre-attest is the proposed mechanism. v35 gates these bars hard.

| # | Bar | Threshold | Source | Severity |
|---|---|---|---|---|
| C-1 | Deploy-complete content-check rounds | **`≤ 2`** (v34: 4; v33: 3; v31: 3; target: 1; v35: 11 unmeasured-close) | count of `complete step=deploy` responses where `json.loads(response).checkResult.passed == false` (tightened at v35 post-run analysis — the prior `is_error:true` literal reading only fires on hard MCP errors, not `checkResult.passed==false` which is how check-round-count actually surfaces) | `[gate]` — bar #0 |
| C-2 | Finalize-complete content-check rounds | **`≤ 1`** (v34: 3; v33: 2; v31: 3; target: 1; v35: N/A finalize never ran) | count of `complete step=finalize` responses where `json.loads(response).checkResult.passed == false` (same tightening as C-1) | `[gate]` — bar #0 |
| C-3 | First `complete step=deploy` passes | ≥ 50% of runs (v34: 0%; target: ≥ 80%) | single-run binary; full-gate measured over run series | `[signal]` |
| C-4 | First `complete step=finalize` passes | ≥ 50% of runs (v34: 0%; target: ≥ 80%) | single-run binary | `[signal]` |
| C-5 | Post-writer in-main `Edit` count on any `{host}/README.md` after writer sub-agent returns | ≤ 3 per file (v34: low; v22: 11 on workerdev) | `Edit` tool use scan post-writer dispatch | `[gate]` |
| C-6 | Out-of-order substep-complete attestations | `== 0` (v34: 0; v25: 13 backfilled) | workflow action=complete validation errors | `[gate]` |
| C-7 | First-substep-complete latency from step-entry | ≤ 5 min (v34: real-time; v25: 38 min late) | log timestamps | `[gate]` |
| C-8 | 2-min-window backfill burst with ≥5 substep-completes | `== 0` (v34: 0; v25: 1 burst) | log timestamp clustering | `[gate]` |
| C-9 | `Scope="downstream"` facts recorded (sub-agents that run after scaffold) | **`≥ 2`** (v33: 3; v34: 1 main + 18 sub, unclear split) | `zerops_record_fact` + facts-log scan | `[gate]` — Theme-B adoption calibration |
| C-10 | Duplicate-framework-archaeology wall time (same API re-investigated by multiple sub-agents) | `≤ 5s` (v31: ~80s; v33: not deep-audited) | sub-agent bash scan vs facts-log cross-reference | `[signal]` |
| C-11 | Main-agent TodoWrite full-rewrite count at step-entry boundaries | `≤ 1` total (v34: 12; v25: end-of-step backfill) — P4 check-off-only | TodoWrite tool use scan + content delta | `[gate]` |

---

## 3. Content quality — writer surface

Per [principles.md P5 + P7](../03-architecture/principles.md) + [README.md §10](../README.md). Every bar here is measurable at post-write, pre-attest, or post-close; no "we'll check the vibes."

| # | Bar | Threshold | Source | Severity |
|---|---|---|---|---|
| CQ-1 | Gotcha-origin ratio genuine-platform-teaching `[S]` | **`≥ 80%`** (v34: 85%; v33: 100%; v31: 100%; v30: 88%; v28: 33%) | manual classification against [spec-content-surfaces.md](../../spec-content-surfaces.md) surface 5 test | `[gate]` |
| CQ-1M | Gotcha-origin ratio `[M]` | `≥ 80%` (no prior baseline — commissioned minimal run per RESUME #1) | manual classification | `[gate]` |
| CQ-2 | Self-inflicted gotchas shipped | `== 0` (v34: 1 DB_PASS; v28: 5) | writer-classification manifest cross-check | `[gate]` |
| CQ-3 | Folk-doctrine / fabricated-mental-model gotchas | `== 0` (v33: 0; v31: 0; v28: 1; v23: 1 execOnce-burn) | cold-read audit + `zerops_knowledge` citation check | `[gate]` |
| CQ-4 | Version anchors (`v\d+(\.\d+)*`, `v8\.\d+`) in published content `{host}/README.md`, `{host}/CLAUDE.md`, `environments/*/README.md` | `grep -rE 'v[0-9]+(\.[0-9]+)*' export/ | wc -l == 0` (new — zcprecipator2 specific) | filesystem grep | `[gate]` |
| CQ-5 | Wrong-surface items (framework-docs, library-meta, scaffold-helper) shipped as Zerops gotchas | `== 0` (v33: 0; v31: 0; v28: 5) | manual classification | `[gate]` |
| CQ-6 | Writer DISCARD override rate (facts classified `routed_to=discarded` that ship as published gotchas) | `== 0` (v34: 0; v31: 0; v30: 0; v29: 2/14) | manifest fact-set × README grep | `[gate]` |
| CQ-7 | Published gotchas whose mechanism matches a `zerops_knowledge` guide AND carry explicit citation | `≥ 80%` of applicable gotchas (v31: 5/6 on apidev) | manual citation audit | `[signal]` |
| CQ-8 | `{host}/CLAUDE.md` size `[S]` | `≥ 1200 bytes` AND `≥ 2 custom sections` beyond 4-section template per codebase (v34: 6246/4942/5602) | `wc -c` + awk section count | `[gate]` |
| CQ-8M | `{host}/CLAUDE.md` `[M]` | `≥ 1200 bytes` AND `≥ 2 custom sections` | same | `[gate]` |
| CQ-9 | README fragment markers present — intro, integration-guide, knowledge-base | 3 pairs per `{host}/README.md`, each fragment non-empty | grep + awk | `[gate]` |
| CQ-10 | IG item standalone: each `### N.` block has ≥1 fenced code block AND ≥1 platform-anchor token in first prose paragraph | All IG items in every README pass (v34: 17 items held) | `zcp check ig-per-item-code` shim | `[gate]` |
| CQ-11 | Cross-README gotcha uniqueness — no two codebases carry Jaccard-similar stems | `zcp check cross-readme-dedup` exits 0 (v34: held after 4 rounds) | shim | `[gate]` |
| CQ-12 | Self-referential gotcha count (where removing recipe-specific names leaves the gotcha meaningless) | `≤ 1` (v34: 1 /api/status; v28: 2 api.ts + Multer) | cold-read audit | `[signal]` |

---

## 4. Content quality — env-README Go-template surface (v8.95 Fix B sustain)

Per [defect-class-registry.md rows 9.2, 9.3](defect-class-registry.md). v8.95 Fix B corrected the Go templates at source; v31/v33 held byte-identically. v35 must sustain.

| # | Bar | Threshold | Source | Severity |
|---|---|---|---|---|
| ER-1 | Cross-tier data-persistence fabrications in env 0 README | `grep -rE '(data persists across tiers?|hostnames stay stable)' environments/*/README.md | wc -l == 0` (v31/v33: 0; v29: 2) | filesystem grep | `[gate]` |
| ER-2 | `minContainers: N` / `mode: (HA\|NON_HA)` / `objectStorageSize: N` claims in env 0–5 READMEs match adjacent import.yaml ground truth | Every claim line matches (v31/v33: 0 drifts; v29: 11 drifts) | `zcp check env-readme-vs-yaml` shim | `[gate]` |
| ER-3 | Env README line count `[S]` | `≥ 40` lines per env (v29: 40–46; v33: template-driven) | `wc -l` | `[gate]` |
| ER-3M | Env README line count `[M]` | `≥ 30` lines per env (minimal has fewer tiers typically) | `wc -l` | `[signal]` |
| ER-4 | Env README contains tier-transition teaching tokens (`promotion`, `tier bump`, `next tier`, `from`/`to`) | ≥ 1 token per env 1–5 (env 0 is dev-only, exempt) | grep | `[signal]` |

---

## 5. Content quality — env import.yaml comment surface

Per [registry rows 1.11, 2.6, 6.3, 8.4](defect-class-registry.md).

| # | Bar | Threshold | Source | Severity |
|---|---|---|---|---|
| EI-1 | env 4 + env 5 runtime-service comments (app/api/worker) contain both-axis `minContainers` teaching (throughput + HA/rolling) | Every runtime-service block matches `(throughput|HA|rolling)` ≥ 1 each axis (v31/v33/v34: held) | grep | `[gate]` |
| EI-2 | `{env}_service_comment_uniqueness` — no two service comments within one env file Jaccard ≥ 0.6 | All pass (v22/v34: held) | shim | `[gate]` |
| EI-3 | `{prefix}_comment_ratio` — per-env comment density ≥ 30% | All 6 envs pass first finalize round (v34: 1 finalize retry; target: 0 retries) | awk ratio | `[gate]` |
| EI-4 | `{prefix}_comment_depth` — per-env reasoning-marker density ≥ 35% | All 6 envs pass first finalize round | awk ratio | `[gate]` |
| EI-5 | `{prefix}_factual_claims` — numeric claims in comments match adjacent YAML declarations | All envs pass (v34: semantic-contradiction class still editorial) | shim | `[gate]` |
| EI-6 | `{prefix}_cross_env_refs` — comments don't explicitly reference sibling tiers | All envs pass (v30: 6 fails; v34: held after 1 round) | grep | `[gate]` |
| EI-7 | Semantic-contradiction check — comment claims "X is not / stays default" adjacent to YAML declaring X | `== 0` (v25 + v29: 1 each; v30+: held) | cold-read audit OR extended shim | `[signal]` |
| EI-8 | `#zeropsPreprocessor=on` directive on all 6 envs (showcase) / 4 envs (minimal default) | Every env import.yaml contains directive as first comment line | grep | `[gate]` |

---

## 6. Manifest-consistency — P5 two-way graph surface

Per [principles.md P5](../03-architecture/principles.md) + [registry rows 9.4, 14.1](defect-class-registry.md). This is the v34-surfaced class; the single-direction honesty check misses 5 of 6 routing dimensions.

| # | Bar | Threshold | Source | Severity |
|---|---|---|---|---|
| M-1 | `ZCP_CONTENT_MANIFEST.json` exists at `/var/www/` (outside published tree) | File present + valid JSON (v31/v34: held) | `jq empty ZCP_CONTENT_MANIFEST.json` | `[gate]` |
| M-2 | Every fact in manifest has non-empty `routed_to` ∈ {`discarded`, `content_gotcha`, `content_intro`, `content_ig`, `content_env_comment`, `claude_md`, `zerops_yaml_comment`, `scaffold_preamble`, `feature_preamble`} | 100% (v34: held for DB_PASS fact's `routed_to=claude_md` classification) | `jq '.facts[] | select(.routed_to == null or .routed_to == "")' | wc -l == 0` | `[gate]` |
| M-3 | `writer_manifest_honesty` check — expanded per P5 — passes all `(routed_to × surface)` pairs, not only `(discarded, published_gotcha)` | `zcp check manifest-honesty --mount-root=./` exits 0 (v34: would have caught DB_PASS) | shim | `[gate]` |
| M-3a | `(routed_to=discarded, published_gotcha)` mismatches | `== 0` (v29: 2; v30+: 0) | manifest × README grep | `[gate]` |
| M-3b | `(routed_to=claude_md, published_gotcha)` mismatches — the v34 class | `== 0` (v34: 1; NEW bar) | manifest × README grep | `[gate]` |
| M-3c | `(routed_to=integration_guide, published_gotcha)` mismatches | `== 0` (NEW bar) | manifest × README grep | `[gate]` |
| M-3d | `(routed_to=zerops_yaml_comment, published_gotcha)` mismatches | `== 0` (NEW bar) | manifest × README grep | `[gate]` |
| M-3e | `(routed_to=env_comment, published_gotcha)` mismatches | `== 0` (NEW bar) | manifest × README grep | `[gate]` |
| M-3f | `(routed_to=any, published_intro)` — intro-surface carries no routed-to-gotcha facts | `== 0` (NEW bar) | manifest × README grep | `[signal]` |
| M-4 | `writer_discard_classification_consistency` — every fact classified as framework-quirk/library-meta/self-inflicted either `routed_to=discarded` OR has non-empty `override_reason` | 100% (v34: held — DB_PASS had override_reason but was still incorrectly published) | `jq` query | `[gate]` |
| M-5 | `writer_content_manifest_completeness` — every distinct `FactRecord.Title` in facts log has exactly one manifest entry | 100% (v34: held) | shim | `[gate]` |
| M-6 | Every published gotcha's title-tokens intersect a manifest fact's title-tokens (every published item has a fact source) | ≥ 95% (NEW P5 forward direction) | manifest × README grep | `[gate]` |

---

## 7. Cross-scaffold symbol-naming contract — P3 surface

Per [principles.md P3](../03-architecture/principles.md) + [registry rows 4.1, 10.1, 10.2, 14.2](defect-class-registry.md). The v22→v34 recurrence class (parallel scaffolders without shared contract).

| # | Bar | Threshold | Source | Severity |
|---|---|---|---|---|
| CS-1 | Env-var name tokens DB_*, NATS_*, CACHE_*, QUEUE_*, STORAGE_*, SEARCH_* match across all codebases' source + .env.example + zerops.yaml | `zcp check symbol-contract-env-consistency --mount-root=./` exits 0 (v34: would have caught DB_PASS / DB_PASSWORD) | shim | `[gate]` |
| CS-2 | Runtime env-var mismatch errors at deploy-start (`SASL: client password must be a string`, `AUTHORIZATION_VIOLATION`, `ECONNREFUSED 127.0.0.1:5432` from missing mapping) | `== 0` (v34: 1 DB_PASS; v22: 1 NATS_PASS; v31: 1 NATS_PASS) | deploy logs | `[gate]` |
| CS-3 | Close-step code-review WRONG findings of form "codebase X reads VAR_A while codebase Y reads VAR_B" | `== 0` (v34: 1) | code-review output | `[gate]` |
| CS-4 | NATS connection code uses separate user/pass `ConnectionOptions`, never URL-embedded | `grep -rE 'nats://[^@]+@' {host}/src/ | wc -l == 0` (v31/v34: held; v21/v22: failed) | filesystem grep | `[gate]` |
| CS-5 | S3 endpoint uses `storage_apiUrl` (HTTPS), never bare `storage_apiHost` | `grep -rE 'storage_apiHost(?!.*Url)' {host}/src/ | wc -l == 0` (v31/v34: held; v22: failed) | filesystem grep | `[gate]` |
| CS-6 | `SymbolContract` JSON byte-identical across all scaffold sub-agent dispatches `[S]` | All 3 scaffold Agent-tool payloads carry identical JSON fragment for contract (NEW P3 bar) | captured dispatch payload diff | `[gate]` |
| CS-7 | `.gitignore` baseline entries across all codebases — `node_modules`, `dist`, `.env`, `.DS_Store` | Every `{host}/.gitignore` contains all 4 tokens (v34: held; v30: .DS_Store missing on 2 codebases) | grep | `[gate]` |
| CS-8 | Worker codebase `src/main.ts` contains SIGTERM handler + `app.close` tokens (when worker is a NestJS codebase) | `[S]` worker: mandatory (v34: held; v30: 1 CRIT) | awk | `[gate]` |
| CS-9 | Any codebase with `OnModuleDestroy` provider also calls `enableShutdownHooks()` in bootstrap | 100% (v34: held; v31: 1 CRIT apidev) | awk | `[gate]` |

---

## 8. Close-review surface

Per [README.md §10](../README.md) + v34 close-review data (0 CRIT / 3 WRONG / 5 STYLE).

| # | Bar | Threshold | Source | Severity |
|---|---|---|---|---|
| CR-1 | Close-step CRITs shipped AFTER close-fix (uncaught, reach export) | `== 0` (v34: 0; v31: 0 shipped after fix) | code-review output + post-close audit | `[gate]` |
| CR-2 | Close-step CRITs found by code-review AND fixed inline `[S]` | `≤ 3` per run (v34: 0 caught; v31: 1; v22: 0) | code-review output | `[signal]` |
| CR-3 | Close-step WRONG findings `[S]` | `≤ 3` per run (v34: 3; v31: 2; v22: 0; v25: 1) | code-review output | `[signal]` |
| CR-4 | Close-step STYLE findings | no gate; accepted | code-review output | `[advisory]` |
| CR-5 | Silent-swallow scan clean (no `.catch(() => {})`/empty error handlers without rationale) | 100% (v34: held) | code-review scan | `[gate]` |
| CR-6 | Feature coverage scan — every `plan.Features[]` entry has source-file:line citation in code-review output | 100% (v34: held) | code-review output | `[gate]` |
| CR-7 | Stage browser walk passes all features with empty errors + empty console `[S]` | All features green (v34: 6/6 held) | `zerops_browser` result | `[gate]` |
| CR-8 | Scaffold e2e leftover tests (e.g. `GET / expecting "Hello World!"`) | `== 0` (v34: 0 after fix; v33: 2 WRONG) | code-review scan + grep | `[gate]` |
| CR-9 | Close-step CRITs classified as missing-mandatory-handler (SIGTERM / enableShutdownHooks) | `== 0` (v34: 0; v31: 1; v30: 1) | classification of CR-2 findings | `[gate]` |

---

## 9. Dispatch + brief-composition integrity (P2, P6, P8 surface)

Per [principles.md P2, P6, P8](../03-architecture/principles.md) + new zcprecipator2 architecture. These bars are checkable against captured sub-agent dispatch payloads (from step-1 flow reconstruction + step-4 composed briefs) and against atomic-tree build-time lints.

| # | Bar | Threshold | Source | Severity |
|---|---|---|---|---|
| B-1 | Version anchors (`v\d+(\.\d+)*`, `v8\.\d+`) in atomic brief tree `internal/content/workflows/recipe/` | `grep -rE 'v[0-9]+(\.[0-9]+)*|v8\.[0-9]+' internal/content/workflows/recipe/ | wc -l == 0` | build-time grep | `[gate]` |
| B-2 | Dispatcher-vocabulary tokens in atomic brief tree (`compress`, `verbatim`, `include as-is`, `main agent`, `dispatcher`) | `== 0` (P2 build guard) | build-time grep | `[gate]` |
| B-3 | Internal check names in atomic brief tree (`writer_manifest_*`, `_env_self_shadow`, `_content_reality`, `_causal_anchor`) | `== 0` (P2 build guard) | build-time grep | `[gate]` |
| B-4 | Go-source paths in atomic brief tree (`internal/*.go`) | `== 0` (P2 build guard) | build-time grep | `[gate]` |
| B-5 | Atom file size | Every `*.md` under atomic tree ≤ 300 lines (P6) | `wc -l` | `[gate]` |
| B-6 | Captured Agent-tool dispatch payloads carry byte-identical `_shared-mandatory.md` content across all dispatches of same role `[S]` | All 3 scaffold dispatches carry identical MANDATORY region (P2 + v32 fix) | dispatch payload diff | `[gate]` |
| B-7 | Orphan prohibitions (atoms containing "do not", "avoid", "never", "MUST NOT" without a positive-form statement within the same atom) | `== 0` (P8 build lint) | build-time lint | `[gate]` |
| B-8 | Sub-agent dispatch prompt size | `≤ 20 KB` per dispatch (v34: in-range; unbounded in old architecture; v35: 6.3-13.5 KB across all 7 dispatches — passes) | dispatch payload `wc -c` | `[signal]` |
| B-9 | `zerops_workflow` tool-response size (inbound to main agent) — catches dispatch-brief overflow into spillover files | `≤ 32 KB` per response (v35: **71,720 chars overflow** on `complete substep=feature-sweep-stage` → spilled to scratch file → main agent lost wire contract) | response `wc -c` | `[gate]` pending Cx-BRIEF-OVERFLOW evaluator; currently `[signal]` |
| B-10 | Check `Detail` strings use JSON-key notation, not Go struct-field notation | zero `grep -rE '\b(FactRecord\|ContentManifestFact\|StepCheck\|ManifestFact)\.[A-Z]\w+\b' internal/ops/checks/ internal/tools/workflow_checks_*.go` hits (v35: `FactRecord.Title` misled main agent into envelope-guess loop through 9 rounds) | build-time grep / unit test | `[gate]` pending Cx-CHECK-WIRE-NOTATION |
| B-11 | Substep `complete` calls following `action=iterate` within same iteration without intervening tool work | `== 0` (v35: 12 fake-pass substep-completes in 84s after iterate) | log timestamp + tool-use sequence | `[gate]` pending Cx-ITERATE-GUARD |
| B-12 | `zerops_guidance` unknown-topic responses + zero-byte valid-topic responses | `== 0` each (v35: 2 unknown-topic responses + 1 silent-empty response) | log scan for `Error: unknown guidance topic` + `result_size==0` on `zerops_guidance` | `[gate]` pending Cx-GUIDANCE-TOPIC-REGISTRY |
| B-13 | Wire-contract atoms recoverable via canonical keyword queries in `zerops_knowledge` top-3 | 5/5 canonical atom queries return the target atom in top 3 hits (v35: `manifest-contract.md` NOT recoverable via "ZCP_CONTENT_MANIFEST.json schema" / "writer_manifest_completeness" queries) | test rig in `internal/knowledge/` | `[gate]` pending Cx-KNOWLEDGE-INDEX-MANIFEST |
| B-14 | `action=skip` attempts on mandatory steps per session — retry-exhaustion telemetry | `== 0` (v35: 1 attempt on deploy; signals upstream defect B-9/B-10) | log scan for `action=skip step=<deploy\|generate\|research\|provision>` | `[signal]` — PAUSE verdict if > 0 |

**Bars B-9..B-14 provenance**: added at v35 post-run analysis; each closes a defect class surfaced by v35 (see [`../../05-regression/defect-class-registry.md §16.1–16.6`](../../05-regression/defect-class-registry.md)). All six are `[gate]` once their Cx-commits land and evaluators exist; until then they are `[signal]` — the lack of enforcement is tracked in the fix-stack handoff ([`../../HANDOFF-to-I6.md`](../../HANDOFF-to-I6.md)).

---

## 10. Tier-specific bars — showcase vs minimal asymmetry

Per [README.md §2 tier coverage](../README.md) — both tiers ship as first-class. v35 must hit tier-specific bars.

### 10.1. Showcase-only `[S]`

| # | Bar | Threshold | Source | Severity |
|---|---|---|---|---|
| T-S-1 | Sub-agent count | `≥ 6` (scaffold×3 + feature + writer + code-review; v34: 6) | Agent-tool call count | `[gate]` |
| T-S-2 | Scaffold sub-agents dispatched in parallel | 3 parallel (apidev + appdev + workerdev) | dispatch timestamps | `[gate]` |
| T-S-3 | Feature sub-agent single-author across all 3 mounts | 1 feature dispatch (not per-codebase) | dispatch count | `[gate]` |
| T-S-4 | Writer sub-agent fresh-context v8.94 shape | writer brief contains "no memory of the run" + facts-log pointer (v34: held) | dispatch payload grep | `[gate]` |
| T-S-5 | Code-review 3-split framework-expert pattern (apidev NestJS / appdev Svelte / workerdev NestJS-worker) | 3 parallel reviews `[S]` (v22 introduced; v34: combined) — target: parallel when tier has ≥2 framework kinds | dispatch count | `[signal]` |
| T-S-6 | Both deploy.browser AND close.browser fire | Each ≥ 1 `zerops_browser` call (v34: held; v18/v19 regressed on close) | `zerops_browser` event timestamps | `[gate]` |
| T-S-7 | Root README intro names ≥ 1 Zerops brand from managed-service allowlist | Passes `v17 dbDriver` class (12th+ consecutive run; v34 held) | grep | `[gate]` |
| T-S-8 | Showcase workerdev codebase exists when `plan.Research.SharesCodebaseWith` empty | Deliverable has `workerdev/` directory with `src/` | `ls` | `[gate]` |

### 10.2. Minimal-only `[M]`

Per README.md §2 — no existing minimal run has `SESSIONS_LOGS/`; RESUME decision #1 leans toward commissioning a fresh minimal run for step-1 flow reconstruction. These bars are derived from `recipe.md` specs + extrapolation; refined after the commissioned minimal run lands.

| # | Bar | Threshold | Source | Severity |
|---|---|---|---|---|
| T-M-1 | Sub-agent count | `≥ 3` (scaffold-single-inline + writer + code-review) | Agent-tool call count | `[gate]` |
| T-M-2 | Feature sub-agent NOT dispatched `[M]` | main agent writes features inline (per README.md §2 asymmetry) | dispatch count | `[signal]` |
| T-M-3 | Writer sub-agent fresh-context shape (NOT the old `readme-with-fragments`) | brief matches v8.94 fresh-context template (NEW per README.md §2 "stays as-is / gets rewritten") | dispatch payload grep | `[gate]` |
| T-M-4 | Per-codebase surface | 1–2 codebases (not 3); worker codebase absent unless multi-runtime minimal | `ls` | `[signal]` |
| T-M-5 | Env tiers | 4 (dev + stage + prod tier) — fewer than showcase's 6 | `ls environments/` | `[signal]` |
| T-M-6 | Published tree size | `≤ 2 MB` (minimal is smaller-surface) | `du -sh export/` | `[signal]` |

### 10.3. Shared bars (apply at both tiers)

All bars in §1–§9 that do NOT carry `[S]` / `[M]` prefix apply unchanged to both tiers.

---

## 11. Hybrid / derived bars — cross-category consistency

| # | Bar | Threshold | Source | Severity |
|---|---|---|---|---|
| H-1 | Every check in the current suite with a `keep` or `rewrite-to-runnable` disposition has an author-runnable shim OR in-atom Runnable Pre-Attest block | 100% per [check-rewrite.md §17](../03-architecture/check-rewrite.md) — 72 of 73 (1 deleted) | atom inspection | `[gate]` |
| H-2 | Shim subcommand surface — 16 `zcp check <name>` subcommands exist per [check-rewrite.md §18](../03-architecture/check-rewrite.md) | 16 subcommands callable, each returns exit 0/non-0 against mount input | `zcp check --help` + invocation | `[gate]` |
| H-3 | Writer's ZCP_CONTENT_MANIFEST.json fact count matches facts-log distinct-title count | Equal (v34: held) | `jq` vs facts-log scan | `[gate]` |
| H-4 | Every atom file `phases/*/entry.md` predicate reads "substep X completes when predicate P holds" (P4 positive form) | 100% (NEW build lint) | build-time grep for forbidden "your tasks for this phase are" phrasing | `[gate]` |
| H-5 | TodoWrite use within substep (check-off-only pattern per RESUME #3) | `≥ 50% of TodoWrite calls are incremental updates, not full-list rewrites` | TodoWrite tool use content diff | `[signal]` |

---

## 11a. Editorial-review surface (refinement 2026-04-20 — P7 institutionalized at runtime)

Per [principles.md P7](../03-architecture/principles.md) + [spec-content-surfaces.md line 317-319](../../spec-content-surfaces.md) + [defect-class-registry.md §15.1 classification-error-at-source](defect-class-registry.md). Editorial-review is a new sub-agent dispatched at close.editorial-review; its return payload populates these bars. All are dispatch-runnable rather than shell-runnable (per [check-rewrite.md §16a](../03-architecture/check-rewrite.md)).

| # | Bar | Threshold | Source | Severity |
|---|---|---|---|---|
| ER-1 | Editorial-review CRIT count shipped after inline-fix (wrong-surface items that survive editorial's inline-delete to export) | `== 0` (wrong-surface CRITs must be deleted or reclassified before close complete) | `Sub[editorial-review].return.CRIT_count` post inline-fix | `[gate]` — **NEW headline bar #6** |
| ER-2 | Editorial-review reclassification delta (writer-classification vs reviewer-classification disagreement) | `== 0` post inline-reclassify | `Sub[editorial-review].return.reclassification_delta` | `[gate]` |
| ER-3 | Editorial-review citation-audit failures (matching-topic gotchas without `zerops_knowledge` citation) | `== 0` | `Sub[editorial-review].return.citation_audit.uncited` | `[gate]` |
| ER-4 | Editorial-review cross-surface-duplication catches (same fact body appearing on 2+ surfaces; cross-refs don't count) | `== 0` | `Sub[editorial-review].return.cross_surface_ledger.duplicates` | `[gate]` |
| ER-5 | Editorial-review WRONG count (boundary violations + uncited + factually-wrong remaining after inline-fix) | `≤ 1` per tier (showcase: ≤1 across 3 codebases; minimal: ≤1 across 1-2 codebases) | `Sub[editorial-review].return.WRONG_count` | `[gate]` |

**Tier notes**:
- All 5 ER bars apply to both tiers — editorial-review dispatches for showcase (gated substep) and minimal (ungated-discretionary default-on per [data-flow-minimal.md §7](../03-architecture/data-flow-minimal.md)).
- Minimal's fresh-reader premise is especially load-bearing because main-inline writer tier on minimal collapses author+judge; editorial-review restores the split (per data-flow-minimal.md §11 escalation-trigger #5).

**Relationship to other bars**:
- **ER-2 (reclassification delta)** is the complement of **M-3 (writer_manifest_honesty)**. M-3 catches manifest↔content drift assuming classification is right. ER-2 catches classification wrong at source (independent reclassification). Both together: belt + suspenders.
- **ER-3 (citation-audit)** is the editorial-dispatch form of **CQ-7 (gotcha citation ratio signal)**. ER-3 upgrades CQ-7 from signal to gate at editorial-review time.
- **ER-1 (wrong-surface CRIT shipped)** catches **CQ-5 (wrong-surface items gated)** at independent-reviewer-time. ER-1 fail = CQ-5 fail + editorial reviewer didn't inline-fix (dispatch failure OR reviewer-brief gap).
- **ER-4 (cross-surface duplication)** gates the class CQ-11 (`cross-README dedup`) expresses in one dimension (Jaccard); ER-4 covers the broader "fact body appears on multiple surfaces" case the Jaccard doesn't.

---

## 12. Bar-set audit — coverage check

### 12.1. Every README.md §10 success criterion → ≥1 bar

| README §10 criterion | Bar(s) |
|---|---|
| Wall ≤ 90 min | O-1, O-1M |
| Main bash ≤ 10 min | O-2 |
| 0 very-long | O-3 |
| 0 MCP schema errors | O-5 |
| 0 SUBAGENT_MISUSE | O-6 |
| 0 `.git/index.lock` contention | O-7 |
| 0 "File has not been read yet" | O-8 |
| 0 zcp-side execs | O-9 |
| 0 phantom output trees | O-10 |
| 0 auto-export | O-11 |
| 0 Unicode box-drawing | O-12 |
| Seed uses static key | O-13 |
| Deploy fix rounds ≤ 2 | C-1 |
| Finalize rounds ≤ 1 | C-2 |
| Gotcha-origin ≥ 80% genuine | CQ-1 |
| 0 self-inflicted as gotchas | CQ-2 |
| 0 folk-doctrine fabrications | CQ-3 |
| 0 version anchors in published | CQ-4 |
| No wrong-surface items | CQ-5 |
| CLAUDE.md ≥ 1200 bytes + ≥ 2 custom sections | CQ-8 |
| 0 facts shipped as gotchas while manifest routes elsewhere | M-3a..f |
| 0 cross-scaffold env-var naming mismatches | CS-1, CS-2 |
| 0 CRIT shipped after close | CR-1 |
| WRONG count ≤ v34's 3 | CR-3 |
| Both tiers hit tier-specific bars | §10.1 + §10.2 |

No README §10 criterion lacks a bar.

### 12.2. Every `defect-class-registry.md` `calibration_bar` → ≥1 bar in this file

Cross-checked all 68 registry rows — every `calibration_bar` value appears verbatim or as a more-restrictive variant in §1–§10.

### 12.3. Every principle P1–P8 gates ≥1 bar

| Principle | Representative bars |
|---|---|
| **P1** author-runnable pre-attest | C-1, C-2, C-3, C-4, H-1, H-2 |
| **P2** leaf-artifact brief | B-1, B-2, B-3, B-4, B-6 |
| **P3** SymbolContract | CS-1..9 |
| **P4** server state = plan | C-6, C-7, C-8, C-11, H-4 |
| **P5** two-way graph | M-1..6 |
| **P6** atomic guidance | B-5, H-4 |
| **P7** cold-read + defect-coverage | CQ-1..12 (measured via step-4 cold-read artifacts + post-run audit); **ER-1..ER-5 (refinement 2026-04-20 — P7 institutionalized at runtime via editorial-review sub-agent; every v35+ run executes the cold-read tests automatically)** |
| **P8** positive allow-list | B-7, O-10..14 (positive-form-declared canonical states) |

---

## 13. Single most-important v35 measurements (headline)

Updated refinement 2026-04-20 — **6 headline bars** instead of 5; adds ER-1. If forced to pick 6 bars that encode "did the rewrite work":

1. **C-1** — Deploy rounds ≤ 2. The single-most-empirically-refuted axis in the v31→v34 sequence.
2. **C-2** — Finalize rounds ≤ 1. Same class, different phase.
3. **C-9** — ≥ 2 `Scope="downstream"` facts per run. Theme-B adoption was the most-load-bearing v8.96 unknown; v33 passed (3); v34 unclear.
4. **M-3b** — `(routed_to=claude_md, published_gotcha)` mismatches = 0. The v34 surfaced class; P5 direct fix; not covered by any prior mechanism.
5. **CS-1** — `symbol-contract-env-consistency` shim exits 0. The v22→v34 recurrence class's direct fix; P3 proves out or fails.
6. **ER-1** — Editorial-review CRIT count shipped after inline-fix = 0 (refinement 2026-04-20). The spec-prescribed reviewer role's direct test; classification-error-at-source + wrong-surface + self-referential all surface here or are inline-fixed before close-complete. Without editorial-review the class ships despite compliance gates; with editorial-review it either inline-fixes or is rolled back.

If any ONE of these 6 regresses against v35's run, the rewrite's core thesis needs revisiting.

---

## 14. Bars explicitly NOT gated

Per [README.md §9](../README.md) + RESUME #5 (conservative). Deferred to post-v35 evaluation.

- **Cross-codebase architecture narrative in root README** (registry 4.6) — `[advisory]`; check rolled back per v24; v35 doesn't gate.
- **Self-referential gotcha count** (registry 14.4) — `[signal]` only; editorial.
- **3-split code-review pattern** (registry T-S-5) — `[signal]`; historical behavior was 1 unified review, v22 introduced split; v35 target but not gate.
- **Environment README tier-transition token presence** (ER-4) — `[signal]`; coverage check, not hard gate.
- **CLAUDE.md byte-size upper bound** — no bar (v34 shipped 6246/4942/5602 as new peak; growth is a positive direction).
- **Sub-agent dispatch prompt size upper bound** — `[signal]` at 20 KB; not a gate until P6 atomization surfaces whether 20 KB is the right floor or ceiling.

---

## 15. Using this file

- **During v35 run**: the pre-attest runnable shims (§3 rows with "shim" source) are invoked by the author before each attestation; every gate-class bar maps to at least one shim, one check, or one session-log-parse.
- **Immediately post-v35**: run the grep/jq/wc one-liners in §1–§9 against the exported deliverable + session logs; produce a markdown report crossing each numbered bar with `PASS`/`FAIL` + evidence.
- **v35 post-mortem**: any `[gate]` bar failing triggers a design-level review per step-6 rollback-criteria; any `[signal]` regression is captured as a v36 plan input, not a rollback trigger.
- **Step-6 migration proposal**: cleanroom vs parallel-run is decided in part by the risk of regressing a `[gate]` bar the current system already satisfied; parallel-run preserves fallback, cleanroom forces binary outcome per bar.
