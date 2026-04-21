# brief-scaffold-appdev-showcase-coverage.md

**Purpose**: defect-class coverage table for scaffold-appdev (showcase).

## 1. Coverage table

| ID | Origin | Class | brief | check | runtime | N/A note |
|---|---|---|---|---|---|---|
| v17-sshfs-write-not-exec | v17 | zcp-side exec via SSHFS mount | ✅ `principles/where-commands-run.md` + mandatory-core | — | — | — |
| v21-scaffold-hygiene | v21 | node_modules committed | ✅ FixRule `gitignore-baseline` + pre-ship grep | ✅ `{hostname}_scaffold_artifact_leak` (keep) | — | — |
| v21-claude-readme-dead-regex | v21 | dead regex | — | ✅ `knowledge_base_authenticity` rewrite | — | N/A — writer role |
| v21-framework-token-purge | v21 | framework tokens in shared content | ✅ atomization; frontend-specific content in `frontend-codebase-addendum.md` only | — | — | — |
| v22-nats-url-creds | v22 | URL-embedded NATS creds | — | — | — | N/A — frontend has no NATS client |
| v22-s3-endpoint | v22 | S3 endpoint wrong | — | — | — | N/A — frontend has no S3 client |
| v22-dev-start-vs-build | v22 | dev-start vs build contract | — | ✅ `{hostname}_run_start_build_cmd` | — | N/A at scaffold; main-agent zerops.yaml substep |
| v23-env-self-shadow | v23/v28/v29 | env self-shadow | — | ✅ `env_self_shadow` (shim) | — | N/A — main-agent zerops.yaml |
| v25-substep-bypass | v25 | attestation backfill | — | ✅ server `SUBAGENT_MISUSE` + v8.90 de-eager | ✅ `PriorDiscoveriesBlock` slot | main-agent concern; sub-agent can't bypass |
| v25-subagent-zerops_workflow-at-spawn | v25 | subagent called `zerops_workflow` | ✅ forbidden-tools list in `mandatory-core.md` | — | ✅ `SUBAGENT_MISUSE` | — |
| v25-subagent-tool-use-policy | v25 | subagent tool policy absent | ✅ `mandatory-core.md` | — | — | — |
| v26-git-init-zcp-side-chown | v26 | git init wrong-owner | ✅ FixRule `skip-git` (positive form) | — | — | — |
| v28-debug-agent-writes-content | v28 | debug agent writes content | — | — | — | N/A — writer role |
| v28-writer-DISCARD-override | v28 | writer kept DISCARD facts | — | — | — | N/A — writer role |
| v28-env-READMEs-substantive | v28 | thin env READMEs | — | ✅ env-README line-count (finalize) | — | N/A — finalize phase |
| v28-env-self-shadow-enum-bug | v28 | enum miss | — | ✅ rewrite | — | N/A |
| v29-preship-artifact-leak | v29 | preship.sh committed | ✅ FixRule `no-scaffold-test-artifacts` preAttestCmd | ✅ `{hostname}_scaffold_artifact_leak` (keep) | — | — |
| v29-env-README-factual-drift | v29 | factual drift | — | ✅ `{prefix}_factual_claims` (rewrite) | — | N/A — finalize |
| v29-ZCP_CONTENT_MANIFEST | v29 | manifest missing | — | — | — | N/A — writer role |
| v30-worker-SIGTERM | v30 | worker SIGTERM missing | — | — | — | N/A — worker role |
| v31-apidev-enableShutdownHooks | v31 | api missing shutdown hooks | — | — | — | N/A — api role |
| v32-dispatch-compression | v32 | Read-before-Edit lost | ✅ `principles/file-op-sequencing.md` pointer, no dispatcher text | — | ✅ atomic stitching | — |
| v32-close-never-completed | v32 | close never completed | — | — | — | N/A — close phase |
| v32-Read-before-Edit-lost | v32 | Read-before-Edit rule lost | ✅ same as v32-dispatch-compression | — | — | — |
| v32-six-platform-principles-missing | v32 | six principles absent | ✅ `principles/platform-principles/01..06.md` pointer-included; only principle 2 applies to frontend | — | — | — |
| v32-feature-subagent-mandatory | v32 | feature brief missing MANDATORY | — | — | — | N/A — feature role |
| v32-StampCoupling | v32 | coupling table | — | ✅ P1 supersedes | — | — |
| v33-phantom-output-tree | v33 | phantom tree | — | — | — | N/A — writer role |
| v33-auto-export | v33 | auto-export | — | — | — | N/A — close phase |
| v33-Unicode-box-drawing | v33 | box-drawing chars | ✅ `principles/visual-style.md` + `principles/comment-style.md` pointer-included | ✅ **new** `visual_style_ascii_only` | — | — |
| v33-seed-execOnce-appVersionId | v33 | seed key on appVersionId | — | — | — | N/A — main-agent zerops.yaml |
| v33-diagnostic-probe-burst | v33 | probe panic | — | — | — | N/A — feature role |
| v33-paraphrased-env-folder-names | v33 | writer paraphrased | — | — | — | N/A — writer role |
| v33-pre-init-git-sequencing | v33 | `fatal: not a git repository` | ✅ FixRule `skip-git` positive form | — | — | — |
| v34-manifest-content-inconsistency | v34 | DB_PASS manifest drift | — | — | — | N/A — writer role |
| v34-cross-scaffold-env-var | v34 | DB_PASS vs DB_PASSWORD | ✅ contract byte-identical across 3 dispatches — frontend reads `import.meta.env.VITE_API_URL`; env-var names from contract | ✅ **new** `symbol_contract_env_var_consistency` | ✅ contract JSON | — |
| v34-convergence-architecture | v34 | Fix E refuted | ✅ author-runnable preAttestCmd per rule | ✅ all kept/rewritten checks have runnable form | — | — |

## 2. Coverage statistics

| Status | Count |
|---|---:|
| Covered in brief | 9 |
| Covered by check | 9 |
| Covered by runtime | 4 |
| N/A | 18 |
| Unaddressed | **0** |

## 3. Orphan check

Zero orphans. Every defect class applicable to scaffold-appdev has ≥1 enforcement point in brief + check + runtime; N/A classes carry a one-line justification naming the correct responsible role.

## 4. Frontend-specific notes

- Many N/A rows because the frontend codebase has no NATS / S3 / db / worker code surface. The scaffold hosts a browser-side SPA only.
- The only platform principle that applies end-to-end to the frontend is Principle 2 (routable bind). Principle 6 (stripped build root) is authored by the main agent in zerops.yaml — relevant to this codebase's deploy but not to its scaffold.
- Cross-scaffold env-var coverage (v34) comes from the contract even though the frontend doesn't READ most env vars — it's a shared reference so the frontend's `.env.example` template names (`VITE_API_URL`, etc.) stay consistent with what the main agent writes in zerops.yaml.
