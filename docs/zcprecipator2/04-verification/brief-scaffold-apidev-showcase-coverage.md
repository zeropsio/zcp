# brief-scaffold-apidev-showcase-coverage.md

**Purpose**: defect-class coverage table. Rows = every class from README §6 seed list + the expanded audit in principles.md §9 (step 5's full registry isn't emitted yet, so I use the seed). Columns = (covered by brief sentinel / covered by new check / covered by Go-injected runtime data / not applicable to this role). Every class must have ≥1 enforcement point OR a justified "N/A" note. Class IDs match principles.md §9 where possible.

Legend:
- **brief** = prevention surfaces in the composed brief (atom text, FixRule preAttestCmd, pointer-included principle).
- **check** = server-side check still runs (per check-rewrite.md) — kept / rewritten-to-runnable / new.
- **runtime** = Go-layer runtime injection (contract JSON, prior-discoveries block, Go-template interpolation) supplies the data needed to avoid the class.
- **N/A** = class doesn't apply to this (role × tier) — e.g. a writer-brief class for a scaffold-role brief.

---

## 1. Coverage table

| ID | Origin | Class | brief | check | runtime | N/A note |
|---|---|---|---|---|---|---|
| v17-sshfs-write-not-exec | v17 | scaffold ran `cd /var/www/{host} && <exec>` zcp-side | ✅ `principles/where-commands-run.md` positive form, pointer-included + `mandatory-core.md` SSH clause | ✅ `{hostname}_prepare_varwww` (keep) — not directly this role's concern but main-agent zerops.yaml path | — | — |
| v21-scaffold-hygiene | v21 | 208 MB node_modules leak | ✅ FixRule `gitignore-baseline` preAttestCmd + `no-scaffold-test-artifacts` rule + pre-ship aggregate grep | ✅ `{hostname}_scaffold_artifact_leak` (keep) | — | — |
| v21-claude-readme-dead-regex | v21 | silent-pass regex on phrase variants | — | ✅ `knowledge_base_authenticity` (rewrite-to-runnable) — writer-role-scoped | — | N/A to scaffold role |
| v21-framework-token-purge | v21 | framework-specific tokens in shared content | ✅ atomization — api-specific content lives in `api-codebase-addendum.md`, not in a shared atom | — | — | — |
| v22-nats-url-creds | v22 | URL-embedded NATS creds recurring | ✅ FixRule `nats-separate-creds` preAttestCmd + contract `EnvVarsByKind.queue.user`/`.pass` separate fields + principle 5 pointer-include + pre-ship assertion | — | ✅ contract JSON interpolation names `NATS_USER` and `NATS_PASS` as separate env vars | — |
| v22-s3-endpoint | v22 | S3 301 HTTPS endpoint recurrence (storage_apiHost → storage_apiUrl) | ✅ FixRule `s3-uses-api-url` preAttestCmd + contract `EnvVarsByKind.storage.endpoint = "STORAGE_ENDPOINT"` | — | ✅ contract names canonical env-var key | — |
| v22-dev-start-vs-build | v22 | dev-start vs buildCommands contract | — | ✅ `{hostname}_run_start_build_cmd` (rewrite-to-runnable) — main-agent substep | — | N/A to scaffold (main writes zerops.yaml) |
| v23-env-self-shadow | v23/v28/v29 | `key: ${key}` in run.envVariables | Partial — FixRule listed with `appliesAtSubstep=main`; sub-agent sees it but skips (see simulation I1) | ✅ `{hostname}_env_self_shadow` (rewrite-to-runnable shim) — main-agent substep | — | Scaffold sub-agent doesn't write zerops.yaml; filterable at stitch-time |
| v25-substep-bypass | v25 | main did 40min of deploy work, backfilled attestations | — | — | ✅ `PriorDiscoveriesBlock` slot + v8.90 `SUBAGENT_MISUSE` preserved + P4 | scaffold cannot bypass substeps — it's dispatched; main-agent concern |
| v25-subagent-zerops_workflow-at-spawn | v25 | subagent called `zerops_workflow` at spawn | ✅ forbidden-tools list in `mandatory-core.md` + `principles/tool-use-policy.md` pointer-include | — | ✅ server-side `SUBAGENT_MISUSE` (v8.90 substrate) | — |
| v25-subagent-tool-use-policy | v25 | subagent tool policy missing | ✅ `mandatory-core.md` permit + forbid list, transmitted once, not compressed | — | — | — |
| v26-git-init-zcp-side-chown | v26 | `git init` zcp-side wrong-owner | ✅ FixRule `skip-git` positive form (`--skip-git` preferred; `rm -rf /var/www/.git` fallback via ssh) | — | — | — |
| v28-debug-agent-writes-content | v28 | debug agent writing reader content | — | — | — | N/A — writer-role concern (covered in writer brief) |
| v28-writer-DISCARD-override | v28 | writer kept DISCARD-flagged facts | — | — | — | N/A — writer-role |
| v28-env-READMEs-substantive | v28 | env READMEs too thin | — | ✅ env-README line-count check (finalize) | — | N/A — finalize phase, main agent |
| v28-env-self-shadow-enum-bug | v28 | self-shadow enum miss | — | ✅ `env_self_shadow` rewrite-to-runnable covers enumeration | — | N/A to scaffold |
| v29-preship-artifact-leak | v29 | `preship.sh` committed | ✅ FixRule `no-scaffold-test-artifacts` preAttestCmd `! find /var/www/{h} -maxdepth 3 \( -name 'preship.sh' -o -name '*.assert.sh' \)` | ✅ `{hostname}_scaffold_artifact_leak` (keep) | — | — |
| v29-env-README-factual-drift | v29 | env README numeric/mode drift | — | ✅ `{prefix}_factual_claims` (rewrite-to-runnable) | — | N/A — finalize phase |
| v29-ZCP_CONTENT_MANIFEST | v29 | missing structured manifest | — | — | — | N/A — writer role |
| v30-worker-SIGTERM | v30 | missing SIGTERM drain | — | — | — | N/A to apidev (worker-role concern) |
| v31-apidev-enableShutdownHooks | v31 | apidev missing enableShutdownHooks | ✅ FixRule `graceful-shutdown` preAttestCmd `grep -q 'enableShutdownHooks' /var/www/apidev/src/main.ts` + pre-ship assertion + principle 1 pointer-include | — | ✅ contract `FixRecurrenceRules` entry | — |
| v32-dispatch-compression | v32 | 3 scaffold sub-agents lost Read-before-Edit | ✅ `principles/file-op-sequencing.md` pointer-included once, verbatim — no compression pressure | — | ✅ atomic stitching — no dispatcher text to compress | — |
| v32-close-never-completed | v32 | close never completed + premature export | — | — | — | N/A — close-phase, main-agent concern |
| v32-Read-before-Edit-lost | v32 | Read-before-Edit rule lost | ✅ same as v32-dispatch-compression | — | — | — |
| v32-six-platform-principles-missing | v32 | six platform principles absent from scaffold brief | ✅ `principles/platform-principles/01..06.md` pointer-included | — | — | — |
| v32-feature-subagent-mandatory | v32 | feature brief missing MANDATORY block | — | — | — | N/A — feature role |
| v32-StampCoupling | v32 | coupling table incomplete | — | ✅ FixRule preAttestCmds are the coupling (author-runnable) | — | P1 supersedes StampCoupling |
| v33-phantom-output-tree | v33 | writer wrote to `/var/www/recipe-{slug}/` | — | — | — | N/A — writer role |
| v33-auto-export | v33 | NextSteps[0] autonomous export | — | — | — | N/A — close-phase |
| v33-Unicode-box-drawing | v33 | Unicode box-drawing in zerops.yaml | ✅ `principles/visual-style.md` + `principles/comment-style.md` pointer-included | ✅ **new** `visual_style_ascii_only` check (per check-rewrite.md §16) | — | — |
| v33-seed-execOnce-appVersionId | v33 | seed keyed on `${appVersionId}` | — | — | — | N/A — main-agent zerops.yaml authoring |
| v33-diagnostic-probe-burst | v33 | feature subagent's 9-min probe panic | — | — | — | N/A — feature role |
| v33-paraphrased-env-folder-names | v33 | writer paraphrased env folder names | — | — | — | N/A — writer role |
| v33-pre-init-git-sequencing | v33 | `fatal: not a git repository` after scaffold | ✅ FixRule `skip-git` (positive form: `--skip-git` flag OR `.git/` removed after return) + `framework-task.md` step 1 passes `--skip-git` | — | — | — |
| v34-manifest-content-inconsistency | v34 | DB_PASS gotcha-despite-manifest-claude-md-routing | — | — | — | N/A — writer role (covered in writer coverage) |
| v34-cross-scaffold-env-var | v34 | apidev `DB_PASS` vs workerdev `DB_PASSWORD` | ✅ contract `EnvVarsByKind.db.pass = "DB_PASS"` transmitted byte-identically to all 3 scaffold dispatches — scaffold does not re-derive | ✅ **new** `symbol_contract_env_var_consistency` (check-rewrite.md §16) — cross-codebase grep-diff | ✅ Go stitcher interpolates same JSON | — |
| v34-convergence-architecture | v34 | Fix E metadata did not collapse rounds | ✅ every scaffold check has author-runnable preAttestCmd; gate becomes confirmation (principle P1) | ✅ all kept/rewritten checks emit runnable forms | — | — |

---

## 2. Coverage statistics

| Status | Count |
|---|---:|
| Covered in brief (sentinel or FixRule or pointer-include) | 15 |
| Covered by check (keep/rewrite/new) | 11 |
| Covered by Go-injected runtime data | 6 |
| N/A to this role with justification | 14 |
| Unaddressed | **0** |

Some classes appear in multiple columns (defense-in-depth per principles.md §9 cross-audit). E.g. `v22-nats-url-creds` is covered by brief (FixRule) + runtime (contract JSON) — both enforcement points prevent recurrence.

---

## 3. Orphan check (step 4 success criterion)

> "Every defect class must have ≥1 enforcement point OR a justified 'not applicable' note."

Walk: every row above has either (a) an enforcement point marked ✅ in one of the first three columns, or (b) an `N/A` marker with a one-line justification naming the correct responsible role/phase.

**Verdict**: zero orphans. Brief meets the coverage criterion for scaffold-apidev (showcase).

---

## 4. Coverage gaps to feed back to step 5 (regression fixture)

The N/A-marked rows are not covered in THIS brief but must be covered elsewhere. Step 5's registry should record the responsible coverage doc per ID:

- `v25-substep-bypass` → main-agent + server-state; coverage doc = (phases/*/entry.md audit, main-agent flow).
- `v29-env-README-factual-drift` → finalize-phase; coverage doc = `brief-finalize` (there is no dedicated sub-agent, but the check-rewrite entry stands).
- `v28-debug-agent-writes-content` + `v28-writer-DISCARD-override` + `v29-ZCP_CONTENT_MANIFEST` + `v33-phantom-output-tree` + `v33-paraphrased-env-folder-names` + `v34-manifest-content-inconsistency` → writer role. Expected to appear as ✅ in the writer-showcase coverage doc.
- `v30-worker-SIGTERM` → workerdev scaffold role. Expected ✅ in scaffold-workerdev-showcase coverage.
- `v33-diagnostic-probe-burst` → feature role. Expected ✅ in feature-showcase coverage.
- `v32-close-never-completed` / `v33-auto-export` → close-phase main-agent; atomic `phases/close/completion.md` + `phases/close/export-on-request.md` handle it (no sub-agent brief).

---

## 5. Notes on the seed registry vs full registry

The full registry is step 5's output. This coverage doc uses the README §6 seed list + the expanded cross-audit in `principles.md §9`. If step 5 surfaces classes not in either list (e.g. `v8.NN` fix for a defect class that isn't mapped to a per-run `vNN` entry), this coverage doc will need a follow-up pass to mark those rows. Non-blocking for step 4 completion per task operating constraints ("use README §6's seed defect registry if step 5's full registry isn't ready").
