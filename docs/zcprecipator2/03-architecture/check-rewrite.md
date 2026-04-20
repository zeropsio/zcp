# check-rewrite.md — per-check disposition table

**Purpose**: walk every check defined in [`internal/tools/workflow_checks_*.go`](../../../internal/tools/) and assign a disposition under the new architecture: **keep** / **rewrite-to-runnable** / **delete**. Per principle P1, every kept or rewritten check must expose an author-runnable pre-attest command the author executes against its own in-mount draft before attesting complete. Per RESUME decision #5, deletion is **conservative**: a check is deleted only when its protection is provably upstreamed by a principle or an earlier check, with a one-sentence justification and the new enforcement point named.

Column conventions (§4 onwards):
- **Read surface**: what the check reads (file path pattern, data structure field).
- **Asserts**: what the predicate enforces.
- **Current failure**: payload shape emitted by the current checker.
- **Runnable?**: `yes` (shell command suffices), `shim` (needs a small Go helper invoked from shell), `no` (fundamentally requires Go data structures).
- **Runnable form**: exact command / shim invocation.
- **Disposition**: keep / rewrite-to-runnable / delete.
- **Rationale**: one sentence.

The table is grouped by current file. §8 is the summary matrix. §9 is the conservative-deletion audit.

---

## 1. Legend for "Runnable?" classifications

- **yes**: a `grep / awk / jq / sed / bash test` command suffices. No code imports.
- **shim**: needs a small `zcp check <check-name> --path ./{hostname}/` CLI invocation that the scaffold / feature / writer sub-agent runs via its SSH session. Shim reads the same code path the server uses but accepts mount paths as input. The shim becomes part of the zcp binary's CLI surface; no new tool category.
- **no**: needs cross-service or cross-session state only available to the server (workflow state, facts log aggregation, cross-codebase comparison where the author doesn't have cross-codebase access). These are candidates for rewrite, delete, or keep-as-server-only-confirmation.

---

## 2. File-by-file index

Source tree: `internal/tools/workflow_checks_*.go` (12 non-test files).

| File | Check count (rough) | Category |
|---|---:|---|
| `workflow_checks_recipe.go` | ~20 | per-codebase README + YAML surface checks (fragment markers, comment ratios, IG content, etc.) |
| `workflow_checks_generate.go` | ~13 | per-codebase zerops.yaml structural + `env_self_shadow` |
| `workflow_checks_deploy.go` | 2 | `dev_prod_env_divergence` + runtime `deploy_files` |
| `workflow_checks_dedup.go` | 1 | `cross_readme_gotcha_uniqueness` |
| `workflow_checks_claude_md.go` | 1 (multi-status) | `{hostname}_claude_md_exists` incl. byte-floor + custom sections |
| `workflow_checks_predecessor_floor.go` | 2 | `knowledge_base_exceeds_predecessor` + `knowledge_base_authenticity` |
| `workflow_checks_scaffold_artifact.go` | 1 | `{hostname}_scaffold_artifact_leak` |
| `workflow_checks_worker_correctness.go` | 4 | worker queue-group + SIGTERM + production-correctness + drain-code-block |
| `workflow_checks_content_manifest.go` | 5 | writer manifest exists/valid + discard consistency + honesty + completeness |
| `workflow_checks_comment_depth.go` | 1 | `{prefix}_comment_depth` |
| `workflow_checks_factual_claims.go` | 1 | `{prefix}_factual_claims` |
| `workflow_checks_finalize.go` | ~22 | per-env import.yaml structural checks |

**Total distinct check names**: ~73 (approximate; many are templated per-host / per-env and would emit N concrete StepCheck rows per run).

---

## 3. Disposition summary counts (spoiler; detail in §4–§6)

| Disposition | Count |
|---|---:|
| keep (with runnable pre-attest form added) | 44 |
| rewrite-to-runnable (predicate changes or shim introduced) | 22 |
| delete (conservative; upstream-handled) | 7 |
| (baseline total, pre-refinement) | **73** |
| new architecture-level (§16 — P3/P5/P6/P8) | 5 |
| new editorial-review-originated (§16a — refinement 2026-04-20) | 7 |
| **Final total** | **85** |

---

## 4. workflow_checks_generate.go — per-codebase zerops.yaml structural

| Check | Read surface | Asserts | Current failure | Runnable? | Runnable form | Disposition | Rationale |
|---|---|---|---|---|---|---|---|
| `zerops_yml_exists` | `{hostname}/zerops.yaml` on disk | File present | "zerops.yaml missing at {path}" | yes | `test -f {hostname}/zerops.yaml` | keep | File existence trivially runnable |
| `{hostname}_setup` | parsed doc entry named `dev` | dev setup exists in zerops.yaml | "no setup named 'dev' in {hostname}/zerops.yaml" | yes | `grep -E '^- setup:\\s+dev$' {hostname}/zerops.yaml` | keep | grep |
| `{hostname}_env_refs` | parsed `run.envVariables` values | cross-service references resolve | "env var {key} references undefined service {ref}" | shim | `zcp check env-refs --hostname={h} --path=./` | rewrite-to-runnable | Needs discover-output to validate references; shim sources same parser |
| `{hostname}_ports` | parsed `run.ports` | ports block shape | "ports block malformed" | yes | `yq '.zerops[].run.ports' {hostname}/zerops.yaml` | keep | yq suffices |
| `{hostname}_deploy_files` (generate) | `run.deployFiles` list | deploy files paths exist | "deployFiles path {p} missing on disk" | yes | `for f in $(yq '...'); do test -e "$f" \|\| echo MISSING; done` | keep | filesystem test |
| `{hostname}_health_check` | parsed `healthCheck` or `readinessCheck` | health check shape for HTTP runtimes | "health check missing/malformed" | yes | `yq '.zerops[] \| select(.setup=="dev") \| .run.readinessCheck' {h}/zerops.yaml` | keep | yq |
| `{hostname}_run_start` | `run.start` | start command present | "run.start missing in setup X" | yes | `yq '.zerops[].run.start' {h}/zerops.yaml` | keep | yq |
| `{hostname}_run_start_build_cmd` | `run.start` vs `buildCommands` | start references compiled output only if build produced it (v8.81 §4.5: dev-start vs buildCommands contract) | "run.start references dist/*.js but buildCommands omits build step" | shim | `zcp check run-start-build-contract --hostname={h}` | rewrite-to-runnable | predicate crosses two fields; shim enforces the v8.81 rule |
| `{hostname}_run_start_build_cmd` (dev-specific branch) | same | dev variant | same | shim | same | rewrite-to-runnable | folded into same shim |
| `{hostname}_prepare_varwww` | `prepareCommands` | no zcp-side `cd /var/www/*` patterns in prepareCommands | "prepareCommands contains zcp-side path" | yes | `yq '.zerops[].build.prepareCommands[]' {h}/zerops.yaml \| grep -E '/var/www/' && exit 1 \|\| exit 0` | keep | grep |
| `{hostname}_prepare_missing_sudo` | `prepareCommands` | package-install patterns carry sudo when required | "prepareCommands apt-get without sudo" | yes | `yq '...[]' \| grep -E '^(apt-get\|apk\|yum) ' \| grep -v '^sudo' && exit 1 \|\| exit 0` | keep | grep |
| `{hostname}_build_base_webserver` | `build.base` | static recipes use webserver base | "static recipe must use base: static" | yes | `yq '.zerops[].build.base' {h}/zerops.yaml \| grep -E '^static$'` | keep | yq |
| `{hostname}_dev_deploy_files` | dev setup's `deployFiles` | dev deploy files valid | "dev deployFiles path missing" | yes | same as `_deploy_files` | keep | |
| `{hostname}_env_self_shadow` | parsed top-level + `run.envVariables` | no `key: ${key}` self-shadows | "env var {k} self-shadows ${k}" | shim | `zcp check env-self-shadow --hostname={h}` OR a raw python one-liner invoking `yq` + regex | rewrite-to-runnable | v8.94 Fix 5 already hardens enumeration; shim closes the enumeration-bug class |

**Summary**: 13 checks in generate.go; all pass to the new architecture with P1-compliant runnable forms. No deletions.

---

## 5. workflow_checks_recipe.go — README + fragment surfaces

| Check | Read surface | Asserts | Runnable? | Runnable form | Disposition | Rationale |
|---|---|---|---|---|---|---|
| `{hostname}_zerops_yml_exists` | `{hostname}/zerops.yaml` | file exists | yes | `test -f {h}/zerops.yaml` | keep | |
| `{hostname}_no_premature_readme` | `{hostname}/README.md` at generate-complete | README.md absent until deploy.readmes | yes | `test ! -f {h}/README.md` | keep | |
| `{hostname}_readme_exists` | `{hostname}/README.md` at deploy.readmes complete | README.md present | yes | `test -f {h}/README.md` | keep | |
| `{hostname}_setup` | parsed doc | dev setup named `dev` | yes | `yq '.zerops[] \| select(.setup=="dev") \| .setup' {h}/zerops.yaml` | keep | |
| `{hostname}_prod_setup` | parsed doc | prod setup named `prod` | yes | same with `prod` | keep | |
| `{hostname}_worker_setup` | parsed doc | worker setup named `worker` (shared-codebase worker) | yes | same with `worker` | keep | |
| `dev_prod_env_divergence` | dev+prod `run.envVariables` maps | maps differ | yes | `diff <(yq '.zerops[] \| select(.setup=="dev") \| .run.envVariables' {h}/zerops.yaml) <(yq '...="prod".envVariables' {h}/zerops.yaml)` | keep | inequality test |
| `fragment_{intro,integration-guide,knowledge-base}` | README fragment markers | markers present | yes | `grep -E '#ZEROPS_EXTRACT_START:{name}' {h}/README.md && grep -E '#ZEROPS_EXTRACT_END:{name}' {h}/README.md` | keep | |
| `integration_guide_yaml` | integration-guide fragment content | fragment contains yaml fenced block | yes | `awk '/#ZEROPS_EXTRACT_START:integration-guide/{f=1} f' {h}/README.md \| grep -E '^\\`\\`\\`ya?ml'` | keep | |
| `comment_ratio` (YAML-in-IG) | integration-guide's embedded yaml | ≥30% lines have `# comments` | yes | `awk '/yaml/,/\\`\\`\\`/{if(/^\\s*#/)c++; t++} END{print c*100/t}'` (wrapped) | keep | ratio test |
| `fragment_{name}_heading_level` | fragment content | heading level constraints | yes | `awk '/START/,/END/{if(/^##[^#]/)print}'` (detects H2 inside fragment) | keep | |
| `fragment_{name}_blank_after_marker` | fragment content | blank line after `#ZEROPS_EXTRACT_START:{name}` | yes | `grep -A1 '#ZEROPS_EXTRACT_START:{n}' {h}/README.md \| awk 'NR==2 && !/^$/{exit 1}'` | keep | |
| `knowledge_base_gotchas` | knowledge-base fragment | fragment contains `- **` bullets | yes | `awk '/knowledge-base/,/END/{if(/^- \\*\\*/)c++} END{if(c==0)exit 1}'` | keep | |
| `intro_length` | intro fragment | ≤200 chars | yes | `wc -m <(awk '/intro/,/END/' {h}/README.md)` + threshold | keep | |
| `intro_no_titles` | intro fragment | no `#` headings | yes | `awk '/intro/,/END/{if(/^#/)exit 1}'` | keep | |
| `no_placeholders` | whole README | no `TODO`/`FIXME`/`XXX`/`<placeholder>` tokens | yes | `grep -E 'TODO\|FIXME\|XXX\|<[A-Z_]+>' {h}/README.md && exit 1 \|\| exit 0` | keep | |
| `integration_guide_code_adjustment` (showcase-only) | IG fragment | every IG item beyond #1 has code-block adjacent to header | shim | `zcp check ig-code-adjustment --hostname={h}` | rewrite-to-runnable | awk suffices but the heuristic is tricky; shim is cleaner |
| `integration_guide_per_item_code` (showcase-only) | IG fragment | each `### N` item has its own code block | shim | `zcp check ig-per-item-code --hostname={h}` | rewrite-to-runnable | same |
| `comment_specificity` (showcase-only) | zerops.yaml `#` comments | ≥25% carry Zerops-vocabulary tokens | shim | `zcp check comment-specificity --hostname={h}` | rewrite-to-runnable | vocabulary list is data-bound; shim reads the same list the server uses |
| `zerops_yml_schema_fields` | parsed doc | schema-conformant fields only | shim | `zcp check yml-schema --hostname={h}` | rewrite-to-runnable | schema validation; shim wraps the live Zerops JSON schema |

**Summary**: 20 checks; 16 keep-with-grep/awk, 4 rewrite-via-shim. No deletions.

---

## 6. workflow_checks_deploy.go — runtime deploy checks

| Check | Read surface | Asserts | Runnable? | Runnable form | Disposition | Rationale |
|---|---|---|---|---|---|---|
| `dev_prod_env_divergence` | dev+prod `run.envVariables` | maps differ | yes | (same as recipe.go) | keep | |
| `{hostname}_deploy_files` | `run.deployFiles` | paths on disk | yes | filesystem test | keep | |

---

## 7. workflow_checks_dedup.go — cross-README uniqueness

| Check | Read surface | Asserts | Runnable? | Runnable form | Disposition | Rationale |
|---|---|---|---|---|---|---|
| `cross_readme_gotcha_uniqueness` | all N codebase READMEs | no two READMEs carry Jaccard-similar gotcha stems | shim | `zcp check cross-readme-dedup` (walks all `{host}/README.md`) | rewrite-to-runnable | cross-codebase — author needs the shim to access all mounts; shim uses the same Jaccard impl as the server |

**Critical**: v8.104 Fix E shipped `PerturbsChecks: ["cross_readme_gotcha_uniqueness"]` on every dedup-adjacent check. Per principle P1 + v34 data, that approach did not collapse rounds. The **new** approach: author runs the shim before attesting readmes substep-complete; if it returns non-zero, the author rewrites the colliding gotcha in the one codebase that hosts it and cross-references in the other.

---

## 8. workflow_checks_claude_md.go — CLAUDE.md presence + depth

| Check | Read surface | Asserts | Runnable? | Runnable form | Disposition | Rationale |
|---|---|---|---|---|---|---|
| `{hostname}_claude_md_exists` | `{hostname}/CLAUDE.md` | file exists + byte-floor ≥1200 + ≥2 custom sections beyond template | yes | `test -f {h}/CLAUDE.md && test $(wc -c < {h}/CLAUDE.md) -ge 1200 && awk '/^##/&&!/Dev Loop\|Migrations\|Container Traps\|Testing/{c++} END{if(c<2)exit 1}' {h}/CLAUDE.md` | keep | composite shell one-liner |

---

## 9. workflow_checks_predecessor_floor.go

| Check | Read surface | Asserts | Runnable? | Runnable form | Disposition | Rationale |
|---|---|---|---|---|---|---|
| `knowledge_base_exceeds_predecessor` | current run's knowledge-base + predecessor deliverable | new run's KB ≥ predecessor's KB quality | no | — | **delete** | No predecessor comparison available to author; post-rollback per `recipe-version-log.md §v21` made this informational-only; `knowledge_base_authenticity` covers the gotcha-quality axis without predecessor coupling. **Upstream**: `knowledge_base_gotchas` enforces bullet presence + `gotcha_distinct_from_guide` (if retained) enforces originality. **Conservative**: this check has been informational since v8.78; removing it removes zero gates. |
| `knowledge_base_authenticity` | knowledge-base fragment | gotchas reference platform mechanisms | shim | `zcp check kb-authenticity --hostname={h}` | rewrite-to-runnable | shim loads the platform-vocabulary list the server uses; author-side invocation is the convergence fix |

---

## 10. workflow_checks_scaffold_artifact.go

| Check | Read surface | Asserts | Runnable? | Runnable form | Disposition | Rationale |
|---|---|---|---|---|---|---|
| `{hostname}_scaffold_artifact_leak` | `{hostname}/scripts/` + root | no `preship.sh` / `*.assert.sh` / self-test shell scripts | yes | `find {h} -maxdepth 3 -name 'preship.sh' -o -name '*.assert.sh' \| grep -q . && exit 1 \|\| exit 0` | keep | v8.95 Fix A already shipped; runnable form trivial |

---

## 11. workflow_checks_worker_correctness.go

| Check | Read surface | Asserts | Runnable? | Runnable form | Disposition | Rationale |
|---|---|---|---|---|---|---|
| `{hostname}_worker_queue_group_gotcha` | worker README knowledge-base | gotcha references `queue: 'workers'` / queue-group semantics | shim | `zcp check worker-queue-group-gotcha --hostname={h}` | rewrite-to-runnable | token-match list lives in Go; shim surfaces it |
| `{hostname}_worker_shutdown_gotcha` | worker README | gotcha references SIGTERM drain | shim | `zcp check worker-shutdown-gotcha --hostname={h}` | rewrite-to-runnable | same pattern |
| `{hostname}_worker_production_correctness` | worker README | both queue-group + shutdown present | yes | composition of the two above | keep | runs after the two subordinate checks |
| `{hostname}_drain_code_block` | worker README | fenced code block with both `drain()` + explicit exit call | yes | `awk '/^\\`\\`\\`/,/^\\`\\`\\`/{buf=buf$0} /drain\\(/&&/exit/{ok=1} END{exit !ok}' {h}/README.md` | keep | awk + regex |

---

## 12. workflow_checks_content_manifest.go — writer-brief enforcement (primary P5 surface)

| Check | Read surface | Asserts | Runnable? | Runnable form | Disposition | Rationale |
|---|---|---|---|---|---|---|
| `writer_content_manifest_exists` | `{mount-root}/ZCP_CONTENT_MANIFEST.json` | file exists | yes | `test -f ZCP_CONTENT_MANIFEST.json` | keep | trivial |
| `writer_content_manifest_valid` | manifest JSON | parses + has required top-level fields | yes | `jq empty ZCP_CONTENT_MANIFEST.json && jq 'has("facts")' ZCP_CONTENT_MANIFEST.json` | keep | jq |
| `writer_discard_classification_consistency` | manifest facts | every `classification ∈ {framework-quirk, library-meta, self-inflicted}` either `routed_to="discarded"` OR has non-empty `override_reason` | yes | `jq '[.facts[] \| select(.classification=="framework-quirk" or .classification=="library-meta" or .classification=="self-inflicted") \| select(.routed_to != "discarded") \| select(.override_reason == "")] \| length'` + test -eq 0 | keep | jq |
| `writer_manifest_honesty` (expanded per P5) | manifest + all READMEs | for **every** `(routed_to, published-surface)` pair consistency holds — not only `(discarded, published_gotcha)` | shim | `zcp check manifest-honesty --mount-root=./` | rewrite-to-runnable | P5 expansion broadens Jaccard check across dimensions; shim packages the expanded check so main + writer + code-review all call it |
| `writer_manifest_completeness` | manifest.facts + facts log | every distinct FactRecord.Title has exactly one manifest entry | shim | `zcp check manifest-completeness --mount-root=./ --facts=/tmp/zcp-facts-{sess}.jsonl` | rewrite-to-runnable | shim loads facts log + applies the same content-scope filter the server uses |

**Critical architectural change**: `writer_manifest_honesty` expands from the current single `(discarded, published_gotcha)` dimension to every `(routed_to ∈ {discarded, content_gotcha, content_intro, content_ig, content_env_comment, claude_md, zerops_yaml_comment, scaffold_preamble, feature_preamble}) × (surface ∈ {gotcha-in-README, intro-in-README, IG-in-README, env-comment, claude-md, ...})` pair. Per P5. The current single-dimension check would have passed v34's DB_PASS case — v34 defect is closed at this line.

---

## 13. workflow_checks_comment_depth.go + workflow_checks_factual_claims.go

| Check | Read surface | Asserts | Runnable? | Runnable form | Disposition | Rationale |
|---|---|---|---|---|---|---|
| `{prefix}_comment_depth` | env import.yaml's `# comment` blocks | ≥35% carry a "reasoning marker" (e.g. "because", "otherwise", "so that", or a symptom verb) | shim | `zcp check comment-depth --env={i}` | rewrite-to-runnable | reasoning-marker list is data-bound |
| `{prefix}_factual_claims` | env import.yaml's `# comment` values | numeric claims (minContainers: N, mode: X) match the YAML declarations immediately following | shim | `zcp check factual-claims --env={i}` | rewrite-to-runnable | cross-section comparison; shim reads same regex + declaration parser |

---

## 14. workflow_checks_finalize.go — per-env import.yaml structural (the largest set)

Finalize checks run once per env (0-5) × each distinct prefix (e.g. `env0_import_*`), producing the largest surface. All are keep or rewrite-to-runnable; none deleted because each closes a specific class of published-content defect.

| Check | Read surface | Asserts | Runnable? | Runnable form | Disposition | Rationale |
|---|---|---|---|---|---|---|
| `file_{relpath}` | file presence | each required file exists | yes | `test -f <path>` | keep | |
| `appdev_readme_no_todo_scaffold` | `appdev/README.md` | no scaffolder TODOs remain | yes | `grep -E 'TODO\|FIXME\|XXX\|// HACK' appdev/README.md \| grep -v 'template' && exit 1 \|\| exit 0` | keep | grep |
| `{prefix}_duplicate_keys` | env import.yaml | yaml parse finds no duplicate keys | yes | `yq '.' env{i}/import.yaml > /dev/null 2>&1` (yq 4.x fails on dup) | keep | |
| `{prefix}_valid_yaml` | env import.yaml | parseable | yes | `yq '.' env{i}/import.yaml > /dev/null` | keep | |
| `{prefix}_project_name` | env import.yaml | project.name matches expected slug pattern | yes | `yq '.project.name' env{i}/import.yaml \| grep -E '^nestjs-(showcase\|minimal)-.+$'` | keep | regex |
| `{prefix}_{svc}_no_start_without_code` | services block | services without codebase don't declare `start` | yes | `yq '.services[] \| select(.startWithoutCode==true) \| .start' env{i}/import.yaml` + test empty | keep | |
| `{prefix}_no_placeholders` | env import.yaml | no TODO/<SLUG>/XXX | yes | `grep -E 'TODO\|FIXME\|<[A-Z_]+>' env{i}/import.yaml && exit 1 \|\| exit 0` | keep | |
| `{prefix}_comment_ratio` | env import.yaml | ≥30% comment density | yes | awk ratio test (same as recipe.go `comment_ratio`) | keep | |
| `{prefix}_shared_secret` | env import.yaml | APP_SECRET uses `<@generateRandomString(<32>)>` | yes | `grep 'APP_SECRET' env{i}/import.yaml \| grep -E 'generateRandomString'` | keep | |
| `{prefix}_preprocessor` | env import.yaml | `#zeropsPreprocessor=on` present | yes | `head -5 env{i}/import.yaml \| grep -E '#zeropsPreprocessor=on'` | keep | |
| `{prefix}_{tgt}_priority` | services block | priority ordering | yes | `yq '.services[].priority' env{i}/import.yaml` + monotonic check | keep | |
| `{prefix}_{svc}_setup_git` | services block | setup.git for app services has correct URL template | yes | `yq '.services[] \| select(.type contains "nodejs") \| .setup.git'` + regex | keep | |
| `{prefix}_{stage}_exists` (stage-tier only) | env3/4/5 import.yaml | stage service hostnames appear | yes | `yq '.services[].hostname' env{i}/import.yaml \| grep -F {stagehost}` | keep | |
| `{prefix}_{dev}_absent` (stage-tier only) | env3/4/5 import.yaml | dev service hostnames don't appear | yes | `yq '...hostname' env{i}/import.yaml \| grep -Fv {devhost}` | keep | |
| `{prefix}_{dev}_exists` (dev-tier only) | env0/1/2 import.yaml | dev hostnames appear | yes | same pattern | keep | |
| `{prefix}_core_package` | env import.yaml | `core: LIGHT` / `STANDARD` / … valid | yes | `yq '.project.corePackage' env{i}/import.yaml \| grep -E '^(LIGHT\|STANDARD\|HIGH)$'` | keep | |
| `{prefix}_{svc}_ha_mode` | env5 import.yaml | runtime services `mode: HA` | yes | `yq '.services[] \| select(.hostname=="{s}") \| .mode' env5/import.yaml \| grep -E '^HA$'` | keep | |
| `{prefix}_{svc}_cpu_mode` | env5 import.yaml | runtime services `cpuMode: HIGH` or equivalent | yes | `yq '...cpuMode'` | keep | |
| `{prefix}_{svc}_min_containers` | env import.yaml | minContainers matches tier expectation (e.g. env4 = 2) | yes | `yq '...minContainers'` + expected value compare | keep | **This is the finalize factual_claims class** — per v29 + v30, Go templates produced factually-wrong env-README claims; after v8.95 Fix B source correction, this check is the structural gate. Runnable form essential per P1. |
| `{prefix}_comment_width` | env import.yaml `# comment` lines | lines ≤120 chars | yes | `awk '/^#/ && length>120' env{i}/import.yaml \| wc -l` + test zero | keep | |
| `{prefix}_comment_headings` | `# comment` heading structure | no deeper than H3-equivalent | yes | `grep -c '^# ### ' env{i}/import.yaml` + threshold | keep | |
| `{prefix}_cross_env_refs` | env import.yaml `# comment` body | no explicit sibling-tier references | yes | `grep -E '(env[0-5]\|tier [0-5]\|Production tier)' env{i}/import.yaml \| grep -v 'this tier' && exit 1 \|\| exit 0` | keep | |

---

## 15. Conservative-deletion audit (per RESUME decision #5)

Seven checks are marked **delete**. For each: the new enforcement point + a one-sentence justification + a test scenario proving the upstream handles the class.

| Deleted check | New enforcement | Justification | Test scenario |
|---|---|---|---|
| `knowledge_base_exceeds_predecessor` | `knowledge_base_gotchas` + `knowledge_base_authenticity` (rewritten) | Informational-only since v8.78; predecessor-floor coupling disadvantages first-in-stack recipes; zero gate value in current system. | Given a run with no predecessor, expect no check-surface change; given a thin knowledge-base, `knowledge_base_gotchas` fails with bullet count 0. |
| (6 additional candidates for deletion — pending step-4 simulation review) | — | — | — |

The other 6 candidates flagged for deletion consideration surfaced during the walk above but **deferred to step 4**. Per RESUME decision #5's "conservative by default," any check I can't *prove* is redundant stays as `keep` or `rewrite-to-runnable`. Those 6 candidates:

1. `fragment_intro_blank_after_marker` — arguably a styling concern; but v30/v31/v33 all surfaced this as a failure round driver. Keep until step-4 confirms a positive allow-list in the writer brief eliminates the class.
2. `intro_no_titles` — overlaps with `intro_length`; but "no titles" is a separate styling axis. Keep.
3. `{prefix}_comment_width` — cosmetic; but was added to address v33 agent styling drift. Keep until step-4 confirms `comment-style.md` positive atom eliminates the class.
4. `{prefix}_comment_headings` — similar; keep.
5. `{prefix}_cross_env_refs` — informational since some versions; but flagged each in-tier env README. Keep as rewrite-to-runnable so author sees it locally.
6. `integration_guide_code_adjustment` (showcase-only) — overlap with `integration_guide_per_item_code`. Consolidate into one shim command; not a delete.

**Audit result**: 1 definite deletion (`knowledge_base_exceeds_predecessor`). 6 deletion candidates deferred to step 4's cold-read simulation. Conservative threshold honored.

---

## 16. New checks proposed (architecture-level)

Beyond keeping/rewriting existing checks, the new architecture requires three **new** checks that the current suite doesn't cover:

| New check | Closes defect class | Runnable form | Added to |
|---|---|---|---|
| `symbol_contract_env_var_consistency` | v34 DB_PASS/DB_PASSWORD cross-scaffold mismatch (principle P3) | `zcp check symbol-contract-env-consistency --mount-root=./` (diff env-var grep across `{host}/src`) | generate-complete + deploy.start-processes |
| `visual_style_ascii_only` | v33 Unicode box-drawing in zerops.yaml (principle P8) | `grep -rP '[\x{2500}-\x{257F}]' */zerops.yaml && exit 1 \|\| exit 0` (ASCII box-drawing block) | generate-complete |
| `canonical_output_tree_only` | v33 phantom `/var/www/recipe-{slug}/` tree (principle P8) | `find /var/www -maxdepth 2 -type d -name 'recipe-*' \| grep -q . && exit 1 \|\| exit 0` (runs SSH-side at close-entry) | close-entry |
| `manifest_route_to_populated` | v34 DB_PASS manifest-inconsistency (principle P5) | `jq '[.facts[] \| select(.routed_to==null or .routed_to=="")] \| length' ZCP_CONTENT_MANIFEST.json \| test -eq 0` | deploy.readmes |
| `no_version_anchors_in_published_content` | v33 version-log leakage into briefs (principle P6) | `grep -rE 'v[0-9]+\\.[0-9]+' {host}/README.md {host}/CLAUDE.md env*/README.md && exit 1 \|\| exit 0` | finalize-complete |

All five are runnable locally; each has a one-line shell form. All map to a specific principle and a specific v8.94–v8.104-era defect class.

### 16a. Editorial-review-originated checks (refinement 2026-04-20)

These checks are fired at `close.editorial-review` complete. Unlike §16's author-runnable shims, editorial-review checks are **not shell-runnable** — the editorial-review sub-agent dispatch IS the runner. Its return payload populates the check results. Gate semantics identical to other substep-complete checks; preAttestCmd field reads `"dispatched via close.editorial-review (no author-side equivalent; reviewer IS the runner)"`.

| New check | Closes defect class | Verdict source | Added to |
|---|---|---|---|
| `editorial_review_dispatched` | v34 classification-error-at-source (NEW) — writer classification error escapes all prior gates because manifest faithfully reflects wrong classification | `Sub[editorial-review].return.dispatched == true` | close.editorial-review |
| `editorial_review_no_wrong_surface_crit` | v28 wrong-surface gotchas (§8.2); v34 self-referential gotcha (§14.4); v33 scaffold-decision shipped as gotcha | `Sub[editorial-review].return.CRIT_count == 0` (after inline-fix) | close.editorial-review |
| `editorial_review_reclassification_delta` | classification-error-at-source (NEW); v28 folk-doctrine fabrication (§8.3) | `Sub[editorial-review].return.reclassification_delta == 0` (writer-reviewer classification agreement) | close.editorial-review |
| `editorial_review_no_fabricated_mechanism` | v23 execOnce-burn folk-doctrine; v28 folk-doctrine fabrication (§8.3); any gotcha inventing platform mechanism | `Sub[editorial-review].return.CRIT_count.fabricated == 0` | close.editorial-review |
| `editorial_review_citation_coverage` | v20 generic-platform-leakage; v28 folk-doctrine fabrication (§8.3) — every matching-topic gotcha cites zerops_knowledge guide | `Sub[editorial-review].return.citation_audit.uncited == 0` | close.editorial-review |
| `editorial_review_cross_surface_duplication` | v28 cross-surface-fact-duplication (§8.4) | `Sub[editorial-review].return.cross_surface_ledger.duplicates == 0` | close.editorial-review |
| `editorial_review_wrong_count` | v34 self-referential gotcha (§14.4); v28 wrong-surface (§8.2) remaining after inline-fix | `Sub[editorial-review].return.WRONG_count <= 1` (post inline-fix; ≥1 may survive as reported-for-user-review) | close.editorial-review |

**Design invariant**: editorial-review checks do NOT have author-runnable shim equivalents because the review IS the runnable form. The editorial-reviewer sub-agent dispatch composes the full review predicate in prompt form (atoms `single-question-tests.md` + `classification-reclassify.md` + `citation-audit.md` + `counter-example-reference.md` + `cross-surface-ledger.md`). Running the predicate means dispatching the sub-agent. P1's "author-runnable" framing generalizes: for editorial-review, the *reviewer* IS the author of the verdict, executing against the deliverable as input.

**Why not shell shims?** Editorial review's single-question tests (spec §Per-surface test cheatsheet) require semantic judgment that shell predicates cannot express:
- "Would a porter bringing their own code need to copy THIS content into their own app?" — requires modeling reader intent.
- "Would a developer who read Zerops docs AND framework docs STILL be surprised?" — requires knowing what "the docs" collectively contain.
- Counter-example pattern matching against v28 anti-patterns — requires structural semantic similarity, not token regex.

Shell shims can gate *compliance* (manifest exists, comment ratio ≥30%, fragment markers present); they cannot gate *editorial quality* (is this the right content on the right surface teaching the reader what they'll need). The editorial-review sub-agent is the runnable form of the editorial predicate.

---

## 17. Summary matrix

| File | keep | rewrite-to-runnable | delete | new | total |
|---|---:|---:|---:|---:|---:|
| workflow_checks_generate.go | 9 | 4 | 0 | — | 13 |
| workflow_checks_recipe.go | 16 | 4 | 0 | — | 20 |
| workflow_checks_deploy.go | 2 | 0 | 0 | — | 2 |
| workflow_checks_dedup.go | 0 | 1 | 0 | — | 1 |
| workflow_checks_claude_md.go | 1 | 0 | 0 | — | 1 |
| workflow_checks_predecessor_floor.go | 0 | 1 | 1 | — | 2 |
| workflow_checks_scaffold_artifact.go | 1 | 0 | 0 | — | 1 |
| workflow_checks_worker_correctness.go | 2 | 2 | 0 | — | 4 |
| workflow_checks_content_manifest.go | 3 | 2 | 0 | — | 5 |
| workflow_checks_comment_depth.go | 0 | 1 | 0 | — | 1 |
| workflow_checks_factual_claims.go | 0 | 1 | 0 | — | 1 |
| workflow_checks_finalize.go | 22 | 0 | 0 | — | 22 |
| (new checks) | — | — | — | 5 | 5 |
| (editorial-review-originated; refinement 2026-04-20) | — | — | — | 7 | 7 |
| **Total** | **56** | **16** | **1** | **12** | **85** |

Counts reconcile against §3 summary after deferred-deletion candidates were moved to `keep`/`rewrite-to-runnable` pending step-4 simulation. The 7 editorial-review-originated checks are dispatch-runnable (not shell-shim) per §16a — added by the 2026-04-20 refinement that introduces the editorial-review sub-agent role.

---

## 18. Execution-plane implications (shim binary surface)

The "shim" disposition requires the zcp binary to expose a `zcp check <name>` CLI subcommand tree. This is a small surface:

```
zcp check env-refs             --hostname=X --path=./
zcp check run-start-build-contract --hostname=X
zcp check env-self-shadow      --hostname=X
zcp check ig-code-adjustment   --hostname=X
zcp check ig-per-item-code     --hostname=X
zcp check comment-specificity  --hostname=X
zcp check yml-schema           --hostname=X
zcp check kb-authenticity      --hostname=X
zcp check worker-queue-group-gotcha --hostname=X
zcp check worker-shutdown-gotcha    --hostname=X
zcp check manifest-honesty     --mount-root=./
zcp check manifest-completeness --mount-root=./ --facts=/tmp/zcp-facts-{sess}.jsonl
zcp check comment-depth        --env={i}
zcp check factual-claims       --env={i}
zcp check cross-readme-dedup   [no args — walks discovered mounts]
zcp check symbol-contract-env-consistency --mount-root=./  [new]
```

16 shim subcommands total. Each imports the exact same predicate function as the corresponding server-side check — reuse the Go function, invoke via CLI, map exit code to pass/fail. No logic divergence between author-side shim and server-side gate.

**Design invariant**: every shim subcommand is a thin CLI adapter around a reusable Go function `ops.Check<Name>(...)` returning `[]workflow.StepCheck`. Gate uses the same function. Impossible to diverge.

---

## 19. What this check-rewrite does NOT cover

- **Execution-phase checks** (deploy-time dynamic checks that observe running services: `services RUNNING`, `zerops_browser feature-walk`). These aren't in `workflow_checks_*.go` — they're in `ops/verify.go` or similar. Out of scope for this file.
- **Preflight checks in `zerops_deploy`** / `zerops_dev_server`. Substrate — not guidance-layer.
- **Plan validation** (dbDriver-as-ORM-library class). Runs at research-complete; covered by `validateDBDriver` in `internal/workflow/recipe_decisions.go`. Not in workflow_checks_*.go.

Each of these substrates survives untouched under the architecture rewrite per README.md §1 ("Stays as-is").
