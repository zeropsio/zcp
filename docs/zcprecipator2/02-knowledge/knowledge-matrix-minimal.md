# knowledge-matrix-minimal.md

**Purpose**: enumerate, per (phase × substep × agent), every knowledge source available to the agent in a minimal-tier recipe run.

**Evidence base**:
- Spec-derived from source: [`internal/workflow/recipe_*.go`](../../../internal/workflow/), [`recipe_topic_registry.go`](../../../internal/workflow/recipe_topic_registry.go), [`recipe_guidance.go subStepToTopic`](../../../internal/workflow/recipe_guidance.go), [`recipe_substeps.go`](../../../internal/workflow/recipe_substeps.go)
- Deliverable evidence: `nestjs-minimal-v3` + `laravel-minimal-v1..v26` (shape & dispatched-brief templates)
- Tier mapping:  [`../01-flow/flow-minimal-spec-main.md`](../01-flow/flow-minimal-spec-main.md) + [`../01-flow/flow-comparison.md`](../01-flow/flow-comparison.md)
- Cells marked `(proxy)` carry the showcase v34 size where topic is tier-shared; cells marked `(schematic)` are block-text-only, not dispatch-interpolated. All such cells flagged per RUNBOOK §3 decision #1.

Confidence levels explicitly noted throughout:
- **high** = source-code-verified
- **medium** = deliverable-verified (v3 TIMELINE.md etc.)
- **low** = inferred without live observation

---

## Matrix legend

Same columns as `knowledge-matrix-showcase.md` §Legend. Per cell: evidence pointer + confidence tag.

---

## 1. Main agent — minimal tier (13 gated substeps vs showcase 18)

Run-level invariants identical to showcase: full main-agent MCP profile; facts log at `/tmp/zcp-facts-{sessionID}.jsonl`; `zerops_workspace_manifest` registered but usage not observed (no live log).

### 1.1 Research phase

| Substep | tool.permit | tool.forbid | eager.inlined | scoped.body | plan.fields | knowledge.guide | Confidence | Evidence |
|---|---|---|---|---|---|---|---|---|
| research (entry) | main profile | — | — | `research-minimal` only (~14 KB; NO `research-showcase` prepend) | intent text → `plan.Research` | `zerops_knowledge recipe=nestjs-minimal` expected (same MCP surface) | high (source) | [recipe_guidance.go:L100-130](../../../internal/workflow/recipe_guidance.go) + [flow-comparison.md §3](../01-flow/flow-comparison.md) |

### 1.2 Provision phase

| Substep | eager.inlined | scoped.body | plan.fields | env.catalog | knowledge.guide | Confidence | Evidence |
|---|---|---|---|---|---|---|---|
| provision (entry) | — | provision-framing + import-yaml-standard-mode + import-services-step + mount-dev-filesystem + git-config-mount + git-init-per-codebase + env-var-discovery + provision-attestation — ~20 KB (minimal omits `import-yaml-dual-runtime` since single framework runtime; omits unless hasBundlerDevServer = static frontend case) | plan.Research (single target) | delivered via 1 `zerops_discover` (vs showcase 3) | On-demand — `zerops-yaml-rules` likely fires; `dual-runtime-urls` only if static-frontend shape | medium (source + deliverable shape) | provision guide uses same topic-registry blocks; minimal path at [recipe.md:165-194](../../../internal/content/workflows/recipe.md) |

NB: minimal can ALSO be dual-runtime (nestjs-minimal-v3 had NestJS + minimal frontend) — dual-runtime flag is orthogonal to tier. When present, minimal gets `import-yaml-dual-runtime` and `dual-runtime-urls` on-demand just like showcase.

### 1.3 Generate phase substeps (4 — same count as showcase, one topic swap)

| Substep | eager.inlined | scoped.body | plan.fields | Confidence | Evidence |
|---|---|---|---|---|---|
| generate (entry) | — | step-entry from complete-provision return (~22 KB proxy) | plan.Research.* | medium | [flow-comparison.md §3](../01-flow/flow-comparison.md) |
| generate.scaffold | **`dev-server-host-check`** ONLY (recipe.md:711-715) when `hasBundlerDevServer`; **`scaffold-subagent-brief` is NOT eagerly injected for minimal** because topic-registry EagerAt condition is `isShowcase && multiCodebase` | `where-to-write-files-single` (recipe.md:411-420) — single-mount path | `plan.Research.PrimaryTarget`, `Hostname` (single) | high (source) | topic registry branch + [recipe.md:422-444 vs 411-420](../../../internal/content/workflows/recipe.md) |
| generate.app-code | — | **`execution-order`** (recipe.md:473-493, 21 lines) — minimal branch (NOT `dashboard-skeleton`) | `plan.Research.Features` | high | [recipe_guidance.go:L547-550](../../../internal/workflow/recipe_guidance.go) tier branch |
| generate.smoke-test | — | `on-container-smoke-test` (recipe.md:1263-1299, 37 lines) | — | high | tier-invariant |
| generate.zerops-yaml | — | `zerops-yaml-rules` composite (~155 lines, tier-invariant) | `plan.Research.Features` | high | tier-invariant |

### 1.4 Deploy phase substeps (9 — vs showcase 12)

| Substep | eager.inlined | scoped.body | plan.fields | knowledge.guide | Delta vs showcase | Confidence | Evidence |
|---|---|---|---|---|---|---|---|
| deploy (entry) | — | step-entry (~13 KB proxy) | plan.Research.Targets | — | minimal has single target — smaller interpolation | medium | tier-invariant topic |
| deploy-dev | **`fact-recording-mandatory` + `where-commands-run`** (same 44+43 lines as showcase — both topics are tier-invariant eager) | `deploy-flow` (~92 lines / ~9 KB proxy) | plan.Research.Targets | — | same | high | [topic registry EagerAt=SubStepDeployDev](../../../internal/workflow/recipe_topic_registry.go) |
| start-processes | (inherits deploy-dev eager) | `deploy-flow` substring (~1.6 KB proxy) | — | — | same | high | tier-invariant |
| verify-dev | — | `deploy-target-verification` (~22 lines / ~9 KB proxy with wrapping) | plan.Research.Targets | — | same topic | high | tier-invariant |
| init-commands | — | **`deploy-flow`** (the 21840 B subagent-brief return at this substep return is **showcase-only**; minimal returns `feature-sweep-dev` topic directly, ~3-4 KB) | plan.Research.Features | — | **minimal skips dev-deploy-subagent-brief carry-forward** — main writes features inline | high | [recipe_guidance.go:L556-563](../../../internal/workflow/recipe_guidance.go) isShowcase-gated payload |
| **subagent** | — | — | — | — | **DOES NOT EXIST** — see [recipe_substeps.go:L108](../../../internal/workflow/recipe_substeps.go) `if isShowcase` gate | high | source |
| **snapshot-dev** | — | — | — | — | **DOES NOT EXIST** — same gate | high | source |
| feature-sweep-dev | — | `feature-sweep-dev` (43 lines / ~10 KB proxy) | plan.Research.Features | — | fires right after init-commands (no subagent/snapshot between) | high | [recipe_substeps.go:L108](../../../internal/workflow/recipe_substeps.go) |
| **browser-walk** | — | — | — | — | **DOES NOT EXIST** — showcase-only substep | high | source |
| cross-deploy | — | `stage-deployment-flow` (69 lines / ~1.6 KB proxy) | plan.Research.Targets | — | same | high | tier-invariant |
| verify-stage | — | `deploy-target-verification` | — | — | same | high | tier-invariant |
| feature-sweep-stage | — | `feature-sweep-stage` (37 lines) — but **readmes carry-forward at this substep's return is different** (see next row) | plan.Research.Features | — | minimal carries `readme-fragments` (OLD v8) at return, not `content-authoring-brief` | high | [recipe_guidance.go:L588-591](../../../internal/workflow/recipe_guidance.go) tier branch |
| readmes | — | **`readme-fragments`** (recipe.md:2205-2388, 184 lines) — OLD v8 shape | plan.Research.Features | **?** — not observed; writer dispatch vs main-inline is discretionary | low — dispatch-vs-main-inline is reconstruction gap per [flow-minimal-spec-main.md:L26-L27](../01-flow/flow-minimal-spec-main.md) | tier branch source |

NB: nestjs-minimal-v3 TIMELINE shows main-agent inline README writing with no Agent dispatch at deploy.readmes, which means the minimal writer brief may never actually dispatch in practice. This is the single largest reconstruction gap (decision #1 lean: commission only if step-3 requires).

### 1.5 Finalize phase

| Substep | eager.inlined | scoped.body | plan.fields | Confidence | Evidence |
|---|---|---|---|---|---|
| finalize (entry) | — | `generate-finalize` (~14 KB proxy) | plan.Research.Targets + env import.yaml target list (single target vs 3) | medium | tier-invariant topic |
| (no substeps) | — | — | — | high | [recipe_substeps.go:L139-L150](../../../internal/workflow/recipe_substeps.go) |

### 1.6 Close phase — **no gated substeps** (critical delta vs showcase)

| Substep | eager.inlined | scoped.body | plan.fields | Confidence | Evidence |
|---|---|---|---|---|---|
| close (entry, ungated) | — | step-entry (~2 KB proxy) + optional code-review dispatch reference | plan.Research.* | high | source |
| code-review (ungated dispatch) | — | `code-review-subagent` block text (single-codebase interpolation) | plan.Research.Features, `{framework}`, `{appDir}` | medium — v3 TIMELINE L83 confirms dispatch fires in practice | [recipe.md:3050-3158](../../../internal/content/workflows/recipe.md) |
| close-browser-walk | — | — | — | — | **DOES NOT EXIST** for minimal (no dashboard to walk) |

---

## 2. Sub-agents — minimal tier

Minimal dispatches 0-2 sub-agents vs showcase 6. Two candidate briefs:

### 2.1 readme-with-fragments (discretionary dispatch — observed main-inline in v3)

| Knowledge source | Content | Confidence |
|---|---|---|
| `tool.permit` | Read/Edit/Write/Grep/Glob on SSHFS mount; Bash via `ssh {hostname}`; `zerops_knowledge`, `zerops_logs`, `zerops_discover` | high (block text) |
| `tool.forbid` | `zerops_workflow`, `zerops_import`, `zerops_env`, `zerops_deploy`, `zerops_subdomain`, `zerops_mount`, `zerops_verify` | high |
| `eager.inlined` | `readme-with-fragments` block (recipe.md:2205-2388, 184 lines) — fragment markers (3 IDs), CLAUDE.md template, gotcha authenticity rules | high |
| `scoped.body` | same block | high |
| `facts.read` | **NO explicit Prior Discoveries** — block is pre-v8.94 shape; no facts-log injection pattern | high (block grep) |
| `manifest` | — | high |
| `prior.return` | — | — |
| `plan.fields` | `{framework}`, `{prettyName}`, `{slug}`, `{hostname}dev` (single mount) | high |
| `env.catalog` | — | — |
| `knowledge.guide` | brief references `zerops_knowledge` but doesn't prescribe a citation-map call sequence | medium |
| `record_fact` calls | — (writer consumes facts; not producer) | schematic |
| Return size | (schematic) — estimated 5-8 KB with interpolation | low |
| Tool errors | (no live log) | — |

### 2.2 code-review-subagent (ungated, conventionally dispatched)

| Knowledge source | Content | Confidence |
|---|---|---|
| `tool.permit` | same as showcase — Read/Edit/Write/Grep/Glob on single mount; Bash via `ssh {hostname}`; `zerops_knowledge`, `zerops_logs`, `zerops_discover` | high |
| `tool.forbid` | same as showcase (includes `zerops_browser`, `agent-browser`) | high |
| `eager.inlined` | `code-review-subagent` block (recipe.md:3050-3158, 109 lines) | high |
| `scoped.body` | same | high |
| `facts.read` | block declares `IncludePriorDiscoveries=true` in topic registry — but **minimal has no gated close.code-review substep**, so BuildPriorDiscoveriesBlock is NOT injected (injection is substep-return path). Sub-agent receives only the block text | high (BuildPriorDiscoveriesBlock is substep-return-gated per [recipe_brief_facts.go](../../../internal/workflow/recipe_brief_facts.go)) |
| `manifest` | — | high |
| `plan.fields` | `{framework}` (e.g., "NestJS"), `{appDir}` single mount, `plan.Features` list | high |
| `knowledge.guide` | brief doesn't prescribe calls | high |
| `record_fact` calls | 0 (reports issues as [CRIT]/[WRONG]/[STYLE], inline fixes) | high |
| Return size | (schematic) ~6-7 KB with interpolation | low |

---

## 3. Eager topic injection — summary (minimal)

| Topic | Eager at substep | Block lines | Target agent | Notes vs showcase |
|---|---|---|---|---|
| `dev-server-host-check` | SubStepScaffold | 711-715 | main.generate.scaffold | same trigger condition (hasBundlerDevServer) — applies if minimal has static-frontend shape |
| `scaffold-subagent-brief` | SubStepScaffold | 790-1125 | **NOT eagerly injected for minimal** — trigger is `isShowcase && multiCodebase` | disappears |
| `fact-recording-mandatory` | SubStepDeployDev | 1423-1466 | main.deploy.deploy-dev | identical |
| `where-commands-run` | SubStepDeployDev | 1830-1872 | main.deploy.deploy-dev | identical |

---

## 4. Substep-scoped topic injection — summary (minimal)

| Substep | Scoped topic | Tier branch vs showcase |
|---|---|---|
| generate.scaffold | `where-to-write-files-single` (recipe.md:411-420) | **swap** vs `where-to-write-files-multi` (showcase) |
| generate.app-code | **`execution-order`** (recipe.md:473-493) | **swap** vs `dashboard-skeleton` (showcase) |
| generate.smoke-test | `on-container-smoke-test` | same |
| generate.zerops-yaml | `zerops-yaml-rules` composite | same |
| deploy.deploy-dev | `deploy-flow` | same |
| deploy.start-processes | `deploy-flow` | same |
| deploy.verify-dev | `deploy-target-verification` | same |
| deploy.init-commands | `deploy-flow` (NO subagent-brief carry-forward — showcase-only at [recipe_guidance.go:L556-563](../../../internal/workflow/recipe_guidance.go)) | **drop** |
| deploy.feature-sweep-dev | `feature-sweep-dev` | same |
| deploy.cross-deploy | `stage-deployment-flow` | same |
| deploy.verify-stage | `deploy-target-verification` | same |
| deploy.feature-sweep-stage | `feature-sweep-stage` + **`readme-fragments`** carry-forward at return | **swap** vs `content-authoring-brief` carry-forward |
| deploy.readmes | `readme-fragments` (recipe.md:2205) — OLD v8 | **swap** |
| close (ungated) | `code-review-subagent` (single-codebase interpolation) | same block, different interpolation |

---

## 5. Check surface per substep (minimal)

Tier-aware checks: most assertions are tier-invariant BUT several checks are guarded by `plan.Tier == RecipeTierShowcase` or `isShowcase`:

| Check | Tier gate | Runs for minimal? |
|---|---|---|
| `hostname_worker_setup` | requires `sharesCodebaseWith == ""` worker target | minimal=NO (no worker) |
| `hostname_scaffold_artifact_leak` | tier-invariant | YES |
| `hostname_env_self_shadow` | tier-invariant | YES |
| `integration_guide_code_adjustment` | showcase-only per [workflow_checks_recipe.go:L834-L873](../../../internal/tools/workflow_checks_recipe.go) | NO for minimal |
| `integration_guide_per_item_code` | showcase-only | NO |
| `comment_specificity` | showcase-only per file:L1090-L1122 | NO |
| `knowledge_base_exceeds_predecessor` | showcase-only | NO |
| `knowledge_base_authenticity` | showcase-only | NO |
| `hostname_gotcha_distinct_from_guide` (v8.96 Theme A) | tier-invariant | YES |
| `cross_readme_gotcha_uniqueness` | multi-codebase-only | NO for single-codebase minimal |
| `hostname_worker_queue_group_gotcha` | separate-codebase worker | NO |
| `hostname_worker_shutdown_gotcha` | separate-codebase worker | NO |
| `hostname_drain_code_block` | separate-codebase worker | NO |
| `hostname_claude_md_exists` | tier-invariant | YES |
| `writer_content_manifest_exists/_valid/_*` | tier-invariant for readmes substep | YES if writer fires (low confidence it does) |
| `env{i}_import_*` (~30 per env × 6 envs) | tier-invariant | YES — all 6 env-tier checks run for minimal too |

Rough count: showcase deploy.readmes has ~75 per-codebase checks × 3 = ~225; minimal has ~15 applicable checks × 1 codebase = ~15, plus 5 manifest checks IF writer ran. Cross-codebase checks collapse to zero for single-codebase minimal.

---

## 6. Coverage audit — minimal

**High-confidence cells** (source-verified): substep ordering, topic branches, tier gates on checks, block line ranges, plan-field shapes.

**Medium-confidence cells** (deliverable-verified): research-entry size inferred from block text; plan.Features content from v3 deliverable shape; CLAUDE.md/README.md existence.

**Low-confidence / schematic cells**: per-substep guide-size at minimal scale (proxy from showcase where topic shared), whether writer dispatch actually fires (v3 suggests no), TodoWrite cadence, `is_error` frequency, tool-error profile.

**Gaps explicitly not cell-evidenced** (see [`../01-flow/flow-minimal-spec-main.md:L22-L29`](../01-flow/flow-minimal-spec-main.md)):
1. Main-agent inline feature-writing tool mix / time / bytes at minimal scale.
2. Actually-interpolated minimal writer dispatch prompt (if it ever dispatches).
3. Live per-substep `guidance_landed` bytes at minimal scale.
4. Minimal-tier TodoWrite / fact-record cadence.

Escalation rule: commission a targeted `nestjs-minimal` or `laravel-minimal` run ONLY if step 3 architecture design surfaces a brief-composition question where live evidence is load-bearing. Per RESUME decision #1, reconstruction is the default for step 2.

---

## 7. Cell-count comparison vs showcase matrix

| Section | Showcase | Minimal |
|---|---:|---:|
| Main-agent per-substep rows | 18 (4+12+2 substeps + phase entries) | 13 (4+9+0 substeps + phase entries) |
| Sub-agent descriptor tables | 6 (scaffold×3, feature, writer, code-review) | 2 (writer discretionary, code-review ungated) |
| Eager-injection entries | 4 | 3 (drops scaffold-subagent-brief) |
| Scoped-injection entries | 14 | 14 (tier-swapped, not dropped) |
| Check-surface entries | all checks active | ~half (tier-gated out) |

Minimal matrix has ~65% of showcase's cell count — above the ~60% threshold in the step-2 instructions.
