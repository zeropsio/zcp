# knowledge-matrix-showcase.md

**Purpose**: enumerate, per (phase × substep × agent), every knowledge source actually available to the agent at that point in the showcase-tier recipe run. Evidence: file:line on source, or trace timestamp on v34 session log.

**Evidence base**:
- Showcase v34 run — [`../01-flow/flow-showcase-v34-main.md`](../01-flow/flow-showcase-v34-main.md) + 6 sub-agent traces + 6 dispatches
- Source: [`internal/workflow/recipe_topic_registry.go`](../../../internal/workflow/recipe_topic_registry.go), [`recipe_guidance.go`](../../../internal/workflow/recipe_guidance.go), [`recipe_brief_facts.go`](../../../internal/workflow/recipe_brief_facts.go), [`recipe_substeps.go`](../../../internal/workflow/recipe_substeps.go)
- Check surface: [`internal/tools/workflow_checks_*.go`](../../../internal/tools/)
- Block index: recipe.md line ranges — see also §5 of [Section 5 of the topic-registry summary in this dir]

---

## Matrix legend — knowledge-source columns

| Column | What it means | Evidence cite shape |
|---|---|---|
| `tool.permit` | Tool schema — tools the agent may call (MCP allowlist or brief declaration) | server-side (main) or dispatch-file line (sub) |
| `tool.forbid` | Forbidden tools (MCP denylist or brief declaration) | same |
| `eager.inlined` | Topic bodies auto-prepended into the substep's step-entry `detailedGuide` (EagerAt-matched topic(s)) | `recipe_topic_registry.go` + block line range |
| `scoped.body` | The substep's own `detailedGuide` topic body (substep-scoped per subStepToTopic) | `recipe_guidance.go:subStepToTopic` |
| `facts.read` | Whether the agent reads the facts log (via `BuildPriorDiscoveriesBlock` injection or direct `/tmp/zcp-facts-…` read) | `recipe_brief_facts.go` |
| `manifest` | Whether `zerops_workspace_manifest` is available / called | trace search |
| `prior.return` | What the main agent carried forward from a previous sub-agent return (for dispatch composition) | dispatch-file references |
| `plan.fields` | Which `plan.Research` / `plan.Tier` / `plan.Features` fields were interpolated into the substep guide or dispatch | source interpolation points |
| `env.catalog` | Env vars delivered (either via `zerops_discover includeEnvs=true` or service-env inspection) | discovery trace |
| `deploy.fail` | If the substep is a retry, what failure metadata was returned | check-result trace |
| `knowledge.guide` | On-demand `zerops_knowledge` calls at this substep | trace calls within window |

Cell notation:
- `—` = not delivered / not present
- `(on-demand)` = topic exists but not eager at this substep; fired via on-demand `zerops_guidance` call
- Sizes are bytes of result payload (trace evidence) or line count × ~50 chars (source evidence)

---

## 1. Main agent — showcase v34

**Run-level invariants** (apply to all main-agent rows):
- MCP permit: all `mcp__zerops__*` + `Read/Edit/Write/Grep/Glob/Bash/TodoWrite/Agent/ToolSearch/ScheduleWakeup` — standard main-agent profile
- MCP forbid: none declared
- facts log: `/tmp/zcp-facts-4856bb30df43b2b1.jsonl` (session-local) — readable but **main does not invoke `BuildPriorDiscoveriesBlock`** — that's a substep-scoped injection into sub-agent briefs only
- `zerops_workspace_manifest`: available but **not called in v34 main trace** (grep-checked)

### 1.1 Research phase

| Substep | tool.permit | tool.forbid | eager.inlined | scoped.body | plan.fields | env.catalog | knowledge.guide (on-demand) | Evidence |
|---|---|---|---|---|---|---|---|---|
| research (entry) | main profile | — | — | `research-showcase` + `research-minimal` concatenated (~17 KB) — [recipe_guidance.go:L100-130](../../../internal/workflow/recipe_guidance.go) | intent text + research outputs | — | `zerops_knowledge recipe=nestjs-minimal` (7378 B @ 10:18:11) — [main:L42](../01-flow/flow-showcase-v34-main.md) | v34 main trace event #5 (`action=start workflow=recipe`) returned 16513 B; event #6 is on-demand knowledge |

### 1.2 Provision phase

| Substep | tool.permit | tool.forbid | eager.inlined | scoped.body | plan.fields | env.catalog | knowledge.guide (on-demand) | Evidence |
|---|---|---|---|---|---|---|---|---|
| provision (entry) | main profile | — | — | `provision-framing` (recipe.md:159-163) + `import-yaml-standard-mode` (165-194) + `import-yaml-dual-runtime` (233-257) + `import-services-step` (294-304) + `mount-dev-filesystem` (306-317) + `git-config-mount` (319-345) + `git-init-per-codebase` (347-351) + `env-var-discovery` (353-375) + `provision-attestation` (377-384) — ~22.8 KB | `plan.Research.*` (project name, services list) | delivered later via `zerops_discover` @ 10:20:06 (5381 B result including envs) | `zerops_guidance` × 4: `dual-runtime-urls` (8646 B), `worker-setup` (897 B), `zerops-yaml-rules` (6485 B), `comment-anti-patterns` (650 B) — all before scaffold dispatch | main trace events #7–#25 |

Flag: v34 main fired **4 on-demand `zerops_guidance` calls during provision window** before dispatching scaffolds. All four topics are declared as `on-demand` (no `EagerAt` declaration) in the topic registry → the agent discovered the need for them independent of eager injection.

### 1.3 Generate phase substeps

| Substep | tool.permit | tool.forbid | eager.inlined | scoped.body | plan.fields | env.catalog | deploy.fail | knowledge.guide | Evidence |
|---|---|---|---|---|---|---|---|---|---|
| generate (entry) | main profile | — | — | 41942 B (from `complete step=provision` return, includes subStepToTopic for first generate substep) | plan.Research.* | already known from discover | — | — | main event #19 returned 46358 B (guidance 41942 B) |
| generate.scaffold | main profile | — | **`dev-server-host-check`** (recipe.md:711-715, 5 lines when `hasBundlerDevServer`) + **`scaffold-subagent-brief`** (recipe.md:790-1125, 336 lines when `isShowcase && multiCodebase`) | `where-to-write-files-multi` (recipe.md:422-444, 22 lines, showcase path) | `plan.Research.PrimaryTarget`, `Hostnames`, `SharesCodebaseWith` | discover-delivered env names | — | — | topic-registry EagerAt=SubStepScaffold; subStepToTopic branch |
| generate.app-code | main profile | — | — | **`dashboard-skeleton`** (recipe.md:762-788, 27 lines — showcase branch) | `plan.Research.Features` (6 features) | — | — | — | [recipe_guidance.go:L547-550](../../../internal/workflow/recipe_guidance.go) |
| generate.smoke-test | main profile | — | — | `on-container-smoke-test` (recipe.md:1263-1299, 37 lines) | — | discover-delivered | — | — | — |
| generate.zerops-yaml | main profile | — | — | `zerops-yaml-rules` composite (recipe.md:507-528 + 672-688 + 717-725 + 738-744 + 495-505) ~155 lines | `plan.Research.Features` (informs setup rules) | — | — | — | — |

Flag: v34 main did NOT emit further `zerops_guidance` calls during the entire generate phase — the eager injection + scoped.body covered the need for three scaffold dispatches (10:23:14 / 10:24:38 / 10:25:27).

### 1.4 Deploy phase substeps (showcase = 12 substeps)

For every substep below, `tool.permit`/`tool.forbid` = main profile (unchanged); omitted for brevity.

| Substep | eager.inlined | scoped.body | facts.read (main) | plan.fields | env.catalog | deploy.fail | knowledge.guide | Evidence |
|---|---|---|---|---|---|---|---|---|
| deploy (entry) | — | step-entry (from complete-generate return) 13843 B | — | plan.Research.Targets | — | — | — | main event #40 returned 17244 B (guidance 13843 B) |
| deploy-dev | **`fact-recording-mandatory`** (1423-1466, 44 lines) + **`where-commands-run`** (1830-1872, 43 lines) | `deploy-core-universal` + `deploy-framing` composite (1417-1579, 92 lines) → delivered as `deploy-flow` topic | not read at deploy-dev | plan.Research.Targets | env-from-service subst ready | no fail on first round v34 | — | topic-registry EagerAt=SubStepDeployDev; main event #44 delivered 8980 B |
| start-processes | (inherits deploy-dev eager — topics delivered once per phase-entry) | `deploy-flow` substring 1632 B (event #56) | — | — | **inspected directly via Bash (ssh workerdev env)** — event #60 | — | — | v34 `DB_PASS/DB_PASSWORD` investigation @ 10:42:16–10:42:45 |
| verify-dev | — | `deploy-target-verification` (1652-1673, 22 lines) 8978 B | — | plan.Research.Targets | — | — | — | main event #70 |
| init-commands | — | `deploy-flow` 21840 B — **NOTE: showcase carries `dev-deploy-subagent-brief` (1675-1828, 154 lines) as the substep-return payload** ([recipe_guidance.go:L556-563](../../../internal/workflow/recipe_guidance.go)) | — | plan.Research.Features → stringified into brief | — | — | — | main event #71 (complete init-commands) → 24193 B; next action is Agent dispatch at 10:46:08 (prompt_len=14816) |
| subagent | — (NB: agent eager declares `IncludePriorDiscoveries=true` per topic registry) | same `dev-deploy-subagent-brief` topic echoed | — (main doesn't read facts log; **sub-agent's** brief is seeded with Prior Discoveries via `BuildPriorDiscoveriesBlock`) | plan.Features + prior discoveries | — | — | — | main event #100 returned 10980 B (guidance 8977 B) |
| snapshot-dev | — | `deploy-flow` 3328 B | — | — | — | — | — | main event #107 returned 5240 B |
| feature-sweep-dev | — | `feature-sweep-dev` (1874-1916, 43 lines) 10076 B | — | plan.Research.Features | — | — | — | main event #108 |
| browser-walk | — | `dev-deploy-browser-walk` (1918-2024, 107 lines) 4997 B | — | — | — | — | — | main event #120 (6911 B) |
| cross-deploy | — | `stage-deployment-flow` (2057-2125, 69 lines) 1635 B | — | plan.Research.Targets | — | — | — | main event #128 |
| verify-stage | — | `deploy-target-verification` 2460 B | — | — | — | — | — | main event #130 |
| feature-sweep-stage | — | `feature-sweep-stage` (2149-2185, 37 lines) 25373 B | — | plan.Research.Features | — | — | — | main event #135 returned 27927 B — **disproportionately large** (next substep's readmes topic is carried at return, adds ~15 KB) |
| readmes | — (substep-scoped topic with `IncludePriorDiscoveries=true`) | **`content-authoring-brief`** (recipe.md:2390-2736, 347 lines, v8.94 fresh-context) — substep return at complete-feature-sweep-stage | Sub-agent reads facts log through injected PriorDiscoveriesBlock; **main also receives manifest contract via fresh-context brief** | plan.Research.Features + facts log entries | — | — | writer sub-agent makes 6 `zerops_knowledge` calls (topic: init-commands, rolling-deploys, object-storage, http-support, deploy-files, readiness-health-checks) | writer dispatch @ 11:04:40 prompt_len=11346 |

Flag: Deploy-phase check failure retries (v34 had **4 deploy convergence rounds**) re-run the readmes substep gate with check metadata. Rows #17–#20 in the main trace (timestamps 11:12:23 / 11:14:10 / 11:14:56 / 11:15:26 — step=`deploy` re-completes with decreasing guidance sizes) are the fix rounds. Each carries enriched `Detail/ReadSurface/Required/Actual/HowToFix/CoupledWith/PerturbsChecks` check metadata (v8.96+v8.104 fields).

### 1.5 Finalize phase

| Substep | eager.inlined | scoped.body | plan.fields | knowledge.guide | Evidence |
|---|---|---|---|---|---|
| finalize (entry) | — | `generate-finalize` (~14 KB from topic registry) | plan.Research.Targets + env import.yaml target list | — | main event #145 returned 19152 B |
| (finalize has no substeps — per [recipe_substeps.go:L139-150](../../../internal/workflow/recipe_substeps.go)) | — | — | — | — | — |

v34 main fires `complete step=finalize` **3 times** (11:17:52 / 11:19:03 / 11:19:31 — 19152 / 14102 / 12594 B returned). The 3-round finalize convergence class — check-failure metadata re-delivered each round.

### 1.6 Close phase (showcase = 2 substeps)

| Substep | eager.inlined | scoped.body | facts.read | plan.fields | knowledge.guide | Evidence |
|---|---|---|---|---|---|---|
| close (entry) | — | step-entry from complete-finalize return (~2 KB when close-entry narrative) | — | — | — | — |
| close.code-review | — (topic declares `IncludePriorDiscoveries=true` — sub-agent brief gets prior-discoveries prepend) | `code-review-subagent` (recipe.md:3050-3158, 109 lines) 2183 B return | Sub-agent reads facts log through injected block | `plan.Research.Features`, `plan.Targets` (single-codebase interpolation collapses to one) | — | code-review dispatch @ 11:20:21 prompt_len=6256 |
| close.close-browser-walk | — | `close-browser-walk` (recipe.md:3160-3191, 32 lines) 2240 B | — | — | — | main event #162 |

---

## 2. Sub-agents — showcase v34

Each sub-agent's tool permit/forbid is declared in the dispatched brief (the Agent-tool `prompt` parameter), not by the MCP server — agents can attempt any tool but the brief tells them the contract. Server-level block: `zerops_workflow` enforces `SUBAGENT_MISUSE` when a sub-agent attempts it.

Conventions used below:
- `tool.permit` = "SSHFS mount tools + MCP subset declared in brief"
- `tool.forbid` = verbatim list declared in brief

### 2.1 scaffold-apidev (showcase only) — dispatched 10:24:38 / 15627-char brief

| Knowledge source | Content |
|---|---|
| `tool.permit` | Read/Edit/Write/Grep/Glob on `/var/www/apidev/`; Bash via `ssh apidev "…"`; `zerops_dev_server`, `zerops_knowledge`, `zerops_logs`, `zerops_discover` — dispatch lines 20–22 |
| `tool.forbid` | `zerops_workflow`, `zerops_import`, `zerops_env`, `zerops_deploy`, `zerops_subdomain`, `zerops_mount`, `zerops_verify` — dispatch lines 22–28 |
| `eager.inlined` (received from main's composition) | `scaffold-subagent-brief` block (recipe.md:790-1125) carries principles + SSH-only + file-op sequencing + graceful shutdown + 0.0.0.0 bind + trust-proxy + S3 forcePathStyle + NATS separate user/pass |
| `scoped.body` | — (sub-agents don't get substep-scoped guides; they get the whole brief) |
| `facts.read` | **Prior Discoveries block** injected — 7 entries (meilisearch version pin, Valkey no-auth, NATS URL-embed, forcePathStyle, trust-proxy, graceful shutdown, 0.0.0.0 bind) — dispatch top of file |
| `manifest` | — (not called) |
| `prior.return` | Receives plan fields pre-resolved; doesn't wait for other sub-agents |
| `plan.fields` | primary=API, hostname=apidev, features=[health,status,migrate,seed], managed=[db,redis,queue,storage,search] |
| `env.catalog` | Not in brief; discovered via `zerops_discover` or `ssh apidev env` |
| `knowledge.guide` | 0 calls during sub-agent execution |
| `record_fact` calls | **7** (all scope=both/content): graceful shutdown, 0.0.0.0 bind, trust proxy, NATS separate options, forcePathStyle; +2 downstream (meilisearch @0.57 CJS, Valkey) |
| Return size to main | ~2500 chars (files written list + smoke results + record_fact summaries) |
| Tool errors | 3 — .gitignore read-before-edit miss, pre-ship regex false positive, Write-no-Read on /tmp |

Evidence: [`../01-flow/flow-showcase-v34-sub-scaffold-apidev.md`](../01-flow/flow-showcase-v34-sub-scaffold-apidev.md), [`../01-flow/flow-showcase-v34-dispatches/scaffold-apidev.md`](../01-flow/flow-showcase-v34-dispatches/).

### 2.2 scaffold-appdev — dispatched 10:23:14 / 10459-char brief

| Knowledge source | Content |
|---|---|
| `tool.permit` | Read/Edit/Write/Grep/Glob on `/var/www/appdev/`; Bash via `ssh appdev`; `zerops_dev_server`, `zerops_knowledge`, `zerops_logs`, `zerops_discover` |
| `tool.forbid` | `zerops_workflow`, `zerops_import`, `zerops_env`, `zerops_deploy`, `zerops_subdomain`, `zerops_mount`, `zerops_verify` |
| `eager.inlined` | `scaffold-subagent-brief` (same source block, codebase-customized) — 0.0.0.0 binding, Vite allowedHosts, VITE_API_URL, Svelte 5 runes |
| `facts.read` | Prior Discoveries (1 entry — Svelte+Vite peer-dep conflict, scope=downstream) |
| `manifest` | — |
| `plan.fields` | primary=frontend, hostname=appdev, features=[health-dashboard StatusPanel] |
| `knowledge.guide` | 0 |
| `record_fact` calls | 1 (scope=content): Vite 0.0.0.0 |
| Return size | ~1500 chars |
| Tool errors | 0 |

### 2.3 scaffold-workerdev — dispatched 10:25:27 / 8668-char brief

| Knowledge source | Content |
|---|---|
| `tool.permit` / `tool.forbid` | identical to appdev/apidev — dispatch lines 20-30 |
| `eager.inlined` | `scaffold-subagent-brief` — standalone (no HTTP) + NATS queue group + graceful shutdown drain + separate user/pass |
| `facts.read` | Prior Discoveries (1 entry — NATS queue group, scope=both) |
| `manifest` | — |
| `plan.fields` | primary=worker, hostname=workerdev, features=[subscribe jobs.scaffold] |
| `knowledge.guide` | 0 |
| `record_fact` calls | 3 (scope=both): NATS queue group, graceful shutdown drain, NATS separate user/pass |
| Return size | ~1200 chars |
| Tool errors | 1 (pre-ship regex, self-fixed) |

### 2.4 feature sub-agent — dispatched 10:46:08 / 14816-char brief

| Knowledge source | Content |
|---|---|
| `tool.permit` | Read/Edit/Write/Grep/Glob on 3 mounts; Bash via `ssh {hostname}`; `zerops_dev_server`, `zerops_logs`, `zerops_knowledge`, `zerops_discover`, `zerops_record_fact` — dispatch lines 35-36 |
| `tool.forbid` | `zerops_workflow`, `zerops_import`, `zerops_env`, `zerops_deploy`, `zerops_subdomain`, `zerops_mount`, `zerops_verify` |
| `eager.inlined` | `dev-deploy-subagent-brief` block (recipe.md:1675-1828) — scaffold-state assumptions + contract discipline + UX quality + diagnostic-probe cadence (max 3/ambiguity, max 5 bash/min per v8.104 Fix D) |
| `facts.read` | Prior Discoveries (3 entries: @types/multer, nodemailer jsonTransport, Meilisearch v0.49 response shape, Svelte 5 rune — all scope=downstream) |
| `manifest` | — (feature agent doesn't call workspace_manifest) |
| `plan.fields` | 6 features with surface [api/ui/worker/db/cache/storage/search/queue] boundaries; DB_HOST-style env naming; SMTP jsonTransport fallback |
| `env.catalog` | Same as main — accessible via `ssh {hostname} env` |
| `knowledge.guide` | 0 in-agent calls |
| `record_fact` calls | 9 (mix content / both / downstream): NATS subject+queue (cross_codebase_contract), DTO duplication, nodemailer jsonTransport, Meilisearch response shape, @types/multer, Svelte runes |
| Return size | ~4000 chars |
| Tool errors | 3 (Read-on-dir, ls on missing path, Grep on missing path — all self-corrected) |

### 2.5 writer sub-agent (content-authoring-brief) — dispatched 11:04:40 / 11346-char brief

| Knowledge source | Content |
|---|---|
| `tool.permit` | Read/Edit/Write/Grep/Glob on mounts; Bash via `ssh {hostname}`; `zerops_knowledge` — dispatch lines 31-33 |
| `tool.forbid` | `zerops_workflow`, `zerops_import`, `zerops_env`, `zerops_deploy`, `zerops_subdomain`, `zerops_mount`, `zerops_verify` |
| `eager.inlined` | `content-authoring-brief` block (recipe.md:2390-2736) — canonical output tree + no fabrication + citation map + fact classification taxonomy + reader perspective for 3 surfaces (IG fragment, KB fragment, CLAUDE.md) |
| `facts.read` | **Fresh-context writer** — reads facts log directly at `/tmp/zcp-facts-4856bb30df43b2b1.jsonl` (path interpolated into brief). No Prior Discoveries prepend because the brief IS the prior discoveries consumer |
| `manifest` | Writes `ZCP_CONTENT_MANIFEST.json` at project root — not `workspace_manifest`, a separate content-manifest artifact |
| `plan.fields` | 3 codebases, facts scope routing rules, session fact log path |
| `env.catalog` | — |
| `knowledge.guide` | **6 calls** — `zerops_knowledge recipe=?` on topics: init-commands, rolling-deploys, object-storage, http-support, deploy-files, readiness-health-checks (citation-map consultation before writing) |
| `record_fact` calls | 0 (writer is a consumer, not producer) |
| Return size | Byte counts of 6 output files + manifest (apidev README 14054, CLAUDE.md 6246; appdev 6520/4942; workerdev 7965/5602; manifest 3142) |
| Tool errors | 0 |

### 2.6 code-review sub-agent — dispatched 11:20:21 / 6256-char brief

| Knowledge source | Content |
|---|---|
| `tool.permit` | Read/Edit/Write/Grep/Glob on 3 mounts; Bash via `ssh {hostname}`; no zerops tools |
| `tool.forbid` | `zerops_workflow`, `zerops_import`, `zerops_env`, `zerops_deploy`, `zerops_subdomain`, `zerops_mount`, `zerops_verify`, `zerops_browser`, `agent-browser` |
| `eager.inlined` | `code-review-subagent` block (recipe.md:3050-3158) — framework-expert scope, silent-swallow antipattern scan, feature-coverage scan, data-feature attribute orphan detection |
| `facts.read` | **Prior Discoveries block** — but no facts produced by writer (writer doesn't record facts), so block is populated by scaffold+feature subs' earlier records |
| `manifest` | — |
| `plan.fields` | 6 features with surface contracts; UITestID attributes; worker subject+queue validation |
| `knowledge.guide` | 0 |
| `record_fact` calls | 0 (reports issues as [CRIT]/[WRONG]/[STYLE], applies fixes inline) |
| Return size | Issue summary with counts |
| Tool errors | 0 |

---

## 3. Eager topic injection — summary (showcase)

| Topic | Eager at substep | Block (recipe.md lines) | Target agent | Size |
|---|---|---|---|---|
| `dev-server-host-check` | SubStepScaffold | 711-715 (5 lines) | main agent's generate.scaffold entry | small — fires only when `hasBundlerDevServer` |
| `scaffold-subagent-brief` | SubStepScaffold | 790-1125 (336 lines) | main agent's generate.scaffold entry (before 3 parallel dispatches) | ~16 KB |
| `fact-recording-mandatory` | SubStepDeployDev | 1423-1466 (44 lines) | main agent's deploy.deploy-dev entry | ~2 KB |
| `where-commands-run` | SubStepDeployDev | 1830-1872 (43 lines) | main agent's deploy.deploy-dev entry | ~2 KB |

NB: NO topic is eagerly injected at subagent / readmes / code-review substeps (v8.90 de-eager). Those topics are delivered as the substep's scoped.body at the *previous* complete call (de-eagered to prevent the v25 backfill class).

---

## 4. Substep-scoped topic injection — summary (showcase)

Every substep returns a `scoped.body` via `subStepToTopic()` at `action=complete step=X substep=Y`. Key showcase-specific scoped bodies:

| Substep | Scoped topic | Block lines | IncludePriorDiscoveries? | Notes |
|---|---|---|---|---|
| generate.scaffold | `where-to-write-files-multi` | 422-444 (22 lines) | false | showcase path |
| generate.app-code | `dashboard-skeleton` | 762-788 (27 lines) | false | showcase-only block |
| deploy.init-commands return | `dev-deploy-subagent-brief` | 1675-1828 (154 lines) | false (but brief references facts log) | carried at **return** of init-commands to seed the next dispatch |
| deploy.subagent | echoes subagent-brief | same | **true** — BuildPriorDiscoveriesBlock prepended | [recipe_brief_facts.go:L204-L219](../../../internal/workflow/recipe_brief_facts.go) substep order index 4 |
| deploy.feature-sweep-stage return | `content-authoring-brief` | 2390-2736 (347 lines) | **true** | largest substep return in v34 (27927 B @ 11:03:16) |
| deploy.readmes | echoes content-authoring-brief | same | true | substep order index 11 |
| close.code-review | `code-review-subagent` | 3050-3158 (109 lines) | **true** | substep order index 12 |
| close.close-browser-walk | `close-browser-walk` | 3160-3191 (32 lines) | false | — |

---

## 5. Check surface per substep (showcase)

Each substep's `complete` returns a `StepCheckResult` built by `buildRecipeStepChecker()` ([`workflow_checks_recipe.go:L37-53`](../../../internal/tools/workflow_checks_recipe.go)). Checks carry metadata fields: `Name, Status, Detail, ReadSurface, Required, Actual, CoupledWith, HowToFix, PerturbsChecks` (v8.96 + v8.97 + v8.104).

| Phase | Substep | Check count (showcase, rough) | Key checks | Runnable-locally? |
|---|---|---:|---|---|
| generate | (all 4 substeps checked at `complete step=generate`) | 9 per target × 3 targets + 1 cross = **~28** | `hostname_zerops_yml_exists`, `hostname_setup`, `hostname_prod_setup`, `dev_prod_env_divergence`, `hostname_worker_setup`, `hostname_no_premature_readme`, `hostname_scaffold_artifact_leak`, `hostname_env_self_shadow`, `zerops_yml_schema_fields` | mostly yes (grep/jq/yq); scaffold_artifact_leak = partial |
| deploy | readmes (per-codebase) | ~25 per codebase × 3 = **~75** | fragment markers (×3) + blank_after_marker + heading_level + integration_guide_yaml + `comment_ratio` + `comment_specificity` + `section_heading_comments` + `integration_guide_code_adjustment` + `integration_guide_per_item_code` + `knowledge_base_gotchas` + `no_placeholders` + `intro_length` + `intro_no_titles` + `knowledge_base_exceeds_predecessor` + `knowledge_base_authenticity` + `hostname_gotcha_distinct_from_guide` + `hostname_claude_md_exists` | yes (markdown scanning, regex) except authenticity / predecessor-floor (partial — needs predecessor data) |
| deploy | readmes (cross-codebase) | 1 | `cross_readme_gotcha_uniqueness` | partial |
| deploy | readmes (worker separate-codebase) | 4 | `hostname_worker_queue_group_gotcha`, `hostname_worker_shutdown_gotcha`, `hostname_worker_production_correctness`, `hostname_drain_code_block` | partial (token scan heuristics) |
| deploy | readmes (content manifest) | 5 | `writer_content_manifest_exists/_valid`, `writer_discard_classification_consistency`, **`writer_manifest_honesty`**, **`writer_manifest_completeness`** | partial (JSON schema + Jaccard-similarity check) |
| finalize | (no substep) | ~30 per env × 6 envs = **~180** file + structural + comment-ratio + factual_claims + cross_env_refs + preprocessor + ha/mode/corePackage | mostly yes |

---

## 6. Coverage audit — every cell sourced

- Main-agent rows 1.1–1.6: each substep cell has either recipe.md block evidence (line range) or v34 trace evidence (event #).
- Sub-agent rows 2.1–2.6: each cell has dispatch file or sub-agent trace file evidence; zero cells read "probably".
- Eager injection table (§3) sources `recipe_topic_registry.go` EagerAt declarations.
- Scoped injection table (§4) sources `recipe_guidance.go subStepToTopic` branches.
- Check surface (§5) sources `workflow_checks_*.go` files.

**Coverage gaps** (spec-derived minimal inputs excluded — see `knowledge-matrix-minimal.md`):
- Main-agent **`zerops_workspace_manifest`** availability is **not cell-evidenced** — the tool is registered server-side but no v34 trace invocation. Marked `—` throughout.
- `deploy.init-commands` → subagent return payload: treated as substep-scoped topic delivery but the `v8.90 de-eager` mechanism blurs which substep "owns" the 21840 B. Cited two locations ([recipe_guidance.go:L556-563](../../../internal/workflow/recipe_guidance.go) + trace event #71).
- Feature sub-agent's `prior.return` from scaffold sub-agents: the feature brief doesn't explicitly receive scaffold agents' return summaries; it inherits them indirectly via the facts log's `scope=downstream` records. Marked "via facts log".
