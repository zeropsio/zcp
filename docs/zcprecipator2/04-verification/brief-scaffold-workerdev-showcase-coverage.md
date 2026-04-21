# brief-scaffold-workerdev-showcase-coverage.md

**Purpose**: defect-class coverage for scaffold-workerdev (showcase).

## 1. Coverage table

| ID | Origin | Class | brief | check | runtime | N/A note |
|---|---|---|---|---|---|---|
| v17-sshfs-write-not-exec | v17 | zcp-side exec | ✅ `principles/where-commands-run.md` + mandatory-core | — | — | — |
| v21-scaffold-hygiene | v21 | node_modules leak | ✅ FixRule `gitignore-baseline` + pre-ship grep | ✅ `{hostname}_scaffold_artifact_leak` (keep) | — | — |
| v21-claude-readme-dead-regex | v21 | dead regex | — | ✅ `knowledge_base_authenticity` rewrite | — | N/A — writer role |
| v21-framework-token-purge | v21 | framework tokens | ✅ atomization | — | — | — |
| v22-nats-url-creds | v22 | URL-embedded NATS creds | ✅ FixRule `nats-separate-creds` + pre-ship grep + addendum code literal uses separate user/pass | — | ✅ contract EnvVarsByKind.queue.user / .pass | — |
| v22-s3-endpoint | v22 | S3 endpoint wrong | — | — | — | N/A — worker has no S3 client |
| v22-dev-start-vs-build | v22 | start/build contract | — | ✅ `{hostname}_run_start_build_cmd` | — | N/A — main-agent substep |
| v22-queue-group-missing | v22 | queue group missing | ✅ FixRule `queue-group` + contract `NATSQueues.workers="workers"` + pre-ship grep + addendum code literal | ✅ `{hostname}_worker_queue_group_gotcha` (rewrite-to-runnable) | ✅ contract literal | — |
| v23-env-self-shadow | v23/v28/v29 | env self-shadow | — | ✅ `env_self_shadow` (shim) | — | N/A — main-agent substep |
| v25-substep-bypass | v25 | attestation backfill | — | ✅ server `SUBAGENT_MISUSE` + v8.90 de-eager | ✅ `PriorDiscoveriesBlock` | main-agent concern |
| v25-subagent-zerops_workflow-at-spawn | v25 | workflow call at spawn | ✅ forbidden-tools list | — | ✅ `SUBAGENT_MISUSE` | — |
| v25-subagent-tool-use-policy | v25 | tool policy absent | ✅ `mandatory-core.md` | — | — | — |
| v26-git-init-zcp-side-chown | v26 | git init wrong-owner | ✅ FixRule `skip-git` | — | — | — |
| v28-debug-agent-writes-content | v28 | debug writes content | — | — | — | N/A — writer role |
| v28-writer-DISCARD-override | v28 | writer kept DISCARD | — | — | — | N/A — writer role |
| v28-env-READMEs-substantive | v28 | thin env READMEs | — | ✅ env-README line-count (finalize) | — | N/A — finalize |
| v28-env-self-shadow-enum-bug | v28 | enum miss | — | ✅ rewrite | — | N/A — main-agent substep |
| v29-preship-artifact-leak | v29 | preship.sh committed | ✅ FixRule `no-scaffold-test-artifacts` | ✅ `{hostname}_scaffold_artifact_leak` | — | — |
| v29-env-README-factual-drift | v29 | factual drift | — | ✅ `{prefix}_factual_claims` rewrite | — | N/A — finalize |
| v29-ZCP_CONTENT_MANIFEST | v29 | manifest missing | — | — | — | N/A — writer role |
| v30-worker-SIGTERM | v30 | missing SIGTERM drain | ✅ FixRule `graceful-shutdown` preAttestCmd + `worker.service.ts` `onModuleDestroy` literal + main.ts `enableShutdownHooks` + pre-ship grep `drain` | ✅ `{hostname}_worker_shutdown_gotcha` (rewrite-to-runnable) — also at writer phase | ✅ principle 1 pointer-include | — |
| v31-apidev-enableShutdownHooks | v31 | apidev shutdown hooks missing | — | — | — | N/A — api role |
| v32-dispatch-compression | v32 | Read-before-Edit lost | ✅ `principles/file-op-sequencing.md` | — | ✅ atomic stitching | — |
| v32-close-never-completed | v32 | close skipped | — | — | — | N/A — close phase |
| v32-Read-before-Edit-lost | v32 | rule lost | ✅ same | — | — | — |
| v32-six-platform-principles-missing | v32 | principles absent | ✅ principles/platform-principles pointer-include (1, 4, 5 load-bearing for worker) | — | — | — |
| v32-feature-subagent-mandatory | v32 | feature brief missing MANDATORY | — | — | — | N/A — feature role |
| v32-StampCoupling | v32 | coupling table | — | ✅ P1 supersedes | — | — |
| v33-phantom-output-tree | v33 | phantom tree | — | — | — | N/A — writer role |
| v33-auto-export | v33 | auto-export | — | — | — | N/A — close phase |
| v33-Unicode-box-drawing | v33 | box chars | ✅ `principles/visual-style.md` + `comment-style.md` | ✅ **new** `visual_style_ascii_only` | — | — |
| v33-seed-execOnce-appVersionId | v33 | seed key drift | — | — | — | N/A — main-agent zerops.yaml |
| v33-diagnostic-probe-burst | v33 | probe panic | — | — | — | N/A — feature role |
| v33-paraphrased-env-folder-names | v33 | writer paraphrased | — | — | — | N/A — writer role |
| v33-pre-init-git-sequencing | v33 | `fatal: not a git repository` | ✅ FixRule `skip-git` | — | — | — |
| v34-manifest-content-inconsistency | v34 | DB_PASS-as-gotcha-despite-claude-md-routing | — | — | — | N/A — writer role. **However**: the source of the defect is the cross-scaffold naming class, which IS closed here (v34-cross-scaffold-env-var). |
| v34-cross-scaffold-env-var | v34 | DB_PASS vs DB_PASSWORD | ✅ contract byte-identical; worker reads `process.env.DB_PASS` and `process.env.NATS_USER` / `NATS_PASS` from the contract | ✅ **new** `symbol_contract_env_var_consistency` | ✅ contract JSON | — |
| v34-convergence-architecture | v34 | Fix E refuted | ✅ every rule has author-runnable preAttestCmd | ✅ all kept/rewritten checks runnable | — | — |

## 2. Statistics

| Status | Count |
|---|---:|
| Covered in brief | 12 |
| Covered by check | 10 |
| Covered by runtime | 5 |
| N/A | 15 |
| Unaddressed | **0** |

## 3. Orphan check

Zero orphans. The v30 worker-SIGTERM class and v34-cross-scaffold env-var class (the two classes that motivated the worker-role-specific principles in the first place) both have defense-in-depth coverage: brief + check + runtime.

## 4. Worker-role-specific notes

- Worker role is where the v30 SIGTERM class originated (writer-brief said MANDATORY but the scaffolded `main.ts` didn't implement it). Under the new architecture, the worker-scaffold brief is the author of the implementation; the writer brief documents it. Inverting author/documenter flow closes the class structurally.
- Worker role is one of the two roles where v34 DB_PASS/DB_PASSWORD mismatch originated. The other is api (already covered). Coverage here is the second end of the shared contract.
- Worker has the only codebase in showcase where principle 4 (competing consumer) is load-bearing. Its atom pointer-include is essential to the brief (the api / frontend briefs skip principle 4 at stitch-time).
