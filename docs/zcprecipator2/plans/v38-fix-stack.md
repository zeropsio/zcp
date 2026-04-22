# plans/v38-fix-stack.md — seven Cx-commits before v38 commission

**Status**: TRANSIENT (per CLAUDE.md §Source of Truth #4). Archive to `plans/archive/` after v38 commission verdict ships.
**Prerequisites**: [`../HANDOFF-to-I9-v38-prep.md`](../HANDOFF-to-I9-v38-prep.md) §4 defect inventory, [`../runs/v37/verdict.md`](../runs/v37/verdict.md) §7.
**Target tag**: `v8.110.0`
**Estimated effort**: 3–4 days focused work. Cx-SUBAGENT-BRIEF-BUILDER is the multi-day one; the six atom/check edits are each 1–4 hours.

This doc is the execution plan the fresh instance follows. Each Cx-commit has a scope, files-touched list, RED test name, and acceptance criterion. Commits are ordered by dependency; the first three are independent and can be parallel.

---

## Root-cause recap from v37

The v37 run landed against tag `v8.109.0` with all six Cx commits from [`v37-fix-stack.md`](v37-fix-stack.md) merged at source. Despite that, four of six atom-level fixes had zero effect on the run. Root cause: **the main agent treats the 15 envelope atoms as research material rather than as stitching source**. It reads them via `dispatch-brief-atom`, then composes its OWN `Task()` prompt as a compressed paraphrase. During compression:

- Env folder names drop from canonical `0 — AI Agent` to slug `ai-agent` (main-agent memory fills the gap the paraphrase lost)
- Marker form drops the trailing `#` in description prose (even though the bash check in self-review-per-surface.md has the correct form)
- Standalone `INTEGRATION-GUIDE.md` + `GOTCHAS.md` bullets reappear from memory of prior runs
- Atom IDs like `briefs.editorial-review.per-surface-checklist` get invented that don't exist in the corpus

Proof: [v37/verdict.md §4 F-17](../runs/v37/verdict.md) + the atom→dispatch diff in [v37's checklist](../runs/v37/verification-checklist.md) under "Dispatch integrity".

The six existing v37 commits are source-correct. They just can't reach the run while the main agent owns prompt composition. v38's fix stack changes who owns prompt composition.

---

## Phase 0 — Prerequisites check (≤ 30 min)

1. Read [`../HANDOFF-to-I9-v38-prep.md`](../HANDOFF-to-I9-v38-prep.md) end to end.
2. Read [`../runs/v37/verdict.md`](../runs/v37/verdict.md) + [`../runs/v37/verification-checklist.md`](../runs/v37/verification-checklist.md).
3. Skim the writer dispatch prompt at [`../runs/v37/flow-showcase-v37-dispatches/author-recipe-readmes-claude-md-manifest.md`](../runs/v37/flow-showcase-v37-dispatches/author-recipe-readmes-claude-md-manifest.md) and compare to atom source at [`../../../internal/content/workflows/recipe/briefs/writer/canonical-output-tree.md`](../../../internal/content/workflows/recipe/briefs/writer/canonical-output-tree.md). **Feel the paraphrase**. Everything downstream hinges on making sub-agents see the atom bytes, not the paraphrase.
4. Verify clean working tree: `git status` → clean on `main`.
5. Verify baseline green: `go test ./... -count=1 -race` + `make lint-local`.

If any of 1–5 fails, stop and investigate.

---

## Phase 1 — Atom scope reduction (parallel-safe, Cx-1 through Cx-3)

These three commits edit atoms only. They can land in any order; they don't depend on each other or on the later engine work. Landing them first shrinks the surface the engine-built brief in Cx-4 has to cover.

### Cx-1 — Cx-WRITER-SCOPE-REDUCTION (closes surface-duplication root of F-9 / F-13)

**Scope**: delete writer prescription for root README, per-env README, and the bad `{{.ProjectRoot}}/` manifest path. Writer authors only: per-codebase README fragments + per-codebase CLAUDE.md + content manifest (at the recipe output root).

**Why now**: finalize already emits root README + env READMEs via [`recipe_templates.go:61-67`](../../../internal/workflow/recipe_templates.go#L61). The writer duplicates that work, and when the duplicate wins it's because `OverlayRealREADMEs` only covers per-codebase — everything else is stranded or overwritten. Stopping the writer from authoring these files removes the entire hallucination surface for env slug names.

**Files touched**:
- `internal/content/workflows/recipe/briefs/writer/canonical-output-tree.md` — rewrite per the §Fix-1 snippet in [v37 verdict §7](../runs/v37/verdict.md). Drop lines 14-20 (Per-environment files block) and line 24 (root README). Change line 25 manifest path from `{{.ProjectRoot}}/ZCP_CONTENT_MANIFEST.json` to the recipe output root: `/var/www/zcprecipator/{{.Slug}}/ZCP_CONTENT_MANIFEST.json` (or add a new `{{.RecipeOutputRoot}}` render field — see Fix-4 dependency).
- `internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md` — delete Surface 1 (Root README) and Surface 2 (Per-env README) sections in full. Renumber the remaining four surfaces (Per-codebase IG fragment → Surface 1, KB fragment → Surface 2, CLAUDE.md → Surface 3, env-comment-set payload → Surface 4). Update the Surface Summary table at the bottom to match.
- `internal/content/workflows/recipe/briefs/writer/self-review-per-surface.md` — delete "Surface 1 — Root README" and "Surface 2 — Per-env README" sections. Delete the root-README and env-README lines from the aggregate pre-attest bash block (lines matching `/var/www/README.md` and `/var/www/environments/`).
- `internal/content/workflows/recipe/briefs/writer/mandatory-core.md:9` — change the parenthetical `(per-codebase README, per-codebase CLAUDE.md, per-env README, root README, content manifest)` to `(per-codebase README fragments, per-codebase CLAUDE.md, content manifest)`.
- `internal/content/workflows/recipe/briefs/writer/completion-shape.md` — remove root/env rows from the `authored_files` shape if present; add a sentence reminding the writer the env-comment-set payload is the ONLY env output they produce.
- `internal/content/workflows/recipe/briefs/writer/fresh-context-premise.md` — add a negative: "**Env folder names are NOT your vocabulary.** Writing env-tier README files is finalize's job. Do not create any file or directory under `/var/www/environments/`. When you reference a tier in prose, use its prettyName from the plan (e.g. 'AI Agent', 'Small Production'); never invent a slug."

**RED tests**:

- `TestWriterAtoms_NoRootOrEnvReadmes` (new, in `internal/content/workflows/recipe/`)
  - Walk every `.md` under `internal/content/workflows/recipe/briefs/writer/`.
  - Assert zero occurrences of any of: `Root README`, `Surface 1 — Root`, `Surface 2 — Per-env`, `{{.ProjectRoot}}/README.md`, `/var/www/README.md`, `/var/www/environments/`.
- `TestWriterAtoms_NoEnvFolderSlugs` (new)
  - Assert zero occurrences of `ai-agent`, `remote-dev`, `local-dev`, `small-prod`, `prod-ha`, `stage-only`, `dev-and-stage-hypercde` (the main-agent hallucination vocabulary from v36 + v37).

**Green**: after atom edits, both tests pass.

**Acceptance on v38**: writer dispatch prompt contains zero references to root README / env READMEs / slug env names. Writer writes 7 files per run (3 README × 3 fragments + 3 CLAUDE.md + 1 manifest), not 20.

**Estimated**: 1–2 hours.

---

### Cx-2 — Cx-SCAFFOLD-FRAGMENT-FRAMES (closes F-12 at source)

**Scope**: pre-scaffold the per-codebase README on the mount at generate time with the exact fragment markers already in place. Writer never types a marker — it only replaces the placeholder line between markers via Edit.

**Why now**: the trailing-`#` marker defect is recurring because the writer types markers from memory (or from a prompt paraphrase that drops the `#`). If the markers are already on the mount, the writer physically cannot get them wrong — it Edits BETWEEN them.

**Files touched**:
- `internal/workflow/recipe_templates_app.go` — find the function that writes the per-codebase README scaffold at generate (look for `GenerateAppREADME` or similar in [`recipe_templates.go`](../../../internal/workflow/recipe_templates.go#L84)). Change the emitted template to:
  ```md
  # {hostname}

  <!-- #ZEROPS_EXTRACT_START:intro# -->
  <!-- REPLACE THIS LINE with a 1–3 line plain-prose intro naming the runtime + 3–5 managed services. No H2/H3 inside the markers. -->
  <!-- #ZEROPS_EXTRACT_END:intro# -->

  ## Integration Guide

  <!-- #ZEROPS_EXTRACT_START:integration-guide# -->
  <!-- REPLACE THIS LINE with 3–6 H3 items ("### 1. Adding zerops.yaml", "### 2. ...") each with a fenced code block. -->
  <!-- #ZEROPS_EXTRACT_END:integration-guide# -->

  ## Knowledge Base

  <!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
  <!-- REPLACE THIS LINE with "### Gotchas" followed by 3–6 bullets in `**symptom** — mechanism` form. -->
  <!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
  ```
- `internal/content/workflows/recipe/briefs/writer/canonical-output-tree.md` — add a sentence under the per-codebase section: "The file is pre-scaffolded with the three marker pairs. You Edit the placeholder line between each marker pair; you do NOT touch or retype the markers themselves."

**RED tests**:

- `TestAppScaffold_HasAllThreeFragmentMarkers` (new or extend existing)
  - Generate the scaffold via `GenerateAppREADME(plan)`.
  - Assert string contains all 6 marker lines with exact trailing `#`.
  - Assert the placeholder line between each pair is an HTML comment starting with `<!-- REPLACE THIS LINE`.
- `TestWriterFlow_NeverRetypesMarkers_Integration` (extend the engine-side test at `internal/workflow/recipe_overlay_test.go`)
  - Simulate a writer that Edits only between markers.
  - Assert final README passes all `fragment_*` + `fragment_*_blank_after_marker` checks from [`workflow_checks_recipe.go`](../../../internal/tools/workflow_checks_recipe.go).

**Green**: after scaffold edit + atom update + test additions.

**Acceptance on v38**: zero `fragment_marker` or `fragment_*_blank_after_marker` check failures across the writer-1 first pass.

**Estimated**: 2–3 hours.

---

### Cx-3 — Cx-ENV-COMMENT-PRINCIPLE (closes F-21)

**Scope**: the env-comment-set payload the writer returns cannot contain specific numeric claims (`2 GB`, `minContainers: 3`, `30s TTL`) unless the number literally appears in the adjacent YAML block being commented. Default to qualitative phrasing.

**Why now**: v37 cycle-6 of finalize had 15 `import_factual_claims` failures across all 6 env tiers because writer invented numbers (2 GB / 20 GB / 100 GB quotas, `minContainers: 1/3`) that didn't match the platform-auto-generated YAML. Writer had to retry with aspirational phrasing; the retry should have been the default.

**Files touched**:
- Find the env-comment writer atom (probably at `internal/content/workflows/recipe/briefs/writer/env-comment-set.md` or under `briefs/writer/` — grep `env-comment`). Add a `## Factuality rule` section:
  > Numbers in your comment must come from the YAML block the comment is attached to, verbatim. If the YAML has `objectStorageSize: 1`, your comment may say "1 GB quota" but not "2 GB" or "modest". If the YAML has no number you want to reference, use qualitative phrasing ("single-replica", "HA mode", "modest quota") — never invent a number from memory.
- Tighten `import_factual_claims` check in `internal/tools/workflow_checks_recipe.go` (or `internal/ops/checks/manifest.go` — grep `import_factual_claims`) so the failure detail quotes the specific mismatch: `comment claims "2 GB" but adjacent YAML has objectStorageSize: 1` rather than a generic "numeric claim differs".

**RED tests**:
- `TestEnvCommentFactuality_RejectsInventedNumber` (new)
  - Fixture import.yaml with `objectStorageSize: 1` + comment `# 2 GB quota`.
  - Assert check fails with detail naming both strings.
- `TestEnvCommentFactuality_AcceptsQualitativePhrasing` (new)
  - Fixture with `# Single-replica production` + no `minContainers` claim.
  - Assert check passes.

**Green**: after atom + check edits.

**Acceptance on v38**: finalize `import_factual_claims` failure count ≤ 1 across all 6 env tiers on first pass.

**Estimated**: 2–3 hours.

---

## Phase 2 — Staging / overlay (Cx-4)

### Cx-4 — Cx-MANIFEST-OVERLAY (closes F-23)

**Scope**: writer-authored `ZCP_CONTENT_MANIFEST.json` at the recipe output root gets overlayed into the finalize output tree, same mechanism as `OverlayRealREADMEs` does for per-codebase files. Covers the v37 gap where writer authored the manifest but it never reached the deliverable.

**Why now**: depends on Cx-1 for the path move (manifest is now at `/var/www/zcprecipator/{slug}/` per the scope-reduction atom edit). Independent of Cx-2 / Cx-3.

**Files touched**:
- `internal/workflow/recipe_overlay.go` — add `OverlayManifest` function:
  ```go
  func OverlayManifest(files map[string]string, plan *RecipePlan) bool {
      base := recipeMountBase
      if recipeMountBaseOverride != "" { base = recipeMountBaseOverride }
      src := filepath.Join(base, "zcprecipator", plan.Slug, "ZCP_CONTENT_MANIFEST.json")
      data, err := os.ReadFile(src)
      if err != nil { return false }
      if !json.Valid(data) { return false }
      files["ZCP_CONTENT_MANIFEST.json"] = string(data)
      return true
  }
  ```
- `internal/workflow/recipe_templates.go:BuildFinalizeOutput` — call `OverlayManifest(files, plan)` alongside `OverlayRealREADMEs`. Log whether the overlay succeeded.
- `internal/workflow/recipe_overlay_test.go` — add `TestOverlayManifest_CopiesValidJSON` (setup temp mount with a valid manifest, assert overlayed) + `TestOverlayManifest_SkipsInvalidJSON` (malformed manifest → not overlayed, returns false).

**RED test**: `TestOverlayManifest_CopiesValidJSON` — setup temp mount base via `recipeMountBaseOverride`, write `<base>/zcprecipator/<slug>/ZCP_CONTENT_MANIFEST.json`, call `OverlayManifest`, assert `files["ZCP_CONTENT_MANIFEST.json"]` set.

**Green**: after implementation.

**Acceptance on v38**: `ZCP_CONTENT_MANIFEST.json` appears at `nestjs-showcase-v38/ZCP_CONTENT_MANIFEST.json` and `zcp analyze recipe-run` / B-23 equivalent flags its presence.

**Estimated**: 2 hours.

---

## Phase 3 — The headline fix (Cx-5)

### Cx-5 — Cx-SUBAGENT-BRIEF-BUILDER (closes F-17 — the reason v37 failed)

**Scope**: new engine action `zerops_workflow action=build-subagent-brief role=<role>` returns a fully-stitched, plan-interpolated, ready-to-dispatch brief. Main agent's job becomes `Task(prompt=<that>)` verbatim. Engine-side guard refuses `Task` dispatches where the prompt doesn't match the last-built brief hash for the declared role.

**Why now**: the four atom-level Cx commits from v37 (ENVFOLDERS-WIRED, MARKER-FORM-FIX, STANDALONE-FILES-REMOVED, ATOM-TEMPLATE-LINT) all target atom content. None of them change the compression step where corruption happens. Until a non-paraphraseable path exists, further atom edits are theatre.

**Architecture**:

```
current:
  engine → 15 atoms via dispatch-brief-atom → main agent reads, paraphrases → Task(prompt=paraphrase)
                                                                              ↑ corruption
target:
  engine → one brief via build-subagent-brief → main agent forwards → Task(prompt=verbatim brief)
                                                                      ↑ verified verbatim via hash guard
```

**Files to create**:
- `internal/workflow/subagent_brief.go` — stitching logic per role. Functions:
  - `BuildWriterBrief(plan *RecipePlan) (string, error)` — concatenates writer atom bodies in canonical order (mandatory-core → fresh-context-premise → canonical-output-tree → content-surface-contracts → classification-taxonomy → routing-matrix → citation-map → manifest-contract → self-review-per-surface → completion-shape → principles.file-op-sequencing → principles.tool-use-policy → principles.fact-recording-discipline → principles.comment-style → principles.visual-style). Renders templates via `LoadAtomBodyRendered`. Returns the full stitched string (expect ~16–20 KB).
  - `BuildEditorialReviewBrief(plan *RecipePlan, manifestPath, factsLogPath string) (string, error)` — same for editorial-review atoms.
  - `BuildCodeReviewBrief(plan *RecipePlan) (string, error)` — for code-review.
- `internal/tools/workflow.go` — new handler `handleBuildSubagentBrief(ctx, sessionID, role) (*BuildSubagentBriefResult, error)`. Result shape:
  ```go
  type BuildSubagentBriefResult struct {
      Role        string `json:"role"`
      Prompt      string `json:"prompt"`             // the full verbatim brief
      Description string `json:"description"`         // "Author recipe READMEs + CLAUDE.md + manifest"
      PromptSHA   string `json:"prompt_sha"`          // sha256 of Prompt
      NextTool    NextToolHint `json:"next_tool"`     // hint to call Task with these args
  }
  ```
  Store `PromptSHA` in `WorkflowState.LastSubagentBrief[role]` for the dispatch guard.
- `internal/tools/workflow_dispatch_guard.go` (new) — a pre-dispatch hook registered on `Task`. When `Task.input.description` matches a known role keyword (`"README"` / `"manifest"` / `"editorial"` / `"code-review"` / `"writer"`), look up the session's `LastSubagentBrief[role]`. Hash the submitted `Task.input.prompt`. If hash mismatches OR no brief was built for this role in this session, return `SUBAGENT_MISUSE` with remediation: `"writer dispatch must use the engine-built brief — call zerops_workflow action=build-subagent-brief role=writer, then pass its .prompt to Task verbatim"`. Closed escape hatch: not available (security-critical; treat as architectural invariant).

**Files to modify**:
- `internal/tools/workflow.go` — wire `handleBuildSubagentBrief` into the `zerops_workflow` tool switch.
- `internal/workflow/engine_recipe.go` — ensure `LastSubagentBrief` state survives compaction (per `spec-work-session.md`).
- `internal/content/workflows/recipe.md` (workflow substep guide for deploy-readmes + close-editorial-review etc.) — replace "dispatch the writer sub-agent via Task(…)" language with "call `zerops_workflow action=build-subagent-brief role=writer` and dispatch via `Task(description=result.description, prompt=result.prompt, subagent_type='general-purpose')`".

**RED tests**:
- `TestBuildWriterBrief_ByteIdenticalAtoms` (new, in `internal/workflow/subagent_brief_test.go`)
  - Build brief against showcase fixture plan.
  - Assert result contains the full body of every writer atom (grep per atom, substring match).
  - Assert zero `{{` tokens survive.
  - Assert zero mentions of `INTEGRATION-GUIDE.md`, `GOTCHAS.md`, `ai-agent`, `remote-dev` (combined with Cx-1 atom cleanup).
- `TestBuildEditorialReviewBrief_RejectsMissingAtom` (new)
  - Temporarily break an atom path (e.g. rename `citation-audit.md`).
  - Assert `BuildEditorialReviewBrief` returns error naming the missing atom.
  - (Cf. v37's `briefs.editorial-review.per-surface-checklist unknown` — the engine will fail loud instead of leaving the hallucinated ID to the main agent.)
- `TestDispatchGuard_RefusesParaphrasedTask` (new, in `internal/tools/workflow_dispatch_guard_test.go`)
  - Call `build-subagent-brief role=writer`, capture prompt.
  - Submit `Task(description="Author recipe READMEs", prompt=<modified version of prompt>)`.
  - Assert `SUBAGENT_MISUSE` with exit code + remediation.
- `TestDispatchGuard_AcceptsVerbatimBrief` (new)
  - Same flow but submit prompt verbatim.
  - Assert dispatch accepted.

**Green**: all four tests pass + existing workflow tests remain green.

**Acceptance on v38**: writer dispatch prompt at `runs/v38/flow-showcase-v38-dispatches/author-recipe-readmes-claude-md-manifest.md` byte-equals `BuildWriterBrief(plan)` Go-source output. Harness `B-24_dispatch_integrity` reports `diff_status=clean` for every role.

**Estimated**: 2–3 days. This is the biggest commit. Decompose if needed:
- **Sub-commit 5a**: `subagent_brief.go` + `BuildWriterBrief` + its test (RED-GREEN).
- **Sub-commit 5b**: `handleBuildSubagentBrief` tool handler + state field + test.
- **Sub-commit 5c**: dispatch guard + its two tests.
- **Sub-commit 5d**: workflow-guide text updates pointing main agent at the new action.
- **Sub-commit 5e**: `BuildEditorialReviewBrief` + `BuildCodeReviewBrief` + tests.

Each sub-commit lands with green tests. Ship as a single Cx-5 merge once all five are green.

---

## Phase 4 — Check tightening (Cx-6)

### Cx-6 — Cx-VERSION-ANCHOR-SHARPEN (closes F-22)

**Scope**: the `no_version_anchors_in_published_content` check stops firing on version-suffixed identifiers inside fenced code blocks (e.g. `bootstrap-seed-v1` as an `execOnce` key in a YAML block).

**Why now**: independent of Cx-1..Cx-5. Small fix; ship it before v38 to remove known false-positive noise.

**Files touched**:
- `internal/tools/workflow_checks_recipe.go` or `internal/ops/checks/manifest.go` — find the `no_version_anchors_in_published_content` implementation. Add fenced-code-block exclusion + compound-identifier exclusion.
- Add test in the same file's `_test.go`.

**RED test**: `TestVersionAnchor_SkipsFencedCodeBlock`
- Fixture README with `v1` inside a `\`\`\`yaml` block and separately in prose.
- Assert: prose match fails (expected), code block match passes (new).

`TestVersionAnchor_AcceptsCompoundIdentifier`
- Fixture with `bootstrap-seed-v1` (compound).
- Assert: passes.

`TestVersionAnchor_RejectsBareProseVersion`
- Fixture prose "now on v2".
- Assert: still fails.

**Green**: after implementation.

**Acceptance on v38**: zero `no_version_anchors` failures in the run.

**Estimated**: 1–2 hours.

---

## Phase 5 — Harness sharpness (Cx-7)

### Cx-7 — Cx-HARNESS-V2 (three bar patches + close-step signal)

**Scope**: fix the four harness bar-sharpness issues surfaced by v37 analysis (`runs/v37/verdict.md §5`).

**Why now**: v38 analysis will depend on the harness. Without these patches, (a) F-9 regressions at `environments-generated/` shape don't get caught, (b) legitimate post-close exports get flagged as F-8 regressions, (c) writer detection fails on description names like "Author recipe READMEs", (d) close-step completion is a false negative.

**Files touched**:
- `internal/analyze/structural.go` — `checkB15GhostEnvDirs`:
  - Current: reads only `environments/` subdir.
  - New: walk depth-1 under deliverable root; flag any dir whose name matches `^environments[-_]` (e.g. `environments-generated`, `environments_v2`). Canonical `environments/` stays whitelisted.
- `internal/analyze/session.go` — `checkB21SessionlessExportAttempts`:
  - Current: counts every `zcp sync recipe export` without `--session`.
  - New: ignore exports whose timestamp is ≥ `close_step_completed_at` in the same session. Needs `close_step_completed_at` added to `WorkflowCallScan`.
- `internal/analyze/session.go` — writer-detection in `checkB23WriterFirstPassFailures`:
  - Current: match on role-specific keyword that misses v37 description.
  - New: match on dispatch description containing any of: `"README"`, `"manifest"`, `"writer"`, `"Author recipe"`. Case-insensitive.
- `internal/analyze/session.go` — close-step completion detection:
  - Current: requires `checkResult.passed=true` on `complete step=close`.
  - New: EITHER `checkResult.passed=true` OR `progress.steps[close].status=="complete"` (secondary signal from the engine response shape v37 exposed). Both are acceptable.

**RED tests** (add to `internal/analyze/structural_test.go` + `session_test.go`):
- `TestB15GhostEnvDirs_CatchesEnvironmentsGenerated` — fixture deliverable with `environments/` + `environments-generated/`; assert B-15 fails with count=1.
- `TestB21SessionlessExport_IgnoresPostClose` — fixture session with close-complete at T, export at T+1; assert observed=0.
- `TestB23WriterFirstPass_MatchesAuthorDescription` — fixture session with dispatch description `"Author recipe READMEs + CLAUDE.md + manifest"`; assert bar runs (not `skip`).
- `TestCloseStepCompleted_RecognisesProgressSteps` — fixture with `progress.steps[close].status="complete"` but no `checkResult`; assert detected.

**Green**: after patches + tests.

**Acceptance on v38**: harness machine-report on v38 reflects true state of each bar without false positives/negatives. Analyst does not have to add bar-sharpness caveats to the checklist (as they did for v37).

**Estimated**: 3–4 hours.

---

## Phase 6 — Integration verification (≤ 2 hours)

Before tagging v8.110.0:

1. `go test ./... -count=1 -race` — full suite green including the ~12 new tests from this stack.
2. `make lint-local` green.
3. **Retrospective harness run** against v37 deliverable — `zcp analyze recipe-run /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37 …`. Expected behavior: same fail pattern as before (the v37 tree didn't get the new fixes), but B-15 now catches the `environments-generated/` tree (observed=1), B-21 now excludes post-close exports (observed=0), B-23 now detects the writer dispatch (runs instead of `skip`).
4. **Forward harness run** against a staged test directory (you build it locally — 6 canonical env dirs with correct markers, per-codebase markdown present, ZCP_CONTENT_MANIFEST.json at root, tarball contents correct). All bars should PASS.
5. Dispatch-guard negative path: commit a throwaway test that submits a Task with a paraphrased writer brief. Assert `SUBAGENT_MISUSE` error.

---

## Phase 7 — Release v8.110.0 (15 min)

After Phase 6 validation:

```bash
make release-patch V=v8.110.0
```

Or the explicit version bump per Makefile convention. Confirm tag pushed, GH Actions built, binary artifact published.

---

## Phase 8 — Hand back to user for v38 commission

Edit [`../HANDOFF-to-I9-v38-prep.md`](../HANDOFF-to-I9-v38-prep.md) §1 Slots block with:
- `FIX_STACK_TAG: v8.110.0`
- `FIX_STACK_COMMITS: <7 SHAs>` (or the decomposed sub-commit list if Cx-5 split)
- `HARNESS_V2_LANDED: yes`

Notify user: fix stack shipped. v38 commission can proceed. **Do not commission v38 autonomously** — the user drives commissioning.

---

## Phase 9 — v38 analysis (after user commissions)

See [`../HANDOFF-to-I9-v38-prep.md`](../HANDOFF-to-I9-v38-prep.md) §5 (commission spec) + §6 (analysis discipline rules — inherited from I8 unchanged). Use the harness per the discipline. **Do not reproduce v37's mistake of accepting the first-look verdict — require every PASS claim to cite a bar or a file:line.**

---

## Appendix A — Cx-commit order rationale

| Order | Commit | Depends on | Parallel-safe with |
|---|---|---|---|
| 1 | Cx-WRITER-SCOPE-REDUCTION | — (atom edits only) | Cx-2, Cx-3, Cx-6 |
| 2 | Cx-SCAFFOLD-FRAGMENT-FRAMES | — (scaffold code + atom edit) | Cx-1, Cx-3, Cx-6 |
| 3 | Cx-ENV-COMMENT-PRINCIPLE | — (atom + check edits) | Cx-1, Cx-2, Cx-6 |
| 4 | Cx-MANIFEST-OVERLAY | Cx-1 (atom moved manifest path) | Cx-6 |
| 5 | Cx-SUBAGENT-BRIEF-BUILDER | Cx-1, Cx-2, Cx-3 (needs final atom contents to stitch) | — |
| 6 | Cx-VERSION-ANCHOR-SHARPEN | — | any |
| 7 | Cx-HARNESS-V2 | — | any |

Viable parallel track: Cx-1 + Cx-2 + Cx-3 + Cx-6 in one sitting; Cx-4 after Cx-1 lands; Cx-5 after the three atom commits land; Cx-7 anytime. Total wall time should be 2–3 days if one person drives, 1 day if parallel.

---

## Appendix B — What NOT to fix in this stack

Out of scope for v8.110.0:

- **Root README quality beyond what finalize emits**. Finalize already generates a serviceable root README from the plan; don't touch unless a v38 observation surfaces a defect.
- **Editorial-review hallucination of atom IDs** (main agent requesting `briefs.editorial-review.per-surface-checklist`). Cx-SUBAGENT-BRIEF-BUILDER side-effects this — once the main agent no longer paraphrases, it also no longer requests atoms by name because the engine serves them in one brief. If v38 surfaces residual hallucination, separate Cx after v38.
- **agent-browser wedging during close-browser-walk**. Environmental; not a recipe-workflow defect. Orthogonal fix.
- **Pushing recipe content to `zeropsio/recipes`**. Publish-pipeline; post-v38.
- **Minimal-tier validation**. Independent; v35.5 work has its own track.
- **Framework diversity** (laravel-showcase, python-showcase). v38 uses nestjs for A/B comparability with v34–v37.
- **Writer brief decomposition**. If Cx-5 lands but v38 still shows writer-compliance retry cycles ≥ 3 rounds, a "writer brief is too dense" fix may be needed — but that's a v39 question.

---

## Exit criteria for this plan

Plan is complete when:

- [ ] Phase 1 three atom commits merged with green `TestWriterAtoms_*` + `TestAppScaffold_*` + `TestEnvCommentFactuality_*`.
- [ ] Phase 2 manifest overlay merged with `TestOverlayManifest_*` green.
- [ ] Phase 3 subagent brief builder merged; four new tests green; dispatch-guard exercised in an ad-hoc run.
- [ ] Phase 4 version-anchor sharpen merged with three new tests green.
- [ ] Phase 5 harness-v2 merged with four new tests green.
- [ ] Phase 6 integration verification passed.
- [ ] Phase 7 v8.110.0 tagged + artifact published.
- [ ] Phase 8 slot block populated in HANDOFF-to-I9.
- [ ] Phase 9 v38 verdict shipped.

Archive to `plans/archive/v38-fix-stack.md` after Phase 9 verdict lands.
