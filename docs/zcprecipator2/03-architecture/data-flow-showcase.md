# data-flow-showcase.md — sequence diagrams per phase

**Purpose**: ground-truth sequence diagrams for the showcase-tier flow under the new atomic architecture, one per phase (research / provision / generate / deploy / finalize / close). Ground truth is step 1 v34 main + sub-agent traces + step 2 knowledge-matrix-showcase.md. Every arrow shows one of: server → main-agent delivery, main-agent → sub-agent composition, sub-agent → main-agent return, main-agent → server attestation, checker → failure-payload.

Legend:
- `S` = server (MCP tool handlers in `internal/tools/*`)
- `M` = main agent
- `Sub` = dispatched sub-agent (role suffixed; e.g. `Sub[scaffold-api]`)
- `C` = checker (invoked synchronously inside `zerops_workflow action=complete`)
- `F` = facts log (`/tmp/zcp-facts-{sessionID}.jsonl`)
- `N` = ZCP_CONTENT_MANIFEST.json (writer output, mount-root)

---

## 1. Research phase

```
M ──(1)── S : zerops_workflow action=start workflow=recipe, intent=<text>
S ──(2)── M : step-entry guide = stitch(phases/research/entry.md +
                                          phases/research/symbol-contract-derivation.md +
                                          phases/research/completion.md)
M ──(3)── M : authors plan.Research (targets, features, managed services, tier, dbDriver)
M ──(4)── S : zerops_workflow action=complete step=research
              payload: { plan.Research: {...} }
S ──(5)── C : validate plan.Research (dbDriver != ORM library,
                                        Research.Targets populated,
                                        SymbolContract derivable)
C ──(6)── S : StepCheckResult (pass/fail; if fail → failure-payload includes
                               the pre-attest runnable command for this check)
S ──(7)── M : response = { CurrentStep: provision,
                           DetailedGuide: buildStepEntry("provision"),
                           checks: C.result }
```

**Atomic composition at (2)**: atoms concatenated verbatim with `\n---\n` separators. No interpolation required (research step-entry is plan-independent up to this point).

**Attestation predicate at (4)**: `plan.Research.SymbolContract` is populated by the Go layer at step-complete (not by the agent). Research sub-step writes domain declarations; Go computes the derived contract from them. This puts the contract in main's context before the first scaffold dispatch.

**Failure payload shape at (6)**: per P1, check failure payload = `{ name, detail, preAttestCmd }`. No `ReadSurface/Required/Actual/CoupledWith/HowToFix/PerturbsChecks` (v8.96 + v8.104 verbose fields) — those are logged server-side for human inspection but not delivered to the agent. The agent needs "here's the command you run locally to see the same failure." Richer fields trained the agent to iterate on failure metadata (Fix E refutation, v34).

---

## 2. Provision phase

```
M ──(1)── S : zerops_workflow action=complete step=research  [triggers provision step-entry]
S ──(2)── M : step-entry = stitch(phases/provision/entry.md +
                                   phases/provision/import-yaml/<shape>.md  [shape=standard|static-frontend|dual-runtime] +
                                   phases/provision/import-yaml/workspace-restrictions.md +
                                   phases/provision/import-yaml/framework-secrets.md +
                                   phases/provision/import-services-step.md +
                                   phases/provision/mount-dev-filesystem.md +
                                   phases/provision/git-config-container-side.md   ←  new v8.93.1 shape preserved
                                   [+ phases/provision/git-init-per-codebase.md if multi-codebase] +
                                   phases/provision/env-var-discovery.md +
                                   phases/provision/provision-attestation.md +
                                   phases/provision/completion.md +
                                   pointer-include principles/where-commands-run.md)
M ──(3)── S : zerops_import (writes + validates import.yaml)
M ──(4)── S : zerops_mount × N codebases
M ──(5)── M : ssh {hostname} "git config + git init + initial commit"  [single container-side call per codebase]
M ──(6)── S : zerops_discover includeEnvs=true
S ──(7)── M : envs + svc IDs + URLs
M ──(8)── S : zerops_workflow action=complete step=provision
S ──(9)── C : check services RUNNING + mounts present + envs discoverable
C ──(10)─ S : result
S ──(11)─ M : response = { CurrentStep: generate,
                           DetailedGuide: stitch(phases/generate/entry.md + generate substeps entries + tier branching),
                           checks: C.result }
```

**Why git-config is container-side at (5)**: principle P2 (brief positively declares SSH-only) + P8 (one positive shape, no enumeration of forbidden zcp-side patterns). Closes v26 git-init zcp-side chown class + v33 pre-init `fatal: not a git repository` via `phases/provision/git-config-container-side.md` (which is a rewrite of current `git-config-mount` block with the post-scaffold "delete `.git/`, re-init" sequence folded positively).

**No on-demand `zerops_guidance` fetches expected** (v34 main fired 4 during provision). All topics that v34 pulled on-demand — `dual-runtime-urls`, `worker-setup-block`, `zerops-yaml-rules`, `comment-anti-patterns` — are either (a) atomized and eagerly stitched at step-entry when applicable (P6), or (b) pulled at the substep that needs them (zerops-yaml gets env-var-model + dual-runtime-consumption at its own substep-entry). Redundancy-map.md §1–§7 collapse of ~30 KB duplicated content removes the need for on-demand fetches.

---

## 3. Generate phase

Generate has 4 substeps (scaffold / app-code / smoke-test / zerops-yaml). Showcase dispatches 3 scaffolds in parallel at generate.scaffold.

### 3a — generate.scaffold substep

```
M ──(1)── S : zerops_workflow action=complete step=provision
S ──(2)── M : response.DetailedGuide = stitch(phases/generate/entry.md +
                                               phases/generate/scaffold/entry.md +
                                               phases/generate/scaffold/where-to-write-multi.md +
                                               phases/generate/scaffold/dev-server-host-check.md [if hasBundlerDevServer])

M ──(3)── M : compose 3 Agent dispatches using DISPATCH.md (human-facing, not transmitted):
              for codebase ∈ [api, frontend, worker]:
                prompt = stitch(
                  briefs/scaffold/mandatory-core.md +
                  briefs/scaffold/symbol-contract-consumption.md with {{.SymbolContract | toJSON}} +
                  briefs/scaffold/framework-task.md +
                  briefs/scaffold/<codebase>-codebase-addendum.md +
                  briefs/scaffold/pre-ship-assertions.md +
                  briefs/scaffold/completion-shape.md +
                  pointer-include principles/where-commands-run.md +
                  pointer-include principles/file-op-sequencing.md +
                  pointer-include principles/tool-use-policy.md +
                  pointer-include principles/platform-principles/01..06.md +
                  pointer-include principles/comment-style.md +
                  pointer-include principles/visual-style.md +
                  PriorDiscoveriesBlock(sessionID, substep=generate.scaffold)
                )

M ──(4a) ──→ Sub[scaffold-api]   : Agent(prompt)
M ──(4b) ──→ Sub[scaffold-frontend] : Agent(prompt)
M ──(4c) ──→ Sub[scaffold-worker] : Agent(prompt)

Each Sub:
  Sub ──(5)── Sub : read SymbolContract JSON; for each FixRule where hostname matches AppliesTo:
                    plan implementation to satisfy PositiveForm
  Sub ──(6)── Sub : implement scaffold (nest new / npm create vite / manual worker init)
  Sub ──(7)── Sub : run each applicable FixRule.PreAttestCmd via SSH; all must exit 0
  Sub ──(8)── F  : ssh .. record_fact on any platform observation + fix applied
  Sub ──(9)── M  : return { files-written, smoke-results, record-fact-summary }

M ──(10)── S : zerops_workflow action=complete step=generate substep=scaffold
S ──(11)── C : check each hostname has expected files + pre-ship artifacts absent
C ──(12)── S : result
S ──(13)── M : response.DetailedGuide = phases/generate/app-code/<tier>.md  [dashboard-skeleton-showcase]
```

**Key changes from v34 (and why they close specific classes)**:

- Step (3) composed prompt is **byte-identical** for `briefs/scaffold/symbol-contract-consumption.md` across the three dispatches (contract JSON is the same blob). Per P3, this closes v34 DB_PASS / DB_PASSWORD cross-scaffold mismatch. Scaffold sub-agents derive nothing from framework conventions; they read the contract.
- At step (7), the scaffold sub-agent runs **runnable pre-attest commands** per FixRule before returning. Any non-zero exit = the sub-agent fixes before returning. Per P1, this collapses the v22 NATS URL-embedded creds class + v30 worker SIGTERM missing class + v31 apidev `enableShutdownHooks()` class to author-side work.
- The dispatcher-facing instructions — "compress", "adapt per codebase", "include verbatim" — live in `docs/zcprecipator2/DISPATCH.md`. Main reads those at dispatch-composition time. Sub-agent's prompt is leaf. Per P2, this closes v32 dispatch-compression dropping Read-before-Edit.
- No version anchors in the prompt. Per P6.
- Every prohibition in the current brief (`common-deployment-issues`, `dual-runtime-what-not-to-do`, `comment-anti-patterns`) is rewritten as positive allow-list in the atom. Per P8.

### 3b — generate.app-code + generate.smoke-test

```
M ──(1)── S : zerops_workflow action=complete generate.scaffold  [→ app-code entry delivered]
S ──(2)── M : response.DetailedGuide = phases/generate/app-code/dashboard-skeleton-showcase.md +
                                        phases/generate/app-code/completion.md
M ──(3)── M : author dashboard skeleton inline (showcase health page)
M ──(4)── S : zerops_workflow action=complete generate.app-code
S ──(5)── M : response.DetailedGuide = phases/generate/smoke-test/entry.md +
                                        phases/generate/smoke-test/on-container-smoke-test.md
M ──(6)── M : ssh-side smoke test per codebase
M ──(7)── S : zerops_workflow action=complete generate.smoke-test
```

### 3c — generate.zerops-yaml substep

```
M ──(1)── S : zerops_workflow action=complete generate.smoke-test  [→ zerops-yaml entry delivered]
S ──(2)── M : response.DetailedGuide = stitch(phases/generate/zerops-yaml/entry.md +
                                               phases/generate/zerops-yaml/env-var-model.md +
                                               phases/generate/zerops-yaml/dual-runtime-consumption.md  [if dual-runtime] +
                                               phases/generate/zerops-yaml/setup-rules-dev.md +
                                               phases/generate/zerops-yaml/setup-rules-prod.md +
                                               phases/generate/zerops-yaml/setup-rules-worker.md  [if worker target] +
                                               phases/generate/zerops-yaml/setup-rules-static-frontend.md  [if static frontend] +
                                               phases/generate/zerops-yaml/seed-execonce-keys.md +
                                               phases/generate/zerops-yaml/comment-style-positive.md +
                                               phases/generate/zerops-yaml/completion.md)
M ──(3)── M : author zerops.yaml per codebase (all setups at once)
M ──(4)── M : Pre-attest runnable: `grep 'execOnce ${appVersionId}.*seed' */zerops.yaml && exit 1 || exit 0`
              (closes v33 seed-key class — static key per P1)
M ──(5)── M : Pre-attest runnable: `ops.DetectSelfShadows(zerops.yaml)` equivalent
              (closes v23 + v28 self-shadow class)
M ──(6)── S : zerops_workflow action=complete generate.zerops-yaml
              [triggers generate step-complete]
S ──(7)── C : buildGenerateStepChecker runs — all hostname-prefixed generate checks
C ──(8)── S : result (under new architecture: each failure carries its pre-attest runnable)
S ──(9)── M : response.DetailedGuide = phases/deploy/entry.md + deploy-dev substep entry
```

---

## 4. Deploy phase

Showcase deploy has 12 substeps. Full trace covers the load-bearing handoffs; compressed here for readability. Per P4, every substep attestation happens real-time (v8.90 held).

### 4a — deploy-dev → start-processes → verify-dev → init-commands

```
M ──(1)── S : complete step=generate  [→ deploy-dev entry]
S ──(2)── M : stitch(phases/deploy/entry.md + phases/deploy/deploy-dev.md +
                     principles/where-commands-run.md + principles/fact-recording-discipline.md)
M ──(3)── S : zerops_deploy × N codebases (setup=dev)
M ──(4)── S : complete deploy.deploy-dev
S ──(5)── M : phases/deploy/start-processes.md
M ──(6)── S : zerops_dev_server × N
M ──(7)── S : complete deploy.start-processes
S ──(8)── M : phases/deploy/verify-dev.md
M ──(9)── M : curl dev URLs; check platform principle compliance (0.0.0.0 bind, trust proxy)
M ──(10)── S : complete deploy.verify-dev
S ──(11)── M : phases/deploy/init-commands.md
M ──(12)── S : ssh {apidev} "execOnce bootstrap-seed-v1 -- seed && execOnce ${appVersionId} -- migrate"
M ──(13)── S : complete deploy.init-commands  [triggers next substep-entry = subagent dispatch]
S ──(14)── M : response.DetailedGuide = phases/deploy/subagent.md (showcase only)
```

### 4b — deploy.subagent (feature sub-agent)

```
M ──(1)── M : compose feature dispatch using DISPATCH.md:
              prompt = stitch(
                briefs/feature/mandatory-core.md +
                briefs/feature/symbol-contract-consumption.md with {{.SymbolContract | toJSON}} +
                briefs/feature/task.md +
                briefs/feature/diagnostic-cadence.md +    ← closes v33 probe-burst class (positive cadence: max 5 bash/min)
                briefs/feature/ux-quality.md +
                briefs/feature/completion-shape.md +
                pointer-include principles/where-commands-run.md +
                pointer-include principles/file-op-sequencing.md +
                pointer-include principles/tool-use-policy.md +
                pointer-include principles/platform-principles/01..06.md +
                pointer-include principles/fact-recording-discipline.md +
                PriorDiscoveriesBlock(sessionID, substep=deploy.subagent)
              )
M ──(2)── Sub[feature] : Agent(prompt)
Sub ──(3)── Sub : implement features across 3 mounts; cross-codebase contracts from SymbolContract
Sub ──(4)── F   : record_fact on incidents + fixes (scope=content/downstream/both)
Sub ──(5)── M   : return { features-implemented, files-touched, fact-summary }
M ──(6)── S : complete deploy.subagent
S ──(7)── M : phases/deploy/snapshot-dev.md
```

### 4c — deploy.snapshot-dev → feature-sweep-dev → browser-walk → cross-deploy → verify-stage → feature-sweep-stage

```
M ──(...) : standard substep progression, each substep's entry comes from phases/deploy/<substep>.md at complete-return
            Per P6: no cross-substep payload carry-forward. Writer dispatch composition happens at deploy.readmes dispatch time,
            not pre-loaded at feature-sweep-stage completion.

M ──(N)── S : complete deploy.feature-sweep-stage
S ──(N+1)─ M : phases/deploy/readmes.md  ← substep-entry ONLY; ~2 KB
              (NOT the 25 KB content-authoring-brief carry-forward. Per P6 + misroute-map.md §10.)
```

### 4d — deploy.readmes (writer sub-agent)

```
M ──(1)── M : compose writer dispatch at dispatch-composition time:
              prompt = stitch(
                briefs/writer/mandatory-core.md +
                briefs/writer/fresh-context-premise.md +
                briefs/writer/canonical-output-tree.md +      ← closes v33 phantom-tree class (positive form: every path enumerated)
                briefs/writer/content-surface-contracts.md +
                briefs/writer/classification-taxonomy.md +
                briefs/writer/routing-matrix.md +
                briefs/writer/citation-map.md +
                briefs/writer/manifest-contract.md +
                briefs/writer/self-review-per-surface.md +
                briefs/writer/completion-shape.md +
                pointer-include principles/where-commands-run.md +
                pointer-include principles/file-op-sequencing.md +
                pointer-include principles/tool-use-policy.md +
                pointer-include principles/comment-style.md +
                pointer-include principles/visual-style.md +
                interpolate {factsLogPath, SymbolContract (for citation consistency)}
              )

M ──(2)── Sub[writer] : Agent(prompt)

Sub ──(3)── Sub : read facts log directly from path
Sub ──(4)── Sub : classify every distinct FactRecord.Title (taxonomy)
Sub ──(5)── Sub : route per the routing matrix (per P5, every routed_to → destination tracked)
Sub ──(6)── Sub : author per-codebase README + CLAUDE.md + env READMEs + root README + env-comment-set
Sub ──(7)── Sub : run self-review-per-surface pre-attest commands (all must exit 0)
Sub ──(8)── N   : write ZCP_CONTENT_MANIFEST.json with every fact classified + routed_to + override_reason
Sub ──(9)── M   : return { file-byte-counts, manifest-summary }

M ──(10)── S : zerops_workflow action=complete deploy.readmes

S ──(11)── C : checkWriterContentManifest runs (expanded per P5):
                 - manifest_exists
                 - manifest_valid
                 - classification_consistency
                 - manifest_honesty  [all (routed_to, published-surface) pairs]
                 - manifest_completeness
               PLUS every per-codebase README check, every CLAUDE.md check, every env-README check.
               Each check emits { name, preAttestCmd, detail }.
C ──(12)── S : result
S ──(13)── M : if failures: response.checks = failures; M runs preAttestCmds locally, fixes, re-attests
               if clean: response.DetailedGuide = phases/deploy/completion.md
```

**Convergence under P1**: M runs the per-failure `preAttestCmd` locally before re-attesting. The gate becomes confirmation. v23's 5-round spiral, v33's 3-round loop, v34's 4-round loop: all architecturally eliminated because the check's verdict is re-derivable at author-side.

### 4e — deploy step complete

```
M ──(1)── S : complete step=deploy
S ──(2)── C : no additional deploy-level checks (all live on readmes substep)
S ──(3)── M : phases/finalize/entry.md
```

---

## 5. Finalize phase (no substeps)

```
M ──(1)── S : complete step=deploy  [→ finalize step-entry]
S ──(2)── M : stitch(phases/finalize/entry.md +
                     phases/finalize/env-comment-rules.md +
                     phases/finalize/project-env-vars.md +
                     phases/finalize/service-keys-showcase.md +
                     phases/finalize/review-readmes.md +
                     phases/finalize/completion.md)
M ──(3)── M : compose envComments input (one tailored set per environment 0-5)
M ──(4)── M : compose projectEnvVariables if dual-runtime
M ──(5)── M : Pre-attest runnable per env:
               - comment-ratio check (awk scan for # in context of non-blank YAML lines, target ≥30%)
               - comment-depth check (regex scan for WHY markers, target ≥35%)
               - cross_env_refs check (grep sibling-tier references)
               - factual_claims check (grep `minContainers: \d` + `mode: (HA|NON_HA)` against YAML truth)
M ──(6)── S : zerops_workflow action=complete step=finalize
              payload: { envComments, projectEnvVariables }
S ──(7)── C : buildFinalizeStepChecker runs (each check emits preAttestCmd)
S ──(8)── M : if failures: rerun preAttestCmds, fix, re-attest. if clean: phases/close/entry.md
```

**Convergence**: target ≤ 1 fail round (P1). v31 + v33 both saw 2-3 rounds because checks didn't emit runnable equivalents; under the new architecture, main ran the commands locally first.

---

## 6. Close phase

Showcase close has **3 substeps**: editorial-review + code-review + close-browser-walk (editorial-review added per research-refinement 2026-04-20; closes the spec-content-surfaces.md §Editorial review prescribed reviewer role).

### 6a — close.editorial-review (NEW — editorial-review sub-agent)

```
M ──(1)── S : complete step=finalize  [→ close step-entry + editorial-review substep-entry]
S ──(2)── M : stitch(phases/close/entry.md + phases/close/editorial-review.md)

M ──(3)── M : compose editorial-review dispatch at dispatch-composition time:
              prompt = stitch(
                briefs/editorial-review/mandatory-core.md +
                briefs/editorial-review/porter-premise.md +           ← YOU are the porter; fresh reader of the deliverable; no authorship investment
                briefs/editorial-review/surface-walk-task.md +        ← ordered walk: root/env README/env import.yaml/IG/KB/CLAUDE.md/zerops.yaml
                briefs/editorial-review/single-question-tests.md +    ← spec §Per-surface test cheatsheet per-surface pass/fail predicate
                briefs/editorial-review/classification-reclassify.md + ← re-run spec 7-class taxonomy independently; report writer-vs-reviewer delta
                briefs/editorial-review/citation-audit.md +           ← spec §Citation map — every matching-topic gotcha cites zerops_knowledge guide
                briefs/editorial-review/counter-example-reference.md + ← spec §Counter-examples (v28 anti-patterns) for pattern-match
                briefs/editorial-review/cross-surface-ledger.md +     ← running fact-ledger across surfaces; duplication catches
                briefs/editorial-review/reporting-taxonomy.md +       ← CRIT (wrong-surface) / WRONG (boundary+fabrication+uncited) / STYLE + inline-fix policy
                briefs/editorial-review/completion-shape.md +
                pointer-include principles/where-commands-run.md +
                pointer-include principles/file-op-sequencing.md +
                pointer-include principles/tool-use-policy.md +
                interpolate {manifestPath = <mount-root>/ZCP_CONTENT_MANIFEST.json,
                             factsLogPath = /tmp/zcp-facts-{sessionID}.jsonl}
              )
              [NO Prior Discoveries block — porter-premise requires fresh-reader stance; facts/manifest are pointers the reviewer may open, not pre-stitched context]

M ──(4)── Sub[editorial-review] : Agent(prompt)

Sub ──(5)── Sub : read every published surface cold — root README, environments/*/README.md, environments/*/import.yaml, {host}/README.md (intro/IG/KB fragments), {host}/CLAUDE.md, {host}/zerops.yaml
Sub ──(6)── Sub : for each published item, apply surface-specific single-question test (spec §Per-surface test cheatsheet):
                  - root README → "30-sec tier-decision test"
                  - env README → "tier-outgrow + tier-transition test"
                  - env import.yaml comment → "why-decision vs narrate-what test"
                  - IG item → "porter-copy test"
                  - KB gotcha → "platform-surprise-after-reading-both-docs test"
                  - CLAUDE.md → "operate-this-repo test"
                  - zerops.yaml comment → "trade-off-not-inferable-from-field-name test"
Sub ──(7)── Sub : re-classify every manifest fact independently; flag writer-classification vs reviewer-classification disagreements (classification-error-at-source catches)
Sub ──(8)── Sub : citation audit — every published gotcha whose topic is in the citation map MUST cite the zerops_knowledge guide verbatim
Sub ──(9)── Sub : cross-surface ledger — for each distinct fact, tally surfaces where the fact body appears (not cross-refs). Duplication count > 1 = WRONG.
Sub ──(10)── Sub : counter-example pattern match — scan against spec §Counter-examples from v28 (self-inflicted, framework-quirk, scaffold-decision disguised, folk-doctrine, factually-wrong, cross-surface-dup); pattern hits = CRIT or WRONG per type
Sub ──(11)── Sub : apply inline fixes for CRIT (wrong-surface items DELETED; not rewritten to pass — per spec §Per-surface test cheatsheet line 310 "Items that fail their surface's test are removed, not rewritten to pass")
Sub ──(12)─ M    : return { CRIT_count, WRONG_count, STYLE_count, reclassification_delta, surfaces_walked, per_surface_findings, inline_fixes_applied }

M ──(13)── S : zerops_workflow action=complete close.editorial-review
S ──(14)── C : editorial-review-originated checks fire (see check-rewrite.md §16a):
                 - editorial_review_dispatched
                 - editorial_review_no_wrong_surface_crit (CRIT count = 0 after inline fixes shipped)
                 - editorial_review_reclassification_delta (writer-classification vs reviewer-classification mismatch count)
                 - editorial_review_no_fabricated_mechanism (CRIT subclass = 0)
                 - editorial_review_citation_coverage (matching-topic gotcha citation 100%)
                 - editorial_review_cross_surface_duplication (count = 0)
                 - editorial_review_wrong_count (≤ 1 WRONG shipped after inline fix)
C ──(15)── S : result
S ──(16)── M : phases/close/code-review.md  [→ close.code-review substep-entry]
```

**Why editorial-review is FIRST in close** (per refinement §10 open-question #5, chosen sequential-editorial-first):
- Editorial catches content-revisions (wrong-surface deletion, reclassification, cross-surface duplication collapse); code-review sees the revised deliverable.
- Editorial's inline fixes change content, not code; code-review's scope is code quality — cleanly separated concerns.
- Alternative (parallel editorial + code-review) saves ~8-10 min wall but complicates diagnostic: if editorial CRIT AND code-review CRIT hit same file, fix order matters.

**Why Prior Discoveries is NOT included in editorial brief**:
- Prior Discoveries is the scaffold + feature recorded-fact accumulation. Useful for judging the *process*; contaminating for judging the *deliverable*. The porter-premise (spec line 4-5 root-cause diagnosis: *"the agent which debugs the recipe also writes the reader-facing content, and after 85+ minutes of debug-spiral its mental model is 'what confused me' rather than 'what a reader needs'"*) requires the reviewer to read the deliverable as a first-time reader. Facts log + manifest are provided as opt-in references (reviewer may Read them during reclassification or citation audit) but not pre-stitched into context.

### 6b — close.code-review

```
M ──(1)── S : complete close.editorial-review  [→ code-review substep-entry]
S ──(2)── M : phases/close/code-review.md

M ──(3)── M : compose code-review dispatch:
              prompt = stitch(
                briefs/code-review/mandatory-core.md +
                briefs/code-review/task.md +
                briefs/code-review/manifest-consumption.md +  ← closes v34 DB_PASS manifest↔content gotcha class
                briefs/code-review/reporting-taxonomy.md +
                briefs/code-review/completion-shape.md +
                pointer-include principles/where-commands-run.md +
                pointer-include principles/file-op-sequencing.md +
                pointer-include principles/tool-use-policy.md +
                PriorDiscoveriesBlock(sessionID, substep=close.code-review) +
                interpolate {manifestPath = <mount-root>/ZCP_CONTENT_MANIFEST.json}
              )

M ──(4)── Sub[code-review] : Agent(prompt)
Sub ──(5)── Sub : read ZCP_CONTENT_MANIFEST.json; verify all routing honesty dimensions
Sub ──(6)── Sub : framework-expert scan (NestJS / Svelte / TypeScript)
Sub ──(7)── Sub : feature-coverage scan (every plan.Features entry exercised)
Sub ──(8)── Sub : silent-swallow antipattern scan
Sub ──(9)── Sub : apply inline fixes for CRIT/WRONG
Sub ──(10)─ M   : return { CRIT_count, WRONG_count, STYLE_count, fixes-applied }

M ──(11)─ S : complete close.code-review
S ──(12)─ M : phases/close/close-browser-walk.md
```

### 6c — close.close-browser-walk

```
M ──(1)── S : complete close.code-review  [→ close-browser-walk entry]
M ──(2)── M : zerops_browser × each feature on stage
M ──(3)── S : complete close.close-browser-walk
S ──(4)── M : phases/close/completion.md  ← NextSteps is EMPTY (P4, P8)
M ──(5)── [stops at close-complete; export + publish triggered only by user message]
```

**Per P4 + P8**: NextSteps[] is empty at close-completion. v33 auto-export class architecturally impossible under new layout.

---

## 7. Cross-phase invariants (preserved or newly enforced)

| Invariant | Source | Behavior |
|---|---|---|
| SUBAGENT_MISUSE | v8.90 (substrate, preserved) | Any sub-agent calling `zerops_workflow` → server error. |
| Substep-scoped de-eager | v8.90 (substrate, preserved under P4) | `subagent-brief` / `readme-fragments` atoms delivered at substep-entry, not step-entry |
| Read-before-Edit | v8.97 Fix 3 (substrate, preserved under P2) | Edit tool enforces; brief principles/file-op-sequencing.md atom has a short positive reminder |
| SSH-only execution | v17.1 (substrate) + principles/where-commands-run.md | one positive-form rule atom; all briefs pointer-include |
| Git container-side | v8.93.1 + v8.96 Fix #4 (substrate preserved) + principles/where-commands-run.md | positive form in atom |
| Dev-server spawn shape | v17.1 (substrate, preserved) | not a guidance concern; tool-owned |
| FactRecord.Scope enum + filter | v8.96 Theme B (preserved) | `scope=downstream` facts flow into scaffold/feature briefs via PriorDiscoveriesBlock; content-scoped stay in writer lane |
| FactRecord.RouteTo | NEW (P5) | writer manifest enforces every fact has a route |
| SymbolContract | NEW (P3) | plan.Research.SymbolContract populated at research-complete; interpolated identically into every dispatch prompt |
| Editorial-review dispatch | NEW (refinement 2026-04-20) | close.editorial-review substep; editorial-reviewer sub-agent runs spec-content-surfaces.md's per-surface single-question tests on the cold-read deliverable; reports CRIT/WRONG/STYLE + reclassification delta; inline-fixes CRIT wrong-surface items before code-review runs |
| Fresh-reader invariant | NEW (refinement 2026-04-20) | editorial-review sub-agent receives NO Prior Discoveries block; porter-premise requires stance-as-reader-of-deliverable |

---

## 8. Attestation ordering and payload sizes (expected, showcase)

Under the new architecture, expected substep-return sizes (compared to v34 baseline — per knowledge-matrix-showcase.md §1.4):

| Substep | v34 observed | New (expected) | Rationale |
|---|---:|---:|---|
| research → provision | 22 KB | ~20 KB | atomic composition; minor reduction via duplication collapse |
| provision → generate | 42 KB | ~28 KB | -14 KB: 4 on-demand guidance calls eliminated (atoms stitched eagerly) + version anchors removed |
| generate.scaffold → app-code | — | ~3 KB | substep-entry minimal |
| generate.app-code → smoke-test | — | ~4 KB | |
| generate.smoke-test → zerops-yaml | — | ~12 KB | larger: env-var-model + seed-execonce-keys + dual-runtime-consumption |
| generate → deploy | 14 KB | ~10 KB | minor reduction |
| deploy-dev → start-processes | 9 KB | ~6 KB | principles/where-commands-run pointer-include once |
| deploy.init-commands → subagent | 22 KB (includes brief carry-forward) | ~4 KB | v8.90-eager-preserved substep-entry only; brief composed at dispatch time |
| deploy.subagent → snapshot-dev | 11 KB | ~3 KB | substep entry only |
| deploy.feature-sweep-stage → readmes | 28 KB (includes 25 KB writer-brief carry-forward) | ~3 KB | misroute-map.md §10 fix: writer brief composed at dispatch, not carry-forward |
| deploy.readmes complete (success) | ~2 KB | ~2 KB | matches |
| deploy → finalize | 19 KB | ~16 KB | |
| finalize (1-round success) | 19 KB | ~16 KB | |
| finalize → close.editorial-review | — | ~2 KB | substep-entry only; editorial-review dispatch composes at dispatch-time from ~6 KB of atoms (surface-walk + single-question-tests + classification-reclassify + citation-audit + counter-example-reference + cross-surface-ledger + reporting-taxonomy) + pointer-includes, for a ~8-10 KB transmitted prompt |
| close.editorial-review → code-review | — | ~2 KB | substep-entry only |
| close → close-browser-walk (now close.code-review → close-browser-walk) | ~2 KB | ~2 KB | |

Overall expected context reduction per run: **~35-45 KB** saved (primarily from misroute fix + on-demand-call elimination) minus **~12-15 KB added** from the editorial-review dispatch (one additional sub-agent prompt + return payload at close). Net reduction still positive (~20-30 KB); the cognitive-cost reduction is the target (fewer reconciliations per turn). The editorial-review cost is traded directly for the spec-prescribed reviewer role, closing the classification-error-at-source class v35 would otherwise ship.

---

## 9. Failure payload — new shape

Every check failure emitted to M has this shape:

```
{
  "name":        "worker_queue_group_gotcha",
  "status":      "fail",
  "detail":      "workerdev README knowledge-base fragment missing queue-group gotcha.",
  "preAttestCmd": "awk '/#ZEROPS_EXTRACT_START:knowledge-base/{f=1;next} /#ZEROPS_EXTRACT_END:knowledge-base/{f=0} f' workerdev/README.md | grep -iE 'queue.*group|queue:.*[\"'\\'']workers'",
  "expectedExit": 0
}
```

No `ReadSurface` / `Required` / `Actual` / `CoupledWith` / `HowToFix` / `PerturbsChecks`. Those existed in v8.96 Theme A / v8.104 Fix E and did not improve convergence (v34 data). Under P1, the payload directs the author to re-run the command locally and observe the same failure; the author fixes locally, then re-attests. The gate becomes confirmation.

Failure payload size: ~300-600 bytes per check. Typical readmes-substep failure round: 5 checks × 500 = 2.5 KB vs v34's 5 checks × (300 bytes detail + 150 ReadSurface + 150 Required + 150 Actual + 250 CoupledWith + 400 HowToFix + 150 PerturbsChecks) ≈ 7.5 KB. **~3× smaller payload**, but the point is convergence not byte reduction.

---

## 10. What this diagram set does NOT cover (deferred)

- **Runtime injection paths** (env var discovery delivering to scaffold brief): partially covered in §3a step (3); full injection contract is a step-5 regression fixture concern.
- **Per-role PriorDiscoveriesBlock filter rules**: substep-order index for de-eager correctness. Lives in `internal/workflow/atom_manifest.go` (new) + has its own test. See misroute-map.md §1 for the lockstep requirement.
- **Cold-compaction resilience**: how the new architecture behaves if Claude Code's context compaction fires mid-deploy. Substep state is server-side (P4), so recovery = replay from `zerops_workflow action=status`. No specific cold-resume diagram here; step-4 simulation validates.
- **Minimal tier**: covered in [`data-flow-minimal.md`](data-flow-minimal.md).
