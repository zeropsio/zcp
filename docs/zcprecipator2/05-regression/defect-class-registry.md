# defect-class-registry.md — v20–v34 closed-defect inventory

**Purpose**: codify every defect class named in [`../../recipe-version-log.md`](../../recipe-version-log.md) with a ✅/❌ verdict, a named mechanism-layer fix (v8.XX), or a still-open carry-forward — into testable scenarios that the new zcprecipator2 architecture must still prevent. Per [README.md §3 step 5](../README.md) + [RUNBOOK.md §6](../RUNBOOK.md): one row per class, fields `id / origin_run / class / mechanism_closed_in / current_enforcement / new_enforcement / test_scenario / calibration_bar`.

**Scope**: primary focus v20–v34 (per README §3 step 5 "conservative registry" directive — every defect class v20–v34 named as closed gets a row, even if the fix held cleanly). Selected v6–v19 entries included where the class either (a) recurred in v20+, (b) seeded a lineage still relevant under the new principles, or (c) was referenced as an origin anchor by a v8.78+ mechanism.

**Citation convention**:
- `v25 §Defect 1` = `recipe-version-log.md` per-version entry for v25, its "Defect 1" paragraph
- `v8.96 Theme A` = mechanism-layer release notes inside `recipe-version-log.md`
- `milestones L374` = the cross-version milestones table one-line entry at that line
- Principles P1–P8: see [`../03-architecture/principles.md`](../03-architecture/principles.md)
- Atoms: paths under `internal/content/workflows/recipe/` per [`../03-architecture/atomic-layout.md`](../03-architecture/atomic-layout.md)

**Legend for `mechanism_closed_in`**:
- `v8.XX` = shipped in that release
- `(carry)` = identified but deliberately deferred / unshipped as of v34
- `(rolled back)` = check/gate existed then was reverted to v20 substrate via `rollback-to-v20-substrate` branch (v24 interlude)

Target row count: ≥30 (README §3 step 5 minimum). Actual: **68**.

---

## 1. Registry — chronological by `origin_run`

### v6–v19 (selected; seeds lineages still live in v20–v34)

#### 1.1. `v6-missing-workerdev-readme`
- **origin_run**: v6 (2026-04-10)
- **class**: workerdev README never written — incomplete deliverable, workflow didn't ensure all three codebases authored their README
- **mechanism_closed_in**: v11 `readme_fragments` byte-count check; hardened by v8.94 `writer_content_manifest_exists`/`_valid`
- **current_enforcement**: `{hostname}_readme_exists` at `deploy.readmes` complete + `writer_content_manifest_exists` check
- **new_enforcement**: P7 (cold-read coverage: every role × tier composed brief is verified against the defect registry) + atom `phases/deploy/readmes-substep.md` declaring per-codebase README is REQUIRED
- **test_scenario**: given a 3-codebase recipe run, when `deploy.readmes` substep attests complete, then `find {apidev,appdev,workerdev}/README.md -type f` returns 3 files AND each file is ≥ 100 lines
- **calibration_bar**: `ls {host}/README.md 2>/dev/null | wc -l == 3`; per-file `wc -l {host}/README.md` ≥ 100

#### 1.2. `v10-empty-fragment-markers`
- **origin_run**: v10 (2026-04-12, `C` content catastrophe — apidev+workerdev gotcha sections EMPTY, IG items 0/2/0)
- **class**: README scaffolds emitted with empty `#ZEROPS_EXTRACT_START:knowledge-base` / `integration-guide` fragments
- **mechanism_closed_in**: v11 `readme_fragments` byte-count + `knowledge_base_gotchas` checks
- **current_enforcement**: `fragment_{intro,integration-guide,knowledge-base}` presence + `knowledge_base_gotchas` non-empty bullet list
- **new_enforcement**: P1 runnable pre-attest — author runs `awk '/knowledge-base/,/END/{if(/^- \*\*/)c++} END{exit !c}' {host}/README.md` before attesting
- **test_scenario**: given a README draft with an empty knowledge-base fragment (between START and END markers, zero `- **` bullets), when author runs the pre-attest shell, then exit non-zero
- **calibration_bar**: every `{host}/README.md` contains ≥3 `^- \*\*` bullets inside the knowledge-base fragment, ≥1 `### N.` heading inside integration-guide fragment

#### 1.3. `v11-dev-server-ssh-hangs`
- **origin_run**: v11 (8-hour wall clock from dev-server SSH-channel-hold pattern; longest single bash 358.8s holding SSH channel)
- **class**: hand-rolled `CMD &` + bash timeout + kill pattern hung indefinitely; substrate-level
- **mechanism_closed_in**: v17 `zerops_dev_server` MCP tool + v17.1 spawn-shape fix (setsid + ack marker, bounded phases)
- **current_enforcement**: `zerops_dev_server` tool implementation (substrate — out of scope per README §1 "stays as-is")
- **new_enforcement**: substrate stays; atom `principles/where-commands-run.md` positively declares `zerops_dev_server` as the only start mechanism (P8)
- **test_scenario**: given a recipe run's main + feature subagent bash traces, then zero bash calls exceed 300s with `dev_server`/spawn-shape keywords
- **calibration_bar**: `0` very-long (≥120s) bash calls across main + all sub-agents; `0` "signal: killed" surfacing from `zerops_dev_server`

#### 1.4. `v15-predecessor-clone-gotchas`
- **origin_run**: v15 (apidev 3 gotchas cloned from workerdev — NATS credentials, SSHFS ownership, execOnce burn)
- **class**: decorative-content drift: gotchas verbatim-inherited from sibling-codebase or predecessor recipe without per-codebase anchoring
- **mechanism_closed_in**: v8.79 (rolled back) `knowledge_base_exceeds_predecessor` → replaced by `service_coverage` at v8.79; rollback made predecessor-floor informational; v8.94 fresh-context writer addresses by design
- **current_enforcement**: `service_coverage` (removed) / `cross_readme_gotcha_uniqueness` Jaccard 0.6 check + pre-classified fact taxonomy in writer brief
- **new_enforcement**: P5 (two-way graph) — every published gotcha has a fact source; writer brief positively declares "one codebase owns each fact; siblings cross-reference" (P8)
- **test_scenario**: given 3 per-codebase READMEs, when author runs `zcp check cross-readme-dedup`, then every pair of knowledge-base stems has Jaccard similarity < 0.6 between any two codebases
- **calibration_bar**: `cross_readme_gotcha_uniqueness` passes on round 1 of deploy-complete; `0` cross-codebase gotcha Jaccard ≥ 0.6

#### 1.5. `v15-ig-restates-gotchas`
- **origin_run**: v15 (3 appdev gotchas restating 3 adjacent IG items)
- **class**: gotcha text restates the integration-guide item that immediately precedes it
- **mechanism_closed_in**: v8.67 `gotcha_distinct_from_guide`; hardened v8.96 Theme A (ReadSurface on this check)
- **current_enforcement**: `{hostname}_gotcha_distinct_from_guide` + `{hostname}_knowledge_base_authenticity`
- **new_enforcement**: P1 runnable — author runs `zcp check kb-authenticity --hostname={h}` before attesting; brief positively declares "gotchas name a failure the IG prevents in a different frame" (P8)
- **test_scenario**: given README with IG item "Bind to 0.0.0.0 for Zerops L7 routing" and a gotcha "Forgetting 0.0.0.0 bind returns 502", when author runs the pre-attest check, then exit non-zero with gotcha-IG overlap named
- **calibration_bar**: `0` `gotcha_distinct_from_guide` fails on round 1 of deploy-complete across all codebases

#### 1.6. `v16-dbdriver-orm-as-database`
- **origin_run**: v16 (root README `"connected to typeorm"` because `plan.Research.DBDriver = "typeorm"` — an ORM library, not a managed service)
- **class**: plan-field type confusion — ORM library name rendered as managed-service name on root README intro
- **mechanism_closed_in**: v17 `validateDBDriver` at research-complete; survives rewrite per README §1
- **current_enforcement**: `validateDBDriver` in `internal/workflow/recipe_decisions.go` + `{hostname}_intro_no_titles` check
- **new_enforcement**: substrate stays; atom `phases/research/completion.md` positively declares managed-service vocabulary (PostgreSQL, Valkey, NATS, S3 — not ORM libraries) + P3 `SymbolContract` enumerates db-kind to brand mapping
- **test_scenario**: given a plan with `research.DBDriver = "typeorm"`, when `complete step=research` fires, then validation rejects with "typeorm is an ORM, not a database; choose PostgreSQL / MySQL / …"
- **calibration_bar**: root README intro names ≥1 Zerops brand from the managed-service allowlist; never contains `typeorm|prisma|sequelize|ioredis|keydb` as a "connected to" token

#### 1.7. `v16-preprocessor-dead-code-gated`
- **origin_run**: v16 (all 6 env import.yaml shipped missing `#zeropsPreprocessor=on` — finalize check dead-gated on `plan.Research.NeedsAppSecret` flag)
- **class**: check wired inside a plan-field conditional that evaluates false for NestJS recipes → check never fires
- **mechanism_closed_in**: v17 de-nesting — check fires unconditionally at finalize
- **current_enforcement**: `{prefix}_preprocessor` at finalize (unconditional)
- **new_enforcement**: P1 runnable — `head -5 env{i}/import.yaml | grep -E '#zeropsPreprocessor=on'` returns non-empty; positive-form authoring rule in `phases/finalize/env-comments.md`
- **test_scenario**: given env0-5/import.yaml with `APP_SECRET: <@generateRandomString(<32>)>`, when finalize check fires, then the `#zeropsPreprocessor=on` directive is present as the first comment line
- **calibration_bar**: `grep -l '#zeropsPreprocessor=on' env*/import.yaml | wc -l == 6`

#### 1.8. `v17-sshfs-write-not-exec`
- **origin_run**: v17 (all 3 scaffold sub-agents ran `cd /var/www/{hostname} && <exec>` zcp-side; v17 was the F-grade abort)
- **class**: SSHFS mount read as a write-surface (legitimate for files) conflated with execution-surface (must SSH into container)
- **mechanism_closed_in**: v17.1 SSH preamble in scaffold-subagent-brief; v8.80 `bash_guard` middleware rejects `cd /var/www/{host}` from main-agent side; v8.93.1 single-SSH-call git-config-mount; v8.97 Fix 5 SSH-only executables MANDATORY block
- **current_enforcement**: scaffold-subagent-brief `⚠ CRITICAL: where commands run` preamble + `bash_guard` runtime rejection + MANDATORY sentinel in v8.97 Fix 3
- **new_enforcement**: atom `principles/where-commands-run.md` with positive allow-list "commands execute via `ssh {hostname} \"…\"` only"; atom is referenced from every scaffold/feature/writer brief (P2 leaf artifact); P8 positive form (no enumerated prohibitions)
- **test_scenario**: given a scaffold sub-agent's bash trace, when any bash call is inspected, then zero calls match `^cd /var/www/[a-z]+ && (npm|npx|nest|node|vite|php|composer|bundle|mix|rails|python|go) `
- **calibration_bar**: `0` zcp-side-exec bash calls across main + all 3 scaffold sub-agents (grep the analyzer output)

#### 1.9. `v18-close-browser-silent`
- **origin_run**: v18 + v19 (close.browser never fired — prose in recipe.md but no gate)
- **class**: close-step completion not gated on browser walk
- **mechanism_closed_in**: v8.77 `SubStepCloseReview` + `SubStepCloseBrowserWalk` + `CompleteStep` gate extension
- **current_enforcement**: substep gate in `internal/tools/workflow.go` + showcase-only branching (`closeSubSteps()`)
- **new_enforcement**: substrate stays; atom `phases/close/browser-walk.md` positively declares the substep IS the plan (P4)
- **test_scenario**: given a showcase recipe at close, when `complete step=close` is attempted without `close-browser-walk` substep complete, then rejection with substep-ordering error
- **calibration_bar**: `zerops_browser` tool fires ≥ 1 time during close step for showcase tier; `zerops_workflow action=complete step=close` never accepted with `close-browser-walk` incomplete

#### 1.10. `v19-stale-major-import`
- **origin_run**: v19 (1 CRIT — `CacheModule` imported from `@nestjs/common/cache` NestJS-8 path in NestJS-10 project)
- **class**: agent writes framework import paths from training-data memory without verifying against installed package manifest
- **mechanism_closed_in**: v8.77 installed-package verification rule (framework-agnostic: verify against `node_modules/<pkg>/package.json`, `vendor/<pkg>/composer.json`, `go.mod`, etc.)
- **current_enforcement**: scaffold + feature brief text directive; no structural check
- **new_enforcement**: P3 `SymbolContract` includes framework-version fixed-token list; brief positively declares "verify every import path against the installed package's manifest before first commit" (P8)
- **test_scenario**: given an import statement `from '@nestjs/common/cache'`, when author greps the brief's installed-manifest verification step, then `jq '.dependencies["@nestjs/cache-manager"]' {host}/package.json` reveals the canonical import path
- **calibration_bar**: `0` close-step CRIT from stale-major-import class; every CRIT named by code-review must not be of the form "package X is imported from pre-version-N path"

#### 1.11. `v19-env4-mincontainers-two-axis-conflation`
- **origin_run**: v19 (env-4 app-comment silently dropped HA/rolling-deploy reason; initially flagged as "contradiction", user correction made two-axis explicit)
- **class**: `minContainers ≥ 2` has two independent axes (throughput + HA); comment teaches only one when both apply
- **mechanism_closed_in**: v8.77 two-axis teaching block in recipe.md env-comment-rules; core.md + model.md knowledge updates
- **current_enforcement**: writer brief `content-quality-overview` topic + env-comment authoring rules
- **new_enforcement**: atom `phases/finalize/env-comments.md` positively declares both axes for every runtime-service comment at env≥4 tier; P8 positive form
- **test_scenario**: given env4/import.yaml with `minContainers: 2` on a runtime service, when author runs `awk`/grep for both "throughput" and "HA" or "rolling" keywords in the adjacent comment, then both match
- **calibration_bar**: env4 + env5 import.yaml runtime-service comments contain both-axis teaching (grep `(throughput|HA|rolling)` on each of app/api/worker blocks returns ≥1 per block)

---

### v20 — load-bearing content reform origin

#### 2.1. `v20-generic-platform-leakage`
- **origin_run**: v20 (apidev `.env` gotcha — "`.env` file overrides Zerops-managed values" — factually wrong; runtime never reads `.env` unless app code does)
- **class**: gotcha passes `gotcha_causal_anchor` by naming a Zerops token (`envVariables`) but the claimed failure mechanism doesn't exist on the platform
- **mechanism_closed_in**: v8.79 `content_reality` + `gotcha_causal_anchor` (tightened); v8.94 fresh-context writer discard-class for self-inflicted + library-meta
- **current_enforcement**: `{hostname}_content_reality` + `{hostname}_gotcha_causal_anchor` + writer brief classification taxonomy
- **new_enforcement**: P1 runnable `zcp check kb-authenticity --hostname={h}` — token-match against platform-vocabulary list; P5 manifest must route fact with non-empty `routed_to`; P8 positive form "a gotcha names a Zerops mechanism AND a concrete failure (HTTP status / quoted error / observable symptom)"
- **test_scenario**: given a gotcha "`.env` files override Zerops env", when author runs the platform-vocabulary shim, then it fails with "claim is not a Zerops mechanism — the runtime does not read .env unless app code does"
- **calibration_bar**: `0` gotchas shipped whose failure-mechanism claim contradicts the `env-var-model` guide

#### 2.2. `v20-topology-drift`
- **origin_run**: v20 (appdev gotcha #1 — `_nginx.json` proxy_pass fix for architecture the recipe doesn't ship)
- **class**: gotcha ships a fix-snippet for a file/shape that doesn't exist in the deliverable
- **mechanism_closed_in**: v8.79 `content_reality` — declared symbols + file paths in gotchas must exist OR be framed advisory
- **current_enforcement**: `{hostname}_content_reality`
- **new_enforcement**: P1 runnable — author greps every gotcha's named file path against the mount tree; P8 positive rule in writer brief "each gotcha's file-path references must point at a file that exists in the shipped codebase, or the sentence must start 'If you add …'"
- **test_scenario**: given a gotcha naming `_nginx.json`, when author runs `find {host} -name _nginx.json` and it returns empty, then the gotcha is flagged as unanchored
- **calibration_bar**: `0` `{hostname}_content_reality` fails after writer subagent returns

#### 2.3. `v20-claude-vs-readme-contradiction`
- **origin_run**: v20 (apidev CLAUDE.md "Resetting Dev State" calls `ds.synchronize()` while README gotcha #7 forbids `synchronize: true` in production)
- **class**: CLAUDE.md operational procedure uses a mechanism README explicitly forbids, without dev-only qualifier
- **mechanism_closed_in**: v8.79 `claude_readme_consistency` (regex-based — dead at v21); v8.80 pattern-based rewrite
- **current_enforcement**: `{hostname}_claude_readme_consistency` (pattern-based, v8.80+)
- **new_enforcement**: P1 runnable shim + positive authoring rule in writer brief — "if CLAUDE.md invokes a mechanism README marks as forbidden-in-prod, it MUST carry an explicit `dev only — see README gotcha` cross-reference"
- **test_scenario**: given README gotcha forbidding `synchronize: true` in prod and CLAUDE.md step calling `ds.synchronize()`, when author runs `zcp check claude-readme-consistency`, then it fails unless the CLAUDE.md step carries a dev-only marker
- **calibration_bar**: `0` `claude_readme_consistency` fails after writer returns; shim output on mount passes on round 1

#### 2.4. `v20-declared-but-unimplemented`
- **origin_run**: v20 (workerdev gotcha #4 — watchdog with `setInterval` code, `lastActivity` + watchdog logic don't exist anywhere in src/)
- **class**: gotcha ships imperative prose + full code snippet for a feature that doesn't exist in the deliverable
- **mechanism_closed_in**: v8.79 `content_reality` — same check as 2.2
- **current_enforcement**: `{hostname}_content_reality`
- **new_enforcement**: same as 2.2; P8 positive form "either the symbol exists in src/ or the sentence is prefixed 'Consider adding …' / 'If your app needs …'"
- **test_scenario**: given a gotcha referencing symbol `lastActivity`, when author runs `grep -rn lastActivity {host}/src/`, then either the match is found OR the gotcha is framed advisory
- **calibration_bar**: `0` named-symbol-not-in-src occurrences across all knowledge-base fragments

#### 2.5. `v20-ig-leaning`
- **origin_run**: v20 (apidev IG #2 "Binding to 0.0.0.0" — 3 sentences + 2 lines of code; the why lives in IG #1's adjacent zerops.yaml comment)
- **class**: IG item is not standalone; requires the reader to have absorbed the preceding item
- **mechanism_closed_in**: v8.79 `{hostname}_ig_per_item_standalone` — each `### N.` IG block must ship ≥1 code block AND name a platform anchor in its first prose paragraph
- **current_enforcement**: `{hostname}_ig_per_item_standalone` + `integration_guide_per_item_code`
- **new_enforcement**: P1 runnable shim `zcp check ig-per-item-code --hostname={h}`; P8 positive form in writer brief
- **test_scenario**: given IG with `### 2. Binding to 0.0.0.0` and no code block in items 2/3/…, when shim runs, then fails naming which items miss code
- **calibration_bar**: each `### N.` block in every IG fragment carries ≥1 fenced code block AND ≥1 platform anchor token in its prose

#### 2.6. `v20-env-comment-templating`
- **origin_run**: v20 (env 4 service comments share "minContainers: 2 because rolling deploys" opening across app/api/worker)
- **class**: per-service env import.yaml lead-comment blocks share a templated opening — fails reader uniqueness
- **mechanism_closed_in**: v8.79 `{env}_service_comment_uniqueness` (Jaccard ≥ 0.6 between any two service comments within one env file)
- **current_enforcement**: finalize `service_comment_uniqueness` check
- **new_enforcement**: P1 runnable shim + positive form in env-comment atom ("each service's comment leads with service-specific failure mode, not shared template opener")
- **test_scenario**: given env4/import.yaml with 3 runtime-service comments starting with an identical sentence, when shim runs, then fails naming the overlapping pair
- **calibration_bar**: `0` `*_service_comment_uniqueness` fails

---

### v21 — v8.78 reform ships; operational + workflow regression

#### 3.1. `v21-scaffold-hygiene`
- **origin_run**: v21 (`208 MB node_modules + 748 KB dist + .DS_Store` committed to published tree; main's per-codebase brief synthesis dropped `.gitignore`/`.env.example` line for apidev+workerdev)
- **class**: scaffold sub-agent ships build/runtime-artifacts in the published deliverable because no `.gitignore` baseline was enforced
- **mechanism_closed_in**: v8.80 `{hostname}_scaffold_artifact_leak` check + scaffold-brief hygiene requirement
- **current_enforcement**: `{hostname}_scaffold_artifact_leak` at deploy-step
- **new_enforcement**: P3 `SymbolContract.fixRecurrenceRules` includes `.gitignore` baseline (`node_modules`, `dist`, `build`, `target`, `vendor/`, `.DS_Store`, `.env`); P1 runnable `find {host} -maxdepth 3 -name node_modules -o -name dist -o -name '.DS_Store' | grep -q . && exit 1`; P8 positive brief rule
- **test_scenario**: given a scaffold sub-agent that drops `.gitignore` for one codebase, when deploy-check fires, then `scaffold_artifact_leak` fails with the named offender
- **calibration_bar**: published tree total size ≤ 10 MB; `find {host} -maxdepth 3 -name node_modules | wc -l == 0` across all codebases; every `{host}/.gitignore` contains `node_modules`, `dist`, `.env`, `.DS_Store`

#### 3.2. `v21-claude-readme-consistency-dead-regex`
- **origin_run**: v21 (v8.79's `forbiddenPatternRe` regex matched only v20's exact phrasing — 0 hits against v21 content; silently non-functional)
- **class**: check designed around one sample phrasing becomes dead against natural drift
- **mechanism_closed_in**: v8.80 pattern-based rewrite (closed set of forbidden hazards + whole-document cross-reference markers, shadow-tested)
- **current_enforcement**: `claude_readme_consistency` pattern-based
- **new_enforcement**: P1 every check has a runnable author-form — dead checks are visible at author time because the shim returns "pattern did not match any content" as a diagnostic, not silent pass; P8 positive authoring form
- **test_scenario**: given a CLAUDE.md using mechanism X and README forbidding mechanism X with phrasing DIFFERENT from v20's, when shim runs, then still fails (pattern-based, not sample-phrasing-based)
- **calibration_bar**: check pass-count ≥ 0 AND ≥ 1 author-runnable invocation per recipe run (check isn't silent-dead)

#### 3.3. `v21-framework-token-hardcoding`
- **origin_run**: v21 (`categoryBrands` + `specificMechanismTokens` hardcoded `typeorm`, `prisma`, `ioredis`, `keydb`, `TypeORM synchronize`, `queue: 'workers'` → framework-agnostic claim violated)
- **class**: check classification lists hardcode framework-specific tokens; framework-agnostic claim is structurally broken
- **mechanism_closed_in**: v8.80 framework-token purge — strip framework tokens from classifier lists
- **current_enforcement**: classifier lists in `internal/tools/workflow_checks_service_coverage.go` + `workflow_checks_causal_anchor.go`
- **new_enforcement**: P6 guidance is atomic — framework specifics live in own atoms (e.g. `principles/platform-principles/05-structured-creds.md` names credentials-as-options pattern framework-agnostic); P1 runnable forms use Zerops-vocabulary only
- **test_scenario**: given a classifier list, when grepped for `typeorm|prisma|ioredis|keydb|TypeORM synchronize|queue: 'workers'`, then zero matches
- **calibration_bar**: `grep -rE 'typeorm|prisma|ioredis|keydb' internal/tools/workflow_checks_*.go` returns only test-fixture strings, never classification-list tokens

#### 3.4. `v21-mcp-schema-error-retries`
- **origin_run**: v21 (6 MCP schema errors in feature subagent — `hostname` vs `serviceHostname`, `logLines` vs `lines`, `ramGB` not a property)
- **class**: memory-frozen parameter names produce rejection retries; schema errors return generic validator messages without rename hints
- **mechanism_closed_in**: v8.80 MCP schema-error rename suggestions (explicit `hostname → serviceHostname` rename hints in error payload); v8.96 Fix #1 schema-mode decision-tree for `zerops_knowledge`
- **current_enforcement**: schema-error payloads carry rename hints
- **new_enforcement**: substrate stays; atoms under `briefs/` carry P2-compliant leaf content (no internal tool-schema vocabulary mistakenly exposed as a "property")
- **test_scenario**: given a sub-agent calling `zerops_logs` with `hostname` param, when rejection fires, then payload carries "Did you mean `serviceHostname`?" hint
- **calibration_bar**: `≤1` MCP schema-validation error across full run (v31: 5 during appstage 502 recovery; v32/v33/v34: 0)

#### 3.5. `v21-dev-server-stop-pkill-self-kill`
- **origin_run**: v21 (6 exit-255 events on `zerops_dev_server action=stop` — `pkill -f nest` matched own `sh -c` child, SSH dropped with exit 255; tool surfaced raw 255 rather than classifying)
- **class**: `pkill -f` self-match + SSH exit-255 surfacing as failure
- **mechanism_closed_in**: v8.80 `pkill --ignore-ancestors` (where supported) + `isSSHSelfKill` classifier converting ssh-exit-255 into structured success
- **current_enforcement**: `dev_server_lifecycle.go` self-kill classifier (substrate)
- **new_enforcement**: substrate stays per README §1
- **test_scenario**: given a run that called `zerops_dev_server action=stop` ≥3 times, when session log is parsed for exit-255 classifications, then zero surface as failure
- **calibration_bar**: `0` exit-255 classified as failure in dev_server stop events

#### 3.6. `v21-delegation-collapse`
- **origin_run**: v21 (5 subagents vs v20's 10 — writer, yaml-updater, generate-time fix×2, close-step critical-fix all collapsed)
- **class**: emergent delegation patterns that carried v18–v20 never formalized in recipe.md as required dispatches → agent variance abolished them
- **mechanism_closed_in**: v8.80 `§3.6d` writer-subagent dispatch gate (readmes substep rejected unless writer dispatch observed); v8.81 post-writer content-fix dispatch gate (later rolled back v24 as anti-convergent)
- **current_enforcement**: dispatch-gate `content_fix_dispatch_required` (v8.81, rolled back); writer-subagent dispatch validated via facts-log side-channel
- **new_enforcement**: P4 server workflow state IS the plan — substep-complete predicates name expected dispatches ("this substep completes when writer sub-agent has returned AND `ZCP_CONTENT_MANIFEST.json` exists"); P2 leaf artifact
- **test_scenario**: given a recipe run where main absorbs writer-level work inline, when substep-complete attempt fires, then rejection until predicate matches (dispatch observed OR equivalent evidence)
- **calibration_bar**: showcase tier dispatches ≥ 6 sub-agents (scaffold×3 + feature + writer + code-review); minimal tier ≥ 3 (scaffold + writer + code-review)

---

### v22 — v8.80 gates hold; gotcha-as-run-incident + recurrence classes

#### 4.1. `v22-nats-url-creds`
- **origin_run**: v22 (NATS `TypeError: Invalid URL` on `${queue_password}` containing URL-reserved chars — recurrence of v21 class; scaffold subagents re-emitted URL-embedded creds despite v21 gotcha in scope)
- **class**: parallel scaffold sub-agents independently derive NATS connection string code, each embedding `user:pass@host:port` in URL; URL-reserved chars in generated passwords break `nc.connect`
- **mechanism_closed_in**: v8.81 `§4.3` scaffold-brief NATS credentials preamble (user/pass as separate `ConnectionOptions`)
- **current_enforcement**: scaffold-subagent-brief preamble
- **new_enforcement**: P3 `SymbolContract.fixRecurrenceRules` declares "NATS: separate user/pass in ConnectionOptions, never URL-embed"; atom `principles/platform-principles/05-structured-creds.md` positively declares the pattern; P1 runnable `grep -rE 'nats://[^@]+@' {host}/src/` returns empty
- **test_scenario**: given a scaffold sub-agent's generated NATS client code, when `grep -rE 'nats://\$\{[^}]+\}:\$\{[^}]+\}@' {host}/src/` runs, then empty
- **calibration_bar**: `0` URL-embedded NATS credential occurrences across all codebases; `0` runtime `TypeError: Invalid URL` from NATS bootstrap

#### 4.2. `v22-s3-http-endpoint-recurrence`
- **origin_run**: v22 (S3 `HeadBucketCommand` 301 redirect — apidev `storage.service.ts` used `http://${storage_apiHost}` but object-storage proxy redirects HTTP→HTTPS and AWS SDK does not follow)
- **class**: scaffolder builds S3 endpoint from `storage_apiHost` (host-only, HTTP) instead of `storage_apiUrl` (full URL, HTTPS) — proxy redirect breaks `HeadBucketCommand`
- **mechanism_closed_in**: v8.81 `§4.4` scaffold-brief S3 endpoint preamble (`storage_apiUrl` preferred)
- **current_enforcement**: scaffold-subagent-brief preamble
- **new_enforcement**: P3 `SymbolContract.fixRecurrenceRules` declares "S3: use `storage_apiUrl` + `forcePathStyle: true`"; atom + P1 runnable `grep -rE 'storage_apiHost|http://[^/]*storage' {host}/src/` returns empty
- **test_scenario**: given an S3 client configuration line, when grepped for `storage_apiHost` standalone, then empty (must be `storage_apiUrl`)
- **calibration_bar**: `0` S3 endpoint built from `storage_apiHost` alone; `0` runtime `HeadBucketCommand` 301 redirect failures

#### 4.3. `v22-dev-start-build-contract`
- **origin_run**: v22 (workerdev `zerops_dev_server start` returned `post_spawn_exit` with `Cannot find module '/var/www/dist/main.js'` — dev `buildCommands: npm install` only, no build → `dist/main.js` missing)
- **class**: contract drift between `zerops.yaml` dev `buildCommands` (install only) and dev-start command (expects compiled output)
- **mechanism_closed_in**: v8.81 `§4.5` `{hostname}_dev_start_contract` check (fails generate if `run.start` references compiled build output but dev `buildCommands` omits build)
- **current_enforcement**: `{hostname}_run_start_build_cmd` at generate-step
- **new_enforcement**: P1 runnable shim `zcp check run-start-build-contract --hostname={h}`; atom `phases/generate/zerops-yaml/setup-rules-dev.md` positively declares "dev start uses ts-node / nodemon / php artisan serve / etc. against source, not compiled output"
- **test_scenario**: given `zerops.yaml` dev setup with `run.start: node dist/main.js` and `build.buildCommands: [npm install]`, when shim runs, then fails with "dev start references `dist/main.js` but buildCommands omits build step"
- **calibration_bar**: `0` `dev_start_contract` fails at generate-complete

#### 4.4. `v22-post-writer-iteration-leaks-to-main`
- **origin_run**: v22 (11 Edits on `workerdev/README.md` + 8 on `apidev/README.md` absorbed into main context after writer returned — 15 min wall cost)
- **class**: writer sub-agent dispatched correctly, but post-writer content-check failures loop iteration into main rather than dispatching a content-fix subagent
- **mechanism_closed_in**: v8.81 `content_fix_dispatch_required` gate (rolled back in v24); the convergence-direction root cause is closed by P1 (author-runnable pre-attest replaces external-gate-fix-dispatch loop altogether)
- **current_enforcement**: v8.96 Theme A (ReadSurface/HowToFix) + v8.104 Fix E (PerturbsChecks) — both refuted as convergence fixes by v34 data
- **new_enforcement**: **P1 supersedes** — writer runs author-runnable shim against own draft BEFORE attesting; no external-gate loop; the convergence loop is structurally absent
- **test_scenario**: given a writer sub-agent returning, when author runs pre-attest shim on mount draft, then exits 0 OR names every failure with exact line + required form; writer iterates locally without round-trip
- **calibration_bar**: deploy-complete converges in ≤ 2 rounds (v34: 4); post-writer main-context Edits on any `{host}/README.md` ≤ 3 across full run (v22: 11 on workerdev)

#### 4.5. `v22-gotchas-as-run-incident-log`
- **origin_run**: v22 (19/19 gotchas 1:1 to this run's session-log incidents with run-specific error strings — decoratively passes token-level checks but incident-derived at origin)
- **class**: content shape is "post-mortem of this run" rather than "platform-invariant-teaching"
- **mechanism_closed_in**: v8.94 fresh-context writer (no run transcript in context) + pre-classified fact taxonomy + `IncludePlatformInvariants` list
- **current_enforcement**: writer brief `MANDATORY platform-invariant gotchas to actively consider` list; fact-classification taxonomy
- **new_enforcement**: P2 leaf artifact (writer brief carries no run transcript); P7 cold-read simulation + defect-class coverage check; atom `briefs/writer/showcase.md` + `briefs/writer/minimal.md` both fresh-context shape
- **test_scenario**: given a published gotcha set, when each gotcha's error-token phrases are string-matched against session-log `tool_result` contents, then ≥ 20% of gotchas name a mechanism the agent did NOT invoke during the run
- **calibration_bar**: gotcha-origin ≥ 80% genuine platform teaching; ≥ 2 gotchas per codebase whose exact error strings do NOT appear in session-log tool_results

#### 4.6. `v22-cross-codebase-coherence-limiter`
- **origin_run**: v22 (no root README architecture section naming the three-service split + contracts; reader learns per-codebase but the 3-service integration story lives in their head)
- **class**: root README is a link aggregator, not an architecture narrator
- **mechanism_closed_in**: v8.81 `§4.6` `recipe_architecture_narrative` check (informational) — rolled back in v24
- **current_enforcement**: none post-rollback (editorial gap, intentional)
- **new_enforcement**: **(carry)** — deferred; atom `phases/close/completion.md` MAY positively declare an optional architecture-narrative block for showcase tier, but per RESUME decision #5 (conservative deletion) the check doesn't return unless step-4 simulation surfaces new evidence
- **test_scenario**: given a showcase recipe, when root README is read cold, then a reader identifies each codebase's role + each inter-codebase contract ≤ 2 min
- **calibration_bar**: **(advisory)** — not a v35 gate; if present, root README architecture section names ≥ 2 of {apidev, appdev, workerdev} by hostname + ≥ 1 cross-codebase contract verb

---

### v23 — content recovers, convergence breaks, folk-doctrine ships

#### 5.1. `v23-content-fix-loop-convergence-spiral`
- **origin_run**: v23 (5 rounds / 23 min / anti-convergent brief construction: truncation + "be surgical" framing + wrong file read)
- **class**: external-gate + dispatch-fix-subagent pattern is anti-convergent — strictly decreasing fail count (23→11→5→4→2→0) but 5 rounds where 1 should suffice
- **mechanism_closed_in**: v24 rollback (removed v8.81 gate + v8.86 plan); convergence root-cause closed by v31's asymmetry thesis → P1 (v34 empirically validates)
- **current_enforcement**: **(rolled back)** — gate removed; v8.96 Theme A + v8.104 Fix E both shipped metadata fixes that did NOT collapse rounds
- **new_enforcement**: **P1 author-runnable pre-attest replaces the external-gate loop structurally** — author runs every check locally before attesting; gate becomes confirmation, not discovery
- **test_scenario**: given a writer draft with ≥10 content-check failures, when author invokes the batch of runnable shims locally, then every failure is surfaced with its exact line + fix form BEFORE the first `complete deploy` attempt
- **calibration_bar**: deploy-complete content-check rounds ≤ 2 (v34: 4); finalize content-check rounds ≤ 1 (v34: 3)

#### 5.2. `v23-execOnce-burn-folk-doctrine`
- **origin_run**: v23 (apidev first-deploy seed returned ✅ in 56ms with 0 rows; agent invented "execOnce burn from initial workspace deploy" + codified "Recovering execOnce burn" CLAUDE.md section — factually wrong since `appVersionId` is per-deploy not per-workspace)
- **class**: unexpected runtime observation + incomplete diagnosis → invented platform folk-doctrine codified in CLAUDE.md; recurrent through v28/v32
- **mechanism_closed_in**: v8.104 Fix B — `bootstrap-seed-v1` static `execOnce` key separately from `${appVersionId}` migration key; eliminates the phenomenon at the source
- **current_enforcement**: recipe-pattern fix in `zerops.yaml` templates + writer brief taxonomy (fabricated mental model → DISCARD)
- **new_enforcement**: P1 runnable `grep -E 'execOnce \$\{appVersionId\}.*seed' {host}/zerops.yaml` returns empty; atom `phases/generate/zerops-yaml/setup-rules-dev.md` + `setup-rules-prod.md` positively declare seed key shape
- **test_scenario**: given a recipe's dev + prod setup in `{host}/zerops.yaml`, when grep for `execOnce \$\{appVersionId\}.*seed` runs, then empty; grep for `execOnce bootstrap-seed-v1` returns ≥1 line
- **calibration_bar**: `0` occurrences of `execOnce \$\{appVersionId\}.*seed`; `≥ 1` occurrence of static seed key per recipe; `0` published CLAUDE.md sections titled "Recovering execOnce burn" or equivalent

#### 5.3. `v23-not-connected-misattribution`
- **origin_run**: v23 (3 parallel calls — 2 deploys returned `Not connected` MCP transport error because `zerops_deploy` blocks channel; TIMELINE narrated it as "parallel cross-deploys rejected by platform")
- **class**: MCP transport error (channel busy) misattributed to platform behavior; propagates into published TIMELINE
- **mechanism_closed_in**: v8.96 Fix #1 `zerops_knowledge` schema-mode clarity (adjacent class — named mode collisions); broader: P8 positive recovery form
- **current_enforcement**: tool description clarity; writer brief "fabricated mental models → DISCARD" rule
- **new_enforcement**: atom `principles/tool-error-interpretation.md` positively declares "`Not connected` is an MCP channel-busy condition, not platform refusal"; brief text DISCARDs fabricated-mental-model facts per P5 routing
- **test_scenario**: given a session-log `Not connected` event followed by a published gotcha or TIMELINE claim about "parallel deploys rejected", when author runs a regex scan of published content, then zero misattribution-class claims
- **calibration_bar**: `0` published claims of the form "parallel cross-deploys rejected by platform" / "concurrent deploys disallowed" — such phrasings grep-negative in published tree

#### 5.4. `v23-ts-node-nodenext-shallow-fix`
- **origin_run**: v23 (workerdev `start:dev` failed `Cannot find module './app.module.js'` — apidev identical tsconfig worked because `nest start --watch` proxies through bundler; agent flipped workerdev to commonjs without questioning why apidev's identical tsconfig didn't fail)
- **class**: sustainable fix shipped with shallow understanding — root cause not named in diagnostic; risk of shipping as a gotcha with wrong mental model
- **mechanism_closed_in**: **(carry)** — addressed by v8.94 fresh-context writer + fact-classification (feature-subagent can record `fix_applied` with `mechanism` field that the writer discards or refines)
- **current_enforcement**: `zerops_record_fact` structured `mechanism` field + writer taxonomy
- **new_enforcement**: P5 two-way graph (every fact routed to one surface with override_reason); P7 cold-read catches shallow diagnoses at brief-review time
- **test_scenario**: given a recorded fact with `fix_applied` class + empty or single-word `mechanism` field, when writer reads the fact, then either DISCARD or writer demands mechanism elaboration
- **calibration_bar**: every published gotcha's `mechanism` field (in `ZCP_CONTENT_MANIFEST.json`) has ≥ 2 sentences or explicit cross-reference to a `zerops_knowledge` guide

---

### v25 — first post-rollback; workflow-discipline defects

#### 6.1. `v25-substep-bypass`
- **origin_run**: v25 (main did all 40 min of deploy work silently, backfilled 13 substep completions in 2 min at step end; first attestation out-of-order `INVALID_PARAMETER: expected "deploy-dev", got "subagent"`; ~73 KB of phase-scoped guidance delivered into dead phases)
- **class**: main agent bypasses the substep-delivery mechanism entirely because eager topics at step-entry deliver all guidance up-front
- **mechanism_closed_in**: v8.90 Fix B — de-eager `subagent-brief` + `readme-fragments` (remap to `SubStepInitCommands` / `SubStepFeatureSweepStage`); keep `where-commands-run` eager
- **current_enforcement**: `internal/workflow/recipe_topic_registry.go` Eager=false on two topics; `recipe_substep_briefs_test.go` regression guard
- **new_enforcement**: P4 server workflow state IS the plan — substep-scoped guidance delivers at substep-complete, not step-entry; step-entry atom `phases/deploy/entry.md` forbids "your tasks are …" framing (P8 positive form: "substep X completes when predicate P holds")
- **test_scenario**: given a recipe run, when session-log is parsed for `complete substep=X` events, then they fire in canonical order AS work happens (not backfilled in 2-min bursts at step-end); first `complete substep=deploy-dev` fires within 5 min of deploy-step entry
- **calibration_bar**: `0` out-of-order substep attestations per run; first-substep-complete latency < 5 min from step-entry; no 2-min backfill burst with >5 substep-completes

#### 6.2. `v25-subagent-workflow-at-spawn`
- **origin_run**: v25 (appdev scaffold + feature subagent called `zerops_workflow` at spawn; server returned misleading `PREREQUISITE_MISSING: Run bootstrap first` — latently dangerous rationalization loop)
- **class**: server suggests wrong remediation (bootstrap) when real state is "subagent shouldn't start a workflow"; sub-agent following suggestion literally could corrupt recipe session
- **mechanism_closed_in**: v8.90 Fix A — `SUBAGENT_MISUSE` error code at `handleStart` replacing misleading `Run bootstrap first`; positive message "this session has a recipe workflow; subagent should not start another"
- **current_enforcement**: `internal/platform/errors.go` SUBAGENT_MISUSE + `internal/tools/workflow.go` handleStart rejection
- **new_enforcement**: substrate stays; P8 positive-form recovery — error payload names positive action ("main agent orchestrates; subagent permitted tools are X, Y, Z"); P2 leaf brief declares permitted tools
- **test_scenario**: given a sub-agent's first tool use being `zerops_workflow action=start`, when rejection fires, then payload says "subagent should not start a workflow" NOT "Run bootstrap first"
- **calibration_bar**: `0` `SUBAGENT_MISUSE` rejections per run (sub-agents don't call `zerops_workflow` at all); `0` `PREREQUISITE_MISSING: Run bootstrap first` responses

#### 6.3. `v25-env4-app-static-yaml-comment-contradiction`
- **origin_run**: v25 (env 4 app-static comment "minContainers stays at the platform default" directly above YAML `minContainers: 2`; recurred v29)
- **class**: comment claims one value, adjacent YAML declares another; `factual_claims` regex misses because both sides match the same token "2" (semantic contradiction, not numeric)
- **mechanism_closed_in**: **(carry)** — editorial per rollback-calibration; v30 env 4 comments shipped clean as side-effect of v8.94 fresh-context writer
- **current_enforcement**: `{prefix}_factual_claims` regex (catches numeric disagreement only); writer-brief semantic consistency rule
- **new_enforcement**: P1 author-runnable shim `zcp check factual-claims --env={i}` extended to cover "comment says X is not / stays default; adjacent YAML declares X" pattern; P7 cold-read at brief-review catches semantic contradictions
- **test_scenario**: given env4/import.yaml with comment "minContainers:2 is not needed" + YAML `minContainers: 2`, when semantic-consistency shim runs, then fails naming the contradiction
- **calibration_bar**: `0` "comment claims vs adjacent YAML" contradictions in published env import.yaml files

---

### v26 — aborted early; two defects shipped pre-v28

#### 7.1. `v26-recipeplan-stringification`
- **origin_run**: v26 (agent passed `recipePlan` as JSON string twice before passing as object; two wasted round-trips)
- **class**: `jsonschema` tag on `WorkflowInput.RecipePlan` too terse ("Structured recipe plan") without explicit "object, not string" hint
- **mechanism_closed_in**: v8.93.2 — concrete object-shape example + "stringifying costs retry" warning in jsonschema tag
- **current_enforcement**: `WorkflowInput.RecipePlan` description with shape example
- **new_enforcement**: substrate stays; `briefs/` atoms never reference MCP input-shape details (P2 leaf artifact)
- **test_scenario**: given `complete step=research` with `recipePlan` passed as object, when handler accepts, then succeeds on first call
- **calibration_bar**: `0` stringification retries at `complete step=research`

#### 7.2. `v26-git-init-zcp-side-chown`
- **origin_run**: v26 (post-scaffold `git init && sudo chown -R` zcp-side against SSHFS mount — permission-denied; recurrence of v17/v21 class but in post-scaffold path)
- **class**: zcp-side git + chown over SSHFS mount — same class as v17/v21 execution-surface confusion
- **mechanism_closed_in**: v8.93.1 — `git-config-mount` block rewritten as single container-side SSH call doing config + init + initial commit in one go
- **current_enforcement**: `git-config-mount` block text in recipe.md
- **new_enforcement**: atom `principles/git-workflow.md` positively declares "git runs container-side via ssh only; `.git/` ownership is zerops from creation"; P8 positive form
- **test_scenario**: given post-scaffold git sequencing, when author runs via `ssh {host} "git init && git add -A && git commit"`, then succeeds without chown
- **calibration_bar**: `0` `Permission denied` on `.git/` operations per run; `0` `sudo chown` on `.git/` operations

---

### v28 — mechanics hold, content audit exposes surface gaps

#### 8.1. `v28-env-self-shadow-check-surface-gap`
- **origin_run**: v28 (workerdev shipped 9 self-shadow lines; `complete step=generate` returned 11 checks — 0 `worker_*`; `DetectSelfShadows` exists but worker hostname wasn't enumerated in generate-complete check surface)
- **class**: check implementation correct but the hostname-enumeration loop at generate-complete skipped worker → check never fires for worker codebase
- **mechanism_closed_in**: v8.94 Fix 5 — `env_self_shadow` enumerates every hostname at generate-complete
- **current_enforcement**: `workflow_checks_generate.go` enumerated for all hostnames
- **new_enforcement**: P1 author-runnable `zcp check env-self-shadow --hostname={h}` callable per-hostname; P7 cold-read of check list at brief-review verifies per-hostname coverage
- **test_scenario**: given a 3-codebase plan with `workerdev/zerops.yaml` containing `db_hostname: ${db_hostname}`, when `complete step=generate` fires, then response `checks` array contains `worker_env_self_shadow` with `status=fail`
- **calibration_bar**: generate-complete check array contains `{host}_env_self_shadow` for every hostname in the plan (count == #codebases)

#### 8.2. `v28-wrong-surface-gotchas`
- **origin_run**: v28 (apidev setGlobalPrefix = NestJS framework docs; appdev api.ts = recipe's own scaffold helper; appdev plugin-svelte peer-dep = npm registry metadata — all shipped as gotchas)
- **class**: content that belongs in framework docs / code comments / npm meta shipped as Zerops gotcha
- **mechanism_closed_in**: v8.94 fresh-context writer + pre-classified fact taxonomy (framework-quirk / library-meta / scaffold-decision → DISCARD or route off-gotcha surface); v8.95 Fix C `ZCP_CONTENT_MANIFEST.json` structured contract
- **current_enforcement**: writer brief taxonomy + `writer_discard_classification_consistency` check
- **new_enforcement**: P5 two-way graph — every fact has `routed_to` + every published item has fact source; `writer_manifest_honesty` expanded to all `(routed_to × surface)` pairs; P7 cold-read catches wrong-surface items; **editorial-review (refinement 2026-04-20) adds independent re-classification — `classification-reclassify.md` atom + `counter-example-reference.md` v28 wrong-surface anti-pattern library; CRIT class for wrong-surface items**
- **test_scenario**: given a fact classified `framework-quirk` with `routed_to=discarded` in manifest, when the fact's title-tokens are grepped across `{host}/README.md` knowledge-base fragments, then zero matches; AND editorial-review reclassification agrees with `discarded` (writer-reviewer classification agreement)
- **calibration_bar**: `0` wrong-surface items (framework-docs / library-meta / scaffold-helper) shipped as Zerops gotchas; code-review scan for wrong-surface markers returns zero; editorial-review CRIT count of wrong-surface class `= 0` after inline-fix

#### 8.3. `v28-folk-doctrine-fabrication`
- **origin_run**: v28 (workerdev gotcha #1 — "API codebase avoided the symptom because its resolver path happened to interpolate before the shadow formed; do not rely on that" — fabricated; both api + worker had identical shadow patterns)
- **class**: debugging agent invents platform folk-doctrine to explain an incompletely-understood observation; shipped as published content
- **mechanism_closed_in**: v8.94 fresh-context writer (no run transcript) + `env-var-model` guide citation requirement
- **current_enforcement**: writer brief MANDATORY "fabricated mental models → DISCARD" rule; citation requirement for any gotcha whose topic matches a `zerops_knowledge` guide
- **new_enforcement**: P5 routing — fabricated-mental-model class routes to `discarded` with override_reason naming the true mechanism; P7 cold-read + defect-class check catches fabrications at brief-review; **editorial-review (refinement 2026-04-20) adds three independent catches: `citation-audit.md` atom enforces "every matching-topic gotcha cites `zerops_knowledge` guide verbatim"; `classification-reclassify.md` independently reclassifies and catches writer-classification-vs-reviewer-classification disagreements; `counter-example-reference.md` v28 folk-doctrine anti-pattern library for pattern-matching. Dispatch-runnable checks `editorial_review_no_fabricated_mechanism == 0` + `editorial_review_citation_coverage` = 100% gate pass.**
- **test_scenario**: given a gotcha claim, when author greps `zerops_knowledge` guides for the claim's mechanism tokens, then EITHER the claim matches a guide (citation added) OR the claim is discarded; AND editorial-review's independent citation audit finds 0 matching-topic gotchas without citations AND editorial-review's classification-reclassify flags 0 fabricated-mechanism instances
- **calibration_bar**: `0` folk-doctrine / fabricated-mental-model gotchas in published tree; every gotcha whose topic matches a `zerops_knowledge` guide carries explicit citation; editorial-review `CRIT_count.fabricated == 0` AND `citation_audit.uncited == 0`

#### 8.4. `v28-cross-surface-fact-duplication`
- **origin_run**: v28 (same facts on 3–4 surfaces: .env shadowing on 3 surfaces; forcePathStyle on 4; tsbuildinfo on 4 with factual error on one)
- **class**: same fact published on multiple surfaces; taxonomy-routing didn't enforce "one canonical surface + cross-refs"
- **mechanism_closed_in**: v8.94 fresh-context writer + surface routing map
- **current_enforcement**: writer brief routing taxonomy
- **new_enforcement**: P5 two-way graph — every fact routes to EXACTLY ONE `routed_to`; siblings cross-reference; expanded `writer_manifest_honesty` enforces
- **test_scenario**: given a fact routed to `apidev-gotcha`, when the fact's title-tokens are grepped across `appdev/README.md` knowledge-base + `workerdev/README.md` knowledge-base, then zero matches (siblings carry cross-ref prose, not the fact itself)
- **calibration_bar**: each fact's body appears on exactly 1 surface; if on 2+ surfaces, the ones that are NOT `routed_to` contain only cross-reference prose (not the mechanism body)

---

### v29 — v8.94 ships; env-README surface inherits

#### 9.1. `v29-scaffold-preship-sh-leak`
- **origin_run**: v29 (apidev scaffold wrote 2,840-byte `preship.sh` + committed; asymmetric vs appdev/workerdev inline shell; code-review saw it + said "out of scope")
- **class**: scaffold-phase self-test infrastructure committed to published deliverable; sub-class of v21 scaffold_hygiene but different content type (recipe-authoring assertions, not build artifacts)
- **mechanism_closed_in**: v8.95 Fix A — `scaffold_artifact_leak` check + scaffold-brief hygiene rule + generate-step leak scan
- **current_enforcement**: `{hostname}_scaffold_artifact_leak` (`workflow_checks_scaffold_artifact.go`)
- **new_enforcement**: P1 runnable `find {h} -maxdepth 3 -name 'preship.sh' -o -name '*.assert.sh' | wc -l == 0`; P8 positive-form brief rule ("scripts for your own pre-ship verification go in `/tmp/zcp-preship-*.sh` — never committed; the published tree carries no recipe-authoring scripts")
- **test_scenario**: given an apidev scaffold sub-agent that wrote `scripts/preship.sh` + committed, when check fires, then fail naming the offender
- **calibration_bar**: `0` `preship.sh` / `*.assert.sh` files in published tree across all codebases

#### 9.2. `v29-env-0-cross-tier-persistence-fabrication`
- **origin_run**: v29 (env 0 README: "Data persists across tier promotions because service hostnames stay stable" — factually wrong for default `zerops_import` path where each env imports a new project)
- **class**: Go-template hardcoded fabricated claim about platform semantics in env 0 README prose
- **mechanism_closed_in**: v8.95 Fix B — in-place edit of `recipe_templates.go` line 172 + regression tests pinning claims to YAML truth
- **current_enforcement**: `recipe_templates.go` source corrected; regression test
- **new_enforcement**: P1 runnable `grep -E 'data persists across tier|hostnames stay stable' env0/README.md` returns empty; P8 positive form in env-README atom `phases/finalize/env-comments.md` ("each tier is a separate project; data does NOT carry across unless `override-import` is used")
- **test_scenario**: given env 0 README, when grep runs for cross-tier-persistence claims, then zero matches; or if the README covers the topic, it names `override-import` explicitly
- **calibration_bar**: `0` cross-tier-persistence fabrications in env 0–5 READMEs; `grep -rE '(data persists across tiers?|hostnames stay stable)' environments/*/README.md` returns empty

#### 9.3. `v29-env-readme-go-template-factual-drift`
- **origin_run**: v29 (11 wrong `minContainers` claims in env 3/4/5 READMEs from hardcoded Go-template strings at `recipe_templates.go` lines 172, 279, 285, 305, 324, 330, 370)
- **class**: Go-source factual drift — `envPromotionPath(3)`/`envDiffFromPrevious(4)`/etc. hardcode claims that don't match the YAML truth they purport to describe
- **mechanism_closed_in**: v8.95 Fix B — direct source edit + regression test harness pinning claims to YAML
- **current_enforcement**: `recipe_templates.go` corrected; `recipe_templates_test.go` regression
- **new_enforcement**: P1 runnable — compare each env README's `minContainers`/`mode`/`objectStorageSize` claim against the declared value in `services[].*` of the next-tier `import.yaml`; shim `zcp check env-readme-vs-yaml`
- **test_scenario**: given env3/README.md claiming "Runtime stays at minContainers: 1" and env4/import.yaml declaring `minContainers: 2`, when shim runs, then fails with exact line + mismatch
- **calibration_bar**: `0` factual-drift claims across env 0–5 READMEs; `grep -E 'minContainers: \d|mode: (HA|NON_HA)|objectStorageSize: \d' environments/*/README.md` lines all match the adjacent import.yaml ground truth

#### 9.4. `v29-writer-kept-despite-discard`
- **origin_run**: v29 (v8.94 brief pre-classified 2 of 10 facts DISCARD — apidev healthCheck-bare-GET + appdev Multer-FormData; writer kept both as published gotchas; 2/14 override rate)
- **class**: brief taxonomy treated as suggestion — no hard gate enforces DISCARD
- **mechanism_closed_in**: v8.95 Fix C — structured `ZCP_CONTENT_MANIFEST.json` writer-output contract with DISCARD enforcement at post-write check; v8.94 + v8.95 Fix C combined
- **current_enforcement**: `writer_discard_classification_consistency` + `writer_manifest_honesty` checks
- **new_enforcement**: P5 two-way graph expanded — `writer_manifest_honesty` iterates all `(routed_to × surface)` pairs, not only `(discarded, published_gotcha)`; every fact's title-tokens verified against its declared surface
- **test_scenario**: given `ZCP_CONTENT_MANIFEST.json` with fact F `routed_to=discarded`, when grep F.title-tokens against `{host}/README.md#knowledge-base`, then zero matches
- **calibration_bar**: writer DISCARD override rate = 0 (every fact with `routed_to=discarded` has empty intersection with published gotcha stems)

---

### v30 — v8.94 reproducibility; v8.95 plan unshipped

#### 10.1. `v30-workerdev-sigterm-missing`
- **origin_run**: v30 (writer taught MANDATORY SIGTERM drain in workerdev README + CLAUDE.md; feature subagent's scaffolded `main.ts` didn't implement it → close CRIT)
- **class**: writer brief mandates a pattern; scaffold / feature subagent has no pre-flight trap list forcing the pattern's implementation
- **mechanism_closed_in**: v8.95 Fix #4 — scaffold pre-flight traps list in scaffold-subagent-brief (worker SIGTERM handler required); v8.97 Fix 5 extended to 6 platform principles including graceful shutdown
- **current_enforcement**: scaffold-subagent-brief MANDATORY block (v8.97 Fix 5) + `{hostname}_worker_shutdown_gotcha` check
- **new_enforcement**: P3 `SymbolContract.fixRecurrenceRules` declares "worker codebases: implement SIGTERM handler + `app.close()` drain before SIGKILL window"; P1 runnable `awk '/^\`\`\`/,/^\`\`\`/{buf=buf$0} /process\.on\(.SIGTERM./&&/app\.close\(/{ok=1} END{exit !ok}' workerdev/src/main.ts`
- **test_scenario**: given a workerdev `main.ts` missing SIGTERM handler, when feature subagent runs pre-flight trap check, then fails with "worker codebase MUST install SIGTERM handler before shipping `main.ts`"
- **calibration_bar**: `0` close CRITs from missing-SIGTERM class; workerdev `src/main.ts` contains `process.on('SIGTERM'` + `app.close` tokens

#### 10.2. `v30-ds-store-gitignore-drift`
- **origin_run**: v30 (apidev + workerdev shipped 2 `.DS_Store` files; none of 3 codebases had `.DS_Store` in `.gitignore`)
- **class**: scaffold `.gitignore` template drift — nothing enforces a standard baseline; sub-class of v21 scaffold_hygiene
- **mechanism_closed_in**: v8.95 scaffold baseline `.gitignore` rule (unshipped at v30, shipped via v8.97 scaffold hardening)
- **current_enforcement**: scaffold-brief + `scaffold_artifact_leak` check
- **new_enforcement**: P3 `SymbolContract.fixRecurrenceRules` declares `.gitignore` baseline `{node_modules, dist, build, target, vendor, .DS_Store, .env, .vite, *.log}`; P1 runnable `for f in $(echo node_modules dist .DS_Store .env); do grep -q "^$f" .gitignore || echo MISSING:$f; done`
- **test_scenario**: given a scaffold-complete `.gitignore`, when lint runs for baseline entries, then all of `{node_modules, dist, .DS_Store, .env}` are present
- **calibration_bar**: `0` `.DS_Store` files in published tree; every `{host}/.gitignore` contains the 4 baseline entries above

---

### v31 — first A− since v20; two convergence loops surface

#### 11.1. `v31-apidev-enableshutdownhooks-missing`
- **origin_run**: v31 (apidev/src/main.ts missing `app.enableShutdownHooks()` — JobsGateway's OnModuleDestroy NATS drain never fires on SIGTERM; 1 close CRIT, NEW subclass of v30 worker-SIGTERM class)
- **class**: sub-class of "missing mandatory lifecycle handler" — not covered by worker-only pre-flight; applies to any OnModuleDestroy-bearing provider in apidev
- **mechanism_closed_in**: v8.97 Fix 5 — six platform principles in scaffold MANDATORY block (graceful shutdown generalized beyond worker)
- **current_enforcement**: scaffold-subagent-brief platform-principles section
- **new_enforcement**: P3 `SymbolContract.fixRecurrenceRules` generalized — "any codebase with OnModuleDestroy-bearing provider requires `enableShutdownHooks()` at bootstrap"; P1 runnable `awk '/OnModuleDestroy/{hasDestroy=1} /enableShutdownHooks/{hasHooks=1} END{exit hasDestroy && !hasHooks}' {host}/src/main.ts`
- **test_scenario**: given apidev/src with `@Injectable() class JobsGateway implements OnModuleDestroy`, when author runs the shim, then succeeds only if `main.ts` contains `app.enableShutdownHooks()`
- **calibration_bar**: `0` close CRITs from missing-enableShutdownHooks class; every codebase with an `OnModuleDestroy` provider also calls `enableShutdownHooks()` in bootstrap

#### 11.2. `v31-deploy-3-round-readme-convergence`
- **origin_run**: v31 (`comment_ratio` reads embedded yaml in README IG fragment, not on-disk zerops.yaml; author learned by failure, 3 rounds)
- **class**: check's read surface opaque to author until a failure round teaches it; structural asymmetry "gates know what they read, authors know what they write"
- **mechanism_closed_in**: v8.96 Theme A — `StepCheck.ReadSurface`/`Required`/`Actual`/`CoupledWith`/`HowToFix` fields (refuted by v34 as convergence fix); P1 structurally supersedes
- **current_enforcement**: Theme A metadata + v8.104 Fix E `PerturbsChecks` (both shipped, both refuted)
- **new_enforcement**: P1 author-runnable pre-attest replaces external-gate-loop; atom declares the check's read surface upfront in the substep guidance; author runs shim before attesting
- **test_scenario**: given a README draft, when author runs the batch of pre-attest shims including the embedded-yaml-comment-ratio variant, then every failure is surfaced with exact line + fix form BEFORE `complete deploy`
- **calibration_bar**: deploy-complete ≤ 2 rounds (v34: 4; target v35: 1); first `complete step=deploy` passes ≥ 80% of the time

#### 11.3. `v31-finalize-3-round-env-convergence`
- **origin_run**: v31 (11 env-check violations on first finalize attempt; `comment_ratio` < 30%, `cross_env_refs`, `factual_claims`, `comment_depth` — 3 rounds to converge)
- **class**: same asymmetry class as 11.2 at finalize — author writes without pre-attest verification
- **mechanism_closed_in**: v8.96 Theme A (refuted); P1 supersedes
- **current_enforcement**: Theme A per-mismatch structured failure (refuted)
- **new_enforcement**: P1 author-runnable batch `for i in 0 1 2 3 4 5; do zcp check comment-depth --env=$i; zcp check factual-claims --env=$i; zcp check comment-ratio --env=$i; done` before `complete step=finalize`
- **test_scenario**: given 6 env import.yaml files, when author runs finalize pre-attest batch on the mount, then every failure is surfaced BEFORE attestation; first `complete step=finalize` passes
- **calibration_bar**: finalize-complete ≤ 1 round (v34: 3); first `complete step=finalize` passes ≥ 80% of the time

#### 11.4. `v31-cross-subagent-duplicate-archaeology`
- **origin_run**: v31 (scaffold investigates Meili v0.57 API, feature re-investigates same API 20 min later, code-review re-investigates svelte-check mismatch; ~80s cumulative duplicate work)
- **class**: facts log is writer-only; scaffold-discovered framework-quirks don't flow downstream to feature or code-review sub-agent briefs
- **mechanism_closed_in**: v8.96 Theme B — `FactRecord.Scope ∈ {content, downstream, both}` + `BuildPriorDiscoveriesBlock(sessionID, currentSubstep)` at brief-resolution time
- **current_enforcement**: `internal/workflow/recipe_brief_facts.go` + `GuidanceTopic.IncludePriorDiscoveries` opt-in on `subagent-brief` + `code-review-agent`
- **new_enforcement**: P5 two-way graph — downstream-scope facts flow into feature + code-review briefs; atom `briefs/feature/brief.md` + `briefs/code-review/brief.md` include `{{priorDiscoveries}}` template slot
- **test_scenario**: given a scaffold sub-agent recorded fact with `scope=downstream`, when feature sub-agent is dispatched, then the brief contains the fact's title + mechanism in a "Prior discoveries" block
- **calibration_bar**: ≥ 2 facts with `Scope="downstream"` recorded per run; feature-subagent does NOT re-investigate any fact whose title matches a prior-discoveries entry; duplicate-archaeology wall time ≤ 5s

#### 11.5. `v31-zerops-knowledge-schema-churn`
- **origin_run**: v31 (5 MCP schema errors on `zerops_knowledge` during appstage 502 recovery — two-mode combos query+recipe, runtime+scope — agent learned rules from rejection messages)
- **class**: tool argument-shape ambiguity; rejection messages don't prescribe the positive form
- **mechanism_closed_in**: v8.96 Fix #1 — tool description rewritten as 4-line decision tree (≤60 words); each field description leads with `MODE N:`; rejection messages NAME the conflicting modes
- **current_enforcement**: `internal/tools/knowledge.go` decision-tree description
- **new_enforcement**: substrate stays; P8 positive form in rejection payloads (named mode + remediation)
- **test_scenario**: given a sub-agent calling `zerops_knowledge` with two-mode combo, when rejection fires, then payload says "MODE 1 `query=` + MODE 2 `runtime=` — pick one; for runtime-briefing use MODE 2 alone"
- **calibration_bar**: `0` `zerops_knowledge` schema-churn errors per run (v31: 5; v32/v33/v34: 0)

#### 11.6. `v31-dev-server-stop-port-kill-dance`
- **origin_run**: v31 (feature subagent port-kill dance ~6s after `dev_server stop`; risk of shipping "Zerops requires manual port-kill" as a false gotcha)
- **class**: `dev_server stop` returned before OS reaper cleared port; next start hit "address already in use"; workaround-shape risk
- **mechanism_closed_in**: v8.96 Quality Fix #2 — post-kill `ss -tnlp` port-free polling (1.5s) + `fuser -k -KILL` escalation + explicit remediation text "do NOT add manual pkill workaround to the recipe"
- **current_enforcement**: `dev_server_lifecycle.go` poll substrate
- **new_enforcement**: substrate stays
- **test_scenario**: given a `dev_server stop` followed immediately by `dev_server start` on same port, when substrate runs, then zero "address already in use" errors
- **calibration_bar**: `0` "address in use" errors within 10s of `dev_server stop`; `0` "Zerops requires manual port-kill" gotchas in published tree

#### 11.7. `v31-file-not-read-errors`
- **origin_run**: v31 (7 "File has not been read yet" errors across scaffold subagents — Write-before-Read on scaffolder-generated files; ~18s cumulative; trains defensive over-reading)
- **class**: scaffold sub-agent Edits before Reading framework-scaffolder-generated files → tool error; defensive over-reading pollutes subsequent calls
- **mechanism_closed_in**: v8.96 Quality Fix #3 — Read-before-Edit rule in 5 sub-agent briefs with explicit "plan up front: before first Edit, batch-Read every file you intend to modify"
- **current_enforcement**: recipe.md block text in all 5 briefs (scaffold/feature/writer/code-review/dev-deploy)
- **new_enforcement**: P2 leaf artifact — each brief declares the rule once in the brief's own `_shared-mandatory.md` atom; atoms are transmitted byte-identical; P8 positive form
- **test_scenario**: given a sub-agent trace, when grepped for "File has not been read yet" errors, then zero occurrences
- **calibration_bar**: `0` "File has not been read yet" errors across main + all sub-agents per run (v31: 7; v33: 0; v34: 0)

#### 11.8. `v31-git-lock-sshfs-contention`
- **origin_run**: v31 (~90s `.git/config.lock` contention on close-step batch redeploy — framework-scaffolders' auto-`git init` left partial `.git/` that collided with canonical container-side git init)
- **class**: framework-scaffolder `git init` collision with main-agent's canonical container-side git init; SSHFS-mediated lock contention
- **mechanism_closed_in**: v8.96 Quality Fix #4 — scaffold-subagent-brief rule: framework-scaffolders must `--skip-git` OR `ssh {hostname} "rm -rf /var/www/.git"` immediately after scaffolder SSH returns; pre-ship Assertion 10
- **current_enforcement**: scaffold-subagent-brief text + pre-ship assertion
- **new_enforcement**: atom `principles/git-workflow.md` positively declares "framework scaffolders MUST use `--skip-git` where available; if absent, immediately delete `/var/www/.git` after scaffolder return"; P8 positive form
- **test_scenario**: given a scaffold sub-agent that ran `npx @nestjs/cli new` without `--skip-git` equivalent, when the sub-agent returns, then `/var/www/.git` does not exist
- **calibration_bar**: `0` `.git/index.lock` or `.git/config.lock` contention errors per run; `0` framework-scaffolder `.git/` residues at generate-complete

---

### v32 — v8.96 validation; close step never completed

#### 12.1. `v32-close-step-never-completed`
- **origin_run**: v32 (TIMELINE narrated up through stage feature-sweep then stopped with "Close (pending)"; `zcp sync recipe export` fired anyway)
- **class**: export gate missing; `zcp sync recipe export` ran while `step=close` incomplete
- **mechanism_closed_in**: v8.97 Fix 1 — `ExportRecipe` refuses when close incomplete
- **current_enforcement**: `ExportRecipe` close-gate
- **new_enforcement**: P4 server workflow state IS the plan — export command reads authoritative state + refuses if `step=close` not complete; atom `phases/close/completion.md` positively declares "export runs only after close-complete"
- **test_scenario**: given a recipe session with `step=close` incomplete, when `zcp sync recipe export` fires, then error "close-step incomplete; export refused"
- **calibration_bar**: `0` exports while `step=close` incomplete; export gate rejects any attempt before close-complete

#### 12.2. `v32-per-codebase-content-missing-from-deliverable`
- **origin_run**: v32 (writer produced apidev 303 / appdev 162 / workerdev 218 lines + 3 CLAUDE.md on SSHFS mount; `OverlayRealREADMEs` fires at close-complete which never happened; exported tree missing all per-codebase content)
- **class**: writer output orphaned because overlay fires at a gate the run never reached
- **mechanism_closed_in**: v8.97 Fix 2 — close verify-only sub-steps + publish out of workflow state
- **current_enforcement**: close-step verify-only sub-step re-ordering
- **new_enforcement**: P4 server state enforces ordering; atom `phases/close/completion.md` positively declares the overlay-fires-at-completion invariant; P6 atomic file declares the canonical tree
- **test_scenario**: given a complete recipe run, when export fires, then exported tree contains `{apidev,appdev,workerdev}/README.md` + `{apidev,appdev,workerdev}/CLAUDE.md` (6 files)
- **calibration_bar**: exported tree contains per-codebase README + CLAUDE.md for every hostname (6 files for 3-codebase showcase; 2 files for 1-codebase minimal)

#### 12.3. `v32-read-before-edit-dispatch-compression`
- **origin_run**: v32 (main-agent dispatch compression dropped Read-before-Edit rule across 3 scaffold sub-agents; recipe.md:845 literally calls out "v32 lost the Read-before-Edit rule")
- **class**: main compresses brief when transmitting to sub-agent; MANDATORY sentinels without byte-identical bounded regions lose load-bearing rules
- **mechanism_closed_in**: v8.97 Fix 3 A+B — MANDATORY sentinels wrapping file-op sequencing / tool-use policy / SSH-only executables with byte-identical-transmission rule
- **current_enforcement**: MANDATORY-sentinel bounded regions in recipe.md briefs
- **new_enforcement**: P2 leaf artifact — the transmitted brief IS the atom; dispatcher instructions ("compress", "include verbatim") live in `DISPATCH.md`, never in `briefs/`; Go stitching reads from `briefs/` only; build-time grep guard on forbidden dispatcher tokens
- **test_scenario**: given a captured Agent-tool dispatch payload, when grepped for phrases like "compress this", "include verbatim", "adapt per codebase", then zero matches; rule content appears byte-identical across all 3 scaffold dispatches
- **calibration_bar**: captured dispatch payloads contain byte-identical `_shared-mandatory.md` content; `0` dispatcher-vocabulary tokens in transmitted prompts

#### 12.4. `v32-stamp-coupling-hand-maintained-incomplete`
- **origin_run**: v32 (hand-maintained coupling table in Theme A incomplete → coupled-surface sequencing didn't predict every flip)
- **class**: hand-maintained table drift; coupling declaration out-of-sync with actual shared surfaces
- **mechanism_closed_in**: v8.97 Fix 4 — `StampCoupling` surface-derived (every failed check with populated `ReadSurface` gets `CoupledWith` stamped from shared-surface graph)
- **current_enforcement**: `StampCoupling` in `bootstrap_checks.go`
- **new_enforcement**: **P1 supersedes** — author-runnable pre-attest eliminates the coupling-metadata axis altogether; coupling becomes part of the runnable batch (author runs all checks before attesting)
- **test_scenario**: given a new check added to the suite without explicit coupling declaration, when `StampCoupling` runs, then coupling is derived from ReadSurface graph automatically
- **calibration_bar**: coupling declarations in the pre-attest runnable batch match the ReadSurface-shared-graph; no hand-maintained drift

#### 12.5. `v32-platform-principles-missing-from-scaffold`
- **origin_run**: v32 (TypeORM entity-table mismatch `@Entity({name:'job'})` vs migration `jobs` + other principle violations from scaffold)
- **class**: scaffold sub-agent has no MANDATORY platform-principles block → produces code that violates fundamental platform invariants
- **mechanism_closed_in**: v8.97 Fix 5 — six platform principles in scaffold MANDATORY (graceful shutdown, routable bind, proxy trust, competing-consumer, structured creds, stripped build root)
- **current_enforcement**: scaffold-subagent-brief MANDATORY platform-principles section
- **new_enforcement**: P2 leaf artifact — atoms `principles/platform-principles/{01…06}.md` referenced by scaffold briefs; P3 `SymbolContract` declares the six principles as fix-recurrence rules
- **test_scenario**: given a scaffold sub-agent brief, when content is parsed, then all 6 platform-principle atoms are referenced (by path token); sub-agent's first pass generates code consistent with each
- **calibration_bar**: scaffold sub-agent's first-pass output passes all 6 platform-principle shims locally before return; `0` close CRITs from the 6 platform-principle classes

---

### v33 — peak content body, phantom output tree, guidance-layer inventions

#### 13.1. `v33-phantom-output-tree`
- **origin_run**: v33 (writer wrote 7 orphan files to `/var/www/recipe-nestjs-showcase/` + `environments/{0 — Development with agent, …, 5 — HA production}/README.md` with paraphrased env folder names; main-agent synthesized "Output root" + paraphrased names in writer dispatch prompt)
- **class**: writer brief leaves canonical output paths + env folder names as free parameters that main synthesizes → main hallucinates
- **mechanism_closed_in**: v8.103 close-section `SingleCanonicalOutputLocation` guard; v8.104 Fix A MANDATORY sentinel inside writer brief forbidding "output root" + "env folder names" as parameters
- **current_enforcement**: v8.104 Fix A sentinel + v8.103 close guard
- **new_enforcement**: P8 positive allow-list — brief declares "write env READMEs to `environments/{EnvFolder(i)}/README.md`; per-codebase files to `{hostname}/`; any other path is out-of-scope"; P2 leaf artifact — paths come from Go-templates, not dispatcher prose
- **test_scenario**: given a post-close deliverable tree, when `find /var/www -maxdepth 2 -type d -name 'recipe-*'` runs, then empty
- **calibration_bar**: `find /var/www -maxdepth 2 -type d -name 'recipe-*' | wc -l == 0`; published tree contains exactly one canonical output location

#### 13.2. `v33-auto-export-at-close`
- **origin_run**: v33 (v8.98 Fix B framed export as autonomous `NextSteps[0]` → 3 `zcp sync recipe export` invocations at 08:51–08:54 after close; user objected)
- **class**: close-completion `NextSteps[0]` autonomous framing triggered unprompted export
- **mechanism_closed_in**: v8.103 — both export AND publish now "ON REQUEST ONLY"; summary explicitly says workflow done at close
- **current_enforcement**: close-step `NextSteps` = empty; export/publish require explicit user request
- **new_enforcement**: P4 server state — close step's `NextSteps` positively declares "no autonomous follow-up"; P8 positive form replaces "auto-export" with "on user request only"
- **test_scenario**: given `complete step=close` response, when `NextSteps` is inspected, then array is empty or contains only user-facing messages (no autonomous tool invocations)
- **calibration_bar**: `0` autonomous `zcp sync recipe export` / `publish` invocations after close-complete; all post-close tool calls are user-triggered

#### 13.3. `v33-feature-subagent-diagnostic-panic-burst`
- **origin_run**: v33 (~80 parallel bash probes saturated concurrency queue; trivial commands hit 300+ seconds; no host-confusion, agent convinced itself something was wrong and probed until convinced otherwise)
- **class**: feature sub-agent brief left diagnostic-probe cadence unspecified → invented "panic burst" pattern when probing felt productive
- **mechanism_closed_in**: v8.104 Fix D — feature-subagent MANDATORY: "≤3 targeted probes per ambiguous signal; no parallel-identical probes; stop and report if three don't resolve"
- **current_enforcement**: scaffold + feature brief cadence text
- **new_enforcement**: P8 positive-form brief rule in `briefs/feature/brief.md` ("max 5 bash/min; targeted probes only; stop and report"); P2 leaf artifact
- **test_scenario**: given a feature sub-agent trace, when bash calls within any 30s window are counted, then max ≤ 10
- **calibration_bar**: feature sub-agent bash calls ≤ 5 per minute (v33: ~80 parallel probes in 90s; v34: max 5/min)

#### 13.4. `v33-unicode-box-drawing-invention`
- **origin_run**: v33 (`# ──────────────────` Unicode box-drawing separators in all 3 zerops.yaml files; pure agent stylistic invention; recipe.md:2914 covered comment voice but not visual style)
- **class**: guidance about comment VOICE lived far from guidance about visual STYLE → agent invented a decoration axis because no atom owned it
- **mechanism_closed_in**: v8.104 Fix C — "Visual style" subsection forbidding Unicode box-drawing + ASCII dividers + emoji
- **current_enforcement**: recipe.md visual-style block
- **new_enforcement**: P8 positive form in atom `phases/generate/zerops-yaml/comment-style.md` — "comments are single `#` prefixed, one line, no dividers"; P6 atomic — any decoration axis unowned by an atom is surfaced at atom review (owned = rule; unowned = rule-needed)
- **test_scenario**: given generated zerops.yaml files, when `grep -rP '[\x{2500}-\x{257F}]' */zerops.yaml` runs, then zero matches
- **calibration_bar**: `grep -rP '[\x{2500}-\x{257F}]' */zerops.yaml | wc -l == 0`

#### 13.5. `v33-pre-init-git-commit-sequencing`
- **origin_run**: v33 (main ran `ssh appdev "git add -A && git commit"` before post-scaffold `git init`; scaffolds had `rm -rf /var/www/.git/` per v8.96 Fix #4 → `fatal: not a git repository`)
- **class**: sequencing gap — `git-config-mount` block instructed pre-scaffold init but didn't carry post-scaffold "re-init before commit" note
- **mechanism_closed_in**: v8.104 Fix F — explicit sequencing in `git-config-mount` + scaffold-brief post-scaffold re-init rule
- **current_enforcement**: recipe.md git-config-mount block
- **new_enforcement**: atom `principles/git-workflow.md` positively declares sequencing "pre-scaffold init (mount setup) → framework scaffolder runs (with `--skip-git` or post-delete) → post-scaffold re-init before any commit"; P8 positive form
- **test_scenario**: given a recipe's post-scaffold git-first-commit call, when fired, then zero `fatal: not a git repository` errors
- **calibration_bar**: `0` `fatal: not a git repository` runtime errors per run (v33: 1; v34: 0)

#### 13.6. `v33-execonce-appversionid-seed-burn-recipe-pattern`
- **origin_run**: v33 (`zsc execOnce ${appVersionId} -- npx ts-node src/seed.ts` runs every deploy; only in-script `if (count > 0) return` short-circuit prevents duplicate rows — the Meili `addDocuments(...)` call lives inside the skipped branch → v33 apidev gotcha #7 shipped documenting a bug in our own recipe pattern)
- **class**: recipe-pattern bug shipped as user-facing gotcha; `${appVersionId}` changes per deploy so "once" semantics never engaged
- **mechanism_closed_in**: v8.104 Fix B — `zsc execOnce bootstrap-seed-v1 -- seed` (static key, once per service lifetime) separately from `zsc execOnce ${appVersionId} -- migrate` (per-deploy, idempotent by design)
- **current_enforcement**: recipe templates + scaffold-brief
- **new_enforcement**: atom `phases/generate/zerops-yaml/setup-rules-dev.md` + `setup-rules-prod.md` positively declare the key shapes; P1 runnable `grep` forbids `${appVersionId}` on seed, requires static key; P8 positive form
- **test_scenario**: given `{host}/zerops.yaml` dev + prod setups, when grep runs, then `execOnce bootstrap-seed-v1` present AND `execOnce \$\{appVersionId\}.*seed` absent
- **calibration_bar**: `0` occurrences of `execOnce \$\{appVersionId\}.*seed`; ≥1 occurrence of static seed key per recipe with a seeded service

#### 13.7. `v33-three-round-deploy-convergence-perturbs`
- **origin_run**: v33 (3-round deploy content-fix: fixing one check reshuffles similarity across READMEs and trips `cross_readme_gotcha_uniqueness` with NEW collision)
- **class**: check failure names what to fix on THIS surface but not which SIBLING checks will newly fail from the fix
- **mechanism_closed_in**: v8.104 Fix E — `PerturbsChecks []string` on `StepCheck` naming sibling checks whose pass state likely flips; **refuted by v34 data** (rounds regressed 3→4)
- **current_enforcement**: Fix E stamped on dedup + cross-README checks
- **new_enforcement**: **P1 supersedes** — author runs the ENTIRE runnable batch locally before attesting; perturbation is handled by running every shim, not by metadata
- **test_scenario**: given a writer draft, when author runs the batch of pre-attest shims (dedup + all content-quality), then any perturbed fail is surfaced BEFORE attestation
- **calibration_bar**: deploy-complete round count ≤ 2 (v34: 4); `PerturbsChecks` metadata may still be emitted but the architecture no longer relies on it for convergence

---

### v34 — convergence refuted, manifest inconsistency, cross-scaffold

#### 14.1. `v34-manifest-content-inconsistency`
- **origin_run**: v34 (workerdev DB_PASS gotcha shipped despite manifest routing fact self-inflicted → claude-md with override reason; `writer_manifest_honesty` covers only `(discarded, published_gotcha)` — missing `(routed_to_claude_md, published_gotcha)` dimension)
- **class**: writer classification machinery conceptually correct but enforcement surface is incomplete (single-direction honesty check misses 5 of 6 routing dimensions)
- **mechanism_closed_in**: **(carry — unaddressed in v34 mechanism releases)**; closed by new-architecture P5
- **current_enforcement**: `writer_manifest_honesty` covering `(discarded, published_gotcha)` only
- **new_enforcement**: P5 two-way graph — `writer_manifest_honesty` iterates ALL `(routed_to × surface)` pairs: `(discarded, gotcha)`, `(routed_to=claude_md, gotcha)`, `(routed_to=integration_guide, gotcha)`, `(routed_to=zerops_yaml_comment, gotcha)`, `(routed_to=env_comment, gotcha)`, `(routed_to=any, intro)` → expanded check emitted via shim `zcp check manifest-honesty --mount-root=./`; **editorial-review (refinement 2026-04-20) adds tertiary defense: `classification-reclassify.md` atom catches the class that P5 doesn't — when the CLASSIFICATION itself is wrong (writer calls fact platform-invariant when its observable behavior matches framework-quirk). P5 catches manifest↔content drift; editorial catches classification error at source. Layered enforcement: (1) writer self-classification + routing matrix, (2) P5 expanded manifest honesty at deploy.readmes + close.code-review, (3) editorial-review independent reclassification at close.editorial-review.**
- **test_scenario**: given `ZCP_CONTENT_MANIFEST.json` fact F with `routed_to=claude_md`, when grep F.title-tokens against `{host}/README.md#knowledge-base`, then zero matches (P5 catch); fact body appears under F.routed_to surface (P5 catch); AND editorial-review's reclassification of F agrees with writer's `classification=self-inflicted` (editorial catch for classification-error-at-source, see row 15.1)
- **calibration_bar**: `0` facts shipped as gotchas while manifest routes them elsewhere; `zcp check manifest-honesty` passes with all 6 routing dimensions covered; editorial-review `reclassification_delta == 0` (no writer-reviewer classification disagreements survive to export)

#### 14.2. `v34-cross-scaffold-env-var-coordination`
- **origin_run**: v34 (apidev scaffold decided `process.env.DB_PASS`; workerdev scaffold decided `process.env.DB_PASSWORD`; zerops.yaml maps `DB_PASS`; `.env.example` declared `DB_PASSWORD` — two-way mismatch → runtime crash + close-review WRONG #2)
- **class**: parallel scaffold sub-agents independently derive symbol names without shared contract; single-feature-subagent pattern prevents at feature-phase but not at scaffold-phase; same structural class as v22 NATS-URL-creds recurrence (v21 → v22 → v34)
- **mechanism_closed_in**: **(carry — caught downstream by close code-review in v34)**; closed by new-architecture P3
- **current_enforcement**: none structural; caught by close-step code-review subagent
- **new_enforcement**: P3 `SymbolContract` object — main agent computes once from `plan.Research` before first scaffold dispatch; every scaffold sub-agent receives identical JSON-interpolation (env-var names per managed-service kind, NATS subjects, HTTP routes, DTO names, hostname conventions); P1 runnable `ssh {hostname} 'env | sort'` diffed across codebases; brief MANDATORY "before returning: grep code for contract tokens, confirm each matches"
- **test_scenario**: given 3 codebases' scaffolded `app.module.ts` + `.env.example` + `zerops.yaml`, when env-var name tokens (DB_*, NATS_*, CACHE_*, STORAGE_*, SEARCH_*) are extracted and diffed across codebases, then zero mismatches
- **calibration_bar**: `0` env-var-naming mismatches at deploy runtime; `0` close-review WRONG findings of the form "codebase X reads VAR_A while codebase Y reads VAR_B"

#### 14.3. `v34-convergence-architecture-refuted`
- **origin_run**: v34 (Fix E shipped structurally, deploy rounds 3→4, finalize rounds 2→3; two generations of richer-failure-metadata (v8.96 Theme A + v8.104 Fix E) without round improvement)
- **class**: external-gate-after-writer architecture does NOT collapse rounds via metadata-on-failure; structural asymmetry "gates know what they read; authors know what they write"
- **mechanism_closed_in**: **(carry — v34 is the empirical refutation)**; closed by new-architecture P1
- **current_enforcement**: v8.96 Theme A + v8.104 Fix E (both refuted)
- **new_enforcement**: **P1 author-runnable pre-attest** — every check emits a shell form the author runs locally; gate becomes confirmation not discovery; failure payload shrinks to check-name + rerun-this-locally
- **test_scenario**: given a writer-draft draft, when author runs the full pre-attest batch BEFORE attesting, then first `complete deploy` passes ≥ 80% of the time
- **calibration_bar**: deploy-complete rounds ≤ 2; finalize rounds ≤ 1; **bar #0** (most-important single v35 measurement)

#### 14.4. `v34-self-referential-gotcha`
- **origin_run**: v34 (apidev `/api/status` returns 200 even when a managed service is down — don't point healthcheck at `/api/status`, use `/api/health`: genuine platform coupling but self-referential framing of recipe's OWN split)
- **class**: borderline self-referential decoration — real platform principle with framing about recipe's own feature-coverage namespace
- **mechanism_closed_in**: **(carry — editorial)**; closed by new-architecture P7 + editorial-review sub-agent (refinement 2026-04-20)
- **current_enforcement**: writer brief classification taxonomy
- **new_enforcement**: P7 cold-read at brief-review surfaces self-referential framing; P8 positive form "describe the platform mechanism in framework-agnostic terms; framework- or recipe-specific details go in parentheses"; **editorial-review (refinement) — reviewer's porter-premise catches self-referential framing because reviewer stance is "I am a porter bringing my own NestJS app; does `/api/status` vs `/api/health` make sense without knowing THIS recipe's feature-coverage split?" — answer: no → WRONG/CRIT via `single-question-tests.md` + `counter-example-reference.md` self-referential class**
- **test_scenario**: given a gotcha naming `/api/status` vs `/api/health`, when cold-read by editorial-review reviewer, then flagged as WRONG (or CRIT if the gotcha becomes meaningless on stripping recipe-specific names) with instruction to rewrite in framework-agnostic terms OR remove if it's pure recipe-implementation teaching
- **calibration_bar**: `0` wholly self-referential gotchas (where removing recipe-specific names leaves the gotcha meaningless); editorial-review WRONG finding count of this class `= 0` after inline-fix

---

### v20–v34 spec-compliance class (NEW via refinement 2026-04-20)

#### 15.1. `classification-error-at-source`
- **origin_run**: identified during 2026-04-20 research-refinement (conceptual defect class; observable in v28 33% genuine + v29 2/14 DISCARD override + v34 DB_PASS shipping); the registry row is new because prior enforcement (P5 expanded manifest honesty) catches MANIFEST-CONTENT drift but does NOT catch classification that is wrong at source
- **class**: writer classifies a fact incorrectly (e.g., self-inflicted misclassified as platform-invariant, framework-quirk misclassified as framework×platform). Manifest faithfully reflects the wrong classification. P5 honesty check reads "manifest says claude-md, content shipped as claude-md — consistent, PASS." But the underlying claim is wrong: classification should have been `discarded` (framework-quirk) or `claude_md` (self-inflicted) instead. All compliance checks pass; content ships wrong. Independent re-classification is the only mechanism that catches it.
- **mechanism_closed_in**: **(new via refinement 2026-04-20)** — editorial-review `classification-reclassify.md` atom independently re-runs the spec-content-surfaces.md 7-class taxonomy against every manifest fact; reports writer-vs-reviewer classification delta; CRIT/WRONG per class
- **current_enforcement**: none (v8.94 writer taxonomy is suggestion-strength; v8.95 Fix C manifest contract enforces consistency but not correctness; P5 expansion catches consistency violations across all 6 routing × surface dimensions but does not catch source errors)
- **new_enforcement**: editorial-review sub-agent dispatched at close.editorial-review; `classification-reclassify.md` atom instructs reviewer to independently apply the spec's 7-class taxonomy (platform-invariant / platform×framework / framework-quirk / library-metadata / scaffold-decision / operational / self-inflicted) to each manifest fact. Reports disagreements as `reclassification_delta`. Check `editorial_review_reclassification_delta == 0` gates pass.
- **test_scenario**: given `ZCP_CONTENT_MANIFEST.json` with fact F classified `platform-invariant` but its observable behavior matches the spec's framework-quirk test ("would a porter using this framework hit this regardless of where they deploy"), when editorial-review runs `classification-reclassify.md`, then reviewer reports fact F: writer=platform-invariant, reviewer=framework-quirk, delta=1 → WRONG or CRIT per `reporting-taxonomy.md`
- **calibration_bar**: `editorial_review_reclassification_delta == 0` at v35; if > 0, every delta item fixed inline (reclassified + routed per new classification + surfaces updated); v35 verdict: PAUSE if delta > 0 after inline-fix, ROLLBACK if reviewer's reclassification itself is wrong (meta-error — re-examine the spec + reviewer atoms)

---

### v35 — engine-level defects surfaced (showcase stuck on `writer_manifest_completeness`)

Engine/harness/knowledge-engine-layer defects, all pre-rollout — the v35 showcase exposed them because the C-7e..C-14 rollout drove the run deep enough into the deploy-check retry loop to surface them. Rows 16.1–16.6 are **not** atom or check-logic defects; they sit in the runtime layer the rollout did not touch. Full narrative + evidence: [`../runs/v35/analysis.md`](../runs/v35/analysis.md). Fix-stack handoff: [`../HANDOFF-to-I6.md`](../HANDOFF-to-I6.md).

#### 16.1. `v35-dispatch-brief-overflow`
- **origin_run**: v35 (`complete substep=feature-sweep-stage` at 08:15:21 returns 71,720-char response exceeding MCP tool-response token cap; harness spills to `/home/zerops/.claude/projects/-var-www/.../tool-results/mcp-zerops-zerops_workflow-1776759321598.txt`; main agent reads only first ~3 KB; dispatches writer-1 with a prompt that instructs the sub-agent to `python3 -c "print(open('…').read()[A:B])"` in 80,000-char slices to excavate the wire contract from the JSON blob)
- **class**: composed dispatch brief exceeds tool-response delivery envelope; harness spillover breaks the in-prompt composition pattern `DISPATCH.md` prescribes — `Build*DispatchBrief` composition is pure, but **delivery** loses fidelity because the main agent never sees the full payload
- **mechanism_closed_in**: **(open — identified v35)**
- **current_enforcement**: none; the 32 KB soft cap lives in the MCP tool-response handler but the composition pipeline has no size gate
- **new_enforcement**: candidate Cx-BRIEF-OVERFLOW — split the readmes-substep composed brief into (a) a short envelope that fits the tool-response cap + (b) a side-channel persisted atom reference the main agent embeds by path. Split boundary: `briefs/writer/` atoms move to on-disk paths; stitcher emits an envelope listing which atoms the dispatch brief composes. Main agent passes atom paths to sub-agent; sub-agent Reads each atom locally. Preserves DISPATCH.md's "atoms transit verbatim" principle while avoiding single-response overflow.
- **test_scenario**: given any showcase-tier dispatch brief for any substep, when `action=complete` returns the substep's dispatch-brief payload, then `result_size ≤ 32 KB` AND the main agent's next sub-agent-dispatch prompt contains the atom content (or resolvable atom references) the brief requires — without relying on spillover-file excavation
- **calibration_bar**: `max(zerops_workflow tool_result size)` across session ≤ 32 KB; zero dispatch prompts containing the literal string `tool-results/mcp-zerops` (anti-pattern — sub-agent told to read spillover file)

#### 16.2. `v35-check-detail-go-notation`
- **origin_run**: v35 (11 `writer_manifest_completeness` failures across iterations 0+1, each with detail `"manifest missing entries for 4 distinct FactRecord.Title values that appear in the facts log"`; `FactRecord` is a Go struct name, `Title` is a Go field name; the wire JSON key for the manifest entry is `fact_title`; main agent tried `Title` / `factRecord.Title` sub-object / uppercase-first-letter / 14 top-level aliases — never the actual `fact_title` key)
- **class**: check `Detail` strings reference wire contracts via Go type-field notation, misleading JSON-author readers (main agent, writer sub-agent) into shape-guessing rather than naming the JSON key directly
- **mechanism_closed_in**: **(open — identified v35)**
- **current_enforcement**: none; check authors hand-write `Detail` strings without a convention
- **new_enforcement**: candidate Cx-CHECK-WIRE-NOTATION — audit every `StepCheck.Detail` string in `internal/ops/checks/` + `internal/tools/workflow_checks_*.go`; rewrite any Go `Type.Field` reference to the corresponding JSON key with explicit quoting. Add lint rule to `recipe_atom_lint` (or a new `check_detail_lint`) that rejects `Detail` strings matching `[A-Z][A-Za-z]+\.[A-Z][A-Za-z]+` where the LHS is a known Go struct name from `internal/ops/checks/`. Gate↔shim invariant preserved — both paths surface the same corrected text.
- **test_scenario**: given any check implementation file under `internal/ops/checks/` or `internal/tools/workflow_checks_*.go`, when the test rig greps every `StepCheck{Detail: …}` literal for patterns `FactRecord\.`, `ContentManifestFact\.`, `StepCheck\.`, then zero matches; and for every `Detail` string that names a wire contract key, the JSON key appears in backticks or single quotes
- **calibration_bar**: zero occurrences of Go-struct-field dot-notation in check `Detail` strings; manual read-through of rewritten strings confirms every wire-contract field is named by its JSON key

#### 16.3. `v35-iterate-fake-pass`
- **origin_run**: v35 (`action=iterate` at 08:45:22 → `iteration:1`; between 08:46:26 and 08:47:50 main agent fires `action=complete` on all 12 deploy substeps in sequence with zero tool calls between them; each returns ~5070 B; engine accepts every attestation without evidence)
- **class**: `iterate` mechanism increments iteration counter but does not reset substep-completion state or require fresh evidence for each completion; main agent can walk the step graph top-to-bottom with empty attestations, which the engine accepts — short-circuits what should be re-work
- **mechanism_closed_in**: **(open — identified v35)**
- **current_enforcement**: none; engine-correctness invariant not in the invariants list
- **new_enforcement**: candidate Cx-ITERATE-GUARD — on `action=iterate`, reset all substep-completion markers for the current step to `pending` so the next `action=complete` pass must produce actual work. Alternative: require per-completion attestation to contain a reference to at least one tool call `tool_use_id` from the current iteration (server-side attestation-evidence check). First approach is simpler and preserves the step-graph invariant.
- **test_scenario**: given a recipe run mid-step with N substeps marked complete, when main agent calls `action=iterate`, then server response shows all N substep-completion markers reset to `pending`; and when main agent tries to call `action=complete` on any substep without new tool-call evidence since the iterate, server rejects with `MISSING_EVIDENCE` (or equivalent)
- **calibration_bar**: zero substep `action=complete` calls following an `action=iterate` within the same iteration without at least one non-`zerops_workflow` tool call between them

#### 16.4. `v35-skip-attempt-on-mandatory-step`
- **origin_run**: v35 (`action=skip step=deploy` at 08:49:05 after 11 deploy-check failures and 1 iterate cycle; engine correctly rejects with `INVALID_PARAMETER: "deploy" is mandatory and cannot be skipped`)
- **class**: retry-budget-exhaustion telemetry — main agent reaches for the skip mechanism on a mandatory step, a behavioral signal the retry loop has exhausted productive paths; engine refuses correctly but the signal itself is diagnostic of F-1/F-2 upstream
- **mechanism_closed_in**: **(open — identified v35; engine refusal correct)**
- **current_enforcement**: engine `INVALID_PARAMETER` rejection (correct behavior)
- **new_enforcement**: candidate Cx-SKIP-TELEMETRY — record skip-attempts on mandatory steps in the session log as a first-class event (not just an error result); rollback-criteria T-trigger can key on this as a retry-exhaustion signal. No engine behavior change; telemetry addition only.
- **test_scenario**: given a session log, when scanning for `zerops_workflow action=skip step=<deploy|generate|research|provision>`, then zero attempts per session (positive bar — never reaches this state); if > 0 attempts, session carries an `RETRY_EXHAUSTION` telemetry marker
- **calibration_bar**: zero `action=skip` attempts on mandatory steps per session; if > 0, automatic PAUSE verdict (not rollback — upstream defect F-1/F-2 is the actual problem)

#### 16.5. `v35-guidance-unknown-topic`
- **origin_run**: v35 (rows 31-33 at 07:29:50-51: `topic=dual-runtime-consumption` → `Error: unknown guidance topic`; `topic=client-code-observable-failure` → same; `topic=init-script-loud-failure` → `result_size=0` empty response — silent failure)
- **class**: main agent hallucinates plausible-sounding guidance topic IDs; registry returns one of (a) explicit unknown-topic error, (b) silent empty response; silent empty is the worse path because the main agent can't distinguish "no additional guidance" from "lookup miss"
- **mechanism_closed_in**: **(open — identified v35)**
- **current_enforcement**: unknown topics return an error string; empty topics return empty (no normalization)
- **new_enforcement**: candidate Cx-GUIDANCE-TOPIC-REGISTRY — (a) every `zerops_guidance` error response must include a top-3 nearest-match list from the valid-topic registry; (b) empty valid-topic lookups must return an explicit sentinel (`TOPIC_EMPTY`) rather than zero bytes; (c) the initial session briefing lists the full valid-topic ID set so the main agent has a closed universe to reference. Atoms under `briefs/guidance-topics/` remain source of truth; the registry lints them.
- **test_scenario**: given any session, when main agent calls `zerops_guidance topic=<id>`, then either (a) response is non-empty with actual guidance, or (b) response is an error with 3+ nearest-match topic IDs from the registry; zero silent-empty responses per session; session briefing payload contains the list of valid topic IDs
- **calibration_bar**: zero unknown-topic responses per session; zero zero-byte `zerops_guidance` responses per session

#### 16.6. `v35-knowledge-manifest-schema-miss`
- **origin_run**: v35 (row 161 at 08:45:58: `zerops_knowledge query="ZCP_CONTENT_MANIFEST.json schema writer_manifest_completeness"` returns top hit `decisions/choose-queue` with score 1 — completely unrelated; `manifest-contract.md` atom exists at `internal/content/workflows/recipe/briefs/writer/manifest-contract.md` but is not surfaced for this query)
- **class**: wire-contract atoms not indexed under obvious keyword queries in the knowledge engine; when main agent's paraphrased understanding fails and it falls back to `zerops_knowledge` for rescue, the engine returns irrelevant matches and confirms the dead-end
- **mechanism_closed_in**: **(open — identified v35)**
- **current_enforcement**: none; knowledge engine indexes all atoms but ranks by embedding similarity which scores poorly on short schema-keyword queries
- **new_enforcement**: candidate Cx-KNOWLEDGE-INDEX-MANIFEST — every wire-contract atom (manifest-contract, fragment-markers, completion-shape, routing-matrix, classification-taxonomy) gets explicit keyword synonyms indexed alongside its embedding; `ZCP_CONTENT_MANIFEST.json`, `manifest schema`, `fact_title`, `routed_to`, `writer_manifest_completeness`, `writer_manifest_honesty` all route to `manifest-contract.md` in top-3. Extend `zerops_knowledge` query handler to query synonyms alongside embeddings.
- **test_scenario**: given queries `ZCP_CONTENT_MANIFEST.json schema`, `writer_manifest_completeness`, `fact_title format`, `manifest routing`, when `zerops_knowledge` runs each, then `manifest-contract.md` appears in top 3 results for every query
- **calibration_bar**: for each of 5 canonical wire-contract atoms × 3 representative keyword queries each, atom appears in top 3 of `zerops_knowledge` results; test rig in `internal/knowledge/` verifies

#### 16.7. `v36-guidance-plan-nil-masquerade`
- **origin_run**: v36-attempt-1 (session `43814d9c5e09e85d` 2026-04-21 ~13:45 UTC): 4 consecutive `zerops_guidance` calls for `showcase-service-keys`, `dashboard-skeleton`, `worker-setup`, `recipe-types` at research step — all returned `"Topic %q does not apply to your recipe shape."` despite `tier=showcase` passed to `action=start`
- **class**: at research step `state.Recipe.Plan` is nil (only populated after `action=complete step=research`). The guidance handler ran predicates against nil; every predicate returns false for nil input; the emitted message ("does not apply to your recipe shape") conflates "shape mismatch post-research" with "plan not yet submitted at research step" — semantic collision
- **mechanism_closed_in**: Cx-PLAN-NIL-GUIDANCE (commit `c512757`, released as v8.108.1 2026-04-21 14:06 UTC)
- **current_enforcement**: Cx-PLAN-NIL-GUIDANCE: when `state.Recipe.Plan == nil` but `state.Recipe.Tier != ""`, a tier-only synthetic `&RecipePlan{Tier: recipeTier}` is built; tier-gated predicates (`isShowcase`) resolve correctly. Shape-dependent predicates (`hasWorker`, `isDualRuntime`) still return false against the synthetic; those topics surface a distinct "plan not yet submitted — submit via action=complete step=research first" message that tells the agent exactly when to retry
- **test_scenario**: given a recipe session started with `tier=showcase` and no `action=complete step=research` yet, when main agent calls `zerops_guidance topic=showcase-service-keys` (tier-only), then response contains substantial non-empty content, not "does not apply"; when main agent calls `zerops_guidance topic=worker-setup` (shape-dependent), then response contains "recipePlan" + "research" keywords distinguishing it from the shape-mismatch case
- **calibration_bar**: zero "does not apply to your recipe shape" responses for tier-only topics pre-research-complete per session; shape-dependent pre-research responses must contain the "submit recipePlan via action=complete step=research" message. Tests: `TestGuidanceTool_NilPlan_TierOnlyTopic_Resolves` + `TestGuidanceTool_NilPlan_ShapeTopic_ReturnsPlanNotSubmitted` in `internal/tools/guidance_topic_registry_test.go`

#### 16.8. `v36-close-step-gate-bypass-via-sessionless-export`
- **origin_run**: v36 (row 204 at 16:02:29 UTC: `zcp sync recipe export "/var/www/zcprecipator/nestjs-showcase" --app-dir ... --include-timeline` with no `--session` → tool response `"note: no session context (--session unset, $ZCP_SESSION_ID unset); skipping close-step gate."` → `.tar.gz` produced; close-step `action=complete step=close` never called; editorial-review + code-review + close-browser-walk never dispatched)
- **class**: `zcp sync recipe export` CLI treats absence of `--session` + `$ZCP_SESSION_ID` as opt-out from close-step gate rather than as an error. Agent can elect to force-wrap a recipe without exercising any close-phase verification. Workflow engine knows a session is live but the export CLI doesn't check; the two paths are independent and the CLI's permissive default wins
- **mechanism_closed_in**: **(open — identified v36)**
- **current_enforcement**: none; CLI emits the "skipping close-step gate" note and proceeds. The workflow engine has no mechanism to prevent the CLI from exporting — they share no state channel
- **new_enforcement**: candidate Cx-CLOSE-STEP-GATE-HARD — `zcp sync recipe export` detects when a live recipe session exists whose `OutputDir` matches the target directory (session state in `internal/workflow/`). When such a session exists AND `--session` is unset, exit non-zero with a message naming the session ID and instructing either to pass `--session=<id>` (which then gates on close-step complete) or to complete the close step first via `zerops_workflow action=complete step=close`. Alternative: export always requires prior close-step complete, no bypass
- **test_scenario**: given a directory with a live recipe session whose close step is `in_progress`, when `zcp sync recipe export <dir>` is invoked without `--session`, then exit code != 0 and stderr names the session ID + the remediation commands; given the same session after `action=complete step=close` runs, export with `--session=<id>` succeeds
- **calibration_bar**: zero `zcp sync recipe export` invocations per session that emit "skipping close-step gate" (measured by grep of main-session.jsonl); zero exports that produce a `.tar.gz` without a preceding `action=complete step=close` in the same session

#### 16.9. `v36-writer-brief-unbound-envfolders`
- **origin_run**: v36 (writer-1 dispatched at 15:24:20 UTC via [`flow-showcase-v36-dispatches/recipe-writer-sub-agent.md:95`](../runs/v36/flow-showcase-v36-dispatches/recipe-writer-sub-agent.md#L95); prompt lists paths `/var/www/environments/{dev-and-stage-hypercde,remote-cde-and-stage,local-validator,stage-only,small-prod,prod-ha}/README.md`; writer at [`flow-showcase-v36-sub-writer-1.md` row 40](../runs/v36/flow-showcase-v36-sub-writer-1.md) ran `mkdir -p` on all 6 slug dirs, wrote a README into each)
- **class**: writer brief atoms reference Go text/template variable `{{index .EnvFolders i}}` (see [`canonical-output-tree.md:18`](../../internal/content/workflows/recipe/briefs/writer/canonical-output-tree.md#L18)). Full-tree grep shows `.EnvFolders` is NEVER populated anywhere in Go. When the main agent stitches atoms from `dispatch-brief-atom` results, it must fill in the template holes; lacking a source, it hallucinates slug names. Writer obeys and creates ghost directories. Result: deliverable tree has 12 env dirs (6 canonical from finalize + 6 ghost from writer)
- **mechanism_closed_in**: **(open — identified v36)**
- **current_enforcement**: none; the template variable is used in atom text but unbound in Go. No lint catches unbound template fields in atoms
- **new_enforcement**: candidate Cx-ENVFOLDERS-WIRED — populate `.EnvFolders` at brief-atom render time from `envTiers[].Folder` in [`internal/workflow/recipe_templates.go`](../../internal/workflow/recipe_templates.go). Route through `handleDispatchBriefAtom` so returned atom bodies have concrete paths. PAIR with Cx-ATOM-TEMPLATE-LINT: build-time walker that extracts `{{.X}}` references from every atom and asserts X is in an allowed-fields set tied to actual Go render paths
- **test_scenario**: given a recipe session with tier=showcase, when writer sub-agent is dispatched and the dispatch prompt is captured, then the prompt contains the exact string `environments/0 — AI Agent/README.md` and does NOT contain either `{{` or any slug form like `dev-and-stage-hypercde`
- **calibration_bar**: B-15 ghost env dirs == 0 in deliverable tree; B-22 atom template vars all bound (lint); zero "dev-and-stage-hypercde" / "local-validator" / "prod-ha" / "remote-cde-and-stage" / "stage-only" strings in any writer dispatch prompt

#### 16.10. `v36-writer-output-stranded-by-sessionless-export`
- **origin_run**: v36 (writer-1 authored 12 files at `/var/www/{apidev,appdev,workerdev}/{README,CLAUDE,GOTCHAS,INTEGRATION-GUIDE}.md`; sessionless export at row 204 produced `.tar.gz` containing 0 per-codebase markdown files; user's exported deliverable tree has `apidev/` with only source code, no writer-authored markdown)
- **class**: writer sub-agent writes to `/var/www/{codebase}/*.md` per its canonical-output-tree atom. Writer does NOT commit its output. [`internal/sync/export.go:355`](../../internal/sync/export.go#L355) uses `git ls-files` to enumerate app-dir contents; uncommitted files are invisible; writer's output is stripped from every sessionless export. The engine's close step is supposed to stage (copy) writer content into the recipe output tree `/var/www/zcprecipator/{slug}/{codebase}/`, but close step never runs when export bypasses the gate. [`export.go:110`](../../internal/sync/export.go#L110) documents the problem literally: "exporting without close produces an incomplete deliverable (per-codebase READMEs + CLAUDE.md not staged, no code-review signals)"
- **mechanism_closed_in**: **(open — identified v36)**
- **current_enforcement**: none on the writer side (writer doesn't commit). None on the engine side (close-step staging action doesn't exist; only a documented warning in export.go)
- **new_enforcement**: candidate Cx-CLOSE-STEP-STAGING — add explicit `stageRecipeContent(state)` method to engine's `RecipeComplete(step="close")` flow. For each codebase in `plan.Targets` (runtime, non-shared-codebase-worker), copy `/var/www/{codebase}/{README.md,CLAUDE.md}` (post-Cx-STANDALONE-FILES-REMOVED list) into `/var/www/zcprecipator/{slug}/{codebase}/`. Fail close-step complete if any expected file is missing on disk. Optional git-commit of the staged files so the publish flow's git-based reads see them
- **test_scenario**: given a recipe session with writer output on `/var/www/{codebase}/*.md`, when `action=complete step=close` is called, then (a) files are copied to `/var/www/zcprecipator/{slug}/{codebase}/`, (b) close-step check passes, (c) sessioned export via `zcp sync recipe export --session=<id>` produces a `.tar.gz` containing the per-codebase markdown
- **calibration_bar**: B-16 tarball_per_codebase_md == (codebase_count × expected_files_per_codebase) for any session that reached close-complete; zero sessions where per-codebase writer output exists on source mount but not in exported `.tar.gz`

#### 16.11. `v36-extract-marker-wrong-form-in-brief`
- **origin_run**: v36 (writer-1 authored 3 codebase READMEs without fragment markers at all; first readmes-check round at 15:48:41 reported 9 failures `fragment_intro / fragment_integration-guide / fragment_knowledge-base — missing fragment markers`; writer-fix-pass at 15:49:41 performed 20 Edit calls touching markers across 3 files over ~7 minutes — 7 on appdev/README.md, 6 on apidev, 7 on workerdev — to add trailing `#` to marker forms)
- **class**: [`content-surface-contracts.md:71`](../../internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md#L71) documents markers as `<!-- #ZEROPS_EXTRACT_START:integration-guide -->` — MISSING trailing `#`. Canonical scaffold at [`recipe_templates_app.go:21-48`](../../internal/workflow/recipe_templates_app.go#L21) emits `<!-- #ZEROPS_EXTRACT_START:intro# -->` — WITH trailing `#`. Writer reads brief, writes wrong form. Check in [`internal/ops/checks/manifest.go:404+408`](../../internal/ops/checks/manifest.go#L404) uses `strings.Contains(line, "ZEROPS_EXTRACT_START:intro")` — substring match passes either form, so the check doesn't catch the defect; but separate `fragment_*` checks require the marker to be present at all, and when writer wrote them without the `#` the markers became unrecognizable to the marker-detector (or writer skipped them entirely — v36 data shows both classes, may be distinct defects)
- **mechanism_closed_in**: **(open — identified v36)**
- **current_enforcement**: the check's substring match is too loose; the atom's form is wrong. Two layers of the same defect
- **new_enforcement**: candidate Cx-MARKER-FORM-FIX — (a) update every writer atom showing the marker form to include the trailing `#`, (b) change [`manifest.go:404+408+458+462`](../../internal/ops/checks/manifest.go#L404) from `strings.Contains` substring check to an exact-form regex `regexp.MustCompile(\`<!-- #ZEROPS_EXTRACT_(START|END):(intro|integration-guide|knowledge-base)# -->\`)`; (c) add `fragment_marker_exact_form` check that fails on any README containing a marker missing the trailing `#`
- **test_scenario**: given a README with `<!-- #ZEROPS_EXTRACT_START:intro -->` (no trailing `#`), when the marker-form check runs, then the check fails with detail naming the file:line and the correct form. Given a README with `<!-- #ZEROPS_EXTRACT_START:intro# -->`, check passes
- **calibration_bar**: B-17 marker_exact_form == 0 failures across all per-codebase READMEs; zero writer-fix Edit cycles touching markers on subsequent runs (marker-form is correct first-pass)

#### 16.12. `v36-writer-atoms-prescribe-unneeded-standalone-files`
- **origin_run**: v36 (writer-1 authored 12 files: 3 README.md + 3 CLAUDE.md + 3 INTEGRATION-GUIDE.md + 3 GOTCHAS.md. Writer's output report lists all 12. The 6 standalone INTEGRATION-GUIDE.md + GOTCHAS.md files duplicate fragment content already inside README.md under the `<!-- #ZEROPS_EXTRACT_START:integration-guide# -->` and `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->` markers. Nothing in the publish pipeline consumes the standalones)
- **class**: [`canonical-output-tree.md:11-12`](../../internal/content/workflows/recipe/briefs/writer/canonical-output-tree.md#L11) lists `{h}/INTEGRATION-GUIDE.md` and `{h}/GOTCHAS.md` as standalone files writer should author. [`content-surface-contracts.md:63,81`](../../internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md#L63) frames the surfaces as "README fragment + standalone file". But the `+` is fictive — publish pipeline only reads fragment content from README, no consumer ever reads the standalones. User-confirmed design intent: fragments only. 6 dead files produced per showcase run
- **mechanism_closed_in**: **(open — identified v36)**
- **current_enforcement**: none; writer atoms prescribe the standalone authoring
- **new_enforcement**: candidate Cx-STANDALONE-FILES-REMOVED — (a) delete the `{h}/INTEGRATION-GUIDE.md` and `{h}/GOTCHAS.md` bullets from `canonical-output-tree.md`, (b) remove the "+ INTEGRATION-GUIDE.md" / "+ GOTCHAS.md" suffixes in `content-surface-contracts.md` sections 63 and 81 + surface-summary-table rows, (c) update `completion-shape.md` and `self-review-per-surface.md` to remove any standalone-file checks
- **test_scenario**: after writer sub-agent completion on a showcase run, `find /var/www/{codebase} -maxdepth 1 -name 'INTEGRATION-GUIDE.md' -o -name 'GOTCHAS.md' | wc -l == 0`. Writer authors README.md + CLAUDE.md only per codebase
- **calibration_bar**: B-18 standalone_duplicate_files == 0 in deliverable tree AND == 0 on source mount after writer completion

#### 16.13. `v36-writer-first-pass-compliance-failures`
- **origin_run**: v36 (writer-1 first-pass output on 3 READMEs triggered 9 failures at round 1: `fragment_intro × 3`, `fragment_integration-guide × 3`, `fragment_knowledge-base × 3`. Writer-fix-pass round 2 surfaced additional failures: `comment_ratio 0%` on YAML block, `fragment_*_blank_after_marker × 3`, `knowledge_base_gotchas` (missing H3), `intro_length` (8 lines vs spec 1-3), `app_integration_guide_per_item_code` (H3 IG items without fenced code blocks). Round 3 had 1 remaining: worker comment_ratio 27% < 30%. Round 4 clean. Total: 4 check rounds, 2 writer dispatches, 15+ distinct failure instances)
- **class**: writer sub-agent on v36 ignored or failed to comply with brief instructions across at least 5 distinct dimensions on a single dispatch: (a) fragment marker absence or wrong form (related to 16.11 but a separate writer-behavior layer), (b) missing `### Gotchas` H3 inside knowledge-base fragment despite `content-surface-contracts.md:93` explicit spec, (c) YAML block with 0% comment ratio despite brief calling for zerops.yaml mirror with comments, (d) intro fragment 8 lines vs spec 1-3, (e) integration-guide H3 items without fenced code blocks despite `content-surface-contracts.md:71` explicit spec. Whether this is a single-run anomaly or systemic signal ("writer brief too dense to obey reliably") is an open question post-v36
- **mechanism_closed_in**: **(open — identified v36; v37 determines whether single-run or systemic)**
- **current_enforcement**: brief atoms document the specs but don't enforce them pre-return. Writer self-review-per-surface atom exists but the self-review is itself writer-authored and inherits the writer's blind spots. Checks at manifest.go catch the violations after writer returns, forcing fix-pass dispatches
- **new_enforcement**: if v37 reproduces the pattern (≥ 3 first-pass compliance failures), candidate Cx-WRITER-BRIEF-DECOMPOSITION — split the writer brief into smaller per-file authoring dispatches, OR sharpen self-review-per-surface atom to run concrete checks (grep for fragment markers, count H3 items, count fenced blocks) before return. If v37 shows ≤ 2 first-pass failures, treat v36 as noise and annotate this row as single-run anomaly
- **test_scenario**: given writer sub-agent dispatched on a showcase run, when writer returns and first `complete step=deploy substep=readmes` runs, then `len(failing_checks) ≤ 3` across all codebases. Distinct check failure classes: fragment markers (present + exact form), `### Gotchas` heading, comment_ratio ≥ 30%, intro length 1-3 lines, IG items with code blocks
- **calibration_bar**: B-23 writer_first_pass_failures ≤ 3; B-20 deploy_readmes_retry_rounds ≤ 2

---

## 2. Registry coverage audit

### 2.1. Every v20–v34 closed defect class has a row

Walked the milestones-and-regressions table (lines 354–384 of `recipe-version-log.md`) + every per-version entry v20 through v34 + v8.78–v8.104 release notes. Every class named as `closed / regressed / surfaced / fixed-in v8.XX` has a registry row (§1). Counts:

| Origin run | Rows | Notes |
|---|---:|---|
| v6–v19 (lineage seeds) | 11 | Selected: only classes still referenced by v20+ fixes or principles |
| v20 | 6 | Content-reform origin; v8.79 five-check suite + finalize + predecessor-rollback |
| v21 | 6 | v8.80 reform + dead-regex + framework-token purge + delegation gate |
| v22 | 6 | v8.81 recurrence preambles + gotchas-as-incident + cross-codebase |
| v23 | 4 | Convergence spiral + folk-doctrine + MCP misattribution + shallow fix |
| v25 | 3 | v8.90 state-coherence (substep-bypass + workflow-at-spawn + editorial) |
| v26 | 2 | v8.93.1/2 (stringification + zcp-side chown) |
| v28 | 4 | Check-surface gap + wrong-surface + folk-doctrine + cross-surface dup |
| v29 | 4 | v8.95 scaffold-leak + env-0 fabrication + Go-template drift + DISCARD-override |
| v30 | 2 | Worker-SIGTERM missing + .DS_Store gitignore drift |
| v31 | 8 | v8.96 Theme A + Theme B + 4 quality fixes (knowledge/port-kill/read-before-edit/git-lock) + enableShutdownHooks subclass |
| v32 | 5 | v8.97 close-gate + overlay + dispatch-compression + stamp-coupling + platform-principles |
| v33 | 7 | v8.104 Fix A/B/C/D/E/F — phantom tree + seed + visual-style + probe cadence + perturbs + pre-init git |
| v34 | 4 | Manifest-content-inconsistency + cross-scaffold + convergence-refuted + self-referential |
| (refinement 2026-04-20) | 1 | classification-error-at-source (NEW row 15.1; identified during editorial-review scoping; conceptual class observable retroactively in v28/v29/v34 that prior enforcement doesn't catch) |
| **Total** | **69** | Target ≥ 30 exceeded by ~2.3× |

### 2.2. Every test_scenario is expressible without current-system Go code references

Every `test_scenario` field in §1 uses shell-level / data-level predicates (grep, awk, yq, jq, `find`, `wc -l`, `ssh`, session-log parse) and could be re-run against a v2 system that has a different internal Go structure. Substrate hooks (e.g. `SUBAGENT_MISUSE`, `validateDBDriver`, `dev_server` spawn shape) appear in the `current_enforcement` column only — the scenario describes observable behavior.

### 2.3. Every calibration_bar is numeric / grep-verifiable

Every `calibration_bar` field is either a zero-floor count, a ratio with explicit threshold, a `wc -l`/`grep -c` expression, or an `≤ N` / `≥ N` measurable. No qualitative "looks good" / "mostly clean" / "acceptable" thresholds.

### 2.4. Defect classes NOT included (and why)

- **v7–v19 run-specific incidents** (e.g. v9 worker migration sequencing, v11 worker entity mismatch, v15 specific WRONG findings) not included when they (a) are one-off scaffold bugs with no recurrence class, (b) predate the v20 content-reform lineage, and (c) have no v8.78+ mechanism fix. Their closure is by substrate (feature-subagent single-author pattern, v17 `zerops_dev_server`, etc.).
- **Close-step STYLE findings** — each run ships 1–5 STYLE items in close review; not defect classes, acceptable recipe-quality items.
- **Substrate-layer bugs** (`dev_server` spawn, git-config-mount, zcli setup propagation) — closed by v17.1/v8.93/v8.85; substrate stays per README §1. Rows appear where the class has a sub-agent-brief dimension (e.g. v17 sshfs-write-not-exec, v26 git-init-zcp-side-chown).

---

## 3. Principle-to-row coverage

Every principle P1–P8 ([`../03-architecture/principles.md`](../03-architecture/principles.md)) is invoked by ≥1 registry row. Reverse coverage:

| Principle | Representative rows |
|---|---|
| **P1** author-runnable pre-attest | 1.2, 1.5, 1.6, 2.1–2.6, 3.1, 3.2, 4.3, 5.1, 6.3, 8.1, 9.1, 9.2, 9.3, 10.1, 11.1–11.4, 11.6, 11.7, 13.4, 13.5, 13.6, 14.3 |
| **P2** leaf-artifact brief | 1.3, 1.8, 4.1, 4.2, 11.7, 12.3, 12.5, 13.3 |
| **P3** SymbolContract | 1.6, 1.10, 3.1, 4.1, 4.2, 10.1, 10.2, 11.1, 12.5, 14.2 |
| **P4** server state = plan | 1.9, 3.6, 6.1, 12.1, 12.2, 13.2 |
| **P5** two-way graph | 8.2, 8.4, 9.4, 11.4, 14.1 |
| **P6** atomic guidance | 1.1, 3.3, 13.4 |
| **P7** cold-read + defect-coverage | 1.1, 4.5, 5.4, 8.3, 14.4, **15.1** (P7 institutionalized at runtime via editorial-review sub-agent per refinement 2026-04-20); **editorial-review extends P7 to every v35+ run, not just pre-merge review — rows 8.2, 8.3, 14.1, 14.4, 15.1 gain editorial-review as secondary/tertiary enforcement** |
| **P8** positive allow-list | 1.7, 1.8, 2.1, 2.2, 2.4, 3.4, 4.1, 4.2, 5.3, 6.2, 7.2, 10.2, 11.7, 13.1, 13.2, 13.3, 13.4, 13.5, 13.6, 14.4 |

No registry row fails to map to ≥1 principle. Three rows carry all-four-axis coverage (v33 phantom tree = P2+P6+P8 structural, v34 manifest inconsistency = P5 primary, v25 substep-bypass = P4+P6 structural) — defense-in-depth, not redundancy.

---

## 4. Open follow-ups (for step-4 cold-read simulation + step-6 migration)

1. **Rows marked `(carry)` in `mechanism_closed_in`** (v22 coherence, v25 app-static, v27 shallow fix, v30 DS-Store pre-v8.97, v34 manifest/cross-scaffold/convergence/self-referential): the new architecture's principle coverage exists but the mechanism hasn't shipped as code. Step-6 migration path must name which `(carry)` rows become part of the v35 cleanroom vs. parallel-run scope.
2. **Rows where v31/v33/v34 shipped a mechanism that the v34 data refuted** (4.4 post-writer-iteration, 11.2 deploy-3-round, 11.3 finalize-3-round, 13.7 perturbs): all have **P1 supersedes** as new_enforcement. Step-4 composed briefs must carry runnable-pre-attest blocks for every content check; step-5 (this file) declares the calibration bar; step-6 decides whether to delete the refuted Theme-A/Fix-E metadata (conservative: keep for human-readable debugging, stop relying for convergence).
3. **Rows flagged as editorial** (1.11 two-axis conflation, 6.3 v25 app-static, 8.4 cross-surface dup): editorial fixes get positive-form atom text in the new architecture; no new checks shipped. Calibration bars are soft (grep-verifiable but not gate).

---

## 5. Using this file downstream

- **Step 4 cold-read**: every `brief-<role>-<tier>-coverage.md` (per README §3 step 4) cross-lists every row in §1 × columns = "prevention mechanism cited in composed brief / runnable check / Go injection". Empty cell = brief not merged.
- **Step 6 migration**: rollback-criteria measures v35 against `calibration_bar` column. Any regression against a `calibration_bar` threshold triggers rollback evaluation.
- **v35 post-mortem** (if run ships): every new defect class surfaced by v35 earns a row; rows whose `calibration_bar` held across v35 + 1 subsequent run graduate to `current_enforcement` status for zcprecipator2's own calibration bar set (see [`../runs/v35/calibration-bars.md`](../runs/v35/calibration-bars.md); per-run snapshots under `../runs/vN/` for subsequent runs).
