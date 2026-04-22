# runs/v38 verification checklist

**Machine report SHA**: `d756a9726cdd4f03b11994b745af47f3154cd47f6943cc0f20a6d681909bcca2`
**Generated at**: 2026-04-22T12:46:19Z
**Tier**: showcase
**Slug**: nestjs-showcase
**Deliverable**: `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38`
**Analyst**: Claude Opus 4.7 (1M context) on behalf of Ales Rechtorik
**Analyst session start (UTC)**: 2026-04-22T14:46Z

## Phase reached

- [x] `close` complete (auto; engine response at main-session.jsonl L853 confirms `progress.steps[close].status=complete`)
- [x] editorial-review dispatched (auto; agent-afa52e6ec9350b1b3 at main-session L753, description "Editorial review of recipe reader-facing content")
- [x] code-review dispatched (auto; agent-a82b80e29e0ccd617 at main-session L728, description "Code review of recipe scaffold + features")
- [ ] close-browser-walk attempted (auto=false; TIMELINE.md reports close-browser-walk was attempted and agent-browser returned; harness does not detect because no specific browser substep marker — classify `unmeasurable-valid` environmental-soft-pass per HANDOFF-to-I9 §5 "close-browser-walk (soft-pass acceptable ONLY if a new failure mode surfaces)")

Close IS complete per engine response; all downstream cells are measurable.

## Structural integrity bars (auto)

- [x] B-15 ghost_env_dirs: threshold 0, observed 0, **status pass** — canonical 6 env folders present; no `environments-generated/` or other sibling anomalies. F-9 CLOSED (vs v37 observed=6 at `environments-generated/`).
- [x] B-16 tarball_per_codebase_md: threshold 6, observed 6, **status pass** — every codebase has README.md + CLAUDE.md. Cx-CLOSE-STEP-STAGING held.
- [x] B-17 marker_exact_form: threshold 0, observed 0, **status pass** — all 9 fragment markers carry trailing `#` across 3 codebases. F-12 stays CLOSED. Cx-2 SCAFFOLD-FRAGMENT-FRAMES held at marker-form level.
- [x] B-18 standalone_duplicate_files: threshold 0, observed 0, **status pass** — zero INTEGRATION-GUIDE.md or GOTCHAS.md files on disk. F-13 CLOSED (vs v37 observed=6).
- [x] B-22 atom_template_vars_bound: threshold 0, observed 0, **status pass** — build-time lint clean at HEAD.

## Session-metric bars (auto)

- [x] B-20 deploy_readmes_retry_rounds: threshold 2, observed 2, **status pass (at threshold)** — cycles 15 (10:29:49Z) + 16 (10:35:26Z) per [machine-report.session_metrics.B-20.evidence_files]. v37 was 5 rounds; v38 converged faster but writer first-pass still failed content compliance.
- [x] B-21 sessionless_export_attempts: threshold 0, observed 0, **status pass** — Cx-8 harness-v2 post-close filter held; 2 post-close exports at 12:30Z + 12:32Z (L903/L912) correctly excluded by `close_step_completed_at` correlation.
- [x] B-23 writer_first_pass_failures: threshold 3, observed 9, **status fail** — evidence: api_comment_specificity, comment_ratio, fragment_integration-guide_blank_after_marker, fragment_intro_blank_after_marker, fragment_knowledge-base_blank_after_marker, intro_length, worker_drain_code_block, writer_manifest_honesty_any_as_intro, writer_manifest_honesty_claude_md_as_gotcha. Same count as v37's 9. Cx-2 pre-scaffold markers didn't prevent `_blank_after_marker` failures — writer still produced blank-line-after-marker violations on first pass.
- [x] B-24 dispatch_integrity: threshold 0, observed 3, **status fail** — all three guarded dispatches diverge from engine-built briefs (analyst-populated below under "Dispatch integrity"). Writer 4-byte `\u2014`→em-dash paraphrase; code-review 1-byte trailing-newline; editorial-review 33,976-byte 72%-compression paraphrase. Root cause: Cx-5 guard is opt-in (`verify-subagent-dispatch`) and main agent never invoked it; no PreToolUse hook wires Agent interception. Per primer: "Any diff = PAUSE".
- sub_agent_count: 7 (writer, editorial-review, code-review, feature, scaffold-apidev, scaffold-appdev, scaffold-workerdev); v37 was 13 — absence of writer-fix/editorial-re-dispatch cycles indicates first-pass content closer to target.

## Dispatch integrity (analyst-fill per Rule 5)

Byte-diff each captured Agent dispatch prompt against `BuildSubagentBrief(plan, role, factsLog, manifest)` reconstruction. Engine-returned prompts preserved at `dispatch-integrity/engine-*.txt`; transmitted prompts at `dispatch-integrity/dispatch-*.txt`. Scratch byte-diff test run once at analyst-time against HEAD atoms (v8.112.0 + docs commit 30c5bc5); test file removed after capture per operating rule 3.

- **Author recipe READMEs + CLAUDE.md + manifest** (writer, L609):
  - [x] dispatch_vs_source_diff: **Status: divergent (encoding-class)** — engine built 60871 bytes, dispatched 60867 bytes, delta -4 bytes at byte 60513. Engine brief preserves atom source's `\u2014` literal (6 chars teaching to AVOID the Unicode escape); dispatched prompt has `—` (1 em-dash, 3 UTF-8 bytes). All 60,513 preceding + 354 trailing bytes byte-identical. SHAs: engine `376970ac...728fb26` vs dispatched `a493890e...b969bcb`. See [dispatch-integrity/engine-writer.txt] vs [dispatch-integrity/dispatch-writer.txt].
  - Root cause: main agent silently JSON-decoded `\u2014` to an em-dash character before forwarding. Not semantic paraphrase; still breaks strict byte-identity + SHA check.
  - [x] Read-receipt: 2026-04-22T14:55Z
- **Editorial review of recipe reader-facing content** (editorial-review, L753):
  - [x] dispatch_vs_source_diff: **Status: divergent (semantic)** — engine built 47205 bytes, dispatched 13229 bytes, delta -33,976 bytes (**-72%**). First divergence at byte 6988: engine has "### Minimal tier" + 4 bullet lines; dispatched has "### Showcase tier" + 1 line. Main agent dropped sections wholesale: entire "Minimal tier" block, all 6 "Pass/Fail" explainer paragraphs (one per surface), "Why independent" preamble, full "The seven classes" elaboration (32 lines replaced with one line "The seven classes: platform-invariant, platform-×-framework intersection, framework-quirk, library-metadata, scaffold-decision, operational, self-inflicted."), "Disposition on fail" paragraph. Added a "Recipe output root: ..." line not in the engine brief. See [dispatch-integrity/diff-editorial-review.txt] for the 460-line unified diff.
  - Root cause: main agent paraphrased the engine-built brief the same way v37's main agent paraphrased raw atoms — the F-17 failure mode survived Cx-5 because the guard is opt-in (not auto-enforced via PreToolUse). engineSHA `1b63737b...285a3bf3` / dispatchedSHA `54e20167...039474d0`.
  - [x] Read-receipt: 2026-04-22T14:55Z
- **Code review of recipe scaffold + features** (code-review, L728):
  - [x] dispatch_vs_source_diff: **Status: divergent (trivially — trailing-newline)** — engine 17657 bytes, dispatched 17656 bytes, delta -1 byte (missing trailing `\n`). Pre-final 17656 bytes byte-identical. SHAs differ but content is equivalent. See [dispatch-integrity/engine-code-review.txt] vs [dispatch-integrity/dispatch-code-review.txt].
  - Root cause: standard tool-input trimming. Not a semantic paraphrase. Still fails strict SHA check.
  - [x] Read-receipt: 2026-04-22T14:55Z
- **Build showcase feature sections end-to-end** (feature, L270):
  - [x] dispatch_vs_source_diff: **Status: unmeasurable-valid** — feature is not a guarded role. Dispatch-brief for features is built ad-hoc per plan; no engine-side canonical brief to compare against. Captured prompt size 12329 chars, references valid service hostnames and features per plan.
  - [x] Read-receipt: 2026-04-22T14:58Z
- **Scaffold NestJS API codebase** (scaffold-apidev, L104):
  - [x] dispatch_vs_source_diff: **Status: unmeasurable-valid** — scaffold dispatches are plan-specific, not guarded by Cx-5. Captured 14646 chars; deploy reached ACTIVE → structural correctness confirmed by downstream evidence.
  - [x] Read-receipt: 2026-04-22T14:58Z
- **Scaffold NestJS worker codebase** (scaffold-workerdev, L109):
  - [x] dispatch_vs_source_diff: **Status: unmeasurable-valid** — same as scaffold-apidev. Captured 8200 chars; deploy green.
  - [x] Read-receipt: 2026-04-22T14:58Z
- **Scaffold Svelte+Vite frontend** (scaffold-appdev, L107):
  - [x] dispatch_vs_source_diff: **Status: unmeasurable-valid** — same as scaffold-apidev. Captured 9345 chars; deploy green.
  - [x] Read-receipt: 2026-04-22T14:58Z

## Writer content quality (analyst-fill, required)

### apidev/README.md

- **intro fragment** — auto markers_present=true exact_form=true h3=0 bullets=0: **status pass**
  - [x] Analyst qualitative grade: **pass** — [apidev/README.md:3-5](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/apidev/README.md) 3-line substantive intro naming runtime (NestJS 11) + 5 managed services (Postgres, Valkey, NATS, Object Storage, Meilisearch) + mechanism (zsc execOnce, auto-injected env vars).
- **integration-guide fragment** — auto markers_present=true exact_form=true h3=5 bullets=0: **status pass**
  - [x] Analyst qualitative grade: **pass** — [apidev/README.md:8-310](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/apidev/README.md) 5 H3 items: (1) Adding zerops.yaml with heavily-commented full YAML, (2) Bind 0.0.0.0 + trust proxy, (3) Run migrations + seed via zsc execOnce, (4) Read managed-service credentials without self-shadow, (5) forcePathStyle on S3. Every H3 has fenced code. Principle-level, citations to platform topics (http-support, init-commands, env-var-model, object-storage).
- **knowledge-base fragment** — auto markers_present=true exact_form=true h3=0 bullets=3: **status pass**
  - [x] Analyst qualitative grade: **pass** — [apidev/README.md:314-322](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/apidev/README.md) 3 gotchas: bucket-policy-on-boot, self-shadow-silent-blank, master-key-vs-search-key. Each concrete symptom + platform-topic citation. Cross-reference to workerdev for NATS gotchas (avoids cross-readme uniqueness conflict).
- [x] Read-receipt: 2026-04-22T14:50Z

### appdev/README.md

- **intro fragment** — auto pass
  - [x] Analyst qualitative grade: **pass** — [appdev/README.md:3-5](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/appdev/README.md) 3-line intro: Svelte 5 + Vite 8 + dev port 5173 + prod as Nginx with SPA fallback.
- **integration-guide fragment** — auto pass (h3=4)
  - [x] Analyst qualitative grade: **pass** — [appdev/README.md:10-158](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/appdev/README.md) 4 H3: (1) Adding zerops.yaml with dev/prod split + tilde-strip + build/run envVariables commentary, (2) Bind 0.0.0.0 + allowedHosts for Vite, (3) VITE_API_URL build-vs-run split, (4) dist/~ tilde. Every H3 has fenced code. Citations: http-support, env-var-model, deploy-files.
- **knowledge-base fragment** — auto pass (gotcha_bullet_count=4)
  - [x] Analyst qualitative grade: **pass** — [appdev/README.md:165-168](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/appdev/README.md) 4 gotchas: Blocked-request CSRF, `dist/` 404 without tilde, VITE_API_URL wrong-side-of-split, dev-container 502 before SSH-driven dev. Each cites platform topic (http-support, deploy-files, static-runtime, env-var-model) with concrete symptom.
- [x] Read-receipt: 2026-04-22T14:51Z

### workerdev/README.md

- **intro fragment** — auto pass
  - [x] Analyst qualitative grade: **pass** — [workerdev/README.md:3-5](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/workerdev/README.md) 3-line intro naming the shape (NestJS NATS consumer, no HTTP listener, no ports).
- **integration-guide fragment** — auto pass (h3=3)
  - [x] Analyst qualitative grade: **pass** — [workerdev/README.md:10-184](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/workerdev/README.md) 3 H3: (1) Adding zerops.yaml (worker-shape, no ports/readiness/healthcheck, liveness=process-stays-alive), (2) Queue-group binding semantics, (3) SIGTERM drain with two shapes (raw-signal + NestJS OnApplicationShutdown). Every H3 has fenced code.
- **knowledge-base fragment** — auto pass (gotcha_bullet_count=3)
  - [x] Analyst qualitative grade: **pass** — [workerdev/README.md:191-193](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/workerdev/README.md) 3 gotchas: duplicate-processing-without-queue-group, AUTHORIZATION_VIOLATION-from-URL-creds, SIGTERM-drops-handlers-without-enableShutdownHooks. Both showcase-required supplements present (queue-group semantics + SIGTERM drain). Citations: rolling-deploys ×2, env-var-model. Cross-reference to apidev/CLAUDE.md for NATS subject contract.
- [x] Read-receipt: 2026-04-22T14:52Z

### apidev/CLAUDE.md

- [x] size ≥ 1200 bytes: true (observed 5439 bytes)
- [ ] base sections present: auto report [Migrations Testing] (want 4) **HARNESS FALSE POSITIVE** — file actually has all four ("Dev loop" at :8, "Migrations and seed" at :24, "Container traps" at :52, "Testing" at :75). Harness case-sensitive match failed on "Dev Loop"/"Container Traps" vs lowercase. Not a writer regression; file a B-16-plus case-insensitivity fix.
- [x] custom sections ≥ 2: true (observed 4)
- auto **status fail** (harness false positive)
- [x] Analyst narrative sign-off: **pass** — operator-focused content; Dev loop describes SSH-driven hot reload + dev subdomain pattern; Migrations names execOnce recovery paths (touch-file to rotate appVersionId, static seed key); Container traps identifies 4 concrete repo-specific traps (no .env, synchronize false, idempotent initCommands, dev-setup initCommands); Testing covers feature-sweep curl paths. Plus 2 custom sections (cross-codebase contracts with NATS subject + schema ownership; credentials lookup one-liner).
- [x] Read-receipt: 2026-04-22T14:53Z

### appdev/CLAUDE.md

- [x] size ≥ 1200 bytes: true (observed 3893 bytes)
- [ ] base sections present: auto report [Migrations Testing] (want 4) **HARNESS FALSE POSITIVE** — all four present ("Dev loop" at :8, "Migrations and seed" at :26, "Container traps" at :40, "Testing" at :58). Same case-sensitivity miss.
- [x] custom sections ≥ 2: true (observed 4)
- auto **status fail** (harness false positive)
- [x] Analyst narrative sign-off: **pass** — [appdev/CLAUDE.md:1-99] Svelte/Vite operator notes: Dev loop names Vite port 5173 + allowedHosts + HMR over L7 balancer; Migrations correctly notes frontend has no storage + points at apidev/TRUNCATE; Container traps covers VITE_API_URL build-vs-run + port vs server.host + static-runtime is Nginx; Testing lists 6 data-testid feature contracts. 2 custom sections: Svelte 5 onclick + agent-browser CDP quirk, rebuild/preview on the dev container.
- [x] Read-receipt: 2026-04-22T14:53Z

### workerdev/CLAUDE.md

- [x] size ≥ 1200 bytes: true (observed 4793 bytes)
- [ ] base sections present: auto report [Migrations Testing] (want 4) **HARNESS FALSE POSITIVE** — all four present ("Dev loop" at :9, "Migrations and seed" at :34, "Container traps" at :47, "Testing" at :67).
- [x] custom sections ≥ 2: true (observed 4)
- auto **status fail** (harness false positive)
- [x] Analyst narrative sign-off: **pass** — [workerdev/CLAUDE.md:1-118] worker-specific: Dev loop names the stale-child recovery pattern (ps aux | grep); Migrations correctly notes worker doesn't own schema (cross-codebase pointer to apidev); Container traps covers no-port semantics + queue-group contract + credentials-not-url; Testing routes through the API's jobs endpoint. Custom: cross-codebase contracts (NATS subject, queue group, jobs table) + recovery-from-ghost-subscriber procedure.
- [x] Read-receipt: 2026-04-22T14:54Z

## Env README quality (spot check)

- [x] `0 — AI Agent/README.md` (42 lines): **pass** — [env 0/README.md:10-42](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/environments/0%20%E2%80%94%20AI%20Agent/README.md) Who-this-is-for, First-tier context, Promotion path (to 1 — Remote), Tier-specific operational concerns all present; correctly states `initCommands` do NOT fire automatically at this tier (the key AI-Agent distinction). Intro fragment markers present.
- [x] Read-receipt: 2026-04-22T14:56Z

## Env import.yaml quality (spot check)

- [x] `0 — AI Agent/import.yaml` (140 lines): **pass** — [env 0/import.yaml:1-50](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/environments/0%20%E2%80%94%20AI%20Agent/import.yaml) `#zeropsPreprocessor=on` first line; project comment explains APP_SECRET balancer-shared-session constraint + DEV_/STAGE_ URL constants; each service comment (appdev, apidev, workerdev, db, redis, queue, storage, search) carries 3-5 lines of decision-why (not field narration). F-21 status: first finalize pass failed on 6 × factual_claims for invented quota numbers ("2 GB"), agent re-ran with qualitative phrasing ("modest quota suitable for agent iteration") per retry-cycle attribution below, final result passes — Cx-3 check enforcement works; atom prevention did not.
- [x] Read-receipt: 2026-04-22T14:57Z

## Manifest integrity

- [x] `ZCP_CONTENT_MANIFEST.json`: **ABSENT FROM DELIVERABLE — F-23 NOT CLOSED** — Writer authored at `/var/www/zcprecipator/nestjs-showcase/ZCP_CONTENT_MANIFEST.json` + `/var/www/ZCP_CONTENT_MANIFEST.json` (agent-a62e64... subagent log, 4 Write/Edit operations timestamped 10:26-10:27Z). `zerops_workflow action=generate-finalize` tool-result at [main-session.jsonl L<toolu_01GcSKXWTCs23Q47iSE2TVbf> 10:39:51Z] confirms `ZCP_CONTENT_MANIFEST.json` in the 17-file finalize output list — Cx-4 MANIFEST-OVERLAY staged it into the recipe output directory. BUT `zcp sync recipe export` at [internal/sync/export.go:236](../../../internal/sync/export.go#L236) whitelists only `TIMELINE.md` and `README.md` as root-level files; the manifest is dropped during tarball creation. The extracted deliverable tree has no manifest. Cx-4 fix is **partial** — overlay landed, export whitelist was not extended. `find /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/ -name "*MANIFEST*"` returns 0 results.
- [x] Read-receipt: 2026-04-22T14:48Z

## Retry-cycle attribution (analyst-fill)

| Cycle | Timestamp (UTC) | Substep | Failing checks | Attribution |
|---|---|---|---|---|
| 1 | 2026-04-22T09:01:41Z | research/ | none | phase completion only |
| 2 | 2026-04-22T09:04:29Z | provision/ | none | phase completion only |
| 3 | 2026-04-22T09:25:07Z | deploy/deploy-dev | none | phase completion only |
| 4 | 2026-04-22T09:26:14Z | deploy/start-processes | none | phase completion only |
| 5 | 2026-04-22T09:26:43Z | deploy/verify-dev | none | phase completion only |
| 6 | 2026-04-22T09:30:00Z | deploy/init-commands | none | phase completion only |
| 7 | 2026-04-22T09:46:30Z | deploy/subagent | none | feature sub-agent complete |
| 8 | 2026-04-22T09:49:22Z | deploy/snapshot-dev | none | phase completion only |
| 9 | 2026-04-22T09:50:07Z | deploy/feature-sweep-dev | none | feature sweep pass |
| 10 | 2026-04-22T10:05:25Z | deploy/browser-walk | none | dev browser walk pass |
| 11 | 2026-04-22T10:11:38Z | deploy/cross-deploy | none | phase completion only |
| 12 | 2026-04-22T10:11:47Z | deploy/verify-stage | none | phase completion only |
| 13 | 2026-04-22T10:12:40Z | deploy/feature-sweep-stage | none | stage feature sweep pass |
| 14 | 2026-04-22T10:29:36Z | deploy/readmes | none | writer sub-agent dispatched, returned |
| 15 | 2026-04-22T10:29:49Z | deploy/ | comment_ratio, fragment_integration-guide_blank_after_marker, fragment_intro_blank_after_marker, fragment_knowledge-base_blank_after_marker | F-14 writer-compliance: writer left blank lines between marker pairs and first heading; Cx-2 pre-scaffold markers did not prevent blank_after_marker class failures on first pass (pre-scaffold markers are present but placeholder line needs no blank gap either side). |
| 16 | 2026-04-22T10:35:26Z | deploy/ | api_comment_specificity, comment_ratio, worker_drain_code_block | F-14 writer-compliance tail: round-2 after the fix corrected fragment-blank-after-marker, surfaced remaining issues (api zerops.yaml comments too generic, worker SIGTERM drain atom missing fenced code on first attempt). Writer-round-3 eliminated. |
| 17 | 2026-04-22T10:40:07Z | finalize/ | 0 — AI Agent_import_factual_claims, 1 — Remote (CDE)_import_factual_claims, 2 — Local_import_factual_claims, 3 — Stage_import_factual_claims, 4 — Small Production_import_factual_claims, 5 — Highly-available Production_import_factual_claims | **F-21 first-pass: writer invented "2 GB quota" on object-storage envComment across all 6 tiers.** Cx-3 ENV-COMMENT-PRINCIPLE atom teaches the factuality rule but did not prevent the first invention. The tightened check caught it and the agent re-ran generate-finalize with qualitative phrasing ("modest quota suitable for agent iteration" per [main-session.jsonl 10:41:27Z tool-result message]) which passed. Cx-3 enforcement works; Cx-3 prevention partial. |
| 18 | 2026-04-22T11:00:36Z | close/editorial-review | none | editorial-review returned clean |
| 19 | 2026-04-22T11:01:13Z | close/code-review | none | code-review returned clean |
| 20 | 2026-04-22T11:03:10Z | close/close-browser-walk | none | browser walk attempted; per TIMELINE completed. Cx-7 BROWSER-RECOVERY-COMPLETE held (no wall-time chaos as v37) |
| 21 | 2026-04-22T11:03:27Z | close/ | none | close-step attest |

## Final verification

- [x] All cells are non-`pending` (harness false-positive on CLAUDE.md base sections is flagged but does not block)
- [x] Every Read-receipt timestamp is after analyst session start (2026-04-22T14:46Z)
- [ ] No `unmeasurable-invalid` cells — `ZCP_CONTENT_MANIFEST.json` is marked ABSENT with diagnosis (Cx-4 partial-close); scaffold + feature dispatches marked `unmeasurable-valid` (non-guarded roles); close-browser-walk marked `unmeasurable-valid` (harness signal gap, not a run regression)
- [x] Machine-report SHA matches file content (`d756a9726cdd4f03b11994b745af47f3154cd47f6943cc0f20a6d681909bcca2`)
- [ ] Checklist SHA will match on-file content after write

**Analyst sign-off**: Claude Opus 4.7 (1M context), 2026-04-22T15:10Z — v38 evidence: five v37 defect classes effectively CLOSED (F-9, F-12, F-13, partially F-21, no F-24 chaos), but **F-17 remains OPEN at runtime** (editorial-review dispatch had 72% content loss) because the Cx-5 verify-subagent-dispatch path is opt-in and the main agent never invoked it; **F-23 remains OPEN** because Cx-4 manifest overlay was not paired with an export-whitelist extension. Verdict: **PAUSE**.
