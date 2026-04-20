# brief-feature-showcase-coverage.md

**Purpose**: defect-class coverage for feature (showcase).

## 1. Coverage table

| ID | Origin | Class | brief | check | runtime | N/A note |
|---|---|---|---|---|---|---|
| v17-sshfs-write-not-exec | v17 | zcp-side exec | ✅ `principles/where-commands-run.md` + mandatory-core | — | — | — |
| v21-scaffold-hygiene | v21 | leak | — | ✅ `{hostname}_scaffold_artifact_leak` | — | N/A — scaffold phase already passed |
| v21-claude-readme-dead-regex | v21 | dead regex | — | — | — | N/A — writer role |
| v21-framework-token-purge | v21 | framework tokens | ✅ atomization (feature atoms don't carry scaffold-specifics) | — | — | — |
| v22-nats-url-creds | v22 | URL-embedded creds | ✅ principle 5 pointer + contract separate NATS_USER/NATS_PASS + feature task jobs-dispatch publish uses `this.nats.publish(subject, codec.encode(payload))` (no URL-embed) | — | ✅ contract | — |
| v22-s3-endpoint | v22 | S3 endpoint wrong | — | — | — | N/A — scaffold set endpoint already |
| v22-dev-start-vs-build | v22 | contract | — | ✅ `{hostname}_run_start_build_cmd` | — | N/A — main-agent zerops.yaml |
| v22-queue-group | v22 | missing queue group | ✅ principle 4 pointer + feature task literal `{ queue: 'workers' }` for new `jobs.process` subscription | ✅ `{hostname}_worker_queue_group_gotcha` (rewrite-to-runnable) | ✅ contract NATSQueues.workers | — |
| v23-env-self-shadow | v23/v28/v29 | self-shadow | — | ✅ `env_self_shadow` shim | — | N/A — main-agent zerops.yaml |
| v25-substep-bypass | v25 | bypass | ✅ feature is dispatched via Agent (not `zerops_workflow` action) | ✅ `SUBAGENT_MISUSE` | ✅ `PriorDiscoveriesBlock` | — |
| v25-subagent-zerops_workflow-at-spawn | v25 | workflow call at spawn | ✅ forbidden-tools list | — | ✅ `SUBAGENT_MISUSE` | — |
| v25-subagent-tool-use-policy | v25 | tool policy | ✅ `mandatory-core.md` | — | — | — |
| v26-git-init-zcp-side-chown | v26 | git init wrong-owner | — | — | — | N/A — scaffold substep |
| v28-debug-agent-writes-content | v28 | debug writes content | — | — | — | N/A — writer role; feature sub-agent is a debug-heavy role by nature, BUT explicitly does NOT write reader-facing content (no README/CLAUDE.md touches) |
| v28-writer-DISCARD-override | v28 | writer DISCARD override | — | — | — | N/A — writer role |
| v28-env-READMEs-substantive | v28 | thin env READMEs | — | ✅ env-README line-count | — | N/A — finalize phase |
| v28-env-self-shadow-enum-bug | v28 | enum miss | — | ✅ rewrite | — | N/A — main-agent zerops.yaml |
| v29-preship-artifact-leak | v29 | preship.sh committed | — | ✅ `{hostname}_scaffold_artifact_leak` | — | N/A — scaffold phase; feature should NOT commit test shell scripts either but isn't scoped to write them |
| v29-env-README-factual-drift | v29 | factual drift | — | ✅ `{prefix}_factual_claims` | — | N/A — finalize |
| v29-ZCP_CONTENT_MANIFEST | v29 | manifest missing | — | — | — | N/A — writer role |
| v29-circular-import | v29 | Nest circular imports | ✅ library-import verification preamble in mandatory-core; before writing `@Module({ imports: [...] })` cycles, verify the shape against Nest docs | — | — | Closes class by "verify import before writing" discipline |
| v30-worker-SIGTERM | v30 | SIGTERM missing | ✅ feature extends WorkerService — explicitly told to "keep drain sequence intact (add new Subscription to drain list)" per simulation A6 clarification + principle 1 pointer | ✅ `{hostname}_worker_shutdown_gotcha` | — | — |
| v31-apidev-enableShutdownHooks | v31 | api shutdown hooks | — | — | — | N/A — scaffold authored; feature preserves |
| v32-dispatch-compression | v32 | rule lost | ✅ atomic stitching; `principles/file-op-sequencing.md` pointer-included | — | — | — |
| v32-close-never-completed | v32 | close skipped | — | — | — | N/A — close phase |
| v32-Read-before-Edit-lost | v32 | rule lost | ✅ same | — | — | — |
| v32-six-platform-principles-missing | v32 | principles absent | ✅ pointer-include 1..6 | — | — | — |
| v32-feature-subagent-mandatory | v32 | feature brief missing MANDATORY | ✅ mandatory-core.md pointer-include applies to feature brief by default | — | — | — |
| v32-StampCoupling | v32 | coupling table | — | ✅ P1 supersedes | — | — |
| v33-phantom-output-tree | v33 | phantom tree | — | — | — | N/A — writer role |
| v33-auto-export | v33 | auto-export | — | — | — | N/A — close phase |
| v33-Unicode-box-drawing | v33 | box chars | ✅ visual-style pointer | ✅ `visual_style_ascii_only` new check | — | — |
| v33-seed-execOnce-appVersionId | v33 | seed burn | — | — | — | N/A — main-agent zerops.yaml |
| v33-diagnostic-probe-burst | v33 | probe panic | ✅ `diagnostic-cadence.md` atom (max 3 per hypothesis + batch separator rule) | — | — | — |
| v33-paraphrased-env-folder-names | v33 | writer paraphrase | — | — | — | N/A — writer role |
| v33-pre-init-git-sequencing | v33 | `fatal: not a git repository` | — | — | — | N/A — scaffold phase |
| v34-manifest-content-inconsistency | v34 | DB_PASS manifest drift | Partial — feature sub-agent records facts with RouteTo set where possible; writer makes the final determination | ✅ writer_manifest_honesty (expanded per P5) | ✅ RouteTo field on FactRecord | primary coverage is writer role; feature contributes by setting RouteTo at record time |
| v34-cross-scaffold-env-var | v34 | DB_PASS/DB_PASSWORD | ✅ feature reads env vars from the contract, which is byte-identical across all dispatches; task explicitly names `DB_PASS`, `NATS_PASS` | ✅ `symbol_contract_env_var_consistency` (new) | ✅ contract JSON | — |
| v34-convergence-architecture | v34 | Fix E refuted | ✅ feature's pre-attest verification is curl smoke + build + dev-server probes — all author-runnable; gate reception is confirmation | ✅ all checks runnable | — | — |

## 2. Statistics

| Status | Count |
|---|---:|
| Covered in brief | 13 |
| Covered by check | 10 |
| Covered by runtime | 4 |
| N/A | 18 |
| Unaddressed | **0** |

## 3. Orphan check

Zero orphans. Every class applicable to feature role has ≥1 enforcement point or a clear N/A with role-assignment.

## 4. Feature-role-specific notes

- Feature role consumes the SymbolContract rather than producing it. This is the v34 DB_PASS closure's second end: the scaffolds' same contract is used for the feature implementation's env-var references.
- Feature role is the primary author of downstream-scope facts (v8.96 Theme B). Every platform-behavior observation, every fix, every cross-codebase contract moment — recorded during feature work, consumed by the writer at deploy.readmes.
- Probe cadence (v33 Fix D) applies primarily to the feature role, since feature is the debug-heavy phase. `diagnostic-cadence.md` is feature-role only (not pointer-included by scaffold or writer briefs).
- Worker SIGTERM class is re-enforced here because feature role edits `worker.service.ts` — adding a new subscription without updating the drain sequence is a regression vector. The brief explicitly preserves the drain invariant.
