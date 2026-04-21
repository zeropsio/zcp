# data-flow-minimal.md — sequence diagrams per phase

**Purpose**: sequence diagrams for the minimal-tier flow under the new atomic architecture. Minimal has 13 gated substeps vs showcase's 18 (flow-comparison.md §2 — 5 fewer: subagent, snapshot-dev, browser-walk, close.code-review-as-gate, close-browser-walk). Minimal dispatches 0–2 sub-agents vs showcase's 6. Ground truth is step 1 reconstruction (`flow-minimal-spec-main.md`) + step 2 `knowledge-matrix-minimal.md`; confidence tags from RESUME decision #1 carried forward where applicable.

Legend: same as [`data-flow-showcase.md`](data-flow-showcase.md).

---

## 1. What's different from showcase

Before per-phase diagrams, the delta summary (flow-comparison.md §1 + §2):

| Axis | Minimal | Showcase |
|---|---|---|
| Codebases / SSHFS mounts | 1 (single `appdev`) OR 2 (dual-runtime minimal) | 3 |
| Managed services | 1 (db) + app + stage = 3 services total | 14 total |
| `zerops_mount` calls | 1 | 3 |
| Worker target | never | conditional (separate-codebase default) |
| Scaffold sub-agent dispatch | **none** (single codebase → main writes scaffold inline; multi-codebase minimal → same scaffold dispatch as showcase) | 3 parallel |
| Feature sub-agent dispatch | **none** (main writes features inline) | 1 |
| Writer sub-agent dispatch | **discretionary** — per `flow-comparison.md §4`, nestjs-minimal-v3 TIMELINE shows main-inline writing; the atomic tree supports both paths but `phases/deploy/readmes.md` default is main-inline for minimal | 1 (always dispatched) |
| Code-review dispatch | **discretionary** — v3 TIMELINE confirms dispatch fires; minimal close has no gated substep, so dispatch is advisory not gated | 1 (always dispatched, gated substep) |
| Editorial-review dispatch | **discretionary default-on** — refinement 2026-04-20; dispatched Path B; fresh-reader premise load-bearing on minimal due to main-inline writer tier | 1 (always dispatched, gated substep at close.editorial-review) |
| Substep count: deploy | 9 | 12 |
| Substep count: close | 0 gated | 3 gated (editorial-review + code-review + browser-walk) |
| Writer brief shape | SAME atomic tree as showcase (`briefs/writer/*`); tier-conditional sections within atoms | SAME atomic tree |
| Editorial-review brief shape | SAME atomic tree (`briefs/editorial-review/*`); surface-walk-task tier-branches fewer surfaces | SAME atomic tree |

**Architectural decision** (per atomic-layout.md §7): minimal and showcase share `briefs/writer/*`. Current system has two different blocks (`readme-with-fragments` for minimal vs `content-authoring-brief` for showcase) — the atomic rewrite merges onto one brief shape with tier-conditional sections. Closes gap-map.md §10 (minimal canonical output tree inconsistency).

**Architectural decision** (per atomic-layout.md): minimal main-inline feature-writing uses the same atomic content surfaces as showcase feature-sub; main reads `briefs/feature/task.md` + principles during deploy work. No separate minimal-feature-brief.

---

## 2. Research phase

Identical shape to showcase §1 except step-entry atom composition:

```
M ──(1)── S : zerops_workflow action=start workflow=recipe
S ──(2)── M : step-entry = stitch(phases/research/entry.md +
                                   phases/research/symbol-contract-derivation.md +
                                   phases/research/completion.md)
              [research/entry.md carries tier-conditional sections — minimal + showcase branches live in the
               same atom; the atom is below 300 lines with both sections present]
M ──(3)── M : authors plan.Research (tier=minimal, single target usually)
M ──(4)── S : complete step=research
S ──(5)── C : validate plan.Research + compute SymbolContract (may be empty if single codebase and no NATS/S3)
C ──(6)── S : result
S ──(7)── M : response.DetailedGuide = phases/provision/entry.md + applicable import-yaml atom
```

**SymbolContract under minimal**: single-codebase minimal yields a trivial SymbolContract with Hostnames[] = [{role:primary,dev:appdev,stage:appstage}], empty NATSSubjects + NATSQueues, EnvVarsByKind limited to db (if any managed services). FixRecurrenceRules still applied where relevant (gitignore-baseline, env-self-shadow, no-scaffold-test-artifacts always; routable-bind, trust-proxy, graceful-shutdown when applicable to the framework). Dual-runtime minimal acquires the relevant URL-shape rules.

---

## 3. Provision phase

```
M ──(1)── S : complete step=research
S ──(2)── M : step-entry = stitch(phases/provision/entry.md +
                                   phases/provision/import-yaml/standard-mode.md   [minimal single-framework standard mode]
                                   OR phases/provision/import-yaml/static-frontend.md  [static-frontend minimal]
                                   OR phases/provision/import-yaml/dual-runtime.md    [dual-runtime minimal, e.g. nestjs-minimal-v3]
                                   + import-services-step.md + mount-dev-filesystem.md +
                                   phases/provision/git-config-container-side.md +
                                   phases/provision/env-var-discovery.md +
                                   phases/provision/provision-attestation.md +
                                   phases/provision/completion.md +
                                   pointer-include principles/where-commands-run.md)
M ──(3)── S : zerops_import
M ──(4)── S : zerops_mount × 1 (or 2 if dual-runtime with separate codebase; 1 is typical)
M ──(5)── M : ssh {hostname} "git config + git init + initial commit"  [single call per codebase]
M ──(6)── S : zerops_discover (typically 1 call vs showcase's 3)
M ──(7)── S : complete step=provision
S ──(8)── C : check services RUNNING + mount present + envs discoverable
S ──(9)── M : response.DetailedGuide = generate step-entry
```

**No `phases/provision/git-init-per-codebase.md` for single-codebase minimal** (atom's tier-conditional include only fires when multi-codebase). Closes misroute-map.md §1 substep-order-index scenario: minimal has fewer substeps so adding a new substep requires smaller lockstep updates.

---

## 4. Generate phase

Generate has 4 substeps (same count as showcase; topic branches differ).

### 4a — generate.scaffold (minimal single-codebase)

```
M ──(1)── S : complete step=provision  [→ generate step-entry + scaffold substep-entry]
S ──(2)── M : response.DetailedGuide = stitch(phases/generate/entry.md +
                                               phases/generate/scaffold/entry.md +
                                               phases/generate/scaffold/where-to-write-single.md   ← tier-branch, minimal
                                               [+ dev-server-host-check.md if hasBundlerDevServer])

[For single-codebase minimal: main writes scaffold INLINE. No Agent dispatch.]
M ──(3)── M : ssh {appdev} "npm create vite@latest ." or equivalent framework scaffolder
M ──(4)── M : Pre-attest runnable per FixRule (positive allow-list from SymbolContract.FixRecurrenceRules):
               - gitignore-baseline: grep '.gitignore' for node_modules + dist + .env + .DS_Store
               - env-example-preserved: grep .env.example presence
               - no-scaffold-test-artifacts: find scripts/ -name 'preship.sh'
               - skip-git: test ! -d /var/www/.git
M ──(5)── M : apply fixes per rules that failed
M ──(6)── S : complete generate.scaffold
S ──(7)── C : hostname-prefixed checks (scaffold_artifact_leak, env_self_shadow, claude_md_exists, zerops_yml_exists etc.)
S ──(8)── M : phases/generate/app-code/execution-order-minimal.md  ← tier-branch
```

**For multi-codebase minimal** (dual-runtime with separate frontend codebase): scaffold sub-agent dispatches fire. Exact same composition as showcase §3a, with 2 dispatches instead of 3 (no worker). The `briefs/scaffold/*` atoms are tier-invariant; addenda applied per role.

### 4b — generate.app-code (minimal)

```
M ──(1)── S : complete generate.scaffold  [→ app-code substep entry]
S ──(2)── M : phases/generate/app-code/execution-order-minimal.md + completion.md
              [NOT dashboard-skeleton-showcase.md — tier branch at stitch time]
M ──(3)── M : author app code inline per execution order
M ──(4)── S : complete generate.app-code
```

### 4c — generate.smoke-test (minimal)

Same as showcase §3b — tier-invariant atom.

### 4d — generate.zerops-yaml (minimal)

Same atomic set as showcase §3c, with tier-conditional sections in atoms (no worker-setup section if no worker target; no dual-runtime-consumption if not dual-runtime).

---

## 5. Deploy phase (minimal: 9 substeps)

Minimal deploy structure per knowledge-matrix-minimal.md §1.4:

```
M ──(1)── S : complete step=generate  [→ deploy entry]
S ──(2)── M : phases/deploy/entry.md + deploy-dev substep-entry

M ──(3)── [deploy-dev, start-processes, verify-dev — identical to showcase, single target]
M ──(4)── S : complete deploy.init-commands
S ──(5)── M : phases/deploy/feature-sweep-dev.md  ← direct next substep (NOT subagent/snapshot-dev/browser-walk — those are showcase-only)

[For minimal, main writes features INLINE. No Agent dispatch at deploy.subagent (no gated substep).]
M ──(6)── M : consult briefs/feature/task.md atom (delivered inline in phases/deploy/feature-sweep-dev.md's stitched content)
               plus pointer-includes for principles + SymbolContract awareness.
M ──(7)── M : implement remaining features inline
M ──(8)── F : record_fact on incidents + fixes (scope routing per principles/fact-recording-discipline.md)
M ──(9)── S : complete deploy.feature-sweep-dev

M ──(10)── S : complete deploy.cross-deploy (stage)
M ──(11)── S : complete deploy.verify-stage
M ──(12)── S : complete deploy.feature-sweep-stage
```

**Key decision**: minimal main uses the same `briefs/feature/task.md` + `briefs/feature/ux-quality.md` + pointer-included principles as showcase's feature sub-agent. Delivered inline at deploy-substep-entries. No separate minimal-feature atom tree. Atomic layout §7 preserves this tier-branching at the stitcher level, not at the atom level.

### 5a — deploy.readmes (minimal — writer dispatch OR main-inline)

```
M ──(1)── S : complete deploy.feature-sweep-stage  [→ readmes substep-entry]
S ──(2)── M : phases/deploy/readmes.md

[Two valid paths, author chooses at dispatch-composition time. Default for minimal: main-inline.]

Path A — main-inline (default for minimal):
M ──(3a)── M : consult briefs/writer/* atoms directly (they are tier-invariant)
M ──(4a)── M : author per-codebase README + CLAUDE.md + env READMEs + root README + env-comment-set
M ──(5a)── M : run self-review per surface pre-attest commands
M ──(6a)── N : write ZCP_CONTENT_MANIFEST.json
M ──(7a)── S : complete deploy.readmes

Path B — dispatch writer sub-agent (optional):
M ──(3b)── M : compose writer dispatch using same prompt as showcase §4d
M ──(4b)── Sub[writer] : Agent(prompt)
Sub ──(5b)── Sub/M/N as in showcase
M ──(7b)── S : complete deploy.readmes
```

**Per atomic-layout.md §7**: minimal defaults to Path A because nestjs-minimal-v3 TIMELINE shows main-inline; no load-bearing evidence of the dispatch path firing. Path B is preserved as an option for minimal recipes with high content complexity.

**Check surface on minimal readmes**: per knowledge-matrix-minimal.md §5, many showcase-only checks are skipped (`comment_specificity`, `knowledge_base_exceeds_predecessor`, `knowledge_base_authenticity`, `integration_guide_code_adjustment`, `integration_guide_per_item_code`). Writer content-manifest checks run uniformly (writer_content_manifest_exists, etc.) — Path A produces a manifest the same way Path B does.

```
S ──(X)── C : readmes checks fire (tier-filtered per check-rewrite.md §5)
           per-codebase (single for minimal): fragment markers, claude_md_exists, no_placeholders,
                                              intro_length, intro_no_titles, knowledge_base_gotchas,
                                              integration_guide_yaml, comment_ratio.
           content manifest: writer_content_manifest_exists, _valid, _discard_classification_consistency,
                              _manifest_honesty (ALL routing dimensions per P5), _manifest_completeness.
           skipped: showcase-only (comment_specificity, _exceeds_predecessor, _authenticity, _code_adjustment,
                                    _per_item_code).
S ──(Y)── M : if failures: M runs preAttestCmds locally, fixes, re-attests.
              if clean: phases/deploy/completion.md → phases/finalize/entry.md
```

---

## 6. Finalize phase (minimal)

```
M ──(1)── S : complete step=deploy  [→ finalize step-entry]
S ──(2)── M : stitch(phases/finalize/entry.md + phases/finalize/env-comment-rules.md +
                     phases/finalize/project-env-vars.md +
                     phases/finalize/review-readmes.md +
                     phases/finalize/completion.md)
              [phases/finalize/service-keys-showcase.md SKIPPED for minimal]
M ──(3)── M : compose envComments input (6 environments — same tier ladder as showcase)
M ──(4)── M : compose projectEnvVariables ONLY if dual-runtime
M ──(5)── M : Pre-attest runnable per env (same check commands as showcase, different service set)
M ──(6)── S : complete step=finalize
S ──(7)── C : per-env checks — same shape as showcase; fewer services to validate
S ──(8)── M : if clean: phases/close/entry.md
```

---

## 7. Close phase (minimal — ungated; editorial-review + code-review discretionary, default-on)

```
M ──(1)── S : complete step=finalize  [→ close step-entry (ungated in minimal)]
S ──(2)── M : stitch(phases/close/entry.md +
                     phases/close/editorial-review.md +          ← added refinement 2026-04-20; ungated-discretionary matching code-review
                     phases/close/code-review.md +
                     phases/close/export-on-request.md +
                     phases/close/completion.md)
              [phases/close/close-browser-walk.md SKIPPED — no dashboard to walk for minimal]

[Both editorial-review AND code-review dispatches are discretionary for minimal (ungated), default-on.
 Per refinement recommendation: ship Path B (dispatched) for editorial-review on minimal because the
 fresh-reader premise is especially load-bearing for main-inline writer tier — Path A writer (main-inline
 default for minimal per atomic-layout.md §7) means authorship and judgment collapse onto main without
 the fresh-context writer sub-agent as intermediary. An independent editorial reviewer restores the
 author/judge separation. Per atomic-layout.md §7 + spec-content-surfaces.md line 317-319.]

M ──(3)── M : compose editorial-review dispatch (same atom stitch as data-flow-showcase.md §6a):
              prompt = stitch(
                briefs/editorial-review/mandatory-core.md +
                briefs/editorial-review/porter-premise.md +
                briefs/editorial-review/surface-walk-task.md +
                briefs/editorial-review/single-question-tests.md +
                briefs/editorial-review/classification-reclassify.md +
                briefs/editorial-review/citation-audit.md +
                briefs/editorial-review/counter-example-reference.md +
                briefs/editorial-review/cross-surface-ledger.md +
                briefs/editorial-review/reporting-taxonomy.md +
                briefs/editorial-review/completion-shape.md +
                pointer-include principles/where-commands-run.md +
                pointer-include principles/file-op-sequencing.md +
                pointer-include principles/tool-use-policy.md +
                interpolate {manifestPath, factsLogPath}
              )
              [Surface-walk-task tier-branches: minimal walks fewer surfaces — single-codebase means 1
               IG/KB/CLAUDE.md set instead of 3; no worker codebase; 4 env tiers typical vs 6 showcase.
               Same spec tests apply per surface.]

M ──(4)── Sub[editorial-review] : Agent(prompt)
Sub ──(5)── Sub : cold-read every surface of the deliverable; apply per-surface single-question tests
Sub ──(6)── Sub : re-classify manifest facts; cross-surface ledger; citation audit
Sub ──(7)── Sub : apply inline fixes (CRIT wrong-surface items DELETED; WRONG items revised)
Sub ──(8)── M   : return { CRIT_count, WRONG_count, STYLE_count, reclassification_delta, per_surface_findings }

M ──(9)── M : compose code-review dispatch:
              prompt = stitch(
                briefs/code-review/mandatory-core.md +
                briefs/code-review/task.md +
                briefs/code-review/manifest-consumption.md +
                briefs/code-review/reporting-taxonomy.md +
                briefs/code-review/completion-shape.md +
                pointer-include principles/* +
                interpolate {manifestPath}
              )
M ──(10)── Sub[code-review] : Agent(prompt)
Sub ──(11)── Sub : framework-expert scan (single framework — e.g. NestJS or Laravel);
                   read ZCP_CONTENT_MANIFEST.json; verify routing honesty
Sub ──(12)── Sub : apply inline fixes for CRIT/WRONG
Sub ──(13)── M : return

M ──(14)── S : complete step=close  [no substep gating for minimal]
S ──(15)── M : phases/close/completion.md  [NextSteps EMPTY per P4 + P8]
```

**Why no browser-walk for minimal**: no dashboard to walk. Feature presence at feature-sweep-stage already verified via curl. Per knowledge-matrix-minimal.md §1.6.

**Why editorial-review is especially important for minimal** (refinement-level rationale):

Main-inline writer tier (Path A, minimal default) collapses the author/judge separation that the v8.94 fresh-context writer provides on showcase. On minimal, main-agent writes content inline during deploy.readmes with full session context (deploy rounds, debugging narratives, fix journals) loaded. This is exactly the "journal-not-reader-facing-document" failure mode the spec diagnoses (spec line 4-5). The editorial-review sub-agent re-introduces the author/judge split at close-phase — a fresh-context reader walks the deliverable applying the spec's tests. For minimal tier this is load-bearing; for showcase tier it's defense-in-depth on top of the fresh-context writer.

---

## 8. Convergence expectations for minimal

| Phase | Expected fail rounds at gate | Same as showcase? |
|---|---:|---|
| research | 0 | yes |
| provision | 0 | yes |
| generate | 0–1 | yes |
| deploy.readmes | ≤ 1 | yes (per P1 — runnable pre-attest per check) |
| finalize | ≤ 1 | yes |
| close | 0 (ungated) | N/A for minimal |

Per P1, minimal has fewer checks firing (tier-gated out per knowledge-matrix-minimal.md §5), so fail-round expectation is ≤ showcase. Convergence target is 1 round across all minimal runs.

---

## 9. Failure payload — identical shape

Minimal failure payloads are shape-identical to showcase (data-flow-showcase.md §9). `{ name, status, detail, preAttestCmd, expectedExit }`. No tier-specific payload fields.

---

## 10. Cross-tier invariants (new architecture)

| Invariant | Minimal | Showcase |
|---|---|---|
| Atomic tree | SAME (`internal/content/workflows/recipe/`) | SAME |
| `briefs/*` atoms | SAME (tier-invariant base + tier-conditional sections) | SAME |
| `principles/*` atoms | SAME | SAME |
| SymbolContract schema | SAME (smaller contract for single-codebase) | SAME |
| FactRecord.RouteTo enum | SAME | SAME |
| Writer manifest dimensions | SAME (all routing × surface pairs checked) | SAME |
| Pre-attest runnables | SAME commands; tier-gated inclusion | SAME |
| Version anchors | absent (P6) | absent |
| Dispatcher docs | docs/zcprecipator2/DISPATCH.md | SAME |
| Editorial-review atoms | SAME (`briefs/editorial-review/*`); Path B dispatched default-on | SAME (gated substep) |

---

## 11. What minimal-tier simulation in step 4 needs to verify

Per principle P7 and the deferral noted in RESUME decision #1 (escalation rule), step-4 verification must cold-read-simulate:

1. **Minimal main-inline feature-writing**: does `briefs/feature/task.md` read sensibly when main consumes it in-band (not transmitted-to-sub-agent)? Any dispatcher-implying verbs ("dispatch", "return") are P2 violations when main is the reader.
2. **Minimal writer Path A (main-inline)**: does `briefs/writer/*` stitch sensibly for main? Same P2 concern.
3. **Tier-conditional sections within atoms**: does the stitched output remain ≤ 300 lines when the tier-conditional sections are both present (atom editable) AND when only one fires (agent-visible)? Per atomic-layout.md §7.
4. **Minimal code-review dispatch discretion**: is it clear in `phases/close/code-review.md` whether dispatch is required or optional for minimal? If optional, what's the main-inline alternative path?
5. **Minimal editorial-review Path A vs Path B** (refinement 2026-04-20): recommendation is Path B (dispatched, default-on) because Path A main-inline writer on minimal already collapses author/judgment — adding Path A editorial-review would further collapse to main-as-its-own-reviewer, losing the entire point. Cold-read verifies `briefs/editorial-review/porter-premise.md` reads sensibly when transmitted-to-sub-agent (never consumed in-band).

These are the items that **might** force a commissioned minimal run per RESUME decision #1's escalation rule. They are documented here so step 4 has the explicit checklist to verify before escalating.

---

## 12. Non-deltas (preserved across tiers)

- Research step mechanics
- Provision step mechanics + SSH-side git handling
- Generate step structure (4 substeps)
- Deploy-dev → start-processes → verify-dev → init-commands sequence
- cross-deploy → verify-stage → feature-sweep-stage sequence
- Finalize step mechanics + env-comment authoring
- All substrate invariants (SUBAGENT_MISUSE, Read-before-Edit, SSHFS boundary, dev-server spawn shape, facts log)
- Fact schema + `RouteTo` enum
- Content surface rules + manifest contract
