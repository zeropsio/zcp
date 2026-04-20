# brief-scaffold-minimal-coverage.md

**Purpose**: defect-class coverage for minimal scaffold (single inline). Many v34-era classes are trivially N/A for single-codebase minimal; for multi-codebase minimal they apply the same as showcase.

## 1. Coverage table

| ID | Origin | Class | brief | check | runtime | N/A note |
|---|---|---|---|---|---|---|
| v17-sshfs-write-not-exec | v17 | zcp-side exec | ✅ `principles/where-commands-run.md` pointer | — | — | — |
| v21-scaffold-hygiene | v21 | leak | ✅ FixRule `gitignore-baseline` + `no-scaffold-test-artifacts` + pre-ship reminder | ✅ `{hostname}_scaffold_artifact_leak` (keep) | — | — |
| v21-claude-readme-dead-regex | v21 | dead regex | — | ✅ `knowledge_base_authenticity` rewrite | — | N/A — writer role |
| v21-framework-token-purge | v21 | framework tokens | ✅ atomization — framework-role addenda | — | — | — |
| v22-nats-url-creds | v22 | URL creds | ✅ FixRule `nats-separate-creds` (IF minimal has NATS service; otherwise N/A) | — | ✅ contract | — |
| v22-s3-endpoint | v22 | S3 endpoint | ✅ FixRule `s3-uses-api-url` (IF minimal has storage service) | — | ✅ contract | — |
| v22-queue-group | v22 | queue group missing | — | ✅ `worker_queue_group_gotcha` | — | N/A — minimal has no worker |
| v22-dev-start-vs-build | v22 | contract | — | ✅ `{hostname}_run_start_build_cmd` | — | N/A — main-agent zerops.yaml |
| v23-env-self-shadow | v23 | self-shadow | — | ✅ `env_self_shadow` shim | — | N/A — main-agent zerops.yaml |
| v25-substep-bypass | v25 | bypass | — | ✅ server-state / de-eager | — | main-agent concern; P4 applies normally |
| v26-git-init-zcp-side-chown | v26 | git init | ✅ FixRule `skip-git` | — | — | — |
| v28-debug-agent-writes-content | v28 | debug writes content | — | — | — | N/A — writer role |
| v28-writer-DISCARD-override | v28 | writer DISCARD | — | — | — | N/A — writer role |
| v28-env-READMEs-substantive | v28 | thin env READMEs | — | ✅ env-README line-count | — | N/A — finalize |
| v28-env-self-shadow-enum-bug | v28 | enum miss | — | ✅ shim | — | N/A |
| v29-preship-artifact-leak | v29 | preship.sh committed | ✅ FixRule `no-scaffold-test-artifacts` | ✅ `scaffold_artifact_leak` | — | — |
| v29-env-README-factual-drift | v29 | factual drift | — | ✅ `{prefix}_factual_claims` | — | N/A — finalize |
| v29-ZCP_CONTENT_MANIFEST | v29 | missing manifest | — | — | — | N/A — writer role |
| v29-circular-import | v29 | Nest circular import | ✅ library-import verification preamble (if feature-role extended to main-inline) | — | — | N/A to pure scaffold; feature-role concern |
| v30-worker-SIGTERM | v30 | SIGTERM missing | — | — | — | N/A — minimal has no worker |
| v31-apidev-enableShutdownHooks | v31 | api shutdown hooks | ✅ FixRule `graceful-shutdown` (IF api-style minimal) | — | ✅ contract | — |
| v32-dispatch-compression | v32 | rule lost | ✅ atomic stitching; since minimal consumes inline, no dispatch compression pressure at all | — | — | — |
| v32-close-never-completed | v32 | close skipped | — | — | — | N/A — close phase |
| v32-Read-before-Edit-lost | v32 | rule lost | ✅ `principles/file-op-sequencing.md` pointer-include | — | — | — |
| v32-six-platform-principles-missing | v32 | principles absent | ✅ principles pointer-included filtered to role | — | — | — |
| v32-feature-subagent-mandatory | v32 | feature MANDATORY | — | — | — | N/A — feature role |
| v32-StampCoupling | v32 | coupling | — | ✅ P1 supersedes | — | — |
| v33-phantom-output-tree | v33 | phantom tree | — | — | — | N/A — writer role |
| v33-auto-export | v33 | auto-export | — | — | — | N/A — close phase |
| v33-Unicode-box-drawing | v33 | box chars | ✅ visual-style + comment-style pointer | ✅ **new** `visual_style_ascii_only` | — | — |
| v33-seed-execOnce-appVersionId | v33 | seed burn | — | — | — | N/A — main-agent zerops.yaml |
| v33-diagnostic-probe-burst | v33 | probe panic | — | — | — | N/A — feature-role scope (for minimal, the main agent ALSO writes features inline per data-flow-minimal.md §5; the `diagnostic-cadence.md` atom applies at deploy.feature-sweep-dev substep, not at scaffold) |
| v33-paraphrased-env-folder-names | v33 | writer paraphrase | — | — | — | N/A — writer role (Go-template emitted env folders) |
| v33-pre-init-git-sequencing | v33 | `fatal: not a git repository` | ✅ FixRule `skip-git` | — | — | — |
| v34-manifest-content-inconsistency | v34 | DB_PASS manifest drift | — | — | — | N/A — writer role |
| v34-cross-scaffold-env-var | v34 | DB_PASS/DB_PASSWORD | **Trivially N/A for single-codebase minimal** (only one codebase). For multi-codebase minimal: contract applies same as showcase. | ✅ **new** `symbol_contract_env_var_consistency` (applicable to multi-codebase) | ✅ contract | single-codebase: structurally impossible |
| v34-convergence-architecture | v34 | Fix E | ✅ runnable pre-attest per rule | ✅ all checks runnable | — | — |

## 2. Statistics

| Status | Count |
|---|---:|
| Covered in brief | 10 |
| Covered by check | 9 |
| Covered by runtime | 4 |
| N/A | 19 |
| Unaddressed | **0** |

## 3. Orphan check

Zero orphans. Minimal scaffold has many trivially-N/A classes (no worker, no search, no NATS, no S3 in typical minimal). Classes relevant to single-codebase minimal are all covered.

## 4. Minimal-scaffold-role-specific notes

- v34-cross-scaffold-env-var is trivially closed for single-codebase minimal (no parallel scaffolds). Dual-runtime minimal (e.g. nestjs-minimal-v3 with separate frontend codebase) uses the same SymbolContract shape as showcase, and the class closes the same way.
- Minimal scaffold benefits most from byte-budget reduction (see diff §4: ~54% reduction) because the current system forced it to consume showcase-shaped sub-agent content.
- `diagnostic-cadence.md` is NOT pointer-included at scaffold. It applies at deploy-phase feature work (the phase where v33's probe-burst happened). Minimal's feature work happens in main-inline at deploy.feature-sweep-dev, and the main-agent substep-entry guidance there pulls `briefs/feature/diagnostic-cadence.md` as an in-band read.
- Per data-flow-minimal.md §11, step-4 cold-read simulation must verify "tier-conditional sections within atoms remain ≤300 lines both with and without conditional branches." The scaffold atoms' tier-gated sections are small; no atom crosses the 300-line threshold under either branch.
- Escalation trigger (RESUME decision #1): no concrete question surfaced here that requires a commissioned minimal run. Scaffold composition verifies cleanly from spec + showcase-as-reference.
