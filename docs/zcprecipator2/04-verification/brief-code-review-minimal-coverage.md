# brief-code-review-minimal-coverage.md

**Purpose**: defect-class coverage for code-review (minimal).

## 1. Coverage table

| ID | Origin | Class | brief | check | runtime | N/A note |
|---|---|---|---|---|---|---|
| v17-sshfs-write-not-exec | v17 | zcp-side exec | ✅ where-commands-run pointer | — | — | — |
| v21-scaffold-hygiene | v21 | leak | — | ✅ `scaffold_artifact_leak` | — | N/A — scaffold phase |
| v21-claude-readme-dead-regex | v21 | dead regex | — | ✅ `knowledge_base_authenticity` (rewrite, tier-gated OUT for minimal) | — | minimal: relies on author discipline + manifest-consumption |
| v21-framework-token-purge | v21 | framework tokens | ✅ task.md per-framework scoping | — | — | — |
| v22-nats-url-creds | v22 | URL creds | ✅ framework-expert "does the app work?" (if NATS present) | — | — | N/A for most minimals |
| v22-s3-endpoint | v22 | S3 endpoint | ✅ same (if S3 present) | — | — | N/A for most minimals |
| v22-queue-group | v22 | queue group | — | ✅ `worker_queue_group_gotcha` | — | N/A — no worker |
| v22-dev-start-vs-build | v22 | contract | — | ✅ `run_start_build_cmd` | — | N/A — main-agent zerops.yaml |
| v23-env-self-shadow | v23 | self-shadow | — | ✅ `env_self_shadow` shim | — | N/A — main-agent zerops.yaml |
| v25-substep-bypass | v25 | bypass | ✅ dispatched via Agent | ✅ server state | — | main-agent concern |
| v25-subagent-zerops_workflow-at-spawn | v25 | workflow call | ✅ forbidden-tools list | — | ✅ SUBAGENT_MISUSE | — |
| v25-subagent-tool-use-policy | v25 | tool policy | ✅ mandatory-core | — | — | — |
| v26-git-init-zcp-side-chown | v26 | git init | — | — | — | N/A — scaffold phase |
| v28-debug-agent-writes-content | v28 | debug writes content | — | — | — | N/A — writer role |
| v28-writer-DISCARD-override | v28 | DISCARD override | ✅ manifest-consumption: "fact routed to discarded appearing as gotcha → CRITICAL" | ✅ `writer_discard_classification_consistency` | — | secondary enforcement; primary is writer |
| v28-33-percent-genuine-gotchas | v28 | low authenticity | ✅ manifest-consumption + framework-expert | ✅ authenticity shim (tier-gated OUT at check level; still runnable via shim) | — | tier-gate removes showcase-level automatic check; author-discipline + shim availability |
| v28-env-READMEs-substantive | v28 | thin env READMEs | — | ✅ env-README line-count | — | N/A — finalize |
| v28-env-self-shadow-enum-bug | v28 | enum miss | — | ✅ shim | — | N/A |
| v29-preship-artifact-leak | v29 | preship.sh | — | ✅ `scaffold_artifact_leak` | — | N/A — scaffold phase |
| v29-env-README-factual-drift | v29 | factual drift | — | ✅ `{prefix}_factual_claims` | — | N/A — finalize |
| v29-ZCP_CONTENT_MANIFEST | v29 | missing manifest | ✅ manifest-consumption reads it; absence flagged CRITICAL | ✅ `writer_content_manifest_exists` | — | — |
| v29-circular-import | v29 | Nest circular | ✅ framework-expert: "DI order, modules, async boundaries" | — | — | — |
| v30-worker-SIGTERM | v30 | SIGTERM missing | — | — | — | N/A — no worker in minimal |
| v31-apidev-enableShutdownHooks | v31 | api shutdown hooks | ✅ framework-expert "does the app work?" (if api-style minimal) | — | — | — |
| v32-dispatch-compression | v32 | rule lost | ✅ atomic stitching | — | — | — |
| v32-close-never-completed | v32 | close skipped | — | — | — | N/A — close phase main-agent concern (minimal close is ungated) |
| v32-Read-before-Edit-lost | v32 | rule lost | ✅ file-op-sequencing pointer | — | — | — |
| v32-six-platform-principles-missing | v32 | principles absent | Partial — framework-expert covers behaviorally (no explicit pointer-include same as showcase code-review) | ✅ per-principle checks | — | open question (simulation caveat) |
| v32-feature-subagent-mandatory | v32 | feature MANDATORY | — | — | — | N/A — feature role |
| v32-StampCoupling | v32 | coupling | — | ✅ P1 supersedes | — | — |
| v33-phantom-output-tree | v33 | phantom tree | — | ✅ **new** `canonical_output_tree_only` (close-entry) | — | N/A — writer primary coverage |
| v33-auto-export | v33 | auto-export | — | — | — | N/A — close phase |
| v33-Unicode-box-drawing | v33 | box chars | — | ✅ **new** `visual_style_ascii_only` | — | N/A to framework review |
| v33-seed-execOnce-appVersionId | v33 | seed burn | — | — | — | N/A — main-agent zerops.yaml |
| v33-diagnostic-probe-burst | v33 | probe panic | — | — | — | N/A — feature role |
| v33-paraphrased-env-folder-names | v33 | writer paraphrase | — | — | — | N/A — writer role |
| v33-pre-init-git-sequencing | v33 | git sequencing | — | — | — | N/A — scaffold phase |
| v34-manifest-content-inconsistency | v34 | DB_PASS | ✅ manifest-consumption.md — all routing-vs-surface pairs; aggregate runs `zcp check manifest-honesty` | ✅ `writer_manifest_honesty` (rewrite, expanded per P5) | ✅ FactRecord.RouteTo | **secondary enforcement for writer's primary; applies identically to minimal** |
| v34-cross-scaffold-env-var | v34 | DB_PASS/DB_PASSWORD | ✅ framework-expert rule (if dual-runtime) + aggregate shim | ✅ `symbol_contract_env_var_consistency` | ✅ contract | single-codebase: structurally impossible |
| v34-convergence-architecture | v34 | Fix E | ✅ aggregate runnable; ungated close means main observes exit code directly | ✅ all kept/rewritten runnable | — | tier-note: minimal close is ungated but main still reads aggregate exit |

## 2. Statistics

| Status | Count |
|---|---:|
| Covered in brief | 13 |
| Covered by check | 14 |
| Covered by runtime | 3 |
| N/A | 18 |
| Unaddressed | **0** |

## 3. Orphan check

Zero orphans. Minimal code-review coverage aligns with showcase code-review; tier-specific deltas are:
- No worker checks (N/A)
- Cross-codebase env-var check tier-branched on dual-runtime status
- Close substep is ungated — aggregate exit is main-observed, not server-gated
- v21-dead-regex + v28-authenticity checks tier-gated out at check level; manifest-consumption still catches routing-level issues

## 4. Minimal-code-review-role-specific notes

- Minimal code-review is **discretionary/ungated** per knowledge-matrix-minimal.md + data-flow-minimal.md §7. The sub-agent still fires in practice (nestjs-minimal-v3 TIMELINE confirms); close advances based on main's reading of the return. No server-side substep gate.
- The "must be 0" aggregate exit is an author-discipline contract, not a server gate. Main may still advance close with known CRITICAL findings if it explicitly accepts them (escape hatch) — but the practice-standard expectation is: aggregate exit 0, close advance.
- v34-manifest-content-inconsistency secondary coverage is identical to showcase code-review. Writer is primary; code-review is defense-in-depth. Works the same in minimal tier.
- **Escalation trigger**: per data-flow-minimal.md §11 item 4, "is it clear in `phases/close/code-review.md` whether dispatch is required or optional for minimal? If optional, what's the main-inline alternative path?" — resolved here: dispatch is convention (not required by server); main-inline alternative is main reading the same atoms and running the aggregate itself. Documented in the "Tier note" of the composition.
- Open question from showcase code-review (pointer-include platform-principles atoms?) propagates unchanged to minimal. Step 5 decides.
