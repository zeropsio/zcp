# brief-writer-showcase-coverage.md

**Purpose**: defect-class coverage for writer (showcase). Writer role is the **primary coverage point** for multiple v28/v29/v33/v34 classes that originated in content authorship.

## 1. Coverage table

| ID | Origin | Class | brief | check | runtime | N/A note |
|---|---|---|---|---|---|---|
| v17-sshfs-write-not-exec | v17 | zcp-side exec | ✅ where-commands-run pointer | — | — | low-impact for writer (mostly file ops) |
| v21-scaffold-hygiene | v21 | leak | — | ✅ `{hostname}_scaffold_artifact_leak` | — | N/A — scaffold phase |
| v21-claude-readme-dead-regex | v21 | dead regex | — | ✅ `knowledge_base_authenticity` rewrite-to-runnable | — | check serves writer role; writer sees the pre-attest runnable, not the regex internals |
| v21-framework-token-purge | v21 | framework tokens in shared content | ✅ classification-taxonomy discards framework-quirk; atom content is positive-form not negative | — | — | — |
| v22-nats-url-creds | v22 | URL-embedded creds | ✅ via citation-map (rolling-deploys + object-storage); writer references canonical platform topic rather than re-framing | ✅ symbol_contract_env_var_consistency (new) | ✅ contract | — |
| v22-s3-endpoint | v22 | S3 endpoint | — | — | — | N/A — scaffold phase (writer documents result) |
| v22-queue-group | v22 | queue group missing | ✅ content-surface-contracts tail "For workerdev specifically" requires queue-group gotcha with minContainers + library-option shape | ✅ `{hostname}_worker_queue_group_gotcha` rewrite | — | — |
| v23-env-self-shadow | v23 | self-shadow | ✅ citation-map `env-var-model` topic | ✅ `env_self_shadow` shim | — | N/A for writer's own output |
| v25-substep-bypass | v25 | bypass | ✅ dispatched via Agent, forbidden zerops_workflow | ✅ `SUBAGENT_MISUSE` | — | — |
| v25-subagent-zerops_workflow-at-spawn | v25 | workflow at spawn | ✅ forbidden list | — | ✅ `SUBAGENT_MISUSE` | — |
| v25-subagent-tool-use-policy | v25 | tool policy | ✅ mandatory-core | — | — | — |
| v26-git-init-zcp-side-chown | v26 | git init | — | — | — | N/A — scaffold phase |
| v28-debug-agent-writes-content | v28 | debug agent writes content | ✅ **fresh-context-premise.md atom** — the structural fix for this class | — | — | primary coverage — v8.94 shape preserved |
| v28-writer-DISCARD-override | v28 | writer kept DISCARD facts | ✅ manifest-contract.md: default-discard classifications routed anywhere except `discarded` require non-empty override_reason | ✅ writer_discard_classification_consistency (keep) | — | — |
| v28-33-percent-genuine-gotchas | v28 | low gotcha authenticity | ✅ classification-taxonomy + routing-matrix + citation-map discipline + self-review-per-surface | ✅ `knowledge_base_authenticity` (rewrite-to-runnable shim) | — | — |
| v28-env-READMEs-substantive | v28 | thin env READMEs | — | ✅ env-README line-count (finalize) | — | N/A — writer emits payload, Go-templates write |
| v28-env-self-shadow-enum-bug | v28 | enum miss | — | ✅ shim | — | N/A |
| v29-preship-artifact-leak | v29 | preship.sh | — | ✅ `scaffold_artifact_leak` | — | N/A — scaffold phase |
| v29-env-README-factual-drift | v29 | factual drift | — | ✅ `{prefix}_factual_claims` (rewrite) | — | N/A — finalize; writer's env-comment-set payload IS subject to this via main-agent write |
| v29-ZCP_CONTENT_MANIFEST-missing | v29 | missing manifest | ✅ manifest-contract.md mandates file + schema | ✅ `writer_content_manifest_exists` (keep) + `_valid` (keep) | — | — |
| v29-circular-import | v29 | Nest circular import | — | — | — | N/A — feature role |
| v30-worker-SIGTERM | v30 | missing SIGTERM drain | ✅ content-surface-contracts tail requires workerdev gotcha on SIGTERM drain + fenced code block | ✅ `{hostname}_worker_shutdown_gotcha` (rewrite) + `{hostname}_drain_code_block` (keep) | — | — |
| v31-apidev-enableShutdownHooks | v31 | api shutdown hooks | — | — | — | N/A — scaffold phase; writer documents the result in CLAUDE.md operational section |
| v32-dispatch-compression | v32 | rule lost | ✅ atomic stitching; file-op-sequencing pointer; no dispatcher text | — | ✅ atomic stitching | — |
| v32-close-never-completed | v32 | close skipped | — | — | — | N/A — close phase |
| v32-Read-before-Edit-lost | v32 | rule lost | ✅ same | — | — | — |
| v32-six-platform-principles-missing | v32 | principles absent | ✅ citation-map surfaces principles implicitly; writer documents any applicable principle per gotcha | — | — | — |
| v32-feature-subagent-mandatory | v32 | feature brief missing | — | — | — | N/A — feature role |
| v32-StampCoupling | v32 | coupling table | — | ✅ P1 supersedes | — | — |
| v33-phantom-output-tree | v33 | phantom tree | ✅ **canonical-output-tree.md** positive allow-list + self-review aggregate `! find /var/www -maxdepth 2 -type d -name 'recipe-*'` | ✅ **new** `canonical_output_tree_only` (check-rewrite.md §16) | — | primary coverage — P8 closure |
| v33-auto-export | v33 | auto-export | — | — | — | N/A — close phase |
| v33-Unicode-box-drawing | v33 | box chars | ✅ visual-style + comment-style pointers | ✅ **new** `visual_style_ascii_only` | — | — |
| v33-seed-execOnce-appVersionId | v33 | seed key on appVersionId | ✅ citation-map `init-commands` topic; writer documents bootstrap-seed-v1 pattern in CLAUDE.md | — | — | primary coverage is main-agent zerops.yaml; writer captures the RATIONALE in CLAUDE.md |
| v33-diagnostic-probe-burst | v33 | probe burst | — | — | — | N/A — feature role |
| v33-paraphrased-env-folder-names | v33 | writer paraphrase | ✅ canonical-output-tree.md: env folder names come from Go-template `EnvFolder(i)`, writer does NOT author folder names | — | — | primary coverage — structural; writer emits env-comment-set payload only |
| v33-pre-init-git-sequencing | v33 | git sequencing | — | — | — | N/A — scaffold phase |
| v34-manifest-content-inconsistency | v34 | DB_PASS-as-gotcha-despite-claude-md-routing | ✅ **routing-matrix.md** enumerates all (routed_to × surface) pairs + **manifest-contract.md** enforces single routed_to per fact + override_reason required for non-default + self-review aggregate runs `zcp check manifest-honesty --mount-root=/var/www/` covering ALL dimensions | ✅ **`writer_manifest_honesty` (rewrite-to-runnable, expanded per P5)** — primary coverage | ✅ FactRecord.RouteTo field | **primary coverage — the principal closure point for v34's most significant defect** |
| v34-cross-scaffold-env-var | v34 | DB_PASS/DB_PASSWORD | — | — | — | N/A — scaffold / feature roles; writer documents env-var conventions only |
| v34-convergence-architecture | v34 | Fix E refuted | ✅ self-review-per-surface aggregate = author-runnable pre-attest over every writer-owned check | ✅ all kept/rewritten checks have runnable form | — | primary coverage for writer role's gate rounds |

## 2. Statistics

| Status | Count |
|---|---:|
| Covered in brief | 19 |
| Covered by check | 12 |
| Covered by runtime | 3 |
| N/A | 14 |
| Unaddressed | **0** |

## 3. Orphan check

Zero orphans. Writer role is the **primary coverage point** for v28-debug-writes-content, v28-writer-DISCARD, v33-phantom-output-tree, v33-paraphrased-env-folder-names, v34-manifest-content-inconsistency, and v34-convergence-architecture (for writer's own gate rounds). Defense-in-depth via brief + expanded check + RouteTo runtime.

## 4. Writer-role-specific notes

- v34-manifest-content-inconsistency closure is the **most load-bearing structural change** in the entire rewrite. The routing-matrix atom + expanded honesty check + RouteTo field together turn a single-dimension catch into an all-pairs catch. Without this, the v34 DB_PASS class reproduces.
- v33-phantom-output-tree closure uses positive allow-list per P8 — v8.103/v8.104 used negative prohibitions ("don't write to recipe-{slug}/") which enumerated the invented path; positive form "canonical tree is X" is the structural fix.
- Predecessor-floor (knowledge_base_exceeds_predecessor) is flagged for DELETE in check-rewrite.md §15; writer brief drops the "≥3 net-new gotchas beyond predecessor" requirement accordingly. Authenticity bar replaces it.
- Fresh-context-premise atom preserves v8.94's structural fix for v28-debug-agent-writes-content. Without fresh-context, the writer re-accumulates debug-agent biases.
- env-comment-set payload contract remains the same shape — writer emits, main-agent applies at finalize.
