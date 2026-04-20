# brief-writer-minimal-coverage.md

**Purpose**: defect-class coverage for writer (minimal). Most classes align with writer-showcase coverage; minimal-specific deltas are flagged.

## 1. Coverage table

| ID | Origin | Class | brief | check | runtime | N/A note |
|---|---|---|---|---|---|---|
| v17-sshfs-write-not-exec | v17 | zcp-side exec | ✅ where-commands-run | — | — | — |
| v21-scaffold-hygiene | v21 | leak | — | ✅ `scaffold_artifact_leak` | — | N/A — scaffold phase |
| v21-claude-readme-dead-regex | v21 | dead regex | — | ✅ `knowledge_base_authenticity` (rewrite-to-runnable) — tier-gated OUT for minimal per check-rewrite.md | — | minimal tier doesn't run this check; relies on author's authenticity discipline |
| v21-framework-token-purge | v21 | framework tokens | ✅ classification taxonomy discards framework-quirk | — | — | — |
| v22-nats-url-creds | v22 | URL creds | — | — | — | N/A for typical minimal (no NATS) |
| v22-s3-endpoint | v22 | S3 endpoint | — | — | — | N/A for typical minimal (no S3) |
| v22-queue-group | v22 | queue group missing | — | — | — | N/A — no worker |
| v23-env-self-shadow | v23 | self-shadow | — | ✅ `env_self_shadow` shim | — | N/A to writer output |
| v25-substep-bypass | v25 | bypass | — | ✅ server state | — | main-agent concern |
| v26-git-init-zcp-side-chown | v26 | git init | — | — | — | N/A — scaffold phase |
| v28-debug-agent-writes-content | v28 | debug writes content | ✅ `fresh-context-premise.md` atom (with main-inline aspirational acknowledgement) | — | — | **Minimal-specific caveat**: main-inline delivery means this class's structural protection is partial (A1 in simulation). Enforcement is output-side (pre-attest aggregate + manifest honesty). |
| v28-writer-DISCARD-override | v28 | DISCARD override | ✅ manifest-contract + override_reason required | ✅ `writer_discard_classification_consistency` | — | — |
| v28-33-percent-genuine-gotchas | v28 | low authenticity | ✅ classification-taxonomy + routing-matrix + citation-map; authenticity check tier-gated for minimal | Partial — tier-gate removes showcase-level authenticity shim; minimal relies on author discipline | — | minimal tier has fewer gate checks by design; the author-runnable aggregate is the primary enforcement |
| v28-env-READMEs-substantive | v28 | thin env READMEs | — | ✅ env-README line-count (finalize) | — | N/A — finalize phase |
| v28-env-self-shadow-enum-bug | v28 | enum miss | — | ✅ shim | — | N/A to writer output |
| v29-preship-artifact-leak | v29 | preship.sh | — | ✅ `scaffold_artifact_leak` | — | N/A — scaffold phase |
| v29-env-README-factual-drift | v29 | factual drift | — | ✅ `{prefix}_factual_claims` | — | N/A — finalize |
| v29-ZCP_CONTENT_MANIFEST | v29 | missing manifest | ✅ manifest-contract requires file | ✅ `writer_content_manifest_exists` + `_valid` (keep) | — | — |
| v30-worker-SIGTERM | v30 | SIGTERM missing | — | — | — | N/A — no worker in minimal |
| v31-apidev-enableShutdownHooks | v31 | shutdown hooks | — | — | — | N/A — scaffold phase authored; writer documents in CLAUDE.md operational section |
| v32-dispatch-compression | v32 | rule lost | ✅ atomic stitching; file-op-sequencing pointer-included | — | — | In main-inline path, dispatch compression class is structurally absent (no dispatch fires) |
| v32-close-never-completed | v32 | close skipped | — | — | — | N/A — close phase |
| v32-Read-before-Edit-lost | v32 | rule lost | ✅ same | — | — | — |
| v32-six-platform-principles-missing | v32 | principles absent | — | — | — | N/A — writer role references principles via citation-map topics |
| v32-StampCoupling | v32 | coupling | — | ✅ P1 supersedes | — | — |
| v33-phantom-output-tree | v33 | phantom tree | ✅ `canonical-output-tree.md` positive allow-list + self-review aggregate `! find /var/www -maxdepth 2 -type d -name 'recipe-*'` | ✅ **new** `canonical_output_tree_only` (close-entry) | — | — |
| v33-auto-export | v33 | auto-export | — | — | — | N/A — close phase |
| v33-Unicode-box-drawing | v33 | box chars | ✅ `visual-style.md` + `comment-style.md` pointer | ✅ **new** `visual_style_ascii_only` | — | — |
| v33-seed-execOnce-appVersionId | v33 | seed burn | ✅ citation-map `init-commands` topic; writer documents bootstrap-seed-v1 pattern in CLAUDE.md when applicable | — | — | — |
| v33-diagnostic-probe-burst | v33 | probe panic | — | — | — | N/A — feature role (main-inline in minimal) |
| v33-paraphrased-env-folder-names | v33 | writer paraphrase | ✅ canonical-output-tree: env folder names are Go-template emitted; writer does not author them | — | — | primary coverage |
| v33-pre-init-git-sequencing | v33 | git sequencing | — | — | — | N/A — scaffold phase |
| v34-manifest-content-inconsistency | v34 | DB_PASS | ✅ routing-matrix all-pairs + manifest-contract override_reason + aggregate `zcp check manifest-honesty` | ✅ `writer_manifest_honesty` (rewrite, expanded per P5) | ✅ FactRecord.RouteTo | **Primary coverage for minimal writer. Applies the same whether dispatched (Path B) or main-inline (Path A).** |
| v34-cross-scaffold-env-var | v34 | DB_PASS/DB_PASSWORD | Trivially N/A for single-codebase minimal; contract applies for dual-runtime | ✅ `symbol_contract_env_var_consistency` (if multi-codebase) | ✅ contract | single-codebase: structurally impossible |
| v34-convergence-architecture | v34 | Fix E | ✅ self-review aggregate is runnable | ✅ kept/rewritten checks runnable; tier-gated subset | — | — |

## 2. Statistics

| Status | Count |
|---|---:|
| Covered in brief | 13 |
| Covered by check | 9 |
| Covered by runtime | 3 |
| N/A | 19 |
| Unaddressed | **0** |

## 3. Orphan check

Zero orphans. Minimal writer coverage is largely aligned with showcase writer; tier-specific deltas are in the N/A justifications and in the partial coverage of v28-debug-agent-writes-content (main-inline delivery weakens structural protection, output-side enforcement compensates).

## 4. Minimal-writer-role-specific notes

- **Main-inline path and fresh-context**: the v28-debug-agent-writes-content class has PARTIAL coverage in the main-inline path. Main cannot truly forget the deploy debug rounds; the aspirational fresh-context premise is enforced only by output checks (manifest honesty, classification taxonomy, authenticity bar). This is the most load-bearing minimal-tier caveat.
- **v34-manifest-content-inconsistency is fully covered** regardless of delivery path. The routing-matrix + manifest-contract + honesty check apply identically to Path A (main-inline) and Path B (dispatched writer).
- **Worker-specific rules filtered out**: minimal has no worker, so v30 SIGTERM and v22 queue-group rules do not apply at this role. The tier-conditional filter in `content-surface-contracts.md` drops the worker-specific knowledge-base requirements.
- **Predecessor-floor dropped**: minimal writer no longer carries the "≥3 net-new gotchas beyond predecessor" language. Check-rewrite.md §15 retires this check; both tier writers drop it.
- **Byte budget**: minimal writer composition at ~9 KB is ~57% smaller than the current v8-shape readme-with-fragments block (~21 KB). Showcase-only content (worker rules, predecessor floor, v-anchored storytelling) filtered out at stitch time.
- **Escalation trigger (RESUME decision #1)**: per data-flow-minimal.md §11, step-4 cold-read simulation has verified that `briefs/writer/*` stitches sensibly for main-inline consumption. No commissioned minimal run is required to resolve a step-4 question for this role; audience-adaptation is a stitcher concern.
