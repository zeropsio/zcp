# brief-code-review-showcase-coverage.md

**Purpose**: defect-class coverage for code-review (showcase). Code-review is a **secondary enforcement point** for several writer-role and feature-role classes — defense-in-depth.

## 1. Coverage table

| ID | Origin | Class | brief | check | runtime | N/A note |
|---|---|---|---|---|---|---|
| v17-sshfs-write-not-exec | v17 | zcp-side exec | ✅ where-commands-run pointer | — | — | — |
| v21-scaffold-hygiene | v21 | leak | — | ✅ `scaffold_artifact_leak` | — | N/A — scaffold phase |
| v21-claude-readme-dead-regex | v21 | dead regex | — | ✅ `knowledge_base_authenticity` (rewrite) | — | code-review reviews writer output; rewrite shim is what writer runs |
| v21-framework-token-purge | v21 | framework tokens | ✅ task.md scoping — framework per codebase | — | — | — |
| v22-nats-url-creds | v22 | URL creds | ✅ framework-expert checklist item `Principle 5 / NATS structured creds` (implicit — covered via "does the app work?" + silent-swallow) | — | — | code-review catches regression (scaffold + feature already closed) |
| v22-s3-endpoint | v22 | wrong endpoint | ✅ framework-expert "does the app work?" covers wrong env var | — | — | — |
| v22-queue-group | v22 | missing queue group | ✅ framework-expert: "Worker subscription: uses `{ queue: 'workers' }` queue group. Both `jobs.scaffold` and `jobs.process` subscribed" | ✅ `worker_queue_group_gotcha` | — | — |
| v22-dev-start-vs-build | v22 | start/build contract | — | ✅ `{hostname}_run_start_build_cmd` | — | N/A — main-agent zerops.yaml |
| v23-env-self-shadow | v23 | self-shadow | — | ✅ `env_self_shadow` shim | — | N/A — main-agent substep |
| v25-substep-bypass | v25 | bypass | ✅ dispatched via Agent | ✅ SUBAGENT_MISUSE | — | — |
| v25-subagent-zerops_workflow-at-spawn | v25 | workflow call | ✅ forbidden list | — | ✅ SUBAGENT_MISUSE | — |
| v25-subagent-tool-use-policy | v25 | tool policy | ✅ mandatory-core | — | — | — |
| v26-git-init-zcp-side-chown | v26 | git init | — | — | — | N/A — scaffold phase |
| v28-debug-agent-writes-content | v28 | debug writes content | — | — | — | N/A — writer role |
| v28-writer-DISCARD-override | v28 | writer kept DISCARD | ✅ manifest-consumption: "fact routed to `discarded` appearing as gotcha → CRITICAL" | ✅ `writer_discard_classification_consistency` | — | secondary enforcement; primary is writer brief |
| v28-33-percent-genuine-gotchas | v28 | low authenticity | ✅ manifest-consumption (title-token verification) + framework-expert checks | ✅ `knowledge_base_authenticity` (rewrite) | — | secondary |
| v28-env-READMEs-substantive | v28 | thin env READMEs | — | ✅ env-README line-count (finalize) | — | N/A — finalize phase |
| v28-env-self-shadow-enum-bug | v28 | enum miss | — | ✅ shim | — | N/A |
| v29-preship-artifact-leak | v29 | preship.sh | — | ✅ `scaffold_artifact_leak` | — | N/A — scaffold phase |
| v29-env-README-factual-drift | v29 | factual drift | — | ✅ `{prefix}_factual_claims` | — | N/A — finalize |
| v29-ZCP_CONTENT_MANIFEST | v29 | missing manifest | ✅ manifest-consumption reads it; absence triggers CRITICAL | ✅ `writer_content_manifest_exists` / `_valid` | — | — |
| v29-circular-import | v29 | Nest circular | ✅ framework-expert: "DI order, async boundaries, error propagation, modules" catches circular imports | — | — | code-review is where this class is typically caught |
| v30-worker-SIGTERM | v30 | SIGTERM drain missing | ✅ framework-expert worker-subscription rule: "onModuleDestroy drains BOTH subscriptions plus the connection" | ✅ `worker_shutdown_gotcha` + `drain_code_block` | — | — |
| v31-apidev-enableShutdownHooks | v31 | api shutdown hooks | ✅ framework-expert "does the app work?" catches | — | — | — |
| v32-dispatch-compression | v32 | rule lost | ✅ atomic stitching; file-op-sequencing pointer | — | ✅ atomic stitching | — |
| v32-close-never-completed | v32 | close skipped | — | — | — | N/A — workflow-state concern |
| v32-Read-before-Edit-lost | v32 | rule lost | ✅ same | — | — | — |
| v32-six-platform-principles-missing | v32 | principles absent | Partial — **simulation-flagged gap**; consider pointer-include of platform-principles for code-review | ✅ various per-principle checks | — | currently indirect (framework-expert "does the app work?" covers behaviorally) |
| v32-feature-subagent-mandatory | v32 | feature MANDATORY | — | — | — | N/A |
| v32-StampCoupling | v32 | coupling | — | ✅ P1 supersedes | — | — |
| v33-phantom-output-tree | v33 | phantom tree | — | ✅ **new** `canonical_output_tree_only` (close-entry) + writer's self-review | — | N/A — writer role + close-entry check |
| v33-auto-export | v33 | auto-export | — | — | — | N/A — close phase, main-agent |
| v33-Unicode-box-drawing | v33 | box chars | — | ✅ **new** `visual_style_ascii_only` | — | N/A to framework review (platform config out of scope) |
| v33-seed-execOnce-appVersionId | v33 | seed burn | — | — | — | N/A — main-agent zerops.yaml |
| v33-diagnostic-probe-burst | v33 | probe panic | — | — | — | N/A — feature role |
| v33-paraphrased-env-folder-names | v33 | writer paraphrase | — | — | — | N/A — writer role |
| v33-pre-init-git-sequencing | v33 | git sequencing | — | — | — | N/A — scaffold phase |
| v34-manifest-content-inconsistency | v34 | DB_PASS | ✅ **manifest-consumption.md atom** — secondary coverage for writer's primary. Reads manifest + greps every surface for every routed_to dimension. Catches same class the writer's self-review should. | ✅ `writer_manifest_honesty` (rewrite, expanded per P5) | ✅ FactRecord.RouteTo | **secondary enforcement; defense-in-depth** |
| v34-cross-scaffold-env-var | v34 | DB_PASS/DB_PASSWORD | ✅ framework-expert: "Cross-codebase env-var naming: every codebase reads from canonical SymbolContract names. Any variant is CRITICAL." + aggregate `zcp check symbol-contract-env-consistency` | ✅ **new** `symbol_contract_env_var_consistency` | ✅ contract | — |
| v34-convergence-architecture | v34 | Fix E | ✅ completion aggregate runs shim checks before attestation | ✅ all kept/rewritten runnable | — | — |

## 2. Statistics

| Status | Count |
|---|---:|
| Covered in brief | 15 |
| Covered by check | 14 |
| Covered by runtime | 3 |
| N/A | 17 |
| Unaddressed | **0** |

## 3. Orphan check

Zero orphans. Code-review is the **last-stop framework review** and the **secondary enforcement point** for writer-role + feature-role classes. Defense-in-depth is the design pattern.

## 4. Code-review-role-specific notes

- v34-manifest-content-inconsistency has primary coverage in writer brief. Code-review provides the secondary backstop via `manifest-consumption.md`. Both would need to miss for the class to reship.
- v34-cross-scaffold-env-var has primary coverage in scaffold briefs (via contract) + feature brief + new check; code-review provides secondary via framework-expert checklist rule.
- Code-review does NOT pointer-include `principles/platform-principles/*` atoms — an open question from step-4 simulation. The rationale is that code-review is framework-expert-only, not platform-aware. But for catching v30 / v31 regressions, the principles' positive forms are useful to reference. Step 5 should decide whether to pointer-include them.
- Code-review is the final pre-export gate for framework defects. Under P1, its aggregate runs runnable checks before attesting — no framework issue escapes into the gate's fail-round.
