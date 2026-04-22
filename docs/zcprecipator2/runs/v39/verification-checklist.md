# runs/v39 verification checklist

**Machine report SHA**: `74d7fcbcf6cfe67572dfe684e764ca834c019919edc137d7bfff9e2023f8fbd2`
**Generated at**: 2026-04-22T20:06:38Z
**Tier**: showcase
**Slug**: nestjs-showcase
**Deliverable**: `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v39`
**Analyst**: Claude Opus 4.7 (1M context) on behalf of Ales Rechtorik
**Analyst session start (UTC)**: 2026-04-22T20:06Z

## Phase reached

- [ ] `close` complete (auto=false; [main-session.jsonl L584 `action=skip step=close substep=editorial-review`](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/SESSIONS_LOGS/main-session.jsonl) 19:53:02Z — main agent bypassed close-step gate after `verify-subagent-dispatch` SHA mismatch. Close did NOT complete. All downstream editorial-review + close-browser-walk cells are `unmeasurable-invalid` — the files ARE on disk for analyst Read but engine attestation never landed)
- [x] editorial-review dispatched (auto; agent-af380cd700526da0d at L426, description "Editorial review of recipe reader-facing content")
- [x] code-review dispatched (auto; agent-a677d909a28774a11 at L489, description "Code review: NestJS + Svelte showcase")
- [ ] close-browser-walk attempted (auto=false; not observed in main-session — no `substep=close-browser-walk` after close/editorial-review was skipped; the run aborted close traversal at editorial-review step)

## Structural integrity bars (auto)

- [x] B-15 ghost_env_dirs: threshold 0, observed 0, **status pass** — canonical 6 env folders present; F-9 stays CLOSED.
- [x] B-16 tarball_per_codebase_md: threshold 6, observed 6, **status pass** — every codebase has README.md + CLAUDE.md. NOTE: B-16 measures per-codebase MD presence, which is true. The ZCP_CONTENT_MANIFEST.json is NOT in this bar but IS absent-until-user-added-manually from the deliverable root — F-23 runtime-close did NOT hold (user notes manifest was manually added post-run; automated export still drops it). See §Manifest integrity.
- [x] B-17 marker_exact_form: threshold 0, observed 0, **status pass** — markers correct in FINAL deliverable tree, but writer first-pass emitted `<!--#X#-->` (no spaces around `#`); main agent ran a marker-form fix + re-overlay sequence. See Retry-cycle 15 attribution.
- [x] B-18 standalone_duplicate_files: threshold 0, observed 0, **status pass** — F-13 stays CLOSED.
- [x] B-22 atom_template_vars_bound: threshold 0, observed 0, **status pass** — build-time lint clean at HEAD.

## Session-metric bars (auto)

- [x] B-20 deploy_readmes_retry_rounds: threshold 2, observed 1, **status pass** — evidence: 1 deploy/readmes-class failing cycle at L15 (cycle 15). v38 had 2; v39 improved.
- [x] B-21 sessionless_export_attempts: threshold 0, observed 3, **status fail** — 3 `zcp sync recipe export … --include-timeline` attempts without `--session` before close-completion. Attribution: main agent attempted tarball production after `action=skip` bypass of editorial-review; sessionless export is how the current tree was produced (close never completed → session-bound export refused → main agent dropped `--session` to force the export). F-11 still open (close gate advisory, not hard-blocking).
- [x] B-23 writer_first_pass_failures: threshold 3, observed 6, **status fail** — evidence: `api_comment_specificity, comment_ratio, fragment_integration-guide_blank_after_marker, fragment_intro_blank_after_marker, fragment_knowledge-base_blank_after_marker, intro_length`. v38 was 9; v39 improved but still above threshold. The three `_blank_after_marker` failures class matches v38's pre-scaffold gap — Cx-2b (blank-after-marker close) never landed.
- [x] B-24 dispatch_integrity: threshold 0, observed 0 (auto), **status analyst-divergent** — see Dispatch integrity section. Analyst observes TWO divergences: writer `\u2014`→em-dash class (same as v38, 4-byte), editorial-review SHA mismatch (self-reported by main agent in L584 action=skip reason, "em-dash/quote transcoding"). Harness auto-0 is a reporting gap (B-24 requires captured engine briefs which harness doesn't compute).
- sub_agent_count: 7 (writer, feature, code-review, editorial-review, scaffold-apidev, scaffold-appdev, scaffold-workerdev) — identical to v38.

## Dispatch integrity (analyst-fill per Rule 5)

Byte-diff each captured Agent dispatch prompt against `BuildSubagentBrief(plan, role, factsLog, manifest)` reconstruction at run-binary version. Engine-built briefs reconstructed from v8.113.0 HEAD source diverge STRUCTURALLY from run dispatches (classification-taxonomy + routing-matrix present in run dispatch but replaced by classification-pointer at v8.113.0 HEAD) — **this indicates the v39 run used a pre-v8.113.0 binary** (likely v8.112.0 or v8.112.1), NOT the Commit-1…Commit-5 stack. See verdict §4 FINDING-V39-BINARY-MISMATCH.

- **Author recipe READMEs + CLAUDE.md + manifest** (writer, main-session L~390):
  - [x] dispatch_vs_source_diff: **Status: divergent (structural + encoding)** — prompt_len 60558 bytes (under the 65KB tripwire). Writer brief contains full `briefs.writer.classification-taxonomy` (lines 259-341) + `briefs.writer.routing-matrix` (lines 356-427) — at v8.113.0 HEAD [atom_stitcher.go:209](../../../internal/workflow/atom_stitcher.go#L209) `writerBriefBodyAtomIDs()` uses `briefs.writer.classification-pointer` (a 40-line pointer atom) INSTEAD OF those two. The run bytes reflect the pre-Commit-5a brief shape. Writer brief ALSO lacks "Pre-loaded input — per-codebase `zerops.yaml`" section (Commit 3c absent) — grep count 0 in dispatch. ALSO lacks "Pre-loaded input — annotated {surface} examples" blocks (Commit 3 example-bank absent).
  - Root cause: runtime binary predates v8.113.0. Neither Commit 3 nor Commit 5 content is visible at dispatch time.
  - [x] Read-receipt: 2026-04-22T20:32Z
- **Editorial review of recipe reader-facing content** (editorial-review, main-session L517):
  - [x] dispatch_vs_source_diff: **Status: divergent (encoding-class, per main-agent's own diagnosis)** — prompt_len 46950 bytes (v38 was 13229 bytes at 72% compression). F-17 paraphrase-class at dispatch-byte layer is SIGNIFICANTLY IMPROVED vs v38 (likely because Cx-5 BuildSubagentBrief at v8.112.0 hardened the engine-side stitch; main agent forwarded near-verbatim this time). HOWEVER: main agent's own `verify-subagent-dispatch` call at L564 FAILED SHA check ("re-built brief for SHA verification mismatched dispatched bytes (likely em-dash/quote transcoding)" per L584 action=skip reason). Encoding round-trip is the same class as v38 writer's 4-byte `\u2014` defect. Guard is now CALLED (v39 new behavior, v38 called ZERO times) but the guard's strict SHA refuses the encoding-round-tripped bytes as a false positive.
  - [x] Read-receipt: 2026-04-22T20:32Z
- **Code review: NestJS + Svelte showcase** (code-review, main-session L~450):
  - [x] dispatch_vs_source_diff: **Status: unverified-inferred-divergent** — prompt_len 8177 bytes. Not re-diffed at analyst time; expected trivial divergence modulo trailing-newline (v38 pattern). No signal-grade concern for code-review dispatch shape.
  - [x] Read-receipt: 2026-04-22T20:32Z
- **Implement 5 showcase features** (feature, main-session L~220):
  - [x] dispatch_vs_source_diff: **Status: unmeasurable-valid** — feature is not a guarded role. prompt_len 16857 bytes. Deploy reached ACTIVE; features swept green dev + stage per cycles 9+13.
  - [x] Read-receipt: 2026-04-22T20:32Z
- **Scaffold NestJS API / Scaffold frontend SPA / Scaffold worker codebase** (scaffold-*, L104/107/109):
  - [x] dispatch_vs_source_diff: **Status: unmeasurable-valid** — scaffold dispatches are plan-specific. Prompts 14776 / 8552 / 9065 bytes. All 3 deploys reached ACTIVE.
  - [x] Read-receipt: 2026-04-22T20:32Z

## Primary v39 success checks (per HANDOFF-to-I10 §4 Phase 4)

- **(a) Writer brief size + dispatch integrity**:
  - (i) Writer brief ≤ 65KB tripwire: **60558 bytes = 59.1KB PASS** [flow-showcase-v39-dispatches/author-recipe-readmes-claude-md-manifest.md L7 `Prompt length: 60558`].
  - (ii) Dispatch integrity (semantic paraphrase check): **FAIL — structural divergence** — runtime dispatch contains pre-Commit-5a atom shape. See Dispatch integrity writer row. Not F-17 paraphrase-class; rather runtime-binary-lag class.
- **(b) Citation timestamps (Commit 4)**: **FAIL** — [ZCP_CONTENT_MANIFEST.json](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/ZCP_CONTENT_MANIFEST.json) schema has only `{fact_title, classification, routed_to, override_reason}` per fact; NO `citations[]` field. Commit 4 `readmes_citations_present` gate is not running (gate would refuse every content_gotcha/content_ig entry in this manifest shape). Also: manifest has only 4 facts; v38 had 12+. Under-reporting + missing schema. Commit 1 env-README regressions also confirmed (§Env README quality cross-check).
- **(c) YAML-visible-before-envComment (Commit 3b)**: **FAIL** — grep count 0 for "Pre-loaded input — rendered `import.yaml` per env (schema-only)" across main-session.jsonl. First `generate-finalize` call at [main-session L459 19:28:00Z](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/SESSIONS_LOGS/main-session.jsonl) submits envComments for all 6 tiers in one shot; main agent authored from plan-memory, not from engine-rendered yaml.
- **(d) Editorial-review CRIT count (target ≤ 1 first pass)**: **FAIL** — [main-session L557 19:50:54Z close/editorial-review attestation](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/SESSIONS_LOGS/main-session.jsonl) + [L584 19:53:02Z action=skip reason](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/SESSIONS_LOGS/main-session.jsonl): **13 CRITs** returned on first pass. Main agent attestation partitions them as "12 per-codebase content surfaces appeared absent because the writer's mount-side READMEs hadn't been overlaid into the deliverable tree (marker-form mismatch — mount used <!--#X#--> while overlay required <!-- #X# -->)" + "cross-surface duplication flags on env import.yaml comments which are by design self-contained per the cross_env_refs gate" + dismissed env-README CRITs as "Auto-generated env-tier README contradictions are a recipe-engine concern, not writer-authored content." Main agent did not cite the 4 v38-CRIT-class env-README regressions (§Env README quality cross-check) present in this deliverable.
- **(e) TodoWrite call count + starter-list paste**:
  - (i) TodoWrite count: **16 calls in main-session** (v38 had 28; target ≤ 5; PAUSE threshold > 10). **FAIL**.
  - (ii) Starter-list paste: grep count 0 for "startingTodos" across main-session.jsonl. **FAIL** — Commit 5b did not fire from engine `action=start` response.
- **(f) classify action usage (Commit 5a)**: **FAIL** — grep count 0 for `"action":"classify"` in writer sub-agent jsonl (agent-a498c72b2be6782cf.jsonl). `zerops_knowledge` called 10 times, but 0 classify. The writer brief at dispatch contains full classification-taxonomy + routing-matrix (pre-Commit-5a shape), so the writer has no reason to call runtime classify.
- **(g) Per-codebase zerops.yaml inlined in writer brief (Commit 3c)**: **FAIL** — grep count 0 for "Pre-loaded input — per-codebase" in writer dispatch. Commit 3c did not land in the runtime-binary brief build.

## Writer content quality (analyst-fill, required)

### apidev/README.md (9904 bytes)

- **intro fragment** — auto pass:
  - [x] Analyst qualitative grade: **pass** — [apidev/README.md:3](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/apidev/README.md) 1-line substantive intro naming NestJS 11 + all 5 managed services + cross-service env var mechanism.
- **integration-guide fragment** — auto pass (h3=6):
  - [x] Analyst qualitative grade: **pass** — [apidev/README.md:7-~200](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/apidev/README.md) Adding zerops.yaml (with heavily-commented full YAML), plus principle-level items. YAML comments are decision-why grounded.
- **knowledge-base fragment** — auto pass (bullets=5):
  - [x] Analyst qualitative grade: **pass** — 5 gotchas. Note: Rule 9 spot-check on 5-gotcha count — the gotcha routing can land without citations (Commit 4 absent), so the writer's gotcha shape is self-attested, not gate-verified. No folk-doctrine pattern detected in a sampled read.
- [x] Read-receipt: 2026-04-22T20:40Z

### appdev/README.md (7209 bytes)

- **intro, IG (h3=5), KB (bullets=4)**:
  - [x] Analyst qualitative grade: **pass** on all 3 fragments — substantive Svelte+Vite content; IG items concrete actions.
- [x] Read-receipt: 2026-04-22T20:40Z

### workerdev/README.md (7430 bytes)

- **intro, IG (h3=5), KB (bullets=3)**:
  - [x] Analyst qualitative grade: **pass** on all 3 fragments — worker-shape teaching, queue-group + SIGTERM supplements both present.
- [x] Read-receipt: 2026-04-22T20:40Z

### apidev/CLAUDE.md (4899 bytes)

- auto pass (4 base sections + 3 custom):
  - [x] Analyst narrative sign-off: **pass** — operator-focused content appropriate for Surface 6.
- [x] Read-receipt: 2026-04-22T20:40Z

### appdev/CLAUDE.md (3243 bytes)

- auto pass (4 base sections + 4 custom):
  - [x] Analyst narrative sign-off: **pass** — Svelte/Vite-specific dev loop, feature-by-data-testid testing table.
- [x] Read-receipt: 2026-04-22T20:40Z

### workerdev/CLAUDE.md (3540 bytes)

- auto pass (4 base sections + 3 custom):
  - [x] Analyst narrative sign-off: **pass** — worker no-port dev loop, queue group ownership, SIGTERM drain operator notes.
- [x] Read-receipt: 2026-04-22T20:40Z

## Env README quality — Rule 9 cross-check against source data

Per HANDOFF-to-I10 §6 Rule 9: each env-README prose claim that asserts a field-level difference MUST map to a `GenerateEnvImportYAML(plan, envIndex)` emitted value. A claim with no backing field is a fabrication.

**4 forbidden-phrase hits in v39 env READMEs — ALL are v38-CRIT regression classes that v39 Commit 1 was scoped to eliminate at source**:

- [x] `environments/0 — AI Agent/README.md:32` — *"The dev container in the new project ships the CDE toolchain; the managed-service set stays the same shape, but the containers are separate from this tier's."* — **FAIL** — Cluster A fabrication per [plans/v39-commit1-bullet-audit.md §5](../../plans/v39-commit1-bullet-audit.md): env 0 and env 1 emit IDENTICAL dev-slot yaml via `writeDevService(target)` (same `zeropsSetup: dev`, minRam, autoscaling). No field in `GenerateEnvImportYAML` distinguishes "CDE toolchain" from "agent toolchain". Rule 9: fabrication. v39 Commit 1 dropped this claim at [internal/workflow/recipe_templates.go:336-338](../../../internal/workflow/recipe_templates.go#L336) — present prose reframes as audience-not-image. Regression means the v39 run used a pre-Commit-1 binary.
- [x] `environments/1 — Remote (CDE)/README.md:21` — *"Runtime containers carry an expanded toolchain — IDE Remote server, shell customizations, language-specific debug tools."* — **FAIL** — Cluster A, v38 editorial-review **CRIT #1** verbatim. Same cause + same source-code gap as above.
- [x] `environments/3 — Stage/README.md:42` — *"Stage hits the same DB as dev on tiers 0-2 — schema migrations break backwards compatibility at your own risk."* — **FAIL** — Cluster E, v38 editorial-review **CRIT #3** verbatim. Factually wrong per v38 CORRECTIONS.md §2 (each tier emits distinct `project.name` via `envTiers[i].Suffix`; each has its own DB project). v39 Commit 1 DROPPED this claim at [internal/workflow/recipe_templates.go:400-406](../../../internal/workflow/recipe_templates.go#L400) — present prose says "Each tier's `project.name` is distinct … Stage has its own Zerops project with its own DB and state."
- [x] `environments/4 — Small Production/README.md:26` — *"Backups become meaningful at this tier — daily snapshots of DB and storage are retained per the recipe's backup policy."* — **FAIL** — Cluster C, no backup policy field in `RecipePlan` or env yaml emission. v39 Commit 1 DROPPED this claim.

**Editorial-review missed every one of these 4 forbidden phrases in its 13-CRIT list** (per main-agent's skip-attestation breakdown: 12 marker-overlay false-positives + 1 other-class) — or if it caught them, main agent explicitly dismissed them as "recipe-engine concern, not writer-authored content" ([L557 attestation](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/SESSIONS_LOGS/main-session.jsonl)). Either way: shipped.

- [x] Read-receipt: 2026-04-22T20:36Z

## Env import.yaml quality (spot check)

- [x] `0 — AI Agent/import.yaml`: **pass-with-qualifier** — `#zeropsPreprocessor=on` first line, project comment explains APP_SECRET + DEV_/STAGE_ URL constants, each service comment 4-8 lines of decision-why. envComments authored by main agent with factual content — NOTE per primary-check (c): authored BEFORE engine rendered yaml visibility ever fired (Commit 3b absent), so factuality is a function of the main agent's plan-memory rather than yaml-visibility-grounding. No invented numbers observed in a sampled read of env 0 (unlike v38 F-21 "2 GB quota" class).
- [x] Read-receipt: 2026-04-22T20:42Z

## Manifest integrity

- [x] `ZCP_CONTENT_MANIFEST.json` **ABSENT from run-produced deliverable; USER-ADDED post-run for analysis**. F-23 runtime-close did NOT hold. Commit 2 export whitelist extension is at HEAD [internal/sync/export.go:236](../../../internal/sync/export.go#L236) but either (i) the run binary predates Commit 2 or (ii) Commit 2 landed but overlay failed to stage the manifest into the export root OR (iii) sessionless-export path (used here per B-21=3) bypasses overlay step.
- Manifest shape: `{version: 1, facts: [{fact_title, classification, routed_to, override_reason}]}` — NO `citations` field per fact (Commit 4 schema extension absent). 4 facts total (v38 had 12+). Commit 4 gate `readmes_citations_present` is absent from the run.
- One classification routing concern: `"NATS job dispatch contract: subject=jobs.dispatch, payload=JSON {jobId,type,payload}, queue=showcase-workers"` classified as `scaffold-decision` routed to `claude_md`. Per `workflow_classify.go` handler: this should be `cross_codebase_contract` → `content_ig`. Manifest routing is misclassified — runtime classify wasn't consulted (primary check (f) = 0 classify calls).
- [x] Read-receipt: 2026-04-22T20:35Z

## Retry-cycle attribution (analyst-fill)

| Cycle | Timestamp (UTC) | Substep | Failing checks | Attribution |
|---|---|---|---|---|
| 1 | 2026-04-22T18:18:11.306Z | research/ | — | phase completion only |
| 2 | 2026-04-22T18:22:04.985Z | provision/ | — | phase completion only |
| 3 | 2026-04-22T18:41:09.367Z | deploy/deploy-dev | — | phase completion only |
| 4 | 2026-04-22T18:41:47.337Z | deploy/start-processes | — | phase completion only |
| 5 | 2026-04-22T18:42:13.890Z | deploy/verify-dev | — | phase completion only |
| 6 | 2026-04-22T18:43:13.871Z | deploy/init-commands | — | phase completion only |
| 7 | 2026-04-22T18:56:38.320Z | deploy/subagent | — | feature sub-agent complete |
| 8 | 2026-04-22T18:59:20.221Z | deploy/snapshot-dev | — | phase completion only |
| 9 | 2026-04-22T18:59:44.644Z | deploy/feature-sweep-dev | — | phase completion only |
| 10 | 2026-04-22T19:02:27.735Z | deploy/browser-walk | — | phase completion only |
| 11 | 2026-04-22T19:04:57.871Z | deploy/cross-deploy | — | phase completion only |
| 12 | 2026-04-22T19:05:18.149Z | deploy/verify-stage | — | phase completion only |
| 13 | 2026-04-22T19:05:32.540Z | deploy/feature-sweep-stage | — | phase completion only |
| 14 | 2026-04-22T19:22:51.476Z | deploy/readmes | — | phase completion only (writer dispatch) |
| 15 | 2026-04-22T19:23:04.446Z | deploy/ | `comment_ratio, fragment_integration-guide_blank_after_marker, fragment_intro_blank_after_marker, fragment_knowledge-base_blank_after_marker, api_comment_specificity, intro_length` | **B-23 writer-compliance**: writer first-pass produced unfilled boilerplate templates with wrong marker form (`<!--#X#-->` no spaces). Main agent fixed markers on mount side + re-overlaid. `_blank_after_marker` class is the same v38 Cx-2 pre-scaffold gap — Cx-2b never landed. |
| 16 | 2026-04-22T19:28:11.810Z | finalize/ | `1 — Remote (CDE)_import_cross_env_refs, 2 — Local_import_cross_env_refs, 3 — Stage_import_cross_env_refs, 4 — Small Production_import_cross_env_refs` | **Finalize cross_env_refs**: main-agent-authored envComments at [L459](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/SESSIONS_LOGS/main-session.jsonl) 19:28:00Z referenced other-env identifiers (cross-env-ref violation). Accepted by main agent as "design tradeoff" at [close/editorial-review attestation L557](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/SESSIONS_LOGS/main-session.jsonl) — tension between check and design intent. |
| 17 | 2026-04-22T19:29:17.730Z | finalize/ | `3 — Stage_import_comment_ratio` | **Finalize comment-ratio**: env 3 envComments too terse relative to yaml size. Main agent re-ran finalize with expanded env 3 comments per [L473 19:29:39Z](../../../../zcprecipator/nestjs-showcase/nestjs-showcase-v39/SESSIONS_LOGS/main-session.jsonl). |

## Final verification

- [x] All cells are non-`pending`
- [x] Every Read-receipt timestamp is after analyst session start (20:06Z)
- [x] No `unmeasurable-invalid` cells apart from close-substep cells flagged per §Phase-reached (close never completed → close-browser-walk cell `unmeasurable-invalid` justified by action=skip bypass, NOT by file-absence)
- [x] Machine-report SHA matches file content (committed at 32de5de)
- [x] Checklist SHA — computed at verdict-commit time

**Analyst sign-off**: Claude Opus 4.7 (1M context) on behalf of Ales Rechtorik, 2026-04-22T20:45Z
