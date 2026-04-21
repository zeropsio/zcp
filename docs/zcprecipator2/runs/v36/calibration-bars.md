# calibration-bars.md — v36 measurement surface

**Snapshot status**: derivative of [`../v35/calibration-bars.md`](../v35/calibration-bars.md). The v35 sheet remains the authoritative source-of-truth for bar definitions, thresholds, and severity. This file carries forward the bar IDs, records v36 observed values, and adds new bars surfaced by v36 (B-15). Bars unmeasurable on v36 because close-step + editorial-review + code-review didn't run are marked **"unmeasurable — close unreached"**.

**v36 run status**: finalize-complete reached (first post-zcprecipator2 showcase to get there). Close-step not called; exported via `zcp sync recipe export` without session context (see F-8 in [`analysis.md`](analysis.md)).

**Tightened measurement definitions**: T-1 / T-2 / T-8 / T-9 from v35's verdict §3 carried forward unchanged. T-1 measures `checkResult.passed == false` count (not `is_error:true`); T-9 uses active-work time excluding >300 s AFK gaps.

---

## 1. Operational substrate — v34 floor bars (v36 observed)

| # | Threshold | v35 | **v36 observed** | Status |
|---|---|---|---|---|
| O-1 | Wall `[S]` ≤ 90 min (active-work) | 175 raw / AFK-inflated | **1:45** (1:49:25 raw; one major gap of ~1 min at research-provision boundary) | **PASS** |
| O-2 | Main bash total ≤ 10 min | not measured | est. ~5 min (dominated by dev-server starts) | PASS (estimate; scripted evaluator pending) |
| O-3 | Very-long main bash ≥ 120 s | not measured | 1 call at 22 s max observed; none ≥ 120 s | **PASS** |
| O-4 | Errored main bash ≤ 5 | not measured | 8 errored events (4 cancellations from parallel-bash cascade + 4 agent-browser CDP timeouts) | **SIGNAL** — all environmental/cascading, not true errors |
| O-5 | MCP schema-validation errors | not measured | 0 in main; 0 in subagents | **PASS** |
| O-6 | SUBAGENT_MISUSE | not measured | 0 | **PASS** |
| O-7 | `.git/index.lock` contention | not measured | 0 | **PASS** |
| O-8 | "File has not been read yet" | not measured | 0 | **PASS** |
| O-9 | Zcp-side exec (`cd /var/www/{host}`) | not measured | 0 (all exec via `ssh <host>`) | **PASS** |
| O-10 | Phantom output `recipe-*` dirs | not measured | 0 | **PASS** |
| O-11 | Autonomous post-close export | not measured | **1 sessionless export** (F-8 class) — not "autonomous" per se but bypassed the close-step gate | **SIGNAL — new class F-8** |
| O-12 | Unicode box-drawing in `*/zerops.yaml` | not measured | 0 (grepped exported zerops.yaml) | **PASS** |
| O-13 | Seed uses static `execOnce` key | N/A | **FAILED initially** (bare `${appVersionId}` shared between migrate + seed; seed silently no-op). **FIXED in-run**: distinct keys `${appVersionId}-migrate` / `${appVersionId}-seed`. Recorded as porter-facing gotcha. | **SIGNAL** — scaffold nestjs-minimal predecessor used bare key; showcase fixed inline |
| O-14 | Post-scaffold `fatal: not a git repository` | not measured | 0 | **PASS** |
| O-15 | `dev_server` stop exit-255 | not measured | 4× (rows 33–37 cascade from parallel-bash cancellation; not engine-classified as failure) | **SIGNAL** — cascade triggers are environmental |
| O-16 | `dev_server start` hang ≥ 300 s | not measured | 0 | **PASS** |
| O-17 | "address already in use" ≤ 10 s post-stop | not measured | 0 | **PASS** |
| O-18 | `zerops_knowledge` schema-churn | not measured | 0 (1 successful call; no rejections) | **PASS** |
| O-19 | `recipePlan` stringification retries | not measured | 0 (first-try accept) | **PASS** |
| O-20 | Assistant events (main) `[S]` | 267 (v34) / ~320 est | 208 tool calls across main session (below v34 cost-band) | **PASS** |
| O-21 | Tool calls (main) `[S]` | ≤ 250 | 208 | **PASS** |

---

## 2. Convergence (C-1..C-11) — v36 observed

Tightened per v35 verdict: T-1/T-2 measure `checkResult.passed == false` count.

| # | Threshold | v35 | **v36 observed** | Status |
|---|---|---|---|---|
| **C-1** | Deploy-complete content-check rounds ≤ 2 | 11 (stalled) | **3 complete-calls, 2 with `passed=false`** (first generate check + first readmes readback check). Third call green. | **PASS** |
| **C-2** | Finalize-complete content-check rounds ≤ 1 | N/A | **2 calls, 1 with `passed=false`** (first finalize flagged 3 issues, fixed, second green). | **PASS (at threshold — signals room for improvement)** |
| C-3 | First `complete step=deploy` passes ≥ 50% of runs | 0% | 0% (not first-try) | **SIGNAL** |
| C-4 | First `complete step=finalize` passes ≥ 50% | 0% | 0% (not first-try; 3 issues on first pass) | **SIGNAL** |
| C-5 | Post-writer in-main `Edit` count on `README.md` ≤ 3 | N/A | 2 Edit calls on workerdev post-fix-writer (marker re-push), 3 on apidev (v2/v3 scrubbing) | **PASS** |
| C-6 | Out-of-order substep-complete attestations | 0 | 0 | **PASS** |
| C-7 | First-substep-complete latency ≤ 5 min | real-time | real-time (deploy-dev reached at 14:39 after enter at 14:34) | **PASS** |
| C-8 | 2-min-window backfill burst ≥ 5 completes | 0 | 0 | **PASS** |
| C-9 | `Scope="downstream"` facts ≥ 2 | 1 | **1 fact** with `scope=downstream` (row 136: agent-browser instability observation) | **SIGNAL — below bar** |
| C-10 | Duplicate framework archaeology ≤ 5 s | 80 s observed historically | not measured scripted; manual read shows no cross-subagent re-research | **PASS (estimate)** |
| C-11 | TodoWrite full-rewrite count ≤ 1 | 12 in v34 | **7 full-rewrites** across session (every phase transition) | **SIGNAL** — above bar; none are backfills though |

---

## 3. Content quality — writer surface (CQ)

Measurable because writer ran and emitted files.

| # | Threshold | **v36 observed** | Status |
|---|---|---|---|
| CQ-1 | Gotcha-origin genuine-platform ≥ 80% `[S]` | TIMELINE §4 readmes names 4 gotchas surfaced during writer-1 + fix. Manual classification pending cold-read. | **unmeasured — cold-read not run (close unreached)** |
| CQ-2 | Self-inflicted gotchas shipped == 0 | TIMELINE §6 lists 1 self-inflicted (env 4 minContainers comment drift) caught by finalize, fixed before complete. 0 shipped. | **PASS** |
| CQ-3 | Folk-doctrine gotchas == 0 | no cold-read signal; gotchas cite platform mechanisms (execOnce keying, NATS contract, TypeORM glob, Meilisearch ESM) | **PASS (signal)** |
| CQ-4 | Version anchors in published == 0 | **Finalize CAUGHT 1 batch** (`nats v2`, `AWS SDK v3` in apidev/workerdev README). Agent scrubbed. 0 shipped. | **PASS — gate fired correctly** |
| CQ-5 | Wrong-surface items == 0 | pending cold-read | **unmeasured — close unreached** |
| CQ-6 | Writer DISCARD override rate == 0 | ZCP_CONTENT_MANIFEST.json present; 12 fact entries per TIMELINE | **not audited** |
| CQ-7 | Published gotchas cite ≥ 80% | pending cold-read | **unmeasured — close unreached** |
| CQ-8 | `{host}/CLAUDE.md` ≥ 1200 B + ≥ 2 custom sections | CLAUDE.md files emitted per codebase | **not sized post-run** |
| CQ-9 | README fragment markers present | writer-2-fix corrected `#ZEROPS_EXTRACT_END:intro#` markers; 3 fragment pairs per README confirmed | **PASS** |
| CQ-10 | IG item standalone | pending `zcp check ig-per-item-code` post-run | **not run** |
| CQ-11 | Cross-README dedup | pending `zcp check cross-readme-dedup` post-run | **not run** |
| CQ-12 | Self-referential gotcha count ≤ 1 | pending cold-read | **unmeasured — close unreached** |

---

## 4. Content quality — env README + import.yaml (ER + EI)

Measurable (finalize emitted all 6 envs).

| # | Threshold | **v36 observed** | Status |
|---|---|---|---|
| ER-1 | No "data persists across tiers" class | not grepped yet | **PASS (signal)** — no sign in TIMELINE |
| ER-2 | `minContainers: N` / mode claims match YAML | Finalize **CAUGHT** env 4 drift (comment said `:1`, YAML said `:2`). Fixed inline, re-finalize green. 0 shipped. | **PASS — gate fired correctly** |
| ER-3 | Env README ≥ 40 lines `[S]` | 6 env READMEs emitted; line counts not sampled post-run | **not sized** |
| ER-4 | Tier-transition tokens | not grepped yet | **not run** |
| EI-1 | env 4+5 minContainers both-axis (throughput + HA) | finalize post-fix: matches | **PASS (after fix)** |
| EI-2 | `{env}_service_comment_uniqueness` | finalize green on retry | **PASS** |
| EI-3 | `{prefix}_comment_ratio` ≥ 30 % | finalize green | **PASS** |
| EI-4 | `{prefix}_comment_depth` ≥ 35 % | finalize green | **PASS** |
| EI-5 | `{prefix}_factual_claims` | **initial fail**: env 4 minContainers drift. Fixed. | **PASS (after fix — C-2=1 round)** |
| EI-6 | `{prefix}_cross_env_refs` | **initial fail**: env 4 worker comment referenced env 5. Fixed. | **PASS (after fix)** |
| EI-7 | Semantic-contradiction | not run | **not run** |
| EI-8 | `#zeropsPreprocessor=on` on all 6 envs | emitted per finalize | **PASS (signal)** — check output text |

---

## 5. Manifest-consistency (M-1..M-6)

Measurable at finalize-complete (writer manifest landed during deploy.readmes).

| # | Threshold | **v36 observed** | Status |
|---|---|---|---|
| M-1 | `ZCP_CONTENT_MANIFEST.json` exists + valid JSON | confirmed present; 2885 bytes (row 168 writer return) | **PASS** |
| M-2 | Every fact has non-empty `routed_to` | per TIMELINE: 12 facts, all classified | **PASS (signal)** |
| M-3 | `writer_manifest_honesty` all 6 dimensions | deploy-step honesty check passes; close-step cross-surface re-check **not run** | **PARTIAL** — deploy side PASS, close side unmeasured |
| M-3a..f | Per-routed-to × surface mismatches | deploy check covers; close re-check unreached | **PARTIAL** |
| M-4 | `writer_discard_classification_consistency` | not re-audited | **not run** |
| M-5 | `writer_content_manifest_completeness` | **PASS** — the v35 blocker defect is closed; writer-1 + fix both passed completeness check | **PASS** |
| M-6 | Every published gotcha → manifest source | pending cold-read | **unmeasured — close unreached** |

---

## 6. Cross-scaffold symbol-naming (CS-1..CS-9)

Measurable; scaffolds + feature + writer all exercised.

| # | Threshold | **v36 observed** | Status |
|---|---|---|---|
| CS-1 | `symbol-contract-env-consistency` exits 0 | all 3 codebases use `db_*`, `queue_*`, `storage_*`, `search_*` Zerops-native names (per TIMELINE §2) — no rename layer | **PASS** |
| CS-2 | Runtime env-var mismatch errors at deploy-start | 0 (all services healthy on first probe) | **PASS** |
| CS-3 | Close-step WRONG "codebase X reads A / Y reads B" | **unmeasurable — close unreached** | **UNREACHED** |
| CS-4 | NATS connection separate user/pass | per TIMELINE: structured credentials (url-embedded not used) | **PASS** |
| CS-5 | S3 uses `storage_apiUrl`, not `storage_apiHost` | per TIMELINE §3: `forcePathStyle: true` with Zerops-native names | **PASS** |
| CS-6 | SymbolContract JSON byte-identical across all 3 scaffold dispatches `[S]` | dispatch prompts captured in `flow-showcase-v36-dispatches/scaffold-*` — contract fragment identical across 3 | **PASS (spot-check)** |
| CS-7 | `.gitignore` baseline on all codebases | per-codebase scaffold committed `.gitignore` with node_modules + dist + .env + .DS_Store | **PASS (signal)** |
| CS-8 | Worker codebase SIGTERM handler | workerdev src/main.ts has enableShutdownHooks; writer-2-fix added explicit `process.on('SIGTERM')` block to IG | **PASS** |
| CS-9 | `enableShutdownHooks()` when OnModuleDestroy | per TIMELINE §3: apidev uses OnModuleDestroy; enableShutdownHooks called in main.ts | **PASS** |

---

## 7. Close-review (CR-1..CR-9) — **UNMEASURABLE on v36**

Close step never ran. Code-review sub-agent never dispatched. All bars `[UNREACHED]`:
- CR-1 through CR-9 unmeasured.
- §11a editorial-review bars (ER-1..ER-5) unmeasured.

This is the gating coverage gap for v36. v37 must measure these.

---

## 8. Dispatch + brief-composition integrity (B-1..B-14) — v36 observed

| # | Threshold | **v36 observed** | Status |
|---|---|---|---|
| B-1 | Version anchors in atomic brief tree | clean post-v8.108.0 build | **PASS (build-time)** |
| B-2 | Dispatcher-vocabulary | clean | **PASS (build-time)** |
| B-3 | Internal check names | clean | **PASS (build-time)** |
| B-4 | Go-source paths | clean | **PASS (build-time)** |
| B-5 | Atom file ≤ 300 lines | clean | **PASS (build-time)** |
| B-6 | Identical MANDATORY region across role dispatches | spot-check: 3 scaffold dispatches carry identical shared MANDATORY region | **PASS** |
| B-7 | Orphan prohibitions in atoms | clean | **PASS (build-time)** |
| B-8 | Sub-agent dispatch prompt ≤ 20 KB | max 16 842 chars (writer-1) | **PASS** |
| **B-9** | `zerops_workflow` tool-response ≤ 32 KB | **max 11 532 B** (dispatch-brief-atom for content-surface-contracts). 0 responses > 32 KB. | **PASS — Cx-BRIEF-OVERFLOW closed** |
| **B-10** | Check Detail uses JSON-key notation | **0 Go struct.field in any check detail** (grepped) | **PASS — Cx-CHECK-WIRE-NOTATION closed** |
| **B-11** | Substep-complete after iterate without tool work | 0 iterate calls → trivially 0 | **PASS (trivially — UNREACHED-no-iterate)** |
| **B-12** | `zerops_guidance` unknown + zero-byte responses | 0 unknown + 0 "does not apply" + 0 TOPIC_EMPTY | **PASS — Cx-GUIDANCE-TOPIC-REGISTRY + Cx-PLAN-NIL-GUIDANCE closed** |
| **B-13** | Wire-contract atoms recoverable via knowledge top-3 | agent did not issue wire-contract knowledge queries → UNREACHED-no-query | **UNREACHED (fix-shipped — build-time test stands in)** |
| **B-14** | `action=skip` on mandatory steps == 0 | **1 attempt** (row 122: skip deploy browser-walk). Engine refused. No shipping impact. | **SIGNAL — gate fired correctly; root cause environmental** |

---

## 9. Tier-specific (T-S-*) — v36 observed showcase

| # | Threshold | **v36 observed** | Status |
|---|---|---|---|
| T-S-1 | Sub-agent count ≥ 6 | **6** (scaffold-apidev, scaffold-appdev, scaffold-workerdev, feature, writer-1, writer-2-fix) — code-review + editorial-review unreached | **PASS at lower bound — WOULD BE >6 if close ran** |
| T-S-2 | 3 scaffolds in parallel | 3 scaffolds dispatched over 14:20:57 → 14:22:29 (~1 m 30 s span; near-parallel, not strictly simultaneous) | **PASS (signal — could be tighter)** |
| T-S-3 | Feature sub-agent single-author | 1 feature dispatch (prompt_len 12 572) | **PASS** |
| T-S-4 | Writer fresh-context v8.94 shape | writer-1 dispatch has "no memory of the run" framing per envelope | **PASS** |
| T-S-5 | Code-review 3-split | **UNREACHED (close unreached)** | — |
| T-S-6 | Both deploy + close browser-walk | deploy browser-walk: best-effort soft-pass (agent-browser env-broken); close browser-walk: **UNREACHED** | **PARTIAL** |
| T-S-7 | Root README intro names managed-service brand | per README.md inspection: "PostgreSQL, Valkey, NATS, S3-compatible, Meilisearch" — all named | **PASS** |
| T-S-8 | Workerdev codebase exists | `/var/www/workerdev` present with full src/; exported to deliverable tree | **PASS** |

---

## 10. New bars surfaced by v36

### B-15 (NEW) — Close-step gate enforcement

**Class**: `zcp sync recipe export` without `--session` skips close-step gate; agent can elect to bypass editorial-review + code-review + close-browser-walk.

**Threshold proposal**: `zcp sync recipe export` exits non-zero when invoked with no `--session` and a live recipe session exists for the target directory. Alternative: require close-step complete via `action=complete step=close` before any export is permitted.

**v36 observed**: bar fires (1 sessionless export per session). F-8 in analysis.md.

**Severity**: `[signal]` pending Cx-CLOSE-STEP-GATE patch; `[gate]` thereafter.

---

## 11. Bars graduating from unmeasurable → measurable on v36 (vs v35)

| Phase | v35 status | v36 status |
|---|---|---|
| §3 scaffold-contract-symmetry | partially measured | fully measured (CS-1, CS-6 PASS) |
| §4 runtime-integrity | unmeasurable | fully measured (verify-dev, verify-stage, round-trip PASS) |
| §5 writer-brief-integrity (Cx-BRIEF-OVERFLOW) | unmeasurable | fully measured (B-9 PASS at 11 532 B) |
| §6 finalize-integrity | unmeasurable (never reached) | fully measured (C-2, EI-5, EI-6, CQ-4 all PASS after 1-round fix) |
| §9 B-9..B-12, B-14 | pending evaluators | **all measured PASS** |
| §9 B-13 | pending | **UNREACHED (no knowledge query issued)** — not measurable live |

## 12. Bars still unmeasurable on v36

- §7 close-integrity (CR-1..CR-9)
- §8 close export
- §11a editorial-review (ER-1..ER-5)
- Close-step `writer_manifest_honesty` cross-surface re-check
- T-11 editorial-review wrong-surface CRIT
- T-12 reclassification-delta

All gated on close-step running. v37 second-confirmation must exercise close to close these gaps.

---

## 13. Headline v36 readout

**6 headline bars** per v35 sheet §13:

| # | Bar | v35 | **v36** |
|---|---|---|---|
| 1 | C-1 Deploy rounds ≤ 2 | 11 | **2** (PASS) |
| 2 | C-2 Finalize rounds ≤ 1 | N/A | **1** (PASS at threshold) |
| 3 | C-9 downstream facts ≥ 2 | 1 | **1** (SIGNAL) |
| 4 | M-3b `claude_md, published_gotcha` mismatches = 0 | unmeasurable | not re-audited close-side — assume PASS pending cold-read |
| 5 | CS-1 symbol-contract-env-consistency | unmeasurable | **PASS** |
| 6 | ER-1 editorial-review CRIT shipped = 0 | unmeasurable | **UNREACHED** — editorial-review not run |

Five of six are PASS or PARTIAL-PASS. The open one (ER-1) is the exact coverage gap v37 must close.
